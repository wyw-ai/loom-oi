import base64
import copy
import importlib.util
import json
from pathlib import Path
import unittest


module_spec = importlib.util.spec_from_file_location('g2_analyze_under_test', Path(__file__).parent/'product/g2_analyze.py')
g2 = importlib.util.module_from_spec(module_spec)
module_spec.loader.exec_module(g2)


def encoded(value):
    raw = value if isinstance(value, bytes) else json.dumps(value, separators=(',', ':')).encode()
    return base64.b64encode(raw).decode()


def binding():
    return dict(access_id='lab-a', epoch=1, generation=1, exposure_id='lab-a/http',
                revision='rv_source', observation_revision='ob_1', contract_digest='a'*64,
                source_id='local', contract_version=1)


def wire(body, ident, at, *, direction='oic_to_ois', stage='', channel='', written=None):
    binary = isinstance(body, bytes)
    return dict(ID=ident, ConnID=2 if binary else 1, Role='provider' if binary else 'control',
        Direction=direction, Type={1:'READY',2:'DATA',3:'FIN',4:'ABORT'}[body[0]] if binary else body['type'],
        Stage=stage, ChannelID=channel if binary else body.get('channel_id',''),
        CheckID='' if binary else body.get('check_id',''), QueryID='' if binary else body.get('query_id',''),
        MessageType=2 if binary else 1, Raw=encoded(body), ReceivedNS=at,
        WriteStartedNS=at+1, WrittenNS=at+2 if written is None else written,
        Injected=False, WriteFailed=False, Dropped=False)


def history_row(events):
    return dict(access_id='lab-a', proxy=dict(PID=42, Workers=4, TransportConnections=2,
        Overflow=False, Failure='', Connections=[
            dict(ID=1,Role='control',ChannelID='',AcceptedNS=1,ClosedNS=0,Workers=2,Closed=False),
            dict(ID=2,Role='provider',ChannelID='ch_test',AcceptedNS=1,ClosedNS=0,Workers=2,Closed=False)], Events=events))


def positive_history():
    q=wire(dict(type='admission_check',seq=1,check_id='1'*32,stage='final',binding=binding(),
        effective_admission='require_ready',deadline_ms=500,channel_id='ch_test'),1,100,
        direction='ois_to_oic',stage='final')
    r=wire(dict(type='admission_result',seq=1,check_id='1'*32,binding=binding(),channel_id='ch_test',
        effective_admission='require_ready',decision_id='2'*32,readiness='ready',sample_seq=3,remaining_ms=1000,
        reason='matched_ready_status'),2,120,stage='final')
    r['WriteStartedNS'],r['WrittenNS']=160,161
    ready=wire(b'\x01',3,180,direction='ois_to_oic',channel='ch_test')
    return history_row([q,r,ready]),q,r,ready


def cleanup_record():
    observation=dict(pending_checks=0,pending_queries=0,pending_opens=0,http_waiters=0)
    return dict(origin_ns=100,finished_ns=400,target_closed_observed_ns=300,
        target=[dict(Data=None,Done=True)],
        ois=dict(channels=dict(preparing=0,open=0,closing=0),tickets=0,observation=observation.copy()),
        oic=dict(channels=0,observation=observation.copy()))


