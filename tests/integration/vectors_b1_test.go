package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

var bg = context.Background()

// request runs one request/response exchange through a library stream.
func request(t *testing.T, c *client.Client, exposure, req string) (string, string) {
	t.Helper()
	s, err := c.Dial(bg, exposure)
	if err != nil {
		t.Fatalf("dial %s: %v", exposure, err)
	}
	defer s.Close()
	if _, err := s.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("read %s: %v", exposure, err)
	}
	return string(got), s.ChannelID()
}

// waitTarget waits for the target connection opened after before to end.
func waitTarget(t *testing.T, tg *fixture.Target, before int) fixture.Record {
	t.Helper()
	var r fixture.Record
	ok := tg.Wait(10*time.Second, func(rs []fixture.Record) bool {
		if len(rs) <= before || !rs[before].Done {
			return false
		}
		r = rs[before]
		return true
	})
	if !ok {
		t.Fatalf("target %s: connection %d did not end: %+v", tg.Name, before, tg.Conns())
	}
	return r
}

func errBody(res *http.Response) protocol.Code {
	if res == nil || res.Body == nil {
		return ""
	}
	var e protocol.ErrorBody
	_ = json.NewDecoder(io.LimitReader(res.Body, 1024)).Decode(&e)
	return e.Code
}

// TV-B1-01: a consumer reads T1 through E1 with the Go client and the oi
// CLI; both directions FIN and the channel ends Completed.
func TestTVB1_01_ReadOnlyHTTP(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		start := time.Now()
		got, id := request(t, e.consumer("alice"), "lab-a/http", "GET / HTTP/1.0\r\n\r\n")
		if got != "T1:GET / HTTP/1.0\r\n\r\n" {
			t.Fatalf("%q", got)
		}
		if v := e.closedState("alice", id); v.CloseReason != protocol.CodeCompleted {
			t.Fatalf("%+v", v)
		}
		e.obs("O1", map[string]any{"vector": "TV-B1-01", "admission": "best_effort", "readiness": "unknown", "result": "Completed",
			"exchange_ms": time.Since(start).Milliseconds()})
		out, code, errOut := runIn(oiBin, []string{"OI_TOKEN=" + e.token("alice")}, "GET /cli",
			"--ois", "https://"+e.api, "--ca", e.caFile, "connect", "lab-a/http", "--stdio")
		if code != 0 || out != "T1:GET /cli" {
			t.Fatalf("oi: %d %q %s", code, out, errOut)
		}
		e.settled()
	})
}

// TV-B1-02: only oi.tunnel.v2 offered → 400 VersionUnsupported, no change.
func TestTVB1_02_SubprotocolVersion(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		o := e.open("alice", "lab-a/http")
		_, res, err := e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, "oi.tunnel.v2")
		if err == nil || res == nil || res.StatusCode != 400 || errBody(res) != protocol.CodeVersionUnsupported {
			t.Fatalf("%v %+v", err, res)
		}
		v, err := e.consumer("alice").Channel(bg, o.ChannelID)
		if err != nil || v.State != protocol.StatePreparing {
			t.Fatalf("%+v %v", v, err)
		}
		// The ticket was not consumed: a v1 attach still works.
		leg, _, err := e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
		if err != nil {
			t.Fatal(err)
		}
		if f := readFrame(t, leg); f.Type != protocol.FrameReady {
			t.Fatalf("%+v", f)
		}
		_ = leg.CloseNow()
		e.settled()
	})
}

