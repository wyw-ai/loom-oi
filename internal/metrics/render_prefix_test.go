package metrics

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

type renderCase struct {
	name     string
	snapshot Snapshot
}

func populatedRenderSnapshot(edges bool) Snapshot {
	var r Registry
	for metric, d := range counters {
		for _, s := range d.series {
			for range 3 {
				r.Add(Counter(metric), s.key)
			}
		}
	}
	for metric, d := range histograms {
		for _, s := range d.series {
			r.Observe(Histogram(metric), s.key, 0)
			for _, bound := range durationBounds {
				duration := time.Duration(bound * float64(time.Second))
				if edges {
					r.Observe(Histogram(metric), s.key, duration-time.Nanosecond)
				}
				r.Observe(Histogram(metric), s.key, duration)
				if edges {
					r.Observe(Histogram(metric), s.key, duration+time.Nanosecond)
				}
			}
			r.Observe(Histogram(metric), s.key, 11*time.Second)
		}
	}
	return r.Snapshot()
}

func extremeRenderSnapshot() Snapshot {
	var s Snapshot
	for metric, d := range counters {
		for i := range d.series {
			s.counters[metric][i] = math.MaxUint64
		}
	}
	for metric, d := range histograms {
		for i := range d.series {
			h := &s.histograms[metric][i]
			h.Count, h.Sum = math.MaxUint64, math.MaxFloat64
			for bucket := range h.Buckets {
				if bucket%2 == 0 {
					h.Buckets[bucket] = math.MaxUint64
				} else {
					h.Buckets[bucket] = 2
				}
			}
		}
	}
	return s
}

func renderCases() []renderCase {
	cases := []renderCase{{"zero", Snapshot{}}, {"populated", populatedRenderSnapshot(false)}, {"bucket_edges", populatedRenderSnapshot(true)}, {"uint64_max_and_wrap", extremeRenderSnapshot()}}
	for _, tc := range []struct {
		name  string
		value float64
	}{
		{"smallest_float", math.SmallestNonzeroFloat64}, {"largest_float", math.MaxFloat64},
		{"exponent_float", 1.2345678901234567e-100}, {"negative_zero", math.Copysign(0, -1)},
		{"nan", math.NaN()}, {"positive_infinity", math.Inf(1)}, {"negative_infinity", math.Inf(-1)},
	} {
		s := populatedRenderSnapshot(false)
		for metric, d := range histograms {
			for i := range d.series {
				s.histograms[metric][i].Sum = tc.value
			}
		}
		cases = append(cases, renderCase{tc.name, s})
	}
	return cases
}

func TestMetricsRenderReference(t *testing.T) {
	if MaxResponseBytes != 65536 {
		t.Fatal("original public bound changed")
	}
	for _, tc := range renderCases() {
		t.Run(tc.name, func(t *testing.T) {
			want, oldErr := legacyRender7da(tc.snapshot)
			got, err := Render(tc.snapshot)
			if err != oldErr || !bytes.Equal(got, want) {
				t.Fatalf("output changed: new=%d old=%d errors=%v/%v", len(got), len(want), err, oldErr)
			}
		})
	}
}

