"""Independent local sealing boundaries; no product or clock is started."""
import copy
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch, Mock
from types import SimpleNamespace

import test_product as product
import test_product_local_supervisor as host
import verify
from check_iterations import check_primary_result

s = host.s.seal
HERE = Path(__file__).parent


def fixture(run):
    a = host.config()['attestation']
    launch = dict(executable='qemu.exe',args=['-name','loom-oi-clock-'+host.UUID])
    launch_raw=s.local.canonical(launch)
    a['launch_sha256']=hashlib.sha256(launch_raw).hexdigest()
    command='qemu.exe -name loom-oi-clock-'+host.UUID
    measured=dict(pid=123,creation_filetime=999,executable='qemu.exe',command_line=command,argv=['qemu.exe',*launch['args']])
    (run/'host-launch.json').write_bytes(launch_raw)
    (run/'host-process.json').write_bytes(s.local.canonical(measured))
    case = copy.deepcopy(product.CASE)
    case.update(execution_profile=s.local.PROFILE, local_execution={k:a[k] for k in
                ('vm_uuid', 'base_image_sha256', 'qemu_binary_sha256', 'launch_sha256')})
    manifest = dict(id='LOCAL', subject_version=a['source_sha'], case=case, repeats=1)
    sources = {n: hashlib.sha256((HERE.parent/n).read_bytes()).hexdigest() for n in
               (*s.HOST_SOURCES, 'experiments/product/analyze.py')}
    fact = dict(attestation=a, attestation_sha256=s.local.digest(a))
    pid = dict(pid=100, start_ticks=10, pgid=90, sid=80, uid=1000, gid=1000)
    runner = dict(identity=pid, started_ns=100)
    env = dict(subject_version=a['source_sha'], local_execution=fact, local_runner=runner, ci={})
    rows = product.rows()
    rows[:0] = [dict(step='env',kind='fact',name=k,value=v) for k,v in
                (('local_execution',fact),('local_runner',runner),('subject_version',a['source_sha']))]
    checks = []
    for n, name in enumerate(case['steps']):
        child = dict(pid=200+n, start_ticks=20+n, pgid=200+n, sid=200+n, uid=1000, gid=1000)
        waited = 200+n
        next(r for r in rows if r.get('step')==name and r['kind']=='start')['process']=child
        next(r for r in rows if r.get('step')==name and r['kind']=='end')['process']=dict(identity=child,waited_ns=waited)
        checks.append(dict(step=name,identity=child,exit_code=0,waited_ns=waited,checked_ns=500,
                           state='gone',remaining_pids=[],overflow=False))
    summary = product.analyze(case, rows, 1)
    assert summary['verdict']=='pass', summary
    values = {'manifest.json':manifest, 'source-manifest.json':sources,'environment.json':env,
              'execution.json':dict(exit_code=0,timed_out=False),'summary.json':summary}
    for name,value in values.items():
        (run/name).write_bytes(s.local.canonical(value))
    (run/'raw.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in rows),encoding='utf-8',newline='\n')
    (run/'stderr.log').write_bytes(b'')
    (run/'report.md').write_bytes(b'fixture original report\n')
    receipt = dict(schema=s.SCHEMA,execution_profile=s.local.PROFILE,**{k:a[k] for k in s.IDENTITY},
        evidence_run_id=run.name,attestation_sha256=s.local.digest(a),host_sources={k:sources[k] for k in s.HOST_SOURCES},
        host_vm=dict(pid=123,creation_filetime=999,name='loom-oi-clock-'+host.UUID,launch_nonce=host.UUID,
                     command_sha256=hashlib.sha256(command.encode()).hexdigest(),handle_verified=True),
        runner=dict(runner,observed_running_ns=150,checked_ns=500,state='gone',exit_code=0),
        process_checks=checks,clock=None,files=s.file_hashes(run),
        host_started_monotonic_ns=1,host_completed_monotonic_ns=1000,errors=[])
    return receipt,manifest,env,rows,sources


class LocalSealTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.run=Path(self.temp.name)/'LOCAL-20261105T000000Z-123abc'; self.run.mkdir()
        self.receipt,self.manifest,self.env,self.rows,self.sources=fixture(self.run)

    def validate(self, receipt=None):
        return s.validate_execution_receipt(receipt or self.receipt,self.manifest,self.env,self.rows,
                                             self.sources,s.file_hashes(self.run))

    def test_complete_seal_preserves_all_original_bytes_and_is_verifiable(self):
        originals={p.name:p.read_bytes() for p in self.run.iterdir()}
        self.assertEqual(self.validate(),'complete')
        self.assertEqual(s.seal_run(self.run,self.receipt),'complete')
        self.assertEqual(verify.verify(self.run)['execution_supervision'],'complete')
        self.assertTrue(all((self.run/n).read_bytes()==b for n,b in originals.items()))
        with self.assertRaisesRegex(ValueError,'already sealed'):s.seal_run(self.run,self.receipt)

    def test_missing_receipt_rejected_even_when_raw_product_passes(self):
        hashes=s.file_hashes(self.run)
        (self.run/'SHA256SUMS').write_text(''.join(h+'  '+n+'\n' for n,h in sorted(hashes.items())),encoding='utf-8')
        with self.assertRaisesRegex(AssertionError,'seal absent'):verify.verify(self.run)

    def test_valid_failure_remains_verifiable_but_cannot_be_primary(self):
        self.receipt['runner']['exit_code']=1
        self.receipt['errors']=['residual_process_group']
        self.receipt['process_checks'][0]['remaining_pids']=[999]
        self.assertEqual(s.seal_run(self.run,self.receipt),'failed')
        result=verify.verify(self.run)
        self.assertEqual(result['execution_supervision'],'failed')
        summary=json.loads((self.run/'summary.json').read_bytes())
        self.assertEqual(summary['verdict'],'pass')
        with self.assertRaisesRegex(ValueError,'complete execution'):check_primary_result(self.manifest,summary,result)

    def test_confirmed_product_failure_exit_is_complete_provenance(self):
        self.receipt['runner']['exit_code']=1
        self.receipt['process_checks'][0]['exit_code']=1
        next(r for r in self.rows if r['step']=='unit' and r['kind']=='end')['exit_code']=1
        self.assertEqual(self.validate(),'complete')

    def test_real_supervisor_finish_seals_a_host_copy_without_rewriting_guest(self):
        backend=object.__new__(host.s.SSHBackend)
        a=self.env['local_execution']['attestation']
        backend.config=dict(attestation=a,guest_run='/task/runs/'+a['run_id'],vm=dict(host.config()['vm'],
                            launch_sha256=a['launch_sha256'],command_line='unused config string'))
        backend.clock_enabled=False
        backend.verified_facts=dict(host_sources=self.receipt['host_sources'])
        backend.vm=type('VM',(),{'process_record':json.loads((self.run/'host-process.json').read_bytes())})()
        backend.runner_witness=dict(anchor=dict(runner=self.env['local_runner'],evidence_run_id=self.run.name),
                                    check=dict(checked_ns=150))
        snapshot=dict(evidence_run_id=self.run.name,files={n:h for n,h in self.receipt['files'].items()
                                                        if not n.startswith('host-')},
                      runner=self.env['local_runner'],runner_check=dict(state='gone',checked_ns=500),
                      process_checks=self.receipt['process_checks'])
        backend.fixed_helper=lambda *args:json.dumps(snapshot).encode()
        def remote(argv,**kwargs):
            self.assertEqual(argv[:2],['cat','--'])
            kwargs['stdout'].write((self.run/Path(argv[-1]).name).read_bytes())
        backend.remote=remote
        output=Path(self.temp.name)/'host';output.mkdir()
        for n in ('host-launch.json','host-process.json'):
            (output/n).write_bytes((self.run/n).read_bytes())
        before={p.name:p.read_bytes() for p in self.run.iterdir()}
        result=backend.finish(output,0,1,2,None,None)
        self.assertEqual(result,dict(result='complete',stop_required=False))
        preserved=output/'evidence'/self.run.name
        self.assertEqual(verify.verify(preserved)['execution_supervision'],'complete')
        self.assertEqual({p.name:p.read_bytes() for p in self.run.iterdir()},before)

    def test_host_command_must_match_preserved_measurement_and_launch(self):
        self.receipt['host_vm']['command_sha256']='e'*64
        with self.assertRaisesRegex(ValueError,'command differs'):s.verify_host_materials(self.run,self.receipt)

    def test_clock_requires_actual_subreceipt_and_distinct_final_recovery(self):
        a=self.env['local_execution']['attestation']
        ident=s.local.identity(a,'c'*64)
        files={n:hashlib.sha256(n.encode()).hexdigest() for n in host.s.FILES}
        request=dict(schema='loom-oi-local-supervision-request-v1',**{k:ident[k] for k in host.s.IDENTITY},
                     wrapper_exit_code=0,files=files)
        final=dict(schema='g2-local-recovery-v1',identity=ident,recovery_exit_code=0,errors=[],files=files,exit_code=0,test_group_closed=True)
        sub=host.s.receipt(request,final,1,2)
        self.manifest['case']['g2_clock']={}
        self.manifest['case']['steps'].append('clock')
        child=dict(pid=210,pgid=210,sid=210,start_ticks=30,uid=1000,gid=1000)
        self.receipt['process_checks'].append(dict(step='clock',identity=child,exit_code=0,waited_ns=300,
            checked_ns=500,state='gone',remaining_pids=[],overflow=False))
        self.rows.extend([dict(step='clock',kind='start',process=child),
                          dict(step='clock',kind='end',exit_code=0,timed_out=False,process=dict(identity=child,waited_ns=300))])
        self.receipt['clock']=dict(receipt=sub,final_recovery=final)
        self.rows.append(dict(step='clock',kind='local_supervisor',event=sub))
        for name,value in (('clock-supervisor-receipt.json',sub),('clock-final-recovery.json',final)):
            (self.run/name).write_bytes(s.local.canonical(value))
        self.receipt['files']=s.file_hashes(self.run)
        self.assertEqual(self.validate(),'complete')
        sub['wrapper_exit_code']=1
        (self.run/'clock-supervisor-receipt.json').write_bytes(s.local.canonical(sub))
        self.receipt['files']=s.file_hashes(self.run)
        self.assertEqual(self.validate(),'complete')
        with self.assertRaisesRegex(ValueError,'passing verdict'):
            check_primary_result(self.manifest,{'status':'observed','verdict':'fail'},
                                 {'execution_supervision':'complete'})
        sub['wrapper_exit_code']=0
        (self.run/'clock-supervisor-receipt.json').write_bytes(s.local.canonical(sub))
        self.receipt['files']=s.file_hashes(self.run)
        for field,value in (('nonce_sha256','d'*64),('boot_id',host.UUID)):
            changed=copy.deepcopy(self.receipt)
            changed['clock']['final_recovery']['identity'][field]=value
            (self.run/'clock-final-recovery.json').write_bytes(s.local.canonical(changed['clock']['final_recovery']))
            changed['files']=s.file_hashes(self.run)
            with self.subTest(field=field),self.assertRaises(ValueError):self.validate(changed)
        (self.run/'clock-final-recovery.json').write_bytes(s.local.canonical(dict(final,exit_code=1,recovery_exit_code=1,errors=['recover failed'])))
        self.receipt['clock']['final_recovery']=json.loads((self.run/'clock-final-recovery.json').read_bytes())
        self.receipt['files']=s.file_hashes(self.run)
        self.assertEqual(self.validate(),'failed')

    def test_every_source_identity_and_artifact_is_bound(self):
        for key in (*s.IDENTITY,'attestation_sha256','execution_profile','schema','evidence_run_id'):
            changed=copy.deepcopy(self.receipt); changed[key]='other'
            with self.subTest(key=key),self.assertRaises((ValueError,KeyError)):self.validate(changed)
        for field in ('host_sources','files'):
            changed=copy.deepcopy(self.receipt); changed[field][next(iter(changed[field]))]='0'*64
            with self.subTest(field=field),self.assertRaises(ValueError):self.validate(changed)

    def test_unknown_unbounded_or_duplicate_errors_and_fields_rejected(self):
        for update in (dict(extra=True),dict(errors=['run shell']),dict(errors=['runner_nonzero']*17),
                       dict(errors=['runner_nonzero']*2),dict(host_completed_monotonic_ns=0)):
            with self.subTest(update=update),self.assertRaises(ValueError):self.validate(dict(self.receipt,**update))

    def test_ordinary_never_accepts_clock_objects_or_files(self):
        changed=copy.deepcopy(self.receipt);changed['clock']={}
        with self.assertRaisesRegex(ValueError,'ordinary'):self.validate(changed)
        (self.run/'clock-final-recovery.json').write_bytes(b'{}')
        changed['files']=s.file_hashes(self.run)
        with self.assertRaisesRegex(ValueError,'ordinary'):self.validate(changed)

    def test_live_missing_witness_reused_or_residual_groups_never_complete(self):
        mutations=[lambda r:r['runner'].update(observed_running_ns=None),lambda r:r['runner'].update(state='reused'),
                   lambda r:r['process_checks'][0].update(state='same'),
                   lambda r:r['process_checks'][0].update(remaining_pids=[999]),
                   lambda r:r['process_checks'][0].update(overflow=True)]
        for i,mutate in enumerate(mutations):
            changed=copy.deepcopy(self.receipt);mutate(changed)
            with self.subTest(i=i):self.assertEqual(self.validate(changed),'failed')

    def test_swapped_pid_session_wait_or_step_cannot_match_raw(self):
        for field,value in (('pid',999),('sid',1),('start_ticks',999),('uid',1001)):
            changed=copy.deepcopy(self.receipt);changed['process_checks'][0]['identity'][field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):self.validate(changed)
        for field,value in (('waited_ns',501),('exit_code',True),('checked_ns',199),('remaining_pids',list(range(1,130)))):
            changed=copy.deepcopy(self.receipt);changed['process_checks'][0][field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):self.validate(changed)
        changed=copy.deepcopy(self.receipt);changed['process_checks'].reverse()
        with self.assertRaises(ValueError):self.validate(changed)

    def test_unstarted_steps_are_explicit_failed_not_invented_exit(self):
        self.rows[:]=[r for r in self.rows if r.get('step')!='vet']
        check=self.receipt['process_checks'][-1]
        check.update(identity=None,exit_code=None,waited_ns=None,state='not_started')
        self.assertEqual(self.validate(),'failed')
        check['exit_code']=0
        with self.assertRaisesRegex(ValueError,'unstarted'):self.validate()

    def test_unknown_file_changed_summary_and_foreign_source_refuse_seal(self):
        original=(self.run/'summary.json').read_bytes()
        (self.run/'summary.json').write_bytes(original+b' ')
        with self.assertRaisesRegex(ValueError,'changed before'):s.seal_run(self.run,self.receipt)
        (self.run/'summary.json').write_bytes(original)
        (self.run/'extra').write_bytes(b'new')
        with self.assertRaisesRegex(ValueError,'unknown evidence'):s.file_hashes(self.run)

    def test_current_helper_cannot_replace_recorded_adjacent_dependency(self):
        self.sources['experiments/product/local_run_seal.py']='0'*64
        (self.run/'source-manifest.json').write_bytes(s.local.canonical(self.sources))
        with self.assertRaisesRegex(AssertionError,'dependency source drift'):verify.recorded_module(self.run)

    def test_three_dependency_generations_are_exact(self):
        for declaration in (verify.LEGACY_DEPENDENCIES,verify.LOCAL_DEPENDENCIES,verify.DEPENDENCIES):
            text='ANALYSIS_DEPENDENCIES = '+repr(declaration)
            self.assertEqual(verify.analyzer_dependencies(text,{'g2_behavior':{}}),('g2_analyze.py',))
        bad=copy.deepcopy(verify.DEPENDENCIES);bad['local_execution']+=('../other.py',)
        with self.assertRaisesRegex(AssertionError,'unregistered'):verify.analyzer_dependencies('ANALYSIS_DEPENDENCIES = '+repr(bad),{'local_execution':{}})

    @unittest.skipUnless(sys.platform=='linux','real /proc requires Linux')
    def test_symlink_artifact_cannot_be_sealed(self):
        target=Path(self.temp.name)/'outside';target.write_bytes(b'')
        (self.run/'stderr.log').unlink();(self.run/'stderr.log').symlink_to(target)
        with self.assertRaisesRegex(ValueError,'nonregular'):s.file_hashes(self.run)


class LocalDBRunnerWiringTests(unittest.TestCase):
    def test_scan_uses_real_fixed_db_argv_and_encloses_start_end_without_changing_budget(self):
        runner=product.runner
        for outcome in ('pass','tool_failure','changed_after'):
            with self.subTest(outcome=outcome),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp);prereg=root/'manifest.json';a=host.config()['attestation']
                case=dict(kind='product-p1',fuzz_time='60s',backends=['sqlite','postgres'],
                          fuzz={'fuzz-frame':'FuzzFrame','fuzz-control':'FuzzControl'},steps=['govulncheck'],
                          execution_profile=s.local.PROFILE,local_execution={k:a[k] for k in
                            ('vm_uuid','base_image_sha256','qemu_binary_sha256','launch_sha256')},vulndb_snapshot={})
                manifest=dict(id='LOCAL',gate='G1',evidence_level='Implementation',case=case,repeats=1,
                              hypothesis='fixture',alternatives=['fixture'],disproof=['fixture'],limitations=['fixture'],issues=['fixture'])
                prereg.write_bytes(s.local.canonical(manifest))
                value={'snapshot':'fixed official archive'}
                argv=['go','run',runner.GOVULNCHECK,'-db','file:///fixed/db','./...']
                db=SimpleNamespace(prepare=Mock(return_value={'context':True}),fact=lambda _:value,
                    command=lambda _:argv,verify_directory=Mock(side_effect=[value,ValueError('changed DB')] if outcome=='changed_after' else [value,value]))
                original_sibling=runner.sibling
                local_module=SimpleNamespace(**{n:getattr(s.local,n) for n in ('canonical','digest','validate_attestation')},environment=lambda *a,**k:host.config()['attestation'])
                pid=dict(pid=100,pgid=100,sid=100,start_ticks=1,uid=1000,gid=1000)
                helper=SimpleNamespace(process_identity=lambda:pid,atomic_new=s.atomic_new)
                def sibling(name):return db if name=='vulndb_snapshot' else helper if name=='local_run_seal' else original_sibling(name)
                def git(*args):return 'a'*40 if args[0] in ('rev-parse','log') else ''
                def command(args,**kwargs):
                    if args[:2]==['go','version']:return SimpleNamespace(stdout='go version go1.27.3 linux/amd64')
                    if args[:2]==['go','env']:return SimpleNamespace(stdout=json.dumps({'GOOS':'linux','GOARCH':'amd64','GOPROXY':'file:///module-cache','GOSUMDB':'off','GOMODCACHE':'/module-cache'}))
                    raise AssertionError(args)
                def step(name,actual,extra,timeout,is_json,raw,stderr_log,**kwargs):
                    self.assertEqual(actual,argv);self.assertEqual(timeout,1200)
                    child=dict(pid,pid=200,pgid=200,sid=200)
                    raw.write(json.dumps(dict(step=name,kind='start',command=actual,process=child))+'\n')
                    kwargs['process_record'].update(identity=child,waited_ns=123)
                    return (1 if outcome=='tool_failure' else 0),False,7
                with contextlib.ExitStack() as stack:
                    for obj,key,replacement in ((runner,'ROOT',root),(runner,'git',git),(runner,'workspace_dirty',lambda _:False),
                        (runner,'sibling',sibling),(runner,'run_step',step),(runner._analyzer,'_local_case',lambda _:local_module),
                        (runner.subprocess,'run',command),(runner,'analyze',lambda *a:{'status':'inconclusive','problems':[]})):
                        stack.enter_context(patch.object(obj,key,replacement))
                    stack.enter_context(patch.object(sys,'argv',['run.py',str(prereg),'--work',str(root/'work'),'--output-root',str(root/'out')]))
                    stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
                    self.assertEqual(runner.main(),1)
                run=next((root/'out').iterdir());self.assertFalse((run/'SHA256SUMS').exists())
                rows=[json.loads(line) for line in (run/'raw.jsonl').read_text().splitlines()]
                scan=[r for r in rows if r['step']=='govulncheck']
                self.assertEqual([r.get('name',r['kind']) for r in scan],
                    ['vulndb_scan_before','start','end']+([] if outcome=='changed_after' else ['vulndb_scan_after']))
                self.assertEqual(scan[2]['exit_code'],1 if outcome=='tool_failure' else 0)
                env=json.loads((run/'environment.json').read_bytes())
                self.assertEqual(env['go_env']['GOPROXY'],'file:///module-cache')


