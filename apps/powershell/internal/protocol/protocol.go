// Package protocol owns the wire types used by this independent App.
// Compatibility with OIC is defined by protocol 1 JSON Lines, not a Go import.
package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const Version = 1
const MaxFrameBytes = 1 << 20

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string        { return e.Code + ": " + e.Message }
func Err(code, message string) *Error { return &Error{Code: code, Message: message} }

type Context struct {
	AccessIdentity string `json:"access_identity"`
	VMCPID         string `json:"vmcp_id"`
}

type Frame struct {
	Type     string          `json:"type,omitempty"`
	Protocol int             `json:"protocol,omitempty"`
	CallID   string          `json:"call_id,omitempty"`
	Handler  string          `json:"handler,omitempty"`
	Handlers []string        `json:"handlers,omitempty"`
	Input    map[string]any  `json:"input,omitempty"`
	Config   map[string]any  `json:"config,omitempty"`
	Context  Context         `json:"context,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *Error          `json:"error,omitempty"`
}

func ID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("random identifier: %w", err))
	}
	return prefix + hex.EncodeToString(b)
}

func ResultFrame(callID string, result any) Frame {
	b, err := json.Marshal(result)
	if err != nil {
		return Frame{Type: "error", CallID: callID, Error: Err("INTERNAL_ERROR", "cannot encode result")}
	}
	return Frame{Type: "result", CallID: callID, Result: b}
}
