import hashlib
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import subprocess
from unittest.mock import patch
import unittest
from verify import verify

ROOT = Path(__file__).resolve().parent


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


analyze = load('test_product_analyze', ROOT / 'product/analyze.py').analyze
runner = load('test_product_run', ROOT / 'product/run.py')

CASE = {
    'kind': 'product-p1', 'go': 'go1.27.3', 'backends': ['sqlite', 'postgres'],
    'engines': {'sqlite': '3.54.1', 'postgres_major': '18'}, 'coverage_min_pct': 95.0,
    'steps': ['unit', 'vectors', 'vet'], 'go_steps': ['unit', 'vectors'],
    'vectors': [
        {'id': 'TV-X', 'step': 'vectors', 'tests': ['TestX'], 'subtests': ['sqlite', 'postgres']},
        {'id': 'TV-S', 'step': 'unit', 'tests': ['TestS'], 'subtests': ['sqlite', 'postgres']},
        {'id': 'TV-S', 'step': 'unit', 'tests': ['TestT'], 'subtests': []},
    ],
    'criteria': {'contract': ['TV-S'], 'loop': ['TV-X']},
}


def go(step, action, test=None, output=None):
    event = {'Action': action, 'Package': 'p'}
    if test:
        event['Test'] = test
    if output is not None:
        event['Output'] = output
    return {'step': step, 'kind': 'go', 'event': event}


def rows(skip=None, drop=None, extra=(), coverage='97.2', pg='18.6 (Debian 18.6-1.pgdg13+1)', goos='linux',
         timed_out=False):
    out = [{'step': 'env', 'kind': 'fact', 'name': 'go_version', 'value': 'go version go1.27.3 linux/amd64'},
           {'step': 'env', 'kind': 'fact', 'name': 'goos', 'value': goos},
           {'step': 'unit', 'kind': 'start', 'command': ['go', 'test']},
           go('unit', 'output', 'TestS/sqlite', '    store_test.go:116: ENGINE sqlite 3.54.1\n'),
           go('unit', 'output', 'TestS/postgres', '    store_test.go:116: ENGINE postgres ' + pg + '\n')]
    for name in ['TestS/sqlite', 'TestS/postgres', 'TestS', 'TestT']:
        if name != drop:
            out.append(go('unit', 'skip' if name == skip else 'pass', name))
    out += [{'step': 'unit', 'kind': 'coverage', 'lines': ['a.go:1: f 100.0%', 'total:\t(statements)\t' + coverage + '%']},
            {'step': 'unit', 'kind': 'end', 'exit_code': 0, 'timed_out': False},
            {'step': 'vectors', 'kind': 'start', 'command': ['go', 'test']},
            go('vectors', 'output', 'TestX/sqlite', '    env_test.go:546: OBS {"kind":"O1","vector":"TV-X"}\n')]
    for name in ['TestX/sqlite', 'TestX/postgres', 'TestX']:
        if name != drop:
            out.append(go('vectors', 'skip' if name == skip else 'pass', name))
    out += list(extra)
    out += [{'step': 'vectors', 'kind': 'end', 'exit_code': 0, 'timed_out': timed_out},
            {'step': 'vet', 'kind': 'start', 'command': ['go', 'vet']},
            {'step': 'vet', 'kind': 'end', 'exit_code': 0, 'timed_out': False}]
    return out


