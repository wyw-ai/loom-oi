package fixture

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTargetBoundaryRecords(t *testing.T) {
	for _, command := range []string{"stream 1048576", "hold"} {
		t.Run(command, func(t *testing.T) {
			tg := New(t, "target", Program)
			c0, err := net.Dial("tcp", tg.Addr())
			if err != nil {
				t.Fatal(err)
			}
			c := c0.(*net.TCPConn)
			t.Cleanup(func() { _ = c.Close() })
			_ = c.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := c.Write([]byte(command)); err != nil {
				t.Fatal(err)
			}
			if !tg.Wait(time.Second, func(rs []Record) bool { return len(rs) == 1 && len(rs[0].Data) == len(command) }) {
				t.Fatal("request not observed")
			}
			r := tg.Conns()[0]
			if r.First.IsZero() || !r.EOFAt.IsZero() || !r.ResponseWriteStarted.IsZero() || r.LocalAddress != c.RemoteAddr().String() || r.RemoteAddress != c.LocalAddr().String() {
				t.Fatalf("before FIN: %+v", r)
			}
			if err := c.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			if !tg.Wait(time.Second, func(rs []Record) bool { return rs[0].EOF }) {
				t.Fatal("FIN not observed")
			}
			if command == "hold" {
				r = tg.Conns()[0]
				if r.EOFAt.IsZero() || !r.ResponseWriteStarted.IsZero() || r.ResponseBytes != 0 {
					t.Fatalf("after FIN, before response: %+v", r)
				}
				t.Logf("target has FIN, no response writer: %+v", r)
				return
			}
			n, err := io.Copy(io.Discard, c)
			if err != nil || n != 1<<20 {
				t.Fatalf("%d %v", n, err)
			}
			if !tg.Wait(time.Second, func(rs []Record) bool { return rs[0].Done }) {
				t.Fatal("target not done")
			}
			r = tg.Conns()[0]
			if r.ResponseBytes != n || r.ResponseFirstWriteBytes != 64<<10 || r.ResponseFirstWriteResult != "ok" || r.ResponseWriteResult != "ok" || r.ResponseWriteStarted.Before(r.EOFAt) || r.ResponseFirstWriteReturned.Before(r.ResponseWriteStarted) || r.ResponseWriteFinished.Before(r.ResponseFirstWriteReturned) || r.DoneAt.Before(r.ResponseWriteFinished) {
				t.Fatalf("response boundaries: %+v", r)
			}
			t.Logf("completed response boundary: %+v", r)
		})
	}
}

type heldResponse struct {
	entered, release chan struct{}
	once             sync.Once
	err              error
}

func (w *heldResponse) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	if w.err != nil {
		return 3, w.err
	}
	return len(p), nil
}

func TestTargetResponseBoundaryInsideWrite(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "partial_error"}[fail], func(t *testing.T) {
			tg := &Target{}
			r := &Record{}
			tg.conns = []*Record{r}
			out := &heldResponse{entered: make(chan struct{}), release: make(chan struct{})}
			if fail {
				out.err = errors.New("not exported raw network error")
			}
			w := &responseWriter{target: tg, record: r, conn: out}
			done := make(chan struct{})
			go func() { _, _ = w.Write([]byte("response")); w.finish(); close(done) }()
			select {
			case <-out.entered:
			case <-time.After(time.Second):
				t.Fatal("write not entered")
			}
			s := tg.Conns()[0]
			if s.ResponseWriteStarted.IsZero() || !s.ResponseFirstWriteReturned.IsZero() || !s.ResponseWriteFinished.IsZero() || s.ResponseBytes != 0 {
				t.Fatalf("blocked: %+v", s)
			}
			close(out.release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("write not returned")
			}
			s = tg.Conns()[0]
			want, result := int64(8), "ok"
			if fail {
				want, result = 3, "error"
			}
			if s.ResponseBytes != want || s.ResponseFirstWriteBytes != want || s.ResponseWriteResult != result || s.ResponseFirstWriteResult != result || s.ResponseWriteFinished.IsZero() {
				t.Fatalf("returned: %+v", s)
			}
			at := s.ResponseWriteFinished
			w.finish()
			if !tg.Conns()[0].ResponseWriteFinished.Equal(at) {
				t.Fatal("finish rewrote the observed boundary")
			}
		})
	}
}
