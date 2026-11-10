package oic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wyw-ai/loom-oi/internal/procgroup"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type worker struct {
	cmd      *exec.Cmd
	group    *procgroup.Group
	stdin    io.WriteCloser
	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]chan protocol.Frame
	ready    chan protocol.Frame
	handlers map[string]bool
	done     chan struct{}
	stopOnce sync.Once
	failOnce sync.Once
	alive    atomic.Bool
	err      error
	onExit   func()
}

func startWorker(ctx context.Context, spec runtimeSpec, dir string, config map[string]any, logDir, appCode string, forbiddenEnv string, onExit func()) (*worker, error) {
	command := append([]string(nil), spec.command...)
	program := command[0]
	if strings.ContainsAny(program, "/\\") && !filepath.IsAbs(program) {
		program = filepath.Join(dir, program)
	}
	cmd := exec.Command(program, command[1:]...)
	cmd.Dir = dir
	cmd.Env = workerEnvironment(forbiddenEnv)
	childIn, stdin, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdout, childOut, err := os.Pipe()
	if err != nil {
		childIn.Close()
		stdin.Close()
		return nil, err
	}
	stderr, childErr, err := os.Pipe()
	if err != nil {
		childIn.Close()
		childOut.Close()
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	group, err := procgroup.New()
	if err != nil {
		childIn.Close()
		childOut.Close()
		childErr.Close()
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = childIn, childOut, childErr
	w := &worker{cmd: cmd, group: group, stdin: stdin, pending: map[string]chan protocol.Frame{}, ready: make(chan protocol.Frame, 1), done: make(chan struct{}), onExit: onExit}
	if err = group.Start(cmd); err != nil {
		childIn.Close()
		childOut.Close()
		childErr.Close()
		group.Close()
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("start runtime: %w", err)
	}
	childIn.Close()
	childOut.Close()
	childErr.Close()
	w.alive.Store(true)
	readDone := make(chan struct{})
	go func() { defer close(readDone); defer stdout.Close(); w.read(stdout) }()
	go func() {
		defer stderr.Close()
		// Limit retained diagnostics while continuing to drain a noisy worker.
		if err := os.MkdirAll(logDir, 0700); err == nil {
			f, err := os.OpenFile(filepath.Join(logDir, protocol.ID("worker-")+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err == nil {
				fmt.Fprintf(f, "app=%s runtime=%s\n", appCode, spec.key)
				io.Copy(f, io.LimitReader(stderr, 1<<20))
				f.Close()
			}
		}
		io.Copy(io.Discard, stderr)
	}()
	go func() {
		err := cmd.Wait()
		if err == nil {
			err = errors.New("worker exited")
		}
		group.Close()
		// Independently owned pipes remain readable after cmd.Wait. Drain the
		// final protocol response before publishing process termination.
		select {
		case <-readDone:
		case <-time.After(time.Second):
			stdout.Close()
			<-readDone
		}
		w.fail(err)
		close(w.done)
		if w.onExit != nil {
			w.onExit()
		}
	}()
	if err = w.send(protocol.Frame{Type: "start", Protocol: protocol.Version, Config: config}); err != nil {
		w.close(context.Background())
		return nil, err
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case frame := <-w.ready:
		if frame.Type != "ready" || frame.Protocol != protocol.Version {
			w.close(context.Background())
			return nil, fmt.Errorf("worker did not negotiate protocol %d", protocol.Version)
		}
		w.handlers = map[string]bool{}
		for _, handler := range frame.Handlers {
			w.handlers[handler] = true
		}
		if !w.alive.Load() {
			w.close(context.Background())
			return nil, w.failure()
		}
		return w, nil
	case <-w.done:
		return nil, w.failure()
	case <-ctx.Done():
		w.close(context.Background())
		return nil, ctx.Err()
	case <-timer.C:
		w.close(context.Background())
		return nil, fmt.Errorf("worker ready timed out")
	}
}

func workerEnvironment(forbidden string) []string {
	allowed := map[string]bool{"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "TEMP": true, "TMP": true, "HOME": true, "USERPROFILE": true, "USER": true, "USERNAME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TERM": true, "COLORTERM": true, "PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "PROGRAMDATA": true, "LOCALAPPDATA": true, "APPDATA": true}
	out := []string{}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if ok && allowed[upper] && !strings.EqualFold(key, forbidden) {
			out = append(out, entry)
		}
	}
	return out
}

func (w *worker) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), protocol.MaxFrameBytes)
	for scanner.Scan() {
		var frame protocol.Frame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			w.fail(fmt.Errorf("invalid worker JSON: %w", err))
			w.group.Close()
			return
		}
		switch frame.Type {
		case "ready":
			select {
			case w.ready <- frame:
			default:
			}
		case "result", "error":
			if frame.CallID == "" || (frame.Type == "result" && len(frame.Result) == 0) || (frame.Type == "error" && frame.Error == nil) {
				w.fail(errors.New("worker returned an incomplete result frame"))
				w.group.Close()
				return
			}
			w.mu.Lock()
			ch := w.pending[frame.CallID]
			delete(w.pending, frame.CallID)
			w.mu.Unlock()
			if ch != nil {
				ch <- frame
			}
		default:
			w.fail(fmt.Errorf("invalid worker frame type %q", frame.Type))
			w.group.Close()
			return
		}
	}
	if err := scanner.Err(); err != nil {
		w.fail(fmt.Errorf("read worker: %w", err))
		w.group.Close()
	} else {
		w.fail(errors.New("worker protocol output closed"))
		w.group.Close()
	}
}

