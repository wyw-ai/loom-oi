"""Recompute a product evidence summary from raw rows (Implementation level).

Case kinds: product-p1 (G1, TASK-004) and product-p2 (G4 onwards, TASK-005).
The P1 analyzer that recorded the G1 runs is kept byte for byte in
experiments/sources/product-p1/ so verify.py still recomputes those runs.
The pre-resource-v2 G4 analyzer is archived in sources/product-p2-g4/.

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
RESOURCE_SCHEMA = 'b4-resource-v2'
RESOURCE_KINDS = {'resources_baseline', 'resources_begin', 'resources_sample', 'resources'}


def _integer(value, minimum=0):
    # bool is an int in Python, but is not a JSON counter or timestamp.
    return type(value) is int and minimum <= value <= (1 << 63) - 1


def _required_ints(obj, fields, minimum=0):
    return isinstance(obj, dict) and all(_integer(obj.get(k), minimum) for k in fields)


def _stats_valid(stats, role):
    if not isinstance(stats, dict):
        return False
    proc = stats.get('process')
    if (not _required_ints(proc, ('cpu_ms',))
            or not _required_ints(proc, ('goroutines', 'open_fds', 'rss_bytes', 'heap_bytes'), 1)):
        return False
    if role == 'ois':
        return (_required_ints(stats, ('tickets', 'accesses_online', 'relay_bytes', 'max_buffered_bytes'))
                and _required_ints(stats, ('epoch',), 1)
                and type(stats.get('store_healthy')) is bool
                and _required_ints(stats.get('channels'), ('preparing', 'open', 'closing', 'closed')))
    closed = stats.get('closed')
    return (_required_ints(stats, ('epoch', 'generation', 'license_remaining_ms', 'channels',
                                 'relay_bytes', 'max_buffered_bytes'))
            and isinstance(stats.get('access_id'), str) and bool(stats['access_id'])
            and type(stats.get('connected')) is bool and type(stats.get('registered')) is bool
            and isinstance(stats.get('config_error', ''), str)
            and isinstance(closed, dict) and all(isinstance(k, str) and _integer(v) for k, v in closed.items()))


def _snapshots(obs):
    """Validate a complete Linux process snapshot; never synthesize missing stats."""
    oics = obs.get('oics')
    if not isinstance(oics, dict) or any(not isinstance(a, str) or not a for a in oics):
        raise ValueError('missing/invalid OIC role map')
    roles = {'ois': obs.get('ois'), **{'oic:' + a: v for a, v in oics.items()}}
    for name, snap in roles.items():
        if (not isinstance(snap, dict) or not _integer(snap.get('pid'), 1)
                or not _integer(snap.get('sampled_ns'))
                or not _stats_valid(snap.get('stats'), 'ois' if name == 'ois' else 'oic')):
            raise ValueError('missing/invalid snapshot: ' + name)
        if name != 'ois' and snap['stats']['access_id'] != name[4:]:
            raise ValueError('OIC access_id does not match role: ' + name)
    return roles


def _empty_channels(roles):
    s = roles['ois']['stats']
    return (all(s['channels'][k] == 0 for k in ('preparing', 'open', 'closing')) and s['tickets'] == 0
            and all(v['stats']['channels'] == 0 for k, v in roles.items() if k != 'ois'))


def _resource_analysis(spec, records, results, repeats):
    """Recompute B4 from raw observations, including unsuccessful polling samples.

    Malformed evidence fails permanently. Valid but unsettled samples reset the
    consecutive-success counter; cleanup may use the rest of its fixed deadline.
    """
    if (not isinstance(spec, dict) or spec.get('schema') != RESOURCE_SCHEMA
            or spec.get('vector') != 'TV-B4-03' or not isinstance(spec.get('required_tests'), dict)
            or not spec['required_tests'] or repeats != 1
            or any(not isinstance(t, str) or not t or not _integer(n, 1)
                   for t, n in spec['required_tests'].items())):
        raise ValueError('invalid resource preregistration (one execution per test required)')
    expected = spec.get('expected_oics')
    if (not isinstance(expected, dict) or set(expected) != set(spec['required_tests'])
            or any(not isinstance(a, list) or any(not isinstance(x, str) or not x for x in a)
                   or len(a) != len(set(a)) for a in expected.values())):
        raise ValueError('invalid preregistered OIC role sets')
    profiles, checks, counts, frozen, problems = {}, {}, {}, {}, []
    for event_test, obs in records:
        test, kind = obs.get('test'), obs.get('kind')
        label = str(test) + ': ' + str(kind)
        try:
            if not isinstance(test, str) or not test or test != event_test:
                raise ValueError('OBS.test does not match its Go test event')
            if not isinstance(kind, str) or kind not in RESOURCE_KINDS or obs.get('schema') != RESOURCE_SCHEMA:
                raise ValueError('missing/unknown resource schema or record kind')
            if obs.get('backend') not in ('sqlite', 'postgres'):
                raise ValueError('missing/invalid backend')
            if test in spec['required_tests'] and obs['backend'] != test.rsplit('/', 1)[-1]:
                raise ValueError('backend differs from preregistered test leaf')
            if not isinstance(obs.get('profile_id'), str) or not obs['profile_id']:
                raise ValueError('missing/invalid profile_id')
            pk = (test, obs['profile_id'])
            if kind == 'resources_baseline':
                if pk in profiles:
                    raise ValueError('duplicate/overwritten baseline profile')
                roles = _snapshots(obs)
                if (not _integer(obs.get('recorded_ns')) or not _empty_channels(roles)
                        or any(s['sampled_ns'] > obs['recorded_ns'] for s in roles.values())):
                    raise ValueError('baseline must be empty and captured before it is recorded')
                previous = [v for (t, _), v in profiles.items() if t == test]
                began = any(t == test for t, _ in checks)
                if test in spec['required_tests']:
                    if began:
                        raise ValueError('required test created a baseline after cleanup began')
                    if previous and not (
                            test == 'TestTVB1_17_PrepareOutOfOrder/sqlite' and len(previous) == 1
                            and previous[0]['observation']['oics'] == {} and set(obs['oics']) == {'lab-f'}
                            and obs['ois'] == previous[0]['observation']['ois']):
                        raise ValueError('required test replaced its initial baseline')
                # Migration may combine pre-sampled reference processes into a
                # new profile. Its new ID never permits a new measurement of a
                # known role/PID, or a post-fault reference process.
                for name, snap in roles.items():
                    key = (test, name, snap['pid'])
                    if key in frozen and snap != frozen[key]:
                        raise ValueError('baseline resampled a frozen role/PID: ' + name)
                    if key not in frozen and began:
                        raise ValueError('new reference process appeared after cleanup began: ' + name)
                for name, snap in roles.items():
                    frozen[(test, name, snap['pid'])] = snap
                profiles[pk] = {'observation': obs, 'roles': roles}
                continue
            profile = profiles.get(pk)
            if profile is None or obs['backend'] != profile['observation']['backend']:
                raise ValueError('missing baseline or profile/backend mismatch')
            if not isinstance(obs.get('check_id'), str) or not obs['check_id']:
                raise ValueError('missing/invalid check_id')
            ck = (test, obs['check_id'])
            if kind == 'resources_begin':
                if ck in checks:
                    raise ValueError('duplicate check_id')
                if (not _required_ints(obs, ('started_ns', 'deadline_ns'))
                        or obs['deadline_ns'] - obs['started_ns'] != 2_000_000_000
                        or obs.get('origin') != 'vector_cleanup'
                        or profile['observation']['recorded_ns'] >= obs['started_ns']
                        or any(s['sampled_ns'] >= obs['started_ns'] for s in profile['roles'].values())):
                    raise ValueError('invalid deadline, origin, or baseline captured after cleanup began')
                if test in expected and set(profile['observation']['oics']) != set(expected[test]):
                    raise ValueError('baseline roles differ from preregistered OIC roles')
                previous = [v for (t, _), v in checks.items() if t == test]
                if any(v['completed_ns'] is None or v['completed_ns'] > obs['started_ns'] for v in previous):
                    raise ValueError('resource checks overlap or run out of order')
                checks[ck] = {'profile_id': obs['profile_id'], 'started_ns': obs['started_ns'],
                              'deadline_ns': obs['deadline_ns'], 'samples': 0, 'consecutive': 0,
                              'last_end_ns': obs['started_ns'], 'pids': None, 'completed_ns': None}
                continue
            check = checks.get(ck)
            if check is None or check['profile_id'] != obs['profile_id']:
                raise ValueError('missing begin or check/profile mismatch')
            if check['completed_ns'] is not None:
                raise ValueError('record follows completed check')
            if kind == 'resources_sample':
                check['samples'] += 1
                roles = _snapshots(obs)
                if set(roles) != set(profile['roles']):
                    raise ValueError('process role set differs from baseline')
                if (not _required_ints(obs, ('started_ns', 'ended_ns'))
                        or not check['last_end_ns'] <= obs['started_ns'] < obs['ended_ns'] <= check['deadline_ns']
                        or any(not obs['started_ns'] <= s['sampled_ns'] <= obs['ended_ns'] for s in roles.values())):
                    raise ValueError('overlapping, out-of-order, or late sample')
                pids = {k: v['pid'] for k, v in roles.items()}
                if check['pids'] is not None and pids != check['pids']:
                    raise ValueError('process identity changed during check')
                check['pids'] = pids
                check['last_end_ns'] = obs['ended_ns']
                good = (_empty_channels(roles)
                        and roles['ois']['stats']['accesses_online'] == profile['roles']['ois']['stats']['accesses_online'])
                for name, snap in roles.items():
                    s, base = snap['stats'], profile['roles'][name]['stats']
                    good = (good and abs(s['process']['goroutines'] - base['process']['goroutines']) <= 10
                            and s['process']['open_fds'] == base['process']['open_fds'])
                    if name != 'ois':
                        good = good and s['connected'] and s['registered'] and s['epoch'] > 0 and s['generation'] > 0
                check['consecutive'] = check['consecutive'] + 1 if good else 0
                continue
            if (not _required_ints(obs, ('completed_ns', 'settled_ms', 'samples'))
                    or not check['last_end_ns'] <= obs['completed_ns'] <= check['deadline_ns']
                    or obs['settled_ms'] != (obs['completed_ns'] - check['started_ns']) // 1_000_000
                    or obs['samples'] != check['samples'] or check['consecutive'] < 3):
                raise ValueError('invalid final count, elapsed time, deadline, or fewer than three consecutive samples')
            check['completed_ns'] = obs['completed_ns']
            counts[test] = counts.get(test, 0) + 1
        except ValueError as exc:
            problems.append(label + ': ' + str(exc))
    for (test, check_id), check in checks.items():
        if check['completed_ns'] is None:
            problems.append(test + ': unfinished resource check ' + check_id)
    observed_tests = {test for test, _ in profiles} | {test for test, _ in checks}
    for test in sorted(observed_tests | spec['required_tests'].keys()):
        if results.get(('vectors', test)) != ['pass']:
            problems.append(test + ': resource evidence lacks a passing Go test')
        minimum = spec['required_tests'].get(test, 1)
        if counts.get(test, 0) < minimum:
            problems.append(test + ': resource checks ' + str(counts.get(test, 0)) + ' below ' + str(minimum))
    return {'schema': RESOURCE_SCHEMA, 'result': 'fail' if problems else 'pass',
            'profiles': len(profiles), 'checks': [dict(test=t, check_id=c, **v) for (t, c), v in checks.items()],
            'completed_checks': dict(sorted(counts.items())), 'required_tests': spec['required_tests']}, problems


def analyze(case, rows, repeats):
    if not isinstance(case, dict) or case.get('kind') not in KINDS:
        raise ValueError('unsupported case')
    if not isinstance(repeats, int) or repeats < 1:
        raise ValueError('repeats must be a positive integer')
    problems, failed, zero = [], [], []
    facts, steps, results = {}, {}, {}
    engines, observations, fuzz_started, fuzz_elapsed = {}, {}, set(), {}
    coverage, resource_rows = None, []
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
                if action == 'pass':
                    fuzz_elapsed.setdefault((step, test), []).append(event.get('Elapsed', 0))
                if action == 'fail':
                    failed.append(step + ': ' + test)
            elif action == 'output':
                text = event.get('Output', '')
                if test and re.search(r'fuzz:.*now fuzzing with [1-9][0-9]* workers', text):
                    fuzz_started.add((step, test))
                if ZERO in text:
                    zero.append(step + ': ' + text.strip())
                m = OBS.search(text)
                if m and step == 'vectors':
                    obs = json.loads(m.group(1))
                    observations.setdefault(obs.get('kind', 'unknown'), []).append(obs)
                    if str(obs.get('kind', '')).startswith('resources') or obs.get('schema') == RESOURCE_SCHEMA:
                        resource_rows.append((test, obs))
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

    for step, target in case.get('fuzz', {}).items():
        # A nonexistent -fuzz target still emits package events and exits zero.
        # Seed-only execution is also insufficient: require active fuzzing and
        # the registered target's successful run for the full time budget.
        duration = re.fullmatch(r'([0-9]+(?:\.[0-9]+)?)(ms|s|m|h)', case.get('fuzz_time', ''))
        if duration is None:
            raise ValueError('unsupported fuzz_time')
        seconds = float(duration[1]) * {'ms': .001, 's': 1, 'm': 60, 'h': 3600}[duration[2]]
        key = (step, target)
        elapsed = fuzz_elapsed.get(key, [])
        if (seconds <= 0 or step not in case['steps'] or key not in fuzz_started
                or results.get(key) != ['pass'] * repeats
                or len(elapsed) != repeats or any(t < seconds for t in elapsed)):
            verdict_ok = False
            problems.append(step + ': ' + target + ' did not complete active fuzzing for ' + case['fuzz_time'])

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
    resources = None
    if 'resources' in case:
        resources, resource_problems = _resource_analysis(case['resources'], resource_rows, results, repeats)
        problems.extend(resource_problems)
        checked = vectors.setdefault('TV-B4-03', {'result': 'missing', 'checked': []})
        if resource_problems:
            checked['result'] = 'fail'
        checked['checked'].append('raw: ' + RESOURCE_SCHEMA)
    if any(v['result'] != 'pass' for v in vectors.values()):
        verdict_ok = False
    criteria = {name: ('pass' if all(vectors.get(v, {}).get('result') == 'pass' for v in ids) else 'fail')
                for name, ids in case['criteria'].items()}
    if any(r != 'pass' for r in criteria.values()):
        verdict_ok = False

    summary = {
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
    if resources is not None:
        summary['resources'] = resources
    return summary