// TV-B1-03: unknown request fields are refused; unknown control message
// fields are ignored.
func TestTVB1_03_UnknownFields(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		ex := must(e.consumer("alice").Lookup(bg, "lab-a/http"))
		body := []byte(`{"exposure_id":"lab-a/http","expected_revision":"` + ex.Revision + `","future":1}`)
		req := must(http.NewRequest("POST", "https://"+e.api+"/v1/channels", bytes.NewReader(body)))
		req.Header.Set("Authorization", "Bearer "+e.token("alice"))
		res := must(e.stats.Do(req))
		if res.StatusCode != 400 || errBody(res) != protocol.CodeInvalidRequest {
			t.Fatalf("%d", res.StatusCode)
		}
		_ = res.Body.Close()
		if s := e.oisStats(); s.Channels.Preparing != 0 {
			t.Fatalf("state created: %+v", s.Channels)
		}
		f := e.fakeOIC("lab-c")
		f.sendRaw(`{"type":"hello","seq":1,"oic_version":"fake","last_epoch_seen":0,"future":{"x":1}}`)
		if _, ok := f.recv().(*protocol.Welcome); !ok {
			t.Fatal("no welcome")
		}
		f.sendRaw(`{"type":"register","seq":2,"future":true,"exposures":[{"exposure_id":"lab-c/x","revision":"rv_` +
			strings.Repeat("a", 26) + `","protocol":"http","transport_profile":"tcp","admission_mode":"best_effort","description":"x","future":1}]}`)
		ack, ok := f.recv().(*protocol.RegisterAck)
		if !ok || len(ack.Accepted) != 1 || ack.Accepted[0] != "lab-c/x" {
			t.Fatalf("%+v", ack)
		}
		f.close()
		e.settled()
	})
}

// TV-B1-04: DATA of F+1 bytes → ABORT(PayloadTooLarge); the target is reset.
func TestTVB1_04_PayloadTooLarge(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		before := len(e.t1.Conns())
		o := e.open("alice", "lab-a/http")
		leg := must2(e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol))
		if f := readFrame(t, leg); f.Type != protocol.FrameReady {
			t.Fatalf("%+v", f)
		}
		sendFrame(t, leg, protocol.DataFrame(make([]byte, 32<<10+1)))
		expectAbort(t, leg, protocol.CodePayloadTooLarge)
		r := waitTarget(t, e.t1, before)
		if len(r.Data) != 0 || !r.Reset {
			t.Fatalf("target %+v", r)
		}
		if v := e.closedState("alice", o.ChannelID); v.CloseReason != protocol.CodePayloadTooLarge {
			t.Fatalf("%+v", v)
		}
		e.settled()
	})
}

func must2[A, B any](a A, _ B, err error) A {
	if err != nil {
		panic(err)
	}
	return a
}

// TV-B1-05: DATA before READY → ABORT(ProtocolError); the target gets 0
// bytes (zero tolerance).
func TestTVB1_05_DataBeforeReady(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		before := len(e.t1.Conns())
		e.acc.setMode(hold) // the provider leg cannot reach OIS yet
		o := e.open("alice", "lab-a/http")
		leg := must2(e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol))
		sendFrame(t, leg, protocol.DataFrame([]byte("early")))
		expectAbort(t, leg, protocol.CodeProtocolError)
		if v := e.closedState("alice", o.ChannelID); v.CloseReason != protocol.CodeProtocolError {
			t.Fatalf("%+v", v)
		}
		e.acc.setMode(pass)
		r := waitTarget(t, e.t1, before)
		if len(r.Data) != 0 {
			t.Fatalf("ZERO-TOLERANCE: target received %q before READY", r.Data)
		}
		e.settled()
	})
}

// TV-B1-06: the request ends with FIN; the full answer arrives afterwards.
func TestTVB1_06_HalfClose(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		got, id := request(t, e.consumer("alice"), "lab-a/http", "slow 300 request")
		if got != "T1:slow 300 request" {
			t.Fatalf("%q", got)
		}
		if v := e.closedState("alice", id); v.CloseReason != protocol.CodeCompleted {
			t.Fatalf("%+v", v)
		}
		e.settled()
	})
}

// TV-B1-07: DATA after FIN → ABORT(ProtocolError); the late bytes never
// reach the target, whose connection ends with a reset (zero tolerance).
// The FIN itself was legitimately forwarded before the violation, so the
// target may see the half-close before the reset; and the reset may
// discard pre-FIN bytes the target had not read yet.
func TestTVB1_07_DataAfterFin(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		before := len(e.t1.Conns())
		o := e.open("alice", "lab-a/http")
		leg := must2(e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol))
		if f := readFrame(t, leg); f.Type != protocol.FrameReady {
			t.Fatalf("%+v", f)
		}
		sendFrame(t, leg, protocol.DataFrame([]byte("slow 1500 x")))
		sendFrame(t, leg, protocol.FinFrame())
		sendFrame(t, leg, protocol.DataFrame([]byte("late")))
		expectAbort(t, leg, protocol.CodeProtocolError)
		r := waitTarget(t, e.t1, before)
		if !strings.HasPrefix("slow 1500 x", string(r.Data)) {
			t.Fatalf("ZERO-TOLERANCE: target received %q", r.Data)
		}
		if !r.Reset {
			t.Fatalf("target connection did not end with a reset: %+v", r)
		}
		e.settled()
	})
}