class G2WireTests(unittest.TestCase):
    def test_real_message_challenge_binding_and_reference(self):
        row,q,r,_=positive_history()
        h=g2.History([row])
        self.assertEqual(h.result(r)[2],q)
        stale=copy.deepcopy(r)
        stale['WrittenNS']=0
        stale['WriteStartedNS']=0
        self.assertEqual(h.ref(stale)[0],r)  # the immutable held copy is valid

    def test_wire_mutants_cannot_replace_real_evidence(self):
        mutations={
            'duplicate_event':lambda x:x['proxy']['Events'].append(copy.deepcopy(x['proxy']['Events'][-1])),
            'overflow':lambda x:x['proxy'].__setitem__('Overflow',True),
            'missing_pid':lambda x:x['proxy'].__setitem__('PID',0),
            'unknown_connection':lambda x:x['proxy']['Events'][1].__setitem__('ConnID',999),
            'rebound_provider':lambda x:x['proxy']['Events'][2].__setitem__('ChannelID','ch_other'),
            'fake_type':lambda x:x['proxy']['Events'][1].__setitem__('Type','heartbeat'),
            'wrong_stage':lambda x:x['proxy']['Events'][1].__setitem__('Stage','preflight'),
            'drop_written':lambda x:x['proxy']['Events'][1].__setitem__('Dropped',True),
            'write_before_receive':lambda x:x['proxy']['Events'][1].__setitem__('WriteStartedNS',1),
            'fake_injection_receipt':lambda x:x['proxy']['Events'][1].__setitem__('Injected',True),
            'base64':lambda x:x['proxy']['Events'][1].__setitem__('Raw','!bad'),
            'boolean_time':lambda x:x['proxy']['Events'][1].__setitem__('ReceivedNS',True),
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label):
                row,*_=positive_history()
                mutate(row)
                with self.assertRaises((ValueError,KeyError)):
                    g2.History([row])

    def test_reference_cannot_forge_raw_or_use_injected_positive(self):
        row,_,result,_=positive_history()
        h=g2.History([row])
        bad=copy.deepcopy(result)
        bad['Raw']=encoded({'type':'admission_result'})
        with self.assertRaisesRegex(ValueError,'immutable'):
            h.ref(bad)
        actual=row['proxy']['Events'][1]
        actual['Injected'],actual['ReceivedNS']=True,0
        h=g2.History([row])
        with self.assertRaisesRegex(ValueError,'injection'):
            h.result(actual)

    def test_response_cannot_borrow_another_binding(self):
        row,_,result,_=positive_history()
        body=json.loads(base64.b64decode(result['Raw']))
        body['binding']['generation']=2
        result['Raw']=encoded(body)
        with self.assertRaisesRegex(ValueError,'binding differs'):
            g2.History([row]).result(result)

    def test_duplicate_json_and_unrecognizable_binary_are_rejected(self):
        with self.assertRaisesRegex(ValueError,'duplicate'):
            g2.json_object(b'{"seq":1,"seq":2}')
        with self.assertRaises(ValueError):
            g2.json_object(b'{"seq":NaN}')
        event=wire(b'\x01',1,100,channel='ch_test')
        event['Raw']=encoded(b'\x01extra')
        with self.assertRaises(ValueError):
            g2.wire_body(event)


class G2CleanupTests(unittest.TestCase):
    def test_exact_original_budget(self):
        x=cleanup_record()
        x['target_closed_observed_ns']=x['origin_ns']+g2.NS_SEC
        x['finished_ns']=x['origin_ns']+2*g2.NS_SEC
        g2.cleanup(x)
        for field in ['target_closed_observed_ns','finished_ns']:
            y=copy.deepcopy(x)
            y[field]+=1
            with self.subTest(field=field), self.assertRaises(ValueError):
                g2.cleanup(y)

    def test_zero_or_missing_slots_cannot_be_fabricated(self):
        for role in ['ois','oic']:
            for field in ['pending_checks','pending_queries']:
                for value in [None,1]:
                    x=cleanup_record()
                    x[role]['observation'][field]=value
                    with self.subTest(role=role,field=field,value=value), self.assertRaises(ValueError):
                        g2.cleanup(x)
        with self.assertRaises(ValueError):
            g2.targets([dict(Data=encoded(b'leak'),Done=True)],zero=True)
        with self.assertRaises(ValueError):
            g2.targets([])


class G2CommitTests(unittest.TestCase):
    def fixture(self):
        raw,q,r,ready=positive_history()
        row=dict(event='commit_snapshot_round',final=copy.deepcopy(r),channel=dict(channel_id='ch_test',
            state='closed',close_reason='Completed',readiness_decision=dict(readiness='ready')),
            t_check_bounds_ns=[q['WriteStartedNS'],r['ReceivedNS']],
            ois_commit_bounds_ns=[r['WriteStartedNS'],ready['ReceivedNS']],
            not_ready_reduction_observed_ns=140,release_ns=150,
            target=[dict(Data=encoded(b'actual-request'),Done=True)])
        metric=dict(event='local_metric_advanced',metric='oi_observation_samples_total{result="not_ready"}',sample_started_ns=139,sample_finished_ns=140,before=1,value=2)
        return raw,[row,metric]

    def test_frozen_decision_with_a_newer_negative_can_commit(self):
        raw,rows=self.fixture()
        g2.validate_commit(rows,g2.History([raw]))

    def test_commit_causal_and_payload_mutants_fail(self):
        for key,value in [('not_ready_reduction_observed_ns',100),('release_ns',170),
                          ('t_check_bounds_ns',[120,120]),('target',[dict(Data=None,Done=True)])]:
            raw,rows=self.fixture()
            rows[0][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):
                g2.validate_commit(rows,g2.History([raw]))


