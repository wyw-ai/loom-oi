"""Checkers must give the same verdict on every OS: no locale text I/O, no native path keys."""
import ast
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
# Hash-pinned experiment sources (run.py, analyze.py, cases/) are excluded on purpose:
# changing them would break provenance checks of recorded runs.
CHECKERS = ['tools/check_structure.py', 'tools/check_sdd.py', 'experiments/check_gates.py',
            'experiments/check_iterations.py', 'experiments/check_materials.py', 'experiments/verify.py']


def violations(source):
    found = []
    for node in ast.walk(ast.parse(source)):
        if not isinstance(node, ast.Call):
            continue
        keywords = {k.arg for k in node.keywords}
        name = node.func.attr if isinstance(node.func, ast.Attribute) else getattr(node.func, 'id', '')
        if name in {'read_text', 'write_text'} and 'encoding' not in keywords:
            found.append(f'line {node.lineno}: {name}() without encoding')
        if name == 'write_text' and 'newline' not in keywords:
            found.append(f'line {node.lineno}: write_text() without newline')
        if name == 'open':
            mode = node.args[1] if len(node.args) > 1 and isinstance(node.func, ast.Name) else \
                node.args[0] if node.args and isinstance(node.func, ast.Attribute) else None
            text_mode = not (isinstance(mode, ast.Constant) and 'b' in str(mode.value))
            if text_mode and 'encoding' not in keywords:
                found.append(f'line {node.lineno}: text open() without encoding')
        if name == 'str' and node.args and isinstance(node.args[0], ast.Call) and \
                isinstance(node.args[0].func, ast.Attribute) and node.args[0].func.attr == 'relative_to':
            found.append(f'line {node.lineno}: str(relative_to()) gives OS-specific separators; use as_posix()')
    return found


class PortabilityTests(unittest.TestCase):
    def test_checkers_are_locale_and_separator_independent(self):
        for rel in CHECKERS:
            with self.subTest(checker=rel):
                self.assertEqual(violations((ROOT / rel).read_text(encoding='utf-8')), [])

    def test_detector_catches_the_original_defects(self):
        bad = "p.read_text()\nq.write_text(x, encoding='utf-8')\nopen(f)\nstr(p.relative_to(root))\n"
        self.assertEqual(len(violations(bad)), 4)
        good = "p.read_text(encoding='utf-8')\nq.write_text(x, encoding='utf-8', newline='\\n')\n" \
               "open(f, 'rb')\np.relative_to(root).as_posix()\n"
        self.assertEqual(violations(good), [])


if __name__ == '__main__':
    unittest.main()
