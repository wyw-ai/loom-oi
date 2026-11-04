package relay

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestHappyPathCompletes(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	id := f.id(fl)
	if v := f.state(id); v.State != protocol.StateOpen {
		t.Fatalf("%+v", v)
	}
	fl.cons.data("GET / HTTP/1.0\r\n\r\n")
	if got := fl.prov.expectType(protocol.FrameData); !strings.HasPrefix(string(got.Data), "GET /") {
		t.Fatal(string(got.Data))
	}
	fl.cons.fin()
	fl.prov.expectType(protocol.FrameFin)
	fl.prov.data("HTTP/1.0 200 OK\r\n\r\nT1")
	if got := fl.cons.expectType(protocol.FrameData); string(got.Data) != "HTTP/1.0 200 OK\r\n\r\nT1" {
		t.Fatal(string(got.Data))
	}
	fl.prov.fin()
	fl.cons.expectType(protocol.FrameFin)
	f.waitState(id, protocol.StateClosed, protocol.CodeCompleted)
	fl.cons.conn.waitClosed(t)
	fl.prov.conn.waitClosed(t)
	if !fl.cons.conn.graceful.Load() || !fl.prov.conn.graceful.Load() {
		t.Fatal("completed legs must close gracefully")
	}
	fl.cons.waitDone()
	fl.prov.waitDone()
	s := f.h.Stats()
	if s.Channels.Open+s.Channels.Preparing+s.Channels.Closing != 0 || s.Tickets != 0 || f.h.inflight.Load() != 0 {
		t.Fatalf("%+v inflight %d", s, f.h.inflight.Load())
	}
	f.h.mu.Lock()
	q := f.h.quota.total
	f.h.mu.Unlock()
	if q != 0 {
		t.Fatalf("quota leak %d", q)
	}
}

func TestFrameViolations(t *testing.T) {
	type step func(f *fixture, fl *flow)
	cases := []struct {
		name   string
		act    step
		reason protocol.Code
	}{
		{"FIN after FIN", func(f *fixture, fl *flow) {
			fl.cons.fin()
			fl.prov.expectType(protocol.FrameFin)
			fl.cons.fin()
		}, protocol.CodeProtocolError},
		{"DATA after FIN", func(f *fixture, fl *flow) {
			fl.cons.fin()
			fl.prov.expectType(protocol.FrameFin)
			fl.cons.data("late")
		}, protocol.CodeProtocolError},
		{"leg sends READY", func(f *fixture, fl *flow) { fl.cons.raw(false, protocol.ReadyFrame()) }, protocol.CodeProtocolError},
		{"text message", func(f *fixture, fl *flow) { fl.cons.raw(true, []byte("hi")) }, protocol.CodeProtocolError},
		{"unknown type", func(f *fixture, fl *flow) { fl.cons.raw(false, []byte{9}) }, protocol.CodeProtocolError},
		{"oversize DATA", func(f *fixture, fl *flow) {
			fl.cons.raw(false, protocol.DataFrame(make([]byte, f.h.lim.DataMax+1)))
		}, protocol.CodePayloadTooLarge},
		{"read limit", func(f *fixture, fl *flow) { fl.cons.conn.in <- wsMsg{err: ErrTooBig} }, protocol.CodePayloadTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			fl := f.openFlow()
			c.act(f, fl)
			fl.prov.expectAbort(c.reason)
			if c.name != "read limit" {
				fl.cons.expectAbort(c.reason)
			}
			f.waitState(f.id(fl), protocol.StateClosed, c.reason)
			cc := expect[*protocol.CloseChannel](f.o)
			if cc.Code != c.reason {
				t.Fatalf("%+v", cc)
			}
		})
	}
}

