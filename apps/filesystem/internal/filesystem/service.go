package filesystem

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wyw-ai/loom-oi/apps/filesystem/contract"
)

type Object = map[string]any
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details Object `json:"details,omitempty"`
}

func (e *Failure) Error() string     { return e.Code + ": " + e.Message }
func fail(code, msg string) *Failure { return &Failure{Code: code, Message: msg} }
func ioError(err error) *Failure {
	var code string
	switch {
	case errors.Is(err, os.ErrNotExist):
		code = "FILE_NOT_FOUND"
	case errors.Is(err, os.ErrExist):
		code = "FILE_EXISTS"
	case errors.Is(err, fs.ErrPermission):
		code = "PERMISSION_DENIED"
	case errors.Is(err, context.DeadlineExceeded):
		code = "TIMEOUT"
	case errors.Is(err, context.Canceled):
		code = "CANCELLED"
	case strings.Contains(err.Error(), "escapes"):
		code = "PATH_OUTSIDE_ROOT"
	default:
		code = platformError(err)
	}
	return fail(code, "filesystem operation failed")
}
func check(ctx context.Context) *Failure {
	if err := ctx.Err(); err != nil {
		return ioError(err)
	}
	return nil
}
func str(o Object, k, def string) string {
	v, ok := o[k].(string)
	if !ok {
		return def
	}
	return v
}
func flag(o Object, k string, def bool) bool {
	v, ok := o[k].(bool)
	if !ok {
		return def
	}
	return v
}
func num(o Object, k string, def int) int {
	switch v := o[k].(type) {
	case json.Number:
		if n, e := v.Int64(); e == nil {
			return int(n)
		}
		f, _ := v.Float64()
		if f >= float64(math.MaxInt) {
			return math.MaxInt
		}
		return int(f)
	case int:
		return v
	case float64:
		return int(v)
	}
	return def
}
func object(o Object, k string) Object { x, _ := o[k].(map[string]any); return x }

type Service struct {
	Root                             *os.Root
	limits, images                   map[string]int
	searches                         Object
	excludes                         []string
	readonly                         bool
	schemas                          map[string]*jsonschema.Schema
	key                              [32]byte
	writeSlot, imageSlot, searchSlot chan struct{}
}

