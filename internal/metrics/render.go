package metrics

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const MaxResponseBytes = 64 * 1024
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

var ErrTooLarge = errors.New("metrics exposition exceeds fixed response bound")

type boundedText struct {
	bytes []byte
	limit int
	err   error
}

func (b *boundedText) add(text string) {
	if b.err != nil {
		return
	}
	if len(text) > b.limit-len(b.bytes) {
		b.err = ErrTooLarge
		return
	}
	b.bytes = append(b.bytes, text...)
}
func escapeHelp(s string) string { return strings.NewReplacer("\\", "\\\\", "\n", "\\n").Replace(s) }
func escapeLabel(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(s)
}
func labelText(s series, bound string) string {
	var parts []string
	for i, label := range s.labels {
		if label != "" {
			parts = append(parts, label+"=\""+escapeLabel(s.values[i])+"\"")
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

// Render builds a complete bounded response before any successful HTTP header
// is sent. All names and label dimensions come from the fixed registry schema.
func Render(s Snapshot) ([]byte, error) { return render(s, MaxResponseBytes) }
func render(s Snapshot, limit int) ([]byte, error) {
	if limit < 0 {
		return nil, ErrTooLarge
	}
	b := boundedText{bytes: make([]byte, 0, min(limit, MaxResponseBytes)), limit: limit}
	for metric, d := range counters {
		b.add("# HELP " + d.name + " " + escapeHelp(d.help) + "\n# TYPE " + d.name + " counter\n")
		for i, v := range d.series {
			b.add(fmt.Sprintf("%s%s %d\n", d.name, labelText(v, ""), s.counters[metric][i]))
		}
	}
	for metric, d := range histograms {
		b.add("# HELP " + d.name + " " + escapeHelp(d.help) + "\n# TYPE " + d.name + " histogram\n")
		for i, v := range d.series {
			h := s.histograms[metric][i]
			var cumulative uint64
			for bucket := range h.Buckets {
				cumulative += h.Buckets[bucket]
				bound := "+Inf"
				if bucket < len(durationBounds) {
					bound = strconv.FormatFloat(durationBounds[bucket], 'g', -1, 64)
				}
				b.add(fmt.Sprintf("%s_bucket%s %d\n", d.name, labelText(v, bound), cumulative))
			}
			b.add(fmt.Sprintf("%s_sum%s %s\n", d.name, labelText(v, ""), strconv.FormatFloat(h.Sum, 'g', -1, 64)))
			b.add(fmt.Sprintf("%s_count%s %d\n", d.name, labelText(v, ""), h.Count))
		}
	}
	if b.err != nil {
		return nil, b.err
	}
	return b.bytes, nil
}
