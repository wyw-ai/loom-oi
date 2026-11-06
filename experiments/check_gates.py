#!/usr/bin/env python3
"""Validate recorded stage gates. This is not a product test or approval engine."""
import argparse
import hashlib
import json
import re
import sys
from pathlib import Path
from check_iterations import check as verify_iteration_register
from historical_documents import fingerprint, read_document

ROOT = Path(__file__).resolve().parents[1]
STATES = {'Blocked', 'NotRun', 'Failed', 'Inconclusive', 'Passed'}


def evaluate(data, root, stage=None):
    errors, blocked = [], []
    rows = data.get('gates', [])
    gates = {g['id']: g for g in rows}
    if len(gates) != len(rows) or not gates:
        errors.append('Gate IDs must be unique and nonempty')
    visiting, visited = set(), set()
    closure_checked = False
    closure_error = None

    def completed_evidence(g, v):
        nonlocal closure_checked, closure_error
        gid = g['id']
        if v.get('completion') != 'complete' or v.get('evidence_level') != g.get('evidence_level'):
            errors.append(f'{gid}: closure or evidence level missing/mismatched')
        if g.get('evidence_level') == 'Design':
            if not v.get('decision_sources'):
                errors.append(f'{gid}: human adoption sources missing')
            return
        # Dirty development runs cannot become release evidence even when
        # a caller copies their dirty: subject string into the gate record.
        if not re.fullmatch(r'[0-9a-f]{40}', v.get('subject_version', '')):
            errors.append(f'{gid}: implementation/deployment subject must be a clean commit SHA')
            return
        # A mechanism report cannot fill an implementation/deployment gate.
        iid = v.get('iteration_id', '')
        if not re.fullmatch(r'ITER-[0-9]+', iid):
            errors.append(f'{gid}: completed iteration missing')
            return
        # Reuse the complete run + iteration verifier; do not weaken it here.
        # It verifies required/unique checksum entries, recomputes supported raw
        # evidence, rejects failed primary runs, checks claims and propagation.
        if not closure_checked:
            try:
                coverage = verify_iteration_register(root)
                if coverage['pending_runs']:
                    raise ValueError('unanalyzed experiment runs remain')
            except (OSError, ValueError, KeyError, TypeError, AssertionError) as exc:
                closure_error = str(exc)
            closure_checked = True
        if closure_error is not None:
            errors.append(f'{gid}: full evidence closure invalid: {closure_error}')
            return
        path = root / 'experiments/iterations' / (iid + '.json')
        fingerprints = {a.get('path'): a.get('sha256') for a in v.get('artifacts', [])}
        try:
            record = json.loads(path.read_text(encoding='utf-8'))
            if record['id'] != iid or fingerprints.get(path.relative_to(root).as_posix()) != hashlib.sha256(path.read_bytes()).hexdigest():
                raise ValueError('iteration not fingerprint-bound')
            if record.get('completion') != 'complete' or not record.get('artifacts') or not record.get('issues'):
                raise ValueError('iteration closure incomplete')
            analysis = (root / record['analysis']).resolve()
            if not analysis.is_relative_to(root.resolve()) or fingerprints.get(record['analysis']) != hashlib.sha256(read_document(root, record['analysis'])).hexdigest():
                raise ValueError('analysis not fingerprint-bound')
            runs = v.get('run_ids', [])
            if not runs:
                raise ValueError('implementation/deployment runs missing')
            primary = {x['run']: x for x in record['inputs'] if x['use'] == 'primary'}
            for rid in runs:
                if rid not in primary or '/' in rid or '\\' in rid or rid in {'.', '..'}:
                    raise ValueError('run not a primary iteration input')
                run = root / 'experiments/runs' / rid
                sums = run / 'SHA256SUMS'
                if hashlib.sha256(sums.read_bytes()).hexdigest() != primary[rid]['sha256sums_sha256']:
                    raise ValueError('run fingerprint changed')
                for line in sums.read_text(encoding='utf-8').splitlines():
                    digest, relative = line.split(maxsplit=1)
                    f = (run / relative.strip()).resolve()
                    if not f.is_relative_to(run.resolve()) or hashlib.sha256(f.read_bytes()).hexdigest() != digest:
                        raise ValueError('raw package integrity failed')
                manifest = json.loads((run / 'manifest.json').read_text(encoding='utf-8'))
                if manifest.get('evidence_level') != g['evidence_level'] or manifest.get('subject_version') != v['subject_version']:
                    raise ValueError('run level/version incompatible with gate')
                if manifest.get('gate') != gid:
                    raise ValueError('run belongs to a different or missing gate')
                summary = json.loads((run / 'summary.json').read_text(encoding='utf-8'))
                if summary.get('status') != 'observed' or summary.get('verdict') != 'pass':
                    raise ValueError('primary run does not have an observed passing verdict')
                required = set(g['criteria'])
                case = manifest.get('case')
                planned = case.get('criteria') if isinstance(case, dict) else None
                actual = summary.get('criteria')
                if (not isinstance(planned, dict) or set(planned) != required or
                        not isinstance(actual, dict) or set(actual) != required or
                        any(actual[name] != 'pass' for name in required)):
                    raise ValueError('run criteria do not satisfy the gate')
                vectors = summary.get('vectors', {})
                for name in required:
                    ids = planned[name]
                    if (not isinstance(ids, list) or not ids or not isinstance(vectors, dict) or
                            any(not isinstance(vid, str) or not isinstance(vectors.get(vid), dict) or
                                vectors[vid].get('result') != 'pass' for vid in ids)):
                        raise ValueError('gate criterion lacks passing vector evidence: ' + name)
                env = f'experiments/runs/{rid}/environment.json'
                if fingerprints.get(env) != hashlib.sha256((root / env).read_bytes()).hexdigest():
                    raise ValueError('run environment not fingerprint-bound')
        except (OSError, ValueError, KeyError, TypeError) as exc:
            errors.append(f'{gid}: {exc}')

    def walk(gid):
        if gid not in gates:
            errors.append(f'Unknown dependency: {gid}')
            return
        if gid in visiting:
            errors.append(f'Dependency cycle: {gid}')
            return
        if gid in visited:
            return
        visiting.add(gid)
        g = gates[gid]
        for dep in g.get('requires', []):
            walk(dep)
        visiting.remove(gid)
        visited.add(gid)
        if g.get('status') not in STATES:
            errors.append(f'{gid}: unknown status')
        if not g.get('criteria') or not g.get('scope'):
            errors.append(f'{gid}: missing scope/criteria')
        if g.get('evidence_level') not in {'Design', 'Implementation', 'Deployment'}:
            errors.append(f'{gid}: invalid required evidence level')
        if g.get('status') == 'Passed':
            v = g.get('validation') or {}
            if set(v.get('criteria_passed', [])) != set(g.get('criteria', [])):
                errors.append(f'{gid}: criteria coverage missing')
            if not v.get('reviewer') or not v.get('subject_version') or not v.get('environment'):
                errors.append(f'{gid}: review/version/environment missing')
            if not v.get('artifacts'):
                errors.append(f'{gid}: evidence missing')
            for a in v.get('artifacts', []):
                try:
                    actual = fingerprint(root, a.get('path', ''))
                except (ValueError, OSError):
                    errors.append(f'{gid}: invalid evidence path or scope')
                else:
                    if actual != a.get('sha256'):
                        errors.append(f'{gid}: evidence fingerprint mismatch')
            if any(gates.get(d, {}).get('status') != 'Passed' for d in g.get('requires', [])):
                errors.append(f'{gid}: Passed with unmet dependency')
            completed_evidence(g, v)
        elif not g.get('blockers'):
            errors.append(f'{gid}: missing blocker/next-action record')

    for gid in gates:
        walk(gid)
    stages = data.get('stages', {})
    if not stages:
        errors.append('Stage mapping missing')
    for name, deps in stages.items():
        if not deps or any(d not in gates for d in deps):
            errors.append(f'{name}: missing/unknown gate')
    if stage is not None:
        if stage not in stages:
            errors.append(f'Unknown stage: {stage}')
        else:
            needed = set()
            def collect(gid):
                if gid in needed or gid not in gates:
                    return
                needed.add(gid)
                for dep in gates[gid].get('requires', []):
                    collect(dep)
            for gid in stages[stage]:
                collect(gid)
            blocked = [f"{gid}: {gates[gid]['status']} — {'; '.join(gates[gid].get('blockers', []))}"
                       for gid in sorted(needed) if gates[gid].get('status') != 'Passed']
    return errors, blocked


def main(argv=None, root=None):
    # A real blocked/error path includes Chinese reasons. Explicit UTF-8
    # keeps Windows' legacy redirected code page from replacing exit 2 with
    # UnicodeEncodeError; StringIO used by callers remains supported.
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, 'reconfigure'):
            stream.reconfigure(encoding='utf-8')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--stage', help='P1, P2, P3, P4, release-single, release-ha')
    args = parser.parse_args(argv)
    root = Path(root) if root else ROOT
    try:
        data = json.loads((root / 'experiments/gates.json').read_text(encoding='utf-8'))
        errors, blocked = evaluate(data, root, args.stage)
    except (ValueError, KeyError, TypeError, OSError) as e:
        print(f'INVALID: {e}')
        return 1
    if errors:
        print('\n'.join('INVALID: ' + e for e in errors))
        return 1
    if args.stage is None:
        print('VALID REGISTER; no phase approved. ' + ', '.join(f"{g['id']}={g['status']}" for g in data['gates']))
        return 0
    if blocked:
        print('\n'.join('BLOCKED: ' + x for x in blocked))
        return 2
    print(f'RECORDED PASS: {args.stage}; applies only to the recorded scope/version/environment; record validation only; semantic evidence review still applies, without a new human approval step.')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
