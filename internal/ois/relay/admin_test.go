package relay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

var bg = context.Background()

func TestAccessAdmin(t *testing.T) {
	e := newEnv(t)
	e.addAccess("lab-b", pin(7))
	labels := map[string]string{}
	for i := range 33 {
		labels[fmt.Sprintf("k%d", i)] = "v"
	}
	bad := []protocol.AccessSpec{
		{AccessID: "Bad"},
		{AccessID: "lab-b"},
		{AccessID: "lab-a", SPKIPins: []string{pin(1), pin(2), pin(3)}},
		{AccessID: "lab-a", SPKIPins: []string{"zz"}},
		{AccessID: "lab-a", SPKIPins: []string{pin(1), pin(1)}},
		{AccessID: "lab-a", SPKIPins: []string{pin(7)}},
		{AccessID: "lab-a", MaxExposures: ptr(0)},
		{AccessID: "lab-a", MaxExposures: ptr(129)},
		{AccessID: "lab-a", Labels: labels},
		{AccessID: "lab-a", Labels: map[string]string{"Bad Key": "v"}},
		{AccessID: "lab-a", Labels: map[string]string{"k": strings.Repeat("v", 257)}},
	}
	for i, spec := range bad {
		if _, err := e.h.CreateAccess(bg, e.admin, e.v(), spec); codeOf(err) != protocol.CodeInvalidRequest {
			t.Errorf("case %d: %v", i, err)
		}
	}
	v, err := e.h.CreateAccess(bg, e.admin, e.v(), protocol.AccessSpec{AccessID: "lab-a", SPKIPins: []string{pin(1)},
		Labels: map[string]string{"site": "lab"}, MaxExposures: ptr(5)})
	if err != nil || v != e.v() {
		t.Fatal(v, err)
	}
	a, err := e.h.GetAccess("lab-a")
	if err != nil || !a.Enabled || a.MaxExposures != 5 || a.Labels["site"] != "lab" || a.Online || a.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err := e.h.GetAccess("nope"); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	if l := e.h.ListAccesses(); len(l) != 2 || l[0].AccessID != "lab-a" {
		t.Fatalf("%+v", l)
	}
	if _, err := e.h.PatchAccess(bg, e.admin, e.v(), "nope", protocol.AccessSpec{}); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	if _, err := e.h.PatchAccess(bg, e.admin, e.v(), "lab-a", protocol.AccessSpec{AccessID: "lab-x"}); codeOf(err) != protocol.CodeInvalidRequest {
		t.Fatal(err)
	}
	if _, err := e.h.PatchAccess(bg, e.admin, e.v(), "lab-a", protocol.AccessSpec{MaxExposures: ptr(-1)}); codeOf(err) != protocol.CodeInvalidRequest {
		t.Fatal(err)
	}
	// TV-B2-07: rotation keeps both pins valid, then removing the old pin
	// closes the connection that used it.
	e.ok(e.h.PatchAccess(bg, e.admin, e.v(), "lab-a", protocol.AccessSpec{AccessID: "lab-a", SPKIPins: []string{pin(1), pin(2)}}))
	o1 := e.connect("lab-a", pin(1))
	if _, ok := e.h.AccessForPin(pin(2)); !ok {
		t.Fatal("new pin rejected during rotation")
	}
	e.ok(e.h.PatchAccess(bg, e.admin, e.v(), "lab-a", protocol.AccessSpec{SPKIPins: []string{pin(2)}}))
	if ce := expect[*protocol.ControlError](o1); ce.Code != protocol.CodeRevoked {
		t.Fatalf("%+v", ce)
	}
	o1.waitDone()
	o2 := e.connect("lab-a", pin(2))
	e.ok(e.h.PatchAccess(bg, e.admin, e.v(), "lab-a", protocol.AccessSpec{Labels: map[string]string{}}))
	if a, _ := e.h.GetAccess("lab-a"); !a.Online || len(a.Labels) != 0 {
		t.Fatalf("label change must keep the session: %+v", a)
	}
	// TV-B2-13: disabling closes the session; re-enabling needs a new connection.
	e.ok(e.h.PatchAccess(bg, e.admin, e.v(), "lab-a", protocol.AccessSpec{Enabled: ptr(false)}))
	if ce := expect[*protocol.ControlError](o2); ce.Code != protocol.CodeRevoked {
		t.Fatalf("%+v", ce)
	}
	o2.waitDone()
	e.ok(e.h.PatchAccess(bg, e.admin, e.v(), "lab-a", protocol.AccessSpec{Enabled: ptr(true)}))
	if a, _ := e.h.GetAccess("lab-a"); a.Online {
		t.Fatal("re-enabled access is online without a connection")
	}
	o3 := e.connect("lab-a", pin(2), expo("lab-a/http", rv(1)))
	if o3.welcome.Generation != 3 {
		t.Fatalf("%+v", o3.welcome)
	}
	if _, err := e.h.DeleteAccess(bg, e.admin, e.v(), "nope"); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	e.ok(e.h.DeleteAccess(bg, e.admin, e.v(), "lab-a"))
	if ce := expect[*protocol.ControlError](o3); ce.Code != protocol.CodeRevoked {
		t.Fatalf("%+v", ce)
	}
	o3.waitDone()
	e.h.mu.Lock()
	_, still := e.h.exposures["lab-a/http"]
	e.h.mu.Unlock()
	if still {
		t.Fatal("registry of a deleted access remains")
	}
	got := strings.Join(e.st.auditActions(), ",")
	if !strings.Contains(got, "access_created") || !strings.Contains(got, "access_updated") || !strings.Contains(got, "access_deleted") {
		t.Fatal(got)
	}
}

