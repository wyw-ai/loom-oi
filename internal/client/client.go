// Package client is the Go consumer library of OI and the core of the oi
// command: discover exposures, open channels and use them as byte streams
// (B4 §4). It speaks only the v1 consumer and admin APIs (B1 §3).
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Options configure a Client.
type Options struct {
	// URL is the OIS api listener, https://host:port.
	URL   string
	Token string
	// CAFile holds the CA that signed the OIS certificate; empty means the
	// system roots.
	CAFile     string
	ServerName string
	// DataMax is F, the largest DATA payload sent (default 32 KiB, B4 §3).
	DataMax int
	// CloseGrace bounds leg shutdown (default T_close).
	CloseGrace time.Duration
}

// Client talks to one OIS with one bearer token.
type Client struct {
	base       *url.URL
	token      string
	http       *http.Client
	dataMax    int
	closeGrace time.Duration
}

// New validates the options and prepares the HTTPS client.
func New(o Options) (*Client, error) {
	u, err := url.Parse(o.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return nil, fmt.Errorf("OIS URL must be https://host:port, got %q", o.URL)
	}
	if o.Token == "" {
		return nil, errors.New("a bearer token is required (OI_TOKEN)")
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: o.ServerName, NextProtos: []string{"http/1.1"}}
	if o.CAFile != "" {
		pem, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, err
		}
		tc.RootCAs = x509.NewCertPool()
		if !tc.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s holds no PEM certificate", o.CAFile)
		}
	}
	d := limits.Defaults()
	if o.DataMax <= 0 {
		o.DataMax = d.DataMax
	}
	if o.CloseGrace <= 0 {
		o.CloseGrace = limits.D(d.CloseMS)
	}
	u.Path = ""
	return &Client{base: u, token: o.Token, http: &http.Client{Transport: &http.Transport{TLSClientConfig: tc}},
		dataMax: o.DataMax, closeGrace: o.CloseGrace}, nil
}

// CloseIdleConnections releases idle keep-alive connections to OIS.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

// apiError turns an error body into a coded error.
func apiError(status int, body []byte) error {
	var e protocol.ErrorBody
	if json.Unmarshal(body, &e) != nil || e.Code == "" {
		return protocol.Errorf(protocol.CodeInternal, "HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	return &protocol.Error{Code: e.Code, Message: e.Message, Reason: e.Reason, ConfigVersion: e.ConfigVersion}
}

// Do performs one API call. ifMatch, when not empty, is sent as the
// quoted config_version (B3 §3). It returns the HTTP status.
func (c *Client) Do(ctx context.Context, method, path, ifMatch string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", strconv.Quote(ifMatch))
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return res.StatusCode, err
	}
	if res.StatusCode >= 300 {
		return res.StatusCode, apiError(res.StatusCode, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return res.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	}
	return res.StatusCode, nil
}

// Discover returns one page of visible exposures.
func (c *Client) Discover(ctx context.Context, limit int, cursor string) (protocol.ExposureList, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	path := "/v1/exposures"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var l protocol.ExposureList
	_, err := c.Do(ctx, http.MethodGet, path, "", nil, &l)
	return l, err
}

// DiscoverAll follows the cursor through every page.
func (c *Client) DiscoverAll(ctx context.Context) ([]protocol.ExposureView, error) {
	var all []protocol.ExposureView
	cursor := ""
	for {
		page, err := c.Discover(ctx, 0, cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.NextCursor == "" {
			return all, nil
		}
		cursor = page.NextCursor
	}
}

// Lookup finds one visible exposure.
func (c *Client) Lookup(ctx context.Context, exposureID string) (protocol.ExposureView, error) {
	all, err := c.DiscoverAll(ctx)
	if err != nil {
		return protocol.ExposureView{}, err
	}
	for _, e := range all {
		if e.ExposureID == exposureID {
			return e, nil
		}
	}
	return protocol.ExposureView{}, protocol.Errorf(protocol.CodeNotFound, "exposure %s is not visible", exposureID)
}

// Open creates a channel (B1 §3, §7).
func (c *Client) Open(ctx context.Context, req protocol.OpenRequest) (protocol.OpenResponse, error) {
	var r protocol.OpenResponse
	_, err := c.Do(ctx, http.MethodPost, "/v1/channels", "", req, &r)
	return r, err
}

// Channel reads a channel's state.
func (c *Client) Channel(ctx context.Context, id string) (protocol.ChannelView, error) {
	var v protocol.ChannelView
	_, err := c.Do(ctx, http.MethodGet, "/v1/channels/"+url.PathEscape(id), "", nil, &v)
	return v, err
}

// Close closes a channel (ClosedByClient).
func (c *Client) Close(ctx context.Context, id string) (protocol.ChannelView, error) {
	var v protocol.ChannelView
	_, err := c.Do(ctx, http.MethodDelete, "/v1/channels/"+url.PathEscape(id), "", nil, &v)
	return v, err
}

// AttachLeg upgrades the consumer leg of an opened channel without waiting
// for READY. The ticket travels only in the OI-Attach-Ticket header, and
// the bearer token only to the configured OIS (B2 §3).
func (c *Client) AttachLeg(ctx context.Context, o protocol.OpenResponse) (*websocket.Conn, error) {
	if o.Attach == nil {
		return nil, protocol.Errorf(protocol.CodeTicketConsumed, "the attach ticket of %s was already used", o.ChannelID)
	}
	u, err := url.Parse(o.Attach.URL)
	if err != nil || u.Scheme != "wss" || u.Host != c.base.Host || u.RawQuery != "" {
		return nil, protocol.Errorf(protocol.CodeInvalidRequest, "attach URL %q does not belong to %s", o.Attach.URL, c.base.Host)
	}
	h := http.Header{"Authorization": {"Bearer " + c.token}, protocol.HeaderAttachTicket: {o.Attach.Ticket}}
	leg, res, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: c.http, HTTPHeader: h,
		Subprotocols: []string{protocol.TunnelSubprotocol}})
	if err != nil {
		if res != nil && res.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
			if res.StatusCode >= 300 {
				return nil, apiError(res.StatusCode, body)
			}
		}
		return nil, err
	}
	leg.SetReadLimit(int64(max(c.dataMax, 1<<20) + 3 + protocol.MaxAbortDetail))
	return leg, nil
}

// OpenLatest opens exposureID at its current revision; a revision that
// changed between discovery and Open is rediscovered once (B1 §7).
func (c *Client) OpenLatest(ctx context.Context, exposureID, requestID string, requireReady bool) (protocol.OpenResponse, error) {
	var last error
	for range 2 {
		e, err := c.Lookup(ctx, exposureID)
		if err != nil {
			return protocol.OpenResponse{}, err
		}
		req := protocol.OpenRequest{ExposureID: exposureID, ExpectedRevision: e.Revision, ClientRequestID: requestID}
		if requireReady {
			req.Admission = protocol.AdmissionRequireReady
		}
		o, err := c.Open(ctx, req)
		if protocol.CodeOf(err) != protocol.CodeRevisionMismatch {
			return o, err
		}
		last = err
	}
	return protocol.OpenResponse{}, last
}
