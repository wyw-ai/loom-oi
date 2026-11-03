package relay

import (
	"context"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestRoleString(t *testing.T) {
	if Consumer.String() != "consumer" || Provider.String() != "provider" {
		t.Fatal(Consumer.String(), Provider.String())
	}
}

func TestNewLoadsSnapshot(t *testing.T) {
	now := time.Now()
	snap := store.Snapshot{
		Meta:      store.Meta{StoreID: "st_x", ConfigVersion: 7, Maintenance: true},
		Accesses:  []store.Access{{AccessID: "lab-a", Enabled: true, SPKIPins: []string{pin(1)}, MaxExposures: 4}},
		Tokens:    []store.Token{{TokenID: "0123456789abcdef", Subject: "alice", Scope: protocol.ScopeConsumer, ExpiresAt: now.Add(time.Hour)}},
		Policies:  []store.Policy{{PolicyID: "p", Subject: "alice", ExposureSelector: "lab-a/*", Actions: []string{"discover"}, Enabled: true}},
		Exposures: []store.Exposure{{ExposureID: exp1, AccessID: "lab-a", Revision: rv(1), Protocol: "http", TransportProfile: "tcp", AdmissionMode: "best_effort"}},
	}
	h := New(Config{Limits: testLimits(), Store: newFakeStore(), Snapshot: snap, Epoch: 3, RunID: "r"})
	defer h.Shutdown(context.Background())
	cv := h.ConfigView()
	if cv.ConfigVersion != 7 || cv.Epoch != 3 || !cv.Maintenance || cv.StoreID != "st_x" || cv.Backend != "fake" || cv.RunID != "r" || !cv.StoreHealthy {
		t.Fatalf("%+v", cv)
	}
	list, err := h.Discover(Principal{Subject: "alice"}, 0, "")
	if err != nil || len(list.Items) != 1 || list.Items[0].AccessOnline || list.Items[0].Readiness != protocol.ReadinessUnknown {
		t.Fatalf("%+v %v", list, err)
	}
	if id, ok := h.AccessForPin(pin(1)); !ok || id != "lab-a" {
		t.Fatal(id, ok)
	}
}

func TestAuthenticate(t *testing.T) {
	e := newEnv(t)
	p, bearer := e.token("alice", protocol.ScopeConsumer, 60)
	if p.Subject != "alice" || p.Scope != protocol.ScopeConsumer || len(p.TokenID) != 16 {
		t.Fatalf("%+v", p)
	}
	id, _, _ := identity.ParseToken(bearer)
	other, _, _ := identity.NewToken()
	_, otherBearer, _ := identity.NewToken()
	wrongSecret := "oi_" + id + otherBearer[len("oi_")+16:]
	for _, b := range []string{"", "nope", other, wrongSecret, "oi_" + other + otherBearer[19:]} {
		if _, err := e.h.Authenticate(b); codeOf(err) != protocol.CodeUnauthenticated {
			t.Errorf("%q: %v", b, err)
		}
	}
	e.clk.Add(61 * time.Second)
	if _, err := e.h.Authenticate(bearer); codeOf(err) != protocol.CodeUnauthenticated {
		t.Errorf("expired: %v", err)
	}
	e.clk.Add(-61 * time.Second)
	e.ok(e.h.RevokeToken(context.Background(), e.admin, e.v(), p.TokenID))
	if _, err := e.h.Authenticate(bearer); codeOf(err) != protocol.CodeUnauthenticated {
		t.Errorf("revoked: %v", err)
	}
}

func TestAccessForPin(t *testing.T) {
	e := newEnv(t)
	e.addAccess("lab-a", pin(1), pin(2))
	for _, p := range []string{pin(1), pin(2)} {
		if id, ok := e.h.AccessForPin(p); !ok || id != "lab-a" {
			t.Fatal(p, id, ok)
		}
	}
	if _, ok := e.h.AccessForPin(pin(3)); ok {
		t.Fatal("unknown pin")
	}
	e.ok(e.h.PatchAccess(context.Background(), e.admin, e.v(), "lab-a", protocol.AccessSpec{Enabled: ptr(false)}))
	if _, ok := e.h.AccessForPin(pin(1)); ok {
		t.Fatal("disabled access")
	}
}

func TestDiscover(t *testing.T) {
	f := newFixture(t)
	f.addAccess("lab-b", pin(2))
	f.connect("lab-b", pin(2), expo("lab-b/http", rv(2)), expo("lab-b/alt", rv(3)))
	f.addPolicy("p2", "alice", "lab-b/http", protocol.ActionDiscover)
	list, err := f.h.Discover(f.alice, 0, "")
	if err != nil || len(list.Items) != 2 || list.NextCursor != "" {
		t.Fatalf("%+v %v", list, err)
	}
	if list.Items[0].ExposureID != exp1 || list.Items[1].ExposureID != "lab-b/http" || !list.Items[0].AccessOnline {
		t.Fatalf("%+v", list.Items)
	}
	it := list.Items[0]
	if it.AccessID != "lab-a" || it.Revision != rv(1) || it.Protocol != "http" || it.TransportProfile != "tcp" ||
		it.AdmissionMode != protocol.AdmissionBestEffort || it.Description != "fixture" {
		t.Fatalf("%+v", it)
	}
	page, err := f.h.Discover(f.alice, 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("%+v %v", page, err)
	}
	page, err = f.h.Discover(f.alice, 1, page.NextCursor)
	if err != nil || len(page.Items) != 1 || page.Items[0].ExposureID != "lab-b/http" || page.NextCursor != "" {
		t.Fatalf("%+v %v", page, err)
	}
	for _, lim := range []int{-1, 101} {
		if _, err := f.h.Discover(f.alice, lim, ""); codeOf(err) != protocol.CodeInvalidRequest {
			t.Errorf("limit %d: %v", lim, err)
		}
	}
	if _, err := f.h.Discover(f.alice, 1, "!"); codeOf(err) != protocol.CodeInvalidRequest {
		t.Errorf("cursor: %v", err)
	}
	if l, _ := f.h.Discover(Principal{Subject: "mallory"}, 0, ""); len(l.Items) != 0 {
		t.Fatalf("no grant: %+v", l)
	}
}

func TestOpenPrecheck(t *testing.T) {
	f := newFixture(t, func(l *limits.Limits) { l.NSubject = 1 })
	f.addAccess("lab-b", pin(2))
	f.addAccess("lab-c", pin(3))
	f.addPolicy("p2", "alice", "lab-b/*", protocol.ActionDiscover, protocol.ActionOpen)
	f.addPolicy("p3", "alice", "lab-c/http", protocol.ActionDiscover)
	f.addPolicy("p4", "alice", "lab-a/strict", protocol.ActionDiscover, protocol.ActionOpen)
	f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{expo(exp1, rv(1)), func() protocol.RegisterExposure {
		x := expo("lab-a/strict", rv(4))
		x.AdmissionMode = protocol.AdmissionRequireReady
		return x
	}()}})
	expect[*protocol.RegisterAck](f.o)
	f.connect("lab-c", pin(3), expo("lab-c/http", rv(2)))
	// lab-b registered in a previous run: visible but offline.
	f.h.mu.Lock()
	f.h.exposures["lab-b/http"] = &exposure{rec: store.Exposure{ExposureID: "lab-b/http", AccessID: "lab-b", Revision: rv(5), AdmissionMode: protocol.AdmissionBestEffort}}
	f.h.mu.Unlock()

	cases := []struct {
		name string
		req  protocol.OpenRequest
		code protocol.Code
	}{
		{"bad exposure", protocol.OpenRequest{ExposureID: "nope", ExpectedRevision: rv(1)}, protocol.CodeInvalidRequest},
		{"bad revision", protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: "rv_x"}, protocol.CodeInvalidRequest},
		{"bad request id", protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(1), ClientRequestID: "a b"}, protocol.CodeInvalidRequest},
		{"admission loosening", protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(1), Admission: protocol.AdmissionBestEffort}, protocol.CodeInvalidRequest},
		{"unknown", protocol.OpenRequest{ExposureID: "lab-a/none", ExpectedRevision: rv(1)}, protocol.CodeNotFound},
		{"no discover", protocol.OpenRequest{ExposureID: "lab-z/http", ExpectedRevision: rv(1)}, protocol.CodeNotFound},
		{"no open", protocol.OpenRequest{ExposureID: "lab-c/http", ExpectedRevision: rv(2)}, protocol.CodeDenied},
		{"stale revision", protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(9)}, protocol.CodeRevisionMismatch},
		{"offline", protocol.OpenRequest{ExposureID: "lab-b/http", ExpectedRevision: rv(5)}, protocol.CodeAccessOffline},
		{"require_ready exposure", protocol.OpenRequest{ExposureID: "lab-a/strict", ExpectedRevision: rv(4)}, protocol.CodeReadinessRequired},
		{"require_ready request", protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(1), Admission: protocol.AdmissionRequireReady}, protocol.CodeReadinessRequired},
	}
	for _, c := range cases {
		if _, err := f.h.Open(f.alice, c.req); codeOf(err) != c.code {
			t.Errorf("%s: got %v want %s", c.name, err, c.code)
		}
	}
	r, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(1)})
	if err != nil || r.Response.State != protocol.StatePreparing || r.Response.Readiness != protocol.ReadinessUnknown ||
		r.Response.AdmissionMode != protocol.AdmissionBestEffort || r.Response.Attach == nil || r.Response.PrepareExpiresInMS != 2000 || r.Replay {
		t.Fatalf("%+v %v", r, err)
	}
	expect[*protocol.PrepareChannel](f.o)
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(1)}); codeOf(err) != protocol.CodeResourceExhausted {
		t.Fatalf("subject quota: %v", err)
	}
	f.h.mu.Lock()
	f.h.storeOK = false
	f.h.lim.NSubject = 5
	f.h.mu.Unlock()
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(1)}); codeOf(err) != protocol.CodeStoreUnavailable {
		t.Fatalf("store: %v", err)
	}
	f.h.mu.Lock()
	f.h.shutting = true
	f.h.mu.Unlock()
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(1)}); codeOf(err) != protocol.CodeShutdown {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestQuotaLimits(t *testing.T) {
	for _, mod := range []func(*limits.Limits){
		func(l *limits.Limits) { l.NOIS = 2 },
		func(l *limits.Limits) { l.NAccess = 2 },
		func(l *limits.Limits) { l.NSubject = 2 },
	} {
		f := newFixture(t, mod)
		var ids []string
		for range 2 {
			ids = append(ids, f.id(f.prepare()))
		}
		if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev}); codeOf(err) != protocol.CodeResourceExhausted {
			t.Fatalf("third open: %v", err)
		}
		if _, _, err := f.h.CloseChannel(f.alice, ids[0]); err != nil {
			t.Fatal(err)
		}
		expect[*protocol.CloseChannel](f.o)
		f.prepare()
		f.h.mu.Lock()
		q := f.h.quota
		f.h.mu.Unlock()
		if q.total != 2 || q.perAccess["lab-a"] != 2 || q.perSubject["alice"] != 2 {
			t.Fatalf("%+v", q)
		}
	}
}

