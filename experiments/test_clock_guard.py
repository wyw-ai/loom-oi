"""No test in this file calls a real system clock setter or systemctl."""
import copy
import contextlib
import importlib.util
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('clock_guard_tested', Path(__file__).parent/'product/clock_guard.py')
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)
NONCE = 'b'*64
PROVENANCE=dict(schema='g2-clock-guard-v1',run_id='123',job='clock',source_sha='a'*40,
                nonce_sha256='b'*64,boot_id='11111111-1111-1111-1111-111111111111',
                workflow_ref='wyw-ai/loom-oi/.github/workflows/g2-clock.yml@refs/heads/clock/test')


class FakeClock:
    def __init__(self):
        self.real, self.mono = 1_800_000_000_000_000_000, 50_000_000_000
        self.sets = []
        self.apply = True

    def advance(self, ns):
        self.real += ns
        self.mono += ns

    def sample(self):
        self.advance(100)
        return dict(realtime_ns=self.real, monotonic_ns=self.mono,
                    monotonic_before_ns=self.mono, monotonic_after_ns=self.mono)

    def set(self, value):
        self.sets.append(value)
        if self.apply:
            self.real = value


class ClockStateTests(unittest.TestCase):
    def setUp(self):
        self.clock, self.events = FakeClock(), []
        self.state = guard.ClockState(self.clock, NONCE, self.events.append)

    def request(self, action, **fields):
        return self.state.request(dict(action=action, nonce=NONCE, **fields))

    def test_both_real_step_directions_and_elapsed_time_restoration(self):
        for ident, delta in enumerate((guard.STEP_NS, -guard.STEP_NS), 1):
            before = self.clock.mono
            event = self.request('shift', round_id=ident, delta_ns=delta)
            self.assertEqual(event['requested_delta_ns'], delta)
            self.assertLessEqual(abs(event['actual_delta_ns']-delta), guard.TOLERANCE_NS)
            self.clock.advance(900_000_000)
            self.assertEqual(self.request('witness')['active_round'], ident)
            event = self.request('restore', round_id=ident)
            self.assertEqual(event['round_id'], ident)
            self.assertEqual(self.state.completed, ident)
            self.assertGreater(self.clock.real, self.state.anchor['realtime_ns']+self.clock.mono-before-1000)
            self.assertLessEqual(abs(self.state.offset(self.clock.sample())), guard.TOLERANCE_NS)

    def test_unknown_fields_absolute_time_and_replayed_commands_do_not_write(self):
        requests = [dict(action='set', round_id=1, absolute_ns=1),
                    dict(action='shift', round_id=1, delta_ns=1),
                    dict(action='shift', round_id=True, delta_ns=guard.STEP_NS),
                    dict(action='shift', round_id=2, delta_ns=guard.STEP_NS),
                    dict(action='restore', round_id=1),
                    dict(action='witness', absolute_ns=1)]
        for request in requests:
            with self.subTest(request=request), self.assertRaises(ValueError):
                self.request(**request)
        self.assertEqual(self.clock.sets, [])
        with self.assertRaises(ValueError):
            self.state.request(dict(action='witness', nonce='wrong'))
        self.request('shift', round_id=1, delta_ns=guard.STEP_NS)
        for action, fields in [('shift',dict(round_id=1,delta_ns=guard.STEP_NS)),('restore',dict(round_id=2))]:
            with self.subTest(action=action), self.assertRaises(ValueError):
                self.request(action,**fields)
        self.assertEqual(len(self.clock.sets),1)

    def test_external_correction_is_fatal_and_finally_can_restore(self):
        self.request('shift',round_id=1,delta_ns=guard.STEP_NS)
        self.clock.real += guard.TOLERANCE_NS+1
        with self.assertRaisesRegex(ValueError,'drift'):
            self.request('witness')
        with self.assertRaisesRegex(ValueError,'drift'):
            self.request('restore',round_id=1)
        self.state.restore('guard_finally')
        self.assertIsNone(self.state.active)
        self.assertLessEqual(abs(self.state.offset(self.clock.sample())),guard.TOLERANCE_NS)

    def test_unapplied_syscall_cannot_produce_a_pass(self):
        self.clock.apply=False
        with self.assertRaises(ValueError):
            self.request('shift',round_id=1,delta_ns=guard.STEP_NS)
        self.assertEqual(self.state.active,1)
        self.assertEqual(self.state.completed,0)
        self.assertFalse(self.events)

    def test_failed_restore_is_not_counted(self):
        self.request('shift',round_id=1,delta_ns=guard.STEP_NS)
        self.clock.apply=False
        with self.assertRaisesRegex(ValueError,'restoration'):
            self.request('restore',round_id=1)
        self.assertEqual(self.state.active,1)
        self.assertEqual(self.state.completed,0)

    def test_original_anchor_survives_guard_restart(self):
        self.request('shift',round_id=1,delta_ns=-guard.STEP_NS)
        self.clock.advance(2_000_000_000)
        restarted=guard.ClockState(self.clock,NONCE,self.events.append,anchor=self.state.anchor)
        restarted.restore('workflow_always')
        self.assertLessEqual(abs(restarted.offset(self.clock.sample())),guard.TOLERANCE_NS)
        self.assertGreater(self.clock.real,self.state.anchor['realtime_ns']+1_999_000_000)


