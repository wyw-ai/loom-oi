"""Recompute a product evidence summary from raw rows (Implementation level).

Case kinds: product-p1 (G1, TASK-004) and product-p2 (G4 onwards, TASK-005).
The P1 analyzer that recorded the G1 runs is kept byte for byte in
experiments/sources/product-p1/ so verify.py still recomputes those runs.
The pre-resource-v2 G4 analyzer is archived in sources/product-p2-g4/.
The first resource-v2 run keeps its chunk-unaware analyzer in
sources/product-g1-r2-chunks/; its original failed verdict is not rewritten.

The runner records every `go test -json` event and every tool output line;
this module derives all verdicts from those rows only, so verify.py can
reproduce summary.json exactly. It runs nothing and trusts no summary.
"""
import json
import hashlib
import importlib.util
from pathlib import Path
import re

# verify.py reads this literal before executing a recorded analyzer. Names are
# a fixed whitelist, resolved beside this exact version, never from sys.path.
ANALYSIS_DEPENDENCIES = {
    'g2_behavior': ('g2_analyze.py',),
    'g2_clock': ('g2_analyze.py', 'g2_clock_analyze.py', 'clock_guard.py'),
}


def _sibling(name):
    if name not in {v for names in ANALYSIS_DEPENDENCIES.values() for v in names}:
        raise ValueError('unregistered analyzer dependency')
    path = Path(__file__).with_name(name)
    spec = importlib.util.spec_from_file_location('_product_'+name[:-3], path)
    module = importlib.util.module_from_spec(spec)
    exec(compile(path.read_bytes(), str(path), 'exec'), module.__dict__)
    return module

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


def _resource_analysis(spec, records, results, repeats, test_steps=None):
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
    backends = spec.get('expected_backends')
    if 'expected_backends' in spec and (not isinstance(backends, dict)
            or set(backends) != set(spec['required_tests'])
            or any(value not in ('sqlite', 'postgres') for value in backends.values())):
        raise ValueError('invalid preregistered backend map')
    test_steps = test_steps or {}
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
            backend = backends[test] if backends is not None and test in backends else test.rsplit('/', 1)[-1]
            if test in spec['required_tests'] and obs['backend'] != backend:
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
        if results.get((test_steps.get(test, 'vectors'), test)) != ['pass']:
            problems.append(test + ': resource evidence lacks a passing Go test')
        minimum = spec['required_tests'].get(test, 1)
        if counts.get(test, 0) < minimum:
            problems.append(test + ': resource checks ' + str(counts.get(test, 0)) + ' below ' + str(minimum))
    return {'schema': RESOURCE_SCHEMA, 'result': 'fail' if problems else 'pass',
            'profiles': len(profiles), 'checks': [dict(test=t, check_id=c, **v) for (t, c), v in checks.items()],
            'completed_checks': dict(sorted(counts.items())), 'required_tests': spec['required_tests']}, problems


