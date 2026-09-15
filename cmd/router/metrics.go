package main

// What the service says about itself: a Prometheus exposition and one JSON
// line per request that matters.
//
// Hand-written rather than pulled from client_golang: the numbers are a dozen,
// the format is text, and the image is smaller for it.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// latency buckets in seconds, for /api/route
var routeBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 15}

type metrics struct {
	mu       sync.Mutex
	requests map[string]int64 // "route|status" -> count
	hist     []int64          // le buckets for /api/route
	histSum  float64
	histN    int64

	brokerCalls  int64
	brokerErrors int64
	localRoutes  int64
	rateLimited  int64
	started      time.Time
}

func newMetrics() *metrics {
	return &metrics{requests: map[string]int64{}, hist: make([]int64, len(routeBuckets)+1), started: time.Now()}
}

// observe records one finished request.
func (m *metrics) observe(route string, status int, seconds float64) {
	m.mu.Lock()
	m.requests[route+"|"+fmt.Sprint(status)]++
	if route == "/api/route" {
		i := sort.SearchFloat64s(routeBuckets, seconds)
		m.hist[i]++
		m.histSum += seconds
		m.histN++
	}
	m.mu.Unlock()
}

// routeClass folds a path into the handful of names worth counting: a metric
// per tile would be a cardinality bomb and tells nobody anything.
func routeClass(p string) string {
	switch {
	case p == "/api/route", p == "/api/health", p == "/api/config", p == "/api/geocode",
		p == "/api/reverse", p == "/api/favorites", p == "/api/history", p == "/metrics":
		return p
	case strings.HasPrefix(p, "/api/favorites/"):
		return "/api/favorites/{id}"
	case strings.HasPrefix(p, "/api/history/"):
		return "/api/history/{id}"
	case strings.HasPrefix(p, "/api/layers/"):
		return "/api/layers/{name}"
	case strings.HasPrefix(p, "/map/tiles/"):
		return "/map/tiles"
	case strings.HasPrefix(p, "/map/"):
		return "/map/static"
	case strings.HasPrefix(p, "/api/"):
		return "/api/other"
	default:
		return "/static"
	}
}

