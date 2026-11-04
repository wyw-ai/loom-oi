package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// ---- G4 helpers (TASK-005, B3 §8) ----

// pgURL is OI_TEST_PG_DSN pointed at database db.
func pgURL(db string) *url.URL {
	u := must(url.Parse(os.Getenv("OI_TEST_PG_DSN")))
	u.Path = "/" + db
	return u
}

// pgAdmin runs statements on the server's maintenance database.
func pgAdmin(t *testing.T, qs ...string) {
	t.Helper()
	db := must(sql.Open("pgx", os.Getenv("OI_TEST_PG_DSN")))
	defer db.Close()
	for _, q := range qs {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// pgTool runs pg_dump or pg_restore of the PostgreSQL 18 server on
// database db: from OI_IT_PG_BIN, or inside the CI service container
// OI_IT_PG_CONTAINER, whose tools match the server version. A missing
// tool fails the vector rather than skipping it.
func pgTool(t *testing.T, stdin []byte, tool, db string, args ...string) []byte {
	t.Helper()
	u := pgURL(db)
	var cmd *exec.Cmd
	switch {
	case os.Getenv("OI_IT_PG_CONTAINER") != "":
		u.Host = "127.0.0.1:5432" // the server as seen inside its container
		cmd = exec.Command("docker", append([]string{"exec", "-i", os.Getenv("OI_IT_PG_CONTAINER"), tool, "--dbname", u.String()}, args...)...)
	case os.Getenv("OI_IT_PG_BIN") != "":
		bin := filepath.Join(os.Getenv("OI_IT_PG_BIN"), tool)
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd = exec.Command(bin, append([]string{"--dbname", u.String()}, args...)...)
	default:
		t.Fatal("PostgreSQL client tools missing: set OI_IT_PG_BIN or OI_IT_PG_CONTAINER")
	}
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\n%s", tool, err, errb.String())
	}
	return out.Bytes()
}

// backup is what a restore starts from: a SQLite file written by
// `ois backup`, or a pg_dump custom-format archive.
type backup struct {
	path  string
	bytes int64
}

// takeBackup backs up the live database (OIS may be running).
func (e *env) takeBackup(name string) backup {
	e.t.Helper()
	path := filepath.Join(e.dir, name)
	if e.be == "sqlite" {
		var info store.BackupInfo
		if err := json.Unmarshal([]byte(e.runOIS("backup", "--config", e.cfgPath, "--out", path)), &info); err != nil || info.Path != path {
			e.t.Fatalf("backup %+v %v", info, err)
		}
		return backup{path: path, bytes: info.Bytes}
	}
	dump := pgTool(e.t, nil, "pg_dump", e.pgName, "-Fc")
	if err := os.WriteFile(path, dump, 0o600); err != nil {
		e.t.Fatal(err)
	}
	return backup{path: path, bytes: int64(len(dump))}
}

// restoreBackup restores b with OIS stopped: `ois restore --from` for
// SQLite; for PostgreSQL the database is recreated with pg_restore and
// marked with `ois restore --external`, giving minEpoch (the epoch the
// operator saw before the incident) as B3 §8 asks.
func (e *env) restoreBackup(b backup, minEpoch int64) store.RestoreResult {
	e.t.Helper()
	var out string
	if e.be == "sqlite" {
		out = e.runOIS("restore", "--config", e.cfgPath, "--from", b.path, "--yes")
	} else {
		pgAdmin(e.t, "DROP DATABASE IF EXISTS "+e.pgName+" WITH (FORCE)", "CREATE DATABASE "+e.pgName)
		pgTool(e.t, must(os.ReadFile(b.path)), "pg_restore", e.pgName, "--no-owner", "--exit-on-error", "--single-transaction")
		out = e.runOIS("restore", "--config", e.cfgPath, "--external", "pg_restore "+filepath.Base(b.path),
			"--min-epoch", strconv.FormatInt(minEpoch, 10), "--yes")
	}
	var res store.RestoreResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.Maintenance {
		e.t.Fatalf("restore %q: %v", out, err)
	}
	return res
}

// oiEnv runs the oi binary as the administrator.
func (e *env) oiAdmin(args ...string) (string, int, string) {
	return run(oiBin, []string{"OI_URL=https://" + e.api, "OI_TOKEN=" + e.adminTok, "OI_CA_FILE=" + e.caFile}, args...)
}

func (e *env) storeView() protocol.StoreView {
	e.t.Helper()
	var v protocol.StoreView
	if _, err := e.admin.Do(bg, "GET", "/v1/admin/store", "", nil, &v); err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *env) maintain(req protocol.StoreMaintain) protocol.StoreMaintained {
	e.t.Helper()
	var out protocol.StoreMaintained
	if _, err := e.admin.Do(bg, "POST", "/v1/admin/store/maintain", "", req, &out); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// tokenID finds the ID of subject's consumer token.
func (e *env) tokenID(subject string) string {
	e.t.Helper()
	var list struct {
		Items []protocol.TokenView `json:"items"`
	}
	if _, err := e.admin.Do(bg, "GET", "/v1/admin/tokens", "", nil, &list); err != nil {
		e.t.Fatal(err)
	}
	for _, tk := range list.Items {
		if tk.Subject == subject && tk.RevokedAt == nil {
			return tk.TokenID
		}
	}
	e.t.Fatalf("no live token for %s", subject)
	return ""
}

// auditActions lists the actions in the audit log.
func (e *env) auditActions() []string {
	e.t.Helper()
	exp, err := e.adminClient().ExportAudit(bg, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, a := range exp.Items {
		out = append(out, a.Action)
	}
	return out
}

func (e *env) adminClient() *client.Client {
	return must(client.New(client.Options{URL: "https://" + e.api, Token: e.adminTok, CAFile: e.caFile}))
}

// outcome opens exposure for token and reports "ok:<reply>" or the code.
func (e *env) outcome(token, exposure string) string {
	e.t.Helper()
	c := e.clientFor(token)
	s, err := c.Dial(bg, exposure)
	if err != nil {
		return string(protocol.CodeOf(err))
	}
	defer s.Close()
	_, _ = s.Write([]byte("m"))
	_ = s.CloseWrite()
	got, err := io.ReadAll(s)
	if err != nil {
		return "broken:" + err.Error()
	}
	return "ok:" + string(got)
}

// writeConcurrently performs one admin write without t.Fatal so that it
// may run on any goroutine.
func (e *env) writeConcurrently(method, path string, body any) error {
	var cv protocol.ConfigView
	if _, err := e.admin.Do(bg, "GET", "/v1/admin/config", "", nil, &cv); err != nil {
		return err
	}
	_, err := e.admin.Do(bg, method, path, strconv.FormatInt(cv.ConfigVersion, 10), body, nil)
	return err
}

// checkSQLiteCopy verifies a SQLite database file without changing it:
// integrity_check passes and the newest audit version is the meta version.
func checkSQLiteCopy(t *testing.T, path string) {
	t.Helper()
	db := must(sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro"))
	defer db.Close()
	var check string
	var maxAudit, version int64
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Fatalf("integrity_check %q %v", check, err)
	}
	if err := db.QueryRow("SELECT (SELECT max(config_version) FROM audit), config_version FROM meta").Scan(&maxAudit, &version); err != nil || maxAudit != version {
		t.Fatalf("inconsistent copy: audit %d meta %d %v", maxAudit, version, err)
	}
}

// TV-B3-11a: a backup taken while OIS keeps writing is consistent and does
// not disturb the writes (SQLite: ois backup; PostgreSQL: pg_dump -Fc).
func TestTVB3_11a_OnlineBackup(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{noOICs: true})
		var wrote atomic.Int64
		var werr error
		stop, done := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				if err := e.writeConcurrently("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: fmt.Sprintf("w%d", i), Subject: "w",
					ExposureSelector: "lab-a/*", Actions: []string{protocol.ActionDiscover}}); err != nil {
					werr = err
					return
				}
				wrote.Add(1)
			}
		}()
		e.eventually(10*time.Second, "writes under way", func() bool { return wrote.Load() >= 5 })
		start := time.Now()
		b := e.takeBackup("online.backup")
		took := time.Since(start)
		during := wrote.Load()
		e.eventually(10*time.Second, "writes after the backup", func() bool { return wrote.Load() >= during+5 })
		close(stop)
		<-done
		if werr != nil {
			t.Fatalf("a write failed while the backup ran: %v", werr)
		}
		if be == "sqlite" {
			checkSQLiteCopy(t, b.path)
		} else {
			scratch := "oi_it_copy_" + strings.ToLower(protocol.NewID(""))
			pgAdmin(t, "CREATE DATABASE "+scratch)
			t.Cleanup(func() { pgAdmin(t, "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)") })
			pgTool(t, must(os.ReadFile(b.path)), "pg_restore", scratch, "--no-owner", "--exit-on-error", "--single-transaction")
			db := must(sql.Open("pgx", pgURL(scratch).String()))
			var maxAudit, version int64
			err := db.QueryRow("SELECT (SELECT max(config_version) FROM audit), config_version FROM meta").Scan(&maxAudit, &version)
			_ = db.Close()
			if err != nil || maxAudit != version {
				t.Fatalf("inconsistent dump: audit %d meta %d %v", maxAudit, version, err)
			}
		}
		e.obs("O2", map[string]any{"vector": "TV-B3-11a", "backup_ms": took.Milliseconds(), "backup_bytes": b.bytes,
			"writes_total": wrote.Load(), "write_errors": 0})
	})
}

