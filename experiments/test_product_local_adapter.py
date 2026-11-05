"""Local collector/receipt tests: no VM, clock setter, sudo or Go execution."""
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


def load(name,path):
    spec=importlib.util.spec_from_file_location(name,path)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module


HERE=Path(__file__).parent
base=load('local_adapter_base',HERE/'test_product_g2_integration.py')
main=base.main
runner=load('local_adapter_runner',HERE/'product/run.py')
local=load('local_adapter_environment',HERE/'product/local_execution.py')
verify=base.verify


def fixture():
    case,raw=base.fixture()
    attestation=dict(schema=local.ATTESTATION_SCHEMA,run_id='local-11111111-1111-1111-1111-111111111111',
        source_sha=base.SOURCE,boot_id='22222222-2222-2222-2222-222222222222',
        vm_uuid='33333333-3333-3333-3333-333333333333',
        vm_serial='loom-oi-clock-33333333-3333-3333-3333-333333333333',
        runner_uid=1001,runner_gid=1001,network_mode='isolated',host_shares=False,
        **{key:'a'*64 for key in local.HASH_KEYS})
    identity=local.identity(attestation,'e'*64)
    case.update(execution_profile=local.PROFILE,
                local_execution={k:attestation[k] for k in main.LOCAL_MANIFEST_KEYS})
    fact=dict(attestation=attestation,attestation_sha256=local.digest(attestation))
    raw.insert(0,dict(step='env',kind='fact',name='local_execution',value=fact))
    prepare=next(r['event'] for r in raw if r['kind']=='clock_preparation')
    prepare.pop('workflow_ref');prepare.update(identity,schema='g2-clock-preparation-v2',
        runner_environment=local.PROFILE,attestation=attestation)
    result=next(r['event'] for r in raw if r['kind']=='clock_result')
    result.update(identity,schema='g2-clock-result-v2')
    audit=[r['event'] for r in raw if r['kind']=='clock_guard']
    for event in audit:
        event.pop('workflow_ref');event.update(identity)
    result['files']['guard/guard.jsonl']=hashlib.sha256(''.join(json.dumps(e,sort_keys=True)+'\n' for e in audit).encode()).hexdigest()
    request=main._local_supervision_request(identity,0,result['files'])
    receipt=dict(schema='loom-oi-local-supervisor-v1',
        **{k:request[k] for k in main.LOCAL_RECEIPT_IDENTITY},request_sha256=local.digest(request),
        wrapper_exit_code=0,recovery_exit_code=0,test_group_closed=True,files=dict(request['files']),
        host_started_monotonic_ns=10,host_completed_monotonic_ns=20,errors=[])
    i=next(i for i,r in enumerate(raw) if r['kind']=='clock_result')
    raw.insert(i+1,dict(step='clock',kind='local_supervisor',event=receipt))
    return case,raw,attestation,prepare,request,receipt


