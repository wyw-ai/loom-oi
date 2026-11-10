//go:build windows

package terminal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/wyw-ai/loom-oi/apps/powershell/internal/protocol"
)

// Serve processes protocol input independently of waiting Actions. EOF is the
// shutdown request: terminate executions, join readers, then finish responses.
func Serve(input io.Reader, output io.Writer) error {
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 4096), protocol.MaxFrameBytes)
	if !scan.Scan() {
		if e := scan.Err(); e != nil {
			return e
		}
		return io.EOF
	}
	decode := func(b []byte) (protocol.Frame, error) {
		var f protocol.Frame
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		err := dec.Decode(&f)
		if err == nil {
			var extra any
			if trailing := dec.Decode(&extra); trailing != io.EOF {
				return f, fmt.Errorf("one JSON object per line is required")
			}
		}
		return f, err
	}
	start, err := decode(scan.Bytes())
	if err != nil {
		return err
	}
	if start.Type != "start" || start.Protocol != protocol.Version {
		return fmt.Errorf("first frame must be protocol 1 start")
	}
	c, err := ParseConfig(start.Config)
	if err != nil {
		return err
	}
	engine, err := New(c)
	if err != nil {
		return err
	}
	defer engine.Close()
	var writeMu sync.Mutex
	encoder := json.NewEncoder(output)
	send := func(frame protocol.Frame) error { writeMu.Lock(); defer writeMu.Unlock(); return encoder.Encode(frame) }
	if err = send(protocol.Frame{Type: "ready", Protocol: protocol.Version, Handlers: []string{"exec_command", "write_stdin"}}); err != nil {
		return err
	}
	var calls sync.WaitGroup
	var pendingMu sync.Mutex
	pending := map[string]string{}
	for scan.Scan() {
		frame, err := decode(scan.Bytes())
		if err != nil {
			engine.Close()
			calls.Wait()
			return fmt.Errorf("invalid protocol frame: %w", err)
		}
		switch frame.Type {
		case "invoke":
			key, _ := json.Marshal(struct {
				Handler string
				Input   map[string]any
				Context protocol.Context
			}{frame.Handler, frame.Input, frame.Context})
			pendingMu.Lock()
			if prior, exists := pending[frame.CallID]; exists {
				pendingMu.Unlock()
				if prior != string(key) {
					_ = send(errorFrame(frame.CallID, "CALL_ID_CONFLICT", "in-flight call_id belongs to different input or identity"))
				}
				// One pending call produces one response even if the same frame
				// arrives repeatedly. Do not allocate an unbounded set of waiters.
				continue
			}
			if len(pending) >= 64 {
				pendingMu.Unlock()
				_ = send(errorFrame(frame.CallID, "CAPACITY_EXCEEDED", "too many in-flight calls"))
				continue
			}
			pending[frame.CallID] = string(key)
			pendingMu.Unlock()
			calls.Add(1)
			accepted := make(chan struct{})
			go func() {
				defer calls.Done()
				_ = send(engine.invoke(frame, accepted))
				pendingMu.Lock()
				delete(pending, frame.CallID)
				pendingMu.Unlock()
			}()
			// Register the call before reading its following cancel frame. This
			// waits only for bookkeeping, never for shell startup or output.
			<-accepted
		case "cancel":
			engine.Cancel(frame.CallID)
		default:
			engine.Close()
			calls.Wait()
			return fmt.Errorf("unknown frame type %q", frame.Type)
		}
	}
	engine.Close()
	calls.Wait()
	return scan.Err()
}
