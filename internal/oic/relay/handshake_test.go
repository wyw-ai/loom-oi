package relay

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type handshakeBody struct {
	io.Reader
	closed  bool
	read    int
	failure bool
}

func (b *handshakeBody) Read(p []byte) (int, error) {
	if b.failure {
		return 0, errors.New("body read failed")
	}
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}
func (b *handshakeBody) Close() error { b.closed = true; return nil }
func TestControlHandshakeVersionUnsupported(t *testing.T) {
	original := errors.New("generic WebSocket handshake failed")
	secret := "private-peer-diagnostic"
	body := &handshakeBody{Reader: strings.NewReader(`{"code":"VersionUnsupported","message":"` + secret + `"}`)}
	err := controlHandshakeError(&http.Response{StatusCode: 400, Body: body}, original)
	if protocol.CodeOf(err) != protocol.CodeVersionUnsupported || strings.Contains(err.Error(), secret) || !body.closed {
		t.Fatalf("lost safe coded error: %v closed=%v", err, body.closed)
	}
}
func TestControlHandshakeOtherFailuresRemainOriginal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		raw     string
		failure bool
	}{
		{"other_status", 401, `{"code":"VersionUnsupported"}`, false},
		{"other_code", 400, `{"code":"Unauthenticated","message":"private"}`, false},
		{"malformed", 400, `{"code":`, false},
		{"missing_code", 400, `{}`, false},
		{"wrong_type", 400, `{"code":1}`, false},
		{"trailing_value", 400, `{"code":"VersionUnsupported"}{}`, false},
		{"truncated_boundary", 400, `{"code":"VersionUnsupported"}` + strings.Repeat(" ", 1024), false},
		{"read_failure", 400, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := errors.New("original")
			body := &handshakeBody{Reader: strings.NewReader(tc.raw), failure: tc.failure}
			err := controlHandshakeError(&http.Response{StatusCode: tc.status, Body: body}, original)
			if !errors.Is(err, original) || !body.closed || body.read > 1025 {
				t.Fatalf("failure changed/drained: %v closed=%v read=%d", err, body.closed, body.read)
			}
			if tc.status != 400 && body.read != 0 {
				t.Fatal("unrelated status body unnecessarily read")
			}
		})
	}
	original := errors.New("dial failed")
	if controlHandshakeError(nil, original) != original || controlHandshakeError(&http.Response{StatusCode: 400}, original) != original {
		t.Fatal("nil response/body changed failure")
	}
	body := &handshakeBody{Reader: strings.NewReader("upgrade stream")}
	if controlHandshakeError(&http.Response{StatusCode: 101, Body: body}, nil) != nil || body.closed || body.read != 0 {
		t.Fatal("successful upgrade stream touched")
	}
}
