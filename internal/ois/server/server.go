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
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/relay"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Server is a running OIS.
type Server struct {
	cfg    Config
	log    *slog.Logger
	st     *store.Serialized
	hub    *relay.Hub
	api    *http.Server
	access *http.Server
	apiLn  net.Listener
	accLn  net.Listener
	errc   chan error
}

// Start opens the store and the listeners in the B3 §6 order: open the
// store and take the single-instance lock, check the schema and read back
// the settings (store.Open), StartRun, LoadSnapshot, then listen.
func Start(ctx context.Context, cfg Config, log *slog.Logger) (*Server, error) {
	cert, pool, err := loadTLS(cfg.TLS)
	if err != nil {
		return nil, err
	}
	raw, err := store.Open(ctx, cfg.Store, limits.D(cfg.Limits.SQLiteBusyMS))
	if err != nil {
		return nil, err
	}
	st := store.NewSerialized(raw, cfg.Limits.WriteQueue)
	s := &Server{cfg: cfg, log: log, st: st, errc: make(chan error, 2)}
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
	s.hub = relay.New(relay.Config{Limits: s.cfg.Limits, Store: s.st, Snapshot: snap, Epoch: epoch, RunID: runID, Logger: s.log})
	s.hub.Start()
	if s.apiLn, err = net.Listen("tcp", s.cfg.APIListen); err != nil {
		return err
	}
	if s.accLn, err = net.Listen("tcp", s.cfg.AccessListen); err != nil {
		_ = s.apiLn.Close()
		return err
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
	s.api = &http.Server{Handler: s.apiHandler(), ReadHeaderTimeout: 10 * time.Second, ErrorLog: errLog}
	s.access = &http.Server{Handler: s.accessHandler(), ReadHeaderTimeout: 10 * time.Second, ErrorLog: errLog}
	go s.serve(s.api, tls.NewListener(s.apiLn, apiTLS))
	go s.serve(s.access, tls.NewListener(s.accLn, accTLS))
	s.log.Info("ois started", "api", s.apiLn.Addr().String(), "access", s.accLn.Addr().String(),
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

// Err reports a listener that stopped unexpectedly.
func (s *Server) Err() <-chan error { return s.errc }

// Shutdown stops accepting requests, aborts channels with Shutdown, waits
// until ctx ends for connections to finish and closes the store.
func (s *Server) Shutdown(ctx context.Context) {
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
