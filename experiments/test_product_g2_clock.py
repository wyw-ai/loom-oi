"""Synthetic raw evidence only: no test changes a host clock or launches Go."""
import base64
import copy
import importlib.util
import json
from pathlib import Path
import unittest


spec=importlib.util.spec_from_file_location('g2_clock_tested',Path(__file__).parent/'product/g2_clock_analyze.py')
clock=importlib.util.module_from_spec(spec)
spec.loader.exec_module(clock)
MS,SECOND=clock.MS,clock.SECOND
SOURCE='a'*40
TEST_PID,GUARD_PID=101,102
PROVENANCE=dict(schema='g2-clock-guard-v1',run_id='123',job='clock',source_sha=SOURCE,
    nonce_sha256='b'*64,boot_id='11111111-1111-1111-1111-111111111111',
    workflow_ref='wyw-ai/loom-oi/.github/workflows/g2-clock.yml@refs/heads/clock/test',guard_pid=GUARD_PID)


def encoded(value):
    raw=value if isinstance(value,bytes) else json.dumps(value,separators=(',',':')).encode()
    return base64.b64encode(raw).decode()


def binding():
    return dict(access_id='lab-a',epoch=1,generation=1,exposure_id='lab-a/http',revision='rv_source',
        observation_revision='ob_1',contract_digest='a'*64,source_id='local',contract_version=1)


def stats():
    return dict(channels=dict(preparing=0,open=0,closing=0),tickets=0,
        observation=dict(pending_opens=0,http_waiters=0,pending_checks=0,pending_queries=0))


