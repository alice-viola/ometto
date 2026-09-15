// Command router is the backend of the trip planner: one region map in
// memory, a routing engine per mode (car, bike, hike and the combinations), a
// geocoder, and Queen between the HTTP handler and the search.
//
// Every /route request is pushed to the queue `routes` on the partition of its
// region and answered on the queue `replies` on the partition of its request
// id, so many users are a queue and not a thundering herd, and a later worker
// can own a region simply by popping the same queue with -worker. Favourites
// and history live in Queen's KV under the namespace `app`.
//
//	bin/router -city web/public/taa.json -addr :8100 -queen http://localhost:6633
//
// The flags are below, with their defaults; README.md has the endpoints and
// what the service needs around it.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ometto/internal/queenx"
	"ometto/internal/route"
)

func main() {
	var (
		cityP     = flag.String("city", "web/public/taa.json", "region map json")
		placesP   = flag.String("places", "data/osm-taa/places.json", "OSM place nodes")
		poisP     = flag.String("pois", "data/osm-taa/pois.json", "OSM peaks, huts and passes")
		layersP   = flag.String("layers", "web/public", "directory of static GeoJSON layers served at /api/layers/")
		staticP   = flag.String("static", "web/app/dist", "the built frontend")
		addr      = flag.String("addr", ":8100", "listen address")
		queen     = flag.String("queen", envOr("QUEEN_URL", "http://localhost:6633"), "broker url")
		region    = flag.String("region", "taa", "region: the partition of the routes queue")
		workers   = flag.Int("workers", 4, "route workers in this process")
		worker    = flag.Bool("worker", false, "worker only: no HTTP, just pop the routes queue")
		prefix    = flag.String("prefix", envOr("QUEEN_PREFIX", "ometto."), "prefix for every queue and the KV namespace, so a shared tenant cannot collide")
		replyAddr = flag.String("reply-address", envOr("OMETTO_REPLY_ADDRESS", ""), "this instance's partition on the answers queue (default: the region); give each serving instance its own")
		tilesP    = flag.String("tiles", envOr("TILES_DIR", "web/tiles"), "basemap directory: taa.pmtiles, styles, fonts, sprites")
		buildP    = flag.String("build-info", envOr("BUILD_INFO", "data/build-info.json"), "what the data pipeline wrote about its sources")
		trusted   = flag.Bool("trusted-proxy", envOr("TRUSTED_PROXY", "") == "true", "believe X-Forwarded-For: set it only when a proxy you control is in front")
		logLvl    = flag.String("log-level", envOr("LOG_LEVEL", "info"), "info or debug (debug logs tiles too)")
	)
	flag.Parse()
	// The token is env-only, never a flag: a flag is in every ps listing.
	token := os.Getenv("QUEEN_TOKEN")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	t0 := time.Now()
	reg, err := route.Load(*cityP)
	if err != nil {
		log.Fatalf("ometto: map %s: %v", *cityP, err)
	}
	tLoad := time.Since(t0)
	t1 := time.Now()
	g := route.Build(reg)
	tGraph := time.Since(t1)
	t2 := time.Now()
	gc, err := route.BuildGeocoder(g, *placesP, *poisP,
		filepath.Join(*layersP, "pois.geojson"), filepath.Join(*layersP, "huts.geojson"))
	if err != nil {
		log.Fatalf("ometto: geocoder: %v", err)
	}
	log.Printf("ometto: map %s loaded in %.1fs (%d junctions, %d stretches, %d with elevation), graph in %.1fs (%d directed edges: car %d, bike %d, hike %d junctions), geocoder in %.1fs (%d entries, %d duplicate huts merged)",
		*cityP, tLoad.Seconds(), g.Junctions, len(reg.Stretches), reg.WithElevation,
		tGraph.Seconds(), g.DirectedEdges, g.CarN, g.BikeN, g.HikeN,
		time.Since(t2).Seconds(), gc.Len(), gc.Merged)

	qx, err := queenx.NewAuth(*queen, token)
	if err != nil {
		log.Fatalf("ometto: queen %s: %v", *queen, err)
	}
	defer qx.Close(context.Background())

	ns := strings.TrimSuffix(*prefix, ".")
	if ns == "" {
		ns = "app"
	}
	s := &server{
		g: g, gc: gc, qx: qx, raw: qx.Raw(), region: *region,
		layers: *layersP, static: *staticP, tiles: *tilesP, broker: *queen,
		qRoutes: *prefix + "routes", qReplies: *prefix + "answers",
		replyAddr: replyAddress(*replyAddr, *region),
		store:     &store{kv: qx.Raw().KV(), ns: ns},
		build:     loadBuildInfo(*buildP),
		met:       newMetrics(), log: newLogger(*logLvl), brk: &brokerState{},
		ipLim: newLimiter(), uLim: newLimiter(), trustProxy: *trusted,
		pid: os.Getpid(), started: time.Now(),
	}
	// The basemap, if this deployment carries one.
	if pm, err := OpenPMTiles(filepath.Join(*tilesP, "taa.pmtiles")); err == nil {
		s.pm = pm
		defer pm.Close()
		log.Printf("ometto: basemap %s: zoom %d-%d, %.0f MB",
			filepath.Join(*tilesP, "taa.pmtiles"), pm.MinZoom(), pm.MaxZoom(), float64(pm.Size())/(1<<20))
	} else if !os.IsNotExist(err) {
		log.Printf("ometto: WARNING basemap: %v", err)
	}
	// A broker that will not answer is a degraded start, not a dead one — and
	// a credential that may not configure a queue is neither.
	if err := s.alive(ctx); err != nil {
		s.brk.failOp("kv get "+ns+"/probe", err)
		_, msg, _ := s.brk.snapshot()
		log.Printf("ometto: WARNING %s (degraded: routing in process)", msg)
	} else if err := s.declareQueues(ctx); err != nil {
		s.brk.failOp("configure "+s.qRoutes, err)
		_, msg, _ := s.brk.snapshot()
		log.Printf("ometto: WARNING %s (degraded: routing in process)", msg)
	}
	if note := s.brk.adminNote(); note != "" {
		log.Printf("ometto: the broker's admin surface is %s; the queues are the tenant's and a push creates what it needs", note)
	}
	go s.watchBroker(ctx)

	n := *workers
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		go s.work(ctx, i)
	}

	if !*worker {
		// One reader for this instance's answers, not one per request: see
		// routeJob.Reply.
		go s.readReplies(ctx)
	}

	if *worker {
		log.Printf("ometto: worker only, region %s, %d loops on %s/%s", s.region, n, s.qRoutes, s.region)
		<-ctx.Done()
		time.Sleep(200 * time.Millisecond)
		return
	}

	srv := &http.Server{Addr: *addr, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	auth := "no token"
	if token != "" {
		auth = "bearer token"
	}
	log.Printf("ometto: http://localhost%s  region %s, %d workers, broker %s (%s), queues %s/%s, kv %s",
		*addr, s.region, n, *queen, auth, s.qRoutes, s.qReplies, ns)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	log.Printf("ometto: stopped")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// replyAddress is where this instance's answers come back. The region is the
// default because it is stable across restarts and there is one instance per
// region; a second serving instance in the same region must be given its own,
// or the two read each other's answers.
func replyAddress(flagged, region string) string {
	if a := strings.TrimSpace(flagged); a != "" {
		return a
	}
	if region != "" {
		return region
	}
	return "default"
}
