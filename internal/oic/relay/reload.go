package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/observation"
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
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()
	// Compile even when the JSON bytes are unchanged: an administrator may
	// replace a probe CA at the same path. No file I/O runs under Agent.mu.
	next, data, err := config.Load(a.path)
	if err != nil {
		a.setConfigError(err)
		return
	}
	sum := next.ContentDigest(data)
	a.mu.Lock()
	if sum == a.cfgSum {
		a.cfgErr = ""
		a.mu.Unlock()
		return
	}
	if !sameRuntime(a.cfg, next) {
		err = errRestart
	}
	if err != nil {
		a.cfgErr = err.Error()
		a.mu.Unlock()
		a.log.Error("configuration not reloaded", "error", err.Error())
		return
	}
	a.cfgSum = sum
	a.cfgErr = ""
	invalid := registration.Invalidated(a.cfg, next)
	a.cfg = next
	s := a.sess
	if s != nil {
		prior := a.bindings
		a.bindings = make(map[string]observation.Binding)
		for _, e := range next.Exposures {
			if !e.IsEnabled() {
				continue
			}
			b, effect, setErr := a.observations.Set(e.ID, e.Revision, e.ProbePlan(), time.Now())
			a.effectMetrics(effect)
			if setErr != nil {
				err = setErr
				break
			}
			a.bindings[e.ID] = b
			if prior[e.ID] != b {
				delete(a.accepted, e.ID)
			}
		}
		for id := range prior {
			if _, ok := a.bindings[id]; !ok {
				effect, _ := a.observations.Remove(id)
				a.effectMetrics(effect)
				delete(a.accepted, id)
			}
		}
		a.scheduleExpiryLocked(s)
	}
	var cancelled []*channel
	for _, ch := range a.channels {
		if invalid[ch.exposureID] {
			a.markCancelledLocked(ch, protocol.CodeRevisionChanged)
			cancelled = append(cancelled, ch)
		} else if !ch.running && s != nil && a.bindings[ch.exposureID] != ch.binding {
			a.markCancelledLocked(ch, protocol.CodeReadinessCheckFailed)
			cancelled = append(cancelled, ch)
		}
	}
	a.mu.Unlock()
	for _, ch := range cancelled {
		a.closeCancelled(ch)
		if ch.aborted == protocol.CodeReadinessCheckFailed && s != nil {
			_ = s.send(ctx, &protocol.PrepareFailed{ChannelID: ch.id, Code: ch.aborted, Reason: "observation_changed"})
		}
	}
	if err != nil && s != nil {
		s.cancel()
		_ = s.c.CloseNow()
		return
	}
	n := len(registration.Snapshot(next))
	if s != nil {
		n, _ = a.sendRegister(ctx, s)
		if err := a.reconcileProbes(s); err != nil {
			s.cancel()
			_ = s.c.CloseNow()
		}
	}
	a.log.Info("configuration reloaded", "exposures", n, "aborted", len(cancelled))
}

func (a *Agent) setConfigError(err error) {
	a.mu.Lock()
	a.cfgErr = err.Error()
	a.mu.Unlock()
}

// sameRuntime reports whether everything but the exposures is unchanged.
func sameRuntime(a, b *config.Config) bool {
	return a.AccessID == b.AccessID && a.OIS == b.OIS && a.TLS == b.TLS && a.DiagListen == b.DiagListen &&
		a.LogLevel == b.LogLevel && a.Limits == b.Limits && a.ObservationLimits == b.ObservationLimits && a.MetricsEnabled == b.MetricsEnabled
}

// startDiag serves GET /stats on the loopback diag_listen address.
func (a *Agent) startDiag(ctx context.Context, wg *sync.WaitGroup) error {
	a.mu.Lock()
	addr := a.cfg.DiagListen
	export := a.cfg.MetricsEnabled
	a.mu.Unlock()
	if addr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if addr, ok := ln.Addr().(*net.TCPAddr); !ok || !addr.IP.IsLoopback() {
		_ = ln.Close()
		return fmt.Errorf("diagnostics bound outside loopback")
	}
	a.mu.Lock()
	a.diagAddr = ln.Addr().String()
	a.mu.Unlock()
	a.log.Info("diagnostics listening", "addr", ln.Addr().String())
	mux := http.NewServeMux()
	if export {
		mux.Handle("/metrics", metrics.NewHandler(&a.metrics))
	}
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
