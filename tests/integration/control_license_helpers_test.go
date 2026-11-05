package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func controlLicenseExample() controlLicenseObservation {
	_, s := resourceTestStats()
	s.LicenseRemainingMS = 2250
	return controlLicenseObservation{1_000_000_000, 1_250_000_000, resourceProcess[protocol.OICStats]{PID: 11, SampledNS: 1_250_000_000, Stats: s}}
}

func TestControlLicenseRequestWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*controlLicenseObservation, *int64, *int64)
		want  int64
		valid bool
	}{
		{"exact_two_seconds", nil, 2_000_000_000, true},
		{"one_nanosecond_short", func(o *controlLicenseObservation, at, d *int64) { *at = *at + 1 }, 1_999_999_999, false},
		{"delayed_http", func(o *controlLicenseObservation, at, d *int64) {
			o.Process.SampledNS = 1_300_000_000
			o.RequestCompletedNS = 1_300_000_000
			*at = 1_301_000_000
		}, 1_949_000_000, false},
		{"zero_remaining", func(o *controlLicenseObservation, at, d *int64) { o.Process.Stats.LicenseRemainingMS = 0 }, 0, false},
		{"selection_only", func(o *controlLicenseObservation, at, d *int64) {
			o.RequestCompletedNS = o.RequestStartedNS
			o.Process.SampledNS = o.RequestStartedNS
			*at = o.RequestStartedNS
			o.Process.Stats.LicenseRemainingMS = 2249
		}, 2_249_000_000, false},
		{"prepare_expired", func(o *controlLicenseObservation, at, d *int64) { *d = *at - 1 }, 2_000_000_000, false},
		{"future_sample", func(o *controlLicenseObservation, at, d *int64) { *at = o.RequestCompletedNS - 1 }, 0, false},
		{"reversed_request", func(o *controlLicenseObservation, at, d *int64) { o.RequestStartedNS = o.RequestCompletedNS + 1 }, 0, false},
		{"sample_before_request", func(o *controlLicenseObservation, at, d *int64) { o.Process.SampledNS = o.RequestStartedNS - 1 }, 0, false},
		{"sample_after_completion", func(o *controlLicenseObservation, at, d *int64) { o.Process.SampledNS = o.RequestCompletedNS + 1 }, 0, false},
		{"negative_start", func(o *controlLicenseObservation, at, d *int64) { o.RequestStartedNS = -1 }, 0, false},
		{"excess_remaining", func(o *controlLicenseObservation, at, d *int64) { o.Process.Stats.LicenseRemainingMS = 3001 }, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := controlLicenseExample()
			at, deadline := o.RequestCompletedNS, int64(6_000_000_000)
			if tc.alter != nil {
				tc.alter(&o, &at, &deadline)
			}
			got, err := controlLicenseEligible(o, at, deadline)
			if (err == nil) != tc.valid || got != tc.want {
				t.Fatalf("remaining=%d err=%v want=%d valid=%t", got, err, tc.want, tc.valid)
			}
			if tc.name == "delayed_http" {
				old := o.Process.Stats.LicenseRemainingMS*int64(time.Millisecond) - (at - o.Process.SampledNS)
				if old != 2_249_000_000 || old < int64(2*time.Second) {
					t.Fatal("old after-only false-positive input not established")
				}
			}
		})
	}
}

func TestControlLicenseIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*controlLicenseObservation)
		valid bool
	}{
		{"valid", nil, true},
		{"wrong_pid", func(o *controlLicenseObservation) { o.Process.PID++ }, false},
		{"zero_pid", func(o *controlLicenseObservation) { o.Process.PID = 0 }, false},
		{"wrong_access", func(o *controlLicenseObservation) { o.Process.Stats.AccessID = "lab-b" }, false},
		{"wrong_epoch", func(o *controlLicenseObservation) { o.Process.Stats.Epoch++ }, false},
		{"wrong_generation", func(o *controlLicenseObservation) { o.Process.Stats.Generation++ }, false},
		{"disconnected", func(o *controlLicenseObservation) { o.Process.Stats.Connected = false }, false},
		{"unregistered", func(o *controlLicenseObservation) { o.Process.Stats.Registered = false }, false},
		{"invalid_process", func(o *controlLicenseObservation) { o.Process.Stats.Process.Goroutines = 0 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := controlLicenseExample()
			if tc.alter != nil {
				tc.alter(&o)
			}
			err := controlLicenseIdentity(o, 11, "lab-a", 1, 1)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}

func TestControlLicenseCaptureDoesNotReuseFailedStats(t *testing.T) {
	var calls atomic.Int64
	_, stats := resourceTestStats()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(stats)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	p := &proc{cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, exited: make(chan struct{})}
	e := &env{stats: server.Client(), oics: map[string]*oicProc{"lab-a": {proc: p, diag: strings.TrimPrefix(server.URL, "http://")}}}
	first, err := e.controlLicense(context.Background(), "lab-a")
	if err != nil || first.Process.Stats.Generation != 1 {
		t.Fatal(first, err)
	}
	second, err := e.controlLicense(context.Background(), "lab-a")
	if err == nil || second.Process.Stats.Generation != 0 || second.RequestStartedNS < first.RequestCompletedNS {
		t.Fatalf("failed observation reused a sample: %+v %v", second, err)
	}
}

type controlCloseGate struct {
	net.Conn
	closeFn func() error
}

func (c *controlCloseGate) Close() error { return c.closeFn() }

func TestControlCutObservedBounds(t *testing.T) {
	for _, closed := range []bool{false, true} {
		name := "ordered_closes"
		if closed {
			name = "already_closed_is_observation"
		}
		t.Run(name, func(t *testing.T) {
			a, peerA := net.Pipe()
			b, peerB := net.Pipe()
			defer a.Close()
			defer peerA.Close()
			defer b.Close()
			defer peerB.Close()
			otherA, otherB := net.Pipe()
			defer otherA.Close()
			defer otherB.Close()
			var clock atomic.Int64
			clock.Store(100)
			secondEntered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			p := &proxy{}
			first := &controlCloseGate{Conn: a, closeFn: func() error {
				if !p.mu.TryLock() {
					return errors.New("owner lock held during first Close")
				}
				p.mu.Unlock()
				clock.Store(200)
				_ = a.Close()
				if closed {
					return net.ErrClosed
				}
				return nil
			}}
			second := &controlCloseGate{Conn: b, closeFn: func() error {
				if !p.mu.TryLock() {
					return errors.New("owner lock held during second Close")
				}
				p.mu.Unlock()
				close(secondEntered)
				<-release
				return b.Close()
			}}
			p.conns = []*pconn{{a: otherA, b: otherB}, {a: first, b: second}}
			type result struct {
				o   controlCutObservation
				err error
			}
			done := make(chan result, 1)
			finished := make(chan struct{})
			go func() { defer close(finished); o, err := p.cutObserved(1, clock.Load); done <- result{o, err} }()
			defer func() {
				releaseOnce.Do(func() { close(release) })
				select {
				case <-finished:
				case <-time.After(2 * time.Second):
					t.Error("cut worker did not join")
				}
			}()
			select {
			case <-secondEntered:
			case <-time.After(2 * time.Second):
				t.Fatal("second Close not entered")
			}
			select {
			case <-done:
				t.Fatal("returned before second Close")
			default:
			}
			// Unselected data/control connection still carries actual bytes.
			sent := make(chan error, 1)
			go func() { _, err := otherA.Write([]byte("x")); sent <- err }()
			_ = otherB.SetReadDeadline(time.Now().Add(2 * time.Second))
			var data [1]byte
			if _, err := io.ReadFull(otherB, data[:]); err != nil || data[0] != 'x' {
				t.Fatal("unselected connection was closed", err)
			}
			if err := <-sent; err != nil {
				t.Fatal(err)
			}
			clock.Store(350)
			releaseOnce.Do(func() { close(release) })
			select {
			case r := <-done:
				if r.err != nil || r.o.StartedNS != 100 || r.o.CompletedNS != 350 || r.o.ControlIndex != 1 || r.o.ServerCloseError != nil {
					t.Fatal(r)
				}
				if closed != (r.o.ClientCloseError != nil) {
					t.Fatal("Close error was changed", r.o)
				}
				if closed && *r.o.ClientCloseError != net.ErrClosed.Error() {
					t.Fatal("Close error was not preserved")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cut did not join")
			}
		})
	}
	t.Run("invalid_index", func(t *testing.T) {
		p := &proxy{}
		if _, err := p.cutObserved(0, func() int64 { t.Fatal("clock read before valid handle"); return 1 }); err == nil {
			t.Fatal("missing connection accepted")
		}
	})
}

func TestControlTerminalInterval(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		start, complete, terminal int64
		code                      protocol.Code
		valid                     bool
	}{
		{"exact_lower", 100, 200, 2_000_000_200, protocol.CodeLicenseExpired, true},
		{"one_ns_early", 100, 200, 2_000_000_199, protocol.CodeLicenseExpired, false},
		{"exact_upper", 100, 200, 4_000_000_100, protocol.CodeAccessOffline, true},
		{"one_ns_late", 100, 200, 4_000_000_101, protocol.CodeAccessOffline, false},
		{"close_cost_not_hidden", 100, 1_000_000_100, 4_500_000_100, protocol.CodeLicenseExpired, false},
		{"wrong_reason", 100, 200, 2_500_000_100, protocol.CodeChannelInterrupted, false},
		{"reversed_cut", 200, 100, 2_500_000_100, protocol.CodeLicenseExpired, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := controlTerminalWithin(controlTerminal{AtNS: tc.terminal, Reason: tc.code}, controlCutObservation{StartedNS: tc.start, CompletedNS: tc.complete})
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}

type controlWorkerStream struct {
	entered, released chan struct{}
	once, enterOnce   sync.Once
}

func (s *controlWorkerStream) Read([]byte) (int, error) {
	s.enterOnce.Do(func() { close(s.entered) })
	<-s.released
	return 0, &client.AbortError{Code: protocol.CodeLicenseExpired}
}
func (s *controlWorkerStream) Write(b []byte) (int, error) { return len(b), nil }
func (s *controlWorkerStream) Close() error                { s.once.Do(func() { close(s.released) }); return nil }

func TestControlTerminalCapturedBeforeJoin(t *testing.T) {
	var clock atomic.Int64
	clock.Store(10)
	s := &controlWorkerStream{entered: make(chan struct{}), released: make(chan struct{})}
	w := startControlWorkers(s, clock.Load)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	defer func() {
		if err := w.close(ctx, s); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-s.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	clock.Store(20)
	_ = s.Close()
	select {
	case <-w.readDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	clock.Store(1000) // The coordinator observes completion much later.
	if w.terminal.AtNS != 20 || w.terminal.Reason != protocol.CodeLicenseExpired {
		t.Fatal(w.terminal)
	}
	if err := w.close(ctx, s); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.writeDone:
	default:
		t.Fatal("writer was not joined")
	}
}

func TestControlTargetRunningWitness(t *testing.T) {
	old := controlTargetRecord{Index: 0, AcceptedNS: 10, LocalAddress: "old-local", RemoteAddress: "old-remote", DataBase64: ""}
	fresh := controlTargetRecord{Index: 1, AcceptedNS: 20, LocalAddress: "local", RemoteAddress: "remote", DataBase64: base64.StdEncoding.EncodeToString([]byte("k"))}
	before := controlTargetObservation{Connections: []controlTargetRecord{old}}
	for _, tc := range []struct {
		name      string
		records   []controlTargetRecord
		want, bad bool
	}{
		{"one_new_target_received_k", []controlTargetRecord{old, fresh}, true, false},
		{"no_new_target", []controlTargetRecord{old}, false, false},
		{"extra_target", []controlTargetRecord{old, fresh, fresh}, false, true},
		{"replaced_prefix", []controlTargetRecord{fresh, fresh}, false, true},
		{"no_keepalive", []controlTargetRecord{old, {Index: 1, AcceptedNS: 20, LocalAddress: "local", RemoteAddress: "remote", DataBase64: ""}}, false, false},
		{"invalid_base64", []controlTargetRecord{old, {Index: 1, AcceptedNS: 20, LocalAddress: "local", RemoteAddress: "remote", DataBase64: "!"}}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := controlRunningTarget(before, controlTargetObservation{Connections: tc.records})
			if got != tc.want || (err != nil) != tc.bad {
				t.Fatal(got, err)
			}
		})
	}
}

func TestControlConnectedLogIsUnique(t *testing.T) {
	row := func(gen float64) map[string]any {
		return map[string]any{"msg": "control connected", "epoch": float64(1), "generation": gen}
	}
	for _, tc := range []struct {
		name  string
		rows  []map[string]any
		after int
		valid bool
	}{
		{"original", []map[string]any{row(1)}, 0, true},
		{"missing", nil, 0, false},
		{"unproven_reconnect", []map[string]any{row(1), row(2)}, 0, false},
		{"new_after_mark", []map[string]any{row(1), row(2)}, 1, true},
		{"two_after_mark", []map[string]any{row(1), row(2), row(3)}, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := controlConnectedLog(&proc{lines: tc.rows}, tc.after)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}

type controlBlockedCloseStream struct {
	*controlWorkerStream
	closeEntered, closeRelease chan struct{}
}

func (s *controlBlockedCloseStream) Close() error {
	close(s.closeEntered)
	<-s.closeRelease
	return s.controlWorkerStream.Close()
}

func TestControlWorkersFailedCleanupCannotPass(t *testing.T) {
	s := &controlBlockedCloseStream{controlWorkerStream: &controlWorkerStream{entered: make(chan struct{}), released: make(chan struct{})},
		closeEntered: make(chan struct{}), closeRelease: make(chan struct{})}
	w := startControlWorkers(s, func() int64 { return 100 })
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCleanup()
	var releaseOnce sync.Once
	defer func() {
		releaseOnce.Do(func() { close(s.closeRelease) })
		if err := w.close(cleanup, s); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-s.entered:
	case <-cleanup.Done():
		t.Fatal(cleanup.Err())
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.close(expired, s); !errors.Is(err, context.Canceled) {
		t.Fatalf("failed cleanup became success: %v", err)
	}
	select {
	case <-s.closeEntered:
	case <-cleanup.Done():
		t.Fatal(cleanup.Err())
	}
	// Failure is already fixed. Only teardown now releases the owned I/O.
	releaseOnce.Do(func() { close(s.closeRelease) })
	if err := w.close(cleanup, s); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.writeDone:
	default:
		t.Fatal("writer survived forced cleanup")
	}
	select {
	case <-w.readDone:
	default:
		t.Fatal("reader survived forced cleanup")
	}
}
