package integration

import (
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func (e *env) configView() protocol.ConfigView {
	e.t.Helper()
	var cv protocol.ConfigView
	if _, err := e.admin.Do(bg, "GET", "/v1/admin/config", "", nil, &cv); err != nil {
		e.t.Fatal(err)
	}
	return cv
}

// rawDB opens the OIS database directly (OIS must be stopped for SQLite).
func (e *env) rawDB() *sql.DB {
	e.t.Helper()
	if e.be == "sqlite" {
		return must(sql.Open("sqlite", e.sqlite))
	}
	return must(sql.Open("pgx", e.pgDSN))
}

func (e *env) execDB(q string) {
	e.t.Helper()
	db := e.rawDB()
	defer db.Close()
	if _, err := db.Exec(q); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

// serveOnce starts `ois serve` expecting it to refuse and returns its output.
func (e *env) serveRefused(cfgPath string) string {
	e.t.Helper()
	p := startProc(e.t, e.dir, "ois-refused-"+strings.ReplaceAll(time.Now().Format("150405.000000"), ".", ""), oisBin, nil, "serve", "--config", cfgPath)
	if code := p.waitExit(e.t, 20*time.Second); code != 1 {
		e.t.Fatalf("ois serve exit %d, want 1:\n%s", code, p.tail(1000))
	}
	return p.tail(1000)
}

// TV-B3-04: a restart ends every channel, old tickets stay dead, the epoch
// grows, OICs reconnect and the directory comes back from the store with
// readiness unknown (zero tolerance: nothing revives).
func TestTVB3_04_Restart(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		c := e.consumer("alice")
		s := must(c.Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("hold"))
		pending := e.open("alice", "lab-a/http")
		epoch := e.configView().Epoch
		e.acc.setMode(refuse) // keep OICs away to observe the restored directory
		e.acc.cutAll()
		e.stopOIS()
		if _, err := io.ReadAll(s); abortCode(err) == "" {
			t.Fatalf("old channel survived the restart: %v", err)
		}
		_ = s.Close()
		restart := time.Now()
		e.startOIS()
		oisStart := time.Since(restart)
		if got := e.configView().Epoch; got != epoch+1 {
			t.Fatalf("epoch %d after %d", got, epoch)
		}
		list := must(e.consumer("alice").DiscoverAll(bg))
		if len(list) != 2 || list[0].AccessOnline || list[0].Readiness != protocol.ReadinessUnknown {
			t.Fatalf("restored directory %+v", list)
		}
		_, res, err := e.rawAttach(strings.Replace(pending.Attach.URL, hostOf(pending.Attach.URL), e.api, 1), e.token("alice"), pending.Attach.Ticket, nil, protocol.TunnelSubprotocol)
		if err == nil || res == nil || (res.StatusCode != 401 && res.StatusCode != 410) {
			t.Fatalf("ZERO-TOLERANCE: old ticket accepted: %v", err)
		}
		if _, err := e.consumer("alice").Channel(bg, pending.ChannelID); protocol.CodeOf(err) != protocol.CodeNotFound {
			t.Fatalf("ZERO-TOLERANCE: old channel visible: %v", err)
		}
		e.acc.setMode(pass)
		back := time.Now()
		for _, a := range []string{"lab-a", "lab-b"} {
			e.waitRegistered(a)
			if st := e.oicStats(a); st.Epoch != epoch+1 {
				t.Fatalf("%s epoch %d", a, st.Epoch)
			}
		}
		got, _ := request(t, e.consumer("alice"), "lab-a/http", "after restart")
		if got != "T1:after restart" {
			t.Fatalf("%q", got)
		}
		// O2/O4 (B4 §6): the restart needs no manual step; OICs reconnect on
		// their own and the first Open after they are let back succeeds.
		e.obs("O2", map[string]any{"vector": "TV-B3-04", "ois_start_ms": oisStart.Milliseconds(),
			"reconnect_to_open_ms": time.Since(back).Milliseconds()})
		e.obs("O4", map[string]any{"vector": "TV-B3-04", "old_channels": "ended", "old_tickets": "refused", "manual_steps": 0})
		e.baseline()
		e.settled()
	})
}

func hostOf(u string) string {
	h := strings.TrimPrefix(u, "wss://")
	return h[:strings.Index(h, "/")]
}

