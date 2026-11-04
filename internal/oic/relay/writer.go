package relay

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type queuedControl struct {
	ctx       context.Context
	key       string
	build     func(time.Time) (protocol.Message, error)
	done      chan error
	telemetry bool
	cancel    context.CancelFunc
	query     *queuedQuery
}

// queuedQuery is one slot owner. A callback from an expired owner must not
// remove another entry with the same key, nor a query already being written.
type queuedQuery struct{ stop func() bool }

// controlWriter gives critical traffic its own bounded queue. Only its run
// goroutine assigns wire sequence numbers, after fair selection and validation.
type controlWriter struct {
	ctx                      context.Context
	cancel                   context.CancelFunc
	c                        *websocket.Conn
	critical                 chan queuedControl
	wake                     chan struct{}
	done                     chan struct{}
	mu                       sync.Mutex
	telemetry                map[string]queuedControl
	order                    []string
	maxTelemetry, messageMax int
	writeTimeout             time.Duration
	queryMax                 int
	queries                  map[string]*queuedQuery
}

func newControlWriter(ctx context.Context, c *websocket.Conn, critical, telemetry, queryMax, messageMax int, timeout time.Duration) *controlWriter {
	ctx, cancel := context.WithCancel(ctx)
	w := &controlWriter{ctx: ctx, cancel: cancel, c: c, critical: make(chan queuedControl, critical), wake: make(chan struct{}, 1), done: make(chan struct{}), telemetry: make(map[string]queuedControl), maxTelemetry: telemetry, messageMax: messageMax, writeTimeout: timeout, queryMax: queryMax, queries: make(map[string]*queuedQuery)}
	go w.run()
	return w
}

func (w *controlWriter) narrow(maxBytes, queries int, write time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messageMax = min(w.messageMax, maxBytes)
	w.queryMax = min(w.queryMax, queries)
	w.writeTimeout = min(w.writeTimeout, write)
}

func (w *controlWriter) stop() { w.cancel(); _ = w.c.CloseNow(); <-w.done }

func (w *controlWriter) send(ctx context.Context, m protocol.Message) error {
	return w.sendBuilt(ctx, func(time.Time) (protocol.Message, error) { return m, nil })
}

func (w *controlWriter) sendBuilt(ctx context.Context, build func(time.Time) (protocol.Message, error)) error {
	q := queuedControl{ctx: ctx, build: build, done: make(chan error, 1)}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case <-w.ctx.Done():
		return w.ctx.Err()
	default:
	}
	select {
	case w.critical <- q:
		select {
		case err := <-q.done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-w.ctx.Done():
			return w.ctx.Err()
		}
	default:
		w.cancel()
		_ = w.c.CloseNow()
		return protocol.Errorf(protocol.CodeResourceExhausted, "critical control queue full")
	}
}

// offer coalesces only a caller-selected immutable identity. Dirty keys use
// one Exposure; query responses use a distinct query ID and a fixed slot bound.
func (w *controlWriter) offer(key string, ctx context.Context, build func(time.Time) (protocol.Message, error)) (bool, error) {
	return w.offerUntil(key, ctx, time.Time{}, build)
}

func (w *controlWriter) offerUntil(key string, ctx context.Context, deadline time.Time, build func(time.Time) (protocol.Message, error)) (bool, error) {
	if w.ctx.Err() != nil {
		return false, w.ctx.Err()
	}
	w.mu.Lock()
	if w.ctx.Err() != nil {
		w.mu.Unlock()
		return false, w.ctx.Err()
	}
	if ctx.Err() != nil || !deadline.IsZero() && !time.Now().Before(deadline) {
		w.mu.Unlock()
		return false, nil
	}
	// The deadline callback reclaims slots without needing another offer.
	// Also sweep here so callback scheduling cannot reject a valid new query.
	for _, q := range w.telemetry {
		if q.query != nil && q.ctx.Err() != nil {
			w.expireQueuedQueryLocked(q)
		}
	}
	query := strings.HasPrefix(key, "query:")
	if query && (w.queries[key] != nil || len(w.queries) >= w.queryMax) {
		w.mu.Unlock()
		return false, protocol.Errorf(protocol.CodeProtocolError, "duplicate or excess outstanding query")
	}
	_, merged := w.telemetry[key]
	if !merged && len(w.telemetry) >= w.maxTelemetry {
		w.mu.Unlock()
		return false, protocol.Errorf(protocol.CodeResourceExhausted, "telemetry queue full")
	}
	if !merged {
		w.order = append(w.order, key)
	}
	var cancel context.CancelFunc
	if !deadline.IsZero() {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	}
	q := queuedControl{ctx: ctx, build: build, telemetry: true, key: key, cancel: cancel}
	if query {
		q.query = &queuedQuery{}
		w.queries[key] = q.query
	}
	w.telemetry[key] = q
	if query {
		q.query.stop = context.AfterFunc(ctx, func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.expireQueuedQueryLocked(q)
		})
	}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return merged, nil
}

