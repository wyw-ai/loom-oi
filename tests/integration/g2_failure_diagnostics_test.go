package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// These records describe failed inputs only. They never authorize a successful
// vector, add a network request, or change the context owned by the caller.
func g2DiagnosticError(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "eof"
	case errors.Is(err, net.ErrClosed):
		return "closed"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "other"
}

func g2DiagnosticContext(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "deadline"
	}
	if ctx.Err() != nil {
		return "canceled"
	}
	return "active"
}

func g2DiagnosticPtr[T any](v T) *T { return &v }

func g2DiagnosticHTTPStatus(status int) *int {
	if status == 0 {
		return nil
	}
	return g2DiagnosticPtr(status)
}

func g2DiagnosticCode(err error) *protocol.Code {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return g2DiagnosticPtr(pe.Code)
	}
	var ae *client.AbortError
	if errors.As(err, &ae) {
		return g2DiagnosticPtr(ae.Code)
	}
	return nil
}

// The outer return is real even when the HTTP helper has no response and its
// original CompletedNS is zero. Never repair the original response fields.
type g2DiagnosticCall struct {
	StartedNS    int64  `json:"call_started_ns"`
	ReturnedNS   int64  `json:"call_returned_ns"`
	ErrorClass   string `json:"error_class"`
	ContextState string `json:"context_state"`
}

func g2ObserveExposition(ctx context.Context, read func() (g2Exposition, error)) (g2Exposition, g2DiagnosticCall, error) {
	call := g2DiagnosticCall{StartedNS: resourceNS()}
	x, err := read()
	call.ReturnedNS = resourceNS()
	call.ErrorClass, call.ContextState = g2DiagnosticError(err), g2DiagnosticContext(ctx)
	return x, call, err
}

type g2FailureStamp struct {
	CapturedNS *int64  `json:"failure_captured_ns"`
	Class      *string `json:"failure_class"`
}

func (s *g2FailureStamp) freeze(class string) {
	if s.CapturedNS == nil {
		s.CapturedNS, s.Class = g2DiagnosticPtr(resourceNS()), g2DiagnosticPtr(class)
	}
}

// Callers freeze before cleanup. Even a cleanup panic cannot erase the earlier
// record; this helper neither recovers nor changes that panic.
func g2FinishDiagnostic(cleanup, emit func()) {
	defer emit()
	cleanup()
}

// Missing or non-finite diagnostic counters stay unknown. The original
// qualification still uses g2ExpositionCounter and is not changed here.
func g2DiagnosticCounter(x g2Exposition, result string) *float64 {
	value := g2ExpositionCounter(x, "oi_metrics_exports_total", map[string]string{"result": result})
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return g2DiagnosticPtr(value)
}

type g2FailurePoll struct {
	Ordinal int    `json:"ordinal"`
	Phase   string `json:"phase"`
	g2DiagnosticCall
	Status        int      `json:"status"`
	StartedNS     int64    `json:"started_ns"`
	CompletedNS   int64    `json:"completed_ns"`
	BodyBytes     int      `json:"body_bytes"`
	BodySHA256    string   `json:"body_sha256"`
	ParseComplete bool     `json:"parse_complete"`
	Timeout       *float64 `json:"timeout"`
	WriteError    *float64 `json:"write_error"`
}

type g2FailureNative struct {
	StartedNS  int64          `json:"started_ns"`
	ReturnedNS *int64         `json:"returned_ns"`
	ChannelID  *string        `json:"channel_id"`
	Code       *protocol.Code `json:"code"`
}