class GuardEnvironmentTests(unittest.TestCase):
    def test_every_guard_gate_is_required(self):
        env=dict(OI_IT_CLOCK='1',GITHUB_ACTIONS='true',RUNNER_ENVIRONMENT='github-hosted',
                 GITHUB_JOB='clock',GITHUB_REPOSITORY='wyw-ai/loom-oi',GITHUB_RUN_ID='123',
                 GITHUB_SHA='a'*40,
                 GITHUB_WORKFLOW_REF='wyw-ai/loom-oi/.github/workflows/g2-clock.yml@refs/heads/clock/test')
        with mock.patch.object(guard.sys,'platform','linux'):
            guard.hosted_environment(env)
            for field in env:
                candidate=env.copy();candidate.pop(field)
                with self.subTest(field=field),self.assertRaises(ValueError):guard.hosted_environment(candidate)
            candidate=env.copy();candidate['RUNNER_ENVIRONMENT']='self-hosted'
            with self.assertRaises(ValueError):guard.hosted_environment(candidate)
        with mock.patch.object(guard.sys,'platform','win32'),self.assertRaises(ValueError):
            guard.hosted_environment(env)

    def test_only_two_fixed_workflow_and_branch_combinations_are_allowed(self):
        self.assertTrue(guard.allowed_workflow('wyw-ai/loom-oi/.github/workflows/g2-evidence.yml@refs/heads/evidence/G2-P2'))
        for value in ('wyw-ai/loom-oi/.github/workflows/ci.yml@refs/heads/clock/test',
                      'wyw-ai/loom-oi/.github/workflows/g2-clock.yml@refs/heads/alpha',
                      'wyw-ai/loom-oi/.github/workflows/g2-evidence.yml@refs/heads/clock/test',
                      'wyw-ai/loom-oi/.github/workflows/g2-evidence.yml@refs/heads/evidence/G4-P2',
                      'wyw-ai/loom-oi/.github/workflows/g2-clock.yml@refs/pull/1/merge'):
            with self.subTest(value=value):self.assertFalse(guard.allowed_workflow(value))

    def test_ntp_runtime_mask_restores_only_original_active_units(self):
        states=[dict(unit='chrony.service',canonical='chrony.service',active='active',enabled='enabled'),
                dict(unit='ntp.service',canonical='ntp.service',active='inactive',enabled='disabled'),
                dict(unit='ntpd.service',canonical='ntpd.service',active='inactive',enabled='masked')]
        stopped=[dict(s,active='inactive',enabled='masked-runtime' if s['enabled']!='masked' else 'masked') for s in states]
        with mock.patch.object(guard.subprocess,'run',return_value=mock.Mock(returncode=3)) as run, \
                mock.patch.object(guard,'unit_states',return_value=stopped) as inventory:
            guard.stop_sync(states)
            inventory.assert_called_once_with(tuple(s['unit'] for s in states),required=True)
            commands=[c.args[0] for c in run.call_args_list]
            self.assertIn(['systemctl','mask','--runtime','--now','chrony.service'],commands)
            self.assertNotIn(['systemctl','mask','--runtime','--now','ntpd.service'],commands)
        with mock.patch.object(guard.subprocess,'run') as run,mock.patch.object(guard,'unit_states',return_value=states):
            self.assertEqual(guard.restore_sync(states),states)
            starts=[c.args[0] for c in run.call_args_list if 'start' in c.args[0]]
            self.assertEqual(starts,[['systemctl','start','chrony.service']])
        with mock.patch.object(guard.subprocess,'run'),mock.patch.object(guard,'unit_states',return_value=[]),self.assertRaises(ValueError):
            guard.restore_sync(states)

    def test_saved_anchor_and_finished_receipt_must_match_current_boot_job_source(self):
        saved=dict(identity=PROVENANCE.copy(),uid=1001)
        guard.check_saved_identity(saved,PROVENANCE,1001)
        for field in PROVENANCE:
            changed=copy.deepcopy(saved);changed['identity'][field]='foreign'
            with self.subTest(field=field),self.assertRaises(ValueError):
                guard.check_saved_identity(changed,PROVENANCE,1001)
        with self.assertRaises(ValueError):guard.check_saved_identity(saved,PROVENANCE,1002)

    def test_inventory_accepts_only_safely_inactive_known_masked_units(self):
        def inventory(load, active, enabled, status=0):
            def run(command, **kwargs):
                unit = command[2]
                if unit != 'chrony.service':
                    return SimpleNamespace(returncode=0, stdout='LoadState=not-found\n')
                return SimpleNamespace(returncode=status, stdout=
                    f'Id=chrony.service\nLoadState={load}\nActiveState={active}\nUnitFileState={enabled}\n')
            with mock.patch.object(guard.subprocess, 'run', side_effect=run):
                return guard.unit_states()
        for enabled in ('masked', 'masked-runtime'):
            with self.subTest(enabled=enabled):
                self.assertEqual(inventory('masked', 'inactive', enabled),
                    [dict(unit='chrony.service', canonical='chrony.service', active='inactive', enabled=enabled)])
        for fields in [('masked', 'active', 'masked-runtime'),
                       ('masked', 'activating', 'masked-runtime'),
                       ('masked', 'inactive', 'enabled'),
                       ('error', 'inactive', 'masked'),
                       ('masked', 'inactive', 'masked-runtime', 1)]:
            with self.subTest(fields=fields), self.assertRaises(ValueError):
                inventory(*fields)

    def test_canonical_mask_avoids_alias_lookup_error_and_restores_complete_original_mapping(self):
        aliases = ('chrony.service', 'chronyd.service')
        masks, calls = set(), []
        running = True
        wrong_enabled = False
        def run(command, **kwargs):
            nonlocal running
            calls.append(command)
            action = command[1]
            unit = command[2] if action == 'show' else command[-1]
            if action == 'show':
                if unit not in aliases:
                    return SimpleNamespace(returncode=0, stdout='LoadState=not-found\n')
                if unit == 'chronyd.service' and 'chrony.service' in masks:
                    # Real second-run counterexample: canonical masking makes
                    # this alias temporarily fail resolution despite status 0.
                    return SimpleNamespace(returncode=0, stdout=
                        'Id=chronyd.service\nLoadState=error\nActiveState=inactive\nUnitFileState=masked-runtime\n')
                load = 'masked' if unit in masks else 'loaded'
                active = 'inactive' if unit in masks or not running else 'active'
                enabled = 'masked-runtime' if unit in masks else ('disabled' if wrong_enabled else 'enabled')
                return SimpleNamespace(returncode=0, stdout=
                    f'Id=chrony.service\nLoadState={load}\nActiveState={active}\nUnitFileState={enabled}\n')
            if action == 'mask':
                masks.add(unit)
                running = False
            elif action == 'unmask':
                masks.remove(unit)
            elif action == 'start':
                self.assertNotIn(unit, masks)
                running = True
            elif action == 'is-active':
                return SimpleNamespace(returncode=0 if running and unit not in masks else 3)
            else:
                self.fail('unexpected systemctl action: '+action)
            return SimpleNamespace(returncode=0)
        with mock.patch.object(guard.subprocess, 'run', side_effect=run):
            original = guard.unit_states()
            self.assertEqual([s['unit'] for s in original], list(aliases))
            self.assertEqual([s['canonical'] for s in original], ['chrony.service']*2)
            self.assertEqual(guard.stop_sync(original),
                [dict(unit='chrony.service', canonical='chrony.service', active='inactive', enabled='masked-runtime')])
            self.assertEqual([c[-1] for c in calls if c[1] == 'mask'], ['chrony.service'])
            self.assertEqual(guard.restore_sync(original), original)
            self.assertFalse(masks)
            self.assertTrue(running)
            self.assertEqual([c[-1] for c in calls if c[1] == 'unmask'], ['chrony.service'])
            guard.stop_sync(original)
            wrong_enabled = True
            with self.assertRaisesRegex(ValueError, 'restoration mismatch'):
                guard.restore_sync(original)

    def test_canonical_identity_and_stopped_state_fail_closed(self):
        original = [dict(unit='chrony.service', canonical='chrony.service', active='active', enabled='enabled'),
                    dict(unit='chronyd.service', canonical='chrony.service', active='active', enabled='enabled')]
        for change in ('missing', 'cycle', 'unknown', 'duplicate', 'activity'):
            states = copy.deepcopy(original)
            if change == 'missing': states.pop(0)
            elif change == 'cycle': states[0]['canonical']='chronyd.service'
            elif change == 'unknown': states[1]['canonical']='foreign.service'
            elif change == 'duplicate': states.append(copy.deepcopy(states[0]))
            else: states[1]['active']='inactive'
            with self.subTest(change=change), mock.patch.object(guard.subprocess,'run') as run, \
                    self.assertRaises(ValueError):
                guard.stop_sync(states)
            run.assert_not_called()
        for state in ('active', 'unmasked', 'wrong_identity'):
            stopped=[dict(original[0],active='inactive',enabled='masked-runtime')]
            if state=='active': stopped[0]['active']='active'
            elif state=='unmasked': stopped[0]['enabled']='enabled'
            else: stopped[0]['canonical']='chronyd.service'
            with self.subTest(state=state), \
                    mock.patch.object(guard.subprocess,'run',return_value=SimpleNamespace(returncode=3)), \
                    mock.patch.object(guard,'unit_states',return_value=stopped), self.assertRaises(ValueError):
                guard.stop_sync(original)
        for text in ('Id=chrony.service\nLoadState=error\nActiveState=inactive\nUnitFileState=masked-runtime\n',
                     'Id=chrony.service\nLoadState=not-found\n',
                     'Id=foreign.service\nLoadState=loaded\nActiveState=inactive\nUnitFileState=enabled\n'):
            with self.subTest(text=text), mock.patch.object(guard.subprocess,'run',
                    return_value=SimpleNamespace(returncode=0,stdout=text)), self.assertRaises(ValueError):
                guard.unit_states(('chrony.service',),required=True)

    def test_restoration_checks_alias_mapping_and_activity_not_just_canonical_unit(self):
        original=[dict(unit='chrony.service',canonical='chrony.service',active='active',enabled='enabled'),
                  dict(unit='chronyd.service',canonical='chrony.service',active='active',enabled='enabled')]
        for changed in ('canonical','active','enabled','removed'):
            actual=copy.deepcopy(original)
            if changed=='removed':actual.pop()
            else:actual[1][changed]={'canonical':'chronyd.service','active':'inactive','enabled':'disabled'}[changed]
            with self.subTest(changed=changed), mock.patch.object(guard.subprocess,'run') as run, \
                    mock.patch.object(guard,'unit_states',return_value=actual), \
                    self.assertRaisesRegex(ValueError,'restoration mismatch'):
                guard.restore_sync(original)
            self.assertEqual([c.args[0] for c in run.call_args_list],
                [['systemctl','unmask','--runtime','chrony.service'],['systemctl','start','chrony.service']])

    def test_clock_restore_failure_still_restores_original_ntp_state(self):
        state=mock.Mock();state.restore.side_effect=OSError('clock denied')
        with mock.patch.object(guard,'restore_sync',return_value=[]) as restore,self.assertRaisesRegex(ValueError,'clock denied'):
            guard.restore_environment(state,[],lambda event:None,'guard_finally')
        restore.assert_called_once_with([])

    def test_child_cleanup_exceptions_cannot_bypass_clock_or_ntp_restoration(self):
        for failure in (ProcessLookupError('already gone'),guard.subprocess.TimeoutExpired('child',1)):
            with self.subTest(failure=type(failure).__name__),tempfile.TemporaryDirectory() as tmp,contextlib.ExitStack() as stack:
                root=Path(tmp);clock=FakeClock()
                stack.enter_context(mock.patch.dict(guard.sys.modules,{'pwd':SimpleNamespace(getpwuid=lambda uid:SimpleNamespace(pw_dir=tmp,pw_name='runner'))}))
                stack.enter_context(mock.patch.object(guard,'hosted_environment'))
                stack.enter_context(mock.patch.object(guard.os,'geteuid',return_value=0,create=True))
                stack.enter_context(mock.patch.object(guard.os,'chown',create=True))
                stack.enter_context(mock.patch.object(guard.os,'chmod'))
                stack.enter_context(mock.patch.object(guard,'nonce_from',return_value=NONCE))
                stack.enter_context(mock.patch.object(guard,'identity',return_value=PROVENANCE))
                stack.enter_context(mock.patch.object(guard,'guard_directory',return_value=root/'guard'))
                stack.enter_context(mock.patch.object(guard,'SystemClock',return_value=clock))
                stack.enter_context(mock.patch.object(guard,'unit_states',return_value=[]))
                stack.enter_context(mock.patch.object(guard,'stop_sync',return_value=[]))
                restore=stack.enter_context(mock.patch.object(guard,'restore_sync',return_value=[]))
                stack.enter_context(mock.patch.object(guard.signal,'signal'))
                stack.enter_context(mock.patch.object(guard.signal,'SIGHUP',1,create=True))
                stack.enter_context(mock.patch.object(guard.socket,'socket'))
                stack.enter_context(mock.patch.object(guard.socket,'AF_UNIX',1,create=True))
                stack.enter_context(mock.patch.object(guard.subprocess,'Popen',return_value=SimpleNamespace(pid=123,poll=lambda:0,returncode=0)))
                stack.enter_context(mock.patch.object(guard,'terminate_group',side_effect=failure))
                args=SimpleNamespace(uid=1001,gid=1001,nonce_file='unused',raw=str(root/'raw.jsonl'),go='go',binary='test',work=tmp,parent_pid=1)
                self.assertEqual(guard.serve(args),1)
                self.assertTrue(clock.sets)
                restore.assert_called_once_with([])
                audit=[json.loads(line) for line in (root/'guard/guard.jsonl').read_text().splitlines()]
                self.assertIn('child_cleanup_failed',[row['action'] for row in audit])
                self.assertIn('child_started',[row['action'] for row in audit])
                self.assertEqual(audit[-1]['action'],'finished')
                self.assertFalse(audit[-1]['ok'])


