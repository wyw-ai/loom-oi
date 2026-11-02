import copy
import tempfile
from pathlib import Path
import unittest
from check_structure import validate, catalog_markdown, HEADINGS


class StructureTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'docs').mkdir()
        (self.root / 'kb').mkdir()
        (self.root / 'docs/a.md').write_text('# A\n\n<a id="scope"></a>\n## 范围\n')
        (self.root / 'docs/b.md').write_text('# B\n\n[scope](a.md#scope)\n')
        self.data = {'schema_version': 1, 'documents': [
            {'path': 'docs/a.md', 'role': 'specification', 'purpose': 'Shared contract', 'primary_for': 'contract'},
            {'path': 'docs/b.md', 'role': 'design', 'purpose': 'Usage', 'relations': [{'kind': 'depends-on', 'target': 'docs/a.md#scope', 'reason': 'Uses the shared contract'}]}]}
        self.generate()

    def generate(self):
        (self.root / 'docs/catalog.md').write_text(catalog_markdown(self.data))

    def test_valid_and_idempotent(self):
        self.assertEqual(validate(self.root, self.data), [])
        self.assertEqual(catalog_markdown(self.data), catalog_markdown(copy.deepcopy(self.data)))

    def test_broken_anchor_and_unregistered_file(self):
        (self.root / 'docs/b.md').write_text('[missing](a.md#missing)\n')
        (self.root / 'docs/c.md').write_text('# C\n')
        result = '\n'.join(validate(self.root, self.data))
        self.assertIn('missing anchor', result)
        self.assertIn('coverage mismatch', result)

    def test_duplicate_authority_and_cycle(self):
        self.data['documents'][1]['primary_for'] = 'contract'
        self.data['documents'][0]['relations'] = [{'kind': 'depends-on', 'target': 'docs/b.md', 'reason': 'Deliberate invalid cycle'}]
        self.generate()
        result = '\n'.join(validate(self.root, self.data))
        self.assertIn('Duplicate primary', result)
        self.assertIn('Dependency cycle', result)

    def test_relation_requires_reason(self):
        self.data['documents'][1]['relations'][0]['reason'] = ''
        self.assertTrue(any('unexplained' in e for e in validate(self.root, self.data)))

    def test_stale_index_and_escape(self):
        (self.root / 'docs/catalog.md').write_text('manual edit')
        (self.root / 'docs/b.md').write_text('[bad](../../../outside.md)')
        result = '\n'.join(validate(self.root, self.data))
        self.assertIn('stale', result)
        self.assertIn('escapes', result)

    def test_issue_current_shape(self):
        body = 'Type: Technical Topic: alpha Work: Review Maturity: Walkthrough Decision: Pending Priority: P1 I/U/N/E/C: TBD\n'
        body += '\n'.join('## ' + h + '\ntext\n' for h in HEADINGS)
        self.assertEqual(validate(self.root, self.data, [{'number': 3, 'body': body}]), [])
        bad = body + '\n## 本轮追加回应\nnew answer\n'
        self.assertTrue(any('six current' in e for e in validate(self.root, self.data, [{'number': 3, 'body': bad}])))


if __name__ == '__main__':
    unittest.main()