func (w *controlWriter) release(q queuedControl) {
	w.mu.Lock()
	w.releaseQueryLocked(q)
	w.mu.Unlock()
	if q.cancel != nil {
		q.cancel()
	}
}

func (w *controlWriter) releaseQueryLocked(q queuedControl) {
	if q.query == nil {
		return
	}
	if q.query.stop != nil {
		q.query.stop()
	}
	if w.queries[q.key] == q.query {
		delete(w.queries, q.key)
	}
}

func (w *controlWriter) expireQueuedQueryLocked(q queuedControl) {
	current, queued := w.telemetry[q.key]
	if !queued || current.query != q.query {
		return // selected/writing, already released, or replaced
	}
	delete(w.telemetry, q.key)
	for i, key := range w.order {
		if key == q.key {
			copy(w.order[i:], w.order[i+1:])
			w.order[len(w.order)-1] = ""
			w.order = w.order[:len(w.order)-1]
			break
		}
	}
	w.releaseQueryLocked(q)
	if q.cancel != nil {
		q.cancel()
	}
}

func (w *controlWriter) popTelemetry() (queuedControl, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.order) == 0 {
		return queuedControl{}, false
	}
	k := w.order[0]
	w.order = w.order[1:]
	q := w.telemetry[k]
	delete(w.telemetry, k)
	return q, true
}

func (w *controlWriter) run() {
	defer close(w.done)
	defer w.discardPending()
	var seq uint64
	criticalRun := 0
	for {
		if w.ctx.Err() != nil {
			return
		}
		var q queuedControl
		var ok bool
		if criticalRun >= 8 {
			q, ok = w.popTelemetry()
		}
		if !ok {
			select {
			case q = <-w.critical:
				ok = true
			default:
			}
		}
		if !ok {
			q, ok = w.popTelemetry()
		}
		if !ok {
			select {
			case <-w.ctx.Done():
				return
			case q = <-w.critical:
				ok = true
			case <-w.wake:
				continue
			}
		}
		if q.telemetry {
			criticalRun = 0
		} else {
			criticalRun = min(8, criticalRun+1)
		}
		if err := q.ctx.Err(); err != nil {
			if q.done != nil {
				q.done <- err
			}
			w.release(q)
			continue
		}
		m, err := q.build(time.Now())
		if contextErr := q.ctx.Err(); contextErr != nil {
			if q.done != nil {
				q.done <- contextErr
			}
			w.release(q)
			continue // no socket write began; keep the session and sequence
		}
		if err == nil && m != nil {
			if seq == math.MaxUint64 {
				err = errors.New("control sequence exhausted")
			} else {
				seq++
				data := protocol.EncodeControl(m, seq)
				w.mu.Lock()
				maxBytes, writeTimeout := w.messageMax, w.writeTimeout
				w.mu.Unlock()
				if q.telemetry && len(data) > maxBytes {
					err = protocol.Errorf(protocol.CodePayloadTooLarge, "telemetry exceeds negotiated limit")
				} else {
					d := connectTimeout
					if q.telemetry {
						d = writeTimeout
					}
					ctx, cancel := context.WithTimeout(q.ctx, d)
					err = w.c.Write(ctx, websocket.MessageText, data)
					cancel()
				}
			}
		}
		if q.done != nil {
			q.done <- err
		}
		w.release(q)
		if err != nil {
			// A WebSocket write interrupted by its deadline is no longer usable.
			w.cancel()
			_ = w.c.CloseNow()
			return
		}
	}
}

func (w *controlWriter) discardPending() {
	w.mu.Lock()
	queued := w.telemetry
	w.telemetry = make(map[string]queuedControl)
	w.order = nil
	w.queries = make(map[string]*queuedQuery)
	w.mu.Unlock()
	for _, q := range queued {
		w.release(q)
	}
	for {
		select {
		case q := <-w.critical:
			if q.done != nil {
				q.done <- context.Canceled
			}
		default:
			return
		}
	}
}
