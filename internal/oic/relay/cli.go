package relay

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/wyw-ai/loom-oi/internal/logx"
	"github.com/wyw-ai/loom-oi/internal/oic/config"
)

const usage = `usage: oic <command> [flags]

commands:
  run      --config FILE   connect to OIS and serve the configured exposures
  check    --config FILE   validate the configuration and print revisions
  version                  print the version
`

// notifyContext is replaced in tests.
var notifyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

// Main runs the oic command line and returns the process exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "run", "check":
		fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
		fs.SetOutput(stderr)
		path := fs.String("config", "", "configuration file")
		if err := fs.Parse(args[1:]); err != nil || *path == "" || fs.NArg() > 0 {
			fmt.Fprintf(stderr, "%s: --config FILE is required and no arguments are accepted\n", args[0])
			return 2
		}
		if args[0] == "check" {
			return check(*path, stdout, stderr)
		}
		return run(ctx, *path, stderr)
	case "version":
		fmt.Fprintf(stdout, "oic %s %s\n", Version, runtime.Version())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
	return 2
}

type checked struct {
	ExposureID string `json:"exposure_id"`
	Revision   string `json:"revision"`
	Enabled    bool   `json:"enabled"`
	Target     string `json:"target"`
}

func check(path string, stdout, stderr io.Writer) int {
	cfg, _, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	out := []checked{}
	for _, e := range cfg.Exposures {
		out = append(out, checked{ExposureID: e.ID, Revision: e.Revision, Enabled: e.IsEnabled(), Target: e.Target.Address})
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return 0
}

func run(ctx context.Context, path string, stderr io.Writer) int {
	cfg, _, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	log := logx.New(stderr, cfg.LogLevel)
	a, err := New(Options{ConfigPath: path, Logger: log})
	if err != nil {
		fmt.Fprintln(stderr, "oic:", err)
		return 1
	}
	ctx, stop := notifyContext(ctx)
	defer stop()
	if err := a.Run(ctx); err != nil {
		fmt.Fprintln(stderr, "oic:", err)
		return 1
	}
	return 0
}
