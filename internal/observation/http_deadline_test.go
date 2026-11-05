package observation

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTPProbeRejectsSuccessfulResponseAtDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		late time.Duration
	}{{"at_deadline", 0}, {"after_deadline", time.Nanosecond}} {
		t.Run(tc.name, func(t *testing.T) {
			plan := testPlan(t)
			if plan.timeout != 200*time.Millisecond {
				t.Fatal("deadline fixture must retain the original 200ms budget")
			}
			client, server := net.Pipe()
			conn := &readCountConn{Conn: client}
			const response = "HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
			done := make(chan struct{})
			var serverErr error
			t.Cleanup(func() {
				_ = client.Close()
				_ = server.Close()
				<-done
			})
			go func() {
				defer close(done)
				defer server.Close()
				request, err := http.ReadRequest(bufio.NewReader(server))
				if err != nil {
					serverErr = err
					return
				}
				if request.Method != http.MethodGet || !request.Close {
					serverErr = fmt.Errorf("unexpected probe request: %s close=%t", request.Method, request.Close)
					return
				}
				_, serverErr = io.WriteString(server, response)
			}()
			started := time.Now()
			deadline := started.Add(plan.timeout)
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			decision := deadline.Add(tc.late)
			calls := 0
			now := func() time.Time {
				calls++
				if calls == 1 {
					return started
				}
				if calls == 2 {
					if got := conn.bytes.Load(); got != int64(len(response)) {
						t.Fatalf("final decision without the complete real 200 header: %d bytes", got)
					}
					if ctx.Err() != nil || !time.Now().Before(deadline) {
						t.Fatal("fixture reached the real deadline before the injected final decision")
					}
				}
				return decision
			}
			out := execute(ctx, plan, func(context.Context, string, string) (net.Conn, error) {
				return conn, nil
			}, started, now)
			<-done
			if serverErr != nil {
				t.Fatal("real HTTP exchange failed", serverErr)
			}
			// The final-decision now call is short-circuited when the request
			// context has expired. Exactly three calls prove this result used
			// the injected decision boundary, not that unrelated timeout path.
			if calls != 3 || out.Readiness != Unknown || out.Reason != Timeout || out.Duration != plan.timeout+tc.late {
				t.Fatalf("late successful response was not rejected at the fixed boundary: calls=%d outcome=%+v", calls, out)
			}
		})
	}
}
