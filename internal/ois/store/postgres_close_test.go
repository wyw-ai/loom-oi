package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// delayedPGClose keeps the real server session alive after the client has
// sent Terminate and closed locally. Queries and their responses are real;
// only the network's asynchronous delivery of connection teardown is held.
type delayedPGClose struct {
	net.Conn
	mu          sync.Mutex
	terminating bool
	closed      chan struct{}
	once        sync.Once
}

func pgCloseConfig(t *testing.T, wrap func(net.Conn) net.Conn) (Config, Config) {
	t.Helper()
	return pgCloseConfigConnections(t, func(n int, c net.Conn) net.Conn {
		if n == 1 {
			return wrap(c)
		}
		return c
	})
}

func pgCloseConfigConnections(t *testing.T, wrap func(int, net.Conn) net.Conn) (Config, Config) {
	t.Helper()
	if os.Getenv("OI_TEST_PG_DSN") == "" {
		t.Skip("OI_TEST_PG_DSN not set")
	}
	base := newPGDatabase(t)
	pc, err := pgx.ParseConfig(base.PostgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	if pc.TLSConfig != nil {
		t.Fatal("local PostgreSQL fixture requires sslmode=disable")
	}
	dial := pc.DialFunc
	var mu sync.Mutex
	connectionNumber := 0
	pc.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		connectionNumber++
		return wrap(connectionNumber, c), nil
	}
	registered := stdlib.RegisterConnConfig(pc)
	t.Cleanup(func() { stdlib.UnregisterConnConfig(registered) })
	return base, Config{Driver: Postgres, PostgresDSN: registered}
}

type pgQueryGate struct {
	net.Conn
	mu      sync.Mutex
	match   []byte
	entered chan struct{}
	release chan struct{}
	drop    bool
	seen    bool
	closed  chan struct{}
	once    sync.Once
}

func (c *pgQueryGate) Write(p []byte) (int, error) {
	c.mu.Lock()
	hold := len(c.match) != 0 && bytes.Contains(p, c.match) && !c.seen
	if hold {
		c.seen = true
		close(c.entered)
	}
	drop := c.drop
	c.mu.Unlock()
	if hold {
		if drop {
			return len(p), nil
		}
		<-c.release
	}
	return c.Conn.Write(p)
}

func (c *pgQueryGate) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { close(c.closed) })
	return err
}

type pgExportGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *pgExportGate) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func waitPGClose[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("PostgreSQL close fixture did not reach its explicit barrier")
		var zero T
		return zero
	}
}

