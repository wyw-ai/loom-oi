"""Request-window license witnesses: complete synthetic raw and hostile mutations.

No product process, guest, developer log or passing Go assertion supplies a verdict.
"""
import copy
import json
import unittest

from product.analyze import (CONTROL_B2, CONTROL_G3, CONTROL_PACKAGE, CONTROL_WITNESS,
                             _control_case, _control_license_analysis, _control_observation,
                             _g3_behavior_analysis, analyze)
from test_product import CASE, go, rows, resource_records
from test_product_g3_behavior import control, control_v2

MS = 1_000_000
PKG = CONTROL_PACKAGE


def observation(start, end=None, *, pid=200, generation=1, remaining=2500):
    process = copy.deepcopy(resource_records()[0]['oics']['lab-a'])
    process.update(pid=pid, sampled_ns=end if end is not None else start)
    process['stats'].update(generation=generation, license_remaining_ms=remaining)
    return dict(request_started_ns=start, request_completed_ns=process['sampled_ns'], process=process)


def socket_tuple(n):
    return dict(client_local=f'127.0.0.1:{100+n}', client_remote='127.0.0.1:443',
                server_local=f'127.0.0.1:{200+n}', server_remote='127.0.0.1:8443')


def connected(generation):
    return dict(msg='control connected', epoch=1, generation=generation,
                time='2026-11-05T00:00:00Z', level='INFO', run_id='synthetic')


def target_snapshot(at, present=True, done=False):
    return dict(started_ns=at, completed_ns=at+MS,
                connections=[dict(index=0, accepted_ns=10010*MS, local_address='target:1', remote_address='oic:2',
                                  data_base64='aw==', eof=done, reset=done, done=done)] if present else [])


def channel_log(at):
    return dict(started_ns=at, completed_ns=at+MS, after_index=5, ready_index=3,
                ready_event=dict(msg='ready', channel='b2-channel'), new_open_events=[], new_ready_events=[])


def b2(backend):
    test = 'TestTVB2_10_ControlCut/'+backend
    resource = resource_records(test, offset=14000*MS)
    for record in resource:
        record['backend'] = backend
        if record['kind'] == 'resources_baseline':
            record['recorded_ns'] = 1000*MS
            for process in [record['ois'], *record['oics'].values()]:
                process['sampled_ns'] = 900*MS
        if record['kind'] == 'resources_sample':
            record['oics']['lab-a']['stats']['generation'] = 2
    cut = dict(control_index=0, **socket_tuple(0), started_ns=10500*MS, completed_ns=10501*MS,
               client_close_error=None, server_close_error='use of closed network connection', remaining_ns=2399*MS)
    event = dict(kind='O4', vector='TV-B2-10', event='control_cut', test=test, backend=backend,
                 channel_id='b2-channel', access_id='lab-a', epoch=1, generation=1,
                 preparation_started_ns=10000*MS, preparation_deadline_ns=15000*MS, selection_attempts=2,
                 control_binding=dict(pid=200, control_index=0, control_tuple=socket_tuple(0), log_index=1,
                                      control_connected_event=connected(1)),
                 license_observation=observation(10400*MS, 10420*MS), cut=cut,
                 terminal=dict(at_ns=13000*MS, reason='LicenseExpired', read_bytes=2), reason='LicenseExpired', ms=2499,
                 offline=dict(deadline_ns=15500*MS, access_request_started_ns=13010*MS, access_request_completed_ns=13020*MS,
                              access_status=200, access_view=dict(access_id='lab-a', online=False, generation=1),
                              open_request_started_ns=13030*MS, open_request_completed_ns=13040*MS,
                              open_status=503, open_code='AccessOffline', channel_id='', attach_present=False),
                 targets=dict(before_dial=target_snapshot(9000*MS, False), running=target_snapshot(10200*MS),
                              fault_complete=target_snapshot(13100*MS, done=True), restored=target_snapshot(15500*MS, done=True)),
                 channel_log=channel_log(13110*MS),
                 restored=dict(connection_mark=1, control_index=1, control_tuple=socket_tuple(1), control_connected_event=connected(2),
                               license_observation=observation(14000*MS, 14010*MS, generation=2), channel_log=channel_log(15510*MS)),
                 recorded_ns=15520*MS)
    return [(test, r) for r in resource+[event]]


