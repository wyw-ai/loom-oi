package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/oic"
	"github.com/wyw-ai/loom-oi/internal/ois"
	"gopkg.in/yaml.v3"
)

// The standalone App distributions cannot import the project documentation.
// Check their embedded contracts and the consumer bindings at the repository boundary.
func TestOfficialAppContracts(t *testing.T) {
	base := "../../docs/spec/reference/oiapps"
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var center ois.Config
	if err := yaml.Unmarshal(read(filepath.Join(base, "examples/ois.yaml")), &center); err != nil {
		t.Fatal(err)
	}
	exposed := make(map[string]bool)
	for _, name := range center.VMCPS["workbench"].Tools {
		if exposed[name] {
			t.Fatalf("duplicate Tool in workbench: %s", name)
		}
		exposed[name] = true
	}
	checked := 0
	for _, app := range []string{"filesystem", "browser"} {
		source := filepath.Join("../../apps", app)
		var documented, embedded any
		if err := json.Unmarshal(read(filepath.Join(base, app+".schemas.json")), &documented); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(read(filepath.Join(source, "contract/schemas.json")), &embedded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(documented, embedded) {
			t.Fatalf("%s input/output definitions differ from the published contract", app)
		}
		var manifest, example oic.AppDefinition
		if err := yaml.Unmarshal(read(filepath.Join(source, "app.yaml")), &manifest); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(read(filepath.Join(base, "examples", app, "app.yaml")), &example); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(manifest, example) || manifest.App.Code != app {
			t.Fatalf("%s source and example manifests differ", app)
		}
		for name, action := range manifest.Actions {
			toolName := app + "_" + name
			tool := center.Tools[toolName]
			if !exposed[toolName] || tool.Source != (ois.Source{Channel: "local-workstation", App: app, Action: name}) || !reflect.DeepEqual(tool.InputSchema, action.InputSchema) {
				t.Fatalf("%s Tool does not expose its declared Action", toolName)
			}
			checked++
		}
	}
	if checked != 25 || len(exposed) != checked || len(center.Tools) != checked {
		t.Fatalf("expected 25 bindings, got %d declared, %d tools and %d exposed", checked, len(center.Tools), len(exposed))
	}
}
