package relay

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestSessCauseString(t *testing.T) {
	if causeLost.String() != "lost" || causeShutdown.String() != "shutdown" {
		t.Fatal(causeLost.String(), causeShutdown.String())
	}
}

func TestControlHelloRejected(t *testing.T) {
	e := newEnv(t)
	e.addAccess("lab-a", pin(1))
	e.addAccess("lab-off", pin(2))
	e.ok(e.h.PatchAccess(context.Background(), e.admin, e.v(), "lab-off", protocol.AccessSpec{Enabled: ptr(false)}))

	o := e.dial("lab-a", pin(1))
	_ = o.conn.CloseNow()
	o.waitDone()

	reject := func(name string, o *oic, code protocol.Code) {
		t.Helper()
		m := expect[*protocol.ControlError](o)
		if m.Code != code {
			t.Errorf("%s: %+v", name, m)
		}
		o.conn.waitClosed(t)
		o.waitDone()
	}
	o = e.dial("lab-a", pin(1))
	o.conn.push(false, []byte(`{"type":"hello","seq":1}`))
	reject("binary", o, protocol.CodeProtocolError)
	o = e.dial("lab-a", pin(1))
	o.send(&protocol.Register{})
	reject("register first", o, protocol.CodeProtocolError)
	o = e.dial("lab-a", pin(1))
	o.conn.push(true, []byte("{"))
	reject("malformed", o, protocol.CodeProtocolError)
	for name, args := range map[string][2]string{
		"unknown":  {"lab-z", pin(1)},
		"disabled": {"lab-off", pin(2)},
		"unpinned": {"lab-a", pin(9)},
	} {
		o = e.dial(args[0], args[1])
		o.send(&protocol.Hello{})
		reject(name, o, protocol.CodeRevoked)
	}
	e.h.mu.Lock()
	e.h.shutting = true
	e.h.mu.Unlock()
	o = e.dial("lab-a", pin(1))
	o.send(&protocol.Hello{})
	reject("shutdown", o, protocol.CodeShutdown)
}

func TestWelcome(t *testing.T) {
	e := newEnv(t)
	e.addAccess("lab-a", pin(1))
	e.ok(e.h.PatchAccess(context.Background(), e.admin, e.v(), "lab-a", protocol.AccessSpec{MaxExposures: ptr(3)}))
	o := e.connect("lab-a", pin(1))
	w := o.welcome
	if w.Epoch != 1 || w.Generation != 1 || w.RunID != "run_test" || w.HeartbeatIntervalMS != 1000 || w.LicenseTTLMS != 3000 ||
		w.Limits.DataMax != 32<<10 || w.Limits.CloseGraceMS != 1000 || w.Limits.DescMax != 2<<10 || w.Limits.MaxExposures != 3 {
		t.Fatalf("%+v", w)
	}
	if v, _ := e.h.GetAccess("lab-a"); !v.Online || v.Generation != 1 {
		t.Fatalf("%+v", v)
	}
}

// TV-B3-10: an OIC that saw a later epoch moves the epoch forward.
func TestEpochRollback(t *testing.T) {
	e := newEnv(t)
	e.addAccess("lab-a", pin(1))
	e.addAccess("lab-b", pin(2))
	o := e.dial("lab-a", pin(1))
	o.send(&protocol.Hello{LastEpochSeen: 5})
	if w := expect[*protocol.Welcome](o); w.Epoch != 6 {
		t.Fatalf("%+v", w)
	}
	e.st.set(func(f *fakeStore) { f.raiseErr = errUnavailable })
	if e.h.persist() {
		t.Fatal("persist must report the failed raise")
	}
	e.st.set(func(f *fakeStore) { f.raiseErr = nil })
	e.h.mu.Lock()
	e.h.storeOK = true
	e.h.mu.Unlock()
	if !e.h.persist() {
		t.Fatal("persist failed")
	}
	o2 := e.dial("lab-b", pin(2))
	o2.send(&protocol.Hello{LastEpochSeen: 9})
	if w := expect[*protocol.Welcome](o2); w.Epoch != 10 {
		t.Fatalf("%+v", w)
	}
	if !e.h.persist() {
		t.Fatal("persist failed")
	}
	if got := e.st.auditActions(); slices.Index(got, "epoch_rollback_detected") < 0 || strings.Count(strings.Join(got, ","), "epoch_rollback") != 1 {
		t.Fatalf("audits %v", got)
	}
	e.st.mu.Lock()
	epoch := e.st.epoch
	e.st.mu.Unlock()
	if epoch != 10 || e.h.ConfigView().Epoch != 10 {
		t.Fatal(epoch, e.h.ConfigView().Epoch)
	}
	e.h.mu.Lock()
	dirty := e.h.epochDirty
	e.h.mu.Unlock()
	if dirty {
		t.Fatal("epoch still dirty")
	}
	// One Access cannot raise the epoch to or past protocol.MaxEpoch; up to
	// just below it the raise works, and OICs then reconnect with the epoch
	// OIS issued even though it is at the bound.
	o3 := e.dial("lab-a", pin(1))
	o3.send(&protocol.Hello{LastEpochSeen: protocol.MaxEpoch})
	if ce := expect[*protocol.ControlError](o3); ce.Code != protocol.CodeProtocolError {
		t.Fatalf("%+v", ce)
	}
	o4 := e.dial("lab-a", pin(1))
	o4.send(&protocol.Hello{LastEpochSeen: protocol.MaxEpoch - 1})
	if w := expect[*protocol.Welcome](o4); w.Epoch != protocol.MaxEpoch {
		t.Fatalf("%+v", w)
	}
	o5 := e.dial("lab-a", pin(1))
	o5.send(&protocol.Hello{LastEpochSeen: protocol.MaxEpoch})
	if w := expect[*protocol.Welcome](o5); w.Epoch != protocol.MaxEpoch {
		t.Fatalf("%+v", w)
	}
}

