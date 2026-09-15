package main

// The basemap, served by us.
//
// A PMTiles archive is one file: a header, a root directory, optional leaf
// directories and the tile data, all addressed by a Hilbert index. Reading a
// tile is two or three ReadAt calls, so the whole archive never enters memory
// and a 400 MB regional basemap costs a file handle.
//
// This is a reader, not the library: the format is small enough to write
// against the specification (PMTiles v3) and the alternative is a dependency
// the image would have to carry.

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	compNone = 1
	compGzip = 2
)

type pmHeader struct {
	rootOffset, rootLength uint64
	metaOffset, metaLength uint64
	leafOffset, leafLength uint64
	dataOffset, dataLength uint64
	internalComp           byte
	tileComp               byte
	tileType               byte
	minZoom, maxZoom       byte
}

// PMTiles is an open archive.
type PMTiles struct {
	f   *os.File
	h   pmHeader
	mu  sync.Mutex
	rt  []pmEntry // the root directory, read once
	et  string    // the ETag every tile of this archive carries
	siz int64
}

type pmEntry struct {
	tileID uint64
	offset uint64
	length uint32
	run    uint32
}

// OpenPMTiles opens an archive and reads its header and root directory.
func OpenPMTiles(path string) (*PMTiles, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	var buf [127]byte
	if _, err := f.ReadAt(buf[:], 0); err != nil {
		f.Close()
		return nil, err
	}
	if string(buf[0:7]) != "PMTiles" || buf[7] != 3 {
		f.Close()
		return nil, errors.New("not a PMTiles v3 archive")
	}
	u := func(off int) uint64 { return binary.LittleEndian.Uint64(buf[off : off+8]) }
	p := &PMTiles{f: f, siz: st.Size()}
	p.h = pmHeader{
		rootOffset: u(8), rootLength: u(16),
		metaOffset: u(24), metaLength: u(32),
		leafOffset: u(40), leafLength: u(48),
		dataOffset: u(56), dataLength: u(64),
		internalComp: buf[97], tileComp: buf[98], tileType: buf[99],
		minZoom: buf[100], maxZoom: buf[101],
	}
	root, err := p.directory(p.h.rootOffset, p.h.rootLength)
	if err != nil {
		f.Close()
		return nil, err
	}
	p.rt = root
	p.et = fmt.Sprintf(`"pm-%x-%x"`, st.ModTime().UnixNano(), st.Size())
	return p, nil
}

func (p *PMTiles) Close() error { return p.f.Close() }

// MinZoom and MaxZoom are what the archive holds, for the config.
func (p *PMTiles) MinZoom() int { return int(p.h.minZoom) }
func (p *PMTiles) MaxZoom() int { return int(p.h.maxZoom) }
func (p *PMTiles) Size() int64  { return p.siz }

// Gzipped says the tiles inside are gzip-encoded, which is what lets them be
// handed to the browser untouched with a Content-Encoding header.
func (p *PMTiles) Gzipped() bool { return p.h.tileComp == compGzip }
func (p *PMTiles) ETag() string  { return p.et }

func (p *PMTiles) read(off, length uint64) ([]byte, error) {
	if length == 0 || off+length > uint64(p.siz) {
		return nil, errors.New("pmtiles: read out of range")
	}
	b := make([]byte, length)
	if _, err := p.f.ReadAt(b, int64(off)); err != nil {
		return nil, err
	}
	return b, nil
}

func decompress(b []byte, kind byte) ([]byte, error) {
	if kind != compGzip {
		return b, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// directory reads and decodes one directory.
func (p *PMTiles) directory(off, length uint64) ([]pmEntry, error) {
	raw, err := p.read(off, length)
	if err != nil {
		return nil, err
	}
	raw, err = decompress(raw, p.h.internalComp)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(raw)
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, err
	}
	if n > 1<<24 {
		return nil, errors.New("pmtiles: directory too large")
	}
	out := make([]pmEntry, n)
	var last uint64
	for i := range out { // tile ids, delta encoded
		d, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, err
		}
		last += d
		out[i].tileID = last
	}
	for i := range out { // run lengths
		v, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, err
		}
		out[i].run = uint32(v)
	}
	for i := range out { // lengths
		v, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, err
		}
		out[i].length = uint32(v)
	}
	for i := range out { // offsets: 0 means "right after the previous one"
		v, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, err
		}
		if v == 0 && i > 0 {
			out[i].offset = out[i-1].offset + uint64(out[i-1].length)
		} else {
			out[i].offset = v - 1
		}
	}
	return out, nil
}

