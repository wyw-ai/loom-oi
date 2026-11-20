package oic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type AppStatus struct {
	App          string                `json:"app"`
	Path         string                `json:"path"`
	Ready        bool                  `json:"ready"`
	Registered   bool                  `json:"registered"`
	Registration string                `json:"registration"`
	Error        string                `json:"error,omitempty"`
	Actions      []protocol.ActionInfo `json:"actions"`
	RuntimeCount int                   `json:"runtime_count"`
}

type Status struct {
	Running   bool   `json:"running"`
	Connected bool   `json:"connected"`
	HostID    string `json:"host_id"`
	ChannelID string `json:"channel_id"`
	AppCount  int    `json:"app_count"`
	LastError string `json:"last_error,omitempty"`
}

type loadedApp struct {
	spec       *appSpec
	workers    map[string]*worker
	registered bool
	loadError  string
}
type loadRecord struct {
	App  string `json:"app"`
	Path string `json:"path"`
}
type persistedLoads struct {
	Version int          `json:"version"`
	Apps    []loadRecord `json:"apps"`
}
type callRecord struct {
	fingerprint string
	app         *loadedApp
	worker      *worker
	done        chan struct{}
	result      protocol.Frame
	finished    time.Time
	cancel      context.CancelFunc
}

type Host struct {
	config          Config
	configPath      string
	mu              sync.RWMutex
	lifecycle       sync.Mutex
	apps            map[string]*loadedApp
	records         map[string]loadRecord
	calls           map[string]*callRecord
	ctx             context.Context
	cancel          context.CancelFunc
	started, closed bool
	peer            *connection
	lastError       string
}

func New(configPath string) (*Host, error) {
	c, abs, err := readConfig(configPath)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(c.StateDir, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &Host{config: c, configPath: abs, apps: map[string]*loadedApp{}, records: map[string]loadRecord{}, calls: map[string]*callRecord{}, ctx: ctx, cancel: cancel}
	b, err := os.ReadFile(filepath.Join(c.StateDir, "loaded.json"))
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		cancel()
		return nil, err
	}
	var persisted persistedLoads
	if err = json.Unmarshal(b, &persisted); err != nil {
		cancel()
		return nil, fmt.Errorf("read loaded records: %w", err)
	}
	if persisted.Version != 1 {
		cancel()
		return nil, fmt.Errorf("unsupported loaded record version")
	}
	for _, record := range persisted.Apps {
		if record.App == "" || record.Path == "" {
			cancel()
			return nil, fmt.Errorf("invalid loaded record")
		}
		h.records[record.App] = record
	}
	return h, nil
}

func (h *Host) Config() Config   { return h.config }
func (h *Host) StateDir() string { return h.config.StateDir }

func (h *Host) Status() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return Status{Running: h.started && !h.closed, Connected: h.peer != nil, HostID: h.config.Host.ID, ChannelID: h.config.Channel.ID, AppCount: len(h.apps), LastError: h.lastError}
}

func (h *Host) Apps() []AppStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]AppStatus, 0, len(h.apps))
	for _, app := range h.apps {
		out = append(out, statusOf(app))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return out
}

func statusOf(a *loadedApp) AppStatus {
	s := AppStatus{App: a.spec.definition.App.Code, Path: a.spec.path, Ready: a.loadError == "", Registered: a.registered, Registration: "pending", Error: a.loadError, RuntimeCount: len(a.workers), Actions: []protocol.ActionInfo{}}
	if s.Registered {
		s.Registration = "registered"
	}
	for name, action := range a.spec.definition.Actions {
		w := a.workers[a.spec.actionRuntime[name]]
		ready := w != nil && w.alive.Load()
		if !ready {
			s.Ready = false
			if s.Error == "" {
				s.Error = "worker is unavailable"
			}
		}
		s.Actions = append(s.Actions, protocol.ActionInfo{Name: name, Description: action.Description, InputSchema: action.InputSchema, Ready: ready, ResultFormat: action.ResultFormat})
	}
	sort.Slice(s.Actions, func(i, j int) bool { return s.Actions[i].Name < s.Actions[j].Name })
	return s
}

