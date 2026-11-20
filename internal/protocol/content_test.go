package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStrictWireJSON(t *testing.T) {
	for _, raw := range []string{
		"{\"v\":\"\xff\"}", "{\"v\":\"\\ud800\"}", "{\"v\":\"\\udc00\"}", "{\"v\":\"\\ud801\\u0020\"}",
		"{\"v\":{\"one\":1,\"\\u006fne\":2}}", "[{\"\\ud83d\\ude00\":1,\"😀\":2}]", "{} {}", "",
	} {
		if ValidateJSON([]byte(raw)) == nil {
			t.Fatalf("accepted invalid JSON %q", raw)
		}
	}
	for _, raw := range []string{
		"[{\"v\":1}, {\"v\":2}]", "{\"a\":\"\\ud83d\\ude00\",\"b\":\"�\"}", "{\"text\":\"\\\\ud800\"}", "42", "null",
	} {
		if err := ValidateJSON([]byte(raw)); err != nil {
			t.Fatalf("rejected %q: %v", raw, err)
		}
	}
}

func TestResultMustMatchExplicitlyBoundFormat(t *testing.T) {
	const image = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNgAAAAAgABSK+kcQAAAABJRU5ErkJggg=="
	body := "{\"content\":[{\"type\":\"text\",\"text\":\"pixel\"},{\"type\":\"image\",\"mimeType\":\"image/png\",\"data\":\"" + image + "\"}],\"structuredContent\":{\"encoded_bytes\":67}}"
	f := Frame{Result: json.RawMessage(body), ResultFormat: FormatContent}
	if err := ValidateResult(FormatContent, f); err != nil {
		t.Fatal(err)
	}
	if ValidateResult(FormatJSON, f) == nil {
		t.Fatal("JSON action accepted a format change")
	}
	f.ResultFormat = ""
	if err := ValidateResult("", f); err != nil {
		t.Fatal("ordinary JSON with content was not preserved", err)
	}
	if ValidateResult(FormatContent, f) == nil {
		t.Fatal("content action accepted an unmarked response")
	}
	for _, invalid := range []string{
		strings.Replace(body, "image/png", "image/jpeg", 1), strings.Replace(body, image, "abc=", 1),
		strings.Replace(body, "\"text\":\"pixel\"", "\"text\":null", 1), "{\"content\":[{\"type\":\"resource\"}]}",
		"{\"content\":[{\"type\":\"text\",\"TEXT\":\"hi\"}]}", "{\"content\":[{\"type\":\"text\",\"text\":\"x\"}],\"structuredContent\":null}",
		"{\"content\":[]}",
	} {
		if ValidateResult(FormatContent, Frame{Result: json.RawMessage(invalid), ResultFormat: FormatContent}) == nil {
			t.Fatal("accepted invalid content", invalid)
		}
	}
}

func TestPerHopMarshalingBoundaries(t *testing.T) {
	f := ResultFrame("id", "")
	minimal, err := MarshalFrame(f, false)
	if err != nil {
		t.Fatal(err)
	}
	f = ResultFrame("id", strings.Repeat("x", MaxFrameBytes-len(minimal)))
	b, err := MarshalFrame(f, false)
	if err != nil || len(b) != MaxFrameBytes {
		t.Fatal("WebSocket message at boundary", len(b), err)
	}
	var tooLarge *Error
	if _, err := MarshalFrame(f, true); !errors.As(err, &tooLarge) || tooLarge.Code != "RESULT_TOO_LARGE" {
		t.Fatal("JSONL newline must count", err)
	}
	f.Result = bytes.Replace(f.Result, []byte("x"), nil, 1)
	b, err = MarshalFrame(f, true)
	if err != nil || len(b) != MaxFrameBytes || b[len(b)-1] != '\n' {
		t.Fatal("JSONL frame at boundary", len(b), err)
	}
}

func TestInvokeCarriesEmptyInputObjectOnBothHops(t *testing.T) {
	for _, jsonLine := range []bool{false, true} {
		for _, input := range []map[string]any{nil, {}} {
			wire, err := MarshalFrame(Frame{Type: "invoke", CallID: "id", Input: input}, jsonLine)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(wire, &decoded); err != nil || string(decoded["input"]) != "{}" {
				t.Fatalf("missing empty invoke input on jsonLine=%v: %s, %v", jsonLine, wire, err)
			}
		}
	}
}