type g2SlowFailure struct {
	Role  string `json:"role"`
	Round int    `json:"round"`
	Stage string `json:"stage"`
	g2FailureStamp
	Before                    *g2Exposition     `json:"before"`
	BaselineCall              *g2DiagnosticCall `json:"baseline_call"`
	LastComplete200           *g2Exposition     `json:"last_complete_200"`
	BoundedFailure            *g2Exposition     `json:"bounded_failure"`
	After                     *g2Exposition     `json:"after"`
	SlowWrites                []g2SlowWrite     `json:"slow_writes"`
	StopNS                    *int64            `json:"input_stop_ns"`
	StopSetNS                 *int64            `json:"stop_set_ns"`
	Observations              []g2FailurePoll   `json:"observations"`
	Overflow                  bool              `json:"overflow"`
	FirstRejectionIndex       *int              `json:"first_rejection_index"`
	BoundedFailureIndex       *int              `json:"bounded_failure_index"`
	Native                    *g2FailureNative  `json:"native"`
	Predicate                 map[string]*bool  `json:"predicate"`
	CloseStartedNS            *int64            `json:"close_started_ns"`
	CloseCompletedNS          *int64            `json:"close_completed_ns"`
	CloseErrorClass           *string           `json:"close_error_class"`
	RecoveryDeadlineNS        *int64            `json:"recovery_deadline_ns"`
	inputCount, recoveryCount int
}

func newG2SlowFailure(role string, round int) *g2SlowFailure {
	return &g2SlowFailure{Role: role, Round: round, Stage: "baseline", Observations: make([]g2FailurePoll, 0), Predicate: map[string]*bool{"rejected": nil, "bounded_failure_200": nil, "after_delta_at_least_2": nil}}
}

func (d *g2SlowFailure) observe(phase string, x g2Exposition, err error, call g2DiagnosticCall) *int {
	parsed := err == nil && x.Status == 200 && x.Families != nil
	if parsed {
		d.LastComplete200 = g2DiagnosticPtr(x)
	}
	count, limit := &d.inputCount, 128
	if phase == "recovery" {
		count, limit = &d.recoveryCount, 256
	}
	if *count >= limit {
		d.Overflow = true
		return nil
	}
	*count = *count + 1
	hash := sha256.Sum256([]byte(x.Body))
	r := g2FailurePoll{Ordinal: len(d.Observations), Phase: phase, g2DiagnosticCall: call,
		Status: x.Status, StartedNS: x.StartedNS, CompletedNS: x.CompletedNS,
		BodyBytes: len(x.Body), BodySHA256: hex.EncodeToString(hash[:]), ParseComplete: parsed}
	if parsed {
		r.Timeout = g2DiagnosticCounter(x, "timeout")
		r.WriteError = g2DiagnosticCounter(x, "write_error")
	}
	d.Observations = append(d.Observations, r)
	return g2DiagnosticPtr(r.Ordinal)
}

func (d *g2SlowFailure) close(s *g2SlowScrapes) error {
	if s == nil {
		return nil
	}
	if d.CloseStartedNS != nil {
		return nil
	}
	if d.SlowWrites == nil {
		d.SlowWrites = s.Snapshot()
	}
	d.CloseStartedNS = g2DiagnosticPtr(resourceNS())
	err := s.Close()
	d.CloseCompletedNS = g2DiagnosticPtr(resourceNS())
	d.CloseErrorClass = g2DiagnosticPtr(g2DiagnosticError(err))
	return err
}

func (d *g2SlowFailure) fields() map[string]any {
	return map[string]any{"role": d.Role, "round": d.Round, "stage": d.Stage,
		"failure_captured_ns": d.CapturedNS, "failure_class": d.Class,
		"before": d.Before, "baseline_call": d.BaselineCall, "last_complete_200": d.LastComplete200, "bounded_failure": d.BoundedFailure,
		"after": d.After, "slow_writes": d.SlowWrites, "input_stop_ns": d.StopNS, "stop_set_ns": d.StopSetNS,
		"observations": d.Observations, "overflow": d.Overflow, "first_rejection_index": d.FirstRejectionIndex,
		"bounded_failure_index": d.BoundedFailureIndex, "native": d.Native, "predicate": d.Predicate,
		"close_started_ns": d.CloseStartedNS, "close_completed_ns": d.CloseCompletedNS,
		"close_error_class": d.CloseErrorClass, "recovery_deadline_ns": d.RecoveryDeadlineNS}
}