class ProductAnalyzerTests(unittest.TestCase):
    def test_all_pass(self):
        s = analyze(CASE, rows(), 1)
        self.assertEqual((s['status'], s['verdict']), ('observed', 'pass'), s['problems'])
        self.assertEqual(s['criteria'], {'contract': 'pass', 'loop': 'pass'})
        self.assertEqual(s['coverage_total_pct'], 97.2)
        self.assertEqual(s['engines']['postgres'], ['18.6 (Debian 18.6-1.pgdg13+1)'])
        self.assertEqual(s['observations'], {'O1': [{'kind': 'O1', 'vector': 'TV-X'}]})
        self.assertEqual(len(s['vectors']['TV-S']['checked']), 4)

    def test_skipped_or_missing_required_result_fails(self):
        s = analyze(CASE, rows(skip='TestX/postgres'), 1)
        self.assertEqual((s['verdict'], s['vectors']['TV-X']['result'], s['criteria']['loop']), ('fail', 'fail', 'fail'))
        s = analyze(CASE, rows(drop='TestT'), 1)
        self.assertEqual((s['verdict'], s['vectors']['TV-S']['result']), ('fail', 'missing'))

    def test_repeats_must_match(self):
        self.assertEqual(analyze(CASE, rows(), 2)['verdict'], 'fail')

    def test_zero_tolerance_line_fails(self):
        line = go('vectors', 'output', 'TestX/sqlite', '    v.go:1: ZERO-TOLERANCE: target received "x"\n')
        s = analyze(CASE, rows(extra=[line]), 1)
        self.assertEqual(s['verdict'], 'fail')
        self.assertEqual(len(s['zero_tolerance_lines']), 1)

    def test_quality_and_environment_fail(self):
        for kwargs in [{'coverage': '94.9'}, {'pg': '17.5'}, {'goos': 'windows'}]:
            with self.subTest(kwargs=kwargs):
                self.assertEqual(analyze(CASE, rows(**kwargs), 1)['verdict'], 'fail')
        r = rows()
        r[-1]['exit_code'] = 1
        self.assertEqual(analyze(CASE, r, 1)['verdict'], 'fail')

    def test_incomplete_steps_are_inconclusive(self):
        s = analyze(CASE, rows(timed_out=True), 1)
        self.assertEqual((s['status'], s['verdict']), ('inconclusive', 'fail'))
        r = [x for x in rows() if x['step'] != 'vet']
        self.assertEqual(analyze(CASE, r, 1)['status'], 'inconclusive')
        r = rows()
        r.insert(3, {'step': 'unit', 'kind': 'start', 'command': []})
        self.assertEqual(analyze(CASE, r, 1)['status'], 'inconclusive')

    def test_fuzz_requires_registered_target_and_active_time_budget(self):
        case = dict(CASE, steps=CASE['steps'] + ['fuzz-frame'], go_steps=CASE['go_steps'] + ['fuzz-frame'],
                    fuzz={'fuzz-frame': 'FuzzDecodeFrame'}, fuzz_time='60s')
        def fuzz_rows(target='FuzzDecodeFrame', action='pass', active=True, elapsed=60.01):
            events = [go('fuzz-frame', 'run', target)]
            if active:
                events.append(go('fuzz-frame', 'output', target, 'fuzz: elapsed: 0s, now fuzzing with 2 workers'))
            last = go('fuzz-frame', action, target)
            last['event']['Elapsed'] = elapsed
            return rows() + [{'step': 'fuzz-frame', 'kind': 'start'}] + events + [last,
                {'step': 'fuzz-frame', 'kind': 'end', 'exit_code': 0, 'timed_out': False}]
        self.assertEqual(analyze(case, fuzz_rows(), 1)['verdict'], 'pass')
        for kwargs in [{'target': None}, {'target': 'FuzzOther'}, {'action': 'skip'},
                       {'active': False}, {'elapsed': 0.01}]:
            with self.subTest(kwargs=kwargs):
                summary = analyze(case, fuzz_rows(**kwargs), 1)
                self.assertEqual(summary['verdict'], 'fail')
                self.assertTrue(any('did not complete active fuzzing' in p for p in summary['problems']))

    def test_rejects_unknown_input(self):
        with self.assertRaises(ValueError):
            analyze({'kind': 'probe'}, [], 1)
        with self.assertRaises(ValueError):
            analyze(CASE, [{'step': 'unit', 'kind': 'bogus'}], 1)



def resource_case():
    case = copy.deepcopy(CASE)
    case['vectors'][0]['id'] = 'TV-B4-03'
    case['criteria']['loop'] = ['TV-B4-03']
    case['resources'] = {'schema': 'b4-resource-v2', 'vector': 'TV-B4-03',
                         'required_tests': {'TestX/sqlite': 1},
                         'expected_oics': {'TestX/sqlite': ['lab-a', 'lab-b']}}
    return case


def resource_records(test='TestX/sqlite', profile='profile-1', check='check-1', offset=0):
    proc = {'goroutines': 30, 'open_fds': 20, 'rss_bytes': 1024, 'heap_bytes': 512, 'cpu_ms': 0}
    ois = {'channels': dict.fromkeys(('preparing', 'open', 'closing', 'closed'), 0),
           'tickets': 0, 'accesses_online': 2, 'relay_bytes': 0, 'max_buffered_bytes': 0,
           'store_healthy': True, 'epoch': 1, 'process': copy.deepcopy(proc)}
    def oic(access):
        return {'access_id': access, 'connected': True, 'registered': True, 'epoch': 1, 'generation': 1,
                'license_remaining_ms': 500, 'channels': 0, 'relay_bytes': 0, 'max_buffered_bytes': 0,
                'closed': {}, 'process': copy.deepcopy(proc)}
    common = {'schema': 'b4-resource-v2', 'test': test, 'backend': 'sqlite', 'profile_id': profile}
    def snapshot(at):
        return {'ois': {'pid': 100, 'sampled_ns': at, 'stats': copy.deepcopy(ois)},
                'oics': {a: {'pid': 200+i, 'sampled_ns': at, 'stats': oic(a)}
                         for i, a in enumerate(('lab-a', 'lab-b'))}}
    records = [dict(common, kind='resources_baseline', recorded_ns=500_000_000+offset,
                    **snapshot(400_000_000+offset)),
               dict(common, kind='resources_begin', check_id=check, started_ns=1_000_000_000+offset,
                    deadline_ns=3_000_000_000+offset, origin='vector_cleanup')]
    for i in range(3):
        start = 1_100_000_000 + i*100_000_000 + offset
        records.append(dict(common, kind='resources_sample', check_id=check, started_ns=start,
                            ended_ns=start+50_000_000, **snapshot(start+25_000_000)))
    records.append(dict(common, kind='resources', check_id=check, completed_ns=1_400_000_000+offset,
                        settled_ms=400, samples=3))
    return records


