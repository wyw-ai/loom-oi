"""G2 real CLOCK_REALTIME evidence, separate from ordinary-v1 registration.

This module never changes a clock. All behavioral judgments use original wire
and monotonic measurements; privileged audit verification stays independent.
A pass is this component's evidence only, and requires ordinary G2-04 alongside
it for the released-old-snapshot nonrevival claim.
"""
import copy
import json
from collections import defaultdict
from pathlib import Path
import re
import types


def sibling(name):
    path=Path(__file__).with_name(name+'.py')
    module=types.ModuleType('_g2_clock_'+name)
    module.__file__=str(path)
    exec(compile(path.read_bytes(),str(path),'exec'),module.__dict__)
    return module


core=sibling('g2_analyze')
guard=sibling('clock_guard')
require,number=core.require,core.number
MS,SECOND=core.NS_MS,core.NS_SEC
PACKAGE='github.com/wyw-ai/loom-oi/tests/integration'
ROOT='TestTVG2_04_RealClock'
SCENARIOS=('ready','not_ready','preflight','final','total_prepare')
EXPECTED={'schema':'g2-clock-analysis-v1','package':PACKAGE,'root':ROOT,
          'groups':{ROOT+'/'+s+'/'+d+'/'+b:dict(scenario=s,direction=d,backend=b,rounds=20,resource_checks=20,oics=['lab-a','lab-b'])
                    for s in SCENARIOS for d in ('forward','backward') for b in ('sqlite','postgres')}}


def expected_spec():
    return copy.deepcopy(EXPECTED)


def clock_sample(value):
    require(isinstance(value,dict),'missing independent clock sample')
    for key in ('resource_ns','realtime_ns','monotonic_ns','monotonic_before_ns','monotonic_after_ns'):
        number(value.get(key),1)
    require(value['monotonic_before_ns']<=value['monotonic_ns']<=value['monotonic_after_ns'],'independent clock bracket reversed')
    return value['resource_ns']


def offset(value,anchor):
    return value['realtime_ns']-anchor['realtime_ns']-value['monotonic_ns']+anchor['monotonic_ns']


def fixed_binding(row,value):
    bound=core.binding(value);session=row['session_identity']
    require(bound[0]=='lab-a' and bound[3]=='lab-a/http'
            and bound[1]==session['lab_a_epoch'] and bound[2]==session['lab_a_generation'],
            'clock decision came from another Exposure/session')
    return bound


