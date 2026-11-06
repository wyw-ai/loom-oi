"""Regression checks for omitted evidence and stale/invalid closure records."""
import hashlib
import json
import shutil
import tempfile
import unittest
from pathlib import Path
from check_iterations import ROOT, check, check_primary_result
from historical_documents import read_document

class CoverageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        shutil.copytree(ROOT / 'experiments/runs', self.root / 'experiments/runs')
        shutil.copytree(ROOT / 'experiments/iterations', self.root / 'experiments/iterations')
        self.record = self.root / 'experiments/iterations/ITER-001.json'
        copied = set()
        for record in (self.root / 'experiments/iterations').glob('*.json'):
            data = json.loads(record.read_text(encoding='utf-8'))
            for relative in {data['analysis'], *(x['path'] for x in data['artifacts'])}:
                if relative in copied:
                    continue
                copied.add(relative)
                p = self.root / relative
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_bytes(read_document(ROOT, relative))
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

    def test_observed_failed_product_cannot_be_primary(self):
        # Use the archived, genuinely failed G1 run without changing any run
        # bytes: recomputation succeeds but it must not become primary proof.
        self.record = self.root / 'experiments/iterations/ITER-004.json'
        data = json.loads(self.record.read_text(encoding='utf-8'))
        failed = next(x for x in data['inputs'] if x['use'] == 'superseded')
        run = self.root / 'experiments/runs' / failed['run']
        summary = json.loads((run / 'summary.json').read_text(encoding='utf-8'))
        self.assertEqual((summary['status'], summary['verdict']), ('observed', 'fail'))
        failed['use'] = 'primary'
        self.record.write_text(json.dumps(data), encoding='utf-8')
        with self.assertRaisesRegex(ValueError, 'passing verdict'):
            check(self.root)


class PrimaryResultTests(unittest.TestCase):
    def test_product_requires_explicit_pass(self):
        for verdict in ['fail', None, '']:
            with self.subTest(verdict=verdict):
                summary = {'status': 'observed'}
                if verdict is not None:
                    summary['verdict'] = verdict
                with self.assertRaisesRegex(ValueError, 'passing verdict'):
                    check_primary_result({'case': {'kind': 'product-p2'}}, summary)
        check_primary_result({'case': {'kind': 'product-p1'}}, {'status': 'observed', 'verdict': 'pass'})

    def test_product_class_cannot_hide_missing_verdict(self):
        with self.assertRaisesRegex(ValueError, 'passing verdict'):
            check_primary_result({}, {'status': 'observed', 'evidence_class': 'product_implementation'})

    def test_legacy_mechanism_result_does_not_require_product_verdict(self):
        check_primary_result({'case': {'kind': 'mechanism'}}, {'status': 'observed'})
        check_primary_result({'case': 'legacy-lab-case'}, {'status': 'observed'})
        with self.assertRaisesRegex(ValueError, 'Inconclusive'):
            check_primary_result({}, {'status': 'inconclusive'})


if __name__ == '__main__': unittest.main()
