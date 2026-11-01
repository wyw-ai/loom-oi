"""Meaningful bypass/regression checks using disposable synthetic gate records."""
import copy
import hashlib
import tempfile
import unittest
from pathlib import Path
from check_gates import evaluate


class GateTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root/'review.md').write_text('SYNTHETIC TEST RECORD ONLY')
        v = {'criteria_passed':['contract'], 'reviewer':'test', 'subject_version':'synthetic',
             'completion':'complete','evidence_level':'Design','decision_sources':['synthetic human decision'],
             'environment':'temporary fixture', 'artifacts':[{'path':'review.md','sha256':hashlib.sha256((self.root/'review.md').read_bytes()).hexdigest()}]}
        self.data = {'gates':[{'id':'A','requires':[],'scope':'fixture','criteria':['contract'],'evidence_level':'Design',
                              'status':'Passed','validation':v},
                             {'id':'B','requires':['A'],'scope':'fixture','criteria':['contract'],'evidence_level':'Design',
                              'status':'Passed','validation':copy.deepcopy(v)}], 'stages':{'next':['B']}}

    def test_recorded_pass_has_full_dependency_chain(self):
        self.assertEqual(evaluate(self.data,self.root,'next'),([],[]))

    def test_nonpassing_dependency_cannot_be_hidden_by_downstream_pass(self):
        for state in ['Failed','Blocked','NotRun','Inconclusive']:
            with self.subTest(state=state):
                d=copy.deepcopy(self.data);d['gates'][0].update(status=state,blockers=['needs evidence'])
                errors,blocked=evaluate(d,self.root,'next')
                self.assertTrue(errors);self.assertTrue(blocked)

    def test_missing_evidence_cannot_pass(self):
        del self.data['gates'][0]['validation']
        self.assertTrue(evaluate(self.data,self.root,'next')[0])

    def test_modified_evidence_invalidates_record(self):
        (self.root/'review.md').write_text('modified')
        self.assertTrue(evaluate(self.data,self.root,'next')[0])

    def test_uncovered_criteria_cannot_pass(self):
        self.data['gates'][0]['criteria'].append('additional-contract')
        self.assertTrue(evaluate(self.data,self.root,'next')[0])

    def test_unknown_dependency_and_cycle_rejected(self):
        for dep in ['missing','B']:
            with self.subTest(dep=dep):
                d=copy.deepcopy(self.data);d['gates'][0]['requires']=[dep]
                self.assertTrue(evaluate(d,self.root,'next')[0])

    def test_no_stage_is_validation_only(self):
        for g in self.data['gates']:
            g.update(status='Blocked',blockers=['not implemented'])
        self.assertEqual(evaluate(self.data,self.root),([],[]))
        errors,blocked=evaluate(self.data,self.root,'next')
        self.assertFalse(errors);self.assertEqual(len(blocked),2)

    def test_unknown_stage_rejected(self):
        self.assertTrue(evaluate(self.data,self.root,'unknown')[0])

    def test_interrupted_closure_cannot_pass(self):
        self.data['gates'][0]['validation']['completion']='in_progress'
        self.assertTrue(evaluate(self.data,self.root,'next')[0])

    def test_mechanism_report_cannot_fill_implementation_gate(self):
        self.data['gates'][1]['evidence_level']='Implementation'
        self.data['gates'][1]['validation']['evidence_level']='Mechanism fixture'
        self.assertTrue(evaluate(self.data,self.root,'next')[0])

    def test_implementation_requires_completed_iteration(self):
        self.data['gates'][1]['evidence_level']='Implementation'
        self.data['gates'][1]['validation']['evidence_level']='Implementation'
        self.assertTrue(evaluate(self.data,self.root,'next')[0])


if __name__ == '__main__':
    unittest.main()
