package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const waitTimeout = 3 * time.Second

// ---- manual clock ----

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// ---- fake store ----

type fakeStore struct {
	mu        sync.Mutex
	version   int64
	epoch     int64
	accesses  map[string]store.Access
	tokens    map[string]store.Token
	policies  map[string]store.Policy
	exposures map[string][]store.Exposure
	audits    []store.AuditEntry
	upserts   int
	raises    []int64

	applyErr  error // returned before applying
	commitErr error // returned after applying (unknown outcome)
	upsertErr error
	raiseErr  error
	healthErr error
	listErr   error
	loadErr   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{accesses: map[string]store.Access{}, tokens: map[string]store.Token{},
		policies: map[string]store.Policy{}, exposures: map[string][]store.Exposure{}, epoch: 1}
}

func (f *fakeStore) set(fn func(f *fakeStore)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

func (f *fakeStore) Backend() string       { return "fake" }
func (f *fakeStore) EngineVersion() string { return "fake-1" }

func (f *fakeStore) LoadSnapshot(context.Context) (store.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return store.Snapshot{}, f.loadErr
	}
	s := store.Snapshot{Meta: store.Meta{StoreID: "st_fake", Backend: "fake", ConfigVersion: f.version, Epoch: f.epoch}}
	for _, a := range f.accesses {
		s.Accesses = append(s.Accesses, a)
	}
	for _, t := range f.tokens {
		s.Tokens = append(s.Tokens, t)
	}
	for _, p := range f.policies {
		s.Policies = append(s.Policies, p)
	}
	for _, es := range f.exposures {
		s.Exposures = append(s.Exposures, es...)
	}
	return s, nil
}

func (f *fakeStore) ApplyConfig(_ context.Context, expected int64, muts []store.Mutation, audit []store.AuditEntry) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applyErr != nil {
		return 0, f.applyErr
	}
	if expected != f.version {
		return 0, &store.ConflictError{Current: f.version}
	}
	f.version++
	for _, m := range muts {
		switch v := m.(type) {
		case store.PutAccess:
			f.accesses[v.Access.AccessID] = v.Access
		case store.DeleteAccess:
			delete(f.accesses, v.AccessID)
			delete(f.exposures, v.AccessID)
		case store.PutToken:
			f.tokens[v.Token.TokenID] = v.Token
		case store.RevokeToken:
			t := f.tokens[v.TokenID]
			if t.RevokedAt == nil {
				at := v.At
				t.RevokedAt = &at
			}
			f.tokens[v.TokenID] = t
		case store.PutPolicy:
			p := v.Policy
			p.UpdatedVersion = f.version
			f.policies[p.PolicyID] = p
		case store.DeletePolicy:
			delete(f.policies, v.PolicyID)
		}
	}
	for _, a := range audit {
		a.ConfigVersion = f.version
		f.audits = append(f.audits, a)
	}
	return f.version, f.commitErr
}

func (f *fakeStore) UpsertRegistry(_ context.Context, accessID string, exposures []store.Exposure, audit []store.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserts++
	f.exposures[accessID] = append([]store.Exposure(nil), exposures...)
	f.audits = append(f.audits, audit...)
	return nil
}

func (f *fakeStore) StartRun(context.Context, string, time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.epoch++
	return f.epoch, nil
}

func (f *fakeStore) RaiseEpoch(_ context.Context, min int64, audit store.AuditEntry) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.raiseErr != nil {
		return false, f.raiseErr
	}
	f.raises = append(f.raises, min)
	if f.epoch >= min {
		return false, nil
	}
	f.epoch = min
	if audit.Action != "" {
		f.audits = append(f.audits, audit)
	}
	return true, nil
}

func (f *fakeStore) AppendAudit(_ context.Context, entries []store.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audits = append(f.audits, entries...)
	return nil
}

func (f *fakeStore) ListAudit(_ context.Context, after int64, limit int) ([]store.AuditEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []store.AuditEntry
	for i, a := range f.audits {
		a.Seq = int64(i + 1)
		if a.Seq > after && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeStore) Health(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.healthErr
}

func (f *fakeStore) Close() error { return nil }

func (f *fakeStore) auditActions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, a := range f.audits {
		out = append(out, a.Action)
	}
	return out
}

// ---- fake WebSocket ----

var writeSeq atomic.Int64

type wsMsg struct {
	text bool
	data []byte
	err  error
	seq  int64
}

