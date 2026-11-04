package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// ProbeWrite commits a write that changes nothing. Health only reads, which
// still succeeds on a full or read-only store, so after a write failure
// OIS waits for this probe before it accepts writes and Opens again.
func (c *sqlCore) ProbeWrite(ctx context.Context) error {
	return c.writeTx(ctx, func(tx *sql.Tx) error {
		return c.exec(ctx, tx, "UPDATE meta SET maintenance = maintenance WHERE id = 1")
	})
}

// Usage reports size counters: pages and WAL for SQLite (§8.1.1: tell the
// live pages, the free pages and the WAL apart), the database size for
// PostgreSQL.
func (c *sqlCore) Usage(ctx context.Context) (protocol.StoreUsage, error) {
	var u protocol.StoreUsage
	if c.path == "" {
		err := c.rdb.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&u.DBBytes)
		return u, c.classify(err)
	}
	for q, dst := range map[string]*int64{
		"PRAGMA page_size": &u.PageSize, "PRAGMA page_count": &u.PageCount, "PRAGMA freelist_count": &u.FreelistCount,
	} {
		if err := c.rdb.QueryRowContext(ctx, q).Scan(dst); err != nil {
			return u, c.classify(err)
		}
	}
	u.DBBytes = fileSize(c.path)
	u.WALBytes = fileSize(c.path + "-wal")
	return u, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// Maintain runs operator-triggered maintenance on the write connection:
// a WAL checkpoint, a bounded incremental vacuum and PRAGMA optimize on
// SQLite, ANALYZE on PostgreSQL (§8.1.1). OIS calls it through the
// serialized writer, so it never runs beside a configuration write.
func (c *sqlCore) Maintain(ctx context.Context, req protocol.StoreMaintain) (protocol.StoreMaintained, error) {
	var out protocol.StoreMaintained
	if err := c.validMaintain(req); err != nil {
		return out, err
	}
	if err := c.writable(ctx); err != nil {
		return out, err
	}
	before, err := c.Usage(ctx)
	if err != nil {
		return out, err
	}
	out.Before = before
	if c.path == "" {
		if req.Optimize {
			if _, err := c.wdb.ExecContext(ctx, "ANALYZE"); err != nil {
				return out, c.classify(err)
			}
		}
	} else {
		conn, err := c.wdb.Conn(ctx)
		if err != nil {
			return out, c.classify(err)
		}
		defer conn.Close()
		// Vacuum first: in WAL mode the file shrinks only when the
		// checkpoint copies the truncation back into it.
		if req.IncrementalVacuumPages > 0 {
			if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA incremental_vacuum(%d)", req.IncrementalVacuumPages)); err != nil {
				return out, c.classify(err)
			}
		}
		if req.Optimize {
			if _, err := conn.ExecContext(ctx, "PRAGMA optimize"); err != nil {
				return out, c.classify(err)
			}
		}
		if req.Checkpoint != "" {
			var busy int64
			cp := &protocol.CheckpointResult{}
			mode := map[string]string{"passive": "PASSIVE", "truncate": "TRUNCATE"}[req.Checkpoint]
			if err := conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint("+mode+")").Scan(&busy, &cp.LogFrames, &cp.CheckpointedFrames); err != nil {
				return out, c.classify(err)
			}
			cp.Busy = busy != 0
			out.Checkpoint = cp
		}
	}
	out.After, err = c.Usage(ctx)
	return out, err
}

func (c *sqlCore) validMaintain(req protocol.StoreMaintain) error {
	switch {
	case req.Checkpoint != "" && req.Checkpoint != "passive" && req.Checkpoint != "truncate":
		return fmt.Errorf("%w: checkpoint must be passive or truncate", ErrInvalid)
	case req.IncrementalVacuumPages < 0 || req.IncrementalVacuumPages > protocol.MaxVacuumPages:
		return fmt.Errorf("%w: incremental_vacuum_pages must be 0..%d", ErrInvalid, protocol.MaxVacuumPages)
	case c.path == "" && (req.Checkpoint != "" || req.IncrementalVacuumPages != 0):
		return fmt.Errorf("%w: postgres supports only optimize; WAL and vacuum are managed by the server", ErrInvalid)
	}
	return nil
}

// ErrDigestMismatch refuses a prune whose export does not match the
// records it would delete.
var ErrDigestMismatch = fmt.Errorf("%w: audit digest does not match the records to delete; export them again", ErrInvalid)

// PruneAudit deletes the audit records with seq ≤ throughSeq only when
// their canonical digest equals digest, i.e. exactly what an operator
// exported, and records the prune itself (§8.1.1).
func (c *sqlCore) PruneAudit(ctx context.Context, throughSeq int64, digest string, entry AuditEntry) (int64, error) {
	if throughSeq < 1 {
		return 0, fmt.Errorf("%w: through_seq must be positive", ErrInvalid)
	}
	var deleted int64
	err := c.writeTx(ctx, func(tx *sql.Tx) error {
		rows, err := c.auditRows(ctx, tx, throughSeq)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("%w: no audit records with seq <= %d", ErrInvalid, throughSeq)
		}
		got, err := protocol.AuditDigest(rows)
		if err != nil {
			return err
		}
		if got != digest {
			return ErrDigestMismatch
		}
		res, err := tx.ExecContext(ctx, c.d.expand("DELETE FROM audit WHERE seq <= ?"), throughSeq)
		if err != nil {
			return err
		}
		if deleted, err = res.RowsAffected(); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"through_seq": throughSeq, "deleted": deleted, "sha256": digest})
		entry.Detail = detail
		v, err := c.currentVersion(ctx, tx)
		if err != nil {
			return err
		}
		return c.insertAudit(ctx, tx, []AuditEntry{entry}, v)
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// auditRows reads the audit records with seq ≤ through in seq order, in
// their API form.
func (c *sqlCore) auditRows(ctx context.Context, tx *sql.Tx, through int64) ([]protocol.AuditView, error) {
	var out []protocol.AuditView
	err := c.eachAudit(ctx, tx, through, func(a protocol.AuditView) error {
		out = append(out, a)
		return nil
	})
	return out, err
}