// find is the largest entry whose id is at most target.
func find(dir []pmEntry, target uint64) int {
	lo, hi := 0, len(dir)-1
	best := -1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case dir[mid].tileID == target:
			return mid
		case dir[mid].tileID < target:
			best = mid
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return best
}

// Tile answers one tile, exactly as it lies in the archive (still gzipped when
// the archive says so).
func (p *PMTiles) Tile(z, x, y int) ([]byte, bool, error) {
	if z < int(p.h.minZoom) || z > int(p.h.maxZoom) {
		return nil, false, nil
	}
	id := zxyToTileID(z, x, y)
	dir := p.rt
	for depth := 0; depth < 4; depth++ {
		i := find(dir, id)
		if i < 0 {
			return nil, false, nil
		}
		e := dir[i]
		if e.run == 0 { // a pointer to a leaf directory
			p.mu.Lock()
			leaf, err := p.directory(p.h.leafOffset+e.offset, uint64(e.length))
			p.mu.Unlock()
			if err != nil {
				return nil, false, err
			}
			dir = leaf
			continue
		}
		if id >= e.tileID+uint64(e.run) {
			return nil, false, nil
		}
		b, err := p.read(p.h.dataOffset+e.offset, uint64(e.length))
		if err != nil {
			return nil, false, err
		}
		return b, true, nil
	}
	return nil, false, errors.New("pmtiles: directory nesting too deep")
}

// zxyToTileID is the Hilbert index the format addresses tiles by.
func zxyToTileID(z, x, y int) uint64 {
	var acc uint64
	for t := 0; t < z; t++ {
		acc += uint64(1) << (2 * uint(t))
	}
	n := 1 << uint(z)
	rx, ry := 0, 0
	d := 0
	xx, yy := x, y
	for s := n / 2; s > 0; s /= 2 {
		rx, ry = 0, 0
		if xx&s > 0 {
			rx = 1
		}
		if yy&s > 0 {
			ry = 1
		}
		d += s * s * ((3 * rx) ^ ry)
		// rotate
		if ry == 0 {
			if rx == 1 {
				xx = s - 1 - xx
				yy = s - 1 - yy
			}
			xx, yy = yy, xx
		}
	}
	return acc + uint64(d)
}

// ------------------------------------------------------------------ handlers

// handleTile serves /map/tiles/{z}/{x}/{y}.pbf out of the archive.
func (s *server) handleTile(w http.ResponseWriter, r *http.Request) {
	if s.pm == nil {
		http.Error(w, "no basemap archive on this server", http.StatusServiceUnavailable)
		return
	}
	z, err1 := strconv.Atoi(r.PathValue("z"))
	x, err2 := strconv.Atoi(r.PathValue("x"))
	ys := strings.TrimSuffix(r.PathValue("y"), ".pbf")
	y, err3 := strconv.Atoi(ys)
	if err1 != nil || err2 != nil || err3 != nil || z < 0 || z > 22 || x < 0 || y < 0 {
		http.Error(w, "bad tile", http.StatusBadRequest)
		return
	}
	if n := 1 << uint(z); x >= n || y >= n {
		http.Error(w, "bad tile", http.StatusBadRequest)
		return
	}
	w.Header().Set("ETag", s.pm.ETag())
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, s.pm.ETag()) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	b, ok, err := s.pm.Tile(z, x, y)
	if err != nil {
		http.Error(w, "tile read failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		// An empty 204 is what a vector map expects where there is no data.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	if s.pm.Gzipped() {
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Write(b)
}

// handleMapFile serves the styles, the fonts and the sprites from the tiles
// directory: static files with their own cache lives.
func (s *server) handleMapFile(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/map/"+kind+"/")
		if rest == "" || strings.Contains(rest, "..") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		p := filepath.Join(s.tiles, kind, filepath.Clean("/"+rest))
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch {
		case strings.HasSuffix(p, ".json"):
			// A style or a sprite index: short, because it is the thing that
			// changes when the map is restyled.
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "public, max-age=300")
		case strings.HasSuffix(p, ".pbf"):
			// A glyph range: addressed by its own content, keep it for a year.
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case strings.HasSuffix(p, ".png"):
			// A sprite sheet or a relief tile: a day, the same as a vector tile.
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
		default:
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		http.ServeFile(w, r, p)
	}
}
