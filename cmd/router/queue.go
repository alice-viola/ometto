package main

// Queen between the handler and the search.
//
// THE PATTERN. A /route request is a message on the queue `routes`, on the
// partition of its region ("taa"), with the request id as its transactionId —
// so the same request pushed twice inside the dedup window is computed once.
// A worker pops the partition, computes in memory, and in ONE transaction
// acks the request and pushes the answer to the queue `replies` on the
// partition of that request id. The handler long-polls exactly that partition
// and acks what it reads. Nothing else can read it: a partition per request is
// the reply address.
//
// The Go SDK has no request/reply helper (there is no rpc surface in
// third_party/queen-client-go), so this is the two-queue version of it, which
// is also what lets a worker run in another process: `-worker` pops the same
// queue and needs no HTTP at all.

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	queen "github.com/smartpricing/queen/clients/client-go"

	"ometto/internal/queenx"
	"ometto/internal/route"
)

const (
	replyTimeout = 15 * time.Second
	popBatch     = 4
	// workerPollMs is how long a claim waits before asking again.
	workerPollMs = 2000
	// replyPollMs is the same for the answer line.
	replyPollMs = 2000
)

// brokerState is what the service believes about the broker right now.
//
// On a free plan the broker may answer 429, or be down for maintenance, or
// simply not be reachable from this machine. None of that is a reason to stop
// answering: the engine is in this process, and the queue buys durability and
// distribution, not the ability to route. So a broker that refuses is noted,
// bypassed, and probed in the background until it comes back.
type brokerState struct {
	mu        sync.Mutex
	degraded  bool
	lastErr   string
	lastAt    time.Time
	since     time.Time
	fails     int
	nextProbe time.Time
	// admin is set when the credential may not use the ADMIN surface —
	// /health, the queue depth, configure. On a cloud tenant that is the
	// normal shape of a deployed app's credential, not a fault: the queues
	// are the tenant's, a push creates what it needs, and routing goes on.
	// It is recorded once, shown in health, and never degrades the service.
	admin string
}

// isForbidden says the broker refused this credential rather than failed.
func isForbidden(err error) bool {
	var he *queen.HTTPError
	return errors.As(err, &he) && he.StatusCode == 403
}

// adminDenied records that one admin call is not permitted. It is not a
// failure: nothing about routing depends on it.
func (b *brokerState) adminDenied(op string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.admin == "" {
		b.admin = "not permitted: " + op
	}
}

func (b *brokerState) adminNote() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.admin
}

func (b *brokerState) isDegraded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.degraded
}

// fail records a broker that would not answer and puts the service in
// degraded mode, with a backoff that doubles to a minute.
func (b *brokerState) fail(err error) { b.failOp("", err) }

// failOp is fail with the operation that failed in front of the message —
// "configure ometto.routes: HTTP 403 …" rather than "HTTP 403 …", because on
// a tenant whose credential may do some things and not others, WHICH call was
// refused is the whole diagnosis. The label is not repeated when the error
// already carries it.
func (b *brokerState) failOp(op string, err error) {
	msg := strings.TrimPrefix(err.Error(), "queenx: ")
	if op != "" && !strings.HasPrefix(msg, op) {
		msg = op + ": " + msg
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastErr, b.lastAt = msg, time.Now()
	if !b.degraded {
		b.degraded, b.since, b.fails = true, time.Now(), 0
	}
	b.fails++
	wait := time.Duration(5<<uint(min(b.fails-1, 4))) * time.Second
	b.nextProbe = time.Now().Add(wait)
}

func (b *brokerState) recovered() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.degraded, b.fails, b.lastErr = false, 0, ""
}

func (b *brokerState) dueForProbe() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.degraded && time.Now().After(b.nextProbe)
}

