package store

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// ExportFormat identifies export files; ExportFormatVersion changes only
// with an incompatible layout.
const (
	ExportFormat        = "loom-oi-export"
	ExportFormatVersion = 1
)

// exportTables is the fixed table order of an export file: parents before
// children, the append-only audit last.
var exportTables = []string{"accesses", "consumer_tokens", "policies", "exposures", "audit"}

// TableDigest is the row count and the SHA-256 over the canonical rows of
// one table.
type TableDigest struct {
	Count  int64  `json:"count"`
	SHA256 string `json:"sha256"`
}

// ExportMeta is the persistent part of the meta row.
type ExportMeta struct {
	StoreID       string  `json:"store_id"`
	ConfigVersion int64   `json:"config_version"`
	Epoch         int64   `json:"epoch"`
	LastRunID     string  `json:"last_run_id"`
	LastStartedAt *string `json:"last_started_at"`
	Maintenance   bool    `json:"maintenance"`
}

// ExportSource describes where an export came from.
type ExportSource struct {
	Backend       string `json:"backend"`
	Engine        string `json:"engine"`
	SchemaVersion int    `json:"schema_version"`
}

// ExportSummary is the trailer of an export file and the result of an
// export or import.
type ExportSummary struct {
	MetaSHA256 string                 `json:"meta_sha256"`
	Tables     map[string]TableDigest `json:"tables"`
}

// exportLine is any line of an export file: the header carries Format,
// rows carry Table and Row, the last line carries Trailer.
type exportLine struct {
	Format        string          `json:"format,omitempty"`
	FormatVersion int             `json:"format_version,omitempty"`
	ExportedAt    string          `json:"exported_at,omitempty"`
	Source        *ExportSource   `json:"source,omitempty"`
	Meta          *ExportMeta     `json:"meta,omitempty"`
	Table         string          `json:"table,omitempty"`
	Row           json.RawMessage `json:"row,omitempty"`
	Trailer       *ExportSummary  `json:"trailer,omitempty"`
}