func (h *Host) Load(path string) (AppStatus, error) {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	spec, err := readApp(path, h.config.AppConfig)
	if err != nil {
		return AppStatus{}, err
	}
	h.mu.RLock()
	existing := h.apps[spec.definition.App.Code]
	closed := h.closed
	h.mu.RUnlock()
	if closed {
		return AppStatus{}, protocol.Err("HOST_STOPPED", "OIC is stopping")
	}
	if existing != nil && existing.loadError == "" {
		if existing.spec.hash != spec.hash {
			return AppStatus{}, protocol.Err("APP_ALREADY_LOADED", "definition changed; unload the App before loading it again")
		}
		h.mu.RLock()
		status := statusOf(existing)
		h.mu.RUnlock()
		return status, nil
	}
	a := &loadedApp{spec: spec, workers: map[string]*worker{}}
	keys := make([]string, 0, len(spec.runtimes))
	for key := range spec.runtimes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rollback := func() {
		for _, w := range a.workers {
			_ = w.close(context.Background())
		}
	}
	for _, key := range keys {
		w, err := startWorker(h.ctx, spec.runtimes[key], spec.dir, h.config.AppConfig[spec.definition.App.Code], filepath.Join(h.config.StateDir, "logs"), spec.definition.App.Code, h.config.OIS.CredentialEnv, h.workerExited)
		if err != nil {
			rollback()
			return AppStatus{}, fmt.Errorf("initialize %s: %w", key, err)
		}
		a.workers[key] = w
		for name, action := range spec.definition.Actions {
			if spec.actionRuntime[name] == key && !w.handlers[action.Handler] {
				rollback()
				return AppStatus{}, fmt.Errorf("worker %s does not expose handler %s", key, action.Handler)
			}
		}
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		rollback()
		return AppStatus{}, protocol.Err("HOST_STOPPED", "OIC is stopping")
	}
	oldRecord, hadRecord := h.records[spec.definition.App.Code]
	if !statusOf(a).Ready {
		h.mu.Unlock()
		rollback()
		return AppStatus{}, fmt.Errorf("App runtime exited during initialization")
	}
	// A complete registration must fit even when a later connection supports
	// fewer formats (JSON false is one byte longer than true). Check before
	// publishing or persisting the new App, including during offline loads.
	h.apps[spec.definition.App.Code] = a
	maxRegistration := h.registrationLocked([]string{})
	for i := range maxRegistration.Apps {
		maxRegistration.Apps[i].Ready = false
	}
	_, frameErr := protocol.MarshalFrame(maxRegistration, false)
	if existing == nil {
		delete(h.apps, spec.definition.App.Code)
	} else {
		h.apps[spec.definition.App.Code] = existing
	}
	if frameErr != nil {
		h.mu.Unlock()
		rollback()
		return AppStatus{}, fmt.Errorf("App registration: %w", frameErr)
	}
	h.records[spec.definition.App.Code] = loadRecord{App: spec.definition.App.Code, Path: spec.path}
	if err = h.persistLocked(); err != nil {
		if hadRecord {
			h.records[spec.definition.App.Code] = oldRecord
		} else {
			delete(h.records, spec.definition.App.Code)
		}
		h.mu.Unlock()
		rollback()
		return AppStatus{}, err
	}
	h.apps[spec.definition.App.Code] = a
	h.mu.Unlock()
	h.publishRegistration()
	h.mu.RLock()
	status := statusOf(a)
	h.mu.RUnlock()
	return status, nil
}

func (h *Host) Unload(code string) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	h.mu.Lock()
	a := h.apps[code]
	record, hasRecord := h.records[code]
	if a == nil && !hasRecord {
		h.mu.Unlock()
		return protocol.Err("NOT_FOUND", "App is not loaded")
	}
	delete(h.records, code)
	if err := h.persistLocked(); err != nil {
		if hasRecord {
			h.records[code] = record
		}
		h.mu.Unlock()
		return err
	}
	delete(h.apps, code)
	for id, r := range h.calls {
		if r.app == a {
			r.cancel()
			delete(h.calls, id)
		}
	}
	h.mu.Unlock()
	h.publishRegistration()
	var result error
	if a != nil {
		for _, w := range a.workers {
			result = errors.Join(result, w.close(context.Background()))
		}
	}
	return result
}

