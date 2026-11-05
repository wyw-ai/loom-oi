package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type diagnosticStore struct {
	*fakeStore
	call func(string, context.Context) error
}

func (s *diagnosticStore) ApplyConfig(ctx context.Context, version int64, muts []store.Mutation, audit []store.AuditEntry) (int64, error) {
	if err := s.call("apply_config", ctx); err != nil {
		return 0, err
	}
	return s.fakeStore.ApplyConfig(ctx, version, muts, audit)
}

func (s *diagnosticStore) RaiseEpoch(ctx context.Context, epoch int64, audit store.AuditEntry) (bool, error) {
	if err := s.call("raise_epoch", ctx); err != nil {
		return false, err
	}
	return s.fakeStore.RaiseEpoch(ctx, epoch, audit)
}

func (s *diagnosticStore) UpsertRegistry(ctx context.Context, id string, entries []store.Exposure, audit []store.AuditEntry) error {
	if err := s.call("upsert_registry", ctx); err != nil {
		return err
	}
	return s.fakeStore.UpsertRegistry(ctx, id, entries, audit)
}

func (s *diagnosticStore) Maintain(ctx context.Context, req protocol.StoreMaintain) (protocol.StoreMaintained, error) {
	if err := s.call("maintain", ctx); err != nil {
		return protocol.StoreMaintained{}, err
	}
	return s.fakeStore.Maintain(ctx, req)
}

func (s *diagnosticStore) AppendAudit(ctx context.Context, audit []store.AuditEntry) error {
	if err := s.call("append_audit", ctx); err != nil {
		return err
	}
	return s.fakeStore.AppendAudit(ctx, audit)
}

func (s *diagnosticStore) PruneAudit(ctx context.Context, through int64, digest string, audit store.AuditEntry) (int64, error) {
	if err := s.call("prune_audit", ctx); err != nil {
		return 0, err
	}
	return s.fakeStore.PruneAudit(ctx, through, digest, audit)
}

type diagnosticWriteEvent struct {
	Msg       string                `json:"msg"`
	Failure   string                `json:"failure"`
	Error     string                `json:"error"`
	Operation *storeWriteDiagnostic `json:"operation"`
}

func diagnosticWriteEvents(t *testing.T, logs *bytes.Buffer) []diagnosticWriteEvent {
	t.Helper()
	var out []diagnosticWriteEvent
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event diagnosticWriteEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("invalid log JSON: %v", err)
		}
		if event.Msg == "store write failed" {
			out = append(out, event)
		}
	}
	return out
}