type accessRow struct {
	AccessID     string            `json:"access_id"`
	Enabled      bool              `json:"enabled"`
	SPKIPins     []string          `json:"spki_pins"`
	MaxExposures int               `json:"max_exposures"`
	Labels       map[string]string `json:"labels"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
}

type tokenRow struct {
	TokenID      string  `json:"token_id"`
	Subject      string  `json:"subject"`
	Scope        string  `json:"scope"`
	SecretSHA256 []byte  `json:"secret_sha256"`
	ExpiresAt    string  `json:"expires_at"`
	RevokedAt    *string `json:"revoked_at"`
	CreatedAt    string  `json:"created_at"`
}

type policyRow struct {
	PolicyID         string   `json:"policy_id"`
	Subject          string   `json:"subject"`
	ExposureSelector string   `json:"exposure_selector"`
	Actions          []string `json:"actions"`
	Enabled          bool     `json:"enabled"`
	UpdatedVersion   int64    `json:"updated_version"`
}

type exposureRow struct {
	ExposureID       string `json:"exposure_id"`
	AccessID         string `json:"access_id"`
	Revision         string `json:"revision"`
	Protocol         string `json:"protocol"`
	TransportProfile string `json:"transport_profile"`
	AdmissionMode    string `json:"admission_mode"`
	Description      string `json:"description"`
	UpdatedAt        string `json:"updated_at"`
}

func ctime(t time.Time) string { return protocol.CanonicalTime(t) }

func ctimep(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ctime(*t)
	return &s
}

func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

func parseTimep(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := parseTime(*s)
	return &t, err
}

// digester accumulates the per-table counts and hashes of canonical rows.
type digester struct {
	h map[string]hash.Hash
	n map[string]int64
}

func newDigester() *digester { return &digester{h: map[string]hash.Hash{}, n: map[string]int64{}} }

func (d *digester) add(table string, row []byte) {
	if d.h[table] == nil {
		d.h[table] = sha256.New()
	}
	d.h[table].Write(row)
	d.h[table].Write([]byte{'\n'})
	d.n[table]++
}

func (d *digester) summary(meta []byte) ExportSummary {
	s := ExportSummary{Tables: map[string]TableDigest{}}
	m := sha256.Sum256(meta)
	s.MetaSHA256 = hex.EncodeToString(m[:])
	for _, t := range exportTables {
		h := d.h[t]
		if h == nil {
			h = sha256.New()
		}
		s.Tables[t] = TableDigest{Count: d.n[t], SHA256: hex.EncodeToString(h.Sum(nil))}
	}
	return s
}

// canonicalRows reads every table inside tx in export order and calls emit
// with each canonical row.
func (c *sqlCore) canonicalRows(ctx context.Context, tx *sql.Tx, emit func(table string, row []byte) error) error {
	var snap Snapshot
	if err := c.loadInto(ctx, tx, &snap); err != nil {
		return err
	}
	put := func(table string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return emit(table, b)
	}
	for _, a := range snap.Accesses {
		if err := put("accesses", accessRow{a.AccessID, a.Enabled, nonNil(a.SPKIPins), a.MaxExposures, nonNilMap(a.Labels), ctime(a.CreatedAt), ctime(a.UpdatedAt)}); err != nil {
			return err
		}
	}
	for _, k := range snap.Tokens {
		if err := put("consumer_tokens", tokenRow{k.TokenID, k.Subject, k.Scope, k.SecretSHA256, ctime(k.ExpiresAt), ctimep(k.RevokedAt), ctime(k.CreatedAt)}); err != nil {
			return err
		}
	}
	for _, p := range snap.Policies {
		if err := put("policies", policyRow{p.PolicyID, p.Subject, p.ExposureSelector, nonNil(p.Actions), p.Enabled, p.UpdatedVersion}); err != nil {
			return err
		}
	}
	for _, e := range snap.Exposures {
		if err := put("exposures", exposureRow{e.ExposureID, e.AccessID, e.Revision, e.Protocol, e.TransportProfile, e.AdmissionMode, e.Description, ctime(e.UpdatedAt)}); err != nil {
			return err
		}
	}
	return c.eachAudit(ctx, tx, -1, func(a protocol.AuditView) error {
		b, err := protocol.CanonicalAudit(a)
		if err != nil {
			return err
		}
		return emit("audit", b)
	})
}

func (c *sqlCore) exportMeta(ctx context.Context, tx *sql.Tx) (ExportMeta, error) {
	var m ExportMeta
	var started nullTime
	err := tx.QueryRowContext(ctx, "SELECT store_id, config_version, epoch, last_run_id, last_started_at, maintenance FROM meta WHERE id = 1").
		Scan(&m.StoreID, &m.ConfigVersion, &m.Epoch, &m.LastRunID, &started, &m.Maintenance)
	m.LastStartedAt = ctimep(started.ptr())
	return m, err
}

// Export writes the database to w as a versioned JSON-lines file: a
// header with the source and meta, one line per canonical row in a fixed
// table order, and a trailer with per-table counts and SHA-256 (B3 §8).
// It takes the single-instance lock, so OIS and its writes are stopped.
func Export(ctx context.Context, cfg Config, busy time.Duration, w io.Writer, now time.Time) (ExportSummary, error) {
	s, err := Open(ctx, cfg, busy)
	if err != nil {
		return ExportSummary{}, err
	}
	defer s.Close()
	return s.export(ctx, w, now)
}

func (c *sqlCore) export(ctx context.Context, w io.Writer, now time.Time) (ExportSummary, error) {
	tx, err := c.rdb.BeginTx(ctx, c.d.readTx)
	if err != nil {
		return ExportSummary{}, err
	}
	defer func() { _ = tx.Rollback() }()
	migs, err := loadMigrations(migrationsFS, c.d.name)
	if err != nil {
		return ExportSummary{}, err
	}
	version, err := schemaVersion(ctx, tx, migs)
	if err != nil {
		return ExportSummary{}, err
	}
	meta, err := c.exportMeta(ctx, tx)
	if err != nil {
		return ExportSummary{}, err
	}
	bw := bufio.NewWriterSize(w, 1<<16)
	enc := json.NewEncoder(bw)
	if err := enc.Encode(exportLine{Format: ExportFormat, FormatVersion: ExportFormatVersion, ExportedAt: ctime(now),
		Source: &ExportSource{Backend: c.d.name, Engine: c.engine, SchemaVersion: version}, Meta: &meta}); err != nil {
		return ExportSummary{}, err
	}
	dg := newDigester()
	if err := c.canonicalRows(ctx, tx, func(table string, row []byte) error {
		dg.add(table, row)
		return enc.Encode(exportLine{Table: table, Row: row})
	}); err != nil {
		return ExportSummary{}, err
	}
	mb, _ := json.Marshal(meta)
	sum := dg.summary(mb)
	if err := enc.Encode(exportLine{Trailer: &sum}); err != nil {
		return ExportSummary{}, err
	}
	return sum, bw.Flush()
}

// ImportResult reports an import.
type ImportResult struct {
	StoreID       string        `json:"store_id"`
	Backend       string        `json:"backend"`
	SourceBackend string        `json:"source_backend"`
	ConfigVersion int64         `json:"config_version"`
	Epoch         int64         `json:"epoch"`
	Summary       ExportSummary `json:"summary"`
}

// Import loads an export file into an empty database of either backend in
// one transaction. Rows keep their identity (store_id, epoch,
// config_version, audit seq); the file must match its own trailer and the
// re-read target must match the trailer before the commit, otherwise
// nothing is written. It takes the single-instance lock and creates the
// schema of an empty database, but never migrates an existing one.
func Import(ctx context.Context, cfg Config, busy time.Duration, r io.Reader, now time.Time) (ImportResult, error) {
	s, _, err := Initialize(ctx, cfg, busy)
	if err != nil {
		return ImportResult{}, err
	}
	defer s.Close()
	return s.importFrom(ctx, r, now)
}

func (c *sqlCore) importFrom(ctx context.Context, r io.Reader, now time.Time) (ImportResult, error) {
	var res ImportResult
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<16))
	var head exportLine
	if err := dec.Decode(&head); err != nil {
		return res, fmt.Errorf("%w: export header: %v", ErrInvalid, err)
	}
	if head.Format != ExportFormat || head.FormatVersion != ExportFormatVersion || head.Source == nil || head.Meta == nil {
		return res, fmt.Errorf("%w: not a %s v%d file", ErrInvalid, ExportFormat, ExportFormatVersion)
	}
	migs, err := loadMigrations(migrationsFS, c.d.name)
	if err != nil {
		return res, err
	}
	if latest := migs[len(migs)-1].version; head.Source.SchemaVersion != latest {
		return res, fmt.Errorf("%w: export schema version %d, this binary expects %d", ErrInvalid, head.Source.SchemaVersion, latest)
	}
	if err := c.writable(ctx); err != nil {
		return res, err
	}
	tx, err := c.wdb.BeginTx(ctx, nil)
	if err != nil {
		return res, c.classify(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := c.requireEmpty(ctx, tx); err != nil {
		return res, err
	}
	m := head.Meta
	started, err := parseTimep(m.LastStartedAt)
	if err != nil {
		return res, fmt.Errorf("%w: meta.last_started_at: %v", ErrInvalid, err)
	}
	if err := c.exec(ctx, tx, "UPDATE meta SET store_id = ?, config_version = ?, epoch = ?, last_run_id = ?, last_started_at = ?, maintenance = ? WHERE id = 1",
		m.StoreID, m.ConfigVersion, m.Epoch, m.LastRunID, c.nt(started), m.Maintenance); err != nil {
		return res, c.classify(err)
	}
	read := newDigester()
	order := 0
	var trailer *ExportSummary
	for trailer == nil {
		var line exportLine
		if err := dec.Decode(&line); err != nil {
			return res, fmt.Errorf("%w: export body: %v", ErrInvalid, err)
		}
		if line.Trailer != nil {
			trailer = line.Trailer
			break
		}
		for order < len(exportTables) && exportTables[order] != line.Table {
			order++
		}
		if order == len(exportTables) {
			return res, fmt.Errorf("%w: unknown or out-of-order table %q", ErrInvalid, line.Table)
		}
		canon, err := c.insertRow(ctx, tx, line.Table, line.Row)
		if err != nil {
			return res, err
		}
		read.add(line.Table, canon)
	}
	if dec.More() {
		return res, fmt.Errorf("%w: data after the trailer", ErrInvalid)
	}
	mb, _ := json.Marshal(m)
	if got := read.summary(mb); !sameSummary(got, *trailer) {
		return res, fmt.Errorf("%w: rows do not match the export trailer", ErrInvalid)
	}
	if c.d.name == Postgres {
		if _, err := tx.ExecContext(ctx, "SELECT setval(pg_get_serial_sequence('audit', 'seq'), COALESCE(MAX(seq), 1), MAX(seq) IS NOT NULL) FROM audit"); err != nil {
			return res, c.classify(err)
		}
	}
	back := newDigester()
	if err := c.canonicalRows(ctx, tx, func(table string, row []byte) error { back.add(table, row); return nil }); err != nil {
		return res, c.classify(err)
	}
	if got := back.summary(mb); !sameSummary(got, *trailer) {
		return res, fmt.Errorf("%w: imported rows read back differently", ErrInvalid)
	}
	detail, _ := json.Marshal(map[string]any{"source_backend": head.Source.Backend, "source_engine": head.Source.Engine,
		"exported_at": head.ExportedAt, "tables": trailer.Tables, "meta_sha256": trailer.MetaSHA256})
	if err := c.insertAudit(ctx, tx, []AuditEntry{{At: now, Actor: "ois-import", Action: "imported_from",
		ObjectType: "store", ObjectID: m.StoreID, Detail: detail}}, m.ConfigVersion); err != nil {
		return res, c.classify(err)
	}
	if err := tx.Commit(); err != nil {
		return res, c.classify(err)
	}
	return ImportResult{StoreID: m.StoreID, Backend: c.d.name, SourceBackend: head.Source.Backend,
		ConfigVersion: m.ConfigVersion, Epoch: m.Epoch, Summary: *trailer}, nil
}

func sameSummary(a, b ExportSummary) bool {
	if a.MetaSHA256 != b.MetaSHA256 || len(a.Tables) != len(b.Tables) {
		return false
	}
	for k, v := range a.Tables {
		if b.Tables[k] != v {
			return false
		}
	}
	return true
}

// requireEmpty accepts only a database that holds nothing but a fresh
// schema: no configuration, no history.
func (c *sqlCore) requireEmpty(ctx context.Context, tx *sql.Tx) error {
	var version, epoch int64
	if err := tx.QueryRowContext(ctx, "SELECT config_version, epoch FROM meta WHERE id = 1").Scan(&version, &epoch); err != nil {
		return c.classify(err)
	}
	if version != 0 || epoch != 0 {
		return fmt.Errorf("%w: config_version %d, epoch %d", ErrNotEmpty, version, epoch)
	}
	for _, t := range exportTables {
		var n int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+t).Scan(&n); err != nil {
			return c.classify(err)
		}
		if n != 0 {
			return fmt.Errorf("%w: table %s has %d rows", ErrNotEmpty, t, n)
		}
	}
	return nil
}

// insertRow decodes one exported row strictly, inserts it and returns its
// canonical encoding for the trailer check.
func (c *sqlCore) insertRow(ctx context.Context, tx *sql.Tx, table string, raw json.RawMessage) ([]byte, error) {
	bad := func(err error) ([]byte, error) {
		return nil, fmt.Errorf("%w: %s row: %v", ErrInvalid, table, err)
	}
	var err error
	switch table {
	case "accesses":
		var a accessRow
		if err := protocol.DecodeStrictBytes(raw, &a); err != nil {
			return bad(err)
		}
		created, err1 := parseTime(a.CreatedAt)
		updated, err2 := parseTime(a.UpdatedAt)
		if err := errors.Join(err1, err2); err != nil {
			return bad(err)
		}
		err = c.applyMutation(ctx, tx, PutAccess{Access{AccessID: a.AccessID, Enabled: a.Enabled, SPKIPins: a.SPKIPins,
			MaxExposures: a.MaxExposures, Labels: a.Labels, CreatedAt: created, UpdatedAt: updated}}, 0)
		raw, _ = json.Marshal(accessRow{a.AccessID, a.Enabled, nonNil(a.SPKIPins), a.MaxExposures, nonNilMap(a.Labels), ctime(created), ctime(updated)})
	case "consumer_tokens":
		var k tokenRow
		if err := protocol.DecodeStrictBytes(raw, &k); err != nil {
			return bad(err)
		}
		expires, err1 := parseTime(k.ExpiresAt)
		revoked, err2 := parseTimep(k.RevokedAt)
		created, err3 := parseTime(k.CreatedAt)
		if err := errors.Join(err1, err2, err3); err != nil {
			return bad(err)
		}
		err = c.applyMutation(ctx, tx, PutToken{Token{TokenID: k.TokenID, Subject: k.Subject, Scope: k.Scope,
			SecretSHA256: k.SecretSHA256, ExpiresAt: expires, RevokedAt: revoked, CreatedAt: created}}, 0)
		raw, _ = json.Marshal(tokenRow{k.TokenID, k.Subject, k.Scope, k.SecretSHA256, ctime(expires), ctimep(revoked), ctime(created)})
	case "policies":
		var p policyRow
		if err := protocol.DecodeStrictBytes(raw, &p); err != nil {
			return bad(err)
		}
		err = c.applyMutation(ctx, tx, PutPolicy{Policy{PolicyID: p.PolicyID, Subject: p.Subject, ExposureSelector: p.ExposureSelector,
			Actions: p.Actions, Enabled: p.Enabled}}, p.UpdatedVersion)
		raw, _ = json.Marshal(policyRow{p.PolicyID, p.Subject, p.ExposureSelector, nonNil(p.Actions), p.Enabled, p.UpdatedVersion})
	case "exposures":
		var e exposureRow
		if err := protocol.DecodeStrictBytes(raw, &e); err != nil {
			return bad(err)
		}
		updated, perr := parseTime(e.UpdatedAt)
		if perr != nil {
			return bad(perr)
		}
		err = c.exec(ctx, tx, `INSERT INTO exposures (exposure_id, access_id, revision, protocol, transport_profile, admission_mode, description, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, e.ExposureID, e.AccessID, e.Revision, e.Protocol, e.TransportProfile, e.AdmissionMode, e.Description, c.t(updated))
		raw, _ = json.Marshal(exposureRow{e.ExposureID, e.AccessID, e.Revision, e.Protocol, e.TransportProfile, e.AdmissionMode, e.Description, ctime(updated)})
	case "audit":
		var a protocol.AuditView
		if err := protocol.DecodeStrictBytes(raw, &a); err != nil {
			return bad(err)
		}
		detail, derr := protocol.CanonicalJSON(a.Detail)
		if derr != nil {
			return bad(derr)
		}
		err = c.exec(ctx, tx, `INSERT INTO audit (seq, at, actor, action, object_type, object_id, config_version, detail)`+c.d.identityInsert+`
VALUES (?, ?, ?, ?, ?, ?, ?, {J})`, a.Seq, c.t(a.At), a.Actor, a.Action, a.ObjectType, a.ObjectID, a.ConfigVersion, string(detail))
		if raw, derr = protocol.CanonicalAudit(a); derr != nil {
			return bad(derr)
		}
	}
	if err != nil {
		return nil, c.classify(err)
	}
	return raw, nil
}

// eachAudit streams audit records with seq ≤ through (all when through is
// negative) in seq order.
func (c *sqlCore) eachAudit(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, through int64, fn func(protocol.AuditView) error) error {
	query := `SELECT seq, at, actor, action, object_type, object_id, config_version, {JO:detail} FROM audit`
	var args []any
	if through >= 0 {
		query += " WHERE seq <= ?"
		args = append(args, through)
	}
	rows, err := q.QueryContext(ctx, c.d.expand(query+" ORDER BY seq"), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a protocol.AuditView
		var at dbTime
		var detail string
		if err := rows.Scan(&a.Seq, &at, &a.Actor, &a.Action, &a.ObjectType, &a.ObjectID, &a.ConfigVersion, &detail); err != nil {
			return err
		}
		a.At, a.Detail = at.t, json.RawMessage(detail)
		if err := fn(a); err != nil {
			return err
		}
	}
	return rows.Err()
}
