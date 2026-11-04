package client

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// SetMaintenance enters or leaves maintenance mode at the current
// config_version and returns the configuration afterwards (B3 §8).
func (c *Client) SetMaintenance(ctx context.Context, on bool) (protocol.ConfigView, error) {
	var cv protocol.ConfigView
	if _, err := c.Do(ctx, http.MethodGet, "/v1/admin/config", "", nil, &cv); err != nil {
		return cv, err
	}
	if _, err := c.Do(ctx, http.MethodPut, "/v1/admin/maintenance", strconv.FormatInt(cv.ConfigVersion, 10),
		protocol.MaintenanceSpec{Maintenance: &on}, nil); err != nil {
		return cv, err
	}
	_, err := c.Do(ctx, http.MethodGet, "/v1/admin/config", "", nil, &cv)
	return cv, err
}

// ExportAudit pages through the audit log up to throughSeq (everything when
// throughSeq is 0) and returns the records with their canonical digest,
// which a later prune must match (§8.1.1).
func (c *Client) ExportAudit(ctx context.Context, throughSeq int64) (protocol.AuditExport, error) {
	out := protocol.AuditExport{Items: []protocol.AuditView{}}
	var after int64
	for done := false; !done; {
		var page protocol.AuditList
		if _, err := c.Do(ctx, http.MethodGet, "/v1/admin/audit?"+url.Values{"after": {strconv.FormatInt(after, 10)}}.Encode(), "", nil, &page); err != nil {
			return out, err
		}
		done = len(page.Items) == 0
		for _, a := range page.Items {
			if throughSeq > 0 && a.Seq > throughSeq {
				done = true
				break
			}
			out.Items = append(out.Items, a)
			after = a.Seq
		}
	}
	out.ThroughSeq = throughSeq
	if throughSeq == 0 {
		out.ThroughSeq = after
	}
	out.Count = len(out.Items)
	var err error
	out.SHA256, err = protocol.AuditDigest(out.Items)
	return out, err
}

// OISAdmin runs `ois admin resume`, which leaves maintenance mode on a
// running OIS after a restore (B3 §8). cmd/ois routes `ois admin` here so
// the server package does not depend on the client.
func OISAdmin(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ois admin resume", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts Options
	fs.StringVar(&opts.URL, "ois", os.Getenv("OI_URL"), "")
	fs.StringVar(&opts.Token, "token", os.Getenv("OI_TOKEN"), "")
	fs.StringVar(&opts.CAFile, "ca", os.Getenv("OI_CA_FILE"), "")
	if len(args) == 0 || args[0] != "resume" || fs.Parse(args[1:]) != nil || fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: ois admin resume [--ois URL] [--token T] [--ca FILE] (defaults: OI_URL, OI_TOKEN, OI_CA_FILE)")
		return 2
	}
	c, err := New(opts)
	if err != nil {
		fmt.Fprintln(stderr, "ois admin resume:", err)
		return 2
	}
	defer c.CloseIdleConnections()
	cv, err := c.SetMaintenance(ctx, false)
	if err != nil {
		fmt.Fprintln(stderr, "ois admin resume:", err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(cv)
	return 0
}
