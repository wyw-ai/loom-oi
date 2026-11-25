package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type caller struct {
	Service  string `json:"vmcp_id"`
	Identity string `json:"access_identity"`
}
type invocation struct {
	Type     string          `json:"type"`
	Protocol int             `json:"protocol"`
	ID       string          `json:"call_id"`
	Config   json.RawMessage `json:"config"`
	Input    json.RawMessage `json:"input"`
	Handler  string          `json:"handler"`
	Caller   caller          `json:"context"`
}
type fault struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (f *fault) Error() string           { return f.Message }
func problem(code, message string) error { return &fault{code, message} }
func decode(raw json.RawMessage, value any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return problem("INVALID_ARGUMENT", "expected an object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return problem("INVALID_ARGUMENT", "invalid fields: "+err.Error())
	}
	return nil
}
func serve(input io.Reader, output io.Writer) error {
	sc := bufio.NewScanner(input)
	sc.Buffer(make([]byte, 32768), 1<<20)
	var start invocation
	if !sc.Scan() || json.Unmarshal(sc.Bytes(), &start) != nil || start.Type != "start" || start.Protocol != 1 {
		return errors.New("expected protocol 1 start")
	}
	svc, err := configure(start.Config)
	if err != nil {
		return err
	}
	var writer sync.Mutex
	send := func(frame any) { writer.Lock(); defer writer.Unlock(); _ = json.NewEncoder(output).Encode(frame) }
	reject := func(id string, err error) {
		failure := &fault{"APP_ERROR", err.Error()}
		if errors.Is(err, context.Canceled) {
			failure = &fault{"CANCELED", "operation canceled"}
		} else {
			var f *fault
			if errors.As(err, &f) {
				failure = f
			}
		}
		send(map[string]any{"type": "error", "call_id": id, "error": failure})
	}
	send(map[string]any{"type": "ready", "protocol": 1, "handlers": handlers})
	parent, stop := context.WithCancel(context.Background())
	var group sync.WaitGroup
	defer func() { stop(); group.Wait() }()
	var mu sync.Mutex
	active := map[string]context.CancelFunc{}
	for sc.Scan() {
		var msg invocation
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.ID == "" {
			reject("", problem("INVALID_ARGUMENT", "invalid frame"))
			continue
		}
		mu.Lock()
		if msg.Type == "cancel" {
			if cancel := active[msg.ID]; cancel != nil {
				cancel()
			}
			mu.Unlock()
			continue
		}
		if msg.Type != "invoke" || msg.Caller.Service == "" || msg.Caller.Identity == "" {
			mu.Unlock()
			reject(msg.ID, problem("ACCESS_DENIED", "trusted caller is required"))
			continue
		}
		if _, exists := active[msg.ID]; exists {
			mu.Unlock()
			reject(msg.ID, problem("CALL_ID_CONFLICT", "call already running"))
			continue
		}
		if len(active) >= 16 {
			mu.Unlock()
			reject(msg.ID, problem("CAPACITY_EXCEEDED", "worker is busy"))
			continue
		}
		ctx, cancel := context.WithCancel(parent)
		active[msg.ID] = cancel
		mu.Unlock()
		group.Add(1)
		// OIC omits the input field for an empty argument object.
		if msg.Input == nil {
			msg.Input = json.RawMessage("{}")
		}
		go func() {
			defer group.Done()
			defer cancel()
			defer func() { mu.Lock(); delete(active, msg.ID); mu.Unlock() }()
			result, err := svc.call(ctx, msg.Caller, msg.Handler, msg.Input)
			if err != nil {
				reject(msg.ID, err)
				return
			}
			frame := map[string]any{"type": "result", "call_id": msg.ID, "result": result}
			raw, err := json.Marshal(frame)
			if err != nil || len(raw)+1 >= 1<<20 {
				reject(msg.ID, problem("RESULT_TOO_LARGE", "result exceeds worker frame size"))
				return
			}
			send(json.RawMessage(raw))
		}()
	}
	return sc.Err()
}
func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