func TestPostgresCloseDrainsOperations(t *testing.T) {
	for _, kind := range []string{"write", "health", "export"} {
		t.Run(kind, func(t *testing.T) {
			var gate *pgQueryGate
			base, cfg := pgCloseConfig(t, func(c net.Conn) net.Conn {
				gate = &pgQueryGate{Conn: c, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
				return gate
			})
			s := migrated(t, cfg)
			ctx := context.Background()
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			done := make(chan error, 1)
			switch kind {
			case "write":
				s.beforeAudit = func() error { close(entered); <-release; return nil }
				go func() {
					_, err := s.ApplyConfig(ctx, 0, nil, []AuditEntry{audit("close-drain")})
					done <- err
				}()
			case "health":
				gate.mu.Lock()
				gate.match, gate.entered, gate.release = []byte("SELECT 1"), entered, release
				gate.mu.Unlock()
				go func() { done <- s.Health(ctx) }()
			case "export":
				w := &pgExportGate{entered: entered, release: release}
				go func() { _, err := s.export(ctx, w, t0); done <- err }()
			}
			waitPGClose(t, entered)
			closed := make(chan error, 3)
			for range 3 {
				go func() { closed <- s.Close() }()
			}
			waitPGClose(t, s.lifecycle.stopping)
			select {
			case err := <-closed:
				t.Fatalf("Close passed an admitted %s operation: %v", kind, err)
			default:
			}
			if other, err := Open(ctx, base, testBusy); !errors.Is(err, ErrAnotherInstance) {
				if other != nil {
					_ = other.Close()
				}
				t.Fatalf("old %s still running but another instance was admitted: %v", kind, err)
			}
			// These calls occur after the closing barrier while the original
			// operation is still blocked. None can issue SQL or reacquire.
			assertPGClosedOperations(t, s)
			unblock()
			if err := waitPGClose(t, done); err != nil {
				t.Fatalf("admitted operation did not finish: %v", err)
			}
			for range 3 {
				if err := waitPGClose(t, closed); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatalf("idempotent close: %v", err)
			}
			assertPGClosedOperations(t, s)
			other, err := Open(ctx, base, testBusy)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			snap, err := other.LoadSnapshot(ctx)
			if err != nil || (kind == "write" && snap.Meta.ConfigVersion != 1) {
				t.Fatalf("committed state after handoff: %+v, %v", snap.Meta, err)
			}
		})
	}
}

func TestPostgresFailedOpenAcknowledgesUnlock(t *testing.T) {
	var held *delayedPGClose
	base, cfg := pgCloseConfig(t, func(c net.Conn) net.Conn {
		held = &delayedPGClose{Conn: c, closed: make(chan struct{})}
		return held
	})
	// A new database has no schema. Open acquires its lock, then fails its
	// schema check. Failure destruction must acknowledge the unlock too.
	if s, err := Open(context.Background(), cfg, testBusy); err == nil || errors.Is(err, ErrAnotherInstance) {
		if s != nil {
			_ = s.Close()
		}
		t.Fatalf("missing-schema fixture did not reach failure destruction: %v", err)
	}
	if held == nil {
		t.Fatal("no lock connection")
	}
	defer held.Conn.Close()
	waitPGClose(t, held.closed)
	s, _, err := Migrate(context.Background(), base, testBusy)
	if err != nil {
		t.Fatalf("failed Open left an unacknowledged lock: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreLifecycleStopIsIdempotent(t *testing.T) {
	lifecycle := newStoreLifecycle()
	lifecycle.stop()
	lifecycle.stop()
	if leave, err := lifecycle.enter(); leave != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stopped lifecycle admitted an operation: %v", err)
	}
}

func TestPostgresLockCloseAfterLoss(t *testing.T) {
	for _, recover := range []bool{false, true} {
		name := "unconfirmed_release"
		if recover {
			name = "successful_reacquire"
		}
		t.Run(name, func(t *testing.T) {
			base, _ := pgCloseConfig(t, func(c net.Conn) net.Conn { return c })
			db, err := sql.Open("pgx", base.PostgresDSN)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			lock := &pgLock{db: db}
			defer lock.close()
			ctx := context.Background()
			if err := lock.acquire(ctx); err != nil {
				t.Fatal(err)
			}
			var pid int
			if err := lock.conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				t.Fatal(err)
			}
			var killed bool
			if err := db.QueryRowContext(ctx, "SELECT pg_terminate_backend($1)", pid).Scan(&killed); err != nil || !killed {
				t.Fatalf("terminate our recorded backend: %v %v", killed, err)
			}
			if err := lock.held(ctx, false); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("lost session not observed: %v", err)
			}
			if lock.conn != nil || lock.lostErr == nil {
				t.Fatal("lost session was not retained as unconfirmed")
			}
			if recover {
				if err := lock.held(ctx, true); err != nil {
					t.Fatal(err)
				}
				if lock.lostErr != nil {
					t.Fatal("successful reacquisition did not clear old uncertainty")
				}
			}
			err = lock.close()
			if (recover && err != nil) || (!recover && !errors.Is(err, ErrUnavailable)) {
				t.Fatalf("Close after loss, reacquired=%v: %v", recover, err)
			}
			if got := lock.close(); got != err {
				t.Fatalf("Close terminal changed: %v / %v", got, err)
			}
		})
	}
}

func assertPGClosedOperations(t *testing.T, s *SQLStore) {
	t.Helper()
	ctx := context.Background()
	cases := map[string]func() error{
		"health":   func() error { return s.Health(ctx) },
		"write":    func() error { _, err := s.ApplyConfig(ctx, 0, nil, nil); return err },
		"snapshot": func() error { _, err := s.LoadSnapshot(ctx); return err },
		"audit":    func() error { _, err := s.ListAudit(ctx, 0, 1); return err },
		"usage":    func() error { _, err := s.Usage(ctx); return err },
		"maintain": func() error { _, err := s.Maintain(ctx, protocol.StoreMaintain{Optimize: true}); return err },
		"export":   func() error { _, err := s.export(ctx, io.Discard, t0); return err },
		"import":   func() error { _, err := s.importFrom(ctx, bytes.NewReader(nil), t0); return err },
	}
	for name, f := range cases {
		if err := f(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s admitted after closing: %v", name, err)
		}
	}
}

