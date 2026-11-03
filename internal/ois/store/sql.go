package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dialect captures the few differences between the two SQL backends.
type dialect struct {
	name       string
	numbered   bool // PostgreSQL uses $n placeholders
	forUpdate  string
	jsonIn     string // placeholder expression for a JSON parameter
	jsonOutFmt string // %s is the column name
	readTx     *sql.TxOptions
	isBusy     func(error) bool
	isInvalid  func(error) bool
	timeParam  func(time.Time) any
}

var jsonOutRe = regexp.MustCompile(`\{JO:([a-z_]+)\}`)

// expand turns the shared SQL template into the backend's SQL: {J} is a
// JSON parameter, {JO:col} reads a JSON column as text.
func (d *dialect) expand(q string) string {
	q = strings.ReplaceAll(q, "{J}", d.jsonIn)
	q = jsonOutRe.ReplaceAllStringFunc(q, func(m string) string {
		return fmt.Sprintf(d.jsonOutFmt, jsonOutRe.FindStringSubmatch(m)[1])
	})
	if !d.numbered {
		return q
	}
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sqlCore implements the shared contract on database/sql.
type sqlCore struct {
	d      *dialect
	wdb    *sql.DB // writes; SQLite keeps exactly one connection
	rdb    *sql.DB // reads
	engine string

	// writable is consulted before every write; PostgreSQL uses it to
	// require the single-instance lock (B3 §5).
	writable func(ctx context.Context) error

	// hooks are test seams for fault injection (TV-B3-03).
	hookMu      sync.Mutex
	beforeAudit func() error
}

func (c *sqlCore) EngineVersion() string { return c.engine }

func (c *sqlCore) Backend() string { return c.d.name }

func (c *sqlCore) classify(err error) error {
	var conflict *ConflictError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &conflict), errors.Is(err, ErrInvalid), errors.Is(err, ErrUnavailable), errors.Is(err, ErrBusy):
		return err
	case c.d.isInvalid(err):
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	case c.d.isBusy(err):
		return fmt.Errorf("%w: %v", ErrBusy, err)
	default:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
}

func (c *sqlCore) writeTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if err := c.writable(ctx); err != nil {
		return err
	}
	tx, err := c.wdb.BeginTx(ctx, nil)
	if err != nil {
		return c.classify(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return c.classify(err)
	}
	return c.classify(tx.Commit())
}

func (c *sqlCore) exec(ctx context.Context, tx *sql.Tx, q string, args ...any) error {
	_, err := tx.ExecContext(ctx, c.d.expand(q), args...)
	return err
}

func (c *sqlCore) t(t time.Time) any { return c.d.timeParam(t.UTC().Truncate(time.Microsecond)) }

