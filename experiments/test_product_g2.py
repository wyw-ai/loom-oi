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



class G2CompetitionWindowTests(unittest.TestCase):
    def fixture(self):
        second=g2.NS_SEC
        common=dict(window_start_ns=second,prepare_started_ns=second+100,
                    prepare_deadline_ns=4*second+100,prepared_ns=3*second,
                    slow_input_applied_ns=2*second)
        full=dict(common,sample=dict(started_ns=2*second+100,completed_ns=3*second-100))
        end=dict(common,window_end_ns=62*second)
        return full,end

    def test_healthy_preparation_does_not_consume_slow_window(self):
        full,end=self.fixture()
        self.assertEqual(g2.competition_window(full,end),(g2.NS_SEC,62*g2.NS_SEC))

    def test_overall_sixty_seconds_cannot_hide_short_slow_input(self):
        full,end=self.fixture()
        end['window_end_ns']=61*g2.NS_SEC
        with self.assertRaisesRegex(ValueError,'slow competition input'):
            g2.competition_window(full,end)
        end['window_end_ns']=62*g2.NS_SEC-1
        with self.assertRaisesRegex(ValueError,'slow competition input'):
            g2.competition_window(full,end)

    def test_original_preparation_deadline_and_actual_sample_must_hold(self):
        for mutation in ('renewed','before_start','slow_before_preparation','sample_before_slow',
                         'sample_reversed','sample_after_prepared','returned_after_deadline'):
            full,end=self.fixture()
            if mutation=='renewed':full['prepare_deadline_ns']=end['prepare_deadline_ns']=5*g2.NS_SEC
            elif mutation=='before_start':full['prepare_started_ns']=end['prepare_started_ns']=1
            elif mutation=='slow_before_preparation':full['slow_input_applied_ns']=end['slow_input_applied_ns']=g2.NS_SEC
            elif mutation=='sample_before_slow':full['sample']['started_ns']=g2.NS_SEC
            elif mutation=='sample_reversed':full['sample']['completed_ns']=2*g2.NS_SEC
            elif mutation=='sample_after_prepared':full['sample']['completed_ns']=end['prepared_ns']+1
            else:full['prepared_ns']=end['prepared_ns']=end['prepare_deadline_ns']+1
            with self.subTest(mutation=mutation),self.assertRaisesRegex(ValueError,'preparation'):
                g2.competition_window(full,end)

    def test_missing_forged_or_mismatched_raw_anchors_cannot_pass(self):
        fields=('prepare_started_ns','prepare_deadline_ns','prepared_ns','slow_input_applied_ns')
        for field in fields:
            for mutation in ('missing_full','missing_end','mismatch','boolean','zero'):
                full,end=self.fixture()
                if mutation=='missing_full':full.pop(field)
                elif mutation=='missing_end':end.pop(field)
                elif mutation=='mismatch':full[field]+=1
                else:full[field]=end[field]=True if mutation=='boolean' else 0
                with self.subTest(field=field,mutation=mutation),self.assertRaises((ValueError,KeyError)):
                    g2.competition_window(full,end)

    def test_full_critical_cycle_cannot_precede_declared_slow_input(self):
        # Exact causal10 PG round_1 timing anchors. Moving only the declared
        # slow/full/prepared anchors after cycle 0 used to preserve both 3s/60s
        # checks while placing all its critical work before the slow input.
        origin=258341769200
        common=dict(window_start_ns=origin,prepare_started_ns=origin,
                    prepare_deadline_ns=origin+3*g2.NS_SEC,
                    slow_input_applied_ns=origin+296714400,
                    prepared_ns=origin+740563800)
        full=dict(common,sample=dict(started_ns=origin+737528100,
                                     completed_ns=origin+740563800))
        end=dict(common,window_end_ns=319130930100)
        cycle=dict(backlog_before=dict(started_ns=origin+766588600),
                   native_started_ns=origin+769474200,
                   native_completed_ns=259120097400,
                   revoke_started_ns=259120097400,
                   revoke_returned_ns=259122696700,
                   backlog_after=dict(completed_ns=259125295800))
        g2.competition_window(full,end)
        g2.competition_cycle_window(cycle,end['prepared_ns'],end['window_end_ns'])
        slow=cycle['backlog_after']['completed_ns']+1
        for row in (full,end):
            row['slow_input_applied_ns']=slow
            row['prepared_ns']=slow+3
        full['sample'].update(started_ns=slow+1,completed_ns=slow+2)
        # The original window predicates still hold; the cycle must not pass.
        g2.competition_window(full,end)
        with self.assertRaisesRegex(ValueError,'prepared slow-input'):
            g2.competition_cycle_window(cycle,end['prepared_ns'],end['window_end_ns'])

    def test_each_cycle_must_start_at_or_after_preparation(self):
        for cycle_id in (0,1,19):
            row=dict(cycle=cycle_id,backlog_before=dict(started_ns=100),
                     native_started_ns=101,native_completed_ns=102,
                     revoke_started_ns=103,revoke_returned_ns=104,
                     backlog_after=dict(completed_ns=105))
            g2.competition_cycle_window(row,100,105)
            with self.subTest(cycle=cycle_id),self.assertRaisesRegex(ValueError,'prepared slow-input'):
                g2.competition_cycle_window(row,101,105)


