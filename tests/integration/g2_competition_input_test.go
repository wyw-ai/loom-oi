package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const g2CompetitionHeaderCap = 8 << 10

type g2CompetitionResponseHeader struct {
	Socket      int    `json:"socket"`
	StartedNS   int64  `json:"started_ns"`
	CompletedNS int64  `json:"completed_ns"`
	Status      int    `json:"status"`
	Complete    bool   `json:"complete"`
	HeaderRaw   []byte `json:"header_raw"`
	PeekedBytes int    `json:"peeked_bytes"`
	Error       string `json:"error"`
}

func g2CompetitionParseHeader(raw []byte) (header []byte, status int, complete bool, err error) {
	if len(raw) > g2CompetitionHeaderCap {
		return append([]byte(nil), raw[:g2CompetitionHeaderCap]...), 0, false, fmt.Errorf("response header exceeds peek capacity")
	}
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		return append([]byte(nil), raw...), 0, false, fmt.Errorf("first peek did not contain a complete response header")
	}
	header = append([]byte(nil), raw[:end+4]...)
	r, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(header)), nil)
	if err != nil {
		return header, 0, true, fmt.Errorf("invalid first response header: %w", err)
	}
	// This Body is backed only by the copied header, never by the socket.
	_ = r.Body.Close()
	if r.ProtoMajor != 1 || (r.ProtoMinor != 0 && r.ProtoMinor != 1) || r.StatusCode != http.StatusOK {
		return header, r.StatusCode, true, fmt.Errorf("first slow response is not HTTP/1.x 200")
	}
	return header, r.StatusCode, true, nil
}

func g2CompetitionAwaitWrites(s *g2SlowScrapes, deadline time.Time) error {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	writes := s.Snapshot()
	if len(writes) != 2 {
		return fmt.Errorf("expected exactly two completed request streams")
	}
	for _, w := range writes {
		if w.Error != "" || w.CompletedNS < w.StartedNS || w.StartedNS < w.ConnectedNS || w.ConnectedNS <= 0 || w.PlannedBytes <= 0 || w.WrittenBytes != w.PlannedBytes {
			return fmt.Errorf("slow request stream incomplete: %+v", w)
		}
	}
	return nil
}

func g2CompetitionPeekResponse(c *net.TCPConn, socket int, deadline time.Time) (x g2CompetitionResponseHeader, err error) {
	x.Socket, x.StartedNS = socket, resourceNS()
	defer func() {
		x.CompletedNS = resourceNS()
		if !resourceOrigin.Add(time.Duration(x.CompletedNS)).Before(deadline) {
			err = errors.Join(err, context.DeadlineExceeded)
		}
		if err != nil {
			x.Error = err.Error()
		}
	}()
	if err = c.SetReadDeadline(deadline); err != nil {
		return x, err
	}
	buf := make([]byte, g2CompetitionHeaderCap)
	n, err := g2CompetitionPeekSocket(c, buf)
	x.PeekedBytes = n
	if n > 0 {
		var parseErr error
		x.HeaderRaw, x.Status, x.Complete, parseErr = g2CompetitionParseHeader(buf[:n])
		err = errors.Join(err, parseErr)
	} else if err == nil {
		err = io.EOF
	}
	return x, err
}

// Two first-response peeks finish before the third observer is admitted. The
// same absolute connection-origin deadline applies to both; no bytes are read
// off either socket and an incomplete first peek fails instead of retrying.
func g2CompetitionPeekHeaders(s *g2SlowScrapes, deadline time.Time) ([]g2CompetitionResponseHeader, error) {
	if len(s.conns) != 2 {
		return nil, fmt.Errorf("expected exactly two slow sockets")
	}
	result := make([]g2CompetitionResponseHeader, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, c := range s.conns {
		wg.Add(1)
		go func(i int, c *net.TCPConn) {
			defer wg.Done()
			result[i], errs[i] = g2CompetitionPeekResponse(c, i, deadline)
		}(i, c)
	}
	wg.Wait()
	return result, errors.Join(errs...)
}

// Set the receive capacity before connect advertises the initial TCP window.
// Shrinking it only after the handshake permits Linux to negotiate a much
// larger initial window and grow the peer's send buffer before the first
// supposedly slow response. The HTTP request/byte limits remain unchanged.
func g2CompetitionDialer() *net.Dialer {
	return &net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		if err := raw.Control(func(fd uintptr) { socketErr = g2CompetitionSocketBuffer(fd) }); err != nil {
			return err
		}
		return socketErr
	}}
}