type g2PreparationRead struct {
	StartedNS      int64              `json:"started_ns"`
	ReturnedNS     int64              `json:"returned_ns"`
	CompletedNS    *int64             `json:"completed_ns"`
	OIS            *protocol.OISStats `json:"ois"`
	OIC            *protocol.OICStats `json:"oic"`
	CompletedRoles []string           `json:"completed_roles"`
	FailedRole     *string            `json:"failed_role"`
	ErrorClass     string             `json:"error_class"`
	ContextState   string             `json:"context_state"`
}

// Exact request order/contexts from g2CompetitionSample, with errors returned
// instead of Fatal so the last partial observation survives the original exit.
func g2TryCompetitionSample(g *g2Env, hc *http.Client) (g2CompetitionStats, g2PreparationRead, error) {
	return g2TryCompetitionSampleObserved(g, hc, nil)
}

func g2TryCompetitionSampleObserved(g *g2Env, hc *http.Client, failed func(context.Context, g2PreparationRead, error)) (x g2CompetitionStats, read g2PreparationRead, err error) {
	x.StartedNS = resourceNS()
	read.StartedNS, read.CompletedRoles = x.StartedNS, []string{}
	role := "ois"
	finish := func(ctx context.Context) {
		read.ReturnedNS = resourceNS()
		read.ErrorClass, read.ContextState = g2DiagnosticError(err), g2DiagnosticContext(ctx)
		if err != nil {
			read.FailedRole = g2DiagnosticPtr(role)
			if failed != nil {
				failed(ctx, read, err)
			}
		}
	}
	req, err := http.NewRequest("GET", "https://"+g.api+"/v1/admin/stats", nil)
	if err != nil {
		finish(context.Background())
		return x, read, err
	}
	req.Header.Set("Authorization", "Bearer "+g.adminTok)
	if err = processAlive(g.ois); err != nil {
		finish(context.Background())
		return x, read, err
	}
	if err = statsRequest(context.Background(), g.stats, req, &x.OIS); err != nil {
		finish(context.Background())
		return x, read, err
	}
	read.OIS, read.CompletedRoles = g2DiagnosticPtr(x.OIS), append(read.CompletedRoles, "ois")
	role = "oic"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// LIFO: record and freeze the actual return before canceling this context.
	defer func() { finish(ctx) }()
	req, err = http.NewRequestWithContext(ctx, "GET", "http://"+g.oics["lab-a"].diag+"/stats", nil)
	if err != nil {
		return x, read, err
	}
	res, err := hc.Do(req)
	if err != nil {
		return x, read, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(res.Body, 65537))
	_ = res.Body.Close()
	if readErr != nil {
		return x, read, readErr
	}
	if res.StatusCode != 200 || len(raw) > 65536 {
		return x, read, fmt.Errorf("competition stats %d %v", res.StatusCode, readErr)
	}
	if err = json.Unmarshal(raw, &x.OIC); err != nil {
		return x, read, err
	}
	x.CompletedNS = resourceNS()
	read.CompletedNS, read.OIC = g2DiagnosticPtr(x.CompletedNS), g2DiagnosticPtr(x.OIC)
	read.CompletedRoles = append(read.CompletedRoles, "oic")
	o := x.OIC.Observation
	if !x.OIC.Connected || !x.OIC.Registered || x.OIC.LicenseRemainingMS <= 0 || o == nil || o.ProbeJobs != 16 || o.ProbeActive > 2 || o.ProbeQueued > 4 || o.MaxProbeActive > 2 || o.MaxProbeQueued > 4 {
		return x, read, fmt.Errorf("competition process/bounded scheduler invalid %+v", x)
	}
	return x, read, nil
}

