package oic

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func validateSchema(s map[string]any, root bool) error {
	t, ok := s["type"].(string)
	if !ok {
		return fmt.Errorf("input_schema requires a type")
	}
	if root && t != "object" {
		return fmt.Errorf("Action input_schema must describe an object")
	}
	switch t {
	case "string", "integer", "boolean", "number", "object", "array":
	default:
		return fmt.Errorf("unsupported Schema type %q", t)
	}
	if props, exists := s["properties"]; exists {
		m, ok := props.(map[string]any)
		if !ok || t != "object" {
			return fmt.Errorf("properties must be an object on an object Schema")
		}
		for name, v := range m {
			sub, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("property %s must be a Schema", name)
			}
			if err := validateSchema(sub, false); err != nil {
				return fmt.Errorf("property %s: %w", name, err)
			}
		}
	}
	if required, exists := s["required"]; exists {
		list, ok := required.([]any)
		if !ok {
			return fmt.Errorf("required must be a string array")
		}
		for _, v := range list {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("required must be a string array")
			}
		}
	}
	if v, exists := s["default"]; exists {
		if _, err := adaptValue(v, s); err != nil {
			return fmt.Errorf("invalid Schema default: %w", err)
		}
	}
	return nil
}

// BindInput applies explicit top-level renaming before local defaults and type
// adaptation. It never compares the consumer and implementation Schemas.
func BindInput(input map[string]any, action ActionDefinition) (map[string]any, error) {
	if input == nil {
		input = map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	// Read all mapping sources from the original input so swaps and cycles are
	// deterministic. A supplied destination may not be silently overwritten.
	for dest, src := range action.InputMapping {
		if dest == src {
			continue
		}
		if _, present := input[src]; !present {
			continue
		}
		if _, exists := input[dest]; exists {
			isSource := false
			for _, v := range action.InputMapping {
				if v == dest {
					isSource = true
					break
				}
			}
			if !isSource {
				return nil, argumentError(dest, "ambiguous mapped destination")
			}
		}
		delete(out, src)
	}
	for dest, src := range action.InputMapping {
		if v, exists := input[src]; exists {
			out[dest] = v
		}
	}
	bound, err := adaptValue(out, action.InputSchema)
	if err != nil {
		return nil, argumentError("input", err.Error())
	}
	return bound.(map[string]any), nil
}

func argumentError(field, message string) *protocol.Error {
	return &protocol.Error{Code: "INVALID_ARGUMENT", Message: message, Details: map[string]any{"field": field}}
}

func adaptValue(v any, s map[string]any) (any, error) {
	t, _ := s["type"].(string)
	if v == nil {
		return nil, fmt.Errorf("null cannot be converted to %s", t)
	}
	switch t {
	case "string":
		if str, ok := v.(string); ok {
			return str, nil
		}
		if n, ok := integerValue(v); ok {
			return strconv.FormatInt(n, 10), nil
		}
	case "integer":
		if str, ok := v.(string); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(str), 10, 64)
			if err == nil {
				return n, nil
			}
		} else if n, ok := integerValue(v); ok {
			return n, nil
		}
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		if str, ok := v.(string); ok {
			switch strings.ToLower(strings.TrimSpace(str)) {
			case "true":
				return true, nil
			case "false":
				return false, nil
			}
		}
	case "number":
		switch n := v.(type) {
		case float64:
			if !math.IsNaN(n) && !math.IsInf(n, 0) {
				return n, nil
			}
		case json.Number:
			f, err := n.Float64()
			if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				return f, nil
			}
		default:
			if n, ok := integerValue(v); ok {
				return n, nil
			}
		}
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			break
		}
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = v
		}
		props, _ := s["properties"].(map[string]any)
		for name, raw := range props {
			sub, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid Schema for %s", name)
			}
			value, exists := out[name]
			if !exists {
				value, exists = sub["default"]
			}
			if exists {
				n, err := adaptValue(value, sub)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", name, err)
				}
				out[name] = n
			}
		}
		if required, ok := s["required"].([]any); ok {
			for _, raw := range required {
				name, _ := raw.(string)
				if _, exists := out[name]; !exists {
					return nil, fmt.Errorf("required field %s is missing", name)
				}
			}
		}
		return out, nil
	case "array":
		if a, ok := v.([]any); ok {
			return a, nil
		}
	}
	return nil, fmt.Errorf("%T cannot be converted to %s", v, t)
}

func integerValue(v any) (int64, bool) {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return i, true
		}
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return integralFloat(f)
	}
	r := reflect.ValueOf(v)
	if !r.IsValid() {
		return 0, false
	}
	switch r.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return r.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := r.Uint()
		if n <= math.MaxInt64 {
			return int64(n), true
		}
	case reflect.Float32, reflect.Float64:
		return integralFloat(r.Float())
	}
	return 0, false
}

func integralFloat(n float64) (int64, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < math.MinInt64 || n >= 9223372036854775808.0 {
		return 0, false
	}
	return int64(n), true
}
