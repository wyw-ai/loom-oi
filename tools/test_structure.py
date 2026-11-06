import copy
import contextlib
import io
import json
import tempfile
from pathlib import Path
import unittest
from check_structure import validate, catalog_markdown, HEADINGS, main


class StructureTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'docs').mkdir()
        (self.root / 'kb').mkdir()
        (self.root / 'docs/a.md').write_text('# A\n\n<a id="scope"></a>\n## 范围\n', encoding='utf-8')
        (self.root / 'docs/b.md').write_text('# B\n\n[scope](a.md#scope)\n', encoding='utf-8')
        self.data = {'schema_version': 1, 'documents': [
            {'path': 'docs/a.md', 'role': 'specification', 'purpose': 'Shared contract', 'primary_for': 'contract'},
            {'path': 'docs/b.md', 'role': 'design', 'purpose': 'Usage', 'relations': [{'kind': 'depends-on', 'target': 'docs/a.md#scope', 'reason': 'Uses the shared contract'}]}]}
        self.generate()

    def generate(self):
        (self.root / 'docs/catalog.md').write_text(catalog_markdown(self.data), encoding='utf-8')

    def test_valid_and_idempotent(self):
        self.assertEqual(validate(self.root, self.data), [])
        self.assertEqual(catalog_markdown(self.data), catalog_markdown(copy.deepcopy(self.data)))

    def test_broken_anchor_and_unregistered_file(self):
        (self.root / 'docs/b.md').write_text('[missing](a.md#missing)\n', encoding='utf-8')
        (self.root / 'docs/c.md').write_text('# C\n', encoding='utf-8')
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
        (self.root / 'docs/catalog.md').write_text('manual edit', encoding='utf-8')
        (self.root / 'docs/b.md').write_text('[bad](../../../outside.md)', encoding='utf-8')
        result = '\n'.join(validate(self.root, self.data))
        self.assertIn('stale', result)
        self.assertIn('escapes', result)

    def test_issue_current_shape(self):
        body = '分类：技术 · 主题：alpha · 工作状态：评审中 · 成熟度：方案推演 · 采用：待确认 · 优先级：高（P1） · 评分：待定\n'
        body += '\n'.join('## ' + h + '\ntext\n' for h in HEADINGS)
        self.assertEqual(validate(self.root, self.data, [{'number': 3, 'body': body}]), [])
        english = body.replace('工作状态：评审中', '工作状态：Review')
        self.assertTrue(any('Chinese' in e for e in validate(self.root, self.data, [{'number': 3, 'body': english}])))
        bad = body + '\n## 本轮追加回应\nnew answer\n'
        self.assertTrue(any('six current' in e for e in validate(self.root, self.data, [{'number': 3, 'body': bad}])))

    def test_invalid_catalog_records_are_rejected(self):
        self.assertIn('Invalid', '\n'.join(validate(self.root, {'schema_version': 0})))
        self.data['documents'].append(copy.deepcopy(self.data['documents'][0]))
        self.data['documents'][1].update(path='docs/absent.md', role='wrong', purpose='')
        errors = '\n'.join(validate(self.root, self.data))
        for expected in ['Duplicate catalog', 'Missing/outside', 'invalid role']:
            self.assertIn(expected, errors)

    def test_task_links_and_issue_links_are_checked(self):
        task = self.root / 'spec/TASK/README.md'; task.parent.mkdir(parents=True)
        task.write_text('[unknown](missing.md)\n[external](https://example.org)\n', encoding='utf-8')
        self.assertIn('spec/TASK/README.md: missing', '\n'.join(validate(self.root, self.data)))
        task.write_text('[contract](../../docs/a.md#scope)', encoding='utf-8')
        self.assertEqual(validate(self.root, self.data), [])
        body = '分类：技术 · 主题：alpha · 工作状态：评审中 · 成熟度：方案推演 · 采用：待确认 · 优先级：高 · 评分：待定\n'
        body += '\n'.join('## ' + h + '\ntext\n' for h in HEADINGS)
        issue = {'title': '[alpha][技术] fixture', 'body': body + '\n[missing](docs/absent.md)'}
        self.assertIn('missing', '\n'.join(validate(self.root, self.data, [issue])))
        issue.update(title='[alpha][Technical] fixture', body='incomplete')
        errors = '\n'.join(validate(self.root, self.data, [issue]))
        self.assertIn('missing 分类', errors); self.assertIn('title category', errors)
        self.assertIn('JSON array', '\n'.join(validate(self.root, self.data, {})))

    def test_cli_regenerates_catalog_and_returns_failure_for_invalid_input(self):
        catalog = self.root / 'docs/catalog.json'
        catalog.write_text(json.dumps(self.data), encoding='utf-8')
        (self.root / 'docs/catalog.md').write_text('stale', encoding='utf-8')
        issues = self.root / 'issues.json'; issues.write_text('[]', encoding='utf-8')
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(main([], root=self.root), 1)
            self.assertEqual(main(['--write-index', '--issues', str(issues)], root=self.root), 0)
            self.assertEqual((self.root / 'docs/catalog.md').read_text(encoding='utf-8'), catalog_markdown(self.data))
            catalog.write_text('{', encoding='utf-8')
            self.assertEqual(main([], root=self.root), 1)