func TestStoreWriteDiagnosticBoundaries(t *testing.T) {
	start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		atCall        time.Duration
		atReturn      time.Duration
		wantRemaining int64
	}{
		{"deadline_before_call", 600 * time.Millisecond, 700 * time.Millisecond, -200_000_000},
		{"deadline_during_call", 450 * time.Millisecond, 510 * time.Millisecond, -10_000_000},
		{"exact_deadline", 400 * time.Millisecond, 500 * time.Millisecond, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := storeWriteCall{name: "upsert_registry", started: start.Add(tc.atCall),
				deadline: start.Add(500 * time.Millisecond), hasDeadline: true,
				batch: &storeWriteBatch{started: start, budget: 500 * time.Millisecond}}
			d := call.diagnosticAt(start.Add(tc.atReturn), "deadline", "deadline")
			if d.DurationNS != (tc.atReturn-tc.atCall).Nanoseconds() || d.ContextStateAtReturn != "deadline" || d.ErrorClass != "deadline" ||
				d.DeadlineRemainingAtCallNS == nil || *d.DeadlineRemainingAtCallNS != (500*time.Millisecond-tc.atCall).Nanoseconds() ||
				d.DeadlineRemainingAtReturnNS == nil || *d.DeadlineRemainingAtReturnNS != tc.wantRemaining ||
				d.BatchElapsedAtCallNS == nil || *d.BatchElapsedAtCallNS != tc.atCall.Nanoseconds() ||
				d.BatchElapsedAtReturnNS == nil || *d.BatchElapsedAtReturnNS != tc.atReturn.Nanoseconds() ||
				d.BatchBudgetNS == nil || *d.BatchBudgetNS != 500_000_000 {
				t.Fatalf("deadline/batch was clamped or restarted: %+v", d)
			}
		})
	}
	t.Run("no_deadline", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		call := beginStoreWrite(ctx, "maintain", nil)
		d := call.returned(ctx, errors.New("token secret DSN SELECT deadline exceeded"))
		if d.ContextStateAtReturn != "active" || d.ErrorClass != "other" || d.DurationNS < 0 ||
			d.DeadlineRemainingAtCallNS != nil || d.DeadlineRemainingAtReturnNS != nil ||
			d.BatchElapsedAtCallNS != nil || d.BatchElapsedAtReturnNS != nil || d.BatchBudgetNS != nil {
			t.Fatalf("unobserved fields invented: %+v", d)
		}
		encoded, err := json.Marshal(d)
		if err != nil || strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "SELECT") ||
			!strings.Contains(string(encoded), `"deadline_remaining_at_call_ns":null`) {
			t.Fatalf("operation copied error text or omitted null: %s / %v", encoded, err)
		}
		cancel()
		if d.ContextStateAtReturn != "active" {
			t.Fatal("later cleanup altered the return observation")
		}
	})
	t.Run("canceled_return", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		call := beginStoreWrite(ctx, "apply_config", nil)
		cancel()
		d := call.returned(ctx, errors.Join(store.ErrUnavailable, context.Canceled))
		if d.ContextStateAtReturn != "canceled" || d.ErrorClass != "canceled" || d.DurationNS < 0 {
			t.Fatalf("wrapped cancellation lost: %+v", d)
		}
	})
	t.Run("successful_return_before_cleanup", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		call := beginStoreWrite(ctx, "append_audit", nil)
		d := call.returned(ctx, nil)
		cancel()
		if d.ContextStateAtReturn != "active" || d.ErrorClass != "none" || d.DeadlineRemainingAtReturnNS == nil ||
			*d.DeadlineRemainingAtReturnNS <= 0 || *d.DeadlineRemainingAtReturnNS > *d.DeadlineRemainingAtCallNS {
			t.Fatalf("cleanup was sampled instead of return: %+v", d)
		}
	})
}

