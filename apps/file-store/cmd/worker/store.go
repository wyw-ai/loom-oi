package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type config struct {
	Root         string `json:"root"`
	Store        string `json:"store"`
	WebDAVURL    string `json:"webdav_url"`
	Username     string `json:"username"`
	PasswordEnv  string `json:"password_env"`
	PasswordFile string `json:"password_file"`
}

type service struct {
	root           *os.Root
	store          string
	dav            dav
	confirmTimeout time.Duration
}

func openService(c config) (*service, error) {
	u, err := url.Parse(c.WebDAVURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("webdav_url must be an HTTP(S) mount URL without credentials, query or fragment")
	}
	if c.Store == "" || len(c.Store) > 128 || !filepath.IsAbs(c.Root) {
		return nil, errors.New("store and an absolute local root are required")
	}
	password, err := c.password()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(c.Root)
	if err != nil {
		return nil, err
	}
	// A redirected read could escape the configured mount or forward private input.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &service{root: root, store: c.Store, confirmTimeout: 6 * time.Second, dav: dav{base: u, username: c.Username, password: password, client: client}}, nil
}

func (c config) password() (string, error) {
	if c.Username == "" && c.PasswordFile == "" && c.PasswordEnv == "" {
		return "", nil
	}
	if c.Username == "" || (c.PasswordFile == "") == (c.PasswordEnv == "") {
		return "", errors.New("username requires exactly one of password_file or password_env")
	}
	if c.PasswordEnv != "" {
		p := os.Getenv(c.PasswordEnv)
		if p == "" {
			return "", errors.New("password_env is empty")
		}
		return p, nil
	}
	if !filepath.IsAbs(c.PasswordFile) {
		return "", errors.New("password_file must be absolute")
	}
	localRoot, err := filepath.EvalSymlinks(c.Root)
	if err != nil {
		return "", err
	}
	credential, err := filepath.EvalSymlinks(c.PasswordFile)
	if err != nil {
		return "", errors.New("unable to resolve password_file")
	}
	relative, err := filepath.Rel(localRoot, credential)
	if err == nil && (relative == "." || filepath.IsLocal(relative)) {
		return "", errors.New("password_file must be outside the local file root")
	}
	f, err := os.Open(credential)
	if err != nil {
		return "", errors.New("unable to open password_file")
	}
	defer f.Close()
	content, err := boundedRead(f, 64<<10)
	if err != nil {
		return "", errors.New("unable to read password_file")
	}
	p := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(string(content), "\uFEFF"), "\n"), "\r")
	if p == "" || strings.ContainsAny(p, "\r\n") {
		return "", errors.New("password_file must contain one nonempty line")
	}
	return p, nil
}

type fileRef struct {
	Store string `json:"store"`
	ID    string `json:"file_id"`
	Size  int64  `json:"size_bytes"`
	SHA   string `json:"sha256"`
}
type receipt struct {
	Path string  `json:"store_path"`
	Ref  fileRef `json:"file_ref"`
}

type failure struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (f *failure) Error() string         { return f.Message }
func fail(code, message string) *failure { return &failure{Code: code, Message: message} }

func validPath(s string, rootOK bool) bool {
	if rootOK && s == "." {
		return true
	}
	if !utf8.ValidString(s) || len(s) > 4096 || !fs.ValidPath(s) || s == "." || strings.ContainsAny(s, "\\:") || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if strings.HasPrefix(strings.ToLower(p), ".oi-") || !filepath.IsLocal(p) || strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ") {
			return false
		}
	}
	return true
}

func decode(input json.RawMessage, to any) error {
	input = bytes.TrimSpace(input)
	if len(input) == 0 || input[0] != '{' {
		return fail("INVALID_ARGUMENT", "expected an argument object")
	}
	d := json.NewDecoder(bytes.NewReader(input))
	d.DisallowUnknownFields()
	if err := d.Decode(to); err != nil {
		return fail("INVALID_ARGUMENT", "unrecognized or invalid arguments")
	}
	return nil
}

func namespace(store, grant, user string) string {
	payload, _ := json.Marshal([3]string{store, grant, user})
	sum := sha256.Sum256(payload)
	return "oi-v1/" + hex.EncodeToString(sum[:])
}

