package metrics

import (
	"errors"
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

func (b *boundedText) numberLine(prefix string, number []byte) {
	if b.err != nil {
		return
	}
	if len(prefix)+len(number)+1 > b.limit-len(b.bytes) {
		b.err = ErrTooLarge
		return
	}
	b.bytes = append(b.bytes, prefix...)
	b.bytes = append(b.bytes, number...)
	b.bytes = append(b.bytes, '\n')
}

func (b *boundedText) uintLine(prefix string, value uint64) {
	var scratch [20]byte
	b.numberLine(prefix, strconv.AppendUint(scratch[:0], value, 10))
}

func (b *boundedText) floatLine(prefix string, value float64) {
	// 'g', -1 float64 needs at most 24 bytes, including sign/exponent.
	var scratch [32]byte
	b.numberLine(prefix, strconv.AppendFloat(scratch[:0], value, 'g', -1, 64))
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

type counterText struct {
	header string
	series []string
}

type histogramSeriesText struct {
	buckets    [len(durationBounds) + 1]string
	sum, count string
}

type histogramText struct {
	header string
	series []histogramSeriesText
}

func compileCounterText(d descriptor) counterText {
	p := counterText{header: "# HELP " + d.name + " " + escapeHelp(d.help) + "\n# TYPE " + d.name + " counter\n", series: make([]string, len(d.series))}
	for i, s := range d.series {
		p.series[i] = d.name + labelText(s, "") + " "
	}
	return p
}

func compileHistogramText(d descriptor) histogramText {
	p := histogramText{header: "# HELP " + d.name + " " + escapeHelp(d.help) + "\n# TYPE " + d.name + " histogram\n", series: make([]histogramSeriesText, len(d.series))}
	for i, s := range d.series {
		for bucket := range p.series[i].buckets {
			bound := "+Inf"
			if bucket < len(durationBounds) {
				bound = strconv.FormatFloat(durationBounds[bucket], 'g', -1, 64)
			}
			p.series[i].buckets[bucket] = d.name + "_bucket" + labelText(s, bound) + " "
		}
		labels := labelText(s, "")
		p.series[i].sum = d.name + "_sum" + labels + " "
		p.series[i].count = d.name + "_count" + labels + " "
	}
	return p
}

// These finite schema-only tables are initialized once and never mutated.
// Strings cannot alias a returned body; no Registry or Snapshot is cached.
var counterPrefixes = func() [counterCount]counterText {
	var p [counterCount]counterText
	for metric, d := range counters {
		p[metric] = compileCounterText(d)
	}
	return p
}()

var histogramPrefixes = func() [histogramCount]histogramText {
	var p [histogramCount]histogramText
	for metric, d := range histograms {
		p[metric] = compileHistogramText(d)
	}
	return p
}()

// Render builds a complete bounded response before any successful HTTP header
// is sent. All names and label dimensions come from the fixed registry schema.
func Render(s Snapshot) ([]byte, error) { return render(s, MaxResponseBytes) }
func render(s Snapshot, limit int) ([]byte, error) {
	if limit < 0 {
		return nil, ErrTooLarge
	}
	b := boundedText{bytes: make([]byte, 0, min(limit, MaxResponseBytes)), limit: limit}
	for metric, p := range counterPrefixes {
		b.add(p.header)
		for i, prefix := range p.series {
			b.uintLine(prefix, s.counters[metric][i])
		}
	}
	for metric, p := range histogramPrefixes {
		b.add(p.header)
		for i, prefixes := range p.series {
			h := s.histograms[metric][i]
			var cumulative uint64
			for bucket := range h.Buckets {
				cumulative += h.Buckets[bucket]
				b.uintLine(prefixes.buckets[bucket], cumulative)
			}
			b.floatLine(prefixes.sum, h.Sum)
			b.uintLine(prefixes.count, h.Count)
		}
	}
	if b.err != nil {
		return nil, b.err
	}
	return b.bytes, nil
}