func TestTokenAdmin(t *testing.T) {
	e := newEnv(t)
	for _, req := range []protocol.TokenCreate{
		{Subject: "", Scope: protocol.ScopeConsumer},
		{Subject: "alice", Scope: "root"},
		{Subject: "alice", Scope: protocol.ScopeConsumer, TTLSeconds: -1},
		{Subject: "alice", Scope: protocol.ScopeConsumer, TTLSeconds: 90*86400 + 1},
	} {
		if _, err := e.h.CreateToken(bg, e.admin, e.v(), req); codeOf(err) != protocol.CodeInvalidRequest {
			t.Errorf("%+v: %v", req, err)
		}
	}
	now := e.clk.Now()
	c, err := e.h.CreateToken(bg, e.admin, e.v(), protocol.TokenCreate{Subject: "alice", Scope: protocol.ScopeConsumer})
	if err != nil || c.ConfigVersion != e.v() || !c.ExpiresAt.Equal(now.Add(30*24*time.Hour)) || !strings.HasPrefix(c.Token, "oi_"+c.TokenID+"_") {
		t.Fatalf("%+v %v", c, err)
	}
	c2, _ := e.h.CreateToken(bg, e.admin, e.v(), protocol.TokenCreate{Subject: "ops", Scope: protocol.ScopeAdmin, TTLSeconds: 60})
	if !c2.ExpiresAt.Equal(now.Add(time.Minute)) || c2.Scope != protocol.ScopeAdmin {
		t.Fatalf("%+v", c2)
	}
	list := e.h.ListTokens()
	if len(list) != 2 || list[0].TokenID > list[1].TokenID {
		t.Fatalf("%+v", list)
	}
	if _, err := e.h.RevokeToken(bg, e.admin, e.v(), "ffffffffffffffff"); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	e.ok(e.h.RevokeToken(bg, e.admin, e.v(), c.TokenID))
	first := e.h.tokens[c.TokenID].RevokedAt
	e.clk.Add(time.Second)
	e.ok(e.h.RevokeToken(bg, e.admin, e.v(), c.TokenID))
	if got := e.h.tokens[c.TokenID].RevokedAt; got == nil || !got.Equal(*first) {
		t.Fatal("revocation time changed")
	}
	for _, v := range e.h.ListTokens() {
		if v.TokenID == c.TokenID && v.RevokedAt == nil {
			t.Fatal("revocation not listed")
		}
	}
}

