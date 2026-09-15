package main

// Who is asking, and how often they may.
//
// The service is about to stand on the public internet behind Caddy, on a
// 4 GB machine, with a broker on a free plan. Three things follow: an id has
// to be an id (32 hex characters, not "alice"), a client has a budget, and the
// address a budget is counted against is the socket's unless a trusted proxy
// put a real one in X-Forwarded-For.

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// validUser is the only shape of `user` the API accepts: 128 bits as 32
// lowercase hex characters, which the browser generates once and keeps. The
// short ids of the first weeks are refused on purpose — they were guessable,
// and a favourite list is a list of where someone goes.
func validUser(u string) bool {
	if len(u) != 32 {
		return false
	}
	for _, r := range u {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// clientIP is the address a budget is charged to. The forwarded headers are
// believed ONLY when the deployment says a proxy is in front (the compose sets
// it, because nothing but the tunnel can reach the app); otherwise they are
// noise a client controls.
//
// Cloudflare's CF-Connecting-IP comes first: through a tunnel it is the one
// header that carries a single address the edge vouches for, while
// X-Forwarded-For is a list anybody may have prepended to.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
			return cf
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// the left-most entry is the client; the rest are proxies
			if i := strings.IndexByte(xff, ','); i > 0 {
				xff = xff[:i]
			}
			if ip := strings.TrimSpace(xff); ip != "" {
				return ip
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-Ip")); xr != "" {
			return xr
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// The budget: a minute window and an hour window, per key.
const (
	perMinute = 60
	perHour   = 600
)

type window struct {
	start time.Time
	n     int
}

type bucket struct {
	min  window
	hour window
	seen time.Time
}

// limiter is a fixed-window counter per key. Fixed windows let a caller spend
// two minutes' worth across a boundary; that is a burst of 120 in a service
// whose answer costs 30 ms, and it buys a counter that costs nothing.
type limiter struct {
	mu sync.Mutex
	m  map[string]*bucket
}

func newLimiter() *limiter { return &limiter{m: map[string]*bucket{}} }

// allow charges one request to key. It answers whether the request may
// proceed and, when it may not, the seconds until the window opens.
func (l *limiter) allow(key string, now time.Time) (bool, int) {
	if key == "" {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > 20000 {
		// A slow sweep, on the call that finds the map too big: no goroutine,
		// no timer, and nothing to leak.
		for k, b := range l.m {
			if now.Sub(b.seen) > time.Hour {
				delete(l.m, k)
			}
		}
	}
	b := l.m[key]
	if b == nil {
		b = &bucket{min: window{start: now}, hour: window{start: now}}
		l.m[key] = b
	}
	b.seen = now
	if now.Sub(b.min.start) >= time.Minute {
		b.min = window{start: now}
	}
	if now.Sub(b.hour.start) >= time.Hour {
		b.hour = window{start: now}
	}
	if b.min.n >= perMinute {
		return false, int(time.Minute-now.Sub(b.min.start))/int(time.Second) + 1
	}
	if b.hour.n >= perHour {
		return false, int(time.Hour-now.Sub(b.hour.start))/int(time.Second) + 1
	}
	b.min.n++
	b.hour.n++
	return true, 0
}
