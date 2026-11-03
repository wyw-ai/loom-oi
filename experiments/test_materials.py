import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from check_materials import ROOT, validate


class MaterialTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        shutil.copytree(ROOT / 'experiments/materials', self.root / 'experiments/materials')

    def change(self, file, fn):
        p = self.root / 'experiments/materials' / file
        obj = json.loads(p.read_text(encoding='utf-8')); fn(obj)
        p.write_text(json.dumps(obj), encoding='utf-8')

    def test_plan_valid_but_not_runnable(self):
        self.assertEqual(validate(self.root), ([], []))
        errors, missing = validate(self.root, 'all')
        self.assertEqual(errors, [])
        self.assertEqual(len(missing), 16)

    def test_fake_results_rejected(self):
        self.change('r1-remote-exec/result-template.json', lambda x: x.update(status='Passed', observations=[1]))
        self.assertTrue(validate(self.root)[0])

    def test_missing_counterexample_rejected(self):
        self.change('r2-app-combinations/cases.json', lambda x: x[0].update(falsifier=''))
        self.assertTrue(validate(self.root)[0])

    def test_outside_path_rejected(self):
        self.change('r1-remote-exec/manifest.json', lambda x: x.update(inputs_file='/etc/passwd'))
        with self.assertRaises(ValueError): validate(self.root)

    def test_missing_resource_contrast_rejected(self):
        self.change('r2-app-combinations/inputs.json', lambda x: x.pop())
        self.assertTrue(validate(self.root)[0])

    def test_probe_preserves_arguments_without_execution(self):
        probe = ROOT / 'experiments/materials/fixtures/argv_probe.py'
        args = ['', '中 文', 'x\ny', 'a"b', '$(touch should-not-exist)', '$HOME', '--help']
        p = subprocess.run([sys.executable, str(probe), *args], cwd=self.root,
                           env={'LAB_FIXTURE_INSTANCE': 'test-only'}, capture_output=True, text=True, timeout=5, check=True)
        obj = json.loads(p.stdout)
        self.assertEqual(obj['argv'], args)
        self.assertEqual(obj['fixture_instance'], 'test-only')
        self.assertFalse((self.root / 'should-not-exist').exists())


if __name__ == '__main__':
    unittest.main()
