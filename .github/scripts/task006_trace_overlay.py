"""Create a diagnostic-only Go overlay; never edit the product source in place.

One unchanged Load invocation collects Go runtime traces from the test process
and the real OIS/OIC processes. The overlay is NOT formal evidence: tracing has
overhead. Preserve originals, transformed sources, hashes and the overlay map.
"""
import hashlib
import json
from pathlib import Path
import sys

root = Path(__file__).resolve().parents[2]
out = Path(sys.argv[1]).resolve()
out.mkdir(parents=True, exist_ok=True)
replacements = {}
sources = []


def overlay(relative, transform):
    source = root / relative
    original = source.read_bytes()
    transformed = transform(original.decode("utf-8")).encode("utf-8")
    name = relative.replace("/", "_")
    (out / (name + ".original.txt")).write_bytes(original)
    destination = out / (name + ".overlay.txt")
    destination.write_bytes(transformed)
    replacements[str(source)] = str(destination)
    sources.append({"path": relative,
                    "original_sha256": hashlib.sha256(original).hexdigest(),
                    "overlay_sha256": hashlib.sha256(transformed).hexdigest()})


def replace_once(text, old, new):
    assert text.count(old) == 1, (old, text.count(old))
    return text.replace(old, new, 1)


trace_main = r'''

// Diagnostic overlay only. No endpoint, payload or credentials are recorded.
var d19StopTrace = func() {}

func init() {
    dir := os.Getenv("OI_D19_TRACE_DIR")
    if dir == "" || len(os.Args) < 2 || (os.Args[1] != "serve" && os.Args[1] != "run") {
        return
    }
    prefix := filepath.Join(dir, strconv.Itoa(os.Getpid()))
    f, err := os.Create(prefix + ".trace")
    if err != nil { panic(err) }
    if err = trace.Start(f); err != nil { panic(err) }
    trace.Log(context.Background(), "d19-wall-anchor", time.Now().UTC().Format(time.RFC3339Nano))
    var once sync.Once
    d19StopTrace = func() { once.Do(func() {
        trace.Log(context.Background(), "d19-wall-anchor", time.Now().UTC().Format(time.RFC3339Nano))
        trace.Stop()
        if err := f.Close(); err != nil { panic(err) }
        if err := os.WriteFile(prefix + ".stopped", []byte("trace stopped\n"), 0600); err != nil { panic(err) }
    }) }
    go func() {
        tick := time.NewTicker(50 * time.Millisecond)
        defer tick.Stop()
        for range tick.C {
            if _, err := os.Stat(prefix + ".stop"); err == nil {
                d19StopTrace()
                return
            }
        }
    }()
}

func d19Exit(code int) { d19StopTrace(); os.Exit(code) }
'''


def main_trace(text):
    text = replace_once(text, '"os"', '"os"\n"path/filepath"\n"runtime/trace"\n"strconv"\n"sync"\n"time"')
    return text.replace("os.Exit(", "d19Exit(") + trace_main


for binary in ("ois", "oic"):
    overlay(f"cmd/{binary}/main.go", main_trace)


def leg_trace(text):
    text = replace_once(text, '"sync/atomic"', '"sync/atomic"\n"runtime/trace"\n"strconv"')
    for method in ("readLoop", "writeLoop"):
        old = f"func (l *leg) {method}() {{"
        text = replace_once(text, old, old + f'\ntrace.Log(l.ctx, "d19-leg-{method}", l.ch.id+"/"+strconv.Itoa(int(l.role)))')
    return text


overlay("internal/ois/relay/leg.go", leg_trace)


def worker_trace(text):
    text = replace_once(text, '"runtime"', '"runtime"\n"runtime/trace"')
    old = 'func TestTVB4_06_Load(t *testing.T) {'
    text = replace_once(text, old, old + '\ntrace.Log(bg, "d19-resource-anchor", fmt.Sprintf("before_ns=%d wall=%s after_ns=%d", resourceNS(), time.Now().UTC().Format(time.RFC3339Nano), resourceNS()))')
    old = 'w.ChannelID, w.OpenMS = s.ChannelID(), time.Since(t0).Milliseconds()'
    return replace_once(text, old, old + '\ntrace.Log(bg, "d19-consumer", w.ChannelID)')


overlay("tests/integration/vectors_b4_test.go", worker_trace)


def stop_before_kill(text):
    old = "func (p *proc) kill() {"
    new = old + r'''
    // All workload/cleanup assertions have already run. Flush diagnostics
    // before the existing process kill; this does not extend product deadlines.
    select {
    case <-p.exited:
        return // Preserve the original crash; an incomplete trace is not evidence.
    default:
    }
    if dir := os.Getenv("OI_D19_TRACE_DIR"); dir != "" {
        prefix := filepath.Join(dir, fmt.Sprint(p.cmd.Process.Pid))
        if _, err := os.Stat(prefix + ".trace"); err == nil {
            if err := os.WriteFile(prefix + ".stop", []byte("stop\n"), 0600); err != nil { panic(err) }
            deadline := time.Now().Add(5 * time.Second)
            for {
                if _, err := os.Stat(prefix + ".stopped"); err == nil { break }
                select {
                case <-p.exited:
                    fmt.Fprintln(os.Stderr, "D19 process exited before trace flush:", p.name)
                    return
                default:
                }
                if !time.Now().Before(deadline) { panic("D19 trace flush exceeded diagnostic 5s budget") }
                time.Sleep(10 * time.Millisecond)
            }
        }
    }
'''
    return replace_once(text, old, new)


overlay("tests/integration/main_test.go", stop_before_kill)
(out / "overlay.json").write_text(json.dumps({"Replace": replacements}, indent=2) + "\n", encoding="utf-8")
(out / "sources.json").write_text(json.dumps(sources, indent=2) + "\n", encoding="utf-8")
print(out / "overlay.json")