class G2RegistrationTests(unittest.TestCase):
    def test_round_metadata_cannot_rename_or_multiply_one_real_trace(self):
        group=dict(scenario='query_delay',events={'query_ttl_bound':20})
        rows=[dict(event='query_ttl_bound',round=i,scenario='query_delay',query={'QueryID':format(i+1,'032x')}) for i in range(20)]
        g2.validate_rounds(group,rows)
        for mutation in ['missing_round','copied_trace','wrong_scenario','repeated_round','boolean_round']:
            changed=copy.deepcopy(rows)
            if mutation=='missing_round':
                for row in changed: row.pop('round')
            elif mutation=='copied_trace':
                for row in changed: row['query']=copy.deepcopy(rows[0]['query'])
            elif mutation=='wrong_scenario':
                for row in changed: row['scenario']='cache_wait'
            elif mutation=='repeated_round':
                changed[-1]['round']=0
            else:
                changed[0]['round']=False
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):
                g2.validate_rounds(group,changed)

    def test_inventory_has_all_leaves_and_resource_owners(self):
        spec=g2.expected_spec()
        self.assertEqual(sum(len(g['leaves']) for g in spec['groups'].values()),130)
        self.assertEqual(sum(g['resource'] for g in spec['groups'].values()),106)
        self.assertEqual(sum(not g['resource'] for g in spec['groups'].values()),2)

    def test_deleted_manifest_case_and_lowered_count_are_rejected(self):
        spec=g2.expected_spec()
        spec['groups'].pop(next(iter(spec['groups'])))
        with self.assertRaises(ValueError):
            g2.analyze_g2(spec,[],{})
        spec=g2.expected_spec()
        first=next(iter(spec['groups'].values()))
        first['events'][next(iter(first['events']))]=0
        with self.assertRaises(ValueError):
            g2.analyze_g2(spec,[],{})

    def test_all_go_pass_without_raw_is_never_evidence(self):
        spec=g2.expected_spec()
        results={}
        for test,g in spec['groups'].items():
            for name in g['leaves']+[test,g['root']]:
                results[spec['package'],name]=['pass']
        summary,problems=g2.analyze_g2(spec,[],results)
        self.assertEqual(summary['result'],'fail')
        self.assertTrue(problems)
        self.assertEqual(summary['unimplemented'],[])

    def test_untrusted_output_provenance_is_not_accepted(self):
        spec=g2.expected_spec()
        test=next(iter(spec['groups']))
        row=dict(test=test,schema='g2-behavior-v1',kind='G2',backend='sqlite',vector='G2-01',event='environment',observed_ns=1)
        _,problems=g2.analyze_g2(spec,[('wrong-package',test,row)],{})
        self.assertTrue(any('provenance' in p for p in problems))



def exposition(failures=0):
    lines=[];keys=[];vocab={'result':['timeout','write_error']}
    counters=['oi_metrics_exports_total']+[n for n in g2.EXPORT_COUNTERS if n!='oi_metrics_exports_total']
    for name in counters:
        lines += ['# HELP '+name+' Test counter.','# TYPE '+name+' counter']
        labels=['result="timeout"','result="write_error"'] if name==counters[0] else ['']
        for label in labels:
            metric=name+('{'+label+'}' if label else '')
            lines.append(metric+' '+str(failures if label=='result="timeout"' else 0))
            keys.append(name+'{'+label+'}')
    for i in range(6):
        name=g2.EXPORT_HISTOGRAMS[i]
        lines+=['# HELP '+name+' Test duration.','# TYPE '+name+' histogram']
        for bound in ['0.001','0.005','0.025','0.1','0.5','2','10','+Inf']:
            metric=name+'_bucket{le="'+bound+'"}'
            lines.append(metric+' 0');keys.append(metric)
        for suffix in ['sum','count']:
            lines.append(name+'_'+suffix+' 0');keys.append(name+'_'+suffix+'{}')
    return dict(status=200,body='\n'.join(lines)+'\n',content_type='text/plain; version=0.0.4; charset=utf-8',started_ns=1,completed_ns=2,series_keys=sorted(keys),label_values=vocab)


