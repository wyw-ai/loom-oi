package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// dav addresses a single trusted mount. Credentials stay on this origin.
type dav struct {
	base               *url.URL
	username, password string
	client             *http.Client
}

type httpFailure int

func (s httpFailure) Error() string { return fmt.Sprintf("storage returned HTTP %d", int(s)) }

func (d *dav) address(key string) string {
	u := *d.base
	u.Path = path.Join(u.Path, key)
	u.RawPath = ""
	return u.String()
}

func (d *dav) request(ctx context.Context, method, key string, body io.Reader, size int64, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.address(key), body)
	if err != nil {
		return nil, err
	}
	if d.username != "" {
		req.SetBasicAuth(d.username, d.password)
	}
	if body != nil && size >= 0 {
		req.ContentLength = size
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := d.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("unable to reach WebDAV mount")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || res.StatusCode == http.StatusAccepted {
		res.Body.Close()
		return nil, httpFailure(res.StatusCode)
	}
	return res, nil
}

func (d *dav) command(ctx context.Context, method, key string, body io.Reader, size int64, headers map[string]string) error {
	res, err := d.request(ctx, method, key, body, size, headers)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func boundedRead(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if int64(len(data)) > max {
		return nil, errors.New("WebDAV response exceeds size limit")
	}
	return data, err
}

type davItem struct {
	Name      string
	Directory bool
	Size      int64
}
type property struct {
	Collection *struct{} `xml:"resourcetype>collection"`
	Size       string    `xml:"getcontentlength"`
}

func (d *dav) properties(ctx context.Context, key string, depth int) ([]davItem, error) {
	const query = `<D:propfind xmlns:D="DAV:"><D:prop><D:resourcetype/><D:getcontentlength/></D:prop></D:propfind>`
	res, err := d.request(ctx, "PROPFIND", key, strings.NewReader(query), int64(len(query)), map[string]string{"Depth": strconv.Itoa(depth), "Content-Type": "application/xml"})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := boundedRead(res.Body, 16<<20)
	if err != nil {
		return nil, err
	}
	var multi struct {
		Responses []struct {
			Href  string `xml:"href"`
			Props []struct {
				Status string   `xml:"status"`
				Prop   property `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if err = xml.Unmarshal(data, &multi); err != nil {
		return nil, errors.New("invalid WebDAV directory response")
	}
	requested, _ := url.Parse(d.address(key))
	requestedPath := path.Clean(requested.Path)
	rows := []davItem{}
	for _, entry := range multi.Responses {
		href, err := url.Parse(entry.Href)
		if err != nil {
			return nil, errors.New("invalid WebDAV entry path")
		}
		p := path.Clean(href.Path)
		if p != requestedPath && (depth == 0 || path.Dir(p) != requestedPath) {
			continue
		}
		name := path.Base(p)
		if p == requestedPath {
			name = "."
		}
		for _, stat := range entry.Props {
			if !strings.Contains(stat.Status, " 200 ") {
				continue
			}
			n, _ := strconv.ParseInt(stat.Prop.Size, 10, 64)
			rows = append(rows, davItem{Name: name, Directory: stat.Prop.Collection != nil, Size: n})
			break
		}
	}
	if len(rows) == 0 {
		return nil, errors.New("missing WebDAV properties")
	}
	return rows, nil
}

func (d *dav) mkdirAll(ctx context.Context, key string) error {
	parts := strings.Split(key, "/")
	current := ""
	for _, part := range parts {
		current = path.Join(current, part)
		err := d.command(ctx, "MKCOL", current, nil, 0, nil)
		if err == nil {
			continue
		}
		// Some servers return 405 for existing collections; verify it is a directory.
		rows, statErr := d.properties(ctx, current, 0)
		if statErr != nil || !rows[0].Directory {
			return err
		}
	}
	return nil
}

func (d *dav) read(ctx context.Context, key string, max int64) ([]byte, error) {
	res, err := d.request(ctx, http.MethodGet, key, nil, 0, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return boundedRead(res.Body, max)
}

func (d *dav) move(ctx context.Context, src, dst string) error {
	return d.command(ctx, "MOVE", src, nil, 0, map[string]string{"Destination": d.address(dst), "Overwrite": "F"})
}

// AList's PUT does not guarantee create-only writes. All File Store instances
// sharing this namespace lock the same distinct resource for the short commit.
func (d *dav) commitLock(ctx context.Context, key string) (func(), error) {
	const lockInfo = `<D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockinfo>`
	for {
		res, err := d.request(ctx, "LOCK", key, strings.NewReader(lockInfo), int64(len(lockInfo)), map[string]string{"Timeout": "Second-60", "Depth": "0", "Content-Type": "application/xml"})
		if errors.Is(err, httpFailure(423)) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(80 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			return nil, err
		}
		token := res.Header.Get("Lock-Token")
		res.Body.Close()
		if len(token) < 3 || token[0] != '<' || token[len(token)-1] != '>' {
			return nil, errors.New("storage returned no exclusive lock token")
		}
		return func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = d.command(cleanup, "UNLOCK", key, nil, 0, map[string]string{"Lock-Token": token})
		}, nil
	}
}

func (d *dav) putBytes(ctx context.Context, key string, payload []byte) error {
	return d.command(ctx, "PUT", key, bytes.NewReader(payload), int64(len(payload)), map[string]string{"Content-Type": "application/json"})
}
