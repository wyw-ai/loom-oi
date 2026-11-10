// Package servicecmd supplies the local CLI and process lifecycle shared by
// OIS and OIC. Its authenticated control listener is loopback-only.
package servicecmd

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"gopkg.in/yaml.v3"
)

type Backend interface {
	Run(context.Context) error
	Close(context.Context) error
	Status() any
	Apps() (any, error)
	Load(string) (any, error)
	Unload(string) error
}
type Factory func(string) (Backend, error)

type Options struct {
	Config, Command string
	Args            []string
	Foreground      bool
}

func Parse(name string, args []string) (Options, error) {
	o := Options{Config: name + ".yaml"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			i++
			if i >= len(args) {
				return o, errors.New("--config requires a path")
			}
			o.Config = args[i]
		case "--foreground":
			o.Foreground = true
		case "--help", "-h":
			o.Command = "help"
		default:
			if strings.HasPrefix(args[i], "--") {
				return o, fmt.Errorf("unknown option %s", args[i])
			}
			if o.Command == "" {
				o.Command = args[i]
			} else {
				o.Args = append(o.Args, args[i])
			}
		}
	}
	if o.Command == "" {
		o.Command = "help"
	}
	if o.Foreground && o.Command != "start" {
		return o, errors.New("--foreground is only valid for start")
	}
	switch o.Command {
	case "start", "stop", "status", "apps", "help":
		if o.Command == "apps" && name != "oic" {
			return o, errors.New("apps is only available on OIC")
		}
		if len(o.Args) != 0 {
			return o, errors.New("unexpected command arguments")
		}
	case "load", "unload":
		if name != "oic" || len(o.Args) != 1 {
			return o, errors.New("oic load/unload requires one argument")
		}
	default:
		return o, fmt.Errorf("unknown command %s", o.Command)
	}
	var err error
	o.Config, err = filepath.Abs(o.Config)
	return o, err
}

type control struct {
	URL    string `json:"url"`
	Token  string `json:"token"`
	PID    int    `json:"pid"`
	Config string `json:"config"`
}

func stateDir(name, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var c struct {
		StateDir string `yaml:"state_dir"`
	}
	if err = yaml.Unmarshal(b, &c); err != nil {
		return "", err
	}
	if c.StateDir == "" {
		c.StateDir = "." + name
	}
	if !filepath.IsAbs(c.StateDir) {
		c.StateDir = filepath.Join(filepath.Dir(path), c.StateDir)
	}
	return filepath.Clean(c.StateDir), nil
}