func TestStoreWriteDiagnosticEntryPoints(t *testing.T) {
	for _, name := range []string{"apply_config", "raise_epoch", "upsert_registry", "maintain", "append_audit", "prune_audit"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			var logs bytes.Buffer
			e.h.log = slog.New(slog.NewJSONHandler(&logs, nil))
			ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			failure := store.NewUnavailable(store.FailureIO, "unchanged original error text")
			var original context.Context
			calls := 0
			e.h.st = &diagnosticStore{fakeStore: e.st, call: func(operation string, observed context.Context) error {
				if operation != name {
					return nil
				}
				calls++
				original = observed
				return failure
			}}
			var err error
			switch name {
			case "apply_config":
				_, err = e.h.CreateAccess(ctx, e.admin, e.v(), protocol.AccessSpec{AccessID: "diagnostic"})
			case "raise_epoch", "upsert_registry":
				e.h.mu.Lock()
				e.h.epochDirty = name == "raise_epoch"
				if name == "upsert_registry" {
					e.h.regDirty["diagnostic"] = &regPending{seq: 1}
				}
				e.h.mu.Unlock()
				if e.h.persist() {
					t.Fatal("persistence failure became success")
				}
			case "maintain", "append_audit":
				_, err = e.h.MaintainStore(ctx, e.admin, protocol.StoreMaintain{})
			case "prune_audit":
				_, err = e.h.PruneAudit(ctx, e.admin, protocol.AuditPrune{ThroughSeq: 1, SHA256: strings.Repeat("a", 64)})
			}
			wantCode := protocol.CodeStoreUnavailable
			if name == "raise_epoch" || name == "upsert_registry" || name == "append_audit" {
				wantCode = ""
			}
			if codeOf(err) != wantCode || calls != 1 || original == nil {
				t.Fatalf("original outcome/call changed: code=%s err=%v calls=%d", codeOf(err), err, calls)
			}
			batch := name == "raise_epoch" || name == "upsert_registry"
			if !batch && original != ctx {
				t.Fatal("store call received a replacement context")
			}
			e.h.mu.Lock()
			healthy, kind, failures := e.h.storeOK, e.h.writeFailure, e.h.storeFailures
			e.h.mu.Unlock()
			if healthy || kind != store.FailureIO || failures != 1 {
				t.Fatalf("latch changed: healthy=%v kind=%s failures=%d", healthy, kind, failures)
			}
			events := diagnosticWriteEvents(t, &logs)
			if len(events) != 1 || events[0].Operation == nil || events[0].Error != failure.Error() || events[0].Failure != store.FailureIO {
				t.Fatalf("unexpected existing error event: %+v", events)
			}
			d := events[0].Operation
			if d.Name != name || d.ContextStateAtReturn != "active" || d.ErrorClass != "other" || d.DurationNS < 0 ||
				d.DeadlineRemainingAtCallNS == nil || d.DeadlineRemainingAtReturnNS == nil ||
				*d.DeadlineRemainingAtReturnNS > *d.DeadlineRemainingAtCallNS ||
				*d.DeadlineRemainingAtCallNS-*d.DeadlineRemainingAtReturnNS != d.DurationNS {
				t.Fatalf("call/return not paired: %+v", d)
			}
			if batch {
				if d.BatchBudgetNS == nil || *d.BatchBudgetNS != 500_000_000 || d.BatchElapsedAtCallNS == nil ||
					d.BatchElapsedAtReturnNS == nil || *d.BatchElapsedAtReturnNS-*d.BatchElapsedAtCallNS != d.DurationNS {
					t.Fatalf("batch not paired: %+v", d)
				}
			} else if d.BatchBudgetNS != nil || d.BatchElapsedAtCallNS != nil || d.BatchElapsedAtReturnNS != nil {
				t.Fatalf("batch invented outside persist: %+v", d)
			}
		})
	}
}

func TestStoreWriteDiagnosticPreservesLatchAndSuppression(t *testing.T) {
	e := newEnv(t)
	var logs bytes.Buffer
	e.h.log = slog.New(slog.NewJSONHandler(&logs, nil))
	full := store.NewUnavailable(store.FailureDiskFull, "full")
	readOnly := store.NewUnavailable(store.FailureReadOnly, "read only")
	d := beginStoreWrite(context.Background(), "maintain", nil).returned(context.Background(), full)
	e.h.writeFailedObserved(full, d)
	e.h.writeFailedObserved(full, d)
	e.h.writeFailedObserved(readOnly, d)
	e.h.writeFailed(store.ErrUnavailable) // Existing artificial callers have no observation.
	e.h.writeFailed(store.ErrBusy)
	if events := diagnosticWriteEvents(t, &logs); len(events) != 3 || events[2].Operation != nil {
		t.Fatalf("suppression/null source changed: %+v", events)
	}
	e.h.mu.Lock()
	count := e.h.storeFailures
	e.h.mu.Unlock()
	if count != 4 || e.h.ConfigView().StoreHealthy {
		t.Fatalf("failure serial was suppressed with logs: %d", count)
	}
	e.h.probe()
	if !e.h.ConfigView().StoreHealthy || e.st.probes != 1 {
		t.Fatal("diagnostics changed real write-probe recovery")
	}
	e.h.writeFailed(store.ErrUnavailable)
	if events := diagnosticWriteEvents(t, &logs); len(events) != 4 || events[3].Operation != nil {
		t.Fatalf("recovered failure did not log once: %+v", events)
	}
}