// The persistence interval is an input-selection rule, not an exporter SLO.
// It consumes the existing connection-origin one-second window. A 200 resets
// only this candidate; it never restarts sockets, requests, or the deadline.
const g2CompetitionRejectionPersistence = 40 * time.Millisecond

type g2CompetitionInputObservation struct {
	Status      int   `json:"status"`
	StartedNS   int64 `json:"started_ns"`
	CompletedNS int64 `json:"completed_ns"`
}

type g2CompetitionRejectionGate struct {
	deadlineNS       int64
	consecutive      int
	firstCompletedNS int64
	lastCompletedNS  int64
	observations     []g2CompetitionInputObservation
}

func (g *g2CompetitionRejectionGate) observe(x g2Exposition) (bool, error) {
	if len(g.observations) >= 128 {
		return false, fmt.Errorf("export input observation capacity exhausted")
	}
	if x.StartedNS <= 0 || x.CompletedNS < x.StartedNS || x.StartedNS <= g.lastCompletedNS || x.CompletedNS > g.deadlineNS {
		return false, fmt.Errorf("export input observation is duplicate, overlapping, or incomplete")
	}
	if x.Status != http.StatusOK && x.Status != http.StatusTooManyRequests {
		return false, fmt.Errorf("unexpected exporter input status %d", x.Status)
	}
	g.observations = append(g.observations, g2CompetitionInputObservation{Status: x.Status, StartedNS: x.StartedNS, CompletedNS: x.CompletedNS})
	g.lastCompletedNS = x.CompletedNS
	if x.Status == http.StatusOK {
		g.firstCompletedNS = 0
		g.consecutive = 0
		return false, nil
	}
	g.consecutive++
	if g.firstCompletedNS == 0 {
		g.firstCompletedNS = x.CompletedNS
		return false, nil
	}
	// Use the previous response's completion and the final request's start:
	// time spent waiting for one observer response is not a stable interval.
	return g.consecutive >= 5 && x.StartedNS-g.firstCompletedNS >= int64(g2CompetitionRejectionPersistence), nil
}