// TV-B1-08: the target resets mid-answer → the consumer gets
// ABORT(ChannelInterrupted) and the oi local connection sees a reset, not EOF.
func TestTVB1_08_TargetReset(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		p := startProc(t, e.dir, "oi-listen", oiBin, []string{"OI_TOKEN=" + e.token("alice")},
			"--ois", "https://"+e.api, "--ca", e.caFile, "connect", "lab-a/http", "--listen", "127.0.0.1:0")
		l, n := p.waitLine(t, 10*time.Second, "listening", 0, func(m map[string]any) bool { return m["listening"] != nil })
		conn := must(net.Dial("tcp", l["listening"].(string)))
		_, _ = conn.Write([]byte("reset"))
		_ = conn.(*net.TCPConn).CloseWrite()
		got, err := io.ReadAll(conn)
		if err == nil {
			t.Fatalf("local connection ended with EOF after %q; want a reset", got)
		}
		if !strings.HasPrefix("T1:part", string(got)) {
			t.Fatalf("%q", got)
		}
		p.waitLine(t, 10*time.Second, "ChannelInterrupted report", n, func(m map[string]any) bool { return m["code"] == string(protocol.CodeChannelInterrupted) })
		p.stop(t)
		e.obs("O4", map[string]any{"vector": "TV-B1-08", "local_end": "reset"})
		e.settled()
	})
}

// TV-B1-09: a 64 MiB answer read slowly keeps every component's buffer
// within the B4 §3 bound and arrives byte for byte.
func TestTVB1_09_LargeSlowRead(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		const size = 64 << 20
		const f = 32 << 10
		oisBefore, oicBefore := e.oisStats().Process, e.oicStats("lab-a").Process
		start := time.Now()
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("stream 67108864"))
		_ = s.CloseWrite()
		buf := make([]byte, f)
		var off int64
		var maxOIS, maxOIC int64
		for {
			n, err := s.Read(buf)
			for i := range n {
				if buf[i] != fixture.Pattern(off+int64(i)) {
					t.Fatalf("byte %d differs", off+int64(i))
				}
			}
			off += int64(n)
			if off%(4<<20) < int64(n) {
				maxOIS = max(maxOIS, e.oisStats().MaxBufferedBytes)
				maxOIC = max(maxOIC, e.oicStats("lab-a").MaxBufferedBytes)
				time.Sleep(20 * time.Millisecond) // a slow reader
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		_ = s.Close()
		if off != size {
			t.Fatalf("received %d bytes", off)
		}
		maxOIS = max(maxOIS, e.oisStats().MaxBufferedBytes)
		maxOIC = max(maxOIC, e.oicStats("lab-a").MaxBufferedBytes)
		if maxOIS > 2*f || maxOIC > 2*f {
			t.Fatalf("buffers above 2F: ois %d oic %d", maxOIS, maxOIC)
		}
		oisAfter, oicAfter := e.oisStats().Process, e.oicStats("lab-a").Process
		e.obs("O3", map[string]any{"vector": "TV-B1-09", "bytes": size, "ms": time.Since(start).Milliseconds(),
			"ois_max_buffered": maxOIS, "oic_max_buffered": maxOIC,
			"ois_cpu_ms": oisAfter.CPUMillis - oisBefore.CPUMillis, "oic_cpu_ms": oicAfter.CPUMillis - oicBefore.CPUMillis,
			"ois_rss": oisAfter.RSSBytes, "oic_rss": oicAfter.RSSBytes})
		e.settled()
	})
}

