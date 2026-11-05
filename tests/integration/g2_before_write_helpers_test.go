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
			f.preparation.beforeWriteContextState = g2DiagnosticPtr(g2DiagnosticContext(ctx))
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

// This runs only inside the original two-socket beforeWrite boundary. The
// original Zero observation and writer-release timestamp follow its return.
func (f *g2CompetitionFinalSelection) prepareBeforeWrite(ctx context.Context, slow *g2SlowScrapes) error {
	if f == nil {
		return nil
	}
	if f.beforeCalled {
		return fmt.Errorf("cycle-zero beforeWrite preparation called twice")
	}
	f.beforeCalled = true
	dialDeadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("beforeWrite context has no original deadline")
	}
	f.preparation.beforeWriteContextDeadlineNS = g2DiagnosticPtr(int64(dialDeadline.Sub(resourceOrigin)))
	writes := slow.Snapshot()
	if len(writes) != 2 || writes[0].ConnectedNS <= 0 || writes[1].ConnectedNS <= 0 {
		return fmt.Errorf("beforeWrite preparation needs both original connections")
	}
	deadline := f.inputDeadline(resourceOrigin.Add(time.Duration(min(writes[0].ConnectedNS, writes[1].ConnectedNS)) + time.Second))
	if dialDeadline.Before(deadline) {
		deadline = dialDeadline
	}
	gateCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if !time.Now().Before(deadline) || gateCtx.Err() != nil {
		return context.DeadlineExceeded
	}
	if err := f.slowInput(deadline); err != nil {
		return err
	}
	if err := f.beforeInput(gateCtx); err != nil {
		return err
	}
	if time.Now().After(deadline) || gateCtx.Err() != nil {
		return context.DeadlineExceeded
	}
	f.beforeReady = true
	return nil
}

// Unlike eventually/sample this small loop returns errors to the connected
// socket owner. It preserves the original full short circuit, Snapshot and
// young evaluation order, using the same ten-millisecond observation cadence.
func (d *g2PreparationFailure) beforeWriteSelection(ctx context.Context, g *g2Env, hc *http.Client) error {
	d.Phase = "before_write_full"
	deadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("beforeWrite selection has no original deadline")
	}
	for time.Now().Before(deadline) && ctx.Err() == nil {
		s, err := d.sampleContext(ctx, g, hc)
		if err != nil {
			return err
		}
		d.full(s)
		snapshot := g.paths["lab-a"].Snapshot()
		snapshotNS := resourceNS()
		qualified := g2CompetitionFull(s) && d.young(snapshot.Events, resourceNS())
		if d.returned(qualified, snapshot, snapshotNS) {
			return d.acceptedBeforeWrite(ctx)
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

func (d *g2PreparationFailure) acceptedBeforeWrite(ctx context.Context) error {
	w := d.lastQueryWitness
	accepted := resourceNS()
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline || ctx.Err() != nil || accepted > int64(deadline.Sub(resourceOrigin)) ||
		d.beforeWriteQueryWitness != nil || w == nil || w.Attempt == nil || w.CallbackReturnedNS == nil ||
		!g2CompetitionFull(d.lastSample) || !g2ProbeQueueMatchesQueries(d.preloadQueueWitness, w) {
		return fmt.Errorf("beforeWrite full/query observation did not qualify inside the original deadline")
	}
	// The standalone accepted witness survives history overflow and subsequent
	// final samples. None of these pointers may refer to mutable sample scratch.
	copy := *w
	copy.Queries = append([]g2PreparationQuery(nil), w.Queries...)
	copy.Attempt = g2DiagnosticPtr(*w.Attempt)
	copy.CallbackReturnedNS = g2DiagnosticPtr(*w.CallbackReturnedNS)
	copy.Sample = g2DiagnosticPtr(g2CopyCompetitionSample(d.lastSample))
	d.beforeWriteQueryWitness = &copy
	d.beforeWriteAcceptedNS = g2DiagnosticPtr(accepted)
	return nil
}