func TestPostgresLockCloseFailsClosed(t *testing.T) {
	for _, kind := range []string{"not_held", "wrapper_closed", "unlock_blackhole"} {
		t.Run(kind, func(t *testing.T) {
			var gate *pgQueryGate
			_, cfg := pgCloseConfig(t, func(c net.Conn) net.Conn {
				gate = &pgQueryGate{Conn: c, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
				return gate
			})
			db, err := sql.Open("pgx", cfg.PostgresDSN)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			lock := &pgLock{db: db}
			if err := lock.acquire(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer lock.close()
			switch kind {
			case "not_held":
				var ok bool
				if err := lock.conn.QueryRowContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryKey).Scan(&ok); err != nil || !ok {
					t.Fatalf("fixture unlock: %v %v", ok, err)
				}
			case "wrapper_closed":
				if err := lock.conn.Close(); err != nil {
					t.Fatal(err)
				}
			case "unlock_blackhole":
				gate.mu.Lock()
				gate.match, gate.drop = []byte("pg_advisory_unlock"), true
				gate.mu.Unlock()
			}
			// Match production ordering: the pooled connections close before
			// the reserved lock connection's final SQL acknowledgment.
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			err = lock.close()
			elapsed := time.Since(started)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("unconfirmed unlock reported success: %v", err)
			}
			if kind == "unlock_blackhole" {
				waitPGClose(t, gate.entered)
				if !errors.Is(err, context.DeadlineExceeded) || elapsed < lockCheck || elapsed > lockCheck+time.Second {
					t.Fatalf("original %v unlock deadline: elapsed %v, error %v", lockCheck, elapsed, err)
				}
				t.Logf("unlock blackhole uses original %v deadline; elapsed %v, physical socket closed", lockCheck, elapsed)
			}
			waitPGClose(t, gate.closed)
			if got := lock.close(); got != err {
				t.Fatalf("second Close changed terminal error: %v / %v", got, err)
			}
			if got := lock.held(context.Background(), true); !errors.Is(got, ErrUnavailable) {
				t.Fatalf("closed lock reacquired via Health: %v", got)
			}
			if got := lock.acquire(context.Background()); !errors.Is(got, ErrUnavailable) {
				t.Fatalf("closed lock acquired: %v", got)
			}
		})
	}
}

func (c *delayedPGClose) Write(p []byte) (int, error) {
	if bytes.Equal(p, []byte{'X', 0, 0, 0, 4}) {
		c.mu.Lock()
		c.terminating = true
		c.mu.Unlock()
		return len(p), nil
	}
	return c.Conn.Write(p)
}

func (c *delayedPGClose) Close() error {
	c.mu.Lock()
	hold := c.terminating
	c.mu.Unlock()
	if hold {
		c.once.Do(func() { close(c.closed) })
		return nil
	}
	return c.Conn.Close()
}

func TestPostgresCloseAcknowledgesUnlock(t *testing.T) {
	if os.Getenv("OI_TEST_PG_DSN") == "" {
		t.Skip("OI_TEST_PG_DSN not set")
	}
	cfg := newPGDatabase(t)
	pc, err := pgx.ParseConfig(cfg.PostgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	if pc.TLSConfig != nil {
		t.Fatal("deterministic wire fixture requires the local test PostgreSQL sslmode=disable")
	}
	dial := pc.DialFunc
	var mu sync.Mutex
	var held *delayedPGClose
	pc.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		if held == nil {
			held = &delayedPGClose{Conn: conn, closed: make(chan struct{})}
			return held, nil
		}
		return conn, nil
	}
	registered := stdlib.RegisterConnConfig(pc)
	defer stdlib.UnregisterConnConfig(registered)
	s, _, err := Migrate(context.Background(), Config{Driver: Postgres, PostgresDSN: registered}, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mu.Lock()
	conn := held
	mu.Unlock()
	if conn == nil {
		t.Fatal("the dedicated lock connection was not observed")
	}
	defer conn.Conn.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("fixture did not hold the real lock-session teardown")
	}
	// The server still has the old TCP session. Close must have synchronously
	// released its lock, rather than depending on eventual Terminate/EOF.
	result, err := Restore(context.Background(), cfg, testBusy, RestoreRequest{External: "close-ack", MinEpoch: 17, Now: t0})
	if err != nil || result.Epoch != 17 || !result.Maintenance {
		t.Fatalf("Restore after completed Close while old TCP teardown is held: %+v, %v", result, err)
	}
}

