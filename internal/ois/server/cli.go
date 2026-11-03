package server

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/relay"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Version is set at build time with -ldflags "-X ...server.Version=...".
var Version = "dev"

const usage = `usage: ois <command> [flags]

commands:
  serve    --config FILE              run OIS
  migrate  --config FILE [--yes]      apply pending schema migrations
  init     --config FILE [--subject S] [--ttl-seconds N]
                                      create the schema of an empty database
                                      and print the first admin token
  version                             print the version
`

// Main runs the ois command line and returns the process exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		return cmdServe(ctx, args[1:], stderr)
	case "migrate":
		return cmdMigrate(ctx, args[1:], stdout, stderr)
	case "init":
		return cmdInit(ctx, args[1:], stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "ois %s %s\n", Version, runtime.Version())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
	return 2
}

// parse handles a subcommand's flags and loads its configuration.
func parse(fs *flag.FlagSet, args []string, stderr io.Writer) (Config, bool) {
	fs.SetOutput(stderr)
	path := fs.String("config", "", "configuration file")
	if err := fs.Parse(args); err != nil {
		return Config{}, false
	}
	if *path == "" || fs.NArg() > 0 {
		fmt.Fprintf(stderr, "%s: --config FILE is required and no arguments are accepted\n", fs.Name())
		return Config{}, false
	}
	cfg, err := LoadConfig(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return Config{}, false
	}
	return cfg, true
}

// notifyContext is replaced in tests.
var notifyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

func cmdServe(ctx context.Context, args []string, stderr io.Writer) int {
	cfg, ok := parse(flag.NewFlagSet("serve", flag.ContinueOnError), args, stderr)
	if !ok {
		return 2
	}
	ctx, stop := notifyContext(ctx)
	defer stop()
	if err := Serve(ctx, cfg, NewLogger(stderr, cfg.LogLevel)); err != nil {
		fmt.Fprintln(stderr, "ois:", err)
		return 1
	}
	return 0
}

func cmdMigrate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "apply without asking (take a backup first)")
	cfg, ok := parse(fs, args, stderr)
	if !ok {
		return 2
	}
	fmt.Fprintln(stderr, "Back up the database before migrating; migrations are not reversible (B3 §7).")
	if !*yes {
		fmt.Fprintln(stderr, "Re-run with --yes to apply the pending migrations.")
		return 2
	}
	st, applied, err := store.Migrate(ctx, cfg.Store, limits.D(cfg.Limits.SQLiteBusyMS))
	if err != nil {
		fmt.Fprintln(stderr, "ois migrate:", err)
		return 1
	}
	defer st.Close()
	if len(applied) == 0 {
		fmt.Fprintln(stdout, "schema is current")
		return 0
	}
	fmt.Fprintf(stdout, "applied migrations %v\n", applied)
	return 0
}

// cmdInit creates the schema of an empty database and prints the first
// admin token once (B2 §2). On an initialized database it issues a new
// admin token only when no valid one exists, which recovers from expired
// admin tokens while OIS is stopped (the store lock guarantees that).
func cmdInit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	subject := fs.String("subject", "admin", "subject of the admin token")
	ttl := fs.Int64("ttl-seconds", 0, "admin token lifetime (default TOKEN_TTL)")
	cfg, ok := parse(fs, args, stderr)
	if !ok {
		return 2
	}
	if err := initStore(ctx, cfg, protocol.TokenCreate{Subject: *subject, Scope: protocol.ScopeAdmin, TTLSeconds: *ttl}, stdout); err != nil {
		fmt.Fprintln(stderr, "ois init:", err)
		return 1
	}
	return 0
}

var errInitialized = errors.New("store already has a valid admin token; create further tokens through the admin API")

func initStore(ctx context.Context, cfg Config, req protocol.TokenCreate, stdout io.Writer) error {
	st, applied, err := store.Initialize(ctx, cfg.Store, limits.D(cfg.Limits.SQLiteBusyMS))
	if err != nil {
		return err
	}
	defer st.Close()
	snap, err := st.LoadSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, t := range snap.Tokens {
		if t.Scope == protocol.ScopeAdmin && t.RevokedAt == nil && now.Before(t.ExpiresAt) {
			return errInitialized
		}
	}
	tok, bearer, err := relay.NewTokenRecord(cfg.Limits, req, now)
	if err != nil {
		return err
	}
	detail, _ := json.Marshal(map[string]any{"subject": tok.Subject, "expires_at": tok.ExpiresAt, "migrations": applied})
	v, err := st.ApplyConfig(ctx, snap.Meta.ConfigVersion, []store.Mutation{store.PutToken{Token: tok}},
		[]store.AuditEntry{{At: now, Actor: "ois-init", Action: "admin_token_bootstrapped", ObjectType: "token", ObjectID: tok.TokenID, Detail: detail}})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(protocol.TokenCreated{TokenID: tok.TokenID, Token: bearer, Subject: tok.Subject,
		Scope: tok.Scope, ExpiresAt: tok.ExpiresAt, ConfigVersion: v})
}
