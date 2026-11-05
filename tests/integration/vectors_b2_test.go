package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

func (e *env) newToken(subject string, ttl int64) protocol.TokenCreated {
	e.t.Helper()
	var c protocol.TokenCreated
	if err := json.Unmarshal(e.write("POST", "/v1/admin/tokens", protocol.TokenCreate{Subject: subject, Scope: protocol.ScopeConsumer, TTLSeconds: ttl}), &c); err != nil {
		e.t.Fatal(err)
	}
	return c
}

func abortCode(err error) protocol.Code {
	var ae *client.AbortError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// keepAlive writes to s every period until stop is closed, so IDLE does
// not end the channel while a vector waits for another reason.
func keepAlive(s *client.Stream, period time.Duration, stop chan struct{}) {
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(period):
				if _, err := s.Write([]byte("k")); err != nil {
					return
				}
			}
		}
	}()
}

// TV-B2-01: missing, wrong, expired and revoked consumer tokens → 401 on
// the API and on attach, without state changes.
func TestTVB2_01_BadCredentials(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		o := e.open("alice", "lab-a/http")
		expired := e.newToken("alice", 1)
		revoked := e.newToken("alice", 0)
		e.write("DELETE", "/v1/admin/tokens/"+revoked.TokenID, nil)
		id, _, _ := identity.ParseToken(e.token("alice"))
		wrong := "oi_" + id + "_" + strings.Repeat("A", 43)
		time.Sleep(1100 * time.Millisecond)
		for name, tok := range map[string]string{"missing": "", "wrong": wrong, "expired": expired.Token, "revoked": revoked.Token} {
			req := must(http.NewRequest("GET", "https://"+e.api+"/v1/exposures", nil))
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			res := must(e.stats.Do(req))
			if res.StatusCode != 401 || errBody(res) != protocol.CodeUnauthenticated {
				t.Errorf("%s API: %d", name, res.StatusCode)
			}
			_ = res.Body.Close()
			_, res, err := e.rawAttach(o.Attach.URL, tok, o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
			if err == nil || res == nil || res.StatusCode != 401 {
				t.Errorf("%s attach: %v", name, err)
			}
		}
		if v := must(e.consumer("alice").Channel(bg, o.ChannelID)); v.State != protocol.StatePreparing {
			t.Fatalf("%+v", v)
		}
		_, _ = e.consumer("alice").Close(bg, o.ChannelID)
		e.settled()
	})
}

// TV-B2-02: no discover → absent and 404; discover without open → 403.
func TestTVB2_02_DiscoverAndOpenGrants(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "pd", Subject: "dave", ExposureSelector: "lab-a/http",
			Actions: []string{protocol.ActionDiscover}})
		rev := must(e.consumer("alice").Lookup(bg, "lab-a/http")).Revision
		req := protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: rev}
		if l := must(e.consumer("dave").DiscoverAll(bg)); len(l) != 1 || l[0].ExposureID != "lab-a/http" {
			t.Fatalf("%+v", l)
		}
		if _, err := e.consumer("dave").Open(bg, req); protocol.CodeOf(err) != protocol.CodeDenied {
			t.Fatal(err)
		}
		if l := must(e.consumer("erin").DiscoverAll(bg)); len(l) != 0 {
			t.Fatalf("%+v", l)
		}
		if _, err := e.consumer("erin").Open(bg, req); protocol.CodeOf(err) != protocol.CodeNotFound {
			t.Fatal(err)
		}
		e.obs("O1", map[string]any{"vector": "TV-B2-02", "no_discover": "NotFound", "no_open": "Denied"})
		e.settled()
	})
}

