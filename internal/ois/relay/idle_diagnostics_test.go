package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type blockedIdleHandler struct{ entered, release chan struct{} }

func (h *blockedIdleHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *blockedIdleHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "closing" {
		close(h.entered)
		<-h.release
	}
	return nil
}
func (h *blockedIdleHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *blockedIdleHandler) WithGroup(string) slog.Handler      { return h }

func TestIdleDiagnosticKeepsExistingLogBoundary(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	f.h.mu.Lock()
	ch := f.h.channels[f.id(fl)]
	f.h.mu.Unlock()
	h := &blockedIdleHandler{entered: make(chan struct{}), release: make(chan struct{})}
	f.h.log = slog.New(h)
	f.clk.Add(2 * time.Second)
	done := make(chan struct{})
	go func() { defer close(done); f.h.sweep(f.clk.Now()) }()
	select {
	case <-h.entered:
	case <-time.After(time.Second):
		t.Fatal("diagnostic not reached")
	}
	defer func() { close(h.release); <-done }()
	fl.cons.expectAbort(protocol.CodeIdleTimeout)
	fl.prov.expectAbort(protocol.CodeIdleTimeout)
	if ch.st.Load() != stateClosing {
		t.Fatal("diagnostic moved the existing log before the closing decision")
	}
}

type idleLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *idleLog) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *idleLog) diagnostic(t *testing.T) map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var result map[string]any
	for _, line := range bytes.Split(l.b.Bytes(), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatal(err)
		}
		if m["idle_limit_ms"] != nil {
			if m["msg"] != "closing" && m["msg"] != "closed" {
				t.Fatal("diagnostic added a new log call")
			}
			if result != nil {
				t.Fatal("duplicate idle decision diagnostic")
			}
			result = m
		}
	}
	return result
}

func TestIdleDiagnosticDistinguishesFINStages(t *testing.T) {
	for _, stage := range []string{"not_received", "received_write_blocked", "forwarded"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			log := &idleLog{}
			f.h.log = slog.New(slog.NewJSONHandler(log, nil))
			fl := f.openFlow()
			fl.cons.data("stream 67108864")
			fl.prov.expectType(protocol.FrameData)
			var release chan struct{}
			if stage == "received_write_blocked" {
				release = make(chan struct{})
				fl.prov.conn.setGate(release)
			}
			if stage != "not_received" {
				fl.cons.fin()
				waitFor(t, "FIN received", func() bool {
					f.h.mu.Lock()
					defer f.h.mu.Unlock()
					return f.h.channels[f.id(fl)].fin[Consumer]
				})
				if stage == "forwarded" {
					fl.prov.expectType(protocol.FrameFin)
					waitFor(t, "FIN write completed", func() bool {
						f.h.mu.Lock()
						defer f.h.mu.Unlock()
						return f.h.channels[f.id(fl)].legs[Provider].finWritten.Load()
					})
				}
			}
			f.clk.Add(1999 * time.Millisecond)
			f.h.sweep(f.clk.Now())
			if log.diagnostic(t) != nil || f.state(f.id(fl)).State != protocol.StateOpen {
				t.Fatal("diagnostic or idle decision before 2 s")
			}
			f.clk.Add(time.Millisecond)
			f.h.sweep(f.clk.Now())
			m := log.diagnostic(t)
			if m == nil || m["channel"] != f.id(fl) || m["idle_age_ms"] != float64(2000) || m["idle_limit_ms"] != float64(2000) || m["consumer_fin_received"] != (stage != "not_received") || m["provider_fin_received"] != false {
				t.Fatalf("%+v", m)
			}
			provider := m["provider_leg"].(map[string]any)
			if provider["fin_written"] != (stage == "forwarded") {
				t.Fatalf("%+v", m)
			}
			if release != nil {
				close(release)
				fl.prov.expectType(protocol.FrameFin)
			}
			fl.cons.expectAbort(protocol.CodeIdleTimeout)
			fl.prov.expectAbort(protocol.CodeIdleTimeout)
			f.h.sweep(f.clk.Now())
			_ = log.diagnostic(t) // one decision remains one diagnostic after a later sweep
			t.Logf("stage=%s diagnostic=%+v", stage, m)
		})
	}
}
