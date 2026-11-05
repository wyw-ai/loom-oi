package metrics

// Independent reference copied from Git 7da7159a339986659b18842350ae1edc242e958a
// internal/metrics/render.go (blob 3d0a15ca6b76d5b9bd84cd5d24b72a52efc8104c).
// Only identifiers are renamed and the unchanged public bound/error constants
// are shared. This deliberately does not use compiled prefix tables.

import (
	"fmt"
	"strconv"
	"strings"
)

type legacyBoundedText struct {
	bytes []byte
	limit int
	err   error
}

func (b *legacyBoundedText) add(text string) {
	if b.err != nil {
		return
	}
	if len(text) > b.limit-len(b.bytes) {
		b.err = ErrTooLarge
		return
	}
	b.bytes = append(b.bytes, text...)
}
func legacyEscapeHelp(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n").Replace(s)
}
func legacyEscapeLabel(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(s)
}
func legacyLabelText(s series, bound string) string {
	var parts []string
	for i, label := range s.labels {
		if label != "" {
			parts = append(parts, label+"=\""+legacyEscapeLabel(s.values[i])+"\"")
		}
	}
	if bound != "" {
		parts = append(parts, "le=\""+bound+"\"")
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// legacyRender7da builds a complete bounded response before any successful HTTP header
// is sent. All names and label dimensions come from the fixed registry schema.
func legacyRender7da(s Snapshot) ([]byte, error) { return legacyRenderLimit7da(s, MaxResponseBytes) }
func legacyRenderLimit7da(s Snapshot, limit int) ([]byte, error) {
	if limit < 0 {
		return nil, ErrTooLarge
	}
	b := legacyBoundedText{bytes: make([]byte, 0, min(limit, MaxResponseBytes)), limit: limit}
	for metric, d := range counters {
		b.add("# HELP " + d.name + " " + legacyEscapeHelp(d.help) + "\n# TYPE " + d.name + " counter\n")
		for i, v := range d.series {
			b.add(fmt.Sprintf("%s%s %d\n", d.name, legacyLabelText(v, ""), s.counters[metric][i]))
		}
	}
	for metric, d := range histograms {
		b.add("# HELP " + d.name + " " + legacyEscapeHelp(d.help) + "\n# TYPE " + d.name + " histogram\n")
		for i, v := range d.series {
			h := s.histograms[metric][i]
			var cumulative uint64
			for bucket := range h.Buckets {
				cumulative += h.Buckets[bucket]
				bound := "+Inf"
				if bucket < len(durationBounds) {
					bound = strconv.FormatFloat(durationBounds[bucket], 'g', -1, 64)
				}
				b.add(fmt.Sprintf("%s_bucket%s %d\n", d.name, legacyLabelText(v, bound), cumulative))
			}
			b.add(fmt.Sprintf("%s_sum%s %s\n", d.name, legacyLabelText(v, ""), strconv.FormatFloat(h.Sum, 'g', -1, 64)))
			b.add(fmt.Sprintf("%s_count%s %d\n", d.name, legacyLabelText(v, ""), h.Count))
		}
	}
	if b.err != nil {
		return nil, b.err
	}
	return b.bytes, nil
}
