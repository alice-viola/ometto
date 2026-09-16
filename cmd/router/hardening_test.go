package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	queen "github.com/smartpricing/queen/clients/client-go"

	"ometto/internal/city"
	"ometto/internal/route"
)

// TestValidUser: an id is 128 bits of hex, and the short ids of the first
// weeks are refused — a favourite list is a list of where somebody goes.
func TestValidUser(t *testing.T) {
	good := []string{
		"0123456789abcdef0123456789abcdef",
		"ffffffffffffffffffffffffffffffff",
	}
	bad := []string{
		"", "alice", "smoketest1", "0123456789ABCDEF0123456789ABCDEF",
		"0123456789abcdef0123456789abcde", "0123456789abcdef0123456789abcdef0",
		"0123456789abcdef0123456789abcdeg", "../../etc/passwd",
	}
	for _, u := range good {
		if !validUser(u) {
			t.Errorf("%q refused", u)
		}
	}
	for _, u := range bad {
		if validUser(u) {
			t.Errorf("%q accepted", u)
		}
	}
}

// TestLimiter: sixty a minute, six hundred an hour, and the refusal says when
// to come back.
func TestLimiter(t *testing.T) {
	l := newLimiter()
	now := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	for i := 0; i < perMinute; i++ {
		if ok, _ := l.allow("1.2.3.4", now); !ok {
			t.Fatalf("refused at %d, the minute allows %d", i+1, perMinute)
		}
	}
	ok, retry := l.allow("1.2.3.4", now)
	if ok {
		t.Fatalf("the 61st request in a minute was allowed")
	}
	if retry < 1 || retry > 61 {
		t.Errorf("retryAfter is %d s", retry)
	}
	// Another client is not charged for it.
	if ok, _ := l.allow("5.6.7.8", now); !ok {
		t.Errorf("one client's burst refused another's request")
	}
	// The hour keeps counting across minutes: ten full minutes is six hundred
	// requests, and the six hundred and first is refused even though its own
	// minute is empty.
	l2 := newLimiter()
	for m := 0; m < perHour/perMinute; m++ {
		at := now.Add(time.Duration(m) * time.Minute)
		for i := 0; i < perMinute; i++ {
			if ok, _ := l2.allow("9.9.9.9", at); !ok {
				t.Fatalf("refused in minute %d, request %d", m, i+1)
			}
		}
	}
	at := now.Add(time.Duration(perHour/perMinute) * time.Minute)
	ok2, retry2 := l2.allow("9.9.9.9", at)
	if ok2 {
		t.Errorf("the hour budget never closed")
	}
	if retry2 < 1 {
		t.Errorf("no retryAfter on the hour refusal")
	}

	// An empty key (no user) is never charged.
	for i := 0; i < perMinute*2; i++ {
		if ok, _ := l.allow("", now); !ok {
			t.Fatalf("an empty key was rate limited")
		}
	}
}

// TestClientIP: X-Forwarded-For counts only where a proxy is trusted.
func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.9:34567"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := clientIP(r, false); got != "10.0.0.9" {
		t.Errorf("untrusted: %q, want the socket address", got)
	}
	if got := clientIP(r, true); got != "203.0.113.7" {
		t.Errorf("trusted: %q, want the left-most forwarded address", got)
	}
}

// ---------------------------------------------------------------- degraded

func tinyServer(t *testing.T) *server {
	t.Helper()
	r := &route.Region{Name: "tiny", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}}
	pts := [][2]float64{r.Pts[0], r.Pts[1]}
	r.Stretches = []*city.Stretch{{
		ID: "s0", Pts: pts, Cum: []float64{0, 1000}, Len: 1000, A: 0, B: 1,
		Cls: "residential", Name: "Via Prova", MTB: -1,
	}}
	r.Sat = []route.Sat{{}}
	r.Lift = []route.Lift{{}}
	g := route.Build(r)
	return &server{
		g: g, gc: route.NewGeocoder(nil), region: "taa", met: newMetrics(), log: newLogger("info"),
		brk: &brokerState{}, ipLim: newLimiter(), uLim: newLimiter(),
		qRoutes: "ometto.routes", qReplies: "ometto.replies", started: time.Now(),
	}
}

