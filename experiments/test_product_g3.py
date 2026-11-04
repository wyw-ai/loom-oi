"""G3 evidence validation: reject results outside the registered package."""
import copy
import json
import unittest

from test_product import CASE, analyze, go, rows, resource_records


def topology(kind):
    return {'ois_accesses_online': 2 if kind == 'online' else 1,
            'oics': {a: {'connected': not (a == 'lab-a' and kind == 'control_cut'),
                         'registered': not (a == 'lab-a' and kind == 'control_cut')} for a in ('lab-a', 'lab-b')},
            'controls': {a: {'control_open': not (a == 'lab-a' and kind == 'control_cut'),
                             'downstream_paused': a == 'lab-a' and kind == 'control_blackhole',
                             'pending_control_count': int(a == 'lab-a' and kind == 'control_cut')} for a in ('lab-a', 'lab-b')},
            'active_data_connections': 0, 'active_target_workers': 0, 'unclassified_connections': 0}


def g3_fixture(control=False, instrument=False):
    test, scenario = 'TestG3/sqlite/plain', 'plain'
    spec = {'schema': 'g3-resource-v1', 'vector': 'G3-RESOURCE-PHASES', 'package': 'p',
            'required_tests': [test], 'expected_oics': ['lab-a', 'lab-b'],
            'required_profiles': [], 'required_checks': [], 'instrument_tests': [],
            'instrument_profiles': [], 'preparation_checks': [],
            'topologies': {k: topology(k) for k in ('online', 'control_cut', 'control_blackhole')}}
    records = []

    def make_leaf(test, scenario, kinds, is_instrument=False):
        vector = 'TV-G3-FIXTURE' if is_instrument else 'TV-G3-01'
        common = {'schema': spec['schema'], 'package': 'p', 'vector': vector, 'test': test,
                  'backend': 'sqlite', 'scenario': scenario, 'phase': None, 'cycle': None}
        profile_specs = spec['instrument_profiles'] if is_instrument else spec['required_profiles']
        profiles = {}
        for index, kind in enumerate(kinds):
            profile_specs.append({'test': test, 'backend': 'sqlite', 'scenario': scenario,
                                  'vector': vector, 'profile_kind': kind})
            expected, start = topology(kind), 1 + index * 1_000_000_000
            observed = {'controls': {a: {'control_conn_id': test + '/' + a,
                        'control_open': c['control_open'], 'downstream_paused': c['downstream_paused'],
                        'pending_control_ids': [test + '/pending'] if c['pending_control_count'] else []}
                        for a, c in expected['controls'].items()}, 'active_data_connection_ids': [],
                        'active_target_worker_ids': [], 'unclassified_connection_ids': []}
            samples = []
            for i in range(3):
                at = start + i * 150_000_000
                base = resource_records()[0]
                snap = {k: copy.deepcopy(base[k]) for k in ('ois', 'oics')}
                snap['ois']['stats']['accesses_online'] = expected['ois_accesses_online']
                snap['ois']['sampled_ns'] = at + 25_000_000
                for a, value in snap['oics'].items():
                    value['sampled_ns'] = at + 25_000_000
                    value['stats'].update(expected['oics'][a])
                samples.append(dict(snap, sample_start_ns=at, sample_end_ns=at+50_000_000, observed=copy.deepcopy(observed)))
            profile = dict(common, kind='g3_resources_baseline', profile_id=test+'/'+kind, profile_kind=kind,
                           stage='preparation', prepare_start_ns=start, prepare_deadline_ns=start+5_000_000_000,
                           recorded_ns=start+400_000_000, preparation_samples=samples, expected_topology=expected,
                           **{k: copy.deepcopy(samples[-1][k]) for k in ('ois', 'oics', 'observed')})
            profiles[kind] = profile
            records.append(profile)

        def check(kind, stage, start, phase):
            planned = dict(test=test, backend='sqlite', scenario=scenario, phase=phase, cycle=None,
                           profile_kind=kind, order_in_leaf=0, origin_event='control_restored' if stage == 'preparation' else 'vector_cleanup')
            spec['preparation_checks' if stage == 'preparation' else 'required_checks'].append(planned)
            profile = copy.deepcopy(profiles[kind])
            if len(kinds) > 1:
                for sample in [profile, *profile['preparation_samples']]:
                    sample['oics']['lab-a']['stats']['generation'] = 2
                    sample['observed']['controls']['lab-a']['control_conn_id'] += '/restored'
            binding = {'ois_pid': profile['ois']['pid'],
                       'oics': {a: {k: s['stats'][k] for k in ('epoch', 'generation')} | {'pid': s['pid']} for a, s in profile['oics'].items()},
                       'control_conn_ids': {a: c['control_conn_id'] for a, c in profile['observed']['controls'].items()},
                       'pending_control_ids': {a: c['pending_control_ids'] for a, c in profile['observed']['controls'].items()}}
            identity = dict(common, stage=stage, profile_id=profile['profile_id'], profile_kind=kind,
                            phase=phase, cycle=None, check_id=test+'/'+phase, start_ns=start, deadline_ns=start+2_000_000_000)
            initial = copy.deepcopy(profile['preparation_samples'][-1])
            initial.update(sample_start_ns=start, sample_end_ns=start+10_000_000)
            initial['ois']['sampled_ns'] = start+5_000_000
            for value in initial['oics'].values():
                value['sampled_ns'] = start+5_000_000
            records.append(dict(identity, kind='g3_resources_begin', origin_event=planned['origin_event'], origin_ns=start,
                                binding=binding, binding_samples=[initial]))
            for i in range(3):
                sample = copy.deepcopy(profile['preparation_samples'][-1])
                at = start+50_000_000+i*150_000_000
                sample.update(sample_start_ns=at, sample_end_ns=at+50_000_000)
                sample['ois']['sampled_ns'] = at+25_000_000
                for value in sample['oics'].values():
                    value['sampled_ns'] = at+25_000_000
                records.append(dict(identity, kind='g3_resources_sample', **sample))
            records.append(dict(identity, kind='g3_resources_complete', completed_ns=start+450_000_000, settled_ms=450, samples=3))

        if len(kinds) > 1:
            check('online', 'preparation', 3_000_000_000, 'online_restored_preparation')
        if not is_instrument:
            check(kinds[-1], 'validation', 4_000_000_000, 'final_cleanup')

    make_leaf(test, scenario, ['online', 'control_cut'] if control else ['online'])
    if instrument:
        name = 'TestG3ResourceTopologyProfiles/environment_control_blackhole/sqlite'
        spec['instrument_tests'].append(name)
        make_leaf(name, 'environment_control_blackhole', ['online', 'control_blackhole'], True)
    case = copy.deepcopy(CASE)
    case['g3_resources'] = spec
    case['criteria']['g3'] = ['G3-RESOURCE-PHASES']
    return case, records


