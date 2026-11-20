package filesystem

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"sort"
)

func (s *Service) source(p string) (*os.File, os.FileInfo, []byte, *Failure) {
	st, err := s.Root.Lstat(p)
	if err != nil {
		return nil, nil, nil, ioError(err)
	}
	if !st.Mode().IsRegular() {
		return nil, nil, nil, fail("UNSUPPORTED_FILE_ATTRIBUTES", "target must be an ordinary unlinked file")
	}
	f, openSt, e := s.ordinary(p)
	if e != nil {
		return nil, nil, nil, e
	}
	bad := func(e *Failure) (*os.File, os.FileInfo, []byte, *Failure) { f.Close(); return nil, nil, nil, e }
	if !os.SameFile(st, openSt) {
		return bad(fail("REVISION_CONFLICT", "target identity changed"))
	}
	if e := validateSource(f, openSt); e != nil {
		return bad(e)
	}
	if openSt.Size() > int64(s.limits["max_edit_file_bytes"]) {
		return bad(fail("FILE_TOO_LARGE", "target exceeds edit limit"))
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(s.limits["max_edit_file_bytes"])+1))
	if err != nil {
		return bad(ioError(err))
	}
	if int64(len(b)) != openSt.Size() {
		return bad(fail("REVISION_CONFLICT", "target size changed"))
	}
	if !textOK(b) {
		return bad(fail("UNSUPPORTED_ENCODING", "target must be UTF-8 without NUL"))
	}
	return f, openSt, b, nil
}
func editText(original []byte, edits []any, limit int) ([]byte, *Failure) {
	text := bytes.TrimPrefix(original, []byte{0xef, 0xbb, 0xbf})
	offset := len(original) - len(text)
	type edit struct {
		at, end int
		v       string
	}
	list := make([]edit, 0, len(edits))
	inputSize := 0
	for _, v := range edits {
		m := v.(map[string]any)
		old, newText := m["old_text"].(string), m["new_text"].(string)
		inputSize += len(old) + len(newText)
		if inputSize > limit {
			return nil, fail("FILE_TOO_LARGE", "edit arguments exceed write budget")
		}
		if !textOK([]byte(old)) || !textOK([]byte(newText)) {
			return nil, fail("UNSUPPORTED_ENCODING", "edits must be UTF-8 without NUL")
		}
		at := bytes.Index(text, []byte(old))
		if at < 0 {
			return nil, fail("EDIT_NOT_FOUND", "old_text not found")
		}
		if bytes.Contains(text[at+1:], []byte(old)) {
			return nil, fail("EDIT_AMBIGUOUS", "old_text occurs more than once")
		}
		list = append(list, edit{offset + at, offset + at + len(old), newText})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].at < list[j].at })
	var buf bytes.Buffer
	last := 0
	for _, e := range list {
		if e.at < last {
			return nil, fail("EDIT_OVERLAP", "edit ranges overlap")
		}
		buf.Write(original[last:e.at])
		buf.WriteString(e.v)
		last = e.end
	}
	buf.Write(original[last:])
	return buf.Bytes(), nil
}
func (s *Service) mutate(ctx context.Context, q Request, p string) (out Object, failure *Failure) {
	create := q.Handler == "write_file" && str(q.Input, "mode", "create") == "create"
	content := []byte(str(q.Input, "content", ""))
	if len(content) > s.limits["max_write_bytes"] {
		return nil, fail("FILE_TOO_LARGE", "content exceeds write budget")
	}
	if !textOK(content) {
		return nil, fail("UNSUPPORTED_ENCODING", "content must be UTF-8 without NUL")
	}
	var expected any
	var st os.FileInfo
	var src *os.File
	var attributes string
	if !create {
		var b []byte
		var e *Failure
		src, st, b, e = s.source(p)
		if e != nil {
			return nil, e
		}
		defer func() {
			if src != nil {
				src.Close()
			}
		}()
		current := revision(b)
		if current != q.Input["expected_revision"] {
			return nil, &Failure{"REVISION_CONFLICT", "target revision differs", Object{"current_revision": current}}
		}
		expected = current
		if q.Handler == "edit_file" {
			content, e = editText(b, q.Input["edits"].([]any), s.limits["max_write_bytes"])
			if e != nil {
				return nil, e
			}
			if len(content) > s.limits["max_edit_file_bytes"] {
				return nil, fail("FILE_TOO_LARGE", "edited file exceeds edit limit")
			}
		}
	}
	created := []string{}
	warnings := []issue{}
	state := "not_applied"
	defer func() {
		if failure != nil {
			if failure.Details == nil {
				failure.Details = Object{}
			}
			failure.Details["mutation_state"] = state
			failure.Details["created_directories"] = created
		}
	}()
	digest := revision(content)
	receipt := func(state string, dirs []string) Object {
		n := len(content)
		if state == "unchanged" {
			n = 0
		}
		return Object{"path": p, "state": state, "created": create, "bytes_written": n, "previous_revision": expected, "revision": digest, "created_directories": dirs, "warnings": warnings}
	}
	if expected == digest {
		return receipt("unchanged", created), nil
	}
	dir := path.Dir(p)
	var possible []string
	if create && flag(q.Input, "create_parents", false) {
		for d := dir; d != "."; d = path.Dir(d) {
			info, err := s.Root.Stat(d)
			if err == nil {
				if !info.IsDir() {
					return nil, fail("NOT_A_DIRECTORY", "target parent is not a directory")
				}
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, ioError(err)
			}
			possible = append(possible, d)
			if len(possible) > 64 {
				return nil, fail("INVALID_ARGUMENT", "too many parents to create")
			}
		}
	}
	// Reserve both the success form and bounded diagnostic data before the first mkdir.
	reserved := receipt("applied", possible)
	reserved["warnings"] = []issue{
		{p, "PUBLISH_WARNING", "published identity and content verified after system error"},
		{p, "STAGING_CLEANUP_FAILED", "temporary name could not be removed"},
		{p, "DIRECTORY_SYNC_FAILED", "directory persistence could not be confirmed"},
		{p, "ATTRIBUTE_RESTORE_FAILED", "published data is verified but final file attributes could not be restored"},
	}
	if frameSize(q.ID, "", reserved)+512 > s.FrameLimit() {
		return nil, fail("RESULT_TOO_LARGE", "mutation receipts exceed frame limit")
	}
	for i := len(possible) - 1; i >= 0; i-- {
		if e := check(ctx); e != nil {
			return nil, e
		}
		d := possible[i]
		if err := s.Root.Mkdir(d, 0755); err != nil {
			info, se := s.Root.Stat(d)
			if !errors.Is(err, os.ErrExist) || se != nil || !info.IsDir() {
				return nil, ioError(err)
			}
		} else {
			created = append(created, d)
		}
	}
	var id [16]byte
	rand.Read(id[:])
	tmp := join(dir, ".oi-filesystem-"+hex.EncodeToString(id[:]))
	f, err := openStaging(s.Root, tmp, src != nil)
	if err != nil {
		return nil, ioError(err)
	}
	cleanup := true
	defer func() {
		f.Close()
		if cleanup {
			s.Root.Remove(tmp)
		}
	}()
	if e := checkVolume(f); e != nil {
		return nil, e
	}
	if src != nil {
		if e := copyAttributes(src, f); e != nil {
			return nil, e
		}
	}
	if _, err = f.Write(content); err != nil {
		return nil, ioError(err)
	}
	if src != nil {
		// Writing can set the NTFS archive attribute. Recheck the final metadata.
		if e := copyAttributes(src, f); e != nil {
			return nil, e
		}
		var e *Failure
		attributes, e = attributeKey(f)
		if e != nil {
			return nil, e
		}
		src.Close()
		src = nil
	}
	if err = f.Sync(); err != nil {
		return nil, ioError(err)
	}
	stageInfo, err := f.Stat()
	if err != nil {
		return nil, ioError(err)
	}
	if err = f.Close(); err != nil {
		return nil, ioError(err)
	}
	var postPublish func() *Failure
	if !create {
		target, cur, body, e := s.source(p)
		if e != nil {
			return nil, e
		}
		curAttributes, attributeFailure := attributeKey(target)
		if attributeFailure != nil {
			target.Close()
			return nil, attributeFailure
		}
		if !os.SameFile(st, cur) || revision(body) != expected || curAttributes != attributes {
			target.Close()
			return nil, fail("REVISION_CONFLICT", "target changed before publish")
		}
		var release func()
		postPublish, release, attributeFailure = finishStaging(s.Root, tmp, stageInfo, target)
		target.Close()
		if attributeFailure != nil {
			return nil, attributeFailure
		}
		if release != nil {
			defer release()
		}
	}
	if e := check(ctx); e != nil {
		return nil, e
	}
	if create {
		err = s.Root.Link(tmp, p)
	} else {
		err = s.Root.Rename(tmp, p)
	}
	if err != nil {
		if create && errors.Is(err, os.ErrExist) {
			return nil, fail("FILE_EXISTS", "target already exists")
		}
		now, statErr := s.Root.Stat(p)
		switch {
		case statErr == nil && os.SameFile(stageInfo, now):
			if !s.verifyPublished(p, stageInfo, len(content), digest) {
				state = "unknown"
				return nil, fail("CALL_RESULT_UNKNOWN", "could not verify published content")
			}
			warnings = append(warnings, issue{p, "PUBLISH_WARNING", "published identity and content verified after system error"})
		case create && errors.Is(statErr, os.ErrNotExist):
			return nil, ioError(err)
		case !create && statErr == nil && os.SameFile(st, now):
			current, _, b, e := s.source(p)
			if e == nil {
				publishFailure := publishError(err, current)
				current.Close()
				if revision(b) == expected {
					return nil, publishFailure
				}
			}
			fallthrough
		default:
			state = "unknown"
			return nil, fail("CALL_RESULT_UNKNOWN", "could not establish publish outcome")
		}
	}
	state = "applied"
	if postPublish != nil {
		if e := postPublish(); e != nil {
			warnings = append(warnings, issue{p, "ATTRIBUTE_RESTORE_FAILED", "published data is verified but final file attributes could not be restored"})
		}
	}
	if create {
		if err := s.Root.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
			warnings = append(warnings, issue{p, "STAGING_CLEANUP_FAILED", "temporary name could not be removed"})
		}
	}
	cleanup = false
	if err := syncDirectory(s.Root, dir); err != nil {
		warnings = append(warnings, issue{p, "DIRECTORY_SYNC_FAILED", "directory persistence could not be confirmed"})
	}
	return receipt("applied", created), nil
}

func (s *Service) verifyPublished(p string, expected os.FileInfo, size int, digest string) bool {
	file, info, failure := s.ordinary(p)
	if failure != nil {
		return false
	}
	defer file.Close()
	if !os.SameFile(expected, info) || info.Size() != int64(size) {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(file, int64(size)+1))
	return err == nil && len(b) == size && revision(b) == digest
}
