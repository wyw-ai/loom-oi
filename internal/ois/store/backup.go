package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupInfo describes a verified SQLite backup file (B3 §8).
type BackupInfo struct {
	Path          string    `json:"path"`
	Bytes         int64     `json:"bytes"`
	SHA256        string    `json:"sha256"`
	StoreID       string    `json:"store_id"`
	ConfigVersion int64     `json:"config_version"`
	Epoch         int64     `json:"epoch"`
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
}

// readOnlyDSN opens path through SQLite's URI form with mode=ro.
func readOnlyDSN(path string, busy time.Duration) string {
	p := strings.ReplaceAll(filepath.ToSlash(path), "%", "%25")
	return fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(%d)", p, busy.Milliseconds())
}

// Backup writes a consistent copy of a SQLite database with VACUUM INTO
// over a read-only connection, so OIS may keep serving and writing. The
// copy gets its final name only after integrity_check and the meta and
// schema checks pass; an interrupted backup leaves at most a ".partial"
// file. PostgreSQL is backed up with the deployment's pg_dump or
// pg_basebackup (B3 §8).
func Backup(ctx context.Context, cfg Config, busy time.Duration, out string, now time.Time) (BackupInfo, error) {
	if err := cfg.Validate(); err != nil {
		return BackupInfo{}, err
	}
	if cfg.Driver != SQLite {
		return BackupInfo{}, fmt.Errorf("%w: back up PostgreSQL with pg_dump -Fc or pg_basebackup (B3 §8)", ErrInvalid)
	}
	if _, err := os.Lstat(out); err == nil {
		return BackupInfo{}, fmt.Errorf("backup %s already exists", out)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return BackupInfo{}, err
	}
	exists, err := sqliteFile(cfg.SQLitePath)
	if err != nil {
		return BackupInfo{}, err
	}
	if !exists {
		return BackupInfo{}, fmt.Errorf("database %s does not exist", cfg.SQLitePath)
	}
	tmp := out + ".partial"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return BackupInfo{}, err
	}
	fail := func(err error) (BackupInfo, error) {
		_ = os.Remove(tmp)
		return BackupInfo{}, err
	}
	db, err := sql.Open("sqlite", readOnlyDSN(cfg.SQLitePath, busy))
	if err != nil {
		return fail(err)
	}
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", tmp)
	if cerr := db.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fail(fmt.Errorf("VACUUM INTO: %w", err))
	}
	info, err := inspectSQLiteFile(ctx, tmp, busy)
	if err != nil {
		return fail(fmt.Errorf("backup verification failed: %w", err))
	}
	if err := syncFile(tmp); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, out); err != nil {
		return fail(err)
	}
	syncDir(filepath.Dir(out))
	info.Path, info.CreatedAt = out, now.UTC()
	return info, nil
}

// inspectSQLiteFile verifies a database file without writing to it:
// integrity_check, the schema against the embedded migrations (older is
// allowed, newer or altered is not) and the meta row.
func inspectSQLiteFile(ctx context.Context, path string, busy time.Duration) (BackupInfo, error) {
	var info BackupInfo
	db, err := sql.Open("sqlite", readOnlyDSN(path, busy))
	if err != nil {
		return info, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var check string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil {
		return info, err
	}
	if check != "ok" {
		return info, fmt.Errorf("integrity_check: %s", check)
	}
	migs, err := loadMigrations(migrationsFS, SQLite)
	if err != nil {
		return info, err
	}
	if info.SchemaVersion, err = schemaVersion(ctx, db, migs); err != nil {
		return info, err
	}
	var backend string
	if err := db.QueryRowContext(ctx, "SELECT store_id, backend, config_version, epoch FROM meta WHERE id = 1").
		Scan(&info.StoreID, &backend, &info.ConfigVersion, &info.Epoch); err != nil {
		return info, fmt.Errorf("meta: %w", err)
	}
	if backend != SQLite {
		return info, fmt.Errorf("meta.backend is %q, not sqlite", backend)
	}
	info.SHA256, info.Bytes, err = fileDigest(path)
	return info, err
}

// schemaVersion returns the newest applied migration after checking that
// every applied migration is known to this binary with the same checksum.
func schemaVersion(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, migs []migration) (int, error) {
	rows, err := db.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return 0, fmt.Errorf("schema_migrations: %w", err)
	}
	defer rows.Close()
	known := map[int]string{}
	for _, m := range migs {
		known[m.version] = m.checksum
	}
	newest := 0
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return 0, err
		}
		want, ok := known[v]
		switch {
		case !ok:
			return 0, fmt.Errorf("%w (version %d)", ErrSchemaTooNew, v)
		case want != sum:
			return 0, fmt.Errorf("%w (version %d)", ErrMigrationChecksum, v)
		}
		newest = v
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if newest == 0 {
		return 0, fmt.Errorf("%w: no applied migrations", ErrMigrationPending)
	}
	return newest, nil
}

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = f.Sync()
	return errors.Join(err, f.Close())
}