class Audit:
    def __init__(self,events,source_sha):
        require(isinstance(events,list) and events,'missing independent guard audit')
        require(isinstance(source_sha,str) and re.fullmatch('[0-9a-f]{40}',source_sha),'trusted run source SHA absent')
        self.shifts,self.restores=guard.verify_audit(events)
        self.events=events;self.identity={k:events[0][k] for k in guard.IDENTITY_KEYS}
        require(self.identity['source_sha']==source_sha,'clock belongs to another fixed source')
        self.guard_pid=number(events[0]['guard_pid'],1)
        peers=[e for e in events if e['action']=='test_peer']
        require(len(peers)==1,'guard did not authenticate exactly one test process')
        self.test_pid=number(peers[0]['test_pid'],1)
        require(self.test_pid!=self.guard_pid and number(peers[0]['test_uid'],1)==events[0]['runner_uid'],'guard/test identity or nonroot scope invalid')
        self.used=set()
        self.witnesses=defaultdict(list)
        for e in events:
            if e['action']=='witness' and e.get('peer_pid')==self.test_pid:
                self.witnesses[e.get('active_round')].append(e)
        self.used_witnesses=set()
        self.resource_offset_interval=None

    def independent(self,sample):
        at=clock_sample(sample)
        lo,hi=sample['monotonic_before_ns']-at,sample['monotonic_after_ns']-at
        if self.resource_offset_interval is not None:
            lo=max(lo,self.resource_offset_interval[0]);hi=min(hi,self.resource_offset_interval[1])
        require(lo<=hi,'resource clock and CLOCK_MONOTONIC use inconsistent origins')
        self.resource_offset_interval=(lo,hi)
        return at

    def witness(self,value,ident,anchor,delta):
        require(isinstance(value,dict),'missing active-offset witness')
        reported=value['guard'];sample=value['test'];at=self.independent(sample)
        actual=offset(sample,anchor)
        require(value['offset_ns']==actual and abs(actual-delta)<=guard.TOLERANCE_NS,'independent active offset not preserved')
        require(reported.get('active_round')==ident and reported.get('peer_pid')==self.test_pid and reported.get('guard_pid')==self.guard_pid and reported.get('ok') is True,'witness from another round/process')
        matches=[e for e in self.witnesses[ident] if e==reported]
        require(len(matches)==1,'witness was not actually logged by independent guard')
        token=json.dumps(reported,sort_keys=True,separators=(',',':'))
        require(token not in self.used_witnesses,'witness reused across observations/rounds')
        self.used_witnesses.add(token)
        require(sample['monotonic_after_ns']<=reported['sample']['monotonic_before_ns'],'guard witness preceded independent test observation')
        return at

    def round(self,row):
        shift,restore=row['shift'],row['restore'];event=shift['guard'];ident=number(event.get('round_id'),1,400)
        require(ident not in self.used and ident in self.shifts and restore['guard']['round_id']==ident,'clock operation identity reused/missing')
        self.used.add(ident)
        delta=guard.STEP_NS if row['direction']=='forward' else -guard.STEP_NS
        require(row['delta_ns']==event['requested_delta_ns']==delta,'declared direction differs from applied shift')
        require(row['guard_pid']==self.guard_pid and shift['test_pid']==self.test_pid,'clock PID provenance differs')
        for original,reported in [(self.shifts[ident],event),(self.restores[ident],restore['guard'])]:
            require(all(reported.get(k)==original.get(k) for k in (*guard.IDENTITY_KEYS,'guard_pid','action','round_id','before','after','actual_delta_ns')),'raw syscall event differs from independent guard')
            require(reported.get('ok') is True and reported.get('peer_pid')==self.test_pid,'syscall response returned to another process')
        before,after=shift['test_before'],shift['test_after']
        start=self.independent(before);end=self.independent(after)
        actual=offset(after,before)
        require(start<=end and shift['actual_delta_ns']==actual and abs(actual-delta)<=guard.TOLERANCE_NS,'test process did not observe real requested step')
        require(before['monotonic_after_ns']<=event['before']['monotonic_before_ns'] and event['after']['monotonic_after_ns']<=after['monotonic_before_ns'],'syscall outside independent bracket')
        terminal=number(row['terminal_ns'],1)
        require(row['cleanup_origin_ns']==terminal,'cleanup origin reset after actual terminal')
        witness_at=self.witness(row['terminal_witness'],ident,before,delta)
        require(terminal<=witness_at,'terminal witness preceded actual terminal')
        restore_before=self.independent(restore['test_before']);restore_after=self.independent(restore['test_after'])
        require(max(end,terminal,witness_at)<=restore_before<=restore_after<=terminal+2*SECOND,'clock restore escaped original cleanup window')
        require(restore['test_before']['monotonic_after_ns']<=restore['guard']['before']['monotonic_before_ns'] and restore['guard']['after']['monotonic_after_ns']<=restore['test_after']['monotonic_before_ns'],'restore syscall outside independent bracket')
        restored=offset(restore['test_after'],before)
        require(restore['offset_ns']==restored and abs(restored)<=guard.TOLERANCE_NS,'independent restoration failed')
        if row['scenario'] in ('ready','not_ready'):
            live=self.witness(row['live_witness'],ident,before,delta)
            require(end<=row['view_ended_ns']<=live<=terminal,'sample live witness outside first-view/shifted interval')
        else:
            release_witness=self.witness(row['late_release_witness'],ident,before,delta)
            require(end<=release_witness<=row['release_requested_ns']<=restore_before,'late positive was not released while shifted')
        require(restore_after<=number(row['resource_completed_ns'],1)<=terminal+2*SECOND,'restore/resource completion escaped original 2s')
        return ident


