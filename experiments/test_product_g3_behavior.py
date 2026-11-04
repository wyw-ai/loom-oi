"""Independent G3 behavior evidence: synthetic valid histories and counterexamples.

No product execution or developer coverage files are read by these tests.
"""
import copy
import unittest

from product.analyze import _g3_behavior_analysis, _g3_pattern_hash

MS = 1_000_000
PKG = 'github.com/wyw-ai/loom-oi/tests/integration'


def target(request='', received=0, response=0, *, at=10_000 * MS, closed=False, fin=False, markers=None):
    events = {'accepted': at - 5 * MS}
    if fin:
        events.update(request_fin=at - 4 * MS, effect_committed=at - 3 * MS,
                      writer_entered=at - 3 * MS, writer_returned=at - 2 * MS, response_fin=at - MS)
    if closed:
        events['closed'] = at
    return dict(ID=1, RequestID=request, ReceivedBytes=received, ResponseBytes=response,
                WireReceivedBytes=received + (len(request) + 4 if request else 0), RequestFIN=fin,
                ResponseFIN=fin, WriterEntered=fin, WriterReturned=fin, Closed=closed,
                Error='', SHA256=_g3_pattern_hash(received), Markers=markers or {}, Events=events)


def history(snapshots):
    return {'Exchanges': snapshots, 'Effects': {}, 'Workers': sum(not s['Closed'] for s in snapshots), 'Closed': False}


def binding(generation=2, control=1):
    return {'oics': {'lab-a': {'generation': generation}}, 'control_conn_ids': {'lab-a': f'access/lab-a/{control}'}}


class Evidence:
    def __init__(self, vector, scenario):
        self.case = dict(test=f'TestG3/sqlite/{scenario}', backend='sqlite', vector=vector, scenario=scenario)
        self.test, self.now = self.case['test'], 10_000 * MS
        self.events, self.checks = [], []
        self.resources = [dict(schema='g3-resource-v1', kind='g3_resources_baseline', recorded_ns=1)]

    def event(self, name, *, at=None, **fields):
        when = self.now if at is None else at
        self.now = max(self.now, when) + MS
        kind = 'G3-effect' if name in ('committed_before_loss', 'no_replay_window', 'explicit_request_complete', 'final_counts') else 'G3-transfer'
        if name in ('fin_delay','backpressure_held','data_rst','preparing_rst','response_lost','revoked','preparing_revoked',
                    'control_applied','blackhole_new_open','control_terminated','control_restored','quota_occupied',
                    'quota_rejected','cycle_first_rst','cycle_rst','cycle_revoked'):kind='G3-fault'
        value = dict(self.case, package=PKG, schema='g3-behavior-v1', stage='validation', kind=kind,
                     event=name, observed_ns=when, **fields)
        self.events.append(value)
        return value

    def ready(self, channel, number=1):
        return self.event('ready', channel_id=channel, provider_conn_id=f'access/lab-a/{100+number}', consumer_conn_id=f'consumer/{number}')

    def closed(self, channel, reason, at):
        return self.event('channel_closed', at=at, channel_id=channel, state='closed', reason=reason, terminal_observed_ns=at)

    def ticket(self, channel, at):
        self.event('ticket_rejected', at=at, channel_id=channel, status=410)

    def no_ready(self, channel, at):
        self.event('preparing_no_ready', at=at, channel_id=channel, ready_events=0)

    def phase(self, name, start, origin, cycle=None, identity=None):
        value = dict(test=self.test, stage='validation', phase=name, cycle=cycle, check_id=f'{name}/{cycle}',
                     profile_id='online', start_ns=start, deadline_ns=start+2000*MS, completed_ns=start+500*MS,
                     binding=identity or binding())
        self.checks.append(value)
        common = dict(value, schema='g3-resource-v1', origin_ns=start, origin_event=origin)
        self.resources += [dict(common, kind='g3_resources_begin'), dict(common, kind='g3_resources_complete')]
        return value

    def buffers(self, channel):
        def proxy(role, number):
            d = dict(ReadBytes=32768, ForwardedBytes=0, BufferedBytes=32768, MaxBufferedBytes=32768)
            return {'Workers': 3, 'Conns': [dict(ID=number, Role=role, ChannelID=channel, AccessID='lab-a' if role=='provider' else '',
                       Pending=False, ServerConnected=True, ClientClosed=False, ServerClosed=False,
                       ClientToServer=d, ServerToClient=dict(d))]}
        return dict(F=32768, ois_max_buffered=65536, oic_max_buffered=65536,
                    consumer_proxy=proxy('consumer', 1), provider_proxy=proxy('provider', 101))

    def run(self):
        records = [(PKG,self.test,e) for e in sorted(self.events,key=lambda e:e['observed_ns'])]
        resources = [(PKG,self.test,e) for e in self.resources]
        return _g3_behavior_analysis({'schema':'g3-behavior-v1','package':PKG,'cases':[self.case]}, records, resources,
                                    {'checks':self.checks,'required_tests':[self.test]}, {('vectors',PKG,self.test):['pass']}, 1)

    def get(self, name, cycle=None):
        return next(e for e in self.events if e['event']==name and (cycle is None or e.get('cycle')==cycle))


