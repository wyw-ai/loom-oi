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
