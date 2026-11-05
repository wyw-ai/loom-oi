"""Fixed adapter counterexamples; no Go execution, network or clock mutation."""
import copy
import importlib.util
import json
from pathlib import Path, PurePosixPath
import shutil
import tempfile
import unittest


HERE = Path(__file__).parent
ROOT = HERE.parent
spec = importlib.util.spec_from_file_location('test_dependency_quality', HERE/'product/dependency_quality.py')
dq = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dq)


def case():
    return dict(dependency_quality=dq.expected_spec(), steps=['unit', 'race', *dq.STEPS],
                go_steps=['unit', 'race', 'dependency-unit', 'dependency-race'],
                quality_package_parallelism=1, coverage_min_pct=95,
                execution_profile='local-isolated-vm-v1', vulndb_snapshot={'fixture': 'fixed'})


def profiles():
    root = 'mode: atomic\n'+dq.ROOT_MODULE+'/internal/example/example.go:1.1,2.1 100 1\n'
    patch = 'mode: atomic\n'+''.join(f'{k} {n} 1\n' for k, n in dq.NATIVE_BLOCKS.items())
    # Upstream zero-statement blocks are real Go coverage output, not an error.
    patch += dq.MODULE+'/conn.go:1.1,1.1 0 0\n'
    return root, patch


def query(uri='file:///fixed/db'):
    return json.dumps(dict(config=dict(protocol_version='v1.0.0', scanner_name='govulncheck',
                      scanner_version='v1.9.0', db=uri, scan_mode='query', scan_level='symbol')))+'\n'+\
           json.dumps(dict(progress=dict(message='Looking up vulnerabilities in '+dq.MODULE+' at v1.8.15...')))+'\n'


def go_events(step):
    names = {(x['package'], x['test']) for x in dq.REQUIRED_ROOTS+dq.OWNED_TESTS}
    events = []
    for pkg in dq.PACKAGES:
        events.append(dict(Action='start', Package=pkg))
        if pkg in dq.NO_TEST_SKIPS[step]:
            events += [dict(Action='output', Package=pkg, Output='?   \t'+pkg+'\t[no test files]\n'),
                       dict(Action='skip', Package=pkg)]
            continue
        for _, name in sorted(x for x in names if x[0] == pkg):
            events += [dict(Action='run', Package=pkg, Test=name),
                       dict(Action='skip' if (pkg, name) in dq.ALLOWED_SKIPS else 'pass', Package=pkg, Test=name)]
        events.append(dict(Action='pass', Package=pkg))
    return events


def fixture():
    c = case()
    work = PurePosixPath('/work')
    source = dq.source_fact(ROOT)
    db = dict(schema='go-vulndb-input-v1', snapshot=c['vulndb_snapshot'], tool=dq.GOVULNCHECK,
              uri='file:///fixed/db', members_sha256='1'*64)
    rows = [dict(step='env', kind='fact', name='dependency_source', value=source),
            dict(step='env', kind='fact', name='vulndb_snapshot', value=db)]
    table = {
        'unit': (['go', 'test', '-json', '-p=1', '-count=1', '-covermode=atomic',
                  '-coverprofile=/work/unit.out', './cmd/...', './internal/...'], {}, 1800, True),
        'race': (['go', 'test', '-json', '-p=1', '-race', '-count=1', './cmd/...', './internal/...'],
                  {'CGO_ENABLED': '1'}, 2400, True),
        **dq.commands(work, c, db['uri'])}
    root, patch = profiles()
    for name in c['steps']:
        if name == 'dependency-vuln':
            rows.append(dict(step=name, kind='fact', name='dependency_vulndb_before', value=copy.deepcopy(db)))
        argv, env, _, _ = table[name]
        rows.append(dict(step=name, kind='start', command=argv, env=env))
        if name in ('dependency-unit', 'dependency-race'):
            rows += [dict(step=name, kind='go', event=x) for x in go_events(name)]
        if name == 'dependency-vuln':
            rows += [dict(step=name, kind='line', text=x, terminated=True) for x in query().splitlines()]
        rows.append(dict(step=name, kind='end', exit_code=0, timed_out=False))
        if name == 'unit':
            rows.append(dict(step=name, kind='fact', name='root_unit_profile', value=root))
        if name == 'dependency-unit':
            rows.append(dict(step=name, kind='fact', name='dependency_unit_profile', value=patch))
        if name == 'dependency-vuln':
            rows.append(dict(step=name, kind='fact', name='dependency_vulndb_after', value=copy.deepcopy(db)))
    rows.append(dict(step='env', kind='fact', name='dependency_source_after', value=copy.deepcopy(source)))
    return c, rows, source


