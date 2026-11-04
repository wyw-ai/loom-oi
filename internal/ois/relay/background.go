package relay

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// janitorInterval is the resolution of prepare, close, idle and retention
// deadlines.
const janitorInterval = 50 * time.Millisecond

// Start runs the janitor, heartbeat, store probe and persistence loops
// until Shutdown.
func (h *Hub) Start() {
	if h.meta.Maintenance {
		h.log.Warn("maintenance mode: new channels are refused until an administrator resumes (ois admin resume)")
	}
	h.loops.Add(4)
	go h.every(janitorInterval, func() { h.sweep(h.now()) })
	go h.every(limits.D(h.lim.HeartbeatMS), h.heartbeatTick)
	go h.every(limits.D(h.lim.StoreProbeMS), h.probe)
	go h.persistLoop()
}

func (h *Hub) every(d time.Duration, fn func()) {
	defer h.loops.Done()
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

func (h *Hub) kickPersist() {
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

// probe runs Health every STORE_PROBE; recovery needs no restart (B3 §6).
// After a write failed, a read probe is not enough: the store comes back
// only when ProbeWrite commits (§8.1.1 disk full, read-only).
func (h *Hub) probe() {
	ctx, cancel := context.WithTimeout(h.ctx, limits.D(h.lim.StoreProbeMS))
	defer cancel()
	h.mu.Lock()
	latched := h.writeFailure != ""
	failures := h.storeFailures
	h.mu.Unlock()
	err := h.st.Health(ctx)
	if err == nil && latched {
		err = h.st.ProbeWrite(ctx)
	}
	if err == nil {
		// An unknown configuration commit may have entered maintenance or
		// revoked access. Reload it before permitting any new Open. Keep
		// adminMu until publishing recovery so a later failed write cannot
		// insert another stale snapshot in between.
		h.adminMu.Lock()
		defer h.adminMu.Unlock()
		h.mu.Lock()
		unchanged := failures == h.storeFailures
		h.mu.Unlock()
		if !unchanged {
			return
		}
		err = h.resyncIfStale(ctx)
	}
	h.mu.Lock()
	// A write (or read) can fail after this probe's I/O has succeeded,
	// before its caller receives the result. That newer failure must be
	// tested by a later probe, even if its category is unchanged.
	if failures != h.storeFailures {
		h.mu.Unlock()
		return
	}
	was := h.storeOK
	h.storeOK = err == nil
	if err == nil {
		h.writeFailure = ""
	} else if kind := store.FailureKind(err); latched && kind != "" {
		h.writeFailure = kind
	}
	h.mu.Unlock()
	switch {
	case err != nil && was:
		h.log.Error("store unavailable", "error", err.Error())
	case err == nil && !was:
		h.log.Info("store available again")
		h.kickPersist()
	}
}

func (h *Hub) persistLoop() {
	defer h.loops.Done()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-h.kick:
			if !h.persist() {
				time.AfterFunc(limits.D(h.lim.StoreProbeMS), h.kickPersist)
			}
		}
	}
}

type regJob struct {
	accessID string
	seq      int64
	entries  []store.Exposure
	audits   []store.AuditEntry
}

// persist writes a raised epoch and dirty registry snapshots. Registration
// is accepted in memory while the store is down and persisted here once it
// recovers (B3 §6). It reports whether everything was written.
func (h *Hub) persist() bool {
	ctx, cancel := context.WithTimeout(h.ctx, limits.D(h.lim.StoreProbeMS))
	defer cancel()
	ok := true
	h.mu.Lock()
	dirty, epoch, audit := h.epochDirty, h.epoch, h.epochAudit
	h.mu.Unlock()
	if dirty {
		entry := store.AuditEntry{}
		if audit != nil {
			entry = *audit
		}
		if _, err := h.st.RaiseEpoch(ctx, epoch, entry); err != nil {
			ok = false
			h.writeFailed(err)
		} else {
			h.mu.Lock()
			h.epochDirty = h.epoch != epoch
			if h.epochAudit == audit {
				h.epochAudit = nil
			}
			h.mu.Unlock()
		}
	}
	h.mu.Lock()
	var jobs []regJob
	for id, p := range h.regDirty {
		j := regJob{accessID: id, seq: p.seq, audits: slices.Clone(p.audits), entries: []store.Exposure{}}
		for _, e := range h.exposures {
			if e.rec.AccessID == id {
				j.entries = append(j.entries, e.rec)
			}
		}
		jobs = append(jobs, j)
	}
	h.mu.Unlock()
	for _, j := range jobs {
		err := h.st.UpsertRegistry(ctx, j.accessID, j.entries, j.audits)
		h.mu.Lock()
		p := h.regDirty[j.accessID]
		switch {
		case p == nil:
		case err == nil && p.seq == j.seq:
			delete(h.regDirty, j.accessID)
		case err == nil:
			p.audits = p.audits[min(len(j.audits), len(p.audits)):]
		case errors.Is(err, store.ErrInvalid):
			// The Access was deleted after this snapshot was taken.
			delete(h.regDirty, j.accessID)
		default:
			ok = false
		}
		h.mu.Unlock()
		if err != nil {
			h.writeFailed(err)
		}
	}
	return ok
}

// Shutdown refuses new work, aborts every channel with Shutdown, closes
// the control sessions and waits until ctx ends for connections to finish
// before dropping the rest.
func (h *Hub) Shutdown(ctx context.Context) {
	h.mu.Lock()
	h.shutting = true
	now := h.now()
	for _, ch := range h.channels {
		h.terminate(ch, protocol.CodeShutdown, now)
	}
	for _, a := range h.accesses {
		if a.sess != nil {
			h.endSession(a.sess, causeShutdown, "", "")
		}
	}
	h.mu.Unlock()
	done := make(chan struct{})
	go func() {
		h.conns.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	h.cancel()
	<-done
	h.loops.Wait()
}
