"""Verify recorded bytes, source provenance and recomputation, not external validity."""
import argparse
import ast
import hashlib
import json
import importlib.util
from pathlib import Path

ANALYZERS={"experiments/analyze.py":"analyze.py","experiments/product/analyze.py":"product/analyze.py"}
LEGACY_DEPENDENCIES={
    'g2_behavior': ('g2_analyze.py',),
    'g2_clock': ('g2_analyze.py', 'g2_clock_analyze.py', 'clock_guard.py'),
}
LOCAL_DEPENDENCIES={
    'g2_behavior': ('g2_analyze.py',),
    'g2_clock': ('g2_analyze.py', 'g2_clock_analyze.py', 'clock_guard.py','local_execution.py'),
    'local_execution': ('local_execution.py',),
}
DEPENDENCIES={
    'g2_behavior': ('g2_analyze.py',),
    'g2_clock': ('g2_analyze.py', 'g2_clock_analyze.py', 'clock_guard.py','local_execution.py'),
    'local_execution': ('local_execution.py','local_run_seal.py'),
    'vulndb_snapshot': ('vulndb_snapshot.py',),
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
    assert declared in (LEGACY_DEPENDENCIES,LOCAL_DEPENDENCIES,DEPENDENCIES), 'unregistered analyzer dependencies'
    assert enabled <= set(declared), 'execution profile lacks recorded analysis dependency'
    return tuple(sorted({name for key in enabled for name in declared[key]}))

def recorded_module(run):
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
            module._recorded_dependency_hashes={name:recorded['experiments/product/'+name]
                                               for name in analyzer_dependencies(source,case)}
            return module
    raise AssertionError("Recorded analyzer source unavailable"+(': '+'; '.join(rejected) if rejected else ''))


def recorded_analyzer(run):
    return recorded_module(run).analyze


def recorded_helper(module, name, case):
    source=Path(module.__file__).read_bytes()
    assert name in analyzer_dependencies(source,case), 'helper lacks a fixed recorded dependency'
    path=Path(module.__file__).with_name(name)
    spec=importlib.util.spec_from_file_location('recorded_'+name[:-3],path)
    helper=importlib.util.module_from_spec(spec)
    data=path.read_bytes()
    expected=getattr(module,'_recorded_dependency_hashes',{})
    if expected:
        assert hashlib.sha256(data).hexdigest()==expected[name], 'recorded helper changed before execution'
    if name == 'local_run_seal.py':
        helper._expected_local_sha256=expected['local_execution.py']
    exec(compile(data,str(path),'exec'),helper.__dict__)
    return helper


def verify_g2_provenance(run, manifest, rows, module=None):
    """Join raw clock provenance to this run's separately recorded source files."""
    case=manifest['case']
    is_local='execution_profile' in case or 'local_execution' in case
    if not ({'g2_behavior','g2_clock'} & set(case)) and not is_local:
        return
    subject=manifest.get('subject_version')
    import re
    assert isinstance(subject,str) and re.fullmatch('[0-9a-f]{40}',subject), 'G2 subject is not a fixed commit'
    environment=json.loads((run/'environment.json').read_text(encoding='utf-8'))
    assert environment.get('subject_version')==subject, 'G2 environment subject differs'
    facts=[row for row in rows if row.get('kind')=='fact' and row.get('name')=='subject_version']
    assert len(facts)==1 and facts[0].get('step')=='env' and facts[0].get('value')==subject, 'G2 raw subject differs'
    local_fact=None
    if is_local:
        local=recorded_helper(module or recorded_module(run),'local_execution.py',case)
        assert case.get('execution_profile')==local.PROFILE, 'unknown local execution profile'
        expected_keys={'vm_uuid','base_image_sha256','qemu_binary_sha256','launch_sha256'}
        assert isinstance(case.get('local_execution'),dict) and set(case['local_execution'])==expected_keys, 'local manifest keys differ'
        local_rows=[r for r in rows if r.get('kind')=='fact' and r.get('name')=='local_execution']
        assert len(local_rows)==1 and local_rows[0].get('step')=='env','local environment fact missing/duplicated'
        local_fact=local_rows[0]['value']
        assert isinstance(local_fact,dict) and set(local_fact)=={'attestation','attestation_sha256'},'invalid local fact'
        attestation=local.validate_attestation(local_fact['attestation'])
        assert local_fact['attestation_sha256']==local.digest(attestation), 'local attestation digest differs'
        assert attestation['source_sha']==subject and all(attestation[k]==v for k,v in case['local_execution'].items()), 'local manifest/source differs'
        assert environment.get('local_execution')==local_fact and not environment.get('ci'), 'local environment identity differs or impersonates CI'
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
        if is_local:
            assert record.get('schema')=='g2-clock-preparation-v2' and record.get('attestation')==local_fact['attestation'], 'local clock attestation differs'
            assert record.get('attestation_sha256')==local_fact['attestation_sha256'] and record.get('runner_environment')==local.PROFILE, 'local clock profile/digest differs'
            assert 'workflow_ref' not in record and all(record.get(k)==attestation[k] for k in ('run_id','source_sha','boot_id','vm_uuid','vm_serial','runner_uid','runner_gid',*local.HASH_KEYS)), 'local prepared identity differs'
            continue
        for field,env_key in (('run_id','GITHUB_RUN_ID'),('workflow_ref','GITHUB_WORKFLOW_REF'),
                              ('runner_environment','RUNNER_ENVIRONMENT')):
            assert ci.get(env_key) and record.get(field)==ci[env_key], 'clock preparation differs from job: '+field
        assert ci.get('GITHUB_JOB')=='clock', 'clock evidence came from another job'

def verify(run, check_current_source=False):
    run=Path(run)
    required={"manifest.json","source-manifest.json","environment.json","execution.json","raw.jsonl",
              "stderr.log","summary.json","report.md"}
    names=set()
    hashes={}
    for line in (run/"SHA256SUMS").read_text(encoding="utf-8").splitlines():
        expected,name=line.split("  ",1)
        assert Path(name).name==name and name not in names,"invalid checksum entry"
        names.add(name)
        assert not (run/name).is_symlink() and (run/name).is_file(), 'linked/nonregular evidence artifact'
        assert hashlib.sha256((run/name).read_bytes()).hexdigest()==expected,"digest mismatch: "+name
        hashes[name]=expected
    assert required<=names,"missing required evidence"
    assert names=={p.name for p in run.iterdir() if p.is_file() and p.name!="SHA256SUMS"},"untracked run files"
    manifest=json.loads((run/"manifest.json").read_text(encoding="utf-8"))
    summary=json.loads((run/"summary.json").read_text(encoding="utf-8"))
    execution=json.loads((run/"execution.json").read_text(encoding="utf-8"))
    rows=[json.loads(x) for x in (run/"raw.jsonl").read_text(encoding="utf-8").splitlines() if x]
    is_local='execution_profile' in manifest['case'] or 'local_execution' in manifest['case']
    module=recorded_module(run) if is_local or execution['exit_code']==0 and not execution['timed_out'] else None
    supervision=None
    if is_local:
        verify_g2_provenance(run,manifest,rows,module)
        helper=recorded_helper(module,'local_run_seal.py',manifest['case'])
        path=run/'execution-receipt.json'
        assert path.is_file() and not path.is_symlink() and path.stat().st_size<=65536, 'local execution seal absent/unbounded'
        receipt=json.loads(path.read_bytes())
        assert helper.local.canonical(receipt)==path.read_bytes(), 'noncanonical execution receipt'
        assert receipt.get('evidence_run_id')==run.name, 'execution seal names another directory'
        environment=json.loads((run/'environment.json').read_bytes())
        sources=json.loads((run/'source-manifest.json').read_bytes())
        actual=helper.file_hashes(run)
        assert actual=={k:v for k,v in hashes.items() if k!='execution-receipt.json'}, 'execution seal file inventory differs'
        supervision=helper.validate_execution_receipt(receipt,manifest,environment,rows,sources,actual)
        helper.verify_host_materials(run,receipt)
    if 'vulndb_snapshot' in manifest['case']:
        helper=recorded_helper(module or recorded_module(run),'vulndb_snapshot.py',manifest['case'])
        helper.verify_materials(run,manifest['case'],rows,json.loads((run/'environment.json').read_bytes()))
    if execution["exit_code"]==0 and not execution["timed_out"]:
        verify_g2_provenance(run,manifest,rows,module)
        assert module.analyze(manifest["case"],rows,manifest["repeats"])==summary,"summary cannot be reproduced"
    else:assert summary["status"]=="inconclusive","failed run claimed success"
    if check_current_source:
        root=Path(__file__).resolve().parent.parent
        for rel,sha in json.loads((run/"source-manifest.json").read_text(encoding="utf-8")).items():
            p=(root/rel).resolve()
            assert p.is_relative_to(root)
            assert hashlib.sha256(p.read_bytes()).hexdigest()==sha,"source drift: "+rel
    return {"integrity":"verified","recomputation":summary["status"],
            **({'execution_supervision':supervision} if is_local else {}),
            "warning":"Checksums are integrity evidence, not proof of independent authorship or product validity."}

if __name__=="__main__":
    parser=argparse.ArgumentParser()
    parser.add_argument("run",type=Path)
    parser.add_argument("--current-source",action="store_true")
    args=parser.parse_args()
    print(json.dumps(verify(args.run,args.current_source),ensure_ascii=False))