// TV-B1-10: idempotent Open replay, conflict, consumed ticket and restart.
func TestTVB1_10_Idempotency(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		c := e.consumer("alice")
		rev := must(c.Lookup(bg, "lab-a/http")).Revision
		req := protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: rev, ClientRequestID: "it-1"}
		var r1, r2 protocol.OpenResponse
		if st, err := c.Do(bg, "POST", "/v1/channels", "", req, &r1); err != nil || st != 201 {
			t.Fatal(st, err)
		}
		if st, err := c.Do(bg, "POST", "/v1/channels", "", req, &r2); err != nil || st != 200 || r2.ChannelID != r1.ChannelID || r2.Attach.Ticket != r1.Attach.Ticket {
			t.Fatal(st, err, r2)
		}
		other := req
		other.ExpectedRevision = "rv_" + strings.Repeat("z", 26)
		if _, err := c.Do(bg, "POST", "/v1/channels", "", other, nil); protocol.CodeOf(err) != protocol.CodeIdempotencyConflict {
			t.Fatal(err)
		}
		s := must(c.Attach(bg, r1))
		var r3 protocol.OpenResponse
		if st, err := c.Do(bg, "POST", "/v1/channels", "", req, &r3); err != nil || st != 200 || r3.Attach != nil {
			t.Fatal(st, err, r3)
		}
		_ = s.Close()
		e.stopOIS()
		e.startOIS()
		e.waitRegistered("lab-a")
		c = e.consumer("alice")
		var r4 protocol.OpenResponse
		if st, err := c.Do(bg, "POST", "/v1/channels", "", req, &r4); err != nil || st != 201 || r4.ChannelID == r1.ChannelID {
			t.Fatal(st, err, r4)
		}
		_, _ = c.Close(bg, r4.ChannelID)
		e.baseline() // a new OIS process
		e.settled()
	})
}

// TV-B1-11: revoking while one leg is attached never lets READY out; the
// attached leg gets ABORT(Revoked), the late leg 410 and the target 0
// bytes, over 200 randomized runs (zero tolerance). TV-B3-09 is the same
// property: the commit check after the admin call sees the new version.
func TestTVB1_11_RevokeDuringPrepare(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		before := len(e.t1.Conns())
		for i := range 200 {
			consumerFirst := i%2 == 0
			if consumerFirst {
				e.acc.setMode(hold)
			}
			o := e.open("alice", "lab-a/http")
			var leg *websocket.Conn
			if consumerFirst {
				leg = must2(e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol))
			} else {
				e.eventually(5*time.Second, "provider attached", func() bool { return e.oisStats().Tickets == 1 })
			}
			time.Sleep(time.Duration(rand.IntN(5)) * time.Millisecond)
			e.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(false)})
			if consumerFirst {
				expectAbort(t, leg, protocol.CodeRevoked)
				_ = leg.CloseNow()
				e.acc.setMode(pass)
			} else {
				_, res, err := e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
				if err == nil || res == nil || res.StatusCode != 410 {
					t.Fatalf("run %d: late consumer leg: %v %+v", i, err, res)
				}
			}
			e.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(true)})
		}
		e.t1.Wait(10*time.Second, func(rs []fixture.Record) bool {
			for _, r := range rs[before:] {
				if !r.Done {
					return false
				}
			}
			return true
		})
		for i, r := range e.t1.Conns()[before:] {
			if len(r.Data) != 0 {
				t.Fatalf("ZERO-TOLERANCE: run %d target received %q", i, r.Data)
			}
		}
		e.obs("O1", map[string]any{"vector": "TV-B1-11", "runs": 200, "ready_sent": 0})
		e.settled()
	})
}

func ptr[T any](v T) *T { return &v }

// TV-B1-12: revoking an Open channel aborts both legs within T_close and
// resets the target; a new Open is 403.
func TestTVB1_12_RevokeOpen(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		before := len(e.t1.Conns())
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("hold"))
		start := time.Now()
		e.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Actions: []string{protocol.ActionDiscover}})
		_, err := io.ReadAll(s)
		var ae *client.AbortError
		if !errors.As(err, &ae) || ae.Code != protocol.CodeRevoked || time.Since(start) > tClose {
			t.Fatalf("%v after %v", err, time.Since(start))
		}
		if r := waitTarget(t, e.t1, before); !r.Reset {
			t.Fatalf("target %+v", r)
		}
		if _, err := e.consumer("alice").OpenLatest(bg, "lab-a/http", "", false); protocol.CodeOf(err) != protocol.CodeDenied {
			t.Fatal(err)
		}
		e.obs("O4", map[string]any{"vector": "TV-B1-12", "revoke_to_abort_ms": time.Since(start).Milliseconds()})
		_ = s.Close()
		e.settled()
	})
}