// TV-B1-13: a new generation aborts the old generation's channels only.
func TestSupersede(t *testing.T) {
	f := newFixture(t)
	x := f.openFlow()
	old := f.o
	f.h.mu.Lock()
	oldSess := f.h.accesses["lab-a"].sess
	f.h.mu.Unlock()
	f.o = f.connect("lab-a", pin(1), expo(exp1, f.rev))
	if f.o.welcome.Generation != 2 {
		t.Fatalf("%+v", f.o.welcome)
	}
	for {
		m := old.recv()
		if ce, ok := m.(*protocol.ControlError); ok {
			if ce.Code != protocol.CodeSuperseded {
				t.Fatalf("%+v", ce)
			}
			break
		}
	}
	old.conn.waitClosed(t)
	old.waitDone()
	x.cons.expectAbort(protocol.CodeChannelInterrupted)
	x.prov.expectAbort(protocol.CodeChannelInterrupted)
	y := f.openFlow()
	// A late prepare_failed from the old generation must not touch Y.
	f.h.mu.Lock()
	f.h.onPrepareFailed(oldSess, &protocol.PrepareFailed{ChannelID: f.id(y), Code: protocol.CodeTargetConnectFailed}, f.clk.Now())
	f.h.mu.Unlock()
	y.cons.data("still")
	if d := y.prov.expectType(protocol.FrameData); string(d.Data) != "still" {
		t.Fatal(string(d.Data))
	}
	if v := f.state(f.id(y)); v.State != protocol.StateOpen {
		t.Fatalf("%+v", v)
	}
}

func TestRegisterValidation(t *testing.T) {
	e := newEnv(t)
	e.addAccess("lab-a", pin(1))
	e.ok(e.h.PatchAccess(context.Background(), e.admin, e.v(), "lab-a", protocol.AccessSpec{MaxExposures: ptr(3)}))
	o := e.connect("lab-a", pin(1))
	bad := func(mut func(*protocol.RegisterExposure)) protocol.RegisterExposure {
		x := expo("lab-a/bad", rv(1))
		mut(&x)
		return x
	}
	o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{
		expo("lab-a/a", rv(1)),
		expo("lab-a/a", rv(2)),
		expo("lab-b/x", rv(1)),
		expo("nope", rv(1)),
		bad(func(x *protocol.RegisterExposure) { x.Revision = "rv_1" }),
		bad(func(x *protocol.RegisterExposure) { x.Protocol = "HTTP" }),
		bad(func(x *protocol.RegisterExposure) { x.TransportProfile = "udp" }),
		bad(func(x *protocol.RegisterExposure) { x.AdmissionMode = "maybe" }),
		bad(func(x *protocol.RegisterExposure) { x.Description = strings.Repeat("d", 2049) }),
		expo("lab-a/b", rv(1)),
		expo("lab-a/c", rv(1)),
		expo("lab-a/d", rv(1)),
	}})
	ack := expect[*protocol.RegisterAck](o)
	if strings.Join(ack.Accepted, ",") != "lab-a/a,lab-a/b,lab-a/c" {
		t.Fatalf("accepted %v", ack.Accepted)
	}
	var codes []string
	for _, r := range ack.Rejected {
		codes = append(codes, r.ExposureID+"="+string(r.Code))
	}
	want := "lab-a/a=InvalidRequest,lab-b/x=Denied,nope=InvalidRequest," + strings.Repeat("lab-a/bad=InvalidRequest,", 4) +
		"lab-a/bad=PayloadTooLarge,lab-a/d=ResourceExhausted"
	if strings.Join(codes, ",") != want {
		t.Fatalf("rejected %v", codes)
	}
	e.addPolicy("p", "alice", "lab-a/*", protocol.ActionDiscover)
	alice, _ := e.token("alice", protocol.ScopeConsumer, 0)
	if l, _ := e.h.Discover(alice, 0, ""); len(l.Items) != 3 {
		t.Fatalf("%+v", l)
	}
}