// TV-B3-11b: after a restore OIS is in maintenance: discovery and admin
// work, every new Open is refused with Maintenance (zero tolerance) until
// the administrator re-applies what the backup lost and resumes.
func TestTVB3_11b_RestoreMaintenance(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		for id, subject := range map[string]string{"pt": "trent", "pb": "bob"} {
			e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: id, Subject: subject, ExposureSelector: "lab-a/*",
				Actions: []string{protocol.ActionDiscover, protocol.ActionOpen}})
		}
		trent, bob, alice := e.token("trent"), e.token("bob"), e.token("alice")
		for _, tok := range []string{trent, bob, alice} {
			if got := e.outcome(tok, "lab-a/http"); got != "ok:T1:m" {
				t.Fatalf("before the backup: %s", got)
			}
		}
		b := e.takeBackup("restore.backup")
		epoch := e.configView().Epoch
		// After the backup point: revoke trent's token, delete bob's policy.
		e.write("DELETE", "/v1/admin/tokens/"+e.tokenID("trent"), nil)
		e.write("DELETE", "/v1/admin/policies/pb", nil)
		e.closeIdle()
		e.stopOIS()
		start := time.Now()
		res := e.restoreBackup(b, epoch)
		restoreMS := time.Since(start).Milliseconds()
		e.startOIS()
		cv := e.configView()
		if !cv.Maintenance || res.Epoch < epoch || !slices.Contains(e.auditActions(), "restored_from") {
			t.Fatalf("after restore: %+v %+v", cv, res)
		}
		if list, err := e.clientFor(alice).DiscoverAll(bg); err != nil || len(list) == 0 {
			t.Fatalf("discovery in maintenance: %v", err)
		}
		// The restore brought trent's token and bob's policy back, but no
		// Open is accepted until the administrator resumes.
		for _, a := range []string{"lab-a", "lab-b"} {
			e.waitRegistered(a)
		}
		for name, tok := range map[string]string{"alice": alice, "trent": trent, "bob": bob} {
			if got := e.outcome(tok, "lab-a/http"); got != string(protocol.CodeMaintenance) {
				t.Fatalf("ZERO-TOLERANCE: %s opened in maintenance: %s", name, got)
			}
		}
		e.write("DELETE", "/v1/admin/tokens/"+e.tokenID("trent"), nil)
		e.write("DELETE", "/v1/admin/policies/pb", nil)
		out, code, errOut := run(oisBin, []string{"OI_URL=https://" + e.api, "OI_TOKEN=" + e.adminTok, "OI_CA_FILE=" + e.caFile}, "admin", "resume")
		if code != 0 || !strings.Contains(out, `"maintenance": false`) {
			t.Fatalf("ois admin resume: %d %s %s", code, out, errOut)
		}
		resumed := time.Now()
		if got := e.outcome(trent, "lab-a/http"); got != string(protocol.CodeUnauthenticated) {
			t.Fatalf("revoked token after resume: %s", got)
		}
		if got := e.outcome(bob, "lab-a/http"); got != string(protocol.CodeNotFound) {
			t.Fatalf("deleted policy after resume: %s", got)
		}
		if got := e.outcome(alice, "lab-a/http"); got != "ok:T1:m" {
			t.Fatalf("alice after resume: %s", got)
		}
		e.obs("O2", map[string]any{"vector": "TV-B3-11b", "backup_bytes": b.bytes, "restore_ms": restoreMS,
			"resume_to_open_ms": time.Since(resumed).Milliseconds()})
		e.obs("O4", map[string]any{"vector": "TV-B3-11b", "opens_in_maintenance": 0,
			"manual_steps": []string{"restore", "re-revoke lost revocations", "ois admin resume"}})
		e.baseline()
		e.settled()
	})
}