func (b *brokerState) snapshot() (bool, string, time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.degraded, b.lastErr, b.lastAt
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// alive is the liveness probe, and it is a DATA-plane call: a KV read of our
// own namespace. /health belongs to the admin surface, which a deployed app's
// credential may not touch, and a pop would consume a message to prove the
// broker is up. Found and not-found both mean alive; 403 here is a real
// refusal, because without KV there are no favourites.
func (s *server) alive(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.store.kv.Get(c, s.store.ns, "probe")
	return err
}

// declareQueues configures the two queues, unless the credential may not —
// the cloud provisions them, and a push creates what is missing with the
// broker's defaults.
func (s *server) declareQueues(ctx context.Context) error {
	err := s.configure(ctx)
	if err != nil && isForbidden(err) {
		s.brk.adminDenied("configure " + s.qRoutes)
		return nil
	}
	return err
}

// watchBroker leaves degraded mode by itself, as soon as the broker answers.
func (s *server) watchBroker(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !s.brk.dueForProbe() {
				continue
			}
			err := s.alive(ctx)
			op := "kv get " + s.store.ns + "/probe"
			if err == nil {
				if cerr := s.declareQueues(ctx); cerr == nil {
					s.brk.recovered()
					s.log.event("info", "broker is back: leaving degraded mode", "")
					continue
				} else {
					err, op = cerr, "configure "+s.qRoutes
				}
			}
			s.brk.failOp(op, err)
		}
	}
}

// errTimeout is the 504: the queue did not answer inside replyTimeout.
var errTimeout = errors.New("route request timed out")

// routeJob is what travels on `routes`.
type routeJob struct {
	ID     string        `json:"id"`
	Region string        `json:"region"`
	At     int64         `json:"at"`
	Req    route.Request `json:"req"`
	// Reply is the partition of `replies` the answer belongs on: the ADDRESS
	// OF THE INSTANCE that asked, not the id of the request. A partition per
	// request looks tidy and is a leak — partitions are permanent, a tenant
	// is allowed a handful, and this one refused the ninth reply with
	// "partition limit reached (8)" after eight routes. One address per
	// instance is the usual shape of request/reply: the answers come back on
	// one line and the instance hands each to whoever is waiting for it.
	Reply string `json:"reply,omitempty"`
}

// routeReply is what travels on `replies`.
//
// The answer travels GZIPPED, in `gz`, because a route is mostly geometry: a
// three-alternative crossing of the region is 360 KB of JSON and the tenant's
// broker refuses an item over 256 KB (HTTP 413). Compressed it is a tenth of
// that. `result` is still read when it is there, so an old worker and a new
// handler can pass in either direction.
type routeReply struct {
	ID     string     `json:"id"`
	OK     bool       `json:"ok"`
	Error  string     `json:"error,omitempty"`
	Result *routeResp `json:"result,omitempty"`
	GZ     string     `json:"gz,omitempty"`
	// bytes is the compressed size, kept for the log even when the reply
	// was refused for it: "0 bytes" told nobody how far over it was.
	bytes int
}

// maxReplyBytes is the room a reply may take inside one message, under the
// 256 KB the broker accepts. An answer that does not fit even compressed is
// refused by the worker and computed again by the handler: the queue carried
// the question, and the answer comes back the short way.
const maxReplyBytes = 200 << 10

// errTooLarge is that refusal, recognised on the other side.
const errTooLarge = "answer too large for the queue"

