"""Strict G2 raw-evidence validation (no execution, summaries are not inputs).

The registration table is an implementation-owned versioned contract. A
manifest cannot delete a leaf or reduce its event/round counts to obtain pass.
Callers supply complete D16-reassembled OBS records with their Go package/test
provenance and the independently recomputed B4 result from the SAME raw rows.
Unimplemented semantic groups fail closed even when all Go tests passed.
"""
import base64
import copy
import hashlib
import json
import re
from decimal import Decimal, InvalidOperation
from datetime import datetime, timedelta
from collections import Counter, defaultdict

NS_MS = 1_000_000
NS_SEC = 1_000_000_000
BINDING_KEYS = ('access_id', 'epoch', 'generation', 'exposure_id', 'revision',
                'observation_revision', 'contract_digest', 'source_id', 'contract_version')
IMMUTABLE_WIRE = ('ID', 'ConnID', 'Role', 'Direction', 'Type', 'Stage', 'ChannelID',
                  'CheckID', 'QueryID', 'MessageType', 'Raw', 'ReceivedNS', 'Injected')
ABORT_CODES = {0x0107: 'ReadinessRequired', 0x0205: 'ReadinessCheckFailed'}
COMPETITION_PREPARATION = {
    'strategy': 'six-source-pre-dial-fresh-v1',
    'target_window': 'all-slow-applied-v1',
    'query_witness': 'pre-dial-final-critical-query-snapshot-v1',
    'preload_witness': 'probe-queue-register-snapshot-v1',
    'pre_dial_query_max_age_ms': 80,
}
COMPETITION_SOURCES = ('lab-a/http', 'lab-a/alt') + tuple(
    'lab-a/competition-%02d' % i for i in range(2, 16))


def require(condition, message):
    if not condition:
        raise ValueError(message)


def number(value, minimum=0, maximum=(1 << 63)-1):
    require(type(value) is int and minimum <= value <= maximum, 'invalid integer/timestamp')
    return value


def data(value, empty=True):
    if value is None and empty:
        return b''
    require(isinstance(value, str), 'missing base64 bytes')
    try:
        raw = base64.b64decode(value, validate=True)
    except (ValueError, TypeError) as exc:
        raise ValueError('invalid base64 bytes') from exc
    require(empty or bool(raw), 'empty wire message')
    return raw


def json_object(raw):
    def pairs(items):
        out = {}
        for k, v in items:
            require(k not in out, 'duplicate wire JSON field')
            out[k] = v
        return out
    obj = json.loads(raw, object_pairs_hook=pairs,
                     parse_constant=lambda _: (_ for _ in ()).throw(ValueError('nonfinite JSON')))
    require(isinstance(obj, dict), 'wire JSON is not an object')
    return obj


def binding(obj):
    require(isinstance(obj, dict), 'missing complete binding')
    b = tuple(obj.get(k) for k in BINDING_KEYS)
    require(isinstance(b[0], str) and isinstance(b[3], str) and b[3].startswith(b[0]+'/'), 'binding namespace mismatch')
    number(b[1], 1)
    number(b[2], 1)
    require(isinstance(b[4], str) and b[4].startswith('rv_'), 'missing target revision')
    require(isinstance(b[5], str) and re.fullmatch(r'ob_[1-9][0-9]*', b[5]), 'invalid observation revision')
    require(isinstance(b[6], str) and re.fullmatch(r'[0-9a-f]{64}', b[6]), 'invalid contract digest')
    require((b[7], b[8]) == ('local', 1), 'unsupported decision source/contract')
    return b


def wire_body(e):
    raw = data(e.get('Raw'), False)
    require(len(raw) <= 256 << 10, 'wire message exceeds instrument capacity')
    if e.get('MessageType') == 1:
        obj = json_object(raw)
        require(obj.get('type') == e.get('Type'), 'wire type metadata differs from bytes')
        for key, field in [('channel_id', 'ChannelID'), ('check_id', 'CheckID'), ('query_id', 'QueryID')]:
            require(obj.get(key, '') == e.get(field), 'wire ID metadata differs from bytes')
        return obj
    require(e.get('MessageType') == 2, 'invalid WebSocket message type')
    typ = {1:'READY', 2:'DATA', 3:'FIN', 4:'ABORT'}.get(raw[0])
    require(typ == e.get('Type'), 'binary frame metadata differs from bytes')
    if typ in ('READY', 'FIN'):
        require(len(raw) == 1, 'control frame has payload')
    elif typ == 'DATA':
        require(len(raw) > 1, 'empty DATA frame')
    elif typ == 'ABORT':
        require(3 <= len(raw) <= 131, 'invalid ABORT length')
        raw[3:].decode('utf-8')
    return {'type':typ, 'value':int.from_bytes(raw[1:3], 'big') if typ == 'ABORT' else None}


class History:
    """Index immutable complete histories; small event copies are references."""
    def __init__(self, records):
        self.events, self.bodies, self.connections = {}, {}, {}
        self.snapshots = {}
        self.checks, self.queries = {}, {}
        accesses = set()
        for row in records:
            access, snap = row.get('access_id'), row.get('proxy')
            require(isinstance(access, str) and access not in accesses, 'duplicate/missing wire history access')
            accesses.add(access)
            require(isinstance(snap, dict) and snap.get('Overflow') is False and snap.get('Failure') == '', 'incomplete/overflowed proxy history')
            self.snapshots[access] = snap
            number(snap.get('PID'), 1)
            number(snap.get('Workers'))
            number(snap.get('TransportConnections'))
            conns, events = snap.get('Connections'), snap.get('Events')
            require(isinstance(conns, list) and len(conns) <= 4096 and isinstance(events, list) and len(events) <= 32768, 'missing/unbounded proxy history')
            for c in conns:
                cid = number(c.get('ID'), 1)
                key = access, cid
                require(key not in self.connections and c.get('Role') in ('control','provider'), 'connection ID reused/role absent')
                require(type(c.get('Closed')) is bool, 'missing closed state')
                number(c.get('AcceptedNS'), 1)
                number(c.get('Workers'))
                if c['Closed']:
                    require(number(c.get('ClosedNS'), 1) >= c['AcceptedNS'] and c['Workers'] == 0, 'closed connection retained workers/time reversed')
                if c['Role'] == 'provider':
                    require(isinstance(c.get('ChannelID'), str) and c['ChannelID'], 'provider has no channel binding')
                self.connections[key] = c
            previous, seqs, received = 0, {}, {}
            total_bytes = 0
            for e in events:
                eid, cid = number(e.get('ID'), 1), number(e.get('ConnID'), 1)
                require(eid > previous, 'wire events duplicated/out of order')
                previous = eid
                c = self.connections.get((access, cid))
                require(c is not None and c['Role'] == e.get('Role'), 'wire refers to unbound connection/role')
                require(e.get('Direction') in ('oic_to_ois','ois_to_oic'), 'invalid wire direction')
                for flag in ('Injected','WriteFailed','Dropped'):
                    require(type(e.get(flag)) is bool, 'missing wire provenance flag')
                start, done, at = (number(e.get(k)) for k in ('WriteStartedNS','WrittenNS','ReceivedNS'))
                lane = cid, e['Direction']
                if e['Injected']:
                    require(at == 0, 'injected event claims real receipt')
                else:
                    require(at >= c['AcceptedNS'] and at >= received.get(lane, 0), 'wire receive time reversed')
                    received[lane] = at
                require(not start or start >= at, 'write preceded receipt')
                require(not done or (start > 0 and done >= start and not e['WriteFailed']), 'invalid successful write boundary')
                require(not e['Dropped'] or (start == 0 and done == 0), 'dropped message was written')
                if c['Role'] == 'provider':
                    require(e.get('ChannelID') == c['ChannelID'], 'provider connection rebound to another channel')
                body = wire_body(e)
                if e['MessageType'] == 1:
                    seq = number(body.get('seq'), 1, (1 << 64)-1)
                    if not e['Injected']:
                        require(seq > seqs.get(lane, 0), 'real ordered control sequence regressed')
                        seqs[lane] = seq
                key = access, eid
                self.events[key], self.bodies[key] = e, body
                total_bytes += len(data(e['Raw'], False))
                for typ, field, table in [('admission_check','CheckID',self.checks), ('observation_query','QueryID',self.queries)]:
                    if e['Type'] == typ and not e['Injected']:
                        ident = e.get(field)
                        require(isinstance(ident, str) and re.fullmatch('[0-9a-f]{32}', ident), 'invalid challenge ID')
                        ikey = access, cid, ident
                        require(ikey not in table, 'challenge identity reused')
                        table[ikey] = key
                if e['Type'] == 'admission_check':
                    require(e['Stage'] == body.get('stage'), 'check stage metadata mismatch')
                if e['Type'] == 'admission_result':
                    query = self.checks.get((access,cid,e['CheckID']))
                    require(query is not None and e['Stage'] == self.bodies[query]['stage'], 'result has no matching real check/stage')
                if e['Type']=='observation_snapshot' and not e['Injected']:
                    query=self.queries.get((access,cid,e['QueryID']))
                    require(query is not None and self.bodies[query]['binding']==body.get('binding'),'snapshot has no matching real query/binding')
            require(total_bytes <= 32 << 20, 'wire history exceeds byte bound')

    def ref(self, copy_event, access='lab-a', typ=None, genuine=True):
        require(isinstance(copy_event, dict), 'missing wire reference')
        key = access, copy_event.get('ID')
        actual = self.events.get(key)
        require(actual is not None and all(actual.get(k) == copy_event.get(k) for k in IMMUTABLE_WIRE), 'wire reference absent or immutable fields forged')
        require(typ is None or actual['Type'] == typ, 'unexpected referenced wire type')
        require(not genuine or not actual['Injected'], 'injection substituted for real product output')
        return actual, self.bodies[key]

    def result(self, copy_event, stage='final'):
        e, body = self.ref(copy_event, typ='admission_result')
        qkey = self.checks.get(('lab-a',e['ConnID'],e['CheckID']))
        require(qkey is not None, 'final lacks a real check')
        q, request = self.events[qkey], self.bodies[qkey]
        number(request.get('deadline_ms'),1,500)
        require(request.get('stage') == stage and e['Direction'] == 'oic_to_ois' and q['Direction'] == 'ois_to_oic', 'check stage/direction mismatch')
        require(binding(request.get('binding')) == binding(body.get('binding')), 'response binding differs from challenge')
        require(request.get('channel_id', '') == body.get('channel_id', '') and request.get('effective_admission') == body.get('effective_admission'), 'response lost Channel or effective admission')
        require(q['WriteStartedNS'] > 0 and q['WriteStartedNS'] <= e['ReceivedNS'], 'response preceded actual check write')
        return e, body, q

    def ready(self, channel):
        return [e for e in self.events.values() if e['Type'] == 'READY' and e['ChannelID'] == channel and not e['Injected']]


def closed(row, code=None):
    require(isinstance(row, dict) and row.get('state') == 'closed' and isinstance(row.get('channel_id'), str) and row['channel_id'], 'Channel was not closed')
    require(code is None or row.get('close_reason') == code, 'wrong first terminal cause')
    return row['channel_id']


def targets(rows, zero=False, minimum=1):
    require(isinstance(rows, list) and len(rows) >= minimum, 'missing real target records')
    for row in rows:
        require(row.get('Done') is True, 'target socket is not closed')
        payload = data(row.get('Data'))
        require(not zero or not payload, 'zero-tolerance target received bytes')


def cleanup(row):
    origin, done, target_done = (number(row.get(k), 1) for k in ('origin_ns','finished_ns','target_closed_observed_ns'))
    require(origin <= target_done <= origin+NS_SEC and target_done <= done <= origin+2*NS_SEC, 'cleanup exceeds original T_close/2s')
    for role in ('ois','oic'):
        stats = row.get(role)
        require(isinstance(stats, dict), 'missing cleanup process stats')
        channels = stats.get('channels')
        if role == 'ois':
            require(isinstance(channels, dict) and all(channels.get(k) == 0 for k in ('preparing','open','closing')) and stats.get('tickets') == 0, 'OIS retains Channel/ticket')
        else:
            require(channels == 0, 'OIC retains Channel')
        observation = stats.get('observation')
        require(isinstance(observation, dict) and all(observation.get(k) == 0 for k in ('pending_checks','pending_queries')), 'check/query slot survived')
        if role == 'ois':
            require(all(observation.get(k) == 0 for k in ('pending_opens','http_waiters')), 'HTTP pending slot survived')
    targets(row.get('target'), minimum=0)