// pgDestructorGate stops the actual business socket teardown after database/sql
// has marked the cancelled transaction done, but before driver.Rollback returns.
// It leaves all SQL and server responses untouched.
type pgDestructorGate struct {
	net.Conn
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *pgDestructorGate) Close() error {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return c.Conn.Close()
}

func TestPostgresCloseJoinsCancelledTransaction(t *testing.T) {
	for _, kind := range []string{"write", "export", "migration"} {
		t.Run(kind, func(t *testing.T) {
			var gate *pgDestructorGate
			base, cfg := pgCloseConfigConnections(t, func(n int, c net.Conn) net.Conn {
				if n != 2 {
					return c
				}
				gate = &pgDestructorGate{Conn: c, entered: make(chan struct{}), release: make(chan struct{})}
				return gate
			})
			s := migrated(t, cfg)
			if gate == nil {
				t.Fatal("business connection missing")
			}
			var releaseOnce sync.Once
			releaseDestructor := func() { releaseOnce.Do(func() { close(gate.release) }) }
			defer releaseDestructor()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			var operationOnce sync.Once
			releaseOperation := func() { operationOnce.Do(func() { close(release) }) }
			defer releaseOperation()
			done := make(chan error, 1)
			closed := make(chan error, 1)
			if kind == "write" {
				s.beforeAudit = func() error { close(entered); <-release; return nil }
				go func() { _, err := s.ApplyConfig(ctx, 0, nil, []AuditEntry{audit("cancel-close")}); done <- err }()
			} else if kind == "migration" {
				ops := postgresOps()
				ops.d.timeParam = func(at time.Time) any { close(entered); <-release; return at }
				go func() {
					err := ops.applyOne(ctx, s.wdb, migration{version: 2, name: "cancelled", sql: "SELECT 1", checksum: "test"})
					done <- err
					// Initialization failure destroys the store only after the
					// schema preparation call has returned.
					closed <- s.Close()
				}()
			} else {
				w := &pgExportGate{entered: entered, release: release}
				go func() { _, err := s.export(ctx, w, t0); done <- err }()
			}
			waitPGClose(t, entered)
			cancel()
			waitPGClose(t, gate.entered)
			// awaitDone has already won Tx.done. The foreground Rollback can
			// now return ErrTxDone without joining that driver's destruction.
			releaseOperation()
			if kind != "migration" {
				go func() { closed <- s.Close() }()
				waitPGClose(t, s.lifecycle.stopping)
			}
			select {
			case err := <-done:
				closeErr := waitPGClose(t, closed)
				other, openErr := Open(context.Background(), base, testBusy)
				if other != nil {
					_ = other.Close()
				}
				t.Fatalf("operation left lifecycle before cancelled transaction destruction: operation=%v Close=%v competing Open=%v", err, closeErr, openErr)
			case <-time.After(100 * time.Millisecond):
				// This is only a bounded non-completion assertion while the
				// real destructor is held; it is not a product latency budget.
			}
			select {
			case err := <-closed:
				t.Fatalf("Close passed pending rollback: %v", err)
			default:
			}
			if other, err := Open(context.Background(), base, testBusy); !errors.Is(err, ErrAnotherInstance) {
				if other != nil {
					_ = other.Close()
				}
				t.Fatalf("pending rollback lost instance ownership: %v", err)
			}
			releaseDestructor()
			err := waitPGClose(t, done)
			if kind != "export" && err == nil {
				t.Fatal("cancelled write reported success")
			}
			if err := waitPGClose(t, closed); err != nil {
				t.Fatal(err)
			}
			other, err := Open(context.Background(), base, testBusy)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			snap, err := other.LoadSnapshot(context.Background())
			if err != nil || snap.Meta.ConfigVersion != 0 {
				t.Fatalf("cancelled transaction state: %+v %v", snap.Meta, err)
			}
		})
	}
}
