"""Retired documents may use fixed Git history; current evidence may not."""
import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from historical_documents import DOCUMENT_BASE, fingerprint, read_document


class HistoricalDocumentTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve()

    def test_existing_bytes_and_scoped_drift_stay_authoritative(self):
        p = self.root / 'docs/old.md'
        p.parent.mkdir()
        p.write_bytes(b'<!-- sdd-scope:x -->original<!-- /sdd-scope:x -->')
        with patch('historical_documents.subprocess.run') as run:
            self.assertEqual(fingerprint(self.root, 'docs/old.md#x'), hashlib.sha256(b'original').hexdigest())
            p.write_bytes(b'<!-- sdd-scope:x -->changed<!-- /sdd-scope:x -->')
            self.assertEqual(fingerprint(self.root, 'docs/old.md#x'), hashlib.sha256(b'changed').hexdigest())
            run.assert_not_called()

    def test_missing_retired_document_uses_only_fixed_commit(self):
        calls = []
        def run(argv, **kwargs):
            calls.append(argv)
            self.assertTrue(kwargs['check'])
            self.assertEqual(kwargs['timeout'], 10)
            data = str(self.root).encode() if argv[-1] == '--show-toplevel' else b'old bytes'
            return subprocess.CompletedProcess(argv, 0, stdout=data, stderr=b'')
        with patch('historical_documents.subprocess.run', side_effect=run):
            for name in ('docs/old.md', 'experiments/README.md', 'experiments/GATES.md',
                         'experiments/materials/README.md'):
                with self.subTest(name=name):
                    self.assertEqual(read_document(self.root, name), b'old bytes')
                    self.assertEqual(calls[-1][-3:], ['cat-file', 'blob', DOCUMENT_BASE + ':' + name])

    def test_missing_data_source_kept_docs_and_unsafe_paths_never_fallback(self):
        for name in ('experiments/runs/run/raw.jsonl', 'experiments/product/analyze.py',
                     'experiments/other.md', 'experiments/materials/README2.md',
                     'internal/a.go', 'docs/concept/loom-oi.md', '../docs/x',
                     '/docs/x', 'docs/../x', 'docs\\x', 'C:/x', 'docs//x'):
            with self.subTest(name=name), patch('historical_documents.subprocess.run') as run:
                with self.assertRaises(ValueError):read_document(self.root, name)
                run.assert_not_called()

    def test_unknown_history_or_wrong_repository_stays_missing(self):
        with patch('historical_documents.subprocess.run', side_effect=subprocess.CalledProcessError(128, ['git'])):
            with self.assertRaisesRegex(ValueError, 'Missing historical'):read_document(self.root, 'docs/missing.md')
        wrong = subprocess.CompletedProcess(['git'], 0, stdout=str(self.root.parent).encode(), stderr=b'')
        with patch('historical_documents.subprocess.run', return_value=wrong):
            with self.assertRaisesRegex(ValueError, 'repository root'):read_document(self.root, 'docs/missing.md')

    def test_real_retired_gate_scope_matches_unchanged_record(self):
        root = Path(__file__).resolve().parents[1]
        self.assertEqual(fingerprint(root, 'docs/architecture/p1/b1-protocol.md#p1-b1'),
                         '38ffa781917b48eded08d23ffc3252625edc72a956ed9f35490864432e95b63d')


if __name__ == '__main__':
    unittest.main()
