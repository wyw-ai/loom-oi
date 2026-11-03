"""Negative-path checks for the workflow validator; not product experiments."""
import copy
import hashlib
import tempfile
import unittest
from pathlib import Path
from check_sdd import validate


class SDDChecks(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'spec.md').write_text('review specification\n')
        fp = {'spec.md': hashlib.sha256((self.root / 'spec.md').read_bytes()).hexdigest()}
        self.data = {'schema_version': 1, 'id': 'DISC-999', 'topic': 'alpha', 'analysis': 'spec.md',
                     'sources': [{'id': 'D1', 'kind': 'discussion', 'locator': 'p1', 'sha256': 'a' * 64}],
                     'review_issues': [99],
                     'verifications': [{'id': 'V1', 'status': 'NotRun', 'kind': 'product', 'criterion': 'preserves argument boundaries', 'evidence': []}],
                     'requirements': [{'id': 'DISC-999-R1', 'status': 'Pending', 'source_ids': ['D1'], 'issues': [99], 'checks': ['V1'], 'depends_on': [], 'fingerprints': fp, 'confirmation': None}],
                     'tasks': [{'id': 'T1', 'requirements': ['DISC-999-R1'], 'checks': ['V1'], 'status': 'Planned', 'kind': 'implementation'}]}

    def approve(self):
        req = self.data['requirements'][0]
        req['status'] = 'Accepted'
        req['confirmation'] = {'author': 'fixture-reviewer', 'date': '2026-11-02', 'url': 'https://github.com/wyw-ai/loom-oi/issues/99#issuecomment-1', 'scope': [req['id']], 'fingerprints': copy.deepcopy(req['fingerprints'])}

    def test_review_allows_pending_but_implementation_refuses(self):
        self.assertEqual(validate(self.data, self.root), [])
        self.assertTrue(validate(self.data, self.root, 'implement'))

    def test_missing_issue_and_dangling_verification_fail(self):
        self.data['review_issues'] = []
        self.data['requirements'][0]['checks'] = ['missing']
        errors = validate(self.data, self.root)
        self.assertTrue(any('Issue' in x for x in errors))
        self.assertTrue(any('verification' in x for x in errors))

    def test_accepted_without_original_confirmation_fails(self):
        self.data['requirements'][0]['status'] = 'Accepted'
        self.assertTrue(validate(self.data, self.root, 'implement'))

    def test_structural_confirmation_and_scope(self):
        self.approve()
        self.assertEqual(validate(self.data, self.root, 'implement'), [])
        self.data['requirements'][0]['confirmation']['scope'] = []
        self.assertTrue(validate(self.data, self.root, 'implement'))

    def test_modified_spec_invalidates_old_approval(self):
        self.approve()
        (self.root / 'spec.md').write_text('different contract\n')
        self.assertTrue(validate(self.data, self.root, 'implement'))
        self.data['requirements'][0]['fingerprints']['spec.md'] = hashlib.sha256((self.root / 'spec.md').read_bytes()).hexdigest()
        self.assertTrue(validate(self.data, self.root, 'implement'))

    def test_unknown_selection_cannot_bypass_gate(self):
        self.assertTrue(validate(self.data, self.root, 'implement', ['unknown']))

    def test_cycle_and_unapproved_dependency_are_rejected(self):
        self.approve()
        req = copy.deepcopy(self.data['requirements'][0])
        req.update(id='DISC-999-R2', status='Pending', confirmation=None)
        self.data['requirements'].append(req)
        self.data['tasks'][0]['requirements'].append(req['id'])
        self.data['requirements'][0]['depends_on'] = [req['id']]
        self.assertTrue(validate(self.data, self.root, 'implement', ['DISC-999-R1']))
        req['depends_on'] = ['DISC-999-R1']
        self.assertTrue(any('cycle' in e for e in validate(self.data, self.root)))

    def test_passed_without_evidence_or_task_without_requirement_fails(self):
        self.data['verifications'][0]['status'] = 'Passed'
        self.data['tasks'][0]['requirements'] = []
        self.assertTrue(validate(self.data, self.root))

    def test_done_implementation_cannot_hide_pending_decision(self):
        self.data['tasks'][0]['status'] = 'Done'
        self.assertTrue(validate(self.data, self.root))


if __name__ == '__main__':
    unittest.main()