// TV-B2-03: two consumers, 500 interleaved channels each, never cross
// (zero tolerance).
func TestTVB2_03_NoCrossTalk(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		b1, b2 := len(e.t1.Conns()), len(e.t2.Conns())
		var wg sync.WaitGroup
		var mu sync.Mutex
		var failures []string
		start := time.Now()
		for _, who := range []struct{ subject, exposure, prefix, target string }{
			{"alice", "lab-a/http", "a", "T1"}, {"carol", "lab-b/http", "c", "T2"},
		} {
			c := e.consumer(who.subject)
			for w := range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := w; i < 500; i += 4 {
						payload := fmt.Sprintf("%s-%d", who.prefix, i)
						s, err := c.Dial(bg, who.exposure)
						if err == nil {
							_, err = s.Write([]byte(payload))
						}
						var got []byte
						if err == nil {
							_ = s.CloseWrite()
							got, err = io.ReadAll(s)
							_ = s.Close()
						}
						if err != nil || string(got) != who.target+":"+payload {
							mu.Lock()
							failures = append(failures, fmt.Sprintf("%s: %q %v", payload, got, err))
							mu.Unlock()
						}
					}
				}()
			}
		}
		wg.Wait()
		if len(failures) > 0 {
			t.Fatalf("ZERO-TOLERANCE: %d wrong answers, e.g. %v", len(failures), failures[:min(5, len(failures))])
		}
		for name, rs := range map[string][]fixture.Record{"T1": e.t1.Conns()[b1:], "T2": e.t2.Conns()[b2:]} {
			want := map[string]string{"T1": "a-", "T2": "c-"}[name]
			for _, r := range rs {
				if !strings.HasPrefix(string(r.Data), want) {
					t.Fatalf("ZERO-TOLERANCE: %s received %q", name, r.Data)
				}
			}
		}
		e.obs("O3", map[string]any{"vector": "TV-B2-03", "channels": 1000, "ms": time.Since(start).Milliseconds()})
		e.settled()
	})
}

// TV-B2-04: retargeting lab-a/http from T1 to T3 aborts its channels with
// RevisionChanged; the old revision never reaches T3 (zero tolerance).
func TestTVB2_04_TargetReplaced(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		c := e.consumer("alice")
		old := must(c.Lookup(bg, "lab-a/http")).Revision
		s := must(c.Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("hold"))
		b3 := len(e.t3.Conns())
		e.rewriteOIC("lab-a", []any{
			exposure("http", e.t3.Addr(), protocol.AdmissionBestEffort, nil),
			exposure("alt", e.t3.Addr(), protocol.AdmissionRequireReady, nil),
		})
		_, err := io.ReadAll(s)
		if abortCode(err) != protocol.CodeRevisionChanged {
			t.Fatalf("%v", err)
		}
		_ = s.Close()
		// Opens with the old revision race the re-registration: each one is
		// refused at precheck (409) or by the OIC re-check, never served by T3.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			o, err := c.Open(bg, protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: old})
			if protocol.CodeOf(err) == protocol.CodeRevisionMismatch {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if st, err := c.Attach(bg, o); err == nil {
				_, _ = st.Write([]byte("old"))
				_ = st.CloseWrite()
				if got, _ := io.ReadAll(st); strings.HasPrefix(string(got), "T3") {
					t.Fatalf("ZERO-TOLERANCE: old revision served by T3: %q", got)
				}
				_ = st.Close()
			}
		}
		for _, r := range e.t3.Conns()[b3:] {
			if strings.Contains(string(r.Data), "old") {
				t.Fatalf("ZERO-TOLERANCE: T3 received %q", r.Data)
			}
		}
		if _, err := c.Open(bg, protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: old}); protocol.CodeOf(err) != protocol.CodeRevisionMismatch {
			t.Fatal(err)
		}
		got, _ := request(t, c, "lab-a/http", "new")
		if got != "T3:new" {
			t.Fatalf("%q", got)
		}
		e.settled()
	})
}

// TV-B2-05: one consumer ticket attached twice concurrently → exactly one
// success and one 409; after closing, reuse gives 410 (zero tolerance:
// two successes).
func TestTVB2_05_TicketOnce(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		for range 20 {
			o := e.open("alice", "lab-a/http")
			var wg sync.WaitGroup
			type result struct {
				code int
				leg  *websocket.Conn
			}
			results := make(chan result, 2)
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					c, res, err := e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
					switch {
					case err == nil:
						results <- result{101, c}
					case res != nil:
						results <- result{res.StatusCode, nil}
					default:
						results <- result{-1, nil}
					}
				}()
			}
			// The successful leg stays attached until both attempts were
			// answered, so the loser meets a live channel (409, not 410).
			wg.Wait()
			close(results)
			got := map[int]int{}
			for r := range results {
				got[r.code]++
				if r.leg != nil {
					readFrame(t, r.leg)
					_ = r.leg.CloseNow()
				}
			}
			if got[101] != 1 || got[409] != 1 {
				t.Fatalf("ZERO-TOLERANCE: attach results %v", got)
			}
			_, _ = e.consumer("alice").Close(bg, o.ChannelID)
			_, res, err := e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
			if err == nil || (res.StatusCode != 410 && res.StatusCode != 401) {
				t.Fatalf("reuse after close: %v", err)
			}
		}
		e.settled()
	})
}

