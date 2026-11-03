package relay

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/wyw-ai/loom-oi/internal/oic/config"
	"github.com/wyw-ai/loom-oi/internal/oic/registration"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// watchConfig reloads on SIGHUP and whenever the file content changes.
func (a *Agent) watchConfig(ctx context.Context) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	defer signal.Stop(sig)
	t := time.NewTicker(a.opts.ReloadEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
		case <-t.C:
		}
		a.reload(ctx)
	}
}

// reload applies a changed exposures section: channels bound to a removed,
// disabled or re-targeted exposure end with RevisionChanged and the new
// snapshot is registered (B2 §4). An invalid file keeps the running
// configuration and is reported in diagnostics.
func (a *Agent) reload(ctx context.Context) {
	data, err := os.ReadFile(a.path)
	if err != nil {
		a.setConfigError(err)
		return
	}
	sum := sha256.Sum256(data)
	a.mu.Lock()
	if sum == a.cfgSum {
		a.mu.Unlock()
		return
	}
	a.cfgSum = sum
	next, err := config.Parse(data, filepath.Dir(a.path))
	if err == nil && !sameRuntime(a.cfg, next) {
		err = errRestart
	}
	if err != nil {
		a.cfgErr = err.Error()
		a.mu.Unlock()
		a.log.Error("configuration not reloaded", "error", err.Error())
		return
	}
	a.cfgErr = ""
	invalid := registration.Invalidated(a.cfg, next)
	a.cfg = next
	var ids []string
	for id, ch := range a.channels {
		if invalid[ch.exposureID] {
			ids = append(ids, id)
		}
	}
	s := a.sess
	snapshot := registration.Snapshot(next)
	a.mu.Unlock()
	for _, id := range ids {
		a.abort(id, protocol.CodeRevisionChanged)
	}
	if s != nil {
		_ = s.send(ctx, &protocol.Register{Exposures: snapshot})
	}
	a.log.Info("configuration reloaded", "exposures", len(snapshot), "aborted", len(ids))
}

func (a *Agent) setConfigError(err error) {
	a.mu.Lock()
	a.cfgErr = err.Error()
	a.mu.Unlock()
}

// sameRuntime reports whether everything but the exposures is unchanged.
func sameRuntime(a, b *config.Config) bool {
	return a.AccessID == b.AccessID && a.OIS == b.OIS && a.TLS == b.TLS && a.DiagListen == b.DiagListen &&
		a.LogLevel == b.LogLevel && a.Limits == b.Limits
}

// startDiag serves GET /stats on the loopback diag_listen address.
func (a *Agent) startDiag(ctx context.Context, wg *sync.WaitGroup) error {
	a.mu.Lock()
	addr := a.cfg.DiagListen
	a.mu.Unlock()
	if addr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.diagAddr = ln.Addr().String()
	a.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a.Stats())
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = srv.Serve(ln)
	}()
	go func() {
		defer wg.Done()
		<-ctx.Done()
		_ = srv.Close()
	}()
	return nil
}

// DiagAddr is the bound diagnostics address ("" when disabled).
func (a *Agent) DiagAddr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.diagAddr
}
