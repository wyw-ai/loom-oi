package observation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}
type fakeTimer struct {
	owner   *fakeClock
	at      time.Time
	fn      func()
	stopped bool
}

func newFakeClock() *fakeClock      { return &fakeClock{now: time.Now()} }
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{owner: c, at: c.now.Add(d), fn: f}
	c.timers = append(c.timers, t)
	return t
}
func (t *fakeTimer) Stop() bool {
	t.owner.mu.Lock()
	defer t.owner.mu.Unlock()
	old := t.stopped
	t.stopped = true
	return !old
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var callbacks []func()
	for _, t := range c.timers {
		if !t.stopped && !c.now.Before(t.at) {
			t.stopped = true
			callbacks = append(callbacks, t.fn)
		}
	}
	c.mu.Unlock()
	for _, f := range callbacks {
		f()
	}
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("event did not arrive")
		var zero T
		return zero
	}
}
func schedulerJob(t *testing.T, id string) Job {
	t.Helper()
	o := testOptions()
	o.URL = "http://127.0.0.1:8080/" + id
	p, err := NewPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	return Job{Binding: Binding{AccessID: "lab", ExposureID: id, Revision: "r1", ObservationRevision: "ob_1", ContractDigest: p.Digest(), Epoch: 1, Generation: 1, SourceID: "local", ContractVersion: 1}, Plan: p, Dial: func(context.Context, string, string) (net.Conn, error) { return nil, ErrAddress }}
}

type execution struct {
	binding Binding
	release chan struct{}
	ctx     context.Context
	started time.Time
	wait    time.Duration
}