func (w *worker) send(frame protocol.Frame) error {
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if len(b)+1 > protocol.MaxFrameBytes {
		return protocol.Err("INVALID_ARGUMENT", "worker frame exceeds limit")
	}
	written := make(chan error, 1)
	go func() {
		w.writeMu.Lock()
		defer w.writeMu.Unlock()
		if !w.alive.Load() {
			written <- w.failure()
			return
		}
		_, err := w.stdin.Write(append(b, '\n'))
		written <- err
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-written:
		return err
	case <-timer.C:
		w.fail(errors.New("worker protocol write timed out"))
		_ = w.stdin.Close()
		_ = w.group.Close()
		return w.failure()
	}
}

func (w *worker) call(ctx context.Context, frame protocol.Frame) protocol.Frame {
	response := make(chan protocol.Frame, 1)
	w.mu.Lock()
	if !w.alive.Load() {
		w.mu.Unlock()
		return workerError(frame.CallID, "APP_UNAVAILABLE", "worker is unavailable")
	}
	w.pending[frame.CallID] = response
	w.mu.Unlock()
	defer func() { w.mu.Lock(); delete(w.pending, frame.CallID); w.mu.Unlock() }()
	if err := w.send(frame); err != nil {
		var pe *protocol.Error
		if errors.As(err, &pe) {
			return protocol.Frame{Type: "error", CallID: frame.CallID, Error: pe}
		}
		return workerError(frame.CallID, "CALL_RESULT_UNKNOWN", err.Error())
	}
	select {
	case result := <-response:
		return result
	case <-ctx.Done():
		_ = w.send(protocol.Frame{Type: "cancel", CallID: frame.CallID})
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case result := <-response:
			return result
		case <-w.done:
			select {
			case result := <-response:
				return result
			default:
				return workerError(frame.CallID, "CALL_RESULT_UNKNOWN", "worker stopped before the result was known")
			}
		case <-timer.C:
			return workerError(frame.CallID, "CALL_RESULT_UNKNOWN", "cancellation was requested but no result was confirmed")
		}
	case <-w.done:
		select {
		case result := <-response:
			return result
		default:
			return workerError(frame.CallID, "CALL_RESULT_UNKNOWN", "worker stopped before the result was known")
		}
	}
}

func workerError(callID, code, message string) protocol.Frame {
	return protocol.Frame{Type: "error", CallID: callID, Error: protocol.Err(code, message)}
}
func (w *worker) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	return errors.New("worker unavailable")
}
func (w *worker) fail(err error) {
	w.failOnce.Do(func() {
		w.alive.Store(false)
		w.mu.Lock()
		w.err = err
		pending := w.pending
		w.pending = map[string]chan protocol.Frame{}
		w.mu.Unlock()
		for id, ch := range pending {
			ch <- workerError(id, "CALL_RESULT_UNKNOWN", "worker ended before returning a result")
		}
	})
}
func (w *worker) close(ctx context.Context) error {
	w.stopOnce.Do(func() { _ = w.stdin.Close() })
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-w.done:
		return nil
	case <-timer.C:
	case <-ctx.Done():
	}
	err := w.group.Close()
	select {
	case <-w.done:
	case <-time.After(3 * time.Second):
		if err == nil {
			err = fmt.Errorf("worker did not stop after cleanup")
		}
	}
	return err
}
