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
import re
import hashlib
from functools import lru_cache

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


G3_SCHEMA = 'g3-resource-v1'
G3_KINDS = {'g3_resources_baseline', 'g3_resources_begin', 'g3_resources_sample', 'g3_resources_complete'}
G3_BEHAVIOR_SCHEMA = 'g3-behavior-v1'
G3_SCENARIOS = {
    'TV-G3-01': ('consumer_fin_first', 'target_fin_first'),
    'TV-G3-02': ('upload_target_paused', 'download_consumer_paused'),
    'TV-G3-03': ('consumer_open_rst', 'provider_open_rst', 'consumer_preparing_rst', 'provider_preparing_rst'),
    'TV-G3-04': ('consumer_leg_loss', 'provider_leg_loss'),
    'TV-G3-05': ('upload_backpressure_revoke', 'download_backpressure_revoke', 'preparing_revoke', 'control_cut', 'control_blackhole'),
    'TV-G3-06': ('mixed_limit_reuse', 'repeated_failures'),
}
G3_EVENTS = {
    'G3-fault': {'fin_delay', 'backpressure_held', 'data_rst', 'preparing_rst', 'response_lost', 'revoked',
                 'preparing_revoked', 'control_applied', 'blackhole_new_open', 'control_terminated', 'control_restored',
                 'quota_occupied', 'quota_rejected', 'cycle_first_rst', 'cycle_rst', 'cycle_revoked'},
    'G3-effect': {'committed_before_loss', 'no_replay_window', 'explicit_request_complete', 'final_counts'},
    'G3-transfer': {'ready', 'cli_ready', 'channel_closed', 'exchange_complete', 'exchange_fin_observed', 'buffer_bounds',
                    'ticket_rejected', 'surviving_provider_interrupted', 'interrupted_verified', 'preparing_interruption_verified',
                    'response_loss_verified', 'preparing_no_ready', 'channel_non_active', 'open_refused', 'revoke_verified',
                    'preparing_revoke_verified', 'expiry_marker_rejected', 'control_failure_verified', 'cycle_complete', 'quota_round_verified'},
}


def _g3_ids(value):
    return (isinstance(value, list) and all(isinstance(x, str) and x for x in value)
            and len(value) == len(set(value)))


def _g3_exact(left, right):
    # JSON numbers and booleans must not alias through Python dict equality.
    return json.dumps(left, sort_keys=True, allow_nan=False) == json.dumps(right, sort_keys=True, allow_nan=False)


def _g3_topology(expected, observed, roles, binding=None):
    """Malformed observations fail; valid unsettled observations may be polled."""
    accesses = {name[4:] for name in roles if name.startswith('oic:')}
    if (not isinstance(expected, dict) or not isinstance(observed, dict)
            or not _required_ints(expected, ('ois_accesses_online', 'active_data_connections',
                                            'active_target_workers', 'unclassified_connections'))
            or not isinstance(expected.get('oics'), dict) or set(expected['oics']) != accesses
            or not isinstance(expected.get('controls'), dict) or set(expected['controls']) != accesses
            or not isinstance(observed.get('controls'), dict) or set(observed['controls']) != accesses):
        raise ValueError('invalid registered or observed topology')
    good = roles['ois']['stats']['accesses_online'] == expected['ois_accesses_online']
    for count, ids in [('active_data_connections', 'active_data_connection_ids'),
                       ('active_target_workers', 'active_target_worker_ids'),
                       ('unclassified_connections', 'unclassified_connection_ids')]:
        if not _g3_ids(observed.get(ids)):
            raise ValueError('missing, duplicate or malformed connection/worker IDs')
        good = good and len(observed[ids]) == expected[count]
    control_ids = []
    for access in accesses:
        want, actual = expected['oics'][access], roles['oic:' + access]['stats']
        control, seen = expected['controls'][access], observed['controls'][access]
        if (not isinstance(want, dict) or any(type(want.get(k)) is not bool for k in ('connected', 'registered'))
                or not isinstance(control, dict) or not isinstance(seen, dict)
                or any(type(control.get(k)) is not bool or type(seen.get(k)) is not bool
                       for k in ('control_open', 'downstream_paused'))
                or not _required_ints(control, ('pending_control_count',))
                or not isinstance(seen.get('control_conn_id'), str) or not seen['control_conn_id']
                or not _g3_ids(seen.get('pending_control_ids'))):
            raise ValueError('invalid control topology')
        control_ids.extend([seen['control_conn_id'], *seen['pending_control_ids']])
        good = (good and all(actual[k] == want[k] for k in ('connected', 'registered'))
                and all(seen[k] == control[k] for k in ('control_open', 'downstream_paused'))
                and len(seen['pending_control_ids']) == control['pending_control_count'])
        if binding is not None:
            if (seen['control_conn_id'] != binding['control_conn_ids'][access]
                    or set(seen['pending_control_ids']) != set(binding['pending_control_ids'][access])):
                raise ValueError('control identities changed within a resource phase')
    if len(control_ids) != len(set(control_ids)):
        raise ValueError('control and pending identities must be distinct across accesses')
    return good


def _g3_sample(sample, expected, accesses, pids, binding=None):
    if not isinstance(sample, dict):
        raise ValueError('resource sample must be an object')
    roles = _snapshots(sample)
    if set(roles) != {'ois', *('oic:' + a for a in accesses)}:
        raise ValueError('resource role set differs from preregistration')
    if (not _required_ints(sample, ('sample_start_ns', 'sample_end_ns'))
            or sample['sample_start_ns'] >= sample['sample_end_ns']
            or any(not sample['sample_start_ns'] <= s['sampled_ns'] <= sample['sample_end_ns']
                   for s in roles.values())):
        raise ValueError('invalid sample timestamps')
    actual_pids = {k: s['pid'] for k, s in roles.items()}
    if len(set(actual_pids.values())) != len(actual_pids):
        raise ValueError('G3 roles must refer to distinct processes')
    if pids is not None and actual_pids != pids:
        raise ValueError('process replacement cannot satisfy G3 cleanup')
    for role, snapshot in roles.items():
        if role == 'ois':
            continue
        stats = snapshot['stats']
        if stats['epoch'] <= 0 or stats['generation'] <= 0:
            raise ValueError('unregistered OIC identity')
        if binding is not None:
            identity = binding['oics'][role[4:]]
            if any(stats[k] != identity[k] for k in ('epoch', 'generation')):
                raise ValueError('OIC generation changed within a resource phase')
    topology_good = _g3_topology(expected, sample.get('observed'), roles, binding)
    good = _empty_channels(roles) and topology_good
    return roles, actual_pids, good


def _g3_binding(sample):
    return {'ois_pid': sample['ois']['pid'],
            'oics': {a: {k: value['stats'][k] for k in ('epoch', 'generation')} | {'pid': value['pid']}
                     for a, value in sample['oics'].items()},
            'control_conn_ids': {a: c['control_conn_id'] for a, c in sample['observed']['controls'].items()},
            'pending_control_ids': {a: sorted(c['pending_control_ids']) for a, c in sample['observed']['controls'].items()}}