// TV-B3-05: with PostgreSQL down, admin writes and Opens get 503, no
// SQLite store appears, open channels continue, and service resumes
// without a restart.
func TestTVB3_05_PostgresDown(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		if be != "postgres" {
			t.Skip("PostgreSQL vector")
		}
		e := newEnv(t, be, envOpts{})
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("during outage"))
		start := time.Now()
		e.pg.setMode(refuse)
		e.pg.cutAll()
		e.eventually(10*time.Second, "store unhealthy", func() bool { return !e.configView().StoreHealthy })
		if _, err := e.tryWrite("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "px", Subject: "x", ExposureSelector: "lab-a/*", Actions: []string{"open"}}); protocol.CodeOf(err) != protocol.CodeStoreUnavailable {
			t.Fatalf("admin write: %v", err)
		}
		if _, err := e.consumer("alice").OpenLatest(bg, "lab-a/http", "", false); protocol.CodeOf(err) != protocol.CodeStoreUnavailable {
			t.Fatalf("open: %v", err)
		}
		if m, _ := filepath.Glob(filepath.Join(e.dir, "*.db*")); len(m) != 0 {
			t.Fatalf("SQLite files appeared: %v", m)
		}
		_ = s.CloseWrite()
		if got, err := io.ReadAll(s); err != nil || string(got) != "T1:during outage" {
			t.Fatalf("open channel broken by the outage: %q %v", got, err)
		}
		_ = s.Close()
		e.pg.setMode(pass)
		e.eventually(10*time.Second, "store healthy", func() bool { return e.configView().StoreHealthy })
		e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "px", Subject: "x", ExposureSelector: "lab-a/*", Actions: []string{"open"}})
		got, _ := request(t, e.consumer("alice"), "lab-a/http", "after")
		if got != "T1:after" {
			t.Fatalf("%q", got)
		}
		e.obs("O4", map[string]any{"vector": "TV-B3-05", "outage_ms": time.Since(start).Milliseconds(), "manual_steps": 0,
			"admin_write_during_outage": "StoreUnavailable", "open_during_outage": "StoreUnavailable", "open_channel": "continued"})
		e.settled()
	})
}

// TV-B3-06: a second OIS on the same store refuses to start; losing the
// PostgreSQL lock connection makes the store unavailable until the lock is
// taken again.
func TestTVB3_06_SingleInstance(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{noOICs: true})
		if out := e.serveRefused(e.cfgPath); !strings.Contains(out, "AnotherInstanceActive") {
			t.Fatalf("second instance: %s", out)
		}
		if be == "postgres" {
			// Cutting every PostgreSQL connection includes the lock connection.
			e.pg.setMode(refuse)
			e.pg.cutAll()
			e.eventually(10*time.Second, "lock loss detected", func() bool { return !e.configView().StoreHealthy })
			if _, err := e.tryWrite("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "py", Subject: "y", ExposureSelector: "lab-a/*", Actions: []string{"open"}}); protocol.CodeOf(err) != protocol.CodeStoreUnavailable {
				t.Fatalf("write without the lock: %v", err)
			}
			e.pg.setMode(pass)
			e.eventually(10*time.Second, "lock reacquired", func() bool { return e.configView().StoreHealthy })
			e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "pz", Subject: "z", ExposureSelector: "lab-a/*", Actions: []string{"open"}})
		}
	})
}

// TV-B3-07: a newer schema, a pending migration or a tampered checksum
// makes `ois serve` refuse with the reason.
func TestTVB3_07_SchemaChecks(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{noOICs: true})
		e.stopOIS()
		now := "'2026-11-04T00:00:00Z'"
		if be == "postgres" {
			now = "now()"
		}
		cases := []struct{ name, apply, undo, want string }{
			{"too new", "INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (99, 'future', 'x', " + now + ")",
				"DELETE FROM schema_migrations WHERE version = 99", "SchemaTooNew"},
			{"checksum", "UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1", "", "checksum"},
		}
		db := e.rawDB()
		var sum string
		if err := db.QueryRow("SELECT checksum FROM schema_migrations WHERE version = 1").Scan(&sum); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		cases[1].undo = "UPDATE schema_migrations SET checksum = '" + sum + "' WHERE version = 1"
		for _, c := range cases {
			e.execDB(c.apply)
			if out := e.serveRefused(e.cfgPath); !strings.Contains(out, c.want) {
				t.Fatalf("%s: %s", c.name, out)
			}
			e.execDB(c.undo)
		}
		// A pending migration: drop the record of migration 1.
		db = e.rawDB()
		var name, applied string
		_ = db.QueryRow("SELECT name, CAST(applied_at AS TEXT) FROM schema_migrations WHERE version = 1").Scan(&name, &applied)
		_ = db.Close()
		e.execDB("DELETE FROM schema_migrations WHERE version = 1")
		if out := e.serveRefused(e.cfgPath); !strings.Contains(out, "ois migrate") {
			t.Fatalf("pending: %s", out)
		}
		e.execDB("INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (1, '" + name + "', '" + sum + "', " + now + ")")
		e.startOIS()
	})
}

// TV-B3-08: SQLite settings that do not satisfy B3 §5 (auto_vacuum fixed
// before init) make init and serve refuse.
func TestTVB3_08_SQLiteSettings(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		if be != "sqlite" {
			t.Skip("SQLite vector")
		}
		e := newEnv(t, be, envOpts{noOICs: true})
		path := filepath.Join(e.dir, "preexisting.db")
		db := must(sql.Open("sqlite", path))
		if _, err := db.Exec("CREATE TABLE unrelated (x INTEGER)"); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		var cfg map[string]any
		if err := json.Unmarshal(must(os.ReadFile(e.cfgPath)), &cfg); err != nil {
			t.Fatal(err)
		}
		cfg["store"] = map[string]string{"driver": "sqlite", "sqlite_path": path}
		cfg["api_listen"], cfg["access_listen"] = "127.0.0.1:0", "127.0.0.1:0"
		other := filepath.Join(e.dir, "preexisting.json")
		writeJSON(t, other, cfg)
		if _, code, errOut := run(oisBin, nil, "init", "--config", other); code != 1 || !strings.Contains(errOut, "auto_vacuum") {
			t.Fatalf("init: %d %s", code, errOut)
		}
		if out := e.serveRefused(other); !strings.Contains(out, "auto_vacuum") {
			t.Fatalf("serve: %s", out)
		}
	})
}

// TV-B3-09: once a revoking admin call returns, the next commit check
// sees the new version: the late leg always gets 410.
func TestTVB3_09_RevokeVisibleToCommit(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		for i := range 50 {
			o := e.open("alice", "lab-a/http")
			e.eventually(5*time.Second, "provider attached", func() bool { return e.oisStats().Tickets == 1 })
			e.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(false)})
			_, res, err := e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
			if err == nil || res == nil || res.StatusCode != 410 {
				t.Fatalf("run %d: the commit check missed the revocation: %v", i, err)
			}
			e.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(true)})
		}
		e.settled()
	})
}

// TV-B3-10: starting from an older database, an OIC that saw a later
// epoch moves the epoch forward with one audit alert.
func TestTVB3_10_EpochRollback(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		e.stopOIS()
		e.startOIS()
		for _, a := range []string{"lab-a", "lab-b"} {
			e.eventually(15*time.Second, a+" at epoch 2", func() bool { st := e.oicStats(a); return st.Registered && st.Epoch == 2 })
		}
		e.stopOIS()
		e.execDB("UPDATE meta SET epoch = 0 WHERE id = 1") // an older copy of the database
		e.startOIS()
		e.eventually(15*time.Second, "epoch raised past what OICs saw", func() bool { return e.configView().Epoch == 3 })
		var audit protocol.AuditList
		e.eventually(10*time.Second, "rollback audited", func() bool {
			if _, err := e.admin.Do(bg, "GET", "/v1/admin/audit?limit=100", "", nil, &audit); err != nil {
				return false
			}
			for _, a := range audit.Items {
				if a.Action == "epoch_rollback_detected" {
					return true
				}
			}
			return false
		})
		n := 0
		for _, a := range audit.Items {
			if a.Action == "epoch_rollback_detected" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%d rollback alerts", n)
		}
		e.obs("O2", map[string]any{"vector": "TV-B3-10", "raised_to": 3})
	})
}
