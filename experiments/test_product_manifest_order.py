"""Collector failures must reproduce from the manifest actually stored on disk."""
import contextlib
import hashlib
import io
import json
from pathlib import Path
import shutil
import sys
import tempfile
import types
import unittest
from unittest import mock

from check_iterations import check_primary_result
from verify import verify


SOURCE = Path(__file__).parent / 'product/run.py'
runner = types.ModuleType('manifest_order_runner')
runner.__file__ = str(SOURCE)
exec(compile(SOURCE.read_bytes(), str(SOURCE), 'exec'), runner.__dict__)


class ProductManifestOrderTests(unittest.TestCase):
    def collect(self, root, observations):
        # Preserve the first real DEV failure's nonalphabetic fuzz order, while
        # using an ordinary collector fixture (no VM, clock or product process).
        manifest = json.loads((Path(__file__).parent / 'manifests/DEV-LOCAL-SEAL.json').read_bytes())
        case = manifest['case']
        del case['execution_profile'], case['local_execution']
        case['steps'] = ['vet', 'staticcheck']
        prereg = root / 'preregistered.json'
        prereg.write_text(json.dumps(manifest, ensure_ascii=False) + '\n', encoding='utf-8')
        original = prereg.read_bytes()
        self.assertEqual(list(case['fuzz']), ['fuzz-frame', 'fuzz-control'])
        analyzer_path = root / 'experiments/product/analyze.py'
        analyzer_path.parent.mkdir(parents=True)
        shutil.copyfile(SOURCE.with_name('analyze.py'), analyzer_path)
        actual_sibling = runner.sibling
        actual_analyze = runner.analyze

        def persisted():
            run = next((root / 'runs').iterdir())
            return json.loads((run / 'manifest.json').read_bytes())

        def git(*args):
            if args[0] in ('rev-parse', 'log'):
                return 'a' * 40
            if args[0] == 'ls-files':
                return 'experiments/product/analyze.py\npreregistered.json'
            raise AssertionError(args)

        def sibling(name):
            module = actual_sibling(name)
            self.assertEqual(name, 'vulndb_snapshot')
            actual_prepare = module.prepare

            def prepare(actual_case, env, run):
                observations.append(('prepare', actual_case, persisted()['case']))
                return actual_prepare(actual_case, env, run)

            module.prepare = prepare
            return module

        def command(argv, **kwargs):
            observations.append((argv[1], None, persisted()['case']))
            if argv[:2] == ['go', 'version']:
                return types.SimpleNamespace(stdout='go version go1.27.3 linux/amd64\n')
            if argv[:2] == ['go', 'env']:
                return types.SimpleNamespace(stdout=json.dumps({'GOOS': 'linux', 'GOARCH': 'amd64'}))
            raise AssertionError(argv)

        def step(name, argv, extra, timeout, is_json, raw, err):
            observations.append(('step', name, persisted()['case']))
            raw.write(json.dumps(dict(step=name, kind='start', command=argv)) + '\n')
            # Deliberately reverse alphabetic order in raw: never sort the rows.
            for line in ['z diagnostic', 'a diagnostic']:
                raw.write(json.dumps(dict(step=name, kind='line', text=line)) + '\n')
            return 0, False, 1

        def analyze(actual_case, rows, repeats):
            observations.append(('analyze', actual_case, persisted()['case']))
            return actual_analyze(actual_case, rows, repeats)

        with mock.patch.object(runner, 'ROOT', root), \
                mock.patch.object(runner, 'git', side_effect=git), \
                mock.patch.object(runner, 'workspace_dirty', return_value=False), \
                mock.patch.object(runner, 'sibling', side_effect=sibling), \
                mock.patch.object(runner.subprocess, 'run', side_effect=command), \
                mock.patch.object(runner, 'run_step', side_effect=step), \
                mock.patch.object(runner, 'analyze', side_effect=analyze), \
                mock.patch.object(runner.platform, 'system', return_value='Linux'), \
                mock.patch.object(runner.platform, 'release', return_value='collector-fixture'), \
                mock.patch.object(runner.platform, 'machine', return_value='x86_64'), \
                mock.patch.dict(runner.os.environ, {}, clear=True), \
                mock.patch.object(sys, 'argv', ['run.py', str(prereg), '--output-root', str(root/'runs'),
                                              '--work', str(root/'work')]), \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(runner.main(), 1)
        self.assertEqual(prereg.read_bytes(), original)
        run = next((root / 'runs').iterdir())
        stored = persisted()
        self.assertEqual(stored['preregistration']['sha256'], hashlib.sha256(original).hexdigest())
        self.assertEqual(stored['case'], case)  # Values, including array order, are unchanged.
        return run, stored

    def test_failed_summary_reproduces_from_persisted_manifest_and_cannot_be_primary(self):
        with tempfile.TemporaryDirectory() as tmp:
            run, stored = self.collect(Path(tmp), [])
            summary = json.loads((run / 'summary.json').read_bytes())
            rows = [json.loads(line) for line in (run / 'raw.jsonl').read_text(encoding='utf-8').splitlines()]
            self.assertEqual(runner.analyze(stored['case'], rows, stored['repeats']), summary)
            self.assertEqual(verify(run)['recomputation'], 'observed')
            self.assertEqual((summary['status'], summary['verdict']), ('observed', 'fail'))
            fuzz_problems = [p for p in summary['problems'] if p.startswith('fuzz-')]
            self.assertEqual([p.split(':', 1)[0] for p in fuzz_problems], ['fuzz-control', 'fuzz-frame'])
            with self.assertRaisesRegex(ValueError, 'passing verdict'):
                check_primary_result(stored, summary)
            self.assertEqual([r['step'] for r in rows if r['kind'] == 'start'], ['vet', 'staticcheck'])
            self.assertEqual([r['text'] for r in rows if r['kind'] == 'line'],
                             ['z diagnostic', 'a diagnostic', 'z diagnostic', 'a diagnostic'])

    def test_first_observation_and_analysis_share_the_stored_case(self):
        with tempfile.TemporaryDirectory() as tmp:
            observations = []
            self.collect(Path(tmp), observations)
            self.assertEqual([kind for kind, _, _ in observations],
                             ['prepare', 'version', 'env', 'step', 'step', 'analyze'])
            prepare_case = observations[0][1]
            self.assertIs(prepare_case, observations[-1][1])
            for kind, actual_case, saved_case in observations:
                self.assertEqual(list(prepare_case['fuzz']), list(saved_case['fuzz']), kind)
                if kind in ('prepare', 'analyze'):
                    self.assertEqual(list(actual_case), list(saved_case), kind)
                    self.assertEqual(list(actual_case['engines']), list(saved_case['engines']), kind)


if __name__ == '__main__':
    unittest.main()
