package filesystem

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func revision(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
func textOK(b []byte) bool     { return utf8.Valid(b) && bytes.IndexByte(b, 0) < 0 }

type position struct {
	Line int `json:"line"`
	Col  int `json:"byte_column"`
}
type pageToken struct {
	Binding, Version string
	Offset           int64
	Pos              position
	Expiry           int64
}

func (s *Service) binding(q Request, p string) string {
	j, _ := json.Marshal([]string{q.Handler, p, q.Identity, q.VMCP})
	return revision(j)
}
func (s *Service) token(q Request, p, version string, off int64, pos position) string {
	v := pageToken{s.binding(q, p), version, off, pos, time.Now().Add(time.Duration(s.limits["cursor_ttl_ms"]) * time.Millisecond).UnixMilli()}
	body, _ := json.Marshal(v)
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(append(mac.Sum(nil), body...))
}
func (s *Service) decodeToken(q Request, p, version string) (pageToken, *Failure) {
	var t pageToken
	raw, err := base64.RawURLEncoding.DecodeString(str(q.Input, "cursor", ""))
	if err != nil || len(raw) <= sha256.Size {
		return t, fail("CURSOR_EXPIRED", "invalid or expired cursor")
	}
	sum, body := raw[:sha256.Size], raw[sha256.Size:]
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), sum) || json.Unmarshal(body, &t) != nil || time.Now().UnixMilli() > t.Expiry {
		return t, fail("CURSOR_EXPIRED", "invalid or expired cursor")
	}
	if t.Binding != s.binding(q, p) || t.Version != version {
		return t, fail("STALE_CURSOR", "cursor binding or source changed")
	}
	return t, nil
}

