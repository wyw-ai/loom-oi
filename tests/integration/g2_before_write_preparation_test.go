package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestG2BeforeWriteSocketOwnership(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "error_observer_panic", "error_observer_goexit", "panic_observer_panic", "panic_observer_goexit", "goexit_observer_panic", "goexit_observer_goexit", "success", "zero_error", "late_callback"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var peers []net.Conn
			accepted := make(chan struct{})
			serverDone := make(chan struct{})
			counts := make(chan int64, 2)
			go func() {
				defer close(serverDone)
				var wg sync.WaitGroup
				defer wg.Wait()
				for i := 0; i < 2; i++ {
					c, err := listener.Accept()
					if err != nil {
						return
					}
					mu.Lock()
					peers = append(peers, c)
					mu.Unlock()
					wg.Add(1)
					go func() { defer wg.Done(); defer c.Close(); n, _ := io.Copy(io.Discard, c); counts <- n }()
				}
				close(accepted)
			}()
			t.Cleanup(func() {
				_ = listener.Close()
				mu.Lock()
				for _, c := range peers {
					_ = c.Close()
				}
				mu.Unlock()
				select {
				case <-serverDone:
				case <-time.After(3 * time.Second):
					t.Error("test server did not join after forced cleanup")
				}
			})
			diagnostic := &g2PreparationFailure{active: true, DeadlineNS: resourceNS() + int64(3*time.Second)}
			selection := &g2CompetitionFinalSelection{preparation: diagnostic}
			var owned *g2SlowScrapes
			var callbackCtx context.Context
			var recovered any
			var callErr error
			returned, notified, frozenActive := false, 0, false
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { recovered = recover() }()
				_, callErr = g2BeginSlowScrapesDiagnostic(context.Background(), listener.Addr().String(), &net.Dialer{}, func(ctx context.Context, s *g2SlowScrapes) error {
					owned, callbackCtx = s, ctx
					select {
					case <-accepted:
					case <-ctx.Done():
						return ctx.Err()
					}
					return selection.observeBeforeWrite(ctx, func() error {
						if strings.HasPrefix(mode, "panic") {
							panic("original callback")
						}
						if strings.HasPrefix(mode, "goexit") {
							runtime.Goexit()
						}
						if mode == "late_callback" {
							<-ctx.Done()
							return nil
						}
						if mode != "success" {
							return errors.New("original beforeWrite/Zero error")
						}
						return nil
					})
				}, func(s *g2SlowScrapes, err error) {
					notified++
					selection.freezeBeforeCleanup(nil, true)
					if diagnostic.CapturedNS == nil || (strings.HasPrefix(mode, "panic") && (diagnostic.Class == nil || *diagnostic.Class != "panic")) {
						t.Error("first abnormal callback was not frozen before observer")
					}
					frozenActive = callbackCtx.Err() == nil
					if err == nil || len(s.Snapshot()) != 2 {
						t.Error("failure lost original sockets/error")
					}
					select {
					case <-s.done:
						t.Error("cleanup preceded freeze")
					default:
					}
					for _, w := range s.Snapshot() {
						if w.StartedNS != 0 || w.CompletedNS != 0 || w.WrittenBytes != 0 {
							t.Error("GET writer started before callback success")
						}
					}
					if strings.HasSuffix(mode, "observer_panic") {
						panic("observer")
					}
					if strings.HasSuffix(mode, "observer_goexit") {
						runtime.Goexit()
					}
				})
				returned = true
				if mode == "success" {
					if callErr != nil {
						t.Error(callErr)
					}
					if err := g2CompetitionAwaitWrites(owned, time.Now().Add(time.Second)); err != nil {
						t.Error(err)
					}
					if err := owned.Close(); err != nil {
						t.Error(err)
					}
				}
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("original bounded input did not return/exit")
			}
			if owned == nil {
				t.Fatal("callback never received both real sockets")
			}
			select {
			case <-owned.done:
			default:
				t.Fatal("pre-writer failure left done unfinished")
			}
			if err := owned.Close(); err != nil {
				t.Fatal(err)
			}
			wantNotifications := 1
			if mode == "success" {
				wantNotifications = 0
			}
			if notified != wantNotifications || (notified != 0 && frozenActive != (mode != "late_callback")) {
				t.Fatal("notification duplicated or context observed after cleanup", notified, frozenActive)
			}
			wantReturned := mode == "error" || mode == "zero_error" || mode == "late_callback" || mode == "success"
			if returned != wantReturned {
				t.Fatal("panic/Goexit became normal return", returned)
			}
			wantPanic := ""
			if strings.HasPrefix(mode, "panic") {
				wantPanic = "original callback"
			} else if mode == "error_observer_panic" {
				wantPanic = "observer"
			}
			if (wantPanic == "" && recovered != nil) || (wantPanic != "" && recovered != wantPanic) {
				t.Fatal("original abnormal callback overwritten by observer", recovered)
			}
			if wantReturned && mode != "success" && callErr == nil {
				t.Fatal("failed or late callback returned success")
			}
			for i := 0; i < 2; i++ {
				select {
				case n := <-counts:
					if (mode != "success" && n != 0) || (mode == "success" && n == 0) {
						t.Fatal("actual loopback GET bytes contradict callback result", n)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("owned socket did not reach peer EOF")
				}
			}
		})
	}
}

