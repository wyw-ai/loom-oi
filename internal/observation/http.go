package observation

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"sync/atomic"
	"time"
)

// Execute performs only the fixed status probe. The authorized dial closure is
// mandatory; this package never silently substitutes environment DNS/proxies.
// started is the actual Begin time, before DNS, not the scheduler's due time.
func Execute(ctx context.Context, plan *Plan, dial DialFunc, started time.Time) Outcome {
	return execute(ctx, plan, dial, started, time.Now)
}

func execute(ctx context.Context, plan *Plan, dial DialFunc, started time.Time, now func() time.Time) Outcome {
	finish := func(readiness Readiness, reason Reason) Outcome {
		return Outcome{Readiness: readiness, Reason: reason, Duration: max(0, now().Sub(started))}
	}
	if plan == nil {
		return finish(Unknown, Unsupported)
	}
	if dial == nil {
		return finish(Unknown, PermissionError)
	}
	deadline := started.Add(plan.timeout)
	if !now().Before(deadline) || ctx.Err() != nil {
		return finish(Unknown, Timeout)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var connected, tlsFailed atomic.Bool
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn:          func(httptrace.GotConnInfo) { connected.Store(true) },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) { tlsFailed.Store(err != nil) },
	})
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		Proxy: nil, Protocols: protocols, DisableKeepAlives: true, DisableCompression: true,
		ReadBufferSize: ReadBufferLimit, MaxResponseHeaderBytes: HeaderLimit,
		TLSClientConfig: &tls.Config{RootCAs: plan.roots, ServerName: plan.host, MinVersion: tls.VersionTLS12,
			NextProtos: []string{"http/1.1"}},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != plan.address {
				return nil, ErrAddress
			}
			return dial(ctx, network, address)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, plan.url, nil)
	if err != nil {
		return finish(Unknown, Malformed)
	}
	// DisableKeepAlives alone still permits Go's early-body-close drain path.
	// Request.Close prevents that readLoop path, even if the peer ignores the
	// Connection header and advertises a small or unknown response body.
	request.Close = true
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if ctx.Err() != nil || !now().Before(deadline) {
		return finish(Unknown, Timeout)
	}
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			return finish(Unknown, Timeout)
		case errors.Is(err, ErrAddress):
			return finish(Unknown, AddressDenied)
		case errors.Is(err, ErrPermission), errors.Is(err, os.ErrPermission):
			return finish(Unknown, PermissionError)
		case tlsFailed.Load():
			return finish(Unknown, TLSError)
		case !connected.Load():
			return finish(Unknown, ConnectError)
		default:
			return finish(Unknown, Malformed)
		}
	}
	if response.StatusCode >= 200 && response.StatusCode <= 599 {
		if plan.ready[response.StatusCode] {
			return finish(Ready, MatchedReady)
		}
		if plan.notReady[response.StatusCode] {
			return finish(NotReady, MatchedNotReady)
		}
	}
	return finish(Unknown, StatusUnmapped)
}
