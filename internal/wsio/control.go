package wsio

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrControlStopped = errors.New("data control owner is stopped")
	ErrControlStarted = errors.New("data control owner is already started")
	ErrControlPayload = errors.New("data control payload exceeds 125 bytes")
	ErrDataOwner      = errors.New("data activity slot is invalid or already owned")
	ErrIdleWindows    = errors.New("data leg missed two idle observation windows")
	ErrProbeExhausted = errors.New("data leg probe nonce exhausted")
)

// ControlWriter writes one real control frame, without waiting for a reply or
// imposing a per-probe timeout. Its context belongs to the transport lifetime.
type ControlWriter interface {
	WritePing(context.Context, []byte) error
	WritePong(context.Context, []byte) error
}

// DataSlot identifies the two existing, bounded DATA owners of one leg.
// Neither a control write nor application work is a DATA owner.
type DataSlot uint8

const (
	DataForwarding DataSlot = iota // accepted DATA awaiting forwarding to the other leg
	DataPending                    // accepted DATA queued for, or writing to, this leg
)

// DataOwner is one lifetime, not a reusable ID. End is safe to call more than
// once. There can be at most one live owner per slot, and no owner map.
type DataOwner struct {
	control *Control
	slot    DataSlot
	ended   atomic.Bool
}

func (o *DataOwner) End() {
	if o == nil || o.control == nil || o.ended.Swap(true) {
		return
	}
	c := o.control
	c.mu.Lock()
	c.advanceLocked(c.now()) // settle elapsed windows before releasing busy
	c.busy[o.slot] = false
	c.mu.Unlock()
}

// Control owns a fixed pending PONG, one in-flight control write, one probe
// nonce, and a single worker. Construct it before installing WebSocket hooks;
// Start after the handshake. Only OIS enables probes, at the original Attach
// start time. The caller retains its existing reader and tick coordinator.
//
// No method invokes an external callback under mu. Failed only signals a real
// write/input error: the original coordinator decides the first terminal cause.
// Advance reports idle expiry separately. Neither path closes the transport.
type Control struct {
	mu                           sync.Mutex
	now                          func() time.Time
	ctx                          context.Context
	cancel                       context.CancelFunc
	wake                         chan struct{}
	done                         chan struct{}
	failed                       chan struct{}
	doneOnce                     sync.Once
	started, stopped, workerDone bool
	err                          error
	writer                       ControlWriter
	pending                      [125]byte
	pendingLen                   int
	pendingPong                  bool
	inflight                     [125]byte
	inflightLen                  int
	wire                         controlKind
	last                         controlKind
	ping                         [8]byte
	pendingPing                  bool
	probe                        probePhase
	nonce                        uint64
	busy                         [2]bool
	window                       controlWindow
}

type controlKind uint8

const (
	controlNone controlKind = iota
	controlPing
	controlPong
)

type probePhase uint8

const (
	probeNone probePhase = iota
	probeQueued
	probeClaimed
	probeAwaiting
	probeRetired
)

func NewControl(now func() time.Time) *Control {
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Control{now: now, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1),
		done: make(chan struct{}), failed: make(chan struct{}), last: controlPing}
}

func (c *Control) Start(writer ControlWriter) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return ErrControlStopped
	}
	if c.started {
		return ErrControlStarted
	}
	if writer == nil {
		return errors.New("nil data control writer")
	}
	c.writer, c.started = writer, true
	go c.run()
	return nil
}

// OnPing is installed directly as the WebSocket OnPingReceived hook. The
// library has already validated the frame. Never retain its payload or its
// short read-control context. False suppresses the default synchronous PONG.
func (c *Control) OnPing(_ context.Context, payload []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || c.err != nil {
		return false
	}
	if len(payload) > len(c.pending) {
		c.failLocked(ErrControlPayload)
		return false
	}
	c.pendingLen = copy(c.pending[:], payload)
	c.pendingPong = true // replace only the not-yet-claimed PONG
	c.signalLocked()
	return false
}