def g3(backend, scenario):
    b = control_v2() if scenario == 'control_blackhole' else control(scenario)
    test = f'TestTVG3_05_RevokeAndControlFailure/{backend}/{scenario}'
    b.case.update(test=test, backend=backend)
    b.test = test
    for event in b.events:
        event.update(test=test, backend=backend)
    # Use complete real-shaped process snapshots rather than Evidence's narrow
    # behavior-only bindings. The full G3 baseline graph is deliberately absent.
    def capture(start, end, generation, control_id, fault=False):
        base = resource_records()[0]
        c = dict(sample_start_ns=start, sample_end_ns=end,
                 ois=copy.deepcopy(base['ois']), oics=copy.deepcopy(base['oics']))
        c['ois']['sampled_ns'] = start+1
        c['ois']['stats']['accesses_online'] = 1 if fault else 2
        for access, proc in c['oics'].items():
            proc['sampled_ns'] = start+1
            if access == 'lab-a':
                proc['stats']['generation'] = generation
                if fault and scenario == 'control_cut':
                    proc['stats'].update(connected=False, registered=False)
        c['observed'] = dict(controls={
            'lab-a': dict(control_conn_id=control_id, control_open=not(fault and scenario == 'control_cut'),
                          downstream_paused=fault and scenario == 'control_blackhole',
                          pending_control_ids=['access/lab-a/50'] if fault and scenario == 'control_cut' else []),
            'lab-b': dict(control_conn_id='access/lab-b/1', control_open=True, downstream_paused=False, pending_control_ids=[])},
            active_data_connection_ids=[], active_target_worker_ids=[], unclassified_connection_ids=[])
        return c
    resources = [dict(schema='g3-resource-v1', kind='g3_resources_baseline', package=PKG, test=test, backend=backend,
                      vector='TV-G3-05', scenario=scenario, stage='preparation', recorded_ns=1)]
    for index, phase in enumerate(b.checks):
        fault, generation = index == 0, 2 if index == 0 else 3
        start, ref = phase['start_ns'], f'access/lab-a/{index+1}'
        first = capture(start, start+10*MS, generation, ref, fault)
        identity = dict(ois_pid=100, oics={a:dict(pid=p['pid'], epoch=1, generation=p['stats']['generation']) for a,p in first['oics'].items()},
                        control_conn_ids={a:p['control_conn_id'] for a,p in first['observed']['controls'].items()},
                        pending_control_ids={a:p['pending_control_ids'] for a,p in first['observed']['controls'].items()})
        common = dict(schema='g3-resource-v1', package=PKG, test=test, backend=backend, vector='TV-G3-05', scenario=scenario,
                      stage='validation', phase=phase['phase'], cycle=None, profile_id='fault' if fault else 'online',
                      profile_kind=scenario if fault else 'online', check_id=phase['check_id'], start_ns=start, deadline_ns=start+2000*MS)
        resources.append(dict(common, kind='g3_resources_begin', origin_ns=start,
                              origin_event='control_termination_and_offline_observed' if fault else 'control_restored',
                              binding=identity, binding_samples=[first]))
        for i in range(3):
            at=start+(20+150*i)*MS
            resources.append(dict(common, kind='g3_resources_sample', **capture(at, at+20*MS, generation, ref, fault)))
        resources.append(dict(common, kind='g3_resources_complete', completed_ns=phase['completed_ns'], settled_ms=500, samples=3))
    applied=b.get('control_applied')
    applied['license_observation']=observation(applied['applied_ns']-MS, applied['applied_ns']-MS, generation=2)
    applied['license_observation']['process']['stats']['closed']=copy.deepcopy(applied['oic_closed_before'])
    applied.update(license_remaining_at_apply_ns=2499*MS, license_remaining_at_apply_ms=2499,
                   license_sampled_ns=applied['applied_ns']-MS)
    if scenario == 'control_blackhole':
        b.get('expiry_marker_rejected')['oic_stats']['epoch']=1
        for name in ('blackhole_new_open', 'control_failure_verified'):
            for key in ('ois_before', 'ois_after'):
                if key in b.get(name):
                    b.get(name)[key]['pid']=100
                    b.get(name)[key]['stats']['epoch']=1
    rehearsal=[]
    if scenario=='control_blackhole':
        rehearsal=[dict(kind='G3-preparation',vector='TV-G3-05',test=test,backend=backend,scenario=scenario,
                        stage='preparation',event='blackhole_phase',applied_ns=9000*MS,phase_sampled_ns=8999*MS,license_remaining_ms=1700)]
    return [(test,r) for r in rehearsal+resources+b.events]