class G2CompetitionVictimInputTests(unittest.TestCase):
    def test_new_event_is_required_and_bound_to_its_registered_round(self):
        groups=[g for g in g2.EXPECTED['groups'].values() if g['vector']=='TV-G2-11']
        self.assertEqual(len(groups),6)
        for group in groups:
            self.assertEqual(group['events']['competition_victim_input'],
                             dict(exact_from='competition_complete.cycles',min=20,max=512))
            round_id=int(group['scenario'][6:])
            good=dict(event='competition_victim_input',round=round_id)
            g2.validate_rounds(group,[good])
            for bad in (dict(event=good['event']),dict(good,round=True),dict(good,round=round_id+1)):
                with self.subTest(group=group['scenario'],bad=bad),self.assertRaisesRegex(ValueError,'registered repeat'):
                    g2.validate_rounds(group,[bad])

    def fixture(self):
        at=g2.NS_SEC;ms=g2.NS_MS;b=binding()
        rv=dict(authority='directory_snapshot',source='http_status_v1',revision=b['revision'],
                generation=b['generation'],observation_revision=b['observation_revision'],
                contract_digest=b['contract_digest'],sample_seq=3,remaining_ms=502,reason='matched_ready_status')
        row=dict(cycle=0,token_id='tok_one',channel_id='ch_test',code='',lookup_attempts=1,
            preparation_started_ns=at,deadline_ns=at+3*g2.NS_SEC,lookup_started_ns=at+ms,
            lookup_completed_ns=at+2*ms,selected_ns=at+2*ms,conservative_remaining_ns=500*ms,
            dial_started_ns=at+3*ms,dial_completed_ns=at+10*ms,observed_ns=at+11*ms,
            view=dict(exposure_id='lab-a/http',access_id='lab-a',access_online=True,
                      readiness='ready',revision=b['revision'],readiness_view=rv))
        cycle=dict(cycle=0,revoke_token_id='tok_one',victim_channel_id='ch_test',
                   slow_writes=[dict(connected_ns=at+20*ms),dict(connected_ns=at+20*ms+1)])
        events=[wire(dict(type='observation_query',seq=1,query_id='9'*32,binding=b,deadline_ms=500),
                     1,at-10*ms,direction='ois_to_oic'),
                wire(dict(type='observation_snapshot',seq=1,query_id='9'*32,binding=b,
                          readiness='ready',sample_seq=3,remaining_ms=1000,reason='matched_ready_status'),
                     2,at-5*ms)]
        for i,stage in enumerate(('preflight','final')):
            cid='' if i==0 else 'ch_test';check=str(i+1)*32
            events.append(wire(dict(type='admission_check',seq=i+2,check_id=check,stage=stage,
                binding=b,effective_admission='best_effort',deadline_ms=500,channel_id=cid),2*i+3,
                at+(4+i*2)*ms,direction='ois_to_oic',stage=stage))
            events.append(wire(dict(type='admission_result',seq=i+2,check_id=check,binding=b,
                effective_admission='best_effort',channel_id=cid,readiness='ready',reason='matched_ready_status',
                sample_seq=3+i,remaining_ms=800,**({'decision_id':'a'*32} if i else {})),2*i+4,
                at+(5+i*2)*ms,stage=stage))
        return row,cycle,history_row(events)

    def test_exact_conservative_boundary_and_new_samples_are_valid(self):
        row,cycle,raw=self.fixture()
        self.assertEqual(g2.competition_victim_input(row,cycle,g2.History([raw])),('1'*32,'2'*32))
        row['view']['readiness_view']['remaining_ms']+=1
        row['conservative_remaining_ns']+=g2.NS_MS
        row['selected_ns']+=1;row['conservative_remaining_ns']-=1
        g2.competition_victim_input(row,cycle,g2.History([raw]))

    def test_zero_floor_and_full_lookup_budget_cannot_be_forged(self):
        for change in ('floor_millisecond','lookup_nanosecond','selection_nanosecond','claimed_bound','negative'):
            row,cycle,raw=self.fixture()
            if change=='floor_millisecond':row['view']['readiness_view']['remaining_ms']-=1
            elif change=='lookup_nanosecond':row['lookup_started_ns']-=1;row['conservative_remaining_ns']-=1
            elif change=='selection_nanosecond':row['selected_ns']+=1;row['conservative_remaining_ns']-=1
            elif change=='claimed_bound':row['conservative_remaining_ns']+=g2.NS_MS
            else:row['view']['readiness_view']['remaining_ms']=1;row['conservative_remaining_ns']=0
            with self.subTest(change=change),self.assertRaisesRegex(ValueError,'conservative 500ms'):
                g2.competition_victim_input(row,cycle,g2.History([raw]))

    def test_directory_history_or_wrong_identity_is_not_current_input(self):
        changes={'offline':lambda r:r['view'].__setitem__('access_online',False),
            'unknown':lambda r:r['view'].__setitem__('readiness','unknown'),
            'wrong_access':lambda r:r['view'].__setitem__('access_id','lab-b'),
            'wrong_target':lambda r:r['view'].__setitem__('exposure_id','lab-a/alt'),
            'authority':lambda r:r['view']['readiness_view'].__setitem__('authority','final'),
            'source':lambda r:r['view']['readiness_view'].__setitem__('source','tcp'),
            'reason':lambda r:r['view']['readiness_view'].__setitem__('reason','expired'),
            'historical_only':lambda r:r['view']['readiness_view'].pop('sample_seq'),
            'zero_sample':lambda r:r['view']['readiness_view'].__setitem__('sample_seq',0),
            'wrong_generation':lambda r:r['view']['readiness_view'].__setitem__('generation',2),
            'wrong_ob_revision':lambda r:r['view']['readiness_view'].__setitem__('observation_revision','ob_2'),
            'wrong_digest':lambda r:r['view']['readiness_view'].__setitem__('contract_digest','b'*64),
            'wrong_token':lambda r:r.__setitem__('token_id','tok_other'),
            'wrong_channel':lambda r:r.__setitem__('channel_id','ch_other'),
            'wrong_cycle':lambda r:r.__setitem__('cycle',True),
            'failure':lambda r:r.__setitem__('code','ReadinessCheckFailed'),
            'input_failure':lambda r:r.__setitem__('input_error','deadline')}
        for name,mutate in changes.items():
            row,cycle,raw=self.fixture();mutate(row)
            with self.subTest(change=name),self.assertRaises((ValueError,KeyError)):
                g2.competition_victim_input(row,cycle,g2.History([raw]))

    def test_selection_and_single_dial_share_original_deadline(self):
        for change in ('renewed_deadline','selection_after_deadline','late_dial','reversed_lookup',
                       'future_record','late_record','too_many_lookups','bool_attempts'):
            row,cycle,raw=self.fixture()
            if change=='renewed_deadline':row['deadline_ns']+=1
            elif change=='selection_after_deadline':row['selected_ns']=row['deadline_ns']+1
            elif change=='late_dial':row['dial_completed_ns']=row['deadline_ns']+1
            elif change=='reversed_lookup':row['lookup_completed_ns']=row['lookup_started_ns']-1
            elif change=='future_record':row['observed_ns']=row['dial_completed_ns']-1
            elif change=='late_record':row['observed_ns']=cycle['slow_writes'][0]['connected_ns']+1
            elif change=='too_many_lookups':row['lookup_attempts']=513
            else:row['lookup_attempts']=True
            with self.subTest(change=change),self.assertRaises(ValueError):
                g2.competition_victim_input(row,cycle,g2.History([raw]))

    def test_selected_sample_and_lifetime_require_real_delivered_snapshot(self):
        for change in ('invented_sample','inflated_remaining','missing_snapshot','dropped_snapshot','late_snapshot'):
            row,cycle,raw=self.fixture();events=raw['proxy']['Events']
            if change=='invented_sample':row['view']['readiness_view']['sample_seq']=50
            elif change=='inflated_remaining':
                row['view']['readiness_view']['remaining_ms']=2000
                row['conservative_remaining_ns']=(2000-1)*g2.NS_MS-(row['selected_ns']-row['lookup_started_ns'])
            elif change=='missing_snapshot':events.pop(1)
            elif change=='dropped_snapshot':events[1].update(Dropped=True,WrittenNS=0,WriteStartedNS=0)
            else:events[1].update(WriteStartedNS=row['lookup_completed_ns']+1,WrittenNS=row['lookup_completed_ns']+2)
            with self.subTest(change=change),self.assertRaisesRegex(ValueError,'delivered source snapshot'):
                g2.competition_victim_input(row,cycle,g2.History([raw]))

    def test_actual_wire_dial_cannot_be_retried_or_substituted(self):
        for change in ('missing_final','duplicate_preflight','failed_response','unwritten',
                       'wrong_binding','outside_dial','wrong_mode','final_changed_sample_allowed'):
            row,cycle,raw=self.fixture();events=raw['proxy']['Events']
            if change=='missing_final':events[:]=events[:4]
            elif change=='duplicate_preflight':
                extra=wire(dict(type='admission_check',seq=4,check_id='3'*32,stage='preflight',
                    binding=binding(),effective_admission='best_effort',deadline_ms=500),7,
                    row['dial_started_ns']+5*g2.NS_MS,direction='ois_to_oic',stage='preflight')
                events.append(extra)
            elif change=='unwritten':events[3]['WrittenNS']=0;events[3]['WriteStartedNS']=0
            else:
                index=4 if change in ('wrong_binding','wrong_mode') else 5
                obj=json.loads(base64.b64decode(events[index]['Raw']))
                if change=='failed_response':obj['code']='ReadinessCheckFailed'
                elif change=='wrong_binding':obj['binding']['observation_revision']='ob_2'
                elif change=='wrong_mode':obj['effective_admission']='require_ready'
                elif change=='final_changed_sample_allowed':obj['sample_seq']=100
                else:row['dial_completed_ns']=events[4]['ReceivedNS']-1
                events[index]['Raw']=encoded(obj)
            if change=='final_changed_sample_allowed':
                g2.competition_victim_input(row,cycle,g2.History([raw]))
            else:
                with self.subTest(change=change),self.assertRaises(ValueError):
                    g2.competition_victim_input(row,cycle,g2.History([raw]))


