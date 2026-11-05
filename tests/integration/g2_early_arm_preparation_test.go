package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func g2EarlyArmInput() *g2PreparationFailure {
	now := resourceNS()
	return &g2PreparationFailure{StartedNS: now, DeadlineNS: now + int64(3*time.Second), active: true}
}

func TestG2EarlyArmCursor(t *testing.T) {
	for _, mode := range []string{"empty", "prefix", "overflow", "failure", "gap", "future", "old_drop", "late_snapshot", "set_started", "sample_started"} {
		t.Run(mode, func(t *testing.T) {
			d := g2EarlyArmInput()
			s := g2ProxySnapshot{Events: []g2WireEvent{{ID: 1, ReceivedNS: resourceNS()}}}
			switch mode {
			case "empty":
				s.Events = nil
			case "overflow":
				s.Overflow = true
			case "failure":
				s.Failure = "original proxy failure"
			case "gap":
				s.Events[0].ID = 2
			case "future":
				s.Events[0].ReceivedNS += int64(time.Second)
			case "old_drop":
				s.Events[0].Dropped = true
			case "set_started":
				d.preloadSetStartedNS = g2DiagnosticPtr(resourceNS())
			case "sample_started":
				d.Attempts = 1
			}
			snapshots, calls := 0, 0
			err := d.armSnapshotDrops(func() g2ProxySnapshot {
				snapshots++
				if mode == "late_snapshot" {
					d.DeadlineNS = resourceNS() - 1
				}
				return s
			}, func() error { calls++; return nil })
			valid := mode == "empty" || mode == "prefix"
			if (err == nil) != valid || valid && calls != 32 || !valid && calls != 0 {
				t.Fatal("cursor/arm boundary", err, calls)
			}
			if mode == "set_started" || mode == "sample_started" {
				if snapshots != 0 || d.dropArmBeforeSnapshotReturnedNS != nil {
					t.Fatal("new Snapshot after preload began")
				}
				return
			}
			if snapshots != 1 || d.dropArmBeforeSnapshotReturnedNS == nil || d.dropArmBeforeEventID == nil {
				t.Fatal("original Snapshot result lost")
			}
			if valid && (*d.dropArmBeforeEventID != uint64(len(s.Events)) || *d.dropArmBeforeSnapshotReturnedNS > *d.dropArmStartedNS) {
				t.Fatal("cursor not bound to original return")
			}
			s.Events = append(s.Events, g2WireEvent{ID: 100})
			if valid && *d.dropArmBeforeEventID == 100 {
				t.Fatal("cursor backfilled from later wire")
			}
			if !valid && d.dropArmStartedNS != nil {
				t.Fatal("invalid prefix claimed arm started")
			}
		})
	}
}

func g2EarlyArmAppendDrop(t *testing.T, s *g2ProxySnapshot, binding protocol.ObservationBinding, conn uint64, received int64, n int) {
	t.Helper()
	id := fmt.Sprintf("%032x", n)
	q := protocol.ObservationQuery{Header: protocol.Header{Type: protocol.MsgObservationQuery}, QueryID: id, Binding: binding, DeadlineMS: 500}
	r := protocol.ObservationSnapshot{Header: protocol.Header{Type: protocol.MsgObservationSnapshot}, QueryID: id, Binding: binding}
	qr, _ := json.Marshal(q)
	rr, _ := json.Marshal(r)
	base := uint64(len(s.Events))
	s.Events = append(s.Events,
		g2WireEvent{ID: base + 1, ConnID: conn, Role: g2RoleControl, Direction: g2OISToOIC, Type: q.Type, QueryID: id, ReceivedNS: received, WriteStartedNS: received, WrittenNS: received, Raw: qr},
		g2WireEvent{ID: base + 2, ConnID: conn, Role: g2RoleControl, Direction: g2OICToOIS, Type: r.Type, QueryID: id, ReceivedNS: received, Dropped: true, Raw: rr})
}

