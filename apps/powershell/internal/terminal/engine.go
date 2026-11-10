//go:build windows

package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wyw-ai/loom-oi/apps/powershell/internal/protocol"
)

type Result struct {
	Output            string `json:"output"`
	SessionID         string `json:"session_id,omitempty"`
	ExitCode          *int   `json:"exit_code,omitempty"`
	TerminationReason string `json:"termination_reason,omitempty"`
	Signal            int    `json:"signal,omitempty"`
	OutputTruncated   bool   `json:"output_truncated"`
	OmittedBytes      int64  `json:"omitted_bytes"`
	OutputIncomplete  bool   `json:"output_incomplete"`
}
type call struct {
	fingerprint string
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	response    protocol.Frame
	completed   bool
	finished    time.Time
}
type session struct {
	id                              string
	owner                           protocol.Context
	proc                            *process
	tty                             bool
	output                          outputBuffer
	done                            chan struct{}
	finished                        bool
	finishing                       bool
	busy                            bool
	closed                          bool
	started, lastActive, finishedAt time.Time
	exit                            int
	signal                          int
	reason                          string
	incomplete                      bool
}
type Engine struct {
	mu          sync.Mutex
	config      Config
	sessions    map[string]*session
	calls       map[string]*call
	active      int
	closing     bool
	monitorDone chan struct{}
	workers     sync.WaitGroup
}

func New(c Config) (*Engine, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	e := &Engine{config: c, sessions: map[string]*session{}, calls: map[string]*call{}, monitorDone: make(chan struct{})}
	go e.monitor()
	return e, nil
}

func errorFrame(id, code, msg string) protocol.Frame {
	return protocol.Frame{Type: "error", CallID: id, Error: protocol.Err(code, msg)}
}
func (e *Engine) Invoke(frame protocol.Frame) protocol.Frame {
	return e.invoke(frame, nil)
}

