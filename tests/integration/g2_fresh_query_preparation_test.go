package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// The existing reverse-prefix predicate still chooses all eight queries. Only
// the additional pre-Dial age changes; later 200/300ms gates remain independent.
func TestG2PreDialFreshQueryBoundary(t *testing.T) {
	for _, mode := range []string{"exact_80ms", "one_ns_old", "one_old", "future", "unwritten", "wrong_binding", "wrong_pid", "younger_subset", "wrong_age_limit", "different_context_deadline"} {
		t.Run(mode, func(t *testing.T) {
			d, s := g2PreDialWitnessInput(t)
			now := resourceNS()
			for i := 2; i < len(s.Events); i++ {
				s.Events[i].ReceivedNS = now - int64(80*time.Millisecond)
				s.Events[i].WriteStartedNS = s.Events[i].ReceivedNS + 1
				s.Events[i].WrittenNS = s.Events[i].ReceivedNS + 2
			}
			switch mode {
			case "one_ns_old":
				for i := 2; i < len(s.Events); i++ {
					s.Events[i].ReceivedNS--
				}
			case "one_old":
				s.Events[2].ReceivedNS--
			case "future":
				s.Events[2].ReceivedNS = now + 1
			case "unwritten":
				s.Events[2].WrittenNS = 0
			case "wrong_binding":
				var q protocol.ObservationQuery
				if err := json.Unmarshal(s.Events[2].Raw, &q); err != nil {
					t.Fatal(err)
				}
				q.Binding.ObservationRevision = "ob_2"
				s.Events[2].Raw, _ = json.Marshal(q)
			case "younger_subset":
				// Older valid records must not let selection skip a later, expired query.
				q := s.Events[2]
				q.ID = uint64(len(s.Events) + 1)
				q.QueryID = "new_expired"
				q.ReceivedNS--
				var body protocol.ObservationQuery
				_ = json.Unmarshal(q.Raw, &body)
				body.QueryID = q.QueryID
				q.Raw, _ = json.Marshal(body)
				s.Events = append(s.Events, q)
			}
			d.youngWithin(s.Events, now, 80*time.Millisecond)
			if mode == "wrong_age_limit" {
				d.youngWithin(s.Events, now, 200*time.Millisecond)
			}
			d.returned(d.lastQueryWitness.Result, s, resourceNS())
			if mode == "wrong_pid" {
				d.lastQueryWitness.OICPID++
			}
			deadline := resourceOrigin.Add(time.Duration(d.DeadlineNS))
			if mode == "different_context_deadline" {
				deadline = deadline.Add(-time.Millisecond)
			}
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			err := d.acceptedPreDial(ctx)
			if (err == nil) != (mode == "exact_80ms") {
				t.Fatal("full eight-query boundary was not enforced", mode, err)
			}
			if err != nil && (d.preDialQueryWitness != nil || d.preDialAcceptedNS != nil) {
				t.Fatal("rejected input invented acceptance")
			}
		})
	}
}