// TV-B2-06: a consumer ticket used as provider, with another subject, for
// another channel or in the URL is refused; the channel is unaffected.
func TestTVB2_06_TicketMisuse(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		o := e.open("alice", "lab-a/http")
		other := e.open("alice", "lab-a/http")
		accURL := "wss://" + e.acc.addr() + "/v1/channels/" + o.ChannelID + "/attach"
		cases := []struct {
			name           string
			url, tok, tick string
			cert           *pki.Leaf
			want           int
		}{
			{"provider role", accURL, "", o.Attach.Ticket, e.oics["lab-b"].leaf, 401},
			{"other subject", o.Attach.URL, e.token("carol"), o.Attach.Ticket, nil, 401},
			{"other channel", other.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, 401},
			{"query string", o.Attach.URL + "?ticket=" + o.Attach.Ticket, e.token("alice"), "", nil, 400},
		}
		for _, c := range cases {
			_, res, err := e.rawAttach(c.url, c.tok, c.tick, c.cert, protocol.TunnelSubprotocol)
			if err == nil || res == nil || res.StatusCode != c.want {
				t.Errorf("%s: %v %+v", c.name, err, res)
			}
		}
		leg := must2(e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol))
		if f := readFrame(t, leg); f.Type != protocol.FrameReady {
			t.Fatalf("%+v", f)
		}
		_ = leg.CloseNow()
		_, _ = e.consumer("alice").Close(bg, other.ChannelID)
		e.settled()
	})
}

func (e *env) accessHandshake(leaf *pki.Leaf) error {
	res, err := certClient(e.ca, leaf).Get("https://" + e.acc.addr() + "/v1/none")
	if err == nil {
		_ = res.Body.Close()
	}
	return err
}

// TV-B2-07: foreign CA, unpinned and disabled certificates are refused;
// during rotation both pins work; removing the old pin closes the
// connection that used it and aborts its channels.
func TestTVB2_07_Certificates(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		foreign := must(must(pki.NewCA("foreign")).Client("lab-b"))
		stranger := must(e.ca.Client("stranger"))
		disabled := must(e.ca.Client("lab-d"))
		e.write("POST", "/v1/admin/accesses", protocol.AccessSpec{AccessID: "lab-d", SPKIPins: []string{identity.SPKIPin(disabled.Cert)}, Enabled: ptr(false)})
		for name, leaf := range map[string]*pki.Leaf{"foreign CA": foreign, "unpinned": stranger, "disabled": disabled} {
			if err := e.accessHandshake(leaf); err == nil {
				t.Errorf("%s certificate accepted", name)
			}
		}
		b := e.oics["lab-b"]
		next := must(e.ca.Client("lab-b"))
		pin1, pin2 := identity.SPKIPin(b.leaf.Cert), identity.SPKIPin(next.Cert)
		e.write("PATCH", "/v1/admin/accesses/lab-b", protocol.AccessSpec{SPKIPins: []string{pin1, pin2}})
		if err := e.accessHandshake(next); err != nil {
			t.Fatalf("new pin refused during rotation: %v", err)
		}
		s := must(e.consumer("carol").Dial(bg, "lab-b/http"))
		_, _ = s.Write([]byte("hold"))
		start := time.Now()
		e.write("PATCH", "/v1/admin/accesses/lab-b", protocol.AccessSpec{SPKIPins: []string{pin2}})
		if _, err := io.ReadAll(s); abortCode(err) != protocol.CodeRevoked {
			t.Fatalf("%v", err)
		}
		_ = s.Close()
		e.eventually(10*time.Second, "old pin disconnected", func() bool { return !e.oicStats("lab-b").Connected })
		e.obs("O4", map[string]any{"vector": "TV-B2-07", "unpin_to_abort_ms": time.Since(start).Milliseconds()})
		// The OIC switches to the new certificate and reconnects.
		b.stop(t)
		b.leaf = next
		if _, _, err := next.Write(e.dir, "lab-b"); err != nil {
			t.Fatal(err)
		}
		e.startOIC("lab-b", b.cfg["exposures"].([]any))
		e.settled()
	})
}