def resource_events(records, event_test=None):
    return [go('vectors', 'output', event_test or r['test'], 'OBS ' + json.dumps(r) + '\n') for r in records]


class ProductResourceTests(unittest.TestCase):
    def summary(self, records=None, case=None, extra=(), event_test=None):
        return analyze(case or resource_case(), rows(extra=resource_events(
            resource_records() if records is None else records, event_test) + list(extra)), 1)

    def assert_bad(self, records, case=None, extra=(), event_test=None):
        summary = self.summary(records, case, extra, event_test)
        self.assertEqual((summary['verdict'], summary['resources']['result'],
                          summary['vectors']['TV-B4-03']['result'], summary['criteria']['loop']),
                         ('fail', 'fail', 'fail', 'fail'), summary)
        self.assertTrue(summary['problems'])

    def test_recomputes_valid_records(self):
        s = self.summary()
        self.assertEqual((s['verdict'], s['resources']['result']), ('pass', 'pass'), s['problems'])
        self.assertEqual(s['resources']['completed_checks'], {'TestX/sqlite': 1})
        check = s['resources']['checks'][0]
        self.assertEqual((check['samples'], check['consecutive'], check['completed_ns']), (3, 3, 1_400_000_000))

    def test_go_output_chunks_preserve_complete_resource_records(self):
        original = rows(extra=resource_events(resource_records()))
        expected = analyze(resource_case(), original, 1)
        # Real test2json splits long test logs. The full resource snapshots
        # exceed its chunk size; parsing each event alone loses the baseline.
        for size in (1024, 17):
            split = []
            for row in original:
                if row['kind'] != 'go' or row['event'].get('Action') != 'output':
                    split.append(row)
                    continue
                output = row['event']['Output']
                for offset in range(0, len(output), size):
                    part = copy.deepcopy(row)
                    part['event']['Output'] = output[offset:offset+size]
                    split.append(part)
            self.assertEqual(analyze(resource_case(), split, 1), expected)

    def test_interleaved_output_is_scoped_to_step_package_and_test(self):
        obs = 'OBS {"kind":"O1","note":"分片记录"}\n'
        extra = [go('vectors', 'output', 'TestX/sqlite', obs[:3]),
                 go('vectors', 'output', 'Other', 'unrelated output\n'),
                 go('unit', 'output', 'TestX/sqlite', 'unit output\n')]
        package = go('vectors', 'output', 'TestX/sqlite', 'package output\n')
        package['event']['Package'] = 'other-package'
        extra.extend([package, go('vectors', 'output', 'TestX/sqlite', obs[3:])])
        summary = self.summary(extra=extra)
        self.assertEqual(summary['verdict'], 'pass', summary['problems'])
        self.assertEqual(summary['observations']['O1'][-1]['note'], '分片记录')

    def test_multiple_lines_and_split_zero_tolerance_marker(self):
        extra = [go('vectors', 'output', 'Other',
                    'OBS {"kind":"O4","note":"甲"}\nOBS {"kind":"O4","note":"乙"}\nZERO-TOL'),
                 go('vectors', 'output', 'Other', 'ERANCE: unexpected forwarding\n')]
        summary = self.summary(extra=extra)
        self.assertEqual([o['note'] for o in summary['observations']['O4']], ['甲', '乙'])
        self.assertEqual(summary['verdict'], 'fail')
        self.assertEqual(summary['zero_tolerance_lines'], ['vectors: ZERO-TOLERANCE: unexpected forwarding'])

    def test_partial_or_malformed_observations_cannot_silently_pass(self):
        for text in ['OBS {"kind":"resources"}', 'OBS {"kind":\n', 'OBS {bad}\n',
                     'OBS', '    env_test.go:1: OBS', 'OBS\n']:
            with self.subTest(text=text):
                summary = self.summary(extra=[go('vectors', 'output', 'TestX/sqlite', text)])
                self.assertEqual(summary['verdict'], 'fail')
                self.assertTrue(any('OBS' in p for p in summary['problems']))
        # A fragment from another test must not repair the incomplete line.
        summary = self.summary(extra=[go('vectors', 'output', 'TestX/sqlite', 'OBS {"kind":'),
                                      go('vectors', 'output', 'Other', '"O1"}\n')])
        self.assertEqual(summary['verdict'], 'fail')

    def test_test_end_does_not_join_output_from_a_later_execution(self):
        extra = [go('vectors', 'output', 'Other', 'OBS {"kind":'),
                 go('vectors', 'pass', 'Other'),
                 go('vectors', 'output', 'Other', '"O1"}\n')]
        summary = self.summary(extra=extra)
        self.assertEqual(summary['verdict'], 'fail')

    def test_required_test_and_check_count_cannot_be_omitted(self):
        self.assert_bad([])
        case = resource_case()
        case['resources']['required_tests']['TestX/postgres'] = 1
        case['resources']['expected_oics']['TestX/postgres'] = ['lab-a', 'lab-b']
        self.assert_bad(resource_records(), case)
        case = resource_case()
        case['resources']['required_tests']['TestX/sqlite'] = 2
        self.assert_bad(resource_records(), case)
        r = resource_records() + resource_records(check='check-2', offset=2_000_000_000)[1:]
        self.assertEqual(self.summary(r, case)['verdict'], 'pass')

    def test_each_record_and_each_role_snapshot_is_required(self):
        for index in range(6):
            with self.subTest(drop=index):
                r = resource_records()
                del r[index]
                self.assert_bad(r)
        for index in (0, 2):
            for key in ('ois', 'oics'):
                with self.subTest(record=index, key=key):
                    r = resource_records()
                    del r[index][key]
                    self.assert_bad(r)
        r = resource_records()
        del r[2]['oics']['lab-b']
        self.assert_bad(r)
        # Omitting a role from every snapshot cannot hide it from preregistration.
        r = resource_records()
        for x in r:
            if 'oics' in x:
                del x['oics']['lab-b']
        self.assert_bad(r)

    def test_missing_stats_fields_and_invalid_types_are_not_zero_defaults(self):
        for index in (0, 2):
            for role in ('ois', 'lab-a'):
                base = resource_records()[index]
                snapshot = base['ois'] if role == 'ois' else base['oics'][role]
                for field in snapshot['stats']:
                    with self.subTest(record=index, role=role, field=field):
                        r = resource_records()
                        s = r[index]['ois'] if role == 'ois' else r[index]['oics'][role]
                        del s['stats'][field]
                        self.assert_bad(r)
                for field in snapshot['stats']['process']:
                    for bad in (None, True, '0', -1, 1 << 63):
                        with self.subTest(record=index, role=role, process=field, bad=bad):
                            r = resource_records()
                            s = r[index]['ois'] if role == 'ois' else r[index]['oics'][role]
                            s['stats']['process'][field] = bad
                            self.assert_bad(r)
        for key in ('pid', 'sampled_ns'):
            r = resource_records()
            del r[2]['ois'][key]
            self.assert_bad(r)

    def test_fd_equality_and_both_goroutine_bounds(self):
        for role in ('ois', 'lab-a'):
            for field, value in (('open_fds', 19), ('open_fds', 21), ('goroutines', 19), ('goroutines', 41)):
                with self.subTest(role=role, field=field, value=value):
                    r = resource_records()
                    for x in r[2:5]:
                        s = x['ois'] if role == 'ois' else x['oics'][role]
                        s['stats']['process'][field] = value
                    self.assert_bad(r)
        for value in (20, 40):
            r = resource_records()
            for x in r[2:5]:
                x['ois']['stats']['process']['goroutines'] = value
            self.assertEqual(self.summary(r)['verdict'], 'pass')

    def test_three_consecutive_samples_not_three_total(self):
        r = resource_records()
        r[3]['ois']['stats']['tickets'] = 1
        self.assert_bad(r)
        # An initial valid-but-not-settled sample is allowed; every attempt is counted.
        r = resource_records()
        bad = copy.deepcopy(r[2])
        bad['started_ns'], bad['ended_ns'] = 1_010_000_000, 1_060_000_000
        bad['ois']['sampled_ns'] = 1_030_000_000
        for s in bad['oics'].values():
            s['sampled_ns'] = 1_030_000_000
        bad['ois']['stats']['tickets'] = 1
        r.insert(2, bad)
        r[-1]['samples'] = 4
        self.assertEqual(self.summary(r)['verdict'], 'pass')
        r[-1]['samples'] = 3
        self.assert_bad(r)

    def test_duplicate_instant_snapshots_cannot_count_as_three_measurements(self):
        r = resource_records()
        sample = r[2]
        sample['started_ns'] = sample['ended_ns'] = sample['ois']['sampled_ns']
        r[2:5] = [copy.deepcopy(sample) for _ in range(3)]
        self.assert_bad(r)
        # A copied positive-duration interval also cannot be counted twice.
        r = resource_records()
        r[2:5] = [copy.deepcopy(r[2]) for _ in range(3)]
        self.assert_bad(r)

    def test_topology_and_channel_cleanup_are_independent_of_resource_counts(self):
        for role, field, value in [('ois', 'tickets', 1), ('ois', 'accesses_online', 1),
                                   ('lab-a', 'channels', 1), ('lab-a', 'connected', False),
                                   ('lab-a', 'registered', False), ('lab-a', 'epoch', 0), ('lab-a', 'generation', 0)]:
            with self.subTest(role=role, field=field):
                r = resource_records()
                for x in r[2:5]:
                    s = x['ois'] if role == 'ois' else x['oics'][role]
                    s['stats'][field] = value
                self.assert_bad(r)
        for channel in ('preparing', 'open', 'closing'):
            r = resource_records()
            r[3]['ois']['stats']['channels'][channel] = 1
            self.assert_bad(r)

    def test_fake_ois_and_restart_do_not_equate_access_count_or_pid(self):
        r = resource_records()
        for x in r:
            if 'ois' in x:
                x['ois']['stats']['accesses_online'] = 0
        for s in r[0]['oics'].values():
            s['stats']['registered'], s['stats']['epoch'], s['stats']['generation'] = False, 0, 0
        for x in r[2:5]:
            x['ois']['pid'] = 900
        self.assertEqual(self.summary(r)['verdict'], 'pass')
        r[3]['ois']['pid'] = 901
        self.assert_bad(r)
        r = resource_records()
        r[3]['oics']['lab-a']['pid'] += 1
        self.assert_bad(r)

    def test_fixed_deadline_record_order_and_raw_sampling_times(self):
        mutations = [lambda r: r[1].update(deadline_ns=3_000_000_001),
                     lambda r: r[1].update(origin='after_cleanup'),
                     lambda r: r[0].update(recorded_ns=r[1]['started_ns']),
                     lambda r: r[0]['ois'].update(sampled_ns=r[1]['started_ns']),
                     lambda r: r[2].update(started_ns=r[1]['started_ns']-1),
                     lambda r: r[2].update(ended_ns=r[1]['deadline_ns']+1),
                     lambda r: r[2]['ois'].update(sampled_ns=r[2]['started_ns']-1),
                     lambda r: r[3].update(started_ns=r[2]['ended_ns']-1),
                     lambda r: r[-1].update(completed_ns=r[1]['deadline_ns']+1),
                     lambda r: r[-1].update(completed_ns=r[-2]['ended_ns']-1),
                     lambda r: r[-1].update(settled_ms=399)]
        for index, mutate in enumerate(mutations):
            with self.subTest(index=index):
                r = resource_records()
                mutate(r)
                self.assert_bad(r)
        r = resource_records()
        r[0], r[1] = r[1], r[0]
        self.assert_bad(r)
        r = resource_records() + resource_records(profile='profile-2', check='check-2')
        self.assert_bad(r)

    def test_profiles_check_ids_provenance_and_duplicate_records(self):
        mutations = [lambda r: r[2].update(profile_id='missing'),
                     lambda r: r[2].update(check_id='missing'),
                     lambda r: r[2].update(backend='postgres'),
                     lambda r: r[2].update(schema='unknown'),
                     lambda r: r[2].update(kind='resources_unknown'),
                     lambda r: r[2]['oics']['lab-a']['stats'].update(access_id='lab-b')]
        for mutate in mutations:
            r = resource_records()
            mutate(r)
            self.assert_bad(r)
        self.assert_bad(resource_records(), event_test='TestX/postgres')
        for index in (0, 1, 5):
            r = resource_records()
            r.insert(index, copy.deepcopy(r[index]))
            self.assert_bad(r)
        r = resource_records()
        r.append(copy.deepcopy(r[2]))
        self.assert_bad(r)

    def test_extra_emitted_g4_records_are_checked(self):
        extra = resource_records('TestExtra/sqlite')
        events = [go('vectors', 'pass', 'TestExtra/sqlite')]
        self.assertEqual(self.summary(resource_records()+extra, extra=events)['verdict'], 'pass')
        extra[-1]['samples'] = 100
        self.assert_bad(resource_records()+extra, extra=events)
        self.assert_bad(resource_records()+resource_records('TestExtra/sqlite'))

    def test_unused_reference_profiles_and_no_oic_topology_are_valid(self):
        r = resource_records()
        extra = resource_records('TestMigration/sqlite-to-postgres')
        extra.insert(0, dict(copy.deepcopy(extra[0]), profile_id='reference'))
        self.assertEqual(self.summary(r+extra, extra=[go('vectors', 'pass', 'TestMigration/sqlite-to-postgres')])['verdict'], 'pass')
        r = resource_records()
        for x in r:
            if 'oics' in x:
                x['oics'] = {}
                x['ois']['stats']['accesses_online'] = 0
        case = resource_case()
        case['resources']['expected_oics']['TestX/sqlite'] = []
        self.assertEqual(self.summary(r, case)['verdict'], 'pass')

    def test_new_profile_id_cannot_absorb_a_leak_before_or_after_first_check(self):
        later = resource_records(profile='after-leak', check='check-2', offset=2_000_000_000)
        for x in later:
            if 'ois' in x:
                x['ois']['stats']['process']['open_fds'] += 100
                x['ois']['stats']['process']['goroutines'] += 100
        case = resource_case()
        case['resources']['required_tests']['TestX/sqlite'] = 2
        self.assert_bad(resource_records()+later, case)
        # Even before the first check, a replacement profile cannot hide drift.
        r = resource_records()
        replacement = copy.deepcopy(r[0])
        replacement.update(profile_id='before-check-leak', recorded_ns=900_000_000)
        replacement['ois']['sampled_ns'] = 800_000_000
        replacement['ois']['stats']['process']['open_fds'] += 100
        r.insert(1, replacement)
        for x in r[2:]:
            x['profile_id'] = replacement['profile_id']
            if 'ois' in x:
                x['ois']['stats']['process']['open_fds'] += 100
        self.assert_bad(r)

    def test_fake_ois_extension_preserves_the_original_ois_snapshot(self):
        test = 'TestTVB1_17_PrepareOutOfOrder/sqlite'
        case = resource_case()
        case['resources']['required_tests'] = {test: 1}
        case['resources']['expected_oics'] = {test: ['lab-f']}
        r = resource_records(test)
        for x in r:
            if 'ois' in x:
                x['ois']['stats']['accesses_online'] = 0
                x['oics'] = {'lab-f': x['oics']['lab-a']}
                x['oics']['lab-f']['stats']['access_id'] = 'lab-f'
        initial = dict(copy.deepcopy(r[0]), profile_id='initial-no-oic', oics={})
        r.insert(0, initial)
        passed = [go('vectors', 'pass', test)]
        self.assertEqual(self.summary(r, case, passed)['verdict'], 'pass')
        r[1]['ois']['stats']['process']['open_fds'] += 1
        self.assert_bad(r, case, passed)

    def test_migration_references_must_reuse_frozen_role_snapshots(self):
        test = 'TestMigration/sqlite-to-postgres'
        original = resource_records(test)
        reference = dict(copy.deepcopy(original[0]), profile_id='reference', backend='postgres')
        reference['ois']['pid'] += 10
        for snap in reference['oics'].values():
            snap['pid'] += 10
        derived = dict(copy.deepcopy(original[0]), profile_id='target', backend='postgres',
                       ois=copy.deepcopy(reference['ois']), recorded_ns=900_000_000)
        for x in original[1:]:
            x.update(profile_id='target', backend='postgres')
            if 'ois' in x:
                x['ois']['pid'] = reference['ois']['pid']
        r = [original[0], reference, derived] + original[1:]
        passed = [go('vectors', 'pass', test)]
        self.assertEqual(self.summary(resource_records()+r, extra=passed)['verdict'], 'pass')
        for role in ('ois', 'lab-a'):
            bad = copy.deepcopy(r)
            snap = bad[2]['ois'] if role == 'ois' else bad[2]['oics'][role]
            snap['stats']['process']['open_fds'] += 100
            self.assert_bad(resource_records()+bad, extra=passed)
        # A new PID first introduced after a completed check cannot become a reference.
        late = copy.deepcopy(derived)
        late.update(profile_id='late-reference', recorded_ns=1_500_000_000)
        late['ois']['pid'] += 1000
        self.assert_bad(resource_records()+r+[late], extra=passed)

    def test_invalid_preregistration_cannot_relax_rules(self):
        for key, value in [('schema', 'old'), ('vector', 'TV-X'), ('required_tests', {}),
                           ('required_tests', {'TestX/sqlite': True}), ('expected_oics', {})]:
            with self.subTest(key=key):
                case = resource_case()
                case['resources'][key] = value
                with self.assertRaises(ValueError):
                    self.summary(case=case)