class G2ExportTests(unittest.TestCase):
    def test_parse_actual_body_and_histogram(self):
        parsed=g2.parse_exposition(exposition(3))
        self.assertEqual(g2.export_failure_count(parsed),3)

    def test_body_cannot_hide_behind_valid_go_series_summary(self):
        for mutation in ['duplicate','nan','entity','negative','noncumulative','wrong_count','missing_help','unknown_family','missing_newline','crlf','oversize','type','empty_label','timestamp','metadata_forgery']:
            x=exposition()
            if mutation=='duplicate':x['body']+='oi_observation_samples_total 0\n'
            elif mutation=='nan':x['body']=x['body'].replace('oi_observation_samples_total 0','oi_observation_samples_total NaN')
            elif mutation=='entity':x['body']=x['body'].replace('oi_observation_samples_total 0','oi_observation_samples_total{subject="alice"} 0')
            elif mutation=='negative':x['body']=x['body'].replace('oi_observation_samples_total 0','oi_observation_samples_total -1')
            elif mutation=='noncumulative':x['body']=x['body'].replace('le="0.005"} 0','le="0.005"} 1')
            elif mutation=='wrong_count':x['body']=x['body'].replace('oi_probe_queue_wait_seconds_count 0','oi_probe_queue_wait_seconds_count 2')
            elif mutation=='missing_help':x['body']=x['body'].replace('# HELP oi_observation_samples_total Test counter.\n','')
            elif mutation=='unknown_family':x['body']+='oi_unbound_total 1\n'
            elif mutation=='missing_newline':x['body']=x['body'][:-1]
            elif mutation=='crlf':x['body']=x['body'].replace('\n','\r\n')
            elif mutation=='oversize':x['body']='x'*65537
            elif mutation=='type':x['body']=x['body'].replace('# TYPE oi_observation_samples_total counter','# TYPE oi_observation_samples_total gauge')
            elif mutation=='empty_label':x['body']=x['body'].replace('result="timeout"','result=""')
            elif mutation=='timestamp':x['body']=x['body'].replace('oi_observation_samples_total 0','oi_observation_samples_total 0 1000')
            else:x['series_keys'].pop()
            with self.subTest(mutation=mutation),self.assertRaises((ValueError,g2.InvalidOperation)):
                g2.parse_exposition(x)

    def fixture(self):
        before=exposition();reject=dict(status=429,body='overloaded\n',started_ns=110,completed_ns=120)
        bounded=exposition(2);bounded.update(started_ns=200,completed_ns=250)
        after=exposition(2);after.update(started_ns=310,completed_ns=320)
        writes=[dict(socket=i,connected_ns=100,started_ns=101,completed_ns=105,planned_bytes=140000,written_bytes=140000,error='') for i in range(2)]
        return [writes,before,reject,bounded,after,300]

    def test_slow_input_requires_real_rejection_and_failures_before_close(self):
        g2.slow_scrapes(*self.fixture())
        for mutation in ['one_socket','no_bytes','late_write','no_rejection','late_failures','only_one_failure','late_recovery','cardinality']:
            args=self.fixture()
            if mutation=='one_socket':args[0].pop()
            elif mutation=='no_bytes':args[0][0]['written_bytes']=0
            elif mutation=='late_write':args[0][1]['completed_ns']=2_000_000_000
            elif mutation=='no_rejection':args[2]['status']=200
            elif mutation=='late_failures':args[3]['completed_ns']=301
            elif mutation=='only_one_failure':args[3]['body']=args[3]['body'].replace('result="timeout"} 2','result="timeout"} 1')
            elif mutation=='late_recovery':args[4]['completed_ns']=3_000_000_000
            else:args[4]['label_values']['result'].append('entity_id')
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):g2.slow_scrapes(*args)


