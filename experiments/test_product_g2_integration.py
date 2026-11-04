"""Collector/provenance counterexamples; no Go execution or host clock changes.

Small child doubles isolate the collector boundary. The real child analyzers'
semantic fixtures live in test_product_g2{,_clock}; a real fixed-inventory
missing-evidence case below additionally prevents Go PASS becoming a shortcut.
"""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import verify
from test_product import CASE, load, rows, resource_records


ROOT = Path(__file__).resolve().parent
main = load('g2_primary_under_test', ROOT/'product/analyze.py')
PACKAGE = 'github.com/wyw-ai/loom-oi/tests/integration'
SOURCE = 'a'*40
ORD = 'TestOrd/sqlite/scenario'
CLOCK = 'TestClock/ready/forward/sqlite'
WORKFLOW = 'wyw-ai/loom-oi/.github/workflows/g2-evidence.yml@refs/heads/evidence/G2-test'


def digest(data):
    return hashlib.sha256(data).hexdigest()


def event(step, action, test, output=None, package=PACKAGE):
    value = dict(Action=action, Package=package, Test=test)
    if output is not None:
        value['Output'] = output
    return dict(step=step, kind='go', event=value)


def output(step, test, observation, package=PACKAGE):
    return event(step, 'output', test, 'OBS '+json.dumps(observation)+'\n', package)


def tiny_spec(clock=False):
    test = CLOCK if clock else ORD
    return dict(schema='clock-boundary-test' if clock else 'ordinary-boundary-test',
                root=test.split('/')[0], groups={test: dict(root=test.split('/')[0],
                resource=True, backend='sqlite', oics=['lab-a', 'lab-b'])})


def tiny_child(clock=False):
    spec = tiny_spec(clock)
    test = CLOCK if clock else ORD

    def check(given, records, results, resource, repeats=1):
        errors = []
        proof = [(p, t, r) for p, t, r in records if r.get('kind') == 'G2']
        expected = dict(kind='G2', schema='g2-behavior-v1', test=test,
                        backend='sqlite', event='proof', value='真实观测')
        if proof != [(PACKAGE, test, expected)]:
            errors.append(test+': original proof absent or mismatched')
        if results.get((PACKAGE, test)) != ['pass'] or results.get((PACKAGE, spec['root'])) != ['pass']:
            errors.append(test+': result absent/skipped/repeated')
        if (resource is None or resource['result'] != 'pass'
                or resource['completed_checks'].get(test) != (20 if clock else 1)):
            errors.append(test+': strict common resource evidence absent')
        return dict(result='fail' if errors else 'pass', groups={test: dict(result='fail' if errors else 'pass')}), errors

    def check_clock(given, records, results, audit, resource, source):
        return check(given, records, results, resource)

    return SimpleNamespace(expected_spec=lambda: copy.deepcopy(spec), analyze_g2=check,
                           analyze_clock=check_clock)


def clock_envelopes():
    audit = [dict(schema='g2-clock-guard-v1', action='start', source_sha=SOURCE, run_id='123',
                  workflow_ref=WORKFLOW, nonce_sha256='b'*64, job='clock', runner_uid=1001, runner_gid=1001)]
    names = ('analyze.py', 'clock_run.py', 'g2_analyze.py', 'g2_clock_analyze.py', 'clock_guard.py')
    prep = dict(schema='g2-clock-preparation-v1', source_sha=SOURCE, run_id='123',
                workflow_ref=WORKFLOW, nonce_sha256='b'*64, runner_environment='github-hosted',
                runner_uid=1001, runner_gid=1001, go_version='go version go1.27.3 linux/amd64',
                go_env={'GOOS': 'linux'}, source_hashes={'experiments/product/'+n: 'c'*64 for n in names},
                binaries={n: 'd'*64 for n in ('integration.test', 'bin/ois', 'bin/oic', 'bin/oi')})
    files = {n: 'e'*64 for n in ('raw.jsonl', 'guard/guard.jsonl', 'guard/state.json', 'guard/finished.json')}
    files['guard/guard.jsonl'] = digest(''.join(json.dumps(a, sort_keys=True)+'\n' for a in audit).encode())
    result = dict(schema='g2-clock-result-v1', source_sha=SOURCE, run_id='123', guard_exit_code=0,
                  recovery_exit_code=0, exit_code=0, verified=True, interrupted=[], errors=[], files=files)
    return [dict(step='clock', kind=k, event=e) for k, e in
            [('clock_preparation', prep), *[('clock_guard', a) for a in audit], ('clock_result', result)]]