// TV-B3-11c: a restore never brings back an old term: channels and
// tickets from before are dead and the epoch exceeds every epoch issued
// before the restore.
func TestTVB3_11c_OldTermRejected(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		e.token("alice") // part of the backup, so lookups below test the channel, not the token
		b := e.takeBackup("term.backup")
		e.stopOIS()
		e.startOIS() // OICs move past the backup's epoch
		seen := e.configView().Epoch
		for _, a := range []string{"lab-a", "lab-b"} {
			e.eventually(15*time.Second, a+" at epoch "+strconv.FormatInt(seen, 10), func() bool {
				st := e.oicStats(a)
				return st.Registered && st.Epoch == seen
			})
		}
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("hold"))
		pending := e.open("alice", "lab-a/http")
		e.stopOIS()
		if _, err := io.ReadAll(s); abortCode(err) == "" {
			t.Fatalf("ZERO-TOLERANCE: old channel survived: %v", err)
		}
		_ = s.Close()
		e.restoreBackup(b, seen)
		e.startOIS()
		if got := e.configView().Epoch; got <= seen {
			t.Fatalf("ZERO-TOLERANCE: epoch %d after a restore, OICs saw %d", got, seen)
		}
		_, res, err := e.rawAttach(strings.Replace(pending.Attach.URL, hostOf(pending.Attach.URL), e.api, 1), e.token("alice"), pending.Attach.Ticket, nil, protocol.TunnelSubprotocol)
		if err == nil || res == nil || (res.StatusCode != 401 && res.StatusCode != 410) {
			t.Fatalf("ZERO-TOLERANCE: old ticket accepted after the restore: %v", err)
		}
		if _, err := e.consumer("alice").Channel(bg, pending.ChannelID); protocol.CodeOf(err) != protocol.CodeNotFound {
			t.Fatalf("ZERO-TOLERANCE: old channel visible: %v", err)
		}
		for _, a := range []string{"lab-a", "lab-b"} {
			e.eventually(15*time.Second, a+" in the new term", func() bool {
				st := e.oicStats(a)
				return st.Registered && st.Epoch > seen
			})
		}
		e.obs("O4", map[string]any{"vector": "TV-B3-11c", "epoch_seen": seen, "epoch_after": e.configView().Epoch,
			"old_channels": "ended", "old_tickets": "refused"})
	})
}

