package main

// The HTTP surface. This file IS the contract — the endpoints are listed in
// README.md and the shapes are the types in internal/route; this file is its only
// implementation. Everything is JSON, CORS is open (the frontend is served
// from anywhere during development), and every coordinate on the wire is
// WGS84 lat/lon.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	queen "github.com/smartpricing/queen/clients/client-go"

	"ometto/internal/queenx"
	"ometto/internal/route"
)

type server struct {
	g      *route.Graph
	gc     *route.Geocoder
	qx     *queenx.Client
	raw    *queen.Queen
	store  *store
	region string
	layers string
	static string
	tiles  string
	broker string
	pid    int

	// the queue names and the KV namespace, prefixed so that a shared tenant
	// on Queen Cloud cannot collide with anything else in it
	qRoutes  string
	qReplies string
	// replyAddr is this instance's address on `replies`: one partition for
	// the whole process, stable across restarts, so the tenant's partition
	// budget is not spent one route at a time.
	replyAddr string
	waitMu    sync.RWMutex
	waiting   map[string]chan *routeReply

	pm    *PMTiles
	build buildInfo
	met   *metrics
	log   *logger
	brk   *brokerState
	ipLim *limiter
	uLim  *limiter

	trustProxy bool

	started  time.Time
	inflight int64
	computed int64

	depthMu sync.Mutex
	depthAt time.Time
	depth   int
}

// Go's table has no entry for a web app manifest, and a sniffed manifest is
// served as text, which some browsers then refuse to install from.
func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".pmtiles", "application/vnd.pmtiles")
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /map/tiles/{z}/{x}/{y}", s.handleTile)
	mux.HandleFunc("GET /map/styles/", s.handleMapFile("styles"))
	mux.HandleFunc("GET /map/fonts/", s.handleMapFile("fonts"))
	mux.HandleFunc("GET /map/sprites/", s.handleMapFile("sprites"))
	// The styles' second local source: the shaded relief the basemap is drawn
	// over, 56 PNG tiles at z0-6, served like the fonts and the sprites.
	mux.HandleFunc("GET /map/natural_earth/", s.handleMapFile("natural_earth"))
	mux.HandleFunc("GET /api/geocode", s.handleGeocode)
	mux.HandleFunc("GET /api/reverse", s.handleReverse)
	mux.HandleFunc("POST /api/route", s.handleRoute)

	mux.HandleFunc("GET /api/favorites", s.handleFavoritesGet)
	mux.HandleFunc("POST /api/favorites", s.handleFavoritesPost)
	mux.HandleFunc("DELETE /api/favorites/{id}", s.handleFavoritesDelete)

	mux.HandleFunc("GET /api/history", s.handleHistoryList)
	mux.HandleFunc("GET /api/history/{id}", s.handleHistoryGet)
	mux.HandleFunc("DELETE /api/history/{id}", s.handleHistoryDelete)
	mux.HandleFunc("DELETE /api/history", s.handleHistoryClear)

	mux.HandleFunc("GET /api/layers/{name}", s.handleLayer)
	mux.HandleFunc("/", s.handleStatic)
	return s.logging(s.secure(cors(mux)))
}

