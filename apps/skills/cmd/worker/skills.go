package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

var handlers = []string{"list_skills", "read_skill"}
var tag = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$")

type rootConfig struct {
	ID      string `json:"root_id"`
	Scope   string `json:"scope"`
	Product string `json:"product"`
	Path    string `json:"path"`
}
type readerGrant struct {
	Service  string    `json:"service"`
	Identity string    `json:"identity"`
	Roots    *[]string `json:"root_ids"`
}
type library struct {
	bases     map[string]string
	roots     []rootConfig
	readers   map[caller]readerGrant
	cursorKey []byte
}

func portablePath(name string) bool {
	return fs.ValidPath(name) && name != "." && !strings.ContainsAny(name, "\\:\x00") && filepath.IsLocal(filepath.FromSlash(name))
}
func configure(raw json.RawMessage) (*library, error) {
	var cfg struct {
		Workspace string        `json:"workspace"`
		Home      string        `json:"home"`
		Roots     *[]rootConfig `json:"roots"`
		Readers   []readerGrant `json:"allowed_callers"`
	}
	if err := decode(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Home == "" {
		var err error
		cfg.Home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	if !filepath.IsAbs(cfg.Workspace) || !filepath.IsAbs(cfg.Home) {
		return nil, fmt.Errorf("workspace and home must be absolute")
	}
	roots := []rootConfig{}
	for _, scope := range []string{"workspace", "home"} {
		for _, pair := range [][2]string{{"shared", ".agents/skills"}, {"codex", ".codex/skills"}} {
			roots = append(roots, rootConfig{scope + "-" + pair[0], scope, pair[0], pair[1]})
		}
	}
	if cfg.Roots != nil {
		roots = *cfg.Roots
	}
	ids := map[string]bool{}
	for _, r := range roots {
		if !tag.MatchString(r.ID) || ids[r.ID] || !tag.MatchString(r.Product) || (r.Scope != "workspace" && r.Scope != "home") || !portablePath(r.Path) {
			return nil, fmt.Errorf("invalid or duplicate Skill root")
		}
		ids[r.ID] = true
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].ID < roots[j].ID })
	lib := &library{bases: map[string]string{"workspace": cfg.Workspace, "home": cfg.Home}, roots: roots, readers: map[caller]readerGrant{}, cursorKey: make([]byte, 32)}
	if _, err := rand.Read(lib.cursorKey); err != nil {
		return nil, err
	}
	for _, r := range cfg.Readers {
		key := caller{r.Service, r.Identity}
		if !tag.MatchString(r.Service) || r.Identity == "" {
			return nil, fmt.Errorf("invalid allowed caller")
		}
		if _, exists := lib.readers[key]; exists {
			return nil, fmt.Errorf("duplicate allowed caller")
		}
		if r.Roots != nil {
			for _, id := range *r.Roots {
				if !ids[id] {
					return nil, fmt.Errorf("unknown granted root")
				}
			}
		}
		lib.readers[key] = r
	}
	return lib, nil
}

type descriptor struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Scope       string `json:"scope" yaml:"-"`
	Product     string `json:"product" yaml:"-"`
	RootID      string `json:"root_id" yaml:"-"`
	Ref         string `json:"ref" yaml:"-"`
}