class G2CompetitionExportInputTests(unittest.TestCase):
    def fixture(self):
        origin=g2.NS_SEC
        observations=[dict(status=200,started_ns=origin+52*g2.NS_MS,completed_ns=origin+53*g2.NS_MS)]
        observations.extend(dict(status=429,started_ns=origin+(i+50)*g2.NS_MS,
                                 completed_ns=origin+(i+51)*g2.NS_MS) for i in (14,25,36,47,58))
        stats=dict(epoch=1,relay_bytes=0,max_buffered_bytes=0,tickets=0,accesses_online=2,store_healthy=True,
            channels=dict(preparing=0,open=1,closing=0,closed=0),
            process=dict(goroutines=10,open_fds=16,rss_bytes=1024,heap_bytes=1024,cpu_ms=1))
        def sample(start,end,active,age):
            st=copy.deepcopy(stats);st['observation']=dict(exporter_active=active,exporter_youngest_active_age_ns=age)
            return dict(started_ns=origin+start,completed_ns=origin+end,http_status=200,
                        process=dict(pid=123,sampled_ns=origin+end,stats=st))
        slot=dict(schema='g2-export-slot-input-v1',origin_ns=origin,deadline_ns=origin+g2.NS_SEC,
            helper_started_ns=origin-1,helper_context_deadline_ns=origin-1+g2.NS_SEC,
            zero=sample(2,3,0,0),writers_released_ns=origin+4,
            samples=[sample(2*g2.NS_MS,3*g2.NS_MS,1,g2.NS_MS),sample(45*g2.NS_MS,46*g2.NS_MS,2,40*g2.NS_MS)])
        return dict(cycle=1,role='ois',export_slot_input=slot,observed_ns=origin+150*g2.NS_MS,
            slow_writes=[dict(connected_ns=origin,started_ns=origin+5,completed_ns=origin+g2.NS_MS),
                         dict(connected_ns=origin+1,started_ns=origin+5,completed_ns=origin+g2.NS_MS)],
            slow_response_headers=[dict(socket=i,started_ns=origin+g2.NS_MS,
                completed_ns=origin+g2.NS_MS+1,status=200,complete=True,
                header_raw=base64.b64encode(b'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n').decode(),
                peeked_bytes=4096,error='') for i in range(2)],
            export_input_observations=observations,
            actual_rejection=dict(observations[-1]),
            backlog_before=dict(started_ns=origin+110*g2.NS_MS))

    def test_current_activity_zero_to_two_is_bound_to_original_process(self):
        row=self.fixture();g2.competition_export_input(row,123)
        with self.assertRaisesRegex(ValueError,'another process'):g2.competition_export_input(row,456)
        slot=row['export_slot_input'];slot['samples'][-1]['process']['stats']['observation']['exporter_youngest_active_age_ns']-=1
        with self.assertRaisesRegex(ValueError,'40ms witness'):g2.competition_export_input(row)

    def test_activity_inputs_reject_malformed_identity_time_and_mixed_snapshots(self):
        for mutation in ('missing','null','unknown','extra','empty','overflow','wrong_status','boolean_status',
                         'missing_active','boolean_active','three','negative_age','float_age','zero_age',
                         'not_empty','late_zero','early_release','wrong_pid','wrong_role','missing_process',
                         'missing_counter','outside_capture','too_early','overlap','future','reset_deadline',
                         'age_before_release','before_peek','before_writer','observer_early','continued_after_qualify'):
            row=self.fixture();slot=row['export_slot_input'];x=slot['samples'][-1];d=x['process']['stats']['observation']
            if mutation=='missing':row.pop('export_slot_input')
            elif mutation=='null':row['export_slot_input']=None
            elif mutation=='unknown':slot['schema']='future'
            elif mutation=='extra':slot['assumed']=True
            elif mutation=='empty':slot['samples']=[]
            elif mutation=='overflow':slot['samples']*=65
            elif mutation=='wrong_status':x['http_status']=429
            elif mutation=='boolean_status':x['http_status']=True
            elif mutation=='missing_active':d.pop('exporter_active')
            elif mutation=='boolean_active':d['exporter_active']=True
            elif mutation=='three':d['exporter_active']=3
            elif mutation=='negative_age':d['exporter_youngest_active_age_ns']=-1
            elif mutation=='float_age':d['exporter_youngest_active_age_ns']=40e6
            elif mutation=='zero_age':d['exporter_active']=0
            elif mutation=='not_empty':slot['zero']['process']['stats']['observation']['exporter_active']=1
            elif mutation=='late_zero':slot['zero']['completed_ns']=slot['writers_released_ns']+1
            elif mutation=='early_release':slot['writers_released_ns']=slot['zero']['completed_ns']-1
            elif mutation=='wrong_pid':x['process']['pid']=456
            elif mutation=='wrong_role':row['role']='oic'
            elif mutation=='missing_process':x['process']['stats'].pop('process')
            elif mutation=='missing_counter':x['process']['stats']['process'].pop('heap_bytes')
            elif mutation=='outside_capture':x['process']['sampled_ns']=x['completed_ns']+1
            elif mutation=='too_early':d['exporter_youngest_active_age_ns']=39*g2.NS_MS
            elif mutation=='overlap':x['started_ns']=slot['samples'][0]['completed_ns']
            elif mutation=='future':x['completed_ns']=row['observed_ns']+1
            elif mutation=='reset_deadline':slot['deadline_ns']+=1
            elif mutation=='age_before_release':d['exporter_youngest_active_age_ns']=x['completed_ns']-slot['writers_released_ns']+1
            elif mutation=='before_peek':slot['samples'][0]['started_ns']=row['slow_response_headers'][0]['completed_ns']-1
            elif mutation=='before_writer':row['slow_writes'][0]['completed_ns']=slot['samples'][0]['started_ns']+1
            elif mutation=='observer_early':row['export_input_observations'][0]['started_ns']=x['completed_ns']-1
            else:slot['samples'].append(copy.deepcopy(x))
            with self.subTest(mutation=mutation),self.assertRaises((ValueError,KeyError)):
                g2.competition_export_input(row)

    def test_real_consecutive_observations_and_exact_span_boundary(self):
        row=self.fixture();g2.competition_export_input(row)
        tail=row['export_input_observations'][1:]
        tail[-1]['started_ns']=tail[0]['completed_ns']+40*g2.NS_MS
        tail[-1]['completed_ns']=tail[-1]['started_ns']+1
        row['actual_rejection']=dict(tail[-1])
        g2.competition_export_input(row)
        tail[-1]['started_ns']-=1
        row['actual_rejection']=dict(tail[-1])
        with self.assertRaisesRegex(ValueError,'consecutive 429s'):
            g2.competition_export_input(row)

    def test_both_slow_first_headers_precede_observer_and_are_original_200(self):
        for mutation in ('missing','one','duplicate_socket','wrong_socket_type','extra','status',
                         'status_type','incomplete','error','before_write','after_observer','backward',
                         'invalid_base64','raw_429','truncated','trailing','invalid_field','over_cap','size_lie'):
            row=self.fixture();headers=row['slow_response_headers'];header=headers[1]
            if mutation=='missing':row.pop('slow_response_headers')
            elif mutation=='one':headers.pop()
            elif mutation=='duplicate_socket':header['socket']=0
            elif mutation=='wrong_socket_type':header['socket']=True
            elif mutation=='extra':header['assumed']=True
            elif mutation=='status':header['status']=429
            elif mutation=='status_type':header['status']=200.0
            elif mutation=='incomplete':header['complete']=False
            elif mutation=='error':header['error']='timeout'
            elif mutation=='before_write':header['started_ns']=g2.NS_SEC
            elif mutation=='after_observer':header['completed_ns']=row['export_input_observations'][0]['started_ns']+1
            elif mutation=='backward':header['completed_ns']=header['started_ns']-1
            elif mutation=='invalid_base64':header['header_raw']='!'
            elif mutation=='over_cap':header['peeked_bytes']=8193
            elif mutation=='size_lie':header['peeked_bytes']=1
            else:
                raw={'raw_429':b'HTTP/1.1 429 Too Many Requests\r\n\r\n',
                     'truncated':b'HTTP/1.1 200 OK\r\n',
                     'trailing':b'HTTP/1.1 200 OK\r\n\r\nHTTP/1.1 200 OK\r\n\r\n',
                     'invalid_field':b'HTTP/1.1 200 OK\r\nbad header\r\n\r\n'}[mutation]
                header['header_raw']=base64.b64encode(raw).decode()
            with self.subTest(mutation=mutation),self.assertRaises((ValueError,KeyError)):
                g2.competition_export_input(row)

    def test_transient_rejections_and_200_reset_cannot_qualify(self):
        for mutation in ('only_first_last','four_long_rejections','single_slow_observer',
                         'middle_200','final_200','prior_429_before_200'):
            row=self.fixture();items=row['export_input_observations']
            if mutation=='only_first_last':row['export_input_observations']=[items[1],items[-1]]
            elif mutation=='four_long_rejections':items.pop(2)
            elif mutation=='single_slow_observer':
                items[-1]['completed_ns']+=40*g2.NS_MS
                row['backlog_before']['started_ns']=items[-1]['completed_ns']+1
                row['observed_ns']=items[-1]['completed_ns']+2
                row['export_input_observations']=items[-1:]
            elif mutation=='middle_200':items[3]['status']=200
            elif mutation=='final_200':items[-1]['status']=200
            else:items[0]['status']=429;items[1]['status']=200
            row['actual_rejection']=dict(row['export_input_observations'][-1])
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):
                g2.competition_export_input(row)

    def test_new_five_rejection_tail_after_200_is_valid(self):
        row=self.fixture();items=row['export_input_observations']
        prefix=[dict(status=429,started_ns=g2.NS_SEC+52*g2.NS_MS,
                     completed_ns=g2.NS_SEC+53*g2.NS_MS),
                dict(status=200,started_ns=g2.NS_SEC+54*g2.NS_MS,
                     completed_ns=g2.NS_SEC+55*g2.NS_MS)]
        row['export_input_observations']=prefix+items[1:]
        g2.competition_export_input(row)

    def test_clock_resolution_can_make_one_request_zero_duration(self):
        row=self.fixture()
        for item in row['export_input_observations']:
            item['completed_ns']=item['started_ns']
        row['actual_rejection']=dict(row['export_input_observations'][-1])
        g2.competition_export_input(row)

    def test_all_observations_are_required_typed_and_causally_ordered(self):
        for mutation in ('missing','null','empty','overflow','extra_field','missing_field',
                         'boolean_status','string_status','other_status','boolean_time','float_time',
                         'duplicate','overlap','unfinished','reversed','before_writes',
                         'future_observation','after_critical','after_deadline','wrong_accepted',
                         'continued_after_acceptance'):
            row=self.fixture();items=row['export_input_observations']
            if mutation=='missing':row.pop('export_input_observations')
            elif mutation=='null':row['export_input_observations']=None
            elif mutation=='empty':row['export_input_observations']=[]
            elif mutation=='overflow':row['export_input_observations']=items*22
            elif mutation=='extra_field':items[0]['accepted']=True
            elif mutation=='missing_field':items[0].pop('status')
            elif mutation=='boolean_status':items[0]['status']=True
            elif mutation=='string_status':items[0]['status']='200'
            elif mutation=='other_status':items[0]['status']=503
            elif mutation=='boolean_time':items[0]['started_ns']=True
            elif mutation=='float_time':items[0]['started_ns']=float(items[0]['started_ns'])
            elif mutation=='duplicate':items.insert(3,dict(items[2]))
            elif mutation=='overlap':items[3]['started_ns']=items[2]['completed_ns']
            elif mutation=='unfinished':items[0]['completed_ns']=0
            elif mutation=='reversed':items[0]['completed_ns']=items[0]['started_ns']-1
            elif mutation=='before_writes':items[0]['started_ns']=g2.NS_SEC
            elif mutation=='future_observation':row['observed_ns']=items[-1]['completed_ns']-1
            elif mutation=='after_critical':row['backlog_before']['started_ns']=items[-1]['completed_ns']-1
            elif mutation=='after_deadline':
                delta=g2.NS_SEC
                for item in items:
                    item['started_ns']+=delta;item['completed_ns']+=delta
                row['actual_rejection']=dict(items[-1])
                row['backlog_before']['started_ns']+=delta;row['observed_ns']+=delta
            elif mutation=='wrong_accepted':row['actual_rejection']['started_ns']-=1
            else:
                items.append(dict(status=429,started_ns=g2.NS_SEC+70*g2.NS_MS,
                                  completed_ns=g2.NS_SEC+71*g2.NS_MS))
                row['actual_rejection']=dict(items[-1]);row['backlog_before']['started_ns']=items[-1]['completed_ns']+1
            with self.subTest(mutation=mutation),self.assertRaises((ValueError,KeyError)):
                g2.competition_export_input(row)

    def test_original_shared_second_is_inclusive_but_never_renewed(self):
        row=self.fixture();items=row['export_input_observations']
        delta=2*g2.NS_SEC-items[-1]['completed_ns']
        for item in items:
            item['started_ns']+=delta;item['completed_ns']+=delta
        row['actual_rejection']=dict(items[-1]);row['observed_ns']=2*g2.NS_SEC+10
        row['backlog_before']['started_ns']=2*g2.NS_SEC+1
        g2.competition_export_input(row)
        items[-1]['completed_ns']+=1;row['actual_rejection']=dict(items[-1])
        with self.assertRaisesRegex(ValueError,'original 1s'):
            g2.competition_export_input(row)


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


