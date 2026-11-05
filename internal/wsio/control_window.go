package wsio

import (
	"encoding/binary"
	"errors"
	"math"
	"time"
)

type controlWindow struct {
	enabled    bool
	start      time.Time
	interval   time.Duration
	index      uint64 // zero is the initial, non-counting interval [t0,T1)
	life, pong bool
	misses     int
	err        error
}

// EnableProbe is called once at the original OIS Attach start, after Accept.
// Entering Open must not call it again. Pong-only endpoints never call it.
func (c *Control) EnableProbe(start time.Time, interval time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return ErrControlStopped
	}
	if c.window.enabled {
		return errors.New("data leg probes already enabled")
	}
	if start.IsZero() || interval <= 0 {
		return errors.New("invalid data leg probe clock")
	}
	c.window = controlWindow{enabled: true, start: start, interval: interval,
		life: c.busy[DataForwarding] || c.busy[DataPending]}
	return nil
}

// Advance settles elapsed half-open windows using the original monotonic
// clock. It creates no timer, goroutine or historical PING. The existing OIS
// coordinator calls it on its fixed tick and handles the returned terminal.
func (c *Control) Advance() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanceLocked(c.now())
	return c.window.err
}

// NextTick is the absolute next boundary, not now+interval. A caller waking
// late calls Advance before selecting another deadline.
func (c *Control) NextTick() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.window.enabled || c.stopped || c.window.err != nil {
		return time.Time{}, false
	}
	return c.window.start.Add(time.Duration(c.window.index) * c.window.interval).Add(c.window.interval), true
}

func (c *Control) retireLocked() {
	c.pendingPing = false
	if c.probe != probeNone {
		c.probe = probeRetired
	}
}

func (c *Control) advanceLocked(now time.Time) {
	w := &c.window
	if !w.enabled || c.stopped || w.err != nil || now.Before(w.start) {
		return
	}
	target := uint64(now.Sub(w.start) / w.interval)
	if target <= w.index {
		return
	}
	if c.busy[DataForwarding] || c.busy[DataPending] {
		// The existing owner spans every elapsed window. Arithmetic skipping
		// keeps a long scheduler pause constant-space and constant-time.
		c.retireLocked()
		w.index, w.life, w.pong, w.misses = target, true, false, 0
		return
	}
	// Without a DATA owner, at most one successful old window and two empty
	// windows can elapse before termination. Never loop over an unbounded gap.
	for w.index < target && w.err == nil {
		life := w.life
		if w.index != 0 {
			if life || w.pong {
				w.misses = 0
			} else {
				w.misses++
			}
			if w.misses == 2 {
				w.err = ErrIdleWindows
			}
		}
		c.retireLocked()
		w.index++
		w.life, w.pong = false, false
		if w.err == nil && w.index == target && !life && c.wire != controlPing {
			if c.nonce == math.MaxUint64 {
				w.err = ErrProbeExhausted
				return
			}
			c.nonce++
			binary.BigEndian.PutUint64(c.ping[:], c.nonce)
			c.pendingPing, c.probe = true, probeQueued
			c.signalLocked()
		}
	}
}

// ControlSnapshot is finite local diagnostic state, not a new wire contract.
type ControlSnapshot struct {
	Started, Stopped, WorkerDone        bool
	Forwarding, PendingData             bool
	PendingPongBytes, InflightPongBytes int
	PendingPong                         bool
	Wire                                string
	Probe                               string
	Nonce, Window                       uint64
	Life, ValidPong                     bool
	Misses                              int
	ProbeEnded                          bool
}

func (c *Control) Snapshot() ControlSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ControlSnapshot{Started: c.started, Stopped: c.stopped, WorkerDone: c.workerDone,
		Forwarding: c.busy[DataForwarding], PendingData: c.busy[DataPending],
		PendingPongBytes: c.pendingLen, InflightPongBytes: c.inflightLen, PendingPong: c.pendingPong,
		Wire:  [...]string{"none", "ping", "pong"}[c.wire],
		Probe: [...]string{"none", "queued", "claimed", "awaiting_pong", "retired"}[c.probe],
		Nonce: c.nonce, Window: c.window.index, Life: c.window.life, ValidPong: c.window.pong,
		Misses: c.window.misses, ProbeEnded: c.window.err != nil}
}
