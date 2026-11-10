package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCertificates(t *testing.T, dir string) (string, string, *x509.CertPool) {
	t.Helper()
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer fixture.Close()
	cert := fixture.TLS.Certificates[0]
	key, e := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	certPath, keyPath := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
	if e = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(fixture.Certificate())
	return certPath, keyPath, roots
}
func testConfig(t *testing.T, dir string, server map[string]any, vmcps map[string]any) string {
	t.Helper()
	server["data_file"] = filepath.Join(dir, "state.json")
	data, _ := json.Marshal(map[string]any{"version": 1, "server": server, "vmcps": vmcps})
	path := filepath.Join(dir, "ois.yaml")
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestAnonymousHTTPAndOptionalHTTPS(t *testing.T) {
	dir := t.TempDir()
	cert, key, roots := testCertificates(t, dir)
	path := testConfig(t, dir, map[string]any{"listen": "127.0.0.1:0", "public_url": "http://localhost:25100", "https": map[string]any{"listen": "localhost:0", "public_url": "https://localhost:25101", "cert_file": cert, "key_file": key}}, map[string]any{"open": map[string]any{"tools": []string{}}, "private": map[string]any{"tools": []string{}, "access": map[string]any{"require_https": true}}})
	service, e := newBackend(path)
	if e != nil {
		t.Fatal(e)
	}
	b := service.(*backend)
	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = b.Close(ctx)
		<-done
	})
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	defer client.CloseIdleConnections()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	for _, row := range []struct {
		endpoint int
		vmcp     string
		want     int
	}{{0, "open", 200}, {0, "private", 403}, {1, "open", 200}, {1, "private", 200}} {
		ep := b.listeners[row.endpoint]
		scheme := "http"
		if ep.tls {
			scheme = "https"
		}
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s://%s/mcp/%s", scheme, ep.listener.Addr(), row.vmcp), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		response, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != row.want {
			t.Fatalf("%s %s: %d", scheme, row.vmcp, response.StatusCode)
		}
	}
}
func TestDefaultListenerNeedsNoCredentialsOrCertificate(t *testing.T) {
	path := testConfig(t, t.TempDir(), map[string]any{"listen": "127.0.0.1:0"}, nil)
	service, e := newBackend(path)
	if e != nil {
		t.Fatal(e)
	}
	b := service.(*backend)
	if len(b.listeners) != 1 || b.listeners[0].tls {
		t.Fatal("default listener requires TLS")
	}
	_ = b.Close(context.Background())
}
func TestSecondListenerFailureReleasesFirst(t *testing.T) {
	dir := t.TempDir()
	reserved, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := reserved.Addr().String()
	reserved.Close()
	path := testConfig(t, dir, map[string]any{"listen": address, "https": map[string]any{"listen": "127.0.0.1:0", "cert_file": "absent.crt", "key_file": "absent.key"}}, nil)
	if _, e = newBackend(path); e == nil {
		t.Fatal("missing TLS certificate accepted")
	}
	listener, e := net.Listen("tcp", address)
	if e != nil {
		t.Fatalf("HTTP listener leaked: %v", e)
	}
	listener.Close()
}

func TestShutdownDeadlineClosesAcceptedConnection(t *testing.T) {
	path := testConfig(t, t.TempDir(), map[string]any{"listen": "127.0.0.1:0"}, nil)
	service, err := newBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	b := service.(*backend)
	active := make(chan struct{}, 1)
	b.listeners[0].http.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateActive {
			select {
			case active <- struct{}{}:
			default:
			}
		}
	}
	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()
	conn, err := net.Dial("tcp", b.listeners[0].listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = fmt.Fprint(conn, "POST /api/v1/exec HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-active:
	case <-time.After(time.Second):
		t.Fatal("request did not become active")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_ = b.Close(ctx)
	<-done
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var data [1]byte
	if _, err = conn.Read(data[:]); err == nil {
		t.Fatal("active socket survived service close")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("active socket was left open after shutdown deadline")
	}
}