class G2CompetitionRevocationTests(unittest.TestCase):
    def fixture(self, outcome='written'):
        ms=g2.NS_MS;origin=10*g2.NS_SEC
        abort=wire(b'\x04\x01\x06',1,origin+50*ms,direction='ois_to_oic',channel='ch_test')
        abort.update(WriteStartedNS=origin+130*ms,WrittenNS=origin+140*ms)
        if outcome=='write_failed':abort.update(WrittenNS=0,WriteFailed=True)
        elif outcome=='closed_before_write':abort.update(WriteStartedNS=0,WrittenNS=0)
        close=wire(dict(type='close_channel',seq=1,channel_id='ch_test',code='Revoked'),2,
                   origin+60*ms,direction='ois_to_oic')
        close.update(WriteStartedNS=origin+80*ms,WrittenNS=origin+90*ms)
        witness=wire(b'\x04\x01\x06',3,origin+85*ms,direction='oic_to_ois',channel='ch_test')
        witness.update(WriteStartedNS=origin+91*ms,WrittenNS=origin+92*ms)
        raw=history_row([abort,close,witness])
        conn=raw['proxy']['Connections'][1]
        conn.update(Closed=True,ClosedNS=origin+150*ms,Workers=0)
        if outcome=='closed_before_write':conn['ClosedNS']=origin+110*ms
        row=dict(victim_channel_id='ch_test',revoke_started_ns=origin+30*ms,
                 revoke_returned_ns=origin+35*ms,consumer_abort_ns=origin+40*ms,
                 revoke_wire=copy.deepcopy(close),provider_abort=copy.deepcopy(abort),
                 oic_abort_witness=copy.deepcopy(witness),
                 provider_abort_connection=copy.deepcopy(conn),
                 provider_abort_barrier=dict(channel_id='ch_test',armed_ns=origin+20*ms,
                     control_release_requested_ns=origin+70*ms,release_requested_ns=origin+120*ms,
                     cancelled_ns=origin+160*ms,event_id=1),
                 slow_writes=[dict(connected_ns=origin),dict(connected_ns=origin+1)],
                 backlog_before=dict(oic=dict(closed={'Revoked':2},epoch=1,generation=3)),
                 backlog_after=dict(completed_ns=origin+145*ms,
                                    oic=dict(closed={'Revoked':3},epoch=1,generation=3)))
        return raw,row

    def test_consumer_can_abort_before_control_receipt_without_negative_latency(self):
        raw,row=self.fixture()
        self.assertLess(row['consumer_abort_ns'],row['revoke_wire']['ReceivedNS'])
        self.assertLess(row['oic_abort_witness']['ReceivedNS'],row['revoke_wire']['WrittenNS'])
        g2.competition_revocation(row,g2.History([raw]))

    def test_genuine_provider_written_failed_and_closed_before_write_are_terminal(self):
        for outcome in ('written','write_failed','closed_before_write'):
            with self.subTest(outcome=outcome):
                raw,row=self.fixture(outcome)
                g2.competition_revocation(row,g2.History([raw]))

    def test_barrier_identity_and_causal_times_cannot_be_forged(self):
        for mode in ('missing','other_channel','wrong_event','boolean_arm','late_arm',
                     'release_before_capture','control_before_release','early_provider_release',
                     'cancel_before_release','cancel_after_budget','new_input_origin',
                     'consumer_before_revoke','consumer_after_observation','missing_closed_increment',
                     'two_closed_increments','new_generation','late_control_observation'):
            raw,row=self.fixture();b=row['provider_abort_barrier'];a=raw['proxy']['Events'][0]
            if mode=='missing':row.pop('provider_abort_barrier')
            elif mode=='other_channel':b['channel_id']='ch_other'
            elif mode=='wrong_event':b['event_id']=2
            elif mode=='boolean_arm':b['armed_ns']=True
            elif mode=='late_arm':b['armed_ns']=row['revoke_started_ns']+1
            elif mode=='release_before_capture':b['control_release_requested_ns']=a['ReceivedNS']-1
            elif mode=='control_before_release':b['control_release_requested_ns']=row['revoke_wire']['WriteStartedNS']+1
            elif mode=='early_provider_release':b['release_requested_ns']=row['oic_abort_witness']['ReceivedNS']-1
            elif mode=='cancel_before_release':b['cancelled_ns']=b['release_requested_ns']-1
            elif mode=='cancel_after_budget':b['cancelled_ns']=row['slow_writes'][0]['connected_ns']+g2.NS_SEC+1
            elif mode=='new_input_origin':
                for w in row['slow_writes']:w['connected_ns']=b['armed_ns']+1
            elif mode=='consumer_before_revoke':row['consumer_abort_ns']=row['revoke_started_ns']-1
            elif mode=='consumer_after_observation':row['consumer_abort_ns']=row['backlog_after']['completed_ns']+1
            elif mode=='missing_closed_increment':row['backlog_after']['oic']['closed']['Revoked']=2
            elif mode=='two_closed_increments':row['backlog_after']['oic']['closed']['Revoked']=4
            elif mode=='new_generation':row['backlog_after']['oic']['generation']+=1
            else:
                # Keep the original 1s input and barrier order valid while
                # exceeding ONLY control Received -> OIC observation 500ms.
                row['backlog_after']['completed_ns']=row['revoke_wire']['ReceivedNS']+500*g2.NS_MS+1
                b['cancelled_ns']=row['backlog_after']['completed_ns']+1
            with self.subTest(mode=mode),self.assertRaises((ValueError,KeyError)):
                g2.competition_revocation(row,g2.History([raw]))

    def test_provider_full_wire_and_terminal_connection_are_independently_verified(self):
        for mode in ('wrong_code','data_instead_abort','injected','dropped','wrong_direction',
                     'early_write','fake_final_write','missing_terminal','unjoined','fake_connection',
                     'closed_after_cancel','write_without_outcome','failed_without_write'):
            raw,row=self.fixture();a=raw['proxy']['Events'][0];c=raw['proxy']['Connections'][1]
            if mode=='wrong_code':a['Raw']=encoded(b'\x04\x01\x07')
            elif mode=='data_instead_abort':a.update(Type='DATA',Raw=encoded(b'\x02payload'))
            elif mode=='injected':a.update(Injected=True,ReceivedNS=0)
            elif mode=='dropped':a.update(Dropped=True,WriteStartedNS=0,WrittenNS=0)
            elif mode=='wrong_direction':a['Direction']='oic_to_ois'
            elif mode=='early_write':a['WriteStartedNS']=row['provider_abort_barrier']['release_requested_ns']-1
            elif mode=='fake_final_write':row['provider_abort']['WrittenNS']+=1
            elif mode=='missing_terminal':c.update(Closed=False,ClosedNS=0)
            elif mode=='unjoined':c['Workers']=1
            elif mode=='fake_connection':row['provider_abort_connection']['ID']=99
            elif mode=='closed_after_cancel':c['ClosedNS']=row['provider_abort_barrier']['cancelled_ns']+1
            elif mode=='write_without_outcome':a['WrittenNS']=0
            else:a.update(WriteStartedNS=0,WrittenNS=0,WriteFailed=True)
            if mode!='fake_final_write':row['provider_abort']=copy.deepcopy(a)
            if mode!='fake_connection':row['provider_abort_connection']=copy.deepcopy(c)
            with self.subTest(mode=mode),self.assertRaises(ValueError):
                g2.competition_revocation(row,g2.History([raw]))

    def test_unreferenced_early_provider_abort_cannot_hide_behind_held_copy(self):
        for mode in ('early_write','injected','dropped'):
            raw,row=self.fixture()
            extra=copy.deepcopy(raw['proxy']['Events'][0]);extra['ID']=4
            extra['ReceivedNS']+=1
            if mode=='early_write':extra['WriteStartedNS']=row['provider_abort_barrier']['release_requested_ns']-1
            elif mode=='injected':extra.update(Injected=True,ReceivedNS=0)
            else:extra.update(Dropped=True,WriteStartedNS=0,WrittenNS=0)
            raw['proxy']['Events'].append(extra)
            with self.subTest(mode=mode),self.assertRaises(ValueError):
                g2.competition_revocation(row,g2.History([raw]))

    def test_first_terminal_witness_is_real_matching_upstream_revoked(self):
        for mode in ('missing','wrong_code','wrong_direction','injected','dropped','wrong_connection',
                     'wrong_channel','before_control_write','after_release','forged_final'):
            raw,row=self.fixture();witness=raw['proxy']['Events'][2]
            if mode=='missing':row.pop('oic_abort_witness')
            elif mode=='wrong_code':witness['Raw']=encoded(b'\x04\x01\x07')
            elif mode=='wrong_direction':witness['Direction']='ois_to_oic'
            elif mode=='injected':witness.update(Injected=True,ReceivedNS=0)
            elif mode=='dropped':witness.update(Dropped=True,WriteStartedNS=0,WrittenNS=0)
            elif mode=='wrong_connection':witness['ConnID']=1
            elif mode=='wrong_channel':witness['ChannelID']='ch_other'
            elif mode=='before_control_write':witness['ReceivedNS']=row['revoke_wire']['WriteStartedNS']-1
            elif mode=='after_release':
                witness['ReceivedNS']=row['provider_abort_barrier']['release_requested_ns']+1
                witness.update(WriteStartedNS=0,WrittenNS=0)
            else:row['oic_abort_witness']['WrittenNS']+=1
            if mode not in ('missing','forged_final'):row['oic_abort_witness']=copy.deepcopy(witness)
            with self.subTest(mode=mode),self.assertRaises((ValueError,KeyError)):
                g2.competition_revocation(row,g2.History([raw]))
        # Receipt itself is the first-terminal witness. The OIS leg may close
        # before the proxy forwards it; successful proxy forwarding is not a
        # new prerequisite for the actual OIC decision.
        raw,row=self.fixture()
        raw['proxy']['Events'][2].update(WriteStartedNS=0,WrittenNS=0)
        row['oic_abort_witness']=copy.deepcopy(raw['proxy']['Events'][2])
        g2.competition_revocation(row,g2.History([raw]))

    def test_control_revocation_cannot_be_substituted_or_observed_before_delivery(self):
        for mode in ('wrong_code','other_channel','wrong_direction','dropped','unwritten','observation_before_delivery'):
            raw,row=self.fixture();close=raw['proxy']['Events'][1]
            if mode=='wrong_code':
                body=json.loads(base64.b64decode(close['Raw']));body['code']='Completed';close['Raw']=encoded(body)
            elif mode=='other_channel':
                body=json.loads(base64.b64decode(close['Raw']));body['channel_id']='ch_other'
                close.update(ChannelID='ch_other',Raw=encoded(body))
            elif mode=='wrong_direction':close['Direction']='oic_to_ois'
            elif mode=='dropped':close.update(Dropped=True,WriteStartedNS=0,WrittenNS=0)
            elif mode=='unwritten':close.update(WrittenNS=0,WriteFailed=True)
            else:row['backlog_after']['completed_ns']=close['WrittenNS']-1
            row['revoke_wire']=copy.deepcopy(close)
            with self.subTest(mode=mode),self.assertRaises(ValueError):
                g2.competition_revocation(row,g2.History([raw]))


if __name__=='__main__':
    unittest.main()