// TV-B1-05: DATA before READY aborts; the provider never gets READY.
func TestDataOrFinBeforeReady(t *testing.T) {
	for _, send := range []func(l *legEnd){func(l *legEnd) { l.data("early") }, func(l *legEnd) { l.fin() }} {
		f := newFixture(t)
		fl := f.prepare()
		cons := f.attach(f.reserve(fl, Consumer))
		send(cons)
		cons.expectAbort(protocol.CodeProtocolError)
		f.waitState(f.id(fl), protocol.StateClosed, protocol.CodeProtocolError)
		if _, err := f.h.ReserveAttach(AttachRequest{ChannelID: f.id(fl), Ticket: fl.prep.ProviderTicket, Role: Provider, AccessID: "lab-a"}); codeOf(err) != protocol.CodeChannelGone {
			t.Fatalf("late provider: %v", err)
		}
	}
}

func TestAbortForwarding(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	fl.prov.abort(protocol.CodeChannelInterrupted)
	fl.cons.expectAbort(protocol.CodeChannelInterrupted)
	fl.prov.conn.waitClosed(t)
	select {
	case m := <-fl.prov.conn.out:
		t.Fatalf("the aborting leg got %v", m.data)
	default:
	}
	f.waitState(f.id(fl), protocol.StateClosed, protocol.CodeChannelInterrupted)

	fl = f.openFlow()
	fl.cons.raw(false, []byte{protocol.FrameAbort, 0x7f, 0x7f})
	fl.prov.expectAbort(protocol.CodeProtocolError)
	f.waitState(f.id(fl), protocol.StateClosed, protocol.CodeProtocolError)
}

func TestLegDisconnect(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	_ = fl.cons.conn.CloseNow()
	fl.prov.expectAbort(protocol.CodeChannelInterrupted)
	f.waitState(f.id(fl), protocol.StateClosed, protocol.CodeChannelInterrupted)
	fl.cons.waitDone()

	p := f.prepare()
	prov := f.attach(f.reserve(p, Provider))
	_ = prov.conn.CloseNow()
	f.waitState(f.id(p), protocol.StateClosed, protocol.CodeChannelInterrupted)
	prov.waitDone()
}

func TestClosingForceClose(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	id := f.id(fl)
	block := make(chan struct{})
	defer close(block)
	// The consumer never accepts the ABORT.
	fl.cons.conn.setGate(block)
	f.h.CloseChannel(f.alice, id)
	fl.prov.expectAbort(protocol.CodeClosedByClient)
	f.waitState(id, protocol.StateClosing, protocol.CodeClosedByClient)
	f.clk.Add(999 * time.Millisecond)
	f.h.sweep(f.clk.Now())
	if f.state(id).State != protocol.StateClosing {
		t.Fatal("closed before T_close")
	}
	f.clk.Add(time.Millisecond)
	f.h.sweep(f.clk.Now())
	if v := f.state(id); v.State != protocol.StateClosed || v.CloseReason != protocol.CodeClosedByClient {
		t.Fatalf("%+v", v)
	}
	fl.cons.conn.waitClosed(t)
	fl.cons.waitDone()
}