func unique() string {
	var id [16]byte
	rand.Read(id[:])
	return hex.EncodeToString(id[:])
}

func (s *service) call(ctx context.Context, ns, action string, input json.RawMessage) (any, error) {
	switch action {
	case "put_file":
		var a struct {
			Path      string  `json:"path"`
			StorePath *string `json:"store_path"`
		}
		if err := decode(input, &a); err != nil {
			return nil, err
		}
		if !validPath(a.Path, false) || (a.StorePath != nil && !validPath(*a.StorePath, false)) {
			return nil, fail("INVALID_PATH", "expected a root-relative file path")
		}
		remote := unique() + "/" + path.Base(a.Path)
		if a.StorePath != nil {
			remote = *a.StorePath
		}
		return s.put(ctx, ns, a.Path, remote)
	case "lookup_file":
		var a struct {
			Path string `json:"store_path"`
		}
		if err := decode(input, &a); err != nil {
			return nil, err
		}
		if !validPath(a.Path, false) {
			return nil, fail("INVALID_PATH", "expected a complete storage path")
		}
		return s.lookup(ctx, ns, a.Path)
	case "get_file":
		var a struct {
			Ref  fileRef `json:"file_ref"`
			Dest string  `json:"dest"`
		}
		if err := decode(input, &a); err != nil {
			return nil, err
		}
		if !validPath(a.Dest, false) {
			return nil, fail("INVALID_PATH", "invalid local destination")
		}
		return s.get(ctx, ns, a.Ref, a.Dest)
	case "list_directory":
		var a struct {
			Dir    string `json:"store_dir"`
			Limit  *int   `json:"limit"`
			Cursor string `json:"cursor"`
		}
		if err := decode(input, &a); err != nil {
			return nil, err
		}
		a.Dir = strings.TrimSuffix(a.Dir, "/")
		if !validPath(a.Dir, true) {
			return nil, fail("INVALID_PATH", "expected a storage directory or '.'")
		}
		limit := 100
		if a.Limit != nil {
			limit = *a.Limit
		}
		if limit < 1 || limit > 200 {
			return nil, fail("INVALID_ARGUMENT", "limit must be in 1..200")
		}
		return s.list(ctx, ns, a.Dir, limit, a.Cursor)
	default:
		return nil, fail("UNKNOWN_ACTION", "unknown file-store action")
	}
}

func (s *service) lookup(ctx context.Context, ns, key string) (receipt, error) {
	var entry receipt
	info, err := s.dav.properties(ctx, path.Join(ns, "files", key), 0)
	if err != nil {
		return entry, err
	}
	if info[0].Directory {
		return entry, fail("NOT_FOUND", "the requested storage path is not a file")
	}
	payload, err := s.dav.read(ctx, path.Join(ns, "meta", key), 32<<10)
	if err != nil {
		return entry, err
	}
	if err = json.Unmarshal(payload, &entry); err != nil || entry.Path != key || entry.Ref.Store != s.store || entry.Ref.ID != ns+"/"+key || entry.Ref.Size < 0 || len(entry.Ref.SHA) != 64 {
		return receipt{}, fail("INVALID_METADATA", "invalid committed object metadata")
	}
	if _, err = hex.DecodeString(entry.Ref.SHA); err != nil {
		return receipt{}, fail("INVALID_METADATA", "invalid object digest")
	}
	if entry.Ref.Size != info[0].Size {
		return receipt{}, fail("INTEGRITY_ERROR", "stored object size differs from committed metadata")
	}
	return entry, nil
}

