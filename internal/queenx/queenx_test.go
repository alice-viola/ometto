package queenx_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"ometto/internal/queenx"
)

// Every test here talks to a REAL broker. Start one with `make run-dev-broker`
// and run them with QUEEN_URL=http://localhost:6632 go test ./...
func client(t *testing.T) (*queenx.Client, context.Context) {
	t.Helper()
	url := os.Getenv("QUEEN_URL")
	if url == "" {
		t.Skip("QUEEN_URL not set: skipping the broker integration test")
	}
	c, err := queenx.New(url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	if err := c.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
	return c, ctx
}

// TestRoundTrip walks the whole contract the worker loop depends on:
// configure, push, claim one partition, commit acks+pushes in one transaction,
// and the duplicate verdict on a replayed transactionId.
func TestRoundTrip(t *testing.T) {
	c, ctx := client(t)

	queue := fmt.Sprintf("queenx-test-%d", time.Now().UnixNano())
	partition := "seg-0001"

	if err := c.Configure(ctx, queue, queenx.QueueOptions{
		LeaseTimeSeconds:          3,
		RetryLimit:                3,
		DedupWindowSeconds:        60,
		CompletedRetentionSeconds: 1800,
		RetentionEnabled:          true,
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}

	// Two messages on one partition: a claim must carry both.
	for i := 0; i < 2; i++ {
		out, err := c.Push(ctx, queue, partition, fmt.Sprintf("car:%d:0", i),
			map[string]any{"car": i, "tick": 0})
		if err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
		if out != queenx.OutcomeCommitted {
			t.Fatalf("push %d: outcome %s, want committed", i, out)
		}
	}

	// A repeated push transactionId is a per-item duplicate, not a rollback.
	if out, err := c.Push(ctx, queue, partition, "car:0:0", map[string]any{"car": 0, "tick": 0}); err != nil {
		t.Fatalf("duplicate push: %v", err)
	} else if out != queenx.OutcomeDuplicate {
		t.Fatalf("duplicate push: outcome %s, want duplicate", out)
	}

	claim, err := c.PopOne(ctx, queue, queenx.QueueModeGroup, 100, true)
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if claim.Empty() {
		t.Fatal("pop: empty claim, want the two pushed messages")
	}
	if claim.Partition != partition {
		t.Fatalf("pop: partition %q, want %q", claim.Partition, partition)
	}
	if len(claim.Messages) != 2 {
		t.Fatalf("pop: %d messages, want 2 (a partition's ready messages are never split)", len(claim.Messages))
	}
	if claim.LeaseID == "" {
		t.Fatal("pop: no lease id on the claim")
	}

	// The worker's commit: ack what was popped, push the next tick, one call.
	acks := make([]queenx.Ack, 0, len(claim.Messages))
	pushes := make([]queenx.Push, 0, len(claim.Messages))
	for i, m := range claim.Messages {
		acks = append(acks, queenx.Ack{Message: m, Group: queenx.QueueModeGroup})
		pushes = append(pushes, queenx.Push{
			Queue:         queue,
			Partition:     partition,
			TransactionID: fmt.Sprintf("car:%d:1", i),
			Payload:       map[string]any{"car": i, "tick": 1},
		})
	}
	out, err := c.Transaction(ctx, acks, pushes)
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if out != queenx.OutcomeCommitted {
		t.Fatalf("transaction: outcome %s, want committed", out)
	}

	// The same push transactionIds again: a cross-call duplicate rolls the whole
	// bundle back and comes back as the Duplicate verdict with no error.
	dupOut, err := c.Transaction(ctx, nil, pushes)
	if err != nil {
		t.Fatalf("duplicate transaction returned an error, want the duplicate verdict: %v", err)
	}
	if dupOut != queenx.OutcomeDuplicate {
		t.Fatalf("duplicate transaction: outcome %s, want duplicate", dupOut)
	}

	// And the tick-1 messages are claimable exactly once.
	claim2, err := c.PopOne(ctx, queue, queenx.QueueModeGroup, 100, true)
	if err != nil {
		t.Fatalf("pop after commit: %v", err)
	}
	if len(claim2.Messages) != 2 {
		t.Fatalf("pop after commit: %d messages, want 2", len(claim2.Messages))
	}
	if _, err := c.Transaction(ctx, []queenx.Ack{{Message: claim2.Messages[0]}, {Message: claim2.Messages[1]}}, nil); err != nil {
		t.Fatalf("drain transaction: %v", err)
	}
}

// TestEphemeralFeed covers the projection side: a bounded queue, a push, and a
// pop by a group that starts at the ring head.
func TestEphemeralFeed(t *testing.T) {
	c, ctx := client(t)

	queue := fmt.Sprintf("queenx-feed-%d", time.Now().UnixNano())
	if err := c.EphemeralConfigure(ctx, queue, queenx.EphemeralOptions{
		MaxLength:  256,
		MaxBytes:   16 << 20,
		Policy:     queenx.PolicyReject,
		TTLSeconds: 10,
	}); err != nil {
		t.Fatalf("ephemeral configure: %v", err)
	}
	if err := c.EphemeralPush(ctx, queue, "tile-0", map[string]any{"tile": 0, "cars": []any{}}); err != nil {
		t.Fatalf("ephemeral push: %v", err)
	}
	frames, err := c.EphemeralPop(ctx, queue, "gateway", 16, true)
	if err != nil {
		t.Fatalf("ephemeral pop: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("ephemeral pop: %d frames, want 1", len(frames))
	}
	if len(frames[0].Payload) == 0 {
		t.Fatal("ephemeral pop: empty payload")
	}
}

// TestOverview asserts the numbers strip has something real behind it.
func TestOverview(t *testing.T) {
	c, ctx := client(t)
	ov, err := c.Overview(ctx)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if ov.Queues < 0 || ov.Partitions < 0 {
		t.Fatalf("overview: nonsense counts: %+v", ov)
	}
	// The in-process counters are cumulative and this suite has pushed: they
	// cannot be zero on a broker that just served the tests above.
	if ov.PushMessagesTotal <= 0 {
		t.Fatalf("overview: queen_process_push_messages_total = %v, want > 0", ov.PushMessagesTotal)
	}
}

// TestPopWideGroupsByPartition covers the aggregator's read path: several
// segments in one call, one Claim each, one shared lease.
func TestPopWideGroupsByPartition(t *testing.T) {
	c, ctx := client(t)

	queue := fmt.Sprintf("queenx-wide-%d", time.Now().UnixNano())
	if err := c.Configure(ctx, queue, queenx.QueueOptions{
		LeaseTimeSeconds:   3,
		DedupWindowSeconds: 60,
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	// A NEW consumer group registers at the TAIL (subscriptionMode `new`), so
	// the group has to exist before the messages it is meant to see. One empty
	// pop is the registration.
	if _, err := c.PopWide(ctx, queue, "view", 1, 1, false); err != nil {
		t.Fatalf("register group: %v", err)
	}

	segments := []string{"h-0001-0001", "h-0002-0001", "v-0001-0001"}
	for i, seg := range segments {
		if _, err := c.Push(ctx, queue, seg, fmt.Sprintf("car:%d:0", i), map[string]any{"seg": seg}); err != nil {
			t.Fatalf("push %s: %v", seg, err)
		}
	}

	seen := map[string]bool{}
	var acks []queenx.Ack
	for len(seen) < len(segments) {
		claims, err := c.PopWide(ctx, queue, "view", 500, 8, true)
		if err != nil {
			t.Fatalf("popWide: %v", err)
		}
		if len(claims) == 0 {
			t.Fatalf("popWide: empty after seeing %d/%d segments", len(seen), len(segments))
		}
		for _, cl := range claims {
			if seen[cl.Partition] {
				t.Fatalf("popWide: partition %s claimed twice", cl.Partition)
			}
			seen[cl.Partition] = true
			for _, m := range cl.Messages {
				if m.Partition != cl.Partition {
					t.Fatalf("popWide: message of %s filed under %s", m.Partition, cl.Partition)
				}
				acks = append(acks, queenx.Ack{Message: m, Group: "view"})
			}
		}
	}
	if out, err := c.Transaction(ctx, acks, nil); err != nil || out != queenx.OutcomeCommitted {
		t.Fatalf("ack transaction: outcome %s err %v", out, err)
	}
}

// TestStaleLease proves the third verdict: a worker whose lease expired before
// it committed gets Stale and must drop its work instead of retrying it.
//
// Note what makes it stale: the ack group's worker is resolved from
// queen.log_consumers, i.e. the partition's CURRENT holder, and the lease held
// by that row has expired. If another worker had already re-claimed the
// partition, the row would carry a LIVE lease and this same bundle would be
// accepted -- it is dedup on the deterministic transactionId, not the lease,
// that keeps a recomputed tick from being written twice.
func TestStaleLease(t *testing.T) {
	c, ctx := client(t)

	queue := fmt.Sprintf("queenx-stale-%d", time.Now().UnixNano())
	partition := "h-0005-0005"
	if err := c.Configure(ctx, queue, queenx.QueueOptions{
		LeaseTimeSeconds:   1,
		RetryLimit:         3,
		DedupWindowSeconds: 60,
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if _, err := c.Push(ctx, queue, partition, "car:7:0", map[string]any{"car": 7, "tick": 0}); err != nil {
		t.Fatalf("push: %v", err)
	}

	claim, err := c.PopOne(ctx, queue, queenx.QueueModeGroup, 100, true)
	if err != nil || claim.Empty() {
		t.Fatalf("pop: %v %+v", err, claim)
	}

	// Outlive the 1 s lease.
	time.Sleep(3 * time.Second)

	out, err := c.Transaction(ctx,
		[]queenx.Ack{{Message: claim.Messages[0]}},
		[]queenx.Push{{Queue: queue, Partition: partition, TransactionID: "car:7:1", Payload: map[string]any{"car": 7, "tick": 1}}})
	if err != nil {
		t.Fatalf("stale transaction returned an error, want the stale verdict: %v", err)
	}
	if out != queenx.OutcomeStale {
		t.Fatalf("stale transaction: outcome %s, want stale", out)
	}
}