func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Never through the tunnel. A request carrying Cloudflare's header came
	// from the internet, whatever else it claims, and these numbers are the
	// operator's: `docker compose exec`, or an SSH tunnel to the instance.
	// The dashboard rule that blocks /metrics is a second lock, not this one.
	if r.Header.Get("CF-Connecting-IP") != "" {
		http.NotFound(w, r)
		return
	}
	m := s.met
	var b strings.Builder
	p := func(format string, a ...interface{}) { fmt.Fprintf(&b, format, a...) }

	m.mu.Lock()
	reqs := make([]string, 0, len(m.requests))
	for k := range m.requests {
		reqs = append(reqs, k)
	}
	sort.Strings(reqs)
	p("# HELP ometto_requests_total HTTP requests by route and status.\n# TYPE ometto_requests_total counter\n")
	for _, k := range reqs {
		i := strings.LastIndexByte(k, '|')
		p("ometto_requests_total{route=%q,status=%q} %d\n", k[:i], k[i+1:], m.requests[k])
	}
	p("# HELP ometto_route_duration_seconds End to end time of POST /api/route.\n# TYPE ometto_route_duration_seconds histogram\n")
	var cum int64
	for i, le := range routeBuckets {
		cum += m.hist[i]
		p("ometto_route_duration_seconds_bucket{le=\"%g\"} %d\n", le, cum)
	}
	cum += m.hist[len(routeBuckets)]
	p("ometto_route_duration_seconds_bucket{le=\"+Inf\"} %d\n", cum)
	p("ometto_route_duration_seconds_sum %f\n", m.histSum)
	p("ometto_route_duration_seconds_count %d\n", m.histN)
	calls, errs, local, limited := m.brokerCalls, m.brokerErrors, m.localRoutes, m.rateLimited
	up := time.Since(m.started).Seconds()
	m.mu.Unlock()

	p("# HELP ometto_broker_calls_total Route requests handed to the broker.\n# TYPE ometto_broker_calls_total counter\nometto_broker_calls_total %d\n", calls)
	p("# HELP ometto_broker_errors_total Broker calls that failed or were refused.\n# TYPE ometto_broker_errors_total counter\nometto_broker_errors_total %d\n", errs)
	p("# HELP ometto_local_routes_total Routes computed in process because the broker was unavailable.\n# TYPE ometto_local_routes_total counter\nometto_local_routes_total %d\n", local)
	p("# HELP ometto_rate_limited_total Requests refused by the rate limiter.\n# TYPE ometto_rate_limited_total counter\nometto_rate_limited_total %d\n", limited)

	deg := 0.0
	if s.brk.isDegraded() {
		deg = 1
	}
	p("# HELP ometto_degraded Whether the broker is currently bypassed.\n# TYPE ometto_degraded gauge\nometto_degraded %g\n", deg)
	p("# HELP ometto_queue_pending Messages pending on the routes queue.\n# TYPE ometto_queue_pending gauge\nometto_queue_pending %d\n", s.pending(r.Context()))
	p("# HELP ometto_graph_junctions Junctions in the loaded graph.\n# TYPE ometto_graph_junctions gauge\nometto_graph_junctions %d\n", s.g.Junctions)
	p("# HELP ometto_graph_stretches Ways in the loaded map.\n# TYPE ometto_graph_stretches gauge\nometto_graph_stretches %d\n", len(s.g.R.Stretches)-s.g.R.Links)
	p("# HELP ometto_lifts Aerialways loaded and linked to the footpaths.\n# TYPE ometto_lifts gauge\nometto_lifts{state=\"loaded\"} %d\nometto_lifts{state=\"linked\"} %d\n", s.g.LiftsLoaded, s.g.LiftsLinked)
	p("# HELP ometto_inflight Route requests waiting for an answer.\n# TYPE ometto_inflight gauge\nometto_inflight %d\n", atomic.LoadInt64(&s.inflight))

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	p("# HELP go_goroutines Goroutines.\n# TYPE go_goroutines gauge\ngo_goroutines %d\n", runtime.NumGoroutine())
	p("# HELP go_memstats_alloc_bytes Heap in use.\n# TYPE go_memstats_alloc_bytes gauge\ngo_memstats_alloc_bytes %d\n", ms.Alloc)
	p("# HELP go_memstats_sys_bytes Memory taken from the system.\n# TYPE go_memstats_sys_bytes gauge\ngo_memstats_sys_bytes %d\n", ms.Sys)
	p("# HELP go_gc_cycles_total Completed GC cycles.\n# TYPE go_gc_cycles_total counter\ngo_gc_cycles_total %d\n", ms.NumGC)
	p("# HELP process_uptime_seconds Uptime.\n# TYPE process_uptime_seconds gauge\nprocess_uptime_seconds %f\n", up)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(b.String()))
}

// ------------------------------------------------------------------ logging

// logLine is one structured line. Logs go to stdout as JSON because that is
// what a container's collector reads; the human-readable form was for the
// terminal this service no longer lives in.
type logLine struct {
	Time    string  `json:"ts"`
	Service string  `json:"service"`
	Level   string  `json:"level"`
	Msg     string  `json:"msg"`
	Method  string  `json:"method,omitempty"`
	Path    string  `json:"path,omitempty"`
	Status  int     `json:"status,omitempty"`
	Bytes   int     `json:"bytes,omitempty"`
	Ms      float64 `json:"ms,omitempty"`
	IP      string  `json:"ip,omitempty"`
	Route   string  `json:"route,omitempty"`
	Extra   string  `json:"extra,omitempty"`
	Dropped int64   `json:"droppedLines,omitempty"`
}

// logger writes at most `rate` lines a second, counting what it drops, and
// never writes a line for a tile: a map pan is two hundred requests.
type logger struct {
	mu      sync.Mutex
	out     *json.Encoder
	sec     int64
	n       int
	dropped int64
	rate    int
	debug   bool
}

func newLogger(level string) *logger {
	return &logger{out: json.NewEncoder(os.Stdout), rate: 20, debug: strings.EqualFold(level, "debug")}
}

// service is the name in every log line: one product, one word, so a
// collector can filter on it.
const service = "ometto"

func (l *logger) write(ln logLine) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if s := now.Unix(); s != l.sec {
		if l.dropped > 0 {
			ln.Dropped = l.dropped
			l.dropped = 0
		}
		l.sec, l.n = s, 0
	}
	if l.n >= l.rate && ln.Level != "error" {
		l.dropped++
		return
	}
	l.n++
	ln.Time = now.UTC().Format(time.RFC3339Nano)
	ln.Service = service
	if ln.Level == "" {
		ln.Level = "info"
	}
	if err := l.out.Encode(ln); err != nil {
		log.Printf("router: log: %v", err)
	}
}

func (l *logger) event(level, msg, extra string) {
	l.write(logLine{Level: level, Msg: msg, Extra: extra})
}