func TestG2CompetitionRejectionPersistence(t *testing.T) {
	ms := int64(time.Millisecond)
	sample := func(status int, start, end int64) g2Exposition {
		return g2Exposition{Status: status, StartedNS: start * ms, CompletedNS: end * ms}
	}
	t.Run("transient_429_then_200", func(t *testing.T) {
		gate := g2CompetitionRejectionGate{deadlineNS: int64(time.Second)}
		for _, x := range []g2Exposition{sample(429, 1, 2), sample(200, 60, 61), sample(429, 70, 71), sample(429, 80, 81), sample(429, 90, 91), sample(429, 100, 101)} {
			if accepted, err := gate.observe(x); err != nil || accepted {
				t.Fatalf("transient or reset candidate advanced critical work: %v %v", accepted, err)
			}
		}
		if accepted, err := gate.observe(sample(429, 111, 112)); err != nil || !accepted {
			t.Fatalf("actual new interval not accepted: %v %v", accepted, err)
		}
	})
	t.Run("single_slow_observer_is_not_persistence", func(t *testing.T) {
		gate := g2CompetitionRejectionGate{deadlineNS: int64(time.Second)}
		for _, x := range []g2Exposition{sample(429, 1, 100), sample(429, 101, 150)} {
			if accepted, err := gate.observe(x); err != nil || accepted {
				t.Fatalf("observer elapsed time used as persistent occupancy: %v %v", accepted, err)
			}
		}
	})
	t.Run("persistence_boundary", func(t *testing.T) {
		gate := g2CompetitionRejectionGate{deadlineNS: int64(time.Second)}
		for _, start := range []int64{1, 11, 21, 31, 41} {
			if accepted, err := gate.observe(sample(429, start, start+1)); err != nil || accepted {
				t.Fatalf("39ms interval passed: %v %v", accepted, err)
			}
		}
		if accepted, err := gate.observe(sample(429, 43, 44)); err != nil || !accepted {
			t.Fatalf("completed interval rejected: %v %v", accepted, err)
		}
	})
	for _, bad := range []string{"duplicate", "overlap", "unfinished", "unexpected_status", "overflow", "after_deadline"} {
		t.Run(bad, func(t *testing.T) {
			gate := g2CompetitionRejectionGate{deadlineNS: int64(time.Second)}
			if _, err := gate.observe(sample(429, 1, 2)); err != nil {
				t.Fatal(err)
			}
			x := sample(429, 3, 4)
			switch bad {
			case "duplicate":
				x = sample(429, 1, 2)
			case "overlap":
				x = sample(429, 2, 4)
			case "unfinished":
				x = sample(429, 3, 0)
			case "unexpected_status":
				x.Status = 503
			case "overflow":
				gate.observations = make([]g2CompetitionInputObservation, 128)
			case "after_deadline":
				x = sample(429, 999, 1001)
			}
			if accepted, err := gate.observe(x); err == nil || accepted {
				t.Fatalf("invalid observation accepted: %v %v", accepted, err)
			}
		})
	}
}

func TestG2CompetitionQueryPhase(t *testing.T) {
	now := int64(time.Second)
	events := []g2WireEvent{}
	for i := range 8 {
		id := fmt.Sprintf("%032x", i+1)
		raw, err := json.Marshal(protocol.ObservationQuery{QueryID: id, DeadlineMS: 499,
			Binding: protocol.ObservationBinding{ExposureID: fmt.Sprintf("lab-a/%d", i)}})
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, g2WireEvent{Type: protocol.MsgObservationQuery, Direction: g2OISToOIC,
			Raw: raw, ReceivedNS: now - int64(50*time.Millisecond), WrittenNS: now - int64(49*time.Millisecond)})
	}
	if !g2CompetitionYoungQueries(events, now, 200*time.Millisecond) {
		t.Fatal("actual young batch rejected")
	}
	for _, change := range []string{"old", "duplicate", "injected", "unwritten", "future", "spent_deadline", "same_source"} {
		t.Run(change, func(t *testing.T) {
			bad := append([]g2WireEvent(nil), events...)
			switch change {
			case "old":
				bad[0].ReceivedNS = now - int64(500*time.Millisecond)
			case "duplicate":
				bad[0].Raw = bad[1].Raw
			case "injected":
				bad[0].Injected = true
			case "unwritten":
				bad[0].WrittenNS = 0
			case "future":
				bad[0].ReceivedNS = now + 1
			case "spent_deadline", "same_source":
				var q protocol.ObservationQuery
				if err := json.Unmarshal(bad[0].Raw, &q); err != nil {
					t.Fatal(err)
				}
				if change == "spent_deadline" {
					q.DeadlineMS = 100
				} else {
					q.Binding.ExposureID = "lab-a/1"
				}
				bad[0].Raw = must(json.Marshal(q))
			}
			if g2CompetitionYoungQueries(bad, now, 200*time.Millisecond) {
				t.Fatal("invalid input was counted as an actual young query")
			}
		})
	}
}

