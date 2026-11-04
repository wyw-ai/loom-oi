"""Verify recorded bytes, source provenance and recomputation, not external validity."""
import argparse
import hashlib
import json
import importlib.util
from pathlib import Path
from analyze import analyze

ANALYZERS={"experiments/analyze.py":"analyze.py","experiments/product/analyze.py":"product/analyze.py"}

def recorded_analyzer(run):
    root=Path(__file__).resolve().parent
    recorded=json.loads((run/"source-manifest.json").read_text(encoding="utf-8"))
    keys=[k for k in ANALYZERS if k in recorded]
    assert len(keys)==1,"recorded analyzer missing or ambiguous"
    expected=recorded[keys[0]]
    for candidate in [root/ANALYZERS[keys[0]], *sorted((root/"sources").glob("*/analyze.py"))]:
        if hashlib.sha256(candidate.read_bytes()).hexdigest()==expected:
            spec=importlib.util.spec_from_file_location("recorded_analysis",candidate)
            module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
            return module.analyze
    raise AssertionError("Recorded analyzer source unavailable")

def verify(run, check_current_source=False):
    required={"manifest.json","source-manifest.json","environment.json","execution.json","raw.jsonl",
              "stderr.log","summary.json","report.md"}
    names=set()
    for line in (run/"SHA256SUMS").read_text(encoding="utf-8").splitlines():
        expected,name=line.split("  ",1)
        assert Path(name).name==name and name not in names,"invalid checksum entry"
        names.add(name)
        assert hashlib.sha256((run/name).read_bytes()).hexdigest()==expected,"digest mismatch: "+name
    assert required<=names,"missing required evidence"
    assert names=={p.name for p in run.iterdir() if p.is_file() and p.name!="SHA256SUMS"},"untracked run files"
    manifest=json.loads((run/"manifest.json").read_text(encoding="utf-8"))
    summary=json.loads((run/"summary.json").read_text(encoding="utf-8"))
    execution=json.loads((run/"execution.json").read_text(encoding="utf-8"))
    if execution["exit_code"]==0 and not execution["timed_out"]:
        rows=[json.loads(x) for x in (run/"raw.jsonl").read_text(encoding="utf-8").splitlines() if x]
        assert recorded_analyzer(run)(manifest["case"],rows,manifest["repeats"])==summary,"summary cannot be reproduced"
    else:assert summary["status"]=="inconclusive","failed run claimed success"
    if check_current_source:
        root=Path(__file__).resolve().parent.parent
        for rel,sha in json.loads((run/"source-manifest.json").read_text(encoding="utf-8")).items():
            p=(root/rel).resolve()
            assert p.is_relative_to(root)
            assert hashlib.sha256(p.read_bytes()).hexdigest()==sha,"source drift: "+rel
    return {"integrity":"verified","recomputation":summary["status"],
            "warning":"Checksums are integrity evidence, not proof of independent authorship or product validity."}

if __name__=="__main__":
    parser=argparse.ArgumentParser()
    parser.add_argument("run",type=Path)
    parser.add_argument("--current-source",action="store_true")
    args=parser.parse_args()
    print(json.dumps(verify(args.run,args.current_source),ensure_ascii=False))
