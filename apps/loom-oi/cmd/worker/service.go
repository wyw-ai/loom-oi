package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var handlers = []string{"list_hosts", "list_apps", "list_actions", "list_services", "get_service", "apply_service", "delete_service"}
var identifier = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$")

type source struct {
	Channel string `json:"channel"`
	App     string `json:"app"`
	Action  string `json:"action"`
}

func (s source) valid() bool {
	return identifier.MatchString(s.Channel) && identifier.MatchString(s.App) && identifier.MatchString(s.Action)
}

type accessRule struct {
	Service  string   `json:"service"`
	Identity string   `json:"identity"`
	Hosts    []string `json:"hosts"`
	Sources  []source `json:"sources"`
	Visible  []string `json:"visible_services"`
	Managed  []string `json:"managed_services"`
}
type management struct {
	base, token string
	rules       map[caller]accessRule
	http        *http.Client
}

func configure(raw json.RawMessage) (*management, error) {
	var cfg struct {
		Base      string       `json:"base_url"`
		TokenFile string       `json:"admin_token_file"`
		Rules     []accessRule `json:"callers"`
	}
	if err := decode(raw, &cfg); err != nil {
		return nil, err
	}
	u, err := url.Parse(cfg.Base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, fmt.Errorf("base_url must be an OIS HTTP(S) origin")
	}
	if !filepath.IsAbs(cfg.TokenFile) {
		return nil, fmt.Errorf("admin_token_file must be absolute")
	}
	token, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read admin_token_file")
	}
	secret := strings.TrimSpace(strings.TrimPrefix(string(token), "\uFEFF"))
	if secret == "" || strings.ContainsAny(secret, "\r\n") {
		return nil, fmt.Errorf("invalid admin_token_file")
	}
	m := &management{base: strings.TrimRight(cfg.Base, "/") + "/api/v1/", token: secret, rules: map[caller]accessRule{},
		http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	for _, rule := range cfg.Rules {
		key := caller{rule.Service, rule.Identity}
		if !identifier.MatchString(key.Service) || key.Identity == "" {
			return nil, fmt.Errorf("each caller needs service and identity")
		}
		if _, exists := m.rules[key]; exists {
			return nil, fmt.Errorf("duplicate caller")
		}
		for _, ids := range [][]string{rule.Hosts, rule.Visible, rule.Managed} {
			for _, id := range ids {
				if !identifier.MatchString(id) {
					return nil, fmt.Errorf("invalid granted id")
				}
			}
		}
		for _, s := range rule.Sources {
			if !s.valid() {
				return nil, fmt.Errorf("invalid granted source")
			}
		}
		m.rules[key] = rule
	}
	return m, nil
}

type object = map[string]any

