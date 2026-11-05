package wsio

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ManagedConn keeps the data-leg control owner with its real transport. Only
// a first unexpected control-write failure creates a short-lived closing
// coordinator; there is no additional listener or control-write worker.
type ManagedConn struct {
	*websocket.Conn
	control              *Control
	grace                time.Duration
	mu                   sync.Mutex
	deadline             time.Time
	transport            net.Conn
	closing              bool
	failure              error
	cleanupError         error
	handler              func(error)
	handlerSet, notified bool
	failureRunning       bool
	failureDone          chan struct{}
}

// DialData installs the borrowed-payload hooks before the handshake. It only
// enables asynchronous PONG replies: endpoints do not originate probes.
func DialData(ctx context.Context, url string, options *websocket.DialOptions, closeGrace time.Duration) (*ManagedConn, *http.Response, error) {
	m := &ManagedConn{control: NewControl(nil), grace: closeGrace, failureDone: make(chan struct{})}
	var opts websocket.DialOptions
	if options != nil {
		opts = *options
	}
	opts.OnPingReceived, opts.OnPongReceived = m.control.OnPing, m.control.OnPong
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		m.mu.Lock()
		m.transport = info.Conn
		m.mu.Unlock()
	}})
	c, response, err := websocket.Dial(ctx, url, &opts)
	if err != nil {
		m.control.Stop()
		return nil, response, err
	}
	m.Conn = c
	if err := m.control.Start(managedControlWriter{m}); err != nil {
		_ = m.CloseNow()
		return nil, response, err
	}
	return m, response, nil
}

// SetCloseDeadline preserves the earliest original terminal/cancel deadline.
// It does not close a Completed transport or start a force-close timer.
func (m *ManagedConn) SetCloseDeadline(deadline time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !deadline.IsZero() && (m.deadline.IsZero() || deadline.Before(m.deadline)) {
		m.deadline = deadline
	}
}

// BindControlFailure binds the original business coordinator exactly once.
// A failure before binding is delivered here; it is never silently lost.
// The callback must not call JoinCleanup, which also waits for this callback.
func (m *ManagedConn) BindControlFailure(handler func(error)) {
	m.mu.Lock()
	if m.handlerSet {
		m.mu.Unlock()
		panic("data control failure handler already bound")
	}
	m.handler, m.handlerSet = handler, true
	err := m.failure
	notify := err != nil && !m.notified && handler != nil
	if notify {
		m.notified = true
	}
	m.mu.Unlock()
	if notify {
		handler(err)
	}
}

func (m *ManagedConn) ControlError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failure
}

func (m *ManagedConn) ControlSnapshot() ControlSnapshot { return m.control.Snapshot() }

func (m *ManagedConn) closingDeadline() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deadline.IsZero() {
		m.deadline = time.Now().Add(m.grace)
	}
	m.closing = true
	return m.deadline
}

