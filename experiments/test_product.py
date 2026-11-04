import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
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

    def test_rejects_unknown_input(self):
        with self.assertRaises(ValueError):
            analyze({'kind': 'probe'}, [], 1)
        with self.assertRaises(ValueError):
            analyze(CASE, [{'step': 'unit', 'kind': 'bogus'}], 1)


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


class PreregistrationTests(unittest.TestCase):
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


if __name__ == '__main__':
    unittest.main()