// syncDir makes a rename durable where the OS supports syncing a
// directory; elsewhere it does nothing.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// copyFile copies src to a new file dst (0600) and returns its digest.
func copyFile(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// RestoreRequest selects the source of a restore; exactly one of From and
// External is set (B3 §8).
type RestoreRequest struct {
	// From is a SQLite backup file written by Backup.
	From string
	// External labels a database already restored by deployment tools
	// (pg_restore, a file-system snapshot); it is only marked.
	External string
	// MinEpoch is a lower bound for the epoch, e.g. the last epoch the
	// operator saw before the incident.
	MinEpoch int64
	Now      time.Time
}

// RestoreResult reports the restored database state.
type RestoreResult struct {
	StoreID       string `json:"store_id"`
	ConfigVersion int64  `json:"config_version"`
	EpochBefore   int64  `json:"epoch_before"`
	Epoch         int64  `json:"epoch"`
	Maintenance   bool   `json:"maintenance"`
	PreservedAs   string `json:"preserved_as,omitempty"`
}

// Restore replaces or marks the database while OIS is stopped (it takes
// the single-instance lock). The result is always in maintenance mode, with
// an epoch no lower than the backup's, the replaced database's and
// MinEpoch, and with a restored_from audit record (B3 §6, §8).
func Restore(ctx context.Context, cfg Config, busy time.Duration, req RestoreRequest) (result RestoreResult, err error) {
	if err := cfg.Validate(); err != nil {
		return RestoreResult{}, err
	}
	switch {
	case (req.From == "") == (req.External == ""):
		return RestoreResult{}, fmt.Errorf("%w: give exactly one of a backup file or an external label", ErrInvalid)
	case req.MinEpoch < 0:
		return RestoreResult{}, fmt.Errorf("%w: min epoch must not be negative", ErrInvalid)
	case req.From != "" && cfg.Driver != SQLite:
		return RestoreResult{}, fmt.Errorf("%w: restore PostgreSQL with pg_restore, then mark it with --external (B3 §8)", ErrInvalid)
	}
	if req.External != "" {
		s, _, openErr := open(ctx, cfg, busy, modeRestore)
		if openErr != nil {
			return RestoreResult{}, openErr
		}
		defer func() { err = operationCleanupError("restore", err, s.Close()) }()
		detail := map[string]any{"external": req.External, "min_epoch": req.MinEpoch}
		return s.markRestored(ctx, req.MinEpoch, detail, req.Now)
	}
	lock, err := lockFile(cfg.SQLitePath + ".lock")
	if err != nil {
		return RestoreResult{}, err
	}
	defer lock.Close()
	return restoreSQLite(ctx, cfg.SQLitePath, busy, req)
}

// restoreSQLite runs with the lock held. The backup is copied and marked as
// a private file, then renamed over the database, so the path always holds
// either the old database or the complete, marked restore.
func restoreSQLite(ctx context.Context, path string, busy time.Duration, req RestoreRequest) (RestoreResult, error) {
	if err := requireStandaloneBackup(req.From); err != nil {
		return RestoreResult{}, err
	}
	backup, err := inspectSQLiteFile(ctx, req.From, busy)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("backup %s: %w", req.From, err)
	}
	prev, err := previousSQLite(ctx, path, busy)
	if err != nil {
		return RestoreResult{}, err
	}
	if prev.meta != nil && prev.meta.StoreID != backup.StoreID {
		return RestoreResult{}, fmt.Errorf("%w: backup belongs to store %s but the database is store %s", ErrInvalid, backup.StoreID, prev.meta.StoreID)
	}
	tmp := path + ".restore.tmp"
	for _, p := range []string{tmp, tmp + "-wal", tmp + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return RestoreResult{}, err
		}
	}
	sum, err := copyFile(req.From, tmp)
	if err != nil {
		return RestoreResult{}, err
	}
	fail := func(err error) (RestoreResult, error) {
		for _, p := range []string{tmp, tmp + "-wal", tmp + "-shm"} {
			_ = os.Remove(p)
		}
		return RestoreResult{}, err
	}
	if err := requireStandaloneBackup(req.From); err != nil {
		return fail(err)
	}
	if sum != backup.SHA256 {
		return fail(fmt.Errorf("backup %s changed while it was copied", req.From))
	}
	minEpoch := max(backup.Epoch, req.MinEpoch)
	detail := map[string]any{
		"source": req.From, "sha256": backup.SHA256,
		"backup":   map[string]any{"store_id": backup.StoreID, "epoch": backup.Epoch, "config_version": backup.ConfigVersion, "schema_version": backup.SchemaVersion},
		"previous": prev.detail(),
	}
	if prev.meta != nil {
		minEpoch = max(minEpoch, prev.meta.Epoch)
	}
	preserved := path + ".replaced-" + req.Now.UTC().Format("20060102T150405.000000000Z")
	hasWAL, err := fileExists(path + "-wal")
	if err != nil {
		return fail(err)
	}
	if prev.exists || hasWAL {
		detail["preserved_as"] = filepath.Base(preserved)
	}
	res, err := markSQLiteFile(ctx, tmp, busy, minEpoch, detail, req.Now)
	if err != nil {
		return fail(err)
	}
	if prev.exists {
		if err := linkOrCopy(path, preserved); err != nil {
			return fail(err)
		}
	}
	if err := installSQLiteRestore(tmp, path, preserved); err != nil {
		return fail(err)
	}
	syncDir(filepath.Dir(path))
	if prev.exists || hasWAL {
		res.PreservedAs = preserved
	}
	return res, nil
}

