"""Archive/command counterexamples; no network, Go execution or clock changes."""
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
import warnings
import zipfile


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    exec(compile(path.read_bytes(), str(path), 'exec'), module.__dict__)
    return module


HERE = Path(__file__).parent
db = load('test_vulndb_snapshot', HERE / 'product/vulndb_snapshot.py')


def fixture(change=None, entry=None):
    documents = {
        'index/db.json': {'modified': '2026-11-02T22:01:43Z'},
        'index/modules.json': [{'path': 'example.test/a', 'vulns': [{'id': 'GO-2026-0001'}]}],
        'index/vulns.json': [{'id': 'GO-2026-0001', 'modified': '2026-11-02T22:01:43Z'}],
        'ID/GO-2026-0001.json': {'id': 'GO-2026-0001', 'affected': []},
    }
    if change:
        change(documents)
    members = {name: db.canonical(value) for name, value in documents.items()}
    buf = io.BytesIO()
    with warnings.catch_warnings(), zipfile.ZipFile(buf, 'w', zipfile.ZIP_DEFLATED) as archive:
        warnings.simplefilter('ignore', UserWarning)
        for name, data in members.items():
            archive.writestr(name, data)
        if entry:
            entry(archive)
    raw = buf.getvalue()
    hashes = {name: db.sha(data) for name, data in members.items()}
    provenance = dict(url=db.URL, resolved_url=db.URL, bytes=len(raw), sha256=db.sha(raw), archive_prefix='',
                      download_started_utc='2026-11-04T17:42:50+00:00',
                      download_completed_utc='2026-11-04T17:42:51+00:00', db_metadata=documents['index/db.json'],
                      modules=len(documents['index/modules.json']), vulnerability_ids=len(members)-3,
                      module_referenced_ids=len(members)-3, members=len(members), zip_crc_valid=True,
                      all_index_ids_present=True, member_sha256=hashes)
    prov = db.canonical(provenance)
    spec = dict(schema=db.SCHEMA, url=db.URL, zip_sha256=db.sha(raw), zip_bytes=len(raw),
                provenance_sha256=db.sha(prov), download_completed_utc=provenance['download_completed_utc'],
                modified=documents['index/db.json']['modified'], index_sha256={k: hashes[k] for k in db.INDEX},
                members=len(members), vulnerability_ids=len(members)-3, modules=provenance['modules'])
    return spec, raw, prov, members


def context_for(spec, members, directory=None):
    directory = Path.cwd()/'task/vulndb' if directory is None else directory
    return dict(spec=spec, directory=str(directory), members={k: db.sha(v) for k, v in members.items()})


def recorded_rows(context):
    fact = db.fact(context)
    return [dict(step='env', kind='fact', name='vulndb_snapshot', value=fact),
            dict(step='govulncheck', kind='fact', name='vulndb_scan_before', value=copy.deepcopy(fact)),
            dict(step='govulncheck', kind='start', command=db.command(context)),
            dict(step='govulncheck', kind='end', exit_code=0, timed_out=False),
            dict(step='govulncheck', kind='fact', name='vulndb_scan_after', value=copy.deepcopy(fact))]