def control(scenario='control_blackhole'):
    b=Evidence('TV-G3-05',scenario)
    b.ready('old')
    applied=b.now+MS
    b.event('control_applied',at=applied,channel_id='old',generation=2,applied_ns=applied,
            license_sampled_ns=applied-100*MS,license_remaining_ms=2500,license_remaining_at_apply_ms=2400,
            direction='both' if scenario=='control_cut' else 'server_to_client',control_conn_id='access/lab-a/1',
            target_exchanges_before=1,oic_closed_before={'LicenseExpired':0})
    if scenario=='control_blackhole':
        b.event('blackhole_new_open',channel_id='blocked',status=201,code='',target_exchanges_before=1,target_exchanges_after=1,
                target=history([target('control',markers={'tick:':1})]))
    end=applied+3000*MS
    origin=end+10*MS
    b.event('open_refused',at=origin,status=503,code='AccessOffline',refused_ns=origin)
    b.event('control_terminated',channel_id='old',generation=2,applied_ns=applied,stream_end_ns=end,
            offline_refused_ns=origin,origin_ns=origin,reason='LicenseExpired')
    fault=b.phase('control_fault_cleanup',origin,'control_termination_and_offline_observed')
    b.event('expiry_marker_rejected',at=origin+30*MS,channel_id='old',generation=2,target_count=0,marker='after-expiry',write_error='closed',
            oic_stats=dict(access_id='lab-a',generation=2,epoch=1,license_remaining_ms=0,closed={'LicenseExpired':1}))
    if scenario=='control_blackhole':b.no_ready('blocked',origin+40*MS)
    restored=fault['completed_ns']+10*MS
    b.event('control_restored',at=restored,old_generation=2,new_generation=3,old_control_conn_id='access/lab-a/1',
            new_control_conn_id='access/lab-a/2',restored_ns=restored)
    online=b.phase('online_restored_cleanup',restored,'control_restored',identity=binding(3,2))
    b.event('control_failure_verified',at=online['completed_ns']+MS,channel_id='old',blocked_channel_id='blocked' if scenario.endswith('blackhole') else '',
            after_expiry_marker='after-expiry',target_count=0,target=history([target('control',at=end,closed=True,markers={'tick:':1})]))
    return b