func TestRegisterDiffAndPersistence(t *testing.T) {
	f := newFixture(t)
	f.addPolicy("pb", "alice", "lab-a/b", protocol.ActionDiscover, protocol.ActionOpen)
	register := func(exps ...protocol.RegisterExposure) {
		t.Helper()
		f.o.send(&protocol.Register{Exposures: exps})
		for {
			if _, ok := f.o.recv().(*protocol.RegisterAck); ok {
				return
			}
		}
	}
	if !f.h.persist() {
		t.Fatal("persist")
	}
	upserts := func() int {
		f.st.mu.Lock()
		defer f.st.mu.Unlock()
		return f.st.upserts
	}
	if upserts() != 1 {
		t.Fatalf("initial upserts %d", upserts())
	}
	register(expo(exp1, f.rev))
	f.h.persist()
	if upserts() != 1 {
		t.Fatal("unchanged snapshot written again")
	}
	described := expo(exp1, f.rev)
	described.Description = "new words"
	register(described, expo("lab-a/b", rv(2)))
	fl := f.openFlow()
	r, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: "lab-a/b", ExpectedRevision: rv(2)})
	if err != nil {
		t.Fatal(err)
	}
	f.o.prepareMsg()
	register(described, expo("lab-a/b", rv(3)))
	if v, _ := f.h.Channel(f.alice, r.Response.ChannelID); v.CloseReason != protocol.CodeRevisionChanged {
		t.Fatalf("%+v", v)
	}
	if f.state(f.id(fl)).State != protocol.StateOpen {
		t.Fatal("a description change must not close channels")
	}
	register(expo("lab-a/b", rv(3)))
	fl.cons.expectAbort(protocol.CodeRevisionChanged)
	if !f.h.persist() {
		t.Fatal("persist")
	}
	f.st.mu.Lock()
	stored := f.st.exposures["lab-a"]
	f.st.mu.Unlock()
	if len(stored) != 1 || stored[0].ExposureID != "lab-a/b" || stored[0].Revision != rv(3) {
		t.Fatalf("%+v", stored)
	}
	got := strings.Join(f.st.auditActions(), ",")
	for _, a := range []string{"exposure_added", "exposure_revision_changed", "exposure_removed"} {
		if !strings.Contains(got, a) {
			t.Fatalf("missing %s in %s", a, got)
		}
	}
}

func TestPrepareFailed(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	cons := f.attach(f.reserve(fl, Consumer))
	f.o.send(&protocol.PrepareFailed{ChannelID: f.id(fl), Code: protocol.CodeTargetConnectFailed})
	cons.expectAbort(protocol.CodeTargetConnectFailed)
	f.waitState(f.id(fl), protocol.StateClosed, protocol.CodeTargetConnectFailed)
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev}); err != nil {
		t.Fatal(err)
	}
	p2 := f.o.prepareMsg()
	f.o.send(&protocol.PrepareFailed{ChannelID: p2.ChannelID, Code: "Bogus"})
	f.waitState(p2.ChannelID, protocol.StateClosed, protocol.CodeProtocolError)
	// Unknown, foreign and already closed channels are ignored.
	f.o.send(&protocol.PrepareFailed{ChannelID: "ch_" + strings.Repeat("b", 26), Code: protocol.CodeLocalDenied})
	f.o.send(&protocol.PrepareFailed{ChannelID: f.id(fl), Code: protocol.CodeLocalDenied})
	f.o.send(&protocol.HeartbeatAck{AckSeq: 99})
	// Messages are handled in order: the ack of a later register proves it.
	f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{expo(exp1, f.rev)}})
	for {
		if _, ok := f.o.recv().(*protocol.RegisterAck); ok {
			break
		}
	}
	if v := f.state(f.id(fl)); v.CloseReason != protocol.CodeTargetConnectFailed {
		t.Fatalf("%+v", v)
	}
}

func heartbeat(t *testing.T, o *oic) *protocol.Heartbeat {
	t.Helper()
	for {
		if hb, ok := o.recv().(*protocol.Heartbeat); ok {
			return hb
		}
	}
}