func TestOpenIdempotency(t *testing.T) {
	f := newFixture(t)
	req := protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev, ClientRequestID: "req-1"}
	r1, err := f.h.Open(f.alice, req)
	if err != nil {
		t.Fatal(err)
	}
	prep := expect[*protocol.PrepareChannel](f.o)
	r2, err := f.h.Open(f.alice, req)
	if err != nil || !r2.Replay || r2.Response.ChannelID != r1.Response.ChannelID || r2.Ticket != r1.Ticket || r2.Response.Attach == nil {
		t.Fatalf("replay %+v %v", r2, err)
	}
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: rv(7), ClientRequestID: "req-1"}); codeOf(err) != protocol.CodeIdempotencyConflict {
		t.Fatalf("conflict: %v", err)
	}
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev, ClientRequestID: "req-1", Admission: protocol.AdmissionRequireReady}); codeOf(err) != protocol.CodeIdempotencyConflict {
		t.Fatalf("admission conflict: %v", err)
	}
	fl := &flow{open: r1, prep: prep}
	f.attach(f.reserve(fl, Consumer))
	r3, err := f.h.Open(f.alice, req)
	if err != nil || r3.Response.Attach != nil || r3.Ticket != "" || r3.Response.ChannelID != r1.Response.ChannelID {
		t.Fatalf("after consume %+v %v", r3, err)
	}
	// Another subject's request ID is a different key.
	f.addPolicy("pb", "bob", "lab-a/*", protocol.ActionDiscover, protocol.ActionOpen)
	bob, _ := f.token("bob", protocol.ScopeConsumer, 0)
	rb, err := f.h.Open(bob, req)
	if err != nil || rb.Replay || rb.Response.ChannelID == r1.Response.ChannelID {
		t.Fatalf("bob %+v %v", rb, err)
	}
	expect[*protocol.PrepareChannel](f.o)
	// Closed records keep the mapping for RETAIN, then a new channel results.
	if _, _, err := f.h.CloseChannel(f.alice, r1.Response.ChannelID); err != nil {
		t.Fatal(err)
	}
	r4, _ := f.h.Open(f.alice, req)
	if r4.Response.State != protocol.StateClosed || r4.Response.ChannelID != r1.Response.ChannelID || r4.Response.Attach != nil {
		t.Fatalf("closed replay %+v", r4)
	}
	f.clk.Add(6 * time.Second)
	f.h.sweep(f.clk.Now())
	r5, err := f.h.Open(f.alice, req)
	if err != nil || r5.Replay || r5.Response.ChannelID == r1.Response.ChannelID {
		t.Fatalf("after retain %+v %v", r5, err)
	}
}

