package protocol

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

const (
	FormatJSON    = "json"
	FormatContent = "content.v1"
)

// Missing capabilities identify a protocol-1 peer that only handles JSON.
// An explicitly empty set has no common formats.
func SupportsFormat(formats []string, format string) bool {
	if format == "" {
		format = FormatJSON
	}
	if formats == nil {
		return format == FormatJSON
	}
	return slices.Contains(formats, format)
}

func NegotiateFormats(offered []string) []string {
	common := []string{}
	for _, format := range []string{FormatJSON, FormatContent} {
		if SupportsFormat(offered, format) {
			common = append(common, format)
		}
	}
	return common
}

// MarshalFrame measures the final representation sent by this hop.
func MarshalFrame(frame Frame, jsonLine bool) ([]byte, error) {
	var message any = frame
	if frame.Type == "invoke" && len(frame.Input) == 0 {
		// An invoke always carries an object, even for parameterless actions.
		// Other frame types keep their legacy omitted input member.
		message = struct {
			Frame
			Input map[string]any `json:"input"`
		}{Frame: frame, Input: map[string]any{}}
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	if jsonLine {
		raw = append(raw, '\n')
	}
	if len(raw) > MaxFrameBytes {
		return nil, Err("RESULT_TOO_LARGE", "protocol frame exceeds the byte limit")
	}
	return raw, nil
}

// ValidateResult checks the explicitly bound format. Application data named
// "content" has no special meaning for a normal JSON action.
func ValidateResult(expected string, f Frame) error {
	if expected == "" {
		expected = FormatJSON
	}
	actual := f.ResultFormat
	if actual == "" {
		actual = FormatJSON
	}
	if expected != actual || (actual != FormatJSON && actual != FormatContent) {
		return Err("RESULT_FORMAT_UNSUPPORTED", "result format does not match the bound action")
	}
	if err := ValidateJSON(f.Result); err != nil {
		return Err("EXECUTION_ERROR", "invalid result JSON")
	}
	if actual == FormatJSON {
		return nil
	}
	return validateContent(f.Result)
}

func validateContent(raw []byte) error {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		return Err("EXECUTION_ERROR", "content.v1 result must be an object")
	}
	for key := range body {
		if key != "content" && key != "structuredContent" {
			return Err("EXECUTION_ERROR", "unexpected field in content.v1 result")
		}
	}
	if meta, ok := body["structuredContent"]; ok && !bytes.HasPrefix(bytes.TrimSpace(meta), []byte("{")) {
		return Err("EXECUTION_ERROR", "structuredContent must be an object")
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(body["content"], &blocks); err != nil || len(blocks) == 0 {
		return Err("EXECUTION_ERROR", "content must be a nonempty block array")
	}
	for _, block := range blocks {
		// Read exact member names. The struct decoder also accepts a
		// case-insensitive match, which would hide malformed MCP blocks.
		switch stringField(block, "type") {
		case "text":
			text := block["text"]
			if len(block) != 2 || len(text) == 0 || text[0] != '"' {
				return Err("EXECUTION_ERROR", "invalid content.v1 text block")
			}
		case "image":
			mimeType := stringField(block, "mimeType")
			data := stringField(block, "data")
			if len(block) != 3 || data == "" || strings.ContainsAny(data, "\r\n") ||
				(mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp") {
				return Err("EXECUTION_ERROR", "invalid content.v1 image block")
			}
			image, err := base64.StdEncoding.Strict().DecodeString(data)
			if err != nil || http.DetectContentType(image) != mimeType {
				return Err("EXECUTION_ERROR", "invalid image data or MIME")
			}
		default:
			return Err("EXECUTION_ERROR", "unsupported content.v1 block type")
		}
	}
	return nil
}

func stringField(block map[string]json.RawMessage, name string) string {
	var value string
	_ = json.Unmarshal(block[name], &value)
	return value
}