def revoke(scenario='preparing_revoke'):
    b=Evidence('TV-G3-05',scenario)
    if scenario!='preparing_revoke':
        b.ready('channel')
        upload=scenario.startswith('upload')
        start=b.now
        held=target(scenario,0 if upload else 4,at=start)
        if not upload:held.update(WriterEntered=True);held['Events']['writer_entered']=start
        b.event('backpressure_held',at=start+250*MS,channel_id='channel',request_id=scenario,
                direction='client_to_server' if upload else 'server_to_client',writer_entered=True,writer_returned=False,
                hold_start_ns=start,held_until_ns=start+250*MS,written_bytes=32768,target=held,**b.buffers('channel'))
    trigger=b.now
    start=trigger+10*MS
    fields=dict(channel_id='channel',trigger_ns=trigger,returned_ns=trigger+MS,terminal_observed_ns=start)
    if scenario=='preparing_revoke':
        b.event('preparing_revoked',at=start,prepare_started_ns=trigger-MS,ready_sent=0,tickets=1,**fields)
    else:
        b.event('open_refused',at=start+MS,status=403 if upload else 401,code='Denied' if upload else 'Unauthenticated',refused_ns=start+MS)
        b.event('revoked',new_open_refused_ns=start+MS,request_id=scenario,**fields)
        start+=MS
    p=b.phase('revoked_cleanup',start,'revocation_observed')
    b.closed('channel','Revoked',start+10*MS)
    if scenario=='preparing_revoke':
        b.ticket('channel',start+20*MS);b.no_ready('channel',start+20*MS)
        b.event('preparing_revoke_verified',at=p['completed_ns']+MS,channel_id='channel',ready_sent=0,target_wire_bytes=0,
                target_exchange_after=0,target=[],consumer_code='Revoked',abort_received_ns=start+10*MS)
    else:
        b.event('revoke_verified',at=start+30*MS,channel_id='channel',trigger_ns=trigger,consumer_code='Revoked',
                received_bytes=0,read_end_ns=start+20*MS,target=target(scenario,at=start+20*MS,closed=True))
    return b


def quota(scenario='repeated_failures'):
    b=Evidence('TV-G3-06',scenario)
    mixed=scenario=='mixed_limit_reuse';all_targets=[]
    for cycle in range(2 if mixed else 10):
        opened,preparing=(8,8) if mixed else ((8,0),(0,8),(4,4))[cycle%3]
        ids=[f'ch-{cycle}-{i}' for i in range(opened+preparing)]
        prepares,conns,opens=[],[],[]
        for i,ch in enumerate(ids):
            number=cycle*20+i+1
            if i<opened:
                b.ready(ch,number)
                opens.append(dict(channel_id=ch,request_id=f'request-{ch}',target_exchange_id=len(all_targets)+i+1))
            else:
                prepares.append(dict(channel_id=ch,opened_ns=b.now,prepare_expires_in_ms=2000,provider_conn_id=f'access/lab-a/{100+number}'))
                conns.append(dict(ID=100+number,Role='provider',AccessID='lab-a',ChannelID=ch,ServerConnected=True,
                                  Pending=False,ClientClosed=False,ServerClosed=False))
        occupied=b.event('quota_occupied',cycle=cycle,open=opened,preparing=preparing,tickets=preparing,channel_ids=ids,prepares=prepares,
                opens=opens,target_before_id=len(all_targets),
                provider_proxy={'Conns':conns},ois_stats={'epoch':1,'tickets':preparing,'channels':dict(open=opened,preparing=preparing,closing=0,closed=cycle*8)})
        reason='ClosedByClient' if mixed else ('ChannelInterrupted','PrepareTimeout','Revoked')[cycle%3]
        start=b.now+10*MS
        if mixed:
            b.event('quota_rejected',cycle=cycle,status=429,code='ResourceExhausted',open=8,preparing=8)
            name,origin='quota_round_cleanup','quota_round_complete'
        else:
            name='failure_cycle_cleanup'
            origin=('first_reset_applied','all_prepare_timeouts_observed','revocation_observed')[cycle%3]
            if cycle%3==0:
                for i,ch in enumerate(ids):
                    b.event('cycle_first_rst' if i==0 else 'cycle_rst',at=start+i*MS,cycle=cycle,channel_id=ch,applied_ns=start+i*MS,
                            conn_ref=f'consumer/{cycle*20+i+1}')
            elif cycle%3==1:start=max(p['opened_ns'] for p in prepares)+2100*MS
            else:
                b.event('cycle_revoked',at=start,cycle=cycle,trigger_ns=occupied['observed_ns']+MS,returned_ns=occupied['observed_ns']+2*MS,
                        terminal_observed_ns=start,reason=reason)
        p=b.phase(name,start,origin,cycle)
        for i,ch in enumerate(ids):
            b.closed(ch,reason,start if not mixed and cycle%3 in (1,2) else start+20*MS)
            b.ticket(ch,start+30*MS)
            if i>=opened:b.no_ready(ch,start+30*MS)
            snap=target(f'request-{ch}' if i<opened else '',at=start+30*MS,closed=True)
            snap['ID']=len(all_targets)+1;all_targets.append(snap)
        extra={} if mixed else dict(reason=reason,open=opened,preparing=preparing)
        b.event('quota_round_verified' if mixed else 'cycle_complete',at=p['completed_ns']+MS,cycle=cycle,
                target=history(copy.deepcopy(all_targets)),**extra)
    return b


