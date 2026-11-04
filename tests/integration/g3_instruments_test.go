package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/tests/fixture"
)

func g3InstrumentContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func g3InstrumentWait(t *testing.T, ctx context.Context, what string, fn func() bool) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !fn() {
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", what, ctx.Err())
		case <-tick.C:
		}
	}
}
func g3InstrumentPair(t *testing.T) (*g3FaultProxy, uint64, *net.TCPConn, *net.TCPConn) {
	t.Helper()
	ctx := g3InstrumentContext(t)
	ln := must(net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}))
	t.Cleanup(func() { _ = ln.Close() })
	p := must(newG3FaultProxy(ln.Addr().String(), resourceNS))
	t.Cleanup(func() { _ = p.Close() })
	c := must(net.DialTCP("tcp", nil, must(net.ResolveTCPAddr("tcp", p.Addr()))))
	t.Cleanup(func() { _ = c.Close() })
	_ = ln.SetDeadline(time.Now().Add(5 * time.Second))
	s := must(ln.AcceptTCP())
	t.Cleanup(func() { _ = s.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	ids := must(p.WaitAccepted(ctx, 0, 1))
	if len(ids) != 1 {
		t.Fatal(ids)
	}
	g3InstrumentWait(t, ctx, "upstream connected", func() bool { return p.Snapshot().Conns[0].ServerConnected })
	return p, ids[0], c, s
}
func g3AssertEOF(t *testing.T, r io.Reader) {
	t.Helper()
	var b [1]byte
	n, err := r.Read(b[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %d %v", n, err)
	}
}
func g3AssertReset(t *testing.T, c *net.TCPConn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	n, err := c.Read(b[:])
	var ne net.Error
	if n != 0 || err == nil || errors.Is(err, io.EOF) || (errors.As(err, &ne) && ne.Timeout()) {
		t.Fatalf("want actual TCP reset, got %d %v", n, err)
	}
}

func TestG3ProxyHalfClose(t *testing.T) {
	for _, first := range []string{g3ClientToServer, g3ServerToClient} {
		t.Run(first, func(t *testing.T) {
			p, id, c, s := g3InstrumentPair(t)
			ctx := g3InstrumentContext(t)
			a, b := c, s
			if first == g3ServerToClient {
				a, b = s, c
			}
			if _, err := a.Write([]byte("prefix")); err != nil {
				t.Fatal(err)
			}
			if err := a.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(b)
			if err != nil || string(got) != "prefix" {
				t.Fatalf("prefix %q %v", got, err)
			}
			done := make(chan error, 1)
			go func() {
				err := fixture.WritePathPattern(b, 1<<20)
				if err == nil {
					err = b.CloseWrite()
				}
				done <- err
			}()
			if err := fixture.CheckPathPattern(a, 1<<20); err != nil {
				t.Fatal(err)
			}
			g3AssertEOF(t, a)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if err := p.WaitClosed(ctx, id); err != nil {
				t.Fatal(err)
			}
			x := p.Snapshot().Conns[0]
			if !x.ClientToServer.EOFObserved || !x.ServerToClient.EOFObserved {
				t.Fatalf("missing independent FIN: %+v", x)
			}
			for _, d := range []g3DirectionSnapshot{x.ClientToServer, x.ServerToClient} {
				if d.ReadBytes != d.ForwardedBytes || d.BufferedBytes != 0 || d.MaxBufferedBytes > g3FrameBytes || d.MaxBufferedBytes == 0 {
					t.Fatalf("unbounded/lost bytes: %+v", d)
				}
			}
		})
	}
}
func TestG3ProxyDirectionalPause(t *testing.T) {
	for _, dir := range []string{g3ClientToServer, g3ServerToClient} {
		t.Run(dir, func(t *testing.T) {
			p, id, c, s := g3InstrumentPair(t)
			ctx := g3InstrumentContext(t)
			a, b := c, s
			if dir == g3ServerToClient {
				a, b = s, c
			}
			before := resourceNS()
			ev, err := p.PauseRead(ctx, id, dir)
			if err != nil {
				t.Fatal(err)
			}
			if ev.AppliedNS < before || ev.AppliedNS > resourceNS() || !ev.State.Paused || ev.State.BufferedBytes > g3FrameBytes {
				t.Fatalf("invalid pause ack: %+v", ev)
			}
			if _, err := a.Write([]byte("blocked")); err != nil {
				t.Fatal(err)
			}
			_ = b.SetReadDeadline(time.Now().Add(25 * time.Millisecond))
			var one [1]byte
			if _, err := b.Read(one[:]); err == nil {
				t.Fatal("paused direction forwarded data")
			} else {
				var ne net.Error
				if !errors.As(err, &ne) || !ne.Timeout() {
					t.Fatal(err)
				}
			}
			_ = b.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, err := b.Write([]byte("reverse")); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, 7)
			if _, err := io.ReadFull(a, got); err != nil || string(got) != "reverse" {
				t.Fatalf("reverse blocked %q %v", got, err)
			}
			if err := p.ResumeRead(id, dir); err != nil {
				t.Fatal(err)
			}
			if _, err := io.ReadFull(b, got); err != nil || string(got) != "blocked" {
				t.Fatalf("resume %q %v", got, err)
			}
			if _, err := p.PauseRead(ctx, id, "wrong"); err == nil {
				t.Fatal("accepted unknown direction")
			}
		})
	}
	t.Run("cancelled_pause_wait", func(t *testing.T) {
		p, id, _, _ := g3InstrumentPair(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// A cancelled waiter cannot force the proxy to wait forever; an ack
		// already reached concurrently is also a valid terminal result.
		_, err := p.PauseRead(ctx, id, g3ClientToServer)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
func TestG3ProxyReset(t *testing.T) {
	p, id, c, s := g3InstrumentPair(t)
	ctx := g3InstrumentContext(t)
	if err := p.BindRole(id, "provider", "lab-a", "ch-one"); err != nil {
		t.Fatal(err)
	}
	if err := p.BindRole(id, "provider", "lab-a", "ch-two"); err == nil {
		t.Fatal("connection rebound")
	}
	if err := p.BindRole(id, "control", "lab-a", "ch-one"); err == nil {
		t.Fatal("accepted invalid control identity")
	}
	before := resourceNS()
	at, err := p.Reset(id)
	if err != nil {
		t.Fatal(err)
	}
	if at < before || at > resourceNS() || p.Snapshot().Conns[0].ResetAtNS != at {
		t.Fatal("reset time was not its application point")
	}
	g3AssertReset(t, c)
	g3AssertReset(t, s)
	if err := p.WaitClosed(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Reset(id + 1); err == nil {
		t.Fatal("accepted unknown connection")
	}
	if err := p.BindRole(id+1, "api", "", ""); err == nil {
		t.Fatal("bound unknown connection")
	}
}
func TestG3ProxyCleanup(t *testing.T) {
	t.Run("held_handshake", func(t *testing.T) {
		ctx := g3InstrumentContext(t)
		ln := must(net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}))
		defer ln.Close()
		p := must(newG3FaultProxy(ln.Addr().String(), resourceNS))
		defer p.Close()
		p.HoldNew(true)
		c := must(net.Dial("tcp", p.Addr()))
		defer c.Close()
		ids := must(p.WaitAccepted(ctx, 0, 1))
		if err := p.BindRole(ids[0], "control", "lab-a", ""); err != nil {
			t.Fatal(err)
		}
		x := p.Snapshot().Conns[0]
		if !x.Pending || x.ServerConnected {
			t.Fatalf("held socket reached upstream: %+v", x)
		}
		_ = ln.SetDeadline(time.Now().Add(25 * time.Millisecond))
		if up, err := ln.Accept(); err == nil {
			up.Close()
			t.Fatal("hold dialed upstream")
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		if err := p.WaitClosed(ctx, ids[0]); err != nil {
			t.Fatal(err)
		}
		if got := p.Snapshot(); got.Workers != 0 || !got.Closed || !got.Conns[0].ClientClosed {
			t.Fatalf("cleanup incomplete: %+v", got)
		}
	})
	t.Run("release_and_nonreused_ID", func(t *testing.T) {
		ctx := g3InstrumentContext(t)
		ln := must(net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}))
		defer ln.Close()
		p := must(newG3FaultProxy(ln.Addr().String(), resourceNS))
		defer p.Close()
		p.HoldNew(true)
		c := must(net.Dial("tcp", p.Addr()))
		defer c.Close()
		ids := must(p.WaitAccepted(ctx, 0, 1))
		p.ReleaseNew()
		_ = ln.SetDeadline(time.Now().Add(time.Second))
		up := must(ln.Accept())
		defer up.Close()
		if _, err := p.Reset(ids[0]); err != nil {
			t.Fatal(err)
		}
		if err := p.WaitClosed(ctx, ids[0]); err != nil {
			t.Fatal(err)
		}
		mark := p.Mark()
		next := must(net.Dial("tcp", p.Addr()))
		defer next.Close()
		more := must(p.WaitAccepted(ctx, mark, 1))
		if more[0] <= ids[0] {
			t.Fatal("reused connection ID")
		}
	})
	t.Run("paused_half_closed", func(t *testing.T) {
		p, id, c, s := g3InstrumentPair(t)
		ctx := g3InstrumentContext(t)
		if _, err := p.PauseRead(ctx, id, g3ServerToClient); err != nil {
			t.Fatal(err)
		}
		if err := s.CloseWrite(); err != nil {
			t.Fatal(err)
		}
		// A paused downstream must not consume and propagate the server EOF.
		_ = c.SetReadDeadline(time.Now().Add(25 * time.Millisecond))
		var b [1]byte
		_, err := c.Read(b[:])
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			t.Fatalf("paused EOF leaked: %v", err)
		}
		x := p.Snapshot().Conns[0]
		if x.ServerToClient.EOFObserved || x.ClientClosed {
			t.Fatalf("blackhole closed consumer: %+v", x)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		if err := p.WaitClosed(ctx, id); err != nil {
			t.Fatal(err)
		}
		if p.Snapshot().Workers != 0 {
			t.Fatal("workers survived Close")
		}
	})
	t.Run("blocked_writer", func(t *testing.T) {
		p, id, c, s := g3InstrumentPair(t)
		ctx := g3InstrumentContext(t)
		_ = c.SetReadBuffer(g3FrameBytes)
		_ = s.SetWriteBuffer(g3FrameBytes)
		written := make(chan error, 1)
		go func() { written <- fixture.WritePathPattern(s, 64<<20) }()
		g3InstrumentWait(t, ctx, "downstream F in flight", func() bool {
			x := p.Snapshot().Conns[0].ServerToClient
			return x.BufferedBytes > 0 && x.ForwardedBytes > 0
		})
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		if err := p.WaitClosed(ctx, id); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-written:
			if err == nil {
				t.Fatal("fixture failed to block 64MiB")
			}
		case <-ctx.Done():
			t.Fatal("writer not released")
		}
		if x := p.Snapshot(); x.Workers != 0 || x.Conns[0].ServerToClient.MaxBufferedBytes > g3FrameBytes {
			t.Fatalf("cleanup/buffer: %+v", x)
		}
	})
}

func g3PathTarget(t *testing.T) *fixture.PathTarget {
	t.Helper()
	tg := must(fixture.NewPathTarget(resourceNS))
	t.Cleanup(func() { _ = tg.Close() })
	return tg
}
func g3PathDial(t *testing.T, tg *fixture.PathTarget, id string) *net.TCPConn {
	t.Helper()
	c := must(net.DialTCP("tcp", nil, must(net.ResolveTCPAddr("tcp", tg.Addr()))))
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(fixture.PathRequestHeader(id)); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestG3TargetEffectLatch(t *testing.T) {
	t.Run("repeat_ID_is_not_deduplicated", func(t *testing.T) {
		tg := g3PathTarget(t)
		ctx := g3InstrumentContext(t)
		if err := tg.AddPlan("same", fixture.PathPlan{RequestBytes: 4, ResponseBytes: 7, Pattern: true, HoldResponse: true}); err != nil {
			t.Fatal(err)
		}
		if err := tg.AddPlan("same", fixture.PathPlan{}); err == nil {
			t.Fatal("allowed plan replacement")
		}
		for occurrence := 1; occurrence <= 2; occurrence++ {
			c := g3PathDial(t, tg, "same")
			if err := fixture.WritePathPattern(c, 4); err != nil {
				t.Fatal(err)
			}
			if err := c.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			x := must(tg.WaitRequest(ctx, "same", occurrence))
			if err := x.Wait(ctx, fixture.PathEffectCommitted); err != nil {
				t.Fatal(err)
			}
			if s := x.Snapshot(); s.ResponseBytes != 0 || s.WriterEntered || s.SHA256 == "" {
				t.Fatalf("response escaped commit latch: %+v", s)
			}
			if tg.Snapshot().Effects["same"] != occurrence {
				t.Fatal("effect silently deduplicated")
			}
			x.ReleaseResponse()
			if err := fixture.CheckPathPattern(c, 7); err != nil {
				t.Fatal(err)
			}
			g3AssertEOF(t, c)
			if err := x.Wait(ctx, fixture.PathClosed); err != nil {
				t.Fatal(err)
			}
			if s := x.Snapshot(); s.Error != "" || !s.WriterReturned || !s.ResponseFIN {
				t.Fatalf("incomplete exchange %+v", s)
			}
		}
		if len(tg.Snapshot().Exchanges) != 2 {
			t.Fatal("accepted ID history lost")
		}
	})
	for _, name := range []string{"short_body", "bad_pattern", "unknown_ID"} {
		t.Run(name, func(t *testing.T) {
			tg := g3PathTarget(t)
			ctx := g3InstrumentContext(t)
			if err := tg.AddPlan("bad", fixture.PathPlan{RequestBytes: 4, Pattern: true}); err != nil {
				t.Fatal(err)
			}
			id := "bad"
			if name == "unknown_ID" {
				id = "unknown"
			}
			c := g3PathDial(t, tg, id)
			if name == "short_body" {
				_ = fixture.WritePathPattern(c, 3)
			} else if name == "bad_pattern" {
				_, _ = c.Write([]byte("bad!"))
			}
			_ = c.CloseWrite()
			x := must(tg.WaitRequest(ctx, id, 1))
			if err := x.Wait(ctx, fixture.PathClosed); err != nil {
				t.Fatal(err)
			}
			if x.Snapshot().Error == "" || len(tg.Snapshot().Effects) != 0 {
				t.Fatal("invalid request committed")
			}
			if x.Snapshot().WireReceivedBytes < int64(len(fixture.PathRequestHeader(id))) {
				t.Fatal("invalid request erased actual wire bytes")
			}
			if name == "bad_pattern" && x.Snapshot().ReceivedBytes != 4 {
				t.Fatal("invalid pattern was hidden from body byte count")
			}
		})
	}
	t.Run("split_marker", func(t *testing.T) {
		tg := g3PathTarget(t)
		ctx := g3InstrumentContext(t)
		if err := tg.AddPlan("markers", fixture.PathPlan{RequestBytes: -1, Observe: []string{"after-expiry:id"}}); err != nil {
			t.Fatal(err)
		}
		c := g3PathDial(t, tg, "markers")
		x := must(tg.WaitRequest(ctx, "markers", 1))
		_, _ = c.Write([]byte("before\nafter-"))
		g3InstrumentWait(t, ctx, "first marker segment", func() bool { return x.Snapshot().ReceivedBytes == 13 })
		_, _ = c.Write([]byte("expiry:id\n"))
		_ = c.CloseWrite()
		if err := x.Wait(ctx, fixture.PathClosed); err != nil {
			t.Fatal(err)
		}
		if x.Snapshot().Markers["after-expiry:id"] != 1 {
			t.Fatalf("split marker lost: %+v", x.Snapshot())
		}
	})
}
func TestG3TargetHalfClose(t *testing.T) {
	t.Run("consumer_FIN_first", func(t *testing.T) {
		tg := g3PathTarget(t)
		ctx := g3InstrumentContext(t)
		if err := tg.AddPlan("request", fixture.PathPlan{RequestBytes: 4, ResponseBytes: 1 << 20, Pattern: true, HoldResponse: true}); err != nil {
			t.Fatal(err)
		}
		c := g3PathDial(t, tg, "request")
		if err := fixture.WritePathPattern(c, 4); err != nil {
			t.Fatal(err)
		}
		_ = c.CloseWrite()
		x := must(tg.WaitRequest(ctx, "request", 1))
		if err := x.Wait(ctx, fixture.PathRequestFIN); err != nil {
			t.Fatal(err)
		}
		x.ReleaseResponse()
		if err := fixture.CheckPathPattern(c, 1<<20); err != nil {
			t.Fatal(err)
		}
		g3AssertEOF(t, c)
		if err := x.Wait(ctx, fixture.PathClosed); err != nil {
			t.Fatal(err)
		}
		if s := x.Snapshot(); s.Error != "" || s.ReceivedBytes != 4 || s.ResponseBytes != 1<<20 {
			t.Fatalf("half-close truncated: %+v", s)
		}
	})
	t.Run("target_FIN_first", func(t *testing.T) {
		tg := g3PathTarget(t)
		ctx := g3InstrumentContext(t)
		if err := tg.AddPlan("reverse", fixture.PathPlan{RequestBytes: 1 << 20, ResponseBytes: 17, Pattern: true, ResponseFirst: true, HoldRead: true}); err != nil {
			t.Fatal(err)
		}
		c := g3PathDial(t, tg, "reverse")
		x := must(tg.WaitRequest(ctx, "reverse", 1))
		if err := fixture.CheckPathPattern(c, 17); err != nil {
			t.Fatal(err)
		}
		g3AssertEOF(t, c)
		if err := x.Wait(ctx, fixture.PathResponseFIN); err != nil {
			t.Fatal(err)
		}
		if x.Snapshot().Closed {
			t.Fatal("target FIN prematurely closed reverse reader")
		}
		x.ReleaseRead()
		if err := fixture.WritePathPattern(c, 1<<20); err != nil {
			t.Fatal(err)
		}
		_ = c.CloseWrite()
		if err := x.Wait(ctx, fixture.PathClosed); err != nil {
			t.Fatal(err)
		}
		if s := x.Snapshot(); s.Error != "" || s.ReceivedBytes != 1<<20 || !s.RequestFIN || tg.Snapshot().Effects["reverse"] != 1 {
			t.Fatalf("reverse direction lost: %+v", s)
		}
	})
}
func TestG3TargetCleanup(t *testing.T) {
	for _, name := range []string{"waiting_header", "held_read", "held_response", "blocked_response"} {
		t.Run(name, func(t *testing.T) {
			tg := g3PathTarget(t)
			ctx := g3InstrumentContext(t)
			plan := fixture.PathPlan{RequestBytes: 0, ResponseBytes: 64 << 20, HoldRead: name == "held_read", HoldResponse: name == "held_response"}
			if err := tg.AddPlan("cleanup", plan); err != nil {
				t.Fatal(err)
			}
			var c *net.TCPConn
			if name == "waiting_header" {
				c = must(net.DialTCP("tcp", nil, must(net.ResolveTCPAddr("tcp", tg.Addr()))))
				defer c.Close()
				g3InstrumentWait(t, ctx, "accepted", func() bool { return len(tg.Snapshot().Exchanges) == 1 })
			} else {
				c = g3PathDial(t, tg, "cleanup")
				_ = c.SetReadBuffer(g3FrameBytes)
				_ = c.CloseWrite()
				x := must(tg.WaitRequest(ctx, "cleanup", 1))
				if name == "held_response" {
					if err := x.Wait(ctx, fixture.PathEffectCommitted); err != nil {
						t.Fatal(err)
					}
				}
				if name == "blocked_response" {
					if err := x.Wait(ctx, fixture.PathWriterEntered); err != nil {
						t.Fatal(err)
					}
					if x.Snapshot().WriterReturned {
						t.Fatal("64MiB was not blocked")
					}
				}
			}
			if err := tg.Close(); err != nil {
				t.Fatal(err)
			}
			s := tg.Snapshot()
			if !s.Closed || s.Workers != 0 || len(s.Exchanges) != 1 || !s.Exchanges[0].Closed {
				t.Fatalf("target workers survived: %+v", s)
			}
			if name == "held_read" && s.Exchanges[0].ReceivedBytes != 0 {
				t.Fatal("held read consumed body")
			}
			if name == "held_response" && s.Exchanges[0].ResponseBytes != 0 {
				t.Fatal("held response escaped before cancellation")
			}
		})
	}
	t.Run("cancelled_waits_and_pattern_helpers", func(t *testing.T) {
		tg := g3PathTarget(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := tg.WaitRequest(ctx, "missing", 1); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := tg.AddPlan("", fixture.PathPlan{}); err == nil {
			t.Fatal("accepted invalid ID")
		}
		if err := tg.AddPlan("x", fixture.PathPlan{Observe: []string{""}}); err == nil {
			t.Fatal("accepted empty marker")
		}
		if err := fixture.CheckPathPattern(bytes.NewReader([]byte("bad")), 3); err == nil {
			t.Fatal("pattern mismatch ignored")
		}
		if err := fixture.WritePathPattern(io.Discard, -1); err == nil {
			t.Fatal("negative length accepted")
		}
	})
}