func TestMetricsRenderLimitReference(t *testing.T) {
	for _, tc := range []renderCase{{"zero", Snapshot{}}, {"populated", populatedRenderSnapshot(true)}, {"extreme", extremeRenderSnapshot()}} {
		t.Run(tc.name, func(t *testing.T) {
			full, err := legacyRender7da(tc.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			for _, boundary := range []struct {
				name  string
				limit int
			}{{"negative", -1}, {"zero", 0}, {"one_short", len(full) - 1}, {"exact", len(full)}, {"one_spare", len(full) + 1}, {"public_max", MaxResponseBytes}} {
				t.Run(boundary.name, func(t *testing.T) {
					want, oldErr := legacyRenderLimit7da(tc.snapshot, boundary.limit)
					got, err := render(tc.snapshot, boundary.limit)
					if err != oldErr || !bytes.Equal(got, want) {
						t.Fatal("limit/error bytes differ", boundary.limit, err, oldErr)
					}
					if boundary.limit < len(full) && (err != ErrTooLarge || got != nil) {
						t.Fatal("partial output escaped failed bound")
					}
				})
			}
		})
	}
}

func TestMetricsNumericLineCapacity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		number string
		write  func(*boundedText, string)
	}{
		{"uint64_max", "18446744073709551615", func(b *boundedText, p string) { b.uintLine(p, math.MaxUint64) }},
		{"float64_max", "1.7976931348623157e+308", func(b *boundedText, p string) { b.floatLine(p, math.MaxFloat64) }},
		{"negative_zero", "-0", func(b *boundedText, p string) { b.floatLine(p, math.Copysign(0, -1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := strings.Repeat("x", MaxResponseBytes-len(tc.number)-1)
			b := boundedText{bytes: make([]byte, 0, MaxResponseBytes), limit: MaxResponseBytes}
			tc.write(&b, prefix)
			if b.err != nil || string(b.bytes) != prefix+tc.number+"\n" || len(b.bytes) != MaxResponseBytes || cap(b.bytes) != MaxResponseBytes {
				t.Fatal("exact public numeric boundary changed")
			}
			before := append([]byte(nil), b.bytes...)
			tc.write(&b, "next ")
			if b.err != ErrTooLarge || !bytes.Equal(b.bytes, before) || cap(b.bytes) != MaxResponseBytes {
				t.Fatal("over-limit line mutated/expanded buffer")
			}
			b = boundedText{bytes: make([]byte, 0, MaxResponseBytes-1), limit: MaxResponseBytes - 1}
			tc.write(&b, prefix)
			if b.err != ErrTooLarge || len(b.bytes) != 0 || cap(b.bytes) != MaxResponseBytes-1 {
				t.Fatal("append happened before capacity check")
			}
			tc.write(&b, "ignored ")
			if len(b.bytes) != 0 {
				t.Fatal("failed builder resumed")
			}
		})
	}
}

func TestMetricsCompiledPrefixEscaping(t *testing.T) {
	value := "quote\" slash\\ newline\nvalue"
	d := descriptor{name: "escape", help: value, series: []series{{labels: [2]string{"reason", "stage"}, values: [2]string{value, value}}}}
	for _, kind := range []string{"counter", "histogram"} {
		t.Run(kind, func(t *testing.T) {
			var header, lines string
			if kind == "counter" {
				p := compileCounterText(d)
				header = p.header
				lines = p.series[0] + "1\n"
			} else {
				p := compileHistogramText(d)
				header = p.header
				for _, prefix := range p.series[0].buckets {
					lines += prefix + "1\n"
				}
				lines += p.series[0].sum + "1\n" + p.series[0].count + "1\n"
			}
			wantHeader := "# HELP escape " + legacyEscapeHelp(value) + "\n# TYPE escape " + kind + "\n"
			if header != wantHeader {
				t.Fatal("HELP/TYPE bytes changed")
			}
			parser := expfmt.NewTextParser(model.LegacyValidation)
			parsed, err := parser.TextToMetricFamilies(strings.NewReader(header + lines))
			if err != nil {
				t.Fatal(err)
			}
			f := parsed["escape"]
			if f.GetHelp() != value || len(f.Metric) != 1 || len(f.Metric[0].Label) != 2 {
				t.Fatal("prefix dimensions changed")
			}
			for _, label := range f.Metric[0].Label {
				if label.GetValue() != value {
					t.Fatal("label escaping changed")
				}
			}
			if kind == "counter" && lines != "escape"+legacyLabelText(d.series[0], "")+" 1\n" {
				t.Fatal("counter prefix order changed")
			}
		})
	}
}

func TestMetricsRenderParsesNormalProfiles(t *testing.T) {
	for _, tc := range renderCases()[:3] {
		t.Run(tc.name, func(t *testing.T) {
			body, err := Render(tc.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			parser := expfmt.NewTextParser(model.LegacyValidation)
			families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if len(families) != 13 {
				t.Fatal("fixed family count changed", len(families))
			}
			for _, d := range counters {
				if len(families[d.name].Metric) != len(d.series) {
					t.Fatal("counter series missing")
				}
			}
			for _, d := range histograms {
				if len(families[d.name].Metric) != len(d.series) {
					t.Fatal("histogram series missing")
				}
			}
		})
	}
}

func TestMetricsRenderIndependentBodies(t *testing.T) {
	s := populatedRenderSnapshot(true)
	want, err := legacyRender7da(s)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		first[i] = 'x'
	}
	if !bytes.Equal(second, want) {
		t.Fatal("returned bodies share storage")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 32 {
				body, e := Render(s)
				if e != nil || !bytes.Equal(body, want) {
					t.Error("parallel output/cache changed")
					return
				}
				for i := range body {
					body[i] = 'y'
				}
			}
		})
	}
	wg.Wait()
	last, err := Render(s)
	if err != nil || !bytes.Equal(last, want) {
		t.Fatal("body mutation corrupted fixed cache")
	}
}