func TestG2CompetitionSlowInput(t *testing.T) {
	registry := &metrics.Registry{}
	server := httptest.NewServer(metrics.NewHandler(registry))
	defer server.Close()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	hc := &http.Client{Transport: transport}
	addr := server.Listener.Addr().String()
	before := g2ExporterRead(t, hc, addr)
	slow, err := g2BeginSlowScrapesWithDialer(context.Background(), addr, g2CompetitionDialer())
	if err != nil {
		t.Fatal(err)
	}
	var lastScrape g2Exposition
	defer func() {
		t.Logf("actual_slow_writes=%s last_scrape=%s", must(json.Marshal(slow.Snapshot())), must(json.Marshal(lastScrape)))
		if err := slow.Close(); err != nil {
			t.Error(err)
		}
	}()
	connected := slow.Snapshot()
	if len(connected) != 2 {
		t.Fatalf("slow sockets=%d", len(connected))
	}
	d := resourceOrigin.Add(time.Duration(min(connected[0].ConnectedNS, connected[1].ConnectedNS)) + time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), d)
	defer cancel()
	if err := g2CompetitionAwaitWrites(slow, d); err != nil {
		t.Fatal(err)
	}
	headers, err := g2CompetitionPeekHeaders(slow, d)
	if err != nil {
		t.Fatalf("first response header input invalid: %v raw=%s", err, must(json.Marshal(headers)))
	}
	t.Logf("actual_slow_response_headers=%s", must(json.Marshal(headers)))
	rejected, failed := false, false
	gate := g2CompetitionRejectionGate{deadlineNS: int64(d.Sub(resourceOrigin))}
	for ctx.Err() == nil {
		x, err := g2ReadExposition(ctx, hc, addr)
		lastScrape = x
		if err != nil {
			t.Fatal(err)
		}
		if !rejected {
			rejected, err = gate.observe(x)
			if err != nil {
				t.Fatal(err)
			}
		}
		switch x.Status {
		case http.StatusTooManyRequests:
		case http.StatusOK:
			if g2CompetitionExportFailures(x)-g2CompetitionExportFailures(before) >= 2 {
				failed = true
			}
		default:
			t.Fatalf("unexpected actual exporter response %d", x.Status)
		}
		if rejected && failed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !rejected || !failed {
		t.Fatalf("bounded input did not establish persistent rejection and two real blocked exporters before close: rejected=%v failures=%v observations=%+v", rejected, failed, gate.observations)
	}
	t.Logf("actual_input_observations=%s", must(json.Marshal(gate.observations)))
	writes := slow.Snapshot()
	if len(writes) != 2 {
		t.Fatalf("slow sockets=%d", len(writes))
	}
	for _, w := range writes {
		if w.Error != "" || w.WrittenBytes != w.PlannedBytes || w.PlannedBytes <= 0 || w.CompletedNS < w.StartedNS || w.CompletedNS-w.StartedNS > int64(time.Second) {
			t.Fatalf("bounded request stream not actually sent: %+v", w)
		}
	}
	if err := slow.Close(); err != nil {
		t.Fatal(err)
	}
	_ = g2ExporterRead(t, hc, addr)
}

func TestG2CompetitionResponseHeader(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		status    int
		complete  bool
		valid     bool
	}{
		{"complete_200", "HTTP/1.1 200 OK\r\nContent-Length: 3\r\n\r\nabc", 200, true, true},
		{"first_response_not_later_200", "HTTP/1.1 429 Too Many Requests\r\nContent-Length: 0\r\n\r\nHTTP/1.1 200 OK\r\n\r\n", 429, true, false},
		{"incomplete_200_status_line", "HTTP/1.1 200 OK\r\nContent-Length: 3\r\n", 0, false, false},
		{"malformed_status", "HTTP/1.1 TWO OK\r\n\r\n", 0, true, false},
		{"malformed_header", "HTTP/1.1 200 OK\r\nbad header\r\n\r\n", 0, true, false},
		{"wrong_protocol", "HTTP/2.0 200 OK\r\n\r\n", 200, true, false},
		{"unsupported_http_minor", "HTTP/1.2 200 OK\r\n\r\n", 200, true, false},
		{"capacity_without_delimiter", strings.Repeat("x", g2CompetitionHeaderCap), 0, false, false},
		{"over_capacity", strings.Repeat("x", g2CompetitionHeaderCap+1), 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.raw)
			header, status, complete, err := g2CompetitionParseHeader(raw)
			if status != tc.status || complete != tc.complete || (err == nil) != tc.valid || len(header) > g2CompetitionHeaderCap {
				t.Fatalf("parsed status=%d complete=%v error=%v bytes=%d", status, complete, err, len(header))
			}
			if complete && !bytes.HasSuffix(header, []byte("\r\n\r\n")) {
				t.Fatal("complete header lost its original terminator")
			}
			if len(header) > 0 {
				old := header[0]
				raw[0] ^= 0xff
				if header[0] != old {
					t.Fatal("raw evidence aliases caller storage")
				}
			}
		})
	}
}

