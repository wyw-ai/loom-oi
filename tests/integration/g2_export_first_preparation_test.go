package integration

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"testing/synctest"
	"time"
)

func TestG2ExportFirstFinalSelection(t *testing.T) {
	for _, mode := range []string{"prepare_first", "connect_first", "exact_return", "late_return", "already_expired", "duplicate_selection", "later_cycle_nil"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				prepare, connect := start.Add(3*time.Second), start.Add(time.Second)
				if mode == "prepare_first" {
					prepare, connect = connect, prepare
				}
				f := &g2CompetitionFinalSelection{deadline: prepare}
				if mode == "later_cycle_nil" {
					var later *g2CompetitionFinalSelection
					ctx, cancel := later.inputContext(context.Background())
					defer cancel()
					if _, ok := ctx.Deadline(); ok {
						t.Fatal("later cycle acquired preparation deadline")
					}
					if later.inputDeadline(connect) != connect || later.selectFinal(connect) != nil {
						t.Fatal("later cycle changed")
					}
					later.freezeBeforeCleanup(nil, true)
					return
				}
				calls := 0
				f.selectInput = func(d time.Time) {
					calls++
					if d != start.Add(time.Second) {
						t.Fatal("original deadline intersection changed")
					}
					if mode == "exact_return" || mode == "late_return" {
						time.Sleep(time.Until(d))
					}
					if mode == "late_return" {
						time.Sleep(time.Nanosecond)
					}
				}
				ctx, cancel := f.inputContext(context.Background())
				defer cancel()
				if d, ok := ctx.Deadline(); !ok || d != prepare {
					t.Fatal("connect context changed preparation D")
				}
				if mode == "already_expired" {
					time.Sleep(time.Until(connect))
				}
				err := f.selectFinal(connect)
				if mode == "late_return" || mode == "already_expired" {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal("late final accepted", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				want := 1
				if mode == "already_expired" {
					want = 0
				}
				if calls != want {
					t.Fatal("expired first callback ran or callback duplicated", calls)
				}
				if mode == "duplicate_selection" {
					if f.selectFinal(connect) == nil || calls != 1 {
						t.Fatal("same cycle rebuilt its final input")
					}
				}
				if f.deadline != prepare {
					t.Fatal("preparation D renewed")
				}
			})
		})
	}
}

func TestG2ExportFirstFailureFreezesBeforeCleanup(t *testing.T) {
	for _, mode := range []string{"callback_error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			d := &g2PreparationFailure{active: true, Phase: "slow_full", DeadlineNS: resourceNS() + int64(time.Second)}
			f := &g2CompetitionFinalSelection{preparation: d, deadline: time.Now().Add(time.Second)}
			var order []string
			var recovered any
			returned := false
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() {
					recovered = recover()
					f.freezeBeforeCleanup(recovered, !returned)
					order = append(order, "freeze")
					g2FinishDiagnostic(func() {
						if d.CapturedNS == nil {
							order = append(order, "close-before-freeze")
						}
						order = append(order, "close", "join")
					}, func() { order = append(order, "emit") })
				}()
				f.selectInput = func(time.Time) {
					order = append(order, "final")
					if mode == "panic" {
						panic("actual callback panic")
					}
					if mode == "goexit" {
						runtime.Goexit()
					}
					d.freeze("stats_error")
					runtime.Goexit() // Fatal's exit path after its actual error is frozen.
				}
				_ = f.selectFinal(time.Now().Add(time.Second))
				returned = true
			}()
			<-done
			if returned || d.CapturedNS == nil || !reflect.DeepEqual(order, []string{"final", "freeze", "close", "join", "emit"}) {
				t.Fatal("failure became normal false or cleanup preceded freeze", order)
			}
			if (mode == "panic") != (recovered != nil) {
				t.Fatal("panic/Goexit conflated")
			}
			first := *d.CapturedNS
			d.finish(nil, true) // outer preparation defer must keep the first capture.
			if *d.CapturedNS != first || (mode == "callback_error" && (d.Class == nil || *d.Class != "stats_error")) {
				t.Fatal("outer cleanup replaced original failure")
			}
		})
	}
}
