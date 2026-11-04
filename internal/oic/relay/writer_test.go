package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func writerPair(t *testing.T, critical, telemetry, queries, maxBytes int) (*controlWriter, *peer) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	quit := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(rw, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.ControlSubprotocolV2}})
		if err != nil {
			return
		}
		accepted <- c
		<-quit
		_ = c.CloseNow()
	}))
	c, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{Subprotocols: []string{protocol.ControlSubprotocolV2}})
	if err != nil {
		close(quit)
		srv.Close()
		t.Fatal(err)
	}
	p := &peer{t: t, c: <-accepted}
	w := newControlWriter(context.Background(), c, critical, telemetry, queries, maxBytes, 100*time.Millisecond)
	t.Cleanup(func() { w.stop(); close(quit); srv.Close() })
	return w, p
}

func blockWriter(t *testing.T, w *controlWriter) (func(), <-chan error) {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	go func() {
		done <- w.sendBuilt(context.Background(), func(time.Time) (protocol.Message, error) {
			close(entered)
			select {
			case <-release:
			case <-w.ctx.Done():
				return nil, w.ctx.Err()
			}
			return &protocol.Hello{OICVersion: "first"}, nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(wait):
		t.Fatal("writer did not enter barrier")
	}
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	return free, done
}

func TestControlWriterFairSequenceAndReservedLane(t *testing.T) {
	w, p := writerPair(t, 16, 4, 1, 4096)
	release, first := blockWriter(t, w)
	results := make(chan error, 9)
	for i := range 9 {
		go func() { results <- w.send(context.Background(), &protocol.Hello{OICVersion: fmt.Sprint(i)}) }()
	}
	waitFor(t, "critical queue", func() bool { return len(w.critical) == 9 })
	merged, err := w.offer("dirty:lab-a/http", context.Background(), func(time.Time) (protocol.Message, error) { return &protocol.HeartbeatAck{AckSeq: 77}, nil })
	if merged || err != nil {
		t.Fatal(merged, err)
	}
	merged, err = w.offer("dirty:lab-a/http", context.Background(), func(time.Time) (protocol.Message, error) { return &protocol.HeartbeatAck{AckSeq: 99}, nil })
	if !merged || err != nil {
		t.Fatal("dirty was not coalesced", merged, err)
	}
	release()
	for i := range 11 {
		m := p.recv()
		if protocol.ControlHeader(m).Seq != uint64(i+1) {
			t.Fatal("sequence assigned before fair dequeue", m)
		}
		if i == 8 {
			ack, ok := m.(*protocol.HeartbeatAck)
			if !ok || ack.AckSeq != 99 {
				t.Fatalf("no telemetry opportunity after 8 critical writes: %T %+v", m, m)
			}
		} else if _, ok := m.(*protocol.Hello); !ok {
			t.Fatalf("unexpected order at %d: %T", i, m)
		}
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	for range 9 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestControlWriterQuerySlotsAndAbsoluteDeadline(t *testing.T) {
	w, p := writerPair(t, 2, 4, 1, 4096)
	release, first := blockWriter(t, w)
	build := func(time.Time) (protocol.Message, error) { t.Error("expired response builder ran"); return nil, nil }
	if _, err := w.offerUntil("query:first", context.Background(), time.Now().Add(-time.Millisecond), build); err != nil {
		t.Fatal(err)
	}
	if !queryQueueEmpty(w) {
		t.Fatal("already expired query took a slot")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := w.offerUntil("query:live", ctx, time.Now().Add(time.Hour), build); err != nil {
		t.Fatal(err)
	}
	if _, err := w.offer("query:second", context.Background(), build); err == nil {
		t.Fatal("query slot bound bypassed")
	}
	if _, err := w.offer("query:live", context.Background(), build); err == nil {
		t.Fatal("duplicate pending query accepted")
	}
	cancel()
	waitFor(t, "cancelled query release without dequeue", func() bool { return queryQueueEmpty(w) })
	second := make(chan error, 1)
	go func() { second <- w.send(context.Background(), &protocol.Hello{OICVersion: "second"}) }()
	waitFor(t, "reserved critical lane", func() bool { return len(w.critical) == 1 })
	release()
	for seq := uint64(1); seq <= 2; seq++ {
		if m := p.recv(); protocol.ControlHeader(m).Seq != seq {
			t.Fatal("expired telemetry consumed wire sequence", m)
		}
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	waitFor(t, "query release", func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.queries) == 0 })
}

func TestControlWriterTelemetryBoundDoesNotShrinkRegister(t *testing.T) {
	w, p := writerPair(t, 2, 2, 1, 128)
	b := protocol.ObservationBinding{AccessID: "lab-a", Epoch: 1, Generation: 1, ExposureID: "lab-a/http", Revision: "rv_" + strings.Repeat("a", 26), ObservationRevision: "ob_1", ContractDigest: protocol.NullObservationDigest}
	large := func() protocol.Message {
		return &protocol.Register{Exposures: []protocol.RegisterExposure{{ExposureID: b.ExposureID, Revision: b.Revision, Protocol: "http", TransportProfile: protocol.TransportTCP, AdmissionMode: protocol.AdmissionBestEffort, Description: strings.Repeat("x", 1024), Binding: &b, ProbeSupport: protocol.ProbeUnsupported}}}
	}
	done := make(chan error, 1)
	go func() { done <- w.send(context.Background(), large()) }()
	if m := p.recv(); len(m.(*protocol.Register).Exposures[0].Description) != 1024 {
		t.Fatal("critical frame was truncated")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := w.offer("dirty:large", context.Background(), func(time.Time) (protocol.Message, error) {
		return large(), nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.done:
	case <-time.After(wait):
		t.Fatal("oversize telemetry did not close bounded session")
	}
}

func TestControlWriterOverflowAndCancelledQueue(t *testing.T) {
	w, _ := writerPair(t, 1, 1, 1, 4096)
	release, first := blockWriter(t, w)
	queued := make(chan error, 1)
	go func() { queued <- w.send(context.Background(), &protocol.Hello{OICVersion: "pending"}) }()
	waitFor(t, "full critical lane", func() bool { return len(w.critical) == 1 })
	if err := w.send(context.Background(), &protocol.Hello{OICVersion: "excess"}); protocol.CodeOf(err) != protocol.CodeResourceExhausted {
		t.Fatal(err)
	}
	release()
	if err := <-first; err == nil {
		t.Fatal("cancelled writer returned success")
	}
	if err := <-queued; err == nil {
		t.Fatal("cancelled queued writer returned success")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.send(ctx, &protocol.Hello{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := w.offer("dirty:closed", context.Background(), func(time.Time) (protocol.Message, error) { return nil, nil }); err == nil {
		t.Fatal("closed telemetry accepted")
	}
}