// pack compresses an answer into the reply, or says it cannot.
func pack(rep *routeReply, res *routeResp) {
	raw, err := json.Marshal(res)
	if err != nil {
		rep.OK, rep.Error = false, "reply could not be encoded: "+err.Error()
		return
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if _, err := zw.Write(raw); err != nil {
		rep.OK, rep.Error = false, "reply could not be compressed: "+err.Error()
		return
	}
	zw.Close()
	gz := base64.StdEncoding.EncodeToString(buf.Bytes())
	rep.bytes = len(gz)
	if len(gz) > maxReplyBytes {
		rep.OK, rep.Error = false, errTooLarge
		return
	}
	rep.OK, rep.GZ = true, gz
}

// unpack is the other side of it.
func unpack(rep *routeReply) (*routeResp, error) {
	if rep.GZ == "" {
		return rep.Result, nil
	}
	raw, err := base64.StdEncoding.DecodeString(rep.GZ)
	if err != nil {
		return nil, fmt.Errorf("reply: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("reply: %w", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("reply: %w", err)
	}
	var res routeResp
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("reply: %w", err)
	}
	return &res, nil
}

// routeResp is the body of POST /api/route.
type routeResp struct {
	Routes  []*route.Route `json:"routes"`
	Snapped []route.Snap   `json:"snapped"`
	Reason  string         `json:"reason,omitempty"`
	// NeededGrade is the easiest grade that would answer, when the asked one
	// could not: the frontend offers it as one click.
	NeededGrade string `json:"neededGrade,omitempty"`
	// Avoided are the ways this request struck out, for the map to hatch.
	Avoided []route.Avoided `json:"avoided,omitempty"`
	// Cached says this refusal was remembered rather than searched again: a
	// refusal costs a whole component, and asking twice must not pay twice.
	Cached bool `json:"cached,omitempty"`
	// Degraded says the broker was not available and this answer was computed
	// in the web process. The route is the same; the queue is not in it.
	Degraded bool `json:"degraded,omitempty"`
	// Local says the answer was computed in the web process, and why: "broker
	// unavailable" (then Degraded is set too) or "answer too large for the
	// queue" — the worker found the route and could not post it, so the same
	// engine ran it again here. Nothing about the answer is worse; the page
	// must not tell the person it is.
	Local      string  `json:"local,omitempty"`
	ComputedMs float64 `json:"computedMs"`
	Engine     string  `json:"engine"`
	Worker     string  `json:"worker"`
}

func (s *server) configure(ctx context.Context) error {
	// Short retentions: a route request is worth nothing a minute later, and
	// the broker sweeps both queues instead of keeping the night's traffic.
	opts := queenx.QueueOptions{
		LeaseTimeSeconds: 60, RetryLimit: 2,
		RetentionSeconds: 300, CompletedRetentionSeconds: 60,
		RetentionEnabled: true, DedupWindowSeconds: 120,
	}
	if err := s.qx.Configure(ctx, s.qRoutes, opts); err != nil {
		return err
	}
	return s.qx.Configure(ctx, s.qReplies, opts)
}

// newID is a request id: unique across restarts, because it is also the
// dedup key and the reply address.
func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// submit pushes the request and waits for its answer — unless the broker is
// known to be down, in which case it answers here and says so.
func (s *server) submit(ctx context.Context, req route.Request) (*routeResp, error) {
	if s.brk.isDegraded() {
		return s.computeLocally(req, true), nil
	}
	id := newID()
	job := routeJob{ID: id, Region: s.region, At: time.Now().UnixMilli(), Req: req, Reply: s.replyAddr}
	wait := s.expect(id)
	defer s.forget(id)
	atomic.AddInt64(&s.inflight, 1)
	defer atomic.AddInt64(&s.inflight, -1)
	s.met.mu.Lock()
	s.met.brokerCalls++
	s.met.mu.Unlock()

	if _, err := s.qx.Push(ctx, s.qRoutes, s.region, id, job); err != nil {
		return s.brokerDown(req, "push "+s.qRoutes+"/"+s.region, err), nil
	}
	rep, err := s.waitReply(ctx, wait)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err() // the client went away: not the broker's fault
		}
		return s.brokerDown(req, "pop "+s.qReplies+"/"+s.replyAddr, err), nil
	}
	if !rep.OK {
		if rep.Error == errTooLarge {
			// The worker computed it and could not post it. Compute it here:
			// the same engine, one more time, and the person gets an answer.
			s.log.event("warn", "answer too large for the queue: computing it here", id)
			return s.computeLocally(req, false), nil
		}
		return nil, errors.New(rep.Error)
	}
	res, err := unpack(rep)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// brokerDown notes the failure, drops into degraded mode and answers anyway.
func (s *server) brokerDown(req route.Request, op string, err error) *routeResp {
	was := s.brk.isDegraded()
	s.brk.failOp(op, err)
	s.met.mu.Lock()
	s.met.brokerErrors++
	s.met.mu.Unlock()
	if !was {
		_, msg, _ := s.brk.snapshot()
		s.log.event("error", "broker unavailable: answering in process", msg)
	}
	return s.computeLocally(req, true)
}

// computeLocally is the whole product without the queue: the engine is in this
// process and always was. degraded says the broker is down, which the answer
// admits to; a reply that was only too large for the queue is not that.
func (s *server) computeLocally(req route.Request, degraded bool) *routeResp {
	t0 := time.Now()
	res := s.g.Route(req)
	s.met.mu.Lock()
	s.met.localRoutes++
	s.met.mu.Unlock()
	atomic.AddInt64(&s.computed, 1)
	return &routeResp{
		Routes: res.Routes, Snapped: res.Snapped, Reason: res.Reason,
		NeededGrade: res.NeededGrade, Avoided: res.Avoided, Cached: res.Cached,
		ComputedMs: float64(time.Since(t0).Microseconds()) / 1000,
		Engine:     "inmem", Worker: s.workerName(), Degraded: degraded,
		Local: map[bool]string{true: "broker unavailable", false: errTooLarge}[degraded],
	}
}

