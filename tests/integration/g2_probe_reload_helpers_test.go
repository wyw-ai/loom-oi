package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wyw-ai/loom-oi/internal/oic/config"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

type g2ReloadPrefix struct {
	EventCount  int    `json:"event_count"`
	LastEventID uint64 `json:"last_event_id"`
	ReturnedNS  int64  `json:"returned_ns"`
}

type g2ReloadProbe struct {
	ExposureID string          `json:"exposure_id"`
	Old        json.RawMessage `json:"old"`
	New        json.RawMessage `json:"new"`
	OldDigest  string          `json:"old_digest"`
	NewDigest  string          `json:"new_digest"`
}

// All nullable fields describe operations actually observed. In particular a
// panic/Goexit cannot manufacture a callback return, an Ack or an acceptance.
type g2ProbeReloadWitness struct {
	Schema              string                                 `json:"schema"`
	RequestIndex        int                                    `json:"request_index"`
	Path                string                                 `json:"path"`
	OISPID              int                                    `json:"ois_pid"`
	OICPID              int                                    `json:"oic_pid"`
	BeforePrefix        *g2ReloadPrefix                        `json:"before_prefix"`
	LastPrefix          *g2ReloadPrefix                        `json:"last_prefix"`
	AcceptedPrefix      *g2ReloadPrefix                        `json:"accepted_prefix"`
	Probes              []g2ReloadProbe                        `json:"probes"`
	ConfigBeforeSHA256  *string                                `json:"config_before_sha256"`
	ConfigAfterSHA256   *string                                `json:"config_after_sha256"`
	WriteStartedNS      *int64                                 `json:"write_started_ns"`
	WriteReturnedNS     *int64                                 `json:"write_returned_ns"`
	WriteErrorClass     *string                                `json:"write_error_class"`
	NotifyMethod        string                                 `json:"notify_method"`
	NotifyStartedNS     *int64                                 `json:"notify_started_ns"`
	NotifyReturnedNS    *int64                                 `json:"notify_returned_ns"`
	NotifyErrorClass    *string                                `json:"notify_error_class"`
	ObservationAttempts int                                    `json:"observation_attempts"`
	OldRegister         *g2RegistrationProjection              `json:"old_register"`
	OldRegisterAck      *g2RegistrationProjection              `json:"old_register_ack"`
	NewRegister         *g2RegistrationProjection              `json:"new_register"`
	NewRegisterAck      *g2RegistrationProjection              `json:"new_register_ack"`
	OldBindings         map[string]protocol.ObservationBinding `json:"old_bindings"`
	NewBindings         map[string]protocol.ObservationBinding `json:"new_bindings"`
	AcceptedNS          *int64                                 `json:"accepted_ns"`
	Result              bool                                   `json:"result"`
}

type g2ReloadConfig struct {
	Before, After []byte
	Next          map[string]any
	Probes        []g2ReloadProbe
}

func g2ReloadHash(b []byte) string {
	x := sha256.Sum256(b)
	return hex.EncodeToString(x[:])
}