type fakeConn struct {
	in       chan wsMsg
	out      chan wsMsg
	closed   chan struct{}
	once     sync.Once
	graceful atomic.Bool
	pings    atomic.Int64
	pingErr  atomic.Pointer[error]
	writeErr atomic.Pointer[error]
	gate     atomic.Pointer[chan struct{}] // when set, each Write waits for one token
}

func newFakeConn() *fakeConn {
	return &fakeConn{in: make(chan wsMsg, 64), out: make(chan wsMsg, 256), closed: make(chan struct{})}
}

func (c *fakeConn) Read(ctx context.Context) (bool, []byte, error) {
	select {
	case m := <-c.in:
		return m.text, m.data, m.err
	case <-c.closed:
		return false, nil, net.ErrClosed
	case <-ctx.Done():
		_ = c.CloseNow()
		return false, nil, ctx.Err()
	}
}

func (c *fakeConn) Write(ctx context.Context, text bool, data []byte) error {
	if p := c.writeErr.Load(); p != nil {
		return *p
	}
	if g := c.gate.Load(); g != nil {
		select {
		case <-*g:
		case <-c.closed:
			return net.ErrClosed
		case <-ctx.Done():
			_ = c.CloseNow()
			return ctx.Err()
		}
	}
	select {
	case <-c.closed:
		return net.ErrClosed
	default:
	}
	b := append([]byte(nil), data...)
	select {
	case c.out <- wsMsg{text: text, data: b, seq: writeSeq.Add(1)}:
		return nil
	case <-c.closed:
		return net.ErrClosed
	case <-ctx.Done():
		_ = c.CloseNow()
		return ctx.Err()
	}
}

func (c *fakeConn) Ping(ctx context.Context) error {
	c.pings.Add(1)
	if p := c.pingErr.Load(); p != nil {
		return *p
	}
	return nil
}

func (c *fakeConn) Close(string) error {
	c.graceful.Store(true)
	return c.CloseNow()
}

func (c *fakeConn) CloseNow() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *fakeConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *fakeConn) push(text bool, data []byte) { c.in <- wsMsg{text: text, data: data} }

func (c *fakeConn) setGate(g chan struct{}) { c.gate.Store(&g) }

func (c *fakeConn) next(t *testing.T) wsMsg {
	t.Helper()
	select {
	case m := <-c.out:
		return m
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for a written message")
		return wsMsg{}
	}
}

// nextOrClosed returns the next message or ok=false once the connection
// is closed and drained.
func (c *fakeConn) nextOrClosed(t *testing.T) (wsMsg, bool) {
	t.Helper()
	select {
	case m := <-c.out:
		return m, true
	case <-time.After(waitTimeout):
	case <-c.closed:
		select {
		case m := <-c.out:
			return m, true
		default:
			return wsMsg{}, false
		}
	}
	t.Fatal("timed out waiting for a message or close")
	return wsMsg{}, false
}

func (c *fakeConn) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-c.closed:
	case <-time.After(waitTimeout):
		t.Fatal("connection not closed")
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// ---- environment ----

type env struct {
	t     *testing.T
	h     *Hub
	st    *fakeStore
	clk   *clock
	admin Principal
}

func testLimits() limits.Limits {
	l := limits.Defaults()
	l.PrepareMS, l.CloseMS, l.RetainMS, l.IdleMS = 2000, 1000, 5000, 2000
	l.HeartbeatMS, l.LicenseTTLMS, l.LicenseMarginMS = 1000, 3000, 500
	l.PingMS = 3_600_000
	l.StoreProbeMS = 500
	return l
}

func newEnv(t *testing.T, mods ...func(*limits.Limits)) *env {
	t.Helper()
	l := testLimits()
	for _, m := range mods {
		m(&l)
	}
	st := newFakeStore()
	clk := &clock{t: time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)}
	h := New(Config{Limits: l, Store: st, Snapshot: store.Snapshot{Meta: store.Meta{StoreID: "st_fake"}},
		Epoch: 1, RunID: "run_test", Now: clk.Now})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		h.Shutdown(ctx)
	})
	return &env{t: t, h: h, st: st, clk: clk, admin: Principal{TokenID: "00000000000000ad", Subject: "root", Scope: protocol.ScopeAdmin}}
}

func (e *env) v() int64 { return e.h.ConfigView().ConfigVersion }

