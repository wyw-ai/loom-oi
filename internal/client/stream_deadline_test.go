package client

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type streamDeadlineLeg struct {
	*cancelLeg
	mu                                     sync.Mutex
	deadline, abortDeadline, drainDeadline time.Time
}

func (l *streamDeadlineLeg) SetCloseDeadline(d time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.deadline.IsZero() || d.Before(l.deadline) {
		l.deadline = d
	}
}
func (l *streamDeadlineLeg) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	if f, err := protocol.DecodeFrame(p, 32768); err == nil && f.Type == protocol.FrameAbort {
		l.mu.Lock()
		l.abortDeadline, _ = ctx.Deadline()
		l.mu.Unlock()
	}
	return l.cancelLeg.Write(ctx, typ, p)
}
func (l *streamDeadlineLeg) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	l.mu.Lock()
	l.drainDeadline, _ = ctx.Deadline()
	l.mu.Unlock()
	return l.cancelLeg.Read(ctx)
}
func (l *streamDeadlineLeg) Reader(ctx context.Context) (websocket.MessageType, io.Reader, error) {
	typ, p, err := l.Read(ctx)
	return typ, bytes.NewReader(p), err
}

func TestStreamCloseUsesFirstDeadline(t *testing.T) {
	for _, mode := range []string{"blocked_abort_consumes_grace", "original_abort_cap", "peer_closes", "completed_keeps_drain"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				leg := &streamDeadlineLeg{cancelLeg: newCancelLeg()}
				s := &Stream{leg: leg, dataMax: 32768, closeGrace: time.Second}
				start := time.Now()
				switch mode {
				case "blocked_abort_consumes_grace":
					leg.blockWrite = true
					if err := s.Close(); err != nil {
						t.Fatal(err)
					}
					if !time.Now().Equal(start.Add(time.Second)) || !leg.abortDeadline.Equal(start.Add(time.Second)) || !leg.drainDeadline.Equal(leg.abortDeadline) {
						t.Fatalf("ABORT/drain renewed original D: elapsed=%v abort=%v drain=%v", time.Since(start), leg.abortDeadline.Sub(start), leg.drainDeadline.Sub(start))
					}
				case "original_abort_cap":
					s.closeGrace = 5 * time.Second
					leg.blockWrite = true
					s.Abort(protocol.CodeClosedByClient)
					if !time.Now().Equal(start.Add(2*time.Second)) || !leg.abortDeadline.Equal(start.Add(2*time.Second)) || !leg.deadline.Equal(start.Add(5*time.Second)) {
						t.Fatal("original 2s ABORT cap changed")
					}
					if err := s.Close(); err != nil {
						t.Fatal(err)
					}
				case "peer_closes":
					leg.in <- cancelMessage{err: io.EOF}
					if err := s.Close(); err != nil {
						t.Fatal(err)
					}
					if !time.Now().Equal(start) || !leg.drainDeadline.Equal(start.Add(time.Second)) {
						t.Fatal("peer close delayed or drain D changed")
					}
				case "completed_keeps_drain":
					if err := s.CloseWrite(); err != nil {
						t.Fatal(err)
					}
					leg.push(protocol.FinFrame())
					if _, err := s.Read(make([]byte, 1)); err != io.EOF {
						t.Fatal(err)
					}
					finished := make(chan error, 1)
					go func() { finished <- s.Close() }()
					synctest.Wait()
					time.Sleep(2 * time.Second)
					synctest.Wait()
					select {
					case err := <-finished:
						t.Fatal("Completed gained a force-close deadline", err)
					default:
					}
					if !leg.drainDeadline.IsZero() || !leg.deadline.Equal(start.Add(time.Second)) {
						t.Fatal("Completed changed drain or first join D")
					}
					leg.in <- cancelMessage{err: io.EOF}
					if err := <-finished; err != nil {
						t.Fatal(err)
					}
				}
				select {
				case <-leg.closed:
				default:
					t.Fatal("transport not closed")
				}
				if leg.active.Load() != 0 {
					t.Fatal("I/O worker remains")
				}
			})
		})
	}
}