// expect registers a waiting request, so the reply reader can hand the answer
// to the goroutine that asked for it.
func (s *server) expect(id string) chan *routeReply {
	ch := make(chan *routeReply, 1)
	s.waitMu.Lock()
	if s.waiting == nil {
		s.waiting = map[string]chan *routeReply{}
	}
	s.waiting[id] = ch
	s.waitMu.Unlock()
	return ch
}

// forget drops a waiter: its request was answered, timed out, or the person
// closed the tab. A reply that arrives afterwards is acked and discarded.
func (s *server) forget(id string) {
	s.waitMu.Lock()
	delete(s.waiting, id)
	s.waitMu.Unlock()
}

// deliver hands one reply to whoever is waiting for it, and says whether
// anyone was. Nobody waiting is normal: a request that timed out here may
// still be answered by a worker a second later.
func (s *server) deliver(rep *routeReply) bool {
	s.waitMu.RLock()
	ch, ok := s.waiting[rep.ID]
	s.waitMu.RUnlock()
	if !ok {
		return false
	}
	select {
	case ch <- rep:
		return true
	default:
		return false
	}
}

// waitReply waits for the answer to one request, or for the deadline.
func (s *server) waitReply(ctx context.Context, wait chan *routeReply) (*routeReply, error) {
	t := time.NewTimer(replyTimeout)
	defer t.Stop()
	select {
	case rep := <-wait:
		return rep, nil
	case <-t.C:
		return nil, errTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// readReplies is the one reader of this instance's answer line. It runs for
// the life of the process: pop, hand each answer to its waiter, ack the lot.
func (s *server) readReplies(ctx context.Context) {
	for ctx.Err() == nil {
		if s.brk.isDegraded() {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		msgs, err := s.raw.Queue(s.qReplies).Partition(s.replyAddr).
			Batch(16).Wait(true).TimeoutMillis(replyPollMs).Pop(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// An address nobody has written to yet is not a fault: it comes
			// into being with the first answer.
			if isMissing(err) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			s.brk.failOp("pop "+s.qReplies+"/"+s.replyAddr, err)
			time.Sleep(time.Second)
			continue
		}
		if len(msgs) == 0 {
			continue
		}
		for _, m := range msgs {
			var rep routeReply
			if err := remarshal(m.Data, &rep); err != nil {
				continue
			}
			s.deliver(&rep)
		}
		ackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if _, err := s.raw.Ack(ackCtx, msgs, true, queen.AckOptions{}); err != nil {
			log.Printf("ometto: ack replies: %v", err)
		}
		cancel()
	}
}

func isMissing(err error) bool {
	var he *queen.HTTPError
	if errors.As(err, &he) && (he.StatusCode == 404 || he.StatusCode == 400) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "404")
}

// work is one pop loop: claim THIS REGION's partition of `routes`, compute
// every request of the claim in parallel, then ack them and push the answers
// in one transaction.
//
// The pop names the partition (`/pop/queue/routes/partition/<region>`) rather
// than letting the broker sweep the queue: that is what makes a worker own a
// region. A plain queue-wide pop would happily claim another region's lane and
// answer it from a map that does not hold it.
func (s *server) work(ctx context.Context, i int) {
	for ctx.Err() == nil {
		if s.brk.isDegraded() {
			// Nothing to pop from a broker that is not answering; the probe
			// in watchBroker decides when to come back.
			time.Sleep(time.Second)
			continue
		}
		cl, err := s.popRegion(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !isMissing(err) {
				s.brk.failOp("pop "+s.qRoutes+"/"+s.region, err)
				_, msg, _ := s.brk.snapshot()
				s.log.event("error", "worker pop failed", msg)
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		s.serve(ctx, cl)
	}
}

// popRegion long-polls this region's partition of the routes queue. Queue
// mode (no consumer group): the workers of a region are competing consumers.
func (s *server) popRegion(ctx context.Context) (queenx.Claim, error) {
	// A SHORT window, on purpose. A long poll is only as good as the broker's
	// wake-up: this tenant's returns what was ready when the poll began, so a
	// twenty-second window meant a request waiting up to twenty seconds for a
	// worker while the handler gave up at fifteen. Two seconds costs two
	// requests a second per worker and claims a route almost as it lands.
	msgs, err := s.raw.Queue(s.qRoutes).Partition(s.region).
		Batch(popBatch).Wait(true).TimeoutMillis(workerPollMs).Pop(ctx)
	if err != nil || len(msgs) == 0 {
		return queenx.Claim{}, err
	}
	return queenx.Claim{
		Partition:   msgs[0].Partition,
		PartitionID: msgs[0].PartitionID,
		LeaseID:     msgs[0].LeaseID,
		Messages:    msgs,
	}, nil
}

// serve computes one claim.
func (s *server) serve(ctx context.Context, cl queenx.Claim) {
	if cl.Empty() {
		return
	}
	acks := make([]queenx.Ack, 0, len(cl.Messages))
	replies := make([]routeReply, len(cl.Messages))
	jobs := make([]routeJob, len(cl.Messages))
	took := make([]time.Duration, len(cl.Messages))
	var wg sync.WaitGroup
	for i, m := range cl.Messages {
		acks = append(acks, queenx.Ack{Message: m})
		var job routeJob
		if err := remarshal(m.Data, &job); err != nil {
			replies[i] = routeReply{ID: m.TransactionID, Error: "bad request payload: " + err.Error()}
			continue
		}
		jobs[i] = job
		if err := validJob(job); err != nil {
			replies[i] = routeReply{ID: job.ID, Error: err.Error()}
			continue
		}
		wg.Add(1)
		go func(i int, job routeJob) {
			defer wg.Done()
			// One bad message must not take the service down with it: the
			// queue carries whatever anything ever pushed to it.
			defer func() {
				if p := recover(); p != nil {
					s.log.event("error", "worker recovered from a panic", fmt.Sprint(p))
					replies[i] = routeReply{ID: job.ID, Error: "the request could not be computed"}
				}
			}()
			t0 := time.Now()
			res := s.g.Route(job.Req)
			took[i] = time.Since(t0)
			rep := routeReply{ID: job.ID}
			pack(&rep, &routeResp{
				Routes: res.Routes, Snapped: res.Snapped, Reason: res.Reason,
				NeededGrade: res.NeededGrade, Avoided: res.Avoided, Cached: res.Cached,
				ComputedMs: float64(took[i].Microseconds()) / 1000,
				Engine:     "inmem", Worker: s.workerName(),
			})
			replies[i] = rep
			atomic.AddInt64(&s.computed, 1)
		}(i, job)
	}
	wg.Wait()
	// One transaction per request, not one for the claim: a single answer the
	// broker will not accept must not cost the others their acks as well.
	for i, rep := range replies {
		id := rep.ID
		if id == "" {
			id = cl.Messages[i].TransactionID
			rep.ID = id
		}
		// Back to the address the asker gave. A job from before this change
		// carries none; its answer goes to this instance's own line, which is
		// where a single-instance deployment wants it anyway.
		addr := jobs[i].Reply
		if addr == "" {
			addr = s.replyAddr
		}
		push := queenx.Push{
			Queue: s.qReplies, Partition: addr, TransactionID: "reply:" + id, Payload: rep,
		}
		s.log.write(logLine{Msg: "answered", Route: "worker", Ms: took[i].Seconds() * 1000,
			Extra: fmt.Sprintf("%s %s %d bytes %s", id, map[bool]string{true: "ok", false: rep.Error}[rep.OK],
				rep.bytes, s.workerName())})
		out, err := s.qx.Transaction(ctx, []queenx.Ack{acks[i]}, []queenx.Push{push})
		if err != nil {
			log.Printf("ometto: commit reply %s: %v", id, err)
			continue
		}
		if out != queenx.OutcomeCommitted {
			log.Printf("ometto: commit reply %s: %s", id, out)
		}
	}
}

func (s *server) workerName() string {
	return fmt.Sprintf("%s/%d", s.region, os.Getpid())
}

// validJob is the same gate the HTTP handler applies, applied again to what
// comes off the queue: a message is not a request just because it parsed.
func validJob(job routeJob) error {
	stops := 0
	for _, p := range job.Req.Points {
		if !p.Via {
			stops++
		}
	}
	switch {
	case job.ID == "":
		return errors.New("bad request payload: no request id")
	case stops < 2 || stops > maxStops || len(job.Req.Points) > maxEntries:
		return fmt.Errorf("bad request payload: points must hold between 2 and %d stops", maxStops)
	case job.Req.Mode != "" && !route.ValidMode(job.Req.Mode):
		return errors.New("bad request payload: unknown mode")
	case job.Req.Grade != "" && route.SatGrade(job.Req.Grade) == 0:
		return errors.New("bad request payload: unknown grade")
	}
	return nil
}

// remarshal moves the SDK's generic payload into a typed struct.
func remarshal(data map[string]interface{}, out interface{}) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