// migrationTarget creates an empty database of backend be and an OIS
// configuration that uses it with the env's certificates and limits.
func (e *env) migrationTarget(be string) (string, map[string]string) {
	e.t.Helper()
	var st map[string]string
	if be == "sqlite" {
		st = map[string]string{"driver": "sqlite", "sqlite_path": filepath.Join(e.dir, "migrated.db")}
	} else {
		name := "oi_it_mig_" + strings.ToLower(protocol.NewID(""))
		pgAdmin(e.t, "CREATE DATABASE "+name)
		e.t.Cleanup(func() { pgAdmin(e.t, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })
		st = map[string]string{"driver": "postgres", "postgres_dsn": pgURL(name).String()}
	}
	var cfg map[string]any
	if err := json.Unmarshal(must(os.ReadFile(e.cfgPath)), &cfg); err != nil {
		e.t.Fatal(err)
	}
	cfg["store"] = st
	path := filepath.Join(e.dir, "ois-"+be+".json")
	writeJSON(e.t, path, cfg)
	return path, st
}

// matrix records the access decisions a migration must preserve.
func (e *env) matrix(tokens map[string]string) map[string]string {
	e.t.Helper()
	out := map[string]string{}
	for name, tok := range tokens {
		for _, x := range []string{"lab-a/http", "lab-b/http"} {
			out[name+" "+x] = e.outcome(tok, x)
		}
	}
	return out
}

// TV-B3-11d: a stopped database moves between backends through ois export
// and ois import with identical counts, digests and access decisions;
// refusals leave the target empty and the source is never changed.
func TestTVB3_11d_Migration(t *testing.T) {
	bes := backends(t)
	if !slices.Contains(bes, "sqlite") || !slices.Contains(bes, "postgres") {
		t.Skip("migration needs both backends (OI_IT_BACKENDS=sqlite,postgres)")
	}
	for _, dir := range [][2]string{{"sqlite", "postgres"}, {"postgres", "sqlite"}} {
		t.Run(dir[0]+"-to-"+dir[1], func(t *testing.T) {
			e := newEnv(t, dir[0], envOpts{})
			e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "pt", Subject: "trent", ExposureSelector: "lab-a/*",
				Actions: []string{protocol.ActionDiscover, protocol.ActionOpen}})
			tokens := map[string]string{"alice": e.token("alice"), "carol": e.token("carol"), "bob": e.token("bob"), "trent": e.token("trent")}
			e.write("DELETE", "/v1/admin/tokens/"+e.tokenID("trent"), nil)
			before := e.matrix(tokens)
			epoch := e.configView().Epoch
			e.closeIdle()
			e.stopOIS()
			export := filepath.Join(e.dir, "export.jsonl")
			start := time.Now()
			var sum store.ExportSummary
			if err := json.Unmarshal([]byte(e.runOIS("export", "--config", e.cfgPath, "--out", export)), &sum); err != nil {
				t.Fatal(err)
			}
			exportMS := time.Since(start).Milliseconds()
			target, st := e.migrationTarget(dir[1])
			lines := strings.Split(strings.TrimSpace(string(must(os.ReadFile(export)))), "\n")
			bad := map[string]string{
				"tampered":  strings.Join(append([]string{lines[0], strings.Replace(lines[1], `"enabled":true`, `"enabled":false`, 1)}, lines[2:]...), "\n") + "\n",
				"truncated": strings.Join(lines[:len(lines)-1], "\n") + "\n",
			}
			for name, body := range bad {
				p := filepath.Join(e.dir, name+".jsonl")
				if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, code, _ := run(oisBin, nil, "import", "--config", target, "--in", p, "--yes"); code != 1 {
					t.Fatalf("ZERO-TOLERANCE: %s export imported (exit %d)", name, code)
				}
			}
			start = time.Now()
			var res store.ImportResult
			if err := json.Unmarshal([]byte(e.runOIS("import", "--config", target, "--in", export, "--yes")), &res); err != nil {
				t.Fatal(err)
			}
			importMS := time.Since(start).Milliseconds()
			for table, d := range sum.Tables {
				if res.Summary.Tables[table] != d {
					t.Fatalf("ZERO-TOLERANCE: %s differs: %+v vs %+v", table, res.Summary.Tables[table], d)
				}
			}
			if _, code, _ := run(oisBin, nil, "import", "--config", target, "--in", export, "--yes"); code != 1 {
				t.Fatal("import into a non-empty target accepted")
			}
			// The source is unchanged: exporting it again gives the same digests.
			again := filepath.Join(e.dir, "again.jsonl")
			var sum2 store.ExportSummary
			if err := json.Unmarshal([]byte(e.runOIS("export", "--config", e.cfgPath, "--out", again)), &sum2); err != nil || !reflectEqual(sum, sum2) {
				t.Fatalf("source changed: %v", err)
			}
			// Switch OIS to the target and compare the decisions.
			source := e.cfgPath
			e.cfgPath, e.be = target, dir[1]
			if dir[1] == "sqlite" {
				e.sqlite = st["sqlite_path"]
			} else {
				e.pgDSN = st["postgres_dsn"]
			}
			e.startOIS()
			for _, a := range []string{"lab-a", "lab-b"} {
				e.waitRegistered(a)
			}
			if got := e.configView(); got.Epoch <= epoch || got.Backend != dir[1] {
				t.Fatalf("epoch went back or wrong backend: %+v (was %d)", got, epoch)
			}
			after := e.matrix(tokens)
			if !reflectEqual(before, after) {
				t.Fatalf("decisions changed:\n%v\n%v", before, after)
			}
			e.obs("O2", map[string]any{"vector": "TV-B3-11d", "direction": dir[0] + "-to-" + dir[1], "export_ms": exportMS,
				"import_ms": importMS, "audit_rows": sum.Tables["audit"].Count, "source": filepath.Base(source)})
			e.baseline()
			e.settled()
		})
	}
}

func reflectEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// fill writes into dir until the file system is full and returns the
// filler's path.
func fill(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "filler")
	f := must(os.Create(p))
	defer f.Close()
	chunk := make([]byte, 64<<10)
	for {
		if _, err := f.Write(chunk); err != nil {
			return p
		}
	}
}

// TV-B3-11e: a full or read-only store refuses admin writes and new Opens
// with StoreUnavailable, alerts with the cause, keeps open channels and
// recovers without a restart once writes succeed again.
func TestTVB3_11e_DiskFullReadOnly(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		var e *env
		var cause, small string
		if be == "sqlite" {
			fsRoot := os.Getenv("OI_IT_SMALLFS")
			if fsRoot == "" {
				t.Skip("set OI_IT_SMALLFS to a small writable file system (CI mounts a tmpfs)")
			}
			small = filepath.Join(fsRoot, strings.NewReplacer("/", "_").Replace(t.Name()))
			_ = os.RemoveAll(small)
			if err := os.MkdirAll(small, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(small) })
			e = newEnv(t, be, envOpts{sqlitePath: filepath.Join(small, "ois.db")})
			cause = store.FailureDiskFull
		} else {
			e = newEnv(t, be, envOpts{})
			cause = store.FailureReadOnly
		}
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("during "))
		var filler string
		if be == "sqlite" {
			filler = fill(t, small)
		} else {
			pgAdmin(t, "ALTER SYSTEM SET default_transaction_read_only = on", "SELECT pg_reload_conf()")
			t.Cleanup(func() { pgAdmin(t, "ALTER SYSTEM RESET default_transaction_read_only", "SELECT pg_reload_conf()") })
		}
		start := time.Now()
		labels := map[string]string{}
		for i := range 32 {
			labels[fmt.Sprintf("k%02d", i)] = strings.Repeat("v", 250)
		}
		var werr error
		for i := 0; i < 400 && werr == nil; i++ {
			_, werr = e.tryWrite("POST", "/v1/admin/accesses", protocol.AccessSpec{AccessID: fmt.Sprintf("fill-%d", i), Labels: labels})
		}
		if protocol.CodeOf(werr) != protocol.CodeStoreUnavailable {
			t.Fatalf("admin write on a %s store: %v", cause, werr)
		}
		detected := time.Since(start)
		if _, err := e.consumer("alice").OpenLatest(bg, "lab-a/http", "", false); protocol.CodeOf(err) != protocol.CodeStoreUnavailable {
			t.Fatalf("open on a %s store: %v", cause, err)
		}
		if v := e.storeView(); v.Healthy || v.WriteFailure != cause {
			t.Fatalf("store view %+v", v)
		}
		e.ois.waitLine(t, 5*time.Second, "store write failed", 0, func(m map[string]any) bool {
			return m["msg"] == "store write failed" && m["failure"] == cause
		})
		// A read probe succeeds but the store stays down: only a write
		// proves recovery.
		time.Sleep(3 * 500 * time.Millisecond)
		if e.configView().StoreHealthy {
			t.Fatal("recovered while writes still fail")
		}
		_ = s.CloseWrite()
		if got, err := io.ReadAll(s); err != nil || string(got) != "T1:during " {
			t.Fatalf("open channel broken: %q %v", got, err)
		}
		_ = s.Close()
		freed := time.Now()
		if be == "sqlite" {
			if err := os.Remove(filler); err != nil {
				t.Fatal(err)
			}
		} else {
			pgAdmin(t, "ALTER SYSTEM RESET default_transaction_read_only", "SELECT pg_reload_conf()")
		}
		e.eventually(10*time.Second, "store writable again", func() bool { return e.configView().StoreHealthy })
		recovered := time.Since(freed)
		e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "after", Subject: "x", ExposureSelector: "lab-a/*", Actions: []string{"open"}})
		if got, _ := request(t, e.consumer("alice"), "lab-a/http", "after"); got != "T1:after" {
			t.Fatalf("%q", got)
		}
		e.obs("O4", map[string]any{"vector": "TV-B3-11e", "cause": cause, "detect_ms": detected.Milliseconds(),
			"recovery_ms": recovered.Milliseconds(), "manual_steps": []string{"free space or make the store writable"}, "restart": false})
		e.baseline()
		e.settled()
		if be != "sqlite" {
			return
		}
		e.closeIdle()
		e.stopOIS()
		checkSQLiteCopy(t, e.sqlite)
		if runtime.GOOS == "windows" {
			return
		}
		// A read-only database directory is refused at startup with the reason.
		for _, p := range []string{e.sqlite + "-wal", e.sqlite + "-shm"} {
			_ = os.Remove(p)
		}
		if err := os.Chmod(small, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(small, 0o755) })
		if out := e.serveRefused(e.cfgPath); !strings.Contains(strings.ToLower(out), "readonly") && !strings.Contains(strings.ToLower(out), "read-only") && !strings.Contains(out, "unable to open") {
			t.Fatalf("read-only start: %s", out)
		}
		_ = os.Chmod(small, 0o755)
	})
}