func (e *Engine) invoke(frame protocol.Frame, accepted chan struct{}) protocol.Frame {
	var once sync.Once
	accept := func() {
		if accepted != nil {
			once.Do(func() { close(accepted) })
		}
	}
	defer accept()
	if frame.CallID == "" {
		return errorFrame("", "INVALID_ARGUMENT", "call_id is required")
	}
	if frame.Context.AccessIdentity == "" || frame.Context.VMCPID == "" {
		return errorFrame(frame.CallID, "INVALID_ARGUMENT", "trusted invocation context is required")
	}
	bytes, _ := json.Marshal(struct {
		Handler string
		Input   map[string]any
		Context protocol.Context
	}{frame.Handler, frame.Input, frame.Context})
	fingerprint := string(bytes)
	e.mu.Lock()
	if old := e.calls[frame.CallID]; old != nil {
		if old.fingerprint != fingerprint {
			e.mu.Unlock()
			return errorFrame(frame.CallID, "CALL_ID_CONFLICT", "call_id belongs to different input or identity")
		}
		done := old.done
		e.mu.Unlock()
		accept()
		<-done
		return old.response
	}
	if e.closing {
		e.mu.Unlock()
		return errorFrame(frame.CallID, "APP_UNAVAILABLE", "worker is shutting down")
	}
	inflight := 0
	for _, c := range e.calls {
		if !c.completed {
			inflight++
		}
	}
	if inflight >= 64 {
		e.mu.Unlock()
		return errorFrame(frame.CallID, "CAPACITY_EXCEEDED", "too many in-flight calls")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &call{fingerprint: fingerprint, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	e.calls[frame.CallID] = c
	e.mu.Unlock()
	accept()
	var s *session
	var maxTokens, yield int
	var actionErr *protocol.Error
	created := false
	switch frame.Handler {
	case "exec_command":
		s, yield, maxTokens, actionErr = e.exec(c, frame.Input, frame.Context)
		created = s != nil
	case "write_stdin":
		s, yield, maxTokens, actionErr = e.write(c, frame.Input, frame.Context)
	default:
		actionErr = protocol.Err("INVALID_ARGUMENT", "unknown Action handler")
	}
	if actionErr == nil && s != nil {
		timer := time.NewTimer(time.Duration(yield) * time.Millisecond)
		select {
		case <-s.done:
		case <-timer.C:
		case <-c.ctx.Done():
		}
		timer.Stop()
	}
	e.mu.Lock()
	if c.ctx.Err() != nil {
		if created && s != nil {
			e.terminateLocked(s, "cancelled")
		}
		if actionErr == nil {
			actionErr = protocol.Err("CALL_CANCELLED", "current Action was cancelled; completed side effects are not rolled back")
			if frame.Handler == "write_stdin" {
				chars, _ := stringArg(frame.Input, "chars", "", false)
				actionErr.Details = map[string]any{"input_bytes_written": len(chars)}
			}
		}
	}
	var response protocol.Frame
	if actionErr != nil {
		response = protocol.Frame{Type: "error", CallID: frame.CallID, Error: actionErr}
	} else {
		output, truncated, omitted := s.output.take(maxTokens * 4)
		result := Result{Output: output, OutputTruncated: truncated, OmittedBytes: omitted, OutputIncomplete: s.incomplete}
		if s.finished {
			code := s.exit
			result.ExitCode = &code
			result.TerminationReason = s.reason
			result.Signal = s.signal
			s.closed = true
			delete(e.sessions, s.id)
		} else {
			result.SessionID = s.id
		}
		response = protocol.ResultFrame(frame.CallID, result)
	}
	if s != nil {
		s.busy = false
	}
	// Cached response and output consumption commit in the same critical section.
	c.response = response
	c.completed = true
	c.finished = time.Now()
	close(c.done)
	c.cancel()
	e.pruneCallsLocked()
	e.mu.Unlock()
	return response
}

func (e *Engine) Cancel(id string) {
	e.mu.Lock()
	if c := e.calls[id]; c != nil && !c.completed {
		c.cancel()
	}
	e.mu.Unlock()
}
func (e *Engine) exec(c *call, input map[string]any, owner protocol.Context) (*session, int, int, *protocol.Error) {
	if err := unknown(input, "cmd", "workdir", "tty", "yield_time_ms", "max_output_tokens"); err != nil {
		return nil, 0, 0, err
	}
	script, er := stringArg(input, "cmd", "", true)
	if er != nil {
		return nil, 0, 0, er
	}
	if strings.TrimSpace(script) == "" || len(script) > e.config.Limits.MaxCmdBytes {
		return nil, 0, 0, protocol.Err("INVALID_ARGUMENT", "cmd must be nonblank and fit max_cmd_bytes")
	}
	cwd, er := stringArg(input, "workdir", e.config.DefaultCwd, false)
	if er != nil {
		return nil, 0, 0, er
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(e.config.DefaultCwd, cwd)
	}
	st, err := os.Stat(cwd)
	if err != nil || !st.IsDir() {
		return nil, 0, 0, protocol.Err("WORKDIR_UNAVAILABLE", "workdir is not an existing directory")
	}
	tty, er := boolArg(input, "tty", false)
	if er != nil {
		return nil, 0, 0, er
	}
	yield, tokens, er := e.waitArgs(input, 10000)
	if er != nil {
		return nil, 0, 0, er
	}
	e.mu.Lock()
	if c.ctx.Err() != nil || e.closing {
		e.mu.Unlock()
		return nil, 0, 0, protocol.Err("CALL_CANCELLED", "execution was cancelled before startup")
	}
	if e.active >= e.config.Limits.MaxConcurrentSessions {
		e.mu.Unlock()
		return nil, 0, 0, protocol.Err("CAPACITY_EXCEEDED", "maximum concurrent executions reached")
	}
	e.active++
	e.workers.Add(1)
	e.mu.Unlock()
	proc, err := launch(e.config, script, cwd, tty)
	if err != nil {
		e.mu.Lock()
		e.active--
		e.mu.Unlock()
		e.workers.Done()
		return nil, 0, 0, protocol.Err("START_FAILED", err.Error())
	}
	now := time.Now()
	s := &session{id: protocol.ID("s_"), owner: owner, proc: proc, tty: tty, output: outputBuffer{limit: e.config.Limits.SessionBufferBytes}, done: make(chan struct{}), started: now, lastActive: now, busy: true, reason: "exited"}
	e.mu.Lock()
	e.sessions[s.id] = s
	if e.closing || c.ctx.Err() != nil {
		e.terminateLocked(s, "cancelled")
	}
	e.mu.Unlock()
	go e.collect(s)
	return s, yield, tokens, nil
}

func (e *Engine) write(c *call, input map[string]any, owner protocol.Context) (*session, int, int, *protocol.Error) {
	if er := unknown(input, "session_id", "chars", "yield_time_ms", "max_output_tokens"); er != nil {
		return nil, 0, 0, er
	}
	id, er := stringArg(input, "session_id", "", true)
	if er != nil {
		return nil, 0, 0, er
	}
	chars, er := stringArg(input, "chars", "", false)
	if er != nil {
		return nil, 0, 0, er
	}
	if len(chars) > e.config.Limits.MaxInputBytes {
		return nil, 0, 0, protocol.Err("INVALID_ARGUMENT", "chars exceeds max_input_bytes")
	}
	def := 5000
	if chars != "" {
		def = 250
	}
	yield, tokens, er := e.waitArgs(input, def)
	if er != nil {
		return nil, 0, 0, er
	}
	e.mu.Lock()
	s := e.sessions[id]
	if s == nil || s.closed || s.owner != owner {
		e.mu.Unlock()
		return nil, 0, 0, protocol.Err("SESSION_UNAVAILABLE", "session is unavailable to this identity and implementation")
	}
	if s.busy {
		e.mu.Unlock()
		return nil, 0, 0, protocol.Err("SESSION_BUSY", "another call is operating on this session")
	}
	if chars != "" && (!s.tty || s.finishing || s.finished) {
		e.mu.Unlock()
		return nil, 0, 0, protocol.Err("STDIN_NOT_WRITABLE", "stdin is closed or this execution uses pipe mode")
	}
	if c.ctx.Err() != nil {
		e.mu.Unlock()
		return nil, 0, 0, protocol.Err("CALL_CANCELLED", "call was cancelled before writing")
	}
	s.busy = true
	s.lastActive = time.Now()
	e.mu.Unlock()
	if chars != "" {
		ctx, cancel := context.WithTimeout(c.ctx, time.Second)
		n, err := s.proc.write(ctx, []byte(chars))
		cancel()
		if err != nil {
			code := "INPUT_WRITE_FAILED"
			if c.ctx.Err() != nil {
				code = "CALL_CANCELLED"
			}
			return s, 0, 0, &protocol.Error{Code: code, Message: "input may have been partially delivered: " + err.Error(), Details: map[string]any{"input_bytes_written": n}}
		}
	}
	return s, yield, tokens, nil
}

func (e *Engine) collect(s *session) {
	defer e.workers.Done()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32768)
		for {
			n, err := s.proc.output.Read(buf)
			e.mu.Lock()
			if n > 0 {
				s.output.append(buf[:n], false)
			}
			if err != nil {
				s.output.append(nil, true)
			}
			e.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	code, sig, waitErr := s.proc.wait()
	e.mu.Lock()
	s.finishing = true
	e.mu.Unlock()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		e.mu.Lock()
		s.incomplete = true
		e.mu.Unlock()
		s.proc.closeOutput()
		<-readDone
	}
	s.proc.closeOutput()
	e.mu.Lock()
	defer e.mu.Unlock()
	if code < 0 {
		s.incomplete = true // Keep an honest unknown state; shutdown still reclaims resources.
		s.reason = "result_unknown"
		if waitErr != nil {
			s.output.append([]byte("\n[process status unavailable]\n"), true)
		}
		return
	}
	s.exit = code
	s.signal = sig
	if sig != 0 && s.reason == "exited" {
		s.reason = "signal"
	}
	s.finished = true
	s.finishedAt = time.Now()
	e.active--
	close(s.done)
	e.pruneSessionsLocked()
}
func (e *Engine) terminateLocked(s *session, reason string) {
	if s.finished || s.finishing {
		return
	}
	s.finishing = true
	s.reason = reason
	go func() { _ = s.proc.terminate() }()
}
func (e *Engine) monitor() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.monitorDone:
			return
		case now := <-ticker.C:
			e.mu.Lock()
			for _, s := range e.sessions {
				if s.finished {
					continue
				}
				if now.Sub(s.started) >= time.Duration(e.config.Limits.MaxRuntimeMS)*time.Millisecond {
					e.terminateLocked(s, "runtime_limit")
				} else if !s.busy && now.Sub(s.lastActive) >= time.Duration(e.config.Limits.IdleTimeoutMS)*time.Millisecond {
					e.terminateLocked(s, "idle_limit")
				}
			}
			e.pruneSessionsLocked()
			e.pruneCallsLocked()
			e.mu.Unlock()
		}
	}
}
func (e *Engine) pruneSessionsLocked() {
	now := time.Now()
	var finished []*session
	for id, s := range e.sessions {
		if !s.finished || s.busy {
			continue
		}
		if now.Sub(s.finishedAt) >= time.Duration(e.config.Limits.FinishedSessionTTLMS)*time.Millisecond {
			delete(e.sessions, id)
			continue
		}
		finished = append(finished, s)
	}
	for len(finished) > e.config.Limits.MaxFinishedSessions {
		old := 0
		for i, s := range finished {
			if s.finishedAt.Before(finished[old].finishedAt) {
				old = i
			}
		}
		delete(e.sessions, finished[old].id)
		finished = append(finished[:old], finished[old+1:]...)
	}
}
func (e *Engine) pruneCallsLocked() {
	now := time.Now()
	for id, c := range e.calls {
		if c.completed && now.Sub(c.finished) > 10*time.Minute {
			delete(e.calls, id)
		}
	}
	for {
		n := 0
		oldID := ""
		var oldest time.Time
		for id, c := range e.calls {
			if !c.completed {
				continue
			}
			n++
			if oldID == "" || c.finished.Before(oldest) {
				oldID = id
				oldest = c.finished
			}
		}
		if n <= 128 {
			return
		}
		delete(e.calls, oldID)
	}
}
func (e *Engine) Close() {
	e.mu.Lock()
	if e.closing {
		e.mu.Unlock()
		e.workers.Wait()
		return
	}
	e.closing = true
	close(e.monitorDone)
	for _, c := range e.calls {
		if !c.completed {
			c.cancel()
		}
	}
	for _, s := range e.sessions {
		e.terminateLocked(s, "shutdown")
	}
	e.mu.Unlock()
	e.workers.Wait()
}