func TestG2PreDialOriginalDeadline(t *testing.T) {
	for _, mode := range []string{"original_prepare", "expired", "gate_late", "duplicate", "later_cycle_nil"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				time.Sleep(time.Until(resourceOrigin.Add(time.Second)))
				start := time.Now()
				prepare := start.Add(3 * time.Second)
				d, calls, set := g2LateSetInput(t, prepare)
				f := &g2CompetitionFinalSelection{preparation: d, deadline: prepare}
				f.slowInput = func(deadline time.Time) error {
					if !deadline.Equal(prepare) {
						t.Fatal("pre-Dial Set deadline changed")
					}
					return d.finishSlowSources(set, deadline)
				}
				var observed context.Context
				f.beforeInput = func(ctx context.Context) error {
					observed = ctx
					if deadline, _ := ctx.Deadline(); !deadline.Equal(prepare) {
						t.Fatal("pre-Dial context renewed")
					}
					if mode == "gate_late" {
						<-ctx.Done()
					}
					return nil
				}
				if mode == "later_cycle_nil" {
					var later *g2CompetitionFinalSelection
					if later.preparePreDial() != nil || len(*calls) != 8 {
						t.Fatal("later cycle rebuilt input")
					}
					return
				}
				if mode == "expired" {
					time.Sleep(time.Until(prepare))
				}
				err := f.preparePreDial()
				bad := mode == "expired" || mode == "gate_late"
				if (err != nil) != bad || f.beforeReady == bad || *d.preDialContextDeadlineNS != d.DeadlineNS || d.DeadlineNS != int64(prepare.Sub(resourceOrigin)) {
					t.Fatal("original deadline/acceptance changed", err)
				}
				if observed != nil && observed.Err() == nil {
					t.Fatal("preparation context was not canceled after return")
				}
				if mode == "expired" && len(*calls) != 8 {
					t.Fatal("expired gate performed Set")
				}
				if mode == "duplicate" && (f.preparePreDial() == nil || len(*calls) != 16) {
					t.Fatal("repeated preparation reset sources")
				}
			})
		})
	}
}