class WorkflowScopeTests(unittest.TestCase):
    def fixture(self):
        rows=[]
        clock=FakeClock();audit=[]
        def emit(event):
            event.update(PROVENANCE,guard_pid=42);audit.append(event)
        state=guard.ClockState(clock,NONCE,emit)
        emit(dict(action='start',anchor=state.anchor,ntp_before=[]))
        ident=0
        for scenario in ('ready','not_ready','preflight','final','total_prepare'):
            for direction in ('forward','backward'):
                for backend in ('sqlite','postgres'):
                    test='TestTVG2_04_RealClock/'+scenario+'/'+direction+'/'+backend
                    for round in range(20):
                        ident+=1
                        before=clock.sample()
                        shift=state.request(dict(nonce=NONCE,action='shift',round_id=ident,delta_ns=guard.STEP_NS if direction=='forward' else -guard.STEP_NS))
                        after=clock.sample()
                        delta=after['realtime_ns']-before['realtime_ns']-after['monotonic_ns']+before['monotonic_ns']
                        restore=state.request(dict(nonce=NONCE,action='restore',round_id=ident))
                        restored=clock.sample()
                        offset=restored['realtime_ns']-before['realtime_ns']-restored['monotonic_ns']+before['monotonic_ns']
                        line='OBS '+json.dumps(dict(event='real_clock_round',test=test,round=round,shift=dict(guard=shift,test_before=before,test_after=after,actual_delta_ns=delta),restore=dict(guard=restore,test_after=restored,offset_ns=offset)))+'\n'
                        # Match test2json's split Output behavior instead of relying on line-sized chunks.
                        rows.extend([dict(Action='output',Test=test,Output=line[:17]),dict(Action='output',Test=test,Output=line[17:])])
                    rows.append(dict(Action='pass',Test=test))
        state.restore('guard_finally')
        emit(dict(action='finished',ok=True,completed=400,ntp_after=[],sample=clock.sample()))
        return rows,audit

    def verify(self,rows,audit):
        with tempfile.TemporaryDirectory() as directory:
            raw,log=Path(directory)/'raw.jsonl',Path(directory)/'guard.jsonl'
            raw.write_text(''.join(json.dumps(r)+'\n' for r in rows),encoding='utf-8')
            log.write_text(''.join(json.dumps(r)+'\n' for r in audit),encoding='utf-8')
            guard.verify(raw,log)

    def test_all_20_real_leaves_and_400_rounds_are_required(self):
        rows,audit=self.fixture();self.verify(rows,audit)
        for change in ('skip','missing_round','missing_shift','missing_restore','failed_guard','unapplied_shift','wrong_restore','foreign_source','foreign_boot','backward_monotonic','independent_delta','duplicate_start'):
            r,a=copy.deepcopy(rows),copy.deepcopy(audit)
            if change=='skip':next(x for x in r if x['Action']=='pass')['Action']='skip'
            elif change=='missing_round':del r[:2]
            elif change=='missing_shift':del a[1]
            elif change=='missing_restore':del a[2]
            elif change=='unapplied_shift':a[1]['after']['realtime_ns']-=guard.STEP_NS
            elif change=='wrong_restore':a[2]['after']['realtime_ns']+=guard.STEP_NS
            elif change=='foreign_source':a[2]['source_sha']='c'*40
            elif change=='foreign_boot':a[2]['boot_id']='22222222-2222-2222-2222-222222222222'
            elif change=='backward_monotonic':a[2]['after']['monotonic_before_ns']=1
            elif change=='duplicate_start':a.insert(1,copy.deepcopy(a[0]))
            elif change=='independent_delta':
                text=r[0]['Output']+r[1]['Output'];row=json.loads(text.split('OBS ',1)[1])
                row['shift']['actual_delta_ns']=0
                text='OBS '+json.dumps(row)+'\n';r[0]['Output']=text[:17];r[1]['Output']=text[17:]
            else:a[-1]['ok']=False
            with self.subTest(change=change),self.assertRaises(ValueError):self.verify(r,a)


if __name__=='__main__':
    unittest.main()
