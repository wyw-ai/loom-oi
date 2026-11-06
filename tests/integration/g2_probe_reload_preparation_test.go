package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wyw-ai/loom-oi/internal/oic/config"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

func g2ReloadTestConfig(t *testing.T) (map[string]any, []byte, map[string]string) {
	t.Helper()
	health := map[string]string{}
	exps := []any{}
	for i, id := range g2CompetitionSourceIDs() {
		endpoint := fmt.Sprintf("http://127.0.0.1:%d", 16000+i)
		health[id] = endpoint
		e := exposure(strings.TrimPrefix(id, "lab-a/"), "127.0.0.1:17000", protocol.AdmissionBestEffort, nil)
		e["probe"] = map[string]any{"kind": "http_status_v1", "url": endpoint, "allowed_cidrs": []string{"127.0.0.0/8"},
			"ready_statuses": []int{200}, "not_ready_statuses": []int{503}, "interval_ms": 500, "timeout_ms": 200, "validity_ms": 1500, "jitter_ms": 50}
		exps = append(exps, e)
	}
	cfg := map[string]any{"access_id": "lab-a", "ois": map[string]any{"url": "wss://127.0.0.1:19000", "ca_file": "fixture-ca"},
		"tls": map[string]any{"cert_file": "fixture-cert", "key_file": "fixture-key"}, "exposures": exps}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return cfg, b, health
}

