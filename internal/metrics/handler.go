package metrics

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const MaxConcurrentExports = 2
const WriteBudget = 200 * time.Millisecond

// Handler is opt-in. Its owner must install it only on the validated loopback
// diagnostic listener. It has no listener, registration, or background worker.
type Handler struct {
	registry   *Registry
	slots      chan struct{}
	render     func(Snapshot) ([]byte, error)
	activityMu sync.Mutex
	active     [MaxConcurrentExports]time.Time
}

// ExportActivity is a single Handler's process-local diagnostic snapshot. It
// has no admission authority and exposes neither request nor entity identity.
type ExportActivity struct {
	Active            int
	YoungestActiveAge time.Duration
}

func (h *Handler) Activity() ExportActivity {
	if h == nil {
		return ExportActivity{}
	}
	h.activityMu.Lock()
	defer h.activityMu.Unlock()
	now := time.Now()
	var result ExportActivity
	for _, start := range h.active {
		if start.IsZero() {
			continue
		}
		age := max(0, now.Sub(start))
		if result.Active == 0 || age < result.YoungestActiveAge {
			result.YoungestActiveAge = age
		}
		result.Active++
	}
	return result
}

func (h *Handler) acquire() (int, bool) {
	h.activityMu.Lock()
	defer h.activityMu.Unlock()
	select {
	case h.slots <- struct{}{}:
		for i, start := range h.active {
			if start.IsZero() {
				h.active[i] = time.Now()
				return i, true
			}
		}
		panic("metrics slot/activity ownership diverged")
	default:
		return 0, false
	}
}

func (h *Handler) release(i int) {
	h.activityMu.Lock()
	defer h.activityMu.Unlock()
	if h.active[i].IsZero() {
		panic("metrics slot released twice")
	}
	select {
	case <-h.slots:
		h.active[i] = time.Time{}
	default:
		panic("metrics slot/activity ownership diverged")
	}
}

func NewHandler(r *Registry) *Handler {
	if r == nil {
		panic("nil metrics registry")
	}
	return &Handler{registry: r, slots: make(chan struct{}, MaxConcurrentExports), render: Render}
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	deadline := time.Now().Add(WriteBudget)
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(deadline); err != nil {
		h.deadlineFailure(err)
		// Without a controllable deadline even an error response could block.
		// net/http handles this sentinel by closing the connection, without an
		// accidental implicit 200 or an unbounded diagnostic write.
		panic(http.ErrAbortHandler)
	}
	if r.Body != nil && r.Body != http.NoBody {
		r.Close = true
		w.Header().Set("Connection", "close")
		if err := controller.SetReadDeadline(deadline); err != nil {
			h.deadlineFailure(err)
			panic(http.ErrAbortHandler)
		}
	}
	slot, acquired := h.acquire()
	if !acquired {
		h.registry.Add(Exports, "rejected")
		h.write(w, controller, http.StatusTooManyRequests, []byte("metrics busy\n"))
		return
	}
	defer h.release(slot)
	if r.Method != http.MethodGet || (r.Body != nil && r.Body != http.NoBody) {
		h.registry.Add(Exports, "request_rejected")
		h.write(w, controller, http.StatusServiceUnavailable, []byte("metrics request rejected\n"))
		return
	}
	body, err := h.render(h.registry.Snapshot())
	if err != nil || len(body) > MaxResponseBytes {
		h.registry.Add(Exports, "too_large")
		h.write(w, controller, http.StatusServiceUnavailable, []byte("metrics response too large\n"))
		return
	}
	if !time.Now().Before(deadline) {
		h.registry.Add(Exports, "timeout")
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Content-Type", ContentType)
	if h.write(w, controller, http.StatusOK, body) {
		h.registry.Add(Exports, "success")
	}
}
func (h *Handler) write(w http.ResponseWriter, c *http.ResponseController, status int, body []byte) bool {
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	n, err := w.Write(body)
	if err == nil && n != len(body) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = c.Flush()
	}
	if err != nil {
		h.failure(err)
		return false
	}
	return true
}

func (h *Handler) deadlineFailure(err error) {
	if errors.Is(err, http.ErrNotSupported) {
		h.registry.Add(Exports, "unsupported_writer")
		return
	}
	h.failure(err)
}

func (h *Handler) failure(err error) {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		h.registry.Add(Exports, "timeout")
	} else {
		h.registry.Add(Exports, "write_error")
	}
}
