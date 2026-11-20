package filesystem

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreflightBoundsAndStreaming(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false, "limits": Object{"max_result_frame_bytes": 65536}})
	component := strings.Repeat("a", 38)
	long := strings.Repeat(component+"/", 60) + "end.txt"
	wantError(t, s, "write_file", Object{"path": long, "create_parents": true, "content": "x"}, "RESULT_TOO_LARGE")
	if _, err := os.Stat(filepath.Join(dir, component)); !os.IsNotExist(err) {
		t.Fatalf("preflight created a directory: %v", err)
	}
	data := []byte(strings.Repeat("x", 2<<20) + "tail\n")
	if err := os.WriteFile(filepath.Join(dir, "log"), data, 0600); err != nil {
		t.Fatal(err)
	}
	first := ok(t, s, "read_file", Object{"path": "log", "max_bytes": 9})
	if first["revision"] != nil || first["text"] != "xxxxxxxxx" {
		t.Fatal(first)
	}
	_, e := s.Call(context.Background(), Request{"other", "read_file", "other identity", "vmcp", Object{"path": "log", "cursor": first["next_cursor"]}})
	if e == nil || e.Code != "STALE_CURSOR" {
		t.Fatal(e)
	}
	s2, err := Open(Object{"root": dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	wantError(t, s2, "read_file", Object{"path": "log", "cursor": first["next_cursor"]}, "CURSOR_EXPIRED")
	wantError(t, s, "write_file", Object{"path": "z", "content": strings.Repeat("界", 45000)}, "FILE_TOO_LARGE")
}
func TestRejectHardLinkedTargets(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false})
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "file"), filepath.Join(dir, "alias")); err != nil {
		t.Skip(err)
	}
	wantError(t, s, "write_file", Object{"path": "file", "mode": "replace", "expected_revision": revision([]byte("a")), "content": "b"}, "UNSUPPORTED_FILE_ATTRIBUTES")
}

func TestCancellationWhileWaitingForWriteSlot(t *testing.T) {
	s, dir := newService(t, Object{"read_only": false})
	s.writeSlot <- struct{}{}
	t.Cleanup(func() { <-s.writeSlot })
	ctx, cancel := context.WithCancel(context.Background())
	q := Request{
		ID: "pending-write", Handler: "write_file", Identity: "tester", VMCP: "scope",
		Input: Object{"path": "new/private.txt", "content": "pending", "create_parents": true},
	}
	done := make(chan *Failure, 1)
	go func() { _, failure := s.Call(ctx, q); done <- failure }()
	cancel()
	select {
	case failure := <-done:
		if failure == nil || failure.Code != "CANCELLED" || failure.Details["mutation_state"] != "not_applied" {
			t.Fatalf("unexpected cancellation outcome: %#v", failure)
		}
	case <-time.After(time.Second):
		t.Fatal("a waiting writer ignored cancellation")
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); !os.IsNotExist(err) {
		t.Fatalf("unexpected parent on cancellation: %v", err)
	}
}
func TestEXIFMappingsAndAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	for i := range 6 {
		src.SetNRGBA(i%2, i/2, color.NRGBA{R: uint8(i + 1), G: 127, A: uint8(i * 40)})
	}
	expected := []string{"123456", "214365", "654321", "563412", "135246", "531642", "642531", "246135"}
	for idx, want := range expected {
		im, e := orientImage(context.Background(), src, idx+1)
		if e != nil {
			t.Fatal(e)
		}
		var got strings.Builder
		for y := 0; y < im.Bounds().Dy(); y++ {
			for x := 0; x < im.Bounds().Dx(); x++ {
				c := color.NRGBAModel.Convert(im.At(x, y)).(color.NRGBA)
				if c.G != 127 {
					t.Fatal("lost alpha or RGB channels", idx, c)
				}
				got.WriteByte('0' + c.R)
			}
		}
		if got.String() != want {
			t.Errorf("orientation %d got %s want %s", idx+1, got.String(), want)
		}
	}
	// Insert a valid PNG eXIf chunk after IHDR. Orientation 6 rotates 2x3 to 3x2.
	var out bytes.Buffer
	png.Encode(&out, src)
	exif := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	chunk := make([]byte, 12+len(exif))
	binary.BigEndian.PutUint32(chunk, uint32(len(exif)))
	copy(chunk[4:], "eXIf")
	copy(chunk[8:], exif)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	raw := append(bytes.Clone(out.Bytes()[:33]), chunk...)
	raw = append(raw, out.Bytes()[33:]...)
	s, dir := newService(t, nil)
	if err := os.WriteFile(filepath.Join(dir, "rotate"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	result := ok(t, s, "view_image", Object{"path": "rotate", "crop": Object{"x": 2, "y": 1, "width": 1, "height": 1}})
	meta := result["structuredContent"].(Object)
	if meta["orientation_applied"] != true || meta["source_width"] != 3 || meta["source_height"] != 2 {
		t.Fatal(meta)
	}
}

func TestStaticAndAnimatedContainers(t *testing.T) {
	s, dir := newService(t, nil)
	for _, tc := range []struct {
		name, encoded string
		animated      bool
	}{
		{"static.webp", "UklGRigAAABXRUJQVlA4TBsAAAAvAUAAEA/wgrkj0n4G/rjI4QGFAAKgoInofywA", false},
		{"animated.webp", "UklGRowAAABXRUJQVlA4WAoAAAASAAAAAQAAAQAAQU5JTQYAAAAAAAAAAABBTk1GMAAAAAAAAAAAAAEAAAEAAAAAAAJWUDhMFwAAAC8BQAAQD3C8xvPgfaT5SICBhxGI6H8IAEFOTUYoAAAAAAAAAAAAAQAAAQAAAAAAAFZQOEwPAAAALwFAAAAHUMCI/gciov8BAA==", true},
		{"animated.png", "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAACGFjVEwAAAACAAAAAPONk3AAAAAaZmNUTAAAAAAAAAACAAAAAgAAAAAAAAAAAAAAAQAA2WEaUQAAABdJREFUeJxjtKlQbmBgYGBgAhEVvWc9ABooA3XMUMebAAAAGmZjVEwAAAABAAAAAgAAAAIAAAAAAAAAAAAAAAEAAEIS8IUAAAAZZmRBVAAAAAJ4nGNkaGD4z8DAwMAEIkAYABYoAYP4zf4DAAAAAElFTkSuQmCC", true},
	} {
		b, err := base64.StdEncoding.DecodeString(tc.encoded)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, tc.name), b, 0600); err != nil {
			t.Fatal(err)
		}
		input := Object{"path": tc.name}
		if tc.animated {
			wantError(t, s, "view_image", input, "UNSUPPORTED_IMAGE")
		} else {
			m := ok(t, s, "view_image", input)["structuredContent"].(Object)
			if m["source_mime"] != "image/webp" || m["reencoded"] != false {
				t.Fatal(m)
			}
		}
	}
}