class G2SafetyTests(unittest.TestCase):
    def security(self):
        raw,q,r,_=positive_history();raw['proxy']['Events'].pop()
        r.update(WriteStartedNS=160,WrittenNS=161)
        row=dict(event='security_final_round',scenario='credential_revoked',final=copy.deepcopy(r),
            channel=dict(channel_id='ch_test',state='closed',close_reason='Revoked'),
            consumer_abort=dict(Type=4,Reason='Revoked'),target=[dict(Data=None,Done=True)],
            before=dict(revision='rv_source'),security_action_started_ns=140,cleanup_origin_ns=180,cleanup_finished_ns=200,
            admin_action=dict(method='DELETE',resource='token',status=200,started_ns=140,completed_ns=150))
        return raw,row

    def test_security_guard_wins_frozen_positive(self):
        raw,row=self.security();g2.validate_security([row],g2.History([raw]))

    def test_security_guard_cannot_use_wrong_channel_payload_or_late_action(self):
        for mutation in ['late_action','before_result','payload','wrong_reason','wrong_abort','ready','unreleased','different_revision']:
            raw,row=self.security()
            if mutation=='late_action':row['security_action_started_ns']=190
            elif mutation=='before_result':row['security_action_started_ns']=119
            elif mutation=='payload':row['target'][0]['Data']=encoded(b'secret')
            elif mutation=='wrong_reason':row['channel']['close_reason']='Completed'
            elif mutation=='wrong_abort':row['consumer_abort']['Reason']='Completed'
            elif mutation=='ready':raw['proxy']['Events'].append(wire(b'\x01',3,175,direction='ois_to_oic',channel='ch_test'))
            elif mutation=='unreleased':raw['proxy']['Events'][1].update(WriteStartedNS=0,WrittenNS=0)
            else:row['before']['revision']='rv_other'
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):g2.validate_security([row],g2.History([raw]))

    def loss(self):
        raw,q,r,_=positive_history();raw['proxy']['Events'].pop();r.update(WriteStartedNS=0,WrittenNS=0,Dropped=True)
        row=dict(event='final_loss_round',scenario='final_response_drop',final=copy.deepcopy(r),
            channel=dict(channel_id='ch_test',state='closed',close_reason='ReadinessCheckFailed',readiness_decision=dict(reason='timeout')),
            consumer_abort=dict(Type=4,Reason='ReadinessCheckFailed'),target=[dict(Data=None,Done=True)],cleanup_origin_ns=600_000_000,cleanup_finished_ns=600_000_010)
        return raw,row

    def test_lost_final_never_commits_and_retains_first_cause(self):
        raw,row=self.loss();g2.validate_lifecycle([row],g2.History([raw]))
        for mutation in ['wrong_first','forwarded','late_cleanup','target_payload']:
            raw,row=self.loss()
            if mutation=='wrong_first':row['channel']['readiness_decision']['reason']='unavailable'
            elif mutation=='forwarded':raw['proxy']['Events'][1].update(Dropped=False,WriteStartedNS=130,WrittenNS=131)
            elif mutation=='late_cleanup':row['cleanup_finished_ns']=3_000_000_000
            else:row['target'][0]['Data']=encoded(b'forbidden')
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):g2.validate_lifecycle([row],g2.History([raw]))

    def test_preflight_capacity_has_no_channel_side_effect(self):
        stats=dict(channels=dict(preparing=0,open=0,closing=0),tickets=0,observation=dict(pending_opens=1,http_waiters=16,pending_checks=1))
        g2.slots(stats,1,16,1)
        for key in ['preparing','open','closing']:
            bad=copy.deepcopy(stats);bad['channels'][key]=1
            with self.subTest(key=key),self.assertRaises(ValueError):g2.slots(bad,1,16,1)
        bad=copy.deepcopy(stats);bad['tickets']=1
        with self.assertRaises(ValueError):g2.slots(bad,1,16,1)

    def test_actual_cli_failure_is_not_a_killed_timeout(self):
        value=dict(TimedOut=False,ExitCode=1,StartedNS=1,FinishedNS=2,Stdout='',Stderr='unknown ABORT')
        g2.cli_result(value)
        for key,v in [('TimedOut',True),('ExitCode',0),('Stdout','payload'),('Stderr',''),('FinishedNS',0)]:
            bad=dict(value);bad[key]=v
            with self.subTest(key=key),self.assertRaises(ValueError):g2.cli_result(bad)

    def test_http_errors_and_cancel_cannot_create_channel(self):
        out=dict(status=409,code='TargetNotReady',open={})
        g2.http_outcome(out,409,'TargetNotReady')
        out['open']={'channel_id':'ch_hidden'}
        with self.assertRaises(ValueError):g2.http_outcome(out,409,'TargetNotReady')
        out=dict(status=0,error='context canceled',open={},StartedNS=1,CompletedNS=2)
        g2.cancelled(out)
        out['status']=201
        with self.assertRaises(ValueError):g2.cancelled(out)



