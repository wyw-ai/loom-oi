"""Verify recorded bytes, source provenance and recomputation, not external validity."""
import argparse
import ast
import hashlib
import json
import importlib.util
from pathlib import Path

ANALYZERS={"experiments/analyze.py":"analyze.py","experiments/product/analyze.py":"product/analyze.py"}
DEPENDENCIES={
    'g2_behavior': ('g2_analyze.py',),
    'g2_clock': ('g2_analyze.py', 'g2_clock_analyze.py', 'clock_guard.py'),
}


def analyzer_dependencies(source, case):
    """Read a literal fixed whitelist, before any recorded module is executed."""
    declarations = [node.value for node in ast.parse(source).body if isinstance(node, ast.Assign)
                    and any(isinstance(target, ast.Name) and target.id == 'ANALYSIS_DEPENDENCIES'
                            for target in node.targets)]
    enabled = set(case) & set(DEPENDENCIES)
    if not declarations:
        assert not enabled, 'recorded analyzer has no versioned G2 dependencies'
        return ()
    assert len(declarations) == 1, 'ambiguous analyzer dependency declaration'
    try:
        declared = ast.literal_eval(declarations[0])
    except (ValueError, TypeError, SyntaxError) as exc:
        raise AssertionError('analyzer dependencies must be a literal whitelist') from exc
    assert declared == DEPENDENCIES, 'unregistered analyzer dependencies'
    return tuple(sorted({name for key in enabled for name in declared[key]}))

def recorded_analyzer(run):
    root=Path(__file__).resolve().parent
    recorded=json.loads((run/"source-manifest.json").read_text(encoding="utf-8"))
    keys=[k for k in ANALYZERS if k in recorded]
    assert len(keys)==1,"recorded analyzer missing or ambiguous"
    expected=recorded[keys[0]]
    case=json.loads((run/'manifest.json').read_text(encoding='utf-8'))['case']
    rejected=[]
    for candidate in [root/ANALYZERS[keys[0]], *sorted((root/"sources").glob("*/analyze.py"))]:
        source=candidate.read_bytes()
        if hashlib.sha256(source).hexdigest()==expected:
            try:
                for name in analyzer_dependencies(source,case):
                    dependency=candidate.with_name(name)
                    key='experiments/product/'+name
                    assert key in recorded, 'recorded dependency hash missing: '+key
                    assert dependency.is_file(), 'recorded dependency source unavailable: '+name
                    assert dependency.resolve().parent==candidate.resolve().parent, 'dependency is not an adjacent versioned file'
                    assert hashlib.sha256(dependency.read_bytes()).hexdigest()==recorded[key], 'recorded dependency source drift: '+name
            except AssertionError as exc:
                rejected.append(str(exc))
                continue
            spec=importlib.util.spec_from_file_location("recorded_analysis",candidate)
            module=importlib.util.module_from_spec(spec)
            exec(compile(source,str(candidate),'exec'),module.__dict__)
            return module.analyze
    raise AssertionError("Recorded analyzer source unavailable"+(': '+'; '.join(rejected) if rejected else ''))


def verify_g2_provenance(run, manifest, rows):
    """Join raw clock provenance to this run's separately recorded source files."""
    case=manifest['case']
    if not ({'g2_behavior','g2_clock'} & set(case)):
        return
    subject=manifest.get('subject_version')
    import re
    assert isinstance(subject,str) and re.fullmatch('[0-9a-f]{40}',subject), 'G2 subject is not a fixed commit'
    environment=json.loads((run/'environment.json').read_text(encoding='utf-8'))
    assert environment.get('subject_version')==subject, 'G2 environment subject differs'
    facts=[row for row in rows if row.get('kind')=='fact' and row.get('name')=='subject_version']
    assert len(facts)==1 and facts[0].get('step')=='env' and facts[0].get('value')==subject, 'G2 raw subject differs'
    if 'g2_clock' not in case:
        return
    records=[row['event'] for row in rows if row.get('step')=='clock' and row.get('kind')=='clock_preparation']
    # Incomplete/failed runs remain reproducible; the analyzer rejects missing
    # clock evidence. Any emitted preparation must still identify this run.
    recorded=json.loads((run/'source-manifest.json').read_text(encoding='utf-8'))
    source_roots=('cmd/','internal/','tests/','experiments/product/','.github/workflows/')
    expected={key:value for key,value in recorded.items()
              if key in ('go.mod','go.sum') or key.startswith(source_roots)}
    ci=environment.get('ci',{})
    for record in records:
        assert record.get('source_sha')==subject and record.get('source_hashes')==expected, 'clock prepared different source bytes'
        for field,env_key in (('run_id','GITHUB_RUN_ID'),('workflow_ref','GITHUB_WORKFLOW_REF'),
                              ('runner_environment','RUNNER_ENVIRONMENT')):
            assert ci.get(env_key) and record.get(field)==ci[env_key], 'clock preparation differs from job: '+field
        assert ci.get('GITHUB_JOB')=='clock', 'clock evidence came from another job'

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
        verify_g2_provenance(run,manifest,rows)
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