class ScopedSDDChecks(SDDChecks):
    def scope(self):
        from check_sdd import fingerprint
        (self.root / 'spec.md').write_text('intro\n<!-- sdd-scope:R1 -->\ncontract one\n<!-- /sdd-scope:R1 -->\nother section\n')
        req = self.data['requirements'][0]
        req['fingerprints'] = {'spec.md#R1': fingerprint(self.root, 'spec.md#R1')}
        self.data['schema_version'] = 2
        self.approve()
        return req

    def test_unrelated_text_preserves_scoped_approval(self):
        self.scope()
        p = self.root / 'spec.md'
        p.write_text(p.read_text().replace('other section', 'unrelated updated section'))
        self.assertEqual(validate(self.data, self.root, 'implement'), [])
        p.write_text(p.read_text().replace('contract one', 'changed behavior'))
        self.assertIn('Stale specification', '\n'.join(validate(self.data, self.root, 'implement')))

    def test_scope_missing_duplicate_empty_reversed(self):
        req = self.scope()
        for content in ['plain', '<!-- sdd-scope:R1 --><!-- /sdd-scope:R1 -->', '<!-- /sdd-scope:R1 -->text<!-- sdd-scope:R1 -->', '<!-- sdd-scope:R1 -->one<!-- sdd-scope:R1 -->two<!-- /sdd-scope:R1 -->']:
            with self.subTest(content=content):
                (self.root / 'spec.md').write_text(content)
                self.assertTrue(validate(self.data, self.root))

    def test_external_user_confirmation_is_canonical(self):
        req = self.scope()
        approval = req.pop('confirmation')
        approval.pop('url')
        approval['source'] = {'kind':'user_instruction','locator':'chat-request:3cce496f-7eee-48d4-bc53-48c1425fa72e','quote':'同步并修复上一轮评审中的问题'}
        import json
        p = self.root / 'approval.json'
        p.write_text(json.dumps(approval))
        req['confirmation_ref'] = 'approval.json'
        self.assertEqual(validate(self.data, self.root, 'implement'), [])
        for key, value in [('author', ''), ('date', 'not-a-date'), ('source', {'kind':'summary','locator':'local','quote':'AI thinks accepted'})]:
            altered = copy.deepcopy(approval); altered[key] = value
            p.write_text(json.dumps(altered))
            self.assertTrue(validate(self.data, self.root, 'implement'))
        approval['source'] = {'kind':'github_comment','locator':'https://github.com/wyw-ai/loom-oi/issues/99#issuecomment-1','quote':'同意这个范围'}
        p.write_text(json.dumps(approval))
        self.assertEqual(validate(self.data, self.root, 'implement'), [])
        req['confirmation'] = approval
        self.assertTrue(validate(self.data, self.root))

    def test_selection_excludes_unrelated_stale_or_pending_requirement(self):
        self.approve()
        req2 = copy.deepcopy(self.data['requirements'][0])
        req2.update(id='DISC-999-R2', status='Pending', confirmation=None, checks=['V2'], fingerprints={'missing.md':'b'*64})
        self.data['requirements'].append(req2)
        self.data['tasks'].append({'id':'T2','requirements':['DISC-999-R2'],'checks':['V2'],'kind':'implementation','status':'Planned'})
        self.data['verifications'].append({'id':'V2','kind':'product','status':'Passed','criterion':'separate task','evidence':['missing-result.md']})
        self.assertEqual(validate(self.data, self.root, 'implement', ['DISC-999-R1']), [])
        self.assertTrue(validate(self.data, self.root))
        self.data['requirements'][0]['depends_on'] = ['DISC-999-R2']
        self.assertTrue(validate(self.data, self.root, 'implement', ['DISC-999-R1']))

    def test_malformed_records_fail_closed(self):
        variants = [None, {}, {'schema_version':99}]
        for data in variants:
            self.assertTrue(validate(data, self.root))
        req = self.data['requirements'][0]
        req['fingerprints'] = {'../outside.md':'a'*64}
        self.assertTrue(validate(self.data, self.root))
        req['fingerprints'] = {}; req['depends_on'] = ['DISC-999-R1']
        self.assertTrue(any('Self dependency' in e for e in validate(self.data, self.root)))
        self.data['sources'].append(copy.deepcopy(self.data['sources'][0]))
        self.assertTrue(any('duplicate' in e for e in validate(self.data, self.root)))
        self.assertTrue(validate(self.data, self.root, 'review', ['DISC-999-R1']))

    def test_cli_success_failure_and_invalid_input(self):
        import contextlib, io, json
        from check_sdd import main
        p = self.root / 'manifest.json'; p.write_text(json.dumps(self.data))
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(main([str(p)], root=self.root), 0)
            self.assertEqual(main([str(p),'--phase','implement'], root=self.root), 1)
            with self.assertRaises(SystemExit):
                main([str(p),'--require','DISC-999-R1'], root=self.root)
            p.write_text('{')
            self.assertEqual(main([str(p)], root=self.root), 1)