// No slow helper exists on these paths. The first failure is captured before
// the preparation context is canceled; abnormal calls do not become returns.
func TestG2PreDialFailureBeforeConnect(t *testing.T) {
	for _, mode := range []string{"no_full_until_deadline", "stats_error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				time.Sleep(time.Until(resourceOrigin.Add(time.Second)))
				deadline := time.Now().Add(3 * time.Second)
				d, _, set := g2LateSetInput(t, deadline)
				f := &g2CompetitionFinalSelection{preparation: d, deadline: deadline}
				f.slowInput = func(at time.Time) error { return d.finishSlowSources(set, at) }
				var observed context.Context
				f.beforeInput = func(ctx context.Context) error {
					observed = ctx
					d.Phase = "pre_dial_full"
					switch mode {
					case "no_full_until_deadline":
						<-ctx.Done()
						return ctx.Err()
					case "panic":
						panic("original pre-Dial")
					case "goexit":
						runtime.Goexit()
					}
					return errors.New("original stats failure")
				}
				dialed, returned := false, false
				var panicValue any
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer func() { panicValue = recover() }()
					err := f.preparePreDial()
					returned = true
					if err == nil {
						dialed = true
					}
				}()
				<-done
				if dialed || f.beforeReady || d.CapturedNS == nil || d.preDialContextState == nil || d.preDialAcceptedNS != nil || d.preDialQueryWitness != nil {
					t.Fatal("failed preparation reached helper or lost first capture")
				}
				wantState := "active"
				if mode == "no_full_until_deadline" {
					wantState = "deadline"
				}
				if *d.preDialContextState != wantState || observed.Err() == nil {
					t.Fatal("context captured after cancellation", *d.preDialContextState, observed.Err())
				}
				if returned != (mode == "stats_error" || mode == "no_full_until_deadline") || (panicValue != nil) != (mode == "panic") {
					t.Fatal("abnormal callback became normal return", panicValue, returned)
				}
				var slot g2ExportSlotInput
				if slot.failureFields() != nil {
					t.Fatal("pre-Dial failure fabricated helper/Zero evidence")
				}
				first := *d.CapturedNS
				f.freezeBeforeCleanup(nil, true)
				if *d.CapturedNS != first {
					t.Fatal("outer cleanup replaced first freeze")
				}
			})
		})
	}
}

