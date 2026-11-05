"""Real collector/analysis adapter seams, with only process/host boundaries stubbed."""
import contextlib
import copy
import hashlib
import io
import json
from pathlib import Path
import shutil
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import test_product as product
import test_product_dependency_quality as dependency
import test_product_local_adapter as adapter
import test_product_vulndb as database
import verify


ROOT = Path(__file__).resolve().parent.parent
runner = product.load('dependency_wiring_runner', ROOT/'experiments/product/run.py')
dq = dependency.dq
db = database.db


def joined_fixture():
    """Synthetic product events plus the actual three pure validators; no PASS mock."""
    c, rows, _ = dependency.fixture()
    c.update(kind='product-p1', go='go1.27.3', backends=['sqlite', 'postgres'],
             engines=product.CASE['engines'], vectors=[dict(id='TV-UNIT', step='unit', tests=['TestEvidence'], subtests=[])],
             criteria={'contract': ['TV-UNIT']})
    local_case, _, attestation, *_ = adapter.fixture()
    c['local_execution'] = local_case['local_execution']
    spec, archive, provenance, members = database.fixture()
    context = database.context_for(spec, members)
    value = db.fact(context)
    c['vulndb_snapshot'] = spec
    for row in rows:
        if row.get('name') in ('vulndb_snapshot', *dq.DB_FACTS):
            row['value'] = copy.deepcopy(value)
        if row.get('step') == 'dependency-vuln' and row['kind'] == 'start':
            row['command'][row['command'].index('-db')+1] = value['uri']
        if row.get('step') == 'dependency-vuln' and row['kind'] == 'line':
            row['text'] = row['text'].replace('file:///fixed/db', value['uri'])
    prefix = [dict(step='env', kind='fact', name=name, value=value_) for name, value_ in (
        ('go_version', 'go version go1.27.3 linux/amd64'), ('goos', 'linux'), ('go_flags', ''),
        ('subject_version', attestation['source_sha']),
        ('local_execution', dict(attestation=attestation, attestation_sha256=adapter.local.digest(attestation))))]
    rows[:0] = prefix
    for step in ('unit', 'race'):
        index = rows.index(dependency.find(rows, step=step, kind='end'))
        events = [dict(step=step, kind='go', event=dict(Action='pass', Package='p', Test='TestEvidence'))]
        if step == 'unit':
            events[:0] = [dict(step=step, kind='go', event=dict(Action='output', Package='p', Test='TestEvidence',
                Output='ENGINE sqlite 3.54.1\nENGINE postgres 18.6\n')),
                dict(step=step, kind='coverage', lines=['total: (statements) 1.0%'])]
        rows[index:index] = events
    c['steps'].append('govulncheck')
    rows[-1:-1] = database.recorded_rows(context)[1:]
    return c, rows, attestation, (spec, archive, provenance, members)