func schedulerFixture(t *testing.T, workers, queue, maximum int) (*Scheduler, *fakeClock, chan execution, chan WorkToken) {
	t.Helper()
	clock := newFakeClock()
	entered := make(chan execution, 64)
	finished := make(chan WorkToken, 64)
	var seq atomic.Uint64
	// The executor receives its identity through the context-independent plan
	// start callback, paired per worker by a work token kept in a small map.
	var mu sync.Mutex
	starts := map[string]execution{}
	s, err := NewScheduler(SchedulerOptions{Workers: workers, Queue: queue, MaxExposures: maximum, Clock: clock, Jitter: func(time.Duration) time.Duration { return 0 },
		OnStart: func(b Binding, at time.Time, wait time.Duration) (WorkToken, bool) {
			mu.Lock()
			starts[b.ExposureID] = execution{binding: b, started: at, wait: wait}
			mu.Unlock()
			return WorkToken{Binding: b, WorkID: seq.Add(1)}, true
		},
		Execute: func(ctx context.Context, p *Plan, d DialFunc, at time.Time) Outcome {
			mu.Lock()
			e := starts[strings.TrimPrefix(p.URL(), "http://127.0.0.1:8080/")]
			mu.Unlock()
			e.ctx, e.release = ctx, make(chan struct{})
			entered <- e
			select {
			case <-e.release:
			case <-ctx.Done():
			}
			return Outcome{Readiness: Unknown, Reason: Timeout}
		},
		OnFinish: func(w WorkToken, _ Outcome, _ time.Time) { finished <- w },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return s, clock, entered, finished
}

func TestSchedulerBoundedFairness(t *testing.T) {
	s, clock, entered, finished := schedulerFixture(t, 2, 4, 16)
	if got := s.Stats(); got.Jobs != 0 || got.Active != 0 || len(clock.timers) != 0 {
		t.Fatalf("idle scheduler: %+v", got)
	}
	for i := range 16 {
		if err := s.Update(schedulerJob(t, fmt.Sprintf("e%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	first, second := receive(t, entered), receive(t, entered)
	seen[first.binding.ExposureID], seen[second.binding.ExposureID] = true, true
	// Keep one target blocked. Other targets must complete and publish without
	// waiting for that target or a complete round. Advancing time creates due
	// pressure while Q remains bounded, rather than relying on goroutine sleeps.
	current := second
	for len(seen) < 16 {
		clock.Advance(100 * time.Millisecond)
		close(current.release)
		receive(t, finished)
		current = receive(t, entered)
		seen[current.binding.ExposureID] = true
		if now := clock.Now().Sub(first.started); now > 2500*time.Millisecond {
			t.Fatalf("starved identities after %v: %v", now, seen)
		}
	}
	stats := s.Stats()
	if stats.Active != 2 || stats.MaxActive != 2 || stats.MaxQueued != 4 || stats.Queued > 4 || stats.Jobs != 16 || stats.Coalesced == 0 {
		t.Fatalf("bounds: %+v", stats)
	}
	if current.wait <= 0 {
		t.Fatal("queue wait was not measured from initial due")
	}
	// Completing one target does not implicitly complete the slow target.
	select {
	case <-first.ctx.Done():
		t.Fatal("unrelated probe cancelled")
	default:
	}
}

func TestSchedulerReplacementWaitsForExit(t *testing.T) {
	clock := newFakeClock()
	started := make(chan Binding, 8)
	release := make(chan struct{})
	ended := make(chan WorkToken, 8)
	var count atomic.Uint64
	s, err := NewScheduler(SchedulerOptions{Workers: 2, Queue: 2, MaxExposures: 2, Clock: clock,
		OnStart: func(b Binding, _ time.Time, _ time.Duration) (WorkToken, bool) {
			started <- b
			return WorkToken{Binding: b, WorkID: count.Add(1)}, true
		},
		Execute: func(ctx context.Context, _ *Plan, _ DialFunc, _ time.Time) Outcome {
			<-release
			return Outcome{Readiness: Unknown, Reason: Timeout}
		},
		OnFinish: func(w WorkToken, _ Outcome, _ time.Time) { ended <- w },
	})
	if err != nil {
		t.Fatal(err)
	}
	a := schedulerJob(t, "one")
	if err = s.Update(a); err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	b := a
	b.Binding.ObservationRevision = "ob_2"
	if err = s.Update(b); err != nil {
		t.Fatal(err)
	}
	if err = s.Update(a); err != nil {
		t.Fatal(err)
	} // A->B->A still cannot overlap.
	if got := s.Stats(); got.Active != 1 || got.Due != 1 || got.Queued != 0 {
		t.Fatalf("old slot released early: %+v", got)
	}
	select {
	case <-started:
		t.Fatal("replacement overlapped old worker")
	default:
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close falsely joined worker: %v", err)
	}
	if got := s.Stats(); got.Active != 1 {
		t.Fatal(got)
	}
	close(release)
	receive(t, ended)
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.Stats(); got.Active != 0 || got.Jobs != 0 || got.Started != 1 || got.Finished != 1 {
		t.Fatal(got)
	}
	if err = s.Update(a); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestSchedulerRemoveRestartAndReject(t *testing.T) {
	clock := newFakeClock()
	starts := make(chan Binding, 16)
	finishes := make(chan struct{}, 16)
	accepted := atomic.Bool{}
	s, err := NewScheduler(SchedulerOptions{Workers: 1, Queue: 1, MaxExposures: 2, Clock: clock, Jitter: func(d time.Duration) time.Duration { return 2 * d },
		OnStart: func(b Binding, _ time.Time, _ time.Duration) (WorkToken, bool) {
			ok := accepted.Load()
			starts <- b
			return WorkToken{Binding: b, WorkID: 1}, ok
		},
		Execute: func(ctx context.Context, _ *Plan, _ DialFunc, _ time.Time) Outcome {
			<-ctx.Done()
			return Outcome{Readiness: Unknown, Reason: Timeout}
		},
		OnFinish: func(WorkToken, Outcome, time.Time) { finishes <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	j := schedulerJob(t, "one")
	if err = s.Update(j); err != nil {
		t.Fatal(err)
	}
	receive(t, starts)
	// Synchronize with a rejected callback's slot release without timing sleeps.
	s.mu.Lock()
	timerVersion := s.timerVersion
	s.mu.Unlock()
	accepted.Store(true)
	clock.Advance(j.Plan.Interval() + j.Plan.Jitter())
	// A rejected attempt can be followed by one queued due. If its callback
	// reached release later, pump still observes the same marked due identity.
	receive(t, starts)
	s.Remove("lab", "one")
	receive(t, finishes)
	if err = s.Update(j); err != nil {
		t.Fatal(err)
	}
	receive(t, starts)
	// A stale timer callback cannot add work or alter the new timer.
	s.tick(timerVersion)
	s.Remove("lab", "missing")
	s.Remove("lab", "one")
	receive(t, finishes)
}

func TestSchedulerValidationAndQueueRemoval(t *testing.T) {
	for _, o := range []SchedulerOptions{{}, {Workers: 2, Queue: 1, MaxExposures: 1}, {Workers: 1, Queue: 2, MaxExposures: 1}} {
		if _, err := NewScheduler(o); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	s, clock, entered, _ := schedulerFixture(t, 1, 1, 2)
	j := schedulerJob(t, "one")
	bad := j
	bad.Binding.ContractDigest = "wrong"
	if !errors.Is(s.Update(bad), ErrBinding) {
		t.Fatal("bad digest")
	}
	if err := s.Update(j); err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	if err := s.Update(j); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(schedulerJob(t, "two")); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Update(schedulerJob(t, "three")), ErrCapacity) {
		t.Fatal("unbounded identities")
	}
	s.Remove("lab", "two")
	if err := s.Update(schedulerJob(t, "three")); err != nil {
		t.Fatal(err)
	}
	clock.Advance(10 * time.Second)
	if got := s.Stats(); got.Active != 1 || got.Queued != 1 || got.Due != 2 {
		t.Fatal(got)
	}
	s.Remove("lab", "three")
	if got := s.Stats(); got.Queued != 0 || got.Jobs != 1 {
		t.Fatal(got)
	}
}