@unittest.skipUnless(sys.platform=='linux' and os.getuid()>0,'Linux ordinary UID process boundaries')
class LocalProcessBoundaryTests(unittest.TestCase):
    def test_short_child_metadata_is_read_before_wait_and_reuse_refused(self):
        p=subprocess.Popen([sys.executable,'-c','pass'],start_new_session=True)
        try:
            child=s.process_identity(p.pid)
            self.assertEqual(child['pid'],child['pgid']);self.assertEqual(child['pid'],child['sid'])
            self.assertEqual(s.inspect_process(dict(child,start_ticks=child['start_ticks']+1))['state'],'reused')
            self.assertEqual(p.wait(timeout=3),0)
            check=s.inspect_process(child)
            self.assertEqual((check['state'],check['remaining_pids']),('gone',[]))
        finally:
            if p.poll() is None:p.kill();p.wait()

    def test_actual_runner_step_records_original_identity_and_wait(self):
        raw,err,record=io.StringIO(),io.StringIO(),{}
        code,timeout,_=product.runner.run_step('vet',[sys.executable,'-c','print("child")'],{},3,False,raw,err,process_record=record)
        rows=[json.loads(line) for line in raw.getvalue().splitlines()]
        self.assertEqual((code,timeout),(0,False))
        self.assertEqual(rows[0]['process'],record['identity'])
        self.assertGreater(record['waited_ns'],0)
        self.assertEqual(s.inspect_process(record['identity'])['remaining_pids'],[])

    def test_exited_leader_with_remaining_group_is_not_cleanup_success(self):
        # The parent waits for the child's readiness via pipe. The child remains
        # in the original session; the test owns both identities and reaps it.
        script='import os,sys\np=os.fork()\nif p==0:\n sys.stdin.buffer.read(1);os._exit(0)\nprint(p,flush=True);os._exit(0)'
        p=subprocess.Popen([sys.executable,'-c',script],start_new_session=True,stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
        try:
            child=s.process_identity(p.pid)
            descendant=int(p.stdout.readline())
            p.wait(timeout=3)
            check=s.inspect_process(child)
            self.assertIn(descendant,check['remaining_pids'])
            self.assertEqual(check['state'],'gone')
            p.stdin.write('x');p.stdin.flush();p.stdin.close()
            deadline=time.monotonic()+3
            while s.inspect_process(child)['remaining_pids'] and time.monotonic()<deadline:
                time.sleep(.01)
            self.assertEqual(s.inspect_process(child)['remaining_pids'],[])
        finally:
            if p.stdin and not p.stdin.closed:p.stdin.close()
            if p.stdout:p.stdout.close()
            if p.poll() is None:os.killpg(p.pid,signal.SIGKILL);p.wait()


@unittest.skipUnless(sys.platform=='linux','guest fixed-source paths require Linux')
class LocalGuestHelperLoadTests(unittest.TestCase):
    def test_each_helper_invocation_rechecks_source_bytes_and_repository_path(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp); repo=root/'source'; product_dir=repo/'experiments/product';product_dir.mkdir(parents=True)
            run=root/'runs/id';run.mkdir(parents=True)
            expected={}
            for name in s.HOST_SOURCES:
                data=(HERE.parent/name).read_bytes();target=repo/name;target.write_bytes(data)
                expected[name]=hashlib.sha256(data).hexdigest()
            backend=object.__new__(host.s.SSHBackend)
            backend.config=dict(guest_root=str(root),guest_run=str(run),guest_repository=str(repo),
                attestation=dict(boot_id=Path('/proc/sys/kernel/random/boot_id').read_text().strip()))
            backend.verified_facts=dict(host_sources=expected)
            backend.remote=lambda argv,**kwargs:subprocess.run([sys.executable,*argv[1:]],check=True,**kwargs)
            self.assertEqual(backend.fixed_helper('print("verified")').strip(),b'verified')
            target=product_dir/'local_execution.py';original=target.read_bytes()
            target.write_bytes(b'raise RuntimeError("DRIFT EXECUTED")\n')
            with self.assertRaises(subprocess.CalledProcessError) as caught:
                backend.fixed_helper('print("untrusted")')
            self.assertIn(b'fixed host/guest source changed',caught.exception.stderr)
            self.assertNotIn(b'DRIFT EXECUTED',caught.exception.stderr)
            target.write_bytes(original)
            moved=root/'outside';repo.rename(moved);repo.symlink_to(moved,target_is_directory=True)
            with self.assertRaises(subprocess.CalledProcessError) as caught:
                backend.fixed_helper('print("untrusted")')
            self.assertIn(b'fixed repository path changed',caught.exception.stderr)


if __name__=='__main__':unittest.main()