func TestMetricsPrefixCacheCost(t *testing.T) {
	textBytes, stringsCount, seriesCount := 0, 0, 0
	structBytes := int(unsafe.Sizeof(counterPrefixes) + unsafe.Sizeof(histogramPrefixes))
	for _, p := range counterPrefixes {
		textBytes += len(p.header)
		stringsCount++
		structBytes += len(p.series) * int(unsafe.Sizeof(""))
		for _, s := range p.series {
			textBytes += len(s)
			stringsCount++
			seriesCount++
		}
	}
	for _, p := range histogramPrefixes {
		textBytes += len(p.header)
		stringsCount++
		structBytes += len(p.series) * int(unsafe.Sizeof(histogramSeriesText{}))
		for _, s := range p.series {
			for _, b := range s.buckets {
				textBytes += len(b)
				stringsCount++
				seriesCount++
			}
			textBytes += len(s.sum) + len(s.count)
			stringsCount += 2
			seriesCount += 2
		}
	}
	if seriesCount != 295 || stringsCount != 308 || textBytes == 0 {
		t.Fatal("cache/schema inventory mismatch")
	}
	t.Logf("fixed cache retained text_bytes=%d struct_and_slice_backing_bytes=%d logical_total_bytes=%d string_entries=%d sample_prefixes=%d; excludes allocator rounding and temporary initialization work", textBytes, structBytes, textBytes+structBytes, stringsCount, seriesCount)
}

var renderBenchmarkBody []byte

func BenchmarkMetricsRender(b *testing.B) {
	for _, tc := range []renderCase{{"zero", Snapshot{}}, {"populated", populatedRenderSnapshot(true)}, {"extreme", extremeRenderSnapshot()}} {
		b.Run(tc.name, func(b *testing.B) {
			want, err := legacyRender7da(tc.snapshot)
			if err != nil {
				b.Fatal(err)
			}
			for _, implementation := range []struct {
				name   string
				render func(Snapshot) ([]byte, error)
			}{{"legacy_7da", legacyRender7da}, {"fixed_prefixes", Render}} {
				b.Run(implementation.name, func(b *testing.B) {
					actual, e := implementation.render(tc.snapshot)
					if e != nil || !bytes.Equal(actual, want) {
						b.Fatal("benchmark bytes differ")
					}
					b.ReportAllocs()
					b.SetBytes(int64(len(want)))
					b.ResetTimer()
					for range b.N {
						body, e := implementation.render(tc.snapshot)
						if e != nil || len(body) != len(want) {
							b.Fatal(fmt.Sprint(e))
						}
						renderBenchmarkBody = body
					}
				})
			}
		})
	}
}