func (s *service) put(ctx context.Context, ns, src, key string) (receipt, error) {
	var empty receipt
	local, err := filepath.Localize(src)
	if err != nil {
		return empty, err
	}
	info, err := s.root.Stat(local)
	if err != nil {
		return empty, err
	}
	if !info.Mode().IsRegular() {
		return empty, fail("INVALID_ARGUMENT", "source must be a regular file")
	}
	data, err := s.root.Open(local)
	if err != nil {
		return empty, err
	}
	defer data.Close()
	info, err = data.Stat()
	if err != nil {
		return empty, err
	}
	if !info.Mode().IsRegular() {
		return empty, fail("INVALID_ARGUMENT", "source must be a regular file")
	}
	target := path.Join(ns, "files", key)
	meta := path.Join(ns, "meta", key)
	for _, directory := range []string{path.Dir(target), path.Dir(meta)} {
		if err = s.dav.mkdirAll(ctx, directory); err != nil {
			return empty, err
		}
	}
	stage := path.Join(path.Dir(target), ".oi-stage-"+unique())
	cleanupStage := true
	// On cancellation/timeout only our unpublishable staging name is cleaned.
	defer func() {
		if !cleanupStage {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.dav.command(cleanup, "DELETE", stage, nil, 0, nil)
	}()
	hash := sha256.New()
	var body io.Reader = io.TeeReader(data, hash)
	if info.Size() == 0 {
		body = http.NoBody
	}
	if err = s.dav.command(ctx, "PUT", stage, body, info.Size(), map[string]string{"Content-Type": "application/octet-stream"}); err != nil && !errors.Is(err, httpFailure(202)) {
		return empty, err
	}
	if err = s.await(ctx, func(check context.Context) error {
		staged, e := s.dav.properties(check, stage, 0)
		if e != nil {
			return e
		}
		if staged[0].Directory || staged[0].Size != info.Size() {
			return fail("INTEGRITY_ERROR", "staged object size differs from the source")
		}
		return nil
	}); err != nil {
		return empty, err
	}
	entry := receipt{Path: key, Ref: fileRef{Store: s.store, ID: ns + "/" + key, Size: info.Size(), SHA: hex.EncodeToString(hash.Sum(nil))}}
	// Case-insensitive storage aliases use one lock. A single short commit
	// lock also handles driver-specific path normalization.
	unlock, err := s.dav.commitLock(ctx, path.Join(ns, "commit.lock"))
	if err != nil {
		return empty, err
	}
	defer unlock()
	ctx, cancelCommit := context.WithTimeout(ctx, 35*time.Second)
	defer cancelCommit()
	if _, err = s.dav.properties(ctx, target, 0); err == nil {
		return empty, fail("ALREADY_EXISTS", "storage path already exists")
	} else if !errors.Is(err, httpFailure(404)) {
		return empty, err
	}
	// A final data name is the commit marker. A lingering intent can be
	// resumed with the same bytes, even after a lost MOVE acknowledgement.
	metadata, _ := json.Marshal(entry)
	old, err := s.dav.read(ctx, meta, 32<<10)
	switch {
	case err == nil:
		if !bytes.Equal(old, metadata) {
			return empty, fail("ALREADY_EXISTS", "storage path reserved by an earlier upload")
		}
	case errors.Is(err, httpFailure(404)):
		if err = s.dav.putBytes(ctx, meta, metadata); err != nil && !errors.Is(err, httpFailure(202)) {
			return empty, err
		}
	default:
		return empty, err
	}
	if err = s.await(ctx, func(check context.Context) error {
		record, e := s.dav.read(check, meta, 32<<10)
		if e != nil {
			return e
		}
		if !bytes.Equal(record, metadata) {
			return errors.New("metadata not yet visible")
		}
		return nil
	}); err != nil {
		return empty, err
	}
	moveErr := s.dav.move(ctx, stage, target)
	// MOVE might already have committed even if its response was lost or the
	// invocation was canceled. Reconcile independently while still locked.
	err = s.await(context.WithoutCancel(ctx), func(check context.Context) error {
		committed, e := s.lookup(check, ns, key)
		if e != nil {
			return e
		}
		if committed != entry {
			return errors.New("published receipt differs from this upload")
		}
		return nil
	})
	if err != nil {
		var httpErr httpFailure
		cleanupStage = errors.As(moveErr, &httpErr) && httpErr >= 400 && httpErr < 500
		return empty, &failure{Code: "COMMIT_UNCONFIRMED", Message: "lookup the returned storage path before retrying", Details: map[string]any{"store_path": key, "expected_file_ref": entry.Ref, "mutation_state": "unknown"}}
	}
	return entry, nil
}

// Bound eventual-visibility and response-loss recovery independently of the
// caller's lifetime. An accepted backend task alone never confirms a file.
func (s *service) await(ctx context.Context, check func(context.Context) error) error {
	wait, cancel := context.WithTimeout(ctx, s.confirmTimeout)
	defer cancel()
	for {
		err := check(wait)
		if err == nil {
			return nil
		}
		if errors.Is(err, httpFailure(401)) || errors.Is(err, httpFailure(403)) {
			return err
		}
		select {
		case <-wait.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		case <-time.After(80 * time.Millisecond):
		}
	}
}

func (s *service) get(ctx context.Context, ns string, ref fileRef, dest string) (any, error) {
	key, ok := strings.CutPrefix(ref.ID, ns+"/")
	if !ok || ref.Store != s.store || !validPath(key, false) {
		return nil, fail("ACCESS_DENIED", "file reference is outside this storage scope")
	}
	stored, err := s.lookup(ctx, ns, key)
	if err != nil {
		return nil, err
	}
	if stored.Ref != ref {
		return nil, fail("INTEGRITY_ERROR", "file reference differs from committed metadata")
	}
	local, err := filepath.Localize(dest)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(local)
	if err = s.root.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	dir, err := s.root.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	name := filepath.Base(local)
	tmp := ".oi-stage-" + unique()
	if _, err = dir.Lstat(name); err == nil {
		return nil, fail("ALREADY_EXISTS", "destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	res, err := s.dav.request(ctx, http.MethodGet, path.Join(ns, "files", key), nil, 0, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	f, err := dir.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer dir.Remove(tmp)
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(res.Body, ref.Size+1))
	closeErr := f.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if n != ref.Size || hex.EncodeToString(hash.Sum(nil)) != ref.SHA {
		return nil, fail("INTEGRITY_ERROR", "download failed content verification")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = dir.Link(tmp, name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fail("ALREADY_EXISTS", "destination already exists")
		}
		return nil, err
	}
	return map[string]any{"path": dest, "size_bytes": n, "sha256": ref.SHA}, nil
}

type listEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"store_path"`
}

func (s *service) list(ctx context.Context, ns, key string, limit int, cursor string) (any, error) {
	rows, err := s.dav.properties(ctx, path.Join(ns, "files", key), 1)
	if err != nil && !(key == "." && errors.Is(err, httpFailure(404))) {
		return nil, err
	}
	entries := []listEntry{}
	// Intersect raw entries with the metadata tree. Foreign/unmanaged files
	// and intents left before a final data MOVE are not completed uploads.
	metadataRows, metaErr := s.dav.properties(ctx, path.Join(ns, "meta", key), 1)
	if metaErr != nil && !errors.Is(metaErr, httpFailure(404)) {
		return nil, metaErr
	}
	metaFiles := map[string]bool{}
	for _, row := range metadataRows {
		if !row.Directory {
			metaFiles[row.Name] = true
		}
	}
	for _, row := range rows {
		if row.Name == "." {
			if !row.Directory {
				return nil, fail("INVALID_PATH", "path is not a directory")
			}
			continue
		}
		if !validPath(row.Name, false) || (!row.Directory && !metaFiles[row.Name]) {
			continue
		}
		kind := "file"
		if row.Directory {
			kind = "directory"
		}
		entries = append(entries, listEntry{row.Name, kind, path.Join(key, row.Name)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	sum := sha256.New()
	fmt.Fprintf(sum, "%s\x00%s", ns, key)
	for _, entry := range entries {
		fmt.Fprintf(sum, "\x00%s\x00%s", entry.Kind, entry.Name)
	}
	revision := hex.EncodeToString(sum.Sum(nil))
	offset := 0
	if cursor != "" {
		stamp, n, ok := strings.Cut(cursor, ":")
		i, e := strconv.Atoi(n)
		if !ok || stamp != revision || e != nil || i < 0 || i >= len(entries) {
			return nil, fail("STALE_CURSOR", "cursor does not match this directory revision")
		}
		offset = i
	}
	end := offset
	size := 0
	for end < len(entries) && end-offset < limit {
		raw, _ := json.Marshal(entries[end])
		if end > offset && size+len(raw) > 700<<10 {
			break
		}
		size += len(raw) + 1
		end++
	}
	var next any
	if end < len(entries) {
		next = revision + ":" + strconv.Itoa(end)
	}
	return map[string]any{"store_dir": key, "entries": entries[offset:end], "next_cursor": next}, nil
}