type g2PreparationQuery struct {
	EventID        uint64                      `json:"event_id"`
	QueryID        string                      `json:"query_id"`
	Binding        protocol.ObservationBinding `json:"binding"`
	ReceivedNS     int64                       `json:"received_ns"`
	WriteStartedNS int64                       `json:"write_started_ns"`
	WrittenNS      int64                       `json:"written_ns"`
	DeadlineMS     int64                       `json:"deadline_ms"`
	Injected       bool                        `json:"injected"`
	Dropped        bool                        `json:"dropped"`
	Decoded        bool                        `json:"decoded"`
}

type g2PreparationFailure struct {
	Round         int                  `json:"round"`
	Phase         string               `json:"phase"`
	Attempts      int                  `json:"attempts"`
	StartedNS     int64                `json:"prepare_started_ns"`
	DeadlineNS    int64                `json:"prepare_deadline_ns"`
	SlowAppliedNS *int64               `json:"slow_input_applied_ns"`
	Read          g2PreparationRead    `json:"last_preparation"`
	EvaluatedNS   *int64               `json:"evaluated_ns"`
	Full          map[string]bool      `json:"full"`
	Queries       []g2PreparationQuery `json:"queries"`
	Young         *bool                `json:"young"`
	g2FailureStamp
	DeadlinePassed *bool `json:"deadline_passed"`
	active         bool
}

func (d *g2PreparationFailure) sample(g *g2Env, hc *http.Client) g2CompetitionStats {
	d.Attempts++
	d.EvaluatedNS, d.Full, d.Queries, d.Young = nil, nil, nil, nil
	x, read, err := g2TryCompetitionSampleObserved(g, hc, func(_ context.Context, partial g2PreparationRead, _ error) {
		d.Read = partial
		d.freeze("stats_error")
	})
	d.Read = read
	if err != nil {
		g.t.Fatal(err)
	}
	return x
}

func (d *g2PreparationFailure) full(x g2CompetitionStats) {
	d.EvaluatedNS = g2DiagnosticPtr(resourceNS())
	o, p := x.OIS.Observation, x.OIC.Observation
	d.Full = map[string]bool{"capacity16": o != nil && o.ObservationCapacity == 16,
		"backlog16": o != nil && o.ObservationBacklog == 16, "pending8": o != nil && o.PendingQueries == 8,
		"probe_active2": p != nil && p.ProbeActive == 2, "probe_queued4": p != nil && p.ProbeQueued == 4}
}

func (d *g2PreparationFailure) young(events []g2WireEvent, now int64) bool {
	d.EvaluatedNS = g2DiagnosticPtr(now)
	d.Queries = make([]g2PreparationQuery, 0, 8)
	ok := g2CompetitionYoungQueriesObserved(events, now, 200*time.Millisecond, &d.Queries)
	d.Young = g2DiagnosticPtr(ok)
	return ok
}

func (d *g2PreparationFailure) finish(panicValue any, failed bool) {
	if !d.active || (!failed && panicValue == nil) {
		return
	}
	class := "input_not_qualified"
	if panicValue != nil {
		class = "panic"
	}
	d.freeze(class)
	d.DeadlinePassed = g2DiagnosticPtr(*d.CapturedNS > d.DeadlineNS)
}

func (d *g2PreparationFailure) fields() map[string]any {
	return map[string]any{"round": d.Round, "phase": d.Phase, "attempts": d.Attempts,
		"prepare_started_ns": d.StartedNS, "prepare_deadline_ns": d.DeadlineNS, "slow_input_applied_ns": d.SlowAppliedNS,
		"last_preparation": d.Read, "evaluated_ns": d.EvaluatedNS, "full": d.Full, "queries": d.Queries, "young": d.Young,
		"failure_captured_ns": d.CapturedNS, "failure_class": d.Class, "deadline_passed": d.DeadlinePassed}
}