def fixture(clock=True):
    case = copy.deepcopy(CASE)
    case['vectors'] = [dict(e, package='p') for e in case['vectors'][1:]]
    case['g2_behavior'] = tiny_spec()
    case['resources'] = dict(schema='b4-resource-v2', vector='TV-B4-03', required_tests={},
                             expected_oics={}, expected_backends={})
    case['criteria'] = dict(contract=['TV-S'], readiness=['TV-G2'], resources=['TV-B4-03'])
    raw = rows()+[dict(step='env', kind='fact', name='subject_version', value=SOURCE)]
    if clock:
        case['g2_clock'] = tiny_spec(True)
        case['steps'].append('clock'); case['go_steps'].append('clock')
        case['criteria']['clock'] = ['TV-CLOCK']
        raw.append(dict(step='clock', kind='start', command=['clock-wrapper']))
    for test, step, count, ident in [(ORD, 'vectors', 1, 'TV-G2')]+([(CLOCK, 'clock', 20, 'TV-CLOCK')] if clock else []):
        root, tail = test.split('/', 1)
        entry = dict(id=ident, step=step, package=PACKAGE, tests=[root], subtests=[tail])
        case['vectors'].append(entry)
        if step == 'vectors':
            case['vectors'].append(dict(entry, id='TV-B4-03'))
        for key, value in [('required_tests', count), ('expected_oics', ['lab-a', 'lab-b']), ('expected_backends', 'sqlite')]:
            case['resources'][key][test] = value
        for i in range(count):
            observations = resource_records(test, profile=test, check=str(i), offset=i*2_000_000_000)
            if i:
                observations = observations[1:]
            raw.extend(output(step, test, r) for r in observations)
        raw.append(output(step, test, dict(kind='G2', schema='g2-behavior-v1', test=test,
                                          backend='sqlite', event='proof', value='真实观测')))
        raw.extend(event(step, 'pass', n) for n in (test, root))
    if clock:
        raw.extend(clock_envelopes())
        raw.append(dict(step='clock', kind='end', exit_code=0, timed_out=False))
    return case, raw