def normal(vector='TV-G3-01',scenario='consumer_fin_first'):
    b=Evidence(vector,scenario);b.ready('channel')
    fin=vector=='TV-G3-01';upload=scenario in ('target_fin_first','upload_target_paused')
    size=1048576 if fin else 67108864
    request,response=(size,4) if upload else (4,size)
    start=b.now;release=start+250*MS;finished=release+10*MS
    snap=target(scenario,request,response,at=finished,closed=True,fin=True)
    if fin:
        first,second=('response_fin','request_fin') if upload else ('request_fin','response_fin')
        snap['Events'][first]=start-MS;snap['Events'][second]=release+MS
        b.event('fin_delay',at=release,channel_id='channel',request_id=scenario,direction='client_to_server' if upload else 'server_to_client',
                first_fin_ns=start-MS,hold_start_ns=start,released_ns=release,delay_ms=250)
    else:
        held=target(scenario,0 if upload else 4,at=release)
        if not upload:held['WriterEntered']=True;held['Events']['writer_entered']=start
        b.event('backpressure_held',at=release,channel_id='channel',request_id=scenario,direction='client_to_server' if upload else 'server_to_client',
                hold_start_ns=start,released_ns=release,writer_entered=True,writer_returned=False,written_bytes=32768,target=held,**b.buffers('channel'))
    b.event('exchange_fin_observed',at=finished,channel_id='channel',request_id=scenario,finished_ns=finished,request_bytes=request,response_bytes=response)
    b.phase('exchange_complete' if fin else 'transfer_complete',finished,'exchange_fin_observed')
    b.closed('channel','Completed',finished+10*MS)
    b.event('exchange_complete',at=finished+20*MS,channel_id='channel',request_id=scenario,request_bytes=request,response_bytes=response,
            target=snap,effects={scenario:1})
    if not fin:b.event('buffer_bounds',**b.buffers('channel'))
    return b


def interrupted(scenario='consumer_open_rst'):
    b=Evidence('TV-G3-03',scenario)
    preparing='preparing' in scenario;consumer=scenario.startswith('consumer')
    if not preparing:b.ready('channel')
    applied=b.now+10*MS
    fields=dict(channel_id='channel',role=scenario,applied_ns=applied,conn_id=1 if consumer else 101,
                conn_ref='consumer/1' if consumer else 'access/lab-a/101')
    if preparing:fields.update(prepare_started_ns=applied-10*MS,ready_sent=0,tickets=1)
    else:fields.update(request_id=scenario,target_before=target(scenario,10,at=applied-MS,markers={'before-rst':1}))
    b.event('preparing_rst' if preparing else 'data_rst',at=applied,**fields)
    p=b.phase('interrupted_cleanup',applied,'first_reset_applied')
    b.closed('channel','ChannelInterrupted',applied+10*MS)
    if consumer or preparing:b.ticket('channel',applied+20*MS)
    if preparing:
        b.no_ready('channel',applied+30*MS)
        b.event('preparing_interruption_verified',at=p['completed_ns']+MS,channel_id='channel',ready_sent=0,
                target_wire_bytes=0,target_exchange_after=0,target=[])
    else:
        if consumer:b.event('surviving_provider_interrupted',channel_id='channel',before=0,after=1)
        b.event('interrupted_verified',at=p['completed_ns']+MS,channel_id='channel',request_id=scenario,local_end='error_not_eof',
                target=target(scenario,10,at=applied+30*MS,closed=True,markers={'before-rst':1}))
    return b