// secure puts the headers a public page needs on every answer, and the cache
// lives on the ones a browser should keep. The bundle is hashed by the build,
// so it may be kept for a year; index.html names the bundle and may not be
// kept at all.
func (s *server) secure(next http.Handler) http.Handler {
	csp := s.csp()
	apex := strings.TrimSpace(envOr("DOMAIN", ""))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The tunnel sends both hostnames to this one service, so the www one
		// is answered with a redirect rather than a second copy of the site.
		if host := hostOnly(r.Host); strings.HasPrefix(host, "www.") {
			if bare := strings.TrimPrefix(host, "www."); apex == "" || bare == apex {
				u := *r.URL
				u.Host, u.Scheme = bare, "https"
				http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
				return
			}
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", csp)
		switch p := r.URL.Path; {
		case strings.HasPrefix(p, "/assets/"):
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		case p == "/" || strings.HasSuffix(p, "/index.html"):
			h.Set("Cache-Control", "no-cache")
		case p == "/sw.js":
			// A service worker that is served stale keeps a browser on an old
			// app for as long as the cache lives, and there is no way in from
			// the outside to correct it.
			h.Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// csp allows this origin, the terrain host the config names, and the blobs a
// vector map needs for its workers and its canvas.
func (s *server) csp() string {
	// The relief tiles, the sprites, the glyphs and the vector tiles are all
	// this origin; only the terrain is somebody else's.
	hosts := ""
	if h := terrainHost(s.config().Terrain.URL); h != "" {
		hosts = " " + h
	}
	return strings.Join([]string{
		"default-src 'self'",
		"img-src 'self' data: blob:" + hosts,
		"connect-src 'self' data: blob:" + hosts,
		"worker-src 'self' blob:",
		"child-src 'self' blob:",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"font-src 'self' data:",
		"object-src 'none'",
		"base-uri 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

// ------------------------------------------------------------- middleware

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		h.Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	n      int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, err := w.ResponseWriter.Write(b)
	w.n += n
	return n, err
}

// logging measures every request and writes a JSON line for the ones worth a
// line: a map pan is two hundred tiles, and none of them is news.
func (s *server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t0 := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = 200
		}
		took := time.Since(t0)
		class := routeClass(r.URL.Path)
		s.met.observe(class, sw.status, took.Seconds())
		if class == "/map/tiles" || class == "/map/static" || class == "/static" {
			if sw.status < 400 && !s.log.debug {
				return
			}
		}
		level := "info"
		if sw.status >= 500 {
			level = "error"
		} else if sw.status >= 400 {
			level = "warn"
		}
		s.log.write(logLine{Level: level, Msg: "request", Method: r.Method, Path: r.URL.Path,
			Route: class, Status: sw.status, Bytes: sw.n,
			Ms: float64(took.Microseconds()) / 1000, IP: clientIP(r, s.trustProxy)})
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		log.Printf("ometto: encode: %v", err)
	}
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// tooMany is the answer to a client over its budget, with the seconds until it
// may ask again.
func (s *server) tooMany(w http.ResponseWriter, retry int) {
	if retry < 1 {
		retry = 1
	}
	s.met.mu.Lock()
	s.met.rateLimited++
	s.met.mu.Unlock()
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
		"error": "too many requests", "retryAfter": retry,
	})
}

// storeReady refuses the lists while the broker is away: favourites and
// history live in its KV, and a silent empty list would read as "they are
// gone".
func (s *server) storeReady(w http.ResponseWriter) bool {
	if !s.brk.isDegraded() {
		return true
	}
	_, lastErr, _ := s.brk.snapshot()
	writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
		"error":    "favourites and history are unavailable while the broker is unreachable",
		"degraded": true, "reason": lastErr,
	})
	return false
}

// ------------------------------------------------------------------ health

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	degraded, lastErr, lastAt := s.brk.snapshot()
	queue := map[string]interface{}{
		"inflight": s.inflight,
		"computed": s.computed,
		"degraded": degraded,
	}
	// The depth is the broker's to tell; when this credential may not ask,
	// the field is absent rather than zero — zero would read as "empty".
	if p := s.pending(r.Context()); p >= 0 {
		queue["pending"] = p
	}
	if note := s.brk.adminNote(); note != "" {
		queue["admin"] = note
	}
	if degraded {
		queue["lastError"] = lastErr
		queue["lastErrorAt"] = lastAt.UTC().Format(time.RFC3339)
	}
	basemap := map[string]interface{}{"tiles": s.pm != nil}
	if s.pm != nil {
		basemap["minZoom"], basemap["maxZoom"] = s.pm.MinZoom(), s.pm.MaxZoom()
		basemap["bytes"] = s.pm.Size()
	}
	// ok stays true while routing works: the queue is how the answer travels,
	// not whether there is one.
	writeJSON(w, 200, map[string]interface{}{
		"ok":     true,
		"region": s.region,
		"graph": map[string]interface{}{
			"junctions":       s.g.Junctions,
			"stretches":       len(s.g.R.Stretches) - s.g.R.Links,
			"withElevation":   s.g.R.WithElevation,
			"directedEdges":   s.g.DirectedEdges,
			"geocoderEntries": s.gc.Len(),
			"lifts": map[string]interface{}{
				"loaded": s.g.LiftsLoaded,
				"linked": s.g.LiftsLinked,
			},
		},
		"queue": queue,
		"data": map[string]interface{}{
			"osm": s.build.OSMDate, "sat": s.build.SATDate, "build": s.build.BuildDate,
		},
		"basemap": basemap,
		"map":     filepath.Base(s.g.R.File),
		"broker":  s.broker,
		"worker":  s.workerName(),
		"uptimeS": math.Round(time.Since(s.started).Seconds()),
	})
}