func Main(name string, args []string, factory Factory) int {
	if err := Execute(name, args, factory, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func Execute(name string, args []string, factory Factory, out io.Writer) error {
	o, err := Parse(name, args)
	if err != nil {
		return err
	}
	if o.Command == "help" {
		fmt.Fprintf(out, "%s --config %s.yaml start [--foreground]\n%s --config %s.yaml status|stop\n", name, name, name, name)
		if name == "oic" {
			fmt.Fprintln(out, "oic --config oic.yaml load <app.yaml>|unload <app-code>|apps")
		}
		return nil
	}
	dir, err := stateDir(name, o.Config)
	if err != nil {
		return err
	}
	if o.Command == "start" {
		if existing, e := readControl(dir, o.Config); e == nil {
			if b, e := request(existing, "/status", nil); e == nil {
				_, err = fmt.Fprintln(out, string(b))
				return err
			}
		}
		if o.Foreground {
			return foreground(name, o.Config, dir, factory)
		}
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		logFile, err := os.OpenFile(filepath.Join(dir, name+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer logFile.Close()
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := exec.Command(exe, "--config", o.Config, "start", "--foreground")
		cmd.Dir = filepath.Dir(o.Config)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		detach(cmd)
		if err = cmd.Start(); err != nil {
			return err
		}
		pid := cmd.Process.Pid
		_ = cmd.Process.Release()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			c, e := readControl(dir, o.Config)
			if e == nil {
				if b, e := request(c, "/status", nil); e == nil {
					_, err = fmt.Fprintln(out, string(b))
					return err
				}
			}
			if !alive(pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("%s did not become ready; inspect %s", name, filepath.Join(dir, name+".log"))
	}
	c, err := readControl(dir, o.Config)
	if err != nil {
		if o.Command == "status" {
			fmt.Fprintln(out, `{"running":false}`)
			return nil
		}
		return fmt.Errorf("%s is not running: %w", name, err)
	}
	var body any
	if o.Command == "load" {
		path, e := filepath.Abs(o.Args[0])
		if e != nil {
			return e
		}
		body = map[string]string{"path": path}
	}
	if o.Command == "unload" {
		body = map[string]string{"app": o.Args[0]}
	}
	b, err := request(c, "/"+o.Command, body)
	if err != nil {
		if o.Command == "status" && !alive(c.PID) {
			fmt.Fprintln(out, `{"running":false}`)
			return nil
		}
		return err
	}
	if o.Command == "stop" && c.PID != os.Getpid() {
		deadline := time.Now().Add(10 * time.Second)
		for alive(c.PID) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if alive(c.PID) {
			return errors.New("service cleaned up but its process has not exited")
		}
	}
	_, err = fmt.Fprintln(out, string(b))
	return err
}

func readControl(dir, config string) (control, error) {
	var c control
	b, err := os.ReadFile(filepath.Join(dir, "control.json"))
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Config != config || c.Token == "" || !strings.HasPrefix(c.URL, "http://127.0.0.1:") {
		return c, errors.New("invalid local service record")
	}
	return c, nil
}

func request(c control, path string, body any) ([]byte, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	method := http.MethodGet
	if path == "/load" || path == "/unload" || path == "/stop" {
		method = http.MethodPost
	}
	req, err := http.NewRequest(method, c.URL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, protocol.MaxFrameBytes))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("local control: %s", bytes.TrimSpace(b))
	}
	return bytes.TrimSpace(b), nil
}

func acquire(dir string) (func(), error) {
	// Keep this inode for the lifetime of the directory. Unlinking a lock file
	// allows a second process to lock a replacement inode while the old holder
	// is still exiting. The OS releases the lock even after an unclean crash.
	lock, err := os.OpenFile(filepath.Join(dir, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockService(lock); err != nil {
		_ = lock.Close()
		return nil, errors.New("service already running or starting")
	}
	path := filepath.Join(dir, "service.pid")
	// The PID check also recognizes an older beta process that predates the OS
	// lock. Recovery of a stale PID and acquisition are protected by one lock.
	if b, e := os.ReadFile(path); e == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(b)))
		if parseErr == nil && alive(pid) {
			_ = lock.Close()
			return nil, errors.New("service already running or starting")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		_ = lock.Close()
		return nil, e
	}
	if err = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return func() { _ = os.Remove(path); _ = lock.Close() }, nil
}

func foreground(name, config, dir string, factory Factory) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	release, err := acquire(dir)
	if err != nil {
		return err
	}
	defer release()
	b, err := factory(config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var closeOnce sync.Once
	var closeErr error
	closeBackend := func() {
		closeOnce.Do(func() {
			closeCtx, c := context.WithTimeout(context.Background(), 12*time.Second)
			defer c()
			closeErr = b.Close(closeCtx)
		})
	}
	defer closeBackend()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	c := control{URL: "http://" + listener.Addr().String(), Token: protocol.ID("ctl_"), PID: os.Getpid(), Config: config}
	stop := make(chan struct{})
	done := make(chan struct{})
	var stopOnce sync.Once
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		expected := "Bearer " + c.Token
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			write(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		mutating := r.URL.Path == "/load" || r.URL.Path == "/unload" || r.URL.Path == "/stop"
		if (mutating && r.Method != http.MethodPost) || (!mutating && r.Method != http.MethodGet) {
			write(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var result any
		var opErr error
		switch r.URL.Path {
		case "/status":
			result = map[string]any{"running": true, "pid": c.PID, "service": name, "status": b.Status()}
		case "/apps":
			result, opErr = b.Apps()
		case "/load", "/unload":
			var input struct {
				Path string `json:"path"`
				App  string `json:"app"`
			}
			if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, protocol.MaxFrameBytes)).Decode(&input); e != nil {
				write(w, 400, map[string]string{"error": "invalid request"})
				return
			}
			if r.URL.Path == "/load" {
				if input.Path == "" {
					opErr = errors.New("path required")
				} else {
					result, opErr = b.Load(input.Path)
				}
			} else {
				if input.App == "" {
					opErr = errors.New("app required")
				} else {
					opErr = b.Unload(input.App)
					result = map[string]bool{"unloaded": opErr == nil}
				}
			}
		case "/stop":
			stopOnce.Do(func() { close(stop) })
			<-done
			opErr = closeErr
			result = map[string]bool{"stopped": opErr == nil}
		default:
			write(w, 404, map[string]string{"error": "not found"})
			return
		}
		if opErr != nil {
			write(w, 409, map[string]string{"error": opErr.Error()})
			return
		}
		write(w, 200, result)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	controlErrors := make(chan error, 1)
	go func() { controlErrors <- server.Serve(listener) }()
	data, _ := json.Marshal(c)
	record := filepath.Join(dir, "control.json")
	if err = os.WriteFile(record, data, 0600); err != nil {
		_ = server.Close()
		return err
	}
	defer os.Remove(record)
	runErrors := make(chan error, 1)
	go func() { runErrors <- b.Run(ctx) }()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	fmt.Fprintf(os.Stderr, "%s started pid=%d\n", name, os.Getpid())
	select {
	case <-stop:
	case <-sigs:
	case err = <-runErrors:
	case e := <-controlErrors:
		if !errors.Is(e, http.ErrServerClosed) {
			err = e
		}
	}
	cancel()
	closeBackend()
	close(done)
	shutdownCtx, finish := context.WithTimeout(context.Background(), 3*time.Second)
	defer finish()
	_ = server.Shutdown(shutdownCtx)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return closeErr
}