func (e *env) ok(_ int64, err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

func pin(n int) string { return fmt.Sprintf("%064x", n) }

func rv(n int) string { return "rv_" + strings.Repeat("a", 25) + string(rune('a'+n)) }

func ptr[T any](v T) *T { return &v }

func (e *env) addAccess(id string, pins ...string) {
	e.t.Helper()
	e.ok(e.h.CreateAccess(context.Background(), e.admin, e.v(), protocol.AccessSpec{AccessID: id, SPKIPins: pins}))
}

func (e *env) addPolicy(id, subject, selector string, actions ...string) {
	e.t.Helper()
	e.ok(e.h.CreatePolicy(context.Background(), e.admin, e.v(), protocol.PolicySpec{
		PolicyID: id, Subject: subject, ExposureSelector: selector, Actions: actions}))
}

func (e *env) token(subject, scope string, ttl int64) (Principal, string) {
	e.t.Helper()
	c, err := e.h.CreateToken(context.Background(), e.admin, e.v(), protocol.TokenCreate{Subject: subject, Scope: scope, TTLSeconds: ttl})
	if err != nil {
		e.t.Fatal(err)
	}
	p, err := e.h.Authenticate(c.Token)
	if err != nil {
		e.t.Fatal(err)
	}
	return p, c.Token
}

func expo(id, rev string) protocol.RegisterExposure {
	return protocol.RegisterExposure{ExposureID: id, Revision: rev, Protocol: "http",
		TransportProfile: protocol.TransportTCP, AdmissionMode: protocol.AdmissionBestEffort, Description: "fixture"}
}

// ---- OIC side ----

type oic struct {
	t       *testing.T
	access  string
	conn    *fakeConn
	done    chan struct{}
	seq     uint64
	welcome *protocol.Welcome
}

func (e *env) dial(accessID, pinValue string) *oic {
	o := &oic{t: e.t, access: accessID, conn: newFakeConn(), done: make(chan struct{})}
	go func() {
		e.h.ServeControl(o.conn, accessID, pinValue)
		close(o.done)
	}()
	return o
}

func (o *oic) send(m protocol.Message) {
	o.seq++
	o.conn.push(true, protocol.EncodeControl(m, o.seq))
}

func (o *oic) recv() protocol.Message {
	o.t.Helper()
	m := o.conn.next(o.t)
	msg, err := protocol.DecodeControl(m.data)
	if err != nil || !m.text {
		o.t.Fatalf("bad control message %q: %v", m.data, err)
	}
	return msg
}

func expect[T protocol.Message](o *oic) T {
	o.t.Helper()
	m := o.recv()
	v, ok := m.(T)
	if !ok {
		o.t.Fatalf("got %T %+v", m, m)
	}
	return v
}

// prepareMsg returns the next prepare_channel, skipping close_channel and
// heartbeat.
func (o *oic) prepareMsg() *protocol.PrepareChannel {
	o.t.Helper()
	for {
		switch m := o.recv().(type) {
		case *protocol.PrepareChannel:
			return m
		case *protocol.CloseChannel, *protocol.Heartbeat:
		default:
			o.t.Fatalf("got %T %+v", m, m)
		}
	}
}

func (o *oic) waitDone() {
	o.t.Helper()
	select {
	case <-o.done:
	case <-time.After(waitTimeout):
		o.t.Fatal("control session did not end")
	}
}

func (e *env) connect(accessID, pinValue string, exps ...protocol.RegisterExposure) *oic {
	e.t.Helper()
	o := e.dial(accessID, pinValue)
	o.send(&protocol.Hello{OICVersion: "test"})
	o.welcome = expect[*protocol.Welcome](o)
	o.send(&protocol.Register{Exposures: exps})
	ack := expect[*protocol.RegisterAck](o)
	if len(ack.Rejected) != 0 {
		e.t.Fatalf("rejected %+v", ack.Rejected)
	}
	return o
}

// ---- data legs ----

type legEnd struct {
	t    *testing.T
	conn *fakeConn
	done chan struct{}
}

func (e *env) attachConn(res *Reservation, c *fakeConn) *legEnd {
	l := &legEnd{t: e.t, conn: c, done: make(chan struct{})}
	go func() {
		e.h.Attach(res, c)
		close(l.done)
	}()
	return l
}

func (e *env) attach(res *Reservation) *legEnd { return e.attachConn(res, newFakeConn()) }

func (l *legEnd) frame() protocol.Frame {
	l.t.Helper()
	m := l.conn.next(l.t)
	f, err := protocol.DecodeFrame(m.data, 1<<20)
	if err != nil || m.text {
		l.t.Fatalf("bad frame %v: %v", m.data, err)
	}
	return f
}

func (l *legEnd) expectAbort(code protocol.Code) {
	l.t.Helper()
	f := l.frame()
	if f.Type != protocol.FrameAbort || f.Reason != code {
		l.t.Fatalf("want ABORT(%s), got type %d reason %s", code, f.Type, f.Reason)
	}
	l.conn.waitClosed(l.t)
}

func (l *legEnd) expectType(typ byte) protocol.Frame {
	l.t.Helper()
	f := l.frame()
	if f.Type != typ {
		l.t.Fatalf("want frame %d, got %d (%s)", typ, f.Type, f.Reason)
	}
	return f
}

func (l *legEnd) data(p string)              { l.conn.push(false, protocol.DataFrame([]byte(p))) }
func (l *legEnd) fin()                       { l.conn.push(false, protocol.FinFrame()) }
func (l *legEnd) abort(code protocol.Code)   { l.conn.push(false, protocol.AbortFrame(code, "test")) }
func (l *legEnd) raw(text bool, data []byte) { l.conn.push(text, data) }

func (l *legEnd) waitDone() {
	l.t.Helper()
	select {
	case <-l.done:
	case <-time.After(waitTimeout):
		l.t.Fatal("leg did not end")
	}
}

// fixture is one Access with a registered exposure, a consumer token and
// a policy granting discover and open.
type fixture struct {
	*env
	o     *oic
	alice Principal
	rev   string
}

const exp1 = "lab-a/http"

func newFixture(t *testing.T, mods ...func(*limits.Limits)) *fixture {
	t.Helper()
	e := newEnv(t, mods...)
	e.addAccess("lab-a", pin(1))
	e.addPolicy("p1", "alice", "lab-a/*", protocol.ActionDiscover, protocol.ActionOpen)
	alice, _ := e.token("alice", protocol.ScopeConsumer, 0)
	o := e.connect("lab-a", pin(1), expo(exp1, rv(1)))
	return &fixture{env: e, o: o, alice: alice, rev: rv(1)}
}

type flow struct {
	open       OpenResult
	prep       *protocol.PrepareChannel
	cons, prov *legEnd
}

func (f *fixture) id(fl *flow) string { return fl.open.Response.ChannelID }

// prepare opens a channel and reads the prepare_channel message.
func (f *fixture) prepare() *flow {
	f.t.Helper()
	r, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev})
	if err != nil {
		f.t.Fatal(err)
	}
	return &flow{open: r, prep: f.o.prepareMsg()}
}