func TestReserveAttach(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	id := f.id(fl)
	bob := Principal{Subject: "bob", TokenID: f.alice.TokenID}
	otherToken := Principal{Subject: "alice", TokenID: "ffffffffffffffff"}
	cases := []struct {
		name string
		req  AttachRequest
		code protocol.Code
	}{
		{"no prefix", AttachRequest{ChannelID: id, Ticket: "x", Role: Consumer, Principal: f.alice}, protocol.CodeTicketInvalid},
		{"too long", AttachRequest{ChannelID: id, Ticket: "tk_" + strings.Repeat("a", 200), Role: Consumer, Principal: f.alice}, protocol.CodeTicketInvalid},
		{"unknown channel", AttachRequest{ChannelID: "ch_" + strings.Repeat("a", 26), Ticket: fl.open.Ticket, Role: Consumer, Principal: f.alice}, protocol.CodeTicketInvalid},
		{"other channel", AttachRequest{ChannelID: f.id(f.prepare()), Ticket: fl.open.Ticket, Role: Consumer, Principal: f.alice}, protocol.CodeTicketInvalid},
		{"consumer ticket as provider", AttachRequest{ChannelID: id, Ticket: fl.open.Ticket, Role: Provider, AccessID: "lab-a"}, protocol.CodeTicketInvalid},
		{"provider ticket as consumer", AttachRequest{ChannelID: id, Ticket: fl.prep.ProviderTicket, Role: Consumer, Principal: f.alice}, protocol.CodeTicketInvalid},
		{"other subject", AttachRequest{ChannelID: id, Ticket: fl.open.Ticket, Role: Consumer, Principal: bob}, protocol.CodeTicketInvalid},
		{"other token", AttachRequest{ChannelID: id, Ticket: fl.open.Ticket, Role: Consumer, Principal: otherToken}, protocol.CodeTicketInvalid},
		{"other access", AttachRequest{ChannelID: id, Ticket: fl.prep.ProviderTicket, Role: Provider, AccessID: "lab-b"}, protocol.CodeTicketInvalid},
	}
	for _, c := range cases {
		if _, err := f.h.ReserveAttach(c.req); codeOf(err) != c.code {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// TV-B2-05: concurrent use of one ticket succeeds exactly once.
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.h.ReserveAttach(AttachRequest{ChannelID: id, Ticket: fl.open.Ticket, Role: Consumer, Principal: f.alice})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	okCount, consumed := 0, 0
	for err := range results {
		switch codeOf(err) {
		case "":
			okCount++
		case protocol.CodeTicketConsumed:
			consumed++
		}
	}
	if okCount != 1 || consumed != 7 {
		t.Fatalf("ok %d consumed %d", okCount, consumed)
	}
	// Provider whose generation changed (white-box: the session is gone).
	f.h.mu.Lock()
	sess := f.h.accesses["lab-a"].sess
	f.h.accesses["lab-a"].sess = nil
	f.h.mu.Unlock()
	if _, err := f.h.ReserveAttach(AttachRequest{ChannelID: id, Ticket: fl.prep.ProviderTicket, Role: Provider, AccessID: "lab-a"}); codeOf(err) != protocol.CodeTicketInvalid {
		t.Fatalf("stale generation: %v", err)
	}
	f.h.mu.Lock()
	f.h.accesses["lab-a"].sess = sess
	f.h.mu.Unlock()
	// After the prepare deadline the channel is gone.
	f.clk.Add(2 * time.Second)
	if _, err := f.h.ReserveAttach(AttachRequest{ChannelID: id, Ticket: fl.prep.ProviderTicket, Role: Provider, AccessID: "lab-a"}); codeOf(err) != protocol.CodeChannelGone {
		t.Fatalf("expired: %v", err)
	}
	if v := f.state(id); v.CloseReason != protocol.CodePrepareTimeout {
		t.Fatalf("%+v", v)
	}
}

func TestAttachAfterTermination(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	res := f.reserve(fl, Consumer)
	f.h.CloseChannel(f.alice, f.id(fl))
	l := f.attach(res)
	l.expectAbort(protocol.CodeClosedByClient)
	l.waitDone()

	fl = f.prepare()
	res = f.reserve(fl, Provider)
	f.h.CancelAttach(res)
	if v := f.state(f.id(fl)); v.CloseReason != protocol.CodeChannelInterrupted {
		t.Fatalf("%+v", v)
	}
}

func TestCommitCheckFailures(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(f *fixture)
		reason protocol.Code
	}{
		{"deadline", func(f *fixture) { f.clk.Add(2 * time.Second) }, protocol.CodePrepareTimeout},
		{"policy", func(f *fixture) {
			f.h.policies = nil
			f.h.rebuildIndexes()
		}, protocol.CodeRevoked},
		{"token revoked", func(f *fixture) {
			tok := f.h.tokens[f.alice.TokenID]
			now := f.clk.Now()
			tok.RevokedAt = &now
			f.h.tokens[f.alice.TokenID] = tok
		}, protocol.CodeRevoked},
		{"token deleted", func(f *fixture) { delete(f.h.tokens, f.alice.TokenID) }, protocol.CodeRevoked},
		{"token expired", func(f *fixture) {
			tok := f.h.tokens[f.alice.TokenID]
			tok.ExpiresAt = f.clk.Now()
			f.h.tokens[f.alice.TokenID] = tok
		}, protocol.CodeUnauthenticated},
		{"revision", func(f *fixture) { f.h.exposures[exp1].rec.Revision = rv(5) }, protocol.CodeRevisionChanged},
		{"registry generation", func(f *fixture) { f.h.exposures[exp1].gen++ }, protocol.CodeRevisionChanged},
		{"exposure removed", func(f *fixture) { delete(f.h.exposures, exp1) }, protocol.CodeRevisionChanged},
		{"access disabled", func(f *fixture) { f.h.accesses["lab-a"].rec.Enabled = false }, protocol.CodeRevoked},
		{"access deleted", func(f *fixture) { delete(f.h.accesses, "lab-a") }, protocol.CodeRevoked},
		{"not registered", func(f *fixture) { f.h.accesses["lab-a"].sess.registered = false }, protocol.CodeAccessOffline},
		{"generation", func(f *fixture) { f.h.accesses["lab-a"].sess.gen++ }, protocol.CodeAccessOffline},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			fl := f.prepare()
			prov := f.attach(f.reserve(fl, Provider))
			res := f.reserve(fl, Consumer)
			f.h.mu.Lock()
			c.break_(f)
			f.h.mu.Unlock()
			cons := f.attach(res)
			prov.expectAbort(c.reason)
			cons.expectAbort(c.reason)
			f.h.mu.Lock()
			ch := f.h.channels[f.id(fl)]
			state, reason := ch.state, ch.reason
			f.h.mu.Unlock()
			if state != stateClosed || reason != c.reason {
				t.Fatalf("state %d reason %s", state, reason)
			}
		})
	}
}

