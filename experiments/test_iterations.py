"""Regression checks for omitted evidence and stale/invalid closure records."""
import hashlib
import json
import shutil
import tempfile
import unittest
from pathlib import Path
from check_iterations import ROOT, check

class CoverageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        shutil.copytree(ROOT / 'experiments/runs', self.root / 'experiments/runs')
        shutil.copytree(ROOT / 'experiments/iterations', self.root / 'experiments/iterations')
        self.record = self.root / 'experiments/iterations/ITER-001.json'
        for record in (self.root / 'experiments/iterations').glob('*.json'):
            data = json.loads(record.read_text(encoding='utf-8'))
            for relative in {data['analysis'], *(x['path'] for x in data['artifacts'])}:
                p = self.root / relative
                p.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / relative, p)
    def tearDown(self):
        self.tmp.cleanup()
    def change(self, fn):
        d = json.loads(self.record.read_text(encoding='utf-8')); fn(d)
        self.record.write_text(json.dumps(d), encoding='utf-8')
    def test_complete(self):
        self.assertEqual(check(self.root)['pending_runs'], [])
    def test_unanalyzed_run_is_pending(self):
        source = next((self.root / 'experiments/runs').iterdir())
        shutil.copytree(source, source.parent / 'LAB-NEW')
        self.assertEqual(check(self.root)['pending_runs'], ['LAB-NEW'])
    def test_stale_fingerprint_rejected(self):
        self.change(lambda d: d['inputs'][0].update(sha256sums_sha256='0' * 64))
        with self.assertRaisesRegex(ValueError, 'input changed'): check(self.root)
    def test_missing_analysis_rejected(self):
        self.change(lambda d: d.update(analysis='docs/missing.md'))
        with self.assertRaisesRegex(ValueError, 'Missing'): check(self.root)
    def test_failed_run_cannot_be_primary(self):
        d = json.loads(self.record.read_text(encoding='utf-8'))
        item = next(x for x in d['inputs'] if x['use'] == 'primary')
        run = self.root / 'experiments/runs' / item['run']
        execution = json.loads((run / 'execution.json').read_text(encoding='utf-8'))
        execution['exit_code'] = 1
        (run / 'execution.json').write_text(json.dumps(execution))
        (run / 'summary.json').write_text(json.dumps({'status': 'inconclusive'}))
        (run / 'SHA256SUMS').write_text(''.join(
            hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n'
            for p in sorted(run.iterdir()) if p.name != 'SHA256SUMS'))
        item['sha256sums_sha256'] = hashlib.sha256((run / 'SHA256SUMS').read_bytes()).hexdigest()
        self.record.write_text(json.dumps(d))
        with self.assertRaisesRegex(ValueError, 'Inconclusive'): check(self.root)

if __name__ == '__main__': unittest.main()
