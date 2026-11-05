package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// These are local, bounded failure records. A call's normal return and the
// context seen before cleanup are separate facts; neither supplies a deadline.
type g2JointCall struct {
	StartedNS         int64   `json:"started_ns"`
	ReturnedNS        *int64  `json:"returned_ns"`
	ContextDeadlineNS *int64  `json:"context_deadline_ns"`
	ContextState      *string `json:"context_state"`
	ErrorClass        *string `json:"error_class"`
}

func g2JointDeadline(ctx context.Context) *int64 {
	if deadline, ok := ctx.Deadline(); ok {
		return g2DiagnosticPtr(int64(deadline.Sub(resourceOrigin)))
	}
	return nil
}

func g2BeginJointCall(ctx context.Context) *g2JointCall {
	return &g2JointCall{StartedNS: resourceNS(), ContextDeadlineNS: g2JointDeadline(ctx)}
}

func (c *g2JointCall) returned(ctx context.Context, err error) {
	c.ReturnedNS = g2DiagnosticPtr(resourceNS())
	c.ContextState, c.ErrorClass = g2DiagnosticPtr(g2DiagnosticContext(ctx)), g2DiagnosticPtr(g2DiagnosticError(err))
}

type g2JointNative struct {
	g2JointCall
	ChannelID       string         `json:"channel_id"`
	ErrorObservedNS *int64         `json:"error_observed_ns"`
	Phase           *string        `json:"phase"`
	Code            *protocol.Code `json:"code"`
}

type g2JointAdminCall struct {
	g2JointCall
	HTTPStatus *int           `json:"http_status"`
	Code       *protocol.Code `json:"code"`
}

type g2JointRevoke struct {
	StartedNS   int64             `json:"started_ns"`
	ReturnedNS  *int64            `json:"returned_ns"`
	ConfigGet   *g2JointAdminCall `json:"config_get"`
	TokenDelete *g2JointAdminCall `json:"token_delete"`
}

type g2JointCritical struct {
	CycleDeadlineNS       int64              `json:"cycle_deadline_ns"`
	InputDeadlineNS       int64              `json:"input_deadline_ns"`
	OISPID                int                `json:"ois_pid"`
	OICPID                int                `json:"oic_pid"`
	BeforeStats           *g2PreparationRead `json:"before_stats"`
	AfterStats            *g2PreparationRead `json:"after_stats"`
	AfterStatsAttempts    int                `json:"after_stats_attempts"`
	Native                *g2JointNative     `json:"native"`
	Revoke                *g2JointRevoke     `json:"revoke"`
	ProviderWait          *g2JointCall       `json:"provider_wait"`
	ControlWait           *g2JointCall       `json:"control_wait"`
	ContextDeadlineNS     *int64             `json:"context_deadline_ns"`
	ContextStateAtFailure *string            `json:"context_state_at_failure"`
	capturedNS            *int64
	inputCtx              context.Context
}

func (d *g2JointCritical) freeze() {
	if d == nil || d.capturedNS != nil {
		return
	}
	d.capturedNS = g2DiagnosticPtr(resourceNS())
	if d.inputCtx != nil {
		d.ContextDeadlineNS = g2JointDeadline(d.inputCtx)
		d.ContextStateAtFailure = g2DiagnosticPtr(g2DiagnosticContext(d.inputCtx))
	}
}

// Keep this wrapper at the original defer registration point. Incomplete
// execution includes panic and Goexit; it does not require recovering either.
func g2JointCleanup(d **g2JointCritical, completed *bool, failed func() bool, cleanup func()) {
	if !*completed || failed() {
		(*d).freeze()
	}
	cleanup()
}

func (d *g2JointCritical) sample(g *g2Env, hc *http.Client, after bool) g2CompetitionStats {
	g.t.Helper()
	x, err := d.readStats(g, hc, after)
	if err != nil {
		g.t.Fatal(err)
	}
	return x
}

func (d *g2JointCritical) readStats(g *g2Env, hc *http.Client, after bool) (g2CompetitionStats, error) {
	target := &d.BeforeStats
	if after {
		d.AfterStatsAttempts++
		target = &d.AfterStats
	}
	x, read, err := g2TryCompetitionSampleObserved(g, hc, func(_ context.Context, partial g2PreparationRead, _ error) {
		*target = &partial
		d.freeze()
	})
	if d.capturedNS == nil {
		*target = &read
	}
	return x, err
}

