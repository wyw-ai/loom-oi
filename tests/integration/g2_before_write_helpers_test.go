package integration

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Capture the actual callback outcome before the socket owner's observer and
// cleanup. A later nil-panic notification cannot overwrite the original panic.
func (f *g2CompetitionFinalSelection) observeBeforeWrite(ctx context.Context, callback func() error) (err error) {
	returned := false
	defer func() {
		p := recover()
		if f != nil && (!returned || err != nil) {
			f.freezeBeforeCleanup(p, true)
		}
		if p != nil {
			panic(p)
		}
	}()
	err = callback()
	returned = true
	return err
}

// Cycle zero prepares before the slow helper exists. This context is exactly
// the original preparation deadline, and is never derived from a connection.
func (f *g2CompetitionFinalSelection) preparePreDial() (err error) {
	if f == nil {
		return nil
	}
	if f.beforeCalled {
		return fmt.Errorf("cycle-zero pre-Dial preparation called twice")
	}
	f.beforeCalled = true
	ctx, cancel := context.WithDeadline(context.Background(), f.deadline)
	defer cancel()
	returned := false
	defer func() {
		p := recover()
		if !returned || err != nil {
			f.preparation.preDialContextState = g2DiagnosticPtr(g2DiagnosticContext(ctx))
			f.freezeBeforeCleanup(p, true)
		}
		if p != nil {
			panic(p)
		}
	}()
	f.preparation.preDialContextDeadlineNS = g2DiagnosticPtr(int64(f.deadline.Sub(resourceOrigin)))
	err = func() error {
		if !time.Now().Before(f.deadline) || ctx.Err() != nil {
			return context.DeadlineExceeded
		}
		if err := f.slowInput(f.deadline); err != nil {
			return err
		}
		if err := f.beforeInput(ctx); err != nil {
			return err
		}
		if !time.Now().Before(f.deadline) || ctx.Err() != nil {
			return context.DeadlineExceeded
		}
		f.beforeReady = true
		return nil
	}()
	returned = true
	return err
}

// This loop returns errors to the preparation owner before any Dial. It keeps
// the original full short circuit, Snapshot/young order and ten-millisecond
// observation cadence.
func (d *g2PreparationFailure) preDialSelection(ctx context.Context, g *g2Env, hc *http.Client) error {
	d.Phase = "pre_dial_full"
	deadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("pre-Dial selection has no original deadline")
	}
	for time.Now().Before(deadline) && ctx.Err() == nil {
		s, err := d.sampleContext(ctx, g, hc)
		if err != nil {
			return err
		}
		d.full(s)
		snapshot := g.paths["lab-a"].Snapshot()
		snapshotNS := resourceNS()
		qualified := g2CompetitionFull(s) && d.youngWithin(snapshot.Events, resourceNS(), 80*time.Millisecond)
		if d.returned(qualified, snapshot, snapshotNS) {
			return d.acceptedPreDial(ctx)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return context.DeadlineExceeded
}

func (d *g2PreparationFailure) acceptedPreDial(ctx context.Context) error {
	w := d.lastQueryWitness
	accepted := resourceNS()
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline || int64(deadline.Sub(resourceOrigin)) != d.DeadlineNS || ctx.Err() != nil || accepted > int64(deadline.Sub(resourceOrigin)) ||
		d.preDialQueryWitness != nil || w == nil || w.Attempt == nil || w.CallbackReturnedNS == nil || w.MaxAgeMS != 80 ||
		!g2CompetitionFull(d.lastSample) || !g2ProbeQueueMatchesQueries(d.preloadQueueWitness, w) {
		return fmt.Errorf("pre-Dial full/query observation did not qualify inside the original deadline")
	}
	// The standalone accepted witness survives history overflow and subsequent
	// final samples. None of these pointers may refer to mutable sample scratch.
	copy := *w
	copy.Queries = append([]g2PreparationQuery(nil), w.Queries...)
	copy.Attempt = g2DiagnosticPtr(*w.Attempt)
	copy.CallbackReturnedNS = g2DiagnosticPtr(*w.CallbackReturnedNS)
	copy.Sample = g2DiagnosticPtr(g2CopyCompetitionSample(d.lastSample))
	d.preDialQueryWitness = &copy
	d.preDialAcceptedNS = g2DiagnosticPtr(accepted)
	return nil
}
