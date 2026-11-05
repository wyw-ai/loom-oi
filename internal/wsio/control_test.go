package wsio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

type controlClock struct {
	mu sync.Mutex
	at time.Time
}

func (m *controlClock) now() time.Time   { m.mu.Lock(); defer m.mu.Unlock(); return m.at }
func (m *controlClock) set(at time.Time) { m.mu.Lock(); m.at = at; m.mu.Unlock() }

type controlCall struct {
	kind        string
	payload     []byte // original borrowed bytes, deliberately examined before return
	ctx         context.Context
	returnWrite chan error
	returned    chan struct{}
}
type controlRecorder struct{ calls chan *controlCall }

func (w *controlRecorder) WritePing(ctx context.Context, b []byte) error {
	return w.write(ctx, "ping", b)
}
func (w *controlRecorder) WritePong(ctx context.Context, b []byte) error {
	return w.write(ctx, "pong", b)
}
func (w *controlRecorder) write(ctx context.Context, kind string, b []byte) error {
	call := &controlCall{kind: kind, payload: b, ctx: ctx, returnWrite: make(chan error, 1), returned: make(chan struct{})}
	defer close(call.returned)
	select {
	case w.calls <- call:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-call.returnWrite:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func controlFixture(t *testing.T, startWorker bool) (*Control, *controlClock, *controlRecorder, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	clock := &controlClock{at: time.Now()}
	c := NewControl(clock.now)
	w := &controlRecorder{calls: make(chan *controlCall, 1)}
	t.Cleanup(func() {
		c.Stop()
		if err := c.Join(ctx); err != nil {
			t.Error("control cleanup", err)
		}
		cancel()
	})
	if startWorker {
		if err := c.Start(w); err != nil {
			t.Fatal(err)
		}
	}
	return c, clock, w, ctx
}

func nextControlCall(t *testing.T, ctx context.Context, w *controlRecorder, kind string) *controlCall {
	t.Helper()
	select {
	case call := <-w.calls:
		if call.kind != kind {
			t.Fatalf("write kind = %s, want %s", call.kind, kind)
		}
		if _, ok := call.ctx.Deadline(); ok {
			t.Fatal("worker inherited a per-operation deadline")
		}
		return call
	case <-ctx.Done():
		t.Fatal("control Write entry", ctx.Err())
		return nil
	}
}

func TestControlPayloadOwnership(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
	}{{"empty", 0}, {"maximum", 125}} {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.n
			c, _, w, ctx := controlFixture(t, false)
			original := bytes.Repeat([]byte{'a'}, n)
			short, cancel := context.WithCancel(context.Background())
			if c.OnPing(short, original) {
				t.Fatal("default synchronous PONG was enabled")
			}
			cancel()
			for i := range original {
				original[i] = 'x'
			}
			if err := c.Start(w); err != nil {
				t.Fatal(err)
			}
			first := nextControlCall(t, ctx, w, "pong")
			if !bytes.Equal(first.payload, bytes.Repeat([]byte{'a'}, n)) || first.ctx.Err() != nil {
				t.Fatal("borrowed callback payload/context escaped")
			}
			c.OnPing(context.Background(), []byte("discarded"))
			latest := []byte("latest")
			c.OnPing(context.Background(), latest)
			latest[0] = 'x'
			s := c.Snapshot()
			if s.InflightPongBytes != n || s.PendingPongBytes != 6 || !bytes.Equal(first.payload, bytes.Repeat([]byte{'a'}, n)) {
				t.Fatal("in-flight PONG was replaced", s)
			}
			first.returnWrite <- nil
			second := nextControlCall(t, ctx, w, "pong")
			if string(second.payload) != "latest" {
				t.Fatal("latest unclaimed PONG missing")
			}
			c.Stop()
			if err := c.Join(ctx); err != nil || c.Err() != nil {
				t.Fatal("normal Stop became a write failure", err, c.Err())
			}
		})
	}
}