// pending is the broker's own depth of the routes queue, at most one read a
// second: it is a health line, not a metric.
func (s *server) pending(ctx context.Context) int {
	s.depthMu.Lock()
	defer s.depthMu.Unlock()
	if time.Since(s.depthAt) < time.Second {
		return s.depth
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if s.raw == nil || s.brk.isDegraded() {
		return 0
	}
	res, err := s.raw.Admin().GetQueueDepth(c, s.qRoutes, "")
	s.depthAt = time.Now()
	if err != nil {
		if isForbidden(err) {
			// Not ours to read on this tenant. Say so once, and stop
			// reporting a number we cannot know.
			s.brk.adminDenied("depth " + s.qRoutes)
			s.depth = -1
		}
		return s.depth
	}
	switch v := res["pending"].(type) {
	case float64:
		s.depth = int(v)
	case int:
		s.depth = v
	}
	return s.depth
}

// ----------------------------------------------------------------- geocode

func (s *server) handleGeocode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	res := s.gc.Search(q, limit)
	if res == nil {
		res = []*route.Entry{}
	}
	writeJSON(w, 200, map[string]interface{}{"results": res})
}

func (s *server) handleReverse(w http.ResponseWriter, r *http.Request) {
	lat, err1 := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lon, err2 := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if err1 != nil || err2 != nil {
		fail(w, 400, "lat and lon must be numbers")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "car"
	}
	if !route.ValidMode(mode) {
		fail(w, 400, "mode must be one of car, bike, hike, car+hike, bike+hike")
		return
	}
	plan := route.ParsePlan(mode)
	x, y := s.g.R.XY(lat, lon)
	n := s.g.Nearest(x, y, "to", plan, 10000)
	if n < 0 {
		fail(w, 404, "no "+mode+" road within 10 km")
		return
	}
	nlat, nlon := s.g.LatLon(n)
	writeJSON(w, 200, map[string]interface{}{
		"name": s.g.NameAt(n, plan), "kind": s.g.KindAt(n),
		"lat": round6(nlat), "lon": round6(nlon),
	})
}

// ------------------------------------------------------------------- route