class G2TTLTests(unittest.TestCase):
    def fixture(self):
        q=wire(dict(type='observation_query',seq=1,query_id='1'*32,binding=binding()),1,100_000_000,direction='ois_to_oic')
        q.update(WriteStartedNS=200_000_000,WrittenNS=201_000_000)
        r=wire(dict(type='observation_snapshot',seq=1,query_id='1'*32,binding=binding(),remaining_ms=1000,sample_seq=3),2,210_000_000)
        r.update(WriteStartedNS=220_000_000,WrittenNS=221_000_000)
        q2=wire(dict(type='observation_query',seq=2,query_id='2'*32,binding=binding()),3,250_000_000,direction='ois_to_oic')
        guard=wire(dict(type='observation_snapshot',seq=2,query_id='2'*32,binding=binding(),remaining_ms=900,sample_seq=3),4,300_000_000)
        guard.update(WriteStartedNS=0,WrittenNS=0)
        h=history_row([q,r,q2,guard]);h['proxy']['Connections']=h['proxy']['Connections'][:1]
        row=dict(event='query_ttl_bound',scenario='query_delay',round=0,query=copy.deepcopy(q),response=copy.deepcopy(r),response_written=copy.deepcopy(r),next_response_held=copy.deepcopy(guard),
            query_delay_ms=100,response_delay_ms=0,view_request_started_ns=350_000_000,view_request_ended_ns=351_000_000,remaining_upper_bound_ms=750,
            view=dict(readiness_view=dict(remaining_ms=750,last_reported=dict(sample_seq=3))))
        return h,row

    def test_complete_query_rtt_and_cache_age_are_subtracted(self):
        raw,row=self.fixture();g2.validate_ttl([row],g2.History([raw]))

    def test_ttl_raw_mutants_cannot_invent_delay_or_extend_lifetime(self):
        for mutation in ['query_delay','remaining','fake_bound','wrong_sample','guard_forwarded','query_missing','cross_query','wrong_declared_delay','response_not_written']:
            raw,row=self.fixture()
            if mutation=='query_delay':raw['proxy']['Events'][0].update(WriteStartedNS=100_000_001,WrittenNS=100_000_002)
            elif mutation=='remaining':row['view']['readiness_view']['remaining_ms']=751
            elif mutation=='fake_bound':row['remaining_upper_bound_ms']=751
            elif mutation=='wrong_sample':row['view']['readiness_view']['last_reported']['sample_seq']=4
            elif mutation=='guard_forwarded':raw['proxy']['Events'][3].update(WriteStartedNS=340_000_000,WrittenNS=341_000_000)
            elif mutation=='query_missing':raw['proxy']['Events'].pop(0)
            elif mutation=='cross_query':row['response']['QueryID']='2'*32
            elif mutation=='wrong_declared_delay':row['query_delay_ms']=0
            else:raw['proxy']['Events'][1].update(WriteStartedNS=0,WrittenNS=0)
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):g2.validate_ttl([row],g2.History([raw]))