func g2ReloadTestInput(t *testing.T) (*g2PreparationFailure, g2ProxySnapshot, *g2ReloadConfig) {
	t.Helper()
	cfg, b, health := g2ReloadTestConfig(t)
	c, err := g2ReloadConfigPlan(cfg, b, t.TempDir(), "/g2-competition-reload/0", health)
	if err != nil {
		t.Fatal(err)
	}
	d, s, base := g2ProbeQueueInput(t)
	compiled, err := config.Parse(b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*config.Exposure{}
	for _, e := range compiled.Exposures {
		byID[e.ID] = e
	}
	g2EditProbeRegister(t, &s, func(r *protocol.Register) {
		for i := range r.Exposures {
			e := &r.Exposures[i]
			e.Revision = byID[e.ExposureID].Revision
			e.Binding.Revision = e.Revision
			e.Binding.ContractDigest = byID[e.ExposureID].ProbePlan().Digest()
		}
	})
	if !d.probeQueue(s, resourceNS(), base) || !d.returned(true, s, resourceNS()) || d.acceptedPreload() != nil {
		t.Fatal("original preload fixture")
	}
	d.active = true
	return d, s, c
}

func g2ReloadTestNext(t *testing.T, s g2ProxySnapshot, c *g2ReloadConfig) g2ProxySnapshot {
	t.Helper()
	s.Events = append([]g2WireEvent(nil), s.Events...)
	var r protocol.Register
	if err := json.Unmarshal(s.Events[0].Raw, &r); err != nil {
		t.Fatal(err)
	}
	for i := range r.Exposures {
		e := &r.Exposures[i]
		e.Binding.ObservationRevision = "ob_2"
		for _, p := range c.Probes {
			if p.ExposureID == e.ExposureID {
				e.Binding.ContractDigest = p.NewDigest
			}
		}
	}
	r.Seq++
	b, _ := json.Marshal(r)
	now := resourceNS()
	id := uint64(len(s.Events))
	s.Events = append(s.Events, g2WireEvent{ID: id + 1, ConnID: 9, Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgRegister, ReceivedNS: now, WriteStartedNS: now, WrittenNS: now, Raw: b})
	a := s.Events[1]
	a.ID = id + 2
	a.ReceivedNS = now
	a.WriteStartedNS = now
	a.WrittenNS = now
	a.Raw = append([]byte(nil), a.Raw...)
	s.Events = append(s.Events, a)
	return s
}

func g2ReloadTestActions(t *testing.T, d *g2PreparationFailure, s g2ProxySnapshot, c *g2ReloadConfig) (g2ReloadActions, *[]string) {
	t.Helper()
	calls := []string{}
	notified := false
	a := g2ReloadActions{NotifyMethod: "poll", Build: func() (*g2ReloadConfig, error) { calls = append(calls, "build"); return c, nil },
		Snapshot: func() g2ProxySnapshot {
			calls = append(calls, "snapshot")
			if notified {
				return g2ReloadTestNext(t, s, c)
			}
			return s
		},
		Write: func(b []byte) error {
			calls = append(calls, "write")
			if !reflect.DeepEqual(b, c.After) {
				t.Fatal("wrong actual write bytes")
			}
			return nil
		},
		Notify: func() error { calls = append(calls, "notify"); notified = true; return nil }}
	return a, &calls
}

func TestG2ProbeReloadConfig(t *testing.T) {
	for _, mode := range []string{"path_only", "alias_independence", "query", "fragment", "wrong_endpoint", "missing_source", "duplicate_source", "non_http", "tls_input", "file_drift", "other_path", "target_mutation", "permission_mutation", "timeout_mutation", "status_mutation", "runtime_mutation"} {
		t.Run(mode, func(t *testing.T) {
			cfg, b, health := g2ReloadTestConfig(t)
			before := append([]byte(nil), b...)
			path := "/g2-competition-reload/0"
			switch mode {
			case "query":
				cfg["exposures"].([]any)[0].(map[string]any)["probe"].(map[string]any)["url"] = health["lab-a/http"] + "?x=1"
			case "fragment":
				cfg["exposures"].([]any)[0].(map[string]any)["probe"].(map[string]any)["url"] = health["lab-a/http"] + "#x"
			case "wrong_endpoint":
				health["lab-a/http"] = "http://127.0.0.1:22222"
			case "missing_source":
				delete(health, "lab-a/http")
			case "duplicate_source":
				xs := cfg["exposures"].([]any)
				xs[1] = xs[0]
			case "non_http":
				cfg["exposures"].([]any)[0].(map[string]any)["probe"].(map[string]any)["url"] = "https://127.0.0.1:16000"
				health["lab-a/http"] = "https://127.0.0.1:16000"
			case "tls_input":
				cfg["exposures"].([]any)[0].(map[string]any)["probe"].(map[string]any)["tls_ca_file"] = "not-read"
			case "other_path":
				path = "/another"
			}
			if mode != "file_drift" {
				b, _ = json.MarshalIndent(cfg, "", "  ")
			} else {
				cfg["diag_listen"] = "127.0.0.1:1234"
			}
			c, err := g2ReloadConfigPlan(cfg, b, t.TempDir(), path, health)
			bad := map[string]bool{"query": true, "fragment": true, "wrong_endpoint": true, "missing_source": true, "duplicate_source": true, "non_http": true, "tls_input": true, "file_drift": true, "other_path": true}[mode]
			if bad {
				if err == nil {
					t.Fatal("unsafe configuration accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Probes) != 16 || g2ReloadHash(c.Before) == g2ReloadHash(c.After) {
				t.Fatal("missing independent plans")
			}
			for _, p := range c.Probes {
				if g2ReloadHash(p.Old) != p.OldDigest || g2ReloadHash(p.New) != p.NewDigest || p.OldDigest == p.NewDigest {
					t.Fatal("canonical digest")
				}
			}
			if mode == "alias_independence" {
				c.Next["exposures"].([]any)[0].(map[string]any)["name"] = "later"
				fresh, _ := json.MarshalIndent(cfg, "", "  ")
				if !reflect.DeepEqual(fresh, b) || !reflect.DeepEqual(before, b) {
					t.Fatal("original configuration aliased")
				}
				return
			}
			if strings.HasSuffix(mode, "mutation") {
				var edited map[string]any
				_ = json.Unmarshal(c.After, &edited)
				e := edited["exposures"].([]any)[0].(map[string]any)
				switch mode {
				case "target_mutation":
					e["target"].(map[string]any)["address"] = "127.0.0.1:2"
				case "permission_mutation":
					e["target"].(map[string]any)["allowed_cidrs"] = []string{"0.0.0.0/0"}
				case "timeout_mutation":
					e["probe"].(map[string]any)["timeout_ms"] = 201
				case "status_mutation":
					e["probe"].(map[string]any)["ready_statuses"] = []int{201}
				case "runtime_mutation":
					edited["access_id"] = "lab-b"
				}
				mutated, _ := json.Marshal(edited)
				if g2ReloadOnlyPaths(c.Before, mutated, health, path) == nil {
					t.Fatal("whole-object comparator missed independent mutation")
				}
			}
		})
	}
}

func TestG2ProbeReloadTransition(t *testing.T) {
	for _, mode := range []string{"valid", "late_register_write_return", "unpublished_wait", "duplicate_register", "rejected", "duplicate_source", "same_ob", "skip_ob", "wrong_digest", "wrong_target_revision", "wrong_epoch", "wrong_generation", "wrong_access", "wrong_source", "wrong_contract", "other_control", "third_generation", "future_prefix", "gap_prefix", "overflow", "query_before_register_write", "old_and_new_drop", "thirty_two", "thirty_three", "old_query_late_response", "late_query_write_return"} {
		t.Run(mode, func(t *testing.T) {
			d, s, c := g2ReloadTestInput(t)
			a, _ := g2ReloadTestActions(t, d, s, c)
			calls := 0
			var final g2ProxySnapshot
			a.Snapshot = func() g2ProxySnapshot {
				calls++
				if calls == 1 {
					return s
				}
				x := g2ReloadTestNext(t, s, c)
				var reg protocol.Register
				_ = json.Unmarshal(x.Events[2].Raw, &reg)
				switch mode {
				case "late_register_write_return":
					x.Events[2].WrittenNS = resourceNS()
				case "unpublished_wait":
					if calls == 2 {
						x.Events[2].WrittenNS = 0
					}
				case "duplicate_register", "third_generation":
					e := x.Events[2]
					e.ID = 5
					x.Events = append(x.Events, e)
				case "rejected":
					var ack protocol.RegisterAck
					_ = json.Unmarshal(x.Events[3].Raw, &ack)
					ack.Accepted = ack.Accepted[:15]
					x.Events[3].Raw, _ = json.Marshal(ack)
				case "duplicate_source":
					reg.Exposures[1] = reg.Exposures[0]
				case "same_ob":
					reg.Exposures[0].Binding.ObservationRevision = "ob_1"
				case "skip_ob":
					reg.Exposures[0].Binding.ObservationRevision = "ob_3"
				case "wrong_digest":
					reg.Exposures[0].Binding.ContractDigest = strings.Repeat("b", 64)
				case "wrong_target_revision":
					reg.Exposures[0].Binding.Revision = "rv_" + strings.Repeat("b", 26)
					reg.Exposures[0].Revision = reg.Exposures[0].Binding.Revision
				case "wrong_epoch":
					reg.Exposures[0].Binding.Epoch++
				case "wrong_generation":
					reg.Exposures[0].Binding.Generation++
				case "wrong_access":
					reg.Exposures[0].Binding.AccessID = "lab-b"
				case "wrong_source":
					reg.Exposures[0].Binding.SourceID = "other"
				case "wrong_contract":
					reg.Exposures[0].Binding.ContractVersion = 2
				case "other_control":
					x.Events[2].ConnID = 11
				case "future_prefix":
					x.Events[2].ReceivedNS += int64(time.Second)
				case "gap_prefix":
					x.Events[2].ID = 20
				case "overflow":
					x.Overflow = true
				}
				x.Events[2].Raw, _ = json.Marshal(reg)
				if mode == "old_and_new_drop" || mode == "thirty_two" || mode == "thirty_three" || mode == "old_query_late_response" || mode == "late_query_write_return" || mode == "query_before_register_write" {
					count := 2
					if mode == "thirty_two" {
						count = 32
					}
					if mode == "thirty_three" {
						count = 33
					}
					for i := range count {
						b := *reg.Exposures[0].Binding
						if i == 0 {
							b = d.preloadQueueWitness.Bindings[b.ExposureID]
						}
						g2EarlyArmAppendDrop(t, &x, b, 9, resourceNS(), i+1)
					}
					if mode == "late_query_write_return" {
						x.Events[4].WrittenNS = 0
					}
					if mode == "query_before_register_write" {
						x.Events[6].ReceivedNS = x.Events[2].WriteStartedNS - 1
						x.Events[6].WriteStartedNS = x.Events[6].ReceivedNS
					}
					if mode == "old_query_late_response" {
						// The old query actually preceded the new Register, whereas its
						// response arrived afterwards; it still consumes the same cap.
						q := x.Events[4]
						q.ReceivedNS, q.WriteStartedNS, q.WrittenNS = s.Events[1].WrittenNS, s.Events[1].WrittenNS, s.Events[1].WrittenNS
						events := append([]g2WireEvent(nil), x.Events[:2]...)
						events = append(events, q)
						events = append(events, x.Events[2:4]...)
						x.Events = append(events, x.Events[5:]...)
						for i := range x.Events {
							x.Events[i].ID = uint64(i + 1)
						}
					}
				}
				final = x
				return x
			}
			ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(d.DeadlineNS)))
			defer cancel()
			err := d.reloadProbes(ctx, a)
			valid := mode == "valid" || mode == "late_register_write_return" || mode == "unpublished_wait" || mode == "old_and_new_drop" || mode == "thirty_two" || mode == "old_query_late_response" || mode == "late_query_write_return"
			if (err == nil) != valid {
				t.Fatal(mode, err)
			}
			w := d.probeReload
			if valid {
				if !w.Result || w.AcceptedNS == nil || w.BeforePrefix.LastEventID != 2 || !reflect.DeepEqual(w.AcceptedPrefix, w.LastPrefix) || len(w.NewBindings) != 16 {
					t.Fatal("incomplete transition witness")
				}
				if mode == "unpublished_wait" && w.ObservationAttempts != 2 {
					t.Fatal("future Write was backfilled")
				}
				if mode == "late_query_write_return" {
					if d.reloadHistory(final) == nil {
						t.Fatal("incomplete final query Write accepted")
					}
					// Completion in a later real final history is valid; the accepted
					// original Snapshot remains unchanged and was allowed Written=0.
					final.Events = append([]g2WireEvent(nil), final.Events...)
					final.Events[4].WrittenNS = resourceNS()
				}
				if d.reloadHistory(final) != nil {
					t.Fatal("valid complete history rejected")
				}
				frozen, _ := json.Marshal(w)
				final.Events[w.NewRegister.EventID-1].WrittenNS++
				if now, _ := json.Marshal(w); !reflect.DeepEqual(now, frozen) {
					t.Fatal("accepted projection aliases final wire")
				}
				if d.reloadProbes(ctx, a) == nil {
					t.Fatal("second request accepted")
				}
			} else if w.Result || w.AcceptedNS != nil || w.AcceptedPrefix != nil {
				t.Fatal("failure invented acceptance")
			}
		})
	}
}

func TestG2ProbeReloadFailureOwnership(t *testing.T) {
	for _, mode := range []string{"build_error", "write_error", "notify_error", "build_panic", "write_panic", "notify_panic", "build_goexit", "write_goexit", "notify_goexit", "snapshot_panic", "snapshot_goexit", "write_late", "notify_late", "accept_late", "missing_ack", "no_preload", "after_final_set", "expired"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				time.Sleep(time.Until(resourceOrigin.Add(time.Second)))
				d, s, c := g2ReloadTestInput(t)
				d.DeadlineNS = resourceNS() + int64(3*time.Second)
				a, calls := g2ReloadTestActions(t, d, s, c)
				sentinel := errors.New("original failure")
				switch mode {
				case "build_error":
					a.Build = func() (*g2ReloadConfig, error) { return nil, sentinel }
				case "write_error":
					a.Write = func([]byte) error { return sentinel }
				case "notify_error":
					a.Notify = func() error { return sentinel }
				case "build_panic":
					a.Build = func() (*g2ReloadConfig, error) { panic("build panic") }
				case "build_goexit":
					a.Build = func() (*g2ReloadConfig, error) { runtime.Goexit(); return nil, nil }
				case "write_goexit":
					a.Write = func([]byte) error { runtime.Goexit(); return nil }
				case "notify_goexit":
					a.Notify = func() error { runtime.Goexit(); return nil }
				case "write_panic":
					a.Write = func([]byte) error { panic("write panic") }
				case "notify_panic":
					a.Notify = func() error { panic("notify panic") }
				case "write_late":
					a.Write = func([]byte) error { time.Sleep(3 * time.Second); return nil }
				case "notify_late":
					a.Notify = func() error { time.Sleep(3 * time.Second); return nil }
				case "missing_ack":
					a.Snapshot = func() g2ProxySnapshot { return s }
				case "no_preload":
					d.preloadAcceptedNS = nil
				case "after_final_set":
					d.finalSetStartedNS = g2DiagnosticPtr(resourceNS())
				case "expired":
					d.DeadlineNS = resourceNS() - 1
				case "snapshot_panic", "snapshot_goexit", "accept_late":
					n := 0
					a.Snapshot = func() g2ProxySnapshot {
						n++
						if n == 1 {
							return s
						}
						if mode == "snapshot_goexit" {
							runtime.Goexit()
						}
						if mode == "snapshot_panic" {
							panic("snapshot panic")
						}
						time.Sleep(3 * time.Second)
						return g2ReloadTestNext(t, s, c)
					}
				}
				ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(d.DeadlineNS)))
				defer cancel()
				var err error
				var panicValue any
				returned := false
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer func() { panicValue = recover() }()
					err = d.reloadProbes(ctx, a)
					returned = true
				}()
				<-done
				if err == nil && panicValue == nil && !strings.HasSuffix(mode, "_goexit") {
					t.Fatal("failure became success", mode)
				}
				if strings.HasSuffix(mode, "_panic") && panicValue == nil {
					t.Fatal("original panic swallowed")
				}
				if strings.HasSuffix(mode, "_goexit") && returned {
					t.Fatal("Goexit invented return")
				}
				if w := d.probeReload; w != nil {
					if w.Result || w.AcceptedPrefix != nil {
						t.Fatal("failed callback invented acceptance")
					}
					if mode == "write_panic" && w.WriteReturnedNS != nil || mode == "notify_panic" && w.NotifyReturnedNS != nil {
						t.Fatal("panic invented callback completion")
					}
					if mode == "write_late" && w.NotifyStartedNS != nil {
						t.Fatal("late write continued notification")
					}
					if d.CapturedNS == nil {
						t.Fatal("failure was not frozen")
					}
					frozen := *d.CapturedNS
					d.finish(nil, true)
					if *d.CapturedNS != frozen {
						t.Fatal("first failure overwritten")
					}
				} else if len(*calls) != 0 {
					t.Fatal("invalid state performed work")
				}
			})
		})
	}
}