def _clock_inputs(envelopes, source_sha, facts):
    """Bind independently recorded guard bytes to this runner's fixed subject."""
    prepare = envelopes['clock_preparation']
    result = envelopes['clock_result']
    audit = envelopes['clock_guard']
    if len(prepare) != 1 or len(result) != 1 or not audit:
        raise ValueError('clock preparation/result must be unique and guard audit complete')
    prepare, result = prepare[0], result[0]
    if (prepare.get('schema') != 'g2-clock-preparation-v1'
            or result.get('schema') != 'g2-clock-result-v1'
            or prepare.get('source_sha') != source_sha or result.get('source_sha') != source_sha
            or not re.fullmatch('[0-9a-f]{40}', source_sha or '')):
        raise ValueError('clock fixed source or schema mismatch')
    run_id = prepare.get('run_id')
    workflow = prepare.get('workflow_ref')
    nonce = prepare.get('nonce_sha256')
    if (not isinstance(run_id, str) or not re.fullmatch('[1-9][0-9]*', run_id)
            or result.get('run_id') != run_id or not isinstance(workflow, str)
            or not workflow.startswith('wyw-ai/loom-oi/.github/workflows/g2-evidence.yml@refs/heads/evidence/G2-')
            or not isinstance(nonce, str) or not re.fullmatch('[0-9a-f]{64}', nonce)
            or prepare.get('runner_environment') != 'github-hosted'
            or not _required_ints(prepare, ('runner_uid', 'runner_gid'), 1)
            or prepare.get('go_version') != facts.get('go_version')
            or not isinstance(prepare.get('go_env'), dict) or prepare['go_env'].get('GOOS') != 'linux'):
        raise ValueError('clock preparation identity/environment mismatch')
    for key in ('source_hashes', 'binaries'):
        values = prepare.get(key)
        if (not isinstance(values, dict) or not values
                or any(not isinstance(k, str) or not k or not isinstance(v, str)
                       or not re.fullmatch('[0-9a-f]{64}', v) for k, v in values.items())):
            raise ValueError('invalid clock preparation '+key)
    if set(prepare['binaries']) != {'integration.test', 'bin/ois', 'bin/oic', 'bin/oi'}:
        raise ValueError('clock binary inventory mismatch')
    required_sources = {'experiments/product/analyze.py', 'experiments/product/clock_run.py',
                        *('experiments/product/'+name for name in ANALYSIS_DEPENDENCIES['g2_clock'])}
    if not required_sources <= prepare['source_hashes'].keys():
        raise ValueError('clock preparation omitted analyzer/wrapper source')
    if (any(type(result.get(k)) is not int or result[k] != 0 for k in
            ('guard_exit_code', 'recovery_exit_code', 'exit_code'))
            or result.get('verified') is not True or result.get('interrupted') != []
            or result.get('errors') != []):
        raise ValueError('clock wrapper reported failure/incomplete restoration')
    files = result.get('files')
    if (not isinstance(files, dict) or set(files) != {'raw.jsonl', 'guard/guard.jsonl',
                                                     'guard/state.json', 'guard/finished.json'}
            or any(not isinstance(v, str) or not re.fullmatch('[0-9a-f]{64}', v) for v in files.values())):
        raise ValueError('clock original-file inventory missing/invalid')
    identity = dict(source_sha=source_sha, run_id=run_id, workflow_ref=workflow,
                    nonce_sha256=nonce, job='clock')
    if (any(not isinstance(event, dict) or any(event.get(k) != v for k, v in identity.items()) for event in audit)
            or audit[0].get('runner_uid') != prepare['runner_uid']
            or audit[0].get('runner_gid') != prepare['runner_gid']):
        raise ValueError('clock guard belongs to another prepared run/user/source')
    audit_bytes = ''.join(json.dumps(event, sort_keys=True)+'\n' for event in audit).encode('utf-8')
    if hashlib.sha256(audit_bytes).hexdigest() != files['guard/guard.jsonl']:
        raise ValueError('clock guard envelopes differ from original audit bytes')
    return audit


def _component_vectors(case, vectors, spec, component, problems, step):
    """A Go PASS cannot hide a semantic rejection in the same declared vector."""
    groups = spec['groups']
    failed_groups = {test for test, result in component.get('groups', {}).items()
                     if result.get('result') != 'pass'}
    if (problems or component.get('result') != 'pass') and not failed_groups:
        failed_groups = set(groups)
    roots = {groups[test].get('root', spec.get('root')) for test in failed_groups if test in groups}
    for entry in case['vectors']:
        if entry['step'] == step and roots.intersection(entry['tests']):
            vectors[entry['id']]['result'] = 'fail'
            vectors[entry['id']]['checked'].append('raw: '+spec['schema'])