def loss(scenario='consumer_leg_loss'):
    b=Evidence('TV-G3-04',scenario);b.ready('old')
    committed=target('old-id',4,at=b.now,fin=True)
    committed.update(ResponseFIN=False,WriterEntered=False,WriterReturned=False)
    b.event('committed_before_loss',channel_id='old',request_id='old-id',target=committed,effects={'old-id':1})
    applied=b.now
    b.event('response_lost',channel_id='old',request_id='old-id',applied_ns=applied,role=scenario,response_bytes=0,
            conn_id=1 if scenario.startswith('consumer') else 101,conn_ref='consumer/1' if scenario.startswith('consumer') else 'access/lab-a/101')
    p=b.phase('fault_cleanup',applied,'first_reset_applied');b.closed('old','ChannelInterrupted',applied+10*MS)
    old=target('old-id',4,at=applied+20*MS,closed=True)
    b.event('response_loss_verified',at=applied+20*MS,channel_id='old',request_id='old-id',local_end='error_not_eof',target=old)
    start=p['completed_ns']+MS;end=start+5000*MS
    b.event('no_replay_window',at=end,old_request_id='old-id',old_generation=2,new_generation=3,
            old_control_conn_id='access/lab-a/1',new_control_conn_id='access/lab-a/2',start_ns=start,end_ns=end,effects={'old-id':1})
    b.ready('new',2);finished=b.now+10*MS
    b.event('exchange_fin_observed',at=finished,channel_id='new',request_id='new-id',finished_ns=finished,request_bytes=4,response_bytes=4)
    p=b.phase('new_request_cleanup',finished,'exchange_fin_observed',identity=binding(3,2));b.closed('new','Completed',finished+10*MS)
    new=target('new-id',4,4,at=finished+20*MS,closed=True,fin=True);new['ID']=2
    effects={'old-id':1,'new-id':1}
    b.event('exchange_complete',at=finished+30*MS,channel_id='new',request_id='new-id',request_bytes=4,response_bytes=4,target=new,effects=effects)
    b.event('explicit_request_complete',old_request_id='old-id',new_request_id='new-id',effects=effects)
    h=history([old,new]);h['Effects']=effects
    b.event('final_counts',at=p['completed_ns']+MS,old_request_id='old-id',new_request_id='new-id',effects=effects,target=h)
    return b