// TV-B1-11: revocation while one leg is attached never lets READY out.
func TestRevokeDuringPrepare(t *testing.T) {
	for i := range 20 {
		f := newFixture(t)
		fl := f.prepare()
		first, second := Consumer, Provider
		if i%2 == 1 {
			first, second = Provider, Consumer
		}
		l := f.attach(f.reserve(fl, first))
		e := f.env
		go e.h.sweep(e.clk.Now())
		e.ok(e.h.PatchPolicy(context.Background(), e.admin, e.v(), "p1", protocol.PolicySpec{Enabled: ptr(false)}))
		// The admin call has returned: no Preparing channel is left.
		if v := f.state(f.id(fl)); v.State != protocol.StateClosed || v.CloseReason != protocol.CodeRevoked {
			t.Fatalf("%+v", v)
		}
		l.expectAbort(protocol.CodeRevoked)
		req := AttachRequest{ChannelID: f.id(fl), Role: second, Ticket: fl.prep.ProviderTicket, AccessID: "lab-a"}
		if second == Consumer {
			req.Ticket, req.Principal = fl.open.Ticket, f.alice
		}
		if _, err := f.h.ReserveAttach(req); codeOf(err) != protocol.CodeChannelGone {
			t.Fatalf("late leg: %v", err)
		}
	}
}

// TV-B1-12 and TV-B2-12: revoking a policy or a token closes Open channels.
func TestRevokeOpenChannel(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	f.ok(f.h.DeletePolicy(context.Background(), f.admin, f.v(), "p1"))
	fl.prov.expectAbort(protocol.CodeRevoked)
	fl.cons.expectAbort(protocol.CodeRevoked)
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev}); codeOf(err) != protocol.CodeNotFound {
		t.Fatalf("open after delete: %v", err)
	}
	cc := expect[*protocol.CloseChannel](f.o)
	if cc.Code != protocol.CodeRevoked {
		t.Fatalf("%+v", cc)
	}

	f.addPolicy("p1b", "alice", exp1, protocol.ActionDiscover, protocol.ActionOpen)
	fl = f.openFlow()
	f.ok(f.h.RevokeToken(context.Background(), f.admin, f.v(), f.alice.TokenID))
	fl.cons.expectAbort(protocol.CodeRevoked)
	fl.prov.expectAbort(protocol.CodeRevoked)
}

