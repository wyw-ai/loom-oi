package observation

import (
	"errors"
	"math"
	"strconv"
	"time"
)

type entry struct {
	binding                           Binding
	plan                              *Plan
	present, valid                    bool
	observation, sampleSeq, updateSeq uint64
	attempt                           Attempt
	last                              *Sample
	unknown                           Reason
	work                              WorkToken
	started                           time.Time
}

// Table retains bounded tombstones for the whole control generation. Its owner
// holds the same lock for this table, reload and the final READY gate.
type Table struct {
	max                  int
	session              Session
	incarnation, workSeq uint64
	active               bool
	entries              map[string]entry
}

func NewTable(maxEntries int) (*Table, error) {
	if maxEntries <= 0 {
		return nil, errors.New("observation identity limit must be positive")
	}
	return &Table{max: maxEntries, entries: make(map[string]entry)}, nil
}

// Reset is only for a new wire session identity, never a same-generation
// shortcut for reclaiming tombstones or resetting exhausted sequence numbers.
func (t *Table) Reset(s Session) ([]Effect, error) {
	if s.AccessID == "" || s.Epoch <= 0 || s.Generation <= 0 || (t.incarnation != 0 && s == t.session) {
		return nil, ErrBinding
	}
	if t.incarnation == math.MaxUint64 {
		return []Effect{{EndSession: true}}, ErrOverflow
	}
	effects := t.Disconnect()
	t.session, t.active = s, true
	t.incarnation++
	t.workSeq = 0
	t.entries = make(map[string]entry)
	return effects, nil
}

func (t *Table) Disconnect() []Effect {
	effects := make([]Effect, 0, len(t.entries))
	if !t.active {
		return effects
	}
	t.active = false
	for id, e := range t.entries {
		if e.present {
			effects = append(effects, invalidate(&e))
			t.entries[id] = e
		}
	}
	return effects
}

// Set changes only the independent observation revision for probe contract
// changes (or reinstatement). A business-target-only revision change retains
// ob_N while invalidating the complete observation binding and pending work.
func (t *Table) Set(exposureID, revision string, plan *Plan, now time.Time) (Binding, Effect, error) {
	if !t.active || exposureID == "" || revision == "" {
		return Binding{}, Effect{}, ErrBinding
	}
	e, exists := t.entries[exposureID]
	if !exists && len(t.entries) >= t.max {
		return Binding{}, Effect{EndSession: true}, ErrCapacity
	}
	changedProbe := !exists || !e.present || e.plan.Digest() != plan.Digest()
	changed := changedProbe || e.binding.Revision != revision
	if !changed {
		return e.binding, Effect{}, nil
	}
	if e.updateSeq == math.MaxUint64 || (changedProbe && e.observation == math.MaxUint64) {
		return Binding{}, Effect{EndSession: true}, ErrOverflow
	}
	effect := invalidate(&e)
	effect.Dirty = true
	e.updateSeq++
	if changedProbe {
		e.observation++
	}
	e.binding = Binding{AccessID: t.session.AccessID, ExposureID: exposureID, Revision: revision,
		ObservationRevision: "ob_" + strconv.FormatUint(e.observation, 10), ContractDigest: plan.Digest(),
		Epoch: t.session.Epoch, Generation: t.session.Generation}
	if plan != nil {
		e.binding.SourceID, e.binding.ContractVersion = "local", 1
	}
	e.plan, e.present = plan, true
	if plan == nil {
		e.unknown = Unsupported
	} else if !exists {
		e.unknown = NoSample
	}
	t.entries[exposureID] = e
	return e.binding, effect, nil
}

func (t *Table) Remove(exposureID string) (Effect, error) {
	e, exists := t.entries[exposureID]
	if !t.active || !exists || !e.present {
		return Effect{}, nil
	}
	if e.updateSeq == math.MaxUint64 {
		return Effect{EndSession: true}, ErrOverflow
	}
	effect := invalidate(&e)
	effect.Dirty = true
	e.updateSeq++
	e.present = false
	t.entries[exposureID] = e
	return effect, nil
}

func invalidate(e *entry) Effect {
	effect := Effect{InvalidatePreparing: e.present}
	if e.attempt.Running {
		token := e.work
		effect.CancelWork = &token
		e.attempt.Running, e.attempt.Outcome = false, SourceInvalid
	}
	e.work = WorkToken{}
	e.valid, e.unknown = false, SourceInvalid
	return effect
}

func (t *Table) current(b Binding) (entry, error) {
	e, exists := t.entries[b.ExposureID]
	if !t.active || !exists || !e.present || e.binding != b {
		return entry{}, ErrBinding
	}
	return e, nil
}