func TestPolicyAdmin(t *testing.T) {
	e := newEnv(t)
	bad := []protocol.PolicySpec{
		{PolicyID: "Bad", Subject: "a", ExposureSelector: "lab-a/*", Actions: []string{"open"}},
		{PolicyID: "p", ExposureSelector: "lab-a/*", Actions: []string{"open"}},
		{PolicyID: "p", Subject: "a", Actions: []string{"open"}},
		{PolicyID: "p", Subject: "a", ExposureSelector: "lab-a/*"},
		{PolicyID: "p", Subject: " a", ExposureSelector: "lab-a/*", Actions: []string{"open"}},
		{PolicyID: "p", Subject: "a", ExposureSelector: "lab-a", Actions: []string{"open"}},
		{PolicyID: "p", Subject: "a", ExposureSelector: "lab-a/*", Actions: []string{"exec"}},
	}
	for i, spec := range bad {
		if _, err := e.h.CreatePolicy(bg, e.admin, e.v(), spec); codeOf(err) != protocol.CodeInvalidRequest {
			t.Errorf("case %d: %v", i, err)
		}
	}
	e.addPolicy("p", "alice", "lab-a/*", protocol.ActionDiscover)
	if _, err := e.h.CreatePolicy(bg, e.admin, e.v(), protocol.PolicySpec{PolicyID: "p", Subject: "a", ExposureSelector: "lab-a/*", Actions: []string{"open"}}); codeOf(err) != protocol.CodeInvalidRequest {
		t.Fatal(err)
	}
	p, err := e.h.GetPolicy("p")
	if err != nil || !p.Enabled || p.UpdatedVersion != e.v() || p.Subject != "alice" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := e.h.GetPolicy("nope"); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	for _, spec := range []protocol.PolicySpec{
		{PolicyID: "q"}, {Subject: "@"}, {ExposureSelector: "x"}, {Actions: []string{}},
	} {
		if _, err := e.h.PatchPolicy(bg, e.admin, e.v(), "p", spec); codeOf(err) != protocol.CodeInvalidRequest {
			t.Errorf("%+v: %v", spec, err)
		}
	}
	if _, err := e.h.PatchPolicy(bg, e.admin, e.v(), "nope", protocol.PolicySpec{}); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	e.ok(e.h.PatchPolicy(bg, e.admin, e.v(), "p", protocol.PolicySpec{PolicyID: "p", Subject: "bob", ExposureSelector: "lab-b/http",
		Actions: []string{protocol.ActionOpen, protocol.ActionDiscover}, Enabled: ptr(false)}))
	p, _ = e.h.GetPolicy("p")
	if p.Subject != "bob" || p.ExposureSelector != "lab-b/http" || len(p.Actions) != 2 || p.Enabled {
		t.Fatalf("%+v", p)
	}
	if l := e.h.ListPolicies(); len(l) != 1 || l[0].PolicyID != "p" {
		t.Fatalf("%+v", l)
	}
	if _, err := e.h.DeletePolicy(bg, e.admin, e.v(), "nope"); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	e.ok(e.h.DeletePolicy(bg, e.admin, e.v(), "p"))
	if len(e.h.ListPolicies()) != 0 {
		t.Fatal("not deleted")
	}
}

func TestWriteErrors(t *testing.T) {
	e := newEnv(t)
	spec := protocol.AccessSpec{AccessID: "lab-a"}
	_, err := e.h.CreateAccess(bg, e.admin, 5, spec)
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.CodeVersionConflict || pe.ConfigVersion != 0 {
		t.Fatalf("%v", err)
	}
	for _, c := range []struct {
		err  error
		code protocol.Code
	}{
		{fmt.Errorf("%w: queue full", store.ErrBusy), protocol.CodeStoreBusy},
		{fmt.Errorf("%w: constraint", store.ErrInvalid), protocol.CodeInvalidRequest},
	} {
		e.st.set(func(f *fakeStore) { f.applyErr = c.err })
		if _, err := e.h.CreateAccess(bg, e.admin, e.v(), spec); codeOf(err) != c.code {
			t.Errorf("%v: %v", c.err, err)
		}
	}
	e.st.set(func(f *fakeStore) { f.applyErr = errUnavailable })
	if _, err := e.h.CreateAccess(bg, e.admin, e.v(), spec); codeOf(err) != protocol.CodeStoreUnavailable {
		t.Fatal(err)
	}
	if e.h.ConfigView().StoreHealthy {
		t.Fatal("store still healthy")
	}
	if _, err := e.h.CreateAccess(bg, e.admin, e.v(), spec); codeOf(err) != protocol.CodeStoreUnavailable {
		t.Fatal(err)
	}
	// Recovery: the probe marks the store healthy and reloads the snapshot.
	e.st.set(func(f *fakeStore) { f.applyErr = nil })
	e.h.probe()
	e.addAccess("lab-a")

	// A write whose commit outcome is unknown is reloaded before the next.
	e.st.set(func(f *fakeStore) { f.commitErr = errBoom })
	if _, err := e.h.CreateAccess(bg, e.admin, e.v(), protocol.AccessSpec{AccessID: "lab-b"}); codeOf(err) != protocol.CodeStoreUnavailable {
		t.Fatal(err)
	}
	e.st.set(func(f *fakeStore) { f.commitErr = nil })
	if _, err := e.h.CreateAccess(bg, e.admin, 1, protocol.AccessSpec{AccessID: "lab-c"}); codeOf(err) != protocol.CodeVersionConflict {
		t.Fatal(err)
	}
	if _, err := e.h.GetAccess("lab-b"); err != nil {
		t.Fatalf("committed write not reloaded: %v", err)
	}
	// The store moved ahead behind the hub's back: Conflict triggers a reload.
	e.st.set(func(f *fakeStore) {
		f.version += 3
		f.accesses["lab-z"] = store.Access{AccessID: "lab-z", Enabled: true, SPKIPins: []string{}}
	})
	stale := e.v()
	_, err = e.h.CreateAccess(bg, e.admin, stale, protocol.AccessSpec{AccessID: "lab-d"})
	if !errors.As(err, &pe) || pe.Code != protocol.CodeVersionConflict || pe.ConfigVersion != stale+3 || e.v() != stale+3 {
		t.Fatalf("%v (hub version %d)", err, e.v())
	}
	if _, err := e.h.GetAccess("lab-z"); err != nil {
		t.Fatal(err)
	}
	// A failed reload refuses the write.
	e.h.mu.Lock()
	e.h.stale = true
	e.h.mu.Unlock()
	e.st.set(func(f *fakeStore) { f.loadErr = errUnavailable })
	if _, err := e.h.CreateAccess(bg, e.admin, e.v(), protocol.AccessSpec{AccessID: "lab-e"}); codeOf(err) != protocol.CodeStoreUnavailable {
		t.Fatal(err)
	}
}

