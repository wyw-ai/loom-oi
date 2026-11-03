package client

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/tunnel"
)

// Version is set at build time with -ldflags "-X ...client.Version=...".
var Version = "dev"

const usage = `usage: oi [--ois URL] [--ca FILE] [--server-name NAME] [--data-max N] <command>

The bearer token is read from OI_TOKEN (or --token); OI_URL and OI_CA_FILE
provide defaults for --ois and --ca.

consumer commands:
  discover                              list visible exposures
  connect EXPOSURE --listen ADDR        forward loopback TCP connections, one channel each
  connect EXPOSURE --stdio              one channel on standard input and output
  channel ID                            show a channel
  close ID                              close a channel

admin commands (admin token):
  admin config | stats | audit [--after N] [--limit N]
  admin access list | get ID | delete ID
  admin access create ID [--pin HEX | --pin-cert FILE]... [--max-exposures N] [--label K=V]... [--disabled]
  admin access update ID [--pin HEX | --pin-cert FILE]... [--clear-pins] [--max-exposures N] [--enable | --disable]
  admin token list | revoke ID
  admin token create --subject S [--scope consumer|admin] [--ttl-seconds N]
  admin policy list | get ID | delete ID
  admin policy create ID --subject S --selector SEL --actions discover,open [--disabled]
  admin policy update ID [--subject S] [--selector SEL] [--actions A] [--enable | --disable]
  Writes take the current config_version unless --if-match N is given.

  version
`

// notifyContext is replaced in tests.
var notifyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

type cli struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	c              *Client
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return usageError{fmt.Sprintf(format, args...)} }

// Main runs the oi command line and returns the process exit code.
func Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	g := flag.NewFlagSet("oi", flag.ContinueOnError)
	g.SetOutput(io.Discard)
	opts := Options{}
	g.StringVar(&opts.URL, "ois", os.Getenv("OI_URL"), "")
	g.StringVar(&opts.Token, "token", os.Getenv("OI_TOKEN"), "")
	g.StringVar(&opts.CAFile, "ca", os.Getenv("OI_CA_FILE"), "")
	g.StringVar(&opts.ServerName, "server-name", "", "")
	g.IntVar(&opts.DataMax, "data-max", 0, "")
	if err := g.Parse(args); err != nil || g.NArg() == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	rest := g.Args()
	switch rest[0] {
	case "version":
		fmt.Fprintf(stdout, "oi %s %s\n", Version, runtime.Version())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	x := &cli{stdin: stdin, stdout: stdout, stderr: stderr}
	err := x.dispatch(ctx, opts, rest)
	var ue usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ue):
		fmt.Fprintf(stderr, "oi: %s\n%s", ue.msg, usage)
		return 2
	}
	var pe *protocol.Error
	var ae *AbortError
	switch {
	case errors.As(err, &pe):
		_ = json.NewEncoder(stderr).Encode(protocol.ErrorBody{Code: pe.Code, Message: pe.Message, ConfigVersion: pe.ConfigVersion})
	case errors.As(err, &ae):
		_ = json.NewEncoder(stderr).Encode(protocol.ErrorBody{Code: ae.Code, Message: ae.Detail})
	default:
		fmt.Fprintln(stderr, "oi:", err)
	}
	return 1
}

func (x *cli) dispatch(ctx context.Context, opts Options, args []string) error {
	known := map[string]bool{"discover": true, "connect": true, "channel": true, "close": true, "admin": true}
	if !known[args[0]] {
		return usagef("unknown command %q", args[0])
	}
	c, err := New(opts)
	if err != nil {
		return usageError{err.Error()}
	}
	x.c = c
	switch args[0] {
	case "discover":
		return x.discover(ctx, args[1:])
	case "connect":
		return x.connect(ctx, args[1:])
	case "channel", "close":
		if len(args) != 2 {
			return usagef("%s needs a channel ID", args[0])
		}
		var v protocol.ChannelView
		if args[0] == "channel" {
			v, err = c.Channel(ctx, args[1])
		} else {
			v, err = c.Close(ctx, args[1])
		}
		if err != nil {
			return err
		}
		return x.print(v)
	}
	return x.admin(ctx, args[1:])
}

