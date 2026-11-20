package filesystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	ignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

type walkState struct {
	*Service
	ctx                                  context.Context
	q                                    Request
	base, pattern, key, reason           string
	hidden, git                          bool
	depthLimited                         bool
	depth, count, visited, scanned, used int
	excluded                             map[string]int
	issueList
	rows []Object
	re   *regexp.Regexp
}

func (s *Service) search(ctx context.Context, q Request, p string) (Object, *Failure) {
	w := &walkState{
		Service: s, ctx: ctx, q: q, base: p,
		depth:     min(num(q.Input, "max_depth", s.limits["max_depth"]), s.limits["max_depth"]),
		hidden:    flag(q.Input, "include_hidden", flag(s.searches, "include_hidden", true)),
		git:       flag(q.Input, "respect_gitignore", flag(s.searches, "respect_gitignore", true)),
		excluded:  map[string]int{"ignored": 0, "hidden": 0, "symlinks": 0, "binary": 0},
		issueList: issueList{Issues: []issue{}}, rows: []Object{},
	}
	if q.Handler == "find_files" {
		w.key = "files"
		w.pattern = str(q.Input, "pattern", "")
		w.count = min(num(q.Input, "limit", 200), s.limits["max_results"])
	} else {
		w.key = "matches"
		w.count = min(num(q.Input, "limit", 100), s.limits["max_results"])
		w.pattern = str(q.Input, "file_glob", "**")
		pattern := str(q.Input, "query", "")
		if len(pattern) > 4096 {
			return nil, fail("INVALID_ARGUMENT", "query exceeds 4096 bytes")
		}
		if str(q.Input, "mode", "literal") == "literal" {
			pattern = regexp.QuoteMeta(pattern)
		}
		if !flag(q.Input, "case_sensitive", true) {
			pattern = "(?i:" + pattern + ")"
		}
		r, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fail("INVALID_PATTERN", "invalid Go regular expression")
		}
		w.re = r
	}
	if !doublestar.ValidatePattern(w.pattern) {
		return nil, fail("INVALID_PATTERN", "invalid glob")
	}
	info, err := s.Root.Lstat(p)
	if err != nil {
		return nil, ioError(err)
	}
	if !info.IsDir() {
		return nil, fail("NOT_A_DIRECTORY", "search path must be a directory")
	}
	var rules []ignore.Pattern
	if p != "." {
		dir := "."
		for _, part := range strings.Split(p, "/") {
			rules = w.rules(dir, rules)
			dir = join(dir, part)
			st, err := s.Root.Lstat(dir)
			if err != nil {
				return nil, ioError(err)
			}
			if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
				return nil, fail("NOT_A_DIRECTORY", "search paths must traverse ordinary directories")
			}
			if w.ignore(dir, true, rules) {
				w.excluded["ignored"]++
				return w.result(), nil
			}
		}
	}
	w.walk(p, info, 0, rules)
	if len(w.rows) == 0 && w.reason == "cancelled" {
		return nil, fail("CANCELLED", "search cancelled")
	}
	result := w.result()
	for frameSize(q.ID, "", result) > s.FrameLimit() {
		if len(w.Issues) > 1 || len(w.rows) <= 1 && len(w.Issues) > 0 {
			w.dropLast()
			result = w.result()
			continue
		}
		if len(w.rows) == 0 {
			return nil, fail("RESULT_TOO_LARGE", "search diagnostics exceed result budget")
		}
		w.rows = w.rows[:len(w.rows)-1]
		w.reason = "output_limit"
		result = w.result()
	}
	return result, nil
}
func (w *walkState) result() Object {
	reason := w.reason
	if reason == "" {
		reason = "completed"
		if w.depthLimited {
			reason = "scan_limit"
		} else if len(w.Issues) > 0 || w.Truncated {
			reason = "partial_errors"
		}
	}
	return Object{
		"path": w.base, w.key: w.rows,
		"status": Object{
			"complete": reason == "completed", "stop_reason": reason,
			"visited_entries": w.visited, "scanned_bytes": w.scanned, "excluded": w.excluded,
			"issues": w.Issues, "issues_truncated": w.Truncated,
		},
	}
}
func (w *walkState) stop() bool {
	if err := w.ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			w.reason = "cancelled"
		} else {
			w.reason = "timeout"
		}
	}
	return w.reason != ""
}
func (w *walkState) rules(dir string, rules []ignore.Pattern) []ignore.Pattern {
	if !w.git || w.stop() {
		return rules
	}
	p := join(dir, ".gitignore")
	if !pathFits(p) {
		w.add(dir, "PATH_TOO_LONG", "ignore file path exceeds the contract limit")
		return rules
	}
	st, err := w.Root.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return rules
	}
	if err != nil {
		w.add(p, ioError(err).Code, "cannot inspect ignore rules")
		return rules
	}
	if !st.Mode().IsRegular() {
		return rules
	}
	b, ok := w.bytes(p, st, 256<<10)
	if !ok {
		return rules
	}
	if !textOK(b) {
		w.add(p, "UNSUPPORTED_ENCODING", "invalid ignore rules")
		return rules
	}
	var dom []string
	if dir != "." {
		dom = strings.Split(dir, "/")
	}
	out := append([]ignore.Pattern(nil), rules...)
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, ignore.ParsePattern(line, dom))
		}
	}
	return out
}
func (w *walkState) ignore(p string, isDir bool, rules []ignore.Pattern) bool {
	for _, glob := range w.excludes {
		if ok, _ := doublestar.Match(glob, p); ok {
			return true
		}
	}
	return w.git && ignore.NewMatcher(rules).Match(strings.Split(p, "/"), isDir)
}
func (w *walkState) walk(p string, expected os.FileInfo, depth int, rules []ignore.Pattern) {
	if w.stop() {
		return
	}
	d, err := w.Root.OpenFile(p, os.O_RDONLY|readFlags, 0)
	if err != nil {
		w.add(p, ioError(err).Code, "cannot open directory")
		return
	}
	defer d.Close()
	st, err := d.Stat()
	if err != nil || !os.SameFile(st, expected) {
		w.add(p, "FILE_CHANGED", "directory changed")
		return
	}
	rules = w.rules(p, rules)
	for !w.stop() {
		items, err := d.ReadDir(128)
		for _, e := range items {
			if w.stop() {
				return
			}
			if w.visited >= w.limits["max_search_entries"] {
				w.reason = "scan_limit"
				return
			}
			w.visited++
			name := e.Name()
			if !utf8.ValidString(name) {
				w.add(p, "UNSUPPORTED_PATH_ENCODING", "non-UTF-8 name omitted")
				continue
			}
			full := join(p, name)
			if !pathFits(full) {
				w.add(p, "PATH_TOO_LONG", "entry path exceeds the contract limit")
				continue
			}
			if !w.hidden && strings.HasPrefix(name, ".") {
				w.excluded["hidden"]++
				continue
			}
			if w.ignore(full, e.IsDir(), rules) {
				w.excluded["ignored"]++
				continue
			}
			st, err := w.Root.Lstat(full)
			if err != nil {
				w.add(full, ioError(err).Code, "cannot inspect entry")
				continue
			}
			if st.Mode()&os.ModeSymlink != 0 {
				w.excluded["symlinks"]++
				continue
			}
			if st.IsDir() {
				if depth+1 >= w.depth {
					w.depthLimited = true
					w.add(full, "DEPTH_LIMIT", "directory exceeds depth budget")
					continue
				}
				w.walk(full, st, depth+1, rules)
				continue
			}
			if !st.Mode().IsRegular() {
				continue
			}
			rel := full
			if w.base != "." {
				rel = strings.TrimPrefix(full, w.base+"/")
			}
			ok, _ := doublestar.Match(w.pattern, rel)
			if !ok {
				continue
			}
			if w.re == nil {
				w.row(Object{"path": full, "size_bytes": st.Size()})
				continue
			}
			b, ok := w.bytes(full, st, w.limits["max_search_file_bytes"])
			if !ok {
				continue
			}
			if !textOK(b) {
				w.excluded["binary"]++
				continue
			}
			w.lines(full, bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}))
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			w.add(p, ioError(err).Code, "directory scan failed")
			return
		}
	}
}
func (w *walkState) bytes(p string, expected os.FileInfo, limit int) ([]byte, bool) {
	if expected.Size() > int64(limit) {
		w.add(p, "FILE_TOO_LARGE", "file exceeds candidate limit")
		return nil, false
	}
	left := w.limits["max_scan_bytes"] - w.scanned
	if expected.Size() > int64(left) {
		w.reason = "scan_limit"
		return nil, false
	}
	f, st, e := w.ordinary(p)
	if e != nil {
		w.add(p, e.Code, "cannot read candidate")
		return nil, false
	}
	defer f.Close()
	if !os.SameFile(st, expected) {
		w.add(p, "FILE_CHANGED", "candidate identity changed")
		return nil, false
	}
	readLimit := min(limit+1, left)
	b, err := io.ReadAll(io.LimitReader(f, int64(readLimit)))
	w.scanned += len(b)
	if err != nil {
		w.add(p, ioError(err).Code, "read failed")
		return nil, false
	}
	if len(b) > limit {
		w.add(p, "FILE_TOO_LARGE", "candidate grew")
		return nil, false
	}
	current, err := f.Stat()
	if err == nil && current.Size() > int64(left) {
		w.reason = "scan_limit"
		return nil, false
	}
	if err != nil || current.Size() != int64(len(b)) || !st.ModTime().Equal(current.ModTime()) {
		w.add(p, "FILE_CHANGED", "candidate changed")
		return nil, false
	}
	return b, true
}
func (w *walkState) row(row Object) {
	if len(w.rows) >= w.count {
		w.reason = "result_limit"
		return
	}
	b, _ := json.Marshal(row)
	if w.used+len(b)+1 > w.FrameLimit() {
		w.reason = "output_limit"
		return
	}
	w.used += len(b) + 1
	w.rows = append(w.rows, row)
}
func snippet(b []byte, at int) (int, string, bool) {
	if len(b) <= 1024 {
		return 0, string(b), false
	}
	start := max(0, min(at-128, len(b)-1024))
	for !utf8.RuneStart(b[start]) {
		start++
	}
	end := min(len(b), start+1024)
	for end < len(b) && !utf8.RuneStart(b[end]) {
		end--
	}
	return start, string(b[start:end]), true
}
func lineAt(b []byte) (line, rest []byte) {
	i := bytes.IndexByte(b, '\n')
	if i < 0 {
		return b, nil
	}
	return bytes.TrimSuffix(b[:i], []byte{'\r'}), b[i+1:]
}
func ctxLine(line int, b []byte) Object {
	_, text, trunc := snippet(b, 0)
	return Object{"line": line, "text": text, "text_truncated": trunc}
}
func (w *walkState) lines(p string, b []byte) {
	n := num(w.q.Input, "context_lines", 0)
	before := []Object{}
	for line := 1; len(b) > 0 && !w.stop(); line++ {
		var raw []byte
		raw, b = lineAt(b)
		if len(raw) > w.limits["max_search_line_bytes"] {
			w.add(p, "LINE_TOO_LONG", "overlong line omitted")
		} else if idx := w.re.FindIndex(raw); idx != nil {
			pos, text, trunc := snippet(raw, idx[0])
			after := []Object{}
			rest := b
			for i := 0; i < n && len(rest) > 0; i++ {
				var x []byte
				x, rest = lineAt(rest)
				after = append(after, ctxLine(line+i+1, x))
			}
			w.row(Object{"path": p, "line": line, "text": text, "snippet_start_byte": pos, "match_start_byte": idx[0], "match_end_byte": idx[1], "text_truncated": trunc, "before": append([]Object{}, before...), "after": after})
		}
		if n > 0 {
			before = append(before, ctxLine(line, raw))
			if len(before) > n {
				before = before[1:]
			}
		}
	}
}