func TestResyncEnforces(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	f.addAccess("lab-b", pin(2))
	ob := f.connect("lab-b", pin(2), expo("lab-b/http", rv(2)))
	now := f.clk.Now()
	f.st.set(func(s *fakeStore) {
		tok := s.tokens[f.alice.TokenID]
		tok.RevokedAt = &now
		s.tokens[f.alice.TokenID] = tok
		delete(s.accesses, "lab-b")
		s.version++
	})
	f.h.mu.Lock()
	f.h.stale = true
	f.h.mu.Unlock()
	f.h.adminMu.Lock()
	err := f.h.resyncIfStale(bg)
	f.h.adminMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	fl.cons.expectAbort(protocol.CodeRevoked)
	if ce := expect[*protocol.ControlError](ob); ce.Code != protocol.CodeRevoked {
		t.Fatalf("%+v", ce)
	}
	f.h.mu.Lock()
	_, kept := f.h.exposures["lab-b/http"]
	f.h.mu.Unlock()
	if kept {
		t.Fatal("registry of a removed access kept")
	}
}

func TestAudit(t *testing.T) {
	e := newEnv(t)
	e.addAccess("lab-a")
	e.addPolicy("p", "alice", "lab-a/*", protocol.ActionOpen)
	l, err := e.h.Audit(bg, 0, 0)
	if err != nil || len(l.Items) != 2 || l.Items[0].Action != "access_created" || l.Items[0].Actor != "admin:root" ||
		l.Items[1].ConfigVersion != 2 || !strings.Contains(string(l.Items[1].Detail), `"subject":"alice"`) {
		t.Fatalf("%+v %v", l, err)
	}
	if l, _ := e.h.Audit(bg, 1, 5); len(l.Items) != 1 || l.Items[0].Seq != 2 {
		t.Fatalf("%+v", l)
	}
	for _, c := range [][2]int64{{-1, 1}, {0, -1}, {0, 101}} {
		if _, err := e.h.Audit(bg, c[0], int(c[1])); codeOf(err) != protocol.CodeInvalidRequest {
			t.Errorf("%v: %v", c, err)
		}
	}
	e.st.set(func(f *fakeStore) { f.listErr = errUnavailable })
	if _, err := e.h.Audit(bg, 0, 1); codeOf(err) != protocol.CodeStoreUnavailable {
		t.Fatal(err)
	}
	if e.h.ConfigView().StoreHealthy {
		t.Fatal("still healthy")
	}
}

func TestStoreFailedIgnoresOtherErrors(t *testing.T) {
	e := newEnv(t)
	e.h.storeFailed(errBoom)
	if !e.h.ConfigView().StoreHealthy {
		t.Fatal("non-Unavailable error marked the store down")
	}
}