func TestG2ProbeReloadCompletionDeadline(t *testing.T) {
	for _, phase := range []string{"write", "notify", "accept"} {
		for _, bound := range []struct {
			name  string
			delta time.Duration
		}{{"before", -time.Nanosecond}, {"at", 0}, {"after", time.Nanosecond}} {
			t.Run(phase+"_"+bound.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					time.Sleep(time.Until(resourceOrigin.Add(time.Second)))
					d, s, c := g2ReloadTestInput(t)
					d.DeadlineNS = resourceNS() + int64(3*time.Second)
					a, calls := g2ReloadTestActions(t, d, s, c)
					advance := func() { time.Sleep(time.Until(resourceOrigin.Add(time.Duration(d.DeadlineNS) + bound.delta))) }
					switch phase {
					case "write":
						original := a.Write
						a.Write = func(b []byte) error { err := original(b); advance(); return err }
					case "notify":
						original := a.Notify
						a.Notify = func() error { err := original(); advance(); return err }
					case "accept":
						original := a.Snapshot
						a.Snapshot = func() g2ProxySnapshot {
							if d.probeReload.NotifyReturnedNS != nil {
								advance()
							}
							return original()
						}
					}
					ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(d.DeadlineNS)))
					defer cancel()
					err := d.reloadProbes(ctx, a)
					if (err == nil) != (bound.delta < 0) || d.probeReload.Result != (bound.delta < 0) {
						t.Fatal("original absolute deadline boundary", err, *calls)
					}
					if bound.delta < 0 && !reflect.DeepEqual(*calls, []string{"build", "snapshot", "write", "notify", "snapshot"}) {
						t.Fatal("single reload order changed", *calls)
					}
					if bound.delta >= 0 && d.probeReload.AcceptedNS != nil {
						t.Fatal("late callback invented acceptance")
					}
				})
			})
		}
	}
}