def fixture():
    pairs=[]
    for backend in ('sqlite','postgres'):
        pairs.extend(b2(backend))
        for scenario in ('control_cut','control_blackhole'):
            pairs.extend(g3(backend,scenario))
    records=[('vectors',PKG,test,r) for test,r in pairs]
    results={('vectors',PKG,t):['pass'] for t in CONTROL_B2+CONTROL_G3}
    return records,results


def find(records, test=CONTROL_B2[0], **fields):
    return next(r for _,_,t,r in records if t==test and all(r.get(k)==v for k,v in fields.items()))


class ControlLicenseTests(unittest.TestCase):
    def assert_bad(self, mutate):
        records,results=fixture()
        mutate(records,results)
        summary,problems=_control_license_analysis(records,results,1)
        self.assertEqual(summary['result'],'fail',summary)
        self.assertTrue(problems)

    def test_complete_six_leaves_without_implicit_full_g3_gate(self):
        records,results=fixture()
        summary,problems=_control_license_analysis(records,results,1)
        self.assertEqual(problems,[])
        self.assertEqual(summary['result'],'pass')
        self.assertEqual(len(summary['checked']),6)
        self.assertNotIn('g3_resources',summary)

    def test_request_window_exact_bounds_and_old_after_formula_counterexample(self):
        o=observation(1000*MS,1300*MS,remaining=2250)
        self.assertEqual(_control_observation(o,1301*MS)[0],1949*MS)
        # The legacy after-response formula would claim 2249ms instead.
        self.assertEqual(2250*MS-(1301*MS-o['process']['sampled_ns']),2249*MS)
        o=observation(1000*MS,1200*MS,remaining=2500)
        self.assertEqual(_control_observation(o,1500*MS)[0],2000*MS)
        self.assertEqual(_control_observation(o,1500*MS+1)[0],2000*MS-1)
        self.assertEqual(_control_observation(o,4000*MS)[0],0)
        self.assertEqual(_control_observation(observation(1000*MS),1000*MS)[0],2500*MS)

    def test_observation_shape_numbers_identity_and_overflow(self):
        changes=[lambda o:o.pop('process'),lambda o:o.update(request_started_ns=None),lambda o:o.update(request_completed_ns=True),
                 lambda o:o.update(request_started_ns=1.0),lambda o:o.update(request_started_ns=1<<63),
                 lambda o:o.update(request_started_ns=1002*MS),lambda o:o['process'].update(sampled_ns=999*MS),
                 lambda o:o['process'].update(pid=False),lambda o:o['process']['stats'].update(access_id='lab-b'),
                 lambda o:o['process']['stats'].update(generation=0),lambda o:o['process']['stats'].update(connected=False),
                 lambda o:o['process']['stats'].update(license_remaining_ms=3001),lambda o:o['process']['stats'].update(license_remaining_ms=0),
                 lambda o:o['process']['stats'].pop('process')]
        for i,mutate in enumerate(changes):
            with self.subTest(i=i):
                o=observation(1000*MS,1001*MS);mutate(o)
                with self.assertRaises((ValueError,TypeError,KeyError)):_control_observation(o,1001*MS)

    def test_missing_every_b2_field_cannot_be_saved_by_go_pass(self):
        sample=find(fixture()[0],event='control_cut')
        for key in sample:
            with self.subTest(key=key):
                self.assert_bad(lambda r,_:find(r,event='control_cut').pop(key))
        for section in ('cut','terminal','offline','control_binding','restored','license_observation'):
            for key in sample[section]:
                with self.subTest(section=section,key=key):
                    self.assert_bad(lambda r,_,s=section,k=key:find(r,event='control_cut')[s].pop(k))

    def test_b2_binding_refusal_target_and_resource_mutations(self):
        mutations={
            'pid':lambda e:e['license_observation']['process'].update(pid=201),
            'epoch':lambda e:e.update(epoch=2),'generation':lambda e:e.update(generation=2),
            'access':lambda e:e.update(access_id='lab-b'),'channel':lambda e:e.update(channel_id='other'),
            'control':lambda e:e['cut'].update(control_index=1),
            'tuple':lambda e:e['cut'].update(client_local='other'),
            'cut_reversed':lambda e:e['cut'].update(started_ns=e['cut']['completed_ns']+1),
            'deadline':lambda e:e.update(preparation_deadline_ns=e['cut']['completed_ns']-1),
            'future':lambda e:e['license_observation'].update(request_completed_ns=e['cut']['completed_ns']+1),
            'rtt':lambda e:e['license_observation'].update(request_started_ns=10000*MS),
            'offline_code':lambda e:e['offline'].update(open_code='Denied'),
            'offline_channel':lambda e:e['offline'].update(channel_id='created'),
            'offline_ticket':lambda e:e['offline'].update(attach_present=True),
            'offline_access':lambda e:e['offline']['access_view'].update(access_id='lab-b'),
            'offline_generation':lambda e:e['offline']['access_view'].update(generation=2),
            'open_before_get':lambda e:e['offline'].update(open_request_started_ns=e['offline']['access_request_completed_ns']-1),
            'target_replaced':lambda e:e['targets']['restored']['connections'][0].update(remote_address='replacement'),
            'target_added':lambda e:e['targets']['fault_complete']['connections'].append(copy.deepcopy(e['targets']['fault_complete']['connections'][0])),
            'target_no_running':lambda e:e['targets']['running']['connections'][0].update(data_base64=''),
            'target_bad_base64':lambda e:e['targets']['running']['connections'][0].update(data_base64='aw=\n='),
            'target_capacity':lambda e:e['targets']['running']['connections'][0].update(data_base64='eA=='*1025),
            'replay_after_restore':lambda e:e['restored']['channel_log'].update(new_ready_events=[{'msg':'ready'}]),
            'reused_generation':lambda e:e['restored']['license_observation']['process']['stats'].update(generation=1),
            'skipped_restore_connection':lambda e:e['restored'].update(control_index=2),
            'wrong_restored_log':lambda e:e['restored']['control_connected_event'].update(generation=3),
            'observed_before_cleanup':lambda e:e.update(recorded_ns=15000*MS),
        }
        for name,mutate in mutations.items():
            with self.subTest(name=name):self.assert_bad(lambda r,_,m=mutate:m(find(r,event='control_cut')))
        for field,value in [('pid',999),('sampled_ns',0)]:
            self.assert_bad(lambda r,_,k=field,v=value:find(r,kind='resources_sample')['oics']['lab-a'].update({k:v}))
        self.assert_bad(lambda r,_:find(r,kind='resources_sample')['oics']['lab-b']['stats'].update(generation=2))
        self.assert_bad(lambda r,_:r.remove(next(x for x in r if x[2]==CONTROL_B2[0] and x[3]['kind']=='resources_begin')))

    def test_b2_terminal_early_late_nanosecond_and_interval_boundaries(self):
        for delta in (-1,0,1):
            records,results=fixture();e=find(records,event='control_cut')
            e['terminal']['at_ns']=e['cut']['completed_ns']+2000*MS+delta
            e['ms']=(e['terminal']['at_ns']-e['cut']['completed_ns'])//MS
            summary,_=_control_license_analysis(records,results,1)
            self.assertEqual(summary['result'],'fail' if delta<0 else 'pass')
        # Upper boundary is measured from first Close, never the second Close.
        for delta in (-1,0,1):
            records,results=fixture();e=find(records,event='control_cut')
            e['terminal']['at_ns']=14500*MS+delta
            e['ms']=(e['terminal']['at_ns']-e['cut']['completed_ns'])//MS
            e['offline'].update(access_request_started_ns=14510*MS,access_request_completed_ns=14520*MS,
                                open_request_started_ns=14530*MS,open_request_completed_ns=14540*MS)
            e['targets']['fault_complete']=target_snapshot(14550*MS,done=True)
            e['channel_log']=channel_log(14560*MS)
            e['restored']['license_observation']=observation(14700*MS,14710*MS,generation=2)
            summary,problems=_control_license_analysis(records,results,1)
            self.assertEqual(summary['result'],'fail' if delta>0 else 'pass',problems)

    def test_exact_two_second_inputs_and_new_g3_branch_acceptance(self):
        for delta in (0,1):
            records,results=fixture();e=find(records,event='control_cut')
            e['license_observation']=observation(10251*MS-delta,10420*MS,remaining=2250)
            e['cut']['remaining_ns']=2000*MS-delta
            summary,problems=_control_license_analysis(records,results,1)
            self.assertEqual(summary['result'],'pass' if delta==0 else 'fail',problems)
        for delta in (0,1):
            records,results=fixture();test=CONTROL_G3[0]
            e=find(records,test,event='control_applied');at=e['applied_ns']
            find(records,test,event='ready')['observed_ns']=at-300*MS
            e['license_observation']=observation(at-250*MS-delta,at-MS,generation=2,remaining=2250)
            e['license_observation']['process']['stats']['closed']=copy.deepcopy(e['oic_closed_before'])
            e.update(license_remaining_ms=2250,license_remaining_at_apply_ns=2000*MS-delta,
                     license_remaining_at_apply_ms=(2000*MS-delta)//MS)
            summary,problems=_control_license_analysis(records,results,1)
            self.assertEqual(summary['result'],'pass' if delta==0 else 'fail',problems)
            # For delta=0 the legacy formula says 2249ms, not the truthful
            # 2000ms alias. Passing proves the new path does not first apply
            # the incompatible historical formula and silently waive it.
            self.assertEqual(e['license_remaining_ms']-(at-e['license_sampled_ns']+MS-1)//MS,2249)

    def test_g3_full_rtt_identity_aliases_and_phase_mutations(self):
        test=CONTROL_G3[0]
        mutations={
            'missing':lambda e:e.pop('license_observation'),
            'ns_bool':lambda e:e.update(license_remaining_at_apply_ns=True),
            'old_alias':lambda e:e.update(license_remaining_at_apply_ms=2401),
            'sample_alias':lambda e:e.update(license_sampled_ns=e['license_sampled_ns']+1),
            'remaining_alias':lambda e:e.update(license_remaining_ms=2501),
            'rtt_old_pass':lambda e:e['license_observation'].update(request_started_ns=e['applied_ns']-501*MS),
            'future':lambda e:e['license_observation'].update(request_completed_ns=e['applied_ns']+1),
            'pid':lambda e:e['license_observation']['process'].update(pid=201),
            'generation':lambda e:e['license_observation']['process']['stats'].update(generation=3),
            'channel':lambda e:e.update(channel_id='other'),'control':lambda e:e.update(control_conn_id='access/lab-a/99'),
        }
        for name,mutate in mutations.items():
            with self.subTest(name=name):self.assert_bad(lambda r,_,m=mutate:m(find(r,test,event='control_applied')))
        self.assert_bad(lambda r,_:find(r,test,event='control_terminated').update(stream_end_ns=find(r,test,event='control_applied')['applied_ns']+2000*MS-1))
        self.assert_bad(lambda r,_:find(r,test,event='control_terminated').update(stream_end_ns=find(r,test,event='control_applied')['applied_ns']+4000*MS+1))
        self.assert_bad(lambda r,_:find(r,test,kind='g3_resources_begin')['binding_samples'][0]['oics'].pop('lab-b'))
        self.assert_bad(lambda r,_:find(r,test,kind='g3_resources_sample')['oics']['lab-a'].update(pid=999))
        self.assert_bad(lambda r,_:find(r,test,kind='g3_resources_complete').update(samples=2))
        self.assert_bad(lambda r,_:r.remove(next(x for x in r if x[2]==test and x[3]['kind']=='g3_resources_complete')))
        self.assert_bad(lambda r,_:find(r,test,event='control_applied').update(input_error='selection failed'))
        self.assert_bad(lambda r,_:find(r,test,event='control_applied')['oic_closed_before'].update(LicenseExpired=99))
        self.assert_bad(lambda r,_:find(r,test,kind='g3_resources_begin')['binding_samples'][0]['ois']['stats'].update(epoch=2))

    def test_all_nested_witness_fields_are_required(self):
        records,_=fixture();e=find(records,event='control_cut')
        sections=[('license_observation','process'),('license_observation','process','stats'),
                  ('control_binding','control_tuple'),('restored','license_observation'),('restored','channel_log'),
                  ('targets','before_dial'),('targets','running'),('targets','fault_complete'),('targets','restored')]
        def descend(value,path):
            for k in path:value=value[k]
            return value
        for path in sections:
            for key in descend(e,path):
                # config_error is an optional legacy stats field.
                if key=='config_error':continue
                with self.subTest(path=path,key=key):
                    self.assert_bad(lambda r,_,p=path,k=key:descend(find(r,event='control_cut'),p).pop(k))
        for test in CONTROL_G3:
            sample=find(records,test,event='control_applied')['license_observation']
            for key in sample:
                with self.subTest(test=test,key=key):
                    self.assert_bad(lambda r,_,t=test,k=key:find(r,t,event='control_applied')['license_observation'].pop(k))

    def test_exact_inventory_and_go_result_provenance(self):
        for test in CONTROL_B2+CONTROL_G3:
            with self.subTest(test=test):
                self.assert_bad(lambda r,_,t=test:r.__setitem__(slice(None),[x for x in r if x[2]!=t]))
                self.assert_bad(lambda _,s,t=test:s[('vectors',PKG,t)].append('pass'))
                self.assert_bad(lambda _,s,t=test:s.__setitem__(('vectors',PKG,t),['skip']))
                self.assert_bad(lambda _,s,t=test:s.__setitem__(('vectors',PKG,t),['fail']))
        self.assert_bad(lambda r,_:r.append(copy.deepcopy(next(x for x in r if x[3].get('event')=='control_cut'))))
        self.assert_bad(lambda r,_:r.__setitem__(0,('vectors','other',r[0][2],r[0][3])))
        self.assert_bad(lambda r,_:r.__setitem__(0,('unit',PKG,r[0][2],r[0][3])))
        self.assert_bad(lambda r,_:r[0][3].update(backend='postgres'))
        self.assert_bad(lambda r,_:r[0][3].update(test=CONTROL_B2[1]))

    def test_existing_preparation_observation_is_not_formal_license_evidence(self):
        test=CONTROL_G3[1]
        for key,value in [('stage','validation'),('event','control_applied'),('license_remaining_ms',2500),
                          ('phase_sampled_ns',9000*MS+1),('schema','g3-behavior-v1'),('scenario','control_cut')]:
            with self.subTest(key=key):
                self.assert_bad(lambda r,_,k=key,v=value:find(r,test,kind='G3-preparation').update({k:v}))
        self.assert_bad(lambda r,_:r.append(copy.deepcopy(next(x for x in r if x[2]==test and x[3]['kind']=='G3-preparation'))))
        self.assert_bad(lambda r,_:r.remove(next(x for x in r if x[2]==test and x[3].get('event')=='control_applied')))

    def test_capability_is_explicit_and_legacy_summary_unchanged(self):
        self.assertFalse(_control_case(CASE))
        legacy=analyze(CASE,rows(),1)
        self.assertNotIn('control_license_witness',legacy)
        self.assertEqual(legacy['verdict'],'pass')
        for value in (None,True,False,1,1.0,{},'request-window-v2'):
            with self.subTest(value=value),self.assertRaises(ValueError):analyze(dict(CASE,control_license_witness=value),rows(),1)
        for scenario in ('control_cut','control_blackhole'):
            b=control_v2() if scenario=='control_blackhole' else control(scenario)
            summary,problems=b.run()
            self.assertEqual(problems,[])
            self.assertEqual(summary['result'],'pass')

    def test_collector_complete_output_and_global_verdict(self):
        records,results=fixture();extra=[]
        for step,pkg,test,record in records:
            line='OBS '+json.dumps(record)+'\n'
            for part in (line[:31],line[31:]):
                r=go(step,'output',test,part);r['event']['Package']=pkg;extra.append(r)
        for step,pkg,test in results:
            r=go(step,'pass',test);r['event']['Package']=pkg;extra.append(r)
        case=dict(CASE,control_license_witness=CONTROL_WITNESS)
        summary=analyze(case,rows(extra=extra),1)
        self.assertEqual(summary['verdict'],'pass',summary['problems'])
        self.assertEqual(summary['control_license_witness']['result'],'pass')
        self.assertNotIn('g3_resources',summary)
        # One complete bad OBS must affect global verdict even while all Go
        # leaves, original vectors and original criteria still say pass.
        victim=next(r for r in extra if r['kind']=='go' and r['event'].get('Output','').startswith('OBS '))
        victim['event']['Package']='forged'
        summary=analyze(case,rows(extra=extra),1)
        self.assertEqual(summary['verdict'],'fail')
        self.assertEqual(summary['control_license_witness']['result'],'fail')


if __name__=='__main__':
    unittest.main()