func TestChannelAndClose(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	id := f.id(fl)
	if v := f.state(id); v.State != protocol.StatePreparing || v.ExposureID != exp1 || v.Revision != f.rev || v.CloseReason != "" {
		t.Fatalf("%+v", v)
	}
	mallory := Principal{Subject: "mallory", TokenID: "1111111111111111"}
	if _, err := f.h.Channel(mallory, id); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	if _, _, err := f.h.CloseChannel(mallory, id); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	v, changed, err := f.h.CloseChannel(f.alice, id)
	if err != nil || !changed || v.State != protocol.StateClosed || v.CloseReason != protocol.CodeClosedByClient {
		t.Fatalf("%+v %v %v", v, changed, err)
	}
	cc := expect[*protocol.CloseChannel](f.o)
	if cc.ChannelID != id || cc.Code != protocol.CodeClosedByClient {
		t.Fatalf("%+v", cc)
	}
	if _, changed, _ := f.h.CloseChannel(f.alice, id); changed {
		t.Fatal("second close changed state")
	}
	fl2 := f.openFlow()
	v, changed, err = f.h.CloseChannel(f.alice, f.id(fl2))
	if err != nil || !changed || v.State != protocol.StateClosing {
		t.Fatalf("open close %+v %v %v", v, changed, err)
	}
	fl2.prov.expectAbort(protocol.CodeClosedByClient)
	fl2.cons.expectAbort(protocol.CodeClosedByClient)
	f.waitState(f.id(fl2), protocol.StateClosed, protocol.CodeClosedByClient)
}