func (h *Host) persistLocked() error {
	p := persistedLoads{Version: 1, Apps: []loadRecord{}}
	for _, r := range h.records {
		p.Apps = append(p.Apps, r)
	}
	sort.Slice(p.Apps, func(i, j int) bool { return p.Apps[i].App < p.Apps[j].App })
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(h.config.StateDir, ".loads-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(h.config.StateDir, "loaded.json"))
}

func (h *Host) restore() {
	h.mu.RLock()
	records := make([]loadRecord, 0, len(h.records))
	for _, r := range h.records {
		records = append(records, r)
	}
	h.mu.RUnlock()
	for _, r := range records {
		spec, err := readApp(r.Path, h.config.AppConfig)
		if err == nil && spec.definition.App.Code != r.App {
			err = fmt.Errorf("saved App %s changed its code", r.App)
		}
		if err == nil {
			_, err = h.Load(r.Path)
		}
		if err != nil {
			h.mu.Lock()
			h.lastError = fmt.Sprintf("restore %s: %v", r.App, err)
			if _, exists := h.apps[r.App]; !exists {
				fallback := &appSpec{path: r.Path, definition: AppDefinition{Actions: map[string]ActionDefinition{}}}
				fallback.definition.App.Code = r.App
				h.apps[r.App] = &loadedApp{spec: fallback, workers: map[string]*worker{}, loadError: err.Error()}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Host) Close(ctx context.Context) error {
	h.mu.Lock()
	h.closed = true
	h.cancel()
	p := h.peer
	h.peer = nil
	h.mu.Unlock()
	if p != nil {
		p.close()
	}
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	h.mu.Lock()
	apps := h.apps
	h.apps = map[string]*loadedApp{}
	for _, r := range h.calls {
		r.cancel()
	}
	h.calls = map[string]*callRecord{}
	h.mu.Unlock()
	var result error
	var wg sync.WaitGroup
	var errMu sync.Mutex
	for _, a := range apps {
		for _, w := range a.workers {
			wg.Add(1)
			go func(w *worker) {
				defer wg.Done()
				if err := w.close(ctx); err != nil {
					errMu.Lock()
					result = errors.Join(result, err)
					errMu.Unlock()
				}
			}(w)
		}
	}
	wg.Wait()
	return result
}

func (h *Host) workerExited() { h.publishRegistration() }

func (h *Host) registrationLocked(formats []string) protocol.Frame {
	name := h.config.Host.Name
	if name == "" {
		name, _ = os.Hostname()
	}
	f := protocol.Frame{Type: "register", Host: &protocol.HostInfo{ID: h.config.Host.ID, Name: name, OS: runtime.GOOS, Arch: runtime.GOARCH}, Apps: []protocol.AppInfo{}}
	for _, a := range h.apps {
		s := statusOf(a)
		for i := range s.Actions {
			if !protocol.SupportsFormat(formats, s.Actions[i].ResultFormat) {
				s.Actions[i].Ready = false
			}
		}
		f.Apps = append(f.Apps, protocol.AppInfo{App: s.App, Ready: s.Ready, Actions: s.Actions})
	}
	sort.Slice(f.Apps, func(i, j int) bool { return f.Apps[i].App < f.Apps[j].App })
	return f
}

func (h *Host) publishRegistration() {
	h.mu.RLock()
	p := h.peer
	if p == nil {
		h.mu.RUnlock()
		return
	}
	f := h.registrationLocked(p.formats)
	snapshot := map[string]*loadedApp{}
	for code, a := range h.apps {
		snapshot[code] = a
	}
	h.mu.RUnlock()
	if err := p.register(f, snapshot); err != nil {
		p.close()
	}
}

// Invoke is also useful for protocol conformance tests; normal consumers enter
// through an authenticated OIS channel rather than a direct local exec API.
func (h *Host) Invoke(ctx context.Context, f protocol.Frame) protocol.Frame {
	return h.invoke(ctx, f, nil, []string{protocol.FormatJSON, protocol.FormatContent})
}

func (h *Host) invoke(ctx context.Context, f protocol.Frame, started chan struct{}, formats []string) protocol.Frame {
	var signalOnce sync.Once
	signal := func() {
		signalOnce.Do(func() {
			if started != nil {
				close(started)
			}
		})
	}
	defer signal()
	if f.CallID == "" || f.Context.AccessIdentity == "" || f.Context.VMCPID == "" {
		return workerError(f.CallID, "INVALID_ARGUMENT", "call_id and trusted access context are required")
	}
	fingerprintBytes, err := json.Marshal(struct {
		App, Action string
		Input       map[string]any
		Context     protocol.Context
	}{f.App, f.Action, f.Input, f.Context})
	if err != nil {
		return workerError(f.CallID, "INVALID_ARGUMENT", "input is not serializable")
	}
	fingerprint := string(fingerprintBytes)
	h.mu.Lock()
	a := h.apps[f.App]
	if h.closed || a == nil || !statusOf(a).Ready {
		h.mu.Unlock()
		return workerError(f.CallID, "APP_UNAVAILABLE", "App is not ready")
	}
	old := h.calls[f.CallID]
	if old != nil && (old.fingerprint != fingerprint || old.app != a) {
		h.mu.Unlock()
		return workerError(f.CallID, "CALL_ID_CONFLICT", "call_id belongs to another request or implementation")
	}
	action, ok := a.spec.definition.Actions[f.Action]
	if !ok {
		h.mu.Unlock()
		return workerError(f.CallID, "ACTION_UNAVAILABLE", "Action is not loaded")
	}
	// Check on this peer before both new dispatch and cached response replay.
	if !protocol.SupportsFormat(formats, action.ResultFormat) {
		h.mu.Unlock()
		return workerError(f.CallID, "RESULT_FORMAT_UNSUPPORTED", "current connection cannot deliver this Action's result format")
	}
	if old != nil {
		h.mu.Unlock()
		signal()
		select {
		case <-old.done:
			return old.result
		case <-ctx.Done():
			return workerError(f.CallID, "CALL_RESULT_UNKNOWN", "caller stopped waiting")
		}
	}
	h.trimCallsLocked()
	active := 0
	for _, r := range h.calls {
		if r.finished.IsZero() {
			active++
		}
	}
	if active >= 256 {
		h.mu.Unlock()
		return workerError(f.CallID, "CAPACITY_EXCEEDED", "too many in-flight calls")
	}
	w := a.workers[a.spec.actionRuntime[f.Action]]
	callCtx, cancel := context.WithTimeout(h.ctx, 60*time.Second)
	r := &callRecord{fingerprint: fingerprint, app: a, worker: w, done: make(chan struct{}), cancel: cancel}
	h.calls[f.CallID] = r
	h.mu.Unlock()
	signal()
	stopCancel := context.AfterFunc(ctx, cancel)
	defer stopCancel()
	// Preserve the original input and first bound frame for duplicate requests.
	bound, err := BindInput(f.Input, action)
	if err != nil {
		var pe *protocol.Error
		if !errors.As(err, &pe) {
			pe = protocol.Err("INVALID_ARGUMENT", err.Error())
		}
		r.result = protocol.Frame{Type: "error", CallID: f.CallID, Error: pe}
	} else if callCtx.Err() != nil {
		r.result = workerError(f.CallID, "CALL_CANCELLED", "call cancelled before dispatch")
	} else {
		r.result = w.call(callCtx, protocol.Frame{Type: "invoke", CallID: f.CallID, Handler: action.Handler, Input: bound, Context: f.Context})
		if r.result.Type == "result" {
			if err := protocol.ValidateResult(action.ResultFormat, r.result); err != nil {
				var pe *protocol.Error
				if !errors.As(err, &pe) {
					pe = protocol.Err("EXECUTION_ERROR", "invalid worker result")
				}
				r.result = protocol.Frame{Type: "error", CallID: f.CallID, Error: pe}
			}
		}
	}
	cancel()
	h.mu.Lock()
	r.finished = time.Now()
	close(r.done)
	h.mu.Unlock()
	return r.result
}

func (h *Host) Cancel(callID string) {
	h.mu.RLock()
	r := h.calls[callID]
	h.mu.RUnlock()
	if r != nil {
		r.cancel()
	}
}

func (h *Host) trimCallsLocked() {
	var completed []string
	now := time.Now()
	for id, r := range h.calls {
		if !r.finished.IsZero() {
			if now.Sub(r.finished) > 10*time.Minute {
				delete(h.calls, id)
			} else {
				completed = append(completed, id)
			}
		}
	}
	sort.Slice(completed, func(i, j int) bool { return h.calls[completed[i]].finished.Before(h.calls[completed[j]].finished) })
	for len(completed) >= 128 {
		delete(h.calls, completed[0])
		completed = completed[1:]
	}
}
