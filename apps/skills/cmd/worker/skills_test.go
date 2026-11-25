package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestDisclosureGrantsAndReload(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	full := caller{"reader", "consumer:reader:TEST"}
	limited := caller{"other", "consumer:other:TEST"}
	for _, base := range []string{workspace, home} {
		for _, name := range []string{".codex", ".agents"} {
			dir := filepath.Join(base, name, "skills", "same")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: shared-name\ndescription: |\n  Sample description\n---\n"+name), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "guide.txt"), []byte("initial"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	cfg, _ := json.Marshal(map[string]any{"workspace": workspace, "home": home, "allowed_callers": []map[string]any{
		{"service": full.Service, "identity": full.Identity}, {"service": limited.Service, "identity": limited.Identity, "root_ids": []string{"home-codex"}}}})
	lib, err := configure(cfg)
	if err != nil {
		t.Fatal(err)
	}
	call := func(c caller, name string, args map[string]any) (map[string]any, error) {
		raw, _ := json.Marshal(args)
		v, err := lib.call(context.Background(), c, name, raw)
		if err != nil {
			return nil, err
		}
		return v.(map[string]any), nil
	}
	all, err := call(full, "list_skills", map[string]any{})
	if err != nil || len(all["items"].([]descriptor)) != 4 {
		t.Fatal(all, err)
	}
	seen := map[string]bool{}
	page, err := call(full, "list_skills", map[string]any{"limit": 1, "product": "codex"})
	if err != nil {
		t.Fatal(err)
	}
	cursor := page["next_cursor"].(string)
	next, err := call(full, "list_skills", map[string]any{"limit": 1, "product": "codex", "cursor": cursor})
	if err != nil || next["next_cursor"] != nil {
		t.Fatal(next, err)
	}
	for _, p := range []map[string]any{page, next} {
		d := p["items"].([]descriptor)[0]
		if d.Product != "codex" || seen[d.Ref] {
			t.Fatal(d)
		}
		seen[d.Ref] = true
	}
	for _, tc := range []struct {
		c       caller
		product string
	}{{full, "shared"}, {limited, "codex"}} {
		if _, err = call(tc.c, "list_skills", map[string]any{"cursor": cursor, "product": tc.product}); err == nil {
			t.Fatal("cursor accepted across scope")
		}
	}
	ref := "workspace-codex/same"
	if _, err = call(limited, "read_skill", map[string]any{"ref": ref}); err == nil {
		t.Fatal("unauthorized root")
	}
	read, err := call(full, "read_skill", map[string]any{"ref": ref})
	if err != nil || !strings.Contains(read["text"].(string), ".codex") || !slices.Contains(read["resources"].([]string), "guide.txt") {
		t.Fatal(read, err)
	}
	source := filepath.Join(workspace, ".codex", "skills", "same")
	if err = os.WriteFile(filepath.Join(source, "guide.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	read, err = call(full, "read_skill", map[string]any{"ref": ref, "resource": "guide.txt"})
	if err != nil || read["text"] != "changed" {
		t.Fatal(read, err)
	}
	for _, path := range []string{"../same/SKILL.md", "/outside", "C:\\outside", "../other/file"} {
		if _, err = call(full, "read_skill", map[string]any{"ref": ref, "resource": path}); err == nil {
			t.Fatal("escaped with", path)
		}
	}
	if err = os.Symlink(filepath.Join(home, ".codex", "skills", "same", "guide.txt"), filepath.Join(source, "escape")); err == nil {
		if _, err = call(full, "read_skill", map[string]any{"ref": ref, "resource": "escape"}); err == nil {
			t.Fatal("escaped through linked file")
		}
	}
	if err = os.Remove(filepath.Join(source, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err = call(full, "read_skill", map[string]any{"ref": ref, "resource": "guide.txt"}); err == nil {
		t.Fatal("removed Skill remains readable")
	}
}

func TestLinkedRootCannotJoinAnotherGrant(t *testing.T) {
	workspace := t.TempDir()
	private := filepath.Join(workspace, ".codex", "skills", "internal")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "SKILL.md"), []byte("---\nname: private\ndescription: private\n---\nprivate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".codex", filepath.Join(workspace, ".agents")); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		// A directory junction exercises Windows reparse-point handling
		// even on hosts without the symlink creation privilege.
		cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/j", ".agents", ".codex")
		cmd.Dir = workspace
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create test junction: %s, %v", output, err)
		}
	}
	c := caller{"reader", "consumer:reader:TEST"}
	cfg, _ := json.Marshal(map[string]any{"workspace": workspace, "home": workspace, "allowed_callers": []map[string]any{
		{"service": c.Service, "identity": c.Identity, "root_ids": []string{"workspace-shared"}},
	}})
	lib, err := configure(cfg)
	if err != nil {
		t.Fatal(err)
	}
	value, err := lib.call(context.Background(), c, "list_skills", json.RawMessage("{}"))
	if err != nil || len(value.(map[string]any)["items"].([]descriptor)) != 0 {
		t.Fatal("listing followed a registered root link", err)
	}
	_, err = lib.call(context.Background(), c, "read_skill", json.RawMessage(`{"ref":"workspace-shared/internal"}`))
	if err == nil {
		t.Fatal("read followed a registered root link")
	}
}