class Fixture:
    """Construct independent clocks and complete bytes; no validator is mocked."""
    def __init__(self):
        self.records=[];self.results={};self.audit=[];self.rounds=[]
        self.resource=dict(result='pass',completed_checks={})
        self.emit(dict(action='start',anchor=self.sample(SECOND),ntp_before=[],runner_uid=1001))
        self.emit(dict(action='test_peer',test_pid=TEST_PID,test_uid=1001))
        for test,group in clock.EXPECTED['groups'].items():
            self.group(test,group)
        self.emit(dict(action='finished',ok=True,completed=400,ntp_after=[],sample=self.sample(2005*SECOND)))

    def sample(self,at,delta=0):
        mono=at+500*SECOND
        return dict(realtime_ns=1_800_000_000_000_000_000+at+delta,monotonic_ns=mono,
            monotonic_before_ns=mono,monotonic_after_ns=mono)

    def independent(self,at,delta=0):
        return dict(self.sample(at,delta),resource_ns=at)

    def emit(self,event):
        event.update(PROVENANCE);self.audit.append(copy.deepcopy(event));return event

    def witness(self,at,ident,delta):
        event=self.emit(dict(action='witness',active_round=ident,sample=self.sample(at+MS,delta),
            peer_pid=TEST_PID,ok=True))
        return dict(test=self.independent(at,delta),offset_ns=delta,guard=event)

    def clocks(self,row,start,terminal,release_witness=None):
        ident=len(self.rounds)+1;delta=30*SECOND if row['direction']=='forward' else -30*SECOND
        before=self.independent(start);after=self.independent(start+3*MS,delta)
        actual=self.emit(dict(action='shift',round_id=ident,requested_delta_ns=delta,
            before=self.sample(start+MS),after=self.sample(start+2*MS,delta),actual_delta_ns=delta))
        actual=dict(actual,ok=True,peer_pid=TEST_PID)
        row.update(delta_ns=delta,guard_pid=GUARD_PID,shift=dict(guard=actual,test_before=before,
            test_after=after,test_pid=TEST_PID,actual_delta_ns=delta),terminal_ns=terminal,cleanup_origin_ns=terminal)
        if release_witness is None:
            row['live_witness']=self.witness(start+10*MS,ident,delta)
            witness_at=terminal+MS
        else:
            row['late_release_witness']=self.witness(release_witness,ident,delta)
            witness_at=max(terminal+MS,row['late_written']['WrittenNS']+MS)
        row['terminal_witness']=self.witness(witness_at,ident,delta)
        rb=witness_at+2*MS
        restore=self.emit(dict(action='restore',round_id=ident,cause='request',
            before=self.sample(rb+MS,delta),after=self.sample(rb+2*MS),actual_delta_ns=-delta))
        row['restore']=dict(guard=dict(restore,ok=True,peer_pid=TEST_PID),test_before=self.independent(rb,delta),
            test_after=self.independent(rb+3*MS),offset_ns=0)
        row['resource_completed_ns']=terminal+300*MS

    def add(self,test,event,**fields):
        row=dict(schema='g2-behavior-v1',kind='G2',vector='G2-04-clock',event=event,test=test,
            backend=test.split('/')[-1],observed_ns=1,**fields)
        self.records.append((clock.PACKAGE,test,row));return row

    def b4(self,test,kind,**fields):
        row=dict(schema='b4-resource-v2',kind=kind,test=test,backend=test.split('/')[-1],**fields)
        self.records.append((clock.PACKAGE,test,row));return row

    def group(self,test,group):
        for n in range(1,5):self.results[clock.PACKAGE,'/'.join(test.split('/')[:n])]=['pass']
        base=(len(self.rounds)*5+2)*SECOND
        pids=[201+len(self.rounds)*3,202+len(self.rounds)*3,203+len(self.rounds)*3]
        baseline=self.b4(test,'resources_baseline',profile_id=test,recorded_ns=base-MS,
            ois=dict(pid=pids[0],stats=dict(epoch=1)),oics={a:dict(pid=pids[i+1],stats=dict(epoch=1,generation=1)) for i,a in enumerate(('lab-a','lab-b'))})
        self.add(test,'clock_preconditions',product_pids=pids,tokens='fixture_nonexpiring',clock_changed=False)['observed_ns']=base
        self.resource['completed_checks'][test]=20
        events=[];conns=[dict(ID=1,Role='control',ChannelID='',AcceptedNS=1,Closed=False,ClosedNS=0,Workers=2)]
        health=[]
        for rnd in range(20):
            at=base+rnd*5*SECOND
            row=self.add(test,'real_clock_round',round=rnd,scenario=group['scenario'],direction=group['direction'],
                product_pids=pids,session_identity=dict(ois_epoch=1,lab_a_epoch=1,lab_a_generation=1,lab_b_epoch=1,lab_b_generation=1))
            def wire(body,when,stage='',write=None):
                ident=len(events)+1;body=dict(body,seq=ident)
                direction='ois_to_oic' if body['type'] in ('observation_query','admission_check','prepare_channel') else 'oic_to_ois'
                event=dict(ID=ident,ConnID=1,Role='control',Direction=direction,Type=body['type'],Stage=stage,
                    ChannelID=body.get('channel_id',''),CheckID=body.get('check_id',''),QueryID=body.get('query_id',''),
                    MessageType=1,Raw=encoded(body),ReceivedNS=when,WriteStartedNS=when+1 if write is None else write,
                    WrittenNS=when+2 if write is None else write+1,Injected=False,WriteFailed=False,Dropped=False)
                events.append(event);return event
            if group['scenario'] in ('ready','not_ready'):
                qid=format(len(self.rounds)+1,'032x');state=group['scenario'];seq=rnd+1
                query=wire(dict(type='observation_query',query_id=qid,binding=binding()),at+10*MS,write=at+30*MS)
                response=wire(dict(type='observation_snapshot',query_id=qid,binding=binding(),readiness=state,sample_seq=seq,remaining_ms=1000),at+40*MS,write=at+50*MS)
                source=wire(dict(type='observation_snapshot',query_id=qid,binding=binding(),readiness='unknown',reason='expired',remaining_ms=0),at+1040*MS)
                live=dict(readiness=state,readiness_view=dict(sample_seq=seq,remaining_ms=950,last_reported=dict(sample_seq=seq)))
                expired=dict(readiness='unknown',readiness_view=dict(remaining_ms=0,last_reported=dict(sample_seq=seq)))
                row.update(query=query,response=response,response_written=response,source_expired=source,
                    stopped_health_version=rnd+1,view=live,view_started_ns=at+60*MS,view_ended_ns=at+61*MS,
                    remaining_upper_bound_ms=950,expiry_lower_ns=at+1010*MS,expiry_upper_ns=at+1012*MS,
                    expiry_trajectory=[dict(started_ns=at+1010*MS,ended_ns=at+1011*MS,view=expired)],
                    expired=expired,directory_expired_ns=at+1011*MS)
                health.append(dict(ID=rnd+1,Version=rnd+1,StartedNS=at+200*MS,HeaderStartedNS=0,
                    HeaderWrittenNS=0,CancelledNS=at+1100*MS,FinishedNS=at+1100*MS))
                self.clocks(row,at+20*MS,at+1041*MS)
            else:
                scenario=group['scenario'];pre=scenario=='preflight';total=scenario=='total_prepare'
                cid='ch_'+str(len(self.rounds)+1);checkid=format(len(self.rounds)+1,'032x')
                if not pre:wire(dict(type='prepare_channel',channel_id=cid),at+MS)
                stage='preflight' if pre else 'final';checktime=at+(10 if pre else 1710 if total else 20)*MS
                deadline=280 if total else 499
                body=dict(type='admission_check',check_id=checkid,stage=stage,binding=binding(),effective_admission='require_ready',deadline_ms=deadline)
                if not pre:body['channel_id']=cid
                check=wire(body,checktime,stage)
                upper=checktime+(deadline+1)*MS
                result=dict(type='admission_result',check_id=checkid,binding=binding(),effective_admission='require_ready',
                    decision_id=format(len(self.rounds)+1000,'032x'),readiness='ready',sample_seq=rnd+1,remaining_ms=1000)
                if not pre:result['channel_id']=cid
                else:result.pop('decision_id')
                held=wire(result,checktime+10*MS,stage,write=upper+4*MS)
                terminal=at+(505 if pre else 2001 if total else 515)*MS
                row.update(held=held,check=check,late_written=held,deadline_upper_ns=upper,
                    release_requested_ns=upper+3*MS,target=[] if pre else [dict(Data=None,Done=True)])
                if pre:
                    row.update(open_outcome=dict(status=503,code='ReadinessCheckFailed',StartedNS=at,CompletedNS=terminal),http_reason='timeout',after=stats())
                else:
                    code='PrepareTimeout' if total else 'ReadinessCheckFailed'
                    row.update(open_outcome=dict(status=201,open=dict(channel_id=cid),StartedNS=at,CompletedNS=at+5*MS),
                        channel=dict(channel_id=cid,state='closed',close_reason=code,readiness_decision=dict(reason='timeout')),
                        consumer_abort=dict(Type=4,Reason=code),attach_started_ns=at+(1700 if total else 10)*MS)
                    self.add(test,'commit_cleanup',channel_id=cid,origin_ns=terminal,finished_ns=terminal+100*MS,
                        target_closed_observed_ns=terminal+50*MS,target=[dict(Data=None,Done=True)],
                        ois=stats(),oic=dict(channels=0,observation=stats()['observation']))
                self.clocks(row,checktime+20*MS,terminal,upper+MS)
            self.rounds.append(row)
            row['observed_ns']=row['resource_completed_ns']+1
            start=row['terminal_ns'];checkid=str(len(self.rounds))
            self.b4(test,'resources_begin',profile_id=baseline['profile_id'],check_id=checkid,started_ns=start,deadline_ns=start+2*SECOND)
            self.b4(test,'resources',profile_id=baseline['profile_id'],check_id=checkid,completed_ns=start+250*MS,samples=3)
        for access in ('lab-a','lab-b'):
            self.add(test,'wire_history',access_id=access,proxy=dict(PID=TEST_PID,Workers=2,TransportConnections=1,
                Overflow=False,Failure='',Connections=conns,Events=events if access=='lab-a' else []))['observed_ns']=base+100*SECOND
        self.add(test,'health_history',exposure_id='lab-a/http',health=dict(PID=TEST_PID,Overflow=False,Records=health))

    def run(self):
        return clock.analyze_clock(clock.expected_spec(),self.records,self.results,self.audit,self.resource,SOURCE)


class ClockBehaviorTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):cls.original=Fixture()

    def fresh(self):return copy.deepcopy(self.original)

    def test_all_fixed_groups_and_terminal_before_late_release_are_supported(self):
        f=self.fresh();summary,problems=f.run()
        self.assertEqual(problems,[])
        self.assertEqual(summary['result'],'pass')
        self.assertEqual(len(summary['groups']),20)
        self.assertEqual(sum(g['rounds'] for g in summary['groups'].values()),400)
        self.assertTrue(any(r['terminal_ns']<r['release_requested_ns'] for r in f.rounds if r['scenario']=='final'))
        self.assertIn('ordinary_G2-04',summary['sample_scope'])

    def test_manifest_cannot_remove_clock_leaf_or_reduce_rounds(self):
        for operation in ('remove','reduce'):
            spec=clock.expected_spec();first=next(iter(spec['groups']))
            if operation=='remove':del spec['groups'][first]
            else:spec['groups'][first]['rounds']=19
            with self.subTest(operation=operation),self.assertRaises(ValueError):
                clock.analyze_clock(spec,[],{},[],None,SOURCE)

    def test_independent_audit_and_source_are_required_not_a_summary_boolean(self):
        for operation in ('audit_missing','source_foreign','peer_missing','witness_unlogged','syscall_unapplied','restore_missing'):
            f=self.fresh()
            if operation=='audit_missing':f.audit=[]
            elif operation=='source_foreign':
                for e in f.audit:e['source_sha']='c'*40
            elif operation=='peer_missing':f.audit=[e for e in f.audit if e['action']!='test_peer']
            elif operation=='witness_unlogged':f.audit.remove(next(e for e in f.audit if e['action']=='witness'))
            elif operation=='syscall_unapplied':next(e for e in f.audit if e['action']=='shift')['after']['realtime_ns']-=30*SECOND
            else:f.audit.remove(next(e for e in f.audit if e['action']=='restore'))
            with self.subTest(operation=operation):self.assertEqual(f.run()[0]['result'],'fail')

    def test_go_pass_cannot_replace_round_identity_and_provenance(self):
        for operation in ('missing_round','duplicate_trace','wrong_direction','wrong_backend','wrong_package','failed_parent','restarted_pid','replaced_generation'):
            f=self.fresh();r=f.rounds[0]
            if operation=='missing_round':r.pop('round')
            elif operation=='duplicate_trace':f.rounds[1]['query']=r['query']
            elif operation=='wrong_direction':r['direction']='backward'
            elif operation=='wrong_backend':r['backend']='postgres'
            elif operation=='wrong_package':f.records[0]=('foreign',*f.records[0][1:])
            elif operation=='failed_parent':f.results[clock.PACKAGE,clock.ROOT]=['fail']
            elif operation=='restarted_pid':r['product_pids']=[301,302,303]
            else:r['session_identity']['lab_a_generation']=2
            with self.subTest(operation=operation):self.assertEqual(f.run()[0]['result'],'fail')

    def test_real_step_witnesses_cannot_be_relabelled_or_outlive_cleanup(self):
        for operation in ('not_shifted','resource_origin','duplicate_witness','late_restore','reset_cleanup','late_resource','unobserved_terminal'):
            f=self.fresh();r=f.rounds[0]
            if operation=='not_shifted':r['live_witness']['test']['realtime_ns']-=30*SECOND
            elif operation=='resource_origin':r['live_witness']['test']['resource_ns']+=MS
            elif operation=='duplicate_witness':r['live_witness']=r['terminal_witness']
            elif operation=='late_restore':r['restore']['test_after']['resource_ns']=r['terminal_ns']+2*SECOND+1
            elif operation=='reset_cleanup':r['cleanup_origin_ns']+=MS
            elif operation=='late_resource':r['resource_completed_ns']=r['terminal_ns']+2*SECOND+1
            else:r['terminal_ns']=r['terminal_witness']['test']['resource_ns']+MS;r['cleanup_origin_ns']=r['terminal_ns']
            with self.subTest(operation=operation):self.assertEqual(f.run()[0]['result'],'fail')

    def test_ttl_uses_original_sample_rtt_and_both_actual_endpoint_expiries(self):
        for operation in ('rtt_refund','premature_expiry','late_authority','replacement_sample','source_not_expired','new_response','wrong_expired_ref'):
            f=self.fresh();r=f.rounds[0]
            if operation=='rtt_refund':r['view']['readiness_view']['remaining_ms']+=1
            elif operation=='premature_expiry':r['expiry_trajectory'][-1]['ended_ns']=r['expiry_lower_ns']-1
            elif operation=='late_authority':
                poll=copy.deepcopy(r['expiry_trajectory'][0]);poll['view']=copy.deepcopy(r['view'])
                poll['started_ns']=r['expiry_upper_ns'];poll['ended_ns']=r['expiry_upper_ns']+1
                r['expiry_trajectory'].insert(0,poll)
            elif operation=='replacement_sample':r['view']['readiness_view']['last_reported']['sample_seq']+=1
            elif operation=='source_not_expired':r['source_expired']=r['response']
            elif operation=='new_response':
                health=next(x for _,_,x in f.records if x.get('event')=='health_history')
                health['health']['Records'][0]['HeaderStartedNS']=health['health']['Records'][0]['StartedNS']+1
            else:r['expired']=copy.deepcopy(r['view'])
            with self.subTest(operation=operation):self.assertEqual(f.run()[0]['result'],'fail')

    def test_original_deadline_late_positive_and_zero_payload_are_independent_requirements(self):
        for operation in ('early_release','invented_deadline','shift_after_budget','early_abort','wrong_abort','payload','ready','restore_before_write','fake_positive'):
            f=self.fresh();r=next(r for r in f.rounds if r['scenario']=='final')
            if operation=='early_release':r['release_requested_ns']=r['deadline_upper_ns']-1
            elif operation=='invented_deadline':r['deadline_upper_ns']+=1
            elif operation=='shift_after_budget':r['shift']['test_after']['resource_ns']=r['attach_started_ns']+500*MS
            elif operation=='early_abort':r['terminal_ns']=r['attach_started_ns']+499*MS;r['cleanup_origin_ns']=r['terminal_ns']
            elif operation=='wrong_abort':r['consumer_abort']['Reason']='Completed'
            elif operation=='payload':r['target'][0]['Data']=encoded(b'leaked')
            elif operation=='ready':
                hist=next(x for _,test,x in f.records if test==r['test'] and x.get('event')=='wire_history' and x['access_id']=='lab-a')
                hist['proxy']['Connections'].append(dict(ID=2,Role='provider',ChannelID=r['channel']['channel_id'],AcceptedNS=1,Closed=False,ClosedNS=0,Workers=2))
                hist['proxy']['Events'].append(dict(ID=1000,ConnID=2,Role='provider',Direction='ois_to_oic',Type='READY',Stage='',ChannelID=r['channel']['channel_id'],CheckID='',QueryID='',MessageType=2,Raw=encoded(b'\x01'),ReceivedNS=r['terminal_ns'],WriteStartedNS=r['terminal_ns']+1,WrittenNS=r['terminal_ns']+2,Injected=False,WriteFailed=False,Dropped=False))
            elif operation=='restore_before_write':r['late_written']['WriteStartedNS']=r['restore']['test_before']['resource_ns']+1;r['late_written']['WrittenNS']=r['late_written']['WriteStartedNS']+1
            else:r['held']['Injected']=True;r['held']['ReceivedNS']=0
            with self.subTest(operation=operation):self.assertEqual(f.run()[0]['result'],'fail')

    def test_every_round_uses_original_profile_and_independent_strict_resource_result(self):
        for operation in ('common_missing','common_fail','nineteen','missing_begin','rebase','late_completion','two_samples','duplicate_check'):
            f=self.fresh();test=f.rounds[0]['test']
            begin=next(r for _,_,r in f.records if r.get('kind')=='resources_begin')
            done=next(r for _,_,r in f.records if r.get('kind')=='resources')
            if operation=='common_missing':f.resource=None
            elif operation=='common_fail':f.resource['result']='fail'
            elif operation=='nineteen':f.resource['completed_checks'][test]=19
            elif operation=='missing_begin':f.records=[x for x in f.records if x[2] is not begin]
            elif operation=='rebase':begin['profile_id']='new-after-fault'
            elif operation=='late_completion':done['completed_ns']=begin['deadline_ns']+1
            elif operation=='two_samples':done['samples']=2
            else:next(r for _,_,r in f.records if r.get('kind')=='resources' and r is not done)['check_id']=done['check_id']
            with self.subTest(operation=operation):self.assertEqual(f.run()[0]['result'],'fail')

    def test_positive_protocol_shape_and_effective_mode_are_checked_from_bytes(self):
        for scenario,operation in [('preflight','decision_id'),('final','missing_decision'),
                ('final','unknown'),('final','best_effort'),('final','foreign_binding')]:
            f=self.fresh();r=next(r for r in f.rounds if r['scenario']==scenario)
            result=json.loads(base64.b64decode(r['held']['Raw']))
            request=json.loads(base64.b64decode(r['check']['Raw']))
            if operation=='decision_id':result['decision_id']='c'*32
            elif operation=='missing_decision':result.pop('decision_id')
            elif operation=='unknown':result['readiness']='unknown'
            elif operation=='best_effort':result['effective_admission']=request['effective_admission']='best_effort'
            else:result['binding']['generation']=request['binding']['generation']=2
            r['held']['Raw']=encoded(result);r['check']['Raw']=encoded(request)
            with self.subTest(scenario=scenario,operation=operation):self.assertEqual(f.run()[0]['result'],'fail')

    def test_cleanup_cannot_borrow_another_channel_or_round(self):
        for operation in ('channel','duplicate','origin','payload'):
            f=self.fresh()
            cleanups=[r for _,_,r in f.records if r.get('event')=='commit_cleanup']
            r=cleanups[0]
            if operation=='channel':r['channel_id']='ch_foreign'
            elif operation=='duplicate':cleanups[1].update(copy.deepcopy(r))
            elif operation=='origin':r['origin_ns']+=1
            else:r['target'][0]['Data']=encoded(b'leaked')
            with self.subTest(operation=operation):self.assertEqual(f.run()[0]['result'],'fail')

    def test_independent_source_receipt_detects_early_expiry_even_if_released_later(self):
        f=self.fresh();r=f.rounds[0]
        source=r['source_expired'];source['ReceivedNS']=r['query']['WriteStartedNS']+1000*MS-1
        source['WriteStartedNS']=source['ReceivedNS']+200*MS;source['WrittenNS']=source['WriteStartedNS']+1
        r['directory_expired_ns']=source['ReceivedNS']-1
        # The directory poll remains sound; this mutation isolates the earlier
        # source receipt against the real query send + source remaining bound.
        r['expiry_trajectory'][0]['ended_ns']=r['directory_expired_ns']
        self.assertEqual(f.run()[0]['result'],'fail')


if __name__=='__main__':unittest.main()