func TestControlPongHalfOpenWindow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset time.Duration
		accept bool
	}{
		{"before_deadline", -time.Nanosecond, true}, {"at_deadline", 0, false}, {"after_deadline", time.Nanosecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, clock, w, ctx := controlFixture(t, true)
			start := clock.now()
			p := time.Second
			if err := c.EnableProbe(start, p); err != nil {
				t.Fatal(err)
			}
			clock.set(start.Add(p))
			if err := c.Advance(); err != nil {
				t.Fatal(err)
			}
			call := nextControlCall(t, ctx, w, "ping")
			clock.set(start.Add(2*p + tc.offset))
			c.OnPong(context.Background(), call.payload)
			s := c.Snapshot()
			if s.ValidPong != tc.accept || s.Life {
				t.Fatal("PONG classified as DATA or wrong window", s)
			}
			if call.ctx.Err() != nil {
				t.Fatal("window retirement canceled the wire owner")
			}
			clock.set(start.Add(3 * p))
			err := c.Advance()
			if tc.accept && err != nil || !tc.accept && !errors.Is(err, ErrIdleWindows) {
				t.Fatal("wrong consecutive empty-window outcome", err)
			}
		})
	}
}

func TestControlPongIdentity(t *testing.T) {
	for _, kind := range []string{"queued", "wrong_nonce", "short", "other_connection", "duplicate", "retired"} {
		t.Run(kind, func(t *testing.T) {
			c, clock, w, ctx := controlFixture(t, kind != "queued")
			start := clock.now()
			if err := c.EnableProbe(start, time.Second); err != nil {
				t.Fatal(err)
			}
			clock.set(start.Add(time.Second))
			_ = c.Advance()
			var payload [8]byte
			binary.BigEndian.PutUint64(payload[:], 1)
			if kind != "queued" {
				_ = nextControlCall(t, ctx, w, "ping")
			}
			b := payload[:]
			switch kind {
			case "wrong_nonce":
				binary.BigEndian.PutUint64(b, 2)
			case "short":
				b = b[:7]
			case "other_connection":
				other := NewControl(clock.now)
				defer other.Stop()
				other.OnPong(context.Background(), b)
				if other.Snapshot().ValidPong {
					t.Fatal("another connection accepted the probe")
				}
				return
			case "duplicate":
				c.OnPong(context.Background(), b)
				clock.set(start.Add(2 * time.Second))
				_ = c.Advance()
			case "retired":
				clock.set(start.Add(2 * time.Second))
				_ = c.Advance()
			}
			c.OnPong(context.Background(), b)
			if c.Snapshot().ValidPong {
				t.Fatal("invalid or duplicate PONG granted a current window")
			}
		})
	}
}

func TestControlProbeRetirement(t *testing.T) {
	t.Run("queued", func(t *testing.T) {
		c, clock, w, ctx := controlFixture(t, false)
		start := clock.now()
		_ = c.EnableProbe(start, time.Second)
		clock.set(start.Add(time.Second))
		_ = c.Advance()
		clock.set(start.Add(2 * time.Second))
		_ = c.Advance()
		if err := c.Start(w); err != nil {
			t.Fatal(err)
		}
		call := nextControlCall(t, ctx, w, "ping")
		if binary.BigEndian.Uint64(call.payload) != 2 {
			t.Fatal("retired queued PING reached the writer")
		}
	})
	t.Run("claimed", func(t *testing.T) {
		c, clock, w, ctx := controlFixture(t, true)
		start := clock.now()
		_ = c.EnableProbe(start, time.Second)
		clock.set(start.Add(time.Second))
		_ = c.Advance()
		call := nextControlCall(t, ctx, w, "ping")
		clock.set(start.Add(2 * time.Second))
		_ = c.Advance()
		s := c.Snapshot()
		if s.Nonce != 1 || s.Probe != "retired" || s.Wire != "ping" || call.ctx.Err() != nil {
			t.Fatal("probe and wire lifetimes were conflated", s)
		}
		clock.set(start.Add(3 * time.Second))
		if !errors.Is(c.Advance(), ErrIdleWindows) || call.ctx.Err() != nil {
			t.Fatal("idle expiry canceled transport before coordinator close")
		}
		select {
		case <-c.Failed():
			t.Fatal("idle expiry misreported as real write error")
		default:
		}
	})
	t.Run("late_return", func(t *testing.T) {
		c, clock, w, ctx := controlFixture(t, true)
		start := clock.now()
		_ = c.EnableProbe(start, time.Second)
		clock.set(start.Add(time.Second))
		_ = c.Advance()
		call := nextControlCall(t, ctx, w, "ping")
		clock.set(start.Add(2 * time.Second))
		_ = c.Advance()
		// A PONG acts as a worker barrier after the old PING has returned.
		c.OnPing(context.Background(), []byte("barrier"))
		call.returnWrite <- nil
		_ = nextControlCall(t, ctx, w, "pong")
		if s := c.Snapshot(); s.Nonce != 1 || s.Misses != 1 || s.Probe != "retired" {
			t.Fatal("late Write renewed probe/window", s)
		}
		clock.set(start.Add(3 * time.Second))
		if !errors.Is(c.Advance(), ErrIdleWindows) {
			t.Fatal("late successful Write erased an empty window")
		}
	})
}