func (f *fixture) reserve(fl *flow, role Role) *Reservation {
	f.t.Helper()
	req := AttachRequest{ChannelID: f.id(fl), Role: role}
	if role == Consumer {
		req.Ticket, req.Principal = fl.open.Ticket, f.alice
	} else {
		req.Ticket, req.AccessID = fl.prep.ProviderTicket, "lab-a"
	}
	res, err := f.h.ReserveAttach(req)
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

// openFlow brings a channel to Open and checks READY ordering.
func (f *fixture) openFlow() *flow {
	f.t.Helper()
	fl := f.prepare()
	fl.prov = f.attach(f.reserve(fl, Provider))
	fl.cons = f.attach(f.reserve(fl, Consumer))
	pm := fl.prov.conn.next(f.t)
	cm := fl.cons.conn.next(f.t)
	if pm.data[0] != protocol.FrameReady || cm.data[0] != protocol.FrameReady || pm.seq > cm.seq {
		f.t.Fatalf("READY order: provider %v@%d consumer %v@%d", pm.data, pm.seq, cm.data, cm.seq)
	}
	return fl
}

func (f *fixture) state(id string) protocol.ChannelView {
	f.t.Helper()
	v, err := f.h.Channel(f.alice, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func (f *fixture) waitState(id, state string, reason protocol.Code) {
	f.t.Helper()
	waitFor(f.t, "channel "+state+"/"+string(reason), func() bool {
		v, err := f.h.Channel(f.alice, id)
		return err == nil && v.State == state && v.CloseReason == reason
	})
}

func codeOf(err error) protocol.Code {
	if err == nil {
		return ""
	}
	return protocol.CodeOf(err)
}

var (
	errBoom        = errors.New("boom")
	errUnavailable = fmt.Errorf("%w: test outage", store.ErrUnavailable)
)
