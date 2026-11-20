// Package protocol defines the business-neutral messages shared by OIS, OIC,
// and independently hosted applications.
package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const Version = 1
const MCPVersion = "2025-11-25"
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

type HostInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type ActionInfo struct {
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"input_schema"`
	Ready        bool           `json:"ready"`
	ResultFormat string         `json:"result_format,omitempty"`
}

type AppInfo struct {
	App     string       `json:"app"`
	Ready   bool         `json:"ready"`
	Actions []ActionInfo `json:"actions"`
}

// Frame is an envelope, not a schema for application results. Application
// payloads remain opaque to the two generic hosts.
type Frame struct {
	Type          string          `json:"type,omitempty"`
	Protocol      int             `json:"protocol,omitempty"`
	CallID        string          `json:"call_id,omitempty"`
	HostID        string          `json:"host_id,omitempty"`
	ChannelID     string          `json:"channel_id,omitempty"`
	Host          *HostInfo       `json:"host,omitempty"`
	Apps          []AppInfo       `json:"apps,omitempty"`
	App           string          `json:"app,omitempty"`
	Action        string          `json:"action,omitempty"`
	Handler       string          `json:"handler,omitempty"`
	Handlers      []string        `json:"handlers,omitempty"`
	Input         map[string]any  `json:"input,omitempty"`
	Config        map[string]any  `json:"config,omitempty"`
	Context       Context         `json:"context,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	ResultFormat  string          `json:"result_format,omitempty"`
	ResultFormats []string        `json:"result_formats,omitempty"`
	Error         *Error          `json:"error,omitempty"`
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