func expire(e *entry, now time.Time) bool {
	if e.valid && RemainingMS(e.last.ExpiresAt.Sub(now)) == 0 {
		e.valid, e.unknown = false, Expired
		return true
	}
	return false
}

func (t *Table) Begin(b Binding, now time.Time) (WorkToken, Effect, error) {
	e, err := t.current(b)
	if err != nil {
		return WorkToken{}, Effect{}, err
	}
	if e.plan == nil {
		return WorkToken{}, Effect{}, ErrNoProbe
	}
	if e.attempt.Running {
		return WorkToken{}, Effect{}, ErrBusy
	}
	if e.attempt.Seq == math.MaxUint64 || e.updateSeq == math.MaxUint64 || t.workSeq == math.MaxUint64 {
		return WorkToken{}, Effect{EndSession: true}, ErrOverflow
	}
	expired := expire(&e, now)
	t.workSeq++
	e.work = WorkToken{b, t.incarnation, t.workSeq}
	e.started, e.attempt.Running = now, true
	e.attempt.Seq++
	e.attempt.Outcome, e.attempt.Duration = "", 0
	e.updateSeq++
	t.entries[b.ExposureID] = e
	return e.work, Effect{Dirty: true, SampleExpired: expired}, nil
}

func (t *Table) Finish(token WorkToken, outcome Outcome, now time.Time) (Effect, error) {
	e, err := t.current(token.Binding)
	if err != nil || token.SessionID != t.incarnation || !e.attempt.Running || e.work != token {
		return Effect{}, nil // stale workers never mutate newer state
	}
	matched := (outcome.Readiness == Ready && outcome.Reason == MatchedReady) ||
		(outcome.Readiness == NotReady && outcome.Reason == MatchedNotReady)
	if !matched && !(outcome.Readiness == Unknown && outcome.Reason.failure()) {
		return Effect{}, errors.New("invalid probe outcome")
	}
	if now.Before(e.started) {
		return Effect{}, errors.New("probe completion predates its start")
	}
	deadline := e.started.Add(e.plan.validity)
	newSample := matched && now.Before(deadline)
	if e.updateSeq == math.MaxUint64 || (newSample && e.sampleSeq == math.MaxUint64) {
		return Effect{EndSession: true}, ErrOverflow
	}
	expired := expire(&e, now)
	effect := Effect{Dirty: true, SampleExpired: expired}
	e.attempt.Running, e.attempt.Outcome = false, outcome.Reason
	e.attempt.Duration = now.Sub(e.started)
	e.work = WorkToken{}
	e.updateSeq++
	if newSample {
		effect.SampleCreated = outcome.Readiness
		e.sampleSeq++
		e.last = &Sample{Seq: e.sampleSeq, Readiness: outcome.Readiness, Reason: outcome.Reason,
			ObservedAt: now.Round(0), StartedAt: e.started, ExpiresAt: deadline}
		e.valid = true
	} else if matched && !e.valid {
		e.unknown = Expired
	}
	t.entries[token.Binding.ExposureID] = e
	return effect, nil
}

func (t *Table) Snapshot(b Binding, now time.Time) (Snapshot, Effect, error) {
	e, err := t.current(b)
	if err != nil {
		return Snapshot{}, Effect{}, err
	}
	effect := Effect{}
	if expire(&e, now) {
		if e.updateSeq == math.MaxUint64 {
			return Snapshot{}, Effect{EndSession: true}, ErrOverflow
		}
		e.updateSeq++
		effect.Dirty = true
		effect.SampleExpired = true
		t.entries[b.ExposureID] = e
	}
	s := Snapshot{Binding: b, UpdateSeq: e.updateSeq, Attempt: e.attempt, Readiness: Unknown, Reason: e.unknown}
	if e.last != nil {
		sample := *e.last
		s.LastSample = &sample
	}
	if e.valid {
		s.Readiness, s.Reason, s.Remaining = e.last.Readiness, e.last.Reason, e.last.ExpiresAt.Sub(now)
	}
	return s, effect, nil
}

func (t *Table) Evaluate(b Binding, mode Mode, now time.Time) (Decision, Effect, error) {
	if mode != BestEffort && mode != RequireReady {
		return Decision{}, Effect{}, errors.New("invalid admission mode")
	}
	s, effect, err := t.Snapshot(b, now)
	if err != nil {
		return Decision{}, effect, err
	}
	return Decision{Allowed: s.Readiness == Ready || (s.Readiness == Unknown && mode == BestEffort), Mode: mode, Snapshot: s}, effect, nil
}

func (t *Table) Identities() int { return len(t.entries) }
