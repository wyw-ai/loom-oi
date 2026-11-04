package store

import (
	"context"
	"sync"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Serialized funnels every write through one goroutine with a bounded
// queue of WRITE_QUEUE jobs; a full queue returns ErrBusy at once (B3 §3).
// Reads pass straight to the wrapped store.
type Serialized struct {
	Store
	jobs chan func()
	stop chan struct{}
	once sync.Once
	wg   sync.WaitGroup
}

// NewSerialized starts the writer goroutine.
func NewSerialized(s Store, queue int) *Serialized {
	w := &Serialized{Store: s, jobs: make(chan func(), queue), stop: make(chan struct{})}
	w.wg.Add(1)
	go w.loop()
	return w
}

func (w *Serialized) loop() {
	defer w.wg.Done()
	for {
		// Check stop first: once Close is called, queued jobs are dropped
		// deterministically instead of racing the stop signal.
		select {
		case <-w.stop:
			return
		default:
		}
		select {
		case job := <-w.jobs:
			job()
		case <-w.stop:
			return
		}
	}
}

// run queues fn and always waits for it: a caller never abandons a write
// that might still commit. fn observes ctx, so cancellation aborts it.
func (w *Serialized) run(fn func()) error {
	done := make(chan struct{})
	select {
	case w.jobs <- func() { fn(); close(done) }:
	default:
		return ErrBusy
	}
	select {
	case <-done:
		return nil
	case <-w.stop:
		// Shutdown: the loop finishes its current job before exiting, so
		// report success only for a job that actually ran.
		w.wg.Wait()
		select {
		case <-done:
			return nil
		default:
			return ErrUnavailable
		}
	}
}

// ApplyConfig is serialized.
func (w *Serialized) ApplyConfig(ctx context.Context, expected int64, muts []Mutation, audit []AuditEntry) (int64, error) {
	var v int64
	var err error
	if qerr := w.run(func() { v, err = w.Store.ApplyConfig(ctx, expected, muts, audit) }); qerr != nil {
		return 0, qerr
	}
	return v, err
}

// UpsertRegistry is serialized.
func (w *Serialized) UpsertRegistry(ctx context.Context, accessID string, exposures []Exposure, audit []AuditEntry) error {
	var err error
	if qerr := w.run(func() { err = w.Store.UpsertRegistry(ctx, accessID, exposures, audit) }); qerr != nil {
		return qerr
	}
	return err
}

// StartRun is serialized.
func (w *Serialized) StartRun(ctx context.Context, runID string, at time.Time) (int64, error) {
	var e int64
	var err error
	if qerr := w.run(func() { e, err = w.Store.StartRun(ctx, runID, at) }); qerr != nil {
		return 0, qerr
	}
	return e, err
}

// RaiseEpoch is serialized.
func (w *Serialized) RaiseEpoch(ctx context.Context, min int64, audit AuditEntry) (bool, error) {
	var raised bool
	var err error
	if qerr := w.run(func() { raised, err = w.Store.RaiseEpoch(ctx, min, audit) }); qerr != nil {
		return false, qerr
	}
	return raised, err
}

// AppendAudit is serialized.
func (w *Serialized) AppendAudit(ctx context.Context, entries []AuditEntry) error {
	var err error
	if qerr := w.run(func() { err = w.Store.AppendAudit(ctx, entries) }); qerr != nil {
		return qerr
	}
	return err
}

// ProbeWrite is serialized.
func (w *Serialized) ProbeWrite(ctx context.Context) error {
	var err error
	if qerr := w.run(func() { err = w.Store.ProbeWrite(ctx) }); qerr != nil {
		return qerr
	}
	return err
}

// Maintain is serialized: maintenance holds the single writer.
func (w *Serialized) Maintain(ctx context.Context, req protocol.StoreMaintain) (protocol.StoreMaintained, error) {
	var out protocol.StoreMaintained
	var err error
	if qerr := w.run(func() { out, err = w.Store.Maintain(ctx, req) }); qerr != nil {
		return out, qerr
	}
	return out, err
}

// PruneAudit is serialized.
func (w *Serialized) PruneAudit(ctx context.Context, throughSeq int64, digest string, audit AuditEntry) (int64, error) {
	var n int64
	var err error
	if qerr := w.run(func() { n, err = w.Store.PruneAudit(ctx, throughSeq, digest, audit) }); qerr != nil {
		return 0, qerr
	}
	return n, err
}

// Close stops the writer after the job in progress and closes the store.
func (w *Serialized) Close() error {
	w.once.Do(func() { close(w.stop) })
	w.wg.Wait()
	return w.Store.Close()
}