def find(rows, **values):
    return next(x for x in rows if all(x.get(k) == v for k, v in values.items()))


class DependencyQualityTests(unittest.TestCase):
    def test_complete_evidence_exact_owned_denominator(self):
        c, rows, _ = fixture()
        result = dq.validate_rows(c, rows)
        self.assertEqual(result['coverage']['root'], {'covered': 100, 'total': 100, 'pct': 100})
        self.assertEqual(result['coverage']['owned']['total'], 12)
        self.assertEqual(result['coverage']['owned']['covered'], 11)
        self.assertEqual(result['coverage']['combined']['total'], 112)
        for step in ('dependency-unit', 'dependency-race'):
            self.assertEqual(len(result['tests'][step]['skipped']), 2)
            self.assertEqual(result['tests'][step]['packages'][dq.MODULE+'/internal/test'], 'skip')

    def test_declaration_and_step_types_never_downgrade(self):
        for value in (None, {}, True, 'websocket-control-v1', dict(dq.expected_spec(), schema='future')):
            with self.subTest(value=value), self.assertRaises(ValueError):
                dq.validate_case(dict(case(), dependency_quality=value))
        for key, value in [('steps', ['unit', 'race']), ('go_steps', ['unit', 'race']),
                           ('quality_package_parallelism', True), ('coverage_min_pct', 94),
                           ('execution_profile', 'github-hosted')]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                dq.validate_case(dict(case(), **{key: value}))
        self.assertFalse(dq.validate_case({'steps': ['unit']}))
        with self.assertRaises(ValueError):
            dq.validate_case({'steps': ['dependency-unit']})
        with self.assertRaises(ValueError):
            dq.validate_rows({'steps': ['unit']}, [dict(step='env', kind='fact', name='dependency_source', value={})])

    def test_fixed_commands_keep_original_timeouts_and_scope(self):
        table = dq.commands(PurePosixPath('/work'), case(), 'file:///fixed/db')
        self.assertEqual([table[s][2] for s in dq.STEPS], [300, 600, 120, 300, 120])
        self.assertEqual(table['dependency-race'][1], {'CGO_ENABLED': '1'})
        for name in ('dependency-unit', 'dependency-race'):
            self.assertEqual(table[name][0][-1], './...')
            self.assertFalse(any(s.startswith(('-timeout', '-run', '-short')) for s in table[name][0]))
        self.assertEqual(table['dependency-vuln'][0], ['go', 'run', dq.GOVULNCHECK, '-db', 'file:///fixed/db',
                                                     '-json', '-mode=query', dq.MODULE+'@'+dq.VERSION])

    def test_fixed_import_and_replace_are_actual_bytes(self):
        source = dq.source_fact(ROOT)
        self.assertEqual(len(source['files']), 73)
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            shutil.copytree(ROOT/dq.DIRECTORY, root/dq.DIRECTORY)
            shutil.copyfile(ROOT/'go.mod', root/'go.mod')
            self.assertEqual(dq.source_fact(root), source)
            patch = root/dq.DIRECTORY/'control_write_js.go'
            patch.write_bytes(patch.read_bytes()+b'\n')
            with self.assertRaises(ValueError):
                dq.source_fact(root)
            shutil.copyfile(ROOT/dq.DIRECTORY/'control_write_js.go', patch)
            (root/'go.mod').write_text(source['root_go_mod'].replace('./third_party/websocket', './other'), encoding='utf-8')
            with self.assertRaises(ValueError):
                dq.source_fact(root)

    def test_profiles_reject_missing_duplicate_and_wrong_scope(self):
        root, patch = profiles()
        variants = [('missing_native', '\n'.join(patch.splitlines()[:-2])+'\n'),
                    ('duplicate', patch+patch.splitlines()[1]+'\n'),
                    ('wrong_numstmt', patch.replace(' 2 1\n', ' 1 1\n')),
                    ('fake_js', patch+dq.MODULE+'/control_write_js.go:9.2,9.30 1 1\n'),
                    ('unknown_file', patch+dq.MODULE+'/new.go:1.1,2.1 1 1\n'),
                    ('wrong_mode', patch.replace('atomic', 'set'))]
        for name, changed in variants:
            with self.subTest(name=name), self.assertRaises(ValueError):
                dq.summarize_coverage(root, changed)
        with self.assertRaises(ValueError):
            dq.summarize_coverage(root.replace('/internal/', '/tests/'), patch)
        with self.assertRaises(ValueError):
            dq.summarize_coverage(root.replace(' 100 1', ' 0 0'), patch)
        # A large fully-covered upstream block cannot dilute the owned denominator.
        diluted = patch+dq.MODULE+'/conn.go:3.1,4.1 1000000 1\n'
        self.assertEqual(dq.summarize_coverage(root, diluted), dq.summarize_coverage(root, patch))

    def test_exact_fraction_not_rounded_total(self):
        _, patch = profiles()
        root = 'mode: atomic\n'+dq.ROOT_MODULE+'/cmd/oi/main.go:1.1,2.1 1889 1\n'+\
               dq.ROOT_MODULE+'/cmd/oi/main.go:3.1,4.1 100 0\n'
        result = dq.summarize_coverage(root, patch)
        self.assertEqual(round(result['combined']['pct'], 1), 95.0)
        self.assertFalse(result['meets_threshold'])  # 1900/2001, not 95%.

    def test_query_zero_exit_cannot_hide_vulnerability_or_truncation(self):
        self.assertEqual(dq.validate_query(query(), expected_db='file:///fixed/db')['osv_count'], 0)
        variants = [query()+json.dumps({'osv': {'id': 'GO-TEST'}})+'\n', query().rstrip('\n'),
                    query().splitlines()[0]+'\n', query()+'{\n', query()+query(),
                    query().replace('v1.8.15', 'v1.8.14'), query().replace('v1.9.0', 'v1.8.0'),
                    query().replace('"query"', '"source"'), query().replace('file:///fixed/db', 'https://vuln.go.dev'),
                    query().replace('"scanner_name":', '"scanner_name":"evil","scanner_name":')]
        for i, value in enumerate(variants):
            with self.subTest(i=i), self.assertRaises(ValueError):
                dq.validate_query(value, expected_db='file:///fixed/db')

    def test_ci_default_and_formal_snapshot_never_fall_back(self):
        self.assertEqual(dq.validate_query(query('https://vuln.go.dev'))['osv_count'], 0)
        for text, expected in [(query(), None), (query('https://elsewhere.invalid'), None),
                               (query('https://vuln.go.dev'), 'file:///fixed/db'),
                               (query(), 'https://vuln.go.dev'), (query(), 'file:///other')]:
            with self.subTest(expected=expected), self.assertRaises(ValueError):
                dq.validate_query(text, expected_db=expected)

    def test_real_step_scope_database_source_and_profile_provenance(self):
        changes = {
            'filter': lambda r: find(r, step='dependency-unit', kind='start')['command'].extend(['-run', 'TestX']),
            'cwd': lambda r: find(r, step='dependency-unit', kind='start')['command'].__setitem__(2, 'other'),
            'race_env': lambda r: find(r, step='dependency-race', kind='start').update(env={}),
            'exit': lambda r: find(r, step='dependency-vuln', kind='end').update(exit_code=1),
            'timeout': lambda r: find(r, step='dependency-unit', kind='end').update(timed_out=True),
            'partial_line': lambda r: find(r, step='dependency-vuln', kind='line').update(terminated=False),
            'changed_db': lambda r: find(r, name='dependency_vulndb_after')['value'].update(uri='file:///different'),
            'changed_source': lambda r: find(r, name='dependency_source_after')['value']['files'].update({'go.mod': '0'*64}),
            'missing_profile': lambda r: r.remove(find(r, name='dependency_unit_profile')),
            'duplicate_fact': lambda r: r.append(copy.deepcopy(find(r, name='dependency_source_after'))),
            'early_after': lambda r: r.insert(0, r.pop()),
            'root_filter': lambda r: find(r, step='unit', kind='start')['command'].append('-short'),
        }
        for name, change in changes.items():
            c, rows, _ = fixture()
            change(rows)
            with self.subTest(name=name), self.assertRaises(ValueError):
                dq.validate_rows(c, rows)

    def test_native_test_inventory_and_only_named_skips(self):
        for mode in ('extra_skip', 'fake_no_tests', 'hide_test', 'missing_package', 'duplicate_test',
                     'missing_local_subtest', 'package_failure', 'race_diagnostic'):
            c, rows, _ = fixture()
            if mode == 'extra_skip':
                e = next(r['event'] for r in rows if r.get('step') == 'dependency-unit' and
                         r.get('event', {}).get('Test', '').startswith('TestExplicitControl') and r['event']['Action'] == 'pass')
                e['Action'] = 'skip'
            elif mode == 'fake_no_tests':
                e = next(r['event'] for r in rows if r.get('step') == 'dependency-unit' and
                         r.get('event', {}).get('Package') == dq.MODULE+'/wsjson' and r['event']['Action'] == 'pass')
                e['Action'] = 'skip'
            elif mode == 'hide_test':
                index = next(i for i, r in enumerate(rows) if r.get('event', {}).get('Package') == dq.MODULE+'/internal/test'
                             and r['event']['Action'] == 'skip')
                rows[index:index] = [dict(step='dependency-unit', kind='go', event=dict(
                    Action=a, Package=dq.MODULE+'/internal/test', Test='TestHidden')) for a in ('run', 'pass')]
            elif mode == 'missing_package':
                rows[:] = [r for r in rows if not (r.get('step') == 'dependency-unit' and
                           r.get('event', {}).get('Package') == dq.MODULE+'/wsjson')]
            elif mode == 'duplicate_test':
                index = next(i for i, r in enumerate(rows) if r.get('event', {}).get('Action') == 'run')
                rows.insert(index, copy.deepcopy(rows[index]))
            elif mode == 'missing_local_subtest':
                name = next(x['test'] for x in dq.OWNED_TESTS if '/' in x['test'])
                rows[:] = [r for r in rows if not (r.get('step') == 'dependency-unit' and r.get('event', {}).get('Test') == name)]
            elif mode == 'package_failure':
                event = next(r['event'] for r in rows if r.get('step') == 'dependency-unit' and
                             r.get('event', {}).get('Package') == dq.MODULE and
                             not r['event'].get('Test') and r['event']['Action'] == 'pass')
                event['Action'] = 'fail'
            else:
                index = next(i for i, r in enumerate(rows) if r.get('event', {}).get('Action') == 'run')
                rows.insert(index+1, dict(step='dependency-unit', kind='go', event=dict(
                    Action='output', Package=dq.MODULE, Output='WARNING: DATA RACE\n')))
            with self.subTest(mode=mode), self.assertRaises(ValueError):
                dq.validate_rows(c, rows)

    def test_no_test_package_terminals_are_command_specific(self):
        for step, wrong in [('dependency-unit', 'skip'), ('dependency-race', 'pass')]:
            c, rows, _ = fixture()
            event = next(r['event'] for r in rows if r.get('step') == step and
                         r.get('event', {}).get('Package') == dq.MODULE+'/internal/bpool' and
                         r['event']['Action'] in ('pass', 'skip'))
            event['Action'] = wrong
            with self.subTest(step=step), self.assertRaises(ValueError):
                dq.validate_rows(c, rows)
        c, rows, _ = fixture()
        output = next(r['event'] for r in rows if r.get('step') == 'dependency-race' and
                      r.get('event', {}).get('Package') == dq.MODULE+'/internal/bpool' and
                      r['event']['Action'] == 'output')
        output['Output'] = '?   \tother/package\t[no test files]\n'
        with self.assertRaises(ValueError):
            dq.validate_rows(c, rows)

    def test_materials_match_facts_without_turning_failure_into_pass(self):
        c, rows, source = fixture()
        with tempfile.TemporaryDirectory() as td:
            run = Path(td)
            for name, (_, artifact) in dq.PROFILE_FACTS.items():
                (run/artifact).write_bytes(find(rows, name=name)['value'].encode('utf-8'))
            find(rows, step='dependency-vuln', kind='end')['exit_code'] = 1
            dq.verify_materials(run, c, rows, source['files'])
            with self.assertRaises(ValueError):
                dq.validate_rows(c, rows)
            (run/'dependency-coverage.out').write_bytes(b'changed')
            with self.assertRaises(ValueError):
                dq.verify_materials(run, c, rows, source['files'])
            (run/'dependency-coverage.out').write_bytes(find(rows, name='dependency_unit_profile')['value'].encode('utf-8'))
            with self.assertRaises(ValueError):
                dq.verify_materials(run, c, rows, dict(source['files'], **{dq.DIRECTORY+'/extra.go': '0'*64}))


if __name__ == '__main__':
    unittest.main()
