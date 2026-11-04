package relay

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// controlHandshakeError preserves the frozen v1 server's explicit rejection.
// coder/websocket retains at most 1024 response bytes on failed handshakes;
// a response at that boundary cannot prove completeness. Never log peer text.
func controlHandshakeError(response *http.Response, err error) error {
	if err == nil || response == nil || response.Body == nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1025))
	if readErr != nil || len(body) >= 1024 {
		return err
	}
	var detail protocol.ErrorBody
	if json.Unmarshal(body, &detail) != nil || detail.Code != protocol.CodeVersionUnsupported {
		return err
	}
	return protocol.Errorf(protocol.CodeVersionUnsupported, "OIS does not support control v2")
}
