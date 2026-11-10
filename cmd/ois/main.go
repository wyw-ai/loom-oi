package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois"
	"github.com/wyw-ai/loom-oi/internal/servicecmd"
)

type backend struct {
	service   *ois.Server
	listeners []endpoint
}
type endpoint struct {
	http     *http.Server
	listener net.Listener
	tls      bool
}

func newBackend(path string) (servicecmd.Backend, error) {
	s, err := ois.New(path)
	if err != nil {
		return nil, err
	}
	c := s.Config().Server
	b := &backend{service: s}
	writeTimeout := time.Duration(c.RequestTimeoutMS)*time.Millisecond + 25*time.Second
	if writeTimeout < 90*time.Second {
		writeTimeout = 90 * time.Second
	}
	add := func(address, certFile, keyFile string) error {
		h := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: writeTimeout, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 << 10}
		secure := certFile != ""
		if secure {
			cert, e := tls.LoadX509KeyPair(certFile, keyFile)
			if e != nil {
				return e
			}
			h.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
		}
		listener, e := net.Listen("tcp", address)
		if e != nil {
			return e
		}
		b.listeners = append(b.listeners, endpoint{h, listener, secure})
		return nil
	}
	err = add(c.Listen, c.TLSCert, c.TLSKey)
	if err == nil && c.HTTPS != nil {
		err = add(c.HTTPS.Listen, c.HTTPS.CertFile, c.HTTPS.KeyFile)
	}
	if err != nil {
		for _, ep := range b.listeners {
			_ = ep.listener.Close()
		}
		_ = s.Close()
		return nil, err
	}
	return b, nil
}
func (b *backend) Run(ctx context.Context) error {
	results := make(chan error, len(b.listeners))
	for _, ep := range b.listeners {
		go func(ep endpoint) {
			if ep.tls {
				results <- ep.http.ServeTLS(ep.listener, "", "")
			} else {
				results <- ep.http.Serve(ep.listener)
			}
		}(ep)
	}
	select {
	case err := <-results:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *backend) Close(ctx context.Context) error {
	err := b.service.Close()
	for _, ep := range b.listeners {
		shutdownErr := ep.http.Shutdown(ctx)
		if shutdownErr != nil {
			_ = ep.http.Close()
		}
		_ = ep.listener.Close()
		if err == nil {
			err = shutdownErr
		}
	}
	return err
}
func (b *backend) Status() any { return b.service.Status() }
func (b *backend) Apps() (any, error) {
	return nil, errors.New("use the OIS management API to inspect hosts and actions")
}
func (b *backend) Load(string) (any, error) { return nil, errors.New("only OIC loads applications") }
func (b *backend) Unload(string) error      { return errors.New("only OIC unloads applications") }
func main()                                 { os.Exit(servicecmd.Main("ois", os.Args[1:], newBackend)) }