func TestG2ProbeReloadPathWindow(t *testing.T) {
	for _, mode := range []string{"new_path", "old_before_window", "old_in_window", "wrong_path", "old_cancel_counts", "end_inclusive"} {
		t.Run(mode, func(t *testing.T) {
			path := "/g2-competition-reload/0"
			h := fixture.HealthSnapshot{PID: 1, Version: 2, Records: []fixture.HealthRecord{{ID: 1, Version: 2, StartedNS: 110, CancelledNS: 150, FinishedNS: 151, RequestURI: path}}}
			switch mode {
			case "old_before_window":
				h.Records = append(h.Records, fixture.HealthRecord{ID: 2, Version: 2, StartedNS: 99, CancelledNS: 160, RequestURI: "/"})
			case "old_in_window", "old_cancel_counts":
				h.Records[0].RequestURI = "/"
			case "wrong_path":
				h.Records[0].RequestURI = "/other"
			case "end_inclusive":
				h.Records[0].StartedNS = 200
				h.Records[0].RequestURI = "/"
			}
			before, _ := json.Marshal(h)
			a, n, e := g2CompetitionTargetCounts(h, 2, 100, 200, true)
			err := g2ReloadTargetWindow(h, 100, 200, path)
			valid := mode == "new_path" || mode == "old_before_window"
			if (err == nil) != valid {
				t.Fatal("wrong URI window acceptance")
			}
			after, _ := json.Marshal(h)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("record filtered or mutated")
			}
			if mode == "old_cancel_counts" && (e != nil || a != 1 || n != 1) {
				t.Fatal("counterexample no longer demonstrates old cancellation counts")
			}
		})
	}
}