class ProductRunVerifyTests(unittest.TestCase):
    def make(self, path, mutate=None):
        manifest = {'id': 'G1-T', 'case': CASE, 'repeats': 1, 'evidence_level': 'Implementation'}
        raw = rows()
        summary = analyze(CASE, raw, 1)
        if mutate:
            mutate(summary)
        files = {
            'manifest.json': json.dumps(manifest),
            'source-manifest.json': json.dumps({'experiments/product/analyze.py': hashlib.sha256(
                (ROOT / 'product/analyze.py').read_bytes()).hexdigest()}),
            'environment.json': '{}', 'execution.json': json.dumps({'exit_code': 0, 'timed_out': False}),
            'raw.jsonl': ''.join(json.dumps(r) + '\n' for r in raw), 'stderr.log': '',
            'summary.json': json.dumps(summary), 'report.md': '# G1-T\n'}
        path.mkdir()
        for name, text in files.items():
            (path / name).write_text(text, encoding='utf-8')
        (path / 'SHA256SUMS').write_text(''.join(hashlib.sha256((path / n).read_bytes()).hexdigest() + '  ' + n + '\n'
                                                 for n in sorted(files)), encoding='utf-8')
        return path

    def test_product_run_recomputes(self):
        with tempfile.TemporaryDirectory() as t:
            self.assertEqual(verify(self.make(Path(t) / 'run'))['recomputation'], 'observed')

    def test_forged_verdict_rejected(self):
        with tempfile.TemporaryDirectory() as t:
            run = self.make(Path(t) / 'run', lambda s: s.update(verdict='fail'))
            with self.assertRaises(AssertionError):
                verify(run)

    def test_unknown_analyzer_rejected(self):
        with tempfile.TemporaryDirectory() as t:
            run = self.make(Path(t) / 'run')
            (run / 'source-manifest.json').write_text('{"experiments/product/analyze.py": "00"}', encoding='utf-8')
            lines = [l for l in (run / 'SHA256SUMS').read_text(encoding='utf-8').splitlines() if 'source-manifest' not in l]
            digest = hashlib.sha256((run / 'source-manifest.json').read_bytes()).hexdigest()
            (run / 'SHA256SUMS').write_text('\n'.join(lines + [digest + '  source-manifest.json']) + '\n', encoding='utf-8')
            with self.assertRaises(AssertionError):
                verify(run)