// markSQLiteFile opens a private copy in WAL mode, marks it restored and
// explicitly checkpoints its WAL and syncs the main file before installation.
func markSQLiteFile(ctx context.Context, path string, busy time.Duration, minEpoch int64, detail map[string]any, now time.Time) (RestoreResult, error) {
	if err := ensureIncrementalVacuum(ctx, path); err != nil {
		return RestoreResult{}, err
	}
	s, _, err := openSQLiteLocked(ctx, Config{Driver: SQLite, SQLitePath: path}, busy, modeRestore, io.NopCloser(nil))
	if err != nil {
		return RestoreResult{}, err
	}
	res, err := s.markRestored(ctx, minEpoch, detail, now)
	if err == nil {
		err = checkpointSQLite(ctx, s.wdb)
	}
	if cerr := s.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return RestoreResult{}, err
	}
	if err := syncFile(path); err != nil {
		return RestoreResult{}, err
	}
	for _, p := range []string{path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return RestoreResult{}, err
		}
	}
	return res, nil
}

// ensureIncrementalVacuum converts a copy whose auto_vacuum is not
// INCREMENTAL, which OIS would refuse to serve (B3 §5).
func ensureIncrementalVacuum(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var mode int
	if err := db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return err
	}
	if mode == 2 {
		return nil
	}
	for _, q := range []string{"PRAGMA auto_vacuum = INCREMENTAL", "VACUUM"} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// markRestored puts the database in maintenance mode with the epoch raised
// to at least minEpoch and records restored_from.
func (c *sqlCore) markRestored(ctx context.Context, minEpoch int64, detail map[string]any, now time.Time) (RestoreResult, error) {
	var res RestoreResult
	err := c.writeTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, c.d.expand("SELECT store_id, config_version, epoch FROM meta WHERE id = 1"+c.d.forUpdate)).
			Scan(&res.StoreID, &res.ConfigVersion, &res.EpochBefore); err != nil {
			return fmt.Errorf("meta: %w", err)
		}
		res.Epoch, res.Maintenance = max(res.EpochBefore, minEpoch), true
		if err := c.exec(ctx, tx, "UPDATE meta SET maintenance = ?, epoch = ? WHERE id = 1", true, res.Epoch); err != nil {
			return err
		}
		detail["epoch_before"], detail["epoch_after"] = res.EpochBefore, res.Epoch
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		return c.insertAudit(ctx, tx, []AuditEntry{{At: now, Actor: "ois-restore", Action: "restored_from",
			ObjectType: "store", ObjectID: res.StoreID, Detail: b}}, res.ConfigVersion)
	})
	return res, err
}