func TestG2ProbeReloadAcceptancePublication(t *testing.T) {
	for _, bound := range []struct {
		name  string
		delta time.Duration
	}{{"before", -time.Nanosecond}, {"at", 0}, {"after", time.Nanosecond}} {
		t.Run(bound.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				time.Sleep(time.Until(resourceOrigin.Add(time.Second)))
				deadline := resourceNS() + int64(3*time.Second)
				w := &g2ProbeReloadWitness{LastPrefix: &g2ReloadPrefix{EventCount: 4, LastEventID: 4, ReturnedNS: resourceNS()}}
				candidateTime := resourceNS()
				err := w.accept(func() error {
					// The actual final check runs after candidate capture; reach its
					// precise boundary without a real sleep or modifying product time.
					time.Sleep(time.Until(resourceOrigin.Add(time.Duration(deadline) + bound.delta)))
					if resourceNS() >= deadline {
						return context.DeadlineExceeded
					}
					return nil
				})
				if bound.delta < 0 {
					if err != nil || !w.Result || w.AcceptedNS == nil || *w.AcceptedNS != candidateTime || !reflect.DeepEqual(w.LastPrefix, w.AcceptedPrefix) {
						t.Fatal("valid original candidate lost", err)
					}
					w.LastPrefix.LastEventID++
					if w.AcceptedPrefix.LastEventID != 4 {
						t.Fatal("acceptance aliases later prefix")
					}
				} else if err == nil || w.Result || w.AcceptedPrefix != nil || w.AcceptedNS != nil {
					t.Fatal("late final check published acceptance", err, w)
				}
			})
		})
	}
}

