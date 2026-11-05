package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type registryBatchTraceKey struct{}

// The real PG driver reports each logical Exec here. This does not count wire
// round trips: a cold statement can also require a separate prepare exchange.
type registryBatchTrace struct {
	mu      sync.Mutex
	inserts []int
}

func (r *registryBatchTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(registryBatchTraceKey{}) == true && strings.HasPrefix(data.SQL, "INSERT INTO exposures ") {
		r.mu.Lock()
		r.inserts = append(r.inserts, len(data.Args))
		r.mu.Unlock()
	}
	return ctx
}

func (*registryBatchTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (r *registryBatchTrace) counts() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.inserts)
}

func registryBatchExposures(n int) []Exposure {
	rows := make([]Exposure, n)
	for i := range rows {
		rows[i] = Exposure{
			ExposureID: fmt.Sprintf("lab-a/new-%03d", i), AccessID: "lab-a",
			Revision: fmt.Sprintf("r-%d", i), Protocol: "tcp", TransportProfile: "tcp",
			AdmissionMode: "require_ready", Description: fmt.Sprintf("字段 %d: ' ? $1 \\", i),
			UpdatedAt: t0.Add(time.Duration(i)*time.Second + 987*time.Nanosecond).In(time.FixedZone("test", 8*60*60)),
		}
	}
	return rows
}

func registryBatchSeed(t *testing.T, b backend) (*SQLStore, *registryBatchTrace, Snapshot, []AuditEntry) {
	t.Helper()
	cfg := b.cfg(t)
	var trace *registryBatchTrace
	if cfg.Driver == Postgres {
		pc, err := pgx.ParseConfig(cfg.PostgresDSN)
		if err != nil {
			t.Fatal(err)
		}
		trace = &registryBatchTrace{}
		pc.Tracer = trace
		registered := stdlib.RegisterConnConfig(pc)
		t.Cleanup(func() { stdlib.UnregisterConnConfig(registered) })
		cfg.PostgresDSN = registered
	}
	s := migrated(t, cfg)
	ctx := context.Background()
	var mutations []Mutation
	for _, id := range []string{"lab-a", "lab-b"} {
		mutations = append(mutations, PutAccess{Access{AccessID: id, Enabled: true, MaxExposures: 128, CreatedAt: t0, UpdatedAt: t0}})
	}
	if _, err := s.ApplyConfig(ctx, 0, mutations, []AuditEntry{audit("seed_config")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, "registry-batch-run", t0); err != nil {
		t.Fatal(err)
	}
	old := registryBatchExposures(2)
	old[0].ExposureID, old[1].ExposureID = "lab-a/old-0", "lab-a/old-1"
	if err := s.UpsertRegistry(ctx, "lab-a", old, []AuditEntry{audit("seed_registry")}); err != nil {
		t.Fatal(err)
	}
	other := registryBatchExposures(1)
	other[0].ExposureID, other[0].AccessID = "lab-b/keep", "lab-b"
	if err := s.UpsertRegistry(ctx, "lab-b", other, nil); err != nil {
		t.Fatal(err)
	}
	before, err := s.LoadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListAudit(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return s, trace, before, entries
}

func TestRegistryBatchReplacement(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				n    int
				args []int
			}{
				{"empty", 0, nil},
				{"one", 1, []int{8}},
				{"three", 3, []int{24}},
				{"full_chunk_64", 64, []int{512}},
				{"next_chunk_65", 65, []int{512, 8}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s, trace, before, previous := registryBatchSeed(t, b)
					ctx := context.WithValue(context.Background(), registryBatchTraceKey{}, true)
					rows := registryBatchExposures(tc.n)
					appended := []AuditEntry{audit("registry_first"), audit("registry_second")}
					if err := s.UpsertRegistry(ctx, "lab-a", rows, appended); err != nil {
						t.Fatal(err)
					}
					if trace != nil && !reflect.DeepEqual(trace.counts(), tc.args) {
						t.Fatalf("logical INSERT parameter counts: got %v want %v", trace.counts(), tc.args)
					}
					after, err := s.LoadSnapshot(ctx)
					if err != nil {
						t.Fatal(err)
					}
					want := before
					want.Exposures = nil
					for _, e := range before.Exposures {
						if e.AccessID != "lab-a" {
							want.Exposures = append(want.Exposures, e)
						}
					}
					for _, e := range rows {
						e.UpdatedAt = e.UpdatedAt.UTC().Truncate(time.Microsecond)
						want.Exposures = append(want.Exposures, e)
					}
					slices.SortFunc(want.Exposures, func(a, b Exposure) int { return strings.Compare(a.ExposureID, b.ExposureID) })
					if !reflect.DeepEqual(after, want) {
						t.Fatalf("replacement changed fields or unrelated state:\ngot  %+v\nwant %+v", after, want)
					}
					entries, err := s.ListAudit(ctx, 0, 100)
					if err != nil || len(entries) != len(previous)+len(appended) {
						t.Fatalf("audit rows: %v %v", entries, err)
					}
					if !reflect.DeepEqual(entries[:len(previous)], previous) {
						t.Fatal("existing audit prefix changed")
					}
					prevSeq := previous[len(previous)-1].Seq
					for i, expected := range appended {
						got := entries[len(previous)+i]
						if got.Seq <= prevSeq {
							t.Fatal("audit insertion order changed")
						}
						prevSeq = got.Seq
						expected.Seq, expected.ConfigVersion = got.Seq, before.Meta.ConfigVersion
						var gotDetail, wantDetail any
						if json.Unmarshal(got.Detail, &gotDetail) != nil || json.Unmarshal(expected.Detail, &wantDetail) != nil || !reflect.DeepEqual(gotDetail, wantDetail) {
							t.Fatalf("audit detail changed: %s", got.Detail)
						}
						got.Detail, expected.Detail = nil, nil
						if !reflect.DeepEqual(got, expected) {
							t.Fatalf("audit fields: got %+v want %+v", got, expected)
						}
					}
				})
			}
		})
	}
}

