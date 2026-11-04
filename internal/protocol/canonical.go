package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// CanonicalJSON re-encodes a JSON document so that equal values produce
// equal bytes on both backends: SQLite returns the text written by OIS,
// PostgreSQL returns normalized jsonb. Object keys are sorted, whitespace
// is dropped and numbers keep integer form when they are integers.
func CanonicalJSON(raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return json.Marshal(normalizeNumbers(v))
}

func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(string(x), 10, 64); err == nil {
			return json.Number(strconv.FormatInt(i, 10))
		}
		if f, err := strconv.ParseFloat(string(x), 64); err == nil {
			return json.Number(strconv.FormatFloat(f, 'g', -1, 64))
		}
		return x
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeNumbers(e)
		}
	case []any:
		for i, e := range x {
			x[i] = normalizeNumbers(e)
		}
	}
	return v
}

// CanonicalTime is the export and digest form of a stored time: UTC with
// the microsecond precision both backends keep.
func CanonicalTime(t time.Time) string {
	return t.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}

// canonicalAudit fixes the field order of a digested audit record.
type canonicalAudit struct {
	Seq           int64           `json:"seq"`
	At            string          `json:"at"`
	Actor         string          `json:"actor"`
	Action        string          `json:"action"`
	ObjectType    string          `json:"object_type"`
	ObjectID      string          `json:"object_id"`
	ConfigVersion int64           `json:"config_version"`
	Detail        json.RawMessage `json:"detail"`
}

// CanonicalAudit is the canonical one-line encoding of an audit record,
// shared by the audit digest and the store export.
func CanonicalAudit(a AuditView) ([]byte, error) {
	detail, err := CanonicalJSON(a.Detail)
	if err != nil {
		return nil, fmt.Errorf("audit %d detail: %w", a.Seq, err)
	}
	return json.Marshal(canonicalAudit{Seq: a.Seq, At: CanonicalTime(a.At), Actor: a.Actor, Action: a.Action,
		ObjectType: a.ObjectType, ObjectID: a.ObjectID, ConfigVersion: a.ConfigVersion, Detail: detail})
}

// AuditDigest is the SHA-256 over the canonical lines of items in seq
// order. The exporter and the pruning store compute it independently, so
// a prune deletes exactly what was exported.
func AuditDigest(items []AuditView) (string, error) {
	h := sha256.New()
	var prev int64
	for i, a := range items {
		if i > 0 && a.Seq <= prev {
			return "", fmt.Errorf("audit records are not in increasing seq order at %d", a.Seq)
		}
		prev = a.Seq
		line, err := CanonicalAudit(a)
		if err != nil {
			return "", err
		}
		h.Write(line)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