// TV-B3-11f: a long read transaction holds back the checkpoint; writes and
// revocations go on, the WAL growth is visible, and a truncate checkpoint
// after the reader ends empties the WAL.
func TestTVB3_11f_LongReader(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		if be != "sqlite" {
			t.Skip("SQLite vector")
		}
		e := newEnv(t, be, envOpts{noOICs: true})
		rdb := must(sql.Open("sqlite", "file:"+filepath.ToSlash(e.sqlite)+"?mode=ro"))
		defer rdb.Close()
		conn := must(rdb.Conn(bg))
		if _, err := conn.ExecContext(bg, "BEGIN"); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := conn.QueryRowContext(bg, "SELECT count(*) FROM audit").Scan(&n); err != nil {
			t.Fatal(err)
		}
		before := e.storeView().Usage.WALBytes
		victim := e.token("victim")
		var worst time.Duration
		for i := range 300 {
			st := time.Now()
			e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: fmt.Sprintf("r%d", i), Subject: "r", ExposureSelector: "lab-a/*", Actions: []string{"open"}})
			worst = max(worst, time.Since(st))
		}
		revoke := time.Now()
		e.write("DELETE", "/v1/admin/tokens/"+e.tokenID("victim"), nil)
		if _, err := e.clientFor(victim).DiscoverAll(bg); protocol.CodeOf(err) != protocol.CodeUnauthenticated {
			t.Fatalf("revocation under a long reader: %v", err)
		}
		revokeMS := time.Since(revoke).Milliseconds()
		peak := e.storeView().Usage.WALBytes
		held := e.maintain(protocol.StoreMaintain{Checkpoint: "truncate"})
		if peak <= before || held.Checkpoint == nil || !held.Checkpoint.Busy || held.After.WALBytes == 0 {
			t.Fatalf("WAL %d -> %d, checkpoint with reader %+v", before, peak, held)
		}
		_, _ = conn.ExecContext(bg, "COMMIT")
		_ = conn.Close()
		done := e.maintain(protocol.StoreMaintain{Checkpoint: "truncate"})
		if done.Checkpoint == nil || done.Checkpoint.Busy || done.After.WALBytes != 0 {
			t.Fatalf("checkpoint after the reader %+v", done)
		}
		e.obs("O2", map[string]any{"vector": "TV-B3-11f", "wal_before": before, "wal_peak": peak, "writes": 300,
			"worst_write_ms": worst.Milliseconds(), "revoke_ms": revokeMS})
	})
}

