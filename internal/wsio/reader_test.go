package wsio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type memorySource struct {
	data []byte
	read bytes.Reader
	typ  websocket.MessageType
	err  error
	body io.Reader
}

func (s *memorySource) Reader(context.Context) (websocket.MessageType, io.Reader, error) {
	s.read.Reset(s.data)
	if s.body != nil {
		return s.typ, s.body, s.err
	}
	return s.typ, &s.read, s.err
}

type chunkReader struct {
	data []byte
	err  error
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data[:1])
	r.data = r.data[n:]
	return n, nil
}

func TestMessageReaderReuseAndBounds(t *testing.T) {
	const f = 32 << 10
	r := NewReader(f+2, 2*f)
	s := &memorySource{typ: websocket.MessageBinary}
	var first *byte
	for _, value := range []byte{0x12, 0xf3, 0x51} {
		s.data = bytes.Repeat([]byte{value}, f+1)
		typ, got, err := r.Read(context.Background(), s)
		if err != nil || typ != s.typ || !bytes.Equal(got, s.data) {
			t.Fatal(typ, len(got), err)
		}
		if first != nil && first != &got[0] {
			t.Fatal("normal F+1 message reallocated")
		}
		first = &got[0]
		if cap(r.buf) != f+2 {
			t.Fatal("normal frame grew buffer", cap(r.buf))
		}
	}
	for _, size := range []int{0, f + 7, 2 * f, 1, 2*f + 1, 8 * f} {
		s.data = bytes.Repeat([]byte{0x67}, size)
		_, got, err := r.Read(context.Background(), s)
		if size <= 2*f && (err != nil || !bytes.Equal(got, s.data)) {
			t.Fatal(size, len(got), err)
		}
		if size > 2*f && !errors.Is(err, websocket.ErrMessageTooBig) {
			t.Fatal("oversized untrusted source accepted", size, err)
		}
		if cap(r.buf) > 2*f+1 || len(got) > 2*f+1 {
			t.Fatal("buffer exceeded original read limit plus probe byte")
		}
	}
}

func TestMessageReaderShortReadsAndErrors(t *testing.T) {
	wantErr := errors.New("broken input")
	for _, tc := range []struct {
		name string
		data []byte
		end  error
	}{
		{"short_reads", []byte("pieces"), io.EOF},
		{"partial_error", []byte("partial"), wantErr},
		{"empty_message", nil, io.EOF},
		{"at_limit", []byte("12345678"), io.EOF},
		{"over_limit", []byte("123456789"), io.EOF},
		{"over_limit_read_error", []byte("123456789"), wantErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReader(3, 8)
			s := &memorySource{typ: websocket.MessageText, body: &chunkReader{data: tc.data, err: tc.end}}
			typ, got, err := r.Read(context.Background(), s)
			want := tc.end
			if want == io.EOF {
				want = nil
				if len(tc.data) > 8 {
					want = websocket.ErrMessageTooBig
				}
			}
			if typ != s.typ || !bytes.Equal(got, tc.data) || !errors.Is(err, want) {
				t.Fatal(typ, string(got), err)
			}
		})
	}
	for _, err := range []error{io.EOF, context.Canceled, wantErr} {
		r := NewReader(2, 8)
		if typ, got, actual := r.Read(context.Background(), &memorySource{err: err}); typ != 0 || got != nil || actual != err {
			t.Fatal("message-start error changed", typ, got, actual)
		}
	}
}

// io.Reader permits the final bytes and their EOF/error in the same call.
type finalBytesReader struct {
	data []byte
	end  error
}

func (r *finalBytesReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.end
	}
	return n, nil
}

func TestMessageReaderFinalBytesAndError(t *testing.T) {
	broken := errors.New("partial message")
	for _, tc := range []struct {
		name, data string
		end, want  error
	}{
		{"short_eof", "1234", io.EOF, nil},
		{"exact_limit_eof", "12345678", io.EOF, nil},
		{"over_limit_eof", "123456789", io.EOF, websocket.ErrMessageTooBig},
		{"partial_error", "1234", broken, broken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReader(3, 8)
			s := &memorySource{typ: websocket.MessageBinary, body: &finalBytesReader{data: []byte(tc.data), end: tc.end}}
			typ, data, err := r.Read(context.Background(), s)
			if typ != s.typ || string(data) != tc.data || !errors.Is(err, tc.want) {
				t.Fatal(typ, string(data), err)
			}
			if cap(r.buf) > 9 {
				t.Fatal("buffer exceeded original read limit plus probe byte")
			}
		})
	}
}