func g2CompetitionTCPPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := net.DialTCP("tcp", nil, ln.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	peer, err := ln.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return c, peer
}

func TestG2CompetitionHeaderPeek(t *testing.T) {
	for _, scenario := range []string{"complete_200_is_not_consumed", "first_non_200", "incomplete_first_read", "closed_without_header", "absolute_deadline", "already_expired"} {
		t.Run(scenario, func(t *testing.T) {
			c, peer := g2CompetitionTCPPair(t)
			raw := []byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nbody")
			deadline := time.Now().Add(time.Second)
			switch scenario {
			case "first_non_200":
				raw = []byte("HTTP/1.1 429 Too Many Requests\r\nContent-Length: 0\r\n\r\n")
			case "incomplete_first_read":
				// The peer sends no second fragment before the peek returns.
				raw = []byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n")
			case "closed_without_header":
				raw = nil
				_ = peer.Close()
			case "absolute_deadline":
				raw = nil
				deadline = time.Now().Add(30 * time.Millisecond)
			case "already_expired":
				deadline = time.Now().Add(-time.Millisecond)
			}
			if len(raw) != 0 {
				if n, err := peer.Write(raw); err != nil || n != len(raw) {
					t.Fatalf("fixture header write %d %v", n, err)
				}
			}
			x, err := g2CompetitionPeekResponse(c, 1, deadline)
			valid := scenario == "complete_200_is_not_consumed"
			if (err == nil) != valid || x.Socket != 1 || x.StartedNS <= 0 || x.CompletedNS < x.StartedNS || (x.Error == "") != valid {
				t.Fatalf("peek result %+v err=%v", x, err)
			}
			if valid && (!x.Complete || x.Status != 200 || !bytes.Equal(x.HeaderRaw, raw[:len(raw)-4])) {
				t.Fatalf("complete original header not retained: %+v", x)
			}
			if scenario == "incomplete_first_read" && (x.Complete || x.Status != 0 || !bytes.Equal(x.HeaderRaw, raw)) {
				t.Fatalf("partial 200 became accepted input: %+v", x)
			}
			if len(raw) != 0 {
				if err := c.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				readback := make([]byte, len(raw))
				if _, err := io.ReadFull(c, readback); err != nil || !bytes.Equal(readback, raw) {
					t.Fatalf("peek consumed or changed bytes: %q %v", readback, err)
				}
			}
		})
	}
	t.Run("two_sockets_join_with_one_timeout", func(t *testing.T) {
		first, peer := g2CompetitionTCPPair(t)
		second, _ := g2CompetitionTCPPair(t)
		raw := []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
		if _, err := peer.Write(raw); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(30 * time.Millisecond)
		got, err := g2CompetitionPeekHeaders(&g2SlowScrapes{conns: []*net.TCPConn{first, second}}, deadline)
		if err == nil || len(got) != 2 || got[0].Status != 200 || got[0].Error != "" || got[1].Error == "" || got[1].CompletedNS < int64(deadline.Sub(resourceOrigin)) {
			t.Fatalf("two actual completed observations absent: %+v %v", got, err)
		}
		// The aggregate returned only after both workers wrote their evidence.
		if got[0].Socket != 0 || got[1].Socket != 1 || !bytes.Equal(got[0].HeaderRaw, raw) {
			t.Fatalf("socket/header association changed: %+v", got)
		}
	})
}

