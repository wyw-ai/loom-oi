// Package contract validates the same input definitions that ship in the manifest.
package contract

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schemas.json
var schemas []byte

var Handlers = []string{"list_directory", "find_files", "search_content", "read_file", "write_file", "edit_file", "view_image"}

func Compile() (map[string]*jsonschema.Schema, error) {
	var doc any
	if err := json.Unmarshal(schemas, &doc); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	const id = "https://loom-oi.invalid/filesystem.schema.json"
	if err := c.AddResource(id, doc); err != nil {
		return nil, err
	}
	out := map[string]*jsonschema.Schema{}
	for _, key := range append([]string{"config"}, Handlers...) {
		def := key
		if key != "config" {
			def += "_input"
		}
		s, err := c.Compile(id + "#/$defs/" + def)
		if err != nil {
			return nil, err
		}
		out[key] = s
	}
	return out, nil
}

// Parse checks names and surrogate pairs before encoding/json can overwrite or replace them.
func Parse(raw []byte) (any, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, errors.New("invalid JSON or UTF-8")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		n, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		i += 4
		if n >= 0xD800 && n <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return nil, errors.New("unpaired high surrogate")
			}
			m, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || m < 0xDC00 || m > 0xDFFF {
				return nil, errors.New("unpaired high surrogate")
			}
			i += 6
		} else if n >= 0xDC00 && n <= 0xDFFF {
			return nil, errors.New("unpaired low surrogate")
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := value(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing data")
	}
	return v, nil
}

func value(d *json.Decoder, depth int) (any, error) {
	if depth > 128 {
		return nil, errors.New("JSON nesting exceeds 128")
	}
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		obj := map[string]any{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return nil, err
			}
			key := k.(string)
			if _, exists := obj[key]; exists {
				return nil, fmt.Errorf("duplicate JSON member")
			}
			val, err := value(d, depth+1)
			if err != nil {
				return nil, err
			}
			obj[key] = val
		}
		_, err = d.Token()
		return obj, err
	case json.Delim('['):
		a := []any{}
		for d.More() {
			v, err := value(d, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err = d.Token()
		return a, err
	default:
		return tok, nil
	}
}