func (c *sqlCore) nt(t *time.Time) any {
	if t == nil {
		return nil
	}
	return c.t(*t)
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ApplyConfig applies mutations and audit atomically under the global
// config_version check (B3 §3, D3-a).
func (c *sqlCore) ApplyConfig(ctx context.Context, expected int64, muts []Mutation, audit []AuditEntry) (int64, error) {
	var next int64
	err := c.writeTx(ctx, func(tx *sql.Tx) error {
		var cur int64
		if err := tx.QueryRowContext(ctx, c.d.expand("SELECT config_version FROM meta WHERE id = 1"+c.d.forUpdate)).Scan(&cur); err != nil {
			return err
		}
		if cur != expected {
			return &ConflictError{Current: cur}
		}
		next = cur + 1
		for _, m := range muts {
			if err := c.applyMutation(ctx, tx, m, next); err != nil {
				return err
			}
		}
		if err := c.insertAudit(ctx, tx, audit, next); err != nil {
			return err
		}
		return c.exec(ctx, tx, "UPDATE meta SET config_version = ? WHERE id = 1", next)
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}

func (c *sqlCore) applyMutation(ctx context.Context, tx *sql.Tx, m Mutation, version int64) error {
	switch v := m.(type) {
	case PutAccess:
		a := v.Access
		return c.exec(ctx, tx, `INSERT INTO accesses (access_id, enabled, spki_pins, max_exposures, labels, created_at, updated_at)
VALUES (?, ?, {J}, ?, {J}, ?, ?)
ON CONFLICT (access_id) DO UPDATE SET enabled = excluded.enabled, spki_pins = excluded.spki_pins,
max_exposures = excluded.max_exposures, labels = excluded.labels, updated_at = excluded.updated_at`,
			a.AccessID, a.Enabled, jsonText(nonNil(a.SPKIPins)), a.MaxExposures, jsonText(nonNilMap(a.Labels)), c.t(a.CreatedAt), c.t(a.UpdatedAt))
	case DeleteAccess:
		return c.exec(ctx, tx, "DELETE FROM accesses WHERE access_id = ?", v.AccessID)
	case PutToken:
		k := v.Token
		return c.exec(ctx, tx, `INSERT INTO consumer_tokens (token_id, subject, scope, secret_sha256, expires_at, revoked_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, k.TokenID, k.Subject, k.Scope, k.SecretSHA256, c.t(k.ExpiresAt), c.nt(k.RevokedAt), c.t(k.CreatedAt))
	case RevokeToken:
		return c.exec(ctx, tx, "UPDATE consumer_tokens SET revoked_at = ? WHERE token_id = ? AND revoked_at IS NULL", c.t(v.At), v.TokenID)
	case PutPolicy:
		p := v.Policy
		return c.exec(ctx, tx, `INSERT INTO policies (policy_id, subject, exposure_selector, actions, enabled, updated_version)
VALUES (?, ?, ?, {J}, ?, ?)
ON CONFLICT (policy_id) DO UPDATE SET subject = excluded.subject, exposure_selector = excluded.exposure_selector,
actions = excluded.actions, enabled = excluded.enabled, updated_version = excluded.updated_version`,
			p.PolicyID, p.Subject, p.ExposureSelector, jsonText(nonNil(p.Actions)), p.Enabled, version)
	case DeletePolicy:
		return c.exec(ctx, tx, "DELETE FROM policies WHERE policy_id = ?", v.PolicyID)
	default:
		return fmt.Errorf("%w: unknown mutation %T", ErrInvalid, m)
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (c *sqlCore) insertAudit(ctx context.Context, tx *sql.Tx, entries []AuditEntry, version int64) error {
	c.hookMu.Lock()
	hook := c.beforeAudit
	c.hookMu.Unlock()
	if hook != nil && len(entries) > 0 {
		if err := hook(); err != nil {
			return err
		}
	}
	for _, a := range entries {
		detail := a.Detail
		if len(detail) == 0 {
			detail = json.RawMessage("{}")
		}
		if err := c.exec(ctx, tx, `INSERT INTO audit (at, actor, action, object_type, object_id, config_version, detail)
VALUES (?, ?, ?, ?, ?, ?, {J})`, c.t(a.At), a.Actor, a.Action, a.ObjectType, a.ObjectID, version, string(detail)); err != nil {
			return err
		}
	}
	return nil
}

func (c *sqlCore) currentVersion(ctx context.Context, tx *sql.Tx) (int64, error) {
	var v int64
	err := tx.QueryRowContext(ctx, c.d.expand("SELECT config_version FROM meta WHERE id = 1")).Scan(&v)
	return v, err
}

// UpsertRegistry atomically replaces one Access's registry snapshot.
func (c *sqlCore) UpsertRegistry(ctx context.Context, accessID string, exposures []Exposure, audit []AuditEntry) error {
	return c.writeTx(ctx, func(tx *sql.Tx) error {
		if err := c.exec(ctx, tx, "DELETE FROM exposures WHERE access_id = ?", accessID); err != nil {
			return err
		}
		for _, e := range exposures {
			if e.AccessID != accessID {
				return fmt.Errorf("%w: exposure %s outside access %s", ErrInvalid, e.ExposureID, accessID)
			}
			if err := c.exec(ctx, tx, `INSERT INTO exposures (exposure_id, access_id, revision, protocol, transport_profile, admission_mode, description, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, e.ExposureID, e.AccessID, e.Revision, e.Protocol, e.TransportProfile, e.AdmissionMode, e.Description, c.t(e.UpdatedAt)); err != nil {
				return err
			}
		}
		v, err := c.currentVersion(ctx, tx)
		if err != nil {
			return err
		}
		return c.insertAudit(ctx, tx, audit, v)
	})
}

// StartRun increments the persistent epoch for a new process run.
func (c *sqlCore) StartRun(ctx context.Context, runID string, at time.Time) (int64, error) {
	var epoch int64
	err := c.writeTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, c.d.expand("UPDATE meta SET epoch = epoch + 1, last_run_id = ?, last_started_at = ? WHERE id = 1 RETURNING epoch"),
			runID, c.t(at)).Scan(&epoch)
	})
	return epoch, err
}

