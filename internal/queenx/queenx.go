// Package queenx is the thin seam between Ometto and the Queen Go SDK.
//
// It exposes exactly what the vertical slice needs and nothing else: declare a
// queue, push one message, claim ONE partition, commit one transaction, and
// read the broker's own numbers. Everything it hides is either a knob the slice
// must not touch or a trap the broker's own notes warned about:
//
//   - a pop claims ONE partition (partitions=1) and `batch` is a call-wide
//     budget, so a Claim is always the ready head of a single segment;
//   - a transaction's ack does NOT read the consumer group off the message, so
//     Ack carries it explicitly;
//   - a transaction answers HTTP 200 with success:false on a duplicate
//     transactionId or a stale lease — both are outcomes, not errors, and
//     Transaction returns them as such;
//   - dedupWindowSeconds is not a field of the SDK's QueueConfig, so Configure
//     sends it as a raw option.
package queenx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	queen "github.com/smartpricing/queen/clients/client-go"
)

// Message is the SDK's message, re-exported so the rest of Ometto never
// imports the SDK directly.
type Message = queen.Message

// QueueModeGroup is the consumer group the broker uses when a pop or an ack
// names none: the competing-consumers lane the sim workers share.
const QueueModeGroup = ""

// Client is a Queen connection.
type Client struct {
	q *queen.Queen
}

// New dials the broker at url (e.g. http://localhost:6632).
//
// The 429 policy is pinned to a single attempt on purpose: the slice's callers
// each have a better answer to backpressure than a blocking retry inside the
// SDK — the aggregator DROPS a rejected feed frame, and the worker retries its
// whole transaction with its own backoff.
func New(url string) (*Client, error) { return NewAuth(url, "") }