// TV-B2-08: entries outside the Access namespace, beyond max_exposures or
// above DESC_MAX are rejected in register_ack and never listed.
func TestTVB2_08_RegisterLimits(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		f := e.fakeOIC("lab-c")
		e.write("PATCH", "/v1/admin/accesses/lab-c", protocol.AccessSpec{MaxExposures: ptr(3)})
		f.send(&protocol.Hello{OICVersion: "fake"})
		expectCtl[*protocol.Welcome](t, f)
		rev := "rv_" + strings.Repeat("b", 26)
		entry := func(id, desc string) protocol.RegisterExposure {
			return protocol.RegisterExposure{ExposureID: id, Revision: rev, Protocol: "http", TransportProfile: "tcp",
				AdmissionMode: protocol.AdmissionBestEffort, Description: desc}
		}
		f.send(&protocol.Register{Exposures: []protocol.RegisterExposure{
			entry("lab-a/stolen", "x"), entry("lab-c/big", strings.Repeat("d", 2049)),
			entry("lab-c/one", "1"), entry("lab-c/two", "2"), entry("lab-c/three", "3"), entry("lab-c/four", "4"),
		}})
		ack := expectCtl[*protocol.RegisterAck](t, f)
		got := map[string]protocol.Code{}
		for _, r := range ack.Rejected {
			got[r.ExposureID] = r.Code
		}
		if len(ack.Accepted) != 3 || got["lab-a/stolen"] != protocol.CodeDenied || got["lab-c/big"] != protocol.CodePayloadTooLarge ||
			got["lab-c/four"] != protocol.CodeResourceExhausted {
			t.Fatalf("%+v", ack)
		}
		e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "pf", Subject: "frank", ExposureSelector: "lab-c/*",
			Actions: []string{protocol.ActionDiscover}})
		e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "pf2", Subject: "frank", ExposureSelector: "lab-a/stolen",
			Actions: []string{protocol.ActionDiscover}})
		if l := must(e.consumer("frank").DiscoverAll(bg)); len(l) != 3 {
			t.Fatalf("%+v", l)
		}
		f.close()
		e.settled()
	})
}

// TV-B2-09: a host name resolving only outside allowed_cidrs is refused
// with TargetAddressNotAllowed and never dialed (zero tolerance).
func TestTVB2_09_AddressNotAllowed(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		b2 := len(e.t2.Conns())
		o := e.open("carol", "lab-b/named")
		if v := e.closedState("carol", o.ChannelID); v.CloseReason != protocol.CodeTargetAddressNotAllowed {
			t.Fatalf("%+v", v)
		}
		time.Sleep(200 * time.Millisecond)
		if n := len(e.t2.Conns()) - b2; n != 0 {
			t.Fatalf("ZERO-TOLERANCE: %d connections to the refused address", n)
		}
		e.settled()
	})
}

// TV-B2-10: cutting only the control connection ends the channel within
// L±1s (LicenseExpired at the OIC or AccessOffline at OIS); no channel is
// prepared meanwhile.
func TestTVB2_10_ControlCut(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		if err := e.controlCutWitness(); err != nil {
			t.Fatal(err)
		}
	})
}

