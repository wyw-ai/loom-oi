//go:build oiapps

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/oic"
	"github.com/wyw-ai/loom-oi/internal/ois"
	"gopkg.in/yaml.v3"
)

func TestCreatorApps(t *testing.T) {
	dir := t.TempDir()
	write := func(path string, value any) {
		t.Helper()
		b, err := yaml.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	adminCred := "mgmt-secret"
	t.Setenv("OI_CREATOR_TEST_ADMIN", adminCred)
	for _, id := range []string{"MANAGE", "CATALOG", "BUSINESS"} {
		t.Setenv("OI_CREATOR_TEST_"+id, "consumer-"+id)
	}
	secretPath := filepath.Join(dir, "admin")
	if err := os.WriteFile(secretPath, []byte(adminCred), 0600); err != nil {
		t.Fatal(err)
	}
	from := func(ch, app, act string) object { return object{"channel": ch, "app": app, "action": act} }
	svc := func(cred, ch, app string, acts ...string) object {
		members := []object{}
		for _, act := range acts {
			members = append(members, object{"name": act, "source": from(ch, app, act)})
		}
		return object{"actions": members, "access": object{"credential_env": "OI_CREATOR_TEST_" + cred}, "publish": object{"mcp": true}}
	}
	oisConfig := filepath.Join(dir, "ois.yaml")
	write(oisConfig, object{"version": 1, "admin": object{"token_env": "OI_CREATOR_TEST_ADMIN"}, "channels": object{"ctrl": object{"expected_host_id": "control"}, "one": object{"expected_host_id": "host-a"}}, "services": object{
		"manage":  svc("MANAGE", "ctrl", "loom-oi", "list_hosts", "list_apps", "list_actions", "get_service", "list_services", "apply_service", "delete_service"),
		"catalog": svc("CATALOG", "ctrl", "loom-oi", "list_services"),
		"public": object{"access": object{}, "publish": object{"mcp": true}, "actions": []object{
			{"name": "list_hosts", "source": from("ctrl", "loom-oi", "list_hosts")},
			{"name": "list_skills", "source": from("one", "skills", "list_skills")},
		}}}, "server": object{"data_file": filepath.Join(dir, "ois.json")}})
	center, err := ois.New(oisConfig)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(center.Handler())
	t.Cleanup(func() { _ = center.Close(); server.Close() })
	start := func(hostID, channel, app string, settings object) func() {
		t.Helper()
		manifest := os.Getenv("OI_" + strings.ToUpper(strings.ReplaceAll(app, "-", "_")) + "_MANIFEST")
		if manifest == "" {
			manifest = filepath.Join("..", "..", "apps", app, "dist", app+"-"+runtime.GOOS, "app.yaml")
		}
		path := filepath.Join(dir, hostID+".yaml")
		write(path, object{"version": 1, "host": object{"id": hostID}, "channel": object{"id": channel}, "ois": object{"url": server.URL}, "state_dir": filepath.Join(dir, hostID), "app_config": object{app: settings}})
		host, err := oic.New(path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		end := make(chan error, 1)
		go func() { end <- host.Run(ctx) }()
		stop := sync.OnceFunc(func() {
			cancel()
			if err := <-end; err != nil {
				t.Error(err)
			}
		})
		t.Cleanup(stop)
		waitUntil(t, "connect "+hostID, func() bool { return host.Status().Connected })
		if _, err = host.Load(manifest); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "register "+app, func() bool { apps := host.Apps(); return len(apps) == 1 && apps[0].Ready && apps[0].Registered })
		return stop
	}
	identity := func(service, token string) string { return "consumer:" + service + ":OI_CREATOR_TEST_" + token }
	start("control", "ctrl", "loom-oi", object{"base_url": server.URL, "admin_token_file": secretPath, "callers": []object{
		{"service": "manage", "identity": identity("manage", "MANAGE"), "hosts": []string{"host-a"}, "sources": []object{from("one", "skills", "list_skills"), from("one", "skills", "read_skill")}, "managed_services": []string{"workbench"}},
		{"service": "catalog", "identity": identity("catalog", "CATALOG"), "visible_services": []string{"workbench"}},
	}})
	workspace, home := filepath.Join(dir, "ws"), filepath.Join(dir, "home")
	for _, base := range []string{workspace, home} {
		for _, p := range []string{".agents", ".codex"} {
			pkg := filepath.Join(base, p, "skills", "same")
			if err := os.MkdirAll(pkg, 0700); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{"SKILL.md": "---\nname: skill\ndescription: target\n---\n" + filepath.Base(base) + p, "guide.md": "material-" + filepath.Base(base) + p} {
				if err := os.WriteFile(filepath.Join(pkg, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	stopData := start("host-a", "one", "skills", object{"workspace": workspace, "home": home, "allowed_callers": []object{{"service": "workbench", "identity": identity("workbench", "BUSINESS")}}})
	exec := func(service, action string, args object, success bool) object {
		t.Helper()
		body, _ := json.Marshal(object{"service": service, "action": action, "arguments": args})
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/exec", bytes.NewReader(body))
		cred := map[string]string{"catalog": "CATALOG", "manage": "MANAGE", "workbench": "BUSINESS"}[service]
		if cred != "" {
			req.Header.Set("Authorization", "Bearer consumer-"+cred)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out object
		if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if success != (resp.StatusCode == 200) {
			t.Fatalf("%s %s: %d %+v", service, action, resp.StatusCode, out)
		}
		if !success {
			return out
		}
		if !strings.HasPrefix(fmt.Sprint(out["call_id"]), "call_") {
			t.Fatal(out)
		}
		return out["result"].(object)
	}
	items := func(row object) []any { return row["items"].([]any) }
	for _, name := range []string{"list_hosts", "list_skills"} {
		res := exec("public", name, object{}, false)
		if res["error"].(object)["code"] != "ACCESS_DENIED" || !strings.HasPrefix(fmt.Sprint(res["call_id"]), "call_") {
			t.Fatalf("public Service did not reach the worker under its own identity: %+v", res)
		}
	}
	if rows := items(exec("manage", "list_hosts", object{}, true)); len(rows) != 1 || rows[0].(object)["id"] != "host-a" {
		t.Fatal(rows)
	}
	if rows := items(exec("manage", "list_apps", object{"host_id": "host-a"}, true)); len(rows) != 1 || rows[0].(object)["available"] != true {
		t.Fatal(rows)
	}
	actions := items(exec("manage", "list_actions", object{"app": "skills"}, true))
	if len(actions) != 2 {
		t.Fatal(actions)
	}
	def := svc("BUSINESS", "one", "skills", "list_skills")
	if created := exec("manage", "apply_service", object{"id": "workbench", "service": def}, true); created["created"] != true {
		t.Fatal(created)
	}
	published := items(exec("catalog", "list_services", object{}, true))
	if len(published) != 1 || published[0].(object)["id"] != "workbench" || published[0].(object)["actions"].([]any)[0].(object)["source"].(object)["channel"] != "one" {
		t.Fatal(published)
	}
	existing := exec("manage", "get_service", object{"id": "workbench"}, true)
	def = object{"access": existing["access"], "publish": existing["publish"], "actions": append(existing["actions"].([]any), object{"name": "read_skill", "source": from("one", "skills", "read_skill")})}
	if res := exec("manage", "apply_service", object{"id": "workbench", "service": def}, true); res["created"] != false {
		t.Fatal(res)
	}
	if rows := items(exec("workbench", "list_skills", object{}, true)); len(rows) != 4 {
		t.Fatal(rows)
	}
	filtered := items(exec("workbench", "list_skills", object{"product": "codex"}, true))
	if len(filtered) != 2 {
		t.Fatal(filtered)
	}
	for _, row := range filtered {
		item := row.(object)
		ref := item["ref"]
		data := exec("workbench", "read_skill", object{"ref": ref}, true)
		marker := map[string]string{"workspace": "ws", "home": "home"}[fmt.Sprint(item["scope"])] + ".codex"
		if !strings.Contains(data["text"].(string), marker) {
			t.Fatal(data)
		}
		if aux := exec("workbench", "read_skill", object{"ref": ref, "resource": "guide.md"}, true); aux["text"] != "material-"+marker {
			t.Fatal(aux)
		}
	}
	exec("workbench", "read_skill", object{"ref": filtered[0].(object)["ref"], "resource": "../escape"}, false)
	invalid := svc("BUSINESS", "ctrl", "loom-oi", "delete_service")
	exec("manage", "apply_service", object{"id": "workbench", "service": invalid}, false)
	exec("manage", "delete_service", object{"id": "catalog"}, false)
	if len(exec("manage", "get_service", object{"id": "workbench"}, true)["actions"].([]any)) != 2 {
		t.Fatal("denied update modified service")
	}
	stopData()
	waitUntil(t, "offline snapshot", func() bool {
		return items(exec("manage", "list_apps", object{"host_id": "host-a"}, true))[0].(object)["stale"] == true
	})
	exec("workbench", "list_skills", object{}, false)
	exec("manage", "delete_service", object{"id": "workbench"}, true)
	if rows := items(exec("catalog", "list_services", object{}, true)); len(rows) != 0 {
		t.Fatal(rows)
	}
	exec("workbench", "list_skills", object{}, false)
}