class G2ObservationTests(unittest.TestCase):
    def attempts(self):
        out=[]
        for state in ('ready','not_ready'):
            for idx,stage in enumerate(['running','timeout']):
                view=dict(readiness=state,readiness_view=dict(sample_seq=2,remaining_ms=1000-idx*100,last_attempt=dict(state='running' if idx==0 else 'idle',outcome='' if idx==0 else 'timeout')))
                out.append(dict(event='sample_retained',expected_readiness=state,stage=stage,observed_ns=idx+1,sample_seq=2,initial_remaining_ms=1000,view=view))
            out.append(dict(event='sample_expired',expected_readiness=state,sample_seq=2,elapsed_ns=1_000_000_000,view=dict(readiness='unknown',readiness_view=dict(remaining_ms=0,last_reported=dict(sample_seq=2,readiness=state)))))
        return out

    def test_running_timeout_keeps_sample_but_does_not_renew_it(self):
        rows=self.attempts();g2.validate_attempts(rows)
        for mutation in ['replace','renew','expire_history','expire_effective','late_expiry','timeout_state']:
            rows=self.attempts()
            if mutation=='replace':rows[1]['view']['readiness_view']['sample_seq']=3
            elif mutation=='renew':rows[1]['view']['readiness_view']['remaining_ms']=1001
            elif mutation=='expire_history':rows[2]['view']['readiness_view']['last_reported']['sample_seq']=3
            elif mutation=='expire_effective':rows[2]['view']['readiness_view']['sample_seq']=2
            elif mutation=='late_expiry':rows[2]['elapsed_ns']=1_251_000_000
            else:rows[1]['view']['readiness_view']['last_attempt']['state']='running'
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):g2.validate_attempts(rows)

    def test_health_snapshot_rejects_missing_or_reused_request_identity(self):
        record=dict(ID=1,Version=1,StartedNS=10,HeaderStartedNS=15,HeaderWrittenNS=16,CancelledNS=0,FinishedNS=20)
        row=dict(event='health_history',exposure_id='lab-a/http',health=dict(Overflow=False,PID=3,Records=[record]))
        self.assertEqual(len(g2.health_index([row])['lab-a/http']),1)
        for mutation in ['duplicate','overflow','pid','time','header']:
            changed=copy.deepcopy(row)
            if mutation=='duplicate':changed['health']['Records'].append(record)
            elif mutation=='overflow':changed['health']['Overflow']=True
            elif mutation=='pid':changed['health']['PID']=0
            elif mutation=='time':changed['health']['Records'][0]['FinishedNS']=9
            else:changed['health']['Records'][0]['HeaderStartedNS']=0
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):g2.health_index([changed])



class G2GroupRoutingTests(unittest.TestCase):
    def fixture(self):
        spec=g2.expected_spec();test='TestTVG2_02_AttemptDoesNotReplaceSample/sqlite';group=spec['groups'][test]
        rows=G2ObservationTests().attempts()+[dict(event='health_history',exposure_id='lab-a/http',health=dict(Overflow=False,PID=5,Records=[]))]
        for i,r in enumerate(rows):r.update(schema='g2-behavior-v1',kind='G2',vector='G2-02',test=test,backend='sqlite',observed_ns=i+1)
        resources=[dict(kind='resources_baseline',recorded_ns=10,oics={'lab-a':{},'lab-b':{}}),dict(kind='resources_begin',started_ns=20,deadline_ns=2_000_000_020),dict(kind='resources')]
        for r in resources:r.update(schema='b4-resource-v2',test=test,backend='sqlite')
        records=[(spec['package'],test,r) for r in rows+resources]
        results={(spec['package'],name):['pass'] for name in group['leaves']+[test,group['root']]}
        resource=dict(result='pass',completed_checks={test:1})
        return spec,test,records,results,resource

    def test_one_complete_component_is_not_whole_suite_pass(self):
        spec,test,records,results,resource=self.fixture()
        summary,errors=g2.analyze_g2(spec,records,results,resource)
        self.assertEqual(summary['groups'][test]['result'],'pass')
        self.assertEqual(summary['result'],'fail')
        self.assertTrue(errors)

    def test_raw_group_requires_go_provenance_counts_and_common_resource_result(self):
        for mutation in ['skip','repeat','missing_event','bad_schema','fake_vector','bad_backend','extra_role','budget','duplicate_baseline','no_common_resources','wrong_resource_test','wrong_event_owner']:
            spec,test,records,results,resource=self.fixture()
            if mutation=='skip':results[spec['package'],test]=['skip']
            elif mutation=='repeat':results[spec['package'],test]=['pass','pass']
            elif mutation=='missing_event':records.pop(0)
            elif mutation=='bad_schema':records[0][2]['schema']='other'
            elif mutation=='fake_vector':records[0][2]['vector']='G2-01'
            elif mutation=='bad_backend':records[0][2]['backend']='postgres'
            elif mutation=='extra_role':records[-3][2]['oics']['lab-c']={}
            elif mutation=='budget':records[-2][2]['deadline_ns']+=1
            elif mutation=='duplicate_baseline':records.append(copy.deepcopy(records[-3]))
            elif mutation=='no_common_resources':resource=None
            elif mutation=='wrong_resource_test':resource['completed_checks']={}
            else:records[0]=(spec['package'],test+'/forged',records[0][2])
            with self.subTest(mutation=mutation):
                summary,errors=g2.analyze_g2(spec,records,results,resource)
                self.assertEqual(summary['groups'][test]['result'],'fail')
                self.assertTrue(errors)


