package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

// These are real product paths; the fixture changes only the health service.
// Directory state is observed separately from the two admission checks.
func TestTVG2_01_AdmissionMatrix(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-01", g2Options{proxy: true, probes: []string{"lab-a/http", "lab-a/alt"}})
		for _, state := range []string{protocol.ReadinessReady, protocol.ReadinessNotReady, protocol.ReadinessUnknown} {
			status, hold := 200, false
			if state == protocol.ReadinessNotReady {
				status = 503
			}
			if state == protocol.ReadinessUnknown {
				hold = true
			}
			for id, h := range g.health {
				h.Set(status, hold)
				g.waitView(id, func(v protocol.ExposureView) bool { return v.Readiness == state })
			}
			for _, exposure := range []string{"lab-a/http", "lab-a/alt"} {
				for _, requestMode := range []string{protocol.AdmissionBestEffort, protocol.AdmissionRequireReady} {
					name := fmt.Sprintf("%s/%s/%s", state, exposure, requestMode)
					t.Run(name, func(t *testing.T) {
						v := g.view(exposure)
						effective := requestMode
						if v.AdmissionMode == protocol.AdmissionRequireReady {
							effective = protocol.AdmissionRequireReady
						}
						allowed := state == protocol.ReadinessReady || (state == protocol.ReadinessUnknown && effective == protocol.AdmissionBestEffort)
						requestField := requestMode
						if requestMode == protocol.AdmissionBestEffort {
							requestField = ""
						}
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						o, err := g.consumer("alice").Open(ctx, protocol.OpenRequest{ExposureID: exposure, ExpectedRevision: v.Revision, Admission: requestField})
						g.event("admission_matrix", map[string]any{"directory": v, "request_admission": requestMode, "effective_admission": effective, "expected_allowed": allowed, "open": o, "error": fmt.Sprint(err)})
						if !allowed {
							var api *protocol.Error
							want := protocol.CodeReadinessRequired
							if state == protocol.ReadinessNotReady {
								want = protocol.CodeTargetNotReady
							}
							if !errors.As(err, &api) || api.Code != want {
								t.Fatalf("rejected matrix: got %v", err)
							}
							return
						}
						if err != nil {
							t.Fatal(err)
						}
						if o.AdmissionMode != effective || o.ReadinessStage != "preflight" {
							t.Fatalf("wrong Open decision: %+v", o)
						}
						s, err := g.consumer("alice").Attach(ctx, o)
						if err != nil {
							t.Fatal(err)
						}
						defer s.Close()
						payload := "g2-matrix-" + protocol.NewID("")
						if _, err := io.WriteString(s, payload); err != nil {
							t.Fatal(err)
						}
						if err := s.CloseWrite(); err != nil {
							t.Fatal(err)
						}
						body, err := io.ReadAll(s)
						want := "T1:" + payload
						if exposure == "lab-a/alt" {
							want = "T3:" + payload
						}
						if err != nil || string(body) != want {
							t.Fatalf("native result %q: %v", body, err)
						}
						cv := g.closedState("alice", o.ChannelID)
						if cv.ReadinessDecision == nil || cv.ReadinessDecision.EffectiveAdmission != effective {
							t.Fatalf("missing final decision: %+v", cv)
						}
						g.assertNoProbeSecret(cv)
						g.event("matrix_business_result", map[string]any{"channel": cv, "request": payload, "response": string(body)})
					})
				}
			}
		}
		g.settled()
		for id := range g.health {
			g.recordHealth(id)
		}
		for access, p := range g.paths {
			s := p.Snapshot()
			if s.Overflow || s.Failure != "" {
				t.Fatalf("proxy failure: %+v", s)
			}
			g.event("wire_history", map[string]any{"access_id": access, "proxy": s})
		}
	})
}