// Decode two independent JSON trees. No mutation may leak into the live
// fixture configuration before the sole file write actually succeeds.
func g2ReloadConfigPlan(current map[string]any, before []byte, dir, path string, health map[string]string) (*g2ReloadConfig, error) {
	if path != "/g2-competition-reload/0" && path != "/g2-competition-reload/1" && path != "/g2-competition-reload/2" {
		return nil, fmt.Errorf("invalid reload path")
	}
	var old, next map[string]any
	if json.Unmarshal(before, &old) != nil || json.Unmarshal(before, &next) != nil || old == nil {
		return nil, fmt.Errorf("invalid original configuration")
	}
	want, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	actual, err := json.Marshal(old)
	if err != nil || !bytes.Equal(want, actual) {
		return nil, fmt.Errorf("configuration file differs from original fixture")
	}
	exps, ok := next["exposures"].([]any)
	if !ok || len(exps) != 16 || len(health) != 16 || next["access_id"] != "lab-a" {
		return nil, fmt.Errorf("reload requires exactly the original sixteen sources")
	}
	seen := map[string]bool{}
	for _, raw := range exps {
		e, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid exposure")
		}
		name, ok := e["name"].(string)
		id := "lab-a/" + name
		endpoint, exists := health[id]
		probe, probeOK := e["probe"].(map[string]any)
		if !ok || !exists || seen[id] || !probeOK || probe["url"] != endpoint {
			return nil, fmt.Errorf("reload source or original health URL differs")
		}
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" ||
			u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" || probe["tls_ca_file"] != nil {
			return nil, fmt.Errorf("reload requires original finite loopback HTTP endpoint")
		}
		seen[id] = true
		probe["url"] = endpoint + path
	}
	after, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := g2ReloadOnlyPaths(before, after, health, path); err != nil {
		return nil, err
	}
	a, err := config.Parse(before, dir)
	if err != nil {
		return nil, err
	}
	b, err := config.Parse(after, dir)
	if err != nil {
		return nil, err
	}
	oldByID, newByID := map[string]*config.Exposure{}, map[string]*config.Exposure{}
	for _, e := range a.Exposures {
		oldByID[e.ID] = e
	}
	for _, e := range b.Exposures {
		newByID[e.ID] = e
	}
	result := &g2ReloadConfig{Before: append([]byte(nil), before...), After: after, Next: next}
	for _, id := range g2CompetitionSourceIDs() {
		x, y := oldByID[id], newByID[id]
		if x == nil || y == nil || x.Probe == nil || y.Probe == nil || !x.IsEnabled() || !y.IsEnabled() || x.Revision != y.Revision ||
			x.ProbePlan().Digest() == y.ProbePlan().Digest() {
			return nil, fmt.Errorf("reload did not change exactly the independent probe plan")
		}
		result.Probes = append(result.Probes, g2ReloadProbe{id, x.ProbePlan().Canonical(), y.ProbePlan().Canonical(), x.ProbePlan().Digest(), y.ProbePlan().Digest()})
	}
	return result, nil
}

// This independent whole-object comparison also rejects accidental changes
// outside the URL loop (target, permissions, TLS, status sets or budgets).
func g2ReloadOnlyPaths(before, after []byte, health map[string]string, path string) error {
	var a, b map[string]any
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil {
		return fmt.Errorf("invalid config comparison")
	}
	xs, ok := b["exposures"].([]any)
	if !ok || len(xs) != 16 {
		return fmt.Errorf("reload exposure inventory changed")
	}
	seen := map[string]bool{}
	for _, raw := range xs {
		e, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid reload exposure")
		}
		name, ok := e["name"].(string)
		id := "lab-a/" + name
		endpoint, exists := health[id]
		p, probeOK := e["probe"].(map[string]any)
		if !ok || !exists || seen[id] || !probeOK || p["url"] != endpoint+path {
			return fmt.Errorf("reload path/source changed")
		}
		seen[id], p["url"] = true, endpoint
	}
	if !reflect.DeepEqual(a, b) {
		return fmt.Errorf("reload changed non-path configuration")
	}
	return nil
}

func g2ReloadSnapshotPrefix(s g2ProxySnapshot, returned int64) (*g2ReloadPrefix, error) {
	p := &g2ReloadPrefix{EventCount: len(s.Events), ReturnedNS: returned}
	if len(s.Events) > 0 {
		p.LastEventID = s.Events[len(s.Events)-1].ID
	}
	if s.Overflow || s.Failure != "" || returned <= 0 {
		return p, fmt.Errorf("incomplete reload Snapshot")
	}
	for i, e := range s.Events {
		if e.ID != uint64(i+1) || e.ReceivedNS <= 0 || e.ReceivedNS > returned {
			return p, fmt.Errorf("invalid reload Snapshot prefix")
		}
	}
	return p, nil
}