def analyze(case, rows, repeats):
    if not isinstance(case, dict) or case.get('kind') not in KINDS:
        raise ValueError('unsupported case')
    if not isinstance(repeats, int) or repeats < 1:
        raise ValueError('repeats must be a positive integer')
    g2_enabled = 'g2_behavior' in case or 'g2_clock' in case
    if 'g2_clock' in case and 'g2_behavior' not in case:
        raise ValueError('real clock requires the ordinary G2 companion in the same run')
    modules = {}
    for key, filename in (('g2_behavior', 'g2_analyze.py'), ('g2_clock', 'g2_clock_analyze.py')):
        if key in case:
            modules[key] = _sibling(filename)
            if case[key] != modules[key].expected_spec():
                raise ValueError(key+': preregistration differs from fixed inventory')
    if g2_enabled and any(not isinstance(entry.get('package'), str) or not entry['package']
                          for entry in case['vectors']):
        raise ValueError('G2 preregistration requires exact Go packages')
    ordinary_groups = case.get('g2_behavior', {}).get('groups', {})
    clock_groups = case.get('g2_clock', {}).get('groups', {})
    if g2_enabled:
        resources_spec = case.get('resources', {})
        for test, group in {**{t: g for t, g in ordinary_groups.items() if g['resource']},
                            **clock_groups}.items():
            count = 20 if test in clock_groups else 1
            if (resources_spec.get('required_tests', {}).get(test) != count
                    or resources_spec.get('expected_oics', {}).get(test) != group['oics']
                    or resources_spec.get('expected_backends', {}).get(test) != group['backend']):
                raise ValueError('G2 resource preregistration missing or different: '+test)
    resource_steps = {test: 'clock' for test in clock_groups}
    package = 'github.com/wyw-ai/loom-oi/tests/integration'
    problems, failed, zero = [], [], []
    facts, steps, results = {}, {}, {}
    engines, observations, fuzz_started, fuzz_elapsed = {}, {}, set(), {}
    coverage, resource_rows, pending_output, output_problems = None, [], {}, []
    qualified_results, resource_results, resource_output_problems = {}, {}, []
    g2_records = {'vectors': [], 'clock': []}
    envelopes = {'clock_preparation': [], 'clock_guard': [], 'clock_result': []}
    envelope_order, subject_facts = [], []

    def output_line(step, pkg, test, text, terminated=True):
        # test2json may split one log line into several Output events. Parse
        # only reconstructed lines, keeping independent package/test streams
        # apart. An incomplete/malformed OBS must never disappear silently.
        if test and re.search(r'fuzz:.*now fuzzing with [1-9][0-9]* workers', text):
            fuzz_started.add((step, test))
        if ZERO in text:
            zero.append(step + ': ' + text.strip())
        if (step == 'vectors' or g2_enabled and step == 'clock') and re.search(r'\bOBS(?:\s|$)', text):
            match = OBS.search(text)
            if not terminated or match is None:
                output_problems.append(str(test) + ': incomplete/malformed OBS line')
            else:
                try:
                    obs = json.loads(match.group(1))
                    if not isinstance(obs, dict):
                        raise ValueError('observation must be an object')
                except ValueError:
                    output_problems.append(str(test) + ': invalid OBS JSON')
                else:
                    observations.setdefault(obs.get('kind', 'unknown'), []).append(obs)
                    if str(obs.get('kind', '')).startswith('resources') or obs.get('schema') == RESOURCE_SCHEMA:
                        if g2_enabled and (pkg != package or step != resource_steps.get(test, 'vectors')):
                            message = str(test)+': resource package/step provenance mismatch'
                            output_problems.append(message)
                            resource_output_problems.append(message)
                        else:
                            resource_rows.append((test, obs))
                    if g2_enabled:
                        relevant = (step == 'clock' or obs.get('kind') == 'G2'
                                    or obs.get('schema') == 'g2-behavior-v1'
                                    or test in ordinary_groups or str(test).startswith('TestTVG2_'))
                        if relevant:
                            g2_records[step].append((pkg, test, obs))
        match = ENGINE.search(text)
        if match:
            versions = engines.setdefault(match.group(1), [])
            if match.group(2) not in versions:
                versions.append(match.group(2))

    def flush_output(key):
        tail = pending_output.pop(key, '')
        if tail:
            output_line(*key, tail, terminated=False)

    for row in rows:
        step, kind = row['step'], row['kind']
        if kind == 'fact':
            facts[row['name']] = row['value']
            if g2_enabled and row['name'] == 'subject_version':
                subject_facts.append((step, row['value']))
            continue
        st = steps.setdefault(step, {'start': 0, 'end': 0, 'events': 0, 'exit_code': None, 'timed_out': False,
                                     'tests': {'pass': 0, 'fail': 0, 'skip': 0}})
        if kind in envelopes:
            if (not g2_enabled or 'g2_clock' not in case or step != 'clock'
                    or set(row) != {'step', 'kind', 'event'} or not isinstance(row['event'], dict)):
                raise ValueError('invalid/unregistered clock envelope')
            envelopes[kind].append(row['event'])
            envelope_order.append(kind)
        elif kind == 'start':
            st['start'] += 1
        elif kind == 'end':
            for key in list(pending_output):
                if key[0] == step:
                    flush_output(key)
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
            key = (step, event.get('Package'), test)
            if action in ('pass', 'fail', 'skip'):
                flush_output(key)
            if test and action in ('pass', 'fail', 'skip'):
                results.setdefault((step, test), []).append(action)
                qualified_results.setdefault((step, event.get('Package'), test), []).append(action)
                if event.get('Package') == package:
                    resource_results.setdefault((step, test), []).append(action)
                st['tests'][action] += 1
                if action == 'pass':
                    fuzz_elapsed.setdefault((step, test), []).append(event.get('Elapsed', 0))
                if action == 'fail':
                    failed.append(step + ': ' + test)
            elif action == 'output':
                text = pending_output.pop(key, '') + event.get('Output', '')
                lines = text.split('\n')
                for line in lines[:-1]:
                    output_line(step, event.get('Package'), test, line)
                if lines[-1]:
                    pending_output[key] = lines[-1]
        elif kind != 'line':
            raise ValueError('unknown row kind: ' + kind)

    for key in list(pending_output):
        flush_output(key)
    problems.extend(output_problems)
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

    verdict_ok = not output_problems
    source_sha = facts.get('subject_version')
    if g2_enabled and (subject_facts != [('env', source_sha)] or not isinstance(source_sha, str)
                       or not re.fullmatch('[0-9a-f]{40}', source_sha)):
        verdict_ok = False
        problems.append('G2 requires one fixed runner subject_version fact')
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
            got = (qualified_results.get((entry['step'], entry['package'], name), []) if g2_enabled
                   else results.get((entry['step'], name), []))
            checked['checked'].append(entry['step'] + ': ' + name)
            if got == ['pass'] * repeats:
                continue
            outcome = 'missing' if not got else 'fail'
            if checked['result'] != 'fail':
                checked['result'] = outcome
            problems.append(entry['id'] + ': ' + name + ' ' + (','.join(got) or 'missing'))
    resources = None
    if 'resources' in case:
        resources, resource_problems = _resource_analysis(case['resources'], resource_rows,
            resource_results if g2_enabled else results, repeats, resource_steps if g2_enabled else None)
        if resource_output_problems:
            resources['result'] = 'fail'
            resource_problems.extend(resource_output_problems)
        problems.extend(resource_problems)
        checked = vectors.setdefault('TV-B4-03', {'result': 'missing', 'checked': []})
        if resource_problems:
            checked['result'] = 'fail'
        checked['checked'].append('raw: ' + RESOURCE_SCHEMA)
    components = {}
    if 'g2_behavior' in modules:
        ordinary_results = {(pkg, test): values for (step, pkg, test), values in qualified_results.items()
                            if step == 'vectors'}
        component, semantic_problems = modules['g2_behavior'].analyze_g2(
            case['g2_behavior'], g2_records['vectors'], ordinary_results, resources, repeats)
        components['g2_behavior'] = component
        problems.extend(semantic_problems)
        if component.get('result') != 'pass' or semantic_problems:
            verdict_ok = False
        _component_vectors(case, vectors, case['g2_behavior'], component, semantic_problems, 'vectors')
    if 'g2_clock' in modules:
        clock_problems = []
        try:
            if (not envelope_order or envelope_order[0] != 'clock_preparation'
                    or envelope_order[-1] != 'clock_result'
                    or any(kind != 'clock_guard' for kind in envelope_order[1:-1])):
                raise ValueError('clock envelopes missing or out of order')
            audit = _clock_inputs(envelopes, source_sha, facts)
        except (ValueError, TypeError, KeyError) as exc:
            audit = envelopes['clock_guard']
            clock_problems.append('clock envelopes: '+str(exc))
        clock_results = {(pkg, test): values for (step, pkg, test), values in qualified_results.items()
                         if step == 'clock'}
        component, semantic_problems = modules['g2_clock'].analyze_clock(
            case['g2_clock'], g2_records['clock'], clock_results, audit, resources, source_sha)
        clock_problems.extend(semantic_problems)
        if components['g2_behavior'].get('result') != 'pass':
            clock_problems.append('clock evidence lacks passing ordinary G2 companion')
        if clock_problems:
            component = dict(component, result='fail')
        components['g2_clock'] = component
        problems.extend(clock_problems)
        if component.get('result') != 'pass' or clock_problems:
            verdict_ok = False
        _component_vectors(case, vectors, case['g2_clock'], component, clock_problems, 'clock')
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
        'scope': case.get('scope', ('Real G2 product behavior on one Linux runner with SQLite and PostgreSQL; '
                                   'not the 1C1G target (G5), HA (G6), or real service compatibility (G7).')
                          if g2_enabled else SCOPE),
    }
    if resources is not None:
        summary['resources'] = resources
    summary.update(components)
    return summary