func TestTokenExpiryAndIdle(t *testing.T) {
	f := newFixture(t)
	bob, _ := f.token("bob", protocol.ScopeConsumer, 5)
	f.addPolicy("pb", "bob", "lab-a/*", protocol.ActionDiscover, protocol.ActionOpen)
	f.alice = bob
	fl := f.openFlow()
	prep := f.prepare()
	f.clk.Add(1500 * time.Millisecond)
	fl.cons.data("x")
	fl.prov.expectType(protocol.FrameData)
	f.clk.Add(1500 * time.Millisecond)
	f.h.sweep(f.clk.Now())
	if f.state(f.id(fl)).State != protocol.StateOpen {
		t.Fatal("idle too early")
	}
	if v := f.state(f.id(prep)); v.CloseReason != protocol.CodePrepareTimeout {
		t.Fatalf("prepare timeout: %+v", v)
	}
	fl.cons.data("y")
	fl.prov.expectType(protocol.FrameData)
	f.clk.Add(time.Second)
	prep2 := f.prepare()
	f.clk.Add(1100 * time.Millisecond)
	f.h.sweep(f.clk.Now())
	fl.cons.expectAbort(protocol.CodeUnauthenticated)
	if v := f.state(f.id(prep2)); v.CloseReason != protocol.CodeUnauthenticated {
		t.Fatalf("%+v", v)
	}

	g := newFixture(t)
	fl = g.openFlow()
	g.clk.Add(2 * time.Second)
	g.h.sweep(g.clk.Now())
	fl.prov.expectAbort(protocol.CodeIdleTimeout)
	fl.cons.expectAbort(protocol.CodeIdleTimeout)
}

// A credential revoked after its request was authenticated cannot act when
// the request takes effect (a held-back body, B2 §2), and the sweep closes
// any channel whose token is revoked.
func TestRevokedCredentialCannotAct(t *testing.T) {
	f := newFixture(t)
	ops, _ := f.token("ops", protocol.ScopeAdmin, 0)
	fl := f.openFlow()
	prep := f.prepare()
	// Revoked behind the coordinator's back: only the sweep can notice.
	f.h.mu.Lock()
	tok := f.h.tokens[f.alice.TokenID]
	at := f.clk.Now()
	tok.RevokedAt = &at
	f.h.tokens[f.alice.TokenID] = tok
	f.h.mu.Unlock()
	f.h.sweep(f.clk.Now())
	fl.cons.expectAbort(protocol.CodeRevoked)
	if v := f.state(f.id(prep)); v.CloseReason != protocol.CodeRevoked {
		t.Fatalf("%+v", v)
	}
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev}); codeOf(err) != protocol.CodeUnauthenticated {
		t.Fatal(err)
	}
	f.ok(f.h.RevokeToken(bg, f.admin, f.v(), ops.TokenID))
	if _, err := f.h.CreateAccess(bg, ops, f.v(), protocol.AccessSpec{AccessID: "lab-z"}); codeOf(err) != protocol.CodeUnauthenticated {
		t.Fatal(err)
	}
}

func TestRetainCleanup(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	f.h.CloseChannel(f.alice, f.id(fl))
	f.clk.Add(4999 * time.Millisecond)
	f.h.sweep(f.clk.Now())
	f.state(f.id(fl))
	f.clk.Add(time.Millisecond)
	f.h.sweep(f.clk.Now())
	if _, err := f.h.Channel(f.alice, f.id(fl)); codeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
}

func TestPingDropsSilentLeg(t *testing.T) {
	f := newFixture(t, func(l *limits.Limits) { l.PingMS = 5 })
	fl := f.openFlow()
	time.Sleep(30 * time.Millisecond)
	if fl.cons.conn.isClosed() || fl.cons.conn.pings.Load() == 0 {
		t.Fatal("healthy leg must stay")
	}
	err := errors.New("no pong")
	fl.cons.conn.pingErr.Store(&err)
	fl.prov.expectAbort(protocol.CodeChannelInterrupted)
	fl.cons.waitDone()
}

