"""Runner framing/regression tests; no real clock, sudo or Go execution."""
import contextlib
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import py_compile
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('product_clock_runner',
    Path(__file__).parent/'product/run.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)

CASE = dict(kind='product-p2', backends=['sqlite', 'postgres'], fuzz_time='60s',
            fuzz={'fuzz-frame': 'FuzzFrame', 'fuzz-control': 'FuzzControl'}, steps=[])


class ProductClockRunnerTests(unittest.TestCase):
    def test_fixed_analyzer_source_is_used_despite_stale_timestamp_cache(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp); analyzer=root/'analyze.py'; source=root/'run.py'
            analyzer.write_bytes(b'def analyze(*args): return "old"\n')
            original=analyzer.stat();py_compile.compile(str(analyzer),doraise=True)
            analyzer.write_bytes(b'def analyze(*args): return "new"\n')
            os.utime(analyzer,ns=(original.st_atime_ns,original.st_mtime_ns))
            old_spec=importlib.util.spec_from_file_location('old_main_analyzer_proof',analyzer)
            old=importlib.util.module_from_spec(old_spec);old_spec.loader.exec_module(old)
            self.assertEqual(old.analyze(),'old')
            source.write_bytes(Path(runner.__file__).read_bytes())
            current_spec=importlib.util.spec_from_file_location('current_runner_bytes',source)
            current=importlib.util.module_from_spec(current_spec)
            exec(compile(source.read_bytes(),str(source),'exec'),current.__dict__)
            self.assertEqual(current.analyze(),'new')

    def test_existing_steps_keep_commands_budgets_and_explicit_clock_scope(self):
        work = Path('test-work')
        table = runner.step_table(work, CASE)
        budgets = dict(unit=1800, race=2400, vectors=2700, **{'fuzz-frame': 900,
            'fuzz-control': 900, 'fuzz-state': 900}, vet=900, staticcheck=1200, govulncheck=1200)
        self.assertEqual(set(table), set(budgets))
        self.assertEqual({name: table[name][2] for name in budgets}, budgets)
        self.assertEqual(table['unit'], (['go', 'test', '-json', '-count=1', '-covermode=atomic',
            '-coverprofile='+str(work/'unit.out'), './cmd/...', './internal/...'], {}, 1800, True))
        self.assertEqual(table['race'], (['go', 'test', '-json', '-race', '-count=1',
            './cmd/...', './internal/...'], {'CGO_ENABLED': '1'}, 2400, True))
        self.assertEqual(table['vectors'], (['go', 'test', '-json', '-count=1', '-timeout',
            '40m', './tests/integration'], {'OI_IT': '1', 'OI_IT_CLOCK': '0',
            'OI_IT_BACKENDS': 'sqlite,postgres', 'OI_IT_RUNDIR': str(work/'it'), 'OI_IT_LOAD': '1'}, 2700, True))
        clock_table=runner.step_table(work,dict(CASE,g2_clock={},steps=['clock']))
        self.assertEqual(clock_table['clock'], ([sys.executable, str(runner.HERE/'clock_run.py'),
            '--work', str(work/'clock')], {'OI_IT': '1', 'OI_IT_CLOCK': '1',
            'OI_IT_BACKENDS': 'sqlite,postgres'}, 3600, True))
        g2 = runner.step_table(work, dict(CASE, g2_behavior={}))
        for name in table.keys()-{'vectors'}:
            self.assertEqual(g2[name], table[name])
        self.assertEqual(g2['vectors'][0][-2:], ['90m', './tests/integration'])
        self.assertEqual(g2['vectors'][2], 5700)
        self.assertNotIn('OI_IT_LOAD', runner.step_table(work, dict(CASE, load=False))['vectors'][1])
        with tempfile.TemporaryDirectory() as tmp:
            runner.step_table(Path(tmp), CASE)
            self.assertFalse((Path(tmp)/'clock').exists())

    def test_ordinary_clock_injection_is_rejected_before_any_process(self):
        for case in (dict(CASE,steps=['clock']),dict(CASE,g2_clock={})):
            with self.subTest(case=case),mock.patch.object(runner.subprocess,'Popen') as proc:
                with self.assertRaisesRegex(ValueError,'explicit G2 clock'):
                    runner.step_table(Path('.'),case)
                proc.assert_not_called()

    def run_step(self, text, code=0, name='clock', timed_out=False):
        proc = mock.Mock(stdout=io.StringIO(text))
        proc.wait.return_value = code
        timer = mock.Mock()
        def new_timer(_timeout, callback):
            if timed_out:
                timer.start.side_effect = callback
            return timer
        raw, errors = io.StringIO(), io.StringIO()
        with mock.patch.object(runner.subprocess, 'Popen', return_value=proc), \
                mock.patch.object(runner.threading, 'Timer', side_effect=new_timer), \
                mock.patch.object(runner.os, 'name', 'nt'):
            try:
                result = runner.run_step(name, ['fixture-child'], {'OI_IT_CLOCK': '1' if name == 'clock' else '0'},
                    3600, True, raw, errors)
            except ValueError as exc:
                result = exc
        return result, [json.loads(line) for line in raw.getvalue().splitlines()], proc, timer

    def test_g2_fixture_step_executes_real_package_without_changing_coverage_scope(self):
        work = Path('test-work')
        old = runner.step_table(work, CASE)
        new = runner.step_table(work, dict(CASE, g2_behavior={}))
        self.assertNotIn('fixtures', old)
        self.assertEqual(new['fixtures'], (['go', 'test', '-json', '-count=1', './tests/fixture'], {}, 900, True))
        self.assertEqual(new['unit'], old['unit'])
        self.assertEqual(new['race'], old['race'])
        real = dict(Action='pass', Package='github.com/wyw-ai/loom-oi/tests/fixture',
                    Test='TestHealthVersionedHeaderGate')
        result, rows, _, _ = self.run_step(json.dumps(real)+'\n', name='fixtures')
        self.assertEqual(result[:2], (0, False))
        self.assertEqual(rows[1], dict(step='fixtures', kind='go', event=real))

    def test_original_envelopes_and_go_events_keep_distinct_raw_kinds(self):
        records = [{'kind': kind, 'event': {'schema': 'fixture', 'nested': [1, True, None], 'source_sha': 'a'*40}}
                   for kind in ('clock_preparation', 'clock_guard', 'clock_result')]
        records.insert(1, {'Action': 'output', 'Package': 'test-package', 'Test': 'TestClock/leaf',
                           'Output': 'OBS {"value": 1}\n'})
        records.insert(2, {'Action': 'pass', 'Package': 'test-package', 'Test': 'TestClock/leaf'})
        result, rows, proc, timer = self.run_step(''.join(json.dumps(r)+'\n' for r in records))
        self.assertEqual(result[:2], (0, False))
        for original, row in zip(records, rows[1:]):
            self.assertEqual(row['step'], 'clock')
            self.assertEqual(row['kind'], original.get('kind', 'go'))
            self.assertEqual(row['event'], original['event'] if 'kind' in original else original)
        proc.wait.assert_called_once()
        timer.cancel.assert_called_once()

    def test_invalid_envelopes_cannot_become_go_pass_and_all_output_is_retained(self):
        invalid = [dict(kind='go', event={'Action': 'pass'}),
                   dict(kind='clock_guard', event=[]), dict(kind='clock_result', event=None),
                   dict(kind='clock_result', event={}, Action='pass'),
                   dict(kind='clock_unknown', event={}), dict(kind=[], event={}),
                   dict(Action='unknown'), dict(Action=[]), dict(unrelated='pass')]
        for record in invalid:
            with self.subTest(record=record):
                original = json.dumps(record)
                tail = {'kind': 'clock_guard', 'event': {'action': 'finished', 'ok': False}}
                result, rows, proc, timer = self.run_step(original+'\n'+json.dumps(tail)+'\n')
                self.assertIsInstance(result, ValueError)
                self.assertIn('subprocess exit_code=0', str(result))
                self.assertEqual(rows[1], dict(step='clock', kind='line', text=original))
                self.assertEqual(rows[2], dict(step='clock', **tail))
                self.assertFalse(any(row['kind'] == 'go' for row in rows))
                proc.wait.assert_called_once()
                timer.cancel.assert_called_once()

    def test_non_json_and_truncated_json_fail_even_after_a_real_pass(self):
        valid = json.dumps({'Action': 'pass', 'Test': 'TestClock/leaf'})+'\n'
        for tail in ('not json\n', '{broken}\n', '[1,2]\n',
                     json.dumps({'kind': 'clock_result', 'event': {}})):
            with self.subTest(tail=tail):
                result, rows, proc, _ = self.run_step(valid+tail)
                self.assertIsInstance(result, ValueError)
                self.assertEqual(rows[1]['kind'], 'go')
                self.assertEqual(rows[-1]['kind'], 'line')
                proc.wait.assert_called_once()

    def test_exit_status_and_timeout_cannot_be_repaired_by_success_envelope(self):
        text = json.dumps({'kind': 'clock_result', 'event': {'exit_code': 0, 'verified': True}})+'\n'
        result, _, _, _ = self.run_step(text, code=7)
        self.assertEqual(result[:2], (7, False))
        result, _, proc, timer = self.run_step(text, code=-9, timed_out=True)
        self.assertEqual(result[:2], (-9, True))
        proc.kill.assert_called_once()
        timer.cancel.assert_called_once()

    def test_clock_envelope_interpretation_does_not_change_ordinary_steps(self):
        record = {'kind': 'clock_result', 'event': {'exit_code': 0}}
        result, rows, _, _ = self.run_step(json.dumps(record)+'\nlegacy diagnostic\n', name='vectors')
        self.assertEqual(result[:2], (0, False))
        self.assertEqual(rows[1], dict(step='vectors', kind='go', event=record))
        self.assertEqual(rows[2], dict(step='vectors', kind='line', text='legacy diagnostic'))

    def test_source_fact_is_exact_and_emitted_only_for_g2_cases(self):
        for extra in ({}, {'g2_behavior': {}}, {'g2_clock': {}}, {'g2_behavior': {}, 'g2_clock': {}}):
            with self.subTest(extra=extra), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                prereg = root/'manifest.json'
                case = dict(copy.deepcopy(CASE), **extra)
                if 'g2_clock' in case:
                    case['steps']=['clock']
                manifest = dict(id='clock-fixture', gate='G2', evidence_level='Implementation', case=case,
                    repeats=1, hypothesis='fixture', alternatives=['fixture'], disproof=['fixture'],
                    limitations=['no product execution'], issues=['fixture'])
                prereg.write_text(json.dumps(manifest), encoding='utf-8')
                def git(*args):
                    if args[0] in ('rev-parse', 'log'):
                        return 'a'*40
                    if args[0] == 'ls-files':
                        return ''
                    raise AssertionError(args)
                def command(argv, **kwargs):
                    if argv[:2] == ['go', 'version']:
                        return SimpleNamespace(stdout='go version fixture linux/amd64\n')
                    if argv[:2] == ['go', 'env']:
                        return SimpleNamespace(stdout=json.dumps({'GOOS': 'linux', 'GOARCH': 'amd64'}))
                    raise AssertionError(argv)
                with mock.patch.object(runner, 'ROOT', root), mock.patch.object(runner, 'git', side_effect=git), \
                        mock.patch.object(runner, 'workspace_dirty', return_value=False), \
                        mock.patch.object(runner, 'run_step', return_value=(0,False,1)), \
                        mock.patch.object(runner.subprocess, 'run', side_effect=command), \
                        mock.patch.dict(runner.os.environ, {'GITHUB_WORKFLOW_REF': 'fixed-workflow',
                            'GITHUB_JOB': 'clock', 'RUNNER_ENVIRONMENT': 'github-hosted'}), \
                        mock.patch.object(runner, 'analyze', return_value={'status': 'inconclusive', 'problems': []}), \
                        mock.patch.object(sys, 'argv', ['run.py', str(prereg), '--output-root', str(root/'runs'),
                                                      '--work', str(root/'work')]), contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(runner.main(), 1)
                path = next((root/'runs').iterdir())
                facts = [json.loads(line) for line in (path/'raw.jsonl').read_text(encoding='utf-8').splitlines()
                         if json.loads(line).get('kind')=='fact']
                source = [r for r in facts if r.get('name') == 'subject_version']
                expected = [dict(step='env', kind='fact', name='subject_version', value='a'*40)] if extra else []
                self.assertEqual(source, expected)
                self.assertEqual([r['name'] for r in facts if r not in source], ['go_version', 'goos', 'goarch'])
                ci = json.loads((path/'environment.json').read_text(encoding='utf-8'))['ci']
                for key, value in {'GITHUB_WORKFLOW_REF': 'fixed-workflow', 'GITHUB_JOB': 'clock',
                                   'RUNNER_ENVIRONMENT': 'github-hosted'}.items():
                    if extra:
                        self.assertEqual(ci[key], value)
                    else:
                        self.assertNotIn(key, ci)


if __name__ == '__main__':
    unittest.main()