class G3ResourceEvidenceTests(unittest.TestCase):
    def summary(self, records=None, case=None, fragment=False):
        if case is None:
            case, default = g3_fixture()
            if records is None:
                records = default
        events = []
        for record in records:
            text = 'OBS ' + json.dumps(record) + '\n'
            pieces = [text[:31], text[31:]] if fragment else [text]
            events += [go('vectors', 'output', record['test'], piece) for piece in pieces]
        events += [go('vectors', 'pass', t) for t in case['g3_resources']['required_tests'] + case['g3_resources']['instrument_tests']]
        return analyze(case, rows(extra=events), 1)

    def test_profiles_and_preparation_are_counted_separately(self):
        case, records = g3_fixture(control=True, instrument=True)
        summary = self.summary(records, case, fragment=True)
        self.assertEqual(summary['verdict'], 'pass', summary['problems'])
        g3 = summary['g3_resources']
        self.assertEqual((g3['profiles'], g3['instrument_profiles']), (2, 2))
        self.assertEqual(sum(g3['completed_checks'].values()), 1)
        self.assertEqual(sum(g3['preparation_checks'].values()), 2)

    def test_raw_mutations_cannot_be_saved_by_passing_go_test(self):
        mutations = {
            'late_baseline': lambda r: r[0].update(recorded_ns=6_000_000_002),
            'short_baseline': lambda r: r[0]['preparation_samples'].pop(),
            'post_fault_baseline': lambda r: r.append(copy.deepcopy(r[0])),
            'unknown_kind': lambda r: r[2].update(kind='g3_resources_silent_drop'),
            'wrong_package': lambda r: r[0].update(package='other'),
            'wrong_vector': lambda r: r[0].update(vector='TV-G3-02'),
            'wrong_backend': lambda r: r[0].update(backend='postgres'),
            'boolean_cycle': lambda r: r[1].update(cycle=False),
            'missing_phase': lambda r: r.pop(),
            'changed_deadline': lambda r: r[2].update(deadline_ns=6_100_000_000),
            'pid_replaced': lambda r: r[2]['ois'].update(pid=101),
            'generation_replaced': lambda r: r[2]['oics']['lab-a']['stats'].update(generation=2),
            'connection_replaced': lambda r: r[2]['observed']['controls']['lab-a'].update(control_conn_id='new'),
            'fd_leak': lambda r: r[-2]['ois']['stats']['process'].update(open_fds=21),
            'fd_disappeared': lambda r: r[-2]['ois']['stats']['process'].update(open_fds=19),
            'goroutine_leak': lambda r: r[-2]['ois']['stats']['process'].update(goroutines=41),
            'data_leak': lambda r: r[-2]['observed'].update(active_data_connection_ids=['leaked']),
            'missing_role': lambda r: r[2]['oics'].pop('lab-b'),
            'bad_sample_count': lambda r: r[-1].update(samples=4),
            'duplicate_complete': lambda r: r.append(copy.deepcopy(r[-1])),
            'reset_origin': lambda r: r[1].update(origin_ns=4_000_000_001),
            'late_sample': lambda r: r[-2].update(sample_end_ns=6_000_000_001),
            'missing_cycle': lambda r: r[0].pop('cycle'),
            'null_preparation_sample': lambda r: r[0]['preparation_samples'].__setitem__(0, None),
            'float_pid': lambda r: r[1]['binding'].update(ois_pid=100.0),
            'float_deadline': lambda r: r[2].update(deadline_ns=6_000_000_000.0),
            'unstable_generation': lambda r: r[0]['preparation_samples'][0]['oics']['lab-a']['stats'].update(generation=2),
            'unstable_control': lambda r: r[0]['preparation_samples'][0]['observed']['controls']['lab-a'].update(control_conn_id='old'),
            'reused_control': lambda r: r[2]['observed']['controls']['lab-a'].update(control_conn_id=r[2]['observed']['controls']['lab-b']['control_conn_id']),
            'control_also_pending': lambda r: r[2]['observed']['controls']['lab-a'].update(pending_control_ids=[r[2]['observed']['controls']['lab-a']['control_conn_id']]),
            'missing_binding_samples': lambda r: r[1].pop('binding_samples'),
            'binding_disagrees_with_sample': lambda r: r[1]['binding_samples'][0]['oics']['lab-a']['stats'].update(generation=2),
            'float_baseline_mirror': lambda r: r[0]['ois']['stats']['process'].update(goroutines=30.0),
            'boolean_topology_number': lambda r: r[0]['expected_topology']['oics']['lab-a'].update(connected=1),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                case, records = g3_fixture()
                mutate(records)
                result = self.summary(records, case)
                self.assertEqual(result['verdict'], 'fail', result)
                self.assertEqual(result['g3_resources']['result'], 'fail', result)

    def test_malformed_unsettled_sample_is_not_ignored(self):
        case, records = g3_fixture()
        bad = copy.deepcopy(records[2])
        bad['ois']['stats']['channels']['open'] = 1
        bad['observed'].pop('controls')
        records.insert(2, bad)
        result = self.summary(records, case)
        self.assertTrue(any('invalid registered or observed topology' in p for p in result['problems']))

    def test_no_resource_records_cannot_satisfy_required_phases(self):
        case, _ = g3_fixture()
        self.assertEqual(self.summary([], case)['verdict'], 'fail')

    def test_preregistration_cannot_relax_topology(self):
        case, records = g3_fixture()
        case['g3_resources']['topologies']['online']['active_data_connections'] = 1
        with self.assertRaisesRegex(ValueError, 'fixed acceptance contract'):
            self.summary(records, case)

    def test_valid_unsettled_sample_recovers_with_three_new_samples(self):
        case, records = g3_fixture()
        for record in records[2:-1]:
            for key in ('sample_start_ns', 'sample_end_ns'):
                record[key] += 150_000_000
            record['ois']['sampled_ns'] += 150_000_000
            for value in record['oics'].values():
                value['sampled_ns'] += 150_000_000
        initial = copy.deepcopy(records[2])
        for key in ('sample_start_ns', 'sample_end_ns'):
            initial[key] -= 150_000_000
        initial['ois']['sampled_ns'] -= 150_000_000
        for value in initial['oics'].values():
            value['sampled_ns'] -= 150_000_000
        initial['ois']['stats']['process']['open_fds'] += 1
        records.insert(2, initial)
        records[-1].update(completed_ns=4_600_000_000, settled_ms=600, samples=4)
        result = self.summary(records, case)
        self.assertEqual(result['verdict'], 'pass', result['problems'])

    def test_deadline_is_inclusive_but_never_extended(self):
        case, records = g3_fixture()
        records[-1].update(completed_ns=6_000_000_000, settled_ms=2000)
        self.assertEqual(self.summary(records, case)['verdict'], 'pass')
        records[-1]['completed_ns'] += 1
        self.assertEqual(self.summary(records, case)['verdict'], 'fail')


class PackageBindingTests(unittest.TestCase):
    def case(self):
        case = copy.deepcopy(CASE)
        for entry in case['vectors']:
            entry['package'] = 'p'
        return case

    def test_other_package_cannot_supply_required_test(self):
        case = self.case()
        events = rows()
        for row in events:
            if row.get('kind') == 'go' and row['event'].get('Test') == 'TestX/sqlite':
                row['event']['Package'] = 'different-package'
        result = analyze(case, events, 1)
        self.assertEqual(result['verdict'], 'fail')
        self.assertEqual(result['vectors']['TV-X']['result'], 'missing')

    def test_other_package_same_name_does_not_create_duplicate(self):
        events = rows()
        extra = go('vectors', 'pass', 'TestX/sqlite')
        extra['event']['Package'] = 'different-package'
        events.insert(-1, extra)
        result = analyze(self.case(), events, 1)
        self.assertEqual(result['verdict'], 'pass', result['problems'])

    def test_empty_or_non_string_package_is_not_a_wildcard(self):
        for package in ['', None, False, []]:
            with self.subTest(package=package):
                case = self.case()
                case['vectors'][0]['package'] = package
                with self.assertRaisesRegex(ValueError, 'invalid vector package'):
                    analyze(case, rows(), 1)


if __name__ == '__main__':
    unittest.main()