// RaiseEpoch moves the epoch forward to min when it lags (B3 §6).
func (c *sqlCore) RaiseEpoch(ctx context.Context, min int64, audit AuditEntry) (bool, error) {
	raised := false
	err := c.writeTx(ctx, func(tx *sql.Tx) error {
		var epoch, version int64
		if err := tx.QueryRowContext(ctx, c.d.expand("SELECT epoch, config_version FROM meta WHERE id = 1"+c.d.forUpdate)).Scan(&epoch, &version); err != nil {
			return err
		}
		if epoch >= min {
			return nil
		}
		raised = true
		if err := c.exec(ctx, tx, "UPDATE meta SET epoch = ? WHERE id = 1", min); err != nil {
			return err
		}
		if audit.Action == "" {
			return nil
		}
		return c.insertAudit(ctx, tx, []AuditEntry{audit}, version)
	})
	return raised, err
}

// AppendAudit records runtime events outside configuration changes.
func (c *sqlCore) AppendAudit(ctx context.Context, entries []AuditEntry) error {
	return c.writeTx(ctx, func(tx *sql.Tx) error {
		v, err := c.currentVersion(ctx, tx)
		if err != nil {
			return err
		}
		return c.insertAudit(ctx, tx, entries, v)
	})
}

// ListAudit pages through the audit log in seq order.
func (c *sqlCore) ListAudit(ctx context.Context, afterSeq int64, limit int) ([]AuditEntry, error) {
	rows, err := c.rdb.QueryContext(ctx, c.d.expand(`SELECT seq, at, actor, action, object_type, object_id, config_version, {JO:detail}
FROM audit WHERE seq > ? ORDER BY seq LIMIT ?`), afterSeq, limit)
	if err != nil {
		return nil, c.classify(err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		var at dbTime
		var detail string
		if err := rows.Scan(&a.Seq, &at, &a.Actor, &a.Action, &a.ObjectType, &a.ObjectID, &a.ConfigVersion, &detail); err != nil {
			return nil, c.classify(err)
		}
		a.At, a.Detail = at.t, json.RawMessage(detail)
		out = append(out, a)
	}
	return out, c.classify(rows.Err())
}

// LoadSnapshot reads all persistent objects in one read transaction.
func (c *sqlCore) LoadSnapshot(ctx context.Context) (Snapshot, error) {
	var s Snapshot
	tx, err := c.rdb.BeginTx(ctx, c.d.readTx)
	if err != nil {
		return s, c.classify(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := c.loadInto(ctx, tx, &s); err != nil {
		return Snapshot{}, c.classify(err)
	}
	return s, nil
}

func (c *sqlCore) loadInto(ctx context.Context, tx *sql.Tx, s *Snapshot) error {
	var started nullTime
	if err := tx.QueryRowContext(ctx, c.d.expand(`SELECT store_id, backend, config_version, epoch, last_run_id, last_started_at, maintenance FROM meta WHERE id = 1`)).
		Scan(&s.Meta.StoreID, &s.Meta.Backend, &s.Meta.ConfigVersion, &s.Meta.Epoch, &s.Meta.LastRunID, &started, &s.Meta.Maintenance); err != nil {
		return err
	}
	s.Meta.LastStartedAt = started.ptr()
	if err := c.query(ctx, tx, `SELECT access_id, enabled, {JO:spki_pins}, max_exposures, {JO:labels}, created_at, updated_at FROM accesses ORDER BY access_id`,
		func(r *sql.Rows) error {
			var a Access
			var pins, labels string
			var created, updated dbTime
			if err := r.Scan(&a.AccessID, &a.Enabled, &pins, &a.MaxExposures, &labels, &created, &updated); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(pins), &a.SPKIPins); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(labels), &a.Labels); err != nil {
				return err
			}
			a.CreatedAt, a.UpdatedAt = created.t, updated.t
			s.Accesses = append(s.Accesses, a)
			return nil
		}); err != nil {
		return err
	}
	if err := c.query(ctx, tx, `SELECT token_id, subject, scope, secret_sha256, expires_at, revoked_at, created_at FROM consumer_tokens ORDER BY token_id`,
		func(r *sql.Rows) error {
			var k Token
			var expires, created dbTime
			var revoked nullTime
			if err := r.Scan(&k.TokenID, &k.Subject, &k.Scope, &k.SecretSHA256, &expires, &revoked, &created); err != nil {
				return err
			}
			k.ExpiresAt, k.RevokedAt, k.CreatedAt = expires.t, revoked.ptr(), created.t
			s.Tokens = append(s.Tokens, k)
			return nil
		}); err != nil {
		return err
	}
	if err := c.query(ctx, tx, `SELECT policy_id, subject, exposure_selector, {JO:actions}, enabled, updated_version FROM policies ORDER BY policy_id`,
		func(r *sql.Rows) error {
			var p Policy
			var actions string
			if err := r.Scan(&p.PolicyID, &p.Subject, &p.ExposureSelector, &actions, &p.Enabled, &p.UpdatedVersion); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(actions), &p.Actions); err != nil {
				return err
			}
			s.Policies = append(s.Policies, p)
			return nil
		}); err != nil {
		return err
	}
	return c.query(ctx, tx, `SELECT exposure_id, access_id, revision, protocol, transport_profile, admission_mode, description, updated_at FROM exposures ORDER BY exposure_id`,
		func(r *sql.Rows) error {
			var e Exposure
			var updated dbTime
			if err := r.Scan(&e.ExposureID, &e.AccessID, &e.Revision, &e.Protocol, &e.TransportProfile, &e.AdmissionMode, &e.Description, &updated); err != nil {
				return err
			}
			e.UpdatedAt = updated.t
			s.Exposures = append(s.Exposures, e)
			return nil
		})
}