// A real listener distinguishes waiting before Dial from merely pausing writers
// on accepted sockets. The peer receives the unmodified 2048-request input.
func TestG2PreDialConnectionsFollowPreparation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var mu sync.Mutex
	var peers []net.Conn
	serverDone := make(chan struct{})
	var wg sync.WaitGroup
	go func() {
		defer close(serverDone)
		defer wg.Wait()
		for i := 0; i < 2; i++ {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			peers = append(peers, c)
			mu.Unlock()
			accepted.Add(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				reader := bufio.NewReader(c)
				request, err := http.ReadRequest(reader)
				if err != nil {
					return
				}
				_ = request.Body.Close()
				_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
				_, _ = io.Copy(io.Discard, reader)
			}()
		}
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
			t.Error("loopback peer did not join")
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	d, _, set := g2LateSetInput(t, deadline)
	f := &g2CompetitionFinalSelection{preparation: d, deadline: deadline}
	f.slowInput = func(at time.Time) error { return d.finishSlowSources(set, at) }
	f.beforeInput = func(ctx context.Context) error {
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		if accepted.Load() != 0 {
			t.Error("preparation consumed the server header deadline")
		}
		return nil
	}
	if err := f.preparePreDial(); err != nil {
		t.Fatal(err)
	}
	acceptedAt := resourceNS()
	if accepted.Load() != 0 {
		t.Fatal("helper entered before acceptance")
	}
	ctx, cancel := f.inputContext(context.Background())
	defer cancel()
	var slot g2ExportSlotInput
	slow, err := g2BeginSlowScrapesBeforeWrite(ctx, listener.Addr().String(), &net.Dialer{}, func(ctx context.Context, s *g2SlowScrapes) error { slot.captureHelper(s); return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	helperD := resourceOrigin.Add(time.Duration(slow.helperContextDeadlineNS))
	if err := g2CompetitionAwaitWrites(slow, helperD); err != nil {
		t.Fatal(err)
	}
	// Consume only the actual first headers here; this test is not an Active or
	// persistent rejection qualification and does not claim an exporter slot.
	for _, c := range slow.conns {
		_ = c.SetReadDeadline(helperD)
		res, err := http.ReadResponse(bufio.NewReader(c), nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode)
		}
	}
	if *slot.HelperStartedNS < acceptedAt || *slot.HelperContextDeadlineNS != min(*slot.HelperStartedNS+int64(time.Second), d.DeadlineNS) {
		t.Fatal("helper reused the preparation origin or renewed deadline")
	}
	for _, w := range slow.Snapshot() {
		if w.ConnectedNS < *slot.HelperStartedNS || w.CompletedNS > *slot.HelperContextDeadlineNS || w.WrittenBytes != w.PlannedBytes {
			t.Fatal("actual input escaped original helper deadline", w)
		}
	}
}

func TestG2PreDialHelperMetadata(t *testing.T) {
	for _, mode := range []string{"first_dial_error", "expired_parent", "parent_clips_both_sockets", "zero_after_deadline"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := listener.Addr().String()
			var mu sync.Mutex
			var peers []net.Conn
			var wg sync.WaitGroup
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
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
					go func() { defer wg.Done(); defer c.Close(); _, _ = io.Copy(io.Discard, c) }()
				}
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
					t.Error("metadata peer did not join")
				}
			})
			parentD := time.Now().Add(150 * time.Millisecond)
			if mode == "first_dial_error" {
				_ = listener.Close()
			}
			if mode == "expired_parent" {
				parentD = time.Now().Add(-time.Millisecond)
			}
			ctx, cancel := context.WithDeadline(context.Background(), parentD)
			defer cancel()
			var slot g2ExportSlotInput
			var owned *g2SlowScrapes
			notifications := 0
			slow, err := g2BeginSlowScrapesDiagnostic(ctx, addr, &net.Dialer{}, func(ctx context.Context, s *g2SlowScrapes) error {
				owned = s
				slot.captureHelper(s)
				actualD, _ := ctx.Deadline()
				if !actualD.Equal(parentD) || s.helperContextDeadlineNS != int64(parentD.Sub(resourceOrigin)) {
					t.Error("helper failed to clip actual context to parent")
				}
				if mode == "zero_after_deadline" {
					<-ctx.Done()
					// Both actual TCP sockets share the same expired write deadline. No
					// request goroutine has started; a successful byte would falsify it.
					for _, c := range s.conns {
						n, e := c.Write([]byte("x"))
						var ne net.Error
						if n != 0 || !errors.As(e, &ne) || !ne.Timeout() {
							t.Error("socket did not retain shared deadline", n, e)
						}
					}
					return nil
				}
				return nil
			}, func(s *g2SlowScrapes, e error) {
				owned = s
				notifications++
				slot.captureHelper(s)
				if e == nil {
					t.Error("lost original failure")
				}
			})
			if slow != nil {
				defer slow.Close()
			}
			if mode == "parent_clips_both_sockets" {
				if err != nil || slow == nil || notifications != 0 {
					t.Fatal("original short parent did not permit immediate input", err)
				}
				if err := g2CompetitionAwaitWrites(slow, parentD); err != nil {
					t.Fatal(err)
				}
				if owned.helperContextDeadlineNS != int64(parentD.Sub(resourceOrigin)) {
					t.Fatal("short original D changed")
				}
				for _, w := range slow.Snapshot() {
					if w.CompletedNS > owned.helperContextDeadlineNS {
						t.Fatal("write finished after original D")
					}
				}
				return
			}
			if err == nil || owned == nil || notifications != 1 {
				t.Fatal("failed helper did not report once", err, notifications)
			}
			select {
			case <-owned.done:
			default:
				t.Fatal("failed helper did not finish ownership")
			}
			fields, ok := slot.failureFields().(map[string]any)
			if !ok || fields["helper_started_ns"] == nil || fields["helper_context_deadline_ns"] == nil || fields["zero"] != nil || fields["writers_released_ns"] != nil || fields["origin_ns"] != nil {
				t.Fatal("pre-Zero failure erased helper or invented later work", fields)
			}
			if *slot.HelperContextDeadlineNS != min(*slot.HelperStartedNS+int64(time.Second), int64(parentD.Sub(resourceOrigin))) {
				t.Fatal("recorded deadline differs from actual constructor")
			}
		})
	}
}