class VulnerabilitySnapshotTests(unittest.TestCase):
    def test_complete_archive_and_index_closure(self):
        spec, raw, prov, members = fixture()
        self.assertEqual(db.validate_archive(spec, raw, prov), {k: db.sha(v) for k, v in members.items()})

    def test_preregistered_bytes_and_types_are_not_advisory(self):
        spec, raw, prov, _ = fixture()
        for key, value in [('schema', 'other'), ('url', 'https://mirror.invalid/db'), ('zip_bytes', True),
                           ('members', 5), ('modules', False), ('vulnerability_ids', 0),
                           ('zip_sha256', 'A'*64), ('provenance_sha256', 'f'*64),
                           ('download_completed_utc', '2026-01-01T00:00:00Z'),
                           ('modified', '2026-11-02T22:01:43'), ('unknown', 1)]:
            changed = copy.deepcopy(spec); changed[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                db.validate_archive(changed, raw, prov)
        with self.assertRaises(ValueError): db.validate_archive(spec, raw[:-1] + b'x', prov)
        with self.assertRaises(ValueError): db.validate_archive(spec, raw, prov + b' ')

    def test_self_consistent_hashes_cannot_hide_bad_index_closure(self):
        changes = [lambda d: d['index/vulns.json'].append(copy.deepcopy(d['index/vulns.json'][0])),
                   lambda d: d['index/modules.json'][0]['vulns'][0].update(id='GO-2026-9999'),
                   lambda d: d['index/modules.json'][0].update(vulns=[]),
                   lambda d: d['index/vulns.json'][0].update(id='GO-2026-9999'),
                   lambda d: d['ID/GO-2026-0001.json'].update(id='GO-2026-9999'),
                   lambda d: d['index/modules.json'].append(copy.deepcopy(d['index/modules.json'][0])),
                   lambda d: d.update({'ID/GO-2026-9999.json': {'id': 'GO-2026-9999'}})]
        for i, change in enumerate(changes):
            spec, raw, prov, _ = fixture(change)
            with self.subTest(change=i), self.assertRaises(ValueError): db.validate_archive(spec, raw, prov)

    def test_unsafe_member_and_duplicate_cannot_be_extracted_or_accepted(self):
        def link(archive):
            entry = zipfile.ZipInfo('ID/GO-2026-0002.json')
            entry.external_attr = (stat.S_IFLNK | 0o777) << 16
            archive.writestr(entry, '../../elsewhere')
        for add in [lambda z: z.writestr('../outside', '{}'), lambda z: z.writestr('/absolute', '{}'),
                    lambda z: z.writestr('index/db.json', '{}'), link]:
            spec, raw, prov, _ = fixture(entry=add)
            # Match the new container count so rejection exercises member validation.
            spec['members'] += 1; spec['vulnerability_ids'] += 1
            with self.subTest(add=add), self.assertRaises(ValueError): db.validate_archive(spec, raw, prov)

    def test_provenance_booleans_do_not_replace_actual_member_checks(self):
        spec, raw, prov, _ = fixture()
        value = json.loads(prov); value['member_sha256']['ID/GO-2026-0001.json'] = '0'*64
        changed = db.canonical(value); spec['provenance_sha256'] = db.sha(changed)
        with self.assertRaises(ValueError): db.validate_archive(spec, raw, changed)

    def test_prepare_directory_scan_and_exclusive_archive_preservation(self):
        spec, raw, prov, members = fixture()
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); directory = root/'db'; output = root/'run'; output.mkdir()
            for name, data in members.items():
                target = directory/name; target.parent.mkdir(parents=True, exist_ok=True); target.write_bytes(data)
            (root/'input.zip').write_bytes(raw); (root/'provenance.json').write_bytes(prov)
            env = dict(zip(db.ENV_KEYS, map(str, [root/'input.zip', root/'provenance.json', directory])))
            case = dict(execution_profile='local-isolated-vm-v1', steps=['govulncheck'], vulndb_snapshot=spec)
            context = db.prepare(case, env, output)
            self.assertEqual((output/'vulndb.zip').read_bytes(), raw)
            self.assertEqual(db.command(context), ['go', 'run', db.TOOL, '-db', directory.as_uri(), './...'])
            self.assertEqual(db.verify_directory(context), db.fact(context))
            with self.assertRaises(FileExistsError): db.prepare(case, env, output)
            target = directory/'ID/GO-2026-0001.json'; target.write_bytes(b'{}')
            with self.assertRaises(ValueError): db.verify_directory(context)
            target.write_bytes(members['ID/GO-2026-0001.json']); target.unlink()
            with self.assertRaises(ValueError): db.verify_directory(context)
            target.write_bytes(members['ID/GO-2026-0001.json']); (directory/'extra').write_bytes(b'{}')
            with self.assertRaises(ValueError): db.verify_directory(context)

    def test_snapshot_environment_never_silently_falls_back(self):
        self.assertIsNone(db.prepare({}, {}, Path('.')))
        for key in db.ENV_KEYS:
            with self.subTest(key=key), self.assertRaises(ValueError): db.prepare({}, {key: ''}, Path('.'))
        spec, *_ = fixture()
        for case in [dict(vulndb_snapshot=spec), dict(vulndb_snapshot=spec, execution_profile='local-isolated-vm-v1', steps=[])]:
            with self.assertRaises(ValueError): db.prepare(case, {}, Path('.'))

    def test_scan_facts_enclose_actual_pinned_command_and_success(self):
        spec, _, _, members = fixture(); context = context_for(spec, members)
        case = dict(execution_profile='local-isolated-vm-v1', vulndb_snapshot=spec)
        rows = recorded_rows(context)
        self.assertEqual(db.validate_rows(case, rows), db.fact(context))
        changes = [lambda r: r.pop(0), lambda r: r.append(copy.deepcopy(r[0])),
                   lambda r: r[1].update(step='env'), lambda r: r[2].update(command=['go', 'run', db.TOOL, './...']),
                   lambda r: r[2]['command'].__setitem__(4, 'https://vuln.go.dev'),
                   lambda r: r[2]['command'].__setitem__(2, 'govulncheck@other'),
                   lambda r: r[3].update(exit_code=1), lambda r: r[3].update(exit_code=False),
                   lambda r: r[3].update(timed_out=True), lambda r: r[4]['value'].update(uri='file:///changed'),
                   lambda r: r[0]['value'].update(members_sha256=True),
                   lambda r: r.insert(2, r.pop(4)), lambda r: r[1].update(name='vulndb_scan_after')]
        for i, change in enumerate(changes):
            candidate = copy.deepcopy(rows); change(candidate)
            with self.subTest(change=i), self.assertRaises(ValueError): db.validate_rows(case, candidate)
        with self.assertRaises(ValueError): db.validate_rows({}, rows)

    def test_recorded_materials_are_independently_opened(self):
        spec, raw, prov, members = fixture(); context = context_for(spec, members)
        case = dict(execution_profile='local-isolated-vm-v1', vulndb_snapshot=spec)
        rows = recorded_rows(context); environment = dict(vulndb_snapshot=db.fact(context))
        with tempfile.TemporaryDirectory() as td:
            run = Path(td); (run/'vulndb.zip').write_bytes(raw); (run/'vulndb-provenance.json').write_bytes(prov)
            db.verify_materials(run, case, rows, environment)
            rows[-2]['exit_code'] = 3
            db.verify_materials(run, case, rows, environment)
            with self.assertRaises(ValueError): db.validate_rows(case, rows)
            with self.assertRaises(ValueError): db.verify_materials(run, case, rows, {})
            (run/'vulndb-provenance.json').write_bytes(prov+b' ')
            with self.assertRaises(ValueError): db.verify_materials(run, case, rows, environment)

    def test_materials_accept_real_scan_prefixes_without_quality_pass(self):
        spec, raw, prov, members = fixture(); context = context_for(spec, members)
        case = dict(execution_profile='local-isolated-vm-v1', vulndb_snapshot=spec)
        full = recorded_rows(context); environment = dict(vulndb_snapshot=db.fact(context))
        with tempfile.TemporaryDirectory() as td:
            run = Path(td); (run/'vulndb.zip').write_bytes(raw); (run/'vulndb-provenance.json').write_bytes(prov)
            for count in range(1, len(full)+1):
                rows = copy.deepcopy(full[:count])
                if count >= 3:
                    rows.insert(3, dict(step='govulncheck', kind='line', text='actual scan diagnostic'))
                original = copy.deepcopy(rows)
                with self.subTest(prefix=count):
                    db.verify_materials(run, case, rows, environment)
                    self.assertEqual(rows, original)  # Never synthesize before/after.
                    if count < len(full):
                        with self.assertRaises(ValueError): db.validate_rows(case, rows)
                    else:
                        self.assertEqual(db.validate_rows(case, rows), environment['vulndb_snapshot'])
            for code, timeout in [(1, False), (-9, True), (0, True)]:
                rows = copy.deepcopy(full)
                rows[-2].update(exit_code=code, timed_out=timeout)
                with self.subTest(code=code, timeout=timeout):
                    db.verify_materials(run, case, rows, environment)
                    with self.assertRaises(ValueError): db.validate_rows(case, rows)

    def test_material_prefix_rejects_holes_duplicates_reorder_and_wrong_command(self):
        spec, raw, prov, members = fixture(); context = context_for(spec, members)
        case = dict(execution_profile='local-isolated-vm-v1', vulndb_snapshot=spec)
        full = recorded_rows(context); environment = dict(vulndb_snapshot=db.fact(context))
        changes = {
            'no_env': lambda r: r.pop(0),
            'no_before': lambda r: r.pop(1),
            'no_start': lambda r: r.pop(2),
            'no_end': lambda r: r.pop(3),
            'before_twice': lambda r: r.insert(2, copy.deepcopy(r[1])),
            'env_twice': lambda r: r.insert(1, copy.deepcopy(r[0])),
            'start_twice': lambda r: r.insert(3, copy.deepcopy(r[2])),
            'end_twice': lambda r: r.insert(4, copy.deepcopy(r[3])),
            'after_twice': lambda r: r.append(copy.deepcopy(r[4])),
            'after_before_start': lambda r: r.insert(2, r.pop(4)),
            'end_before_start': lambda r: r.insert(2, r.pop(3)),
            'before_on_env': lambda r: r[1].update(step='env'),
            'after_other_step': lambda r: r[4].update(step='dependency-vuln'),
            'other_database': lambda r: r[1]['value'].update(uri='file:///other'),
            'other_after_database': lambda r: r[4]['value'].update(uri='file:///other'),
            'unfixed_command': lambda r: r[2].update(command=['go', 'run', db.TOOL, './...']),
            'another_tool': lambda r: r[2]['command'].__setitem__(2, 'govulncheck@other'),
            'bool_exit': lambda r: r[3].update(exit_code=False),
            'nonbool_timeout': lambda r: r[3].update(timed_out=0),
            'line_before_start': lambda r: r.insert(2, dict(step='govulncheck', kind='line', text='diagnostic')),
            'line_after_end': lambda r: r.insert(4, dict(step='govulncheck', kind='line', text='diagnostic')),
            'untyped_output': lambda r: r.insert(3, dict(step='govulncheck', kind='line', text=None)),
            'unexpected_go': lambda r: r.insert(3, dict(step='govulncheck', kind='go', event={})),
            'unknown_fact': lambda r: r.insert(3, dict(step='govulncheck', kind='fact', name='unverified', value={})),
        }
        with tempfile.TemporaryDirectory() as td:
            run = Path(td); (run/'vulndb.zip').write_bytes(raw); (run/'vulndb-provenance.json').write_bytes(prov)
            for name, change in changes.items():
                rows = copy.deepcopy(full); change(rows)
                with self.subTest(name=name), self.assertRaises(ValueError):
                    db.verify_materials(run, case, rows, environment)
            with self.assertRaises(ValueError): db.verify_materials(run, case, [], environment)

    def test_partial_scan_still_requires_exact_environment_and_archived_bytes(self):
        spec, raw, prov, members = fixture(); context = context_for(spec, members)
        case = dict(execution_profile='local-isolated-vm-v1', vulndb_snapshot=spec)
        rows = recorded_rows(context)[:1]; environment = dict(vulndb_snapshot=db.fact(context))
        with tempfile.TemporaryDirectory() as td:
            run = Path(td); (run/'vulndb.zip').write_bytes(raw); (run/'vulndb-provenance.json').write_bytes(prov)
            with self.assertRaises(ValueError): db.verify_materials(run, case, rows, {})
            changed = copy.deepcopy(environment); changed['vulndb_snapshot']['members_sha256'] = '0'*64
            with self.assertRaises(ValueError): db.verify_materials(run, case, rows, changed)
            (run/'vulndb.zip').write_bytes(raw+b'changed')
            with self.assertRaises(ValueError): db.verify_materials(run, case, rows, environment)
            (run/'vulndb.zip').write_bytes(raw); (run/'vulndb-provenance.json').write_bytes(prov+b' ')
            with self.assertRaises(ValueError): db.verify_materials(run, case, rows, environment)

    def test_main_analyzer_cannot_ignore_snapshot_failure_or_pass_without_scan(self):
        base = load('vulndb_product_fixture', HERE/'test_product.py')
        main = load('vulndb_product_analysis', HERE/'product/analyze.py')
        local = load('vulndb_local_identity', HERE/'product/local_execution.py')
        source = 'a'*40
        attestation = dict(schema=local.ATTESTATION_SCHEMA,
            run_id='local-11111111-1111-1111-1111-111111111111', source_sha=source,
            boot_id='22222222-2222-2222-2222-222222222222', vm_uuid='33333333-3333-3333-3333-333333333333',
            vm_serial='loom-oi-clock-33333333-3333-3333-3333-333333333333', runner_uid=1000, runner_gid=1000,
            network_mode='isolated', host_shares=False, **{key: 'b'*64 for key in local.HASH_KEYS})
        spec, _, _, members = fixture(); context = context_for(spec, members)
        case = copy.deepcopy(base.CASE)
        case.update(execution_profile=local.PROFILE,
                    local_execution={k: attestation[k] for k in main.LOCAL_MANIFEST_KEYS}, vulndb_snapshot=spec)
        case['steps'].append('govulncheck')
        snapshot_rows = recorded_rows(context)
        raw = [dict(step='env', kind='fact', name='subject_version', value=source),
               dict(step='env', kind='fact', name='local_execution',
                    value=dict(attestation=attestation, attestation_sha256=local.digest(attestation))),
               snapshot_rows[0], *base.rows(), *snapshot_rows[1:]]
        summary = main.analyze(case, raw, 1)
        self.assertEqual(summary['verdict'], 'pass', summary['problems'])
        raw[-2]['exit_code'] = 1
        summary = main.analyze(case, raw, 1)
        self.assertEqual(summary['verdict'], 'fail')
        self.assertTrue(any('vulnerability snapshot' in p for p in summary['problems']))
        raw.pop()
        self.assertEqual(main.analyze(case, raw, 1)['verdict'], 'fail')

    @unittest.skipUnless(os.name == 'posix', 'real symbolic links require POSIX')
    def test_linked_input_and_database_member_are_rejected(self):
        spec, _, _, members = fixture()
        with tempfile.TemporaryDirectory() as td:
            root = Path(td); directory = root/'db'
            for name, data in members.items():
                target = directory/name; target.parent.mkdir(parents=True, exist_ok=True); target.write_bytes(data)
            target = directory/'ID/GO-2026-0001.json'; target.unlink()
            outside = root/'actual.json'; outside.write_bytes(members['ID/GO-2026-0001.json']); target.symlink_to(outside)
            with self.assertRaises(ValueError): db.verify_directory(context_for(spec, members, directory))
            with self.assertRaises(ValueError): db.regular_bytes(target, db.MAX_EXPANDED)
            target.unlink(); target.write_bytes(members['ID/GO-2026-0001.json'])
            alias = root/'alias'; alias.symlink_to(directory, target_is_directory=True)
            with self.assertRaises(ValueError): db.regular_bytes(alias/'index/db.json', db.MAX_EXPANDED)
            with self.assertRaises(ValueError): db.verify_directory(context_for(spec, members, alias/'index'))


if __name__ == '__main__':
    unittest.main()
