package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type frame struct {
	Type     string `json:"type"`
	Protocol int    `json:"protocol"`
	Config   struct {
		OutputRoot string `json:"output_root"`
	} `json:"config"`
	ID      string         `json:"call_id"`
	Handler string         `json:"handler"`
	Input   map[string]any `json:"input"`
	Context struct {
		Identity string `json:"access_identity"`
		VMCP     string `json:"vmcp_id"`
		Service  string `json:"service_id"`
	} `json:"context"`
}

type captureResult struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type cancelWriter struct {
	ctx context.Context
	w   io.Writer
}

func (w cancelWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

func saveCapture(ctx context.Context, dir *os.Root, capture func() (*image.RGBA, error)) (captureResult, error) {
	var result captureResult
	img, err := capture()
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	name := rand.Text()
	partial := name + ".part"
	f, err := dir.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	defer dir.Remove(partial)
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	err = enc.Encode(cancelWriter{ctx, f}, img)
	closeErr := f.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	filename := name + ".png"
	if err := dir.Rename(partial, filename); err != nil {
		return result, err
	}
	return captureResult{"captures/" + filename, img.Bounds().Dx(), img.Bounds().Dy()}, nil
}

func serve(input io.Reader, output io.Writer, capture func() (*image.RGBA, error)) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	if !scanner.Scan() {
		return errors.New("expected start frame")
	}
	var start frame
	if err := json.Unmarshal(scanner.Bytes(), &start); err != nil {
		return err
	}
	if start.Type != "start" || start.Protocol != 1 || !filepath.IsAbs(start.Config.OutputRoot) {
		return errors.New("protocol 1 start with absolute config.output_root required")
	}
	root, err := os.OpenRoot(start.Config.OutputRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll("captures", 0700); err != nil {
		return err
	}
	dir, err := root.OpenRoot("captures")
	if err != nil {
		return err
	}
	defer dir.Close()

	var outMu sync.Mutex
	encoder := json.NewEncoder(output)
	send := func(value any) error {
		outMu.Lock()
		defer outMu.Unlock()
		return encoder.Encode(value)
	}
	fail := func(id, code, message string) {
		_ = send(map[string]any{"type": "error", "call_id": id, "error": map[string]string{"code": code, "message": message}})
	}
	if err := send(map[string]any{"type": "ready", "protocol": 1, "handlers": []string{"screenshot"}}); err != nil {
		return err
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var activeID string
	var cancel context.CancelFunc
	defer func() {
		mu.Lock()
		if cancel != nil {
			cancel()
		}
		mu.Unlock()
		wg.Wait()
	}()
	for scanner.Scan() {
		var req frame
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return err
		}
		if req.ID == "" {
			return errors.New("missing call_id")
		}
		if req.Type == "cancel" {
			mu.Lock()
			if activeID == req.ID && cancel != nil {
				cancel()
			}
			mu.Unlock()
			continue
		}
		if req.Type != "invoke" || req.Handler != "screenshot" || len(req.Input) != 0 || req.Context.Identity == "" || (req.Context.VMCP == "" && req.Context.Service == "") {
			fail(req.ID, "INVALID_ARGUMENT", "screenshot requires trusted context and accepts no arguments")
			continue
		}
		mu.Lock()
		if activeID != "" {
			same := activeID == req.ID
			mu.Unlock()
			if !same {
				fail(req.ID, "CAPACITY_EXCEEDED", "a capture is already in progress")
			}
			continue
		}
		ctx, stop := context.WithCancel(context.Background())
		activeID, cancel = req.ID, stop
		mu.Unlock()
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer stop()
			result, err := saveCapture(ctx, dir, capture)
			mu.Lock()
			activeID, cancel = "", nil
			mu.Unlock()
			if errors.Is(err, context.Canceled) {
				fail(id, "CANCELED", "capture canceled")
				return
			}
			if err != nil {
				fail(id, "CAPTURE_FAILED", err.Error())
				return
			}
			_ = send(map[string]any{"type": "result", "call_id": id, "result": result})
		}(req.ID)
	}
	return scanner.Err()
}

func main() {
	if err := serve(os.Stdin, os.Stdout, captureDesktop); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