func TestHeartbeatOffline(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	prep := f.prepare()
	f.h.heartbeatTick()
	hb := heartbeat(t, f.o)
	if hb.LicenseTTLMS != 3000 {
		t.Fatalf("%+v", hb)
	}
	f.o.send(&protocol.HeartbeatAck{AckSeq: hb.Seq})
	waitFor(t, "ack", func() bool {
		f.h.mu.Lock()
		defer f.h.mu.Unlock()
		return f.h.accesses["lab-a"].sess.hbSeq == 0
	})
	f.h.heartbeatTick()
	heartbeat(t, f.o)
	f.h.heartbeatTick()
	heartbeat(t, f.o)
	f.h.heartbeatTick()
	if f.state(f.id(fl)).State != protocol.StateOpen {
		t.Fatal("offline after two misses")
	}
	f.h.heartbeatTick()
	fl.cons.expectAbort(protocol.CodeAccessOffline)
	fl.prov.expectAbort(protocol.CodeAccessOffline)
	if v := f.state(f.id(prep)); v.CloseReason != protocol.CodeAccessOffline {
		t.Fatalf("%+v", v)
	}
	f.o.conn.waitClosed(t)
	f.o.waitDone()
	f.h.heartbeatTick()
	if v, _ := f.h.GetAccess("lab-a"); v.Online {
		t.Fatal("still online")
	}
}

// TV-B2-10 shape: losing only the control connection keeps Open channels
// until the Access is judged offline; no new channels are prepared.
func TestLostControlConnection(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	prep := f.prepare()
	_ = f.o.conn.CloseNow()
	f.o.waitDone()
	if v := f.state(f.id(prep)); v.CloseReason != protocol.CodeAccessOffline {
		t.Fatalf("%+v", v)
	}
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev}); codeOf(err) != protocol.CodeAccessOffline {
		t.Fatalf("open: %v", err)
	}
	if l, _ := f.h.Discover(f.alice, 0, ""); l.Items[0].AccessOnline {
		t.Fatal("still online")
	}
	f.h.heartbeatTick()
	f.h.heartbeatTick()
	if f.state(f.id(fl)).State != protocol.StateOpen {
		t.Fatal("closed before OFFLINE_MISSES")
	}
	f.h.heartbeatTick()
	fl.cons.expectAbort(protocol.CodeAccessOffline)
}

func TestReconnectAbortsLostGeneration(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	_ = f.o.conn.CloseNow()
	f.o.waitDone()
	f.o = f.connect("lab-a", pin(1), expo(exp1, f.rev))
	fl.cons.expectAbort(protocol.CodeChannelInterrupted)
	f.openFlow()
}

func TestControlProtocolErrors(t *testing.T) {
	for name, send := range map[string]func(o *oic){
		"binary":    func(o *oic) { o.conn.push(false, []byte(`{}`)) },
		"malformed": func(o *oic) { o.conn.push(true, []byte(`[`)) },
		"unexpected": func(o *oic) {
			o.send(&protocol.Welcome{Epoch: 1, Generation: 1, HeartbeatIntervalMS: 1, LicenseTTLMS: 1, Limits: protocol.WelcomeLimits{DataMax: 1}})
		},
		"hello again": func(o *oic) { o.send(&protocol.Hello{}) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			send(f.o)
			for {
				if ce, ok := f.o.recv().(*protocol.ControlError); ok {
					if ce.Code != protocol.CodeProtocolError {
						t.Fatalf("%+v", ce)
					}
					break
				}
			}
			f.o.waitDone()
		})
	}
	f := newFixture(t)
	f.o.send(&protocol.ControlError{Code: protocol.CodeShutdown})
	f.o.conn.waitClosed(t)
	f.o.waitDone()
}

func TestControlBackPressureEndsSession(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	block := make(chan struct{})
	defer close(block)
	f.o.conn.setGate(block)
	f.h.mu.Lock()
	s := f.h.accesses["lab-a"].sess
	for len(s.out) < cap(s.out) {
		s.out <- ctrlOut{data: []byte(`{}`)}
	}
	s.send(&protocol.Heartbeat{LicenseTTLMS: 1})
	ended := s.ended
	f.h.mu.Unlock()
	if !ended {
		t.Fatal("session survived a full queue")
	}
	if v := f.state(f.id(fl)); v.CloseReason != protocol.CodeAccessOffline {
		t.Fatalf("%+v", v)
	}
	f.o.conn.waitClosed(t)
}

func TestControlWriteFailure(t *testing.T) {
	f := newFixture(t)
	err := errors.New("reset")
	f.o.conn.writeErr.Store(&err)
	f.h.heartbeatTick()
	f.o.conn.waitClosed(t)
	f.o.waitDone()
	if v, _ := f.h.GetAccess("lab-a"); v.Online {
		t.Fatal("online after write failure")
	}
}

func TestHelloTimeout(t *testing.T) {
	f := newEnv(t)
	f.h.lim.PrepareMS = 20
	o := f.dial("lab-a", pin(1))
	o.conn.waitClosed(t)
	o.waitDone()
}