// TV-B1-13: replacing the control connection (G→G+1) aborts the old
// generation's channel X; a new channel Y works normally.
func TestTVB1_13_GenerationReplaced(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		x := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = x.Write([]byte("hold"))
		gen := e.oicStats("lab-a").Generation
		e.acc.cut(e.oics["lab-a"].ctrl)
		start := time.Now()
		_, err := io.ReadAll(x)
		var ae *client.AbortError
		if !errors.As(err, &ae) || (ae.Code != protocol.CodeChannelInterrupted && ae.Code != protocol.CodeAccessOffline) {
			t.Fatalf("%v", err)
		}
		_ = x.Close()
		e.eventually(15*time.Second, "new generation", func() bool {
			s := e.oicStats("lab-a")
			return s.Registered && s.Generation > gen
		})
		e.oics["lab-a"].ctrl = e.acc.count() - 1
		got, id := request(t, e.consumer("alice"), "lab-a/http", "after")
		if got != "T1:after" {
			t.Fatalf("%q", got)
		}
		if v := e.closedState("alice", id); v.CloseReason != protocol.CodeCompleted {
			t.Fatalf("%+v", v)
		}
		e.obs("O4", map[string]any{"vector": "TV-B1-13", "abort": ae.Code, "recovery_ms": time.Since(start).Milliseconds()})
		e.settled()
	})
}

// TV-B1-14: the provider leg never attaches → Closed(PrepareTimeout) after
// T_prepare, quota released and the OIC's dialed target closed.
func TestTVB1_14_PrepareTimeout(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		before := len(e.t1.Conns())
		e.acc.setMode(hold)
		start := time.Now()
		o := e.open("alice", "lab-a/http")
		leg := must2(e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol))
		expectAbort(t, leg, protocol.CodePrepareTimeout)
		if el := time.Since(start); el < 2*time.Second || el > 2*time.Second+tClose {
			t.Fatalf("PrepareTimeout after %v", el)
		}
		if v := e.closedState("alice", o.ChannelID); v.CloseReason != protocol.CodePrepareTimeout {
			t.Fatalf("%+v", v)
		}
		if r := waitTarget(t, e.t1, before); len(r.Data) != 0 {
			t.Fatalf("target %+v", r)
		}
		if s := e.oisStats(); s.Channels.Preparing != 0 {
			t.Fatalf("%+v", s)
		}
		e.acc.setMode(pass)
		e.settled()
	})
}

// TV-B1-15: the target port is closed → prepare_failed(TargetConnectFailed)
// is the consumer-visible reason and the ticket is void.
func TestTVB1_15_TargetConnectFailed(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		o := e.open("carol", "lab-b/down")
		if v := e.closedState("carol", o.ChannelID); v.CloseReason != protocol.CodeTargetConnectFailed {
			t.Fatalf("%+v", v)
		}
		_, res, err := e.rawAttach(o.Attach.URL, e.token("carol"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
		if err == nil || res.StatusCode != 410 {
			t.Fatalf("%v", err)
		}
		e.obs("O1", map[string]any{"vector": "TV-B1-15", "result": "TargetConnectFailed", "kind_of_failure": "connect"})
		e.settled()
	})
}