// TV-B3-11g: a burst of admin writes beyond WRITE_QUEUE is refused fast
// with StoreBusy, a revocation goes through with retries, and Opens and
// OIC heartbeats are not held up.
func TestTVB3_11g_WriteCongestion(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "pv", Subject: "victim", ExposureSelector: "lab-a/*", Actions: []string{"discover", "open"}})
		victim := e.token("victim")
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("burst "))
		const burst = 400
		// One established connection per writer: the burst then arrives at
		// once instead of queueing in the listener's accept backlog.
		tlsConf := e.stats.Transport.(*http.Transport).TLSClientConfig
		clients := make([]*http.Client, burst)
		for i := range clients {
			tr := &http.Transport{MaxConnsPerHost: 1, TLSClientConfig: tlsConf}
			clients[i] = &http.Client{Transport: tr}
			defer tr.CloseIdleConnections()
		}
		do := func(hc *http.Client, method, path, ifMatch string, body []byte) (int, error) {
			req := must(http.NewRequest(method, "https://"+e.api+path, bytes.NewReader(body)))
			req.Header.Set("Authorization", "Bearer "+e.adminTok)
			if ifMatch != "" {
				req.Header.Set("If-Match", strconv.Quote(ifMatch))
			}
			res, err := hc.Do(req)
			if err != nil {
				return 0, err
			}
			defer res.Body.Close()
			_, _ = io.Copy(io.Discard, res.Body)
			return res.StatusCode, nil
		}
		sem := make(chan struct{}, 16)
		var warm sync.WaitGroup
		var warmErr atomic.Value
		for _, hc := range clients {
			warm.Add(1)
			go func() {
				defer warm.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				var err error
				for range 5 {
					if _, err = do(hc, "GET", "/v1/admin/config", "", nil); err == nil {
						return
					}
					time.Sleep(20 * time.Millisecond)
				}
				warmErr.Store(err)
			}()
		}
		warm.Wait()
		if err := warmErr.Load(); err != nil {
			t.Fatalf("warming connections: %v", err)
		}
		version := e.version()
		body := must(json.Marshal(protocol.PolicySpec{Actions: []string{"discover", "open"}}))
		var mu sync.Mutex
		var firstErr error
		codes := map[int]int{}
		var busyWorst, worst time.Duration
		gate := make(chan struct{})
		var wg sync.WaitGroup
		for _, hc := range clients {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-gate
				st := time.Now()
				code, err := do(hc, "PATCH", "/v1/admin/policies/pv", version, body)
				d := time.Since(st)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					code = -1
					if firstErr == nil {
						firstErr = err
					}
				}
				codes[code]++
				worst = max(worst, d)
				if code == 503 {
					busyWorst = max(busyWorst, d)
				}
			}()
		}
		start := time.Now()
		close(gate)
		// Opens and heartbeats during the burst.
		if got, _ := request(t, e.consumer("alice"), "lab-a/http", "meanwhile"); got != "T1:meanwhile" {
			t.Fatalf("open during the burst: %q", got)
		}
		wg.Wait()
		burstMS := time.Since(start).Milliseconds()
		if codes[200] != 1 || codes[503] == 0 || codes[-1] != 0 || codes[200]+codes[409]+codes[503] != burst {
			t.Fatalf("burst outcomes %v (first transport error: %v)", codes, firstErr)
		}
		if busyWorst > 2*time.Second {
			t.Fatalf("StoreBusy took %v: the refusal must be fast", busyWorst)
		}
		// The revocation goes through with retries on Busy or conflict.
		revoke := time.Now()
		var err error
		for attempt := 0; attempt < 50; attempt++ {
			if err = e.writeConcurrently("DELETE", "/v1/admin/tokens/"+e.tokenID("victim"), nil); err == nil {
				break
			}
			if c := protocol.CodeOf(err); c != protocol.CodeStoreBusy && c != protocol.CodeVersionConflict {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err != nil || time.Since(revoke) > 5500*time.Millisecond {
			t.Fatalf("revocation under congestion: %v after %v", err, time.Since(revoke))
		}
		if got := e.outcome(victim, "lab-a/http"); got != string(protocol.CodeUnauthenticated) {
			t.Fatalf("revoked token: %s", got)
		}
		for _, a := range []string{"lab-a", "lab-b"} {
			if st := e.oicStats(a); !st.Registered || st.Closed[string(protocol.CodeLicenseExpired)] != 0 {
				t.Fatalf("%s during the burst: %+v", a, st)
			}
		}
		_ = s.CloseWrite()
		if got, err := io.ReadAll(s); err != nil || string(got) != "T1:burst " {
			t.Fatalf("open channel: %q %v", got, err)
		}
		_ = s.Close()
		for _, hc := range clients {
			hc.CloseIdleConnections()
		}
		e.obs("O2", map[string]any{"vector": "TV-B3-11g", "burst": burst, "ok": codes[200], "conflict": codes[409], "busy": codes[503],
			"busy_worst_ms": busyWorst.Milliseconds(), "worst_ms": worst.Milliseconds(), "burst_ms": burstMS,
			"revoke_ms": time.Since(revoke).Milliseconds()})
		e.baseline()
		e.settled()
	})
}

// TV-B3-11h: audit records are deleted only after an export whose digest
// matches; the freed SQLite pages are returned by an incremental vacuum.
func TestTVB3_11h_AuditRetention(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{noOICs: true})
		labels := map[string]string{}
		for i := range 32 {
			labels[fmt.Sprintf("k%02d", i)] = strings.Repeat("v", 250)
		}
		for i := range 150 {
			id := fmt.Sprintf("bulk-%d", i)
			e.write("POST", "/v1/admin/accesses", protocol.AccessSpec{AccessID: id, Labels: labels})
			e.write("DELETE", "/v1/admin/accesses/"+id, nil)
		}
		if be == "sqlite" {
			e.maintain(protocol.StoreMaintain{Checkpoint: "truncate"})
		}
		before := e.storeView().Usage
		dir := t.TempDir()
		file := filepath.Join(dir, "audit.json")
		out, code, errOut := e.oiAdmin("admin", "audit", "export", "--out", file)
		if code != 0 {
			t.Fatalf("export: %s %s", out, errOut)
		}
		var exp protocol.AuditExport
		if err := json.Unmarshal(must(os.ReadFile(file)), &exp); err != nil || exp.Count < 300 {
			t.Fatalf("%d records %v", exp.Count, err)
		}
		// A changed export and a wrong digest delete nothing.
		exp.Items[1].Action = "edited"
		tampered := filepath.Join(dir, "tampered.json")
		if err := os.WriteFile(tampered, must(json.Marshal(exp)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, code, _ := e.oiAdmin("admin", "audit", "prune", "--export", tampered); code != 1 {
			t.Fatal("ZERO-TOLERANCE: a tampered export was accepted for pruning")
		}
		if _, err := e.admin.Do(bg, "POST", "/v1/admin/audit/prune", "", protocol.AuditPrune{ThroughSeq: exp.ThroughSeq, SHA256: strings.Repeat("0", 64)}, nil); protocol.CodeOf(err) != protocol.CodeInvalidRequest {
			t.Fatalf("wrong digest: %v", err)
		}
		if n := len(e.auditActions()); n < exp.Count {
			t.Fatalf("ZERO-TOLERANCE: records deleted without a matching export (%d left of %d)", n, exp.Count)
		}
		out, code, errOut = e.oiAdmin("admin", "audit", "prune", "--export", file)
		var pr protocol.AuditPruned
		if code != 0 || json.Unmarshal([]byte(out), &pr) != nil || pr.Deleted != int64(exp.Count) {
			t.Fatalf("prune: %s %s", out, errOut)
		}
		if acts := e.auditActions(); len(acts) == 0 || acts[0] != "audit_pruned" {
			t.Fatalf("after prune %v", acts)
		}
		after := before
		if be == "sqlite" {
			e.maintain(protocol.StoreMaintain{Checkpoint: "truncate"})
			freed := e.storeView().Usage
			res := e.maintain(protocol.StoreMaintain{IncrementalVacuumPages: protocol.MaxVacuumPages, Checkpoint: "truncate"})
			after = res.After
			if freed.FreelistCount == 0 || after.FreelistCount != 0 || after.DBBytes >= before.DBBytes {
				t.Fatalf("vacuum: before %+v freed %+v after %+v", before, freed, after)
			}
		} else {
			if _, code, errOut := e.oiAdmin("admin", "store", "maintain", "--optimize"); code != 0 {
				t.Fatal(errOut)
			}
			after = e.storeView().Usage
		}
		e.obs("O2", map[string]any{"vector": "TV-B3-11h", "audit_rows": exp.Count, "db_bytes_before": before.DBBytes,
			"db_bytes_after": after.DBBytes, "pages_before": before.PageCount, "pages_after": after.PageCount})
	})
}