// NewAuth dials a broker that wants a bearer token — a Queen Cloud tenant, as
// opposed to the dev stack on localhost. An empty token attaches no header at
// all, so the same call serves both.
func NewAuth(url, token string) (*Client, error) {
	q, err := queen.New(queen.ClientConfig{
		URL:         url,
		BearerToken: token,
		// Above the 30 s long-poll window the SDK asks the broker for. The
		// SDK's default is exactly 30 s and it is a HARD http.Client.Timeout,
		// so it fires FIRST on an idle long poll and every empty pop comes back
		// as `context deadline exceeded` instead of an empty batch. Measured at
		// exactly 30.000 s by the gateway's feed relay before this line existed.
		TimeoutMillis: 45000,
		// One idle connection per concurrent claim loop, with room to spare:
		// a worker runs WORKER_CONCURRENCY of them and every one holds a pop or
		// a transaction in flight. Below that number the transport churns
		// connections under load.
		MaxIdleConnsPerHost: 512,
		Retry429:            &queen.Retry429Config{MaxAttempts: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("queenx: dial %s: %w", url, err)
	}
	return &Client{q: q}, nil
}

// Close flushes buffers and closes the underlying client.
func (c *Client) Close(ctx context.Context) error { return c.q.Close(ctx) }

// Raw exposes the SDK client for the rare call this wrapper does not cover.
func (c *Client) Raw() *queen.Queen { return c.q }

// Health reports whether the broker answers /health.
func (c *Client) Health(ctx context.Context) error {
	res, err := c.q.Admin().Health(ctx)
	if err != nil {
		return err
	}
	if s, _ := res["status"].(string); s != "healthy" {
		return fmt.Errorf("queenx: broker not healthy: %v", res)
	}
	return nil
}

// -------------------------------------------------------------------- configure

// QueueOptions are the durable-queue knobs the slice sets. Zero means "leave
// the broker's default alone": Configure sends a MERGE, never a replace, so an
// omitted option is never reset.
type QueueOptions struct {
	LeaseTimeSeconds          int
	RetryLimit                int
	DedupWindowSeconds        int
	CompletedRetentionSeconds int
	RetentionSeconds          int
	RetentionEnabled          bool
	DeadLetterQueue           bool
}

// Configure declares a durable queue with the given options.
//
// It is idempotent: calling it again with the same options is a no-op edit, so
// every process may call it at start-up.
func (c *Client) Configure(ctx context.Context, queue string, opts QueueOptions) error {
	b := c.q.Queue(queue).Config(queen.QueueConfig{
		LeaseTime:                 opts.LeaseTimeSeconds,
		RetryLimit:                opts.RetryLimit,
		RetentionSeconds:          opts.RetentionSeconds,
		CompletedRetentionSeconds: opts.CompletedRetentionSeconds,
		RetentionEnabled:          opts.RetentionEnabled,
		DeadLetterQueue:           opts.DeadLetterQueue,
	}).Create()
	if opts.DedupWindowSeconds > 0 {
		// Not a field of the SDK's QueueConfig: it travels as a raw option or
		// not at all, and without it the queue keeps the 3600 s default, whose
		// dedup cache the slice cannot afford (18 x rate x window bytes).
		b = b.Option("dedupWindowSeconds", opts.DedupWindowSeconds)
	}
	if _, err := b.Execute(ctx); err != nil {
		return fmt.Errorf("queenx: configure %s: %w", queue, err)
	}
	return nil
}

// -------------------------------------------------------------------- push

// Push writes one message to (queue, partition) under txnID.
//
// txnID is the dedup key: the same id pushed twice to the same partition inside
// dedupWindowSeconds is accepted once and the second call reports Duplicate.
func (c *Client) Push(ctx context.Context, queue, partition, txnID string, payload any) (Outcome, error) {
	res, err := c.q.Queue(queue).Partition(partition).
		Push(payload).TransactionID(txnID).Execute(ctx)
	if err != nil {
		return OutcomeError, fmt.Errorf("queenx: push %s/%s: %w", queue, partition, err)
	}
	if len(res) == 0 {
		return OutcomeError, fmt.Errorf("queenx: push %s/%s: empty response", queue, partition)
	}
	switch res[0].Status {
	case "duplicate":
		return OutcomeDuplicate, nil
	case "failed":
		return OutcomeError, fmt.Errorf("queenx: push %s/%s rejected: %s", queue, partition, res[0].Error)
	default:
		return OutcomeCommitted, nil
	}
}

// -------------------------------------------------------------------- pop

// Claim is the answer to PopOne: the ready head of ONE partition, with the
// lease that owns it.
//
// Partition is the logical name (a segment id for the `cars` queue),
// PartitionID the broker's id for it, and LeaseID the lease every message of
// the claim shares. An empty claim has no messages and an empty Partition.
type Claim struct {
	Partition   string
	PartitionID string
	LeaseID     string
	Messages    []*Message
}

// Empty reports a claim that carried nothing.
func (c Claim) Empty() bool { return len(c.Messages) == 0 }

// PopOne claims ONE partition of queue for group and returns up to batch of its
// ready messages.
//
// partitions=1 is pinned, never left to autopilot: the slice's whole model is
// "one pop = one segment", and a broker-chosen sweep width would hand a worker
// several segments in one lease. batch is a call-wide budget; a partition's
// ready messages are never split across two concurrent pops of the same group,
// so a claim that returns fewer than batch messages holds the whole segment.
//
// group may be QueueModeGroup ("") for competing consumers.
//
// wait turns the pop into a long poll: the broker parks the request until a
// message is ready or its timeout fires, and an empty Claim comes back.
func (c *Client) PopOne(ctx context.Context, queue, group string, batch int, wait bool) (Claim, error) {
	claims, err := c.PopWide(ctx, queue, group, batch, 1, wait)
	if err != nil {
		return Claim{}, err
	}
	switch len(claims) {
	case 0:
		return Claim{}, nil
	case 1:
		return claims[0], nil
	default:
		return Claim{}, fmt.Errorf(
			"queenx: pop %s returned %d partitions in one claim: partitions=1 was not honoured",
			queue, len(claims))
	}
}

// PopWide claims up to `partitions` partitions in one call, batch being the
// budget shared by all of them, and returns one Claim per partition.
//
// FOR THE PROJECTION SIDE ONLY. The sim workers must use PopOne: one pop = one
// segment is the invariant that makes a segment's transaction a whole-segment
// commit. The aggregator has no such invariant — it reads every segment in any
// order — and would otherwise pay one HTTP round trip per segment per tick.
//
// Every message of one call shares ONE lease id, so a Claim's lease is the
// lease of the whole call: acknowledge all of them in one transaction, or let
// the lease expire on all of them together.
func (c *Client) PopWide(ctx context.Context, queue, group string, batch, partitions int, wait bool) ([]Claim, error) {
	b := c.q.Queue(queue).Partitions(partitions).Batch(batch).Wait(wait)
	if group != QueueModeGroup {
		// subscriptionMode is pinned to `new` rather than left to the broker's
		// DEFAULT_SUBSCRIPTION_MODE. It only matters on the pop that REGISTERS
		// the group — afterwards the stored policy and cursor win — and it is
		// the only sane registration for a live projection: start at the tail.
		// `all` would replay the whole retained backlog, which on the `cars`
		// queue is up to completedRetentionSeconds of dead positions.
		b = b.Group(group).SubscriptionMode(queen.SubscriptionModeNew)
	}
	msgs, err := b.Pop(ctx)
	if err != nil {
		return nil, fmt.Errorf("queenx: pop %s: %w", queue, err)
	}
	if len(msgs) == 0 {
		return nil, nil
	}
	var claims []Claim
	index := make(map[string]int, partitions)
	for _, m := range msgs {
		i, ok := index[m.PartitionID]
		if !ok {
			claims = append(claims, Claim{
				Partition:   m.Partition,
				PartitionID: m.PartitionID,
				LeaseID:     m.LeaseID,
			})
			i = len(claims) - 1
			index[m.PartitionID] = i
		}
		claims[i].Messages = append(claims[i].Messages, m)
	}
	return claims, nil
}

// Renew extends the lease of a claim.
func (c *Client) Renew(ctx context.Context, cl Claim) error {
	if cl.LeaseID == "" {
		return errors.New("queenx: renew: claim has no lease")
	}
	res, err := c.q.Renew(ctx, cl.LeaseID)
	if err != nil {
		return fmt.Errorf("queenx: renew: %w", err)
	}
	if len(res) > 0 && !res[0].Success {
		return fmt.Errorf("queenx: renew: %s", res[0].Error)
	}
	return nil
}

// -------------------------------------------------------------------- transaction

// Outcome is the closed taxonomy of a write's verdict.
type Outcome int

const (
	// OutcomeError is a write that did not happen and may be retried.
	OutcomeError Outcome = iota
	// OutcomeCommitted is a write that happened.
	OutcomeCommitted
	// OutcomeDuplicate means this transactionId is already committed in that
	// partition, inside the dedup window: the work is done, move on. Nothing
	// of the bundle was written a second time.
	OutcomeDuplicate
	// OutcomeStale means a lease in the bundle is no longer ours — another
	// worker took the partition and has already, or will, do this work. Drop
	// the claim, do not retry.
	OutcomeStale
)

func (o Outcome) String() string {
	switch o {
	case OutcomeCommitted:
		return "committed"
	case OutcomeDuplicate:
		return "duplicate"
	case OutcomeStale:
		return "stale"
	default:
		return "error"
	}
}

// Ack is one message to acknowledge inside a transaction.
//
// Group is NOT read off the message by the SDK: leave it empty for queue mode,
// or the broker commits the cursor of the wrong group.
type Ack struct {
	Message *Message
	Group   string
}

// Push is one message to write inside a transaction. TransactionID is the dedup
// key and must be deterministic for the work being committed.
type Push struct {
	Queue         string
	Partition     string
	TransactionID string
	Payload       any
}

// Transaction commits acks and pushes atomically, all-or-nothing, across
// partitions and queues.
//
// The returned Outcome is the verdict; err is non-nil only for OutcomeError,
// and Duplicate/Stale are returned with a nil error because they are the two
// expected answers of a legitimate redelivery.
func (c *Client) Transaction(ctx context.Context, acks []Ack, pushes []Push) (Outcome, error) {
	return c.TransactionKV(ctx, acks, pushes, nil)
}

// TransactionKV is Transaction with KV riders: the state writes commit in the
// same transaction as the acks and the pushes, or not at all.
func (c *Client) TransactionKV(ctx context.Context, acks []Ack, pushes []Push, kv []queen.KVOp) (Outcome, error) {
	if len(acks) == 0 && len(pushes) == 0 {
		return OutcomeError, errors.New("queenx: transaction: nothing to commit")
	}
	tb := c.q.Transaction()
	for _, a := range acks {
		if a.Message == nil {
			return OutcomeError, errors.New("queenx: transaction: nil ack message")
		}
		tb = tb.Ack(a.Message, "completed", queen.AckOptions{ConsumerGroup: a.Group})
	}
	// One push operation per (queue, partition): the builder groups items by
	// the partition set on the queue builder, so they cannot be merged.
	for _, p := range pushes {
		tb = tb.Queue(p.Queue).Partition(p.Partition).Push(queen.PushItem{
			Queue:         p.Queue,
			Partition:     p.Partition,
			TransactionID: p.TransactionID,
			Payload:       p.Payload,
		})
	}

	if len(kv) > 0 {
		tb = tb.KV(kv...)
	}
	resp, err := tb.Commit(ctx)
	if resp != nil && !resp.Success {
		switch resp.Reason {
		case "duplicate":
			return OutcomeDuplicate, nil
		case "ack_rejected":
			return OutcomeStale, nil
		}
	}
	if err != nil {
		return OutcomeError, fmt.Errorf("queenx: transaction: %w", err)
	}
	if resp == nil || !resp.Success {
		return OutcomeError, fmt.Errorf("queenx: transaction: failed without a reason: %+v", resp)
	}
	return OutcomeCommitted, nil
}

// -------------------------------------------------------------------- ephemeral

// EphemeralOptions are the bounds of an ephemeral queue. Zero fields are
// omitted and the broker's defaults own them.
//
// The bounds are PER QUEUE, not per partition, and `dropOldest` degrades to
// reject on a multi-partition queue — so the slice uses PolicyReject and the
// producer drops what the broker refuses.
type EphemeralOptions struct {
	MaxLength    int64
	MaxBytes     int64
	Policy       string
	TTLSeconds   int64
	LeaseSeconds int64
	RetryLimit   int64
}

// The two bound policies.
const (
	PolicyReject     = queen.EphemeralPolicyReject
	PolicyDropOldest = queen.EphemeralPolicyDropOldest
)

// ErrEphemeralFull is a push refused by a `reject` bound (HTTP 429): the queue
// is at its budget. The caller DROPS the message; it never retries.
var ErrEphemeralFull = errors.New("queenx: ephemeral queue full")

// EphemeralConfigure declares an ephemeral queue and its bounds.
func (c *Client) EphemeralConfigure(ctx context.Context, queue string, opts EphemeralOptions) error {
	o := queen.EphemeralOptions{Policy: opts.Policy}
	if opts.MaxLength > 0 {
		o.MaxLength = &opts.MaxLength
	}
	if opts.MaxBytes > 0 {
		o.MaxBytes = &opts.MaxBytes
	}
	if opts.TTLSeconds > 0 {
		o.TTLSeconds = &opts.TTLSeconds
	}
	if opts.LeaseSeconds > 0 {
		o.LeaseSeconds = &opts.LeaseSeconds
	}
	if opts.RetryLimit > 0 {
		o.RetryLimit = &opts.RetryLimit
	}
	if _, err := c.q.Ephemeral().Configure(ctx, queue, o); err != nil {
		return fmt.Errorf("queenx: ephemeral configure %s: %w", queue, err)
	}
	return nil
}

// EphemeralPush writes one message to a partition of an ephemeral queue.
//
// A queue at its bound answers 429 and this returns ErrEphemeralFull: drop the
// frame, the next one is 500 ms away.
func (c *Client) EphemeralPush(ctx context.Context, queue, partition string, payload any) error {
	_, err := c.q.Ephemeral().Push(ctx, queue, queen.EphemeralMessage{Payload: payload},
		queen.EphemeralPushOptions{Partition: partition})
	if err == nil {
		return nil
	}
	var he *queen.HTTPError
	if errors.As(err, &he) && he.StatusCode == 429 {
		return fmt.Errorf("%w: %s", ErrEphemeralFull, queue)
	}
	return fmt.Errorf("queenx: ephemeral push %s/%s: %w", queue, partition, err)
}

// EphemeralFrame is one popped ephemeral message. Payload is raw JSON.
type EphemeralFrame struct {
	ID        string
	Partition string
	Payload   []byte
}

// EphemeralPop takes up to batch messages of an ephemeral queue for group,
// auto-acked at delivery.
//
// AutoAck is the slice's choice: the feed is a projection, a frame nobody
// acknowledged is a frame the next snapshot replaces. A NEW group starts at the
// ring head, so a reconnecting reader sees the present, not a backlog.
func (c *Client) EphemeralPop(ctx context.Context, queue, group string, batch int, wait bool) ([]EphemeralFrame, error) {
	batchRes, err := c.q.Ephemeral().Pop(ctx, queue, queen.EphemeralPopOptions{
		Batch:   batch,
		Wait:    wait,
		Group:   group,
		AutoAck: true,
	})
	if err != nil {
		return nil, fmt.Errorf("queenx: ephemeral pop %s: %w", queue, err)
	}
	out := make([]EphemeralFrame, 0, len(batchRes.Messages))
	for _, m := range batchRes.Messages {
		out = append(out, EphemeralFrame{ID: m.ID, Partition: m.Partition, Payload: []byte(m.Payload)})
	}
	return out, nil
}

// -------------------------------------------------------------------- overview

// Overview is one sample of the broker's own numbers, read from
// GET /api/v1/resources/overview and GET /metrics/prometheus.
//
// Two different kinds of number live here, and the difference is load-bearing:
//
//   - IngestedPerSecond / ProcessedPerSecond are the broker's own 5-minute
//     moving average, up to one metrics flush (60 s) stale. A freshly booted
//     broker reports 0 for about a minute.
//   - PushMessagesTotal / PopMessagesTotal / AckMessagesTotal are cumulative
//     in-process counters that RESET when the broker restarts. Differencing two
//     samples gives a live rate; a sample whose total went DOWN crossed a
//     restart and must be skipped, not reported as a negative rate.
//
// BatchRTT is the only quantile the broker exposes and it is broker->PG->broker
// for a fusion batch, NOT end-to-end client latency. Label it as such wherever
// it is shown.
type Overview struct {
	SampledAt time.Time

	Queues     int
	Partitions int

	Pending    int64
	Processing int64
	Completed  int64
	DeadLetter int64

	IngestedPerSecond  float64
	ProcessedPerSecond float64
	StatsAgeSeconds    float64

	PushMessagesTotal float64
	PopMessagesTotal  float64
	AckMessagesTotal  float64

	// QueueTxnPerMinute is queen_queue_transactions_per_minute per queue:
	// transactions committed against that queue in the last DB metrics bucket.
	//
	// It is the ONLY broker number that moves on the transaction path. The
	// three cumulative counters above are incremented by POST /push, /pop and
	// /ack alone: a slice that writes exclusively through POST /transaction —
	// as this one does — leaves PushMessagesTotal and AckMessagesTotal at zero
	// forever, and only PopMessagesTotal measures anything. This gauge is
	// DB-backed and refreshed on the broker's stats cadence, so it is up to
	// StatsAgeSeconds old; it is a minute rate, so divide by 60 for a per-second
	// number.
	QueueTxnPerMinute map[string]float64

	BatchRTTPopP50Ms float64
	BatchRTTPopP99Ms float64
}

// Overview samples the broker. Both reads must succeed: a strip that silently
// keeps the previous number is a strip that lies.
func (c *Client) Overview(ctx context.Context) (Overview, error) {
	ov := Overview{SampledAt: time.Now()}

	res, err := c.q.Admin().GetOverview(ctx)
	if err != nil {
		return ov, fmt.Errorf("queenx: overview: %w", err)
	}
	ov.Queues = int(numField(res, "queues"))
	ov.Partitions = int(numField(res, "partitions"))
	ov.StatsAgeSeconds = numField(res, "statsAge")
	if m, ok := res["messages"].(map[string]any); ok {
		ov.Pending = int64(numField(m, "pending"))
		ov.Processing = int64(numField(m, "processing"))
		ov.Completed = int64(numField(m, "completed"))
		ov.DeadLetter = int64(numField(m, "deadLetter"))
	}
	if t, ok := res["throughput"].(map[string]any); ok {
		ov.IngestedPerSecond = numField(t, "ingestedPerSecond")
		ov.ProcessedPerSecond = numField(t, "processedPerSecond")
	}

	text, err := c.q.Admin().PrometheusMetrics(ctx)
	if err != nil {
		return ov, fmt.Errorf("queenx: prometheus: %w", err)
	}
	ov.PushMessagesTotal = promValue(text, "queen_process_push_messages_total", nil)
	ov.PopMessagesTotal = promValue(text, "queen_process_pop_messages_total", nil)
	ov.AckMessagesTotal = promValue(text, "queen_process_ack_messages_total", nil)
	ov.BatchRTTPopP50Ms = promValue(text, "queen_batch_rtt_milliseconds",
		map[string]string{"op": "pop", "quantile": "0.5"})
	ov.BatchRTTPopP99Ms = promValue(text, "queen_batch_rtt_milliseconds",
		map[string]string{"op": "pop", "quantile": "0.99"})
	ov.QueueTxnPerMinute = promByLabel(text, "queen_queue_transactions_per_minute", "queue")
	return ov, nil
}

// promByLabel reads every sample of a labelled metric family into a map keyed
// by one label's value — e.g. queen_queue_transactions_per_minute{queue="cars"}
// under "cars". A family the broker is not exposing yields an empty map, which
// is a real state: a queue with no traffic leaves the exposition after about
// five minutes rather than reporting 0.
func promByLabel(text, name, label string) map[string]float64 {
	out := make(map[string]float64)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, name+"{") {
			continue
		}
		rest := line[len(name):]
		end := strings.Index(rest, "}")
		if end < 0 {
			continue
		}
		key, ok := labelValue(rest[1:end], label)
		if !ok {
			continue
		}
		fields := strings.Fields(rest[end+1:])
		if len(fields) == 0 {
			continue
		}
		if f, err := strconv.ParseFloat(fields[0], 64); err == nil {
			out[key] = f
		}
	}
	return out
}

// labelValue pulls one label out of a Prometheus label set.
func labelValue(labels, name string) (string, bool) {
	i := strings.Index(labels, name+`="`)
	if i < 0 {
		return "", false
	}
	rest := labels[i+len(name)+2:]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

func numField(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return 0
}

// promValue reads one sample out of a Prometheus exposition. It returns 0 when
// the metric is absent, which is a real state: a queue with no traffic
// disappears from the exposition after about five minutes rather than
// reporting 0.
func promValue(text, name string, labels map[string]string) float64 {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, name) {
			continue
		}
		rest := line[len(name):]
		var labelPart string
		if strings.HasPrefix(rest, "{") {
			end := strings.Index(rest, "}")
			if end < 0 {
				continue
			}
			labelPart = rest[1:end]
			rest = rest[end+1:]
		} else if !strings.HasPrefix(rest, " ") {
			continue // a longer metric name that merely starts the same way
		}
		match := true
		for k, want := range labels {
			if !strings.Contains(labelPart, k+`="`+want+`"`) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if f, err := strconv.ParseFloat(fields[0], 64); err == nil {
			return f
		}
	}
	return 0
}