class G2EntityTests(unittest.TestCase):
    def fixture(self):
        rows=[]
        for role in ['ois','oic']:
            rows.append(dict(event='export_initial',role=role,exposition=exposition()))
        for i in range(32):
            request='native-entity-'+format(i,'02d')
            rows.append(dict(event='export_entity',index=i,target='127.0.0.1:'+str(2000+i),subject='subject-'+str(i),channel_id='ch_'+str(i),revision='rv_'+str(i),exposure_id='lab-a/export',request=request,response='target-entity-'+format(i,'02d')+':'+request,target_record=dict(Data=encoded(request.encode()),Done=True,EOF=True),observed_ns=100+i*10))
            for role in ['ois','oic']:
                x=exposition(i);x.update(started_ns=101+i*10,completed_ns=102+i*10)
                rows.append(dict(event='export_after_entity',index=i,role=role,exposition=x))
        return rows

    def test_real_distinct_entities_do_not_expand_series(self):
        g2.validate_export(self.fixture())

    def test_entity_mutants_are_not_hidden_by_summary_counts(self):
        for mutation in ['duplicate_target','duplicate_channel','missing_role','wrong_bytes','counter_reset','entity_leak']:
            rows=self.fixture()
            if mutation=='duplicate_target':rows[5]['target']=rows[2]['target']
            elif mutation=='duplicate_channel':rows[5]['channel_id']=rows[2]['channel_id']
            elif mutation=='missing_role':rows.pop(3)
            elif mutation=='wrong_bytes':rows[2]['target_record']['Data']=encoded(b'other')
            elif mutation=='counter_reset':rows[-1]['exposition']['body']=rows[-1]['exposition']['body'].replace('result="timeout"} 31','result="timeout"} 0')
            else:rows[-1]['exposition']['body']+='# HELP extra 127.0.0.1:2031\n'
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):g2.validate_export(rows)


class G2SourceTests(unittest.TestCase):
    def test_legacy_inventory_cannot_be_replaced_by_a_claimed_sha(self):
        artifact=dict(source_sha=g2.LEGACY_SHA,archive_sha256=g2.LEGACY_ARCHIVE_SHA256,actual_build=True,source_files=[])
        with self.assertRaisesRegex(ValueError,'inventory'):
            g2.legacy_source(dict(legacy=artifact,combination='combo'),'combo')
        artifact['actual_build']=False
        with self.assertRaisesRegex(ValueError,'frozen'):
            g2.legacy_source(dict(legacy=artifact,combination='combo'),'combo')

    def test_saturation_round_cannot_be_relabelled_as_another_repeat(self):
        group=dict(scenario='round_2',events={'saturation_complete':1})
        row=dict(event='saturation_complete',round=2)
        g2.validate_rounds(group,[row])
        row['round']=1
        with self.assertRaises(ValueError):g2.validate_rounds(group,[row])


if __name__=='__main__':
    unittest.main()