func (m *ManagedConn) joinControl(deadline time.Time) error {
	// An already-finished owner needs no extra scheduling allowance after D.
	select {
	case <-m.control.Done():
		return m.cleanupResult(nil)
	default:
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	return m.cleanupResult(m.control.Join(ctx))
}

func (m *ManagedConn) cleanupResult(err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil && m.cleanupError == nil {
		m.cleanupError = err
	}
	return m.cleanupError
}

// CloseNow begins no new grace after a previously saved terminal deadline.
// It joins only the control writer, never its calling failure coordinator.
func (m *ManagedConn) CloseNow() error {
	deadline := m.closingDeadline()
	m.control.Stop()
	m.closeUnderlying()
	err := m.Conn.CloseNow()
	return errors.Join(err, m.joinControl(deadline))
}

// A TLS close_notify can wait behind a blocked TLS Write. At the existing
// transport-close point, close this exact Dial's socket first to release it.
// Capturing GotConn does not alter or close unrelated pooled HTTP connections.
func (m *ManagedConn) closeUnderlying() {
	m.mu.Lock()
	c := m.transport
	m.mu.Unlock()
	if tlsConn, ok := c.(*tls.Conn); ok {
		c = tlsConn.NetConn()
	}
	if c != nil {
		_ = c.Close()
	}
}

func (m *ManagedConn) Close(code websocket.StatusCode, reason string) error {
	deadline := m.closingDeadline()
	m.control.Stop()
	forced := make(chan struct{})
	timer := time.AfterFunc(time.Until(deadline), func() { m.closeUnderlying(); _ = m.Conn.CloseNow(); close(forced) })
	err := m.Conn.Close(code, reason)
	if !timer.Stop() {
		<-forced
	}
	return errors.Join(err, m.joinControl(deadline))
}

// JoinCleanup is called outside the business finish callback, after its
// existing transport-close path. It cannot renew the original close deadline.
func (m *ManagedConn) JoinCleanup() error {
	m.mu.Lock()
	deadline, running := m.deadline, m.failureRunning
	m.mu.Unlock()
	if deadline.IsZero() {
		return errors.New("data transport has no closing deadline")
	}
	err := m.joinControl(deadline)
	if !running {
		return err
	}
	select {
	case <-m.failureDone:
		return err
	default:
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	select {
	case <-m.failureDone:
		return err
	case <-ctx.Done():
		return errors.Join(err, m.cleanupResult(ctx.Err()))
	}
}

// DrainAndJoin is only for an outer owner after its original Reader returned.
// In particular, a rejected READY must leave time for the peer to receive the
// ABORT and close; joining the idle control worker alone cannot observe that.
func (m *ManagedConn) DrainAndJoin() error {
	m.mu.Lock()
	deadline := m.deadline
	m.mu.Unlock()
	if deadline.IsZero() {
		return errors.New("data transport has no closing deadline")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	for {
		if _, _, err := m.Read(ctx); err != nil {
			break
		}
	}
	return m.JoinCleanup()
}

func (m *ManagedConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	typ, b, err := m.Conn.Read(ctx)
	if err != nil {
		_ = m.CloseNow()
	}
	return typ, b, err
}

func (m *ManagedConn) Reader(ctx context.Context) (websocket.MessageType, io.Reader, error) {
	typ, r, err := m.Conn.Reader(ctx)
	if err != nil {
		_ = m.CloseNow()
		return typ, nil, err
	}
	return typ, managedMessageReader{m, r}, nil
}

type managedMessageReader struct {
	conn   *ManagedConn
	reader io.Reader
}

func (r managedMessageReader) Read(b []byte) (int, error) {
	n, err := r.reader.Read(b)
	if err != nil && err != io.EOF {
		_ = r.conn.CloseNow()
	}
	return n, err
}

func (m *ManagedConn) Write(ctx context.Context, typ websocket.MessageType, b []byte) error {
	err := m.Conn.Write(ctx, typ, b)
	if err != nil {
		_ = m.CloseNow()
	}
	return err
}

type managedControlWriter struct{ conn *ManagedConn }

func (w managedControlWriter) WritePing(ctx context.Context, b []byte) error {
	err := w.conn.Conn.WritePing(ctx, b)
	if err != nil {
		w.conn.controlFailed(err)
	}
	return err
}

func (w managedControlWriter) WritePong(ctx context.Context, b []byte) error {
	err := w.conn.Conn.WritePong(ctx, b)
	if err != nil {
		w.conn.controlFailed(err)
	}
	return err
}

func (m *ManagedConn) controlFailed(err error) {
	m.mu.Lock()
	if m.closing || m.failure != nil {
		m.mu.Unlock()
		return
	}
	// Stop can suppress Control.Err while closing. Preserve the actual first
	// write error before starting that close, including ordinary Flush errors.
	m.failure, m.failureRunning = err, true
	if m.deadline.IsZero() {
		m.deadline = time.Now().Add(m.grace)
	}
	m.mu.Unlock()
	go func() {
		defer close(m.failureDone)
		// Release WebSocket I/O before invoking a potentially competing finish.
		// The callback also releases a bridge blocked on local TCP I/O.
		_ = m.CloseNow()
		m.mu.Lock()
		handler := m.handler
		notify := handler != nil && !m.notified
		if notify {
			m.notified = true
		}
		m.mu.Unlock()
		if notify {
			handler(err)
		}
	}()
}