def validate_ttl(rows, history):
    for row in rows:
        e = row['event']
        if e == 'query_ttl_bound':
            q, query = history.ref(row['query'], typ='observation_query')
            response, snapshot = history.ref(row['response'], typ='observation_snapshot')
            require(q['ConnID'] == response['ConnID'] and query.get('query_id') == snapshot.get('query_id') and binding(query['binding']) == binding(snapshot['binding']), 'TTL response challenge binding mismatch')
            view = row['view']['readiness_view']
            start, end = number(row['view_request_started_ns'],1), number(row['view_request_ended_ns'],1)
            require(q['ReceivedNS'] <= response['ReceivedNS'] <= start <= end, 'TTL measurement order invalid')
            written,_=history.ref(row['response_written'],typ='observation_snapshot')
            guard,_=history.ref(row['next_response_held'],typ='observation_snapshot')
            require(written['ID']==response['ID'] and 0<written['WrittenNS']<=start, 'TTL response not actually forwarded before view')
            require(guard['QueryID']!=response['QueryID'] and guard['ReceivedNS']<=start and (guard['WriteStartedNS']==0 or guard['WriteStartedNS']>=end), 'later response bypassed cache observation gate')
            delays={'query_delay':(100,0),'response_delay':(0,200),'asymmetric':(75,225),'cache_wait':(0,0),'duplicate_reply':(0,0)}
            query_delay,response_delay=delays[row['scenario']]
            require((row['query_delay_ms'],row['response_delay_ms'])==(query_delay,response_delay), 'declared impairment differs from fixed input')
            require(q['WriteStartedNS']-q['ReceivedNS']>=query_delay*NS_MS and written['WriteStartedNS']-response['ReceivedNS']>=response_delay*NS_MS, 'actual link impairment shorter than registered input')
            require(written['WrittenNS']-q['ReceivedNS']<=500*NS_MS,'query exceeded unchanged challenge deadline')
            if row['scenario']=='cache_wait':
                require(start-guard['ReceivedNS']>=300*NS_MS,'cache wait input not actually applied')
            upper = max(0,number(snapshot['remaining_ms'])-(start-q['ReceivedNS'])//NS_MS)
            require(row.get('remaining_upper_bound_ms') == upper and number(view['remaining_ms']) <= upper, 'complete RTT/cache deduction omitted')
            require(view['last_reported']['sample_seq'] == snapshot['sample_seq'], 'TTL view is another sample')
            if row.get('scenario') == 'duplicate_reply':
                artifact,dup=injected(history,row['injected_raw'],guard['ConnID'],after=guard['ReceivedNS'])
                _,guard_body=history.ref(row['next_response_held'],typ='observation_snapshot')
                expected=copy.deepcopy(snapshot);expected.update(seq=guard_body['seq'],query_id=guard_body['query_id'],remaining_ms=min(snapshot['remaining_ms'],guard_body['remaining_ms']))
                require(dup==expected and guard['Dropped'],'duplicate input changed identity/content or was not actually applied')
                third,_=history.ref(row['third_response_held'],typ='observation_snapshot')
                require(third['ReceivedNS']>=artifact['WrittenNS'] and third['ReceivedNS']<=row['duplicate_view_started_ns'] and (third['WriteStartedNS']==0 or third['WriteStartedNS']>=row['duplicate_view_started_ns']),'duplicate observation lacks a real subsequent response barrier')
                duplicate = row['duplicate_view']['readiness_view']
                bound = max(0,view['remaining_ms']-(number(row['duplicate_view_started_ns'],1)-end)//NS_MS)
                require(row['duplicate_remaining_upper_bound_ms'] == bound and duplicate['remaining_ms'] <= bound and duplicate['last_reported']['sample_seq'] == snapshot['sample_seq'], 'duplicate reply renewed sample')
        elif e == 'late_query_discarded':
            held, _ = history.ref(row['late_response'], typ='observation_snapshot')
            written, _ = history.ref(row['late_response_written'], typ='observation_snapshot')
            require(written['WriteStartedNS']-held['ReceivedNS'] >= 600*NS_MS, 'late query input was not applied')
            before, after = row['before']['readiness_view'], row['after']['readiness_view']
            upper=max(0,before['remaining_ms']-(row['after_request_started_ns']-row['before_request_ended_ns'])//NS_MS)
            require(after['last_reported']['sample_seq'] == before['last_reported']['sample_seq'] and row['remaining_upper_bound_ms'] == upper and after['remaining_ms'] <= upper, 'late query revived cache')
        elif e == 'observed_at_skew':
            require(row.get('host_clock_changed') is False and row.get('scope') == 'source_timestamp_metadata_only', 'metadata test falsely claims host clock step')
            held, original = history.ref(row['held'], typ='observation_snapshot')
            artifact,edited=injected(history,row['injected_raw'],held['ConnID'],after=held['ReceivedNS'])
            require(edited.get('observed_at') == row['shifted_observed_at'] and original.get('observed_at') == row['original_observed_at'] and edited['observed_at'] != original['observed_at'], 'clock metadata was not changed')
            expected=copy.deepcopy(original);expected['observed_at']=row['shifted_observed_at']
            delta=datetime.fromisoformat(edited['observed_at'])-datetime.fromisoformat(original['observed_at'])
            require(edited==expected and held['Dropped'] and delta==timedelta(days=-365 if row['round']%2==0 else 365),'metadata test changed non-diagnostic input or wrong clock direction')
            guard,_=history.ref(row['later_response_held'],typ='observation_snapshot')
            require(guard['ReceivedNS']>=artifact['WrittenNS'] and guard['Dropped'] and guard['WriteStartedNS']==0,'later response bypassed expiry observation')
            require(row['view']['readiness_view']['last_reported']['observed_at']==edited['observed_at'] and row['view']['readiness_view']['remaining_ms']<=original['remaining_ms'],'edited diagnostic timestamp renewed sample')
            require(0<=row['expired_ns']-row['view_started_ns']<=2*NS_SEC,'metadata expiry did not occur within fixed test window')
            expired = row['expired']
            require(expired['readiness'] == 'unknown' and expired['readiness_view']['remaining_ms'] == 0 and expired['readiness_view']['last_reported']['observed_at'] == edited['observed_at'], 'display timestamp preserved authority or lost history')


def metric_at(rows, at, name):
    matches=[r for r in rows if r['event']=='local_metric_advanced' and r.get('sample_finished_ns')==at and r.get('metric')==name]
    require(len(matches)==1,'local reduction lacks its independent metric observation')
    row=matches[0]
    require(number(row['sample_started_ns'],1)<=number(at,1) and number(row['value'],1)>number(row['before']), 'metric did not actually advance')


def validate_commit(rows, history):
    """G2-05/06 core safety and causal evidence, independent of Go assertions."""
    for row in rows:
        event = row['event']
        if event not in ('final_recheck_round','commit_snapshot_round','frozen_ready_expiry_round'):
            continue
        final, result, check = history.result(row['final'])
        channel = closed(row['channel'])
        require(result.get('channel_id') == channel, 'final refers to another Channel')
        if event == 'commit_snapshot_round':
            require(not result.get('code') and result.get('readiness') == 'ready' and result.get('decision_id'), 'not a frozen positive Ready')
            require(row['channel']['close_reason'] == 'Completed' and row['channel']['readiness_decision']['readiness'] == 'ready', 'fixed decision changed')
            check_bounds, commit_bounds = row['t_check_bounds_ns'], row['ois_commit_bounds_ns']
            ready = history.ready(channel)
            require(len(ready) == 1 and ready[0]['WrittenNS'] > 0, 'READY missing or repeated')
            require(check_bounds == [check['WriteStartedNS'],final['ReceivedNS']] and commit_bounds == [final['WriteStartedNS'],ready[0]['ReceivedNS']], 'invented distributed linearization timestamp')
            require(final['ReceivedNS'] < row['not_ready_reduction_observed_ns'] <= row['release_ns'] <= final['WriteStartedNS'] <= ready[0]['ReceivedNS'], 'Commit-Snapshot causal order invalid')
            metric_at(rows,row['not_ready_reduction_observed_ns'],'oi_observation_samples_total{result="not_ready"}')
            require(final['WriteStartedNS'] < check['ReceivedNS']+min(500,number(result['remaining_ms'],1))*NS_MS, 'frozen result exceeded conservative lifetime/check budget')
            targets(row['target'])
            require(any(data(x.get('Data')) for x in row['target']), 'Completed without actual target request')
        else:
            reason = 'ReadinessRequired' if event == 'frozen_ready_expiry_round' or row.get('scenario') == 'ready_expired_before_second_leg' else 'TargetNotReady'
            closed(row['channel'], reason)
            abort = row.get('consumer_abort', {})
            require(abort.get('Type') == 4 and abort.get('Reason') == reason, 'consumer did not receive exact failure')
            require(not history.ready(channel), 'invalid final emitted READY')
            targets(row['target'], zero=True)
            if event == 'final_recheck_round':
                require(result.get('code') == reason, 'local final did not reject')
                require(number(row['local_reduction_observed_ns'],1) <= final['ReceivedNS'], 'negative reduction occurred after final')
                metric_at(rows,row['local_reduction_observed_ns'],'oi_observation_sample_expired_total' if reason=='ReadinessRequired' else 'oi_observation_samples_total{result="not_ready"}')
                if row['scenario']=='after_both_legs_not_ready':
                    held,_=history.ref(row['held_check'],typ='admission_check')
                    require(held['ID']==check['ID'] and check['ReceivedNS']<=row['local_reduction_observed_ns']<=check['WriteStartedNS'],'both-leg final was not held across negative reduction')
                else:
                    require(row['local_reduction_observed_ns']<=check['ReceivedNS'],'negative reduction did not precede second-leg final check')
            else:
                require(not result.get('code') and result.get('readiness') == 'ready', 'expiry fixture lacked positive frozen decision')
                require(final['ReceivedNS'] < row['sample_expiry_observed_ns'] <= final['WriteStartedNS'], 'expiry did not precede released final')
                metric_at(rows,row['sample_expiry_observed_ns'],'oi_observation_sample_expired_total')
                require(row['channel']['readiness_decision']['reason'] == 'expired', 'expiry lost precise first cause')


def native_exchange(history, channel, request, response):
    require(isinstance(request,str) and isinstance(response,str), 'missing native bytes')
    ready=history.ready(channel)
    require(len(ready)==1 and ready[0]['WrittenNS'] > 0, 'native stream lacks unique READY')
    for direction, expected in [('ois_to_oic',request),('oic_to_ois',response)]:
        frames=[e for e in history.events.values() if e['Role']=='provider' and e['ChannelID']==channel and e['Direction']==direction and not e['Injected']]
        payload=b''.join(data(e['Raw'],False)[1:] for e in frames if e['Type']=='DATA')
        require(payload==expected.encode('utf-8'), 'native wire bytes differ from claimed outcome')
        require(sum(e['Type']=='FIN' for e in frames)==1, 'native exchange lacks exact FIN')


def validate_matrix(rows, history):
    matrix=[r for r in rows if r['event']=='admission_matrix']
    business=[r for r in rows if r['event']=='matrix_business_result']
    keys,channels=set(),set()
    for r in matrix:
        view=r['directory']
        state,exposure,mode=view['readiness'],view['exposure_id'],r['request_admission']
        require(state in ('ready','not_ready','unknown') and exposure in ('lab-a/http','lab-a/alt') and mode in ('best_effort','require_ready'), 'matrix input outside frozen Cartesian product')
        key=state,exposure,mode
        require(key not in keys, 'duplicate matrix input')
        keys.add(key)
        local='require_ready' if exposure.endswith('/alt') else 'best_effort'
        effective='require_ready' if 'require_ready' in (local,mode) else 'best_effort'
        allowed=state=='ready' or (state=='unknown' and effective=='best_effort')
        require(view['admission_mode']==local and r['effective_admission']==effective and r.get('expected_allowed') is allowed, 'admission mode weakened')
        opened=r['open']
        if not allowed:
            code='TargetNotReady' if state=='not_ready' else 'ReadinessRequired'
            require(not opened.get('channel_id') and isinstance(r.get('error'),str) and r['error'].startswith(code), 'negative matrix created Channel or wrong error')
            continue
        channel=opened.get('channel_id')
        require(isinstance(channel,str) and channel and channel not in channels and r.get('error')=='<nil>', 'allowed matrix has no unique Open')
        channels.add(channel)
        require(opened.get('admission_mode')==effective and opened.get('readiness_stage')=='preflight', 'Open did not retain preflight/effective admission')
        matches=[b for b in business if b['channel'].get('channel_id')==channel]
        require(len(matches)==1,'business result not bound uniquely to allowed Open')
        b=matches[0]
        closed(b['channel'],'Completed')
        require(b['channel']['readiness_decision']['effective_admission']==effective,'final weakened mode')
        require(b['channel']['readiness_decision']['readiness']==state,'final decision not bound to matrix state')
        require(b['response']==('T3:' if exposure.endswith('/alt') else 'T1:')+b['request'],'native target response differs')
        native_exchange(history,channel,b['request'],b['response'])
    require(len(keys)==12 and len(channels)==5 and len(business)==5, 'matrix Cartesian product/native outputs incomplete')
    env=[r for r in rows if r['event']=='environment']
    require(len(env)==1,'matrix preparation origin missing')
    actual={e['ChannelID'] for e in history.events.values() if e['Type']=='prepare_channel' and not e['Injected'] and e['ReceivedNS']>=env[0]['observed_ns']}
    require(actual==channels,'rejected matrix created hidden Channel/prepare')


def validate_attempts(rows):
    for state in ('ready','not_ready'):
        kept=[r for r in rows if r['event']=='sample_retained' and r['expected_readiness']==state]
        expired=[r for r in rows if r['event']=='sample_expired' and r['expected_readiness']==state]
        require(len(kept)==2 and {r['stage'] for r in kept}=={'running','timeout'} and len(expired)==1,'missing attempt/sample trajectory')
        kept.sort(key=lambda r:r['observed_ns'])
        seq=number(kept[0]['sample_seq'],1)
        remaining=number(kept[0]['initial_remaining_ms'],1)
        previous=remaining
        for r in kept:
            view=r['view']; rv=view['readiness_view']; attempt=rv['last_attempt']
            require(r['sample_seq']==seq and r['initial_remaining_ms']==remaining and view['readiness']==state and rv['sample_seq']==seq,'attempt replaced effective old sample')
            require(number(rv['remaining_ms'],1)<=previous,'attempt renewed sample lifetime')
            require(attempt['state']==('running' if r['stage']=='running' else 'idle') and (r['stage']!='timeout' or attempt['outcome']=='timeout'),'attempt outcome not observed')
            previous=rv['remaining_ms']
        ex=expired[0]; rv=ex['view']['readiness_view']
        require(ex['sample_seq']==seq and ex['view']['readiness']=='unknown' and not rv.get('sample_seq') and rv['remaining_ms']==0,'expired sample retained authority')
        require(rv['last_reported']['sample_seq']==seq and rv['last_reported']['readiness']==state,'expiry lost history')
        require(number(ex['elapsed_ns'],1)<=(remaining+250)*NS_MS,'sample lifetime extended beyond original evidence')


def health_index(rows):
    out={}
    for row in rows:
        if row['event'] not in ('health_history','saturation_target','competition_target'):
            continue
        health=row['health']; exposure=row['exposure_id']
        require(health.get('Overflow') is False and number(health.get('PID'),1)>0,'incomplete health instrument')
        last=0; records={}
        for record in health['Records']:
            ident=number(record.get('ID'),1)
            require(ident>last,'health request ID reused/out of order')
            last=ident
            start=number(record['StartedNS'],1)
            number(record['Version'],1)
            for field in ('HeaderStartedNS','HeaderWrittenNS','CancelledNS','FinishedNS'):
                value=number(record[field])
                require(value==0 or value>=start,'health request time reversed')
            require(not record['HeaderWrittenNS'] or record['HeaderStartedNS']>0 and record['HeaderWrittenNS']>=record['HeaderStartedNS'],'header completion preceded start')
            records[ident]=record
        require(exposure not in out,'duplicate full health history')
        out[exposure]=records
    return out


def validate_publication(rows):
    health=health_index(rows)
    for r in rows:
        if r['event']!='independent_publication':
            continue
        fast,slow=r['completed'],r['slow']
        actual=health['lab-a/http'].get(fast['ID']); blocked=health['lab-a/slow'].get(slow['ID'])
        for reported,full in [(fast,actual),(slow,blocked)]:
            require(full is not None and all(reported[k]==full[k] for k in ('ID','Version','Status','StartedNS','RequestURI')),'publication request not bound to actual health history')
            require(not reported['HeaderStartedNS'] or reported['HeaderStartedNS']==full['HeaderStartedNS'],'publication header boundary changed')
        visible=number(r['visible_ns'],1)
        delay=visible-number(fast['HeaderStartedNS'],1)
        require(r['bound_origin']=='health_header_started_before_oic_reduction' and 0<=delay<=500*NS_MS and delay==r['publication_upper_bound_ns'],'publication exceeded independent 500ms upper bound')
        require(slow['FinishedNS']==0 and blocked['FinishedNS']>=visible,'slow target finished before independent publication')
        state='not_ready' if r['round']%2==0 else 'ready'
        require(fast['Status']==(503 if state=='not_ready' else 200) and r['view']['readiness']==state,'wrong publication state')
        require(fast['RequestURI']==slow['RequestURI']=='/g2-publication/'+str(r['round']),'wrong reload/request phase')


def validate_saturation(rows):
    complete=[r for r in rows if r['event']=='saturation_complete']
    require(len(complete)==1,'missing load completion')
    end=complete[0]; start=number(end['window_start_ns'],1); finish=number(end['window_end_ns'],1)
    require(finish-start>=60*NS_SEC and end['targets']==16 and end['timeout_targets']==14,'short/incorrect load window')
    samples=[r for r in rows if r['event']=='scheduler_sample']
    require(len(samples)==end['samples'] and [r['sample'] for r in samples]==list(range(len(samples))),'scheduler samples missing/reordered')
    saturated=False
    for r in samples:
        o=r['observation']
        require(r['window_start_ns']==start and r['observed_ns']>=start,'load sample outside original window')
        for k in ('probe_jobs','probe_active','probe_queued','max_probe_active','max_probe_queued','probe_max_wait_ns','probe_oldest_due_ns'):
            number(o.get(k))
        require(o['probe_jobs']==16 and o['probe_active']<=2 and o['probe_queued']<=4 and o['max_probe_active']<=2 and o['max_probe_queued']<=4,'scheduler bound exceeded')
        require(max(o['probe_max_wait_ns'],o['probe_oldest_due_ns'])<=2500*NS_MS,'due job wait exceeded fixed load bound')
        saturated|=o['probe_active']==2 and o['probe_queued']==4
    require(saturated and end.get('saturated') is True,'full P/Q input never observed')
    health=health_index(rows)
    targets_=[r for r in rows if r['event']=='saturation_target']
    require(len(targets_)==len(health)==16 and sum(r.get('timeout_target') is True for r in targets_)==14,'missing distinct saturation targets')
    for r in targets_:
        require(r['window_start_ns']==start and r['window_end_ns']==finish,'target uses another load window')
        actual=[x for x in health[r['exposure_id']].values() if x['Version']==r['version'] and start<=x['StartedNS']<=finish]
        timeouts=sum(x['CancelledNS']>x['StartedNS'] and x['HeaderStartedNS']==0 for x in actual)
        require(len(actual)==r['executions'] and len(actual)>0 and timeouts==r['timeouts'] and (not r['timeout_target'] or timeouts>0),'summary hides missing actual target execution/timeout')


def injected(history, raw, conn, *, after=0, access='lab-a'):
    """An explicit artifact must have been really written, never masquerade as OIC."""
    matches=[e for (a,_),e in history.events.items() if a==access and e['Injected']
             and e['ConnID']==conn and e['Raw']==raw and e['WrittenNS']>0 and e['WriteStartedNS']>=after]
    require(len(matches)==1,'test artifact was not uniquely injected on the actual connection')
    return matches[0],history.bodies[(access,matches[0]['ID'])]


def slots(stats, opens=0, waiters=0, checks=None, channels_zero=True):
    o=stats['observation']
    require(o['pending_opens']==opens and o['http_waiters']==waiters,'pending Open/waiter accounting mismatch')
    if checks is not None:
        require(o['pending_checks']==checks,'pending check accounting mismatch')
    if channels_zero:
        require(all(stats['channels'][k]==0 for k in ('preparing','open','closing')) and stats['tickets']==0,'preflight allocated or retained Channel/ticket')


def http_outcome(value, status, code=None):
    require(value['status']==status and (code is None or value['code']==code),'unexpected HTTP status/code')
    if status>=400:
        require(not value.get('open',{}).get('channel_id'),'failed HTTP operation created Channel')
    return value.get('open',{})


def abort_outcome(row, code, history, *, ready=False):
    channel=closed(row['channel'],code)
    require(row['consumer_abort']['Type']==4 and row['consumer_abort']['Reason']==code,'consumer ABORT differs from terminal cause')
    require(ready or not history.ready(channel),'rejected admission emitted READY')
    targets(row['target'],zero=True)
    return channel


def validate_sequence(rows, history):
    for r in rows:
        if r['event'] not in ('binding_rejection','sequence_ignored_or_idempotent'):
            continue
        held,actual=history.ref(r['held'],typ='observation_snapshot')
        prior,old=history.ref(r['prior'],typ='observation_snapshot')
        artifact,candidate=injected(history,r['injected_raw'],held['ConnID'],after=held['ReceivedNS'])
        require(held['Dropped'] and prior['WrittenNS']>0 and prior['ID']<held['ID'] and prior['ConnID']==held['ConnID'],'replacement did not preserve actual prior/held boundary')
        require(candidate['seq']==actual['seq'] and candidate['query_id']==actual['query_id'],'artifact changed transport challenge')
        s=r['scenario']
        if s in ('duplicate','low_update','same_sample_conflict','low_sample'):
            require(binding(candidate['binding'])==binding(old['binding']),'sequence case changed binding')
            require(candidate['sample_seq']==old['sample_seq']-(1 if s=='low_sample' else 0),'wrong lower/duplicate sample input')
            require(candidate['update_seq']==old['update_seq']+({'low_update':-1,'low_sample':1}.get(s,0)),'wrong update ordering input')
            require(candidate['attempt']==old['attempt'] and candidate['readiness']==old['readiness'],'sequence case changed attempt/readiness')
            require(0<candidate['remaining_ms']<=old['remaining_ms'],'sequence artifact invented lifetime')
            require((candidate['observed_at']!=old['observed_at'])==(s=='same_sample_conflict'),'same-sample conflict was not applied')
        else:
            field={'old_generation':'generation','old_revision':'revision','old_source':'contract_digest'}[s]
            before,after=actual['binding'],candidate['binding']
            require(before[field]!=after[field] and all(before[k]==after[k] for k in BINDING_KEYS if k!=field),'binding case changed wrong component')
            if field=='generation':
                require(after[field]==before[field]-1 and after[field]>0,'old generation not the preceding generation')
            # Historical revision/digest must exist on a genuine earlier message.
            require(any(not e['Injected'] and e['ReceivedNS']<held['ReceivedNS'] and history.bodies[key].get('binding',{}).get(field)==after[field]
                        for key,e in history.events.items()),'claimed old identity has no observed history')
        if r['event']=='binding_rejection':
            error,body=history.ref(r['error'],typ='error')
            welcome,w=history.ref(r['welcome'],typ='welcome')
            first,fresh=history.ref(r['new_first_snapshot'],typ='observation_snapshot')
            require(body['code']=='ProtocolError' and error['ConnID']==held['ConnID'] and error['ReceivedNS']>=artifact['WriteStartedNS'],'violation did not terminate offending session')
            require(welcome['ConnID']!=held['ConnID'] and first['ConnID']==welcome['ConnID'] and fresh['binding']['generation']>actual['binding']['generation'],'reconnect reused identity')
            require(w['generation']==fresh['binding']['generation'],'welcome and snapshot disagree')
            unknown=r['new_generation_unknown']
            require(unknown['readiness']=='unknown' and unknown['readiness_view']['generation']==fresh['binding']['generation'] and not unknown['readiness_view'].get('sample_seq'),'new generation inherited old sample')
        else:
            guard,_=history.ref(r['next_response_held'],typ='observation_snapshot')
            require(guard['ConnID']==held['ConnID'] and guard['QueryID']!=held['QueryID'] and guard['ReceivedNS']>=artifact['WrittenNS'],'no actual ordered progress barrier')
            before,after=r['before']['readiness_view'],r['after']['readiness_view']
            require(after['last_reported']['sample_seq']==old['sample_seq'] and after['last_attempt']==old['attempt'] and after['remaining_ms']<=before['remaining_ms'],'lower/duplicate observation replaced or renewed cache')


def validate_security(rows, history):
    expected={'credential_revoked':'Revoked','policy_revoked':'Revoked','target_revision':'RevisionChanged','generation_replaced':'ChannelInterrupted','license_expired':'LicenseExpired'}
    for r in rows:
        if r['event']!='security_final_round':
            continue
        final,result,check=history.result(r['final'])
        require(not result.get('code') and result.get('decision_id'),'security fixture lacks real positive final')
        code=expected[r['scenario']]
        channel=abort_outcome(r,code,history)
        require(result['channel_id']==channel and r['before']['revision']==result['binding']['revision'],'security result/channel binding mismatch')
        action=number(r['security_action_started_ns'],1)
        origin=number(r['cleanup_origin_ns'],1)
        require(final['ReceivedNS']<=action<=origin<=check['ReceivedNS']+500*NS_MS,'security cancellation did not win original final-check window')
        require(r['cleanup_finished_ns']-origin<=2*NS_SEC,'security cleanup exceeded original budget')
        if r['scenario'] in ('credential_revoked','policy_revoked'):
            admin=r['admin_action']
            require((admin['method'],admin['resource'],admin['status'])==(('DELETE','token',200) if r['scenario']=='credential_revoked' else ('PATCH','policy',200)),'security fixture did not apply the registered administrative action')
            require(admin['started_ns']==action and action<=admin['completed_ns']<=origin,'administrative response not observed before terminal outcome')
        if r['scenario']=='generation_replaced':
            require(any(e['Type']=='welcome' and e['ConnID']!=final['ConnID'] and action<=e['ReceivedNS']<=origin
                        and history.bodies[key]['generation']>result['binding']['generation'] for key,e in history.events.items() if not e['Injected']),'replacement generation not observed before termination')
        else:
            require(final['WriteStartedNS']>=action and final['WrittenNS']>0,'old final not actually released after security action')
        if r['scenario']=='license_expired':
            license=r['license_before']
            require(50<number(license['license_remaining_ms'])<=200 and license['generation']==result['binding']['generation'],'license fixture lacks real near-expiry input')
            beats=r['dropped_heartbeats']
            require(len(beats)==2 and len({e['ID'] for e in beats})==2,'license input lacks two independent heartbeats')
            for e in beats:
                beat,_=history.ref(e,typ='heartbeat')
                require(beat['Dropped'] and beat['ReceivedNS']<final['ReceivedNS'],'heartbeat was not actually dropped before final')
            restored=[x for x in rows if x['event']=='license_restored_between_rounds' and x['round']==r['round']]
            require(len(restored)==1,'renewal boundary missing')
            state=restored[0]['restored']
            require(state['connected'] and state['registered'] and state['license_remaining_ms']>=2000,'next round lacks renewed lease')
            require(closed(restored[0]['old_channel'],'LicenseExpired')==channel,'renewal revived old Channel')


def validate_lifecycle(rows, history):
    for r in rows:
        event=r['event']
        if event=='final_loss_round':
            final,result,check=history.result(r['final'])
            channel=abort_outcome(r,'ReadinessCheckFailed',history)
            require(result['channel_id']==channel and not result.get('code') and result.get('decision_id'),'lost final was not positive')
            require(r['channel']['readiness_decision']['reason']=='timeout','known final deadline lost first reason')
            origin=number(r['cleanup_origin_ns'],1)
            # The OIS deadline begins before proxy receipt, so proxy receipt+500ms
            # is not a valid lower bound on observing its timeout.
            require(origin>=final['ReceivedNS'] and r['cleanup_finished_ns']<=origin+2*NS_SEC,'timeout/cleanup original deadline not enforced')
            if r['scenario']=='final_response_drop':
                require(final['Dropped'] and final['WrittenNS']==0,'lost response was forwarded')
            else:
                require(final['WriteStartedNS']>=origin and final['WrittenNS']>0,'late positive did not arrive after failure')
        elif event=='pending_capacity_round':
            final,_,check=history.result(r['held'],'preflight')
            require(final['Dropped'],'full-slot result was not dropped')
            slots(r['full'],16,16,16)
            http_outcome(r['overflow'],429,'ResourceExhausted')
            require(len(r['outcomes'])==16,'missing independent pending outcomes')
            for out in r['outcomes']:
                http_outcome(out,503,'ReadinessCheckFailed')
            real_checks=[e for e in history.events.values() if e['Type']=='admission_check' and e['Stage']=='preflight'
                         and not e['Injected'] and check['ReceivedNS']<=e['ReceivedNS']<=r['cleanup_origin_ns']]
            require(len(real_checks)==16,'full-slot input reused/missed independent real checks')
            slots(r['after'],checks=0)
            require(0<=r['cleanup_finished_ns']-r['cleanup_origin_ns']<=2*NS_SEC,'pending slots outlived cleanup')
        elif event=='cli_response_drop_round':
            final,_,_=history.result(r['held'],'preflight')
            require(final['Dropped'] and final['WrittenNS']==0,'CLI preflight response was not lost')
            require(type(r['exit_code']) is int and r['exit_code']!=0 and r['stdout']=='' and 'ReadinessCheckFailed' in r['stderr'],'CLI loss silently succeeded')
            slots(r['after'],checks=0)
            checks=[e for e in history.events.values() if e['Type']=='admission_check' and not e['Injected'] and final['ReceivedNS']<=e['ReceivedNS']<=r['observed_ns']]
            require(not checks,'CLI retried implicitly after lost response')
        elif event=='duplicate_closed_positive_round':
            final,result,_=history.result(r['final'])
            channel=closed(r['channel'],'Completed'); next_channel=closed(r['next_channel'],'Completed')
            require(result['channel_id']==channel and final['WrittenNS']>0 and next_channel!=channel,'closed positive changed identity')
            require(len(history.ready(channel))==r['provider_ready_count']==1 and len(history.ready(next_channel))==1,'duplicate READY/reused operation')
            for field in ('duplicate_slot','late_slot'):
                slot,original=history.ref(r[field],typ='observation_snapshot')
                require(slot['Dropped'] and slot['ConnID']==final['ConnID'],'duplicate input did not replace a real slot')
                matches=[(e,history.bodies[k]) for k,e in history.events.items() if e['Injected'] and e['ConnID']==final['ConnID'] and e['CheckID']==final['CheckID'] and e['WriteStartedNS']>=slot['ReceivedNS'] and history.bodies[k].get('seq')==original['seq']]
                require(len(matches)==1 and matches[0][0]['WrittenNS']>0,'positive duplicate not really injected')
                require(all(matches[0][1].get(k)==result.get(k) for k in result if k!='seq'),'duplicate artifact changed result')
            require(r['oic_after']['registered'] and r['oic_after']['connected'] and r['oic_after']['generation']==result['binding']['generation'],'late positive damaged session')
        elif event=='observation_cancel_before_ready_round':
            final,result,_=history.result(r['final'])
            channel=abort_outcome(r,'ReadinessCheckFailed',history,ready=True)
            ready,_=history.ref(r['held_ready'],typ='READY')
            abort,_=history.ref(r['held_provider_abort'],typ='ABORT')
            failed,body=history.ref(r['actual_prepare_failed'],typ='prepare_failed')
            require(result['channel_id']==channel and ready['ChannelID']==abort['ChannelID']==failed['ChannelID']==channel,'READY gate mixed Channels')
            require(body['code']=='ReadinessCheckFailed' and body['reason']=='observation_changed' and r['channel']['readiness_decision']['reason']=='observation_changed','observation change lost precise first cause')
            require(final['WrittenNS']>0 and final['ReceivedNS']<=ready['ReceivedNS']<failed['ReceivedNS']<final['ReceivedNS']+500*NS_MS,'reload missed original final window')
            require(failed['WrittenNS']>0 and failed['WrittenNS']<=ready['WriteStartedNS'] and failed['WrittenNS']<=abort['WriteStartedNS'],'held independent legs bypassed prepare_failed ordering')
            require(r['before']['revision']==r['after']['revision'] and r['before']['readiness_view']['observation_revision']!=r['after']['readiness_view']['observation_revision'],'probe-only update changed wrong revision')
        elif event=='running_probe_reload_round':
            old=closed(r['old_channel'],'Completed')
            final,result,_=history.result(r['new_final'])
            before,after=r['before'],r['after']
            require(before['revision']==after['revision'] and before['readiness_view']['observation_revision']!=after['readiness_view']['observation_revision'],'running probe reload changed target revision')
            require(result['binding']['observation_revision']==after['readiness_view']['observation_revision'] and result['binding']['contract_digest']==after['readiness_view']['contract_digest'],'new Channel used old observation contract')
            old_ready=history.ready(old);new_ready=history.ready(result['channel_id'])
            require(len(old_ready)==len(new_ready)==1 and old_ready[0]['WrittenNS']<=r['running_proved_ns']<final['ReceivedNS'],'running-before-reload ordering missing')
            native_exchange(history,old,'g2-running-'+str(r['round']),'T1:g2-running-'+str(r['round']))
            native_exchange(history,result['channel_id'],'new-observation','T1:new-observation')
            targets(r['target'])


def validate_layers(rows):
    cases=[r for r in rows if r['event']=='diagnostic_layers']
    require({r['directory']['readiness'] for r in cases}=={'ready','unknown'},'diagnostic layer input matrix incomplete')
    channels=set()
    for r in cases:
        channel=closed(r['final'],'ChannelInterrupted')
        require(channel not in channels and channel==r['preflight']['channel_id'],'diagnostic Channel reused/mismatched')
        channels.add(channel)
        require(r['preflight']['state']=='preparing' and r['preflight']['readiness_stage']=='preflight' and r['preflight']['admission_mode']=='best_effort','Open response falsely claims final admission')
        require(r['final']['readiness_decision']['source']=='local_check' and r['final']['readiness_decision']['effective_admission']=='best_effort','GET lacks actual final decision')
        require(r['preflight']['readiness']==r['final']['readiness_decision']['readiness']==r['directory']['readiness'],'directory/preflight/final diagnostic state was conflated')
        require(r['business_outcome']=='incomplete_unknown' and r['business_error']=='ChannelInterrupted' and r['response_bytes']==7,'truncated response was classified as successful business')
        require(r['request']=='reset' and r['target_name']=='T1' and data(r['response_raw'])==b'T1:part' and len(data(r['response_raw']))==r['response_bytes'],'reported response count differs from native bytes')
        targets([r['target_record']])
        require(data(r['target_record']['Data'])==b'reset' and r['target_record']['EOF'] is True,'provider did not receive actual reset request/FIN')
        times=[number(r[k],1) for k in ('open_started_ns','open_returned_ns','ready_received_ns','first_read_ns','finished_ns')]
        require(times==sorted(times),'business layer timestamps reversed')
        require(r['o1_group']==dict(admission='best_effort',readiness=r['directory']['readiness'],result='ChannelInterrupted'),'O1 group conflates directory/admission/business')
    rejected=[r for r in rows if r['event']=='strict_expired_rejected']
    require(len(rejected)==1 and rejected[0]['code']=='ReadinessRequired' and rejected[0]['reason']=='expired' and rejected[0]['directory']['readiness']=='unknown' and rejected[0]['directory']['readiness_view']['remaining_ms']==0,'strict expiry was allowed or wrong reason')


EXPORT_COUNTERS=('oi_observation_samples_total','oi_observation_sample_expired_total',
                 'oi_observation_dirty_coalesced_total','oi_readiness_checks_total',
                 'oi_open_best_effort_total','oi_open_require_ready_total','oi_metrics_exports_total')
EXPORT_HISTOGRAMS=('oi_probe_queue_wait_seconds','oi_probe_duration_seconds','oi_observation_query_rtt_seconds',
                   'oi_observation_publication_delay_seconds','oi_readiness_check_duration_seconds','oi_control_critical_duration_seconds')


def parse_exposition(x, status=200):
    """Parse the actual bounded 0.0.4 body, independently of Go's DTO summaries."""
    require(x['status']==status,'unexpected scrape HTTP status')
    start,end=number(x['started_ns'],1),number(x['completed_ns'],1)
    require(end>=start,'scrape time reversed')
    text=x['body']
    require(isinstance(text,str) and len(text.encode('utf-8'))<=65536,'scrape body exceeds 64KiB')
    if status!=200:
        return None
    content=x['content_type'].lower().replace(' ','').replace('"','')
    require(content.startswith('text/plain;') and 'version=0.0.4' in content and 'charset=utf-8' in content,'wrong exposition content type')
    require(text.endswith('\n') and '\r' not in text,'exposition not complete LF text')
    help_,types,samples={}, {}, {}
    vocab=defaultdict(set)
    for line in text.splitlines():
        if line.startswith('# '):
            parts=line.split(' ',3)
            require(len(parts)==4 and parts[1] in ('HELP','TYPE'),'unsupported/malformed exposition metadata')
            dest=help_ if parts[1]=='HELP' else types
            require(parts[2] not in dest and parts[3],'duplicate/empty exposition metadata')
            dest[parts[2]]=parts[3]
            continue
        m=re.fullmatch(r'(oi_[a-z0-9_]+)(?:\{([^{}]*)\})? ([0-9.eE+\-]+)',line)
        require(m is not None,'invalid metric syntax or unexpected timestamp')
        name,labeltext,raw=m.groups(); labels={}
        if labeltext:
            for label in labeltext.split(','):
                lm=re.fullmatch(r'([a-z_]+)="([^"\\]*)"',label)
                require(lm is not None,'invalid metric label/escape')
                key,value=lm.groups()
                require(key not in labels,'duplicate metric label')
                if key!='le':
                    require(key in ('result','reason','protocol','stage') and re.fullmatch('[a-z0-9_]{1,32}',value),'unbounded/entity metric label')
                    vocab[key].add(value)
                labels[key]=value
        value=Decimal(raw)
        require(value.is_finite() and value>=0,'negative/nonfinite metric value')
        key=name,tuple(sorted(labels.items()))
        require(key not in samples,'duplicate metric series')
        samples[key]=value
    require(set(help_)==set(types) and len(types)==13 and Counter(types.values())=={'counter':7,'histogram':6},'missing/incorrect 13 metric families')
    require(types==dict([(n,'counter') for n in EXPORT_COUNTERS]+[(n,'histogram') for n in EXPORT_HISTOGRAMS]),'export omitted an adopted metric category')
    used=set();series=[]
    for name,typ in types.items():
        require(name.startswith('oi_') and name.endswith('_total' if typ=='counter' else '_seconds'),'metric family units/type invalid')
        entries=[(k,v) for k,v in samples.items() if k[0]==name] if typ=='counter' else [(k,v) for k,v in samples.items() if k[0] in (name+'_bucket',name+'_sum',name+'_count')]
        require(entries,'empty metric family')
        if typ=='counter':
            require(all('le' not in dict(k[1]) for k,_ in entries),'counter has histogram label')
        else:
            bins=defaultdict(dict)
            for (metric,lab),value in entries:
                labels=dict(lab);bound=labels.pop('le',None);ident=tuple(sorted(labels.items()))
                if metric.endswith('_bucket'):
                    require(bound is not None,'histogram bucket missing bound')
                    limit=Decimal('Infinity') if bound=='+Inf' else Decimal(bound)
                    require(limit>0 and not limit.is_nan() and limit not in bins[ident],'invalid/duplicate histogram bound')
                    bins[ident][limit]=value
                else:
                    require(bound is None,'sum/count has le label')
            for ident,buckets in bins.items():
                count=samples.get((name+'_count',ident));total=samples.get((name+'_sum',ident))
                require(count is not None and total is not None and count==count.to_integral_value() and len(buckets)==8 and Decimal('Infinity') in buckets,'incomplete fixed histogram')
                counts=[buckets[k] for k in sorted(buckets)]
                require(counts==sorted(counts) and counts[-1]==count and all(v==v.to_integral_value() for v in counts),'histogram is not cumulative/integer')
            require(len(entries)==10*len(bins),'unbound histogram sum/count')
        for key,_ in entries:
            used.add(key)
            metric,lab=key; labels=dict(lab);le=labels.pop('le',None)
            items=[k+'='+json.dumps(v) for k,v in sorted(labels.items())]
            if le is not None:items.append('le='+json.dumps(le))
            series.append(metric+'{'+','.join(items)+'}')
    require(used==set(samples),'sample has no declared family')
    vocabulary={k:sorted(v) for k,v in vocab.items()}
    require(all(len(v)<=32 for v in vocabulary.values()),'unbounded label vocabulary')
    require(sorted(series)==x['series_keys'] and vocabulary==x['label_values'],'raw exposition differs from derived Go series/vocabulary')
    return samples


def export_failure_count(parsed):
    return sum(parsed.get(('oi_metrics_exports_total',(('result',v),)),Decimal('-1000000')) for v in ('timeout','write_error'))


def slow_scrapes(writes,before,rejected,bounded,after,closed_at):
    baseline=parse_exposition(before); parse_exposition(rejected,429)
    failed=parse_exposition(bounded);parse_exposition(after)
    require(len(writes)==2 and {w['socket'] for w in writes}=={0,1},'slow input does not contain two real sockets')
    origin=min(number(w['connected_ns'],1) for w in writes)
    for w in writes:
        require(origin<=w['connected_ns']<=w['started_ns']<=w['completed_ns']<=origin+NS_SEC,'slow socket exceeded original shared 1s write budget')
        require(0<number(w['written_bytes'])<=number(w['planned_bytes'],1) and isinstance(w['error'],str),'slow socket wrote no real requests')
    require(before['completed_ns']<=origin and origin<=rejected['started_ns']<=rejected['completed_ns']<=closed_at,'429 did not overlap slow input')
    require(export_failure_count(failed)-export_failure_count(baseline)>=2,'both slow writes did not fail before fixture close')
    require(rejected['completed_ns']<=bounded['completed_ns']<=closed_at and bounded['completed_ns']<=origin+NS_SEC,'bounded failure observed after close/input budget')
    require(closed_at<=after['completed_ns']<=closed_at+2*NS_SEC,'exporter failed original 2s recovery')
    require(before['series_keys']==after['series_keys'] and before['label_values']==after['label_values'],'slow export changed cardinality')


def validate_export(rows):
    initial={};previous={}
    for r in rows:
        if r['event']=='export_initial':
            require(r['role'] in ('ois','oic') and r['role'] not in initial,'duplicate exporter baseline role')
            previous[r['role']]=parse_exposition(r['exposition']);initial[r['role']]=r['exposition']
    entities=[r for r in rows if r['event']=='export_entity']
    if entities:
        require(set(initial)=={'ois','oic'} and [r['index'] for r in entities]==list(range(32)),'entity input incomplete/reordered')
        for key in ('target','subject','channel_id','revision'):
            require(len({r[key] for r in entities})==32,'entity input reused '+key)
        for r in entities:
            require(r['request']=='native-entity-'+format(r['index'],'02d') and r['response']=='target-entity-'+format(r['index'],'02d')+':'+r['request'],'new entity native response incomplete')
            targets([r['target_record']])
            require(data(r['target_record']['Data'])==r['request'].encode(),'native entity target differs from request')
            changed=[x for x in rows if x['event']=='export_after_entity' and x['index']==r['index']]
            require(len(changed)==2 and {x['role'] for x in changed}=={'ois','oic'},'missing per-entity dual exporter observations')
            for x in changed:
                metrics=x['exposition'];parsed=parse_exposition(metrics);base=initial[x['role']]
                require(metrics['started_ns']>=r['observed_ns'] and metrics['series_keys']==base['series_keys'] and metrics['label_values']==base['label_values'],'entity changed finite series/vocabulary or scrape preceded change')
                require(all(value>=previous[x['role']][key] for key,value in parsed.items()),'counter reset/regressed in the same process')
                previous[x['role']]=parsed
                require(all(str(r[k]) not in metrics['body'] for k in ('target','subject','channel_id','exposure_id')),'metric body leaks entity identity')
    slow=[r for r in rows if r['event']=='export_slow_round']
    if slow:
        require(len(slow)==40 and {(r['role'],r['round']) for r in slow}=={(role,i) for role in ('ois','oic') for i in range(20)},'missing exporter role/round')
        require(len({r['channel_id'] for r in slow})==40,'native operation reused across slow rounds')
        for r in slow:
            require((r['read_buffer_requested'],r['requests_per_socket'],r['write_budget_ms'])==(1024,2048,1000),'slow fixture input differs from registration')
            rejects=[x for x in r['polls'] if x['status']==429]
            require(rejects,'slow input never occupied actual slots')
            for x in r['polls']:parse_exposition(x,x['status'])
            slow_scrapes(r['slow_writes'],r['before'],rejects[0],r['bounded_failure_before_close'],r['after'],r['close_started_ns'])
            require(rejects[0]['completed_ns']<=r['native_started_ns']<=r['native_finished_ns']<=r['close_started_ns'] and r['native_response']=='T1:export-concurrent','native operation did not complete during slow input')


def competition_sample(s,full=False):
    require(number(s['started_ns'],1)<=number(s['completed_ns'],1),'stats interval reversed')
    server,client=s['ois']['observation'],s['oic']['observation']
    require(s['oic']['connected'] and s['oic']['registered'] and s['ois']['accesses_online']==2,'competition lost actual online topology')
    require(client['probe_jobs']==16 and client['probe_active']<=2 and client['probe_queued']<=4 and client['max_probe_active']<=2 and client['max_probe_queued']<=4,'competition exceeded P/Q bounds')
    require(max(client['probe_max_wait_ns'],client['probe_oldest_due_ns'])<=2500*NS_MS,'competition scheduler starved')
    require(0<=server['pending_queries']<=8 and 0<=server['observation_backlog']<=server['observation_capacity']==16 and 0<=server['dirty_marked']<=16,'source backlog/query capacity invalid')
    if full:
        require((server['pending_queries'],server['observation_backlog'],client['probe_active'],client['probe_queued'])==(8,16,2,4),'full input not actually simultaneous')


def competition_revocation(row,history):
    """Prove control fixed OIC's terminal cause while downlink ABORT was held.

    The consumer is another leg and may receive its genuine ABORT before the
    control proxy captures close_channel. Its clock starts at admin revoke,
    while the control processing budget still starts at the real receipt.
    """
    victim=row['victim_channel_id'];barrier=row['provider_abort_barrier']
    require(isinstance(victim,str) and victim and isinstance(barrier,dict)
            and barrier.get('channel_id')==victim,'provider barrier targets another victim')
    armed,control_release,release,cancelled_at=(number(barrier.get(k),1) for k in
        ('armed_ns','control_release_requested_ns','release_requested_ns','cancelled_ns'))
    revoke_started=number(row['revoke_started_ns'],1)
    consumer_abort=number(row['consumer_abort_ns'],1)
    before,after=row['backlog_before'],row['backlog_after']
    completed=number(after['completed_ns'],1)
    require(isinstance(row['slow_writes'],list) and len(row['slow_writes'])==2,'missing shared input window')
    origin=min(number(w['connected_ns'],1) for w in row['slow_writes'])
    revoke,body=history.ref(row['revoke_wire'],typ='close_channel')
    require(revoke['Role']=='control' and revoke['Direction']=='ois_to_oic'
            and revoke['ChannelID']==victim and body.get('code')=='Revoked'
            and not revoke['Dropped'] and not revoke['WriteFailed']
            and revoke['WrittenNS']>0,'missing real control revocation of victim')
    abort,frame=history.ref(row['provider_abort'],typ='ABORT')
    require(abort==row['provider_abort'],'provider ABORT is not its final history snapshot')
    require(abort['Role']=='provider' and abort['Direction']=='ois_to_oic'
            and abort['ChannelID']==victim and frame['value']==0x0106
            and not abort['Dropped'] and number(barrier.get('event_id'),1)==abort['ID'],
            'provider barrier did not capture the genuine Revoked ABORT')
    witness,witness_frame=history.ref(row['oic_abort_witness'],typ='ABORT')
    require(witness==row['oic_abort_witness'] and witness['Role']=='provider'
            and witness['Direction']=='oic_to_ois' and witness['ChannelID']==victim
            and witness['ConnID']==abort['ConnID'] and witness_frame['value']==0x0106
            and not witness['Dropped'],'missing genuine OIC Revoked first-terminal witness')
    require(origin<=armed<=revoke_started<=abort['ReceivedNS']<=control_release
            <=revoke['WriteStartedNS']<=witness['ReceivedNS']<=release<=completed<=cancelled_at<=origin+NS_SEC
            and revoke['WrittenNS']<=completed,
            'provider/control barrier order escaped original 1s input window')
    require(revoke_started<=revoke['ReceivedNS']<=control_release
            and 0<=completed-revoke['ReceivedNS']<=500*NS_MS,
            'control revocation exceeded original conservative 500ms boundary')
    require(revoke_started<=consumer_abort<=completed,'consumer revocation outside admin/observation window')
    require(after['oic']['closed'].get('Revoked',0)==before['oic']['closed'].get('Revoked',0)+1,
            'actual OIC did not finish victim once after first-terminal witness and drain')
    require((before['oic']['epoch'],before['oic']['generation'])
            ==(after['oic']['epoch'],after['oic']['generation']),'control reconnected under load')
    conn=history.connections.get(('lab-a',abort['ConnID']))
    require(conn==row['provider_abort_connection'] and conn['Role']=='provider'
            and conn['ChannelID']==victim and conn['Closed'] is True and conn['Workers']==0
            and max(abort['ReceivedNS'],witness['ReceivedNS'])<=number(conn['ClosedNS'],1)<=cancelled_at,
            'provider ABORT terminal connection was not actually joined inside input window')
    # Inspect the complete immutable history, not just the referenced event:
    # another copy could otherwise close the victim before the observed control.
    for (access,_),event in history.events.items():
        if (event['Type']=='ABORT' and event['Role']=='provider'
                and event['Direction']=='ois_to_oic' and event['ChannelID']==victim):
            require(access=='lab-a' and not event['Injected'] and not event['Dropped'],
                    'victim provider ABORT was injected/dropped or from another access')
            started,done=event['WriteStartedNS'],event['WrittenNS']
            require(not started or release<=started<=conn['ClosedNS'],
                    'victim provider ABORT forwarded before release')
            require(not done or started<=done<=conn['ClosedNS'], 'provider write completed after connection closure')
            require((started>0 and (done>0 or event['WriteFailed']))
                    or (started==0 and done==0 and not event['WriteFailed']),
                    'provider ABORT has no actual write/closed-before-write terminal')


def competition_window(full,end):
    """Keep preparation bounded and measure 60s only after slow input exists."""
    start,finish=number(end['window_start_ns'],1),number(end['window_end_ns'],1)
    fields=('prepare_started_ns','prepare_deadline_ns','prepared_ns','slow_input_applied_ns')
    prepare,deadline,prepared,slow=(number(end[k],1) for k in fields)
    require(full['window_start_ns']==start and all(full.get(k)==end[k] for k in fields),
            'competition preparation/input anchors differ')
    require(deadline-prepare==3*NS_SEC and start<=prepare<=slow
            <=number(full['sample']['started_ns'],1)<=number(full['sample']['completed_ns'],1)
            <=prepared<=deadline, 'competition preparation escaped original 3s/input order')
    require(finish-slow>=60*NS_SEC, 'slow competition input did not last the full 60s')
    return start,finish


def competition_cycle_window(row,prepared,finish):
    require(prepared<=row['backlog_before']['started_ns']<=row['native_started_ns']
            <=row['native_completed_ns']<=row['revoke_started_ns']
            <=row['revoke_returned_ns']<=row['backlog_after']['completed_ns']<=finish,
            'critical operation outside prepared slow-input/causal window')


def competition_query_witness(witness, sample, history, max_age, earliest, latest, resource_base,
                              preparation=False, embedded_sample=False, registered=None):
    """Check the original selected queries, never fill times from later history.

    Final history may contain a write completed after the snapshot copied its
    event. Such a write is not evidence that it was eligible at that snapshot.
    The selected projection must already have its exact successful boundaries;
    the prefix cursor excludes events received only after the original copy.
    """
    keys = {'ois_pid', 'oic_pid', 'sample_started_ns', 'sample_completed_ns', 'sample_returned_ns',
            'wire_event_count', 'wire_last_event_id', 'wire_snapshot_returned_ns',
            'evaluated_ns', 'returned_ns', 'max_age_ms', 'result', 'queries'}
    if preparation:
        keys |= {'attempt', 'callback_returned_ns'}
    if embedded_sample:
        keys.add('sample')
    require(isinstance(witness, dict) and set(witness) == keys,
            'missing/malformed selected query witness')
    require(number(witness['ois_pid'], 1) == number(resource_base['ois']['pid'], 1)
            and number(witness['oic_pid'], 1) == number(resource_base['oics']['lab-a']['pid'], 1),
            'query witness role PID differs from the original resource baseline')
    competition_sample(sample, True)
    require(witness['sample_started_ns'] == sample['started_ns']
            and witness['sample_completed_ns'] == sample['completed_ns'],
            'query witness refers to another composite sample')
    ss, se, sr, wr, evaluated, returned = (number(witness[k], 1) for k in (
        'sample_started_ns', 'sample_completed_ns', 'sample_returned_ns',
        'wire_snapshot_returned_ns', 'evaluated_ns', 'returned_ns'))
    require(earliest <= ss <= se <= sr <= wr <= evaluated <= returned <= latest,
            'query witness escaped original sampling/acceptance window')
    require(type(witness['max_age_ms']) is int and witness['max_age_ms'] == max_age
            and witness['result'] is True, 'query age predicate was changed or not true')
    if preparation:
        # 512 bounds retained failure history, not the original predicate loop.
        # Successful standalone witnesses survive diagnostic-history overflow.
        number(witness['attempt'], 1)
        require(returned <= number(witness['callback_returned_ns'], 1) <= latest,
                'query callback returned outside original preparation deadline')
    if embedded_sample:
        require(witness['sample'] == sample, 'embedded sample is not its own original sample')
    proxy = history.snapshots.get('lab-a')
    require(isinstance(proxy, dict), 'missing lab-a complete query history')
    count = number(witness['wire_event_count'], 1, len(proxy['Events']))
    prefix = proxy['Events'][:count]
    last_id = number(witness['wire_last_event_id'], 1)
    require(prefix[-1]['ID'] == last_id, 'query snapshot cursor/count is not a real prefix')
    # Every genuine prefix receive happened before Snapshot returned. Later
    # writes may be incomplete in that copy and must not be backfilled.
    require(all(e['Injected'] or e['ReceivedNS'] <= wr for e in prefix),
            'query snapshot cursor includes a future receive')
    projections = witness['queries']
    require(isinstance(projections, list) and len(projections) == 8,
            'selected query witness does not contain eight queries')
    qkeys = {'event_id', 'conn_id', 'query_id', 'binding', 'received_ns',
             'write_started_ns', 'written_ns', 'deadline_ms', 'injected', 'dropped', 'decoded'}
    ids, challenges, sources, connections = [], set(), set(), set()
    for q in projections:
        require(isinstance(q, dict) and set(q) == qkeys,
                'malformed selected query projection')
        eid = number(q['event_id'], 1)
        require(eid <= last_id and ('lab-a', eid) in history.events,
                'query projection is outside the original snapshot prefix')
        e, body = history.events['lab-a', eid], history.bodies['lab-a', eid]
        require(e['Role'] == 'control' and e['Type'] == 'observation_query'
                and e['Direction'] == 'ois_to_oic' and not e['Injected']
                and not e['Dropped'] and not e['WriteFailed'],
                'selected query is not a genuine successfully forwarded control query')
        require(q['injected'] is False and q['dropped'] is False and q['decoded'] is True,
                'selected query projection claims an invalid decode/provenance')
        for projected, actual in (('event_id', 'ID'), ('conn_id', 'ConnID'),
                                  ('query_id', 'QueryID'), ('received_ns', 'ReceivedNS'),
                                  ('write_started_ns', 'WriteStartedNS'), ('written_ns', 'WrittenNS')):
            require(q[projected] == e[actual], 'selected query differs from complete wire history')
        number(q['conn_id'], 1)
        received, written_start, written = (number(q[k], 1) for k in
                                           ('received_ns', 'write_started_ns', 'written_ns'))
        require(received <= written_start <= written <= wr
                and 0 <= evaluated-received <= max_age*NS_MS,
                'selected query is stale or was not written at the original snapshot')
        number(q['deadline_ms'], 400, 500)
        require(q['deadline_ms'] == body.get('deadline_ms') and q['query_id'] == body.get('query_id')
                and q['binding'] == body.get('binding'), 'query decoded projection differs from raw bytes')
        b = binding(q['binding'])
        if registered is not None:
            require(type(q['binding'].get('contract_version')) is int
                    and q['binding']['contract_version'] == 1,
                    'query binding has an invalid protocol contract version')
            require(q['binding'] == registered.get(b[3]),
                    'query binding differs from the original registered preload inventory')
        require(b[0] == 'lab-a' and b[3] in COMPETITION_SOURCES
                and b[1] == number(sample['ois']['epoch'], 1) == number(sample['oic']['epoch'], 1)
                and b[2] == number(sample['oic']['generation'], 1),
                'query binding differs from actual composite process generation')
        ids.append(eid); challenges.add(q['query_id']); sources.add(b[3]); connections.add(e['ConnID'])
    require(ids == sorted(set(ids), reverse=True) and len(challenges) == len(sources) == 8
            and len(connections) == 1, 'selected queries reuse identity or reverse scan order/connection differs')
    # WrittenNS is captured before the proxy publishes it under its mutex.
    # Final history cannot prove when an unselected write became visible to
    # Snapshot, even when that numeric timestamp precedes Snapshot's return.
    # Validate the eight original positive projections, not a reconstructed
    # selection with retroactively completed events.
    return next(iter(connections))


def competition_preload_queue(witness, history, earliest, latest, resource_base):
    """A real P2/Q4 sample and registered inventory, without a query-age claim."""
    keys = {'ois_pid', 'oic_pid', 'sample', 'sample_returned_ns', 'attempt',
            'callback_returned_ns', 'wire_event_count', 'wire_last_event_id',
            'wire_snapshot_returned_ns', 'evaluated_ns', 'returned_ns', 'result',
            'control_conn_id', 'register', 'register_ack', 'bindings'}
    require(isinstance(witness, dict) and set(witness) == keys,
            'missing/malformed probe queue preload witness')
    require(number(witness['ois_pid'], 1) == number(resource_base['ois']['pid'], 1)
            and number(witness['oic_pid'], 1) == number(resource_base['oics']['lab-a']['pid'], 1),
            'preload queue witness role PID differs from the original resource baseline')
    sample = witness['sample']
    competition_sample(sample)
    competition_process_binding(sample, resource_base)
    require(sample['oic']['access_id'] == 'lab-a'
            and sample['ois']['epoch'] == sample['oic']['epoch']
            and (sample['oic']['observation']['probe_active'], sample['oic']['observation']['probe_queued']) == (2, 4),
            'preload probe queue or process identity is not established')
    ss, se = number(sample['started_ns'], 1), number(sample['completed_ns'], 1)
    sr, wr, evaluated, returned, callback = (number(witness[k], 1) for k in (
        'sample_returned_ns', 'wire_snapshot_returned_ns', 'evaluated_ns', 'returned_ns', 'callback_returned_ns'))
    number(witness['attempt'], 1)
    require(earliest <= ss <= se <= sr <= wr <= evaluated <= returned <= callback <= latest
            and witness['result'] is True, 'probe queue witness escaped original sampling/acceptance window')
    proxy = history.snapshots.get('lab-a')
    require(isinstance(proxy, dict), 'missing original registered preload history')
    count = number(witness['wire_event_count'], 1, len(proxy['Events']))
    prefix = proxy['Events'][:count]
    last_id = number(witness['wire_last_event_id'], 1)
    require(prefix[-1]['ID'] == last_id
            and [e['ID'] for e in prefix] == list(range(1, count+1))
            and all(e['Injected'] or e['ReceivedNS'] <= wr for e in prefix),
            'preload snapshot cursor includes a future or absent receive')
    require(not any(e['Dropped'] for e in prefix), 'snapshot Drop occurred before probe queue preload')
    conn = number(witness['control_conn_id'], 1)
    require(history.connections.get(('lab-a', conn), {}).get('Role') == 'control',
            'preload registration names another connection')
    bodies, events = {}, {}
    for name, direction in (('register', 'oic_to_ois'), ('register_ack', 'ois_to_oic')):
        projection = witness[name]
        require(isinstance(projection, dict) and set(projection) == {
            'event_id', 'conn_id', 'received_ns', 'write_started_ns', 'written_ns'},
            'malformed preload registration projection')
        eid = number(projection['event_id'], 1)
        require(eid <= last_id and ('lab-a', eid) in history.events,
                'registration outside original preload snapshot prefix')
        event, body = history.events['lab-a', eid], history.bodies['lab-a', eid]
        require(event['Role'] == 'control' and event['Type'] == name
                and event['Direction'] == direction and event['ConnID'] == conn
                and not event['Injected'] and not event['Dropped'] and not event['WriteFailed'],
                'preload inventory is not genuine forwarded registration')
        for projected, actual in (('event_id', 'ID'), ('conn_id', 'ConnID'), ('received_ns', 'ReceivedNS'),
                                  ('write_started_ns', 'WriteStartedNS'), ('written_ns', 'WrittenNS')):
            require(projection[projected] == event[actual], 'preload registration projection differs from raw history')
        received, started, written = (number(projection[k], 1) for k in
                                     ('received_ns', 'write_started_ns', 'written_ns'))
        require(received <= started <= written <= wr, 'preload registration was not written at original snapshot')
        bodies[name], events[name] = body, event
    require(events['register']['ID'] < events['register_ack']['ID']
            and events['register']['WriteStartedNS'] <= events['register_ack']['ReceivedNS'],
            'preload acknowledgement did not follow the actual registration')
    require(not any(e['Role'] == 'control' and e['Type'] == 'register'
                    and e['ID'] > events['register']['ID'] for e in prefix)
            and not any(e['ConnID'] == conn and e['Type'] == 'register_ack'
                    and e['ID'] > events['register_ack']['ID'] for e in prefix),
            'preload inventory substituted an older registration')
    registered = witness['bindings']
    require(isinstance(registered, dict) and set(registered) == set(COMPETITION_SOURCES),
            'preload registered inventory does not contain exactly sixteen fixed sources')
    exposures = bodies['register'].get('exposures')
    accepted, rejected = bodies['register_ack'].get('accepted'), bodies['register_ack'].get('rejected')
    require(isinstance(exposures, list) and len(exposures) == 16
            and isinstance(accepted, list) and len(accepted) == 16
            and all(isinstance(ident, str) for ident in accepted)
            and set(accepted) == set(COMPETITION_SOURCES) and rejected in (None, []),
            'preload registration was incomplete or rejected')
    sources = set()
    for exposure in exposures:
        require(isinstance(exposure, dict), 'malformed registered exposure')
        ident = exposure.get('exposure_id')
        require(ident in registered and ident not in sources and exposure.get('probe_support') == 'Supported',
                'preload registration changed, repeated or unsupported source')
        sources.add(ident)
        b = binding(registered[ident])
        require(set(registered[ident]) == set(BINDING_KEYS)
                and type(b[8]) is int and b[8] == 1
                and re.fullmatch(r'rv_[a-z2-7]{26}', b[4])
                and 1 <= int(b[5][3:]) <= (1 << 64)-1,
                'registered preload binding has an invalid protocol field')
        require(registered[ident] == exposure.get('binding') and b[3] == ident
                and exposure.get('revision') == b[4] and b[0] == 'lab-a'
                and b[1] == sample['ois']['epoch'] and b[2] == sample['oic']['generation'],
                'preload binding inventory differs from registration or sampled process')
    return conn, registered


def competition_preparation(full, end, history, resource_base):
    strategy = COMPETITION_PREPARATION['strategy']
    fields = ('preparation_strategy', 'preload_set_started_ns', 'preload_set_completed_ns',
              'preload_accepted_ns', 'final_set_started_ns', 'input_versions',
              'drop_arm_started_ns', 'drop_arm_completed_ns', 'drop_arm_count')
    require(full.get('preparation_strategy') == end.get('preparation_strategy') == strategy
            and all(k in full and k in end and full[k] == end[k] for k in fields),
            'six-source preparation anchors/strategy missing or different')
    full_observed, end_observed = number(full.get('observed_ns'), 1), number(end.get('observed_ns'), 1)
    require(end['prepared_ns'] <= full_observed <= end_observed
            and end['window_end_ns'] <= end_observed,
            'preparation/window evidence was recorded before its actual observations')
    versions = full['input_versions']
    require(isinstance(versions, dict) and set(versions) == set(COMPETITION_SOURCES),
            'six-source final input versions missing or have another source')
    for version in versions.values():
        number(version, 1)
    first, first_done, accepted, final = (number(full[k], 1) for k in fields[1:5])
    arm_start, arm_end = number(full['drop_arm_started_ns'], 1), number(full['drop_arm_completed_ns'], 1)
    require(type(full['drop_arm_count']) is int and full['drop_arm_count'] == 32,
            'probe-first snapshot Drop quota changed or was not fully armed once')
    require(end['prepare_started_ns'] <= first <= first_done <= accepted <= arm_start <= arm_end <= final
            <= end['slow_input_applied_ns'] <= full['sample']['started_ns'],
            'six-source Set/acceptance order changed or preparation deadline renewed')
    require('preload_witness' not in full and 'preload_witness' not in end,
            'old query preload witness mixed into probe-first preparation')
    preload = full.get('preload_queue_witness')
    require(isinstance(preload, dict) and isinstance(preload.get('sample'), dict),
            'missing preload composite sample')
    conn, registered = competition_preload_queue(preload, history, first_done, accepted, resource_base)
    dropped = [(access, e) for (access, _), e in history.events.items() if e['Dropped']]
    require(len(dropped) <= 32 and all(access == 'lab-a' and e['Type'] == 'observation_snapshot' and e['Role'] == 'control'
            and e['Direction'] == 'oic_to_ois' and e['ConnID'] == conn and not e['Injected']
            and not e['WriteFailed'] and e['ID'] > preload['wire_last_event_id'] for access, e in dropped),
            'probe-first Drop history exceeds its original native quota or preload prefix')
    final_witness = full.get('final_query_witness')
    final_conn = competition_query_witness(final_witness, full['sample'], history, 200,
                                         end['slow_input_applied_ns'], end['prepared_ns'], resource_base,
                                         preparation=True, registered=registered)
    require(conn == final_conn and preload['attempt'] < final_witness['attempt'],
            'preload/final witness replaced the control session or reused an attempt')
    before = full.get('pre_dial_query_witness')
    require(isinstance(before, dict) and isinstance(before.get('sample'), dict),
            'missing pre-Dial independent sample/witness')
    require('pre_dial_query_witness' not in end,
            'pre-Dial witness duplicated in complete record')
    accepted, before_deadline = competition_pre_dial_anchors(full, end)
    before_conn = competition_query_witness(before, before['sample'], history, 80,
        end['slow_input_applied_ns'], min(accepted, before_deadline), resource_base,
        preparation=True, embedded_sample=True, registered=registered)
    require(conn == before_conn and preload['attempt'] < before['attempt'] < final_witness['attempt'],
            'pre-Dial witness changed control session or reused an attempt')
    require(before['sample'] != full['sample']
            and before['sample_started_ns'] < final_witness['sample_started_ns']
            and accepted <= final_witness['sample_started_ns']
            and preload['wire_event_count'] <= before['wire_event_count'] <= final_witness['wire_event_count'],
            'pre-Dial/final reused original sample or reversed snapshot prefix')
    require(number(resource_base['ois']['pid'], 1) > 0
            and number(resource_base['oics']['lab-a']['pid'], 1) > 0,
            'missing actual resource process identity')
    for sample in (preload['sample'], before['sample'], full['sample']):
        competition_process_binding(sample, resource_base)
    return conn


def competition_pre_dial_anchors(full, end):
    require(not any(key.startswith('before_write_') for row in (full, end) for key in row),
            'old before-write fields mixed into pre-Dial preparation')
    fields = ('pre_dial_accepted_ns', 'pre_dial_context_deadline_ns')
    values = tuple(number(full.get(k), 1) for k in fields)
    require(all(type(end.get(k)) is int and end[k] == full[k] for k in fields),
            'pre-Dial shared anchors missing or different')
    accepted, deadline = values
    require(number(end['slow_input_applied_ns'], 1) <= accepted <= deadline
            == number(end['prepare_deadline_ns'], 1)
            and accepted <= number(full['observed_ns'], 1),
            'pre-Dial acceptance escaped original deadline/observation')
    return accepted, deadline


def competition_export_first(full, end, first):
    """Pre-Dial preparation precedes connect and GET; post-export final stays independent.

    Internal Active/header/429 evidence is still checked by competition_export_input;
    this checks the newly preregistered cross-event order, without renewing a window.
    """
    round_ = number(end['round'], 0, 2)
    require(type(first.get('cycle')) is int and first['cycle'] == 0
            and first.get('full_backlog_required') is True
            and full.get('round') == first.get('round') == round_
            and first.get('role') == ('ois' if round_ % 2 == 0 else 'oic'),
            'export-first input is from another cycle/round/role')
    writes = first['slow_writes']
    require(isinstance(writes, list) and len(writes) == 2,
            'export-first lacks the original two sockets')
    connected = [number(w['connected_ns'], 1) for w in writes]
    require([w.get('socket') for w in writes] == [0, 1], 'export-first socket identities changed')
    baseline = first['before_export']
    baseline_start, baseline_end = (number(baseline[k], 1) for k in ('started_ns', 'completed_ns'))
    rejected = number(first['actual_rejection']['completed_ns'], 1)
    final_sample = full['sample']
    final_start, final_end = (number(final_sample[k], 1) for k in ('started_ns', 'completed_ns'))
    returned = number(full['final_query_witness']['returned_ns'], 1)
    prepared = number(end['prepared_ns'], 1)
    deadline = min(number(end['prepare_deadline_ns'], 1), min(connected)+NS_SEC)
    accepted, before_deadline = competition_pre_dial_anchors(full, end)
    before = full.get('pre_dial_query_witness')
    require(isinstance(before, dict) and isinstance(before.get('sample'), dict),
            'missing pre-Dial witness before exporter input')
    slot = first['export_slot_input']
    zero, release = slot['zero'], number(slot['writers_released_ns'], 1)
    helper_start, helper_deadline = competition_helper_deadline(first, end['prepare_deadline_ns'])
    # Preparation ends before the helper exists. Its separately recorded D
    # begins once at actual entry, before either connection.
    require(number(end['slow_input_applied_ns'], 1) <= number(before['sample']['started_ns'], 1)
            <= number(before['sample']['completed_ns'], 1)
            <= number(before['returned_ns'], 1) <= number(before['callback_returned_ns'], 1)
            <= accepted <= helper_start <= min(connected) <= max(connected) <= number(zero['started_ns'], 1) <= number(zero['completed_ns'], 1)
            <= release <= helper_deadline <= deadline,
            'pre-Dial/Zero/release escaped original ctx deadline or order')
    require(before['sample'] != final_sample and before['sample']['started_ns'] < final_start,
            'pre-Dial sample reused as post-export final')
    for write in writes:
        require(release <= number(write['started_ns'], 1) <= number(write['completed_ns'], 1)
                <= helper_deadline, 'slow writer escaped original socket deadline')
    # competition_export_input independently binds rejection to the LAST
    # qualifying 429, after both raw headers and original Active2 observation.
    require(number(end['drop_arm_completed_ns'], 1) <= baseline_start <= baseline_end
            <= number(end['final_set_started_ns'], 1)
            <= number(end['slow_input_applied_ns'], 1) <= accepted <= helper_start
            <= min(connected) <= max(connected) <= release
            <= min(number(w['started_ns'], 1) for w in writes)
            <= max(number(w['completed_ns'], 1) for w in writes) <= rejected <= final_start
            <= final_end <= returned <= prepared <= deadline,
            'export-first input/final escaped original preparation and connection deadline intersection')
    critical = first['backlog_before']
    require(final_sample != critical and prepared <= number(critical['started_ns'], 1)
            <= number(critical['completed_ns'], 1)
            <= number(first['critical_query_witness']['returned_ns'], 1)
            <= number(first['native_started_ns'], 1),
            'export-first final/critical samples were reused or critical order changed')
    require(number(full['observed_ns'], 1) <= number(first['observed_ns'], 1),
            'cycle was recorded before the final preparation event')
    failed, after = first['bounded_failure_before_close'], first['after_export']
    closed_at = number(first['scrapes_closed_ns'], 1)
    require(rejected <= number(failed['started_ns'], 1) <= number(failed['completed_ns'], 1)
            <= closed_at <= number(after['started_ns'], 1) <= number(after['completed_ns'], 1)
            and len({baseline_start, failed['started_ns'], after['started_ns']}) == 3,
            'export-first baseline/failure/recovery samples were reused or reordered')


def competition_process_binding(sample, resource_base):
    require(sample['ois']['epoch'] == resource_base['ois']['stats']['epoch']
            and sample['oic']['epoch'] == resource_base['oics']['lab-a']['stats']['epoch']
            and sample['oic']['generation'] == resource_base['oics']['lab-a']['stats']['generation'],
            'competition changed baseline process epoch/generation')


def competition_critical_query(row, history, original_conn, prepared, resource_base, registered=None):
    """Only cycle zero had the original 300ms young predicate."""
    if row['full_backlog_required']:
        observed = number(row.get('observed_ns'), 1)
        require(competition_query_witness(row.get('critical_query_witness'), row['backlog_before'],
                history, 300, prepared, min(row['native_started_ns'], observed), resource_base,
                registered=registered) == original_conn,
                'critical query witness changed the original control session')
    else:
        require('critical_query_witness' in row and row['critical_query_witness'] is None,
                'non-full cycle fabricated an unevaluated critical query witness')


def competition_targets(rows, history, full, end):
    health = health_index(rows)
    targets = [r for r in rows if r['event'] == 'competition_target']
    require(set(health) == set(COMPETITION_SOURCES) and len(targets) == 16,
            'competition health input does not contain the fixed sixteen sources')
    start, finish = end['slow_input_applied_ns'], end['window_end_ns']
    test_pid = history.snapshots['lab-a']['PID']
    require(all(s['PID'] == test_pid for s in history.snapshots.values()),
            'wire histories came from different test processes')
    for r in targets:
        exposure = r['exposure_id']; version = number(r.get('version'), 1)
        slow = exposure not in COMPETITION_SOURCES[:2]
        require(r.get('preparation_strategy') == COMPETITION_PREPARATION['strategy']
                and r.get('exercise_start_ns') == start and r.get('exercise_end_ns') == finish
                and r.get('timeout_target') is slow and version == full['input_versions'][exposure],
                'target exercise window/version/slow identity differs from final input')
        snapshot = r['health']
        observed = number(r.get('observed_ns'), 1)
        require(snapshot['PID'] == test_pid and number(snapshot.get('Version'), 1) == version
                and isinstance(snapshot.get('Records'), list) and len(snapshot['Records']) <= 4096,
                'health history is incomplete or not this test process/final version')
        require(finish <= observed and all(number(record[k]) <= observed for record in snapshot['Records']
                for k in ('StartedNS', 'HeaderStartedNS', 'HeaderWrittenNS', 'CancelledNS', 'FinishedNS')),
                'health history contains future instrument times')
        for record in snapshot['Records']:
            finished = record['FinishedNS']
            require(not finished or finished >= max(record['HeaderStartedNS'], record['HeaderWrittenNS'],
                                                    record['CancelledNS']),
                    'health handler finished before its recorded header/cancellation')
        actual = [x for x in health[exposure].values() if start <= x['StartedNS'] <= finish]
        require(actual and all(x['Version'] == version and type(x.get('Status')) is int
                              and x['Status'] == 200 for x in actual),
                'target has no exercise-window request or changed final version in window')
        require(not slow or all(not x['HeaderStartedNS'] or x['HeaderStartedNS'] > finish for x in actual),
                'slow target released headers inside the exercise window')
        timeouts = sum(x['StartedNS'] < x['CancelledNS'] <= finish and x['HeaderStartedNS'] == 0 for x in actual)
        require(number(r.get('attempts'), 1) == len(actual) and number(r.get('timeouts')) == timeouts,
                'target attempts/timeouts differ from raw exercise-window records')
        require(not slow or timeouts > 0, 'slow target has no in-window no-header cancellation')


def competition_helper_deadline(row, prepare_deadline=None):
    """Bind every cycle to its actual pre-Dial helper origin, not ConnectedNS."""
    slot = row.get('export_slot_input')
    require(isinstance(slot, dict), 'missing original socket helper')
    start = number(slot.get('helper_started_ns'), 1)
    deadline = number(slot.get('helper_context_deadline_ns'), 1)
    cycle = number(row.get('cycle'), 0, 511)
    expected = start + NS_SEC
    if cycle == 0:
        expected = min(expected, number(prepare_deadline, 1))
    require(start <= deadline == expected, 'socket helper original deadline was renewed/substituted')
    writes = row.get('slow_writes')
    require(isinstance(writes, list) and len(writes) == 2, 'missing original helper sockets')
    zero = slot.get('zero')
    require(isinstance(zero, dict), 'missing original helper Zero')
    connected = [number(w.get('connected_ns'), 1) for w in writes]
    release = number(slot.get('writers_released_ns'), 1)
    require(start <= min(connected) <= max(connected) <= number(zero.get('started_ns'), 1)
            <= number(zero.get('completed_ns'), 1) <= release <= deadline,
            'socket helper/Zero/release escaped original deadline/order')
    for write in writes:
        require(release <= number(write.get('started_ns'), 1)
                <= number(write.get('completed_ns'), 1) <= deadline,
                'slow writer escaped original socket deadline')
    return start, deadline


def competition_slot_input(row, expected_pid=None, prepare_deadline=None):
    """Current non-export occupancy selects input; it is not continuous-write proof."""
    slot=row.get('export_slot_input')
    require(isinstance(slot,dict) and set(slot)=={'schema','origin_ns','deadline_ns','zero','writers_released_ns','samples',
                'helper_started_ns','helper_context_deadline_ns'}
            and slot['schema']=='g2-export-slot-input-v1','missing/malformed exporter slot input')
    competition_helper_deadline(row, prepare_deadline)
    role=row.get('role');require(role in ('ois','oic'),'unknown activity exporter role')
    writes=row['slow_writes'];require(len(writes)==2,'missing activity input sockets')
    origin=min(number(w['connected_ns'],1) for w in writes)
    deadline=origin+NS_SEC
    release=number(slot['writers_released_ns'],1)
    require(number(slot['origin_ns'],1)==origin and number(slot['deadline_ns'],1)==deadline,
            'activity reset original connect deadline')
    zero=slot['zero'];samples=slot['samples']
    require(isinstance(samples,list) and 1<=len(samples)<=128,'missing/over-capacity activity observations')
    pid=None;previous=0
    for i,x in enumerate([zero]+samples):
        require(isinstance(x,dict) and set(x)=={'started_ns','completed_ns','http_status','process'},'malformed activity HTTP observation')
        start,end=number(x['started_ns'],1),number(x['completed_ns'],1)
        require(type(x['http_status']) is int and x['http_status']==200 and origin<=start<=end<=deadline
                and end<=number(row['observed_ns'],1) and start>previous,'activity observation HTTP/order/deadline invalid')
        previous=end
        proc=x['process']
        require(isinstance(proc,dict) and set(proc)=={'pid','sampled_ns','stats'},'malformed activity process')
        actual=number(proc['pid'],1)
        if pid is None:pid=actual
        require(actual==pid and (expected_pid is None or actual==number(expected_pid,1)), 'activity belongs to another process')
        require(start<=number(proc['sampled_ns'],1)<=end,'activity capture lies outside HTTP interval')
        stats=proc['stats'];require(isinstance(stats,dict),'missing complete activity stats')
        process=stats.get('process');require(isinstance(process,dict),'missing activity process stats')
        for key in ('goroutines','open_fds','rss_bytes','heap_bytes','cpu_ms'):
            number(process.get(key),-1 if key in ('open_fds','rss_bytes','cpu_ms') else 1)
        require(process['open_fds']!=0 and process['rss_bytes']!=0,'zero process statistics fallback')
        for key in ('epoch','relay_bytes','max_buffered_bytes'):number(stats.get(key),1 if key=='epoch' else 0)
        if role=='ois':
            require('access_id' not in stats and type(stats.get('store_healthy')) is bool,'wrong/incomplete OIS activity stats')
            for key in ('tickets','accesses_online'):number(stats.get(key))
            ch=stats.get('channels');require(isinstance(ch,dict),'missing OIS channel counters')
            for key in ('preparing','open','closing','closed'):number(ch.get(key))
        else:
            require(stats.get('access_id')=='lab-a' and type(stats.get('connected')) is bool
                    and type(stats.get('registered')) is bool and isinstance(stats.get('closed'),dict),
                    'wrong/incomplete OIC activity stats')
            for key in ('generation','channels','license_remaining_ms'):number(stats.get(key),1 if key=='generation' else 0)
            for value in stats['closed'].values():number(value)
        diag=stats.get('observation');require(isinstance(diag,dict),'missing activity diagnostics')
        active=number(diag.get('exporter_active'),0,2);age=number(diag.get('exporter_youngest_active_age_ns'))
        require(active!=0 or age==0,'empty activity retained age')
        # No request had been sent at zero; a later owner cannot predate it.
        if i==0:
            require(active==age==0 and max(w['connected_ns'] for w in writes)<=start<=end<=release
                    <=min(number(w.get('started_ns'),1) for w in writes),'zero activity not before original writers')
        else:
            require(age<=end-release,'activity age predates empty exporter/writer release')
            ready=max([number(w['completed_ns'],1) for w in writes]+[number(h['completed_ns'],1) for h in row['slow_response_headers']])
            require(ready<=start,'activity selected before writer/header preparation')
            qualifies=active==2 and age>=40*NS_MS
            require(qualifies==(i==len(samples)),'activity did not stop at first current two-slot 40ms witness')
    require(samples[-1]['completed_ns']<=number(row['export_input_observations'][0]['started_ns'],1),
            'metrics observer preceded activity qualification')
    return pid


def competition_export_input(row, expected_pid=None, prepare_deadline=None):
    """Validate observed rejection persistence, not continuous write blocking.

    This is a bounded fixture qualification. The original post-critical 429
    and both exporter failures before socket close remain separate evidence.
    """
    observations=row.get('export_input_observations')
    require(isinstance(observations,list) and 5<=len(observations)<=128,
            'missing/over-capacity exporter input observations')
    writes=row['slow_writes']
    require(isinstance(writes,list) and len(writes)==2,'missing exporter input sockets')
    origin=min(number(w['connected_ns'],1) for w in writes)
    ready=max(number(w['completed_ns'],1) for w in writes)
    deadline=origin+NS_SEC
    recorded=number(row['observed_ns'],1)
    before=number(row['backlog_before']['started_ns'],1)
    headers=row.get('slow_response_headers')
    require(isinstance(headers,list) and len(headers)==2,
            'missing both original slow-socket response headers')
    first_observer=number(observations[0].get('started_ns'),1)
    for index,header in enumerate(headers):
        require(isinstance(header,dict) and set(header)=={'socket','started_ns','completed_ns',
                    'status','complete','header_raw','peeked_bytes','error'},
                'malformed slow response header observation')
        require(type(header['socket']) is int and header['socket']==index
                and type(header['status']) is int and header['status']==200
                and header['complete'] is True and header['error']=='',
                'slow socket did not have its own complete successful response')
        start,end=number(header['started_ns'],1),number(header['completed_ns'],1)
        require(ready<=start<=end<=min(deadline,first_observer,before,recorded),
                'slow response header was not observed before the third reader within original input window')
        require(isinstance(header['header_raw'],str),'missing raw slow response header')
        try:
            data=base64.b64decode(header['header_raw'],validate=True)
        except (ValueError,TypeError) as exc:
            raise ValueError('invalid base64 slow response header') from exc
        peeked=number(header['peeked_bytes'],1,8192)
        require(0<len(data)<=peeked and data.endswith(b'\r\n\r\n')
                and data.find(b'\r\n\r\n')==len(data)-4,
                'slow response header is incomplete or contains unbounded/trailing bytes')
        lines=data[:-4].split(b'\r\n')
        require(re.fullmatch(rb'HTTP/1\.[01] 200(?: [^\r\n]*)?',lines[0]) is not None,
                'raw slow socket status is not 200')
        for line in lines[1:]:
            name,separator,value=line.partition(b':')
            require(separator and re.fullmatch(rb"[!#$%&'*+.^_`|~0-9A-Za-z-]+",name)
                    and all(byte==9 or byte>=32 and byte!=127 for byte in value),
                    'malformed raw slow response header field')
    competition_slot_input(row,expected_pid,prepare_deadline)
    rejected=row['actual_rejection']
    require(type(rejected['status']) is int and rejected['status']==429,
            'accepted exporter input was not a real 429')
    first_end=None;count=0;previous=0
    for index,observation in enumerate(observations):
        require(isinstance(observation,dict) and set(observation)=={'status','started_ns','completed_ns'},
                'malformed exporter input observation')
        status=number(observation['status'])
        start,end=number(observation['started_ns'],1),number(observation['completed_ns'],1)
        require(status in (200,429),'unexpected exporter input status')
        require(origin<=ready<=start<=end<=min(deadline,before,recorded) and start>previous,
                'exporter input observation outside ordered original 1s window')
        previous=end
        if status==200:
            first_end=None;count=0
        else:
            if first_end is None:first_end=end
            count+=1
        accepted=count>=5 and start-first_end>=40*NS_MS
        require(not accepted or index==len(observations)-1,
                'exporter input continued after its first qualifying observation')
    require(accepted,'exporter input did not observe five consecutive 429s spanning 40ms')
    require(all(observations[-1][k]==rejected[k] for k in ('status','started_ns','completed_ns')),
            'accepted exporter rejection differs from final input observation')


def competition_victim_input(row,cycle,history):
    """Select a pre-Open input phase; it does not grant admission authority."""
    require(row.get('cycle')==cycle['cycle'] and type(row.get('cycle')) is int
            and row.get('token_id')==cycle['revoke_token_id']
            and row.get('channel_id')==cycle['victim_channel_id']
            and isinstance(row.get('channel_id'),str) and row['channel_id']
            and row.get('code')=='' and 'input_error' not in row,
            'victim input outcome/identity differs from its single successful Dial')
    names=('preparation_started_ns','deadline_ns','lookup_started_ns','lookup_completed_ns',
           'selected_ns','dial_started_ns','dial_completed_ns','observed_ns')
    start,deadline,ls,le,selected,ds,de,recorded=(number(row.get(k),1) for k in names)
    number(row.get('lookup_attempts'),1,512)
    require(deadline-start==3*NS_SEC and start<=ls<=le<=selected<=ds<=de<=deadline
            and selected<deadline and de<=recorded<=min(w['connected_ns'] for w in cycle['slow_writes']),
            'victim selection/Dial escaped its original 3s or preceded/crossed cycle input')
    view=row.get('view');require(isinstance(view,dict),'missing selected victim directory view')
    rv=view.get('readiness_view');require(isinstance(rv,dict),'missing current victim readiness')
    require(view.get('exposure_id')=='lab-a/http' and view.get('access_id')=='lab-a'
            and view.get('access_online') is True and view.get('readiness')=='ready'
            and rv.get('authority')=='directory_snapshot' and rv.get('source')=='http_status_v1'
            and rv.get('reason')=='matched_ready_status' and rv.get('revision')==view.get('revision'),
            'victim input is not a current Ready HTTP directory sample')
    number(rv.get('sample_seq'),1,(1<<64)-1)
    remaining=number(rv.get('remaining_ms'),1,((1<<63)-1)//NS_MS)
    conservative=max(0,(remaining-1)*NS_MS-(selected-ls))
    require(number(row.get('conservative_remaining_ns'))==conservative and conservative>=500*NS_MS,
            'victim input did not retain conservative 500ms after floor and complete Lookup RTT')
    checks=[(e,history.bodies[k]) for k,e in history.events.items()
            if k[0]=='lab-a' and e['Type']=='admission_check' and e['Stage']=='preflight'
            and not e['Injected'] and ds<=e['ReceivedNS']<=de]
    finals=[(e,history.bodies[k]) for k,e in history.events.items()
            if k[0]=='lab-a' and e['Type']=='admission_check' and e['Stage']=='final'
            and not e['Injected'] and e['ChannelID']==row['channel_id']]
    require(len(checks)==len(finals)==1,'victim Dial was missing or repeated actual preflight/final')
    first,pre=checks[0];last,final=finals[0]
    b=binding(pre.get('binding'))
    require(binding(final.get('binding'))==b and first['ConnID']==last['ConnID']
            and first['WrittenNS']>0 and last['WrittenNS']>0
            and first['ReceivedNS']<=last['ReceivedNS']<=de
            and pre.get('channel_id','')=='' and pre.get('effective_admission')==final.get('effective_admission')=='best_effort',
            'victim preflight/final binding, order or admission changed')
    require(b[0]=='lab-a' and b[3]=='lab-a/http' and b[4]==rv.get('revision')
            and b[2]==number(rv.get('generation'),1) and b[5]==rv.get('observation_revision')
            and b[6]==rv.get('contract_digest'),'selected victim binding differs from real checks')
    sources=[]
    for key,event in history.events.items():
        if (key[0]!='lab-a' or event['Type']!='observation_snapshot' or event['Injected']
                or event['Dropped'] or event['WrittenNS']==0 or event['WriteStartedNS']>le):
            continue
        snapshot=history.bodies[key]
        if (snapshot.get('readiness')!='ready' or snapshot.get('sample_seq')!=rv['sample_seq']
                or binding(snapshot.get('binding'))!=b):
            continue
        qkey=history.queries.get(('lab-a',event['ConnID'],event['QueryID']))
        query=history.events[qkey]
        # OIS expires no later than its query-send time plus the source's
        # encoded remaining lifetime. Proxy receipt is a conservative upper
        # bound on that send, while Lookup start precedes view rendering.
        upper=query['ReceivedNS']+number(snapshot.get('remaining_ms'),1)*NS_MS
        if query['WrittenNS']>0 and ls+remaining*NS_MS<=upper:
            sources.append(event)
    require(sources,'victim directory sample/lifetime has no matching real delivered source snapshot')
    for q,stage in ((first,'preflight'),(last,'final')):
        replies=[e for k,e in history.events.items() if k[0]=='lab-a' and e['Type']=='admission_result'
                 and e['ConnID']==q['ConnID'] and e['CheckID']==q['CheckID'] and not e['Injected']]
        require(len(replies)==1,'victim check response missing/repeated')
        event,body,_=history.result(replies[0],stage)
        require(event['WrittenNS']>0 and q['ReceivedNS']<=event['ReceivedNS']<=de
                and not body.get('code'),'victim check was refused or not actually delivered')
    return first['CheckID'],last['CheckID']


def validate_competition(rows,history,resource_base):
    end=next(r for r in rows if r['event']=='competition_complete')
    full=next(r for r in rows if r['event']=='competition_full')
    start,finish=competition_window(full,end)
    require((end['targets'],end['timeout_targets'])==(16,14),'incorrect competition load')
    cycles=[r for r in rows if r['event']=='competition_cycle']
    require(len(cycles)==end['cycles']>=20 and [r['cycle'] for r in cycles]==list(range(len(cycles))),'competition cycles missing/reordered')
    victims=[r for r in rows if r['event']=='competition_victim_input']
    require(len(victims)==len(cycles) and [r.get('cycle') for r in victims]==list(range(len(cycles))),
            'victim input records missing/reordered/repeated')
    require(full['window_start_ns']==start and full['snapshot_drop_cap']==32,'full backlog changed input/window')
    competition_sample(full['sample'],True)
    query_conn = competition_preparation(full, end, history, resource_base)
    competition_export_first(full, end, cycles[0])
    dropped=[e for e in history.events.values() if e['Type']=='observation_snapshot' and e['Dropped'] and start<=e['ReceivedNS']<=finish]
    require(0<len(dropped)<=32,'actual bounded snapshot Drop input missing/over capacity')
    channels=set();tokens=set();victim_checks=set()
    for r in cycles:
        require(r['round']==end['round'] and r['full_backlog_required'] is (r['cycle']==0),'wrong full-window classification')
        require(r['role']==('ois' if (end['round'] if r['cycle']==0 else r['cycle'])%2==0 else 'oic'),'exporter role did not alternate')
        before,after=r['backlog_before'],r['backlog_after']
        competition_sample(before,r['full_backlog_required']);competition_sample(after,r['full_backlog_required'])
        competition_process_binding(before, resource_base);competition_process_binding(after, resource_base)
        slow_scrapes(r['slow_writes'],r['before_export'],r['actual_rejection'],r['bounded_failure_before_close'],r['after_export'],r['scrapes_closed_ns'])
        expected_pid=resource_base["ois"]["pid"] if r["role"]=="ois" else resource_base["oics"]["lab-a"]["pid"]
        competition_export_input(r,expected_pid,end['prepare_deadline_ns'])
        for observation in [r['export_slot_input']['zero']]+r['export_slot_input']['samples']:
            stats=observation['process']['stats'];reference=before[r['role']]
            require(stats['epoch']==reference['epoch'],'activity epoch differs from critical process')
            if r['role']=='oic':require(stats['generation']==reference['generation'],'activity generation differs from critical process')
        check_ids=competition_victim_input(victims[r['cycle']],r,history)
        require(not victim_checks.intersection(check_ids),'victim check identity reused across cycles')
        victim_checks.update(check_ids)
        require(r['actual_rejection']['completed_ns']<=before['started_ns'] and all(w['completed_ns']<=r['actual_rejection']['completed_ns'] for w in r['slow_writes']),'slow exporter input was not established before critical work')
        competition_cycle_window(r,end['prepared_ns'],finish)
        competition_critical_query(r, history, query_conn, end['prepared_ns'], resource_base,
                                   registered=full['preload_queue_witness']['bindings'])
        require(r['native_response']=='T1:competition-normal','native request failed under competition')
        native_exchange(history,r['normal_channel_id'],'competition-normal','T1:competition-normal')
        competition_revocation(r,history)
        targets([r['target']]);require(data(r['target']['Data'])==('competition-victim-'+str(r['cycle'])).encode(),'revoked target received forbidden extra bytes')
        for cid in (r['normal_channel_id'],r['victim_channel_id']):
            require(cid not in channels,'Channel reused across competition cycles');channels.add(cid)
        require(r['revoke_token_id'] not in tokens,'revocation token reused');tokens.add(r['revoke_token_id'])
        if r['full_backlog_required']:
            parse_exposition(r['post_critical_rejection'],429)
            require(r['post_critical_rejection']['started_ns']>=after['completed_ns'],'full competition input ended before critical observation')
    drain=next(r for r in rows if r['event']=='competition_drain')
    first,_=history.ref(drain['first_forwarded_snapshot'],typ='observation_snapshot')
    require(first['WrittenNS']>0 and first['ReceivedNS']>cycles[0]['backlog_after']['completed_ns'],'telemetry did not recover after full burst')
    beats=[r for r in rows if r['event']=='competition_heartbeats']
    require({r['access_id'] for r in beats}=={'lab-a','lab-b'},'missing heartbeat role')
    for r in beats:
        access=r['access_id'];seen=set()
        expected={e['ID'] for (a,_),e in history.events.items() if a==access and e['Type']=='heartbeat' and not e['Injected'] and start<=e['ReceivedNS']<=finish}
        for pair in r['pairs']:
            beat,b=history.ref(pair['heartbeat'],access,typ='heartbeat');ack,a=history.ref(pair['ack'],access,typ='heartbeat_ack')
            require(beat['ID'] not in seen and beat['ConnID']==ack['ConnID'] and b['seq']==a['ack_seq'],'heartbeat ACK identity duplicated/mismatched')
            seen.add(beat['ID'])
            require(beat['WrittenNS']>0 and ack['WrittenNS']>0 and 0<=ack['WrittenNS']-beat['ReceivedNS']<=500*NS_MS and pair['elapsed_ns']==ack['WrittenNS']-beat['ReceivedNS'],'heartbeat budget not independently recomputed')
            require(b['license_ttl_ms']==3000,'heartbeat altered original lease budget')
        require(seen==expected and len(seen)>=20,'omitted actual heartbeat from measurement window')
    competition_targets(rows, history, full, end)
    requested=next(r for r in rows if r['event']=='competition_cleanup_requested')
    require(requested['window_end_ns']==finish and requested['requested_ns']>=finish,'cleanup request not bound to final load boundary')


def cancelled(out):
    require(out.get('status')==0 and 'context canceled' in out.get('error','') and not out.get('open',{}).get('channel_id'),'HTTP cancellation not actually observed')
    require(number(out['StartedNS'],1)<=number(out['CompletedNS'],1),'HTTP timing reversed')


def validate_pending(rows,history):
    channels=set();operations=set();requests=set()
    for r in rows:
        event=r['event']
        if event not in ('pending_merge','last_cancel_and_late_result','cancel_after_creation','failed_key_retry'):
            continue
        req=r['request'];key=req['client_request_id']
        require(key not in requests and req['exposure_id']=='lab-a/http' and req['expected_revision'],'idempotent input key reused or incomplete')
        requests.add(key)
        slots(r['after'],checks=0)
        ids=r.get('check_ids',[])
        if event!='failed_key_retry':
            require(len(ids)==(2 if event=='last_cancel_and_late_result' else 1),'operation did not use exactly one new check')
            for ident in ids:
                require(ident not in operations and any(k[2]==ident for k in history.checks),'preflight identity reused/invented');operations.add(ident)
        if event=='pending_merge':
            held,result,check=history.result(r['held'],'preflight')
            require(ids==[held['CheckID']] and held['WrittenNS']>0,'merged operation challenge differs')
            slots(r['full'],1,16,1);slots(r['after_creator_cancel'],1,15,1)
            http_outcome(r['overflow'],429,'ResourceExhausted');http_outcome(r['conflict'],409,'IdempotencyConflict');cancelled(r['creator'])
            waiters=r['waiters'];require(len(waiters)==15,'missing merged waiter outcomes')
            opened=[http_outcome(v,200) for v in waiters];channel=opened[0]['channel_id'];ticket=opened[0]['attach']['ticket']
            require(channel and ticket and all(v['channel_id']==channel and v['attach']['ticket']==ticket for v in opened),'merged waiters changed Channel/ticket')
            require(http_outcome(r['replay'],200)['channel_id']==channel,'created replay changed identity')
            foreign=r['foreign_attach']
            require(foreign['credential']=='other_alice' and foreign['channel_id']==channel and foreign['status']==401 and foreign['code']=='TicketInvalid' and foreign['started_ns']<=foreign['completed_ns'],'foreign credential attached creator ticket')
            require(r['creator']['CompletedNS']<=held['WriteStartedNS'],'creator cancellation did not precede shared result')
            require(all(v['CompletedNS']<=check['ReceivedNS']+2*NS_SEC for v in waiters),'merged waiters renewed total prepare deadline')
        elif event=='last_cancel_and_late_result':
            old,_,_=history.result(r['old_result'],'preflight');new,_,_=history.result(r['next_result'],'preflight')
            check,_=history.ref(r['next_check'],typ='admission_check')
            require(ids==[old['CheckID'],new['CheckID']] and check['CheckID']==new['CheckID'] and old['CheckID']!=new['CheckID'],'replacement reused old check identity')
            cancelled(r['cancelled']);slots(r['released'],checks=0);slots(r['replacement_pending'],1,1,1)
            require(check['ReceivedNS']<=old['WriteStartedNS']<=old['WrittenNS']<=new['ReceivedNS'] and new['WrittenNS']>0,'old callback did not actually arrive during replacement')
            channel=http_outcome(r['replacement'],201)['channel_id']
        elif event=='cancel_after_creation':
            prepare,_=history.ref(r['prepare'],typ='prepare_channel')
            require(prepare['ReceivedNS']<=r['cancelled_ns'],'cancel did not follow actual Channel creation')
            channel=prepare['ChannelID'];created=r['created']
            if created['status']==0:cancelled(created)
            else:require(http_outcome(created,201)['channel_id']==channel,'creation outcome changed Channel')
            replay=http_outcome(r['replay'],200);retained=http_outcome(r['retained'],200)
            require(replay['channel_id']==retained['channel_id']==channel and replay['attach']['ticket']==retained['attach']['ticket'],'post-create cancellation changed retained ticket')
            if created['status']==201:require(created['open']['attach']['ticket']==replay['attach']['ticket'],'creator ticket replaced')
        else:
            http_outcome(r['failed'],503,'TargetNotReady')
            channel=http_outcome(r['retried'],201)['channel_id']
        require(channel and channel not in channels,'different operations reused Channel');channels.add(channel)


LEGACY_SHA='570b573f8151466fd0ad42b4cdf94ac3c81d323f'
LEGACY_ARCHIVE_SHA256='cdd2219ce66e13310a74fa31098f8614e8240cda41e1198a38fce5d8516a644e'
# SHA256 of canonical sorted [{path,sha256}] from that Git object's 93 files.
LEGACY_FILES_SHA256='70acb558e41ed29f864fe28d752fb25b6b09f1ab0e37f4597d27ba45df6f81dd'


def legacy_source(r,scenario):
    v=r['legacy']
    require(r['combination']==scenario and v['source_sha']==LEGACY_SHA and v['archive_sha256']==LEGACY_ARCHIVE_SHA256 and v['actual_build'] is True,'legacy source/build not frozen real version')
    files=v['source_files']
    digest=hashlib.sha256(json.dumps(files,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    require(len(files)==93 and digest==LEGACY_FILES_SHA256,'legacy source inventory differs from exact Git object')
    require(set(v['binaries'])=={'oi','ois','oic'},'missing actual old executable')
    for name,b in v['binaries'].items():
        require(re.fullmatch('[0-9a-f]{64}',b['sha256']) and number(b['bytes'],1)>0,'missing binary hash/size')
        info=b['go_version_m']
        require('\tpath\tgithub.com/wyw-ai/loom-oi/cmd/'+name+'\n' in info and '\tbuild\tGOOS='+v['goos']+'\n' in info and '\tbuild\tGOARCH='+v['goarch']+'\n' in info,'binary metadata does not bind actual command/platform')


def cli_result(v,success=False):
    require(v['TimedOut'] is False and type(v['ExitCode']) is int and (v['ExitCode']==0)==success and number(v['StartedNS'],1)<=number(v['FinishedNS'],1),'CLI failed to terminate with correct native outcome')
    if not success:require(v['Stdout']=='' and bool(v['Stderr']),'failed CLI returned success payload/no diagnostic')


def validate_compatibility(rows,history,group):
    for r in rows:
        e=r['event']
        if e=='compatibility_binary_sources':
            legacy_source(r,group['scenario'])
        elif e=='compatibility_unknown_strict':
            require(r['status']==412 and r['code']=='ReadinessRequired','legacy Unknown bypassed strict admission')
            cli_result(r['cli']);require('ReadinessRequired' in r['cli']['Stderr'],'legacy strict CLI lost diagnostic')
        elif e=='compatibility_native_result':
            require(r['combination']==group['scenario'] and r['directory']['readiness']=='unknown','legacy directory falsely ready')
            channel=closed(r['new_client_channel'],'Completed')
            require(r['new_response']=='T1:'+r['new_request'],'new client broke P1 payload')
            native_exchange(history,channel,r['new_request'],r['new_response'])
            cli_result(r['old_cli'],True);require(r['old_cli']['Stdout']=='T1:old-cli-native','old client native payload failed')
            require(all(c['Subprotocol']=='oi.control.v1' for c in history.connections.values() if c['Role']=='control'),'legacy control silently negotiated another version')
            require(not any(e['Type'] in ('admission_check','observation_query') for e in history.events.values()),'v2 checks sent to legacy peer')
        elif e=='compatibility_version_rejected':
            snap=r['proxy'];require(snap['Overflow'] is False and not snap['Failure'] and snap['PID']>0,'refusal instrument incomplete')
            conns=snap['Connections'];require(len(conns)>=2 and len({x['ID'] for x in conns})==len(conns),'missing repeated real old-server handshakes')
            for c in conns:
                require(c['UpstreamStatus']==400 and not c['Subprotocol'] and json_object(data(c['HandshakeBody'],False))['code']=='VersionUnsupported','v2 handshake downgraded or wrong rejection')
            require(not r['oic']['connected'] and not r['oic']['registered'] and r['oic']['generation']==0 and r['ois']['accesses_online']==0,'refused peer acquired active session')
            require(all(r['ois']['channels'][k]==0 for k in ('preparing','open','closing')) and r['ois']['tickets']==0 and 'VersionUnsupported' in r['oic_log'],'refusal created state/lost explicit diagnostic')
            require(not snap['Events'],'rejected handshake still exchanged control messages')
        elif e=='compatibility_old_cli_native':
            cli_result(r['cli'],True);require(r['cli']['Stdout']=='T1:old-cli-new-v2-native','old CLI cannot use current endpoint')
        elif e=='compatibility_old_cli_abort_round':
            final,result,check=history.result(r['final']);channel=closed(r['channel'])
            abort,frame=history.ref(r['wire_abort'],typ='ABORT');value=0x0107 if group['scenario']=='new_new_old_cli_ready_expired' else 0x0205
            require(r['combination']==group['scenario'] and result['channel_id']==channel and abort['ChannelID']==channel and frame['value']==r['raw_reason_value']==value,'new ABORT not bound to old CLI Channel')
            require(r['channel']['close_reason']==ABORT_CODES[value] and r['channel']['readiness_decision']['reason']==('expired' if value==0x0107 else 'timeout'),'GET lost precise new terminal reason')
            cli_result(r['cli']);targets(r['target'],zero=True)
            require(len(r['target'])==1 and not history.ready(channel),'old CLI replayed or committed invalid Channel')
            prepares=[e for e in history.events.values() if e['Type']=='prepare_channel' and e['ChannelID']==channel and not e['Injected']]
            require(len(prepares)==r['prepare_count']==1,'old CLI duplicated prepare')
            require(not result.get('code') and result.get('decision_id') and final['WrittenNS']>0,'old CLI abort fixture lacked actual frozen positive')
            if value==0x0107:
                require(final['WriteStartedNS']-check['ReceivedNS']>=result['remaining_ms']*NS_MS,'expired old CLI input still had lifetime')
        elif e=='compatibility_v2_invalid_reply':
            original,body,_=history.result(r['original'])
            _,bad=injected(history,r['injected'],original['ConnID'],after=original['ReceivedNS'])
            expected=copy.deepcopy(body)
            if r['scenario']=='missing_binding':expected.pop('binding')
            elif r['scenario']=='unknown_enum':expected['readiness']='fabricated_ready'
            else:expected['binding'].update(access_id='lab-b',exposure_id='lab-b/http')
            require(bad==expected and original['Dropped'],'invalid input not exactly applied')
            require(r['channel']['close_reason'] in ('ProtocolError','AccessOffline','ChannelInterrupted'),'invalid v2 response was accepted')
            abort_outcome(r,r['channel']['close_reason'],history)
            require(len(r['target'])==1 and history.connections[('lab-a',original['ConnID'])]['Closed'],'invalid v2 created extra target or retained session')
        elif e=='compatibility_contract_entry_rejected':
            original,reg=history.ref(r['original'],typ='register');_,bad=injected(history,r['injected'],original['ConnID'],after=original['ReceivedNS'])
            expected=copy.deepcopy(reg)
            for exposure in expected['exposures']:
                if exposure['exposure_id']=='lab-a/http':exposure['binding']['contract_version']=2
            require(bad==expected and original['Dropped'],'unknown contract input changed unrelated fields')
            ack,body=history.ref(r['ack'],typ='register_ack')
            require(ack['ConnID']==original['ConnID'] and body['accepted']==['lab-a/alt'] and len(body['rejected'])==1 and body['rejected'][0]['exposure_id']=='lab-a/http' and body['rejected'][0]['code']=='VersionUnsupported','unsupported entry rejected whole session or accepted bad entry')
            require(not r['target_bad'],'unsupported entry dialed business target')
            targets(r['target_good']);require(any(data(x['Data'])==b'supported-independent-entry' for x in r['target_good']),'independent supported entry did not serve native request')
        elif e=='compatibility_late_check':
            original,result,_=history.result(r['old_result']);slot,snapshot=history.ref(r['injection_slot'],typ='observation_snapshot')
            matches=[e for k,e in history.events.items() if e['Injected'] and e['CheckID']==original['CheckID'] and history.bodies[k].get('seq')==snapshot['seq'] and e['WrittenNS']>0]
            require(len(matches)==1 and slot['Dropped'],'old check not actually injected')
            old=closed(r['old_channel'],'Completed');new=closed(r['new_channel'],'Completed')
            require(old!=new and result['channel_id']==old and r['oic']['generation']==result['binding']['generation'],'old check revived/rebound session')
            native_exchange(history,old,'closed-before-late-check','T1:closed-before-late-check')
            native_exchange(history,new,'independent-after-late-check','T1:independent-after-late-check')


IMPLEMENTED = {'G2-01','G2-02','G2-03','G2-04','G2-05','G2-06','G2-07','G2-08a','G2-08b','G2-08c','G2-08d','G2-09','G2-10','G2-11','G2-12','G2-13','G2-14'}

ROUND_WIRE_IDENTITY = {
    'query_ttl_bound':('query','QueryID'), 'observed_at_skew':('held','QueryID'),
    'late_query_discarded':('late_response','QueryID'),
    'final_recheck_round':('final','CheckID'), 'commit_snapshot_round':('final','CheckID'),
    'frozen_ready_expiry_round':('final','CheckID'), 'security_final_round':('final','CheckID'),
    'final_loss_round':('final','CheckID'), 'duplicate_closed_positive_round':('final','CheckID'),
    'observation_cancel_before_ready_round':('final','CheckID'),
    'running_probe_reload_round':('new_final','CheckID'),
    'compatibility_old_cli_abort_round':('final','CheckID'),
    'binding_rejection':('held','QueryID'), 'sequence_ignored_or_idempotent':('held','QueryID'),
    'pending_capacity_round':('held','CheckID'), 'cli_response_drop_round':('held','CheckID'),
    'pending_merge':('held','CheckID'), 'last_cancel_and_late_result':('old_result','CheckID'),
    'cancel_after_creation':('prepare','ChannelID'),
}
SCENARIO_EVENTS = {'query_ttl_bound','final_recheck_round','security_final_round','final_loss_round',
                   'binding_rejection','sequence_ignored_or_idempotent','compatibility_v2_invalid_reply'}


def validate_rounds(group, rows):
    for event, expected in group['events'].items():
        selected=[r for r in rows if r['event']==event]
        if expected==20 and event!='commit_cleanup':
            require(len(selected)==20 and all(type(r.get('round')) is int for r in selected)
                    and sorted(r['round'] for r in selected)==list(range(20)), 'duplicate/missing round identity')
        if event in SCENARIO_EVENTS:
            require(all(r.get('scenario')==group['scenario'] for r in selected), 'event scenario differs from registered leaf')
        if isinstance(group.get('scenario'),str) and group['scenario'].startswith('round_') and event not in ('wire_history','health_history'):
            require(all(type(r.get('round')) is int and r['round']==int(group['scenario'][6:]) for r in selected),'load window belongs to another registered repeat')
        if event in ROUND_WIRE_IDENTITY:
            field,ident=ROUND_WIRE_IDENTITY[event]
            identities=[r.get(field,{}).get(ident) for r in selected]
            require(all(isinstance(v,str) and v for v in identities) and len(set(identities))==len(identities), 'one wire trajectory reused across rounds')
        if selected and 'channel' in selected[0] and event!='commit_cleanup':
            identities=[r.get('channel',{}).get('channel_id') for r in selected]
            require(all(isinstance(v,str) and v for v in identities) and len(set(identities))==len(identities), 'one Channel trajectory reused across rounds')


def expected_spec():
    return copy.deepcopy(EXPECTED)


def analyze_g2(spec, records, results, resource_result=None, repeats=1):
    """Return (small summary, problems). Unknown/incomplete groups never pass."""
    require(spec == EXPECTED and type(spec['competition_preparation'].get('pre_dial_query_max_age_ms')) is int
            and type(repeats) is int and repeats == 1, 'G2 preregistration differs from fixed versioned inventory')
    groups, problems, indexed = EXPECTED['groups'], [], defaultdict(list)
    resource_rows = defaultdict(list)
    for package, event_test, obs in records:
        try:
            require(package == EXPECTED['package'] and isinstance(obs,dict) and obs.get('test') == event_test, 'OBS package/test provenance mismatch')
            require(event_test in groups, 'unregistered raw test')
            require(obs.get('backend') == groups[event_test]['backend'], 'OBS backend differs from registered owner')
            if obs.get('schema') == 'b4-resource-v2':
                require(groups[event_test]['resource'], 'compatibility-only case claims FD proof')
                resource_rows[event_test].append(obs)
                continue
            require(obs.get('schema') == 'g2-behavior-v1' and obs.get('kind') == 'G2' and obs.get('vector') == groups[event_test]['vector'], 'G2 schema/vector mismatch')
            number(obs.get('observed_ns'),1)
            require(isinstance(obs.get('event'),str) and obs['event'], 'missing event name')
            indexed[event_test].append(obs)
        except (ValueError,TypeError,KeyError) as exc:
            problems.append(str(event_test)+': '+str(exc))
    summaries = {}
    required_resources = {t:1 for t,g in groups.items() if g['resource']}
    for test, group in groups.items():
        local = []
        try:
            for name in set(group['leaves']+[group['root'],test]):
                require(results.get((EXPECTED['package'],name)) == ['pass'], 'required Go result missing/skipped/repeated: '+name)
            rows = indexed[test]
            require(rows, 'missing original behavior records')
            counts = Counter(r['event'] for r in rows)
            allowed=set(group['events'])|{'environment','directory_observed','local_metric_advanced'}
            require(set(counts)<=allowed,'unregistered/failure event present: '+str(sorted(set(counts)-allowed)))
            for event, expected in group['events'].items():
                actual = counts[event]
                if type(expected) is int:
                    require(actual == expected, 'event count '+event+': '+str(actual)+' != '+str(expected))
                else:
                    require(expected['min'] <= actual <= expected.get('max', 10000), 'variable event count outside registered bounds')
                    name, field = expected['exact_from'].split('.')
                    complete = [r for r in rows if r['event'] == name]
                    require(len(complete) == 1 and type(complete[0].get(field)) is int and complete[0][field] == actual, 'declared event count differs from raw records')
            validate_rounds(group,rows)
            histories = [r for r in rows if r['event'] == 'wire_history']
            history = History(histories)
            health_index(rows)
            for row in rows:
                if row['event'] == 'commit_cleanup':
                    cleanup(row)
            if group['resource']:
                rr = resource_rows[test]
                base = [r for r in rr if r.get('kind') == 'resources_baseline']
                begin = [r for r in rr if r.get('kind') == 'resources_begin']
                done = [r for r in rr if r.get('kind') == 'resources']
                require(len(base) == len(begin) == len(done) == 1, 'missing/replaced resource profile or check')
                require(set(base[0].get('oics',{})) == set(group['oics']), 'resource role topology differs from registration')
                require(base[0]['recorded_ns'] < begin[0]['started_ns'] and begin[0]['deadline_ns']-begin[0]['started_ns'] == 2*NS_SEC, 'baseline/budget not original')
                cleanup_events=[r for r in rows if r['event']=='commit_cleanup']
                if cleanup_events and group['scenario']!='late_check':
                    require(begin[0]['started_ns'] <= max(r['origin_ns'] for r in cleanup_events), 'resource budget restarted after logical cleanup')
                if group['scenario']=='late_check':
                    # This case completes a second native operation after the old
                    # Channel's cleanup. Bind B4 to that operation, not the old one.
                    late=[r for r in rows if r['event']=='compatibility_late_check']
                    require(len(late)==1,'late-check final operation missing')
                    final=late[0];cid=final['new_channel']['channel_id']
                    fins=[e for e in history.events.values() if e['ChannelID']==cid and e['Type']=='FIN' and not e['Injected']]
                    require(len(fins)==2 and max(e['ReceivedNS'] for e in fins)<=begin[0]['started_ns']<=final['observed_ns'],'resource budget not bound to the final native operation')
                require(resource_result is not None and resource_result.get('result') == 'pass' and resource_result.get('completed_checks',{}).get(test) == 1, 'common raw Linux resource analysis absent/failed')
            vector=group['vector'].removeprefix('TV-')
            if vector == 'G2-01':
                validate_matrix(rows,history)
            elif vector == 'G2-02':
                validate_attempts(rows)
            elif vector == 'G2-03':
                validate_sequence(rows,history)
            elif vector == 'G2-04':
                validate_ttl(rows,history)
            elif vector in ('G2-05','G2-06'):
                validate_commit(rows,history)
            elif vector == 'G2-07':
                validate_security(rows,history)
            elif vector in ('G2-08a','G2-08b','G2-08c'):
                validate_lifecycle(rows,history)
            elif vector == 'G2-08d':
                validate_pending(rows,history)
            elif vector == 'G2-09':
                validate_publication(rows)
            elif vector == 'G2-10':
                validate_saturation(rows)
            elif vector == 'G2-11':
                validate_competition(rows,history,base[0])
            elif vector == 'G2-12':
                validate_export(rows)
            elif vector == 'G2-13':
                validate_compatibility(rows,history,group)
            elif vector == 'G2-14':
                validate_layers(rows)
            require(vector in IMPLEMENTED, 'semantic group not implemented: '+vector)
        except (ValueError,TypeError,KeyError,IndexError,UnicodeError,InvalidOperation) as exc:
            local.append(str(exc))
        summaries[test]={'result':'fail' if local else 'pass','records':len(indexed[test]),'problems':local}
        problems.extend(test+': '+v for v in local)
    return {'schema':EXPECTED['schema'],'result':'fail' if problems else 'pass',
            'coverage_scope':'ordinary_130_leaves_excludes_real_host_clock',
            'groups':summaries,'required_leaves':sum(len(g['leaves']) for g in groups.values()),
            'resource_contract':{'required_tests':required_resources,
                'expected_oics':{t:g['oics'] for t,g in groups.items() if g['resource']},
                'expected_backends':{t:g['backend'] for t,g in groups.items() if g['resource']}},
            'unimplemented':sorted(set(g['vector'].removeprefix('TV-') for g in groups.values())-IMPLEMENTED)}, problems

EXPECTED = {'competition_preparation': COMPETITION_PREPARATION,
 'groups': {'TestTVG2_01_AdmissionMatrix/postgres': {'backend': 'postgres',
                                                     'events': {'admission_matrix': 12,
                                                                'health_history': 2,
                                                                'matrix_business_result': 5,
                                                                'wire_history': 2},
                                                     'leaves': ['TestTVG2_01_AdmissionMatrix/postgres/ready/lab-a/http/best_effort',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/ready/lab-a/http/require_ready',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/ready/lab-a/alt/best_effort',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/ready/lab-a/alt/require_ready',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/not_ready/lab-a/http/best_effort',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/not_ready/lab-a/http/require_ready',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/not_ready/lab-a/alt/best_effort',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/not_ready/lab-a/alt/require_ready',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/unknown/lab-a/http/best_effort',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/unknown/lab-a/http/require_ready',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/unknown/lab-a/alt/best_effort',
                                                                'TestTVG2_01_AdmissionMatrix/postgres/unknown/lab-a/alt/require_ready'],
                                                     'oics': ['lab-a', 'lab-b'],
                                                     'resource': True,
                                                     'root': 'TestTVG2_01_AdmissionMatrix',
                                                     'rounds': 1,
                                                     'scenario': None,
                                                     'vector': 'G2-01'},
            'TestTVG2_01_AdmissionMatrix/sqlite': {'backend': 'sqlite',
                                                   'events': {'admission_matrix': 12,
                                                              'health_history': 2,
                                                              'matrix_business_result': 5,
                                                              'wire_history': 2},
                                                   'leaves': ['TestTVG2_01_AdmissionMatrix/sqlite/ready/lab-a/http/best_effort',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/ready/lab-a/http/require_ready',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/ready/lab-a/alt/best_effort',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/ready/lab-a/alt/require_ready',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/not_ready/lab-a/http/best_effort',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/not_ready/lab-a/http/require_ready',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/not_ready/lab-a/alt/best_effort',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/not_ready/lab-a/alt/require_ready',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/unknown/lab-a/http/best_effort',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/unknown/lab-a/http/require_ready',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/unknown/lab-a/alt/best_effort',
                                                              'TestTVG2_01_AdmissionMatrix/sqlite/unknown/lab-a/alt/require_ready'],
                                                   'oics': ['lab-a', 'lab-b'],
                                                   'resource': True,
                                                   'root': 'TestTVG2_01_AdmissionMatrix',
                                                   'rounds': 1,
                                                   'scenario': None,
                                                   'vector': 'G2-01'},
            'TestTVG2_02_AttemptDoesNotReplaceSample/postgres': {'backend': 'postgres',
                                                                 'events': {'health_history': 1,
                                                                            'sample_expired': 2,
                                                                            'sample_retained': 4},
                                                                 'leaves': ['TestTVG2_02_AttemptDoesNotReplaceSample/postgres'],
                                                                 'oics': ['lab-a', 'lab-b'],
                                                                 'resource': True,
                                                                 'root': 'TestTVG2_02_AttemptDoesNotReplaceSample',
                                                                 'rounds': 2,
                                                                 'scenario': None,
                                                                 'vector': 'G2-02'},
            'TestTVG2_02_AttemptDoesNotReplaceSample/sqlite': {'backend': 'sqlite',
                                                               'events': {'health_history': 1,
                                                                          'sample_expired': 2,
                                                                          'sample_retained': 4},
                                                               'leaves': ['TestTVG2_02_AttemptDoesNotReplaceSample/sqlite'],
                                                               'oics': ['lab-a', 'lab-b'],
                                                               'resource': True,
                                                               'root': 'TestTVG2_02_AttemptDoesNotReplaceSample',
                                                               'rounds': 2,
                                                               'scenario': None,
                                                               'vector': 'G2-02'},
            'TestTVG2_03_SequenceAndBinding/duplicate/postgres': {'backend': 'postgres',
                                                                  'events': {'health_history': 1,
                                                                             'sequence_ignored_or_idempotent': 20,
                                                                             'wire_history': 2},
                                                                  'leaves': ['TestTVG2_03_SequenceAndBinding/duplicate/postgres'],
                                                                  'oics': ['lab-a', 'lab-b'],
                                                                  'resource': True,
                                                                  'root': 'TestTVG2_03_SequenceAndBinding',
                                                                  'rounds': 20,
                                                                  'scenario': 'duplicate',
                                                                  'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/duplicate/sqlite': {'backend': 'sqlite',
                                                                'events': {'health_history': 1,
                                                                           'sequence_ignored_or_idempotent': 20,
                                                                           'wire_history': 2},
                                                                'leaves': ['TestTVG2_03_SequenceAndBinding/duplicate/sqlite'],
                                                                'oics': ['lab-a', 'lab-b'],
                                                                'resource': True,
                                                                'root': 'TestTVG2_03_SequenceAndBinding',
                                                                'rounds': 20,
                                                                'scenario': 'duplicate',
                                                                'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/low_sample/postgres': {'backend': 'postgres',
                                                                   'events': {'health_history': 1,
                                                                              'sequence_ignored_or_idempotent': 20,
                                                                              'wire_history': 2},
                                                                   'leaves': ['TestTVG2_03_SequenceAndBinding/low_sample/postgres'],
                                                                   'oics': ['lab-a', 'lab-b'],
                                                                   'resource': True,
                                                                   'root': 'TestTVG2_03_SequenceAndBinding',
                                                                   'rounds': 20,
                                                                   'scenario': 'low_sample',
                                                                   'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/low_sample/sqlite': {'backend': 'sqlite',
                                                                 'events': {'health_history': 1,
                                                                            'sequence_ignored_or_idempotent': 20,
                                                                            'wire_history': 2},
                                                                 'leaves': ['TestTVG2_03_SequenceAndBinding/low_sample/sqlite'],
                                                                 'oics': ['lab-a', 'lab-b'],
                                                                 'resource': True,
                                                                 'root': 'TestTVG2_03_SequenceAndBinding',
                                                                 'rounds': 20,
                                                                 'scenario': 'low_sample',
                                                                 'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/low_update/postgres': {'backend': 'postgres',
                                                                   'events': {'health_history': 1,
                                                                              'sequence_ignored_or_idempotent': 20,
                                                                              'wire_history': 2},
                                                                   'leaves': ['TestTVG2_03_SequenceAndBinding/low_update/postgres'],
                                                                   'oics': ['lab-a', 'lab-b'],
                                                                   'resource': True,
                                                                   'root': 'TestTVG2_03_SequenceAndBinding',
                                                                   'rounds': 20,
                                                                   'scenario': 'low_update',
                                                                   'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/low_update/sqlite': {'backend': 'sqlite',
                                                                 'events': {'health_history': 1,
                                                                            'sequence_ignored_or_idempotent': 20,
                                                                            'wire_history': 2},
                                                                 'leaves': ['TestTVG2_03_SequenceAndBinding/low_update/sqlite'],
                                                                 'oics': ['lab-a', 'lab-b'],
                                                                 'resource': True,
                                                                 'root': 'TestTVG2_03_SequenceAndBinding',
                                                                 'rounds': 20,
                                                                 'scenario': 'low_update',
                                                                 'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/old_generation/postgres': {'backend': 'postgres',
                                                                       'events': {'binding_rejection': 20,
                                                                                  'health_history': 1,
                                                                                  'wire_history': 2},
                                                                       'leaves': ['TestTVG2_03_SequenceAndBinding/old_generation/postgres'],
                                                                       'oics': ['lab-a', 'lab-b'],
                                                                       'resource': True,
                                                                       'root': 'TestTVG2_03_SequenceAndBinding',
                                                                       'rounds': 20,
                                                                       'scenario': 'old_generation',
                                                                       'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/old_generation/sqlite': {'backend': 'sqlite',
                                                                     'events': {'binding_rejection': 20,
                                                                                'health_history': 1,
                                                                                'wire_history': 2},
                                                                     'leaves': ['TestTVG2_03_SequenceAndBinding/old_generation/sqlite'],
                                                                     'oics': ['lab-a', 'lab-b'],
                                                                     'resource': True,
                                                                     'root': 'TestTVG2_03_SequenceAndBinding',
                                                                     'rounds': 20,
                                                                     'scenario': 'old_generation',
                                                                     'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/old_revision/postgres': {'backend': 'postgres',
                                                                     'events': {'binding_rejection': 20,
                                                                                'health_history': 1,
                                                                                'wire_history': 2},
                                                                     'leaves': ['TestTVG2_03_SequenceAndBinding/old_revision/postgres'],
                                                                     'oics': ['lab-a', 'lab-b'],
                                                                     'resource': True,
                                                                     'root': 'TestTVG2_03_SequenceAndBinding',
                                                                     'rounds': 20,
                                                                     'scenario': 'old_revision',
                                                                     'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/old_revision/sqlite': {'backend': 'sqlite',
                                                                   'events': {'binding_rejection': 20,
                                                                              'health_history': 1,
                                                                              'wire_history': 2},
                                                                   'leaves': ['TestTVG2_03_SequenceAndBinding/old_revision/sqlite'],
                                                                   'oics': ['lab-a', 'lab-b'],
                                                                   'resource': True,
                                                                   'root': 'TestTVG2_03_SequenceAndBinding',
                                                                   'rounds': 20,
                                                                   'scenario': 'old_revision',
                                                                   'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/old_source/postgres': {'backend': 'postgres',
                                                                   'events': {'binding_rejection': 20,
                                                                              'health_history': 1,
                                                                              'wire_history': 2},
                                                                   'leaves': ['TestTVG2_03_SequenceAndBinding/old_source/postgres'],
                                                                   'oics': ['lab-a', 'lab-b'],
                                                                   'resource': True,
                                                                   'root': 'TestTVG2_03_SequenceAndBinding',
                                                                   'rounds': 20,
                                                                   'scenario': 'old_source',
                                                                   'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/old_source/sqlite': {'backend': 'sqlite',
                                                                 'events': {'binding_rejection': 20,
                                                                            'health_history': 1,
                                                                            'wire_history': 2},
                                                                 'leaves': ['TestTVG2_03_SequenceAndBinding/old_source/sqlite'],
                                                                 'oics': ['lab-a', 'lab-b'],
                                                                 'resource': True,
                                                                 'root': 'TestTVG2_03_SequenceAndBinding',
                                                                 'rounds': 20,
                                                                 'scenario': 'old_source',
                                                                 'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/same_sample_conflict/postgres': {'backend': 'postgres',
                                                                             'events': {'binding_rejection': 20,
                                                                                        'health_history': 1,
                                                                                        'wire_history': 2},
                                                                             'leaves': ['TestTVG2_03_SequenceAndBinding/same_sample_conflict/postgres'],
                                                                             'oics': ['lab-a', 'lab-b'],
                                                                             'resource': True,
                                                                             'root': 'TestTVG2_03_SequenceAndBinding',
                                                                             'rounds': 20,
                                                                             'scenario': 'same_sample_conflict',
                                                                             'vector': 'G2-03'},
            'TestTVG2_03_SequenceAndBinding/same_sample_conflict/sqlite': {'backend': 'sqlite',
                                                                           'events': {'binding_rejection': 20,
                                                                                      'health_history': 1,
                                                                                      'wire_history': 2},
                                                                           'leaves': ['TestTVG2_03_SequenceAndBinding/same_sample_conflict/sqlite'],
                                                                           'oics': ['lab-a', 'lab-b'],
                                                                           'resource': True,
                                                                           'root': 'TestTVG2_03_SequenceAndBinding',
                                                                           'rounds': 20,
                                                                           'scenario': 'same_sample_conflict',
                                                                           'vector': 'G2-03'},
            'TestTVG2_04_LateQueryCannotRevive/postgres': {'backend': 'postgres',
                                                           'events': {'health_history': 1,
                                                                      'late_query_discarded': 20,
                                                                      'wire_history': 2},
                                                           'leaves': ['TestTVG2_04_LateQueryCannotRevive/postgres'],
                                                           'oics': ['lab-a', 'lab-b'],
                                                           'resource': True,
                                                           'root': 'TestTVG2_04_LateQueryCannotRevive',
                                                           'rounds': 20,
                                                           'scenario': None,
                                                           'vector': 'G2-04'},
            'TestTVG2_04_LateQueryCannotRevive/sqlite': {'backend': 'sqlite',
                                                         'events': {'health_history': 1,
                                                                    'late_query_discarded': 20,
                                                                    'wire_history': 2},
                                                         'leaves': ['TestTVG2_04_LateQueryCannotRevive/sqlite'],
                                                         'oics': ['lab-a', 'lab-b'],
                                                         'resource': True,
                                                         'root': 'TestTVG2_04_LateQueryCannotRevive',
                                                         'rounds': 20,
                                                         'scenario': None,
                                                         'vector': 'G2-04'},
            'TestTVG2_04_ObservedAtIsDiagnostic/postgres': {'backend': 'postgres',
                                                            'events': {'health_history': 1,
                                                                       'observed_at_skew': 20,
                                                                       'wire_history': 2},
                                                            'leaves': ['TestTVG2_04_ObservedAtIsDiagnostic/postgres'],
                                                            'oics': ['lab-a', 'lab-b'],
                                                            'resource': True,
                                                            'root': 'TestTVG2_04_ObservedAtIsDiagnostic',
                                                            'rounds': 20,
                                                            'scenario': None,
                                                            'vector': 'G2-04'},
            'TestTVG2_04_ObservedAtIsDiagnostic/sqlite': {'backend': 'sqlite',
                                                          'events': {'health_history': 1,
                                                                     'observed_at_skew': 20,
                                                                     'wire_history': 2},
                                                          'leaves': ['TestTVG2_04_ObservedAtIsDiagnostic/sqlite'],
                                                          'oics': ['lab-a', 'lab-b'],
                                                          'resource': True,
                                                          'root': 'TestTVG2_04_ObservedAtIsDiagnostic',
                                                          'rounds': 20,
                                                          'scenario': None,
                                                          'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/asymmetric/postgres': {'backend': 'postgres',
                                                         'events': {'health_history': 1,
                                                                    'query_ttl_bound': 20,
                                                                    'wire_history': 2},
                                                         'leaves': ['TestTVG2_04_QueryTTL/asymmetric/postgres'],
                                                         'oics': ['lab-a', 'lab-b'],
                                                         'resource': True,
                                                         'root': 'TestTVG2_04_QueryTTL',
                                                         'rounds': 20,
                                                         'scenario': 'asymmetric',
                                                         'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/asymmetric/sqlite': {'backend': 'sqlite',
                                                       'events': {'health_history': 1,
                                                                  'query_ttl_bound': 20,
                                                                  'wire_history': 2},
                                                       'leaves': ['TestTVG2_04_QueryTTL/asymmetric/sqlite'],
                                                       'oics': ['lab-a', 'lab-b'],
                                                       'resource': True,
                                                       'root': 'TestTVG2_04_QueryTTL',
                                                       'rounds': 20,
                                                       'scenario': 'asymmetric',
                                                       'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/cache_wait/postgres': {'backend': 'postgres',
                                                         'events': {'health_history': 1,
                                                                    'query_ttl_bound': 20,
                                                                    'wire_history': 2},
                                                         'leaves': ['TestTVG2_04_QueryTTL/cache_wait/postgres'],
                                                         'oics': ['lab-a', 'lab-b'],
                                                         'resource': True,
                                                         'root': 'TestTVG2_04_QueryTTL',
                                                         'rounds': 20,
                                                         'scenario': 'cache_wait',
                                                         'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/cache_wait/sqlite': {'backend': 'sqlite',
                                                       'events': {'health_history': 1,
                                                                  'query_ttl_bound': 20,
                                                                  'wire_history': 2},
                                                       'leaves': ['TestTVG2_04_QueryTTL/cache_wait/sqlite'],
                                                       'oics': ['lab-a', 'lab-b'],
                                                       'resource': True,
                                                       'root': 'TestTVG2_04_QueryTTL',
                                                       'rounds': 20,
                                                       'scenario': 'cache_wait',
                                                       'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/duplicate_reply/postgres': {'backend': 'postgres',
                                                              'events': {'health_history': 1,
                                                                         'query_ttl_bound': 20,
                                                                         'wire_history': 2},
                                                              'leaves': ['TestTVG2_04_QueryTTL/duplicate_reply/postgres'],
                                                              'oics': ['lab-a', 'lab-b'],
                                                              'resource': True,
                                                              'root': 'TestTVG2_04_QueryTTL',
                                                              'rounds': 20,
                                                              'scenario': 'duplicate_reply',
                                                              'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/duplicate_reply/sqlite': {'backend': 'sqlite',
                                                            'events': {'health_history': 1,
                                                                       'query_ttl_bound': 20,
                                                                       'wire_history': 2},
                                                            'leaves': ['TestTVG2_04_QueryTTL/duplicate_reply/sqlite'],
                                                            'oics': ['lab-a', 'lab-b'],
                                                            'resource': True,
                                                            'root': 'TestTVG2_04_QueryTTL',
                                                            'rounds': 20,
                                                            'scenario': 'duplicate_reply',
                                                            'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/query_delay/postgres': {'backend': 'postgres',
                                                          'events': {'health_history': 1,
                                                                     'query_ttl_bound': 20,
                                                                     'wire_history': 2},
                                                          'leaves': ['TestTVG2_04_QueryTTL/query_delay/postgres'],
                                                          'oics': ['lab-a', 'lab-b'],
                                                          'resource': True,
                                                          'root': 'TestTVG2_04_QueryTTL',
                                                          'rounds': 20,
                                                          'scenario': 'query_delay',
                                                          'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/query_delay/sqlite': {'backend': 'sqlite',
                                                        'events': {'health_history': 1,
                                                                   'query_ttl_bound': 20,
                                                                   'wire_history': 2},
                                                        'leaves': ['TestTVG2_04_QueryTTL/query_delay/sqlite'],
                                                        'oics': ['lab-a', 'lab-b'],
                                                        'resource': True,
                                                        'root': 'TestTVG2_04_QueryTTL',
                                                        'rounds': 20,
                                                        'scenario': 'query_delay',
                                                        'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/response_delay/postgres': {'backend': 'postgres',
                                                             'events': {'health_history': 1,
                                                                        'query_ttl_bound': 20,
                                                                        'wire_history': 2},
                                                             'leaves': ['TestTVG2_04_QueryTTL/response_delay/postgres'],
                                                             'oics': ['lab-a', 'lab-b'],
                                                             'resource': True,
                                                             'root': 'TestTVG2_04_QueryTTL',
                                                             'rounds': 20,
                                                             'scenario': 'response_delay',
                                                             'vector': 'G2-04'},
            'TestTVG2_04_QueryTTL/response_delay/sqlite': {'backend': 'sqlite',
                                                           'events': {'health_history': 1,
                                                                      'query_ttl_bound': 20,
                                                                      'wire_history': 2},
                                                           'leaves': ['TestTVG2_04_QueryTTL/response_delay/sqlite'],
                                                           'oics': ['lab-a', 'lab-b'],
                                                           'resource': True,
                                                           'root': 'TestTVG2_04_QueryTTL',
                                                           'rounds': 20,
                                                           'scenario': 'response_delay',
                                                           'vector': 'G2-04'},
            'TestTVG2_05_FinalRecheck/postgres/after_both_legs_not_ready': {'backend': 'postgres',
                                                                            'events': {'commit_cleanup': 20,
                                                                                       'final_recheck_round': 20,
                                                                                       'health_history': 1,
                                                                                       'wire_history': 2},
                                                                            'leaves': ['TestTVG2_05_FinalRecheck/postgres/after_both_legs_not_ready'],
                                                                            'oics': ['lab-a', 'lab-b'],
                                                                            'resource': True,
                                                                            'root': 'TestTVG2_05_FinalRecheck',
                                                                            'rounds': 20,
                                                                            'scenario': 'after_both_legs_not_ready',
                                                                            'vector': 'G2-05'},
            'TestTVG2_05_FinalRecheck/postgres/before_second_leg_not_ready': {'backend': 'postgres',
                                                                              'events': {'commit_cleanup': 20,
                                                                                         'final_recheck_round': 20,
                                                                                         'health_history': 1,
                                                                                         'wire_history': 2},
                                                                              'leaves': ['TestTVG2_05_FinalRecheck/postgres/before_second_leg_not_ready'],
                                                                              'oics': ['lab-a', 'lab-b'],
                                                                              'resource': True,
                                                                              'root': 'TestTVG2_05_FinalRecheck',
                                                                              'rounds': 20,
                                                                              'scenario': 'before_second_leg_not_ready',
                                                                              'vector': 'G2-05'},
            'TestTVG2_05_FinalRecheck/postgres/ready_expired_before_second_leg': {'backend': 'postgres',
                                                                                  'events': {'commit_cleanup': 20,
                                                                                             'final_recheck_round': 20,
                                                                                             'health_history': 1,
                                                                                             'wire_history': 2},
                                                                                  'leaves': ['TestTVG2_05_FinalRecheck/postgres/ready_expired_before_second_leg'],
                                                                                  'oics': ['lab-a', 'lab-b'],
                                                                                  'resource': True,
                                                                                  'root': 'TestTVG2_05_FinalRecheck',
                                                                                  'rounds': 20,
                                                                                  'scenario': 'ready_expired_before_second_leg',
                                                                                  'vector': 'G2-05'},
            'TestTVG2_05_FinalRecheck/sqlite/after_both_legs_not_ready': {'backend': 'sqlite',
                                                                          'events': {'commit_cleanup': 20,
                                                                                     'final_recheck_round': 20,
                                                                                     'health_history': 1,
                                                                                     'wire_history': 2},
                                                                          'leaves': ['TestTVG2_05_FinalRecheck/sqlite/after_both_legs_not_ready'],
                                                                          'oics': ['lab-a', 'lab-b'],
                                                                          'resource': True,
                                                                          'root': 'TestTVG2_05_FinalRecheck',
                                                                          'rounds': 20,
                                                                          'scenario': 'after_both_legs_not_ready',
                                                                          'vector': 'G2-05'},
            'TestTVG2_05_FinalRecheck/sqlite/before_second_leg_not_ready': {'backend': 'sqlite',
                                                                            'events': {'commit_cleanup': 20,
                                                                                       'final_recheck_round': 20,
                                                                                       'health_history': 1,
                                                                                       'wire_history': 2},
                                                                            'leaves': ['TestTVG2_05_FinalRecheck/sqlite/before_second_leg_not_ready'],
                                                                            'oics': ['lab-a', 'lab-b'],
                                                                            'resource': True,
                                                                            'root': 'TestTVG2_05_FinalRecheck',
                                                                            'rounds': 20,
                                                                            'scenario': 'before_second_leg_not_ready',
                                                                            'vector': 'G2-05'},
            'TestTVG2_05_FinalRecheck/sqlite/ready_expired_before_second_leg': {'backend': 'sqlite',
                                                                                'events': {'commit_cleanup': 20,
                                                                                           'final_recheck_round': 20,
                                                                                           'health_history': 1,
                                                                                           'wire_history': 2},
                                                                                'leaves': ['TestTVG2_05_FinalRecheck/sqlite/ready_expired_before_second_leg'],
                                                                                'oics': ['lab-a', 'lab-b'],
                                                                                'resource': True,
                                                                                'root': 'TestTVG2_05_FinalRecheck',
                                                                                'rounds': 20,
                                                                                'scenario': 'ready_expired_before_second_leg',
                                                                                'vector': 'G2-05'},
            'TestTVG2_06_CommitSnapshotWindow/postgres': {'backend': 'postgres',
                                                          'events': {'commit_cleanup': 20,
                                                                     'commit_snapshot_round': 20,
                                                                     'health_history': 1,
                                                                     'wire_history': 2},
                                                          'leaves': ['TestTVG2_06_CommitSnapshotWindow/postgres'],
                                                          'oics': ['lab-a', 'lab-b'],
                                                          'resource': True,
                                                          'root': 'TestTVG2_06_CommitSnapshotWindow',
                                                          'rounds': 20,
                                                          'scenario': None,
                                                          'vector': 'G2-06'},
            'TestTVG2_06_CommitSnapshotWindow/sqlite': {'backend': 'sqlite',
                                                        'events': {'commit_cleanup': 20,
                                                                   'commit_snapshot_round': 20,
                                                                   'health_history': 1,
                                                                   'wire_history': 2},
                                                        'leaves': ['TestTVG2_06_CommitSnapshotWindow/sqlite'],
                                                        'oics': ['lab-a', 'lab-b'],
                                                        'resource': True,
                                                        'root': 'TestTVG2_06_CommitSnapshotWindow',
                                                        'rounds': 20,
                                                        'scenario': None,
                                                        'vector': 'G2-06'},
            'TestTVG2_06_ExpiredFrozenReady/postgres': {'backend': 'postgres',
                                                        'events': {'commit_cleanup': 20,
                                                                   'frozen_ready_expiry_round': 20,
                                                                   'health_history': 1,
                                                                   'wire_history': 2},
                                                        'leaves': ['TestTVG2_06_ExpiredFrozenReady/postgres'],
                                                        'oics': ['lab-a', 'lab-b'],
                                                        'resource': True,
                                                        'root': 'TestTVG2_06_ExpiredFrozenReady',
                                                        'rounds': 20,
                                                        'scenario': None,
                                                        'vector': 'G2-06'},
            'TestTVG2_06_ExpiredFrozenReady/sqlite': {'backend': 'sqlite',
                                                      'events': {'commit_cleanup': 20,
                                                                 'frozen_ready_expiry_round': 20,
                                                                 'health_history': 1,
                                                                 'wire_history': 2},
                                                      'leaves': ['TestTVG2_06_ExpiredFrozenReady/sqlite'],
                                                      'oics': ['lab-a', 'lab-b'],
                                                      'resource': True,
                                                      'root': 'TestTVG2_06_ExpiredFrozenReady',
                                                      'rounds': 20,
                                                      'scenario': None,
                                                      'vector': 'G2-06'},
            'TestTVG2_07_FinalSecurityRecheck/postgres/credential_revoked': {'backend': 'postgres',
                                                                             'events': {'commit_cleanup': 20,
                                                                                        'health_history': 1,
                                                                                        'security_final_round': 20,
                                                                                        'wire_history': 2},
                                                                             'leaves': ['TestTVG2_07_FinalSecurityRecheck/postgres/credential_revoked'],
                                                                             'oics': ['lab-a', 'lab-b'],
                                                                             'resource': True,
                                                                             'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                             'rounds': 20,
                                                                             'scenario': 'credential_revoked',
                                                                             'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/postgres/generation_replaced': {'backend': 'postgres',
                                                                              'events': {'commit_cleanup': 20,
                                                                                         'health_history': 1,
                                                                                         'security_final_round': 20,
                                                                                         'wire_history': 2},
                                                                              'leaves': ['TestTVG2_07_FinalSecurityRecheck/postgres/generation_replaced'],
                                                                              'oics': ['lab-a', 'lab-b'],
                                                                              'resource': True,
                                                                              'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                              'rounds': 20,
                                                                              'scenario': 'generation_replaced',
                                                                              'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/postgres/license_expired': {'backend': 'postgres',
                                                                          'events': {'commit_cleanup': 20,
                                                                                     'health_history': 1,
                                                                                     'license_restored_between_rounds': 20,
                                                                                     'security_final_round': 20,
                                                                                     'wire_history': 2},
                                                                          'leaves': ['TestTVG2_07_FinalSecurityRecheck/postgres/license_expired'],
                                                                          'oics': ['lab-a', 'lab-b'],
                                                                          'resource': True,
                                                                          'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                          'rounds': 20,
                                                                          'scenario': 'license_expired',
                                                                          'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/postgres/policy_revoked': {'backend': 'postgres',
                                                                         'events': {'commit_cleanup': 20,
                                                                                    'health_history': 1,
                                                                                    'security_final_round': 20,
                                                                                    'wire_history': 2},
                                                                         'leaves': ['TestTVG2_07_FinalSecurityRecheck/postgres/policy_revoked'],
                                                                         'oics': ['lab-a', 'lab-b'],
                                                                         'resource': True,
                                                                         'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                         'rounds': 20,
                                                                         'scenario': 'policy_revoked',
                                                                         'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/postgres/target_revision': {'backend': 'postgres',
                                                                          'events': {'commit_cleanup': 20,
                                                                                     'health_history': 1,
                                                                                     'security_final_round': 20,
                                                                                     'wire_history': 2},
                                                                          'leaves': ['TestTVG2_07_FinalSecurityRecheck/postgres/target_revision'],
                                                                          'oics': ['lab-a', 'lab-b'],
                                                                          'resource': True,
                                                                          'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                          'rounds': 20,
                                                                          'scenario': 'target_revision',
                                                                          'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/sqlite/credential_revoked': {'backend': 'sqlite',
                                                                           'events': {'commit_cleanup': 20,
                                                                                      'health_history': 1,
                                                                                      'security_final_round': 20,
                                                                                      'wire_history': 2},
                                                                           'leaves': ['TestTVG2_07_FinalSecurityRecheck/sqlite/credential_revoked'],
                                                                           'oics': ['lab-a', 'lab-b'],
                                                                           'resource': True,
                                                                           'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                           'rounds': 20,
                                                                           'scenario': 'credential_revoked',
                                                                           'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/sqlite/generation_replaced': {'backend': 'sqlite',
                                                                            'events': {'commit_cleanup': 20,
                                                                                       'health_history': 1,
                                                                                       'security_final_round': 20,
                                                                                       'wire_history': 2},
                                                                            'leaves': ['TestTVG2_07_FinalSecurityRecheck/sqlite/generation_replaced'],
                                                                            'oics': ['lab-a', 'lab-b'],
                                                                            'resource': True,
                                                                            'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                            'rounds': 20,
                                                                            'scenario': 'generation_replaced',
                                                                            'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/sqlite/license_expired': {'backend': 'sqlite',
                                                                        'events': {'commit_cleanup': 20,
                                                                                   'health_history': 1,
                                                                                   'license_restored_between_rounds': 20,
                                                                                   'security_final_round': 20,
                                                                                   'wire_history': 2},
                                                                        'leaves': ['TestTVG2_07_FinalSecurityRecheck/sqlite/license_expired'],
                                                                        'oics': ['lab-a', 'lab-b'],
                                                                        'resource': True,
                                                                        'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                        'rounds': 20,
                                                                        'scenario': 'license_expired',
                                                                        'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/sqlite/policy_revoked': {'backend': 'sqlite',
                                                                       'events': {'commit_cleanup': 20,
                                                                                  'health_history': 1,
                                                                                  'security_final_round': 20,
                                                                                  'wire_history': 2},
                                                                       'leaves': ['TestTVG2_07_FinalSecurityRecheck/sqlite/policy_revoked'],
                                                                       'oics': ['lab-a', 'lab-b'],
                                                                       'resource': True,
                                                                       'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                       'rounds': 20,
                                                                       'scenario': 'policy_revoked',
                                                                       'vector': 'G2-07'},
            'TestTVG2_07_FinalSecurityRecheck/sqlite/target_revision': {'backend': 'sqlite',
                                                                        'events': {'commit_cleanup': 20,
                                                                                   'health_history': 1,
                                                                                   'security_final_round': 20,
                                                                                   'wire_history': 2},
                                                                        'leaves': ['TestTVG2_07_FinalSecurityRecheck/sqlite/target_revision'],
                                                                        'oics': ['lab-a', 'lab-b'],
                                                                        'resource': True,
                                                                        'root': 'TestTVG2_07_FinalSecurityRecheck',
                                                                        'rounds': 20,
                                                                        'scenario': 'target_revision',
                                                                        'vector': 'G2-07'},
            'TestTVG2_08a_MessageAndCleanup/postgres/cli_preflight_response_drop': {'backend': 'postgres',
                                                                                    'events': {'cli_response_drop_round': 20,
                                                                                               'health_history': 1,
                                                                                               'wire_history': 2},
                                                                                    'leaves': ['TestTVG2_08a_MessageAndCleanup/postgres/cli_preflight_response_drop'],
                                                                                    'oics': ['lab-a',
                                                                                             'lab-b'],
                                                                                    'resource': True,
                                                                                    'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                                    'rounds': 20,
                                                                                    'scenario': 'cli_preflight_response_drop',
                                                                                    'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/postgres/duplicate_and_closed_positive': {'backend': 'postgres',
                                                                                      'events': {'commit_cleanup': 40,
                                                                                                 'duplicate_closed_positive_round': 20,
                                                                                                 'health_history': 1,
                                                                                                 'wire_history': 2},
                                                                                      'leaves': ['TestTVG2_08a_MessageAndCleanup/postgres/duplicate_and_closed_positive'],
                                                                                      'oics': ['lab-a',
                                                                                               'lab-b'],
                                                                                      'resource': True,
                                                                                      'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                                      'rounds': 20,
                                                                                      'scenario': 'duplicate_and_closed_positive',
                                                                                      'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/postgres/final_response_drop': {'backend': 'postgres',
                                                                            'events': {'commit_cleanup': 20,
                                                                                       'final_loss_round': 20,
                                                                                       'health_history': 1,
                                                                                       'wire_history': 2},
                                                                            'leaves': ['TestTVG2_08a_MessageAndCleanup/postgres/final_response_drop'],
                                                                            'oics': ['lab-a', 'lab-b'],
                                                                            'resource': True,
                                                                            'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                            'rounds': 20,
                                                                            'scenario': 'final_response_drop',
                                                                            'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/postgres/final_timeout_late': {'backend': 'postgres',
                                                                           'events': {'commit_cleanup': 20,
                                                                                      'final_loss_round': 20,
                                                                                      'health_history': 1,
                                                                                      'wire_history': 2},
                                                                           'leaves': ['TestTVG2_08a_MessageAndCleanup/postgres/final_timeout_late'],
                                                                           'oics': ['lab-a', 'lab-b'],
                                                                           'resource': True,
                                                                           'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                           'rounds': 20,
                                                                           'scenario': 'final_timeout_late',
                                                                           'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/postgres/pending_slots_full': {'backend': 'postgres',
                                                                           'events': {'health_history': 1,
                                                                                      'pending_capacity_round': 20,
                                                                                      'wire_history': 2},
                                                                           'leaves': ['TestTVG2_08a_MessageAndCleanup/postgres/pending_slots_full'],
                                                                           'oics': ['lab-a', 'lab-b'],
                                                                           'resource': True,
                                                                           'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                           'rounds': 20,
                                                                           'scenario': 'pending_slots_full',
                                                                           'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/sqlite/cli_preflight_response_drop': {'backend': 'sqlite',
                                                                                  'events': {'cli_response_drop_round': 20,
                                                                                             'health_history': 1,
                                                                                             'wire_history': 2},
                                                                                  'leaves': ['TestTVG2_08a_MessageAndCleanup/sqlite/cli_preflight_response_drop'],
                                                                                  'oics': ['lab-a', 'lab-b'],
                                                                                  'resource': True,
                                                                                  'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                                  'rounds': 20,
                                                                                  'scenario': 'cli_preflight_response_drop',
                                                                                  'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/sqlite/duplicate_and_closed_positive': {'backend': 'sqlite',
                                                                                    'events': {'commit_cleanup': 40,
                                                                                               'duplicate_closed_positive_round': 20,
                                                                                               'health_history': 1,
                                                                                               'wire_history': 2},
                                                                                    'leaves': ['TestTVG2_08a_MessageAndCleanup/sqlite/duplicate_and_closed_positive'],
                                                                                    'oics': ['lab-a',
                                                                                             'lab-b'],
                                                                                    'resource': True,
                                                                                    'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                                    'rounds': 20,
                                                                                    'scenario': 'duplicate_and_closed_positive',
                                                                                    'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/sqlite/final_response_drop': {'backend': 'sqlite',
                                                                          'events': {'commit_cleanup': 20,
                                                                                     'final_loss_round': 20,
                                                                                     'health_history': 1,
                                                                                     'wire_history': 2},
                                                                          'leaves': ['TestTVG2_08a_MessageAndCleanup/sqlite/final_response_drop'],
                                                                          'oics': ['lab-a', 'lab-b'],
                                                                          'resource': True,
                                                                          'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                          'rounds': 20,
                                                                          'scenario': 'final_response_drop',
                                                                          'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/sqlite/final_timeout_late': {'backend': 'sqlite',
                                                                         'events': {'commit_cleanup': 20,
                                                                                    'final_loss_round': 20,
                                                                                    'health_history': 1,
                                                                                    'wire_history': 2},
                                                                         'leaves': ['TestTVG2_08a_MessageAndCleanup/sqlite/final_timeout_late'],
                                                                         'oics': ['lab-a', 'lab-b'],
                                                                         'resource': True,
                                                                         'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                         'rounds': 20,
                                                                         'scenario': 'final_timeout_late',
                                                                         'vector': 'G2-08a'},
            'TestTVG2_08a_MessageAndCleanup/sqlite/pending_slots_full': {'backend': 'sqlite',
                                                                         'events': {'health_history': 1,
                                                                                    'pending_capacity_round': 20,
                                                                                    'wire_history': 2},
                                                                         'leaves': ['TestTVG2_08a_MessageAndCleanup/sqlite/pending_slots_full'],
                                                                         'oics': ['lab-a', 'lab-b'],
                                                                         'resource': True,
                                                                         'root': 'TestTVG2_08a_MessageAndCleanup',
                                                                         'rounds': 20,
                                                                         'scenario': 'pending_slots_full',
                                                                         'vector': 'G2-08a'},
            'TestTVG2_08b_ObservationRevisionCancelsBeforeReady/postgres': {'backend': 'postgres',
                                                                            'events': {'commit_cleanup': 20,
                                                                                       'health_history': 1,
                                                                                       'observation_cancel_before_ready_round': 20,
                                                                                       'wire_history': 2},
                                                                            'leaves': ['TestTVG2_08b_ObservationRevisionCancelsBeforeReady/postgres'],
                                                                            'oics': ['lab-a', 'lab-b'],
                                                                            'resource': True,
                                                                            'root': 'TestTVG2_08b_ObservationRevisionCancelsBeforeReady',
                                                                            'rounds': 20,
                                                                            'scenario': None,
                                                                            'vector': 'G2-08b'},
            'TestTVG2_08b_ObservationRevisionCancelsBeforeReady/sqlite': {'backend': 'sqlite',
                                                                          'events': {'commit_cleanup': 20,
                                                                                     'health_history': 1,
                                                                                     'observation_cancel_before_ready_round': 20,
                                                                                     'wire_history': 2},
                                                                          'leaves': ['TestTVG2_08b_ObservationRevisionCancelsBeforeReady/sqlite'],
                                                                          'oics': ['lab-a', 'lab-b'],
                                                                          'resource': True,
                                                                          'root': 'TestTVG2_08b_ObservationRevisionCancelsBeforeReady',
                                                                          'rounds': 20,
                                                                          'scenario': None,
                                                                          'vector': 'G2-08b'},
            'TestTVG2_08c_RunningProbeReload/postgres': {'backend': 'postgres',
                                                         'events': {'commit_cleanup': 40,
                                                                    'health_history': 1,
                                                                    'running_probe_reload_round': 20,
                                                                    'wire_history': 2},
                                                         'leaves': ['TestTVG2_08c_RunningProbeReload/postgres'],
                                                         'oics': ['lab-a', 'lab-b'],
                                                         'resource': True,
                                                         'root': 'TestTVG2_08c_RunningProbeReload',
                                                         'rounds': 20,
                                                         'scenario': None,
                                                         'vector': 'G2-08c'},
            'TestTVG2_08c_RunningProbeReload/sqlite': {'backend': 'sqlite',
                                                       'events': {'commit_cleanup': 40,
                                                                  'health_history': 1,
                                                                  'running_probe_reload_round': 20,
                                                                  'wire_history': 2},
                                                       'leaves': ['TestTVG2_08c_RunningProbeReload/sqlite'],
                                                       'oics': ['lab-a', 'lab-b'],
                                                       'resource': True,
                                                       'root': 'TestTVG2_08c_RunningProbeReload',
                                                       'rounds': 20,
                                                       'scenario': None,
                                                       'vector': 'G2-08c'},
            'TestTVG2_08d_PendingOpenIdentity/postgres': {'backend': 'postgres',
                                                          'events': {'cancel_after_creation': 20,
                                                                     'failed_key_retry': 1,
                                                                     'health_history': 1,
                                                                     'last_cancel_and_late_result': 20,
                                                                     'pending_merge': 20,
                                                                     'wire_history': 2},
                                                          'leaves': ['TestTVG2_08d_PendingOpenIdentity/postgres'],
                                                          'oics': ['lab-a', 'lab-b'],
                                                          'resource': True,
                                                          'root': 'TestTVG2_08d_PendingOpenIdentity',
                                                          'rounds': 20,
                                                          'scenario': None,
                                                          'vector': 'G2-08d'},
            'TestTVG2_08d_PendingOpenIdentity/sqlite': {'backend': 'sqlite',
                                                        'events': {'cancel_after_creation': 20,
                                                                   'failed_key_retry': 1,
                                                                   'health_history': 1,
                                                                   'last_cancel_and_late_result': 20,
                                                                   'pending_merge': 20,
                                                                   'wire_history': 2},
                                                        'leaves': ['TestTVG2_08d_PendingOpenIdentity/sqlite'],
                                                        'oics': ['lab-a', 'lab-b'],
                                                        'resource': True,
                                                        'root': 'TestTVG2_08d_PendingOpenIdentity',
                                                        'rounds': 20,
                                                        'scenario': None,
                                                        'vector': 'G2-08d'},
            'TestTVG2_09_IndependentPublication/postgres': {'backend': 'postgres',
                                                            'events': {'health_history': 2,
                                                                       'independent_publication': 20},
                                                            'leaves': ['TestTVG2_09_IndependentPublication/postgres'],
                                                            'oics': ['lab-a', 'lab-b'],
                                                            'resource': True,
                                                            'root': 'TestTVG2_09_IndependentPublication',
                                                            'rounds': 20,
                                                            'scenario': None,
                                                            'vector': 'G2-09'},
            'TestTVG2_09_IndependentPublication/sqlite': {'backend': 'sqlite',
                                                          'events': {'health_history': 2,
                                                                     'independent_publication': 20},
                                                          'leaves': ['TestTVG2_09_IndependentPublication/sqlite'],
                                                          'oics': ['lab-a', 'lab-b'],
                                                          'resource': True,
                                                          'root': 'TestTVG2_09_IndependentPublication',
                                                          'rounds': 20,
                                                          'scenario': None,
                                                          'vector': 'G2-09'},
            'TestTVG2_10_SaturatedFairness/postgres/round_0': {'backend': 'postgres',
                                                               'events': {'saturation_complete': 1,
                                                                          'saturation_target': 16,
                                                                          'scheduler_sample': {'exact_from': 'saturation_complete.samples',
                                                                                               'min': 1}},
                                                               'leaves': ['TestTVG2_10_SaturatedFairness/postgres/round_0'],
                                                               'oics': ['lab-a', 'lab-b'],
                                                               'resource': True,
                                                               'root': 'TestTVG2_10_SaturatedFairness',
                                                               'rounds': 1,
                                                               'scenario': 'round_0',
                                                               'vector': 'G2-10'},
            'TestTVG2_10_SaturatedFairness/postgres/round_1': {'backend': 'postgres',
                                                               'events': {'saturation_complete': 1,
                                                                          'saturation_target': 16,
                                                                          'scheduler_sample': {'exact_from': 'saturation_complete.samples',
                                                                                               'min': 1}},
                                                               'leaves': ['TestTVG2_10_SaturatedFairness/postgres/round_1'],
                                                               'oics': ['lab-a', 'lab-b'],
                                                               'resource': True,
                                                               'root': 'TestTVG2_10_SaturatedFairness',
                                                               'rounds': 1,
                                                               'scenario': 'round_1',
                                                               'vector': 'G2-10'},
            'TestTVG2_10_SaturatedFairness/postgres/round_2': {'backend': 'postgres',
                                                               'events': {'saturation_complete': 1,
                                                                          'saturation_target': 16,
                                                                          'scheduler_sample': {'exact_from': 'saturation_complete.samples',
                                                                                               'min': 1}},
                                                               'leaves': ['TestTVG2_10_SaturatedFairness/postgres/round_2'],
                                                               'oics': ['lab-a', 'lab-b'],
                                                               'resource': True,
                                                               'root': 'TestTVG2_10_SaturatedFairness',
                                                               'rounds': 1,
                                                               'scenario': 'round_2',
                                                               'vector': 'G2-10'},
            'TestTVG2_10_SaturatedFairness/sqlite/round_0': {'backend': 'sqlite',
                                                             'events': {'saturation_complete': 1,
                                                                        'saturation_target': 16,
                                                                        'scheduler_sample': {'exact_from': 'saturation_complete.samples',
                                                                                             'min': 1}},
                                                             'leaves': ['TestTVG2_10_SaturatedFairness/sqlite/round_0'],
                                                             'oics': ['lab-a', 'lab-b'],
                                                             'resource': True,
                                                             'root': 'TestTVG2_10_SaturatedFairness',
                                                             'rounds': 1,
                                                             'scenario': 'round_0',
                                                             'vector': 'G2-10'},
            'TestTVG2_10_SaturatedFairness/sqlite/round_1': {'backend': 'sqlite',
                                                             'events': {'saturation_complete': 1,
                                                                        'saturation_target': 16,
                                                                        'scheduler_sample': {'exact_from': 'saturation_complete.samples',
                                                                                             'min': 1}},
                                                             'leaves': ['TestTVG2_10_SaturatedFairness/sqlite/round_1'],
                                                             'oics': ['lab-a', 'lab-b'],
                                                             'resource': True,
                                                             'root': 'TestTVG2_10_SaturatedFairness',
                                                             'rounds': 1,
                                                             'scenario': 'round_1',
                                                             'vector': 'G2-10'},
            'TestTVG2_10_SaturatedFairness/sqlite/round_2': {'backend': 'sqlite',
                                                             'events': {'saturation_complete': 1,
                                                                        'saturation_target': 16,
                                                                        'scheduler_sample': {'exact_from': 'saturation_complete.samples',
                                                                                             'min': 1}},
                                                             'leaves': ['TestTVG2_10_SaturatedFairness/sqlite/round_2'],
                                                             'oics': ['lab-a', 'lab-b'],
                                                             'resource': True,
                                                             'root': 'TestTVG2_10_SaturatedFairness',
                                                             'rounds': 1,
                                                             'scenario': 'round_2',
                                                             'vector': 'G2-10'},
            'TestTVG2_11_ControlExportCompetition/postgres/round_0': {'backend': 'postgres',
                                                                      'events': {'competition_cleanup_requested': 1,
                                                                                 'competition_complete': 1,
                                                                                 'competition_victim_input': {'exact_from': 'competition_complete.cycles', 'max': 512, 'min': 20},
                                                                                 'competition_cycle': {'exact_from': 'competition_complete.cycles',
                                                                                                       'max': 512,
                                                                                                       'min': 20},
                                                                                 'competition_drain': 1,
                                                                                 'competition_full': 1,
                                                                                 'competition_heartbeats': 2,
                                                                                 'competition_target': 16,
                                                                                 'wire_history': 2},
                                                                      'leaves': ['TestTVG2_11_ControlExportCompetition/postgres/round_0'],
                                                                      'oics': ['lab-a', 'lab-b'],
                                                                      'resource': True,
                                                                      'root': 'TestTVG2_11_ControlExportCompetition',
                                                                      'rounds': 1,
                                                                      'scenario': 'round_0',
                                                                      'vector': 'TV-G2-11'},
            'TestTVG2_11_ControlExportCompetition/postgres/round_1': {'backend': 'postgres',
                                                                      'events': {'competition_cleanup_requested': 1,
                                                                                 'competition_complete': 1,
                                                                                 'competition_victim_input': {'exact_from': 'competition_complete.cycles', 'max': 512, 'min': 20},
                                                                                 'competition_cycle': {'exact_from': 'competition_complete.cycles',
                                                                                                       'max': 512,
                                                                                                       'min': 20},
                                                                                 'competition_drain': 1,
                                                                                 'competition_full': 1,
                                                                                 'competition_heartbeats': 2,
                                                                                 'competition_target': 16,
                                                                                 'wire_history': 2},
                                                                      'leaves': ['TestTVG2_11_ControlExportCompetition/postgres/round_1'],
                                                                      'oics': ['lab-a', 'lab-b'],
                                                                      'resource': True,
                                                                      'root': 'TestTVG2_11_ControlExportCompetition',
                                                                      'rounds': 1,
                                                                      'scenario': 'round_1',
                                                                      'vector': 'TV-G2-11'},
            'TestTVG2_11_ControlExportCompetition/postgres/round_2': {'backend': 'postgres',
                                                                      'events': {'competition_cleanup_requested': 1,
                                                                                 'competition_complete': 1,
                                                                                 'competition_victim_input': {'exact_from': 'competition_complete.cycles', 'max': 512, 'min': 20},
                                                                                 'competition_cycle': {'exact_from': 'competition_complete.cycles',
                                                                                                       'max': 512,
                                                                                                       'min': 20},
                                                                                 'competition_drain': 1,
                                                                                 'competition_full': 1,
                                                                                 'competition_heartbeats': 2,
                                                                                 'competition_target': 16,
                                                                                 'wire_history': 2},
                                                                      'leaves': ['TestTVG2_11_ControlExportCompetition/postgres/round_2'],
                                                                      'oics': ['lab-a', 'lab-b'],
                                                                      'resource': True,
                                                                      'root': 'TestTVG2_11_ControlExportCompetition',
                                                                      'rounds': 1,
                                                                      'scenario': 'round_2',
                                                                      'vector': 'TV-G2-11'},
            'TestTVG2_11_ControlExportCompetition/sqlite/round_0': {'backend': 'sqlite',
                                                                    'events': {'competition_cleanup_requested': 1,
                                                                               'competition_complete': 1,
                                                                               'competition_victim_input': {'exact_from': 'competition_complete.cycles', 'max': 512, 'min': 20},
                                                                                 'competition_cycle': {'exact_from': 'competition_complete.cycles',
                                                                                                     'max': 512,
                                                                                                     'min': 20},
                                                                               'competition_drain': 1,
                                                                               'competition_full': 1,
                                                                               'competition_heartbeats': 2,
                                                                               'competition_target': 16,
                                                                               'wire_history': 2},
                                                                    'leaves': ['TestTVG2_11_ControlExportCompetition/sqlite/round_0'],
                                                                    'oics': ['lab-a', 'lab-b'],
                                                                    'resource': True,
                                                                    'root': 'TestTVG2_11_ControlExportCompetition',
                                                                    'rounds': 1,
                                                                    'scenario': 'round_0',
                                                                    'vector': 'TV-G2-11'},
            'TestTVG2_11_ControlExportCompetition/sqlite/round_1': {'backend': 'sqlite',
                                                                    'events': {'competition_cleanup_requested': 1,
                                                                               'competition_complete': 1,
                                                                               'competition_victim_input': {'exact_from': 'competition_complete.cycles', 'max': 512, 'min': 20},
                                                                                 'competition_cycle': {'exact_from': 'competition_complete.cycles',
                                                                                                     'max': 512,
                                                                                                     'min': 20},
                                                                               'competition_drain': 1,
                                                                               'competition_full': 1,
                                                                               'competition_heartbeats': 2,
                                                                               'competition_target': 16,
                                                                               'wire_history': 2},
                                                                    'leaves': ['TestTVG2_11_ControlExportCompetition/sqlite/round_1'],
                                                                    'oics': ['lab-a', 'lab-b'],
                                                                    'resource': True,
                                                                    'root': 'TestTVG2_11_ControlExportCompetition',
                                                                    'rounds': 1,
                                                                    'scenario': 'round_1',
                                                                    'vector': 'TV-G2-11'},
            'TestTVG2_11_ControlExportCompetition/sqlite/round_2': {'backend': 'sqlite',
                                                                    'events': {'competition_cleanup_requested': 1,
                                                                               'competition_complete': 1,
                                                                               'competition_victim_input': {'exact_from': 'competition_complete.cycles', 'max': 512, 'min': 20},
                                                                                 'competition_cycle': {'exact_from': 'competition_complete.cycles',
                                                                                                     'max': 512,
                                                                                                     'min': 20},
                                                                               'competition_drain': 1,
                                                                               'competition_full': 1,
                                                                               'competition_heartbeats': 2,
                                                                               'competition_target': 16,
                                                                               'wire_history': 2},
                                                                    'leaves': ['TestTVG2_11_ControlExportCompetition/sqlite/round_2'],
                                                                    'oics': ['lab-a', 'lab-b'],
                                                                    'resource': True,
                                                                    'root': 'TestTVG2_11_ControlExportCompetition',
                                                                    'rounds': 1,
                                                                    'scenario': 'round_2',
                                                                    'vector': 'TV-G2-11'},
            'TestTVG2_12_ExporterEntityCardinality/postgres': {'backend': 'postgres',
                                                               'events': {'export_after_entity': 64,
                                                                          'export_entity': 32,
                                                                          'export_initial': 2},
                                                               'leaves': ['TestTVG2_12_ExporterEntityCardinality/postgres'],
                                                               'oics': ['lab-a', 'lab-b'],
                                                               'resource': True,
                                                               'root': 'TestTVG2_12_ExporterEntityCardinality',
                                                               'rounds': 32,
                                                               'scenario': None,
                                                               'vector': 'TV-G2-12'},
            'TestTVG2_12_ExporterEntityCardinality/sqlite': {'backend': 'sqlite',
                                                             'events': {'export_after_entity': 64,
                                                                        'export_entity': 32,
                                                                        'export_initial': 2},
                                                             'leaves': ['TestTVG2_12_ExporterEntityCardinality/sqlite'],
                                                             'oics': ['lab-a', 'lab-b'],
                                                             'resource': True,
                                                             'root': 'TestTVG2_12_ExporterEntityCardinality',
                                                             'rounds': 32,
                                                             'scenario': None,
                                                             'vector': 'TV-G2-12'},
            'TestTVG2_12_ExporterSlowReaders/postgres': {'backend': 'postgres',
                                                         'events': {'export_slow_round': 40},
                                                         'leaves': ['TestTVG2_12_ExporterSlowReaders/postgres'],
                                                         'oics': ['lab-a', 'lab-b'],
                                                         'resource': True,
                                                         'root': 'TestTVG2_12_ExporterSlowReaders',
                                                         'rounds': 40,
                                                         'scenario': None,
                                                         'vector': 'TV-G2-12'},
            'TestTVG2_12_ExporterSlowReaders/sqlite': {'backend': 'sqlite',
                                                       'events': {'export_slow_round': 40},
                                                       'leaves': ['TestTVG2_12_ExporterSlowReaders/sqlite'],
                                                       'oics': ['lab-a', 'lab-b'],
                                                       'resource': True,
                                                       'root': 'TestTVG2_12_ExporterSlowReaders',
                                                       'rounds': 40,
                                                       'scenario': None,
                                                       'vector': 'TV-G2-12'},
            'TestTVG2_13_Compatibility/new_new_old_cli_check_timeout/postgres': {'backend': 'postgres',
                                                                                 'events': {'commit_cleanup': 20,
                                                                                            'compatibility_binary_sources': 1,
                                                                                            'compatibility_old_cli_abort_round': 20,
                                                                                            'compatibility_old_cli_native': 1,
                                                                                            'health_history': 1,
                                                                                            'wire_history': 2},
                                                                                 'leaves': ['TestTVG2_13_Compatibility/new_new_old_cli_check_timeout/postgres'],
                                                                                 'oics': ['lab-a', 'lab-b'],
                                                                                 'resource': True,
                                                                                 'root': 'TestTVG2_13_Compatibility',
                                                                                 'rounds': 20,
                                                                                 'scenario': 'new_new_old_cli_check_timeout',
                                                                                 'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/new_new_old_cli_check_timeout/sqlite': {'backend': 'sqlite',
                                                                               'events': {'commit_cleanup': 20,
                                                                                          'compatibility_binary_sources': 1,
                                                                                          'compatibility_old_cli_abort_round': 20,
                                                                                          'compatibility_old_cli_native': 1,
                                                                                          'health_history': 1,
                                                                                          'wire_history': 2},
                                                                               'leaves': ['TestTVG2_13_Compatibility/new_new_old_cli_check_timeout/sqlite'],
                                                                               'oics': ['lab-a', 'lab-b'],
                                                                               'resource': True,
                                                                               'root': 'TestTVG2_13_Compatibility',
                                                                               'rounds': 20,
                                                                               'scenario': 'new_new_old_cli_check_timeout',
                                                                               'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/new_new_old_cli_ready_expired/postgres': {'backend': 'postgres',
                                                                                 'events': {'commit_cleanup': 20,
                                                                                            'compatibility_binary_sources': 1,
                                                                                            'compatibility_old_cli_abort_round': 20,
                                                                                            'compatibility_old_cli_native': 1,
                                                                                            'health_history': 1,
                                                                                            'wire_history': 2},
                                                                                 'leaves': ['TestTVG2_13_Compatibility/new_new_old_cli_ready_expired/postgres'],
                                                                                 'oics': ['lab-a', 'lab-b'],
                                                                                 'resource': True,
                                                                                 'root': 'TestTVG2_13_Compatibility',
                                                                                 'rounds': 20,
                                                                                 'scenario': 'new_new_old_cli_ready_expired',
                                                                                 'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/new_new_old_cli_ready_expired/sqlite': {'backend': 'sqlite',
                                                                               'events': {'commit_cleanup': 20,
                                                                                          'compatibility_binary_sources': 1,
                                                                                          'compatibility_old_cli_abort_round': 20,
                                                                                          'compatibility_old_cli_native': 1,
                                                                                          'health_history': 1,
                                                                                          'wire_history': 2},
                                                                               'leaves': ['TestTVG2_13_Compatibility/new_new_old_cli_ready_expired/sqlite'],
                                                                               'oics': ['lab-a', 'lab-b'],
                                                                               'resource': True,
                                                                               'root': 'TestTVG2_13_Compatibility',
                                                                               'rounds': 20,
                                                                               'scenario': 'new_new_old_cli_ready_expired',
                                                                               'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/new_ois_old_oic_new_and_old_client/postgres': {'backend': 'postgres',
                                                                                      'events': {'compatibility_binary_sources': 1,
                                                                                                 'compatibility_native_result': 1,
                                                                                                 'compatibility_unknown_strict': 1,
                                                                                                 'wire_history': 2},
                                                                                      'leaves': ['TestTVG2_13_Compatibility/new_ois_old_oic_new_and_old_client/postgres'],
                                                                                      'oics': ['lab-a',
                                                                                               'lab-b'],
                                                                                      'resource': True,
                                                                                      'root': 'TestTVG2_13_Compatibility',
                                                                                      'rounds': 1,
                                                                                      'scenario': 'new_ois_old_oic_new_and_old_client',
                                                                                      'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/new_ois_old_oic_new_and_old_client/sqlite': {'backend': 'sqlite',
                                                                                    'events': {'compatibility_binary_sources': 1,
                                                                                               'compatibility_native_result': 1,
                                                                                               'compatibility_unknown_strict': 1,
                                                                                               'wire_history': 2},
                                                                                    'leaves': ['TestTVG2_13_Compatibility/new_ois_old_oic_new_and_old_client/sqlite'],
                                                                                    'oics': ['lab-a',
                                                                                             'lab-b'],
                                                                                    'resource': True,
                                                                                    'root': 'TestTVG2_13_Compatibility',
                                                                                    'rounds': 1,
                                                                                    'scenario': 'new_ois_old_oic_new_and_old_client',
                                                                                    'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/old_ois_new_oic_rejected/postgres': {'backend': 'postgres',
                                                                            'events': {'compatibility_binary_sources': 1,
                                                                                       'compatibility_version_rejected': 1},
                                                                            'leaves': ['TestTVG2_13_Compatibility/old_ois_new_oic_rejected/postgres'],
                                                                            'oics': ['lab-a', 'lab-b'],
                                                                            'resource': False,
                                                                            'root': 'TestTVG2_13_Compatibility',
                                                                            'rounds': 1,
                                                                            'scenario': 'old_ois_new_oic_rejected',
                                                                            'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/old_ois_new_oic_rejected/sqlite': {'backend': 'sqlite',
                                                                          'events': {'compatibility_binary_sources': 1,
                                                                                     'compatibility_version_rejected': 1},
                                                                          'leaves': ['TestTVG2_13_Compatibility/old_ois_new_oic_rejected/sqlite'],
                                                                          'oics': ['lab-a', 'lab-b'],
                                                                          'resource': False,
                                                                          'root': 'TestTVG2_13_Compatibility',
                                                                          'rounds': 1,
                                                                          'scenario': 'old_ois_new_oic_rejected',
                                                                          'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/old_ois_old_oic_new_client/postgres': {'backend': 'postgres',
                                                                              'events': {'compatibility_binary_sources': 1,
                                                                                         'compatibility_native_result': 1,
                                                                                         'compatibility_unknown_strict': 1,
                                                                                         'wire_history': 2},
                                                                              'leaves': ['TestTVG2_13_Compatibility/old_ois_old_oic_new_client/postgres'],
                                                                              'oics': ['lab-a', 'lab-b'],
                                                                              'resource': True,
                                                                              'root': 'TestTVG2_13_Compatibility',
                                                                              'rounds': 1,
                                                                              'scenario': 'old_ois_old_oic_new_client',
                                                                              'vector': 'G2-13'},
            'TestTVG2_13_Compatibility/old_ois_old_oic_new_client/sqlite': {'backend': 'sqlite',
                                                                            'events': {'compatibility_binary_sources': 1,
                                                                                       'compatibility_native_result': 1,
                                                                                       'compatibility_unknown_strict': 1,
                                                                                       'wire_history': 2},
                                                                            'leaves': ['TestTVG2_13_Compatibility/old_ois_old_oic_new_client/sqlite'],
                                                                            'oics': ['lab-a', 'lab-b'],
                                                                            'resource': True,
                                                                            'root': 'TestTVG2_13_Compatibility',
                                                                            'rounds': 1,
                                                                            'scenario': 'old_ois_old_oic_new_client',
                                                                            'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/foreign_namespace/postgres': {'backend': 'postgres',
                                                                       'events': {'commit_cleanup': 1,
                                                                                  'compatibility_v2_invalid_reply': 1,
                                                                                  'health_history': 1,
                                                                                  'wire_history': 2},
                                                                       'leaves': ['TestTVG2_13_V2WireRejection/foreign_namespace/postgres'],
                                                                       'oics': ['lab-a', 'lab-b'],
                                                                       'resource': True,
                                                                       'root': 'TestTVG2_13_V2WireRejection',
                                                                       'rounds': 1,
                                                                       'scenario': 'foreign_namespace',
                                                                       'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/foreign_namespace/sqlite': {'backend': 'sqlite',
                                                                     'events': {'commit_cleanup': 1,
                                                                                'compatibility_v2_invalid_reply': 1,
                                                                                'health_history': 1,
                                                                                'wire_history': 2},
                                                                     'leaves': ['TestTVG2_13_V2WireRejection/foreign_namespace/sqlite'],
                                                                     'oics': ['lab-a', 'lab-b'],
                                                                     'resource': True,
                                                                     'root': 'TestTVG2_13_V2WireRejection',
                                                                     'rounds': 1,
                                                                     'scenario': 'foreign_namespace',
                                                                     'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/late_check/postgres': {'backend': 'postgres',
                                                                'events': {'commit_cleanup': 1,
                                                                           'compatibility_late_check': 1,
                                                                           'health_history': 1,
                                                                           'wire_history': 2},
                                                                'leaves': ['TestTVG2_13_V2WireRejection/late_check/postgres'],
                                                                'oics': ['lab-a', 'lab-b'],
                                                                'resource': True,
                                                                'root': 'TestTVG2_13_V2WireRejection',
                                                                'rounds': 1,
                                                                'scenario': 'late_check',
                                                                'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/late_check/sqlite': {'backend': 'sqlite',
                                                              'events': {'commit_cleanup': 1,
                                                                         'compatibility_late_check': 1,
                                                                         'health_history': 1,
                                                                         'wire_history': 2},
                                                              'leaves': ['TestTVG2_13_V2WireRejection/late_check/sqlite'],
                                                              'oics': ['lab-a', 'lab-b'],
                                                              'resource': True,
                                                              'root': 'TestTVG2_13_V2WireRejection',
                                                              'rounds': 1,
                                                              'scenario': 'late_check',
                                                              'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/missing_binding/postgres': {'backend': 'postgres',
                                                                     'events': {'commit_cleanup': 1,
                                                                                'compatibility_v2_invalid_reply': 1,
                                                                                'health_history': 1,
                                                                                'wire_history': 2},
                                                                     'leaves': ['TestTVG2_13_V2WireRejection/missing_binding/postgres'],
                                                                     'oics': ['lab-a', 'lab-b'],
                                                                     'resource': True,
                                                                     'root': 'TestTVG2_13_V2WireRejection',
                                                                     'rounds': 1,
                                                                     'scenario': 'missing_binding',
                                                                     'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/missing_binding/sqlite': {'backend': 'sqlite',
                                                                   'events': {'commit_cleanup': 1,
                                                                              'compatibility_v2_invalid_reply': 1,
                                                                              'health_history': 1,
                                                                              'wire_history': 2},
                                                                   'leaves': ['TestTVG2_13_V2WireRejection/missing_binding/sqlite'],
                                                                   'oics': ['lab-a', 'lab-b'],
                                                                   'resource': True,
                                                                   'root': 'TestTVG2_13_V2WireRejection',
                                                                   'rounds': 1,
                                                                   'scenario': 'missing_binding',
                                                                   'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/unknown_enum/postgres': {'backend': 'postgres',
                                                                  'events': {'commit_cleanup': 1,
                                                                             'compatibility_v2_invalid_reply': 1,
                                                                             'health_history': 1,
                                                                             'wire_history': 2},
                                                                  'leaves': ['TestTVG2_13_V2WireRejection/unknown_enum/postgres'],
                                                                  'oics': ['lab-a', 'lab-b'],
                                                                  'resource': True,
                                                                  'root': 'TestTVG2_13_V2WireRejection',
                                                                  'rounds': 1,
                                                                  'scenario': 'unknown_enum',
                                                                  'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/unknown_enum/sqlite': {'backend': 'sqlite',
                                                                'events': {'commit_cleanup': 1,
                                                                           'compatibility_v2_invalid_reply': 1,
                                                                           'health_history': 1,
                                                                           'wire_history': 2},
                                                                'leaves': ['TestTVG2_13_V2WireRejection/unknown_enum/sqlite'],
                                                                'oics': ['lab-a', 'lab-b'],
                                                                'resource': True,
                                                                'root': 'TestTVG2_13_V2WireRejection',
                                                                'rounds': 1,
                                                                'scenario': 'unknown_enum',
                                                                'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/unsupported_contract_entry/postgres': {'backend': 'postgres',
                                                                                'events': {'compatibility_contract_entry_rejected': 1,
                                                                                           'health_history': 1,
                                                                                           'wire_history': 1},
                                                                                'leaves': ['TestTVG2_13_V2WireRejection/unsupported_contract_entry/postgres'],
                                                                                'oics': ['lab-a'],
                                                                                'resource': True,
                                                                                'root': 'TestTVG2_13_V2WireRejection',
                                                                                'rounds': 1,
                                                                                'scenario': 'unsupported_contract_entry',
                                                                                'vector': 'G2-13'},
            'TestTVG2_13_V2WireRejection/unsupported_contract_entry/sqlite': {'backend': 'sqlite',
                                                                              'events': {'compatibility_contract_entry_rejected': 1,
                                                                                         'health_history': 1,
                                                                                         'wire_history': 1},
                                                                              'leaves': ['TestTVG2_13_V2WireRejection/unsupported_contract_entry/sqlite'],
                                                                              'oics': ['lab-a'],
                                                                              'resource': True,
                                                                              'root': 'TestTVG2_13_V2WireRejection',
                                                                              'rounds': 1,
                                                                              'scenario': 'unsupported_contract_entry',
                                                                              'vector': 'G2-13'},
            'TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate/postgres': {'backend': 'postgres',
                                                                              'events': {'diagnostic_layers': 2,
                                                                                         'health_history': 1,
                                                                                         'strict_expired_rejected': 1},
                                                                              'leaves': ['TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate/postgres'],
                                                                              'oics': ['lab-a', 'lab-b'],
                                                                              'resource': True,
                                                                              'root': 'TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate',
                                                                              'rounds': 2,
                                                                              'scenario': None,
                                                                              'vector': 'G2-14'},
            'TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate/sqlite': {'backend': 'sqlite',
                                                                            'events': {'diagnostic_layers': 2,
                                                                                       'health_history': 1,
                                                                                       'strict_expired_rejected': 1},
                                                                            'leaves': ['TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate/sqlite'],
                                                                            'oics': ['lab-a', 'lab-b'],
                                                                            'resource': True,
                                                                            'root': 'TestTVG2_14_DirectoryAdmissionAndBusinessAreSeparate',
                                                                            'rounds': 2,
                                                                            'scenario': None,
                                                                            'vector': 'G2-14'}},
 'package': 'github.com/wyw-ai/loom-oi/tests/integration',
 'schema': 'g2-analysis-v1'}