class LocalCollectorTests(unittest.TestCase):
    def analyze(self,case,raw):
        original=main._sibling
        def child(name):
            return original(name) if name=='local_execution.py' else base.tiny_child(name=='g2_clock_analyze.py')
        with patch.object(main,'_sibling',side_effect=child):
            return main.analyze(case,raw,1)

    def test_exact_local_source_environment_and_independent_receipt(self):
        case,raw,*_=fixture()
        summary=self.analyze(case,raw)
        self.assertEqual(summary['verdict'],'pass',summary['problems'])

    def test_explicit_manifest_profile_is_closed_and_hosted_stays_separate(self):
        for value in (None,False,'','unknown','github-hosted'):
            case,raw,*_=fixture();case['execution_profile']=value
            with self.subTest(value=value),self.assertRaises(ValueError):self.analyze(case,raw)
        for field in ('execution_profile','local_execution'):
            case,raw,*_=fixture();case.pop(field)
            with self.subTest(field=field),self.assertRaises(ValueError):self.analyze(case,raw)
        case,raw,*_=fixture();case.pop('execution_profile');case.pop('local_execution')
        with self.assertRaises(ValueError):self.analyze(case,raw)
        case,raw=base.fixture()
        self.assertEqual(self.analyze(case,raw)['verdict'],'pass')

    def test_receipt_provenance_recovery_and_order_cannot_be_forged(self):
        for change in ('missing','duplicate','before_result','foreign_vm','foreign_run','foreign_boot',
                       'foreign_nonce','foreign_source','foreign_request','foreign_files','failed_wrapper',
                       'failed_restore','orphan','error','bool_time','reversed_time','extra_key'):
            case,raw,_,_,_,receipt=fixture()
            line=next(r for r in raw if r['kind']=='local_supervisor')
            if change=='missing':raw.remove(line)
            elif change=='duplicate':raw.insert(raw.index(line),copy.deepcopy(line))
            elif change=='before_result':raw.remove(line);raw.insert(next(i for i,r in enumerate(raw) if r['kind']=='clock_result'),line)
            elif change in ('foreign_vm','foreign_run','foreign_boot','foreign_nonce','foreign_source'):
                key={'foreign_vm':'vm_uuid','foreign_run':'run_id','foreign_boot':'boot_id','foreign_nonce':'nonce_sha256','foreign_source':'source_sha'}[change]
                receipt[key]='bad'
            elif change=='foreign_request':receipt['request_sha256']='f'*64
            elif change=='foreign_files':receipt['files']['raw.jsonl']='f'*64
            elif change=='failed_wrapper':receipt['wrapper_exit_code']=1
            elif change=='failed_restore':receipt['recovery_exit_code']=1
            elif change=='orphan':receipt['test_group_closed']=False
            elif change=='error':receipt['errors']=['cleanup failed']
            elif change=='bool_time':receipt['host_started_monotonic_ns']=True
            elif change=='reversed_time':receipt['host_completed_monotonic_ns']=9
            else:receipt['unverified_override']=True
            with self.subTest(change=change):self.assertEqual(self.analyze(case,raw)['verdict'],'fail')

    def test_attestation_fact_and_preparation_cannot_disagree(self):
        for change in ('missing_fact','duplicate_fact','wrong_fact_step','wrong_manifest','wrong_attestation',
                       'hosted_prepare','workflow_injected','wrong_uid','wrong_digest','wrong_result'):
            case,raw,attestation,prepare,_,_=fixture()
            fact=next(r for r in raw if r.get('name')=='local_execution')
            if change=='missing_fact':raw.remove(fact)
            elif change=='duplicate_fact':raw.append(copy.deepcopy(fact))
            elif change=='wrong_fact_step':fact['step']='clock'
            elif change=='wrong_manifest':case['local_execution']['launch_sha256']='f'*64
            elif change=='wrong_attestation':prepare['attestation']=dict(attestation,host_shares=True)
            elif change=='hosted_prepare':prepare['schema']='g2-clock-preparation-v1'
            elif change=='workflow_injected':prepare['workflow_ref']='forged'
            elif change=='wrong_uid':prepare['runner_uid']=0
            elif change=='wrong_digest':prepare['attestation_sha256']='f'*64
            else:next(r['event'] for r in raw if r['kind']=='clock_result')['boot_id']='bad'
            with self.subTest(change=change):self.assertEqual(self.analyze(case,raw)['verdict'],'fail')

    def test_ordinary_local_environment_does_not_invent_clock_receipt(self):
        case,raw=base.fixture(clock=False)
        full,_,attestation,*_=fixture()
        case.update(execution_profile=local.PROFILE,local_execution=full['local_execution'])
        raw.insert(0,dict(step='env',kind='fact',name='local_execution',
                         value=dict(attestation=attestation,attestation_sha256=local.digest(attestation))))
        summary=self.analyze(case,raw)
        self.assertEqual(summary['verdict'],'pass')
        self.assertNotIn('local_supervisor', {r.get('kind') for r in raw})
        # Raw product validity and independent whole-run sealing are separate.
        # verify/check_iterations reject a missing or failed execution receipt.