class G3BehaviorTests(unittest.TestCase):
    def good(self,b):
        summary,problems=b.run();self.assertEqual(problems,[]);self.assertEqual(summary['result'],'pass')

    def bad(self,b):
        summary,problems=b.run();self.assertTrue(problems);self.assertEqual(summary['result'],'fail')

    def test_all_control_revoke_and_quota_scenarios_have_valid_raw_histories(self):
        for b in [control('control_cut'),control(),revoke(),revoke('upload_backpressure_revoke'),revoke('download_backpressure_revoke'),quota(),quota('mixed_limit_reuse')]:
            with self.subTest(scenario=b.case['scenario']):self.good(b)

    def test_all_transport_and_response_loss_scenarios_have_valid_histories(self):
        scenarios=[normal(),normal(scenario='target_fin_first'),normal('TV-G3-02','upload_target_paused'),normal('TV-G3-02','download_consumer_paused'),
                   *[interrupted(s) for s in ('consumer_open_rst','provider_open_rst','consumer_preparing_rst','provider_preparing_rst')],loss(),loss('provider_leg_loss')]
        for b in scenarios:
            with self.subTest(scenario=b.case['scenario']):self.good(b)

    def test_malformed_or_unbound_observations_cannot_hide_behind_go_pass(self):
        for change in ('package','test','backend','schema','kind','unknown','future','duplicate','bool_cycle'):
            with self.subTest(change=change):
                b=normal();e=b.get('exchange_complete')
                if change in ('package','test','backend','schema','kind'):e[change]='wrong'
                elif change=='unknown':e['event']='ignored-success'
                elif change=='future':e['target']['Events']['closed']=e['observed_ns']+1
                elif change=='duplicate':b.events.append(copy.deepcopy(e))
                else:e['cycle']=False
                self.bad(b)

    def test_normal_exchange_requires_content_fins_effect_and_original_cleanup_start(self):
        for change in ('hash','bytes','effect','fin','first_fin','direction','hold','finished','cleanup','extra_phase'):
            with self.subTest(change=change):
                b=normal();complete=b.get('exchange_complete');fault=b.get('fin_delay')
                if change=='hash':complete['target']['SHA256']='0'*64
                elif change=='bytes':complete['target']['ReceivedBytes']-=1
                elif change=='effect':complete['effects'][b.case['scenario']]=2
                elif change=='fin':complete['target']['ResponseFIN']=False
                elif change=='first_fin':complete['target']['Events']['request_fin']+=MS
                elif change=='direction':fault['direction']='client_to_server'
                elif change=='hold':fault['hold_start_ns']+=MS
                elif change=='finished':b.get('exchange_fin_observed')['finished_ns']+=MS
                elif change=='cleanup':b.checks[0]['deadline_ns']+=MS
                else:b.phase('invented',b.now,'invented')
                self.bad(b)

    def test_backpressure_buffer_measurements_must_bind_to_the_actual_ready_legs(self):
        for change in ('wrong_channel','wrong_id','empty','overflow','writer','prefetched'):
            with self.subTest(change=change):
                b=normal('TV-G3-02','upload_target_paused');h=b.get('backpressure_held')
                if change=='wrong_channel':h['consumer_proxy']['Conns'][0]['ChannelID']='foreign'
                elif change=='wrong_id':h['provider_proxy']['Conns'][0]['ID']=999
                elif change=='empty':h['consumer_proxy']['Conns']=[]
                elif change=='overflow':h['provider_proxy']['Conns'][0]['ClientToServer']['MaxBufferedBytes']=32769
                elif change=='writer':h['written_bytes']=0
                else:h['target']['ReceivedBytes']=1
                self.bad(b)

    def test_rst_requires_correct_leg_no_ready_no_payload_and_rejected_ticket(self):
        for change in ('wrong_leg','wrong_conn_id','delay','ready','wire','ticket','foreign_verified'):
            with self.subTest(change=change):
                b=interrupted('consumer_preparing_rst')
                if change=='wrong_leg':b.get('preparing_rst')['conn_ref']='access/lab-a/1'
                elif change=='wrong_conn_id':b.get('preparing_rst')['conn_id']=999
                elif change=='delay':b.get('preparing_rst')['applied_ns']+=MS
                elif change=='ready':b.get('preparing_no_ready')['ready_events']=1
                elif change=='wire':b.get('preparing_interruption_verified')['target_wire_bytes']=1
                elif change=='ticket':b.get('ticket_rejected')['channel_id']='other'
                else:b.get('preparing_interruption_verified')['channel_id']='other'
                self.bad(b)

    def test_response_loss_requires_error_not_eof_five_seconds_and_one_effect_per_id(self):
        for change in ('eof','no_observation','window','generation','connection','replayed','missing_effect','foreign_request','same_request','history'):
            with self.subTest(change=change):
                b=loss()
                if change=='eof':b.get('response_loss_verified')['local_end']='EOF'
                elif change=='no_observation':b.events.remove(b.get('response_loss_verified'))
                elif change=='window':b.get('no_replay_window')['start_ns']+=MS
                elif change=='generation':b.get('no_replay_window')['new_generation']=2
                elif change=='connection':b.get('response_lost')['conn_ref']='consumer/999'
                elif change=='replayed':b.get('no_replay_window')['effects']['old-id']=2
                elif change=='missing_effect':b.get('committed_before_loss')['target']['Events'].pop('effect_committed')
                elif change=='foreign_request':b.get('response_lost')['request_id']='other'
                elif change=='same_request':b.get('explicit_request_complete')['new_request_id']='old-id'
                else:b.get('final_counts')['target']['Exchanges'].append(copy.deepcopy(b.get('final_counts')['target']['Exchanges'][0]))
                self.bad(b)

    def test_control_requires_actual_remaining_license_and_fixed_fault_direction(self):
        for changes in [dict(direction='client_to_server'),dict(license_remaining_ms=2000,license_remaining_at_apply_ms=1900),
                        dict(license_remaining_at_apply_ms=2500),dict(generation=True),dict(control_conn_id='consumer/1')]:
            with self.subTest(changes=changes):
                b=control();b.get('control_applied').update(changes);self.bad(b)

    def test_control_termination_window_and_cleanup_origin_are_independent(self):
        for change in ('too_early','too_late','delayed_origin','wrong_reason','wrong_refusal','missing_resource'):
            with self.subTest(change=change):
                b=control();e=b.get('control_terminated')
                if change=='too_early':e['stream_end_ns']=e['applied_ns']+1999*MS
                elif change=='too_late':e['stream_end_ns']=e['applied_ns']+4001*MS;e['observed_ns']=e['stream_end_ns']
                elif change=='delayed_origin':e['origin_ns']+=MS
                elif change=='wrong_reason':e['reason']='Completed'
                elif change=='wrong_refusal':b.get('open_refused')['status']=201
                else:b.resources.pop()
                self.bad(b)

    def test_control_generation_marker_counter_and_target_count_cannot_be_faked(self):
        for change in ('generation','connection','expired_counter','marker','new_prepare','blocked_ready'):
            with self.subTest(change=change):
                b=control()
                if change=='generation':b.get('control_restored')['new_generation']=2
                elif change=='connection':b.get('control_restored')['new_control_conn_id']='access/lab-a/1'
                elif change=='expired_counter':b.get('expiry_marker_rejected')['oic_stats']['closed']['LicenseExpired']=0
                elif change=='marker':b.get('control_failure_verified')['target']['Exchanges'][0]['Markers']['after-expiry']=1
                elif change=='new_prepare':b.get('control_applied')['target_exchanges_before']=2
                else:b.get('preparing_no_ready')['ready_events']=1
                self.bad(b)

    def test_revocation_preserves_independent_socket_deadline_and_zero_payload(self):
        for change in ('late_abort','wrong_code','wire','ready','ticket','origin'):
            with self.subTest(change=change):
                b=revoke();v=b.get('preparing_revoke_verified')
                if change=='late_abort':v['abort_received_ns']=b.get('preparing_revoked')['trigger_ns']+1001*MS;v['observed_ns']=v['abort_received_ns']
                elif change=='wrong_code':v['consumer_code']='Completed'
                elif change=='wire':v['target_wire_bytes']=1
                elif change=='ready':b.get('preparing_no_ready')['ready_events']=True
                elif change=='ticket':b.get('ticket_rejected')['channel_id']='different'
                else:b.get('preparing_revoked')['terminal_observed_ns']+=MS
                self.bad(b)

    def test_revocation_must_retain_real_backpressure_and_refused_admission(self):
        for change in ('hold','direction','buffer','returned','refused','deadline'):
            with self.subTest(change=change):
                b=revoke('upload_backpressure_revoke');held=b.get('backpressure_held')
                if change=='hold':held['hold_start_ns']=held['held_until_ns']-249*MS
                elif change=='direction':held['direction']='server_to_client'
                elif change=='buffer':held['oic_max_buffered']=65537
                elif change=='returned':held['writer_returned']=True
                elif change=='refused':b.get('open_refused')['status']=200
                else:b.get('revoke_verified')['read_end_ns']=b.get('revoked')['trigger_ns']+1001*MS
                self.bad(b)

    def test_quota_requires_every_round_and_actual_ois_stats(self):
        for change in ('missing_round','stats','duplicate_channel','missing_prepare','not_attached','malformed_leg','wrong_ticket','no_final'):
            with self.subTest(change=change):
                b=quota('mixed_limit_reuse');o=b.get('quota_occupied',0)
                if change=='missing_round':b.events.remove(b.get('quota_occupied',1))
                elif change=='stats':o['ois_stats']['channels']['open']=7
                elif change=='duplicate_channel':o['channel_ids'][1]=o['channel_ids'][0]
                elif change=='missing_prepare':o['prepares'].pop()
                elif change=='not_attached':o['provider_proxy']['Conns'][0]['Pending']=True
                elif change=='malformed_leg':o['provider_proxy']['Conns'][0]='not a snapshot'
                elif change=='wrong_ticket':b.get('ticket_rejected')['status']=200
                else:b.events.remove(b.get('quota_round_verified',1))
                self.bad(b)

    def test_ready_connections_cannot_be_reused_between_channels_or_rounds(self):
        b=quota('mixed_limit_reuse')
        for e in b.events:
            if e['event']=='ready':e.update(consumer_conn_id='consumer/1',provider_conn_id='access/lab-a/101')
        self.bad(b)
        for role in ('consumer_conn_id','provider_conn_id'):
            with self.subTest(role=role):
                b=quota('mixed_limit_reuse')
                readies=[e for e in b.events if e['event']=='ready']
                readies[8][role]=readies[0][role]
                self.bad(b)

    def test_preparing_provider_and_open_provider_share_one_ownership_map(self):
        b=quota('mixed_limit_reuse');o=b.get('quota_occupied',0)
        o['prepares'][0]['provider_conn_id']='access/lab-a/101'
        o['provider_proxy']['Conns'][0]['ID']=101
        self.bad(b)

    def test_preparing_header_leak_cannot_be_reclassified_as_a_legitimate_open(self):
        b=quota('mixed_limit_reuse')
        for e in b.events:
            if e['event']=='quota_round_verified':
                snap=next(x for x in e['target']['Exchanges'] if x['RequestID']=='')
                snap.update(RequestID='leaked-preparing',WireReceivedBytes=len(b'G3 leaked-preparing\n'))
        self.bad(b)
        b=quota();e=b.get('cycle_complete',1)
        snap=next(x for x in e['target']['Exchanges'] if x['RequestID']=='')
        snap.update(RequestID='request-ch-0-0',WireReceivedBytes=len(b'G3 request-ch-0-0\n'))
        self.bad(b) # Even a legitimate prior request ID cannot excuse a new unbound target.

    def test_quota_requires_actual_open_whitelist_and_unchanged_prior_history(self):
        for change in ('missing_opens','missing_boundary','duplicate_target','foreign_channel','wrong_request','missing_target','shift_boundary','change_closed'):
            with self.subTest(change=change):
                b=quota('mixed_limit_reuse');o=b.get('quota_occupied',0)
                if change=='missing_opens':o.pop('opens')
                elif change=='missing_boundary':o.pop('target_before_id')
                elif change=='duplicate_target':o['opens'][1]['target_exchange_id']=o['opens'][0]['target_exchange_id']
                elif change=='foreign_channel':o['opens'][0]['channel_id']='foreign'
                elif change=='wrong_request':o['opens'][0]['request_id']='forged'
                elif change=='missing_target':b.get('quota_round_verified',0)['target']['Exchanges'].pop(0)
                elif change=='shift_boundary':b.get('quota_occupied',1)['target_before_id']+=1
                else:b.get('quota_round_verified',1)['target']['Exchanges'][0]['WireReceivedBytes']+=1
                self.bad(b)

    def test_ten_cycles_require_all_resets_exact_origins_and_first_terminal_reasons(self):
        for change in ('missing_cycle','missing_rst','wrong_conn','delayed_origin','early_timeout','late_timeout','revoke','wire','active_worker'):
            with self.subTest(change=change):
                b=quota()
                if change=='missing_cycle':b.events.remove(b.get('cycle_complete',9))
                elif change=='missing_rst':b.events.remove(b.get('cycle_rst',0))
                elif change=='wrong_conn':b.get('cycle_rst',0)['conn_ref']='consumer/999'
                elif change=='delayed_origin':b.get('cycle_first_rst',0)['applied_ns']+=MS
                elif change=='early_timeout':b.get('quota_occupied',1)['prepares'][0]['prepare_expires_in_ms']=3000
                elif change=='late_timeout':b.get('quota_occupied',1)['prepares'][0]['opened_ns']-=1001*MS
                elif change=='revoke':b.get('cycle_revoked',2)['reason']='Completed'
                elif change=='wire':b.get('cycle_complete',1)['target']['Exchanges'][-1]['WireReceivedBytes']=1
                else:b.get('cycle_complete',9)['target']['Workers']=1
                self.bad(b)


if __name__=='__main__':unittest.main()