func TestMessageReaderInvalidBounds(t *testing.T) {
	for _, args := range [][2]int{{0, 8}, {2, -1}, {2, int(^uint(0) >> 1)}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid bounds accepted", args)
				}
			}()
			NewReader(args[0], args[1])
		}()
	}
}

func messagePeer(t *testing.T, produce func(context.Context, *websocket.Conn)) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		defer c.CloseNow()
		produce(ctx, c)
	}))
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = c.CloseNow(); srv.Close() })
	return c
}

func TestMessageReaderRealFragmentedAndLimit(t *testing.T) {
	for _, size := range []int{0, 17, 32769, 32770} {
		for _, reusable := range []bool{false, true} {
			name := "original"
			if reusable {
				name = "reusable"
			}
			t.Run(name+"/"+stringSize(size), func(t *testing.T) {
				data := bytes.Repeat([]byte{0xc7}, size)
				peerClosed := make(chan error, 1)
				c := messagePeer(t, func(ctx context.Context, c *websocket.Conn) {
					writer, err := c.Writer(ctx, websocket.MessageBinary)
					if err != nil {
						return
					}
					for left := data; len(left) > 0; {
						n := min(317, len(left))
						if _, err = writer.Write(left[:n]); err != nil {
							return
						}
						left = left[n:]
					}
					_ = writer.Close()
					_, _, readErr := c.Read(ctx) // process the reader's protocol close
					peerClosed <- readErr
				})
				c.SetReadLimit(32769)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				var got []byte
				var err error
				if reusable {
					_, got, err = NewReader(32770, 32769).Read(ctx, c)
				} else {
					_, got, err = c.Read(ctx)
				}
				if size <= 32769 && (err != nil || !bytes.Equal(data, got)) {
					t.Fatal(size, len(got), err)
				}
				if size > 32769 && !errors.Is(err, websocket.ErrMessageTooBig) {
					t.Fatal("changed actual WebSocket limit error", err)
				}
				if size > 32769 {
					select {
					case err = <-peerClosed:
						if websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
							t.Fatal("reader did not send the original 1009 close", err)
						}
					case <-ctx.Done():
						t.Fatal("oversize close did not reach peer")
					}
				}
			})
		}
	}
}

func stringSize(n int) string {
	if n == 0 {
		return "empty"
	}
	if n == 17 {
		return "short"
	}
	if n == 32769 {
		return "maximum"
	}
	return "over_limit"
}

func TestMessageReaderRealAllocationComparison(t *testing.T) {
	data := bytes.Repeat([]byte{0x27}, 32769)
	requests, written := make(chan struct{}), make(chan error)
	c := messagePeer(t, func(ctx context.Context, c *websocket.Conn) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-requests:
				// Both measurements use the same actual, uncompressed socket.
				err := c.Write(context.Background(), websocket.MessageBinary, data)
				select {
				case written <- err:
				case <-ctx.Done():
					return
				}
			}
		}
	})
	c.SetReadLimit(32770)
	r := NewReader(32770, 32770)
	measure := func(reuse bool) float64 {
		return testing.AllocsPerRun(50, func() {
			requests <- struct{}{}
			var got []byte
			var err error
			if reuse {
				_, got, err = r.Read(context.Background(), c)
			} else {
				_, got, err = c.Read(context.Background())
			}
			writeErr := <-written
			if err != nil || writeErr != nil || !bytes.Equal(data, got) {
				t.Fatal(err, writeErr, len(got))
			}
		})
	}
	old, current := measure(false), measure(true)
	t.Logf("real WebSocket maximum DATA (32769 wire bytes): original_allocs=%g reusable_allocs=%g; comparison includes peer writes, not a product SLO", old, current)
	if current >= old {
		t.Fatal("normal maximum messages still allocate like io.ReadAll", old, current)
	}
}
