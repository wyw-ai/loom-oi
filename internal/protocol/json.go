package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// maxJSONDepth bounds nesting so a small hostile body cannot recurse deeply.
const maxJSONDepth = 32

// DecodeStrict decodes exactly one JSON value from r into v, reading at
// most limit bytes. Unknown fields, duplicate keys and trailing data are
// InvalidRequest; an oversize body is PayloadTooLarge (B1 §2 rules 1 and 5).
func DecodeStrict(r io.Reader, limit int64, v any) error {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return Errorf(CodeInvalidRequest, "read body: %v", err)
	}
	if int64(len(data)) > limit {
		return Errorf(CodePayloadTooLarge, "body exceeds %d bytes", limit)
	}
	return DecodeStrictBytes(data, v)
}

// DecodeStrictBytes applies the DecodeStrict rules to an in-memory value.
func DecodeStrictBytes(data []byte, v any) error {
	if err := checkDuplicates(data); err != nil {
		return Errorf(CodeInvalidRequest, "%v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Errorf(CodeInvalidRequest, "%v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Errorf(CodeInvalidRequest, "trailing data after JSON value")
	}
	return nil
}

func checkDuplicates(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return walkJSON(dec, 0)
}

func walkJSON(dec *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("JSON nesting deeper than %d", maxJSONDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '[' {
		for dec.More() {
			if err := walkJSON(dec, depth+1); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		k, _ := key.(string)
		if seen[k] {
			return fmt.Errorf("duplicate key %q", k)
		}
		seen[k] = true
		if err := walkJSON(dec, depth+1); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}