def validate_reduction(row,history,query,q,response,written,start):
    r=row['query_reduction']
    deadline=query['ReceivedNS']+number(q.get('deadline_ms'),1,500)*MS
    require(r['query_id']==q['query_id'] and r['deadline_ns']==deadline,'reduction witness borrowed/reset query deadline')
    before=r['before'];polls=r['polls']
    require(isinstance(polls,list) and 1<=len(polls)<=51,'missing/unbounded reduction scrape trace')
    key=('oi_observation_query_rtt_seconds_count',(('result','success'),))
    def count(scrape):
        parsed=core.parse_exposition(scrape)
        value=parsed.get(key)
        require(value is not None and value==value.to_integral_value() and value<=2**64-1,'missing/noninteger/overflow query success count')
        return value
    previous=count(before)
    require(previous<2**64-1,'query success counter cannot advance exactly once')
    require(response['ReceivedNS']<=number(row['next_snapshot_guard_armed_ns'],1)<=before['started_ns']<=before['completed_ns']
            <=number(row['response_release_requested_ns'],1)<=written['WriteStartedNS']<=written['WrittenNS']<=deadline,
            'reduction baseline/guard did not precede actual held-response release')
    last=written['WrittenNS']
    for i,poll in enumerate(polls):
        current=count(poll)
        require(last<=poll['started_ns']<=poll['completed_ns']<=deadline,'reduction scrape escaped original query deadline/order')
        require(current==previous+(1 if i==len(polls)-1 else 0),'query success did not advance exactly once at final scrape')
        last=poll['completed_ns']
    require(last<=start<=deadline,'first directory view preceded OIS reduction or escaped query deadline')
    # The counter is global. The actual wire history must show this fixture has
    # only the one queried Exposure; no other written snapshot may provide +1.
    bound=core.binding(q['binding'])
    for key,event in history.events.items():
        if event['Type']=='observation_query' and not event['Injected']:
            require(core.binding(history.bodies[key]['binding'])==bound,'another query source can advance global success counter')
        if event['Type']=='observation_snapshot' and not event['Injected'] and event['WrittenNS']:
            if before['started_ns']<=event['WrittenNS']<=row['view_ended_ns']:
                require(event['ID']==response['ID'] and event['ConnID']==response['ConnID'] and key[0]=='lab-a',
                        'another response crossed the reduction/first-view window')