func (s *Service) read(ctx context.Context, q Request, p string) (Object, *Failure) {
	f, st, e := s.ordinary(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	size := st.Size()
	version, e := observation(f, st)
	if e != nil {
		return nil, e
	}
	var rev any
	var reader io.ReadSeeker = f
	if size <= int64(s.limits["max_edit_file_bytes"]) {
		b, err := io.ReadAll(io.LimitReader(f, int64(s.limits["max_edit_file_bytes"])+1))
		if err != nil {
			return nil, ioError(err)
		}
		if int64(len(b)) != size {
			return nil, fail("FILE_CHANGED", "source size changed")
		}
		if !textOK(b) {
			return nil, fail("UNSUPPORTED_ENCODING", "expected UTF-8 text without NUL")
		}
		version = revision(b)
		rev = version
		reader = bytes.NewReader(b)
	}
	var head [3]byte
	n, _ := io.ReadFull(reader, head[:])
	bom := n == 3 && head == [3]byte{0xef, 0xbb, 0xbf}
	pos := position{1, 0}
	var offset int64
	if bom {
		offset = 3
	}
	if _, ok := q.Input["cursor"]; ok {
		t, e := s.decodeToken(q, p, version)
		if e != nil {
			return nil, e
		}
		offset, pos = t.Offset, t.Pos
	}
	if _, err := reader.Seek(offset, io.SeekStart); err != nil {
		return nil, ioError(err)
	}
	br := bufio.NewReaderSize(reader, 32768)
	lineWanted := num(q.Input, "start_line", 1)
	for pos.Line < lineWanted {
		if e := check(ctx); e != nil {
			return nil, e
		}
		// Read runes so that a skipped prefix has the same UTF-8 and NUL policy as a returned page.
		r, n, err := br.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ioError(err)
		}
		if r == 0 || r == utf8.RuneError && n == 1 {
			return nil, fail("UNSUPPORTED_ENCODING", "invalid text in requested range")
		}
		offset += int64(n)
		if offset > int64(s.limits["max_scan_bytes"]) {
			return nil, fail("READ_SEEK_LIMIT_EXCEEDED", "start line exceeds scan budget")
		}
		if r == '\n' {
			pos = position{pos.Line + 1, 0}
		} else {
			pos.Col += n
		}
	}
	start, begin := pos, offset
	maxBytes := min(num(q.Input, "max_bytes", 65536), s.limits["max_read_bytes"])
	maxLines := min(num(q.Input, "max_lines", 200), s.limits["max_read_lines"])
	var b strings.Builder
	for b.Len() < maxBytes && pos.Line-start.Line < maxLines {
		if e := check(ctx); e != nil {
			return nil, e
		}
		r, n, err := br.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ioError(err)
		}
		if r == 0 || r == utf8.RuneError && n == 1 {
			return nil, fail("UNSUPPORTED_ENCODING", "invalid text in returned range")
		}
		if r == '\r' {
			if next, _ := br.Peek(1); len(next) == 1 && next[0] == '\n' {
				if b.Len()+2 > maxBytes {
					break
				}
				br.Discard(1)
				b.WriteString("\r\n")
				offset += 2
				pos = position{pos.Line + 1, 0}
				continue
			}
		}
		if b.Len()+n > maxBytes {
			break
		}
		b.WriteRune(r)
		offset += int64(n)
		if r == '\n' {
			pos = position{pos.Line + 1, 0}
		} else {
			pos.Col += n
		}
	}
	if now, err := f.Stat(); err != nil || now.Size() != st.Size() || !now.ModTime().Equal(st.ModTime()) {
		return nil, fail("FILE_CHANGED", "source changed while reading")
	}
	if now, err := s.Root.Stat(p); err != nil || !os.SameFile(now, st) {
		return nil, fail("FILE_CHANGED", "source name changed while reading")
	}
	result := func(body string, end position, endOff int64) Object {
		var next any
		if endOff < size {
			next = s.token(q, p, version, endOff, end)
		}
		return Object{"path": p, "text": body, "source_size": size, "bom": bom, "range": Object{"start": start, "end": end}, "eof": endOff == size, "next_cursor": next, "revision": rev}
	}
	body := b.String()
	out := result(body, pos, offset)
	for frameSize(q.ID, "", out) > s.FrameLimit() {
		if len(body) == 0 {
			return nil, fail("RESULT_TOO_LARGE", "read metadata exceeds result budget")
		}
		n := len(body) / 2
		for n > 0 && !utf8.RuneStart(body[n]) {
			n--
		}
		if n > 0 && body[n-1] == '\r' && body[n] == '\n' {
			n--
		}
		if n == 0 {
			return nil, fail("RESULT_TOO_LARGE", "one text unit exceeds result budget")
		}
		body = body[:n]
		end := start
		for _, r := range body {
			if r == '\n' {
				end = position{end.Line + 1, 0}
			} else {
				end.Col += utf8.RuneLen(r)
			}
		}
		out = result(body, end, begin+int64(n))
	}
	return out, nil
}

type issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type issueList struct {
	Issues    []issue `json:"issues"`
	Truncated bool    `json:"issues_truncated"`
}

func (i *issueList) add(p, code, msg string) {
	if len(i.Issues) < 20 {
		i.Issues = append(i.Issues, issue{p, code, msg})
	} else {
		i.Truncated = true
	}
}

func (i *issueList) dropLast() bool {
	if len(i.Issues) == 0 {
		return false
	}
	i.Issues = i.Issues[:len(i.Issues)-1]
	i.Truncated = true
	return true
}
func join(p, name string) string {
	if p == "." {
		return name
	}
	return p + "/" + name
}

// All public paths, including paths inside diagnostic records, use the same
// character limit as the action input contract. Do not emit an unusable path.
func pathFits(p string) bool { return len(p) <= 4096 || utf8.RuneCountInString(p) <= 4096 }