func (m *management) request(ctx context.Context, method, path string, body any) (object, int, error) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, m.base+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := m.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, problem("OIS_ERROR", "OIS management request failed")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent {
		return nil, res.StatusCode, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// Management proxies and upstream diagnostics can echo request
		// headers. Only publish the status, never the response body.
		code := "OIS_ERROR"
		if res.StatusCode == http.StatusNotFound {
			code = "NOT_FOUND"
		}
		return nil, res.StatusCode, problem(code, fmt.Sprintf("OIS returned HTTP %d", res.StatusCode))
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, 16<<20+1))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if len(payload) > 16<<20 {
		return nil, res.StatusCode, problem("RESULT_TOO_LARGE", "OIS directory exceeds read budget")
	}
	var v object
	if err = json.Unmarshal(payload, &v); err != nil || v == nil {
		return nil, res.StatusCode, problem("OIS_ERROR", "invalid OIS response")
	}
	return v, res.StatusCode, nil
}
func denied() error                           { return problem("ACCESS_DENIED", "target is outside the caller's grant") }
func stringKey(row object, key string) string { v, _ := row[key].(string); return v }
func selectKeys(row object, keys ...string) object {
	out := object{}
	for _, k := range keys {
		if v, ok := row[k]; ok {
			out[k] = v
		}
	}
	return out
}
func list(v any) ([]object, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, problem("OIS_ERROR", "expected a directory list")
	}
	out := make([]object, 0, len(items))
	for _, i := range items {
		row, ok := i.(map[string]any)
		if !ok {
			return nil, problem("OIS_ERROR", "malformed directory row")
		}
		out = append(out, row)
	}
	return out, nil
}
func fromRow(row object) source {
	return source{stringKey(row, "channel"), stringKey(row, "app"), stringKey(row, "action")}
}
func (m *management) directory(ctx context.Context, r accessRule, action string, raw json.RawMessage) (any, error) {
	var host, app, path string
	switch action {
	case "list_hosts", "list_services":
		if err := decode(raw, &struct{}{}); err != nil {
			return nil, err
		}
		path = map[string]string{"list_hosts": "hosts", "list_services": "services"}[action]
	case "list_apps":
		var args struct {
			Host string `json:"host_id"`
		}
		if err := decode(raw, &args); err != nil {
			return nil, err
		}
		host = args.Host
		if !identifier.MatchString(host) {
			return nil, problem("INVALID_ARGUMENT", "valid host_id required")
		}
		path = "hosts/" + host + "/apps"
	case "list_actions":
		var args struct {
			Host string `json:"host_id"`
			App  string `json:"app"`
		}
		if err := decode(raw, &args); err != nil {
			return nil, err
		}
		host, app = args.Host, args.App
		q := url.Values{}
		if host != "" {
			q.Set("host_id", host)
		}
		if app != "" {
			q.Set("app", app)
		}
		path = "actions?" + q.Encode()
	}
	if host != "" && !slices.Contains(r.Hosts, host) {
		return nil, denied()
	}
	body, _, err := m.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	rows, err := list(body["items"])
	if err != nil {
		return nil, err
	}
	out := []object{}
	for _, row := range rows {
		switch action {
		case "list_hosts":
			if slices.Contains(r.Hosts, stringKey(row, "id")) {
				out = append(out, selectKeys(row, "id", "name", "os", "arch", "online"))
			}
		case "list_apps":
			if stringKey(row, "host_id") == host {
				out = append(out, selectKeys(row, "host_id", "channel", "app", "ready", "available", "stale"))
			}
		case "list_actions":
			s := fromRow(row)
			if slices.Contains(r.Hosts, stringKey(row, "host_id")) && slices.Contains(r.Sources, s) {
				item := selectKeys(row, "host_id", "channel", "app", "action", "description", "input_schema", "result_format", "ready", "available", "stale")
				item["source"] = s
				out = append(out, item)
			}
		case "list_services":
			id := stringKey(row, "id")
			if !slices.Contains(r.Visible, id) && !slices.Contains(r.Managed, id) {
				continue
			}
			members, err := list(row["actions"])
			if err != nil {
				return nil, err
			}
			summary := make([]object, 0, len(members))
			for _, member := range members {
				summary = append(summary, selectKeys(member, "name", "source"))
			}
			entry := selectKeys(row, "id", "publish", "endpoint")
			entry["actions"] = summary
			out = append(out, entry)
		}
	}
	return object{"items": out}, nil
}
func (m *management) call(ctx context.Context, c caller, action string, raw json.RawMessage) (any, error) {
	rule, ok := m.rules[c]
	if !ok {
		return nil, denied()
	}
	switch action {
	case "list_hosts", "list_apps", "list_actions", "list_services":
		return m.directory(ctx, rule, action, raw)
	case "get_service", "delete_service":
		var args struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &args); err != nil {
			return nil, err
		}
		if !identifier.MatchString(args.ID) {
			return nil, problem("INVALID_ARGUMENT", "valid service id required")
		}
		managed := slices.Contains(rule.Managed, args.ID)
		if action == "get_service" {
			if !managed && !slices.Contains(rule.Visible, args.ID) {
				return nil, denied()
			}
			res, _, err := m.request(ctx, "GET", "services/"+args.ID, nil)
			return res, err
		}
		if !managed {
			return nil, denied()
		}
		_, _, err := m.request(ctx, "DELETE", "services/"+args.ID, nil)
		return object{"id": args.ID}, err
	case "apply_service":
		var args struct {
			ID      string `json:"id"`
			Service struct {
				Actions *[]struct {
					Name        string         `json:"name,omitempty"`
					Source      source         `json:"source"`
					Description string         `json:"description,omitempty"`
					Schema      map[string]any `json:"input_schema,omitempty"`
				} `json:"actions"`
				Access *struct {
					Credential string `json:"credential_env,omitempty"`
					HTTPS      bool   `json:"require_https,omitempty"`
				} `json:"access"`
				Publish *struct {
					MCP bool `json:"mcp"`
				} `json:"publish"`
			} `json:"service"`
		}
		if err := decode(raw, &args); err != nil {
			return nil, err
		}
		if !identifier.MatchString(args.ID) || args.Service.Actions == nil || args.Service.Access == nil || args.Service.Publish == nil {
			return nil, problem("INVALID_ARGUMENT", "id and complete actions, access and publish required")
		}
		if !slices.Contains(rule.Managed, args.ID) {
			return nil, denied()
		}
		for _, member := range *args.Service.Actions {
			if !slices.Contains(rule.Sources, member.Source) {
				return nil, denied()
			}
		}
		res, status, err := m.request(ctx, "PUT", "services/"+args.ID, args.Service)
		if err != nil {
			return nil, err
		}
		return object{"created": status == 201, "service": res}, nil
	default:
		return nil, problem("INVALID_ARGUMENT", "unknown action")
	}
}
