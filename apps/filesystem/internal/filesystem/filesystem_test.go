package filesystem

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wyw-ai/loom-oi/apps/filesystem/contract"
)

var outputSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	b, err := os.ReadFile("../../contract/schemas.json")
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	const resource = "https://loom-oi.invalid/filesystem.schema.json"
	if err := compiler.AddResource(resource, doc); err != nil {
		return nil, err
	}
	compiled := map[string]*jsonschema.Schema{}
	for _, name := range append([]string{"error"}, contract.Handlers...) {
		def := name + "_output"
		switch name {
		case "edit_file", "write_file":
			def = "mutation_output"
		case "error":
			def = name
		}
		schema, err := compiler.Compile(resource + "#/$defs/" + def)
		if err != nil {
			return nil, err
		}
		compiled[name] = schema
	}
	return compiled, nil
})

func verifyOutput(t *testing.T, key string, value any) {
	t.Helper()
	schemas, err := outputSchemas()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := schemas[key].Validate(decoded); err != nil {
		t.Fatalf("%s output: %v", key, err)
	}
}

func newService(t *testing.T, options Object) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	if options == nil {
		options = Object{}
	}
	options["root"] = dir
	s, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}
func run(t *testing.T, s *Service, handler string, input Object) (Object, *Failure) {
	t.Helper()
	b, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := contract.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return s.Call(context.Background(), Request{"call-1", handler, "identity", "vmcp", parsed.(Object)})
}
func ok(t *testing.T, s *Service, handler string, input Object) Object {
	t.Helper()
	out, e := run(t, s, handler, input)
	if e != nil {
		t.Fatalf("%s: %v (%v)", handler, e, e.Details)
	}
	verifyOutput(t, handler, out)
	return out
}
func wantError(t *testing.T, s *Service, handler string, input Object, code string) {
	t.Helper()
	_, e := run(t, s, handler, input)
	if e == nil || e.Code != code {
		t.Fatalf("%s expected %s, got %v", handler, code, e)
	}
	verifyOutput(t, "error", e)
}
func TestFileActions(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false})
	path := "docs/note.txt"
	text := "\ufeff你好\r\nprice 12\n"
	r := ok(t, s, "write_file", Object{"path": path, "content": text, "create_parents": true})
	if r["state"] != "applied" || r["created"] != true || r["revision"] != revision([]byte(text)) {
		t.Fatal(r)
	}
	wantError(t, s, "write_file", Object{"path": path, "content": "x"}, "FILE_EXISTS")
	wantError(t, s, "write_file", Object{"path": path, "content": "x", "mode": "replace"}, "INVALID_ARGUMENT")
	page := ok(t, s, "read_file", Object{"path": path, "max_bytes": 4})
	if page["text"] != "你" || page["bom"] != true {
		t.Fatal(page)
	}
	next := ok(t, s, "read_file", Object{"path": path, "cursor": page["next_cursor"], "max_lines": 1})
	if next["text"] != "好\r\n" {
		t.Fatal(next)
	}
	edits := []Object{{"old_text": "price 12", "new_text": "price 34"}}
	written := ok(t, s, "edit_file", Object{"path": path, "edits": edits, "expected_revision": r["revision"]})
	if written["revision"] != revision([]byte(strings.ReplaceAll(text, "12", "34"))) {
		t.Fatal(written)
	}
	wantError(t, s, "read_file", Object{"path": path, "cursor": page["next_cursor"]}, "STALE_CURSOR")
	wantError(t, s, "edit_file", Object{"path": path, "edits": edits, "expected_revision": r["revision"]}, "REVISION_CONFLICT")
	noop := ok(t, s, "write_file", Object{"path": path, "mode": "replace", "content": strings.ReplaceAll(text, "12", "34"), "expected_revision": written["revision"]})
	if noop["state"] != "unchanged" || noop["bytes_written"] != 0 {
		t.Fatal(noop)
	}
	// A failed edit leaves the earlier, matching edits unapplied.
	wantError(t, s, "edit_file", Object{"path": path, "expected_revision": written["revision"], "edits": []Object{{"old_text": "price", "new_text": "x"}, {"old_text": "missing", "new_text": "z"}}}, "EDIT_NOT_FOUND")
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
	if err != nil || string(b) != strings.ReplaceAll(text, "12", "34") {
		t.Fatal(string(b), err)
	}
	listing := ok(t, s, "list_directory", Object{"path": "docs"})
	if len(listing["entries"].([]Object)) != 1 || listing["complete"] != true {
		t.Fatal(listing)
	}
	found := ok(t, s, "find_files", Object{"pattern": "**/*.txt"})
	if len(found["files"].([]Object)) != 1 {
		t.Fatal(found)
	}
	matched := ok(t, s, "search_content", Object{"query": "PRICE", "case_sensitive": false, "context_lines": 1})
	hits := matched["matches"].([]Object)
	if len(hits) != 1 || hits[0]["line"] != 2 || hits[0]["match_start_byte"] != 0 {
		t.Fatal(matched)
	}
}
func TestConfinementAndConflictingWrites(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false})
	os.WriteFile(filepath.Join(dir, "file"), []byte("one"), 0600)
	r := ok(t, s, "read_file", Object{"path": "file"})
	wantError(t, s, "read_file", Object{"path": "../escape"}, "PATH_OUTSIDE_ROOT")
	wantError(t, s, "read_file", Object{"path": "file", "Path": "file"}, "INVALID_ARGUMENT")
	wantError(t, s, "read_file", Object{"path": "file", "cursor": "x", "start_line": 3}, "INVALID_ARGUMENT")
	var wg sync.WaitGroup
	results := make(chan *Failure, 2)
	for _, content := range []string{"left", "right"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Call(context.Background(), Request{ID: content, Handler: "write_file", Identity: "same", VMCP: "vmcp", Input: Object{"path": "file", "mode": "replace", "content": content, "expected_revision": r["revision"]}})
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	applied, conflicts := 0, 0
	for e := range results {
		if e == nil {
			applied++
		} else if e.Code == "REVISION_CONFLICT" {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if applied != 1 || conflicts != 1 {
		t.Fatal(applied, conflicts)
	}
	ro, _ := newService(t, nil)
	wantError(t, ro, "write_file", Object{"path": "n", "content": "v"}, "READ_ONLY")
}
func TestSearchIgnoresAndDirectoryCursors(t *testing.T) {
	s, dir := newService(t, nil)
	for name, content := range map[string]string{".gitignore": "skip*\n*.tmp\n!keep.tmp\n", "keep.tmp": "yes", "omit.tmp": "yes", "x": "yes", "skip.txt": "yes", "z.bin": "yes\x00"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := ok(t, s, "search_content", Object{"query": "yes", "mode": "literal"})
	if len(r["matches"].([]Object)) != 2 || r["status"].(Object)["complete"] != true {
		t.Fatal(r)
	}
	page := ok(t, s, "list_directory", Object{"limit": 1})
	if page["complete"] != false {
		t.Fatal(page)
	}
	p2 := ok(t, s, "list_directory", Object{"cursor": page["next_cursor"], "limit": 1})
	if p2["next_cursor"] == nil {
		t.Fatal(p2)
	}
	os.WriteFile(filepath.Join(dir, "changed"), []byte{}, 0600)
	wantError(t, s, "list_directory", Object{"cursor": page["next_cursor"]}, "STALE_CURSOR")
}
func TestImageCropAndOriginalBudget(t *testing.T) {
	s, dir := newService(t, nil)
	im := image.NewNRGBA(image.Rect(0, 0, 1920, 1080))
	var state uint32 = 17
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			c := color.NRGBA{255, 255, 255, 255}
			if y < 76 {
				state = 1664525*state + 1013904223
				c = color.NRGBA{byte(state >> 24), byte(state >> 16), byte(state >> 8), 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, im); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() <= 384<<10 || encoded.Len() >= 550<<10 {
		t.Fatal(encoded.Len())
	}
	if err := os.WriteFile(filepath.Join(dir, "picture"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	full := ok(t, s, "view_image", Object{"path": "picture"})
	meta := full["structuredContent"].(Object)
	if meta["resized"] != false || meta["reencoded"] != false || meta["encoded_bytes"] != encoded.Len() {
		t.Fatal(meta)
	}
	crop := ok(t, s, "view_image", Object{"path": "picture", "crop": Object{"x": 15, "y": 4, "width": 20, "height": 10}})
	m := crop["structuredContent"].(Object)
	if m["resized"] != false || m["reencoded"] != true {
		t.Fatal(m)
	}
	block := crop["content"].([]Object)[0]
	b, err := base64.StdEncoding.DecodeString(block["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	out, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			if color.NRGBAModel.Convert(out.At(x, y)) != im.NRGBAAt(x+15, y+4) {
				t.Fatal(x, y)
			}
		}
	}
	wantError(t, s, "view_image", Object{"path": "picture", "crop": Object{"x": 1919, "y": 0, "width": 2, "height": 1}}, "CROP_OUT_OF_BOUNDS")
}
