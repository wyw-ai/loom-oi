package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestrictedDirectoryAndWrites(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer private" {
			t.Error("wrong management credential")
		}
		if r.URL.Path == "/api/v1/actions" {
			json.NewEncoder(w).Encode(object{"items": []object{
				{"host_id": "a", "channel": "ca", "app": "skills", "action": "read_skill"},
				{"host_id": "b", "channel": "cb", "app": "skills", "action": "read_skill"},
				{"host_id": "a", "channel": "ca", "app": "skills", "action": "unused"},
			}})
			return
		}
		if r.Method == "PUT" {
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(object{"id": "owned"})
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("private\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := caller{"manage", "consumer:manage:TOKEN"}
	rule := accessRule{c.Service, c.Identity, []string{"a"}, []source{{"ca", "skills", "read_skill"}}, nil, []string{"owned"}}
	cfg, _ := json.Marshal(object{"base_url": server.URL, "admin_token_file": tokenFile, "callers": []accessRule{rule}})
	m, err := configure(cfg)
	if err != nil {
		t.Fatal(err)
	}
	request := func(as caller, name string, args object) (any, error) {
		raw, _ := json.Marshal(args)
		return m.call(context.Background(), as, name, raw)
	}
	rows, err := request(c, "list_actions", object{})
	if err != nil {
		t.Fatal(err)
	}
	items := rows.(object)["items"].([]object)
	if len(items) != 1 || items[0]["source"] != (source{"ca", "skills", "read_skill"}) {
		t.Fatal(items)
	}
	svc := object{"actions": []object{{"name": "read", "source": rule.Sources[0]}}, "access": object{"credential_env": "TEST"}, "publish": object{"mcp": true}}
	if _, err = request(c, "apply_service", object{"id": "owned", "service": svc}); err != nil {
		t.Fatal(err)
	}
	before := calls
	svc["actions"] = []object{{"source": source{"cb", "skills", "read_skill"}}}
	for _, tc := range []struct {
		act  string
		args object
	}{
		{"apply_service", object{"id": "owned", "service": svc}}, {"get_service", object{"id": "another"}}, {"delete_service", object{"id": "another"}},
		{"list_apps", object{"host_id": "b"}}, {"apply_service", object{"id": "../outside", "service": svc}},
		{"list_services", object{"identity": "spoof"}},
	} {
		if _, err = request(c, tc.act, tc.args); err == nil {
			t.Fatal(tc.act, "unexpected success")
		}
	}
	if _, err = request(caller{"other", c.Identity}, "list_services", object{}); err == nil {
		t.Fatal("accepted mismatched service")
	}
	if calls != before {
		t.Fatal("out of scope HTTP request")
	}
	if _, err = request(c, "delete_service", object{"id": "owned"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagementUpstreamDoesNotForwardTokenOrDiagnostics(t *testing.T) {
	secret := "test-management-credential"
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("management request followed a redirection")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer other.Close()
	ois := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/hosts":
			http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
		default:
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(object{"error": object{"code": "PROXY_ERROR", "message": r.Header.Get("Authorization")}})
		}
	}))
	defer ois.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	c := caller{"catalog", "consumer:catalog:READ"}
	cfg, _ := json.Marshal(object{"base_url": ois.URL, "admin_token_file": tokenFile, "callers": []object{{"service": c.Service, "identity": c.Identity}}})
	m, err := configure(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"list_hosts", "list_services"} {
		if _, err := m.call(context.Background(), c, action, json.RawMessage("{}")); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("expected a sanitized management error")
		}
	}
}