func reference(root, pkg string) string     { return root + "/" + pkg }
func allowed(r readerGrant, id string) bool { return r.Roots == nil || slices.Contains(*r.Roots, id) }
func (l *library) openRoot(r rootConfig) (*os.Root, error) {
	dir, err := os.OpenRoot(l.bases[r.Scope])
	if err != nil {
		return nil, err
	}
	// A link inside the workspace may lead to another registered root
	// for which this caller has no grant. Open each actual directory.
	for _, part := range strings.Split(r.Path, "/") {
		next, err := openDirectory(dir, part)
		dir.Close()
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}
func openDirectory(root *os.Root, name string) (*os.Root, error) {
	if !portablePath(name) || strings.Contains(name, "/") {
		return nil, fs.ErrInvalid
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fs.ErrNotExist
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		child.Close()
		return nil, fs.ErrNotExist
	}
	return child, nil
}
func textFile(pkg *os.Root, name string) (*os.File, error) {
	if !portablePath(name) {
		return nil, fs.ErrInvalid
	}
	name = filepath.FromSlash(name)
	info, err := pkg.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	f, err := pkg.Open(name)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		f.Close()
		return nil, fs.ErrInvalid
	}
	return f, nil
}
func metadata(pkg *os.Root) (descriptor, error) {
	file, err := textFile(pkg, "SKILL.md")
	if err != nil {
		return descriptor{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, 64<<10))
	if !scanner.Scan() || strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\uFEFF")) != "---" {
		return descriptor{}, fs.ErrInvalid
	}
	var header strings.Builder
	for scanner.Scan() {
		if strings.TrimRight(scanner.Text(), " \r\t") == "---" {
			var d descriptor
			if err = yaml.Unmarshal([]byte(header.String()), &d); err != nil {
				return d, err
			}
			if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Description) == "" {
				return d, fs.ErrInvalid
			}
			return d, nil
		}
		header.WriteString(scanner.Text())
		header.WriteByte('\n')
	}
	return descriptor{}, fs.ErrInvalid
}
func (l *library) entries(ctx context.Context, r rootConfig, after string, limit int) ([]descriptor, error) {
	lastRoot, lastName, _ := strings.Cut(after, "/")
	if r.ID < lastRoot {
		return nil, nil
	}
	root, err := l.openRoot(r)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	items, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	var result []descriptor
	for _, entry := range items {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		ref := reference(r.ID, name)
		if !entry.IsDir() || (r.ID == lastRoot && name <= lastName) {
			continue
		}
		pkg, err := openDirectory(root, name)
		if err != nil {
			continue
		}
		d, err := metadata(pkg)
		pkg.Close()
		if err != nil {
			continue
		}
		d.Scope, d.Product, d.RootID, d.Ref = r.Scope, r.Product, r.ID, ref
		result = append(result, d)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}
func (l *library) cursor(c caller, product, ref string) string {
	mac := hmac.New(sha256.New, l.cursorKey)
	fmt.Fprintf(mac, "%s\x00%s\x00%s\x00%s", c.Service, c.Identity, product, ref)
	return base64.RawURLEncoding.EncodeToString([]byte(ref)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (l *library) list(ctx context.Context, c caller, rule readerGrant, raw json.RawMessage) (any, error) {
	var args struct {
		Product string `json:"product"`
		Limit   *int   `json:"limit"`
		Cursor  string `json:"cursor"`
	}
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	limit := 50
	if args.Limit != nil {
		limit = *args.Limit
	}
	if limit < 1 || limit > 200 {
		return nil, problem("INVALID_ARGUMENT", "limit must be 1..200")
	}
	after := ""
	if args.Cursor != "" {
		key, _, ok := strings.Cut(args.Cursor, ".")
		ref, err := base64.RawURLEncoding.DecodeString(key)
		after = string(ref)
		if !ok || err != nil || !hmac.Equal([]byte(args.Cursor), []byte(l.cursor(c, args.Product, after))) {
			return nil, problem("INVALID_ARGUMENT", "invalid cursor for this listing")
		}
	}
	out := []descriptor{}
	for _, root := range l.roots {
		if !allowed(rule, root.ID) || (args.Product != "" && args.Product != root.Product) {
			continue
		}
		rows, err := l.entries(ctx, root, after, limit+1-len(out))
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(out) > limit {
			return map[string]any{"items": out[:limit], "next_cursor": l.cursor(c, args.Product, out[limit-1].Ref)}, nil
		}
	}
	return map[string]any{"items": out}, nil
}
func readText(pkg *os.Root, path string) (string, error) {
	f, err := textFile(pkg, path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return "", err
	}
	if len(content) > 1<<20 {
		return "", problem("RESULT_TOO_LARGE", "resource exceeds worker frame")
	}
	if !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
		return "", problem("INVALID_ARGUMENT", "resource must be UTF-8 text")
	}
	return string(content), nil
}
func resources(ctx context.Context, pkg *os.Root) ([]string, error) {
	paths := []string{}
	err := fs.WalkDir(pkg.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || path == "SKILL.md" || !portablePath(path) {
			return nil
		}
		// Sample a short prefix for the index. The requested resource is
		// checked as UTF-8 in full only when a reader opens it.
		f, err := textFile(pkg, path)
		if err != nil {
			return nil
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.Size() > 1<<20 {
			return nil
		}
		prefix, err := io.ReadAll(io.LimitReader(f, 512))
		if err != nil || strings.IndexByte(string(prefix), 0) >= 0 {
			return nil
		}
		// The final code point of a sampled text prefix can be incomplete.
		if info.Size() > int64(len(prefix)) {
			for end := len(prefix); end > 0 && end > len(prefix)-utf8.UTFMax; end-- {
				if utf8.RuneStart(prefix[end-1]) {
					if !utf8.FullRune(prefix[end-1:]) {
						prefix = prefix[:end-1]
					}
					break
				}
			}
		}
		if utf8.Valid(prefix) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}
func (l *library) read(ctx context.Context, rule readerGrant, raw json.RawMessage) (any, error) {
	var args struct {
		Ref      string `json:"ref"`
		Resource string `json:"resource"`
	}
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	rootID, name, ok := strings.Cut(args.Ref, "/")
	if !ok || !portablePath(name) || strings.Contains(name, "/") || (args.Resource != "" && !portablePath(args.Resource)) {
		return nil, problem("INVALID_ARGUMENT", "invalid Skill reference or resource")
	}
	for _, r := range l.roots {
		if r.ID != rootID {
			continue
		}
		if !allowed(rule, r.ID) {
			return nil, problem("ACCESS_DENIED", "root outside caller grant")
		}
		root, err := l.openRoot(r)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		pkg, err := openDirectory(root, name)
		if err != nil {
			return nil, err
		}
		defer pkg.Close()
		if _, err = metadata(pkg); err != nil {
			return nil, problem("NOT_FOUND", "Skill unavailable")
		}
		path := args.Resource
		if path == "" {
			path = "SKILL.md"
		} else {
			// The inventory skips symlink directories. A caller cannot spell an
			// unlisted child through one, even if it points inside this package.
			parts := strings.Split(path, "/")
			for i := 1; i < len(parts); i++ {
				fi, err := pkg.Lstat(filepath.Join(parts[:i]...))
				if err != nil || !fi.IsDir() {
					return nil, problem("NOT_FOUND", "resource unavailable")
				}
			}
		}
		text, err := readText(pkg, path)
		if err != nil {
			return nil, err
		}
		result := map[string]any{"ref": args.Ref, "resource": path, "text": text}
		if args.Resource == "" {
			rows, err := resources(ctx, pkg)
			if err != nil {
				return nil, err
			}
			result["resources"] = rows
		}
		return result, nil
	}
	return nil, problem("NOT_FOUND", "unknown Skill root")
}
func (l *library) call(ctx context.Context, c caller, action string, raw json.RawMessage) (any, error) {
	r, ok := l.readers[c]
	if !ok {
		return nil, problem("ACCESS_DENIED", "caller has no Skill grant")
	}
	switch action {
	case "list_skills":
		v, err := l.list(ctx, c, r, raw)
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return nil, problem("READ_ERROR", "unable to list registered Skill root")
		}
		return v, err
	case "read_skill":
		v, err := l.read(ctx, r, raw)
		var pe *fs.PathError
		if errors.As(err, &pe) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			return nil, problem("NOT_FOUND", "Skill or resource unavailable")
		}
		return v, err
	default:
		return nil, problem("INVALID_ARGUMENT", "unknown action")
	}
}
