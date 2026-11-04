package relay

import (
	"context"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func queryQueueEmpty(w *controlWriter) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.queries) == 0 && len(w.telemetry) == 0 && len(w.order) == 0
}

func TestControlWriterExpiredQueryDoesNotBlockReplacement(t *testing.T) {
	for _, end := range []string{"deadline", "cancel"} {
		t.Run(end, func(t *testing.T) {
			w, p := writerPair(t, 2, 4, 1, 4096)
			release, first := blockWriter(t, w)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deadline := time.Time{}
			if end == "deadline" {
				deadline = time.Now().Add(30 * time.Millisecond)
			}
			if _, err := w.offerUntil("query:old", ctx, deadline, func(time.Time) (protocol.Message, error) {
				t.Error("expired queued query was built")
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			w.mu.Lock()
			old := w.telemetry["query:old"]
			w.mu.Unlock()
			if end == "cancel" {
				cancel()
			}
			select {
			case <-old.ctx.Done():
			case <-time.After(wait):
				t.Fatal("query context did not finish")
			}
			// The writer remains behind its gate. No pop or subsequent offer
			// may be needed to reclaim the expired query and its diagnostics.
			waitFor(t, "expired queued query proactively reclaimed", func() bool { return queryQueueEmpty(w) })
			if _, err := w.offer("query:new", context.Background(), func(time.Time) (protocol.Message, error) {
				return &protocol.HeartbeatAck{AckSeq: 23}, nil
			}); err != nil {
				t.Fatal("new query rejected after prior deadline", err)
			}
			critical := make(chan error, 1)
			go func() { critical <- w.send(context.Background(), &protocol.Hello{OICVersion: "after-expiry"}) }()
			waitFor(t, "critical message queued", func() bool { return len(w.critical) == 1 })
			release()
			for seq := uint64(1); seq <= 3; seq++ {
				m := p.recv()
				if protocol.ControlHeader(m).Seq != seq {
					t.Fatal("expired query consumed wire sequence", m)
				}
				if seq == 3 {
					if ack, ok := m.(*protocol.HeartbeatAck); !ok || ack.AckSeq != 23 {
						t.Fatal("replacement query did not publish", m)
					}
				}
			}
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			if err := <-critical; err != nil || w.ctx.Err() != nil {
				t.Fatal("ordinary expiry disconnected healthy session", err, w.ctx.Err())
			}
			waitFor(t, "replacement query released", func() bool { return queryQueueEmpty(w) })
		})
	}
}

func TestControlWriterLateReleaseKeepsNewQueryIdentity(t *testing.T) {
	w, p := writerPair(t, 2, 4, 1, 4096)
	release, first := blockWriter(t, w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	build := func(time.Time) (protocol.Message, error) { return &protocol.HeartbeatAck{AckSeq: 31}, nil }
	if _, err := w.offer("query:key", ctx, build); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	old := w.telemetry["query:key"]
	w.mu.Unlock()
	cancel()
	waitFor(t, "old query removed", func() bool { return queryQueueEmpty(w) })
	// Wire IDs cannot be reused in a session. Deliberately reuse the internal
	// key here to prove that a stale callback/release cannot erase a new owner.
	if _, err := w.offer("query:key", context.Background(), build); err != nil {
		t.Fatal(err)
	}
	w.release(old)
	w.mu.Lock()
	queries, queued, order := len(w.queries), len(w.telemetry), len(w.order)
	w.mu.Unlock()
	if queries != 1 || queued != 1 || order != 1 {
		t.Fatal("old release removed replacement identity", queries, queued, order)
	}
	release()
	_ = p.recv()
	if ack, ok := p.recv().(*protocol.HeartbeatAck); !ok || ack.AckSeq != 31 {
		t.Fatal("replacement lost", ack)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestControlWriterSelectedQueryCancellation(t *testing.T) {
	w, p := writerPair(t, 2, 4, 1, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	if _, err := w.offer("query:selected", ctx, func(time.Time) (protocol.Message, error) {
		close(entered)
		select {
		case <-release:
		case <-w.ctx.Done():
		}
		return &protocol.HeartbeatAck{AckSeq: 99}, nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(wait):
		t.Fatal("query was not selected")
	}
	cancel()
	w.mu.Lock()
	queries, queued := len(w.queries), len(w.telemetry)
	w.mu.Unlock()
	if queries != 1 || queued != 0 {
		t.Fatal("queued cancellation released in-flight owner", queries, queued)
	}
	if _, err := w.offer("query:new", context.Background(), func(time.Time) (protocol.Message, error) { return nil, nil }); err == nil {
		t.Fatal("in-flight query slot bypassed")
	}
	close(release)
	waitFor(t, "cancelled selected query released", func() bool { return queryQueueEmpty(w) })
	sent := make(chan error, 1)
	go func() { sent <- w.send(context.Background(), &protocol.Hello{OICVersion: "still-connected"}) }()
	m := p.recv()
	if h, ok := m.(*protocol.Hello); !ok || h.Seq != 1 || h.OICVersion != "still-connected" {
		t.Fatal("cancelled builder wrote a stale response or used sequence", m)
	}
	if err := <-sent; err != nil || w.ctx.Err() != nil {
		t.Fatal("cancel before socket write closed session", err, w.ctx.Err())
	}
}