func g2PreDialWitnessInput(t *testing.T) (*g2PreparationFailure, g2ProxySnapshot) {
	t.Helper()
	d, s, base := g2ProbeQueueInput(t)
	if !d.probeQueue(s, resourceNS(), base) || !d.returned(true, s, resourceNS()) || d.acceptedPreload() != nil {
		t.Fatal("preload")
	}
	d.Attempts++
	d.beginHistory(101, 102)
	d.lastSample.StartedNS, d.lastSample.CompletedNS = resourceNS(), resourceNS()
	d.lastSampleReturnedNS = resourceNS()
	now := resourceNS()
	ids := g2CompetitionSourceIDs()
	for i, id := range ids[:8] {
		q := protocol.ObservationQuery{Binding: d.preloadQueueWitness.Bindings[id], QueryID: id, DeadlineMS: 500}
		raw, _ := json.Marshal(q)
		s.Events = append(s.Events, g2WireEvent{ID: uint64(i + 3), ConnID: 9, Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgObservationQuery, QueryID: id, ReceivedNS: now - 3, WriteStartedNS: now - 2, WrittenNS: now - 1, Raw: raw})
	}
	d.full(d.lastSample)
	ok := d.youngWithin(s.Events, resourceNS(), 80*time.Millisecond)
	if !d.returned(ok, s, resourceNS()) {
		t.Fatal("query fixture")
	}
	return d, s
}

func TestG2PreDialWitnessIndependence(t *testing.T) {
	for _, mode := range []string{"deep_copy", "overflow", "final_aged", "final_not_full", "critical_aged", "other_pid", "other_binding", "future_unwritten", "missing_callback", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			d, s := g2PreDialWitnessInput(t)
			ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(d.DeadlineNS)))
			defer cancel()
			if mode == "overflow" {
				d.historyCount, d.Attempts = 512, 513
				d.beginHistory(101, 102)
				d.youngWithin(s.Events, resourceNS(), 80*time.Millisecond)
				d.returned(true, s, resourceNS())
			}
			switch mode {
			case "other_pid":
				d.lastQueryWitness.OISPID++
			case "other_binding":
				d.lastQueryWitness.Queries[0].Binding.ObservationRevision = "ob_2"
			case "missing_callback":
				d.lastQueryWitness.CallbackReturnedNS = nil
			case "deadline":
				cancel()
			}
			err := d.acceptedPreDial(ctx)
			bad := mode == "other_pid" || mode == "other_binding" || mode == "missing_callback" || mode == "deadline"
			if (err != nil) != bad {
				t.Fatal("accepted invalid standalone witness", err)
			}
			if bad {
				if d.preDialQueryWitness != nil || d.preDialAcceptedNS != nil {
					t.Fatal("failure invented acceptance")
				}
				return
			}
			w := d.preDialQueryWitness
			frozen, _ := json.Marshal(w)
			if mode == "overflow" && (*w.Attempt != 513 || !d.historyOverflow) {
				t.Fatal("history cap erased accepted evidence")
			}
			d.lastSample.OIC.Closed["later"] = 1
			d.lastSample.OIS.Observation.PendingQueries = 0
			d.lastSample.OIC.Observation.ProbeActive = 0
			d.lastQueryWitness.Queries[0].Binding.ObservationRevision = "ob_9"
			*d.lastQueryWitness.Attempt = 999
			*d.lastQueryWitness.CallbackReturnedNS = 999
			if b, _ := json.Marshal(w); !reflect.DeepEqual(b, frozen) {
				t.Fatal("saved pre-Dial witness aliases mutable final scratch")
			}
			now := resourceNS()
			switch mode {
			case "final_aged", "critical_aged":
				limit := 200 * time.Millisecond
				if mode == "critical_aged" {
					limit = 300 * time.Millisecond
				}
				s.Events[len(s.Events)-1].ReceivedNS = now - int64(limit) - 1
				if g2ObserveCompetitionQueries(s.Events, now, limit).Result {
					t.Fatal("earlier acceptance replaced independent later age gate")
				}
			case "final_not_full":
				if g2CompetitionFull(d.lastSample) {
					t.Fatal("earlier full replaced empty final sample")
				}
			case "future_unwritten":
				s.Events[len(s.Events)-1].WrittenNS = 0
				later := g2ObserveCompetitionQueries(s.Events, now, 200*time.Millisecond)
				s.Events[len(s.Events)-1].WrittenNS = resourceNS()
				if later.Result {
					t.Fatal("later write backfilled original Snapshot")
				}
			}
		})
	}
}