class DependencyWiringTests(unittest.TestCase):
    def test_current_runner_refuses_omitted_dependency_group_before_processes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            upstream = root/'third_party/websocket/UPSTREAM.json'
            upstream.parent.mkdir(parents=True)
            upstream.write_bytes((ROOT/'third_party/websocket/UPSTREAM.json').read_bytes())
            prereg = root/'manifest.json'
            case = dict(product.CASE, fuzz_time='60s',
                        fuzz={'fuzz-frame': 'FuzzDecodeFrame', 'fuzz-control': 'FuzzDecodeControl'})
            prereg.write_text(json.dumps(dict(case=case, evidence_level='Implementation',
                hypothesis='fixture', alternatives=['fixture'], disproof=['fixture'],
                limitations=['fixture'], issues=['fixture'])), encoding='utf-8')
            with patch.object(runner, 'ROOT', root), patch.object(sys, 'argv', ['run.py', str(prereg)]), \
                    patch.object(runner.subprocess, 'run') as command, patch.object(runner.subprocess, 'Popen') as process:
                with self.assertRaisesRegex(ValueError, 'requires explicit dependency'):
                    runner.main()
                command.assert_not_called()
                process.assert_not_called()

    def test_verify_refuses_recorded_nested_sources_without_declaration(self):
        import test_product_local_seal as sealing
        with tempfile.TemporaryDirectory() as tmp:
            run = Path(tmp)/'LOCAL-20261105T000000Z-123abc'
            run.mkdir()
            receipt, _, _, _, sources = sealing.fixture(run)
            sources['third_party/websocket/control_write.go'] = dq.FIXED_FILES['control_write.go']
            (run/'source-manifest.json').write_bytes(sealing.s.local.canonical(sources))
            receipt['files'] = sealing.s.file_hashes(run)
            sealing.s.seal_run(run, receipt)
            with self.assertRaisesRegex(AssertionError, 'lacks dependency quality declaration'):
                verify.verify(run)

    def test_main_uses_exact_combined_profile_not_legacy_display(self):
        c, rows, _, _ = joined_fixture()
        summary = runner.analyze(c, rows, 1)
        self.assertEqual(summary['verdict'], 'pass', summary['problems'])
        self.assertEqual(summary['coverage_total_pct'], 100*111/112)
        self.assertEqual(summary['dependency_quality']['coverage']['owned']['total'], 12)
        changed = copy.deepcopy(rows)
        root = dependency.find(changed, name='root_unit_profile')
        root['value'] = ('mode: atomic\n'+dq.ROOT_MODULE+'/internal/x.go:1.1,2.1 1889 1\n'+
                         dq.ROOT_MODULE+'/internal/x.go:3.1,4.1 100 0\n')
        # The old formatted total is now deceptively high. Exact 1900/2001 fails.
        dependency.find(changed, step='unit', kind='coverage')['lines'] = ['total: (statements) 100.0%']
        failed = runner.analyze(c, changed, 1)
        self.assertEqual(failed['verdict'], 'fail')
        self.assertTrue(any('combined exact coverage' in x for x in failed['problems']))

    def test_unregistered_facts_and_dependency_steps_never_enable_implicitly(self):
        c, rows = copy.deepcopy(product.CASE), product.rows()
        rows.append(dict(step='env', kind='fact', name='dependency_source', value={}))
        with self.assertRaises(ValueError):
            runner.analyze(c, rows, 1)
        c['steps'].append('dependency-unit')
        c.update(fuzz_time='60s', fuzz={'fuzz-frame': 'FuzzFrame', 'fuzz-control': 'FuzzControl'})
        with self.assertRaises(ValueError):
            runner.step_table(Path('/tmp/work'), c)

    def test_query_is_line_json_with_real_termination_bit(self):
        for terminated in (True, False):
            text = dependency.query()
            if not terminated:
                text = text.rstrip('\n')
            fake = SimpleNamespace(stdout=io.StringIO(text), wait=lambda: 0, kill=lambda: None)
            raw = io.StringIO()
            with patch.object(runner.subprocess, 'Popen', return_value=fake):
                code, timed_out, _ = runner.run_step('dependency-vuln', ['fixture-query'], {}, 2, False, raw, io.StringIO())
            rows = [json.loads(x) for x in raw.getvalue().splitlines()]
            self.assertEqual((code, timed_out), (0, False))
            self.assertEqual(rows[0]['kind'], 'start')
            self.assertTrue(all(r['kind'] == 'line' for r in rows[1:]))
            self.assertEqual(rows[-1]['terminated'], terminated)

    def test_source_roots_and_recorded_dependency_whitelists_remain_versioned(self):
        clock = product.load('dependency_wiring_clock', ROOT/'experiments/product/clock_run.py')
        self.assertIn('third_party/websocket', runner.SOURCES)
        self.assertIn('third_party/websocket', clock.SOURCE_PATHS)
        for declaration in (verify.LEGACY_DEPENDENCIES, verify.LOCAL_DEPENDENCIES,
                            verify.SEALED_DEPENDENCIES, verify.DEPENDENCIES):
            source = 'ANALYSIS_DEPENDENCIES = '+repr(declaration)
            self.assertEqual(verify.analyzer_dependencies(source, {}), ())
            if 'dependency_quality' in declaration:
                self.assertEqual(verify.analyzer_dependencies(source, {'dependency_quality': {}}), ('dependency_quality.py',))
            else:
                with self.assertRaises(AssertionError):
                    verify.analyzer_dependencies(source, {'dependency_quality': {}})

    def collect(self, root, fail_step=None, final_source_error=False):
        c, _, attestation, inputs = joined_fixture()
        spec, archive, provenance, members = inputs
        c.update(fuzz_time='60s', fuzz={'fuzz-frame': 'FuzzFrame', 'fuzz-control': 'FuzzControl'})
        # Missing real fuzz evidence deliberately keeps this collector fixture
        # failing. Only external Go/process boundaries are stubbed below.
        manifest = dict(id='DEPENDENCY-WIRING', gate='G1', evidence_level='Implementation', case=c, repeats=1,
                        hypothesis='fixture', alternatives=['fixture'], disproof=['fixture'], limitations=['fixture'], issues=['fixture'])
        shutil.copytree(ROOT/'third_party/websocket', root/'third_party/websocket')
        shutil.copytree(ROOT/'experiments/product', root/'experiments/product', ignore=shutil.ignore_patterns('__pycache__'))
        shutil.copyfile(ROOT/'go.mod', root/'go.mod')
        prereg = root/'preregistered.json'
        prereg.write_text(json.dumps(manifest), encoding='utf-8')
        inputs_dir = root/'inputs'
        inputs_dir.mkdir()
        (inputs_dir/'db.zip').write_bytes(archive)
        (inputs_dir/'provenance.json').write_bytes(provenance)
        dbdir = inputs_dir/'database'
        for name, content in members.items():
            path = dbdir/name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        env = dict(OI_VULNDB_ZIP=str(inputs_dir/'db.zip'), OI_VULNDB_PROVENANCE=str(inputs_dir/'provenance.json'),
                   OI_VULNDB_DIR=str(dbdir))
        tracked = ['go.mod', 'preregistered.json']+[p.relative_to(root).as_posix() for directory in
            (root/'third_party/websocket', root/'experiments/product') for p in directory.rglob('*') if p.is_file()]
        original_local_case = runner._analyzer._local_case
        original_sibling = runner.sibling
        identity = dict(pid=100, pgid=100, sid=100, start_ticks=1, uid=1001, gid=1001)
        source_reads = []

        def local_case(value):
            module = original_local_case(value)
            if module:
                module.environment = lambda *args, **kwargs: copy.deepcopy(attestation)
            return module

        def sibling(name):
            module = original_sibling(name)
            if name == 'local_run_seal':
                module.process_identity = lambda: identity
            if name == 'dependency_quality' and final_source_error:
                actual_source = module.source_fact
                def source(root):
                    source_reads.append(True)
                    if len(source_reads) == 2:
                        raise OSError('controlled final source read failure')
                    return actual_source(root)
                module.source_fact = source
            return module

        def git(*args):
            if args[0] in ('rev-parse', 'log'):
                return attestation['source_sha']
            if args[0] == 'ls-files':
                return '\n'.join(tracked)
            raise AssertionError(args)

        def command(argv, **kwargs):
            if argv[:2] == ['go', 'version']:
                return SimpleNamespace(stdout='go version go1.27.3 linux/amd64\n')
            if argv[:2] == ['go', 'env']:
                return SimpleNamespace(stdout=json.dumps(dict(GOOS='linux', GOARCH='amd64', GOFLAGS='')))
            if argv[:3] == ['go', 'tool', 'cover']:
                return SimpleNamespace(stdout='total: (statements) 1.0%\n', returncode=0)
            raise AssertionError(argv)

        def step(name, argv, extra, timeout, is_json, raw, stderr, **kwargs):
            raw.write(json.dumps(dict(step=name, kind='start', command=argv, env=extra))+'\n')
            if name == fail_step:
                raise OSError('controlled process boundary failure')
            if name in ('dependency-unit', 'dependency-race'):
                self.assertTrue(is_json)
                for event in dependency.go_events(name):
                    raw.write(json.dumps(dict(step=name, kind='go', event=event))+'\n')
            if name in ('unit', 'race'):
                for event in (dict(Action='output', Package='p', Test='TestEvidence',
                                   Output='ENGINE sqlite 3.54.1\nENGINE postgres 18.6\n'),
                              dict(Action='pass', Package='p', Test='TestEvidence')):
                    raw.write(json.dumps(dict(step=name, kind='go', event=event))+'\n')
            if name in ('unit', 'dependency-unit'):
                text = dependency.profiles()[name != 'unit']
                profile = Path(next(a.split('=', 1)[1] for a in argv if a.startswith('-coverprofile=')))
                self.assertTrue(profile.is_absolute())
                profile.parent.mkdir(parents=True, exist_ok=True)
                profile.write_text(text, encoding='utf-8', newline='\n')
            if name == 'dependency-vuln':
                self.assertFalse(is_json)
                self.assertEqual(timeout, 120)
                actual_uri = argv[argv.index('-db')+1]
                self.assertEqual(actual_uri, dbdir.as_uri())
                for line in dependency.query(actual_uri).splitlines():
                    raw.write(json.dumps(dict(step=name, kind='line', text=line, terminated=True))+'\n')
            kwargs['process_record'].update(identity=identity, waited_ns=123)
            return 0, False, 1

        with contextlib.ExitStack() as stack:
            for obj, key, value in ((runner, 'ROOT', root), (runner, 'git', git),
                    (runner, 'workspace_dirty', lambda _: False), (runner, 'sibling', sibling),
                    (runner._analyzer, '_local_case', local_case), (runner.subprocess, 'run', command),
                    (runner, 'run_step', step), (runner.platform, 'system', lambda: 'Linux'),
                    (runner.platform, 'release', lambda: 'parser-fixture'), (runner.platform, 'machine', lambda: 'x86_64')):
                stack.enter_context(patch.object(obj, key, value))
            stack.enter_context(patch.dict(runner.os.environ, env, clear=True))
            stack.enter_context(patch.object(sys, 'argv', ['run.py', str(prereg), '--work', str(root/'work'),
                                                         '--output-root', str(root/'runs')]))
            stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
            self.assertEqual(runner.main(), 1)
        run = next((root/'runs').iterdir())
        rows = [json.loads(x) for x in (run/'raw.jsonl').read_text(encoding='utf-8').splitlines()]
        return run, rows

    def test_actual_collector_encloses_steps_and_preserves_profile_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            run, rows = self.collect(Path(tmp))
            stored = json.loads((run/'manifest.json').read_bytes())
            summary = json.loads((run/'summary.json').read_bytes())
            self.assertEqual(summary, runner.analyze(stored['case'], rows, 1))
            self.assertEqual((summary['status'], summary['verdict']), ('observed', 'fail'))
            self.assertIsNotNone(summary['dependency_quality'])
            self.assertEqual(rows[-1]['name'], 'dependency_source_after')
            for name, (step, artifact) in dq.PROFILE_FACTS.items():
                end = rows.index(dependency.find(rows, step=step, kind='end'))
                fact = dependency.find(rows, name=name)
                self.assertGreater(rows.index(fact), end)
                self.assertEqual((run/artifact).read_bytes(), fact['value'].encode('utf-8'))
            sources = json.loads((run/'source-manifest.json').read_bytes())
            dq.verify_materials(run, stored['case'], rows, sources)
            module = verify.recorded_module(run)
            self.assertEqual(module.analyze(stored['case'], rows, 1), summary)
            self.assertEqual(set(module._recorded_dependency_hashes),
                {'dependency_quality.py', 'local_execution.py', 'local_run_seal.py', 'vulndb_snapshot.py'})

    def test_actual_runner_exception_keeps_final_source_and_cannot_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            run, rows = self.collect(Path(tmp), fail_step='dependency-vet')
            c = json.loads((run/'manifest.json').read_bytes())['case']
            self.assertEqual(rows[-1]['name'], 'dependency_source_after')
            self.assertEqual(json.loads((run/'execution.json').read_bytes())['exit_code'], 1)
            self.assertEqual(json.loads((run/'summary.json').read_bytes())['status'], 'inconclusive')
            dq.verify_materials(run, c, rows, json.loads((run/'source-manifest.json').read_bytes()))
            db.verify_materials(run, c, rows, json.loads((run/'environment.json').read_bytes()))
            with self.assertRaises(ValueError):
                dq.validate_rows(c, rows)

    def test_missing_final_readback_preserves_materials_without_quality_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            run, rows = self.collect(Path(tmp), final_source_error=True)
            c = json.loads((run/'manifest.json').read_bytes())['case']
            self.assertFalse(any(r.get('name') == 'dependency_source_after' for r in rows))
            execution = json.loads((run/'execution.json').read_bytes())
            self.assertEqual(execution['exit_code'], 1)
            self.assertIn('controlled final source read failure', execution['runner_error'])
            dq.verify_materials(run, c, rows, json.loads((run/'source-manifest.json').read_bytes()))
            with self.assertRaisesRegex(ValueError, 'dependency_source_after'):
                dq.validate_rows(c, rows)
            db.verify_materials(run, c, rows, json.loads((run/'environment.json').read_bytes()))

    def test_ci_uses_integer_threshold_and_both_saved_go_streams(self):
        workflow = (ROOT/'.github/workflows/ci.yml').read_text(encoding='utf-8')
        self.assertIn("coverage['meets_threshold']", workflow)
        self.assertIn('_go_tests', workflow)
        self.assertIn('dependency-unit.jsonl', workflow)
        self.assertIn('dependency-race.jsonl', workflow)


if __name__ == '__main__':
    unittest.main()