func TestG2EarlyArmPreloadDrops(t *testing.T) {
	for _, mode := range []string{"legal", "received_before_arm", "query_return_not_published", "thirty_two", "thirty_three", "cursor_contains_drop", "prefix_future", "missing_cursor", "cursor_after_snapshot", "critical_message", "wrong_direction", "other_control", "injected", "write_failed", "written", "wrong_binding", "missing_query"} {
		t.Run(mode, func(t *testing.T) {
			d, s, base := g2ProbeQueueInput(t)
			var reg protocol.Register
			if err := json.Unmarshal(s.Events[0].Raw, &reg); err != nil {
				t.Fatal(err)
			}
			binding := *reg.Exposures[0].Binding
			received := resourceNS()
			if mode == "received_before_arm" {
				received = *d.dropArmStartedNS - 1
			}
			g2EarlyArmAppendDrop(t, &s, binding, 9, received, 1)
			last := &s.Events[len(s.Events)-1]
			switch mode {
			case "query_return_not_published":
				s.Events[2].WrittenNS = 0
			case "thirty_two", "thirty_three":
				count := 32
				if mode == "thirty_three" {
					count++
				}
				for n := 2; n <= count; n++ {
					g2EarlyArmAppendDrop(t, &s, binding, 9, received, n)
				}
			case "cursor_contains_drop":
				d.dropArmBeforeEventID = g2DiagnosticPtr(last.ID)
			case "prefix_future":
				s.Events[0].ReceivedNS = *d.dropArmBeforeSnapshotReturnedNS + 1
			case "missing_cursor":
				d.dropArmBeforeEventID = nil
			case "cursor_after_snapshot":
				d.dropArmBeforeEventID = g2DiagnosticPtr(uint64(len(s.Events) + 1))
			case "critical_message":
				last.Type = protocol.MsgAdmissionResult
			case "wrong_direction":
				last.Direction = g2OISToOIC
			case "other_control":
				last.ConnID++
			case "injected":
				last.Injected = true
			case "write_failed":
				last.WriteFailed = true
			case "written":
				last.WrittenNS = received
			case "wrong_binding":
				var body protocol.ObservationSnapshot
				_ = json.Unmarshal(last.Raw, &body)
				body.Binding.ObservationRevision = "ob_2"
				last.Raw, _ = json.Marshal(body)
			case "missing_query":
				s.Events[2].Type = protocol.MsgObservationChanged
			}
			want := mode == "legal" || mode == "received_before_arm" || mode == "query_return_not_published" || mode == "thirty_two"
			if d.probeQueue(s, resourceNS(), base) != want {
				t.Fatal("original prefix/native Drop identity boundary")
			}
		})
	}
}

func TestG2EarlyArmFailureOwnership(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit"} {
		for _, at := range []int{1, 4, 32} {
			t.Run(fmt.Sprintf("%s_%d", mode, at), func(t *testing.T) {
				d := g2EarlyArmInput()
				p := &g2Proxy{changed: make(chan struct{})}
				var owned []*g2Barrier
				var caught any
				var armErr error
				normal := false
				frozenBeforeCleanup := false
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer func() {
						caught = recover()
						d.finish(caught, !normal || armErr != nil)
						frozenBeforeCleanup = d.CapturedNS != nil
						for _, b := range owned {
							b.Cancel()
						}
					}()
					calls := 0
					armErr = d.armSnapshotDrops(func() g2ProxySnapshot { return g2ProxySnapshot{} }, func() error {
						calls++
						if calls == at && mode == "error" {
							return errors.New("original arm error")
						}
						b, err := p.Arm(g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgObservationSnapshot})
						if err != nil {
							return err
						}
						owned = append(owned, b) // registered before Drop or an exceptional return
						b.Drop()
						if calls == at {
							if mode == "panic" {
								panic("original arm panic")
							}
							runtime.Goexit()
						}
						return nil
					})
					normal = true
				}()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("fixture cleanup did not join")
				}
				if !frozenBeforeCleanup || d.dropArmCount != at-1 || d.dropArmCompletedNS != nil || d.preloadSetStartedNS != nil || d.inputVersions != nil || d.preloadAcceptedNS != nil || len(p.barriers) != 0 {
					t.Fatal("partial arm lost freeze/count/null/cleanup", d.dropArmCount)
				}
				for _, b := range owned {
					select {
					case <-b.cancelledCh:
					default:
						t.Fatal("owned matcher not canceled")
					}
				}
				if mode == "error" && (!normal || armErr == nil) || mode != "error" && normal || mode == "panic" && caught != "original arm panic" || mode == "goexit" && caught != nil {
					t.Fatal("exception became normal return or lost first cause")
				}
				encoded, err := json.Marshal(d.fields())
				if err != nil || !strings.Contains(string(encoded), `"preload_set_started_ns":null`) || !strings.Contains(string(encoded), `"drop_arm_completed_ns":null`) {
					t.Fatal("partial raw fabricated completion", err)
				}
			})
		}
	}
}