func TestBackPressureBoundsBuffering(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	gate := make(chan struct{})
	fl.cons.conn.setGate(gate)
	chunk := strings.Repeat("x", f.h.lim.DataMax)
	for range 4 {
		fl.prov.data(chunk)
	}
	waitFor(t, "provider reader blocked", func() bool {
		f.h.mu.Lock()
		l := f.h.channels[f.id(fl)].legs[Provider]
		f.h.mu.Unlock()
		return l.forwarding.Load()
	})
	if got := f.h.inflight.Load(); got != int64(f.h.lim.DataMax) {
		t.Fatalf("in-flight %d", got)
	}
	for range 4 {
		gate <- struct{}{}
		if d := fl.cons.expectType(protocol.FrameData); len(d.Data) != f.h.lim.DataMax {
			t.Fatal(len(d.Data))
		}
	}
	if m := f.h.maxInflight.Load(); m > int64(2*f.h.lim.DataMax) {
		t.Fatalf("max in-flight %d", m)
	}
	waitFor(t, "relay bytes", func() bool { return f.h.relayBytes.Load() == int64(4*f.h.lim.DataMax) })
	close(gate)
}

func TestWriteFailureDropsLeg(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	err := errors.New("broken pipe")
	fl.cons.conn.writeErr.Store(&err)
	fl.prov.data("x")
	fl.prov.expectAbort(protocol.CodeChannelInterrupted)
	fl.cons.waitDone()
	if f.h.inflight.Load() != 0 {
		t.Fatal(f.h.inflight.Load())
	}
}

// DATA queued behind a blocked write is dropped once the channel leaves
// Open, and the consumer's READY is written only after the provider's.
func TestDataDroppedOnceClosing(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	prov := f.attach(f.reserve(fl, Provider))
	gate := make(chan struct{})
	cc := newFakeConn()
	cc.setGate(gate)
	cons := f.attachConn(f.reserve(fl, Consumer), cc)
	prov.expectType(protocol.FrameReady)
	prov.data("one")
	waitFor(t, "DATA queued", func() bool { return f.h.inflight.Load() == 3 })
	f.h.CloseChannel(f.alice, f.id(fl))
	close(gate)
	cons.expectType(protocol.FrameReady)
	cons.expectAbort(protocol.CodeClosedByClient)
	prov.expectAbort(protocol.CodeClosedByClient)
	waitFor(t, "in-flight drained", func() bool { return f.h.inflight.Load() == 0 })
	if f.h.relayBytes.Load() != 0 {
		t.Fatal("dropped DATA counted as relayed")
	}
}

func TestEnqueueOverflowFailsClosed(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	res := f.reserve(fl, Consumer)
	c := newFakeConn()
	f.h.mu.Lock()
	l := &leg{h: f.h, ch: res.ch, conn: c, out: make(chan outMsg, 1), done: make(chan struct{})}
	l.enqueue(outMsg{frame: protocol.FinFrame()})
	done := make(chan struct{})
	f.h.addInflight(1)
	l.enqueue(outMsg{frame: protocol.DataFrame([]byte("x")), done: done, n: 1})
	f.h.mu.Unlock()
	<-done
	if !c.isClosed() || f.h.inflight.Load() != 0 {
		t.Fatal("overflow must close the leg and release the message")
	}
	f.h.mu.Lock()
	l.dead = true
	sig := &readyGate{ch: make(chan struct{})}
	l.enqueue(outMsg{frame: protocol.ReadyFrame(), signal: sig})
	f.h.mu.Unlock()
	<-sig.ch
	if sig.written.Load() {
		t.Fatal("a READY for a dead leg counts as written")
	}
}

func TestShutdownDuringReadyWait(t *testing.T) {
	f := newFixture(t)
	fl := f.prepare()
	gate := make(chan struct{})
	defer close(gate)
	pc := newFakeConn()
	pc.setGate(gate)
	prov := f.attachConn(f.reserve(fl, Provider), pc)
	cons := f.attach(f.reserve(fl, Consumer))
	waitFor(t, "open", func() bool { return f.state(f.id(fl)).State == protocol.StateOpen })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	f.h.Shutdown(ctx)
	prov.waitDone()
	cons.waitDone()
	for {
		m, ok := cons.conn.nextOrClosed(t)
		if !ok {
			break
		}
		if m.data[0] == protocol.FrameReady {
			t.Fatal("consumer READY written although the provider's was not")
		}
	}
}
