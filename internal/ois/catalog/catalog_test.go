package catalog

import (
	"fmt"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestPaging(t *testing.T) {
	var items []protocol.ExposureView
	for _, id := range []string{"b/x", "a/z", "a/y", "c/w", "a/x"} {
		items = append(items, protocol.ExposureView{ExposureID: id})
	}
	var got []string
	cursor := ""
	for pages := 0; ; pages++ {
		page, next, err := Page(items, cursor, 2)
		if err != nil || pages > 5 {
			t.Fatal(err)
		}
		for _, it := range page {
			got = append(got, it.ExposureID)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if fmt.Sprint(got) != "[a/x a/y a/z b/x c/w]" {
		t.Fatal(got)
	}
	page, next, _ := Page(items, EncodeCursor("zzz"), 10)
	if len(page) != 0 || next != "" {
		t.Fatal("past end")
	}
	for _, bad := range []string{"!!", "A"} {
		if _, _, err := Page(items, bad, 1); protocol.CodeOf(err) != protocol.CodeInvalidRequest {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