func TestG2ProbeReloadActualPathHandler(t *testing.T) {
	h := fixture.NewHealth(t, resourceNS)
	h.Set(200, false)
	for _, path := range []string{"/", "/g2-competition-reload/0"} {
		resp, err := http.Get(h.URL() + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode)
		}
	}
	s := h.Snapshot()
	if len(s.Records) != 2 || s.Records[0].Version != s.Records[1].Version || s.Records[1].RequestURI != "/g2-competition-reload/0" {
		t.Fatal("new path changed target or Set")
	}
}

func TestG2ProbeReloadRealFileWrite(t *testing.T) {
	d, s, c := g2ReloadTestInput(t)
	a, _ := g2ReloadTestActions(t, d, s, c)
	path := filepath.Join(t.TempDir(), "oic.json")
	if err := os.WriteFile(path, c.Before, 0600); err != nil {
		t.Fatal(err)
	}
	a.Write = func(b []byte) error {
		if err := os.WriteFile(path, b, 0600); err != nil {
			return err
		}
		got, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, b) {
			return errors.New("readback")
		}
		return nil
	}
	ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(d.DeadlineNS)))
	defer cancel()
	if err := d.reloadProbes(ctx, a); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if g2ReloadHash(b) != *d.probeReload.ConfigAfterSHA256 || g2ReloadHash(c.Before) != *d.probeReload.ConfigBeforeSHA256 {
		t.Fatal("actual bytes not bound")
	}
}