type g2EntityFailure struct {
	Index         *int          `json:"index"`
	Stage         string        `json:"stage"`
	LastCompleted *int          `json:"last_completed_index"`
	Call          *g2EntityCall `json:"last_api"`
	g2FailureStamp
	active bool
}

type g2EntityCall struct {
	Operation string `json:"operation"`
	g2DiagnosticCall
	HTTPStatus *int           `json:"http_status"`
	Code       *protocol.Code `json:"code"`
}

func (d *g2EntityFailure) begin(operation string) {
	d.Call = &g2EntityCall{Operation: operation, g2DiagnosticCall: g2DiagnosticCall{StartedNS: resourceNS()}}
}
func (d *g2EntityFailure) returned(ctx context.Context, status *int, err error) {
	d.Call.ReturnedNS = resourceNS()
	d.Call.ErrorClass, d.Call.ContextState = g2DiagnosticError(err), g2DiagnosticContext(ctx)
	d.Call.HTTPStatus = status
	d.Call.Code = g2DiagnosticCode(err)
	if err != nil {
		d.freeze("api_error")
	}
}

// This is the same GET-version then single POST used by env.tryWrite. No
// response body or token is copied to the diagnostic, including on errors.
func g2EntityWrite(g *g2Env, d *g2EntityFailure, method, path string, body any) json.RawMessage {
	var cv protocol.ConfigView
	d.begin("config_version")
	status, err := g.admin.Do(context.Background(), "GET", "/v1/admin/config", "", nil, &cv)
	d.returned(context.Background(), g2DiagnosticHTTPStatus(status), err)
	if err != nil {
		g.t.Fatal(err)
	}
	var out json.RawMessage
	d.begin(d.Stage)
	status, err = g.admin.Do(context.Background(), method, path, strconv.FormatInt(cv.ConfigVersion, 10), body, &out)
	d.returned(context.Background(), g2DiagnosticHTTPStatus(status), err)
	if err != nil {
		g.t.Fatalf("%s %s: %v", method, path, err)
	}
	return out
}

func g2EntityConsumer(g *g2Env, d *g2EntityFailure, subject string) *client.Client {
	if c, ok := g.clients[subject]; ok {
		return c
	}
	token, exists := g.tokens[subject]
	if !exists {
		previous := d.Stage
		d.Stage = "token_create"
		var created protocol.TokenCreated
		if err := json.Unmarshal(g2EntityWrite(g, d, "POST", "/v1/admin/tokens", protocol.TokenCreate{Subject: subject, Scope: protocol.ScopeConsumer}), &created); err != nil {
			d.freeze("decode_error")
			g.t.Fatal(err)
		}
		token = created.Token
		g.tokens[subject] = token
		d.Stage = previous
	}
	c := must(client.New(client.Options{URL: g.consumerEndpoint(), Token: token, CAFile: g.caFile, CloseGrace: tClose}))
	g.clients[subject] = c
	return c
}

// Same one-Dial/write/CloseWrite/read/Close sequence as request. The callback
// observes only an error already returned, before the original defer closes it.
func g2FailureRequest(t *testing.T, c *client.Client, exposure, requestBody string, failed func(error, string)) (string, string) {
	t.Helper()
	s, err := c.Dial(bg, exposure)
	if err != nil {
		failed(err, "")
		t.Fatalf("dial %s: %v", exposure, err)
	}
	defer s.Close()
	if _, err := s.Write([]byte(requestBody)); err != nil {
		failed(err, s.ChannelID())
		t.Fatal(err)
	}
	if err := s.CloseWrite(); err != nil {
		failed(err, s.ChannelID())
		t.Fatal(err)
	}
	got, err := io.ReadAll(s)
	if err != nil {
		failed(err, s.ChannelID())
		t.Fatalf("read %s: %v", exposure, err)
	}
	return string(got), s.ChannelID()
}