// Directory data only selects an input phase. It never substitutes for the
// actual preflight/final decision, and elapsed Lookup/selection time consumes
// the observed sample rather than granting it a new lifetime.
func g2CompetitionVictimEligible(v protocol.ExposureView, started, completed, selected, deadline int64) (int64, bool) {
	if started <= 0 || completed < started || selected < completed || selected >= deadline ||
		v.ExposureID != "lab-a/http" || v.AccessID != "lab-a" || !v.AccessOnline || v.Readiness != "ready" {
		return 0, false
	}
	r := v.ReadinessView
	if r == nil || r.Authority != "directory_snapshot" || r.Source != "http_status_v1" ||
		r.Revision != v.Revision || !protocol.ValidRevision(r.Revision) || r.Generation <= 0 ||
		!protocol.ValidObservationRevision(r.ObservationRevision) || r.SampleSeq == nil || *r.SampleSeq == 0 ||
		r.Reason != "matched_ready_status" || len(r.ContractDigest) != 64 || strings.ToLower(r.ContractDigest) != r.ContractDigest ||
		r.RemainingMS < 500 || r.RemainingMS > int64((1<<63-1)/time.Millisecond) {
		return 0, false
	}
	if _, err := hex.DecodeString(r.ContractDigest); err != nil {
		return 0, false
	}
	// Authority is lost once the integer remaining value reaches zero.
	// Its earliest end is one millisecond before the encoded floor boundary.
	remaining := max(int64(0), (r.RemainingMS-1)*int64(time.Millisecond)-(selected-started))
	return remaining, remaining >= int64(500*time.Millisecond)
}

func TestG2CompetitionVictimInputEligibility(t *testing.T) {
	seq := uint64(3)
	v := protocol.ExposureView{ExposureID: "lab-a/http", AccessID: "lab-a", AccessOnline: true, Readiness: "ready", Revision: protocol.NewID("rv_"),
		ReadinessView: &protocol.ReadinessView{Authority: "directory_snapshot", Source: "http_status_v1", Generation: 1, ObservationRevision: "ob_1",
			ContractDigest: strings.Repeat("a", 64), SampleSeq: &seq, Reason: "matched_ready_status", RemainingMS: 502}}
	v.ReadinessView.Revision = v.Revision
	ms := int64(time.Millisecond)
	for _, scenario := range []string{"exact_500ms", "rtt_spends_budget", "selection_spends_budget", "deadline_expired", "unknown_current", "historical_only", "wrong_binding", "missing_sample", "incomplete_view", "running_keeps_valid_sample"} {
		t.Run(scenario, func(t *testing.T) {
			copyView := v
			readiness := *v.ReadinessView
			copyView.ReadinessView = &readiness
			start, end, selected, deadline := ms, 2*ms, 2*ms, 3000*ms
			want := scenario == "exact_500ms" || scenario == "running_keeps_valid_sample"
			switch scenario {
			case "rtt_spends_budget":
				readiness.RemainingMS = 501
			case "selection_spends_budget":
				selected = 3 * ms
			case "deadline_expired":
				deadline = selected
			case "unknown_current":
				copyView.Readiness = "unknown"
			case "historical_only":
				readiness.Authority = "last_reported"
			case "wrong_binding":
				readiness.Revision = protocol.NewID("rv_")
			case "missing_sample":
				readiness.SampleSeq = nil
			case "incomplete_view":
				readiness.ContractDigest = ""
			case "running_keeps_valid_sample":
				readiness.LastAttempt = protocol.ObservationAttempt{Seq: 4, State: "running"}
			}
			remaining, ok := g2CompetitionVictimEligible(copyView, start, end, selected, deadline)
			if ok != want || (ok && remaining != 500*ms) {
				t.Fatalf("eligibility=%v remaining=%d want=%v", ok, remaining, want)
			}
		})
	}
}