func Open(config Object) (*Service, error) {
	sch, err := contract.Compile()
	if err != nil {
		return nil, err
	}
	if err = sch["config"].Validate(config); err != nil {
		return nil, fmt.Errorf("invalid filesystem config: %w", err)
	}
	l := map[string]int{
		"max_inflight_calls": 16, "operation_timeout_ms": 15000, "max_result_frame_bytes": 768 << 10,
		"max_read_bytes": 64 << 10, "max_read_lines": 2000, "max_scan_bytes": 32 << 20,
		"max_write_bytes": 128 << 10, "max_edit_file_bytes": 2 << 20, "max_directory_entries": 20000,
		"max_search_entries": 50000, "max_search_file_bytes": 4 << 20, "max_search_line_bytes": 1 << 20,
		"max_results": 1000, "max_depth": 64, "cursor_ttl_ms": 600000,
	}
	for k, v := range l {
		l[k] = num(object(config, "limits"), k, v)
	}
	im := map[string]int{
		"max_source_bytes": 16 << 20, "max_pixels": 20000000, "max_dimension": 16384,
		"target_encoded_bytes": 384 << 10, "min_preview_edge": 768,
		"max_encoded_bytes": 0, "max_preview_edge": 0,
	}
	for k, v := range im {
		im[k] = num(object(config, "images"), k, v)
	}
	if l["max_write_bytes"] > l["max_edit_file_bytes"] || l["max_edit_file_bytes"] > l["max_scan_bytes"] || l["max_search_file_bytes"] > l["max_scan_bytes"] || im["max_preview_edge"] > 0 && im["min_preview_edge"] > im["max_preview_edge"] {
		return nil, errors.New("incompatible filesystem limits")
	}
	root := str(config, "root", "")
	if !filepath.IsAbs(root) {
		return nil, errors.New("root must be an existing absolute directory")
	}
	searches := object(config, "search")
	ex := []string{"**/.git", "**/.git/**"}
	if entries, ok := searches["exclude_globs"].([]any); ok {
		ex = nil
		for _, p := range entries {
			s := p.(string)
			if !doublestar.ValidatePattern(s) {
				return nil, errors.New("invalid exclusion glob")
			}
			ex = append(ex, s)
		}
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	directory, err := r.Open(".")
	if err != nil {
		r.Close()
		return nil, err
	}
	volumeFailure := checkVolume(directory)
	directory.Close()
	if volumeFailure != nil {
		r.Close()
		return nil, volumeFailure
	}
	svc := &Service{
		Root: r, limits: l, images: im, searches: searches, excludes: ex,
		readonly: flag(config, "read_only", true), schemas: sch,
		writeSlot: make(chan struct{}, 1), imageSlot: make(chan struct{}, 1), searchSlot: make(chan struct{}, 1),
	}
	rand.Read(svc.key[:])
	return svc, nil
}
func (s *Service) Close() error     { return s.Root.Close() }
func (s *Service) Concurrency() int { return s.limits["max_inflight_calls"] }
func (s *Service) Timeout() time.Duration {
	return time.Duration(s.limits["operation_timeout_ms"]) * time.Millisecond
}
func (s *Service) FrameLimit() int { return s.limits["max_result_frame_bytes"] }

type Request struct {
	ID, Handler, Identity, VMCP string
	Input                       Object
}

func (s *Service) Call(ctx context.Context, q Request) (result Object, failure *Failure) {
	mut := q.Handler == "write_file" || q.Handler == "edit_file"
	if mut {
		defer func() {
			if failure != nil && failure.Details["mutation_state"] == nil {
				if failure.Details == nil {
					failure.Details = Object{}
				}
				failure.Details["mutation_state"] = "not_applied"
				failure.Details["created_directories"] = []string{}
			}
		}()
	}
	schema := s.schemas[q.Handler]
	if schema == nil || q.Handler == "config" {
		return nil, fail("UNKNOWN_HANDLER", "unknown filesystem action")
	}
	if err := schema.Validate(q.Input); err != nil {
		return nil, fail("INVALID_ARGUMENT", "arguments do not match "+q.Handler+" schema")
	}
	if mut && s.readonly {
		return nil, fail("READ_ONLY", "this instance is read-only")
	}
	p, e := s.path(str(q.Input, "path", "."))
	if e != nil {
		return nil, e
	}
	var slot chan struct{}
	switch q.Handler {
	case "write_file", "edit_file":
		slot = s.writeSlot
	case "find_files", "search_content":
		slot = s.searchSlot
	case "view_image":
		slot = s.imageSlot
	}
	if slot != nil {
		select {
		case slot <- struct{}{}:
			defer func() { <-slot }()
		case <-ctx.Done():
			return nil, ioError(ctx.Err())
		}
	}
	if e := check(ctx); e != nil {
		return nil, e
	}
	switch q.Handler {
	case "read_file":
		return s.read(ctx, q, p)
	case "write_file", "edit_file":
		return s.mutate(ctx, q, p)
	case "list_directory":
		return s.list(ctx, q, p)
	case "find_files", "search_content":
		return s.search(ctx, q, p)
	case "view_image":
		result, failure := s.image(ctx, q, p)
		if timeout := check(ctx); timeout != nil {
			return nil, timeout
		}
		return result, failure
	}
	panic("unhandled action")
}
func (s *Service) path(raw string) (string, *Failure) {
	if !utf8.ValidString(raw) || raw == "" || strings.ContainsRune(raw, 0) {
		return "", fail("UNSUPPORTED_PATH_ENCODING", "invalid path")
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, `\\`) || len(raw) >= 2 && raw[1] == ':' && (runtime.GOOS != "windows" || !filepath.IsAbs(raw)) {
		return "", fail("PATH_OUTSIDE_ROOT", "unsupported path prefix")
	}
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(raw)
		suffix := raw[len(volume):]
		if volume == "" && strings.HasPrefix(filepath.ToSlash(suffix), "/") || strings.Contains(suffix, ":") {
			return "", fail("PATH_OUTSIDE_ROOT", "invalid Windows path")
		}
		for _, c := range strings.Split(filepath.ToSlash(suffix), "/") {
			if c == "" || c == "." || c == ".." {
				continue
			}
			if !filepath.IsLocal(c) || strings.HasSuffix(c, ".") || strings.HasSuffix(c, " ") {
				return "", fail("PATH_OUTSIDE_ROOT", "unsupported Windows path component")
			}
		}
	}
	if filepath.IsAbs(raw) {
		rel, err := filepath.Rel(s.Root.Name(), raw)
		if err != nil {
			return "", fail("PATH_OUTSIDE_ROOT", "different volume")
		}
		raw = rel
	}
	p := filepath.Clean(raw)
	if !filepath.IsLocal(p) {
		return "", fail("PATH_OUTSIDE_ROOT", "path outside instance root")
	}
	return filepath.ToSlash(p), nil
}
func (s *Service) ordinary(p string) (*os.File, os.FileInfo, *Failure) {
	f, err := s.Root.OpenFile(p, os.O_RDONLY|readFlags, 0)
	if err != nil {
		return nil, nil, ioError(err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, ioError(err)
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return nil, nil, fail("NOT_A_FILE", "expected ordinary file")
	}
	return f, st, nil
}
func frameSize(id, format string, result Object) int {
	o := Object{"type": "result", "call_id": id, "result": result}
	if format != "" {
		o["result_format"] = format
	}
	b, err := json.Marshal(o)
	if err != nil {
		return 1 << 30
	}
	return len(b) + 1
}
