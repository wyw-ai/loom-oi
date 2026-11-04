package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	slow, err := g2BeginSlowScrapesWithDialer(ctx, addr, g2CompetitionDialer())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := slow.Close(); err != nil {
			t.Error(err)
		}
	}()
	rejected, failed := false, false
	d, _ := ctx.Deadline()
	gate := g2CompetitionRejectionGate{deadlineNS: int64(d.Sub(resourceOrigin))}
	for ctx.Err() == nil {
		x, err := g2ReadExposition(ctx, hc, addr)
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
