package integration

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// The context timer and the two socket timers need not publish simultaneously.
// Each socket must independently terminate a real blocked Write using the
// original clipped deadline; a test-only watchdog can only fail and clean up.
func TestG2PreDialSocketWriteDeadline(t *testing.T) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var peers []*net.TCPConn
	var peerErr error
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for i := 0; i < 2; i++ {
			c, err := listener.AcceptTCP()
			mu.Lock()
			if err == nil {
				peers = append(peers, c)
				err = c.SetReadBuffer(1024)
			}
			if err != nil {
				peerErr = err
			}
			mu.Unlock()
			if err != nil {
				return
			}
		}
		// No peer reads any bytes. Keep both receive windows bounded until the
		// original socket deadlines or the failing safety cleanup closes them.
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-accepted:
		case <-time.After(2 * time.Second):
			t.Error("socket deadline accept worker did not join")
		}
		mu.Lock()
		for _, c := range peers {
			_ = c.Close()
		}
		mu.Unlock()
	})
	// A single large Windows TCP Write can finish after queueing that complete
	// buffer. Use bounded consecutive chunks so an actual in-flight Write must
	// cross the deadline; exhausting the bound is an input failure, not a pass.
	payload := bytes.Repeat([]byte{'x'}, 64<<10)
	const maxBytes = 32 << 20
	parentD := time.Now().Add(150 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), parentD)
	defer cancel()
	notifications := 0
	slow, err := g2BeginSlowScrapesDiagnostic(ctx, listener.Addr().String(), &net.Dialer{}, func(ctx context.Context, s *g2SlowScrapes) error {
		actualD, ok := ctx.Deadline()
		if !ok || !actualD.Equal(parentD) || s.helperContextDeadlineNS != int64(parentD.Sub(resourceOrigin)) {
			t.Error("socket probe lost the original clipped deadline")
			return context.DeadlineExceeded
		}
		watchdogD := parentD.Add(500 * time.Millisecond)
		unclippedD := resourceOrigin.Add(time.Duration(s.helperStartedNS)).Add(time.Second)
		if !watchdogD.Before(unclippedD) || !time.Now().Before(parentD) {
			t.Error("socket probe cannot distinguish the original deadline from a renewed one")
			return context.DeadlineExceeded
		}
		select {
		case <-accepted:
		case <-ctx.Done():
			t.Error("two nonreading peers were not ready before original deadline")
			return ctx.Err()
		}
		mu.Lock()
		count, acceptErr := len(peers), peerErr
		mu.Unlock()
		if acceptErr != nil || count != 2 || len(s.conns) != 2 {
			t.Error("socket probe did not establish two actual peers", count, acceptErr)
			return context.DeadlineExceeded
		}
		for _, c := range s.conns {
			if err := c.SetWriteBuffer(1024); err != nil {
				t.Error(err)
				return err
			}
		}
		type writeResult struct {
			socket                         int
			started, lastStarted, returned time.Time
			n, calls                       int
			err                            error
		}
		results := make(chan writeResult, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, c := range s.conns {
			wg.Add(1)
			go func(i int, c *net.TCPConn) {
				defer wg.Done()
				<-start
				r := writeResult{socket: i, started: time.Now()}
				for r.n < maxBytes {
					at := time.Now()
					if !at.Before(parentD) {
						// Do not manufacture a timeout by starting a new Write after D.
						break
					}
					r.lastStarted = at
					r.calls++
					n, err := c.Write(payload)
					r.n += n
					r.err = err
					if err != nil || n != len(payload) {
						break
					}
				}
				r.returned = time.Now()
				results <- r
			}(i, c)
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		close(start)
		timer := time.NewTimer(time.Until(watchdogD))
		defer timer.Stop()
		watchdog := false
		select {
		case <-done:
			if !time.Now().Before(watchdogD) {
				watchdog = true
			}
		case <-timer.C:
			watchdog = true
		}
		if watchdog {
			t.Error("original socket deadlines did not finish both blocked writes before the test safety boundary")
			for _, c := range s.conns {
				_ = c.Close()
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("socket deadline writers did not join after failing cleanup")
				return context.DeadlineExceeded
			}
		}
		for i := 0; i < 2; i++ {
			r := <-results
			t.Logf("socket=%d started_to_D_ns=%d last_write_to_D_ns=%d returned_after_D_ns=%d calls=%d bytes=%d watchdog=%t error=%v", r.socket, parentD.Sub(r.started), parentD.Sub(r.lastStarted), r.returned.Sub(parentD), r.calls, r.n, watchdog, r.err)
			var timeout net.Error
			if !r.started.Before(parentD) || r.lastStarted.IsZero() || !r.lastStarted.Before(parentD) || r.returned.Before(parentD) || r.n <= 0 || r.n >= maxBytes || !errors.As(r.err, &timeout) || !timeout.Timeout() || watchdog {
				t.Error("socket did not independently demonstrate its original blocked-write timeout", r.socket, r.n, r.err)
			}
		}
		return nil
	}, func(s *g2SlowScrapes, err error) {
		notifications++
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Error("socket probe lost the original helper failure", err)
		}
	})
	if slow != nil {
		defer slow.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) || slow == nil || notifications != 1 {
		t.Fatal("socket probe bypassed the original post-callback gate", err, notifications)
	}
	for _, w := range slow.Snapshot() {
		if w.StartedNS != 0 || w.CompletedNS != 0 || w.PlannedBytes != 0 || w.WrittenBytes != 0 {
			t.Error("socket probe released actual metrics request writers", w)
		}
	}
	select {
	case <-slow.done:
	default:
		t.Error("socket probe helper ownership did not finish")
	}
}
