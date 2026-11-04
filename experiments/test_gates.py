"""Meaningful bypass/regression checks using disposable synthetic gate records."""
import copy
import hashlib
import contextlib
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path
from check_gates import evaluate, main
from check_sdd import fingerprint


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

    def test_chinese_blocker_with_legacy_redirected_codepage(self):
        for g in self.data['gates']:
            g.update(status='Blocked', blockers=['等待真实资源与原始证据'])
        directory = self.root / 'experiments'
        directory.mkdir()
        (directory / 'gates.json').write_text(json.dumps(self.data, ensure_ascii=False), encoding='utf-8')
        code = 'import sys; from check_gates import main; sys.exit(main(["--stage", "next"], root=sys.argv[1]))'
        for encoding in ('cp1252', 'ascii', 'gbk'):
            with self.subTest(encoding=encoding):
                env = dict(os.environ, PYTHONIOENCODING=encoding, PYTHONUTF8='0')
                result = subprocess.run([sys.executable, '-c', code, str(self.root)],
                                        cwd=Path(__file__).resolve().parent, env=env, capture_output=True)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn('等待真实资源与原始证据', result.stdout.decode('utf-8'))
                self.assertNotIn(b'UnicodeEncodeError', result.stderr)

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

    def test_scoped_design_evidence_ignores_status_but_not_contract_changes(self):
        p = self.root / 'review.md'
        p.write_text('status: pending\n<!-- sdd-scope:contract -->exact contract<!-- /sdd-scope:contract -->', encoding='utf-8')
        for g in self.data['gates']:
            g['validation']['artifacts'] = [{'path':'review.md#contract','sha256':fingerprint(self.root,'review.md#contract')}]
        self.assertEqual(evaluate(self.data,self.root,'next'),([],[]))
        p.write_text(p.read_text(encoding='utf-8').replace('pending', 'accepted'), encoding='utf-8')
        self.assertEqual(evaluate(self.data,self.root,'next'),([],[]))
        p.write_text(p.read_text(encoding='utf-8').replace('exact contract', 'different contract'), encoding='utf-8')
        self.assertTrue(evaluate(self.data,self.root,'next')[0])
        p.write_text('missing markers', encoding='utf-8')
        self.assertIn('invalid evidence path or scope', '\n'.join(evaluate(self.data,self.root,'next')[0]))

    def test_invalid_paths_do_not_bypass_evidence(self):
        for path in ['missing.md', '../outside.md', 'review.md#']:
            with self.subTest(path=path):
                self.data['gates'][0]['validation']['artifacts'][0]['path'] = path
                self.assertTrue(evaluate(self.data,self.root,'next')[0])

    def implementation_fixture(self, *, record_change=None, manifest_change=None, validation_change=None, summary_change=None):
        """Synthetic unit data; aggregate run verification is tested separately."""
        def save(path, value):
            p = self.root / path; p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(json.dumps(value), encoding='utf-8')
        run_name = 'SYNTHETIC-UNIT'
        run = self.root / 'experiments/runs' / run_name
        manifest = {'evidence_level':'Implementation','subject_version':'a'*40,'gate':'B',
                    'case':{'kind':'product-p2','criteria':{'contract':['TV-SYNTHETIC']}}}
        manifest.update(manifest_change or {})
        save(f'experiments/runs/{run_name}/manifest.json', manifest)
        save(f'experiments/runs/{run_name}/environment.json', {'isolation':'synthetic unit fixture'})
        summary = {'status':'observed','verdict':'pass','criteria':{'contract':'pass'},
                   'vectors':{'TV-SYNTHETIC':{'result':'pass'}}}
        summary.update(summary_change or {})
        save(f'experiments/runs/{run_name}/summary.json', summary)
        sums = ''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name+'\n' for p in sorted(run.iterdir()) if p.name != 'SHA256SUMS')
        (run/'SHA256SUMS').write_text(sums, encoding='utf-8')
        record = {'id':'ITER-999','completion':'complete','analysis':'review.md','artifacts':['review.md'],
                  'issues':[99],'inputs':[{'run':run_name,'use':'primary','sha256sums_sha256':hashlib.sha256((run/'SHA256SUMS').read_bytes()).hexdigest()}]}
        record.update(record_change or {})
        save('experiments/iterations/ITER-999.json', record)
        g = self.data['gates'][1]; g['evidence_level'] = 'Implementation'
        v = g['validation']; v.update(evidence_level='Implementation',iteration_id='ITER-999',run_ids=[run_name],subject_version='a'*40)
        paths = ['review.md','experiments/iterations/ITER-999.json',f'experiments/runs/{run_name}/environment.json']
        v['artifacts'] = [{'path':p,'sha256':hashlib.sha256((self.root/p).read_bytes()).hexdigest()} for p in paths]
        v.update(validation_change or {})
        return run

    def test_implementation_checks_bound_primary_run_and_environment(self):
        run = self.implementation_fixture()
        with patch('check_gates.verify_iteration_register', return_value={'pending_runs':[]}):
            self.assertEqual(evaluate(self.data,self.root,'next'),([],[]))
            (run/'manifest.json').write_text('{}', encoding='utf-8')
            self.assertIn('raw package integrity', '\n'.join(evaluate(self.data,self.root,'next')[0]))

    def test_aggregate_failure_and_unanalyzed_runs_prevent_pass(self):
        self.implementation_fixture()
        with patch('check_gates.verify_iteration_register', return_value={'pending_runs':['unreviewed']}):
            self.assertIn('unanalyzed', '\n'.join(evaluate(self.data,self.root,'next')[0]))
        with patch('check_gates.verify_iteration_register', side_effect=ValueError('bad aggregate')):
            self.assertIn('bad aggregate', '\n'.join(evaluate(self.data,self.root,'next')[0]))

    def test_incomplete_or_unbound_implementation_evidence_is_rejected(self):
        cases = [
            ({'record_change':{'id':'ITER-998'}}, 'not fingerprint-bound'),
            ({'record_change':{'completion':'in_progress'}}, 'closure incomplete'),
            ({'record_change':{'analysis':'../outside.md'}}, 'analysis not fingerprint-bound'),
            ({'validation_change':{'run_ids':[]}}, 'runs missing'),
            ({'validation_change':{'run_ids':['../outside']}}, 'not a primary'),
            ({'record_change':{'inputs':[{'run':'SYNTHETIC-UNIT','use':'primary','sha256sums_sha256':'0'*64}]}}, 'run fingerprint changed'),
            ({'manifest_change':{'evidence_level':'Mechanism'}}, 'level/version incompatible'),
        ]
        for kwargs, expected in cases:
            with self.subTest(expected=expected), patch('check_gates.verify_iteration_register', return_value={'pending_runs':[]}):
                self.implementation_fixture(**kwargs)
                self.assertIn(expected, '\n'.join(evaluate(self.data,self.root,'next')[0]))
        self.implementation_fixture()
        self.data['gates'][1]['validation']['artifacts'] = self.data['gates'][1]['validation']['artifacts'][:2]
        with patch('check_gates.verify_iteration_register', return_value={'pending_runs':[]}):
            self.assertIn('environment not fingerprint-bound', '\n'.join(evaluate(self.data,self.root,'next')[0]))

    def test_gate_requires_its_own_successful_criteria_and_vectors(self):
        # Aggregate verification is mocked only to isolate the gate binding.
        # Each fixture is fingerprint-consistent, so failures must come from
        # the actual recorded outcome rather than an incidental bad checksum.
        cases = [
            ({'validation_change':{'subject_version':'dirty:'+'a'*40},
              'manifest_change':{'subject_version':'dirty:'+'a'*40}}, 'clean commit SHA'),
            ({'summary_change':{'verdict':'fail'}}, 'passing verdict'),
            ({'summary_change':{'verdict':None}}, 'passing verdict'),
            ({'summary_change':{'status':'inconclusive'}}, 'passing verdict'),
            ({'manifest_change':{'gate':'OTHER'}}, 'different or missing gate'),
            ({'manifest_change':{'gate':None}}, 'different or missing gate'),
            ({'summary_change':{'criteria':{'contract':'fail'}}}, 'criteria do not satisfy'),
            ({'summary_change':{'criteria':{}}}, 'criteria do not satisfy'),
            ({'manifest_change':{'case':{'criteria':{'other':['TV-SYNTHETIC']}}}}, 'criteria do not satisfy'),
            ({'manifest_change':{'case':{'criteria':{'contract':[]}}}}, 'lacks passing vector'),
            ({'summary_change':{'vectors':{}}}, 'lacks passing vector'),
            ({'summary_change':{'vectors':{'TV-SYNTHETIC':{'result':'fail'}}}}, 'lacks passing vector'),
            ({'summary_change':{'vectors':{'TV-SYNTHETIC':{'result':'missing'}}}}, 'lacks passing vector'),
        ]
        for kwargs, expected in cases:
            with self.subTest(kwargs=kwargs), patch('check_gates.verify_iteration_register', return_value={'pending_runs':[]}):
                self.implementation_fixture(**kwargs)
                self.assertIn(expected, '\n'.join(evaluate(self.data, self.root, 'next')[0]))

    def test_invalid_register_cannot_look_like_phase_pass(self):
        self.assertTrue(evaluate({'gates':[],'stages':{}},self.root)[0])
        variants = [({'status':'unknown'}, 'unknown status'), ({'scope':''}, 'missing scope'),
                    ({'evidence_level':'wrong'}, 'invalid required evidence'),
                    ({'status':'Blocked','blockers':[]}, 'missing blocker')]
        for change, expected in variants:
            d=copy.deepcopy(self.data);d['gates'][0].update(change)
            self.assertIn(expected, '\n'.join(evaluate(d,self.root)[0]))
        self.data['stages']={}
        self.assertIn('Stage mapping missing', '\n'.join(evaluate(self.data,self.root)[0]))
        self.data['stages']={'bad':['unknown']}
        self.assertIn('missing/unknown gate', '\n'.join(evaluate(self.data,self.root)[0]))

    def test_cli_distinguishes_register_validation_block_and_pass(self):
        p=self.root/'experiments/gates.json';p.parent.mkdir(parents=True)
        with contextlib.redirect_stdout(io.StringIO()):
            p.write_text(json.dumps(self.data),encoding='utf-8')
            self.assertEqual(main([],root=self.root),0)
            self.assertEqual(main(['--stage','next'],root=self.root),0)
            for g in self.data['gates']:g.update(status='Blocked',blockers=['not yet'])
            p.write_text(json.dumps(self.data),encoding='utf-8')
            self.assertEqual(main(['--stage','next'],root=self.root),2)
            self.assertEqual(main(['--stage','unknown'],root=self.root),1)
            p.write_text('{',encoding='utf-8')
            self.assertEqual(main([],root=self.root),1)


if __name__ == '__main__':
    unittest.main()
