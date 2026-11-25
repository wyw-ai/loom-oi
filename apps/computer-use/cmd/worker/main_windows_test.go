package main

import (
	"bufio"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Exercise the real start/invoke/result boundary and inspect the published file.
func TestScreenshotWorker(t *testing.T) {
	root := t.TempDir()
	input, commands := io.Pipe()
	replies, output := io.Pipe()
	defer commands.Close()
	defer replies.Close()
	go func() {
		err := serve(input, output, func() (*image.RGBA, error) {
			pic := image.NewRGBA(image.Rect(0, 0, 3, 2))
			pic.SetRGBA(2, 1, color.RGBA{21, 62, 34, 255})
			return pic, nil
		})
		output.CloseWithError(err)
		input.CloseWithError(err)
	}()
	enc := json.NewEncoder(commands)
	scanner := bufio.NewScanner(replies)
	run := func(request map[string]any) map[string]any {
		t.Helper()
		if err := enc.Encode(request); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() {
			t.Fatalf("missing response: %v", scanner.Err())
		}
		var response map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	ready := run(map[string]any{"type": "start", "protocol": 1, "config": map[string]string{"output_root": root}})
	if ready["type"] != "ready" {
		t.Fatal(ready)
	}
	req := map[string]any{"type": "invoke", "handler": "screenshot", "call_id": "one", "context": map[string]string{"access_identity": "tester", "vmcp_id": "workbench"}}
	var previous string
	for _, id := range []string{"one", "two"} {
		req["call_id"] = id
		response := run(req)
		if response["type"] != "result" || response["call_id"] != id {
			t.Fatal(response)
		}
		result := response["result"].(map[string]any)
		p := result["path"].(string)
		if filepath.ToSlash(filepath.Dir(p)) != "captures" || p == previous || result["width"] != 3.0 || result["height"] != 2.0 {
			t.Fatal(result)
		}
		previous = p
		file, err := os.Open(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		pic, err := png.Decode(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if c := color.RGBAModel.Convert(pic.At(2, 1)); c != (color.RGBA{21, 62, 34, 255}) {
			t.Fatal(c)
		}
	}
	req["input"] = map[string]string{"path": "outside.png"}
	rejected := run(req)
	if rejected["type"] != "error" || rejected["error"].(map[string]any)["code"] != "INVALID_ARGUMENT" {
		t.Fatal(rejected)
	}
}

func TestCanceledCaptureLeavesNoFile(t *testing.T) {
	dir, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = saveCapture(ctx, dir, func() (*image.RGBA, error) {
		cancel()
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil
	})
	if err != context.Canceled {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir.Name())
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}
