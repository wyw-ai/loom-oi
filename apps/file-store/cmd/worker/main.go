package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type frame struct {
	Type     string          `json:"type"`
	Protocol int             `json:"protocol"`
	ID       string          `json:"call_id"`
	Handler  string          `json:"handler"`
	Input    json.RawMessage `json:"input"`
	Config   json.RawMessage `json:"config"`
	Context  struct {
		Identity string `json:"access_identity"`
		VMCP     string `json:"vmcp_id"`
		Service  string `json:"service_id"`
	} `json:"context"`
}

func classify(err error) *failure {
	var f *failure
	if errors.As(err, &f) {
		return f
	}
	switch {
	case errors.Is(err, context.Canceled):
		return fail("CANCELED", "file operation canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return fail("TIMEOUT", "file operation timed out")
	case errors.Is(err, httpFailure(http.StatusNotFound)), errors.Is(err, os.ErrNotExist):
		return fail("NOT_FOUND", "file or directory not found")
	case errors.Is(err, httpFailure(http.StatusForbidden)), errors.Is(err, httpFailure(http.StatusUnauthorized)), errors.Is(err, os.ErrPermission):
		return fail("ACCESS_DENIED", "file access denied")
	default:
		return fail("STORAGE_ERROR", err.Error())
	}
}

func serve(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 32768), 1<<20)
	if !scanner.Scan() {
		return errors.New("expected start frame")
	}
	var start frame
	if err := json.Unmarshal(scanner.Bytes(), &start); err != nil || start.Type != "start" || start.Protocol != 1 {
		return errors.New("expected protocol 1 start")
	}
	var cfg config
	if err := decode(start.Config, &cfg); err != nil {
		return err
	}
	svc, err := openService(cfg)
	if err != nil {
		return err
	}
	defer svc.root.Close()
	var outputMu sync.Mutex
	encoder := json.NewEncoder(output)
	send := func(msg any) { outputMu.Lock(); defer outputMu.Unlock(); _ = encoder.Encode(msg) }
	reject := func(id string, err error) {
		send(map[string]any{"type": "error", "call_id": id, "error": classify(err)})
	}
	send(map[string]any{"type": "ready", "protocol": 1, "handlers": []string{"put_file", "list_directory", "lookup_file", "get_file"}})
	parent, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() { stop(); wg.Wait() }()
	var mu sync.Mutex
	calls := map[string]context.CancelFunc{}
	for scanner.Scan() {
		var req frame
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil || req.ID == "" {
			reject("", fail("INVALID_ARGUMENT", "invalid call frame"))
			continue
		}
		mu.Lock()
		if req.Type == "cancel" {
			if cancel := calls[req.ID]; cancel != nil {
				cancel()
			}
			mu.Unlock()
			continue
		}
		if req.Type != "invoke" || req.Context.Identity == "" || (req.Context.VMCP == "" && req.Context.Service == "") {
			mu.Unlock()
			reject(req.ID, fail("INVALID_ARGUMENT", "trusted invocation context required"))
			continue
		}
		if _, ok := calls[req.ID]; ok {
			mu.Unlock()
			reject(req.ID, fail("CALL_ID_CONFLICT", "call ID already running"))
			continue
		}
		if len(calls) >= 16 {
			mu.Unlock()
			reject(req.ID, fail("CAPACITY_EXCEEDED", "concurrency limit reached"))
			continue
		}
		ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
		calls[req.ID] = cancel
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			defer func() { mu.Lock(); delete(calls, req.ID); mu.Unlock() }()
			grant := req.Context.Service
			if grant == "" {
				grant = req.Context.VMCP
			}
			val, err := svc.call(ctx, namespace(cfg.Store, grant, req.Context.Identity), req.Handler, req.Input)
			if err != nil {
				reject(req.ID, err)
				return
			}
			send(map[string]any{"type": "result", "call_id": req.ID, "result": val})
		}()
	}
	return scanner.Err()
}

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