if __name__ == '__main__':
    unittest.main()

class ExtendedStructureTests(StructureTests):
    def test_spec_links_are_checked_without_catalog_duplication(self):
        (self.root/'spec/TASK-999-example').mkdir(parents=True)
        (self.root/'spec/TASK-999-example/README.md').write_text('[broken](missing.md)')
        self.assertTrue(any('spec/TASK-999' in x for x in validate(self.root,self.data)))
        (self.root/'spec/TASK-999-example/README.md').write_text('[exists](../../docs/a.md#scope)')
        self.assertEqual(validate(self.root,self.data),[])

    def test_invalid_catalog_inputs_and_paths(self):
        self.assertTrue(validate(self.root,{'documents':[]}))
        self.data['documents'].append(copy.deepcopy(self.data['documents'][0]))
        self.assertTrue(any('Duplicate catalog' in x for x in validate(self.root,self.data)))
        self.data['documents'][0].update(path='../outside.md',role='bad',purpose='')
        self.assertTrue(any('Missing/outside' in x for x in validate(self.root,self.data)))

    def test_links_prose_external_encoding_and_repeated_headings(self):
        from check_structure import anchors,links,local_target
        self.assertIn('a-1', anchors('# A\n# A\n'))
        self.assertEqual(links('```md\n[ignore](missing)\n```\n[x]: https://example.invalid'),['https://example.invalid'])
        self.assertIsNone(local_target(self.root,self.root/'docs/a.md','https://example.invalid'))
        (self.root/'docs/b.md').write_text('[local](a.md#%E8%8C%83%E5%9B%B4)')
        self.assertEqual(validate(self.root,self.data),[])
        (self.root/'raw.bin').write_bytes(b'\xff')
        (self.root/'docs/b.md').write_text('[bad](../raw.bin#x)')
        self.assertTrue(validate(self.root,self.data))

    def test_invalid_issue_shape_values_and_links(self):
        self.assertTrue(validate(self.root,self.data,{}))
        body='\n'.join('## '+h+'\ncontent\n' for h in HEADINGS)
        self.assertTrue(validate(self.root,self.data,[{'number':1,'title':'[alpha][Technical] English','body':body+'[bad](missing.md)'}]))

    def test_cli_paths_and_json_failure(self):
        import contextlib,io,json
        from check_structure import main
        p=self.root/'docs/catalog.json';p.write_text(json.dumps(self.data))
        issues=self.root/'issues.json';issues.write_text('[]')
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(main(['--write-index','--issues',str(issues)],root=self.root),0)
            (self.root/'docs/b.md').write_text('[missing](bad.md)')
            self.assertEqual(main([],root=self.root),1)
            p.write_text('{')
            self.assertEqual(main([],root=self.root),1)
