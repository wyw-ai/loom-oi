"""Contract and failure tests. Synthetic fixture, not OI product measurements."""
import contextlib
import copy
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from issue_evolution import Store, check_append_only, main, parse_json, read_json, sha


class EvolutionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'source.txt').write_text('synthetic evidence: cache display only')
        self.file = self.root / 'kb/evolution/records/STAGE-TEST.json'
        self.file.parent.mkdir(parents=True)
        source = dict(id='SRC-TEST', url='https://example.invalid/source', snapshot='source.txt',
                      sha256=sha((self.root / 'source.txt').read_bytes()), group='one-origin', kind='repository')
        node = dict(id='A-OLD', kind='attempt', issue=14, title='旧目录缓存', text='只有展示缓存，不能离线授权新请求。',
                    sources=['SRC-TEST'], basis='inference', review='verified', mechanism='directory-display',
                    aliases=['节点名册副本', 'display cache'], conditions={'permission': 'online', 'scope': 'display'},
                    outcome='supported', reason='展示与新授权必须分离。', revisit='权限机制或场景改变时重评。')
        newer = copy.deepcopy(node); newer.update(id='A-NEW', title='新显示缓存', mechanism='new-display')
        other = copy.deepcopy(node); other.update(id='A-OTHER', title='容量推论', mechanism='capacity')
        self.data = dict(schema_version=1, stage='STAGE-TEST', recorded_on='2026-11-04', recorder='unit-test',
                         coverage='合成单测，非项目事实。', sources=[source], nodes=[node, newer, other], edges=[], withdrawals=[])
        self.current = dict(mechanism='directory-display', conditions={'permission': 'online', 'scope': 'display'}, purpose='evaluate')
        self.write()

    def write(self):
        self.file.write_text(json.dumps(self.data, ensure_ascii=False))

    def store(self):
        self.write()
        return Store(self.root)

    def edge(self, ident='E-ONE', start='A-OLD', end='A-NEW', relation='depends_on', review='verified'):
        return dict(id=ident, **{'from': start, 'to': end}, relation=relation, scope='synthetic scope',
                    reason='explicit test relation', sources=['SRC-TEST'], review=review)

    def cli(self, *args):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = main(['--root', str(self.root), *args])
        return code, out.getvalue(), err.getvalue()

    def test_valid_digest_and_readonly(self):
        before = self.file.read_bytes()
        a = Store(self.root)
        self.assertEqual(a.digest, Store(self.root).digest)
        self.assertEqual(a.envelope({})['records'], ['kb/evolution/records/STAGE-TEST.json'])
        self.assertIn('辅助', a.envelope({})['decision_authority'])
        self.assertEqual(before, self.file.read_bytes())
        self.data['nodes'][0]['text'] += '变化'; self.write()
        self.assertNotEqual(a.digest, Store(self.root).digest)

    def test_shape_and_types_reject_invalid_records(self):
        original = copy.deepcopy(self.data)
        edits = [lambda d: d.update(schema_version=True), lambda d: d.update(stage='bad'),
                 lambda d: d.update(stage='STAGE-OTHER'), lambda d: d.update(recorded_on='2026-99-01'),
                 lambda d: d.update(recorded_on='yesterday'), lambda d: d.update(recorder=''),
                 lambda d: d.update(nodes={}), lambda d: d['nodes'].append('bad'),
                 lambda d: d['nodes'].append(copy.deepcopy(d['nodes'][0])),
                 lambda d: d['nodes'][0].update(extra=True), lambda d: d['nodes'][0].update(kind='magic'),
                 lambda d: d['nodes'][0].update(issue=True), lambda d: d['nodes'][0].update(mechanism=4),
                 lambda d: d['nodes'][0].update(aliases=['x', 'x']), lambda d: d['nodes'][0].update(aliases='bad'),
                 lambda d: d['nodes'][0].update(conditions=[]), lambda d: d['nodes'][0].update(conditions={'x': 3}),
                 lambda d: d['nodes'][0].update(conditions={'': 'ok'}),
                 lambda d: d['nodes'][0].update(sources=['SRC-NONE']), lambda d: d['nodes'][0].update(review='magic')]
        for edit in edits:
            with self.subTest(edit=edit):
                self.data = copy.deepcopy(original); edit(self.data)
                with self.assertRaises(ValueError): self.store()

    def test_source_failures_and_unverified_url(self):
        source = self.data['sources'][0]
        source.update(snapshot=None, sha256=None)
        s = self.store()
        self.assertIn('未校验远端', ' '.join(s.warnings))
        self.assertEqual(s.compare('A-OLD', self.current)['suggestion'], 'review_evidence')
        source['sha256'] = 'a' * 64
        with self.assertRaises(ValueError): self.store()
        source.update(snapshot='/etc/passwd', sha256='a' * 64)
        with self.assertRaises(ValueError): self.store()
        source['snapshot'] = '../outside'
        with self.assertRaises(ValueError): self.store()
        source['snapshot'] = 'source.txt'; source['sha256'] = 'bad'
        with self.assertRaises(ValueError): self.store()
        source['sha256'] = 'a' * 64
        with self.assertRaises(ValueError): self.store()
        source['sha256'] = sha((self.root / 'source.txt').read_bytes()); source['url'] = 'file:///etc/passwd'
        with self.assertRaises(ValueError): self.store()
        source.update(url='https://example.invalid', kind='magic')
        with self.assertRaises(ValueError): self.store()

    def test_snapshot_tampering_and_symlink(self):
        (self.root / 'source.txt').write_text('modified')
        with self.assertRaisesRegex(ValueError, '哈希改变'): Store(self.root)
        outside = Path(self.temp.name).parent / (Path(self.temp.name).name + '-outside')
        outside.write_text('{}'); self.addCleanup(outside.unlink)
        self.file.unlink(); self.file.symlink_to(outside)
        with self.assertRaisesRegex(ValueError, '路径越界'): Store(self.root)

    def test_json_ambiguity_size_and_empty_store(self):
        for raw in [b'{"x": 1, "x": 2}', b'{"x": NaN}', b'{broken']:
            with self.subTest(raw=raw), self.assertRaises(ValueError): parse_json(raw)
        self.file.write_text(' ' * 2_000_001)
        with self.assertRaises(ValueError): read_json(self.file)
        with self.assertRaises(ValueError): Store(self.root)
        self.file.unlink()
        with self.assertRaisesRegex(ValueError, '没有演进记录'): Store(self.root)

    def test_claims_do_not_become_decisions_or_measurements(self):
        n = self.data['nodes'][0]; n['outcome'] = 'accepted'
        with self.assertRaises(ValueError): self.store()
        n['basis'] = 'human_decision'
        with self.assertRaises(ValueError): self.store()
        self.data['sources'][0]['kind'] = 'human'
        self.assertEqual(self.store().compare('A-OLD', self.current)['suggestion'], 'human_review')
        n.update(kind='evidence', basis='measured', outcome='supported')
        with self.assertRaises(ValueError): self.store()
        self.data['sources'][0]['kind'] = 'experiment'
        self.assertEqual(self.store().nodes['A-OLD']['basis'], 'measured')

    def test_source_groups_not_citation_counts(self):
        other = copy.deepcopy(self.data['sources'][0]); other['id'] = 'SRC-COPY'
        self.data['sources'].append(other); self.data['nodes'][0]['sources'].append('SRC-COPY'); self.write()
        code, out, _ = self.cli('validate')
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out)['result']['source_groups'], 1)

    def test_relation_validation_cycles_and_withdrawal(self):
        for changes in [{'to': 'MISSING'}, {'relation': 'magic'}, {'to': 'A-OLD'}, {'reason': ''}]:
            with self.subTest(changes=changes):
                self.data['edges'] = [dict(self.edge(), **changes)]
                with self.assertRaises(ValueError): self.store()
        for relation in ['depends_on', 'supersedes']:
            self.data['edges'] = [self.edge(relation=relation), self.edge('E-TWO', 'A-NEW', 'A-OLD', relation)]
            with self.assertRaisesRegex(ValueError, '循环'): self.store()
        self.data['edges'] = [self.edge(relation='related'), self.edge('E-TWO', 'A-NEW', 'A-OLD', 'related')]
        self.assertEqual(len(self.store().active), 2)
        self.data['edges'] = [self.edge()]
        self.data['withdrawals'] = [dict(id='W-ONE', edge='NO-EDGE', reason='correction', sources=['SRC-TEST'], review='verified')]
        with self.assertRaises(ValueError): self.store()
        self.data['withdrawals'][0]['edge'] = 'E-ONE'
        self.assertEqual(self.store().walk('A-OLD', 'impact')['paths'], [])
        self.data['withdrawals'][0]['review'] = 'pending'
        self.assertEqual(len(self.store().walk('A-OLD', 'impact')['paths']), 1)

    def test_search_alias_mechanism_short_chinese_and_limits(self):
        s = Store(self.root)
        self.assertEqual(s.search('节点名册副本')['matches'][0]['match'], '登记别名')
        self.assertEqual(s.search('directory-display')['matches'][0]['match'], '机制标识')
        self.assertTrue(s.search('目录')['matches'])
        self.assertTrue(s.search('display cache')['matches'])
        self.assertEqual(s.search('neverexistingword')['matches'], [])
        self.assertEqual(s.search('目录', issue=99)['matches'], [])
        self.assertTrue(s.search('展示', limit=1)['truncated'])
        for query, limit in [('', 1), ('x', 0), ('x', 101)]:
            with self.assertRaises(ValueError): s.search(query, limit=limit)

    def test_compare_all_nonblocking_outcomes(self):
        s = Store(self.root)
        self.assertEqual(s.compare('A-OLD', self.current)['suggestion'], 'reuse_prior_reasoning')
        c = copy.deepcopy(self.current); c['conditions']['permission'] = 'fresh-signed-lease'
        self.assertEqual(s.compare('A-OLD', c)['suggestion'], 'reevaluate')
        c['conditions']['permission'] = None
        self.assertEqual(s.compare('A-OLD', c)['suggestion'], 'insufficient_context')
        c['conditions'].pop('permission')
        self.assertTrue(s.compare('A-OLD', c)['unknown'])
        c = dict(self.current, purpose='replicate')
        self.assertEqual(s.compare('A-OLD', c)['suggestion'], 'replicate_with_plan')
        c = dict(self.current, mechanism='execution-authorization')
        self.assertEqual(s.compare('A-OLD', c)['suggestion'], 'different_mechanism')
        self.data['nodes'][0]['review'] = 'pending'
        self.assertEqual(self.store().compare('A-OLD', self.current)['suggestion'], 'review_evidence')
        self.data['nodes'][0].update(review='verified', outcome='deferred')
        self.assertEqual(self.store().compare('A-OLD', self.current)['suggestion'], 'continue_evidence')
        self.data['nodes'][0].update(outcome='supported', conditions={})
        self.assertEqual(self.store().compare('A-OLD', self.current)['suggestion'], 'insufficient_context')
        self.data['nodes'][0]['mechanism'] = None
        self.assertEqual(self.store().compare('A-OLD', self.current)['suggestion'], 'different_mechanism')

    def test_invalid_comparison_never_guesses(self):
        s = Store(self.root)
        for c in [dict(self.current, purpose='reject'), dict(self.current, mechanism=''), {}, dict(self.current, conditions={'x': ''})]:
            with self.assertRaises(ValueError): s.compare('A-OLD', c)
        with self.assertRaises(ValueError): s.compare('NO-NODE', self.current)

    def test_unknown_history_allows_bounded_new_conditions(self):
        self.data['nodes'][0]['conditions'] = {'permission': None, 'scope': None}
        s = self.store()
        unknown = copy.deepcopy(self.current)
        unknown['conditions'] = {'permission': None, 'scope': None}
        result = s.compare('A-OLD', unknown)
        self.assertEqual(result['suggestion'], 'insufficient_context')
        self.assertEqual(len(result['unknown']), 2)
        clarified = s.compare('A-OLD', self.current)
        self.assertEqual(clarified['suggestion'], 'reevaluate')
        self.assertEqual(clarified['same'], [])
        self.assertEqual([c['kind'] for c in clarified['changed']], ['newly_specified'] * 2)
        mixed = copy.deepcopy(self.current); mixed['conditions']['scope'] = None
        self.assertEqual(s.compare('A-OLD', mixed)['suggestion'], 'insufficient_context')

    def test_trace_impact_and_ancestor_conflict(self):
        self.data['edges'] = [self.edge(), self.edge('E-TWO', 'A-NEW', 'A-OTHER', 'supports'),
                              self.edge('E-RELATED', 'A-OTHER', 'A-OLD', 'related')]
        s = self.store()
        paths = s.walk('A-OLD', 'impact')['paths']
        self.assertEqual([p['node'] for p in paths], ['A-NEW', 'A-OTHER'])
        self.assertEqual(paths[1]['edges'], ['E-ONE', 'E-TWO'])
        self.assertTrue(s.walk('A-OLD', 'impact', 1)['truncated'])
        self.assertEqual(len(s.walk('A-OTHER')['paths']), 2)
        for mode, depth in [('wrong', 8), ('trace', 0), ('trace', 51)]:
            with self.assertRaises(ValueError): s.walk('A-OLD', mode, depth)
        self.data['edges'].append(self.edge('E-CONFLICT', 'A-OTHER', 'A-OLD', 'contradicts'))
        self.assertIn('E-CONFLICT', self.store().concerns('A-NEW')['conflicts'])
        self.data['edges'][-1]['review'] = 'pending'
        self.assertEqual(self.store().concerns('A-NEW')['conflicts'], [])
        self.data['nodes'][0]['review'] = 'pending'
        self.assertIn('A-OLD', self.store().concerns('A-NEW')['unverified_context'])

    def test_fork_and_withdrawn_correction(self):
        self.data['edges'] = [self.edge(relation='supersedes'), self.edge('E-TWO', 'A-OLD', 'A-OTHER', 'supersedes')]
        s = self.store()
        self.assertEqual(s.compare('A-OLD', self.current)['suggestion'], 'review_evidence')
        self.assertEqual(len(s.concerns('A-OLD')['replacements']), 2)
        self.assertEqual(s.nodes['A-OLD']['outcome'], 'supported')

    def test_compare_does_not_hide_truncated_ancestry(self):
        previous = 'A-OLD'
        for i in range(52):
            node = copy.deepcopy(self.data['nodes'][0]); node['id'] = 'A-LINK-' + str(i)
            self.data['nodes'].append(node)
            self.data['edges'].append(self.edge('E-LINK-' + str(i), node['id'], previous))
            previous = node['id']
        result = self.store().compare('A-OLD', self.current)
        self.assertTrue(result['trace_truncated'])
        self.assertEqual(result['suggestion'], 'review_evidence')

    def test_graph_sanitized_scoped_and_readonly(self):
        self.data['nodes'][0]['title'] = '缓存\" --> X [坏] ` <script>\n'
        self.data['nodes'][2]['issue'] = 15
        self.data['edges'] = [self.edge(), self.edge('E-TWO', 'A-NEW', 'A-OTHER')]
        s = self.store(); graph = s.graph(14)
        self.assertIn('flowchart TD', graph); self.assertIn('前提', graph)
        self.assertNotIn('<script>', graph); self.assertNotIn('容量推论', graph)
        self.assertIn(s.digest, graph)
        with self.assertRaises(ValueError): s.graph(99)

    def test_cli_every_command_and_errors(self):
        current = self.root / 'current.json'; current.write_text(json.dumps(self.current))
        for argv in [('validate',), ('search', '目录'), ('compare', 'A-OLD', str(current)),
                     ('trace', 'A-OLD'), ('impact', 'A-OLD'), ('graph', '--issue', '14')]:
            code, out, err = self.cli(*argv)
            self.assertEqual((code, err), (0, '')); self.assertTrue(out)
        code, _, err = self.cli('compare', 'A-OLD', str(current) + '.absent')
        self.assertEqual(code, 1); self.assertIn('error', err)
        with patch('issue_evolution.check_append_only', return_value={'preserved_files': 1}):
            code, out, _ = self.cli('validate', '--base', 'HEAD')
            self.assertEqual(json.loads(out)['result']['history']['preserved_files'], 1)
            self.assertEqual(code, 0)

    def test_git_append_only_preserves_bytes_not_just_ids(self):
        def git(*args):
            return subprocess.check_output(['git', '-C', str(self.root), *args], stderr=subprocess.DEVNULL)
        git('init'); git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'baseline')
        self.assertEqual(check_append_only(self.root, 'HEAD')['preserved_files'], 1)
        with self.assertRaises(ValueError): check_append_only(self.root, '--not-a-revision')
        original = self.file.read_bytes(); self.file.write_bytes(original + b' ')
        with self.assertRaisesRegex(ValueError, '修改或删除'): check_append_only(self.root, 'HEAD')
        self.file.unlink()
        with self.assertRaises(ValueError): check_append_only(self.root, 'HEAD')


if __name__ == '__main__':
    unittest.main()
