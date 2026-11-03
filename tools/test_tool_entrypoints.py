"""Exercise existing validator CLI contracts in isolated repositories."""
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import check_sdd
import check_structure
import test_sdd
import test_structure


class ValidatorEntrypoints(unittest.TestCase):
    def run_main(self, module, root, *args):
        out = io.StringIO()
        with patch.object(module, '__file__', str(root / 'tools' / 'check.py')), \
             patch('sys.argv', ['check.py', *args]), contextlib.redirect_stdout(out):
            code = module.main()
        return code, out.getvalue()

    def test_sdd_review_and_implementation_exit_codes(self):
        case = test_sdd.SDDChecks(); case.setUp(); self.addCleanup(case.doCleanups)
        path = case.root / 'manifest.json'; path.write_text(json.dumps(case.data))
        code, out = self.run_main(check_sdd, case.root, str(path))
        self.assertEqual(code, 0); self.assertIn('record checks only', out)
        code, out = self.run_main(check_sdd, case.root, str(path), '--phase', 'implement')
        self.assertEqual(code, 1); self.assertIn('FAIL', out)

    def test_sdd_invalid_manifest_and_invalid_phase_arguments(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d); path = root / 'bad.json'; path.write_text('{}')
            code, out = self.run_main(check_sdd, root, str(path))
            self.assertEqual(code, 1); self.assertIn('Invalid manifest', out)
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as exc:
                self.run_main(check_sdd, root, str(path), '--require', 'DISC-999-R1')
            self.assertEqual(exc.exception.code, 2)

    def test_structure_generation_and_failure_exit_codes(self):
        case = test_structure.StructureTests(); case.setUp(); self.addCleanup(case.doCleanups)
        catalog = case.root / 'docs/catalog.json'; catalog.write_text(json.dumps(case.data))
        code, out = self.run_main(check_structure, case.root, '--write-index')
        self.assertEqual(code, 0); self.assertIn('PASS', out)
        (case.root / 'docs/a.md').unlink()
        code, out = self.run_main(check_structure, case.root)
        self.assertEqual(code, 1); self.assertIn('Missing', out)
        catalog.write_text('{')
        code, out = self.run_main(check_structure, case.root)
        self.assertEqual(code, 1); self.assertIn('Invalid input', out)
