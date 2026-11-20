package main

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"os"
	"reflect"
	"testing"
)

func TestPackagedManifestAndToolsShareExpandedSchemas(t *testing.T) {
	t.Chdir("../..")
	manifest, tools, err := configs("./runtime/oiapp-filesystem")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]obj{"app.yaml": manifest, "examples/ois.tools.yaml": tools}
	for name, want := range cases {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var got obj
		if err = yaml.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		normalize := func(v any) any {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var out any
			if err = json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		if !reflect.DeepEqual(normalize(got), normalize(want)) {
			t.Fatalf("%s differs from release builder", name)
		}
	}
	win, _, err := configs("./runtime/oiapp-filesystem.exe")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(win["actions"], manifest["actions"]) {
		t.Fatal("platform manifests disagree on actions")
	}
}
