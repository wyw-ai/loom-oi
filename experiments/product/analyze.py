"""Recompute a product evidence summary from raw rows (Implementation level).

Case kinds: product-p1 (G1, TASK-004) and product-p2 (G4 onwards, TASK-005).
The P1 analyzer that recorded the G1 runs is kept byte for byte in
experiments/sources/product-p1/ so verify.py still recomputes those runs.

The runner records every `go test -json` event and every tool output line;
this module derives all verdicts from those rows only, so verify.py can
reproduce summary.json exactly. It runs nothing and trusts no summary.
"""
import json
import re

OBS = re.compile(r'\bOBS (\{.*\})\s*$')
ENGINE = re.compile(r'\bENGINE (\S+) (.+?)\s*$')
TOTAL = re.compile(r'^total:\s+\(statements\)\s+([0-9]+(?:\.[0-9]+)?)%$')
ZERO = 'ZERO-TOLERANCE'
SCOPE = ('Real ois/oic/oi binaries on one Linux runner with SQLite and PostgreSQL; '
         'B4 test configuration. Not the 1C1G target (G5), HA (G6), probes (G2) or real compatibility (G7).')


def _required(entry):
    names = []
    for test in entry['tests']:
        names.append(test)
        names.extend(test + '/' + sub for sub in entry['subtests'])
    return names


KINDS = ('product-p1', 'product-p2')


def analyze(case, rows, repeats):
    if not isinstance(case, dict) or case.get('kind') not in KINDS:
        raise ValueError('unsupported case')
    if not isinstance(repeats, int) or repeats < 1:
        raise ValueError('repeats must be a positive integer')
    problems, failed, zero = [], [], []
    facts, steps, results = {}, {}, {}
    engines, observations = {}, {}
    coverage = None
    for row in rows:
        step, kind = row['step'], row['kind']
        if kind == 'fact':
            facts[row['name']] = row['value']
            continue
        st = steps.setdefault(step, {'start': 0, 'end': 0, 'events': 0, 'exit_code': None, 'timed_out': False,
                                     'tests': {'pass': 0, 'fail': 0, 'skip': 0}})
        if kind == 'start':
            st['start'] += 1
        elif kind == 'end':
            st['end'] += 1
            st['exit_code'] = row['exit_code']
            st['timed_out'] = bool(row['timed_out'])
        elif kind == 'coverage':
            for line in row['lines']:
                m = TOTAL.match(line.strip())
                if m:
                    coverage = float(m.group(1))
        elif kind == 'go':
            st['events'] += 1
            event = row['event']
            action, test = event.get('Action'), event.get('Test')
            if test and action in ('pass', 'fail', 'skip'):
                results.setdefault((step, test), []).append(action)
                st['tests'][action] += 1
                if action == 'fail':
                    failed.append(step + ': ' + test)
            elif action == 'output':
                text = event.get('Output', '')
                if ZERO in text:
                    zero.append(step + ': ' + text.strip())
                m = OBS.search(text)
                if m and step == 'vectors':
                    obs = json.loads(m.group(1))
                    observations.setdefault(obs.get('kind', 'unknown'), []).append(obs)
                m = ENGINE.search(text)
                if m:
                    versions = engines.setdefault(m.group(1), [])
                    if m.group(2) not in versions:
                        versions.append(m.group(2))
        elif kind != 'line':
            raise ValueError('unknown row kind: ' + kind)

    status = 'observed'
    for name in case['steps']:
        st = steps.get(name)
        if st is None or st['start'] != 1 or st['end'] != 1:
            status = 'inconclusive'
            problems.append(name + ': step not recorded exactly once')
        elif st['timed_out']:
            status = 'inconclusive'
            problems.append(name + ': timed out')
        elif name in case['go_steps'] and st['events'] == 0:
            status = 'inconclusive'
            problems.append(name + ': no go test events')

    verdict_ok = True
    if not facts.get('go_version', '').startswith('go version ' + case['go'] + ' '):
        verdict_ok = False
        problems.append('toolchain is not ' + case['go'])
    if facts.get('goos') != 'linux':
        verdict_ok = False
        problems.append('vectors must run on Linux (B4 §4)')
    for name in case['steps']:
        st = steps.get(name)
        if st is not None and st['exit_code'] != 0:
            verdict_ok = False
            problems.append(name + ': exit code ' + str(st['exit_code']))
    if coverage is None or coverage < case['coverage_min_pct']:
        verdict_ok = False
        problems.append('unit coverage ' + str(coverage) + '% below ' + str(case['coverage_min_pct']) + '%')
    if engines.get('sqlite') != [case['engines']['sqlite']]:
        verdict_ok = False
        problems.append('SQLite engine ' + str(engines.get('sqlite')))
    pg = engines.get('postgres') or []
    if len(pg) != 1 or pg[0].split(' ')[0].split('.')[0] != case['engines']['postgres_major']:
        verdict_ok = False
        problems.append('PostgreSQL engine ' + str(engines.get('postgres')))
    if zero:
        verdict_ok = False
        problems.append('zero-tolerance events: ' + str(len(zero)))

    vectors = {}
    for entry in case['vectors']:
        checked = vectors.setdefault(entry['id'], {'result': 'pass', 'checked': []})
        for name in _required(entry):
            got = results.get((entry['step'], name), [])
            checked['checked'].append(entry['step'] + ': ' + name)
            if got == ['pass'] * repeats:
                continue
            outcome = 'missing' if not got else 'fail'
            if checked['result'] != 'fail':
                checked['result'] = outcome
            problems.append(entry['id'] + ': ' + name + ' ' + (','.join(got) or 'missing'))
    if any(v['result'] != 'pass' for v in vectors.values()):
        verdict_ok = False
    criteria = {name: ('pass' if all(vectors.get(v, {}).get('result') == 'pass' for v in ids) else 'fail')
                for name, ids in case['criteria'].items()}
    if any(r != 'pass' for r in criteria.values()):
        verdict_ok = False

    return {
        'status': status,
        'verdict': 'pass' if status == 'observed' and verdict_ok else 'fail',
        'evidence_class': 'product_implementation',
        'facts': facts,
        'steps': {name: {'exit_code': st['exit_code'], 'timed_out': st['timed_out'], 'tests': st['tests']}
                  for name, st in steps.items()},
        'coverage_total_pct': coverage,
        'engines': engines,
        'vectors': vectors,
        'criteria': criteria,
        'failed_tests': failed,
        'zero_tolerance_lines': zero,
        'observations': observations,
        'problems': problems,
        'scope': case.get('scope', SCOPE),
    }
