package relay

import (
	"context"
	"time"

	"github.com/wyw-ai/loom-oi/internal/wsio"
)

// storeWriteDiagnostic describes an existing store call, not a new timeout or
// a SQL execution stage. The operation names are fixed at the six call sites.
// It is attached only to the already suppressed "store write failed" event.
type storeWriteDiagnostic struct {
	Name                        string `json:"name"`
	DurationNS                  int64  `json:"duration_ns"`
	ContextStateAtReturn        string `json:"context_state_at_return"`
	ErrorClass                  string `json:"error_class"`
	DeadlineRemainingAtCallNS   *int64 `json:"deadline_remaining_at_call_ns"`
	DeadlineRemainingAtReturnNS *int64 `json:"deadline_remaining_at_return_ns"`
	BatchElapsedAtCallNS        *int64 `json:"batch_elapsed_at_call_ns"`
	BatchElapsedAtReturnNS      *int64 `json:"batch_elapsed_at_return_ns"`
	BatchBudgetNS               *int64 `json:"batch_budget_ns"`
}

// One persist invocation owns one immutable batch. Its original context is
// shared by RaiseEpoch and every UpsertRegistry call, including late calls.
type storeWriteBatch struct {
	started time.Time
	budget  time.Duration
}

type storeWriteCall struct {
	name        string
	started     time.Time
	deadline    time.Time
	hasDeadline bool
	batch       *storeWriteBatch
}

func beginStoreWrite(ctx context.Context, name string, batch *storeWriteBatch) storeWriteCall {
	deadline, hasDeadline := ctx.Deadline()
	return storeWriteCall{name: name, started: time.Now(), deadline: deadline, hasDeadline: hasDeadline, batch: batch}
}

// returned samples before the caller's original cancellation or coordinator
// lock. Later logging must not change the observed context or elapsed times.
func (c storeWriteCall) returned(ctx context.Context, err error) *storeWriteDiagnostic {
	returned := time.Now()
	state := wsio.ContextState(ctx)
	return c.diagnosticAt(returned, state, wsio.ErrorClass(err))
}

func (c storeWriteCall) diagnosticAt(returned time.Time, state, errorClass string) *storeWriteDiagnostic {
	d := &storeWriteDiagnostic{Name: c.name, DurationNS: returned.Sub(c.started).Nanoseconds(),
		ContextStateAtReturn: state, ErrorClass: errorClass}
	if c.hasDeadline {
		atCall, atReturn := c.deadline.Sub(c.started).Nanoseconds(), c.deadline.Sub(returned).Nanoseconds()
		d.DeadlineRemainingAtCallNS, d.DeadlineRemainingAtReturnNS = &atCall, &atReturn
	}
	if c.batch != nil {
		atCall, atReturn := c.started.Sub(c.batch.started).Nanoseconds(), returned.Sub(c.batch.started).Nanoseconds()
		budget := c.batch.budget.Nanoseconds()
		d.BatchElapsedAtCallNS, d.BatchElapsedAtReturnNS, d.BatchBudgetNS = &atCall, &atReturn, &budget
	}
	return d
}
