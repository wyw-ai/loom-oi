// Package catalog pages the discoverable exposure list with a stable sort
// and an opaque cursor (B1 §3).
package catalog

import (
	"encoding/base64"
	"sort"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// EncodeCursor makes an opaque cursor that resumes after exposureID.
func EncodeCursor(exposureID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(exposureID))
}

// Page returns the items sorted by exposure ID that follow cursor, at most
// limit of them, and the cursor of the next page ("" at the end).
func Page(items []protocol.ExposureView, cursor string, limit int) ([]protocol.ExposureView, string, error) {
	after := ""
	if cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", protocol.Errorf(protocol.CodeInvalidRequest, "bad cursor")
		}
		after = string(b)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ExposureID < items[j].ExposureID })
	start := sort.Search(len(items), func(i int) bool { return items[i].ExposureID > after })
	end := min(start+limit, len(items))
	page := items[start:end]
	next := ""
	if end < len(items) {
		next = EncodeCursor(page[len(page)-1].ExposureID)
	}
	return page, next, nil
}
