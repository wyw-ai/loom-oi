package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"
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
	if err := checkDuplicates(raw); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return json.Marshal(normalizeNumbers(v))
}

func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		return exactNumber(string(x))
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

// exactNumber canonicalizes a validated JSON decimal without floating-point
// rounding. jsonb may expand an exponent or keep decimal zeroes, whereas
// SQLite preserves the original text. Even very large exponents stay compact.
func exactNumber(s string) json.Number {
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	exponent := new(big.Int)
	if at := strings.IndexAny(s, "eE"); at >= 0 {
		exponent.SetString(s[at+1:], 10)
		s = s[:at]
	}
	if at := strings.IndexByte(s, '.'); at >= 0 {
		exponent.Sub(exponent, big.NewInt(int64(len(s)-at-1)))
		s = s[:at] + s[at+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0"
	}
	digits := strings.TrimRight(s, "0")
	exponent.Add(exponent, big.NewInt(int64(len(s)-len(digits))))
	// Scientific exponent of the first nonzero digit.
	position := new(big.Int).Add(exponent, big.NewInt(int64(len(digits)-1)))
	var out string
	if position.IsInt64() && position.Int64() >= -6 && position.Int64() < 21 {
		point := int(position.Int64()) + 1
		switch {
		case point <= 0:
			out = "0." + strings.Repeat("0", -point) + digits
		case point >= len(digits):
			out = digits + strings.Repeat("0", point-len(digits))
		default:
			out = digits[:point] + "." + digits[point:]
		}
	} else {
		out = digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		out += "e" + position.String()
	}
	if negative {
		out = "-" + out
	}
	return json.Number(out)
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