// TestDegradedAnswers: a broker that refuses does not stop the product. The
// route is computed here, said to be degraded, and the lists say why they are
// not available instead of pretending to be empty.
func TestDegradedAnswers(t *testing.T) {
	s := tinyServer(t)
	s.brk.fail(errors.New("429 Too Many Requests"))
	if !s.brk.isDegraded() {
		t.Fatalf("a refusing broker did not degrade the service")
	}
	lat0, lon0 := s.g.R.LatLon(0, 0)
	lat1, lon1 := s.g.R.LatLon(1000, 0)
	res, err := s.submit(context.TODO(), route.Request{
		Points: []route.Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "car",
	})
	if err != nil {
		t.Fatalf("degraded submit failed: %v", err)
	}
	if !res.Degraded {
		t.Errorf("the answer does not say it was computed without the broker")
	}
	if len(res.Routes) != 1 {
		t.Fatalf("no route while degraded: %s", res.Reason)
	}
	if res.Engine != "inmem" || res.Worker == "" {
		t.Errorf("answer is %+v", res)
	}
	// The lists refuse, with a reason and a 503.
	w := httptest.NewRecorder()
	if s.storeReady(w) {
		t.Errorf("the store claimed to be ready while degraded")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", w.Code)
	}
	if body := w.Body.String(); !bytes.Contains([]byte(body), []byte("degraded")) {
		t.Errorf("the refusal does not say why: %s", body)
	}
	// The backoff grows and then the broker comes back.
	b := &brokerState{}
	var last time.Duration
	for i := 0; i < 5; i++ {
		b.fail(errors.New("boom"))
		d := time.Until(b.nextProbe)
		if i > 0 && d < last {
			t.Errorf("probe %d waits %s, less than the previous %s", i, d, last)
		}
		last = d
	}
	if d := time.Until(b.nextProbe); d > 90*time.Second {
		t.Errorf("the backoff grew to %s, want a minute or so", d)
	}
	b.recovered()
	if b.isDegraded() {
		t.Errorf("recovered() left the service degraded")
	}
}

// ---------------------------------------------------------------- pmtiles