func TestControlBoundedFairSelection(t *testing.T) {
	c, clock, w, ctx := controlFixture(t, true)
	start := clock.now()
	_ = c.EnableProbe(start, time.Second)
	c.OnPing(context.Background(), []byte("first"))
	first := nextControlCall(t, ctx, w, "pong")
	c.OnPing(context.Background(), []byte("second"))
	clock.set(start.Add(time.Second))
	_ = c.Advance()
	first.returnWrite <- nil
	ping := nextControlCall(t, ctx, w, "ping")
	c.OnPong(context.Background(), ping.payload) // valid before local Write return
	c.OnPing(context.Background(), []byte("latest"))
	ping.returnWrite <- nil
	pong := nextControlCall(t, ctx, w, "pong")
	if string(pong.payload) != "latest" || !c.Snapshot().ValidPong {
		t.Fatal("fair control selection or early PONG failed")
	}
}

func TestControlDataOwners(t *testing.T) {
	for _, slot := range []DataSlot{DataForwarding, DataPending} {
		t.Run([]string{"source_forwarding", "destination_pending"}[slot], func(t *testing.T) {
			c, clock, _, _ := controlFixture(t, false)
			start := clock.now()
			_ = c.EnableProbe(start, time.Second)
			clock.set(start.Add(time.Second))
			_ = c.Advance()
			clock.set(start.Add(1250 * time.Millisecond))
			owner, err := c.BeginData(slot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.BeginData(slot); !errors.Is(err, ErrDataOwner) {
				t.Fatal("duplicate DATA owner accepted", err)
			}
			clock.set(start.Add(1500 * time.Millisecond))
			owner.End()
			owner.End()
			clock.set(start.Add(2 * time.Second))
			if err = c.Advance(); err != nil {
				t.Fatal(err)
			}
			if s := c.Snapshot(); s.Misses != 0 || s.Nonce != 1 || s.Forwarding || s.PendingData {
				t.Fatal("sticky activity was lost or old owner remained", s)
			}
			clock.set(start.Add(3 * time.Second))
			if err = c.Advance(); err != nil {
				t.Fatal(err)
			}
			clock.set(start.Add(4 * time.Second))
			if !errors.Is(c.Advance(), ErrIdleWindows) {
				t.Fatal("finished DATA owner immortalized leg")
			}
		})
	}
	t.Run("two_slots_and_large_pause", func(t *testing.T) {
		c, clock, _, _ := controlFixture(t, false)
		start := clock.now()
		_ = c.EnableProbe(start, time.Nanosecond)
		a, _ := c.BeginData(DataForwarding)
		b, _ := c.BeginData(DataPending)
		clock.set(start.Add(time.Hour))
		if err := c.Advance(); err != nil {
			t.Fatal(err)
		}
		if s := c.Snapshot(); s.Window != uint64(time.Hour) || !s.Life || s.Misses != 0 || s.Nonce != 0 {
			t.Fatal("large busy pause lost bounded sticky state", s)
		}
		a.End()
		b.End()
		clock.set(start.Add(time.Hour + 3))
		if !errors.Is(c.Advance(), ErrIdleWindows) {
			t.Fatal("unowned control state counted as DATA life")
		}
	})
	t.Run("data_not_pong", func(t *testing.T) {
		c, clock, _, _ := controlFixture(t, false)
		start := clock.now()
		_ = c.EnableProbe(start, time.Second)
		clock.set(start.Add(time.Second))
		_ = c.Advance()
		c.Data()
		if s := c.Snapshot(); !s.Life || s.ValidPong {
			t.Fatal("DATA and PONG activity conflated", s)
		}
	})
}

func TestControlLifecycleAndErrors(t *testing.T) {
	t.Run("write_error", func(t *testing.T) {
		c, _, w, ctx := controlFixture(t, true)
		c.OnPing(context.Background(), []byte("x"))
		call := nextControlCall(t, ctx, w, "pong")
		sentinel := errors.New("actual write failure")
		call.returnWrite <- sentinel
		diagnosticWait(t, ctx, c.Failed(), "write failure notification")
		if c.Err() != sentinel || c.Snapshot().Stopped {
			t.Fatal("worker closed transport or lost first error")
		}
		if err := c.Join(ctx); err != nil {
			t.Fatal(err)
		}
		c.OnPing(context.Background(), bytes.Repeat([]byte{1}, 126))
		if c.Err() != sentinel {
			t.Fatal("later input replaced first error")
		}
	})
	t.Run("invalid_payload", func(t *testing.T) {
		c, _, _, ctx := controlFixture(t, true)
		c.OnPing(context.Background(), make([]byte, 126))
		diagnosticWait(t, ctx, c.Failed(), "oversized control input")
		if !errors.Is(c.Err(), ErrControlPayload) {
			t.Fatal(c.Err())
		}
	})
	t.Run("stopped_before_start", func(t *testing.T) {
		c, _, w, ctx := controlFixture(t, false)
		c.Stop()
		c.Stop()
		if !errors.Is(c.Start(w), ErrControlStopped) || c.Join(ctx) != nil {
			t.Fatal("pre-start cleanup")
		}
		if _, err := c.BeginData(DataForwarding); !errors.Is(err, ErrControlStopped) {
			t.Fatal(err)
		}
		if !errors.Is(c.EnableProbe(time.Now(), time.Second), ErrControlStopped) {
			t.Fatal("stopped probe enabled")
		}
		c.OnPing(context.Background(), []byte("discard"))
		c.Data()
		c.OnPong(context.Background(), make([]byte, 8))
		if c.Snapshot().PendingPong {
			t.Fatal("closed owner accepted PONG work")
		}
	})
	t.Run("start_and_clock_validation", func(t *testing.T) {
		c, clock, w, _ := controlFixture(t, false)
		if c.Start(nil) == nil {
			t.Fatal("nil writer accepted")
		}
		if err := c.Start(w); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(c.Start(w), ErrControlStarted) {
			t.Fatal("second worker started")
		}
		if c.EnableProbe(time.Time{}, time.Second) == nil || c.EnableProbe(clock.now(), 0) == nil {
			t.Fatal("invalid probe clock")
		}
		if err := c.EnableProbe(clock.now(), time.Second); err != nil {
			t.Fatal(err)
		}
		if c.EnableProbe(clock.now(), time.Second) == nil {
			t.Fatal("Open reset Attach clock")
		}
		if tick, ok := c.NextTick(); !ok || !tick.Equal(clock.now().Add(time.Second)) {
			t.Fatal("absolute next tick", tick, ok)
		}
		if _, err := c.BeginData(DataSlot(2)); !errors.Is(err, ErrDataOwner) {
			t.Fatal(err)
		}
	})
	t.Run("join_original_deadline", func(t *testing.T) {
		c, _, w, ctx := controlFixture(t, true)
		c.OnPing(context.Background(), nil)
		_ = nextControlCall(t, ctx, w, "pong")
		already, cancel := context.WithCancel(context.Background())
		cancel()
		if !errors.Is(c.Join(already), context.Canceled) || c.Snapshot().Stopped {
			t.Fatal("Join extended deadline or closed transport")
		}
		c.Stop()
		diagnosticWait(t, ctx, c.Done(), "worker finishes on original transport cancellation")
		if err := c.Join(already); err != nil {
			t.Fatal("already-complete join became an expired-context error", err)
		}
	})
	t.Run("nonce_exhausted", func(t *testing.T) {
		c, clock, _, _ := controlFixture(t, false)
		start := clock.now()
		_ = c.EnableProbe(start, time.Second)
		c.nonce = math.MaxUint64
		clock.set(start.Add(time.Second))
		if !errors.Is(c.Advance(), ErrProbeExhausted) || c.Snapshot().Nonce != math.MaxUint64 {
			t.Fatal("nonce wrapped")
		}
	})
	t.Run("long_idle_pause_no_historical_ping", func(t *testing.T) {
		c, clock, _, _ := controlFixture(t, false)
		start := clock.now()
		_ = c.EnableProbe(start, time.Nanosecond)
		clock.set(start.Add(time.Hour))
		if !errors.Is(c.Advance(), ErrIdleWindows) {
			t.Fatal("long pause renewed the observation clock")
		}
		if c.Snapshot().Nonce != 0 {
			t.Fatal("historical PING was fabricated")
		}
		if _, ok := c.NextTick(); ok {
			t.Fatal("terminated probe remains scheduled")
		}
		if _, err := c.BeginData(DataPending); !errors.Is(err, ErrIdleWindows) {
			t.Fatal("expired leg revived", err)
		}
		c.Data()
	})
	t.Run("default_clock", func(t *testing.T) { c := NewControl(nil); c.Stop(); var o *DataOwner; o.End(); (&DataOwner{}).End() })
}
