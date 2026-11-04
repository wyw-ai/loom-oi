package observation

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"
)

var ErrClosed = errors.New("observation scheduler closed")

// Clock is monotonic in production. AfterFunc must not invoke f synchronously.
// Test clocks may advance explicitly; they must preserve this callback rule.
type Clock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) Timer
}
type Timer interface{ Stop() bool }

type systemClock struct{}

func (systemClock) Now() time.Time                            { return time.Now() }
func (systemClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

type Job struct {
	Binding Binding
	Plan    *Plan
	Dial    DialFunc
}

type SchedulerOptions struct {
	Workers, Queue, MaxExposures int
	Clock                        Clock
	// Jitter is a nonblocking local random source. Its result is clamped to
	// [-limit,+limit]; it cannot alter a plan's configured bounds.
	Jitter func(limit time.Duration) time.Duration
	// OnStart must atomically check the complete binding and call Table.Begin.
	// A rejected start did not begin an attempt and receives no OnFinish.
	OnStart  func(Binding, time.Time, time.Duration) (WorkToken, bool)
	OnFinish func(WorkToken, Outcome, time.Time)
	// Execute is an internal injection point for deterministic executor tests.
	// A nil value uses the fixed HTTP executor; production adapters need not set it.
	Execute func(context.Context, *Plan, DialFunc, time.Time) Outcome
}

type SchedulerStats struct {
	Jobs, Active, Queued, Due              int
	MaxActive, MaxQueued                   int
	Started, Finished, Rejected, Coalesced uint64
	MaxWait, OldestDueAge                 time.Duration
}

type schedulerKey struct{ access, exposure string }
type scheduledEntry struct {
	key                  schedulerKey
	job                  *Job
	present, due, queued bool
	dueAt, next          time.Time
	run                  *scheduledRun
}
type scheduledRun struct {
	entry  *scheduledEntry
	job    *Job
	ctx    context.Context
	cancel context.CancelFunc
	dueAt  time.Time
	started bool // guarded by Scheduler.mu; includes the goroutine dispatch wait
}

// Scheduler owns only execution slots and timing. Product locks are never
// acquired while mu is held, and callbacks/network operations run outside mu.
// Cancelled executions keep their slot until both execution and reduction exit.
type Scheduler struct {
	mu           sync.Mutex
	opts         SchedulerOptions
	entries      map[schedulerKey]*scheduledEntry
	order        []*scheduledEntry
	cursor       int
	queue        []*scheduledEntry
	timer        Timer
	timerVersion uint64
	closed       bool
	done         chan struct{}
	doneClosed   bool
	stats        SchedulerStats
}

func NewScheduler(o SchedulerOptions) (*Scheduler, error) {
	if o.MaxExposures < 1 || o.Workers < 1 || o.Queue < 1 || o.Workers > o.MaxExposures || o.Queue > o.MaxExposures || o.OnStart == nil || o.OnFinish == nil {
		return nil, errors.New("invalid observation scheduler bounds or callbacks")
	}
	if o.Clock == nil {
		o.Clock = systemClock{}
	}
	if o.Jitter == nil {
		o.Jitter = func(d time.Duration) time.Duration {
			return time.Duration(rand.Int64N(int64(2*d)+1)) - d
		}
	}
	if o.Execute == nil {
		o.Execute = Execute
	}
	return &Scheduler{opts: o, entries: make(map[schedulerKey]*scheduledEntry), done: make(chan struct{})}, nil
}

func (s *Scheduler) Update(j Job) error {
	b := j.Binding
	if j.Plan == nil || j.Dial == nil || b.AccessID == "" || b.ExposureID == "" || b.Epoch <= 0 || b.Generation <= 0 || b.Revision == "" || b.ObservationRevision == "" || b.ContractDigest != j.Plan.Digest() || b.SourceID != "local" || b.ContractVersion != 1 {
		return ErrBinding
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	k := schedulerKey{b.AccessID, b.ExposureID}
	e := s.entries[k]
	if e == nil {
		if len(s.entries) >= s.opts.MaxExposures {
			s.mu.Unlock()
			return ErrCapacity
		}
		e = &scheduledEntry{key: k}
		s.entries[k] = e
		s.order = append(s.order, e)
	} else if e.present && e.job.Binding == b {
		// Full binding includes the immutable plan digest. An unchanged update
		// does not restart timing or replace an executing dial closure.
		s.mu.Unlock()
		return nil
	}
	var cancel context.CancelFunc
	if e.run != nil {
		cancel = e.run.cancel
	}
	s.unqueueLocked(e)
	e.job, e.present, e.due, e.dueAt, e.next = &j, true, true, s.opts.Clock.Now(), time.Time{}
	runs := s.pumpLocked()
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.launch(runs)
	return nil
}

func (s *Scheduler) Remove(accessID, exposureID string) {
	s.mu.Lock()
	e := s.entries[schedulerKey{accessID, exposureID}]
	var cancel context.CancelFunc
	if e != nil {
		e.present, e.due, e.next = false, false, time.Time{}
		s.unqueueLocked(e)
		if e.run != nil {
			cancel = e.run.cancel
		} else {
			s.deleteLocked(e)
		}
	}
	runs := s.pumpLocked()
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.launch(runs)
}

func (s *Scheduler) unqueueLocked(e *scheduledEntry) {
	if !e.queued {
		return
	}
	for i, v := range s.queue {
		if v == e {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			break
		}
	}
	e.queued = false
}

func (s *Scheduler) deleteLocked(e *scheduledEntry) {
	delete(s.entries, e.key)
	for i, v := range s.order {
		if v == e {
			s.order = append(s.order[:i], s.order[i+1:]...)
			if i < s.cursor {
				s.cursor--
			}
			if s.cursor >= len(s.order) {
				s.cursor = 0
			}
			break
		}
	}
}

func (s *Scheduler) fillLocked() {
	// Visit each identity at most once. Due identities that cannot enter Q
	// remain marked; the next released slot resumes from this rotating cursor.
	for visited := 0; visited < len(s.order) && len(s.queue) < s.opts.Queue; visited++ {
		e := s.order[s.cursor]
		s.cursor = (s.cursor + 1) % len(s.order)
		if !e.present || !e.due || e.queued || e.run != nil {
			continue
		}
		e.queued = true
		s.queue = append(s.queue, e)
	}
	if len(s.queue) > s.stats.MaxQueued {
		s.stats.MaxQueued = len(s.queue)
	}
}

func (s *Scheduler) pumpLocked() []*scheduledRun {
	var runs []*scheduledRun
	if !s.closed {
		s.fillLocked()
		for s.stats.Active < s.opts.Workers && len(s.queue) != 0 {
			e := s.queue[0]
			s.queue = s.queue[1:]
			e.queued, e.due = false, false
			ctx, cancel := context.WithCancel(context.Background())
			r := &scheduledRun{entry: e, job: e.job, ctx: ctx, cancel: cancel, dueAt: e.dueAt}
			e.run = r
			s.stats.Active++
			if s.stats.Active > s.stats.MaxActive {
				s.stats.MaxActive = s.stats.Active
			}
			runs = append(runs, r)
			s.fillLocked()
		}
	}
	s.armLocked()
	return runs
}

func (s *Scheduler) armLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.timerVersion++
	if s.closed {
		return
	}
	var next time.Time
	for _, e := range s.order {
		if e.present && !e.next.IsZero() && (next.IsZero() || e.next.Before(next)) {
			next = e.next
		}
	}
	if next.IsZero() {
		return
	}
	version := s.timerVersion
	s.timer = s.opts.Clock.AfterFunc(max(0, next.Sub(s.opts.Clock.Now())), func() { s.tick(version) })
}

func (s *Scheduler) tick(version uint64) {
	s.mu.Lock()
	if s.closed || version != s.timerVersion {
		s.mu.Unlock()
		return
	}
	now := s.opts.Clock.Now()
	for _, e := range s.order {
		if !e.present || e.next.IsZero() || now.Before(e.next) {
			continue
		}
		dueAt := e.next
		e.next = time.Time{}
		if e.due || e.run != nil {
			s.stats.Coalesced++
		}
		if !e.due {
			e.due, e.dueAt = true, dueAt
		}
	}
	runs := s.pumpLocked()
	s.mu.Unlock()
	s.launch(runs)
}

func (s *Scheduler) launch(runs []*scheduledRun) {
	for _, r := range runs {
		go s.execute(r)
	}
}

func (s *Scheduler) execute(r *scheduledRun) {
	defer s.release(r)
	if r.ctx.Err() != nil {
		return
	}
	started := s.opts.Clock.Now()
	s.mu.Lock()
	r.started = true
	s.stats.MaxWait = max(s.stats.MaxWait, started.Sub(r.dueAt))
	if r.entry.present && r.entry.job == r.job {
		j := r.job.Plan.Jitter()
		delta := min(j, max(-j, s.opts.Jitter(j)))
		r.entry.next = started.Add(r.job.Plan.Interval() + delta)
		s.armLocked()
	}
	s.mu.Unlock()
	token, ok := s.opts.OnStart(r.job.Binding, started, max(0, started.Sub(r.dueAt)))
	s.mu.Lock()
	if ok {
		s.stats.Started++
	} else {
		s.stats.Rejected++
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	out := s.opts.Execute(r.ctx, r.job.Plan, r.job.Dial, started)
	s.opts.OnFinish(token, out, s.opts.Clock.Now())
	s.mu.Lock()
	s.stats.Finished++
	s.mu.Unlock()
}

func (s *Scheduler) release(r *scheduledRun) {
	r.cancel()
	s.mu.Lock()
	// A cancelled old revision is still the sole owner of this slot. Update
	// changes only entry.job, never this in-flight identity.
	r.entry.run = nil
	s.stats.Active--
	if !r.entry.present {
		s.deleteLocked(r.entry)
	}
	runs := s.pumpLocked()
	if s.closed && s.stats.Active == 0 && !s.doneClosed {
		close(s.done)
		s.doneClosed = true
	}
	s.mu.Unlock()
	s.launch(runs)
}

// Close cancels all work and joins callbacks. A context timeout reports that
// executions still own slots; it never pretends they were reclaimed.
// Call Close from the session owner, never from OnStart/OnFinish themselves:
// it joins those callbacks. Update and Remove are safe within a callback.
func (s *Scheduler) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	var cancels []context.CancelFunc
	for _, e := range s.order {
		e.present, e.due, e.queued, e.next = false, false, false, time.Time{}
		if e.run != nil {
			cancels = append(cancels, e.run.cancel)
		}
	}
	s.queue = nil
	s.order = nil
	clear(s.entries)
	s.armLocked()
	if s.stats.Active == 0 && !s.doneClosed {
		close(s.done)
		s.doneClosed = true
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) Stats() SchedulerStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.stats
	r.Queued = len(s.queue)
	now := s.opts.Clock.Now()
	for _, e := range s.order {
		if e.present {
			r.Jobs++
		}
		if e.due {
			r.Due++
			r.OldestDueAge = max(r.OldestDueAge, now.Sub(e.dueAt))
		}
		if e.present && !e.next.IsZero() {
			r.OldestDueAge = max(r.OldestDueAge, now.Sub(e.next))
		}
		if e.run != nil && !e.run.started {
			r.OldestDueAge = max(r.OldestDueAge, now.Sub(e.run.dueAt))
		}
	}
	return r
}