def validate_sample(row,history,health):
    query,q=history.ref(row['query'],typ='observation_query')
    response,sample=history.ref(row['response'],typ='observation_snapshot')
    written,_=history.ref(row['response_written'],typ='observation_snapshot')
    source,expired_source=history.ref(row['source_expired'],typ='observation_snapshot')
    require(response['ID']==written['ID'] and response['ConnID']==query['ConnID']==source['ConnID'] and q['query_id']==sample['query_id'],'sample query/response identity differs')
    require(core.binding(q['binding'])==core.binding(sample['binding'])==core.binding(expired_source['binding']),'source binding changed during clock experiment')
    fixed_binding(row,sample['binding'])
    shift=row['shift']
    require(query['ReceivedNS']<=shift['test_before']['resource_ns']<=shift['test_after']['resource_ns']<=query['WriteStartedNS']<=response['ReceivedNS'],'clock step did not cross actual query round trip')
    state=row['scenario'];seq=number(sample.get('sample_seq'),1)
    require(sample['readiness']==state and 200<=number(sample['remaining_ms'])<=1500,'source sample not live within original validity')
    view=row['view'];v=view['readiness_view'];start,end=number(row['view_started_ns'],1),number(row['view_ended_ns'],1)
    upper=max(0,sample['remaining_ms']-(start-query['ReceivedNS'])//MS)
    require(0<written['WrittenNS']<=start<=end and upper==row['remaining_upper_bound_ms'] and 0<v['remaining_ms']<=upper,'query RTT not conservatively deducted')
    require(view['readiness']==state and v.get('sample_seq')==seq and v['last_reported']['sample_seq']==seq,'live view lost/changed original sample')
    validate_reduction(row,history,query,q,response,written,start)
    lower=start+(v['remaining_ms']-1)*MS;upper=end+v['remaining_ms']*MS
    require((row['expiry_lower_ns'],row['expiry_upper_ns'])==(lower,upper),'invented expiry interval')
    trajectory=row['expiry_trajectory'];require(isinstance(trajectory,list) and 1<=len(trajectory)<=256,'missing/unbounded TTL trajectory')
    previous=end
    for i,poll in enumerate(trajectory):
        begin,finish=number(poll['started_ns'],1),number(poll['ended_ns'],1);current=poll['view'];rv=current['readiness_view']
        require(previous<=begin<=finish,'TTL polls overlap/regress');previous=finish
        bound=max(0,v['remaining_ms']-(begin-end)//MS)
        require(number(rv['remaining_ms'])<=bound and rv['last_reported']['sample_seq']==seq,'poll renewed/replaced sample')
        if i==len(trajectory)-1:
            require(current['readiness']=='unknown' and not rv.get('sample_seq') and rv['remaining_ms']==0 and finish>=lower,'clock prematurely expired sample or kept authority')
        else:
            require(current['readiness']==state and rv.get('sample_seq')==seq and begin<upper,'sample remained authoritative beyond original expiry')
    require(row['expired']==trajectory[-1]['view'] and trajectory[-1]['ended_ns']<=row['directory_expired_ns']<=row['terminal_ns'],'terminal directory snapshot not the observed expiry')
    require(expired_source['readiness']=='unknown' and expired_source['reason']=='expired' and expired_source['remaining_ms']==0 and not expired_source.get('sample_seq'),'OIC source did not expire original sample')
    require(row['directory_expired_ns']<=source['ReceivedNS']<=row['terminal_ns'],'source expiry outside actual post-directory observation window')
    require(row['terminal_ns']<=response['ReceivedNS']+sample['remaining_ms']*MS+SECOND,'source expiry exceeded original observation window')
    stopped=number(row['stopped_health_version'],1)
    attempts=[r for r in health['lab-a/http'].values() if r['Version']==stopped]
    require(any(r['StartedNS']<=row['terminal_ns'] and
                (not r['FinishedNS'] or r['FinishedNS']>=response['ReceivedNS']) for r in attempts),
            'stopped health version has no request during original sample experiment')
    # The next round's Set releases the old gate. Its response after this
    # terminal is valid full-history evidence, not a renewal during this round.
    require(all(not r['HeaderStartedNS'] or r['HeaderStartedNS']>row['terminal_ns'] for r in attempts),
            'new explicit health response invalidated old-sample expiry experiment')
    require(all(not response['ReceivedNS']<=r['HeaderStartedNS']<=row['terminal_ns']
                for r in health['lab-a/http'].values()),
            'intervening health response invalidated old-sample expiry experiment')
    for key,event in history.events.items():
        if (key[0]=='lab-a' and event['Type']=='observation_snapshot' and not event['Injected']
                and response['ReceivedNS']<=event['ReceivedNS']<=row['terminal_ns']):
            later=history.bodies[key]
            if core.binding(later['binding'])==core.binding(sample['binding']):
                require(not later.get('sample_seq') or later['sample_seq']==seq,'intervening explicit sample invalidated old-sample expiry experiment')
                if later.get('readiness')=='unknown' and later.get('reason')=='expired':
                    # Both the wire remainder and source authority use floor
                    # milliseconds; authority ends when the floor reaches 0.
                    require(event['ReceivedNS']>=query['WriteStartedNS']+(sample['remaining_ms']-1)*MS,
                            'source expired before earliest original sample deadline')
    return query['QueryID'],(core.binding(sample['binding']),seq)


def validate_check(row,history):
    scenario=row['scenario'];stage='preflight' if scenario=='preflight' else 'final'
    held,result,check=history.result(row['held'],stage)
    declared,body=history.ref(row['check'],typ='admission_check')
    late,_=history.ref(row['late_written'],typ='admission_result')
    require(declared['ID']==check['ID'] and late['ID']==held['ID'] and not result.get('code'),'deadline fixture lacks same real positive result/check')
    require(result.get('readiness')=='ready' and number(result.get('sample_seq'),1)>0 and number(result.get('remaining_ms'),1)>0,'deadline fixture lacks a live positive sample')
    require((not result.get('decision_id')) if stage=='preflight' else bool(re.fullmatch('[0-9a-f]{32}',result.get('decision_id',''))),'decision ID violates preflight/final protocol shape')
    deadline=number(body['deadline_ms'],1,500)
    fixed_binding(row,body['binding'])
    require(body.get('effective_admission')=='require_ready','clock CHECK did not exercise strict admission')
    upper=check['ReceivedNS']+(deadline+1)*MS
    require(row['deadline_upper_ns']==upper and upper<=row['release_requested_ns']<=late['WriteStartedNS']<=late['WrittenNS'],'positive did not arrive after conservative original deadline')
    shift=row['shift'];terminal=number(row['terminal_ns'],1);out=row['open_outcome']
    require(held['ReceivedNS']<=shift['test_before']['resource_ns']<=shift['test_after']['resource_ns']<=row['late_release_witness']['test']['resource_ns'],'clock shift not applied while actual result held')
    require(late['WrittenNS']<=row['restore']['test_before']['resource_ns'],'late positive delivered only after clock restoration')
    if scenario=='preflight':
        core.http_outcome(out,503,'ReadinessCheckFailed')
        require(row['http_reason']=='timeout' and terminal==out['CompletedNS'] and not result.get('channel_id'),'preflight first terminal reason/identity changed')
        require(not row['target'],'late preflight dialed target')
        core.slots(row['after'],checks=0)
        lower=out['StartedNS']+500*MS
    else:
        opened=core.http_outcome(out,201);channel=core.closed(row['channel'],'PrepareTimeout' if scenario=='total_prepare' else 'ReadinessCheckFailed')
        require(channel==opened['channel_id']==result['channel_id'] and row['consumer_abort']['Type']==4 and row['consumer_abort']['Reason']==row['channel']['close_reason'],'consumer/GET final terminal cause differs')
        require(not history.ready(channel),'late clock-shifted final emitted READY')
        core.targets(row['target'],zero=True)
        require(len(row['target'])==1,'clock final created repeated target dial')
        require(out['StartedNS']<=out['CompletedNS']<=row['attach_started_ns']<=check['ReceivedNS'],'final did not retain original Open sequence')
        if scenario=='total_prepare':
            require(0<deadline<400 and row['attach_started_ns']>=out['StartedNS']+1700*MS,'total-D fixture did not consume original prepare tail')
            lower=out['StartedNS']+2*SECOND
        else:
            require(row['channel']['readiness_decision']['reason']=='timeout','single CHECK reason changed')
            lower=row['attach_started_ns']+500*MS
    require(shift['test_after']['resource_ns']<lower<=terminal,'clock jump occurred after budget or caused premature terminal')
    # There is deliberately no HTTP-notification <=500ms promise here. Safety
    # is the actually written positive after the conservative deadline being
    # rejected, while the independent offset witness still proves the shift.
    return held['CheckID'],result.get('channel_id')


def resource_rounds(rows,rounds,base,resource_result,test):
    require(resource_result is not None and resource_result.get('result')=='pass' and resource_result.get('completed_checks',{}).get(test)==20,'common strict Linux B4 recomputation absent/failed')
    beginnings=[r for r in rows if r['kind']=='resources_begin'];done=[r for r in rows if r['kind']=='resources']
    require(len(beginnings)==len(done)==20,'missing/repeated per-round original resource window')
    by_start={r['started_ns']:r for r in beginnings};by_id={r['check_id']:r for r in done}
    require(len(by_start)==len(by_id)==20,'resource check identity/start reused')
    for row in rounds:
        start=row['cleanup_origin_ns'];begin=by_start.get(start)
        require(begin is not None and begin['profile_id']==base['profile_id'] and begin['deadline_ns']==start+2*SECOND,'resource deadline/profile reset or round unbound')
        final=by_id.get(begin['check_id'])
        require(final is not None and final['profile_id']==base['profile_id'] and start<=final['completed_ns']<=row['resource_completed_ns']<=begin['deadline_ns'],'resource completion not inside original round window')
        require(number(final['samples'],3)>=3,'insufficient strict resource samples')


def analyze_clock(spec,records,results,guard_audit,resource_result=None,source_sha=None):
    require(spec==EXPECTED,'clock preregistration differs from fixed 20-leaf inventory')
    problems=[];groups=defaultdict(list);resources=defaultdict(list);summaries={}
    try:audit=Audit(guard_audit,source_sha)
    except (ValueError,TypeError,KeyError,IndexError) as exc:
        return {'schema':EXPECTED['schema'],'result':'fail','problems':['guard: '+str(exc)]},['guard: '+str(exc)]
    for package,event_test,row in records:
        try:
            require(package==PACKAGE and event_test in EXPECTED['groups'] and row.get('test')==event_test,'unregistered/mixed clock raw provenance')
            require(row.get('backend')==EXPECTED['groups'][event_test]['backend'],'clock raw backend mismatch')
            if row.get('schema')=='b4-resource-v2':resources[event_test].append(row);continue
            require(row.get('schema')=='g2-behavior-v1' and row.get('kind')=='G2' and row.get('vector')=='G2-04-clock','clock behavior schema/vector mismatch')
            number(row.get('observed_ns'),1);groups[event_test].append(row)
        except (ValueError,TypeError,KeyError) as exc:problems.append(str(event_test)+': '+str(exc))
    for test,group in EXPECTED['groups'].items():
        errors=[]
        try:
            ancestors=['/'.join(test.split('/')[:i]) for i in range(1,5)]
            require(all(results.get((PACKAGE,name))==['pass'] for name in ancestors),'required clock root/parent/leaf not unique PASS')
            rows=groups[test];rounds=[r for r in rows if r['event']=='real_clock_round']
            require(not any(r['event']=='real_clock_sample_failure' for r in rows),'sample instrument preserved an immediate failure record')
            allowed={'real_clock_round','clock_preconditions','environment','directory_observed','local_metric_advanced','wire_history','health_history','commit_cleanup'}
            require(all(r['event'] in allowed for r in rows),'unknown/failure clock event')
            require(len(rounds)==20 and [r['round'] for r in rounds]==list(range(20)) and all(type(r['round']) is int for r in rounds),'clock rounds missing/reordered/repeated')
            require(all(r['scenario']==group['scenario'] and r['direction']==group['direction'] for r in rounds),'clock scenario/direction relabelled')
            pre=[r for r in rows if r['event']=='clock_preconditions'];require(len(pre)==1 and pre[0]['clock_changed'] is False and pre[0]['tokens']=='fixture_nonexpiring','clock credential/preparation gate missing')
            histories=[r for r in rows if r['event']=='wire_history'];require(len(histories)==2 and {r['access_id'] for r in histories}=={'lab-a','lab-b'},'clock wire histories incomplete')
            history=core.History(histories);health=core.health_index(rows)
            require(set(health)=={'lab-a/http'},'clock health history incomplete')
            health_rows=[r for r in rows if r['event']=='health_history']
            require(all(r['health']['PID']==audit.test_pid for r in health_rows),'health history from another test process')
            for r in health_rows:
                for request in r['health']['Records']:
                    require(all(request[field]<=r['observed_ns'] for field in
                                ('StartedNS','HeaderStartedNS','HeaderWrittenNS','CancelledNS','FinishedNS')),
                            'health history claims a future event')
                    require(not request['FinishedNS'] or request['FinishedNS']>=max(
                                request['HeaderStartedNS'],request['HeaderWrittenNS']),
                            'health request finished before its response')
            require(all(max(e['ReceivedNS'],e['WrittenNS'])<=r['observed_ns'] for r in histories for e in r['proxy']['Events']),'wire history claims a future event')
            baseline=[r for r in resources[test] if r['kind']=='resources_baseline'];require(len(baseline)==1,'clock profile resampled after fault')
            base=baseline[0];require(set(base['oics'])=={'lab-a','lab-b'},'clock baseline role omitted')
            pids=[base['ois']['pid'],base['oics']['lab-a']['pid'],base['oics']['lab-b']['pid']]
            require(len(set(pids+[audit.test_pid,audit.guard_pid]))==5 and all(type(p) is int and p>0 for p in pids),'product/guard/test process identity not independent')
            require(pre[0]['product_pids']==pids and all(r['product_pids']==pids for r in rounds),'clock restarted product processes')
            require(all(r['proxy']['PID']==audit.test_pid for r in histories),'wire history from another test process')
            session={'ois_epoch':base['ois']['stats']['epoch'],'lab_a_epoch':base['oics']['lab-a']['stats']['epoch'],'lab_a_generation':base['oics']['lab-a']['stats']['generation'],'lab_b_epoch':base['oics']['lab-b']['stats']['epoch'],'lab_b_generation':base['oics']['lab-b']['stats']['generation']}
            require(all(r['session_identity']==session for r in rounds),'clock reset epoch/generation instead of preserving deadlines')
            require(base['recorded_ns']<rounds[0]['shift']['test_before']['resource_ns'],'clock baseline not frozen before first fault')
            require(base['recorded_ns']<=pre[0]['observed_ns']<rounds[0]['shift']['test_before']['resource_ns'],'clock preparation gate not observed before fault')
            identities=set();samples=set();channels=set()
            for r in rounds:
                audit.round(r)
                require(r['resource_completed_ns']<=r['observed_ns'],'round summary preceded its real observations')
                if group['scenario'] in ('ready','not_ready'):
                    ident,sample=validate_sample(r,history,health);require(sample not in samples,'old sample repeated across clock rounds');samples.add(sample)
                else:
                    ident,channel=validate_check(r,history)
                    if channel:require(channel not in channels,'Channel reused across clock rounds');channels.add(channel)
                require(ident not in identities,'clock query/check trajectory reused');identities.add(ident)
            cleanups=[r for r in rows if r['event']=='commit_cleanup']
            require(len(cleanups)==(20 if group['scenario'] in ('final','total_prepare') else 0),'per-Channel cleanup records missing/repeated')
            for r in cleanups:core.cleanup(r)
            if cleanups:
                origins={r['origin_ns']:r for r in cleanups}
                require(len(origins)==20 and set(origins)=={r['terminal_ns'] for r in rounds},'logical cleanup borrowed another round/origin')
                for r in rounds:
                    cleanup=origins[r['terminal_ns']]
                    require(cleanup['channel_id']==r['channel']['channel_id'] and cleanup['target']==r['target'] and cleanup['finished_ns']<=r['resource_completed_ns'],'logical cleanup channel/payload/completion differs from final evidence')
            created={e['ChannelID'] for e in history.events.values() if e['Type']=='prepare_channel' and not e['Injected'] and e['ReceivedNS']>=base['recorded_ns']}
            require(created==channels,'late positive created hidden/replayed Channel')
            resource_rounds(resources[test],rounds,base,resource_result,test)
        except (ValueError,TypeError,KeyError,IndexError,UnicodeError) as exc:errors.append(str(exc))
        summaries[test]={'result':'fail' if errors else 'pass','rounds':len([r for r in groups[test] if r['event']=='real_clock_round']),'problems':errors}
        problems.extend(test+': '+e for e in errors)
    if audit.used!=set(range(1,401)):problems.append('clock raw did not consume exactly the independent 400 shift/restore pairs')
    return {'schema':EXPECTED['schema'],'result':'fail' if problems else 'pass','groups':summaries,
            'required_leaves':20,'required_rounds':400,'required_companion':'ordinary_g2_analysis_v1',
            'sample_scope':'both_end_original_ttl_expiry; post-release_old_snapshot_nonrevival_requires_ordinary_G2-04',
            'resource_contract':{'required_tests':{t:20 for t in EXPECTED['groups']},'expected_oics':{t:g['oics'] for t,g in EXPECTED['groups'].items()},'expected_backends':{t:g['backend'] for t,g in EXPECTED['groups'].items()}}},problems