func TestStoreWriteDiagnosticSharedPersistBudget(t *testing.T) {
	e := newEnv(t)
	var logs bytes.Buffer
	e.h.log = slog.New(slog.NewJSONHandler(&logs, nil))
	e.h.mu.Lock()
	e.h.epochDirty = true
	e.h.regDirty["diagnostic"] = &regPending{seq: 1}
	e.h.mu.Unlock()
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var first, next context.Context
	var firstDeadline, nextDeadline time.Time
	e.h.st = &diagnosticStore{fakeStore: e.st, call: func(name string, ctx context.Context) error {
		if name == "raise_epoch" {
			first = ctx
			firstDeadline, _ = ctx.Deadline()
			entered <- ctx
			<-release
			return errors.Join(store.NewUnavailable(store.FailureDiskFull, "first"), ctx.Err())
		}
		if name == "upsert_registry" {
			next = ctx
			nextDeadline, _ = ctx.Deadline()
			return errors.Join(store.NewUnavailable(store.FailureReadOnly, "next"), ctx.Err())
		}
		return nil
	}}
	done := make(chan bool, 1)
	go func() { done <- e.h.persist() }()
	joined := false
	defer func() {
		unblock()
		if !joined {
			select {
			case <-done:
			case <-time.After(waitTimeout):
				t.Error("persistence worker did not join")
			}
		}
	}()
	var original context.Context
	select {
	case original = <-entered:
	case <-time.After(waitTimeout):
		t.Fatal("raise epoch did not reach the call gate")
	}
	// Wait for the actual unchanged 500ms context, not an invented diagnostic
	// deadline or a fixed sleep. The first call spans expiry; the next starts late.
	select {
	case <-original.Done():
	case <-time.After(waitTimeout):
		t.Fatal("original persist deadline did not expire")
	}
	unblock()
	select {
	case ok := <-done:
		joined = true
		if ok {
			t.Fatal("failed writes became success")
		}
	case <-time.After(waitTimeout):
		t.Fatal("persistence did not finish")
	}
	if first == nil || first != next || firstDeadline != nextDeadline || first.Err() != context.DeadlineExceeded {
		t.Fatal("each write received a new budget or cancellation was lost")
	}
	events := diagnosticWriteEvents(t, &logs)
	if len(events) != 2 || events[0].Operation == nil || events[1].Operation == nil {
		t.Fatalf("expected both distinct failure categories: %+v", events)
	}
	a, b := events[0].Operation, events[1].Operation
	for _, d := range []*storeWriteDiagnostic{a, b} {
		if d.DeadlineRemainingAtCallNS == nil || d.DeadlineRemainingAtReturnNS == nil ||
			d.BatchElapsedAtCallNS == nil || d.BatchElapsedAtReturnNS == nil || d.BatchBudgetNS == nil {
			t.Fatalf("missing actual batch/deadline: %+v", d)
		}
	}
	if a.Name != "raise_epoch" || b.Name != "upsert_registry" || a.ContextStateAtReturn != "deadline" || b.ContextStateAtReturn != "deadline" ||
		a.ErrorClass != "deadline" || b.ErrorClass != "deadline" || *a.DeadlineRemainingAtCallNS <= 0 ||
		*a.DeadlineRemainingAtReturnNS > 0 || *b.DeadlineRemainingAtCallNS > 0 || *b.DeadlineRemainingAtReturnNS > 0 ||
		*a.BatchBudgetNS != 500_000_000 || *b.BatchBudgetNS != *a.BatchBudgetNS ||
		*b.BatchElapsedAtCallNS < *a.BatchElapsedAtReturnNS || *b.BatchElapsedAtReturnNS < *b.BatchElapsedAtCallNS {
		t.Fatalf("shared original budget misreported: first=%+v next=%+v", a, b)
	}
	e.h.mu.Lock()
	healthy, kind, failures := e.h.storeOK, e.h.writeFailure, e.h.storeFailures
	e.h.mu.Unlock()
	if healthy || kind != store.FailureReadOnly || failures != 2 {
		t.Fatalf("state changed by recording: %v/%s/%d", healthy, kind, failures)
	}
}
