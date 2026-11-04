package relay

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// SetMaintenance turns maintenance mode on or off as a configuration write
// (B3 §8). While it is on, new Opens are refused with Maintenance; the
// administrator re-applies what a restore lost and then resumes.
func (h *Hub) SetMaintenance(ctx context.Context, p Principal, ifMatch int64, on bool) (int64, error) {
	return h.write(ctx, p, ifMatch, func(time.Time) ([]store.Mutation, []store.AuditEntry, error) {
		action := "maintenance_resumed"
		if on {
			action = "maintenance_entered"
		}
		return []store.Mutation{store.SetMaintenance{On: on}},
			[]store.AuditEntry{auditEntry(action, "store", h.meta.StoreID, map[string]bool{"maintenance": on})}, nil
	})
}

// StoreView reports the store state for diagnosis. It answers while the
// store is unavailable, with empty usage counters.
func (h *Hub) StoreView(ctx context.Context) protocol.StoreView {
	u, err := h.st.Usage(ctx)
	if err != nil {
		h.storeFailed(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return protocol.StoreView{Backend: h.st.Backend(), Engine: h.st.EngineVersion(), Healthy: h.storeOK,
		Maintenance: h.meta.Maintenance, WriteFailure: h.writeFailure, Usage: u}
}

// MaintainStore runs operator-triggered store maintenance through the
// single writer and records it in the audit log (§8.1.1).
func (h *Hub) MaintainStore(ctx context.Context, p Principal, req protocol.StoreMaintain) (protocol.StoreMaintained, error) {
	now, err := h.authorizedNow(p)
	if err != nil {
		return protocol.StoreMaintained{}, err
	}
	out, err := h.st.Maintain(ctx, req)
	if err != nil {
		return protocol.StoreMaintained{}, h.opError(err)
	}
	e := auditEntry("store_maintained", "store", h.meta.StoreID, map[string]any{"request": req, "before": out.Before, "after": out.After, "checkpoint": out.Checkpoint})
	e.At, e.Actor = now, "admin:"+p.Subject
	if err := h.st.AppendAudit(ctx, []store.AuditEntry{e}); err != nil {
		h.writeFailed(err)
	}
	h.log.Info("store maintained", "actor", p.Subject, "checkpoint", req.Checkpoint, "vacuum_pages", req.IncrementalVacuumPages,
		"optimize", req.Optimize, "db_bytes", out.After.DBBytes, "wal_bytes", out.After.WALBytes)
	return out, nil
}

// PruneAudit deletes exported audit records whose digest matches.
func (h *Hub) PruneAudit(ctx context.Context, p Principal, req protocol.AuditPrune) (protocol.AuditPruned, error) {
	if _, err := hex.DecodeString(req.SHA256); err != nil || len(req.SHA256) != 64 || req.ThroughSeq < 1 {
		return protocol.AuditPruned{}, invalid("through_seq must be positive and sha256 64 hex digits")
	}
	now, err := h.authorizedNow(p)
	if err != nil {
		return protocol.AuditPruned{}, err
	}
	entry := store.AuditEntry{At: now, Actor: "admin:" + p.Subject, Action: "audit_pruned", ObjectType: "audit",
		ObjectID: strconv.FormatInt(req.ThroughSeq, 10)}
	sum := strings.ToLower(req.SHA256)
	n, err := h.st.PruneAudit(ctx, req.ThroughSeq, sum, entry)
	if err != nil {
		return protocol.AuditPruned{}, h.opError(err)
	}
	h.log.Info("audit pruned", "actor", p.Subject, "through_seq", req.ThroughSeq, "deleted", n)
	return protocol.AuditPruned{ThroughSeq: req.ThroughSeq, Deleted: n, SHA256: sum}, nil
}

// authorizedNow re-checks the admin credential, as configuration writes
// do, and returns the audit time.
func (h *Hub) authorizedNow(p Principal) (time.Time, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now().UTC().Truncate(time.Microsecond)
	return now, h.stillValid(p, now)
}

// opError maps a failed maintenance operation to the API.
func (h *Hub) opError(err error) error {
	switch {
	case errors.Is(err, store.ErrBusy):
		return protocol.Errorf(protocol.CodeStoreBusy, "store busy; retry")
	case errors.Is(err, store.ErrInvalid):
		return protocol.Errorf(protocol.CodeInvalidRequest, "%v", err)
	}
	h.writeFailed(err)
	return protocol.Errorf(protocol.CodeStoreUnavailable, "store unavailable")
}

// writeFailed marks the store unavailable after a failed write and latches
// the failure category: from now on only a successful ProbeWrite brings the
// store back, because a read probe still succeeds on a full or read-only
// store (B3 §6, §8.1.1).
func (h *Hub) writeFailed(err error) {
	kind := store.FailureKind(err)
	if kind == "" {
		return
	}
	h.mu.Lock()
	was, prev := h.storeOK, h.writeFailure
	h.storeOK = false
	h.writeFailure = kind
	h.mu.Unlock()
	if was || prev != kind {
		h.log.Error("store write failed", "failure", kind, "error", strings.TrimSpace(err.Error()))
	}
}