func (e *Engine) waitArgs(m map[string]any, def int) (int, int, *protocol.Error) {
	yield, er := intArg(m, "yield_time_ms", def)
	if er != nil {
		return 0, 0, er
	}
	if yield < 0 || yield > 30000 {
		return 0, 0, protocol.Err("INVALID_ARGUMENT", "yield_time_ms must be between 0 and 30000")
	}
	tokens, er := intArg(m, "max_output_tokens", 10000)
	if er != nil {
		return 0, 0, er
	}
	if tokens <= 0 {
		return 0, 0, protocol.Err("INVALID_ARGUMENT", "max_output_tokens must be positive")
	}
	if tokens > e.config.Limits.MaxOutputTokens {
		tokens = e.config.Limits.MaxOutputTokens
	}
	return yield, tokens, nil
}
func unknown(m map[string]any, keys ...string) *protocol.Error {
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return protocol.Err("INVALID_ARGUMENT", "unsupported argument: "+k)
		}
	}
	return nil
}
func stringArg(m map[string]any, k, def string, required bool) (string, *protocol.Error) {
	v, ok := m[k]
	if !ok {
		if required {
			return "", protocol.Err("INVALID_ARGUMENT", k+" is required")
		}
		return def, nil
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	if i, e := integer(v); e == nil {
		return strconv.Itoa(i), nil
	}
	return "", protocol.Err("INVALID_ARGUMENT", k+" must be a string")
}
func intArg(m map[string]any, k string, def int) (int, *protocol.Error) {
	v, ok := m[k]
	if !ok {
		return def, nil
	}
	i, e := integer(v)
	if e != nil {
		return 0, protocol.Err("INVALID_ARGUMENT", k+" must be an integer")
	}
	return i, nil
}
func integer(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		if int64(int(n)) == n {
			return int(n), nil
		}
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) && math.Trunc(n) == n && n >= float64(math.MinInt) && n < float64(math.MaxInt) {
			return int(n), nil
		}
	case json.Number:
		i, e := strconv.ParseInt(string(n), 10, 64)
		if e == nil {
			return int(i), nil
		}
		f, e := n.Float64()
		if e == nil {
			return integer(f)
		}
	case string:
		i, e := strconv.Atoi(strings.TrimSpace(n))
		if e == nil {
			return i, nil
		}
	}
	return 0, errors.New("not an integer")
}
func boolArg(m map[string]any, k string, def bool) (bool, *protocol.Error) {
	v, ok := m[k]
	if !ok {
		return def, nil
	}
	if b, ok := v.(bool); ok {
		return b, nil
	}
	if s, ok := v.(string); ok {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, protocol.Err("INVALID_ARGUMENT", k+" must be a boolean")
}