class LocalSupervisionWaitTests(unittest.TestCase):
    def test_local_runner_requires_persistent_work_before_any_execution(self):
        with tempfile.TemporaryDirectory() as tmp:
            case,*_=fixture();manifest=Path(tmp)/'manifest.json'
            manifest.write_text(json.dumps(dict(case=case)),encoding='utf-8')
            with patch.object(runner.sys,'argv',['run.py',str(manifest)]),patch.object(runner,'git') as git,patch.object(runner.subprocess,'Popen') as popen,patch.object(runner.sys,'stderr',io.StringIO()) as err:
                self.assertEqual(runner.main(),2)
            git.assert_not_called();popen.assert_not_called()
            self.assertIn('preserve independent recovery originals',err.getvalue())

    def setup_work(self,directory):
        case,_,attestation,prepare,_,_=fixture();work=Path(directory)
        for name in main.CLOCK_FILES:
            path=work/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(name.encode())
        files={name:runner.digest(work/name) for name in main.CLOCK_FILES}
        request=main._local_supervision_request(dict(prepare,schema='g2-clock-guard-v2'),0,files)
        receipt=dict(schema='loom-oi-local-supervisor-v1',**{k:request[k] for k in main.LOCAL_RECEIPT_IDENTITY},
            request_sha256=local.digest(request),wrapper_exit_code=0,recovery_exit_code=0,
            test_group_closed=True,files=files,host_started_monotonic_ns=1,host_completed_monotonic_ns=2,errors=[])
        return case,attestation,prepare,request,receipt

    def test_wait_records_actual_receipt_after_original_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            case,attestation,prepare,request,receipt=self.setup_work(tmp);raw=io.StringIO()
            with patch.object(local,'read_root_json',return_value=receipt) as read:
                runner.supervise_local_clock(Path(tmp),(local,case,attestation),[('clock_preparation',prepare)],0,runner.time.monotonic()+10,raw)
            self.assertEqual(json.loads((Path(tmp)/'supervision-request.json').read_text()),request)
            self.assertEqual(json.loads(raw.getvalue())['event'],receipt)
            self.assertEqual(read.call_args[0][0],Path('/var/lib/loom-oi/receipts')/(attestation['run_id']+'.json'))

    def test_missing_receipt_never_renews_step_deadline_or_overwrites_request(self):
        with tempfile.TemporaryDirectory() as tmp:
            case,attestation,prepare,_,_=self.setup_work(tmp);raw=io.StringIO();now=[100.0]
            def sleep(delay):now[0]+=delay
            with patch.object(runner.time,'monotonic',side_effect=lambda:now[0]),patch.object(runner.time,'sleep',side_effect=sleep),patch.object(local,'read_root_json',side_effect=FileNotFoundError):
                with self.assertRaisesRegex(ValueError,'original clock deadline'):
                    runner.supervise_local_clock(Path(tmp),(local,case,attestation),[('clock_preparation',prepare)],0,100.25,raw)
            self.assertLessEqual(now[0],100.25)
            self.assertEqual(raw.getvalue(),'')
            with self.assertRaisesRegex(ValueError,'overwrite'):
                runner.supervise_local_clock(Path(tmp),(local,case,attestation),[('clock_preparation',prepare)],0,999,raw)

    def test_untrusted_or_failed_receipt_is_not_accepted(self):
        for kind in ('untrusted','failed'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as tmp:
                case,attestation,prepare,_,receipt=self.setup_work(tmp);raw=io.StringIO()
                receipt['test_group_closed']=False
                kwargs={'side_effect':ValueError('root ownership')} if kind=='untrusted' else {'return_value':receipt}
                with patch.object(local,'read_root_json',**kwargs),self.assertRaises(ValueError):
                    runner.supervise_local_clock(Path(tmp),(local,case,attestation),[('clock_preparation',prepare)],0,runner.time.monotonic()+10,raw)
                if kind=='failed':self.assertFalse(json.loads(raw.getvalue())['event']['test_group_closed'])

    def test_receipt_read_cannot_cross_original_deadline_or_120_second_cap(self):
        for original_deadline in (100.25,999):
            with self.subTest(deadline=original_deadline),tempfile.TemporaryDirectory() as tmp:
                case,attestation,prepare,_,receipt=self.setup_work(tmp);raw=io.StringIO();now=[100.0]
                def read(path):
                    now[0]=min(original_deadline,220)+.001
                    return receipt
                with patch.object(runner.time,'monotonic',side_effect=lambda:now[0]),patch.object(local,'read_root_json',side_effect=read):
                    with self.assertRaisesRegex(ValueError,'exceeded original clock deadline'):
                        runner.supervise_local_clock(Path(tmp),(local,case,attestation),[('clock_preparation',prepare)],0,original_deadline,raw)
                self.assertEqual(json.loads(raw.getvalue())['event'],receipt)


class LocalAuditAndArchiveTests(unittest.TestCase):
    def test_all_400_rounds_bind_local_audit_and_original_witnesses(self):
        from test_product_g2_clock import Fixture
        f=Fixture();_,_,attestation,*_=fixture()
        identity=local.identity(attestation,'e'*64)
        def replace(value):
            if isinstance(value,dict):
                if value.get('schema')=='g2-clock-guard-v1':
                    value.pop('workflow_ref');value.update(identity)
                for item in value.values():replace(item)
            elif isinstance(value,(list,tuple)):
                for item in value:replace(item)
        replace(f.records);replace(f.audit)
        summary,problems=f.run()
        self.assertEqual(summary['result'],'pass',problems)
        self.assertEqual(sum(g['rounds'] for g in summary['groups'].values()),400)
        for change in ('foreign_witness','foreign_audit','hosted_witness','missing_receipt_field'):
            bad=copy.deepcopy(f)
            if change=='foreign_audit':bad.audit[2]['launch_sha256']='f'*64
            else:
                witness=bad.rounds[0]['live_witness']['guard']
                if change=='foreign_witness':witness['host_nonce_sha256']='f'*64
                elif change=='hosted_witness':witness['workflow_ref']=base.WORKFLOW
                else:witness.pop('attestation_sha256')
            with self.subTest(change=change):self.assertEqual(bad.run()[0]['result'],'fail')

    def test_local_fact_environment_manifest_and_prepared_source_are_identical(self):
        for change in ('none','foreign_environment','hosted_ci','foreign_source','foreign_launch',
                       'foreign_fact','missing_local_source','foreign_prepared_boot','hosted_preparation'):
            with self.subTest(change=change),tempfile.TemporaryDirectory() as tmp:
                case,raw,attestation,prepare,*_=fixture();run=Path(tmp)
                fact=next(r['value'] for r in raw if r.get('name')=='local_execution')
                env=dict(subject_version=base.SOURCE,local_execution=copy.deepcopy(fact),ci={})
                manifest=dict(case=case,subject_version=base.SOURCE)
                recorded=dict(prepare['source_hashes'])
                if change=='foreign_environment':env['local_execution']['attestation_sha256']='f'*64
                elif change=='hosted_ci':env['ci']={'GITHUB_RUN_ID':'123'}
                elif change=='foreign_source':manifest['subject_version']='b'*40
                elif change=='foreign_launch':case['local_execution']['launch_sha256']='f'*64
                elif change=='foreign_fact':fact['attestation_sha256']='f'*64
                elif change=='missing_local_source':recorded.pop('experiments/product/local_execution.py')
                elif change=='foreign_prepared_boot':prepare['boot_id']='33333333-3333-3333-3333-333333333333'
                elif change=='hosted_preparation':prepare['workflow_ref']=base.WORKFLOW
                (run/'environment.json').write_text(json.dumps(env),encoding='utf-8')
                (run/'source-manifest.json').write_text(json.dumps(recorded),encoding='utf-8')
                if change=='none':verify.verify_g2_provenance(run,manifest,raw,main)
                else:
                    with self.assertRaises((AssertionError,ValueError)):verify.verify_g2_provenance(run,manifest,raw,main)

    def test_legacy_archive_whitelist_stays_exact_and_cannot_claim_local(self):
        old='ANALYSIS_DEPENDENCIES = '+repr(verify.LEGACY_DEPENDENCIES)
        self.assertEqual(verify.analyzer_dependencies(old,{'g2_clock':{}}),
                         ('clock_guard.py','g2_analyze.py','g2_clock_analyze.py'))
        with self.assertRaisesRegex(AssertionError,'profile lacks'):
            verify.analyzer_dependencies(old,{'g2_clock':{},'local_execution':{}})
        current='ANALYSIS_DEPENDENCIES = '+repr(verify.DEPENDENCIES)
        self.assertEqual(verify.analyzer_dependencies(current,{'g2_clock':{},'local_execution':{}}),
                         ('clock_guard.py','g2_analyze.py','g2_clock_analyze.py','local_execution.py','local_run_seal.py'))


if __name__=='__main__':unittest.main()
