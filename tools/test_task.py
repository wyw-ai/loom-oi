import contextlib
import copy
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from check_task import validate, main, source_digest, source_files, documentation_digest


class TaskChecks(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for d in ['tools','spec/TASK-999-demo','docs']:
            (self.root/d).mkdir(parents=True)
        self.write('tools/a.py', 'answer = 42\n')
        self.write('tools/test_a.py', 'assert True\n')
        self.write('tools/coverage-policy.json', {'source_roots':['tools'],'omit':['tools/test_*.py']})
        self.write('spec/TASK-999-demo/README.md', '# Demo\n')
        self.write('spec/TASK-999-demo/review.md', '默认值42 → tools/a.py → 单元测试通过\n')
        digest = hashlib.sha256((self.root/'spec/TASK-999-demo/README.md').read_bytes()).hexdigest()
        fp = {'spec/TASK-999-demo/README.md':digest}
        req = {'id':'DISC-999-R1','status':'Accepted','source_ids':['S1'],'issues':[99],'checks':['V1'],'depends_on':[],'fingerprints':fp,'confirmation':{'author':'test-user','date':'2026-11-03','url':'https://github.com/wyw-ai/loom-oi/issues/99#issuecomment-1','scope':['DISC-999-R1'],'fingerprints':fp}}
        self.manifest = {'schema_version':1,'id':'DISC-999','topic':'alpha','analysis':'spec/TASK-999-demo/README.md','sources':[{'id':'S1','kind':'user_instruction','locator':'fixture'}],'review_issues':[99],'requirements':[req],'verifications':[{'id':'V1','kind':'document','status':'NotRun','criterion':'fixture behavior','evidence':[]}],'tasks':[{'id':'T1','requirements':['DISC-999-R1'],'checks':['V1'],'kind':'implementation','status':'Planned'}]}
        self.write('docs/manifest.json', self.manifest)
        self.task = {'schema_version':1,'id':'TASK-999','goal':'one goal','branch':'feature/TASK-999-demo','base_branch':'alpha','base_commit':'a'*40,'manifest':'docs/manifest.json','requirements':['DISC-999-R1'],'allowed_paths':['tools/*','spec/TASK-999-demo/*','docs/*'],'verification':{'status':'Passed','document_review':'spec/TASK-999-demo/review.md','reviewed_document_digest':documentation_digest(self.root, ['tools/a.py']),'reviewed_source_digest':source_digest(self.root),'source_digest':source_digest(self.root),'coverage_report':'spec/TASK-999-demo/coverage.json','suite':'unit','command':'unit test command'}}
        self.path = 'spec/TASK-999-demo/task.json'
        self.write(self.path, self.task)
        self.report = {'files':{'tools/a.py':{'summary':{'num_statements':100,'covered_lines':95}}},'totals':{'num_statements':100,'covered_lines':95}}
        self.write('spec/TASK-999-demo/coverage.json', self.report)

    def write(self, p, value):
        p = self.root/p; p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(value if isinstance(value,str) else json.dumps(value))

    def check(self, phase='review', changes=None, branch=None):
        return validate(self.task,self.root,self.path,branch or self.task['branch'],changes or ['tools/a.py'],phase)

    def test_draft_can_run_ci_before_test_results(self):
        self.task.pop('verification')
        self.assertEqual(self.check('draft'), [])
        self.assertTrue(self.check('review'))
        self.manifest['requirements'][0].update(status='Pending',confirmation=None)
        self.write('docs/manifest.json',self.manifest)
        self.assertTrue(self.check('draft'))

    def test_coverage_95_passes_94_fails_without_per_module_gate(self):
        self.assertEqual(self.check(), [])
        self.report['files']['tools/a.py']['summary']['covered_lines']=94
        self.report['totals']['covered_lines']=94
        self.write('spec/TASK-999-demo/coverage.json',self.report)
        self.assertTrue(any('below 95%' in e for e in self.check()))
        self.write('tools/b.py', 'other = 1\n')
        self.report['files']['tools/a.py']['summary']['covered_lines'] = 90
        self.report['files']['tools/b.py'] = {'summary': {'num_statements': 100, 'covered_lines': 100}}
        self.report['totals'] = {'num_statements': 200, 'covered_lines': 190}
        self.write('spec/TASK-999-demo/coverage.json', self.report)
        self.task['verification']['source_digest'] = source_digest(self.root)
        self.task['verification']['reviewed_source_digest'] = source_digest(self.root)
        self.assertEqual(self.check(), [])

    def test_missing_unloaded_file_report_fails(self):
        self.write('tools/unloaded.py','never_called = 1\n')
        self.task['verification']['source_digest']=source_digest(self.root)
        self.task['verification']['reviewed_source_digest']=source_digest(self.root)
        self.assertIn('file set', '\n'.join(self.check()))

    def test_source_test_or_policy_change_invalidates_evidence(self):
        self.write('tools/test_a.py','assert False\n')
        result='\n'.join(self.check())
        self.assertIn('Stale unit-test',result)
        self.assertIn('another source version',result)

    def test_wrong_branch_extra_task_and_unlisted_change(self):
        self.assertIn('Branch', '\n'.join(self.check(branch='feature/TASK-998-other')))
        self.assertIn('outside task', '\n'.join(self.check(changes=['unrelated.py'])))
        self.task['allowed_paths'].append('spec/*')
        self.assertIn('Another task', '\n'.join(self.check(changes=['spec/TASK-998-other/README.md'])))
        self.write('spec/TASK-999-second/task.json',{'id':'TASK-999'})
        self.assertIn('Duplicate task ID', '\n'.join(self.check()))

    def test_report_tampering_paths_and_zero_denominator(self):
        for n,c in [(0,0),(100,101),(100,-1),(100,True)]:
            self.report['files']['tools/a.py']['summary']={'num_statements':n,'covered_lines':c}
            self.report['totals']={'num_statements':n,'covered_lines':c}
            self.write('spec/TASK-999-demo/coverage.json',self.report)
            self.assertTrue(self.check())
        self.report['files']['tools/a.py']['summary']={'num_statements':100,'covered_lines':95}
        self.report['totals']={'num_statements':100,'covered_lines':100}
        self.write('spec/TASK-999-demo/coverage.json',self.report)
        self.assertIn('totals', '\n'.join(self.check()))
        self.report['files']['./tools/a.py']=self.report['files']['tools/a.py']
        self.write('spec/TASK-999-demo/coverage.json',self.report)
        self.assertIn('Duplicate coverage', '\n'.join(self.check()))
        self.task['verification']['coverage_report']='../outside.json'
        self.assertTrue(self.check())

    def test_document_task_requires_reason_and_meaningful_review(self):
        self.assertTrue(self.check(changes=['docs/README.md']))
        self.task['verification']['coverage_not_applicable_reason']='Markdown only'
        self.write('docs/README.md', '# Usage')
        self.task['verification']['reviewed_document_digest']=documentation_digest(self.root, ['docs/README.md'])
        self.assertEqual(self.check(changes=['docs/README.md']), [])
        self.write('spec/TASK-999-demo/review.md','')
        self.assertIn('empty','\n'.join(self.check()))
        self.task['verification']['status']='Failed'
        self.task['verification']['suite']='mixed'
        self.assertIn('has not passed','\n'.join(self.check()))
        self.assertIn('Unit-only','\n'.join(self.check()))

    def test_new_language_outside_policy_is_not_silently_covered(self):
        self.task['allowed_paths'].append('app/*')
        self.assertIn('outside this coverage policy','\n'.join(self.check(changes=['app/main.go'])))

    def test_invalid_identity_scope_policy_and_malformed_json(self):
        self.task.update(schema_version=99,id='wrong',goal='',base_commit='no')
        self.task['requirements']=[]
        result='\n'.join(self.check('wrong'))
        for fragment in ['Invalid task','own spec','goal','phase','no requirement']:
            self.assertIn(fragment,result)
        self.write('tools/coverage-policy.json',{'source_roots':['../outside'],'omit':[]})
        with self.assertRaises(ValueError):source_files(self.root)
        self.write('tools/coverage-policy.json',{'source_roots':['tools'],'omit':['*']})
        with self.assertRaises(ValueError):source_files(self.root)
        self.write('tools/coverage-policy.json','{')
        self.assertTrue(self.check())

    def test_real_git_cli_and_stale_base(self):
        def git(*a):
            return subprocess.check_output(['git','-C',str(self.root),*a],stderr=subprocess.DEVNULL,text=True).strip()
        git('init','-q','-b','alpha');git('config','user.name','Fixture');git('config','user.email','fixture@example.invalid')
        git('add','.');git('commit','-qm','baseline')
        self.task['base_commit']=git('rev-parse','HEAD')
        git('switch','-qc',self.task['branch'])
        self.write(self.path,self.task)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(main([self.path,'--phase','draft'],root=self.root),0)
            self.assertEqual(main([self.path,'--phase','draft','--branch','bad'],root=self.root),1)
            git('add','.');git('commit','-qm','task')
            git('update-ref','refs/heads/alpha',git('rev-parse','HEAD'))
            self.assertEqual(main([self.path],root=self.root),1)
            self.task['base_commit']='invalid';self.write(self.path,self.task)
            self.assertEqual(main([self.path],root=self.root),1)
            self.write(self.path,'{')
            self.assertEqual(main([self.path],root=self.root),1)

    def test_document_edit_after_review_is_rejected(self):
        self.write('docs/README.md', '# Default 30 seconds')
        changed=['tools/a.py','docs/README.md']
        self.task['verification']['reviewed_document_digest']=documentation_digest(self.root, changed)
        self.assertEqual(self.check(changes=changed), [])
        self.write('docs/README.md', '# Default 60 seconds')
        self.assertIn('reviewed documents changed', '\n'.join(self.check(changes=changed)))
        with self.assertRaises(ValueError):
            documentation_digest(self.root, ['../outside.md'])