func (c *sqlCore) query(ctx context.Context, tx *sql.Tx, q string, each func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, c.d.expand(q))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// probe is the read part of Health.
func (c *sqlCore) probe(ctx context.Context) error {
	var v int64
	return c.classify(c.rdb.QueryRowContext(ctx, "SELECT config_version FROM meta WHERE id = 1").Scan(&v))
}

// dbTime scans TIMESTAMPTZ (time.Time) or SQLite RFC 3339 text.
type dbTime struct{ t time.Time }

func (d *dbTime) Scan(v any) error {
	var nt nullTime
	if err := nt.Scan(v); err != nil {
		return err
	}
	if !nt.valid {
		return errors.New("unexpected NULL time")
	}
	d.t = nt.t
	return nil
}

type nullTime struct {
	t     time.Time
	valid bool
}

func (n *nullTime) Scan(v any) error {
	switch x := v.(type) {
	case nil:
		n.valid = false
		return nil
	case time.Time:
		n.t, n.valid = x.UTC(), true
		return nil
	case string:
		return n.parse(x)
	case []byte:
		return n.parse(string(x))
	default:
		return fmt.Errorf("cannot scan %T into time", v)
	}
}

func (n *nullTime) parse(s string) error {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}
	n.t, n.valid = t.UTC(), true
	return nil
}

func (n nullTime) ptr() *time.Time {
	if !n.valid {
		return nil
	}
	t := n.t
	return &t
}