def _g3_resource_analysis(spec, records, results, repeats):
    """Check every preregistered G3 leaf/profile/phase, without post-fault baselines."""
    if (not isinstance(spec, dict) or spec.get('schema') != G3_SCHEMA or repeats != 1
            or spec.get('vector') != 'G3-RESOURCE-PHASES'
            or not isinstance(spec.get('package'), str) or not spec['package']
            or not _g3_ids(spec.get('required_tests')) or not spec['required_tests']
            or not _g3_ids(spec.get('expected_oics')) or not spec['expected_oics']
            or not isinstance(spec.get('required_profiles'), list)
            or not isinstance(spec.get('required_checks'), list)
            or not isinstance(spec.get('topologies'), dict)):
        raise ValueError('invalid G3 resource preregistration')
    required_tests, accesses = set(spec['required_tests']), set(spec['expected_oics'])
    instruments = spec.get('instrument_tests', [])
    if not _g3_ids(instruments) or required_tests.intersection(instruments):
        raise ValueError('invalid G3 instrument tests')
    all_tests = required_tests | set(instruments)
    if accesses != {'lab-a', 'lab-b'} or set(spec['topologies']) != {'online', 'control_cut', 'control_blackhole'}:
        raise ValueError('G3 must register the fixed two-access topology')
    for kind, topology in spec['topologies'].items():
        offline = kind != 'online'
        expected = {'ois_accesses_online': 1 if offline else 2,
                    'oics': {a: {'connected': not (a == 'lab-a' and kind == 'control_cut'),
                                 'registered': not (a == 'lab-a' and kind == 'control_cut')} for a in accesses},
                    'controls': {a: {'control_open': not (a == 'lab-a' and kind == 'control_cut'),
                                     'downstream_paused': a == 'lab-a' and kind == 'control_blackhole',
                                     'pending_control_count': int(a == 'lab-a' and kind == 'control_cut')} for a in accesses},
                    'active_data_connections': 0, 'active_target_workers': 0, 'unclassified_connections': 0}
        if not _g3_exact(topology, expected):
            raise ValueError('G3 topology differs from the fixed acceptance contract')
    if any(not isinstance(spec.get(k, []), list) for k in ('instrument_profiles', 'preparation_checks')):
        raise ValueError('invalid G3 preparation preregistration')
    if (any(not isinstance(p, dict) or p.get('test') not in required_tests for p in spec['required_profiles'])
            or any(not isinstance(p, dict) or p.get('test') not in instruments for p in spec.get('instrument_profiles', []))):
        raise ValueError('G3 formal and instrument profiles must be registered separately')
    all_profiles = spec['required_profiles'] + spec.get('instrument_profiles', [])
    all_checks = [(c, 'validation') for c in spec['required_checks']]
    all_checks += [(c, 'preparation') for c in spec.get('preparation_checks', [])]
    planned_profiles, planned_checks, order = {}, {}, {}
    for profile in all_profiles:
        if (not isinstance(profile, dict) or profile.get('test') not in all_tests
                or profile.get('profile_kind') not in ('online', 'control_cut', 'control_blackhole')
                or not isinstance(profile.get('vector'), str) or not profile['vector']):
            raise ValueError('invalid G3 profile preregistration')
        key = (profile['test'], profile['profile_kind'])
        if key in planned_profiles:
            raise ValueError('duplicate G3 profile preregistration')
        planned_profiles[key] = profile
    for check, stage in all_checks:
        if (not isinstance(check, dict) or check.get('test') not in all_tests
                or (stage == 'validation' and check['test'] not in required_tests)
                or not isinstance(check.get('phase'), str) or not check['phase']
                or not (check.get('cycle') is None or _integer(check['cycle']))
                or not _integer(check.get('order_in_leaf'))
                or (check['test'], check.get('profile_kind')) not in planned_profiles):
            raise ValueError('invalid G3 phase preregistration')
        key = (check['test'], stage, check['phase'], check.get('cycle'))
        if key in planned_checks:
            raise ValueError('duplicate G3 phase preregistration')
        planned_checks[key] = check
        order.setdefault((check['test'], stage), []).append(check['order_in_leaf'])
    if ({t for t, stage in order if stage == 'validation'} != required_tests
            or any(sorted(v) != list(range(len(v))) for v in order.values())
            or any((t, 'online') not in planned_profiles for t in all_tests)):
        raise ValueError('missing, duplicate or noncontiguous G3 phases/profiles')
    paths = {}
    for test in all_tests:
        parts = test.split('/')
        backend, scenario = (parts[1:3] if test in required_tests else parts[1:3][::-1]) if len(parts) == 3 else ('', '')
        if backend not in ('sqlite', 'postgres') or not scenario:
            raise ValueError('G3 must register exact backend/scenario leaves')
        paths[test] = (backend, scenario)
        for planned in [p for p in all_profiles + [c for c, _ in all_checks] if p['test'] == test]:
            if planned.get('backend') != backend or planned.get('scenario') != scenario:
                raise ValueError('G3 phase backend/scenario differs from test path')
    profiles, checks, identities, counters, problems = {}, {}, {}, {}, []
    used_check_ids, used_profile_ids, last_completed = set(), set(), {}
    for package, event_test, obs in records:
        label = str(event_test) + ': ' + str(obs.get('kind'))
        try:
            test, kind = obs.get('test'), obs.get('kind')
            if (package != spec['package'] or obs.get('package') != package or test != event_test
                    or test not in all_tests or obs.get('schema') != G3_SCHEMA or kind not in G3_KINDS
                    or 'phase' not in obs or 'cycle' not in obs):
                raise ValueError('G3 record is not bound to its registered Go package/test/schema')
            if (obs.get('backend'), obs.get('scenario')) != paths[test]:
                raise ValueError('G3 record backend/scenario mismatch')
            pk = (test, obs.get('profile_kind'))
            if pk not in planned_profiles or not isinstance(obs.get('profile_id'), str) or not obs['profile_id']:
                raise ValueError('unknown G3 profile')
            if obs.get('vector') != planned_profiles[pk]['vector']:
                raise ValueError('G3 resource vector differs from preregistration')
            expected = spec['topologies'][pk[1]]
            if kind == 'g3_resources_baseline':
                if (obs.get('stage') != 'preparation' or obs.get('phase') is not None or obs.get('cycle') is not None
                        or pk in profiles or obs['profile_id'] in used_profile_ids or any(k[0] == test for k in checks)
                        or (pk[1] != 'online' and (test, 'online') not in profiles)
                        or not _g3_exact(obs.get('expected_topology'), expected)
                        or not _required_ints(obs, ('prepare_start_ns', 'prepare_deadline_ns', 'recorded_ns'))
                        or obs['prepare_deadline_ns'] - obs['prepare_start_ns'] != 5_000_000_000):
                    raise ValueError('invalid, overwritten or post-fault G3 baseline')
                if pk[1] != 'online' and obs['prepare_start_ns'] < profiles[(test, 'online')]['recorded_ns']:
                    raise ValueError('fault preparation overlaps the original online baseline')
                samples = obs.get('preparation_samples')
                if not isinstance(samples, list) or len(samples) < 3:
                    raise ValueError('baseline requires its full preparation samples')
                previous, stable = obs['prepare_start_ns'], []
                for index, sample in enumerate(samples):
                    roles, pids, good = _g3_sample(sample, expected, accesses, identities.get(test))
                    identities.setdefault(test, pids)
                    if (sample['sample_start_ns'] < previous + (100_000_000 if index else 0)
                            or sample['sample_end_ns'] > obs['prepare_deadline_ns']):
                        raise ValueError('baseline sample overlaps, skips its interval or exceeds preparation budget')
                    previous = sample['sample_end_ns']
                    stable.append((roles, good))
                if (not previous <= obs['recorded_ns'] <= obs['prepare_deadline_ns']
                        or not all(good for _, good in stable[-3:])
                        or any(not _g3_exact(obs.get(k), samples[-1].get(k)) for k in ('ois', 'oics', 'observed'))):
                    raise ValueError('baseline must select the third final valid sample inside the preparation budget')
                if any(_g3_binding(s) != _g3_binding(samples[-1]) for s in samples[-3:]):
                    raise ValueError('baseline process/session/control binding is not stable for three samples')
                for role in stable[-1][0]:
                    procs = [r[role]['stats']['process'] for r, _ in stable[-3:]]
                    if (len({p['open_fds'] for p in procs}) != 1
                            or max(p['goroutines'] for p in procs) - min(p['goroutines'] for p in procs) > 2):
                        raise ValueError('baseline is not stable for three samples')
                profiles[pk] = {'profile_id': obs['profile_id'], 'recorded_ns': obs['recorded_ns'],
                                'roles': stable[-1][0], 'binding': _g3_binding(samples[-1])}
                used_profile_ids.add(obs['profile_id'])
                continue
            stage = obs.get('stage')
            if not (obs.get('cycle') is None or _integer(obs['cycle'])):
                raise ValueError('invalid G3 phase cycle')
            ck = (test, stage, obs.get('phase'), obs.get('cycle'))
            planned, profile = planned_checks.get(ck), profiles.get(pk)
            if (stage not in ('validation', 'preparation') or planned is None or planned['profile_kind'] != pk[1]
                    or profile is None or profile['profile_id'] != obs['profile_id']
                    or not isinstance(obs.get('check_id'), str) or not obs['check_id']):
                raise ValueError('missing baseline or unregistered/incorrect G3 phase')
            if kind == 'g3_resources_begin':
                check_id = obs['check_id']
                if (ck in checks or check_id in used_check_ids
                        or planned['order_in_leaf'] != counters.get((test, stage), 0)
                        or any(k[0] == test and c['completed_ns'] is None for k, c in checks.items())
                        or (stage == 'preparation' and (test, 'validation') in counters)
                        or not _required_ints(obs, ('start_ns', 'deadline_ns', 'origin_ns'))
                        or obs['deadline_ns'] - obs['start_ns'] != 2_000_000_000
                        or obs['origin_ns'] != obs['start_ns']
                        or not isinstance(obs.get('origin_event'), str) or not obs['origin_event']
                        or ('origin_event' in planned and obs['origin_event'] != planned['origin_event'])
                        or any(p['recorded_ns'] >= obs['start_ns'] for (t, _), p in profiles.items() if t == test)
                        or last_completed.get(test, 0) > obs['start_ns']):
                    raise ValueError('duplicate/out-of-order phase or reset/invalid cleanup deadline')
                if any((test, p[1]) not in profiles for p in planned_profiles if p[0] == test):
                    raise ValueError('all profiles must be frozen before formal cleanup starts')
                binding = obs.get('binding')
                if (not isinstance(binding, dict) or not _integer(binding.get('ois_pid'), 1)
                        or binding['ois_pid'] != identities[test]['ois']
                        or any(not isinstance(binding.get(k), dict) or set(binding[k]) != accesses
                               for k in ('oics', 'control_conn_ids', 'pending_control_ids'))):
                    raise ValueError('missing phase process/control binding')
                for access in accesses:
                    value = binding['oics'][access]
                    if (not _required_ints(value, ('pid', 'epoch', 'generation'), 1)
                            or value['pid'] != identities[test]['oic:' + access]
                            or not isinstance(binding['control_conn_ids'][access], str)
                            or not binding['control_conn_ids'][access]
                            or not _g3_ids(binding['pending_control_ids'][access])):
                        raise ValueError('phase identity differs from frozen processes')
                binding_samples = obs.get('binding_samples')
                if not isinstance(binding_samples, list) or not binding_samples:
                    raise ValueError('phase must retain every initial binding sample')
                previous = obs['start_ns']
                for index, sample in enumerate(binding_samples):
                    _g3_sample(sample, expected, accesses, identities[test])
                    if (sample['sample_start_ns'] < previous + (100_000_000 if index else 0)
                            or sample['sample_end_ns'] > obs['deadline_ns']):
                        raise ValueError('binding sampling exceeded the original cleanup interval/deadline')
                    previous = sample['sample_end_ns']
                if _g3_binding(binding_samples[-1]) != binding:
                    raise ValueError('binding differs from the final initial sample')
                if any(len(binding['pending_control_ids'][a]) != expected['controls'][a]['pending_control_count']
                       for a in accesses):
                    raise ValueError('phase binding lacks the fixed topology identities')
                if stage == 'validation' and pk[1] != 'online':
                    prep = checks.get((test, 'preparation', 'online_restored_preparation', None))
                    if (prep is None or prep['completed_ns'] is None
                            or any(binding[k] != prep['binding'][k] for k in ('ois_pid', 'oics', 'control_conn_ids'))):
                        raise ValueError('formal control fault did not use the restored preparation session')
                if obs['phase'] in ('online_restored_preparation', 'online_restored_cleanup'):
                    if stage == 'preparation':
                        refs = [p['binding'] for (t, k), p in profiles.items() if t == test and k != 'online']
                    else:
                        refs = [c['binding'] for k, c in checks.items() if k[0] == test and k[1] == 'validation'
                                and c['profile_kind'] != 'online' and c['completed_ns'] is not None]
                    if len(refs) != 1:
                        raise ValueError('restoration lacks its unique prior fault binding')
                    before = refs[0]
                    old, new = before['oics']['lab-a'], binding['oics']['lab-a']
                    if (old['epoch'] != new['epoch'] or new['generation'] <= old['generation']
                            or binding['control_conn_ids']['lab-a'] == before['control_conn_ids']['lab-a']
                            or any(binding[k]['lab-b'] != before[k]['lab-b'] for k in ('oics', 'control_conn_ids', 'pending_control_ids'))):
                        raise ValueError('control restoration must create a new lab-a generation/connection and preserve lab-b')
                used_check_ids.add(check_id)
                checks[ck] = {'check_id': obs['check_id'], 'profile_kind': pk[1], 'profile_id': obs['profile_id'],
                              'start_ns': obs['start_ns'], 'deadline_ns': obs['deadline_ns'], 'binding': binding,
                              'binding_samples': len(binding_samples), 'samples': 0, 'consecutive': 0,
                              'last_end_ns': previous, 'completed_ns': None}
                continue
            check = checks.get(ck)
            if (check is None or check['completed_ns'] is not None or check['check_id'] != obs['check_id']
                    or check['profile_id'] != obs['profile_id']
                    or not _required_ints(obs, ('start_ns', 'deadline_ns'))
                    or any(obs.get(k) != check[k] for k in ('start_ns', 'deadline_ns'))):
                raise ValueError('missing phase begin, changed deadline, or data after completion')
            if kind == 'g3_resources_sample':
                roles, _, good = _g3_sample(obs, expected, accesses, identities[test], check['binding'])
                if (obs['sample_start_ns'] < check['last_end_ns'] + (100_000_000 if check['samples'] else 0)
                        or obs['sample_end_ns'] > check['deadline_ns']):
                    raise ValueError('sample overlaps, skips its interval or exceeds cleanup deadline')
                for role, snap in roles.items():
                    actual, base = snap['stats']['process'], profile['roles'][role]['stats']['process']
                    good = (good and abs(actual['goroutines'] - base['goroutines']) <= 10
                            and actual['open_fds'] == base['open_fds'])
                check['samples'] += 1
                check['last_end_ns'] = obs['sample_end_ns']
                check['consecutive'] = check['consecutive'] + 1 if good else 0
                continue
            if (not _required_ints(obs, ('completed_ns', 'settled_ms', 'samples'))
                    or not check['last_end_ns'] <= obs['completed_ns'] <= check['deadline_ns']
                    or obs['samples'] != check['samples'] or check['consecutive'] < 3
                    or obs['settled_ms'] != (obs['completed_ns'] - check['start_ns']) // 1_000_000):
                raise ValueError('invalid phase completion/count or insufficient final stable samples')
            check['completed_ns'] = obs['completed_ns']
            last_completed[test] = obs['completed_ns']
            counters[(test, stage)] = counters.get((test, stage), 0) + 1
        except (ValueError, TypeError, KeyError) as exc:
            problems.append(label + ': ' + str(exc))
    for test in sorted(all_tests):
        if results.get(('vectors', spec['package'], test)) != ['pass']:
            problems.append(test + ': G3 resources lack the exact passing Go leaf')
    for key in planned_profiles:
        if key not in profiles:
            problems.append(str(key) + ': missing G3 baseline')
    for key in planned_checks:
        if key not in checks or checks[key]['completed_ns'] is None:
            problems.append(str(key) + ': missing/incomplete G3 resource phase')
    return {'schema': G3_SCHEMA, 'result': 'fail' if problems else 'pass',
            'profiles': sum(t in required_tests for t, _ in profiles),
            'instrument_profiles': sum(t in instruments for t, _ in profiles),
            'checks': [dict(test=k[0], stage=k[1], phase=k[2], cycle=k[3], **v) for k, v in checks.items()],
            'completed_checks': {t: n for (t, s), n in sorted(counters.items()) if s == 'validation'},
            'preparation_checks': {t: n for (t, s), n in sorted(counters.items()) if s == 'preparation'},
            'required_tests': spec['required_tests']}, problems


def _g3_need(condition, message):
    if not condition:
        raise ValueError(message)


@lru_cache(maxsize=8)
def _g3_pattern_hash(size):
    # fixture.Pattern(i) = byte(i*31 + i/251), period 251*256.
    block = bytes((i * 31 + i // 251) % 256 for i in range(251 * 256))
    digest = hashlib.sha256()
    for _ in range(size // len(block)):
        digest.update(block)
    digest.update(block[:size % len(block)])
    return digest.hexdigest()


def _g3_target(snapshot):
    _g3_need(_required_ints(snapshot, ('ID', 'ReceivedBytes', 'ResponseBytes', 'WireReceivedBytes'))
             and snapshot['ID'] > 0 and isinstance(snapshot.get('RequestID'), str)
             and all(type(snapshot.get(k)) is bool for k in ('RequestFIN', 'ResponseFIN', 'WriterEntered', 'WriterReturned', 'Closed'))
             and all(isinstance(snapshot.get(k), str) for k in ('Error', 'SHA256'))
             and isinstance(snapshot.get('Markers'), dict) and all(_integer(n) for n in snapshot['Markers'].values())
             and isinstance(snapshot.get('Events'), dict) and all(_integer(n, 1) for n in snapshot['Events'].values()),
             'malformed target snapshot')
    _g3_need(snapshot['WireReceivedBytes'] >= snapshot['ReceivedBytes'], 'target body exceeds wire bytes')
    for flag, name in [('RequestFIN', 'request_fin'), ('ResponseFIN', 'response_fin'),
                       ('WriterEntered', 'writer_entered'), ('WriterReturned', 'writer_returned'), ('Closed', 'closed')]:
        _g3_need(not snapshot[flag] or name in snapshot['Events'], 'target flag lacks its measured event')
    return snapshot


def _g3_history(value):
    _g3_need(isinstance(value, dict) and isinstance(value.get('Exchanges'), list)
             and isinstance(value.get('Effects'), dict) and _integer(value.get('Workers'))
             and all(isinstance(k, str) and _integer(v) for k, v in value['Effects'].items()),
             'missing target history/effect counters')
    snapshots = [_g3_target(x) for x in value['Exchanges']]
    _g3_need(len({x['ID'] for x in snapshots}) == len(snapshots)
             and value['Workers'] == sum(not x['Closed'] for x in snapshots), 'target worker/identity mismatch')
    return snapshots


def _g3_conn(value, prefix):
    return isinstance(value, str) and re.fullmatch(re.escape(prefix) + r'/[1-9][0-9]*', value) is not None


def _g3_buffers(obs):
    _g3_need(_required_ints(obs, ('F', 'ois_max_buffered', 'oic_max_buffered'))
             and obs['F'] == 32768 and max(obs['ois_max_buffered'], obs['oic_max_buffered']) <= 65536,
             'application buffers exceed the fixed 2F bound')
    for name in ('consumer_proxy', 'provider_proxy'):
        proxy = obs.get(name)
        _g3_need(isinstance(proxy, dict) and isinstance(proxy.get('Conns'), list)
                 and _integer(proxy.get('Workers')), 'missing proxy buffer snapshot')
        seen = set()
        for conn in proxy['Conns']:
            _g3_need(_required_ints(conn, ('ID',)) and conn['ID'] > 0 and conn['ID'] not in seen,
                     'duplicate/malformed proxy connection')
            seen.add(conn['ID'])
            for direction in ('ClientToServer', 'ServerToClient'):
                sample = conn.get(direction)
                _g3_need(_required_ints(sample, ('ReadBytes', 'ForwardedBytes', 'BufferedBytes', 'MaxBufferedBytes'))
                         and sample['ForwardedBytes'] <= sample['ReadBytes']
                         and sample['BufferedBytes'] <= sample['MaxBufferedBytes'] <= 32768,
                         'proxy buffer exceeds F or byte counters are inconsistent')


def _g3_buffer_bindings(obs, ready):
    _g3_buffers(obs)
    for key, role, ref in [('consumer_proxy', 'consumer', 'consumer_conn_id'),
                           ('provider_proxy', 'provider', 'provider_conn_id')]:
        number = int(ready[ref].split('/')[-1])
        matches = [c for c in obs[key]['Conns'] if c['ID'] == number]
        _g3_need(len(matches) == 1 and matches[0].get('Role') == role
                 and matches[0].get('ChannelID') == ready['channel_id']
                 and matches[0].get('AccessID') == ('lab-a' if role == 'provider' else ''),
                 'buffer sample belongs to a different data connection')


def _g3_behavior_analysis(spec, records, resource_rows, resource_summary, results, repeats):
    """Independent ordering/identity/effect checks, in addition to Go assertions."""
    if (not isinstance(spec, dict) or spec.get('schema') != G3_BEHAVIOR_SCHEMA or repeats != 1
            or not isinstance(spec.get('package'), str) or not spec['package']
            or not isinstance(spec.get('cases'), list) or not spec['cases']):
        raise ValueError('invalid G3 behavior preregistration')
    cases, grouped, problems = {}, {}, []
    for case in spec['cases']:
        if (not isinstance(case, dict) or not isinstance(case.get('test'), str)
                or case['test'] in cases or case.get('backend') not in ('sqlite', 'postgres')
                or not isinstance(case.get('scenario'), str) or not case['scenario']
                or case['test'].split('/')[1:] != [case['backend'], case['scenario']]
                or case.get('vector') not in G3_SCENARIOS
                or case.get('scenario') not in G3_SCENARIOS[case['vector']]):
            raise ValueError('invalid/duplicate G3 behavior leaf')
        cases[case['test']] = case
    baseline_times, raw_phases = {}, {}
    for _, test, obs in resource_rows:
        if obs.get('kind') == 'g3_resources_baseline' and _integer(obs.get('recorded_ns')):
            baseline_times[test] = max(baseline_times.get(test, 0), obs['recorded_ns'])
        if obs.get('stage') == 'validation' and obs.get('kind') in ('g3_resources_begin', 'g3_resources_complete'):
            key = (test, obs.get('phase'), obs.get('cycle'), obs['kind'])
            raw_phases.setdefault(key, []).append(obs)
    if resource_summary is not None and 'required_tests' in resource_summary:
        _g3_need(set(cases) == set(resource_summary['required_tests']), 'behavior/resource leaf sets differ')
    for package, event_test, obs in records:
        try:
            case = cases.get(event_test)
            _g3_need(case is not None and package == spec['package'] and obs.get('package') == package
                     and obs.get('test') == event_test and obs.get('schema') == G3_BEHAVIOR_SCHEMA
                     and all(obs.get(k) == case[k] for k in ('vector', 'backend', 'scenario'))
                     and obs.get('stage') in ('preparation', 'validation')
                     and obs.get('kind') in ('G3-transfer', 'G3-fault', 'G3-effect')
                     and isinstance(obs.get('event'), str) and bool(obs['event'])
                     and _integer(obs.get('observed_ns'), 1), 'behavior record identity/schema/time mismatch')
            _g3_need(obs['event'] in G3_EVENTS[obs['kind']]
                     and ('cycle' not in obs or _integer(obs['cycle'])), 'unknown/misclassified event or invalid cycle')
            _g3_need(all(_integer(v, 1) and v <= obs['observed_ns'] for k, v in obs.items()
                         if k.endswith('_ns')), 'behavior timestamp is invalid or occurs after its observation')
            for field in ('target', 'target_before'):
                if field not in obs:
                    continue
                value = obs[field]
                snapshots = _g3_history(value) if isinstance(value, dict) and 'Exchanges' in value else value if isinstance(value, list) else [value]
                limit = obs.get('applied_ns', obs['observed_ns']) if field == 'target_before' else obs['observed_ns']
                for snapshot in snapshots:
                    _g3_need(all(t <= limit for t in _g3_target(snapshot)['Events'].values()), 'target snapshot contains a future event')
            if obs['stage'] == 'preparation':
                _g3_need(obs['kind'] == 'G3-transfer' and obs['event'] in ('ready', 'cli_ready', 'channel_closed', 'exchange_complete'),
                         'business fault/effect cannot be disguised as preparation')
                continue
            _g3_need(event_test in baseline_times and obs['observed_ns'] > baseline_times[event_test],
                     'business event precedes frozen profiles')
            events = grouped.setdefault(event_test, [])
            _g3_need(not events or obs['observed_ns'] >= events[-1]['observed_ns'], 'business event time moved backwards')
            events.append(obs)
        except (ValueError, TypeError, KeyError) as exc:
            problems.append(str(event_test) + ': ' + str(exc))
    checked = []
    for test, case in cases.items():
        try:
            events, scenario = grouped.get(test, []), case['scenario']
            consumed_phases = set()
            consumed_readies = set()
            connection_owners = {}
            def claim_connection(ref, owner):
                _g3_need(ref not in connection_owners or connection_owners[ref] == owner,
                         'one proxy connection was rebound to another channel/role')
                connection_owners[ref] = owner
            for check in (resource_summary or {}).get('checks', []):
                if check.get('test') == test:
                    for access, ref in check.get('binding', {}).get('control_conn_ids', {}).items():
                        claim_connection(ref, ('control', access))
            _g3_need(results.get(('vectors', spec['package'], test)) == ['pass'], 'behavior lacks exact passing Go leaf')
            def event(name, count=1, cycle=None):
                matches = [e for e in events if e['event'] == name and (cycle is None or e.get('cycle') == cycle)]
                _g3_need(len(matches) == count, name + ': missing/duplicate event')
                return matches[0] if count == 1 else matches
            def phase(name, cycle=None):
                consumed_phases.add((name, cycle))
                matches = [c for c in (resource_summary or {}).get('checks', [])
                           if c['test'] == test and c['stage'] == 'validation' and c['phase'] == name and c['cycle'] == cycle]
                _g3_need(len(matches) == 1 and matches[0]['completed_ns'] is not None, name + ': missing resource completion')
                check = matches[0]
                begin = raw_phases.get((test, name, cycle, 'g3_resources_begin'), [])
                complete = raw_phases.get((test, name, cycle, 'g3_resources_complete'), [])
                _g3_need(len(begin) == len(complete) == 1, name + ': missing/duplicate raw phase endpoints')
                begin, complete = begin[0], complete[0]
                _g3_need(all(begin.get(k) == complete.get(k) == check.get(k)
                             for k in ('check_id', 'profile_id', 'start_ns', 'deadline_ns'))
                         and begin.get('origin_ns') == check['start_ns']
                         and _required_ints(check, ('start_ns', 'deadline_ns', 'completed_ns'), 1)
                         and check['deadline_ns'] - check['start_ns'] == 2_000_000_000
                         and check['start_ns'] <= check['completed_ns'] <= check['deadline_ns']
                         and complete.get('completed_ns') == check['completed_ns'], 'resource endpoint/deadline mismatch')
                return dict(check, origin_event=begin.get('origin_event'))
            def ready_for(channel):
                _g3_need(isinstance(channel, str) and channel, 'READY lacks a channel ID')
                matches = [e for e in events if e['event'] in ('ready', 'cli_ready') and e.get('channel_id') == channel]
                _g3_need(len(matches) == 1, 'channel lacks one precise READY connection binding')
                consumed_readies.add(channel)
                for key, prefix in [('consumer_conn_id', 'consumer/'), ('provider_conn_id', 'access/lab-a/')]:
                    _g3_need(isinstance(matches[0].get(key), str) and re.fullmatch(re.escape(prefix)+r'[1-9][0-9]*', matches[0][key]),
                             'invalid READY connection identity')
                    claim_connection(matches[0][key], ('data', channel))
                if matches[0]['event'] == 'cli_ready':
                    _g3_need(_g3_conn(matches[0].get('idle_api_conn_id'), 'consumer'), 'CLI READY lacks its separate HTTP connection')
                    claim_connection(matches[0]['idle_api_conn_id'], ('api', channel))
                return matches[0]
            def closed(channel, reason):
                matches = [e for e in events if e['event'] == 'channel_closed' and e.get('channel_id') == channel]
                _g3_need(bool(matches) and all(e.get('state') == 'closed' and e.get('reason') == reason for e in matches),
                         'channel terminal reason differs from the required failure')
                return matches[-1]
            def no_ready(channel):
                matches = [e for e in events if e['event'] == 'preparing_no_ready' and e.get('channel_id') == channel]
                _g3_need(matches and all(type(e.get('ready_events')) is int and e['ready_events'] == 0 for e in matches)
                         and not any(e['event'] in ('ready', 'cli_ready') and e.get('channel_id') == channel for e in events),
                         'preparing channel lacks zero-READY log evidence')
            def ticket(channel, cleanup):
                matches = [e for e in events if e['event'] == 'ticket_rejected' and e.get('channel_id') == channel]
                _g3_need(len(matches) == 1 and type(matches[0].get('status')) is int and matches[0]['status'] == 410
                         and cleanup['start_ns'] <= matches[0]['observed_ns'] <= cleanup['completed_ns'],
                         'old ticket not rejected for this channel inside cleanup')
            def completed(exchange, request_bytes, response_bytes):
                target = _g3_target(exchange.get('target'))
                _g3_need(_required_ints(exchange, ('request_bytes', 'response_bytes'))
                         and exchange['request_bytes'] == target['ReceivedBytes'] == request_bytes
                         and exchange['response_bytes'] == target['ResponseBytes'] == response_bytes
                         and exchange.get('request_id') == target['RequestID']
                         and all(target[k] for k in ('RequestFIN', 'ResponseFIN', 'Closed')) and target['Error'] == ''
                         and target['SHA256'] == _g3_pattern_hash(request_bytes)
                         and isinstance(exchange.get('effects'), dict) and type(exchange['effects'].get(target['RequestID'])) is int
                         and exchange['effects'][target['RequestID']] == 1,
                         'completed exchange lacks complete bytes, FINs, hash or exactly one effect')
                ready_for(exchange.get('channel_id'))
                closed(exchange['channel_id'], 'Completed')
                return target
            def target_closed(snapshot, at):
                target = _g3_target(snapshot)
                _g3_need(target['Closed'] and target['Events']['closed'] <= at, 'target survived the recorded cleanup')
                return target
            def zero_preparing(verified, channel):
                _g3_need(verified.get('channel_id') == channel
                         and _required_ints(verified, ('ready_sent', 'target_wire_bytes', 'target_exchange_after'))
                         and verified['ready_sent'] == verified['target_wire_bytes'] == 0
                         and isinstance(verified.get('target'), list), 'preparing bytes/identity evidence missing')
                for item in verified['target']:
                    snap = target_closed(item, verified['observed_ns'])
                    _g3_need(snap['ID'] > verified['target_exchange_after'] and snap['WireReceivedBytes'] == 0,
                             'preparing target received bytes')
                no_ready(channel)
            if case['vector'] in ('TV-G3-01', 'TV-G3-02'):
                exchange = event('exchange_complete')
                is_fin = case['vector'] == 'TV-G3-01'
                upload = scenario in ('target_fin_first', 'upload_target_paused')
                size = 1048576 if is_fin else 67108864
                target = completed(exchange, size if upload else 4, 4 if upload else size)
                fault = event('fin_delay' if is_fin else 'backpressure_held')
                _g3_need(fault.get('channel_id') == exchange['channel_id'] and fault.get('request_id') == exchange['request_id']
                         and fault.get('direction') == ('client_to_server' if upload else 'server_to_client')
                         and _required_ints(fault, ('hold_start_ns', 'released_ns'))
                         and fault['released_ns'] - fault['hold_start_ns'] >= 250_000_000
                         and fault['released_ns'] <= fault['observed_ns'] <= exchange['observed_ns'],
                         'half-close/backpressure direction, identity or 250ms hold is invalid')
                if is_fin:
                    first, second = ('response_fin', 'request_fin') if upload else ('request_fin', 'response_fin')
                    _g3_need(_integer(fault.get('first_fin_ns'), 1) and fault['first_fin_ns'] == target['Events'].get(first)
                             and fault['first_fin_ns'] <= fault['hold_start_ns']
                             and target['Events'].get(second, 0) >= fault['released_ns']
                             and type(fault.get('delay_ms')) is int and fault['delay_ms'] == 250,
                             'opposite direction did not remain open after the first FIN')
                else:
                    _g3_buffer_bindings(fault, ready_for(exchange['channel_id']))
                    _g3_buffer_bindings(event('buffer_bounds'), ready_for(exchange['channel_id']))
                    held = _g3_target(fault.get('target'))
                    _g3_need(fault.get('writer_entered') is True and fault.get('writer_returned') is False
                             and _integer(fault.get('written_bytes')) and fault['written_bytes'] < size
                             and (not upload or fault['written_bytes'] >= 32768)
                             and (held['ReceivedBytes'] == 0 if upload else held['WriterEntered'] and not held['WriterReturned']),
                             '64MiB writer was not actually blocked at the held direction')
                cleanup = phase('exchange_complete' if is_fin else 'transfer_complete')
                finished = event('exchange_fin_observed')
                _g3_need(_integer(finished.get('finished_ns'), 1) and cleanup['start_ns'] == finished['finished_ns']
                         and finished.get('channel_id') == exchange['channel_id']
                         and finished.get('request_id') == exchange['request_id']
                         and finished.get('request_bytes') == exchange['request_bytes']
                         and finished.get('response_bytes') == exchange['response_bytes']
                         and cleanup['origin_event'] == 'exchange_fin_observed'
                         and cleanup['start_ns'] <= exchange['observed_ns'] <= cleanup['completed_ns'],
                         'normal cleanup omitted waiting for target/channel closure or reset its origin')
            elif case['vector'] in ('TV-G3-03', 'TV-G3-04'):
                loss = case['vector'] == 'TV-G3-04'
                preparing = 'preparing' in scenario
                fault = event('response_lost' if loss else 'preparing_rst' if preparing else 'data_rst')
                _g3_need(_required_ints(fault, ('applied_ns', 'conn_id'), 1) and fault['applied_ns'] <= fault['observed_ns']
                         and fault.get('conn_ref') == ('consumer/' if scenario.startswith('consumer') else 'access/lab-a/') + str(fault['conn_id'])
                         and fault.get('role') == scenario and isinstance(fault.get('channel_id'), str), 'invalid data reset event')
                cleanup = phase('fault_cleanup' if loss else 'interrupted_cleanup')
                _g3_need(cleanup['start_ns'] == fault['applied_ns'] and cleanup['origin_event'] == 'first_reset_applied',
                         'RST cleanup origin was postponed')
                terminal = closed(fault['channel_id'], 'ChannelInterrupted')
                _g3_need(fault['applied_ns'] <= terminal['observed_ns'] <= cleanup['completed_ns'], 'reset termination is outside cleanup')
                if preparing:
                    verified = event('preparing_interruption_verified')
                    _g3_need(_required_ints(fault, ('ready_sent', 'tickets', 'conn_id'))
                             and fault['ready_sent'] == 0 and fault['tickets'] == 1 and fault['conn_id'] > 0
                             and fault.get('conn_ref') == ('consumer/' if scenario.startswith('consumer') else 'access/lab-a/') + str(fault['conn_id'])
                             and _integer(fault.get('prepare_started_ns'), 1)
                             and 0 <= fault['applied_ns'] - fault['prepare_started_ns'] < 2_000_000_000
                             and verified.get('ready_sent') == 0 and verified.get('target_wire_bytes') == 0
                             and _integer(verified.get('target_exchange_after')) and isinstance(verified.get('target'), list),
                             'preparing reset lacks an attached leg before its deadline or leaked payload/READY')
                    zero_preparing(verified, fault['channel_id'])
                    ticket(fault['channel_id'], cleanup)
                else:
                    ready = ready_for(fault['channel_id'])
                    role = 'consumer_conn_id' if scenario.startswith('consumer') else 'provider_conn_id'
                    _g3_need(fault.get('conn_ref') == ready[role], 'fault was not applied to the registered data leg')
                    if not loss:
                        before = _g3_target(fault.get('target_before'))
                        verified = event('interrupted_verified')
                        _g3_need(before['Markers'].get('before-rst') == 1 and before['ResponseBytes'] == 0
                                 and verified.get('local_end') == 'error_not_eof' and _g3_target(verified.get('target'))['Closed'],
                                 'open reset lacks delivered marker, zero response, interruption or target cleanup')
                        _g3_need(verified.get('channel_id') == fault['channel_id'] and before['RequestID'] == fault.get('request_id')
                                 and ready['observed_ns'] <= fault['applied_ns'], 'reset marker belongs to another invocation')
                        if scenario == 'consumer_open_rst':
                            provider = event('surviving_provider_interrupted')
                            _g3_need(provider.get('channel_id') == fault['channel_id'] and _required_ints(provider, ('before', 'after'))
                                     and provider['after'] == provider['before'] + 1, 'surviving provider did not record interruption')
                            ticket(fault['channel_id'], cleanup)
                if loss:
                    committed, window = event('committed_before_loss'), event('no_replay_window')
                    interrupted = event('response_loss_verified')
                    _g3_need(interrupted.get('channel_id') == fault['channel_id']
                             and interrupted.get('request_id') == fault.get('request_id')
                             and interrupted.get('local_end') == 'error_not_eof'
                             and cleanup['start_ns'] <= interrupted['observed_ns'] <= cleanup['completed_ns'],
                             'response loss lacks actual consumer interruption inside cleanup')
                    target_closed(interrupted.get('target'), cleanup['completed_ns'])
                    explicit, final = event('explicit_request_complete'), event('final_counts')
                    old, new = committed.get('request_id'), explicit.get('new_request_id')
                    snap = _g3_target(committed.get('target'))
                    _g3_need(isinstance(old, str) and isinstance(new, str) and old != new and old
                             and snap['RequestID'] == old and snap['ResponseBytes'] == fault.get('response_bytes') == 0
                             and committed.get('channel_id') == fault['channel_id'] and fault.get('request_id') == old
                             and snap['ReceivedBytes'] == 4 and snap['RequestFIN'] and snap['SHA256'] == _g3_pattern_hash(4)
                             and committed['observed_ns'] <= fault['applied_ns']
                             and _integer(snap['Events'].get('effect_committed'), 1)
                             and snap['Events']['effect_committed'] <= committed['observed_ns'],
                             'response loss does not follow a committed effect with zero response')
                    _g3_need(_required_ints(window, ('old_generation', 'new_generation', 'start_ns', 'end_ns'), 1)
                             and window['new_generation'] > window['old_generation']
                             and isinstance(window.get('old_control_conn_id'), str) and isinstance(window.get('new_control_conn_id'), str)
                             and window['old_control_conn_id'] != window['new_control_conn_id']
                             and window['start_ns'] >= cleanup['completed_ns'] and window['end_ns'] - window['start_ns'] >= 5_000_000_000
                             and window['end_ns'] <= window['observed_ns'] <= explicit['observed_ns'],
                             'no-replay observation lacks a fresh generation/control connection and the full 5s window')
                    _g3_need(window.get('old_request_id') == old
                             and window['old_generation'] == cleanup['binding']['oics']['lab-a']['generation']
                             and window['old_control_conn_id'] == cleanup['binding']['control_conn_ids']['lab-a'],
                             'no-replay old session differs from resource binding')
                    for obs in (committed, window, explicit, final):
                        _g3_need(isinstance(obs.get('effects'), dict) and type(obs['effects'].get(old)) is int and obs['effects'][old] == 1,
                                 'old request was replayed or effect evidence is absent')
                    for obs in (explicit, final):
                        _g3_need(obs.get('old_request_id') == old and obs.get('new_request_id') == new
                                 and type(obs['effects'].get(new)) is int and obs['effects'][new] == 1,
                                 'explicit new request must produce one distinct effect')
                    fresh = event('exchange_complete')
                    completed(fresh, 4, 4)
                    _g3_need(fresh.get('request_id') == new and ready_for(fresh['channel_id'])['observed_ns'] >= window['end_ns']
                             and fresh['channel_id'] != fault['channel_id'], 'fresh request reused the old invocation or preceded observation')
                    new_cleanup = phase('new_request_cleanup')
                    fresh_ready = ready_for(fresh['channel_id'])
                    _g3_need(window['new_generation'] == new_cleanup['binding']['oics']['lab-a']['generation']
                             and window['new_control_conn_id'] == new_cleanup['binding']['control_conn_ids']['lab-a']
                             and all(fresh_ready[k] != ready[k] for k in ('consumer_conn_id', 'provider_conn_id')),
                             'new request reused an old session/data connection')
                    finished = event('exchange_fin_observed')
                    _g3_need(_integer(finished.get('finished_ns'), 1) and new_cleanup['start_ns'] == finished['finished_ns']
                             and finished.get('channel_id') == fresh['channel_id']
                             and finished.get('request_id') == new and finished.get('request_bytes') == finished.get('response_bytes') == 4
                             and new_cleanup['origin_event'] == 'exchange_fin_observed'
                             and new_cleanup['start_ns'] <= fresh['observed_ns'] <= explicit['observed_ns'] <= new_cleanup['completed_ns']
                             and final['observed_ns'] >= new_cleanup['completed_ns'], 'final effect count precedes final cleanup')
                    history = final.get('target')
                    _g3_history(history)
                    _g3_need(history['Effects'] == final['effects'] and history['Workers'] == 0, 'final effect/history mismatch')
                    for request in (old, new):
                        matches = [_g3_target(x) for x in history['Exchanges'] if x.get('RequestID') == request]
                        _g3_need(len(matches) == 1 and matches[0]['Closed'], 'accepted request history shows replay or a surviving worker')
            elif case['vector'] == 'TV-G3-05':
                if scenario.startswith('control_'):
                    applied, ended = event('control_applied'), event('control_terminated')
                    marker, restored, final = event('expiry_marker_rejected'), event('control_restored'), event('control_failure_verified')
                    fault_phase, online = phase('control_fault_cleanup'), phase('online_restored_cleanup')
                    channel, generation = applied.get('channel_id'), applied.get('generation')
                    ready = ready_for(channel)
                    _g3_need(_required_ints(applied, ('applied_ns', 'generation', 'license_sampled_ns', 'license_remaining_ms',
                                                    'license_remaining_at_apply_ms', 'target_exchanges_before'), 1)
                             and ready['observed_ns'] <= applied['applied_ns']
                             and applied['license_sampled_ns'] <= applied['applied_ns']
                             and applied['direction'] == ('both' if scenario == 'control_cut' else 'server_to_client')
                             and _g3_conn(applied.get('control_conn_id'), 'access/lab-a'), 'control fault has invalid direction/identity/license sample')
                    remaining = applied['license_remaining_ms'] - (applied['applied_ns'] - applied['license_sampled_ns'] + 999_999) // 1_000_000
                    _g3_need(remaining == applied['license_remaining_at_apply_ms'] and remaining >= 2000
                             and applied['license_remaining_ms'] <= 3000,
                             'control fault injected with less than 2000ms conservative license remaining')
                    _g3_need(ended.get('channel_id') == channel and ended.get('generation') == generation
                             and ended.get('applied_ns') == applied['applied_ns']
                             and _required_ints(ended, ('stream_end_ns', 'offline_refused_ns', 'origin_ns'), 1)
                             and 2_000_000_000 <= ended['stream_end_ns'] - applied['applied_ns'] <= 4_000_000_000
                             and ended['reason'] in ('LicenseExpired', 'AccessOffline')
                             and ended['origin_ns'] == max(ended['stream_end_ns'], ended['offline_refused_ns']) == fault_phase['start_ns']
                             and fault_phase['origin_event'] == 'control_termination_and_offline_observed',
                             'control termination violates L±1s or postponed cleanup origin')
                    refused = event('open_refused')
                    _g3_need(refused.get('status') == 503 and refused.get('code') == 'AccessOffline'
                             and refused.get('refused_ns') == ended['offline_refused_ns'], 'offline did not refuse a new Open')
                    stats = marker.get('oic_stats')
                    _g3_need(marker.get('channel_id') == channel and marker.get('generation') == generation
                             and type(marker.get('target_count')) is int and marker['target_count'] == 0
                             and isinstance(marker.get('marker'), str) and marker['marker']
                             and isinstance(marker.get('write_error'), str) and marker['write_error']
                             and isinstance(stats, dict) and stats.get('generation') == generation and stats.get('access_id') == 'lab-a'
                             and _required_ints(stats, ('epoch', 'generation', 'license_remaining_ms'))
                             and stats['license_remaining_ms'] == 0 and isinstance(stats.get('closed'), dict)
                             and fault_phase['start_ns'] <= marker['observed_ns'] <= fault_phase['completed_ns'],
                             'expired same-generation stream accepted data or lacks OIC evidence')
                    _g3_need(isinstance(applied.get('oic_closed_before'), dict)
                             and all(_integer(v) for v in applied['oic_closed_before'].values())
                             and all(_integer(v) for v in stats['closed'].values()), 'malformed OIC terminal counters')
                    if ended['reason'] == 'LicenseExpired':
                        _g3_need(stats['closed'].get('LicenseExpired', 0) > applied['oic_closed_before'].get('LicenseExpired', 0),
                                 'LicenseExpired lacks an increment in the same generation')
                    before, after = fault_phase['binding'], online['binding']
                    _g3_need(before['oics']['lab-a']['generation'] == generation == restored.get('old_generation')
                             and before['control_conn_ids']['lab-a'] == applied['control_conn_id'] == restored.get('old_control_conn_id')
                             and _integer(restored.get('new_generation'), 1) and restored['new_generation'] > generation
                             and after['oics']['lab-a']['generation'] == restored['new_generation']
                             and after['control_conn_ids']['lab-a'] == restored.get('new_control_conn_id')
                             and restored['new_control_conn_id'] != applied['control_conn_id']
                             and _integer(restored.get('restored_ns'), 1)
                             and fault_phase['completed_ns'] <= restored['restored_ns'] == online['start_ns']
                             and online['origin_event'] == 'control_restored', 'restoration does not match resource generation/connection binding')
                    history = _g3_history(final.get('target'))
                    _g3_need(final.get('channel_id') == channel and final.get('after_expiry_marker') == marker['marker']
                             and type(final.get('target_count')) is int and final['target_count'] == 0
                             and len(history) == applied['target_exchanges_before']
                             and final['target']['Workers'] == 0 and all(x['Markers'].get(marker['marker'], 0) == 0 for x in history)
                             and final['observed_ns'] >= online['completed_ns'], 'control restoration replayed prepare/data or omitted final cleanup')
                    if scenario == 'control_blackhole':
                        blocked = event('blackhole_new_open')
                        _g3_need(_required_ints(blocked, ('status', 'target_exchanges_before', 'target_exchanges_after'))
                                 and blocked['target_exchanges_before'] == blocked['target_exchanges_after'] == applied['target_exchanges_before']
                                 and len(_g3_history(blocked.get('target'))) == blocked['target_exchanges_before']
                                 and ((blocked['status'] == 201 and blocked.get('code') == '' and blocked.get('channel_id'))
                                      or (blocked['status'] == 503 and blocked.get('code') == 'AccessOffline' and not blocked.get('channel_id'))),
                                 'blackhole Open delivered a prepare or has an invalid result')
                        _g3_need(final.get('blocked_channel_id') == blocked.get('channel_id'), 'blackhole channel identity changed')
                        if blocked.get('channel_id'):
                            no_ready(blocked['channel_id'])
                    else:
                        _g3_need(final.get('blocked_channel_id') == '', 'control-cut invented a blocked invocation')
                else:
                    preparing = scenario == 'preparing_revoke'
                    fault = event('preparing_revoked' if preparing else 'revoked')
                    cleanup = phase('revoked_cleanup')
                    channel = fault.get('channel_id')
                    _g3_need(_required_ints(fault, ('trigger_ns', 'returned_ns', 'terminal_observed_ns'), 1)
                             and fault['trigger_ns'] <= fault['returned_ns'] <= fault['terminal_observed_ns']
                             and cleanup['origin_event'] == 'revocation_observed', 'invalid revocation trigger/return/terminal order')
                    terminal = closed(channel, 'Revoked')
                    _g3_need(terminal['observed_ns'] <= cleanup['completed_ns'], 'revoked channel survived cleanup')
                    if preparing:
                        verified = event('preparing_revoke_verified')
                        zero_preparing(verified, channel)
                        ticket(channel, cleanup)
                        _g3_need(_required_ints(fault, ('ready_sent', 'tickets', 'prepare_started_ns'))
                                 and fault['ready_sent'] == 0 and fault['tickets'] == 1
                                 and 0 <= fault['trigger_ns'] - fault['prepare_started_ns'] < 2_000_000_000
                                 and verified.get('consumer_code') == 'Revoked'
                                 and _integer(verified.get('abort_received_ns'), 1)
                                 and fault['trigger_ns'] <= verified['abort_received_ns'] <= fault['trigger_ns'] + 1_000_000_000
                                 and verified['abort_received_ns'] <= cleanup['completed_ns']
                                 and cleanup['start_ns'] == fault['terminal_observed_ns'], 'preparing revocation lacks timely ABORT or moved cleanup origin')
                    else:
                        ready = ready_for(channel)
                        held, verified, refused = event('backpressure_held'), event('revoke_verified'), event('open_refused')
                        _g3_buffer_bindings(held, ready)
                        target = _g3_target(held.get('target'))
                        upload = scenario == 'upload_backpressure_revoke'
                        _g3_need(held.get('channel_id') == channel and held.get('direction') == ('client_to_server' if upload else 'server_to_client')
                                 and held.get('writer_entered') is True and held.get('writer_returned') is False
                                 and _required_ints(held, ('hold_start_ns', 'held_until_ns', 'written_bytes'))
                                 and held['held_until_ns'] - held['hold_start_ns'] >= 250_000_000
                                 and 0 <= held['written_bytes'] < 67108864
                                 and (not upload or held['written_bytes'] >= 32768)
                                 and ready['observed_ns'] <= held['hold_start_ns'] <= held['observed_ns'] <= fault['trigger_ns']
                                 and (target['ReceivedBytes'] == 0 if upload else target['WriterEntered'] and not target['WriterReturned']),
                                 'revocation was not injected under directional backpressure')
                        _g3_need(refused.get('status') == (403 if upload else 401)
                                 and refused.get('code') == ('Denied' if upload else 'Unauthenticated')
                                 and refused.get('refused_ns') == fault.get('new_open_refused_ns')
                                 and cleanup['start_ns'] == max(fault['terminal_observed_ns'], fault['new_open_refused_ns']),
                                 'revoked admission result or cleanup origin is incorrect')
                        _g3_need(verified.get('channel_id') == channel and verified.get('trigger_ns') == fault['trigger_ns']
                                 and verified.get('consumer_code') == 'Revoked' and _integer(verified.get('read_end_ns'), 1)
                                 and _integer(verified.get('received_bytes'))
                                 and fault['trigger_ns'] <= verified['read_end_ns'] <= fault['trigger_ns'] + 1_000_000_000
                                 and verified['observed_ns'] <= cleanup['completed_ns'], 'revocation exceeded T_close or became EOF')
                        target_closed(verified.get('target'), cleanup['completed_ns'])
            elif case['vector'] == 'TV-G3-06':
                mixed = scenario == 'mixed_limit_reuse'
                rounds, used_channels, used_providers = 2 if mixed else 10, set(), set()
                used_requests, previous_targets = set(), None
                _g3_need(len([e for e in events if e['event'] == 'quota_occupied']) == rounds,
                         'missing/extra quota occupancy cycle')
                for cycle in range(rounds):
                    occupied = event('quota_occupied', cycle=cycle)
                    opened, preparing = (8, 8) if mixed else ((8, 0), (0, 8), (4, 4))[cycle % 3]
                    cleanup = phase('quota_round_cleanup' if mixed else 'failure_cycle_cleanup', cycle)
                    stats, channels, prepares = occupied.get('ois_stats'), occupied.get('channel_ids'), occupied.get('prepares')
                    _g3_need(_required_ints(occupied, ('open', 'preparing', 'tickets'))
                             and (occupied['open'], occupied['preparing'], occupied['tickets']) == (opened, preparing, preparing)
                             and _g3_ids(channels) and len(channels) == opened + preparing and not used_channels.intersection(channels)
                             and isinstance(prepares, list) and len(prepares) == preparing
                             and isinstance(stats, dict) and _required_ints(stats.get('channels'), ('open', 'preparing', 'closing', 'closed'))
                             and _required_ints(stats, ('tickets', 'epoch'))
                             and (stats['channels']['open'], stats['channels']['preparing'], stats['channels']['closing'], stats['tickets']) == (opened, preparing, 0, preparing),
                             'quota counts lack actual complete OIS occupancy or reused channel IDs')
                    used_channels.update(channels)
                    prep_ids = [p.get('channel_id') for p in prepares]
                    _g3_need(_g3_ids(prep_ids) and set(prep_ids) <= set(channels), 'preparing channels are duplicate/foreign')
                    opens, before_id = occupied.get('opens'), occupied.get('target_before_id')
                    _g3_need(isinstance(opens, list) and len(opens) == opened and _integer(before_id)
                             and all(isinstance(o, dict) and isinstance(o.get('request_id'), str) and o['request_id']
                                     and _integer(o.get('target_exchange_id'), 1) and o['target_exchange_id'] > before_id for o in opens),
                             'quota lacks measured Open request/target bindings or its pre-cycle target boundary')
                    _g3_need(_g3_ids([o.get('channel_id') for o in opens])
                             and {o['channel_id'] for o in opens} == set(channels) - set(prep_ids)
                             and _g3_ids([o['request_id'] for o in opens])
                             and len({o['target_exchange_id'] for o in opens}) == len(opens)
                             and not used_requests.intersection(o['request_id'] for o in opens),
                             'quota Open channel/request/target identities are duplicated or foreign')
                    used_requests.update(o['request_id'] for o in opens)
                    provider = occupied.get('provider_proxy')
                    _g3_need(isinstance(provider, dict) and isinstance(provider.get('Conns'), list), 'preparing leg snapshot missing')
                    for p in prepares:
                        _g3_need(_required_ints(p, ('opened_ns', 'prepare_expires_in_ms'), 1)
                                 and p['prepare_expires_in_ms'] <= 2000 and p['opened_ns'] <= occupied['observed_ns']
                                 and _g3_conn(p.get('provider_conn_id'), 'access/lab-a')
                                 and p['provider_conn_id'] not in used_providers, 'preparing budget or provider binding invalid')
                        used_providers.add(p['provider_conn_id'])
                        claim_connection(p['provider_conn_id'], ('data', p['channel_id']))
                        matches = [c for c in provider['Conns'] if c.get('ID') == int(p['provider_conn_id'].split('/')[-1])]
                        _g3_need(len(matches) == 1 and matches[0].get('Role') == 'provider'
                                 and matches[0].get('ChannelID') == p['channel_id'] and matches[0].get('AccessID') == 'lab-a'
                                 and matches[0].get('ServerConnected') is True and matches[0].get('Pending') is False
                                 and matches[0].get('ClientClosed') is False and matches[0].get('ServerClosed') is False,
                                 'preparing provider was not actually attached at occupancy')
                        no_ready(p['channel_id'])
                    for channel in set(channels) - set(prep_ids):
                        _g3_need(ready_for(channel)['observed_ns'] <= occupied['observed_ns'], 'quota Open has no prior READY')
                    reason = 'ClosedByClient' if mixed else ('ChannelInterrupted', 'PrepareTimeout', 'Revoked')[cycle % 3]
                    terminals = {channel: closed(channel, reason) for channel in channels}
                    for channel in channels:
                        ticket(channel, cleanup)
                    _g3_need(all(t['observed_ns'] <= cleanup['completed_ns'] for t in terminals.values()), 'quota channel survived cleanup')
                    if mixed:
                        refused = event('quota_rejected', cycle=cycle)
                        _g3_need(refused.get('status') == 429 and refused.get('code') == 'ResourceExhausted'
                                 and refused.get('open') == refused.get('preparing') == 8
                                 and occupied['observed_ns'] <= refused['observed_ns'] <= cleanup['start_ns']
                                 and cleanup['origin_event'] == 'quota_round_complete', '17th Open was not refused before cleanup')
                        final = event('quota_round_verified', cycle=cycle)
                    elif cycle % 3 == 0:
                        first, rest = event('cycle_first_rst', cycle=cycle), event('cycle_rst', count=7, cycle=cycle)
                        resets = [first, *rest]
                        _g3_need({r.get('channel_id') for r in resets} == set(channels)
                                 and cleanup['start_ns'] == first.get('applied_ns') and cleanup['origin_event'] == 'first_reset_applied',
                                 'reset cycle omitted a channel or postponed first-reset origin')
                        for reset in resets:
                            _g3_need(_integer(reset.get('applied_ns'), 1)
                                     and cleanup['start_ns'] <= reset['applied_ns'] <= cleanup['completed_ns']
                                     and reset.get('conn_ref') == ready_for(reset['channel_id'])['consumer_conn_id'], 'cycle RST hit wrong connection/time')
                        final = event('cycle_complete', cycle=cycle)
                    elif cycle % 3 == 1:
                        for p in prepares:
                            _g3_need(terminals[p['channel_id']].get('terminal_observed_ns', 0) - p['opened_ns'] >= p['prepare_expires_in_ms'] * 1_000_000,
                                     'prepare timeout preceded advertised budget')
                        # The first observation for each channel is before cleanup; subsequent verification repeats it.
                        first_times = [min(e['terminal_observed_ns'] for e in events if e['event'] == 'channel_closed'
                                           and e.get('channel_id') == c) for c in channels]
                        _g3_need(all(2_000_000_000 <= first_times[channels.index(p['channel_id'])] - p['opened_ns'] <= 3_000_000_000
                                     for p in prepares), 'prepare timeout is outside the original 2s..3s bound')
                        _g3_need(cleanup['start_ns'] == max(first_times) and cleanup['origin_event'] == 'all_prepare_timeouts_observed',
                                 'prepare cycle cleanup origin was reset')
                        final = event('cycle_complete', cycle=cycle)
                    else:
                        revoked = event('cycle_revoked', cycle=cycle)
                        _g3_need(_required_ints(revoked, ('trigger_ns', 'returned_ns', 'terminal_observed_ns'), 1)
                                 and occupied['observed_ns'] <= revoked['trigger_ns'] <= revoked['returned_ns'] <= revoked['terminal_observed_ns']
                                 and revoked['terminal_observed_ns'] - revoked['trigger_ns'] <= 1_000_000_000
                                 and cleanup['start_ns'] == revoked['terminal_observed_ns'] and cleanup['origin_event'] == 'revocation_observed'
                                 and revoked.get('reason') == reason, 'cycle revoke exceeded T_close or reset its origin')
                        first_times = [min(e['terminal_observed_ns'] for e in events if e['event'] == 'channel_closed'
                                           and e.get('channel_id') == c) for c in channels]
                        _g3_need(max(first_times) == cleanup['start_ns'], 'revocation terminal observations do not match origin')
                        final = event('cycle_complete', cycle=cycle)
                    history = _g3_history(final.get('target'))
                    _g3_need(final['observed_ns'] >= cleanup['completed_ns'] and final['target']['Workers'] == 0
                             and all(x['Closed'] for x in history), 'quota left target workers')
                    targets = {x['ID']: x for x in history}
                    whitelist = {o['target_exchange_id']: o for o in opens}
                    _g3_need(set(whitelist) <= set(targets), 'a quota Open target is missing from final history')
                    for identity, snapshot in targets.items():
                        if identity <= before_id:
                            continue
                        if identity in whitelist:
                            request = whitelist[identity]['request_id']
                            _g3_need(snapshot['RequestID'] == request
                                     and snapshot['WireReceivedBytes'] >= len(('G3 ' + request + '\n').encode('utf-8')),
                                     'quota Open target does not match its measured request binding')
                        else:
                            _g3_need(snapshot['RequestID'] == '' and snapshot['WireReceivedBytes'] == 0,
                                     'unbound/Preparing target received a request header or payload')
                    if previous_targets is not None:
                        _g3_need(before_id == max(previous_targets, default=0)
                                 and all(k in targets and _g3_exact(v, targets[k]) for k, v in previous_targets.items()),
                                 'quota target boundary moved or an already-closed exchange changed')
                    previous_targets = targets
                    if not mixed:
                        _g3_need(final.get('reason') == reason and final.get('open') == opened and final.get('preparing') == preparing,
                                 'cycle completion changed its scenario/counts')
                    if cycle:
                        prior = phase('quota_round_cleanup' if mixed else 'failure_cycle_cleanup', cycle - 1)
                        _g3_need(min(e['observed_ns'] for e in events if e['event'] in ('ready', 'cli_ready')
                                     and e.get('channel_id') in channels) >= prior['completed_ns'] if opened else
                                 min(p['opened_ns'] for p in prepares) >= prior['completed_ns'], 'next cycle began before previous cleanup')
            actual_phases = {(c['phase'], c['cycle']) for c in (resource_summary or {}).get('checks', [])
                             if c['test'] == test and c['stage'] == 'validation'}
            _g3_need(actual_phases == consumed_phases, 'behavior did not account for every resource phase/cycle')
            _g3_need({e.get('channel_id') for e in events if e['event'] in ('ready', 'cli_ready')} == consumed_readies,
                     'unaccounted READY invocation in business history')
            checked.append(test)
        except (ValueError, TypeError, KeyError, AttributeError, IndexError) as exc:
            problems.append(test + ': ' + str(exc))
    return {'schema': G3_BEHAVIOR_SCHEMA, 'result': 'fail' if problems else 'pass', 'checked': checked}, problems


def analyze(case, rows, repeats):
    if not isinstance(case, dict) or case.get('kind') not in KINDS:
        raise ValueError('unsupported case')
    if not isinstance(repeats, int) or repeats < 1:
        raise ValueError('repeats must be a positive integer')
    problems, failed, zero = [], [], []
    facts, steps, results, package_results = {}, {}, {}, {}
    engines, observations, fuzz_started, fuzz_elapsed = {}, {}, set(), {}
    coverage, resource_rows, pending_output, output_problems = None, [], {}, []

    g3_rows, g3_behavior_rows = [], []

    def output_line(step, package, test, text, terminated=True):
        # test2json may split one log line into several Output events. Parse
        # only reconstructed lines, keeping independent package/test streams
        # apart. An incomplete/malformed OBS must never disappear silently.
        if test and re.search(r'fuzz:.*now fuzzing with [1-9][0-9]* workers', text):
            fuzz_started.add((step, test))
        if ZERO in text:
            zero.append(step + ': ' + text.strip())
        if step == 'vectors' and re.search(r'\bOBS(?:\s|$)', text):
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
                        resource_rows.append((test, obs))
                    if str(obs.get('kind', '')).startswith('g3_resources') or obs.get('schema') == G3_SCHEMA:
                        g3_rows.append((package, test, obs))
                    if obs.get('kind') in ('G3-transfer', 'G3-fault', 'G3-effect') or obs.get('schema') == G3_BEHAVIOR_SCHEMA:
                        g3_behavior_rows.append((package, test, obs))
        match = ENGINE.search(text)
        if match:
            versions = engines.setdefault(match.group(1), [])
            if match.group(2) not in versions:
                versions.append(match.group(2))

    def flush_output(key):
        tail = pending_output.pop(key, '')
        if tail:
            output_line(key[0], key[1], key[2], tail, terminated=False)

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
                package_results.setdefault(key, []).append(action)
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
            # New manifests bind the actual Go package, so a same-named test
            # from a different package cannot satisfy a required vector. Older
            # manifests retain their exact historical summary semantics.
            if 'package' in entry:
                if not isinstance(entry['package'], str) or not entry['package']:
                    raise ValueError('invalid vector package')
                got = package_results.get((entry['step'], entry['package'], name), [])
            else:
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
    g3_resources = None
    if 'g3_resources' in case:
        g3_resources, g3_problems = _g3_resource_analysis(case['g3_resources'], g3_rows, package_results, repeats)
        problems.extend(g3_problems)
        vectors['G3-RESOURCE-PHASES'] = {'result': g3_resources['result'], 'checked': ['raw: ' + G3_SCHEMA]}
    g3_behavior = None
    if 'g3_behavior' in case:
        g3_behavior, g3_problems = _g3_behavior_analysis(case['g3_behavior'], g3_behavior_rows, g3_rows,
                                                        g3_resources, package_results, repeats)
        problems.extend(g3_problems)
        vectors['G3-BEHAVIOR'] = {'result': g3_behavior['result'], 'checked': ['raw: ' + G3_BEHAVIOR_SCHEMA]}
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
    if g3_resources is not None:
        summary['g3_resources'] = g3_resources
    if g3_behavior is not None:
        summary['g3_behavior'] = g3_behavior
    return summary