// growAudit adds rows to the stopped database so maintenance commands run
// long enough to be interrupted.
func (e *env) growAudit(rows int) {
	e.t.Helper()
	db := e.rawDB()
	defer db.Close()
	tx := must(db.Begin())
	detail := `{"pad":"` + strings.Repeat("x", 400) + `"}`
	q := "INSERT INTO audit (at, actor, action, object_type, object_id, config_version, detail) VALUES (?, 'grow', 'grow', 'test', 'x', 0, ?)"
	at := any(time.Now().UTC().Format(time.RFC3339Nano))
	if e.be == "postgres" {
		q = "INSERT INTO audit (at, actor, action, object_type, object_id, config_version, detail) VALUES ($1, 'grow', 'grow', 'test', 'x', 0, CAST($2 AS jsonb))"
		at = time.Now().UTC()
	}
	stmt := must(tx.Prepare(q))
	for range rows {
		if _, err := stmt.Exec(at, detail); err != nil {
			e.t.Fatal(err)
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		e.t.Fatal(err)
	}
}

// interrupt starts an ois command and kills it after delay.
func (e *env) interrupt(delay time.Duration, args ...string) {
	e.t.Helper()
	p := startProc(e.t, e.dir, fmt.Sprintf("ois-%s-%d", args[0], time.Now().UnixNano()), oisBin, nil, args...)
	time.Sleep(delay)
	p.kill()
}

// TV-B3-11i: killing backup, restore or import halfway never leaves a
// usable-looking partial result or a damaged source.
func TestTVB3_11i_Interrupted(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{noOICs: true})
		version := e.configView().ConfigVersion
		e.closeIdle()
		e.stopOIS()
		e.growAudit(60_000)
		export := filepath.Join(e.dir, "big.jsonl")
		e.runOIS("export", "--config", e.cfgPath, "--out", export)
		target, _ := e.migrationTarget(be)
		killed := 0
		for _, delay := range []time.Duration{30 * time.Millisecond, 120 * time.Millisecond} {
			e.interrupt(delay, "import", "--config", target, "--in", export, "--yes")
			killed++
		}
		var res store.ImportResult
		if err := json.Unmarshal([]byte(e.runOIS("import", "--config", target, "--in", export, "--yes")), &res); err != nil || res.Summary.Tables["audit"].Count < 60_000 {
			t.Fatalf("import after interrupted imports: %+v %v", res, err)
		}
		if be == "sqlite" {
			out := filepath.Join(e.dir, "cut.backup")
			e.interrupt(40*time.Millisecond, "backup", "--config", e.cfgPath, "--out", out)
			if _, err := os.Stat(out); err == nil {
				checkSQLiteCopy(t, out) // the backup finished before the kill
			}
			if _, err := os.Stat(out + ".partial"); err == nil {
				if _, code, _ := run(oisBin, nil, "restore", "--config", e.cfgPath, "--from", out+".partial", "--yes"); code != 1 {
					t.Fatal("ZERO-TOLERANCE: a partial backup was restored")
				}
			}
			full := e.takeBackup("full.backup")
			e.interrupt(40*time.Millisecond, "restore", "--config", e.cfgPath, "--from", full.path, "--yes")
			checkSQLiteCopy(t, e.sqlite)
		}
		// The source (or whichever complete database is in place) serves.
		e.startOIS()
		cv := e.configView()
		if cv.ConfigVersion != version {
			t.Fatalf("config version %d, want %d", cv.ConfigVersion, version)
		}
		e.obs("O4", map[string]any{"vector": "TV-B3-11i", "killed_imports": killed, "maintenance_after": cv.Maintenance})
	})
}
