package relay

import (
	"bytes"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The first invocation completes real I/O, then yields its return to the test.
// This is the ordering allowed when an Upgrade response arrives before the
// transport's request-writing goroutine returns from net.Conn.Write.
type completedWriteReturnGate struct {
	net.Conn
	first   atomic.Bool
	written chan struct{}
	release chan struct{}
}

func (c *completedWriteReturnGate) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err == nil && n == len(p) && c.first.CompareAndSwap(false, true) {
		close(c.written)
		<-c.release
	}
	return n, err
}

func TestControlWriterHandoffGateBindsWriteEntry(t *testing.T) {
	conn, peer := net.Pipe()
	old := &completedWriteReturnGate{Conn: conn, written: make(chan struct{}), release: make(chan struct{})}
	gate := &handoffConn{Conn: old, written: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	var oldRelease, selectedRelease sync.Once
	var workers sync.WaitGroup
	t.Cleanup(func() {
		oldRelease.Do(func() { close(old.release) })
		selectedRelease.Do(func() { close(gate.release) })
		_ = gate.Close()
		_ = peer.Close()
		workers.Wait()
	})
	if err := peer.SetReadDeadline(time.Now().Add(wait)); err != nil {
		t.Fatal(err)
	}
	type result struct {
		n   int
		err error
	}
	write := func(data []byte) <-chan result {
		done := make(chan result, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			n, err := gate.Write(data)
			done <- result{n, err}
		}()
		return done
	}
	readExact := func(want []byte) {
		t.Helper()
		got := make([]byte, len(want))
		if _, err := io.ReadFull(peer, got); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("peer did not receive complete original bytes: %q %v", got, err)
		}
	}
	first := []byte("already-started upgrade request")
	firstDone := write(first) // Unarmed at invocation entry.
	readExact(first)
	<-old.written
	gate.armed.Store(true) // A future control write is now eligible.
	oldRelease.Do(func() { close(old.release) })
	select {
	case <-gate.written:
		t.Fatal("late return of a preexisting write consumed the future arm")
	case got := <-firstDone:
		if got.err != nil || got.n != len(first) || !gate.armed.Load() {
			t.Fatal("old write did not return with the future arm intact", got)
		}
	case <-time.After(wait):
		t.Fatal("old write did not return after its own gate released")
	}
	second := []byte("selected control write")
	secondDone := write(second)
	readExact(second)
	select {
	case <-gate.written:
	case <-time.After(wait):
		t.Fatal("future write did not acquire the retained arm")
	}
	if gate.armed.Load() {
		t.Fatal("selected write did not consume its arm")
	}
	select {
	case got := <-secondDone:
		t.Fatal("selected real write returned before release", got)
	default:
	}
	selectedRelease.Do(func() { close(gate.release) })
	select {
	case got := <-secondDone:
		if got.err != nil || got.n != len(second) {
			t.Fatal("released write lost its real result", got)
		}
	case <-time.After(wait):
		t.Fatal("released write did not join")
	}
}