// TV-B2-11: an OIC paused beyond L aborts with LicenseExpired as soon as
// it resumes and forwards nothing after expiry (zero tolerance; Linux).
func TestTVB2_11_ProcessPause(t *testing.T) {
	if !canPause {
		t.Skip("SIGSTOP/SIGCONT are Linux-only; the vector runs in Linux CI")
	}
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		b1 := len(e.t1.Conns())
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("before"))
		e.t1.Wait(5*time.Second, func(rs []fixture.Record) bool { return len(rs) > b1 && string(rs[b1].Data) == "before" })
		o := e.oics["lab-a"]
		if err := pause(o.cmd.Process); err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		keepAlive(s, 200*time.Millisecond, stop)
		time.Sleep(4 * time.Second) // beyond L = 3 s
		close(stop)
		if err := resume(o.cmd.Process); err != nil {
			t.Fatal(err)
		}
		resumed := time.Now()
		expired := false
		for deadline := time.Now().Add(10 * time.Second); !expired && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			expired = e.oicStats("lab-a").Closed[string(protocol.CodeLicenseExpired)] >= 1
		}
		resumeToAbort := time.Since(resumed)
		time.Sleep(500 * time.Millisecond)
		// The zero-tolerance check comes first, so a breach is reported as
		// one even when the expiry is not recorded either.
		if got := string(e.t1.Conns()[b1].Data); got != "before" {
			t.Fatalf("ZERO-TOLERANCE: forwarded after expiry: %q", got)
		}
		if !expired {
			t.Fatal("timed out after 10s waiting for LicenseExpired at the OIC")
		}
		e.obs("O4", map[string]any{"vector": "TV-B2-11", "paused_ms": 4000, "resume_to_abort_ms": resumeToAbort.Milliseconds(),
			"forwarded_after_expiry": 0, "manual_steps": 0})
		_ = s.Close()
		e.waitRegistered("lab-a")
		e.oics["lab-a"].ctrl = e.acc.count() - 1
		e.settled()
	})
}

// TV-B2-12: a revoked token ends its channels with Revoked, an expired one
// with Unauthenticated; both get 401 afterwards.
func TestTVB2_12_TokenRevokeAndExpiry(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		tok := e.newToken("alice", 0)
		c := e.clientFor(tok.Token)
		s := must(c.Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("hold"))
		e.write("DELETE", "/v1/admin/tokens/"+tok.TokenID, nil)
		if _, err := io.ReadAll(s); abortCode(err) != protocol.CodeRevoked {
			t.Fatalf("%v", err)
		}
		_ = s.Close()
		if _, err := c.DiscoverAll(bg); protocol.CodeOf(err) != protocol.CodeUnauthenticated {
			t.Fatal(err)
		}
		short := e.newToken("alice", 5)
		c2 := e.clientFor(short.Token)
		s2 := must(c2.Dial(bg, "lab-a/http"))
		stop := make(chan struct{})
		keepAlive(s2, 300*time.Millisecond, stop)
		_, err := io.ReadAll(s2)
		close(stop)
		if abortCode(err) != protocol.CodeUnauthenticated {
			t.Fatalf("%v", err)
		}
		_ = s2.Close()
		if _, err := c2.DiscoverAll(bg); protocol.CodeOf(err) != protocol.CodeUnauthenticated {
			t.Fatal(err)
		}
		e.settled()
	})
}

// TV-B2-13: disabling an Access closes its control connection and revokes
// its channels; re-enabling needs a new connection with a new generation.
func TestTVB2_13_DisableAccess(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		gen := e.oicStats("lab-a").Generation
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("hold"))
		e.write("PATCH", "/v1/admin/accesses/lab-a", protocol.AccessSpec{Enabled: ptr(false)})
		if _, err := io.ReadAll(s); abortCode(err) != protocol.CodeRevoked {
			t.Fatalf("%v", err)
		}
		_ = s.Close()
		e.eventually(10*time.Second, "disconnected", func() bool { return !e.oicStats("lab-a").Connected })
		e.write("PATCH", "/v1/admin/accesses/lab-a", protocol.AccessSpec{Enabled: ptr(true)})
		e.eventually(40*time.Second, "new generation", func() bool {
			st := e.oicStats("lab-a")
			return st.Registered && st.Generation > gen
		})
		e.oics["lab-a"].ctrl = e.acc.count() - 1
		e.settled()
	})
}

// TV-B2-14: an admin token cannot Open and a consumer token cannot
// administer (403).
func TestTVB2_14_ScopeSeparation(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		rev := must(e.consumer("alice").Lookup(bg, "lab-a/http")).Revision
		if _, err := e.admin.Open(bg, protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: rev}); protocol.CodeOf(err) != protocol.CodeDenied {
			t.Fatal(err)
		}
		if _, err := e.consumer("alice").Do(bg, "GET", "/v1/admin/config", "", nil, nil); protocol.CodeOf(err) != protocol.CodeDenied {
			t.Fatal(err)
		}
		e.settled()
	})
}
