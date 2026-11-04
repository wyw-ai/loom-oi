package metrics

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

const MaxConcurrentExports = 2
const WriteBudget = 200 * time.Millisecond

// Handler is opt-in. Its owner must install it only on the validated loopback
// diagnostic listener. It has no listener, registration, or background worker.
type Handler struct {
	registry *Registry
	slots    chan struct{}
	render   func(Snapshot) ([]byte, error)
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
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		h.registry.Add(Exports, "rejected")
		h.write(w, controller, http.StatusTooManyRequests, []byte("metrics busy\n"))
		return
	}
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