// prevSQLite describes the database a restore replaces.
type prevSQLite struct {
	exists bool
	meta   *Meta
	err    error
}

func (p prevSQLite) detail() map[string]any {
	switch {
	case !p.exists:
		return map[string]any{"exists": false}
	case p.meta == nil:
		return map[string]any{"exists": true, "readable": false, "error": p.err.Error()}
	}
	return map[string]any{"exists": true, "readable": true, "store_id": p.meta.StoreID, "epoch": p.meta.Epoch, "config_version": p.meta.ConfigVersion}
}

// previousSQLite reads the meta row of the database a restore replaces
// through a read-only connection, which sees committed WAL content but
// cannot change any file, and then checkpoints a readable database so its
// preserved copy is self-contained. An unreadable database is still
// replaced (that is what restores are for) and preserved byte for byte
// with its WAL; only its epoch is then unknown, which MinEpoch and the OIC
// epoch reports cover (B3 §6).
func previousSQLite(ctx context.Context, path string, busy time.Duration) (prevSQLite, error) {
	exists, err := sqliteFile(path)
	if err != nil || !exists {
		return prevSQLite{}, err
	}
	p := prevSQLite{exists: true}
	var m Meta
	ro, err := sql.Open("sqlite", readOnlyDSN(path, busy))
	if err == nil {
		err = ro.QueryRowContext(ctx, "SELECT store_id, config_version, epoch FROM meta WHERE id = 1").Scan(&m.StoreID, &m.ConfigVersion, &m.Epoch)
		_ = ro.Close()
	}
	if err != nil {
		p.err = err
		return p, nil
	}
	p.meta = &m
	rw, err := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=busy_timeout(%d)", path, busy.Milliseconds()))
	if err != nil {
		return p, err
	}
	defer rw.Close()
	return p, checkpointSQLite(ctx, rw)
}

// linkOrCopy preserves src as dst, by hard link where possible.
func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	_, err := copyFile(src, dst)
	return err
}

// requireStandaloneBackup rejects a live SQLite file: copying its main
// file would omit committed pages in its WAL. --from accepts the standalone
// output of Backup; use that command first to snapshot a live database.
func requireStandaloneBackup(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved + "-wal")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		return fmt.Errorf("%w: backup %s has a non-empty WAL; create a standalone copy with `ois backup` first", ErrInvalid, path)
	}
	return nil
}

// checkpointSQLite must inspect the result row: SQLite reports blocked
// checkpoints with busy=1 and no SQL error. Only a completed truncation
// makes the old main file safe to keep usable without its WAL.
func checkpointSQLite(ctx context.Context, db *sql.DB) error {
	var busy, frames, checkpointed int64
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return fmt.Errorf("%w: restore checkpoint blocked by an active reader (%d of %d WAL frames checkpointed)", ErrBusy, checkpointed, frames)
	}
	return nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// movePrevWAL keeps an unmerged WAL with the preserved database (or as an
// orphan sidecar when the main file was lost), and removes shared memory.
// On an ordinary failure the WAL is returned to its original name, so a
// failed restore does not strand committed data away from the old file.
func movePrevWAL(path, preserved string) (bool, error) {
	exists, err := fileExists(path + "-wal")
	if err != nil {
		return false, err
	}
	if exists {
		if taken, err := fileExists(preserved + "-wal"); err != nil {
			return false, err
		} else if taken {
			return false, fmt.Errorf("preserved WAL %s already exists", preserved+"-wal")
		}
		if err := os.Rename(path+"-wal", preserved+"-wal"); err != nil {
			return false, err
		}
	}
	if err := os.Remove(path + "-shm"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		if exists {
			err = errors.Join(err, os.Rename(preserved+"-wal", path+"-wal"))
		}
		return false, err
	}
	return exists, nil
}

// installSQLiteRestore detaches sidecars even when the main file is absent.
// It returns the old WAL on rename failure; before a successful rename the
// old main file remains in place, after it the complete marked copy does.
func installSQLiteRestore(tmp, path, preserved string) error {
	movedWAL, err := movePrevWAL(path, preserved)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		if movedWAL {
			err = errors.Join(err, os.Rename(preserved+"-wal", path+"-wal"))
		}
		return err
	}
	return nil
}