func TestTVG2_02_AttemptDoesNotReplaceSample(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-02", g2Options{proxy: true})
		for _, state := range []string{protocol.ReadinessReady, protocol.ReadinessNotReady} {
			old := g.view("lab-a/http")
			status := 200
			if state == protocol.ReadinessNotReady {
				status = 503
			}
			g.health["lab-a/http"].Set(status, false)
			v := g.waitView("lab-a/http", func(v protocol.ExposureView) bool {
				return v.Readiness == state && v.ReadinessView != nil && v.ReadinessView.SampleSeq != nil &&
					(old.ReadinessView.SampleSeq == nil || *v.ReadinessView.SampleSeq > *old.ReadinessView.SampleSeq)
			})
			seq, remaining := *v.ReadinessView.SampleSeq, v.ReadinessView.RemainingMS
			before := resourceNS()
			g.health["lab-a/http"].Set(status, true)
			running := g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.ReadinessView.LastAttempt.State == "running" })
			check := func(v protocol.ExposureView, stage string) {
				if v.Readiness != state || v.ReadinessView.SampleSeq == nil || *v.ReadinessView.SampleSeq != seq || v.ReadinessView.RemainingMS > remaining {
					t.Fatalf("%s replaced/renewed old sample: %+v", stage, v)
				}
				g.event("sample_retained", map[string]any{"stage": stage, "expected_readiness": state, "sample_seq": seq, "initial_remaining_ms": remaining, "view": v})
			}
			check(running, "running")
			timed := g.waitView("lab-a/http", func(v protocol.ExposureView) bool {
				return v.ReadinessView.LastAttempt.State == "idle" && v.ReadinessView.LastAttempt.Outcome == "timeout"
			})
			check(timed, "timeout")
			expired := g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessUnknown })
			elapsed := time.Duration(resourceNS() - before)
			if expired.ReadinessView.SampleSeq != nil || expired.ReadinessView.RemainingMS != 0 || expired.ReadinessView.LastReported == nil || expired.ReadinessView.LastReported.SampleSeq != seq || expired.ReadinessView.LastReported.Readiness != state {
				t.Fatalf("expired evidence lost history or retained authority: %+v", expired)
			}
			if elapsed > time.Duration(remaining)*time.Millisecond+250*time.Millisecond {
				t.Fatalf("sample lifetime extended: %v > %dms + polling budget", elapsed, remaining)
			}
			g.event("sample_expired", map[string]any{"expected_readiness": state, "sample_seq": seq, "elapsed_ns": elapsed.Nanoseconds(), "initial_remaining_ms": remaining, "view": expired})
		}
		g.settled()
		g.recordHealth("lab-a/http")
	})
}

func TestTVG2_09_IndependentPublication(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-09", g2Options{proxy: true, probes: []string{"lab-a/http", "lab-a/slow"}})
		for round := range 20 {
			slow := g.health["lab-a/slow"]
			slowVersion := slow.Set(200, true)
			status, want := 503, protocol.ReadinessNotReady
			if round%2 == 1 {
				status, want = 200, protocol.ReadinessReady
			}
			fastVersion := g.health["lab-a/http"].Set(status, true)
			// Rebind both test-owned URLs in one real configuration update.
			// This gives the two workers the same new scheduling origin; the
			// actual URI at each handler proves reload occurred, not a guessed tick.
			uri := fmt.Sprintf("/g2-publication/%d", round)
			for _, raw := range g.oics["lab-a"].cfg["exposures"].([]any) {
				m := raw.(map[string]any)
				id := "lab-a/" + m["name"].(string)
				if h := g.health[id]; h != nil {
					m["probe"].(map[string]any)["url"] = h.URL() + uri
				}
			}
			writeJSON(t, g.oics["lab-a"].path, g.oics["lab-a"].cfg)
			var blocked fixture.HealthRecord
			g.eventually(5*time.Second, "both actual health requests held within original 200ms budgets", func() bool {
				blocked = fixture.HealthRecord{}
				for _, r := range slow.Snapshot().Records {
					if r.Version == slowVersion && r.RequestURI == uri && r.FinishedNS == 0 && r.HeaderStartedNS == 0 && resourceNS()-r.StartedNS < int64(100*time.Millisecond) {
						blocked = r
					}
				}
				if blocked.ID == 0 {
					return false
				}
				for _, r := range g.health["lab-a/http"].Snapshot().Records {
					if r.Version == fastVersion && r.RequestURI == uri && r.FinishedNS == 0 && r.HeaderStartedNS == 0 && resourceNS()-r.StartedNS < int64(100*time.Millisecond) {
						return true
					}
				}
				return false
			})
			g.health["lab-a/http"].Release()
			v := g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == want })
			visible := resourceNS()
			var completed fixture.HealthRecord
			for _, r := range g.health["lab-a/http"].Snapshot().Records {
				if r.Version == fastVersion && r.HeaderStartedNS > 0 {
					completed = r
					break
				}
			}
			if completed.ID == 0 || completed.Status != status {
				t.Fatal("directory changed without corresponding real health response")
			}
			var slowAtVisibility fixture.HealthRecord
			for _, r := range slow.Snapshot().Records {
				if r.ID == blocked.ID {
					slowAtVisibility = r
				}
			}
			if slowAtVisibility.ID == 0 || slowAtVisibility.FinishedNS != 0 {
				t.Fatal("slow response finished before independent publication")
			}
			upper := time.Duration(visible - completed.HeaderStartedNS)
			if upper < 0 || upper > 500*time.Millisecond {
				t.Fatalf("conservative publication upper bound %v", upper)
			}
			g.event("independent_publication", map[string]any{"round": round, "slow": slowAtVisibility, "completed": completed, "visible_ns": visible, "publication_upper_bound_ns": upper.Nanoseconds(), "bound_origin": "health_header_started_before_oic_reduction", "view": v})
			slow.Release()
			g.eventually(time.Second, "held request finished after publication", func() bool {
				for _, r := range slow.Snapshot().Records {
					if r.ID == blocked.ID {
						return r.FinishedNS > 0 && r.FinishedNS >= visible
					}
				}
				return false
			})
		}
		g.settled()
		for id := range g.health {
			g.recordHealth(id)
		}
	})
}

func TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-14", g2Options{proxy: true})
		for _, state := range []string{protocol.ReadinessReady, protocol.ReadinessUnknown} {
			if state == protocol.ReadinessUnknown {
				g.health["lab-a/http"].Set(200, true)
			}
			v := g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == state })
			if v.ReadinessView == nil || v.ReadinessView.Authority != "directory_snapshot" {
				t.Fatalf("directory authority missing: %+v", v)
			}
			if state == protocol.ReadinessUnknown {
				if v.ReadinessView.LastReported == nil || v.ReadinessView.LastReported.Readiness != protocol.ReadinessReady {
					t.Fatal("timeout/expired history disappeared")
				}
				_, err := g.consumer("alice").Open(bg, protocol.OpenRequest{ExposureID: v.ExposureID, ExpectedRevision: v.Revision, Admission: protocol.AdmissionRequireReady})
				var pe *protocol.Error
				if !errors.As(err, &pe) || pe.Code != protocol.CodeReadinessRequired {
					t.Fatalf("strict expired result: %v", err)
				}
				g.event("strict_expired_rejected", map[string]any{"directory": v, "code": pe.Code, "reason": pe.Reason})
			}
			targetBefore := len(g.t1.Conns())
			start := resourceNS()
			o, err := g.consumer("alice").Open(bg, protocol.OpenRequest{ExposureID: v.ExposureID, ExpectedRevision: v.Revision})
			if err != nil {
				t.Fatal(err)
			}
			opened := resourceNS()
			if o.ReadinessStage != "preflight" || o.Readiness != state {
				t.Fatalf("preflight report: %+v", o)
			}
			s, err := g.consumer("alice").Attach(bg, o)
			if err != nil {
				t.Fatal(err)
			}
			ready := resourceNS()
			if _, err = io.WriteString(s, "reset"); err != nil {
				t.Fatal(err)
			}
			if err = s.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			first := make([]byte, 1)
			n, firstErr := s.Read(first)
			firstAt := resourceNS()
			rest, businessErr := io.ReadAll(s)
			_ = s.Close()
			if firstErr != nil && firstErr != io.EOF {
				businessErr = firstErr
			}
			var abort *client.AbortError
			if !errors.As(businessErr, &abort) {
				t.Fatalf("READY did not expose native reset as failure: %d+%d %v", n, len(rest), businessErr)
			}
			cv := g.closedState("alice", o.ChannelID)
			if cv.ReadinessDecision == nil || cv.ReadinessDecision.Readiness != state || cv.ReadinessDecision.EffectiveAdmission != protocol.AdmissionBestEffort {
				t.Fatalf("final admission lost its meaning: %+v", cv)
			}
			g.assertNoProbeSecret([]any{v, o, cv})
			response := append(first[:n:n], rest...)
			targets := g.t1.Conns()[targetBefore:]
			if string(response) != g.t1.Name+":part" || len(targets) != 1 || string(targets[0].Data) != "reset" || !targets[0].EOF || !targets[0].Done {
				t.Fatalf("native partial response/target mismatch: response=%q target=%+v", response, targets)
			}
			g.event("diagnostic_layers", map[string]any{"directory": v, "preflight": o, "final": cv, "business_outcome": "incomplete_unknown", "business_error": abort.Code, "request": "reset", "response_raw": response, "target_record": targets[0], "target_name": g.t1.Name, "response_bytes": n + len(rest), "open_started_ns": start, "open_returned_ns": opened, "ready_received_ns": ready, "first_read_ns": firstAt, "finished_ns": resourceNS(), "o1_group": map[string]any{"admission": o.AdmissionMode, "readiness": state, "result": abort.Code}})
		}
		g.settled()
		g.recordHealth("lab-a/http")
	})
}