class ProductSourceTests(unittest.TestCase):
    def test_untracked_and_ignored_source_cannot_be_bound_to_head(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            def git(*args):
                return subprocess.run(['git', *args], cwd=root, capture_output=True, check=True)
            git('init')
            for name, body in {'internal/a.go': 'package a\n', '.gitignore': 'internal/ignored.go\n',
                               'experiments/manifests/M.json': '{}\n'}.items():
                p = root / name
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_text(body, encoding='utf-8')
            git('add', '.')
            git('-c', 'user.name=Evidence test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'baseline')
            with patch.object(runner, 'ROOT', root):
                self.assertFalse(runner.workspace_dirty('experiments/manifests/M.json'))
                # Output artifacts and import caches do not change built sources.
                for name in ['evidence/raw.jsonl', 'experiments/product/__pycache__/run.cpython-314.pyc']:
                    p = root / name
                    p.parent.mkdir(parents=True, exist_ok=True)
                    p.write_bytes(b'generated')
                self.assertFalse(runner.workspace_dirty('experiments/manifests/M.json'))
                for name in ['internal/probe_test.go', 'internal/ignored.go', 'internal/data.json',
                             'experiments/manifests/new.json']:
                    with self.subTest(name=name):
                        p = root / name
                        p.write_bytes(b'uncommitted')
                        self.assertTrue(runner.workspace_dirty('experiments/manifests/new.json'))
                        p.unlink()
                (root / 'internal/a.go').write_bytes(b'package changed')
                self.assertTrue(runner.workspace_dirty('experiments/manifests/M.json'))


class PreregistrationTests(unittest.TestCase):
    def test_resource_rerun_keeps_the_entire_original_g1_contract(self):
        old = json.loads((ROOT / 'manifests/G1-P1.json').read_text(encoding='utf-8'))
        new = json.loads((ROOT / 'manifests/G1-P1-R2.json').read_text(encoding='utf-8'))
        self.assertEqual((new['id'], new['gate'], new['evidence_level'], new['repeats']),
                         ('G1-P1-R2', 'G1', 'Implementation', 1))
        self.assertEqual({k: v for k, v in new['case'].items() if k != 'resources'}, old['case'])
        self.assertEqual(runner.step_table(Path('.'), new['case'])['vectors'][1]['OI_IT_LOAD'], '1')
        required = new['case']['resources']['required_tests']
        self.assertEqual((len(required), sum(required.values())), (81, 97))
        resource_ids = ({'TV-B1-' + f'{i:02}' for i in range(1, 19)}
                        | {'TV-B2-' + f'{i:02}' for i in range(1, 15)}
                        | {'TV-B3-' + f'{i:02}' for i in range(4, 11)}
                        | {'TV-B4-03', 'TV-B4-04', 'TV-B4-06'})
        expected_tests = {test + '/' + backend for entry in old['case']['vectors']
                          if entry['id'] in resource_ids for test in entry['tests'] for backend in entry['subtests']}
        self.assertEqual(set(required), expected_tests)
        expected = new['case']['resources']['expected_oics']
        self.assertEqual(set(required), set(expected))
        source = ''.join(p.read_text(encoding='utf-8') for p in (ROOT.parent / 'tests/integration').glob('*_test.go'))
        for test, minimum in required.items():
            self.assertIn('func ' + test.split('/')[0] + '(', source)
            self.assertEqual(minimum, 4 if test.startswith('TestTVB4_03_') else 6 if test.startswith('TestTVB4_06_') else 1)
            roles = ['lab-f'] if test.startswith('TestTVB1_17_') else (
                [] if test.startswith(('TestTVB3_06_', 'TestTVB3_07_', 'TestTVB3_08_')) else ['lab-a', 'lab-b'])
            self.assertEqual(expected[test], roles)
        self.assertEqual({t for t in required if t.startswith('TestTVB1_17_')},
                         {'TestTVB1_17_PrepareOutOfOrder/sqlite'})
        self.assertEqual({t for t in required if t.startswith('TestTVB3_05_')},
                         {'TestTVB3_05_PostgresDown/postgres'})
        self.assertEqual({t for t in required if t.startswith('TestTVB3_08_')},
                         {'TestTVB3_08_SQLiteSettings/sqlite'})

    def test_g1_manifest_is_consistent(self):
        m = json.loads((ROOT / 'manifests/G1-P1.json').read_text(encoding='utf-8'))
        case = m['case']
        self.assertEqual((m['gate'], m['evidence_level'], m['repeats']), ('G1', 'Implementation', 1))
        table = runner.step_table(Path('.'), case)
        self.assertLessEqual(set(case['steps']), set(table))
        self.assertLessEqual(set(case['go_steps']), set(case['steps']))
        ids = {v['id'] for v in case['vectors']}
        self.assertTrue(all(v['step'] in case['steps'] for v in case['vectors']))
        for name, vids in case['criteria'].items():
            self.assertTrue(vids and set(vids) <= ids, name)
        gates = json.loads((ROOT / 'gates.json').read_text(encoding='utf-8'))
        g1 = next(g for g in gates['gates'] if g['id'] == 'G1')
        self.assertEqual(set(case['criteria']), set(g1['criteria']))


class ProductP2Tests(unittest.TestCase):
    def test_failed_resource_run_preserves_its_analyzer_and_chunk_counterexample(self):
        archive = ROOT / 'sources/product-g1-r2-chunks/analyze.py'
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        self.assertEqual(digest, '13ed442088652a59ec72fdc8d200a5729fc564c5a8350cde0a001415ce546033')
        run = ROOT / 'runs/G1-P1-R2-20261104T064821Z-0830c3'
        sources = json.loads((run / 'source-manifest.json').read_text(encoding='utf-8'))
        self.assertEqual(sources['experiments/product/analyze.py'], digest)
        self.assertEqual(verify(run)['recomputation'], 'observed')
        manifest = json.loads((run / 'manifest.json').read_text(encoding='utf-8'))
        raw = [json.loads(line) for line in (run / 'raw.jsonl').read_text(encoding='utf-8').splitlines()]
        corrected = analyze(manifest['case'], raw, manifest['repeats'])
        self.assertEqual(corrected['resources']['result'], 'pass')
        self.assertEqual(corrected['problems'], ['race: exit code 1'])
        self.assertEqual(corrected['verdict'], 'fail')

    def test_recorded_g4_run_uses_the_byte_exact_archived_analyzer(self):
        archive = ROOT / 'sources/product-p2-g4/analyze.py'
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        self.assertEqual(digest, '1e7465e1c897a340b54d309177ac4f2a4ae9218ae144e34c07003b3aa0a59dc6')
        runs = [ROOT / 'runs/G4-P2-20261104T054837Z-00be90']
        for run in runs:
            sources = json.loads((run / 'source-manifest.json').read_text(encoding='utf-8'))
            self.assertEqual(sources['experiments/product/analyze.py'], digest)
            self.assertEqual(verify(run)['recomputation'], 'observed')

    def test_p2_case_uses_its_scope(self):
        case = dict(CASE, kind='product-p2', scope='G4 scope')
        s = analyze(case, rows(), 1)
        self.assertEqual((s['verdict'], s['scope']), ('pass', 'G4 scope'))

    def test_load_profile_is_optional(self):
        g1 = json.loads((ROOT / 'manifests/G1-P1.json').read_text(encoding='utf-8'))['case']
        self.assertEqual(runner.step_table(Path('.'), g1)['vectors'][1].get('OI_IT_LOAD'), '1')
        self.assertNotIn('OI_IT_LOAD', runner.step_table(Path('.'), dict(g1, load=False))['vectors'][1])

    def test_g1_runs_still_recompute_with_the_archived_analyzer(self):
        runs = sorted((ROOT / 'runs').glob('G1-P1-*'))
        self.assertTrue(runs)
        for run in runs:
            self.assertIn(verify(run)['recomputation'], ('observed', 'inconclusive'))

    def test_g4_manifest_is_consistent(self):
        m = json.loads((ROOT / 'manifests/G4-P2.json').read_text(encoding='utf-8'))
        case = m['case']
        self.assertEqual((m['gate'], m['evidence_level'], m['repeats'], case['kind']), ('G4', 'Implementation', 1, 'product-p2'))
        table = runner.step_table(Path('.'), case)
        self.assertLessEqual(set(case['steps']), set(table))
        ids = {v['id'] for v in case['vectors']}
        for name, vids in case['criteria'].items():
            self.assertTrue(vids and set(vids) <= ids, name)
        gates = json.loads((ROOT / 'gates.json').read_text(encoding='utf-8'))
        g4 = next(g for g in gates['gates'] if g['id'] == 'G4')
        self.assertEqual(set(case['criteria']), set(g4['criteria']))
        sources = ''.join(p.read_text(encoding='utf-8') for p in (ROOT.parent / 'tests/integration').glob('*_test.go'))
        sources += ''.join(p.read_text(encoding='utf-8') for p in (ROOT.parent / 'internal/ois/store').glob('*_test.go'))
        for v in case['vectors']:
            for test in v['tests']:
                self.assertIn('func ' + test + '(', sources, test)


if __name__ == '__main__':
    unittest.main()