// writePMTiles builds the smallest archive that can be read: one tile at 0/0/0,
// no compression anywhere.
func writePMTiles(t *testing.T, path string, tile []byte) {
	t.Helper()
	var dir bytes.Buffer
	uv := func(v uint64) {
		var b [10]byte
		n := binary.PutUvarint(b[:], v)
		dir.Write(b[:n])
	}
	uv(1)                 // one entry
	uv(0)                 // tile id 0 (z0/x0/y0)
	uv(1)                 // run length
	uv(uint64(len(tile))) // length
	uv(1)                 // offset 0, stored as offset+1
	root := dir.Bytes()

	header := make([]byte, 127)
	copy(header, "PMTiles")
	header[7] = 3
	put := func(off int, v uint64) { binary.LittleEndian.PutUint64(header[off:off+8], v) }
	rootOff := uint64(127)
	dataOff := rootOff + uint64(len(root))
	put(8, rootOff)
	put(16, uint64(len(root)))
	put(24, dataOff) // metadata: empty, parked at the data offset
	put(32, 0)
	put(40, 0) // no leaf directories
	put(48, 0)
	put(56, dataOff)
	put(64, uint64(len(tile)))
	put(72, 1)
	put(80, 1)
	put(88, 1)
	header[96] = 1 // clustered
	header[97] = 1 // internal compression: none
	header[98] = 1 // tile compression: none
	header[99] = 1 // tile type: mvt
	header[100] = 0
	header[101] = 0
	out := append(append(header, root...), tile...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPMTiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "taa.pmtiles")
	want := []byte("a vector tile, honestly")
	writePMTiles(t, path, want)

	pm, err := OpenPMTiles(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pm.Close()
	if pm.MinZoom() != 0 || pm.MaxZoom() != 0 {
		t.Errorf("zooms %d..%d", pm.MinZoom(), pm.MaxZoom())
	}
	if pm.Gzipped() {
		t.Errorf("the fixture is not gzipped")
	}
	got, ok, err := pm.Tile(0, 0, 0)
	if err != nil || !ok {
		t.Fatalf("tile 0/0/0: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("tile is %q", got)
	}
	// Outside the archive's zooms: no tile, and no error.
	if _, ok, err := pm.Tile(5, 1, 1); ok || err != nil {
		t.Errorf("z5 answered ok=%v err=%v", ok, err)
	}

	// Through the handler: the bytes, an ETag, a day of cache, and 304 on the
	// way back.
	s := tinyServer(t)
	s.pm, s.tiles = pm, dir
	h := s.routes()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/map/tiles/0/0/0.pbf", nil))
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("handler: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") == "" || rec.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Errorf("headers: %v", rec.Header())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.mapbox-vector-tile" {
		t.Errorf("content type %q", ct)
	}
	req := httptest.NewRequest("GET", "/map/tiles/0/0/0.pbf", nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("a matching ETag got %d", rec2.Code)
	}
	// The styles' other local sources come off the same handler: the shaded
	// relief, the sprites and the glyphs, each with its own cache life.
	os.MkdirAll(filepath.Join(dir, "natural_earth", "2", "1"), 0o755)
	os.WriteFile(filepath.Join(dir, "natural_earth", "2", "1", "2.png"), []byte("PNG"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sprites"), 0o755)
	os.WriteFile(filepath.Join(dir, "sprites", "ofm.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "sprites", "ofm.png"), []byte("PNG"), 0o644)
	os.MkdirAll(filepath.Join(dir, "fonts", "Noto Sans Regular"), 0o755)
	os.WriteFile(filepath.Join(dir, "fonts", "Noto Sans Regular", "0-255.pbf"), []byte("GLYPH"), 0o644)
	for _, c := range []struct{ path, ctype, cache string }{
		{"/map/natural_earth/2/1/2.png", "image/png", "public, max-age=86400"},
		{"/map/sprites/ofm.png", "image/png", "public, max-age=86400"},
		{"/map/sprites/ofm.json", "application/json", "public, max-age=300"},
		{"/map/fonts/Noto%20Sans%20Regular/0-255.pbf", "application/x-protobuf", "public, max-age=31536000, immutable"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))
		if rec.Code != 200 {
			t.Errorf("%s -> %d", c.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != c.ctype {
			t.Errorf("%s content type %q, want %q", c.path, got, c.ctype)
		}
		if got := rec.Header().Get("Cache-Control"); got != c.cache {
			t.Errorf("%s cache-control %q, want %q", c.path, got, c.cache)
		}
	}
	// Nothing climbs out of the tiles directory.
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, httptest.NewRequest("GET", "/map/natural_earth/../../../etc/passwd", nil))
	if rec5.Code == 200 {
		t.Errorf("a path traversal was served")
	}

	// A tile the archive does not hold is 204, not 404: a vector map expects
	// an empty tile there.
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, httptest.NewRequest("GET", "/map/tiles/3/1/1.pbf", nil))
	if rec3.Code != http.StatusNoContent {
		t.Errorf("missing tile got %d", rec3.Code)
	}
	// Nonsense is refused.
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, httptest.NewRequest("GET", "/map/tiles/2/9/9.pbf", nil))
	if rec4.Code != http.StatusBadRequest {
		t.Errorf("x out of range got %d", rec4.Code)
	}
}

// TestTileIDs pins the Hilbert numbering the format addresses tiles by.
func TestTileIDs(t *testing.T) {
	cases := []struct {
		z, x, y int
		id      uint64
	}{
		{0, 0, 0, 0},
		// z1 in Hilbert order, which is not row order
		{1, 0, 0, 1}, {1, 0, 1, 2}, {1, 1, 1, 3}, {1, 1, 0, 4},
		{2, 0, 0, 5},
	}
	for _, c := range cases {
		if got := zxyToTileID(c.z, c.x, c.y); got != c.id {
			t.Errorf("z%d/%d/%d -> %d, want %d", c.z, c.x, c.y, got, c.id)
		}
	}
	// Every tile of a zoom has its own id, and they fill the zoom's block:
	// that is the property the archive's binary search depends on.
	for z := 0; z <= 6; z++ {
		n := 1 << uint(z)
		var base uint64
		for t := 0; t < z; t++ {
			base += uint64(1) << (2 * uint(t))
		}
		seen := map[uint64]bool{}
		for x := 0; x < n; x++ {
			for y := 0; y < n; y++ {
				id := zxyToTileID(z, x, y)
				if id < base || id >= base+uint64(n*n) {
					t.Fatalf("z%d/%d/%d -> %d, outside the zoom block [%d,%d)", z, x, y, id, base, base+uint64(n*n))
				}
				if seen[id] {
					t.Fatalf("z%d: id %d used twice", z, id)
				}
				seen[id] = true
			}
		}
	}
}

// ---------------------------------------------------------------- config

func TestConfigAndHeaders(t *testing.T) {
	s := tinyServer(t)
	s.build = buildInfo{OSMDate: "2026-09-10", SATDate: "2026-06-01", BuildDate: "2026-09-15"}
	t.Setenv("PUBLIC_URL", "https://omettomaps.com")
	t.Setenv("DISCLAIMER_VERSION", "2")
	c := s.config()
	if c.Public != "https://omettomaps.com" || c.DisclaimerVersion != "2" {
		t.Errorf("env did not reach the config: %+v", c)
	}
	if c.Data.OSM != "2026-09-10" || c.Data.SAT != "2026-06-01" || c.Data.Build != "2026-09-15" {
		t.Errorf("data dates are %+v", c.Data)
	}
	if c.Styles["light"] != "/map/styles/light.json" || c.Styles["dark"] != "/map/styles/dark.json" {
		t.Errorf("styles are %+v", c.Styles)
	}
	if c.Terrain.URL == "" || c.Terrain.Encoding != "terrarium" || c.Terrain.Attribution == "" {
		t.Errorf("terrain is %+v", c.Terrain)
	}
	if c.Basemap {
		t.Errorf("a server without an archive claims a basemap")
	}
	if h := terrainHost(c.Terrain.URL); h != "https://s3.amazonaws.com" {
		t.Errorf("terrain host is %q", h)
	}

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/config", nil))
	if rec.Code != 200 {
		t.Fatalf("config: %d", rec.Code)
	}
	h := rec.Header()
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers missing: %v", h)
	}
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "worker-src 'self' blob:", "https://s3.amazonaws.com", "frame-ancestors 'none'"} {
		if !bytes.Contains([]byte(csp), []byte(want)) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	// The bundle may be kept for a year; the page that names it may not.
	rec2 := httptest.NewRecorder()
	s.routes().ServeHTTP(rec2, httptest.NewRequest("GET", "/assets/app-abc123.js", nil))
	if cc := rec2.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("asset cache-control is %q", cc)
	}
	rec3 := httptest.NewRecorder()
	s.routes().ServeHTTP(rec3, httptest.NewRequest("GET", "/", nil))
	if cc := rec3.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("index cache-control is %q", cc)
	}
	// Nor may the service worker be kept: a stale one holds a browser on an
	// old app with no way in from outside to correct it.
	rec4 := httptest.NewRecorder()
	s.routes().ServeHTTP(rec4, httptest.NewRequest("GET", "/sw.js", nil))
	if cc := rec4.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("service worker cache-control is %q", cc)
	}
}