type g2JointTesting interface {
	Helper()
	Fatal(...any)
	Fatalf(string, ...any)
}

type g2JointStream interface {
	io.Reader
	io.Writer
	CloseWrite() error
	Close() error
	ChannelID() string
}

func g2JointRequest(t g2JointTesting, d *g2JointCritical, dial func() (g2JointStream, error), exposure, requestBody string) (string, string) {
	t.Helper()
	completed := false
	defer func() {
		if !completed {
			d.freeze()
		}
	}()
	fail := func(phase string, err error, channel string) {
		d.Native.ChannelID = channel
		d.Native.ErrorObservedNS = g2DiagnosticPtr(resourceNS())
		d.Native.Phase, d.Native.Code = g2DiagnosticPtr(phase), g2DiagnosticCode(err)
		d.Native.ErrorClass, d.Native.ContextState = g2DiagnosticPtr(g2DiagnosticError(err)), g2DiagnosticPtr(g2DiagnosticContext(bg))
		d.freeze()
	}
	s, err := dial()
	if err != nil {
		fail("dial", err, "")
		t.Fatalf("dial %s: %v", exposure, err)
	}
	defer func() {
		if !completed {
			d.freeze()
		}
		_ = s.Close()
	}()
	d.Native.ChannelID = s.ChannelID()
	if _, err := s.Write([]byte(requestBody)); err != nil {
		fail("write", err, s.ChannelID())
		t.Fatal(err)
	}
	if err := s.CloseWrite(); err != nil {
		fail("close_write", err, s.ChannelID())
		t.Fatal(err)
	}
	got, err := io.ReadAll(s)
	if err != nil {
		fail("read", err, s.ChannelID())
		t.Fatalf("read %s: %v", exposure, err)
	}
	completed = true
	return string(got), s.ChannelID()
}

func g2JointNativeRequest(t *testing.T, c *client.Client, d *g2JointCritical, exposure, requestBody string) (string, string) {
	return g2JointRequest(t, d, func() (g2JointStream, error) { return c.Dial(bg, exposure) }, exposure, requestBody)
}

type g2JointAdminDo func(context.Context, string, string, string, any, any) (int, error)

// This is exactly env.write's GET-version followed by its one DELETE. The
// token creation version is deliberately not substituted in this diagnostic.
func g2JointRevokeToken(t g2JointTesting, d *g2JointCritical, do g2JointAdminDo, tokenID string) json.RawMessage {
	t.Helper()
	completed := false
	defer func() {
		if !completed {
			d.freeze()
		}
	}()
	ctx := context.Background()
	var cv protocol.ConfigView
	d.Revoke.ConfigGet = &g2JointAdminCall{g2JointCall: *g2BeginJointCall(ctx)}
	status, err := do(ctx, "GET", "/v1/admin/config", "", nil, &cv)
	d.Revoke.ConfigGet.returned(ctx, err)
	d.Revoke.ConfigGet.HTTPStatus, d.Revoke.ConfigGet.Code = g2DiagnosticHTTPStatus(status), g2DiagnosticCode(err)
	if err != nil {
		d.freeze()
		t.Fatal(err)
	}
	var out json.RawMessage
	d.Revoke.TokenDelete = &g2JointAdminCall{g2JointCall: *g2BeginJointCall(ctx)}
	path := "/v1/admin/tokens/" + tokenID
	status, err = do(ctx, "DELETE", path, strconv.FormatInt(cv.ConfigVersion, 10), nil, &out)
	d.Revoke.TokenDelete.returned(ctx, err)
	d.Revoke.TokenDelete.HTTPStatus, d.Revoke.TokenDelete.Code = g2DiagnosticHTTPStatus(status), g2DiagnosticCode(err)
	if err != nil {
		d.freeze()
		t.Fatalf("%s %s: %v", "DELETE", path, err)
	}
	completed = true
	return out
}

func g2JointWait(d *g2JointCritical, call **g2JointCall, ctx context.Context, wait func(context.Context) (g2WireEvent, error)) (g2WireEvent, error) {
	*call = g2BeginJointCall(ctx)
	e, err := wait(ctx)
	(*call).returned(ctx, err)
	if err != nil {
		d.freeze()
	}
	return e, err
}