func TestRegistryBatchRollback(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			names := []string{"duplicate_in_chunk", "duplicate_across_chunks", "cross_access_primary_key", "namespace_after_valid_prefix", "namespace_after_first_chunk", "prefix_constraint_before_namespace", "before_audit_failure"}
			// SQLite intentionally stores plain TEXT; only PostgreSQL has the
			// existing JSONB rejection being exercised by this final case.
			if b.name == Postgres {
				names = append(names, "invalid_second_audit")
			}
			for _, name := range names {
				t.Run(name, func(t *testing.T) {
					s, _, before, previous := registryBatchSeed(t, b)
					rows := registryBatchExposures(65)
					appended := []AuditEntry{audit("rolled_back_first"), audit("rolled_back_second")}
					wantErr := ErrInvalid
					hookCalls := 0
					switch name {
					case "duplicate_in_chunk":
						rows[2].ExposureID = rows[0].ExposureID
					case "duplicate_across_chunks":
						rows[64].ExposureID = rows[0].ExposureID
					case "cross_access_primary_key":
						rows[64].ExposureID = "lab-b/keep"
					case "namespace_after_valid_prefix":
						rows[2].AccessID = "lab-b"
					case "namespace_after_first_chunk":
						rows[64].AccessID = "lab-b"
					case "prefix_constraint_before_namespace":
						rows[0].ExposureID = "lab-b/keep"
						rows[2].AccessID = "lab-b"
					case "before_audit_failure":
						wantErr = ErrUnavailable
						s.hookMu.Lock()
						s.beforeAudit = func() error { hookCalls++; return fmt.Errorf("%w: registry audit gate", ErrUnavailable) }
						s.hookMu.Unlock()
					case "invalid_second_audit":
						appended[1].Detail = json.RawMessage(`{"incomplete":`)
					}
					err := s.UpsertRegistry(context.Background(), "lab-a", rows, appended)
					s.hookMu.Lock()
					s.beforeAudit = nil
					s.hookMu.Unlock()
					if !errors.Is(err, wantErr) {
						t.Fatalf("failure classification: got %v want %v", err, wantErr)
					}
					if name == "prefix_constraint_before_namespace" && strings.Contains(err.Error(), "outside access") {
						t.Fatalf("later namespace error hid the earlier SQL failure: %v", err)
					}
					if strings.HasPrefix(name, "namespace_") && !strings.Contains(err.Error(), "outside access") {
						t.Fatalf("missing namespace error: %v", err)
					}
					if name == "before_audit_failure" && hookCalls != 1 {
						t.Fatalf("audit gate calls: %d", hookCalls)
					}
					after, err := s.LoadSnapshot(context.Background())
					if err != nil || !reflect.DeepEqual(after, before) {
						t.Fatalf("failed transaction changed original snapshot: %v\nbefore %+v\nafter %+v", err, before, after)
					}
					entries, err := s.ListAudit(context.Background(), 0, 100)
					if err != nil || !reflect.DeepEqual(entries, previous) {
						t.Fatalf("failed transaction changed original audit: %v\nbefore %+v\nafter %+v", err, previous, entries)
					}
				})
			}
		})
	}
}
