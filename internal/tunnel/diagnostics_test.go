package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type diagnosticConn struct {
	*net.TCPConn
	readStarted, closeReturned chan struct{}
	readOnce, closeOnce        sync.Once
	closeErr                   error
}

func (c *diagnosticConn) Read(p []byte) (int, error) {
	c.readOnce.Do(func() { close(c.readStarted) })
	return c.TCPConn.Read(p)
}
func (c *diagnosticConn) CloseWrite() error {
	err := c.closeErr
	if err == nil {
		err = c.TCPConn.CloseWrite()
	}
	c.closeOnce.Do(func() { close(c.closeReturned) })
	return err
}
func diagnosticPair(t *testing.T) (*diagnosticConn, *net.TCPConn) {
	c, p := pair(t)
	return &diagnosticConn{TCPConn: c, readStarted: make(chan struct{}), closeReturned: make(chan struct{})}, p
}

// The same 15 request bytes and zero response bytes arise both before and
// after request FIN. Boundary records distinguish these states without using
// an increased idle budget or inferring FIN delivery from byte counters.
func TestBridgeBoundaryDiagnostics(t *testing.T) {
	for _, withFIN := range []bool{false, true} {
		name := "request_fin_not_received"
		if withFIN {
			name = "request_fin_received_target_silent"
		}
		t.Run(name, func(t *testing.T) {
			c, p := diagnosticPair(t)
			l := newLeg()
			b := New(context.Background(), l, c, 32<<10, grace, nil)
			done := run(b)
			waitTunnelSignal(t, c.readStarted, "target read entered")
			l.push(protocol.DataFrame([]byte("stream 67108864")))
			_ = p.SetReadDeadline(time.Now().Add(3 * time.Second))
			buf := make([]byte, 15)
			if _, err := io.ReadFull(p, buf); err != nil {
				t.Fatal(err)
			}
			if withFIN {
				l.push(protocol.FinFrame())
				if n, err := p.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
					t.Fatalf("request FIN: %d %v", n, err)
				}
				waitTunnelSignal(t, c.closeReturned, "target half close returned")
			}
			d := b.Diagnostics()
			if d.TargetReadStartedNS == 0 || d.TargetReadReturnedNS != 0 || d.LegWriteStartedNS != 0 || (d.FINReceivedNS > 0) != withFIN {
				t.Fatalf("wrong blocked boundary: %+v", d)
			}
			if d.LocalAddress != p.RemoteAddr().String() || d.PeerAddress != p.LocalAddr().String() {
				t.Fatalf("tuple: %+v", d)
			}
			b.Abort(protocol.CodeIdleTimeout)
			l.expectAbort(t, protocol.CodeIdleTimeout)
			_ = l.CloseNow()
			r := wait(t, done)
			if r.Code != protocol.CodeIdleTimeout || r.Up != 0 || r.Down != 15 {
				t.Fatalf("counterexample changed: %+v", r)
			}
			t.Logf("%s before_abort=%+v result=%+v", name, d, r)
		})
	}
	t.Run("target_response_waits_in_leg_write", func(t *testing.T) {
		c, p := diagnosticPair(t)
		l := &boundedBridgeLeg{fakeLeg: newLeg(), gate: make(chan struct{}), entered: make(chan struct{})}
		b := New(context.Background(), l, c, 32<<10, grace, nil)
		done := run(b)
		l.push(protocol.FinFrame())
		if got := readAll(t, p); got != "" {
			t.Fatal(got)
		}
		if _, err := p.Write([]byte("reply")); err != nil {
			t.Fatal(err)
		}
		waitTunnelSignal(t, l.entered, "DATA write entered")
		d := b.Diagnostics()
		if d.FINReceivedNS == 0 || d.TargetReadReturnedNS == 0 || d.TargetReadBytes != 5 || d.TargetReadResult != "ok" || d.LegWriteStartedNS == 0 || d.LegWriteReturnedNS != 0 || d.LegWriteResult != "pending" {
			t.Fatalf("wrong blocked write boundary: %+v", d)
		}
		close(l.gate)
		if f := l.frame(t); f.Type != protocol.FrameData || string(f.Data) != "reply" {
			t.Fatalf("%+v", f)
		}
		_ = p.CloseWrite()
		if f := l.frame(t); f.Type != protocol.FrameFin {
			t.Fatalf("%+v", f)
		}
		_ = l.CloseNow()
		if r := wait(t, done); r.Code != protocol.CodeCompleted || r.Up != 5 {
			t.Fatalf("%+v", r)
		}
		final := b.Diagnostics()
		if final.LegWriteReturnedNS < final.LegWriteStartedNS || final.LegWriteResult != "ok" || final.CloseWriteResult != "ok" || final.SampledNS < final.LegWriteReturnedNS {
			t.Fatalf("%+v", final)
		}
		t.Logf("blocked=%+v final=%+v", d, final)
	})
}

func TestBridgeBoundaryOutcomesAreFinite(t *testing.T) {
	for _, outcome := range []string{"read_eof", "read_error", "write_error", "half_close_error"} {
		t.Run(outcome, func(t *testing.T) {
			c, p := diagnosticPair(t)
			l := newLeg()
			secretErr := errors.New("private raw error must not be exported")
			if outcome == "read_error" {
				_ = c.Close()
			}
			if outcome == "write_error" {
				l.writeErr.Store(&secretErr)
			}
			if outcome == "half_close_error" {
				c.closeErr = secretErr
			}
			b := New(context.Background(), l, c, 32<<10, grace, nil)
			done := run(b)
			waitTunnelSignal(t, c.readStarted, "read started")
			switch outcome {
			case "read_eof":
				_ = p.CloseWrite()
				if f := l.frame(t); f.Type != protocol.FrameFin {
					t.Fatalf("%+v", f)
				}
				l.push(protocol.FinFrame())
			case "write_error":
				if _, err := p.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			case "half_close_error":
				l.push(protocol.FinFrame())
				waitTunnelSignal(t, c.closeReturned, "CloseWrite returned")
				_ = p.CloseWrite()
				if f := l.frame(t); f.Type != protocol.FrameFin {
					t.Fatalf("%+v", f)
				}
			}
			waitTunnelSignal(t, b.Done(), "terminal result")
			_ = l.CloseNow()
			_ = wait(t, done)
			d := b.Diagnostics()
			if outcome == "read_eof" && d.TargetReadResult != "eof" {
				t.Fatalf("%+v", d)
			}
			if outcome == "read_error" && d.TargetReadResult != "error" {
				t.Fatalf("%+v", d)
			}
			if outcome == "write_error" && d.LegWriteResult != "error" {
				t.Fatalf("%+v", d)
			}
			if outcome == "half_close_error" && d.CloseWriteResult != "error" {
				t.Fatalf("%+v", d)
			}
			data, _ := json.Marshal(d)
			if strings.Contains(string(data), "private raw") {
				t.Fatal(string(data))
			}
		})
	}
}