class CollectorTests(unittest.TestCase):
    def analyze(self, case, raw):
        def child(name):
            return tiny_child(name == 'g2_clock_analyze.py')
        with patch.object(main, '_sibling', side_effect=child):
            return main.analyze(case, raw, 1)

    def test_same_raw_results_resources_and_clock_provenance_pass(self):
        case, raw = fixture()
        result = self.analyze(case, raw)
        self.assertEqual(result['verdict'], 'pass', result['problems'])
        self.assertEqual(result['resources']['completed_checks'], {ORD: 1, CLOCK: 20})
        self.assertTrue(all(v == 'pass' for v in result['criteria'].values()))

    def test_chunked_and_interleaved_observations_reach_children_exactly(self):
        case, raw = fixture()
        split = []
        for row in raw:
            if row['kind'] == 'go' and row['event'].get('Output', '').startswith('OBS '):
                text = row['event']['Output']
                for start in range(0, len(text), 17):
                    part = copy.deepcopy(row); part['event']['Output'] = text[start:start+17]
                    split.append(part)
                    split.append(event(row['step'], 'output', 'Unrelated', 'ordinary log\n', 'other/package'))
            else:
                split.append(row)
        self.assertEqual(self.analyze(case, split)['verdict'], 'pass')

    def test_go_pass_cannot_replace_missing_or_wrong_package_proof(self):
        for mode in ('drop', 'wrong_package', 'truncated'):
            with self.subTest(mode=mode):
                case, raw = fixture()
                proof = next(r for r in raw if r['kind'] == 'go' and r['event'].get('Test') == ORD
                             and '"kind": "G2"' in r['event'].get('Output', ''))
                if mode == 'drop': raw.remove(proof)
                elif mode == 'wrong_package': proof['event']['Package'] = 'another/package'
                else: proof['event']['Output'] = proof['event']['Output'][:-1]
                result = self.analyze(case, raw)
                self.assertEqual(result['verdict'], 'fail')
                self.assertEqual(result['criteria']['readiness'], 'fail')
                self.assertEqual(result['g2_clock']['result'], 'fail')

    def test_result_package_and_step_cannot_be_substituted(self):
        for mode in ('package', 'step', 'skip', 'duplicate'):
            with self.subTest(mode=mode):
                case, raw = fixture()
                terminal = next(r for r in raw if r['kind'] == 'go' and r['event'].get('Test') == CLOCK
                                and r['event'].get('Action') == 'pass')
                if mode == 'package': terminal['event']['Package'] = 'other/package'
                elif mode == 'step': terminal['step'] = 'vectors'
                elif mode == 'skip': terminal['event']['Action'] = 'skip'
                else: raw.append(copy.deepcopy(terminal))
                result = self.analyze(case, raw)
                self.assertEqual((result['verdict'], result['criteria']['clock']), ('fail', 'fail'))

    def test_clock_twenty_checks_are_required_without_fd_tolerance(self):
        for mode in ('missing_check', 'fd', 'step', 'package'):
            with self.subTest(mode=mode):
                case, raw = fixture()
                if mode == 'missing_check':
                    raw = [r for r in raw if not (r['kind'] == 'go' and r['event'].get('Test') == CLOCK
                           and '"check_id": "19"' in r['event'].get('Output', ''))]
                else:
                    row = next(r for r in raw if r['kind'] == 'go' and r['event'].get('Test') == CLOCK
                               and '"kind": "resources_sample"' in r['event'].get('Output', ''))
                    if mode == 'fd':
                        obs = json.loads(row['event']['Output'][4:]); obs['ois']['stats']['process']['open_fds'] += 1
                        row['event']['Output'] = 'OBS '+json.dumps(obs)+'\n'
                    elif mode == 'step': row['step'] = 'vectors'
                    else: row['event']['Package'] = 'other/package'
                result = self.analyze(case, raw)
                self.assertEqual(result['verdict'], 'fail')
                self.assertEqual(result['criteria']['resources'], 'fail')

    def test_clock_envelopes_missing_duplicate_reordered_or_cross_source_fail(self):
        for mode in ('missing', 'duplicate', 'order', 'source', 'nonce', 'hash', 'exit', 'restoration'):
            with self.subTest(mode=mode):
                case, raw = fixture()
                prep = next(r for r in raw if r['kind'] == 'clock_preparation')
                guard = next(r for r in raw if r['kind'] == 'clock_guard')
                result = next(r for r in raw if r['kind'] == 'clock_result')
                if mode == 'missing': raw.remove(prep)
                elif mode == 'duplicate': raw.insert(raw.index(prep), copy.deepcopy(prep))
                elif mode == 'order': raw.remove(prep); raw.insert(raw.index(result)+1, prep)
                elif mode == 'source': prep['event']['source_sha'] = 'f'*40
                elif mode == 'nonce': guard['event']['nonce_sha256'] = 'f'*64
                elif mode == 'hash': result['event']['files']['guard/guard.jsonl'] = 'f'*64
                elif mode == 'exit': result['event']['guard_exit_code'] = 1
                else: result['event']['verified'] = False
                summary = self.analyze(case, raw)
                self.assertEqual((summary['verdict'], summary['criteria']['clock']), ('fail', 'fail'))

    def test_clock_envelope_wrong_step_or_mixed_shape_is_rejected(self):
        for mutate in (lambda r: r.update(step='vectors'), lambda r: r.update(Output='fake')):
            case, raw = fixture()
            mutate(next(r for r in raw if r['kind'] == 'clock_preparation'))
            with self.assertRaisesRegex(ValueError, 'clock envelope'):
                self.analyze(case, raw)

    def test_fixed_subject_is_unique_env_fact(self):
        for mode in ('missing', 'duplicate', 'branch', 'step'):
            with self.subTest(mode=mode):
                case, raw = fixture(clock=False)
                fact = next(r for r in raw if r.get('name') == 'subject_version')
                if mode == 'missing': raw.remove(fact)
                elif mode == 'duplicate': raw.append(copy.deepcopy(fact))
                elif mode == 'branch': fact['value'] = 'alpha'
                else: fact['step'] = 'vectors'
                self.assertEqual(self.analyze(case, raw)['verdict'], 'fail')

    def test_preregistration_cannot_remove_resources_or_packages_or_companion(self):
        for mode in ('resources', 'package', 'companion', 'inventory'):
            case, raw = fixture()
            if mode == 'resources': case['resources']['required_tests'][CLOCK] = 19
            elif mode == 'package': del case['vectors'][0]['package']
            elif mode == 'companion': del case['g2_behavior']
            else: case['g2_behavior']['groups'] = {}
            with self.subTest(mode=mode), self.assertRaises(ValueError):
                self.analyze(case, raw)

    def test_real_fixed_ordinary_inventory_without_raw_fails_despite_all_go_pass(self):
        child = main._sibling('g2_analyze.py')
        spec = child.expected_spec()
        case, raw = fixture(clock=False)
        case['g2_behavior'] = spec
        required = {t: g for t, g in spec['groups'].items() if g['resource']}
        case['resources'] = dict(schema='b4-resource-v2', vector='TV-B4-03',
            required_tests={t: 1 for t in required}, expected_oics={t: g['oics'] for t, g in required.items()},
            expected_backends={t: g['backend'] for t, g in required.items()})
        raw = [r for r in rows() if r['kind'] != 'go' or r['event'].get('Action') != 'output'
               or not r['event'].get('Output', '').startswith('OBS')]
        raw.append(dict(step='env', kind='fact', name='subject_version', value=SOURCE))
        names = {name for t, g in spec['groups'].items() for name in [t, g['root'], *g['leaves']]}
        raw.extend(event('vectors', 'pass', name) for name in sorted(names))
        summary = main.analyze(case, raw, 1)
        self.assertEqual(summary['g2_behavior']['result'], 'fail')
        self.assertTrue(any('missing original behavior' in p for p in summary['problems']))

    def test_legacy_manifests_do_not_import_new_modules(self):
        old = load('pre_g2_primary', ROOT/'sources/product-pre-g2/analyze.py')
        with patch.object(main, '_sibling', side_effect=AssertionError('must not import')):
            self.assertEqual(main.analyze(CASE, rows(), 1), old.analyze(CASE, rows(), 1))

    def test_real_clock_child_consumes_full_audit_and_400_recomputed_resource_windows(self):
        # Reuse the independent synthetic wire/clock trajectories, but replace
        # its stub resource result with complete Linux stats passed through the
        # MAIN parser. Ordinary behavior remains a boundary double here.
        from test_product_g2_clock import Fixture
        clock = main._sibling('g2_clock_analyze.py')
        evidence = Fixture()
        case, raw = fixture(clock=False)
        case['g2_clock'] = clock.expected_spec()
        case['steps'].append('clock'); case['go_steps'].append('clock')
        case['criteria']['clock'] = ['TV-CLOCK']
        case['vectors'].append(dict(id='TV-CLOCK', package=PACKAGE, step='clock',
            tests=[clock.ROOT], subtests=sorted(n[len(clock.ROOT)+1:] for p, n in evidence.results if n != clock.ROOT)))
        for test, group in clock.expected_spec()['groups'].items():
            case['resources']['required_tests'][test] = 20
            case['resources']['expected_oics'][test] = group['oics']
            case['resources']['expected_backends'][test] = group['backend']

        def identity(value):
            if isinstance(value, dict):
                if 'workflow_ref' in value: value['workflow_ref'] = WORKFLOW
                for v in value.values(): identity(v)
            elif isinstance(value, (tuple, list)):
                for v in value: identity(v)
        identity(evidence.records); identity(evidence.audit)
        evidence.audit[0]['runner_gid'] = 1001
        # Real systemd aliases produce TWO before/after records but ONE
        # canonical stopped record. Preserve this format through the collector.
        original_services = [dict(unit='chrony.service', canonical='chrony.service', active='active',
                                  enabled='enabled', load='loaded'),
                             dict(unit='chronyd.service', canonical='chrony.service', active='active',
                                  enabled='alias', load='loaded')]
        evidence.audit[0]['ntp_before'] = copy.deepcopy(original_services)
        evidence.audit[-1]['ntp_after'] = copy.deepcopy(original_services)
        stopped = {k: v for k, v in evidence.audit[0].items()
                   if k not in ('action', 'anchor', 'ntp_before', 'runner_uid', 'runner_gid')}
        stopped.update(action='sync_stopped', sample=evidence.sample(1_001_000_000),
                       ntp_stopped=[dict(unit='chrony.service', canonical='chrony.service', active='inactive',
                                         enabled='masked-runtime', load='masked')])
        evidence.audit.insert(1, stopped)
        raw.append(dict(step='clock', kind='start', command=['clock-wrapper']))
        baseline, starts = {}, {}
        for pkg, test, original in evidence.records:
            obs = copy.deepcopy(original)
            if obs['kind'] == 'resources_baseline':
                template = resource_records(test)[0]
                for role in ['ois', 'oics']:
                    wanted = {'ois': obs['ois']} if role == 'ois' else obs['oics']
                    replacement = {'ois': template['ois']} if role == 'ois' else template['oics']
                    for name, snap in replacement.items():
                        snap['pid'] = wanted[name]['pid']; snap['sampled_ns'] = obs['recorded_ns']-1
                obs['ois'], obs['oics'] = template['ois'], template['oics']
                baseline[test] = copy.deepcopy(obs)
            elif obs['kind'] == 'resources_begin':
                obs['origin'] = 'vector_cleanup'; starts[test, obs['check_id']] = obs['started_ns']
            elif obs['kind'] == 'resources':
                start = starts[test, obs['check_id']]
                obs['settled_ms'] = (obs['completed_ns']-start)//1_000_000
            raw.append(output('clock', test, obs, pkg))
            if obs['kind'] == 'resources_begin':
                for i in range(3):
                    at = obs['started_ns']+(10+i*50)*1_000_000
                    sample = copy.deepcopy(baseline[test]); sample.pop('recorded_ns')
                    sample.update(kind='resources_sample', check_id=obs['check_id'], started_ns=at, ended_ns=at+10_000_000)
                    for snap in [sample['ois'], *sample['oics'].values()]: snap['sampled_ns'] = at+1
                    raw.append(output('clock', test, sample))
        raw.extend(event('clock', action, test, package=pkg)
                   for (pkg, test), actions in evidence.results.items() for action in actions)
        envelopes = clock_envelopes()
        result = envelopes[-1]
        result['event']['files']['guard/guard.jsonl'] = digest(''.join(
            json.dumps(a, sort_keys=True)+'\n' for a in evidence.audit).encode())
        raw.extend([envelopes[0], *[dict(step='clock', kind='clock_guard', event=a) for a in evidence.audit], result])
        raw.append(dict(step='clock', kind='end', exit_code=0, timed_out=False))
        with patch.object(main, '_sibling', side_effect=lambda n: clock if n == 'g2_clock_analyze.py' else tiny_child()):
            summary = main.analyze(case, raw, 1)
            self.assertEqual(summary['verdict'], 'pass', summary['problems'][:8])
            self.assertEqual(summary['g2_clock']['required_rounds'], 400)
            self.assertEqual(sum(summary['resources']['completed_checks'].values()), 401)
            # A terminal Go PASS and 399 other valid rounds cannot replace this
            # original process sample. Both resource and clock criteria fail.
            broken = copy.deepcopy(raw)
            sample = next(r for r in broken if r['kind'] == 'go' and r['step'] == 'clock'
                          and '"kind": "resources_sample"' in r['event'].get('Output', ''))
            value = json.loads(sample['event']['Output'][4:]); value['ois']['stats']['process']['open_fds'] += 1
            sample['event']['Output'] = 'OBS '+json.dumps(value)+'\n'
            rejected = main.analyze(case, broken, 1)
            self.assertEqual((rejected['criteria']['resources'], rejected['criteria']['clock']), ('fail', 'fail'))


