package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Only cycle zero supplies this selection. The cycle remains the sole owner
// of its original slow connections, input baseline and cleanup.
type g2CompetitionFinalSelection struct {
	preparation *g2PreparationFailure
	deadline    time.Time
	selectInput func(time.Time)
	called      bool
}

func (f *g2CompetitionFinalSelection) inputDeadline(original time.Time) time.Time {
	if f != nil && f.deadline.Before(original) {
		return f.deadline
	}
	return original
}

func (f *g2CompetitionFinalSelection) inputContext(parent context.Context) (context.Context, context.CancelFunc) {
	if f == nil {
		return parent, func() {}
	}
	return context.WithDeadline(parent, f.deadline)
}

func (f *g2CompetitionFinalSelection) selectFinal(cycleDeadline time.Time) error {
	if f == nil {
		return nil
	}
	if f.called {
		return fmt.Errorf("cycle-zero final selection called twice")
	}
	f.called = true
	deadline := f.inputDeadline(cycleDeadline)
	if !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	f.selectInput(deadline)
	if time.Now().After(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func (f *g2CompetitionFinalSelection) freezeBeforeCleanup(panicValue any, failed bool) {
	if f != nil {
		f.preparation.finish(panicValue, failed)
	}
}

func g2CompetitionBeforeExport(t *testing.T, hc *http.Client, addr string, f *g2CompetitionFinalSelection) g2Exposition {
	t.Helper()
	if f == nil {
		return g2ExporterRead(t, hc, addr)
	}
	deadline := time.Now().Add(time.Second)
	deadline = f.inputDeadline(deadline)
	if !time.Now().Before(deadline) {
		t.Fatal("export baseline has no original preparation budget")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	x, err := g2ReadExposition(ctx, hc, addr)
	if err != nil || x.Status != 200 || time.Now().After(deadline) {
		t.Fatalf("real exporter baseline status=%d, error=%v, original deadline exceeded=%t", x.Status, err, time.Now().After(deadline))
	}
	return x
}