func g2ReloadBinding(old, next protocol.ObservationBinding, p g2ReloadProbe) bool {
	if !protocol.ValidObservationBinding(old, false) || !protocol.ValidObservationBinding(next, false) ||
		old.ExposureID != p.ExposureID || old.ContractDigest != p.OldDigest || next.ContractDigest != p.NewDigest || p.OldDigest == p.NewDigest {
		return false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(old.ObservationRevision, "ob_"), 10, 64)
	if err != nil || n == math.MaxUint64 || next.ObservationRevision != "ob_"+strconv.FormatUint(n+1, 10) {
		return false
	}
	next.ObservationRevision, next.ContractDigest = old.ObservationRevision, old.ContractDigest
	return next == old
}

// Only the actual original prefix supplies the accepted registration. Later
// wire is used solely to reject new transitions, never to repair this witness.
func (d *g2PreparationFailure) observeProbeReload(s g2ProxySnapshot, returned int64) (bool, error) {
	w := d.probeReload
	w.ObservationAttempts++
	prefix, err := g2ReloadSnapshotPrefix(s, returned)
	w.LastPrefix = prefix
	if err != nil {
		return false, err
	}
	if w.BeforePrefix == nil || prefix.LastEventID < w.BeforePrefix.LastEventID {
		return false, fmt.Errorf("reload prefix moved backwards")
	}
	var reg, ack *g2WireEvent
	for i := int(w.BeforePrefix.LastEventID); i < len(s.Events); i++ {
		e := &s.Events[i]
		if e.Role != g2RoleControl {
			continue
		}
		if e.ConnID != w.OldRegister.ConnID {
			return false, fmt.Errorf("reload changed control connection")
		}
		switch e.Type {
		case protocol.MsgRegister:
			if reg != nil {
				return false, fmt.Errorf("more than one reload Register")
			}
			reg = e
		case protocol.MsgRegisterAck:
			if reg == nil || ack != nil {
				return false, fmt.Errorf("unexpected reload RegisterAck")
			}
			ack = e
		}
	}
	if reg != nil {
		w.NewRegister = g2RegistrationProject(*reg)
	}
	if ack != nil {
		w.NewRegisterAck = g2RegistrationProject(*ack)
	}
	if reg == nil || ack == nil {
		return false, nil
	}
	// Actual publication can precede the proxy's Write return. Wait for that
	// completion in a later *real* Snapshot; never require return before reply.
	if reg.WrittenNS == 0 || ack.WrittenNS == 0 {
		if reg.WriteFailed || ack.WriteFailed || reg.Dropped || ack.Dropped || reg.Injected || ack.Injected {
			return false, fmt.Errorf("reload registration not forwarded")
		}
		return false, nil
	}
	next := &g2ProbeQueueWitness{}
	if !g2ProbeRegistration(s, returned, d.preloadQueueWitness.Sample, next) ||
		next.Register.EventID != reg.ID || next.RegisterAck.EventID != ack.ID || reg.WriteStartedNS > ack.ReceivedNS ||
		len(w.Probes) != 16 {
		return false, fmt.Errorf("invalid reload registration")
	}
	for _, p := range w.Probes {
		if !g2ReloadBinding(w.OldBindings[p.ExposureID], next.Bindings[p.ExposureID], p) {
			return false, fmt.Errorf("reload binding does not match single path change")
		}
	}
	w.NewBindings = maps.Clone(next.Bindings)
	if !d.probeDrops(s, returned, next) {
		return false, fmt.Errorf("reload has invalid old/new snapshot Drop")
	}
	return true, nil
}

type g2ReloadActions struct {
	Build        func() (*g2ReloadConfig, error)
	Snapshot     func() g2ProxySnapshot
	Write        func([]byte) error
	Notify       func() error
	NotifyMethod string
}

func (w *g2ProbeReloadWitness) accept(check func() error) error {
	prefix, accepted := *w.LastPrefix, resourceNS()
	if err := check(); err != nil {
		return err
	}
	// Publish only after the last original deadline check. A candidate captured
	// just before D is not an accepted observation if that check returns late.
	w.AcceptedPrefix, w.AcceptedNS, w.Result = &prefix, &accepted, true
	return nil
}

