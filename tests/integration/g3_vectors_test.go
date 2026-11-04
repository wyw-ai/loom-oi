package integration

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

const g3LargeBytes int64 = 64 << 20
const g3TailBytes int64 = 1 << 20

func g3Context(t *testing.T, duration time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	t.Cleanup(cancel)
	return ctx
}

// Business records use the same controller monotonic clock as the instruments
// and resource phases. They retain individual facts; Go assertions are separate.
func (g *g3Env) event(kind, event string, fields map[string]any) {
	fields["schema"], fields["stage"] = "g3-behavior-v1", "validation"
	fields["package"] = "github.com/wyw-ai/loom-oi/tests/integration"
	if !g.resources.preparationComplete {
		fields["stage"] = "preparation"
	}
	fields["vector"], fields["scenario"] = g.resources.vector, g.resources.scenario
	fields["event"], fields["observed_ns"] = event, resourceNS()
	g.obs(kind, fields)
}

func (g *g3Env) connRef(p *g3FaultProxy, id uint64) string {
	if p == g.consumerProxy {
		return g3ConnID("consumer", id)
	}
	return g3ConnID("access/lab-a", id)
}

func g3Check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func g3WaitResult(ctx context.Context, ch <-chan error) error {
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func g3Delay(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type g3Path struct {
	open               protocol.OpenResponse
	stream             *client.Stream
	provider, consumer uint64
}

func (g *g3Env) dialPath(ctx context.Context) g3Path {
	g.t.Helper()
	o, provider, err := g.openPath(ctx, "alice", "lab-a/http")
	g3Check(g.t, err)
	s, consumer, err := g.attachPath(ctx, "alice", o)
	g3Check(g.t, err)
	// A failed test's watchdog releases real socket IO; it never counts as a
	// successful cleanup. Normal vectors finish before this cancellation.
	stop := context.AfterFunc(ctx, func() {
		_, _ = g.accessProxies["lab-a"].Reset(provider)
		_, _ = g.consumerProxy.Reset(consumer)
	})
	g.t.Cleanup(func() {
		stop()
		_, _ = g.accessProxies["lab-a"].Reset(provider)
		_, _ = g.consumerProxy.Reset(consumer)
	})
	g.event("G3-transfer", "ready", map[string]any{"channel_id": o.ChannelID,
		"provider_conn_id": g3ConnID("access/lab-a", provider), "consumer_conn_id": g3ConnID("consumer", consumer)})
	return g3Path{o, s, provider, consumer}
}

func (g *g3Env) channelState(ctx context.Context, id string) (protocol.ChannelView, error) {
	mark := g.consumerProxy.Mark()
	v, err := g.consumer("alice").Channel(ctx, id)
	if bindErr := g.bindAPI(mark); bindErr != nil {
		return v, bindErr
	}
	return v, err
}

func (g *g3Env) waitReason(ctx context.Context, id string, want protocol.Code) error {
	_, err := g.waitReasonAt(ctx, id, want)
	return err
}

func (g *g3Env) waitReasonAt(ctx context.Context, id string, want protocol.Code) (int64, error) {
	var observed int64
	err := g3Poll(ctx, func() (bool, error) {
		v, err := g.channelState(ctx, id)
		if err != nil {
			return false, err
		}
		if v.State == protocol.StateClosed {
			observed = resourceNS()
			g.event("G3-transfer", "channel_closed", map[string]any{"channel_id": id, "state": v.State, "reason": v.CloseReason, "terminal_observed_ns": observed})
			if v.CloseReason != want {
				return false, fmt.Errorf("channel %s closed %s, want %s", id, v.CloseReason, want)
			}
			return true, nil
		}
		return false, nil
	})
	return observed, err
}

func g3ReadEOF(r io.Reader) error {
	var b [1]byte
	n, err := r.Read(b[:])
	if n != 0 || err != io.EOF {
		return fmt.Errorf("response FIN: read %d, %v; want 0, EOF", n, err)
	}
	return nil
}

func (g *g3Env) finishExchange(ctx context.Context, p g3Path, x *fixture.PathExchange, requestBytes, responseBytes int64) {
	g.t.Helper()
	g3Check(g.t, x.Wait(ctx, fixture.PathClosed))
	snap := x.Snapshot()
	if !snap.RequestFIN || !snap.ResponseFIN || snap.Error != "" || snap.ReceivedBytes != requestBytes || snap.ResponseBytes != responseBytes {
		g.t.Fatalf("incomplete exchange: %+v", snap)
	}
	g3Check(g.t, g.waitReason(ctx, p.open.ChannelID, protocol.CodeCompleted))
	// OIS has already completed and closed the leg. CloseNow may report that
	// the network connection is already closed; the preceding FIN/content and
	// OIS terminal-state checks establish successful completion.
	_ = p.stream.Close()
	g.event("G3-transfer", "exchange_complete", map[string]any{"channel_id": p.open.ChannelID, "request_id": snap.RequestID,
		"request_bytes": requestBytes, "response_bytes": responseBytes, "target": snap, "effects": g.targets["lab-a"].Snapshot().Effects})
}

type g3CountingWriter struct {
	w       io.Writer
	bytes   atomic.Int64
	entered chan struct{}
	once    atomic.Bool
}

func (w *g3CountingWriter) Write(p []byte) (int, error) {
	if w.once.CompareAndSwap(false, true) {
		close(w.entered)
	}
	n, err := w.w.Write(p)
	w.bytes.Add(int64(n))
	return n, err
}

func (g *g3Env) boundedBuffers(ctx context.Context) map[string]any {
	g.t.Helper()
	sample := must(g.resourceSnapshot(ctx))
	o, a := sample.OIS, sample.OICs["lab-a"]
	if o.Stats.MaxBufferedBytes > 2*g3FrameBytes || a.Stats.MaxBufferedBytes > 2*g3FrameBytes {
		g.t.Fatalf("application buffers exceed 2F: OIS %d OIC %d", o.Stats.MaxBufferedBytes, a.Stats.MaxBufferedBytes)
	}
	for _, p := range []*g3FaultProxy{g.consumerProxy, g.accessProxies["lab-a"]} {
		for _, c := range p.Snapshot().Conns {
			if c.ClientToServer.MaxBufferedBytes > g3FrameBytes || c.ServerToClient.MaxBufferedBytes > g3FrameBytes {
				g.t.Fatalf("proxy exceeds per-direction F: %+v", c)
			}
		}
	}
	return map[string]any{"ois_max_buffered": o.Stats.MaxBufferedBytes, "oic_max_buffered": a.Stats.MaxBufferedBytes,
		"F": g3FrameBytes, "consumer_proxy": g.consumerProxy.Snapshot(), "provider_proxy": g.accessProxies["lab-a"].Snapshot()}
}