func (s *server) handleRoute(w http.ResponseWriter, r *http.Request) {
	// The budget, before the body: a refused request should cost a socket and
	// nothing else.
	now := time.Now()
	ip := clientIP(r, s.trustProxy)
	if ok, retry := s.ipLim.allow(ip, now); !ok {
		s.tooMany(w, retry)
		return
	}
	var req route.Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := dec.Decode(&req); err != nil {
		fail(w, 400, "bad json: "+err.Error())
		return
	}
	stops := 0
	for _, p := range req.Points {
		if !p.Via {
			stops++
		}
	}
	if stops < 2 || stops > maxStops {
		fail(w, 400, fmt.Sprintf("points must hold between 2 and %d stops (a point with via:true is not a stop)", maxStops))
		return
	}
	if len(req.Points) > maxEntries {
		fail(w, 400, fmt.Sprintf("points must hold at most %d entries, vias included", maxEntries))
		return
	}
	if len(req.Avoid) > maxAvoids {
		fail(w, 400, fmt.Sprintf("avoid must hold at most %d places", maxAvoids))
		return
	}
	for i, a := range req.Avoid {
		if a.Lat < -90 || a.Lat > 90 || a.Lon < -180 || a.Lon > 180 || (a.Lat == 0 && a.Lon == 0) {
			fail(w, 400, fmt.Sprintf("avoid %d is not a coordinate", i+1))
			return
		}
	}
	for i, p := range req.Points {
		if p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 || (p.Lat == 0 && p.Lon == 0) {
			fail(w, 400, fmt.Sprintf("point %d is not a coordinate", i+1))
			return
		}
	}
	if req.Mode == "" {
		req.Mode = "car"
	}
	if !route.ValidMode(req.Mode) {
		fail(w, 400, "mode must be one of car, bike, hike, car+hike, bike+hike")
		return
	}
	if req.Grade == "" {
		req.Grade = "E"
	}
	if route.SatGrade(req.Grade) == 0 {
		fail(w, 400, "grade must be one of T, E, EE, EEA, A")
		return
	}
	if req.Alternatives != 1 && req.Alternatives != 3 {
		if req.Alternatives == 0 {
			req.Alternatives = 1
		} else {
			fail(w, 400, "alternatives must be 1 or 3")
			return
		}
	}
	user := req.User
	req.User = ""
	if user != "" {
		if !validUser(user) {
			fail(w, 400, "user must be 32 lowercase hex characters")
			return
		}
		if ok, retry := s.uLim.allow("u:"+user, now); !ok {
			s.tooMany(w, retry)
			return
		}
	}

	res, err := s.submit(r.Context(), req)
	if err != nil {
		if errors.Is(err, errTimeout) {
			fail(w, 504, "the queue did not answer in 15 s")
			return
		}
		if r.Context().Err() != nil {
			return
		}
		fail(w, 502, err.Error())
		return
	}
	if user != "" && !s.brk.isDegraded() {
		s.remember(r.Context(), user, req, res)
	}
	writeJSON(w, 200, res)
}

// remember appends the request and the answer to a user's history.
func (s *server) remember(ctx context.Context, user string, req route.Request, res *routeResp) {
	it := HistItem{
		ID: newID(), At: time.Now().UTC().Format(time.RFC3339),
		Request: HistRequest{Points: req.Points, Mode: req.Mode, Grade: req.Grade,
			Alternatives: req.Alternatives, Avoid: req.Avoid, Lifts: req.Lifts},
	}
	if len(res.Snapped) > 0 {
		it.Summary.FromName = res.Snapped[0].Name
		it.Summary.ToName = res.Snapped[len(res.Snapped)-1].Name
	}
	if len(res.Routes) > 0 {
		r0 := res.Routes[0]
		it.Summary.Seconds, it.Summary.Meters = r0.Seconds, r0.Meters
		it.Summary.Ascent, it.Summary.Descent = r0.Ascent, r0.Descent
	}
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.AddHistory(c, user, it); err != nil {
		log.Printf("ometto: history %s: %v", user, err)
	}
}

// -------------------------------------------------------------- favourites

func (s *server) handleFavoritesGet(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if !validUser(user) {
		fail(w, 400, "user must be 32 lowercase hex characters")
		return
	}
	if !s.storeReady(w) {
		return
	}
	list, err := s.store.Favorites(r.Context(), user)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]interface{}{"favorites": list})
}