class ResourceMappingTests(unittest.TestCase):
    def test_explicit_backend_map_only_new_spec_and_old_last_segment_preserved(self):
        spec = dict(schema='b4-resource-v2', vector='TV-B4-03', required_tests={ORD: 1},
                    expected_oics={ORD: ['lab-a', 'lab-b']})
        records = [(ORD, r) for r in resource_records(ORD)]
        results = {('vectors', ORD): ['pass']}
        self.assertEqual(main._resource_analysis(spec, records, results, 1)[0]['result'], 'fail')
        spec['expected_backends'] = {ORD: 'sqlite'}
        self.assertEqual(main._resource_analysis(spec, records, results, 1)[0]['result'], 'pass')
        spec['expected_backends'][ORD] = 'postgres'
        self.assertEqual(main._resource_analysis(spec, records, results, 1)[0]['result'], 'fail')
        spec['expected_backends'] = {}
        with self.assertRaises(ValueError):
            main._resource_analysis(spec, records, results, 1)


class RecordedDependencyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)/'experiments'
        (self.root/'product').mkdir(parents=True)
        self.archive = self.root/'sources'/'frozen'; self.archive.mkdir(parents=True)
        self.run = Path(self.tmp.name)/'run'; self.run.mkdir()
        self.source = ("from pathlib import Path\nANALYSIS_DEPENDENCIES = "+repr(verify.DEPENDENCIES)+
                       "\ndef analyze(*args):\n    return Path(__file__).with_name('g2_analyze.py').read_text()\n").encode()
        (self.archive/'analyze.py').write_bytes(self.source)
        (self.root/'product/analyze.py').write_bytes(self.source+b'\n# current revision\n')
        self.recorded = {'experiments/product/analyze.py': digest(self.source),
                         'experiments/product/g2_analyze.py': digest(b'frozen child')}
        self.write_manifests()

    def write_manifests(self):
        (self.run/'manifest.json').write_text(json.dumps(dict(case={'g2_behavior': {}})), encoding='utf-8')
        (self.run/'source-manifest.json').write_text(json.dumps(self.recorded), encoding='utf-8')

    def recorded_analyzer(self):
        with patch.object(verify, '__file__', str(self.root/'verify.py')):
            return verify.recorded_analyzer(self.run)

    def test_archive_never_borrows_current_dependency(self):
        (self.root/'product/g2_analyze.py').write_bytes(b'frozen child')
        with self.assertRaisesRegex(AssertionError, 'dependency source unavailable'):
            self.recorded_analyzer()
        (self.archive/'g2_analyze.py').write_bytes(b'frozen child')
        self.assertEqual(self.recorded_analyzer()(), 'frozen child')

    def test_same_main_hash_uses_matching_complete_bundle(self):
        (self.root/'product/analyze.py').write_bytes(self.source)
        (self.root/'product/g2_analyze.py').write_bytes(b'new incompatible child')
        (self.archive/'g2_analyze.py').write_bytes(b'frozen child')
        self.assertEqual(self.recorded_analyzer()(), 'frozen child')
        (self.archive/'g2_analyze.py').write_bytes(b'changed frozen child')
        with self.assertRaisesRegex(AssertionError, 'source drift'):
            self.recorded_analyzer()

    def test_missing_recorded_dependency_hash_rejected(self):
        (self.archive/'g2_analyze.py').write_bytes(b'frozen child')
        del self.recorded['experiments/product/g2_analyze.py']; self.write_manifests()
        with self.assertRaisesRegex(AssertionError, 'hash missing'):
            self.recorded_analyzer()

    def test_clock_bundle_requires_all_three_dependencies_before_execution(self):
        (self.run/'manifest.json').write_text(json.dumps(dict(case={'g2_behavior': {}, 'g2_clock': {}})), encoding='utf-8')
        (self.archive/'g2_analyze.py').write_bytes(b'frozen child')
        for name in ('g2_clock_analyze.py', 'clock_guard.py'):
            self.recorded['experiments/product/'+name] = digest(b'fixed dependency')
        (self.run/'source-manifest.json').write_text(json.dumps(self.recorded), encoding='utf-8')
        with self.assertRaises(AssertionError): self.recorded_analyzer()
        for name in ('g2_clock_analyze.py', 'clock_guard.py'):
            (self.archive/name).write_bytes(b'fixed dependency')
        self.assertEqual(self.recorded_analyzer()(), 'frozen child')

    def test_dependency_declaration_is_literal_whitelist_before_execution(self):
        for declaration in ("{'g2_behavior': ('../../outside.py',)}", 'dict()'):
            marker = Path(self.tmp.name)/'must-not-execute'
            source = (f'from pathlib import Path\nPath({str(marker)!r}).write_text("unsafe")\n'
                      f'ANALYSIS_DEPENDENCIES = {declaration}\ndef analyze(*args): return 1\n').encode()
            (self.archive/'analyze.py').write_bytes(source)
            self.recorded['experiments/product/analyze.py'] = digest(source); self.write_manifests()
            with self.assertRaises(AssertionError): self.recorded_analyzer()
            self.assertFalse(marker.exists())


