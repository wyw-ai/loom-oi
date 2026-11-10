package oic

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBindingRenamesDefaultsAndCoerces(t *testing.T) {
	var action ActionDefinition
	err := yaml.Unmarshal([]byte(`description: fixture
input_mapping: {local: remote}
input_schema:
  type: object
  properties:
    local: {type: integer}
    enabled: {type: boolean, default: " TRUE "}
    label: {type: string}
  required: [local, enabled]
`), &action)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"remote": " 42 ", "label": json.Number("9"), "untouched": "  keep spaces  "}
	got, err := BindInput(input, action)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"local": int64(42), "enabled": true, "label": "9", "untouched": "  keep spaces  "}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if input["remote"] != " 42 " {
		t.Fatal("binding mutated original request")
	}
	for _, bad := range []any{nil, true, 1.2, map[string]any{"n": 1}, []any{1}, "1.2", "no"} {
		_, err := BindInput(map[string]any{"remote": bad}, action)
		if err == nil {
			t.Errorf("accepted %#v as integer", bad)
		}
	}
	if _, err := BindInput(map[string]any{}, action); err == nil {
		t.Fatal("missing required input accepted")
	}
	if _, err := BindInput(map[string]any{"remote": 1, "local": 2}, action); err == nil {
		t.Fatal("ambiguous rename silently overwrote supplied field")
	}
}

func TestBindingMappingSwapAndExplicitNull(t *testing.T) {
	a := ActionDefinition{InputMapping: map[string]string{"a": "b", "b": "a"}, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}, "b": map[string]any{"type": "string"}}}}
	out, err := BindInput(map[string]any{"a": "left", "b": "right"}, a)
	if err != nil {
		t.Fatal(err)
	}
	if out["a"] != "right" || out["b"] != "left" {
		t.Fatal(out)
	}
	a.InputMapping = nil
	a.InputSchema["properties"].(map[string]any)["a"] = map[string]any{"type": "integer", "default": 3}
	if _, err = BindInput(map[string]any{"a": nil}, a); err == nil {
		t.Fatal("explicit null was replaced by default")
	}
}

func TestIntegerConversionDoesNotTruncateOrOverflow(t *testing.T) {
	for _, v := range []any{json.Number("9223372036854775808"), float64(9223372036854775808.0), json.Number("1.5")} {
		if _, ok := integerValue(v); ok {
			t.Fatalf("invalid integer accepted: %v", v)
		}
	}
	if n, ok := integerValue(json.Number("9223372036854775807")); !ok || n != 9223372036854775807 {
		t.Fatal("int64 precision lost")
	}
}