// TestMetricsExposition: the numbers a dashboard needs, in the format it reads.
func TestMetricsExposition(t *testing.T) {
	s := tinyServer(t)
	s.met.observe("/api/route", 200, 0.031)
	s.met.observe("/api/route", 504, 15)
	s.met.observe("/api/geocode", 200, 0.001)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 {
		t.Fatalf("metrics: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`ometto_requests_total{route="/api/route",status="200"} 1`,
		`ometto_requests_total{route="/api/route",status="504"} 1`,
		"ometto_route_duration_seconds_bucket{le=\"0.05\"} 1",
		"ometto_route_duration_seconds_count 2",
		"ometto_degraded 0",
		"ometto_graph_junctions 2",
		"go_goroutines",
		"process_uptime_seconds",
	} {
		if !bytes.Contains([]byte(body), []byte(want)) {
			t.Errorf("metrics lack %q", want)
		}
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("content type %q", ct)
	}
	// A tile is not a route class of its own, and never a metric per tile.
	if routeClass("/map/tiles/12/2145/1436.pbf") != "/map/tiles" {
		t.Errorf("tile class is %q", routeClass("/map/tiles/12/2145/1436.pbf"))
	}
	if routeClass("/api/history/abc") != "/api/history/{id}" {
		t.Errorf("history class is %q", routeClass("/api/history/abc"))
	}
}

// TestBuildInfoShapes: the pipeline writes osmExtractDate / satCadastreDate;
// the short names are read too, and the env vars stand in for both. The last
// case reads the file this tree actually carries, so a rename on either side
// fails here rather than on the page.
func TestBuildInfoShapes(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "long.json")
	os.WriteFile(long, []byte(`{"osmExtractDate":"2026-09-11","satCadastreDate":"2019-12-31","buildDate":"2026-09-15T07:46:51Z","region":"taa"}`), 0o644)
	bi := loadBuildInfo(long)
	if bi.OSMDate != "2026-09-11" || bi.SATDate != "2019-12-31" || bi.BuildDate == "" {
		t.Errorf("pipeline spelling not read: %+v", bi)
	}
	short := filepath.Join(dir, "short.json")
	os.WriteFile(short, []byte(`{"osmDate":"2026-01-01","satDate":"2025-01-01","buildDate":"x"}`), 0o644)
	if bi := loadBuildInfo(short); bi.OSMDate != "2026-01-01" || bi.SATDate != "2025-01-01" {
		t.Errorf("short spelling not read: %+v", bi)
	}
	t.Setenv("OSM_DATE", "from-env")
	t.Setenv("SAT_DATE", "also-env")
	if bi := loadBuildInfo(filepath.Join(dir, "absent.json")); bi.OSMDate != "from-env" || bi.SATDate != "also-env" {
		t.Errorf("env fallback not read: %+v", bi)
	}
	// The real one, when this tree has it.
	real := loadBuildInfo("../../data/build-info.json")
	if real.BuildDate == "" {
		t.Skip("no data/build-info.json in this tree")
	}
	if real.OSMDate == "" || real.SATDate == "" {
		t.Errorf("the delivered build-info gives no dates: %+v", real)
	}
}