class ProvenanceTests(unittest.TestCase):
    def test_raw_manifest_environment_and_clock_sources_are_the_same_subject(self):
        with tempfile.TemporaryDirectory() as directory:
            run = Path(directory)
            case, raw = fixture()
            prep = next(r['event'] for r in raw if r['kind'] == 'clock_preparation')
            manifest = dict(case=case, subject_version=SOURCE)
            env = dict(subject_version=SOURCE, ci=dict(GITHUB_RUN_ID='123', GITHUB_WORKFLOW_REF=WORKFLOW,
                                                     GITHUB_JOB='clock', RUNNER_ENVIRONMENT='github-hosted'))
            (run/'environment.json').write_text(json.dumps(env), encoding='utf-8')
            recorded = dict(prep['source_hashes'], **{'experiments/manifests/G2.json': 'f'*64})
            (run/'source-manifest.json').write_text(json.dumps(recorded), encoding='utf-8')
            verify.verify_g2_provenance(run, manifest, raw)
            for key, value in [('source_sha', 'b'*40), ('source_hashes', {}), ('run_id', '124'),
                               ('workflow_ref', WORKFLOW+'other'), ('runner_environment', 'self-hosted')]:
                original = prep[key]; prep[key] = value
                with self.subTest(key=key), self.assertRaises(AssertionError):
                    verify.verify_g2_provenance(run, manifest, raw)
                prep[key] = original
            env['subject_version'] = 'b'*40
            (run/'environment.json').write_text(json.dumps(env), encoding='utf-8')
            with self.assertRaisesRegex(AssertionError, 'environment subject'):
                verify.verify_g2_provenance(run, manifest, raw)


if __name__ == '__main__':
    unittest.main()