// TV-B1-16: with N_OIS=16, 20 concurrent Opens → exactly 16 succeed and 4
// get 429; no quota leaks.
func TestTVB1_16_Quota(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{limits: map[string]any{"n_ois": 16}})
		c := e.consumer("alice")
		rev := must(c.Lookup(bg, "lab-a/http")).Revision
		attempt := func() (ok, full int, ids []string) {
			var mu sync.Mutex
			var wg sync.WaitGroup
			for range 20 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					o, err := c.Open(bg, protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: rev})
					mu.Lock()
					defer mu.Unlock()
					switch {
					case err == nil:
						ok++
						ids = append(ids, o.ChannelID)
					case protocol.CodeOf(err) == protocol.CodeResourceExhausted:
						full++
					default:
						t.Errorf("open: %v", err)
					}
				}()
			}
			wg.Wait()
			return
		}
		for round := range 2 {
			ok, full, ids := attempt()
			if ok != 16 || full != 4 {
				t.Fatalf("round %d: %d ok, %d exhausted", round, ok, full)
			}
			for _, id := range ids {
				if _, err := c.Close(bg, id); err != nil {
					t.Fatal(err)
				}
			}
			e.eventually(settle, "quota released", func() bool {
				s := e.oisStats().Channels
				return s.Preparing+s.Open+s.Closing == 0
			})
		}
		e.settled()
	})
}

// TV-B1-17: a prepare_channel before register_ack or for another
// generation is refused by the OIC with ProtocolError, without dialing.
func TestTVB1_17_PrepareOutOfOrder(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		if be != backends(t)[0] {
			t.Skip("the vector does not involve the OIS store")
		}
		e := newEnv(t, be, envOpts{noOICs: true})
		before := len(e.t1.Conns())
		f := newFakeOIS(t, e)
		o := f.runOIC(t, e, "lab-f", exposure("http", e.t1.Addr(), protocol.AdmissionBestEffort, nil))
		ctl := f.control(t)
		hello, ok := ctl.recv().(*protocol.Hello)
		if !ok {
			t.Fatal("no hello")
		}
		_ = hello
		ctl.send(&protocol.Welcome{Epoch: 1, Generation: 1, RunID: "run_fake", HeartbeatIntervalMS: 1000, LicenseTTLMS: 30000,
			Limits: protocol.WelcomeLimits{DataMax: 32 << 10, CloseGraceMS: 1000, DescMax: 2048, MaxExposures: 128}})
		reg, ok := ctl.recv().(*protocol.Register)
		if !ok || len(reg.Exposures) != 1 {
			t.Fatalf("%+v", reg)
		}
		prep := func(gen int64) *protocol.PrepareChannel {
			return &protocol.PrepareChannel{ChannelID: protocol.NewID("ch_"), ExposureID: "lab-f/http", Revision: reg.Exposures[0].Revision,
				Subject: "alice", Epoch: 1, Generation: gen, PrepareTTLMS: 2000, ProviderTicket: "tk_fake"}
		}
		ctl.send(prep(1)) // before register_ack
		if pf := expectCtl[*protocol.PrepareFailed](t, ctl); pf.Code != protocol.CodeProtocolError {
			t.Fatalf("%+v", pf)
		}
		ctl.send(&protocol.RegisterAck{Accepted: []string{"lab-f/http"}, Rejected: []protocol.Rejection{}})
		ctl.send(prep(7)) // another generation
		if pf := expectCtl[*protocol.PrepareFailed](t, ctl); pf.Code != protocol.CodeProtocolError {
			t.Fatalf("%+v", pf)
		}
		time.Sleep(200 * time.Millisecond)
		if n := len(e.t1.Conns()); n != before {
			t.Fatalf("ZERO-TOLERANCE: %d dials", n-before)
		}
		o.stop(t)
	})
}

// TV-B1-18: require_ready → 412 ReadinessRequired; best_effort opens with
// readiness "unknown" (P1 has no probes).
func TestTVB1_18_Admission(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		c := e.consumer("alice")
		if _, err := c.OpenLatest(bg, "lab-a/alt", "", false); protocol.CodeOf(err) != protocol.CodeReadinessRequired {
			t.Fatal(err)
		}
		if _, err := c.OpenLatest(bg, "lab-a/http", "", true); protocol.CodeOf(err) != protocol.CodeReadinessRequired {
			t.Fatal(err)
		}
		o := e.open("alice", "lab-a/http")
		if o.Readiness != protocol.ReadinessUnknown || o.AdmissionMode != protocol.AdmissionBestEffort {
			t.Fatalf("%+v", o)
		}
		_, _ = c.Close(bg, o.ChannelID)
		e.obs("O1", map[string]any{"vector": "TV-B1-18", "require_ready": "ReadinessRequired", "best_effort_readiness": o.Readiness})
		e.settled()
	})
}