// OnPong accepts only the current, already-claimed probe. A peer can return its
// PONG before the local Write call returns; it cannot answer a queued probe.
func (c *Control) OnPong(_ context.Context, payload []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanceLocked(c.now())
	if c.stopped || c.window.err != nil || !c.window.enabled || len(payload) != 8 ||
		(c.probe != probeClaimed && c.probe != probeAwaiting) ||
		binary.BigEndian.Uint64(payload) != c.nonce {
		return
	}
	c.window.pong = true
	c.window.misses = 0
}

// Data records only an accepted legal DATA message or a successful real DATA
// write. It does not update the caller's channel IDLE or authorization clocks.
func (c *Control) Data() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanceLocked(c.now())
	if c.stopped || c.window.err != nil {
		return
	}
	c.window.life, c.window.misses = true, 0
}

func (c *Control) BeginData(slot DataSlot) (*DataOwner, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanceLocked(c.now())
	if c.stopped {
		return nil, ErrControlStopped
	}
	if c.window.err != nil {
		return nil, c.window.err
	}
	if slot > DataPending || c.busy[slot] {
		return nil, ErrDataOwner
	}
	c.busy[slot] = true
	c.window.life, c.window.misses = true, 0
	return &DataOwner{control: c, slot: slot}, nil
}

func (c *Control) Failed() <-chan struct{} { return c.failed }
func (c *Control) Done() <-chan struct{}   { return c.done }
func (c *Control) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Stop is for the original transport-close point, not the business terminal
// decision. It does not call CloseNow, wait for a PONG, or join a worker. The
// caller must unblock the actual transport before Join, using the original
// cleanup deadline. Expected cancellation during Stop is not a new failure.
func (c *Control) Stop() {
	c.mu.Lock()
	c.stopped = true
	c.pendingPong, c.pendingLen = false, 0
	c.retireLocked()
	if !c.started {
		c.workerDone = true
		c.doneOnce.Do(func() { close(c.done) })
	}
	c.mu.Unlock()
	c.cancel()
}

func (c *Control) Join(ctx context.Context) error {
	// A completed join remains successful, including a later idempotent call
	// after the caller's cleanup context expires. Never randomly choose the
	// canceled branch when completion was already observable on entry.
	select {
	case <-c.done:
		return nil
	default:
	}
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Control) signalLocked() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Control) failLocked(err error) {
	if c.err == nil {
		c.err = err
		close(c.failed)
		c.signalLocked()
	}
}

func (c *Control) run() {
	defer func() {
		c.mu.Lock()
		c.workerDone = true
		c.mu.Unlock()
		c.doneOnce.Do(func() { close(c.done) })
	}()
	for {
		c.mu.Lock()
		c.advanceLocked(c.now())
		if c.stopped || c.err != nil {
			c.mu.Unlock()
			return
		}
		kind := controlNone
		if c.pendingPing && (!c.pendingPong || c.last == controlPong) {
			kind = controlPing
		} else if c.pendingPong {
			kind = controlPong
		}
		if kind == controlNone {
			c.mu.Unlock()
			select {
			case <-c.ctx.Done():
				return
			case <-c.wake:
			}
			continue
		}
		c.wire, c.last = kind, kind
		var payload []byte
		if kind == controlPing {
			c.pendingPing, c.probe = false, probeClaimed
			payload = c.ping[:]
		} else {
			c.inflightLen = copy(c.inflight[:], c.pending[:c.pendingLen])
			c.pendingPong, c.pendingLen = false, 0
			payload = c.inflight[:c.inflightLen]
		}
		c.mu.Unlock()
		var err error
		if kind == controlPing {
			err = c.writer.WritePing(c.ctx, payload)
		} else {
			err = c.writer.WritePong(c.ctx, payload)
		}
		c.mu.Lock()
		c.wire, c.inflightLen = controlNone, 0
		if err != nil {
			if !c.stopped {
				c.failLocked(err)
			}
			c.mu.Unlock()
			return
		}
		c.advanceLocked(c.now())
		if kind == controlPing && c.probe == probeClaimed {
			c.probe = probeAwaiting
		}
		c.mu.Unlock()
	}
}