func (d *g2PreparationFailure) reloadProbes(ctx context.Context, actions g2ReloadActions) (err error) {
	if d.probeReload != nil || d.preloadQueueWitness == nil || !d.preloadQueueWitness.Result || d.preloadAcceptedNS == nil ||
		d.preloadQueueWitness.Register == nil || d.preloadQueueWitness.RegisterAck == nil ||
		d.finalSetStartedNS != nil || d.Round < 0 || d.Round > 2 {
		return fmt.Errorf("reload must occur once after accepted preload and before final Set")
	}
	defer func() {
		p := recover()
		if err != nil || d.probeReload == nil || !d.probeReload.Result {
			d.finish(p, true)
		}
		if p != nil {
			panic(p)
		}
	}()
	w := &g2ProbeReloadWitness{Schema: "path-only-register-transition-v1", RequestIndex: 1, Path: fmt.Sprintf("/g2-competition-reload/%d", d.Round),
		OISPID: d.preloadQueueWitness.OISPID, OICPID: d.preloadQueueWitness.OICPID, NotifyMethod: actions.NotifyMethod,
		OldRegister: g2DiagnosticPtr(*d.preloadQueueWitness.Register), OldRegisterAck: g2DiagnosticPtr(*d.preloadQueueWitness.RegisterAck), OldBindings: maps.Clone(d.preloadQueueWitness.Bindings)}
	d.probeReload, d.Phase, d.current = w, "probe_reload", nil
	check := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		deadline, ok := ctx.Deadline()
		if !ok || int64(deadline.Sub(resourceOrigin)) != d.DeadlineNS || resourceNS() >= d.DeadlineNS {
			return context.DeadlineExceeded
		}
		return nil
	}
	if actions.NotifyMethod != "sighup" && actions.NotifyMethod != "poll" {
		return fmt.Errorf("invalid reload notification")
	}
	if err = check(); err != nil {
		return err
	}
	c, err := actions.Build()
	if err != nil {
		return err
	}
	w.Probes = c.Probes
	w.ConfigBeforeSHA256 = g2DiagnosticPtr(g2ReloadHash(c.Before))
	if err = check(); err != nil {
		return err
	}
	snapshot := actions.Snapshot()
	returned := resourceNS()
	w.BeforePrefix, err = g2ReloadSnapshotPrefix(snapshot, returned)
	if err != nil {
		return err
	}
	prior := &g2ProbeQueueWitness{}
	if !g2ProbeRegistration(snapshot, returned, d.preloadQueueWitness.Sample, prior) ||
		*prior.Register != *w.OldRegister || *prior.RegisterAck != *w.OldRegisterAck || !reflect.DeepEqual(prior.Bindings, w.OldBindings) ||
		!d.probeDrops(snapshot, returned, prior) {
		return fmt.Errorf("registration changed before reload request")
	}
	for _, p := range w.Probes {
		if w.OldBindings[p.ExposureID].ContractDigest != p.OldDigest {
			return fmt.Errorf("original registered probe differs from configuration")
		}
	}
	d.historyWire(snapshot, returned)
	if err = check(); err != nil {
		return err
	}
	w.WriteStartedNS = g2DiagnosticPtr(resourceNS())
	err = actions.Write(c.After)
	w.WriteReturnedNS = g2DiagnosticPtr(resourceNS())
	w.WriteErrorClass = g2DiagnosticPtr(g2DiagnosticError(err))
	if err != nil {
		return err
	}
	w.ConfigAfterSHA256 = g2DiagnosticPtr(g2ReloadHash(c.After))
	if err = check(); err != nil {
		return err
	}
	w.NotifyStartedNS = g2DiagnosticPtr(resourceNS())
	err = actions.Notify()
	w.NotifyReturnedNS = g2DiagnosticPtr(resourceNS())
	w.NotifyErrorClass = g2DiagnosticPtr(g2DiagnosticError(err))
	if err != nil {
		return err
	}
	for {
		if err = check(); err != nil {
			return err
		}
		snapshot = actions.Snapshot()
		returned = resourceNS()
		ok, observeErr := d.observeProbeReload(snapshot, returned)
		d.historyWire(snapshot, returned)
		if observeErr != nil {
			return observeErr
		}
		if err = check(); err != nil {
			return err
		}
		if ok {
			return w.accept(check)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (d *g2PreparationFailure) reloadCompetitionProbes(g *g2Env, deadline time.Time) error {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	o := g.oics["lab-a"]
	var candidate *g2ReloadConfig
	health := map[string]string{}
	for id, h := range g.health {
		health[id] = h.URL()
	}
	method := "poll"
	if runtime.GOOS != "windows" {
		method = "sighup"
	}
	return d.reloadProbes(ctx, g2ReloadActions{
		Build: func() (*g2ReloadConfig, error) {
			before, err := os.ReadFile(o.path)
			if err != nil {
				return nil, err
			}
			candidate, err = g2ReloadConfigPlan(o.cfg, before, filepath.Dir(o.path), fmt.Sprintf("/g2-competition-reload/%d", d.Round), health)
			return candidate, err
		}, Snapshot: g.paths["lab-a"].Snapshot,
		Write: func(data []byte) error {
			if err := os.WriteFile(o.path, data, 0600); err != nil {
				return err
			}
			actual, err := os.ReadFile(o.path)
			if err != nil {
				return err
			}
			if !bytes.Equal(actual, data) {
				return fmt.Errorf("reload configuration readback differs")
			}
			o.cfg = candidate.Next
			return nil
		}, Notify: func() error {
			if method == "sighup" {
				return o.cmd.Process.Signal(syscall.SIGHUP)
			}
			return nil
		}, NotifyMethod: method,
	})
}

func (d *g2PreparationFailure) queryRegistration() *g2ProbeQueueWitness {
	w := d.probeReload
	if w == nil || !w.Result || w.AcceptedNS == nil || len(w.NewBindings) != 16 {
		return nil
	}
	return &g2ProbeQueueWitness{OISPID: w.OISPID, OICPID: w.OICPID, Result: true, ControlConnID: w.OldRegister.ConnID, Register: w.NewRegister, RegisterAck: w.NewRegisterAck, Bindings: w.NewBindings}
}

func (d *g2PreparationFailure) reloadHistory(s g2ProxySnapshot) error {
	w := d.probeReload
	if w == nil || !w.Result || w.NewRegister == nil || w.NewRegisterAck == nil {
		return fmt.Errorf("reload has no accepted inventory")
	}
	if _, err := g2ReloadSnapshotPrefix(s, resourceNS()); err != nil {
		return err
	}
	for _, e := range s.Events {
		if e.ID <= w.BeforePrefix.LastEventID || e.Role != g2RoleControl {
			continue
		}
		if e.ConnID != w.OldRegister.ConnID || e.Type == protocol.MsgRegister && e.ID != w.NewRegister.EventID ||
			e.Type == protocol.MsgRegisterAck && e.ID != w.NewRegisterAck.EventID {
			return fmt.Errorf("second registration or control generation during competition")
		}
	}
	if !d.probeDrops(s, resourceNS(), d.queryRegistration()) {
		return fmt.Errorf("invalid cumulative old/new Drop history")
	}
	queries := make(map[string]g2WireEvent)
	for _, e := range s.Events {
		if e.Type == protocol.MsgObservationQuery && e.Role == g2RoleControl && e.Direction == g2OISToOIC && !e.Injected {
			queries[e.QueryID] = e
		}
		if e.Dropped {
			q := queries[e.QueryID]
			if q.WrittenNS <= 0 || q.WrittenNS < q.WriteStartedNS {
				return fmt.Errorf("Dropped response query lacks actual final successful Write")
			}
		}
	}
	return nil
}

// Keep the original entire window and the original counts. A late old-path
// request is a failed input, never a record filtered out of the denominator.
func g2ReloadTargetWindow(h fixture.HealthSnapshot, start, end int64, path string) error {
	if path != "/g2-competition-reload/0" && path != "/g2-competition-reload/1" && path != "/g2-competition-reload/2" {
		return fmt.Errorf("missing accepted reload path")
	}
	for _, r := range h.Records {
		if r.StartedNS >= start && r.StartedNS <= end && r.RequestURI != path {
			return fmt.Errorf("old or other probe path inside original exercise window")
		}
	}
	return nil
}
