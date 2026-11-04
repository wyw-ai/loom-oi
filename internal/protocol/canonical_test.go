package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCanonicalJSON(t *testing.T) {
	cases := map[string]string{
		`{"b": 1, "a": {"d": [1, 2.50, 2e3, -0], "c": "é<&>"}}`: `{"a":{"c":"é\u003c\u0026\u003e","d":[1,2.5,2000,0]},"b":1}`,
		``:     `{}`,
		"  \n": `{}`,
		`{"big": 123456789012345678901234567890}`:           `{"big":1.2345678901234567890123456789e29}`,
		`[null, true, "x"]`:                                 `[null,true,"x"]`,
		`{"n": 9007199254740993}`:                           `{"n":9007199254740993}`,
		`[9007199254740993e0,9007199254740993.0]`:           `[9007199254740993,9007199254740993]`,
		`[1.00000000000000001,0.000001,1e-7,-12.50]`:        `[1.00000000000000001,0.000001,1e-7,-12.5]`,
		`[1e100000000000000000000,10e99999999999999999999]`: `[1e100000000000000000000,1e100000000000000000000]`,
		`[-0.000e-99999999999999999999,1e21,1e-1000]`:       `[0,1e21,1e-1000]`,
	}
	for in, want := range cases {
		got, err := CanonicalJSON([]byte(in))
		if err != nil || string(got) != want {
			t.Fatalf("%q: %s %v (want %s)", in, got, err, want)
		}
	}
	// SQLite text and PostgreSQL jsonb spellings agree.
	a, _ := CanonicalJSON([]byte(`{"k":"v","n":1.0}`))
	b, _ := CanonicalJSON([]byte(`{"n": 1.0, "k": "v"}`))
	if string(a) != string(b) {
		t.Fatalf("%s != %s", a, b)
	}
	for _, bad := range []string{`{`, `{} {}`, `{"a":1}x`, `1]`, `{}]`, `{"a":1,"a":2}`} {
		if _, err := CanonicalJSON([]byte(bad)); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	for _, pair := range [][2]string{{`9007199254740992.0`, `9007199254740993.0`}, {`1.00000000000000001`, `1.00000000000000002`}} {
		a, _ := CanonicalJSON([]byte(pair[0]))
		b, _ := CanonicalJSON([]byte(pair[1]))
		if string(a) == string(b) {
			t.Fatalf("distinct exact decimals share canonical bytes: %s", a)
		}
	}
}

func TestCanonicalTimeAndAudit(t *testing.T) {
	at := time.Date(2026, 11, 4, 9, 2, 3, 456789123, time.FixedZone("x", 8*3600))
	if got := CanonicalTime(at); got != "2026-11-04T01:02:03.456789Z" {
		t.Fatal(got)
	}
	a := AuditView{Seq: 7, At: at, Actor: "admin:root", Action: "a", ObjectType: "o", ObjectID: "i", ConfigVersion: 3, Detail: json.RawMessage(`{"y":1, "x":2}`)}
	line, err := CanonicalAudit(a)
	if err != nil || string(line) != `{"seq":7,"at":"2026-11-04T01:02:03.456789Z","actor":"admin:root","action":"a","object_type":"o","object_id":"i","config_version":3,"detail":{"x":2,"y":1}}` {
		t.Fatalf("%s %v", line, err)
	}
	a.Detail = json.RawMessage(`{`)
	if _, err := CanonicalAudit(a); err == nil || !strings.Contains(err.Error(), "audit 7") {
		t.Fatal(err)
	}
}

func TestAuditDigest(t *testing.T) {
	empty := sha256.Sum256(nil)
	if d, err := AuditDigest(nil); err != nil || d != hex.EncodeToString(empty[:]) {
		t.Fatal(d, err)
	}
	at := time.Unix(1, 0)
	items := []AuditView{{Seq: 1, At: at, Detail: json.RawMessage(`{}`)}, {Seq: 2, At: at, Detail: json.RawMessage(`{"a": 1}`)}}
	d1, err := AuditDigest(items)
	if err != nil {
		t.Fatal(err)
	}
	items[1].Detail = json.RawMessage(`{"a":1}`)
	if d2, _ := AuditDigest(items); d2 != d1 {
		t.Fatal("formatting changed the digest")
	}
	items[1].Detail = json.RawMessage(`{"a":2}`)
	if d3, _ := AuditDigest(items); d3 == d1 {
		t.Fatal("content change kept the digest")
	}
	if _, err := AuditDigest([]AuditView{items[1], items[0]}); err == nil {
		t.Fatal("out of order accepted")
	}
	if _, err := AuditDigest([]AuditView{{Seq: 1, Detail: json.RawMessage(`[`)}}); err == nil {
		t.Fatal("bad detail accepted")
	}
	if MaxVacuumPages <= 0 || CodeMaintenance.HTTPStatus() != 503 {
		t.Fatal("maintenance constants")
	}
}