func (s *Service) list(ctx context.Context, q Request, p string) (Object, *Failure) {
	d, err := s.Root.OpenFile(p, os.O_RDONLY|readFlags, 0)
	if err != nil {
		return nil, ioError(err)
	}
	defer d.Close()
	st, err := d.Stat()
	if err != nil {
		return nil, ioError(err)
	}
	if !st.IsDir() {
		return nil, fail("NOT_A_DIRECTORY", "expected directory")
	}
	var names []string
	totalBytes := 0
	issues := issueList{Issues: []issue{}}
	for {
		if e := check(ctx); e != nil {
			return nil, e
		}
		batch, err := d.Readdirnames(128)
		for _, name := range batch {
			totalBytes += len(name)
			names = append(names, name)
			if len(names) > s.limits["max_directory_entries"] || totalBytes > 8<<20 {
				return nil, fail("DIRECTORY_TOO_LARGE", "directory exceeds bounded sort budget")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ioError(err)
		}
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		io.WriteString(h, n)
		h.Write([]byte{0})
	}
	observed, e := observation(d, st)
	if e != nil {
		return nil, e
	}
	// Children changing contents or attributes do not invalidate the name-set cursor.
	version := strings.SplitN(observed, "|", 2)[0] + hex.EncodeToString(h.Sum(nil))
	idx := 0
	if _, ok := q.Input["cursor"]; ok {
		t, e := s.decodeToken(q, p, version)
		if e != nil {
			return nil, e
		}
		idx = int(t.Offset)
	}
	limit := min(num(q.Input, "limit", 200), s.limits["max_results"])
	entries := []Object{}
	result := func(end int, page []Object) Object {
		var next any
		if end < len(names) {
			next = s.token(q, p, version, int64(end), position{})
		}
		return Object{
			"path": p, "entries": page, "next_cursor": next,
			"complete": end == len(names) && len(issues.Issues) == 0 && !issues.Truncated,
			"issues":   issues.Issues, "issues_truncated": issues.Truncated,
		}
	}
	fits := func(end, entrySize int) bool {
		for frameSize(q.ID, "", result(end, []Object{}))+entrySize > s.FrameLimit() {
			if !issues.dropLast() {
				return false
			}
		}
		return true
	}
	entryBytes := 0
	for idx < len(names) && len(entries) < limit {
		if e := check(ctx); e != nil {
			return nil, e
		}
		name := names[idx]
		path := join(p, name)
		if !utf8.ValidString(name) {
			issues.add(p, "UNSUPPORTED_PATH_ENCODING", "directory contains a non-UTF-8 name")
			idx++
			continue
		}
		if !pathFits(path) {
			issues.add(p, "PATH_TOO_LONG", "directory contains an entry whose relative path exceeds the contract limit")
			idx++
			continue
		}
		entry := Object{"name": name, "path": path, "kind": "unknown", "size_bytes": nil, "modified_at": nil}
		st, err := s.Root.Lstat(path)
		if err != nil {
			issues.add(path, ioError(err).Code, "could not read entry attributes")
		} else {
			switch {
			case st.Mode()&os.ModeSymlink != 0:
				entry["kind"] = "symlink"
			case st.Mode().IsRegular():
				entry["kind"] = "file"
				entry["size_bytes"] = st.Size()
			case st.IsDir():
				entry["kind"] = "directory"
			default:
				entry["kind"] = "other"
			}
			entry["modified_at"] = st.ModTime().UTC().Format(time.RFC3339Nano)
		}
		encoded, _ := json.Marshal(entry)
		nextSize := entryBytes + len(encoded)
		if len(entries) != 0 {
			nextSize++ // JSON array separator.
		}
		if !fits(idx+1, nextSize) {
			if len(entries) == 0 {
				return nil, fail("RESULT_TOO_LARGE", "directory entry exceeds result limit")
			}
			break
		}
		entryBytes = nextSize
		entries = append(entries, entry)
		idx++
	}
	if !fits(idx, entryBytes) {
		return nil, fail("RESULT_TOO_LARGE", "directory diagnostics exceed result limit")
	}
	return result(idx, entries), nil
}