// TestStaticTypes: the page's own files go out with the types a browser needs
// — a manifest served as text/plain is one a browser will not install from.
func TestStaticTypes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>Ometto</title>"), 0o644)
	os.WriteFile(filepath.Join(dir, "site.webmanifest"), []byte(`{"name":"Ometto"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "favicon.svg"), []byte("<svg/>"), 0o644)
	os.WriteFile(filepath.Join(dir, "sw.js"), []byte("self.addEventListener('fetch',()=>{})"), 0o644)
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644)

	s := tinyServer(t)
	s.static = dir
	h := s.routes()
	for _, c := range []struct{ path, ctype, cache string }{
		{"/site.webmanifest", "application/manifest+json", ""},
		{"/favicon.svg", "image/svg+xml", ""},
		{"/assets/app-abc123.js", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"},
		{"/sw.js", "text/javascript; charset=utf-8", "no-cache"},
		{"/", "text/html; charset=utf-8", "no-cache"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))
		if rec.Code != 200 {
			t.Errorf("%s -> %d", c.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != c.ctype {
			t.Errorf("%s content type %q, want %q", c.path, got, c.ctype)
		}
		if c.cache != "" {
			if got := rec.Header().Get("Cache-Control"); got != c.cache {
				t.Errorf("%s cache-control %q, want %q", c.path, got, c.cache)
			}
		}
	}
}

// TestTunnelBehaviour: behind a Cloudflare tunnel the edge's own header is the
// client, both hostnames arrive at this one service, and the numbers never
// leave the machine.
func TestTunnelBehaviour(t *testing.T) {
	// The client is CF-Connecting-IP first, X-Forwarded-For second, and the
	// socket when nothing is trusted.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.18.0.4:51234" // the tunnel container, on the compose network
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")
	r.Header.Set("X-Forwarded-For", "198.51.100.1, 172.18.0.4")
	if got := clientIP(r, true); got != "203.0.113.9" {
		t.Errorf("trusted: %q, want the Cloudflare address", got)
	}
	if got := clientIP(r, false); got != "172.18.0.4" {
		t.Errorf("untrusted: %q, want the socket address", got)
	}
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "172.18.0.4:51234"
	r2.Header.Set("X-Forwarded-For", "198.51.100.1")
	if got := clientIP(r2, true); got != "198.51.100.1" {
		t.Errorf("without the Cloudflare header: %q, want the forwarded one", got)
	}

	// www is a redirect to the apex, not a second site.
	t.Setenv("DOMAIN", "omettomaps.com")
	s := tinyServer(t)
	h := s.routes()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = "www.omettomaps.com"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("www answered %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://omettomaps.com/api/health" {
		t.Errorf("redirected to %q", loc)
	}
	// The apex itself is served, not redirected.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/health", nil)
	req2.Host = "omettomaps.com"
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Errorf("the apex answered %d", rec2.Code)
	}
	// A www of some other domain is left alone: this service is not a
	// redirector for the internet.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/api/health", nil)
	req3.Host = "www.example.org"
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Errorf("www.example.org answered %d, want the page", rec3.Code)
	}

	// /metrics is for the machine. Through the tunnel it does not exist.
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest("GET", "/metrics", nil)
	req4.Host = "omettomaps.com"
	req4.Header.Set("CF-Connecting-IP", "203.0.113.9")
	h.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusNotFound {
		t.Errorf("/metrics through the tunnel answered %d, want 404", rec4.Code)
	}
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, httptest.NewRequest("GET", "/metrics", nil))
	if rec5.Code != 200 {
		t.Errorf("/metrics from the machine answered %d", rec5.Code)
	}
}

// TestDegradedNamesTheOperation: "HTTP 403 forbidden" on its own sends an
// operator looking through every call the service makes. The message has to
// say WHICH one was refused — the credential on a shared tenant may push and
// pop and still not be allowed to configure a queue.
func TestDegradedNamesTheOperation(t *testing.T) {
	forbidden := errors.New(`HTTP 403 [forbidden]: {"code":"forbidden","error":"operation not permitted for this credential"}`)

	b := &brokerState{}
	b.failOp("configure ometto.routes", forbidden)
	_, msg, _ := b.snapshot()
	if !strings.HasPrefix(msg, "configure ometto.routes: HTTP 403") {
		t.Errorf("lastError is %q", msg)
	}

	// The label is not repeated when the error already carries it, and the
	// SDK's own prefix is dropped.
	b2 := &brokerState{}
	b2.failOp("configure ometto.routes", errors.New("queenx: configure ometto.routes: "+forbidden.Error()))
	_, msg2, _ := b2.snapshot()
	if strings.Count(msg2, "configure ometto.routes") != 1 {
		t.Errorf("the operation is named twice: %q", msg2)
	}
	if strings.Contains(msg2, "queenx:") {
		t.Errorf("the SDK's prefix survived: %q", msg2)
	}

	// Every call site that can degrade the service names itself.
	for _, op := range []string{
		"health https://try.queenmq.cloud/health",
		"push ometto.routes/taa",
		"pop ometto.replies/9f2c",
		"pop ometto.routes/taa",
	} {
		b3 := &brokerState{}
		b3.failOp(op, forbidden)
		_, m, _ := b3.snapshot()
		if !strings.HasPrefix(m, op+": ") {
			t.Errorf("%q -> %q", op, m)
		}
	}

	// And it reaches health, which is where an operator looks first.
	s := tinyServer(t)
	s.brk.failOp("configure ometto.routes", forbidden)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	q, _ := got["queue"].(map[string]interface{})
	if q["degraded"] != true {
		t.Fatalf("health does not report degraded: %v", q)
	}
	le, _ := q["lastError"].(string)
	if !strings.HasPrefix(le, "configure ometto.routes: ") {
		t.Errorf("health lastError is %q", le)
	}
	if got["ok"] != true {
		t.Errorf("health is not ok while routing works")
	}
}

// TestAdminRefusalIsNotAFailure: the decision table a cloud tenant needs.
// A credential that may push, pop and use the KV is the right credential for
// a deployed app; that it may not read /health or configure a queue says
// nothing about whether the app works.
func TestAdminRefusalIsNotAFailure(t *testing.T) {
	forbidden := &queen.HTTPError{StatusCode: 403, Code: "forbidden",
		Body: `{"code":"forbidden","error":"operation not permitted for this credential"}`}
	if !isForbidden(forbidden) {
		t.Fatalf("a 403 was not recognised")
	}
	if !isForbidden(fmt.Errorf("queenx: configure ometto.routes: %w", forbidden)) {
		t.Errorf("a wrapped 403 was not recognised")
	}
	for _, err := range []error{
		errors.New("context deadline exceeded"),
		&queen.HTTPError{StatusCode: 429},
		&queen.HTTPError{StatusCode: 500},
	} {
		if isForbidden(err) {
			t.Errorf("%v was taken for a refusal", err)
		}
	}

	// A refused admin call is recorded once and degrades nothing.
	s := tinyServer(t)
	s.brk.adminDenied("configure ometto.routes")
	s.brk.adminDenied("depth ometto.routes") // the first one is the one kept
	if s.brk.isDegraded() {
		t.Errorf("an admin refusal degraded the service")
	}
	if note := s.brk.adminNote(); note != "not permitted: configure ometto.routes" {
		t.Errorf("admin note is %q", note)
	}

	// Health says so, stays ok, stays undegraded, and drops the depth it
	// cannot read rather than reporting a zero that reads as "empty".
	s.depth, s.depthAt = -1, time.Now()
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))
	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	q := got["queue"].(map[string]interface{})
	if got["ok"] != true || q["degraded"] != false {
		t.Errorf("health is %v", got)
	}
	if q["admin"] != "not permitted: configure ometto.routes" {
		t.Errorf("queue.admin is %v", q["admin"])
	}
	if _, ok := q["pending"]; ok {
		t.Errorf("queue.pending is reported though the depth is unknown: %v", q["pending"])
	}

	// A data-plane failure still degrades: that is the difference.
	s2 := tinyServer(t)
	s2.brk.failOp("push ometto.routes/taa", &queen.HTTPError{StatusCode: 403})
	if !s2.brk.isDegraded() {
		t.Errorf("a refused push did not degrade the service")
	}
}

// TestReplyFitsTheBroker: a route is mostly geometry, and this tenant's broker
// refuses a message over 256 KB. The answer travels compressed; one that will
// not fit even then is refused by name, so the handler can compute it again
// instead of failing the request.
func TestReplyFitsTheBroker(t *testing.T) {
	big := &routeResp{Engine: "inmem", Worker: "taa/1", ComputedMs: 12.5}
	// ~360 KB of coordinates, which is what a three-alternative crossing of
	// the region actually weighs.
	coords := make([][2]float64, 12000)
	for i := range coords {
		coords[i] = [2]float64{11.1213 + float64(i)*1e-6, 46.0669 + float64(i)*1e-6}
	}
	// The line rides on the legs: the route's own copy is not on the wire.
	big.Routes = []*route.Route{{ID: "r1", Seconds: 2339, Meters: 49442,
		Legs: []*route.Leg{{Mode: "car", Geometry: route.Geometry{Type: "LineString", Coordinates: coords}}}}}
	raw, _ := json.Marshal(big)
	if len(raw) < 256<<10 {
		t.Fatalf("the fixture is %d bytes: too small to be the case", len(raw))
	}

	var rep routeReply
	pack(&rep, big)
	if !rep.OK || rep.GZ == "" {
		t.Fatalf("a 360 KB answer was refused: %q", rep.Error)
	}
	if len(rep.GZ) >= 256<<10 {
		t.Errorf("the compressed reply is %d bytes, over the broker's limit", len(rep.GZ))
	}
	back, err := unpack(&rep)
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if len(back.Routes) != 1 || len(back.Routes[0].Legs) != 1 || len(back.Routes[0].Legs[0].Geometry.Coordinates) != len(coords) {
		t.Fatalf("the answer did not survive the round trip")
	}
	if back.Routes[0].Meters != big.Routes[0].Meters || back.Worker != "taa/1" {
		t.Errorf("the answer came back changed: %+v", back.Routes[0])
	}

	// An answer nothing can fit is named, not silently lost.
	huge := &routeResp{Engine: "inmem"}
	var many [][2]float64
	for i := 0; i < 4_000_000; i++ { // incompressible-ish: every point differs
		many = append(many, [2]float64{float64(i) * 1.000001, float64(i) * 0.999999})
	}
	huge.Routes = []*route.Route{{ID: "r1", Legs: []*route.Leg{{Mode: "car", Geometry: route.Geometry{Type: "LineString", Coordinates: many}}}}}
	var rep2 routeReply
	pack(&rep2, huge)
	if rep2.OK || rep2.Error != errTooLarge {
		t.Errorf("an unsendable answer came back as %v/%q", rep2.OK, rep2.Error)
	}

	// The old shape still reads: a reply carrying `result` needs no unpacking.
	plain := routeReply{ID: "x", OK: true, Result: &routeResp{Worker: "taa/9"}}
	if got, err := unpack(&plain); err != nil || got.Worker != "taa/9" {
		t.Errorf("a plain reply did not survive: %v %v", got, err)
	}
}

// A partition is permanent and a tenant is allowed a handful of them; the
// cloud broker refused the ninth answer with "partition limit reached (8)"
// after eight routes, because every request had been given a partition of its
// own. The address is the instance's, and it is stable across restarts.
func TestReplyAddressIsTheInstanceAndIsStable(t *testing.T) {
	if got := replyAddress("", "taa"); got != "taa" {
		t.Fatalf("default address = %q, want the region", got)
	}
	if got := replyAddress("  ", "taa"); got != "taa" {
		t.Fatalf("blank address = %q, want the region", got)
	}
	if got := replyAddress("box-2", "taa"); got != "box-2" {
		t.Fatalf("given address = %q, want it honoured", got)
	}
	if got := replyAddress("", ""); got != "default" {
		t.Fatalf("no region = %q, want a name anyway", got)
	}
}

// One line of answers, many waiters: each answer must reach the request that
// is waiting for it, and an answer nobody waits for must not block the reader.
func TestAnswersReachTheirWaiter(t *testing.T) {
	s := &server{}
	a, b := s.expect("aaa"), s.expect("bbb")
	if s.deliver(&routeReply{ID: "ccc"}) {
		t.Fatal("an answer nobody waits for was delivered")
	}
	if !s.deliver(&routeReply{ID: "bbb", OK: true}) {
		t.Fatal("the answer did not reach its waiter")
	}
	select {
	case rep := <-b:
		if rep.ID != "bbb" {
			t.Fatalf("waiter b got %q", rep.ID)
		}
	default:
		t.Fatal("waiter b got nothing")
	}
	select {
	case <-a:
		t.Fatal("waiter a was handed another request's answer")
	default:
	}
	s.forget("aaa")
	if s.deliver(&routeReply{ID: "aaa"}) {
		t.Fatal("a forgotten waiter still took an answer")
	}
}

// A late answer must not wedge the reader: the waiter has gone home, the
// channel is full, or both.
func TestALateAnswerIsDropped(t *testing.T) {
	s := &server{}
	ch := s.expect("aaa")
	if !s.deliver(&routeReply{ID: "aaa"}) {
		t.Fatal("first answer refused")
	}
	done := make(chan bool, 1)
	go func() { done <- s.deliver(&routeReply{ID: "aaa"}) }()
	select {
	case took := <-done:
		if took {
			t.Fatal("a second answer was accepted into a full channel")
		}
	case <-time.After(time.Second):
		t.Fatal("deliver blocked on a full channel")
	}
	<-ch
}