func (x *cli) print(v any) error {
	enc := json.NewEncoder(x.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// parse handles flags that may follow positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usageError{err.Error()}
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func (x *cli) discover(ctx context.Context, args []string) error {
	if pos, err := parse(flag.NewFlagSet("discover", flag.ContinueOnError), args); err != nil || len(pos) > 0 {
		return usagef("discover takes no arguments")
	}
	all, err := x.c.DiscoverAll(ctx)
	if err != nil {
		return err
	}
	if all == nil {
		all = []protocol.ExposureView{}
	}
	return x.print(all)
}

func (x *cli) connect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	listen := fs.String("listen", "", "")
	stdio := fs.Bool("stdio", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || (*listen == "") == !*stdio {
		return usagef("connect needs one EXPOSURE and exactly one of --listen ADDR or --stdio")
	}
	ctx, stop := notifyContext(ctx)
	defer stop()
	if *stdio {
		return x.stdio(ctx, pos[0])
	}
	enc := json.NewEncoder(x.stderr)
	return x.c.Listen(ctx, *listen, pos[0],
		func(a net.Addr) { _ = json.NewEncoder(x.stdout).Encode(map[string]string{"listening": a.String()}) },
		func(r tunnel.Result, err error) {
			ev := map[string]any{"code": r.Code, "bytes_up": r.Up, "bytes_down": r.Down}
			if err != nil {
				ev["error"] = err.Error()
			}
			_ = enc.Encode(ev)
		})
}

func (x *cli) stdio(ctx context.Context, exposure string) error {
	s, err := x.c.Dial(ctx, exposure)
	if err != nil {
		return err
	}
	defer s.Close()
	go func() {
		<-ctx.Done()
		s.Abort(protocol.CodeClosedByClient)
		_ = s.leg.CloseNow()
	}()
	go func() {
		if _, err := io.Copy(s, x.stdin); err == nil {
			_ = s.CloseWrite()
		}
	}()
	_, err = io.Copy(x.stdout, s)
	return err
}

// ---- admin ----

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// boolFlag records whether a flag was given.
type boolFlag struct{ set, v bool }

func (b *boolFlag) String() string { return strconv.FormatBool(b.v) }
func (b *boolFlag) Set(s string) error {
	b.set = true
	v, err := strconv.ParseBool(s)
	b.v = v
	return err
}
func (b *boolFlag) IsBoolFlag() bool { return true }

func (x *cli) version(ctx context.Context, ifMatch int64) (string, error) {
	if ifMatch >= 0 {
		return strconv.FormatInt(ifMatch, 10), nil
	}
	var cv protocol.ConfigView
	if _, err := x.c.Do(ctx, http.MethodGet, "/v1/admin/config", "", nil, &cv); err != nil {
		return "", err
	}
	return strconv.FormatInt(cv.ConfigVersion, 10), nil
}

func (x *cli) call(ctx context.Context, method, path string, write bool, ifMatch int64, in any) error {
	v := ""
	if write {
		var err error
		if v, err = x.version(ctx, ifMatch); err != nil {
			return err
		}
	}
	var out json.RawMessage
	if _, err := x.c.Do(ctx, method, path, v, in, &out); err != nil {
		return err
	}
	_, err := x.stdout.Write(append(out, '\n'))
	return err
}

func (x *cli) admin(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("admin needs a subcommand")
	}
	fs := flag.NewFlagSet("admin", flag.ContinueOnError)
	ifMatch := fs.Int64("if-match", -1, "")
	switch args[0] {
	case "config", "stats":
		if pos, err := parse(fs, args[1:]); err != nil || len(pos) > 0 {
			return usagef("admin %s takes no arguments", args[0])
		}
		return x.call(ctx, http.MethodGet, "/v1/admin/"+args[0], false, -1, nil)
	case "audit":
		after := fs.Int64("after", 0, "")
		limit := fs.Int("limit", 0, "")
		if pos, err := parse(fs, args[1:]); err != nil || len(pos) > 0 {
			return usagef("admin audit takes only --after and --limit")
		}
		q := url.Values{"after": {strconv.FormatInt(*after, 10)}}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		return x.call(ctx, http.MethodGet, "/v1/admin/audit?"+q.Encode(), false, -1, nil)
	case "access":
		return x.access(ctx, fs, ifMatch, args[1:])
	case "token":
		return x.token(ctx, fs, ifMatch, args[1:])
	case "policy":
		return x.policy(ctx, fs, ifMatch, args[1:])
	}
	return usagef("unknown admin subcommand %q", args[0])
}

// simple handles list, get and delete of a collection.
func (x *cli) simple(ctx context.Context, coll, verb string, pos []string, ifMatch int64) (bool, error) {
	switch {
	case verb == "list" && len(pos) == 0:
		return true, x.call(ctx, http.MethodGet, "/v1/admin/"+coll, false, -1, nil)
	case verb == "get" && len(pos) == 1 && coll != "tokens":
		return true, x.call(ctx, http.MethodGet, "/v1/admin/"+coll+"/"+url.PathEscape(pos[0]), false, -1, nil)
	case (verb == "delete" || verb == "revoke") && len(pos) == 1:
		return true, x.call(ctx, http.MethodDelete, "/v1/admin/"+coll+"/"+url.PathEscape(pos[0]), true, ifMatch, nil)
	}
	return false, nil
}

func pinOf(certFile string) (string, error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("%s holds no PEM certificate", certFile)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	return identity.SPKIPin(cert), nil
}

func (x *cli) access(ctx context.Context, fs *flag.FlagSet, ifMatch *int64, args []string) error {
	if len(args) == 0 {
		return usagef("admin access needs a verb")
	}
	var pins, certs, labels multi
	fs.Var(&pins, "pin", "")
	fs.Var(&certs, "pin-cert", "")
	fs.Var(&labels, "label", "")
	maxExp := fs.Int("max-exposures", 0, "")
	clear := fs.Bool("clear-pins", false, "")
	var enable, disable, disabled boolFlag
	fs.Var(&enable, "enable", "")
	fs.Var(&disable, "disable", "")
	fs.Var(&disabled, "disabled", "")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	if ok, err := x.simple(ctx, "accesses", args[0], pos, *ifMatch); ok {
		return err
	}
	if (args[0] != "create" && args[0] != "update") || len(pos) != 1 {
		return usagef("usage: admin access create|update ID [flags]")
	}
	spec := protocol.AccessSpec{}
	for _, c := range certs {
		p, err := pinOf(c)
		if err != nil {
			return err
		}
		pins = append(pins, p)
	}
	if len(pins) > 0 || *clear {
		spec.SPKIPins = append([]string{}, pins...)
	}
	if *maxExp > 0 {
		spec.MaxExposures = maxExp
	}
	if len(labels) > 0 {
		spec.Labels = map[string]string{}
		for _, l := range labels {
			k, v, ok := strings.Cut(l, "=")
			if !ok {
				return usagef("--label needs K=V")
			}
			spec.Labels[k] = v
		}
	}
	if args[0] == "create" {
		spec.AccessID = pos[0]
		if disabled.set && disabled.v {
			f := false
			spec.Enabled = &f
		}
		return x.call(ctx, http.MethodPost, "/v1/admin/accesses", true, *ifMatch, spec)
	}
	if enable.set && disable.set {
		return usagef("--enable and --disable exclude each other")
	}
	if enable.set || disable.set {
		on := enable.set && enable.v
		spec.Enabled = &on
	}
	return x.call(ctx, http.MethodPatch, "/v1/admin/accesses/"+url.PathEscape(pos[0]), true, *ifMatch, spec)
}

func (x *cli) token(ctx context.Context, fs *flag.FlagSet, ifMatch *int64, args []string) error {
	if len(args) == 0 {
		return usagef("admin token needs a verb")
	}
	subject := fs.String("subject", "", "")
	scope := fs.String("scope", protocol.ScopeConsumer, "")
	ttl := fs.Int64("ttl-seconds", 0, "")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	if ok, err := x.simple(ctx, "tokens", args[0], pos, *ifMatch); ok {
		return err
	}
	if args[0] != "create" || len(pos) != 0 || *subject == "" {
		return usagef("usage: admin token create --subject S [--scope consumer|admin] [--ttl-seconds N]")
	}
	return x.call(ctx, http.MethodPost, "/v1/admin/tokens", true, *ifMatch,
		protocol.TokenCreate{Subject: *subject, Scope: *scope, TTLSeconds: *ttl})
}

func (x *cli) policy(ctx context.Context, fs *flag.FlagSet, ifMatch *int64, args []string) error {
	if len(args) == 0 {
		return usagef("admin policy needs a verb")
	}
	subject := fs.String("subject", "", "")
	selector := fs.String("selector", "", "")
	actions := fs.String("actions", "", "")
	var enable, disable, disabled boolFlag
	fs.Var(&enable, "enable", "")
	fs.Var(&disable, "disable", "")
	fs.Var(&disabled, "disabled", "")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	if ok, err := x.simple(ctx, "policies", args[0], pos, *ifMatch); ok {
		return err
	}
	if (args[0] != "create" && args[0] != "update") || len(pos) != 1 {
		return usagef("usage: admin policy create|update ID [flags]")
	}
	spec := protocol.PolicySpec{Subject: *subject, ExposureSelector: *selector}
	if *actions != "" {
		spec.Actions = strings.Split(*actions, ",")
	}
	if args[0] == "create" {
		spec.PolicyID = pos[0]
		if disabled.set && disabled.v {
			f := false
			spec.Enabled = &f
		}
		return x.call(ctx, http.MethodPost, "/v1/admin/policies", true, *ifMatch, spec)
	}
	if enable.set && disable.set {
		return usagef("--enable and --disable exclude each other")
	}
	if enable.set || disable.set {
		on := enable.set && enable.v
		spec.Enabled = &on
	}
	return x.call(ctx, http.MethodPatch, "/v1/admin/policies/"+url.PathEscape(pos[0]), true, *ifMatch, spec)
}