func TestG2EarlyArmOriginalPublication(t *testing.T) {
	// Use the real proxy capture/Arm/Drop/Snapshot paths. The receiver took its
	// timestamp before the arm-before Snapshot, then published after that cursor.
	d := g2EarlyArmInput()
	p := &g2Proxy{changed: make(chan struct{}), capped: &g2CappedListener{slots: make(chan struct{}, 1)}}
	c := &g2ProxyConn{proxy: p, snap: g2ConnectionSnapshot{ID: 9, Role: g2RoleControl}, stages: map[string]string{}}
	received := resourceNS()
	var owned []*g2Barrier
	t.Cleanup(func() {
		for _, b := range owned {
			b.Cancel()
		}
	})
	if err := d.armSnapshotDrops(p.Snapshot, func() error {
		b, err := p.Arm(g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgObservationSnapshot})
		if err != nil {
			return err
		}
		owned = append(owned, b)
		b.Drop()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if *d.dropArmBeforeEventID != 0 || received > *d.dropArmBeforeSnapshotReturnedNS {
		t.Fatal("original empty cursor lost")
	}
	e, barrier, err := c.capture(g2OICToOIS, websocket.MessageText, []byte(`{"type":"observation_snapshot","query_id":"original"}`), false, received)
	if err != nil || barrier == nil || !e.Dropped || e.ID != 1 || e.ReceivedNS >= *d.dropArmStartedNS {
		t.Fatal("valid pre-received/post-cursor publication rejected", e, err)
	}
	for _, raw := range []string{`{"type":"admission_result"}`, `{"type":"heartbeat_ack"}`} {
		e, barrier, err = c.capture(g2OICToOIS, websocket.MessageText, []byte(raw), false, resourceNS())
		if err != nil || barrier != nil || e.Dropped {
			t.Fatal("critical message consumed Snapshot allowance")
		}
	}
	if len(p.barriers) != 31 || d.dropArmCount != 32 {
		t.Fatal("actual Drop allowance changed")
	}
}

func TestG2EarlyArmCompletionDeadline(t *testing.T) {
	for _, tc := range []struct {
		name  string
		delta time.Duration
	}{{"before_deadline", -time.Nanosecond}, {"at_deadline", 0}, {"after_deadline", time.Nanosecond}} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				time.Sleep(time.Until(resourceOrigin.Add(time.Second)))
				d := g2EarlyArmInput()
				originalStart, originalDeadline := d.StartedNS, d.DeadlineNS
				calls := 0
				err := d.armSnapshotDrops(func() g2ProxySnapshot { return g2ProxySnapshot{} }, func() error {
					calls++
					if calls == 32 {
						time.Sleep(time.Duration(originalDeadline-resourceNS()) + tc.delta)
					}
					return nil
				})
				if (err == nil) != (tc.delta < 0) || calls != 32 || d.dropArmCount != 32 || d.dropArmCompletedNS == nil ||
					*d.dropArmCompletedNS != originalDeadline+int64(tc.delta) || d.StartedNS != originalStart || d.DeadlineNS != originalDeadline {
					t.Fatal("actual last arm completion lost or original deadline renewed", err, calls)
				}
				if tc.delta >= 0 && d.preloadSources(func(string, bool) uint64 { t.Fatal("Set after expired original arm"); return 0 }) == nil {
					t.Fatal("late complete batch allowed first Set")
				}
			})
		})
	}
}
