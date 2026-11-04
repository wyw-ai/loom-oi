package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// A rejected import must roll back the schema and meta as well as the rows.
// This also proves that an interrupted/refused target can be retried without
// accepting a previously initialized store as empty.
func TestImportSchemaAndDataAreAtomic(t *testing.T) {
	ctx := context.Background()
	src := freshConfig(t, SQLite)
	s := migrated(t, src)
	richSeed(t, s)
	_ = s.Close()
	file, _ := exportOf(t, src)
	for _, be := range backends(t) {
		t.Run(be.name, func(t *testing.T) {
			cfg := freshConfig(t, be.name)
			// The decoder reaches the trailer after creating and populating
			// every table, then rejects trailing invalid JSON.
			bad := append(bytes.Clone(file), ']')
			if _, err := Import(ctx, cfg, testBusy, bytes.NewReader(bad), t0); !errors.Is(err, ErrInvalid) {
				t.Fatalf("bad trailer accepted: %v", err)
			}
			driver, dsn, ops := "sqlite", cfg.SQLitePath, sqliteOps()
			if be.name == Postgres {
				driver, dsn, ops = "pgx", cfg.PostgresDSN, postgresOps()
			}
			db, err := sql.Open(driver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"schema_migrations", "meta", "audit", "accesses"} {
				if exists, err := ops.hasTable(ctx, db, table); err != nil || exists {
					t.Fatalf("failed import left %s: exists=%v err=%v", table, exists, err)
				}
			}
			_ = db.Close()
			if _, err := Import(ctx, cfg, testBusy, bytes.NewReader(file), t0); err != nil {
				t.Fatalf("retry: %v", err)
			}
			initialized := freshConfig(t, be.name)
			_ = migrated(t, initialized).Close()
			if _, err := Import(ctx, initialized, testBusy, bytes.NewReader(file), t0); !errors.Is(err, ErrNotEmpty) {
				t.Fatalf("existing meta accepted as empty: %v", err)
			}
		})
	}
}

func TestExportImportPreservesExactAuditNumbers(t *testing.T) {
	ctx := context.Background()
	detail := json.RawMessage(`{"large":9007199254740993e0,"fraction":1.00000000000000001,"negative":-9007199254740993.00}`)
	want, err := protocol.CanonicalJSON(detail)
	if err != nil {
		t.Fatal(err)
	}
	for _, srcBE := range backends(t) {
		for _, dstBE := range backends(t) {
			t.Run(srcBE.name+"-to-"+dstBE.name, func(t *testing.T) {
				src := freshConfig(t, srcBE.name)
				s := migrated(t, src)
				e := audit("exact-number")
				e.Detail = detail
				if err := s.AppendAudit(ctx, []AuditEntry{e}); err != nil {
					t.Fatal(err)
				}
				_ = s.Close()
				file, _ := exportOf(t, src)
				if !bytes.Contains(file, want) {
					t.Fatalf("export changed exact decimal values: %s", file)
				}
				cfg := freshConfig(t, dstBE.name)
				bad := bytes.Replace(file, []byte("1.00000000000000001"), []byte("1.00000000000000002"), 1)
				if _, err := Import(ctx, cfg, testBusy, bytes.NewReader(bad), t0); !errors.Is(err, ErrInvalid) {
					t.Fatalf("changed exact number passed the digest: %v", err)
				}
				if _, err := Import(ctx, cfg, testBusy, bytes.NewReader(file), t0); err != nil {
					t.Fatal(err)
				}
				d, err := Open(ctx, cfg, testBusy)
				if err != nil {
					t.Fatal(err)
				}
				defer d.Close()
				rows := listAll(t, d)
				got, err := protocol.CanonicalJSON(rows[0].Detail)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("import altered audit detail: %s, %v", got, err)
				}
			})
		}
	}
}