func (s *server) handleFavoritesPost(w http.ResponseWriter, r *http.Request) {
	var in struct {
		User string  `json:"user"`
		Name string  `json:"name"`
		Lat  float64 `json:"lat"`
		Lon  float64 `json:"lon"`
		Kind string  `json:"kind"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		fail(w, 400, "bad json: "+err.Error())
		return
	}
	user := in.User
	if !validUser(user) {
		fail(w, 400, "user must be 32 lowercase hex characters")
		return
	}
	if !s.storeReady(w) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		fail(w, 400, "name is required")
		return
	}
	if in.Lat < -90 || in.Lat > 90 || in.Lon < -180 || in.Lon > 180 {
		fail(w, 400, "lat and lon must be a coordinate")
		return
	}
	f, err := s.store.AddFavorite(r.Context(), user, Fav{Name: in.Name, Lat: in.Lat, Lon: in.Lon, Kind: in.Kind})
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, f)
}

func (s *server) handleFavoritesDelete(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if !validUser(user) {
		fail(w, 400, "user must be 32 lowercase hex characters")
		return
	}
	if !s.storeReady(w) {
		return
	}
	if err := s.store.DeleteFavorite(r.Context(), user, r.PathValue("id")); err != nil {
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ----------------------------------------------------------------- history

func (s *server) handleHistoryList(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if !validUser(user) {
		fail(w, 400, "user must be 32 lowercase hex characters")
		return
	}
	if !s.storeReady(w) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > maxHistory {
		limit = 50
	}
	list, err := s.store.History(r.Context(), user, limit)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]interface{}{"history": list})
}

func (s *server) handleHistoryGet(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if !validUser(user) {
		fail(w, 400, "user must be 32 lowercase hex characters")
		return
	}
	if !s.storeReady(w) {
		return
	}
	list, err := s.store.History(r.Context(), user, 0)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	id := r.PathValue("id")
	for _, it := range list {
		if it.ID == id {
			writeJSON(w, 200, it)
			return
		}
	}
	fail(w, 404, "no such history entry")
}

func (s *server) handleHistoryDelete(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if !validUser(user) {
		fail(w, 400, "user must be 32 lowercase hex characters")
		return
	}
	if !s.storeReady(w) {
		return
	}
	if err := s.store.DeleteHistory(r.Context(), user, r.PathValue("id")); err != nil {
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) handleHistoryClear(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if !validUser(user) {
		fail(w, 400, "user must be 32 lowercase hex characters")
		return
	}
	if !s.storeReady(w) {
		return
	}
	if err := s.store.ClearHistory(r.Context(), user); err != nil {
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ------------------------------------------------------------------ layers

// handleLayer serves <layers>/<name>.geojson. The name is a bare word: no
// slashes, no dots, so it cannot walk out of the directory.
func (s *server) handleLayer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	name = strings.TrimSuffix(name, ".geojson")
	if name == "" || strings.ContainsAny(name, `/\.`) {
		fail(w, 400, "layer name must be a single word")
		return
	}
	p := filepath.Join(s.layers, name+".geojson")
	f, err := os.Open(p)
	if err != nil {
		fail(w, 404, "no layer "+name)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		fail(w, 404, "no layer "+name)
		return
	}
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, name+".geojson", st.ModTime(), f)
}

// ------------------------------------------------------------------ static

const notBuilt = `<!doctype html><meta charset="utf-8"><title>router</title>
<style>body{font:15px/1.6 system-ui,sans-serif;margin:3rem auto;max-width:34rem;padding:0 1rem;color:#222}
code{background:#f2f2f2;padding:.1rem .3rem;border-radius:3px}</style>
<h1>The frontend is not built yet</h1>
<p>This is the router backend. It is running and its API is live:
<code>/api/health</code>, <code>/api/geocode</code>, <code>/api/route</code>.</p>
<p>Build the app into <code>%s</code> and reload.</p>`

func (s *server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		fail(w, 404, "no such endpoint")
		return
	}
	if st, err := os.Stat(s.static); err != nil || !st.IsDir() {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, notBuilt, s.static)
		return
	}
	clean := filepath.Clean(r.URL.Path)
	p := filepath.Join(s.static, clean)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		http.ServeFile(w, r, p)
		return
	}
	// A client-side route: hand back the shell.
	index := filepath.Join(s.static, "index.html")
	if _, err := os.Stat(index); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, notBuilt, s.static)
		return
	}
	http.ServeFile(w, r, index)
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// hostOnly drops the port a Host header may carry.
func hostOnly(h string) string {
	if i := strings.LastIndexByte(h, ':'); i > 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}