func TestStats(t *testing.T) {
	f := newFixture(t)
	f.prepare()
	fl := f.openFlow()
	fl.cons.data("hello")
	if got := fl.prov.expectType(protocol.FrameData); string(got.Data) != "hello" {
		t.Fatal(string(got.Data))
	}
	closed := f.prepare()
	f.h.CloseChannel(f.alice, f.id(closed))
	waitFor(t, "relay bytes", func() bool { return f.h.Stats().RelayBytes == 5 })
	s := f.h.Stats()
	if s.Channels.Preparing != 1 || s.Channels.Open != 1 || s.Channels.Closed != 1 || s.Tickets != 2 ||
		s.AccessesOnline != 1 || !s.StoreHealthy || s.Epoch != 1 || s.MaxBufferedBytes < 5 || s.Process.Goroutines == 0 {
		t.Fatalf("%+v", s)
	}
	f.h.CloseChannel(f.alice, f.id(fl))
	waitFor(t, "closing counted", func() bool {
		st := f.h.Stats()
		return st.Channels.Closing == 1 || st.Channels.Closed == 2
	})
}

func TestAddInflightKeepsMaximum(t *testing.T) {
	e := newEnv(t)
	e.h.addInflight(10)
	e.h.addInflight(-10)
	e.h.addInflight(4)
	if e.h.maxInflight.Load() != 10 || e.h.inflight.Load() != 4 {
		t.Fatal(e.h.maxInflight.Load(), e.h.inflight.Load())
	}
}
