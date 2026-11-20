// oiapp-filesystem exposes a single fixed filesystem root over the OIC JSONL protocol.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wyw-ai/loom-oi/apps/filesystem/contract"
	fs "github.com/wyw-ai/loom-oi/apps/filesystem/internal/filesystem"
)

type pending struct {
	signature string
	cancel    context.CancelFunc
}

func serve(input io.Reader, output, diagnostics io.Writer) error {
	reader := bufio.NewReaderSize(input, 1<<20)
	logger := log.New(diagnostics, "", 0)
	var svc *fs.Service
	var wg sync.WaitGroup
	var callsMu, outMu sync.Mutex
	calls := map[string]pending{}
	defer func() {
		callsMu.Lock()
		for _, call := range calls {
			call.cancel()
		}
		callsMu.Unlock()
		wg.Wait()
		if svc != nil {
			svc.Close()
		}
	}()
	write := func(b []byte) {
		outMu.Lock()
		defer outMu.Unlock()
		output.Write(append(b, '\n'))
	}
	send := func(frame fs.Object) {
		b, err := json.Marshal(frame)
		if err == nil {
			write(b)
		}
	}
	reject := func(id, code, message string, handler ...string) {
		failure := fs.Failure{Code: code, Message: message}
		if len(handler) > 0 && (handler[0] == "write_file" || handler[0] == "edit_file") {
			failure.Details = fs.Object{"mutation_state": "not_applied", "created_directories": []string{}}
		}
		send(fs.Object{"type": "error", "call_id": id, "error": failure})
	}
	for {
		raw, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = reader.ReadSlice('\n')
			}
			if svc == nil {
				return errors.New("oversized start frame")
			}
			reject("", "FRAME_TOO_LARGE", "worker frame exceeds 1 MiB")
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			continue
		}
		if len(raw) == 0 && err == io.EOF {
			return nil
		}
		if err != nil && err != io.EOF {
			return err
		}
		val, err := contract.Parse(bytes.TrimSpace(raw))
		if err != nil {
			if svc == nil {
				return err
			}
			reject("", "INVALID_ARGUMENT", "invalid JSON frame")
			continue
		}
		obj, ok := val.(map[string]any)
		if !ok {
			if svc == nil {
				return errors.New("start frame must be an object")
			}
			reject("", "INVALID_ARGUMENT", "worker frame must be an object")
			continue
		}
		kind, _ := obj["type"].(string)
		id, _ := obj["call_id"].(string)
		if svc == nil {
			if kind != "start" || obj["protocol"] != json.Number("1") {
				return errors.New("expected protocol 1 start")
			}
			formats, _ := obj["result_formats"].([]any)
			if !slices.Contains(formats, any("json")) || !slices.Contains(formats, any("content.v1")) {
				return errors.New("RESULT_FORMAT_UNSUPPORTED: filesystem requires json and content.v1")
			}
			cfg, _ := obj["config"].(map[string]any)
			svc, err = fs.Open(cfg)
			if err != nil {
				return err
			}
			send(fs.Object{"type": "ready", "protocol": 1, "handlers": contract.Handlers, "result_formats": []string{"json", "content.v1"}})
			continue
		}
		if id == "" || len(id) > 4096 {
			reject("", "INVALID_ARGUMENT", "missing or oversized call_id")
			continue
		}
		if kind == "cancel" {
			callsMu.Lock()
			if entry, ok := calls[id]; ok {
				entry.cancel()
			}
			callsMu.Unlock()
			continue
		}
		if kind != "invoke" {
			reject(id, "INVALID_ARGUMENT", "expected invoke or cancel")
			continue
		}
		handler, _ := obj["handler"].(string)
		args, _ := obj["input"].(map[string]any)
		if _, present := obj["input"]; !present {
			// OIC's JSONL Frame omits empty input maps.
			args = fs.Object{}
		}
		scope, _ := obj["context"].(map[string]any)
		identity, _ := scope["access_identity"].(string)
		vmcp, _ := scope["vmcp_id"].(string)
		if identity == "" || vmcp == "" || args == nil {
			reject(id, "INVALID_ARGUMENT", "trusted context and input object required", handler)
			continue
		}
		signature, _ := json.Marshal([]any{handler, args, scope})
		callsMu.Lock()
		if prior, ok := calls[id]; ok {
			callsMu.Unlock()
			if string(signature) != prior.signature {
				reject(id, "CALL_ID_CONFLICT", "call ID already in use with different arguments", handler)
			}
			continue
		}
		if len(calls) >= svc.Concurrency() {
			callsMu.Unlock()
			reject(id, "CAPACITY_EXCEEDED", "worker concurrency limit reached", handler)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), svc.Timeout())
		calls[id] = pending{string(signature), cancel}
		callsMu.Unlock()
		q := fs.Request{ID: id, Handler: handler, Identity: identity, VMCP: vmcp, Input: args}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			defer func() { callsMu.Lock(); delete(calls, q.ID); callsMu.Unlock() }()
			started := time.Now()
			result, e := svc.Call(ctx, q)
			frame := fs.Object{"type": "result", "call_id": q.ID, "result": result}
			if e != nil {
				frame = fs.Object{"type": "error", "call_id": q.ID, "error": e}
			} else if q.Handler == "view_image" {
				frame["result_format"] = "content.v1"
			}
			encoded, err := json.Marshal(frame)
			if err != nil || len(encoded)+1 > svc.FrameLimit() {
				reject(q.ID, "RESULT_TOO_LARGE", "serialized response exceeds budget")
				return
			}
			write(encoded)
			info := fs.Object{"action": bounded(q.Handler), "call_id": bounded(q.ID), "duration_ms": time.Since(started).Milliseconds(), "frame_bytes": len(encoded) + 1}
			if e != nil {
				info["error_code"] = e.Code
			}
			for _, k := range []string{"entries", "files", "matches"} {
				if rows, ok := result[k].([]fs.Object); ok {
					info["items"] = len(rows)
					break
				}
			}
			entry, _ := json.Marshal(info)
			logger.Print(string(entry))
		}()
	}
}

func bounded(s string) string {
	if len(s) <= 160 {
		return s
	}
	end := 160
	for !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

func main() {
	if err := serve(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
