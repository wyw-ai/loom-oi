package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/relay"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// idleTimeout closes idle keep-alive HTTP connections.
const idleTimeout = 60 * time.Second

// readTimeout bounds reading a whole request, body included; hijacked
// WebSocket connections drop the deadline. Credentials are also re-checked
// where a request takes effect, so a held-back body cannot outlive a
// revocation (B2 §2).
const readTimeout = 30 * time.Second

// Server is a running OIS.
type Server struct {
	cfg     Config
	log     *slog.Logger
	st      *store.Serialized
	hub     *relay.Hub
	api     *http.Server
	access  *http.Server
	diag    *http.Server
	apiLn   net.Listener
	accLn   net.Listener
	diagLn  net.Listener
	metrics *metrics.Registry
	errc    chan error
}

// Start opens the store and the listeners in the B3 §6 order: open the
// store and take the single-instance lock, check the schema and read back
// the settings (store.Open), StartRun, LoadSnapshot, then listen.
func Start(ctx context.Context, cfg Config, log *slog.Logger) (*Server, error) {
	if err := cfg.validateDiagnostics(); err != nil {
		return nil, err
	}
	if cfg.ObservationLimits == (limits.Observation{}) {
		cfg.ObservationLimits = limits.ObservationDefaults(cfg.Limits)
	}
	if err := cfg.ObservationLimits.Validate(cfg.Limits); err != nil {
		return nil, err
	}
	cert, pool, err := loadTLS(cfg.TLS)
	if err != nil {
		return nil, err
	}
	raw, err := store.Open(ctx, cfg.Store, limits.D(cfg.Limits.SQLiteBusyMS))
	if err != nil {
		return nil, err
	}
	st := store.NewSerialized(raw, cfg.Limits.WriteQueue)
	s := &Server{cfg: cfg, log: log, st: st, errc: make(chan error, 3), metrics: &metrics.Registry{}}
	if err := s.start(ctx, cert, pool); err != nil {
		if s.hub != nil {
			s.hub.Shutdown(ctx)
		}
		_ = st.Close()
		return nil, err
	}
	return s, nil
}

func (s *Server) start(ctx context.Context, cert tls.Certificate, pool *x509.CertPool) error {
	runID := protocol.NewID("run_")
	epoch, err := s.st.StartRun(ctx, runID, time.Now().UTC())
	if err != nil {
		return err
	}
	snap, err := s.st.LoadSnapshot(ctx)
	if err != nil {
		return err
	}
	obs := s.cfg.ObservationLimits
	if obs == (limits.Observation{}) {
		obs = limits.ObservationDefaults(s.cfg.Limits)
	}
	s.hub = relay.New(relay.Config{Limits: s.cfg.Limits, ObservationLimits: &obs, Metrics: s.metrics, Store: s.st, Snapshot: snap, Epoch: epoch, RunID: runID, Logger: s.log})
	s.hub.Start()
	if s.apiLn, err = net.Listen("tcp", s.cfg.APIListen); err != nil {
		return err
	}
	if s.accLn, err = net.Listen("tcp", s.cfg.AccessListen); err != nil {
		_ = s.apiLn.Close()
		return err
	}
	if s.cfg.DiagListen != "" {
		if s.diagLn, err = net.Listen("tcp", s.cfg.DiagListen); err != nil {
			_ = s.apiLn.Close()
			_ = s.accLn.Close()
			return err
		}
	}
	apiTLS := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}}
	accTLS := apiTLS.Clone()
	accTLS.ClientAuth = tls.RequireAndVerifyClientCert
	accTLS.ClientCAs = pool
	accTLS.VerifyConnection = func(cs tls.ConnectionState) error {
		if _, ok := s.hub.AccessForPin(identity.SPKIPin(cs.PeerCertificates[0])); !ok {
			return errors.New("client certificate is not pinned to an enabled access")
		}
		return nil
	}
	errLog := slog.NewLogLogger(s.log.Handler(), slog.LevelWarn)
	// Idle keep-alive connections are dropped so abandoned clients cannot
	// accumulate connections (WebSockets are hijacked and unaffected).
	s.api = &http.Server{Handler: s.apiHandler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: readTimeout, IdleTimeout: idleTimeout, ErrorLog: errLog}
	s.access = &http.Server{Handler: s.accessHandler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: readTimeout, IdleTimeout: idleTimeout, ErrorLog: errLog}
	go s.serve(s.api, tls.NewListener(s.apiLn, apiTLS))
	go s.serve(s.access, tls.NewListener(s.accLn, accTLS))
	if s.diagLn != nil {
		mux := http.NewServeMux()
		if s.cfg.MetricsEnabled {
			mux.Handle("/metrics", metrics.NewHandler(s.metrics))
		}
		s.diag = &http.Server{Handler: mux, ReadHeaderTimeout: metrics.WriteBudget, ReadTimeout: metrics.WriteBudget, WriteTimeout: metrics.WriteBudget, IdleTimeout: time.Second, MaxHeaderBytes: 8 << 10, ErrorLog: errLog}
		go s.serve(s.diag, s.diagLn)
	}
	s.log.Info("ois started", "api", s.apiLn.Addr().String(), "access", s.accLn.Addr().String(),
		"diag", s.DiagAddr(),
		"backend", s.st.Backend(), "engine", s.st.EngineVersion(), "store", redact(s.cfg.Store),
		"store_id", snap.Meta.StoreID, "epoch", epoch, "run_id", runID)
	return nil
}

func (s *Server) serve(srv *http.Server, ln net.Listener) {
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		s.errc <- err
	}
}

// APIAddr is the bound api listener address.
func (s *Server) APIAddr() string { return s.apiLn.Addr().String() }

// AccessAddr is the bound access listener address.
func (s *Server) AccessAddr() string { return s.accLn.Addr().String() }

// DiagAddr is empty unless an explicit loopback diagnostic listener was started.
func (s *Server) DiagAddr() string {
	if s.diagLn == nil {
		return ""
	}
	return s.diagLn.Addr().String()
}

// Err reports a listener that stopped unexpectedly.
func (s *Server) Err() <-chan error { return s.errc }

// Shutdown stops accepting requests, aborts channels with Shutdown, waits
// until ctx ends for connections to finish and closes the store.
func (s *Server) Shutdown(ctx context.Context) {
	if s.diag != nil {
		if s.diag.Shutdown(ctx) != nil {
			_ = s.diag.Close()
		}
	}
	_ = s.api.Shutdown(ctx)
	_ = s.access.Shutdown(ctx)
	s.hub.Shutdown(ctx)
	_ = s.st.Close()
	s.log.Info("ois stopped")
}

// Serve runs OIS until ctx ends or a listener fails.
func Serve(ctx context.Context, cfg Config, log *slog.Logger) error {
	s, err := Start(ctx, cfg, log)
	if err != nil {
		return err
	}
	return s.run(ctx)
}

// run waits for ctx or a listener failure, then shuts down within T_close.
func (s *Server) run(ctx context.Context) error {
	var err error
	select {
	case <-ctx.Done():
	case err = <-s.errc:
	}
	sctx, cancel := context.WithTimeout(context.Background(), limits.D(s.cfg.Limits.CloseMS)+time.Second)
	defer cancel()
	s.Shutdown(sctx)
	return err
}
