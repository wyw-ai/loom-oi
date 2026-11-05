"""Six-source input is independently witnessed; old manifests keep old bytes."""
import base64
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('g2_preparation_under_test', ROOT/'product/g2_analyze.py')
g2 = importlib.util.module_from_spec(spec)
exec(compile(Path(spec.origin).read_bytes(), spec.origin, 'exec'), g2.__dict__)
MS = g2.NS_MS
T = g2.NS_SEC


def sample(start):
    return dict(started_ns=start, completed_ns=start+MS,
        ois=dict(epoch=1, accesses_online=2, observation=dict(pending_queries=8,
            observation_backlog=16, observation_capacity=16, dirty_marked=16)),
        oic=dict(epoch=1, generation=1, connected=True, registered=True,
            observation=dict(probe_jobs=16, probe_active=2, probe_queued=4,
                max_probe_active=2, max_probe_queued=4, probe_max_wait_ns=0, probe_oldest_due_ns=0)))


def fixture():
    events = []
    witnesses = []
    for stage, offset in enumerate((10, 40, 60)):
        s = sample(T+offset*MS)
        projected = []
        for i, exposure in enumerate(g2.COMPETITION_SOURCES[:8]):
            ident = len(events)+1
            b = dict(access_id='lab-a', epoch=1, generation=1, exposure_id=exposure,
                revision='rv_source', observation_revision='ob_1', contract_digest='a'*64,
                source_id='local', contract_version=1)
            at = T+(offset+2+i)*MS
            body = dict(type='observation_query', seq=ident, query_id=f'{ident:032x}', binding=b, deadline_ms=500)
            e = dict(ID=ident, ConnID=1, Role='control', Direction='ois_to_oic',
                Type='observation_query', Stage='', ChannelID='', CheckID='', QueryID=body['query_id'],
                MessageType=1, Raw=base64.b64encode(json.dumps(body).encode()).decode(), ReceivedNS=at,
                WriteStartedNS=at+1, WrittenNS=at+2, Injected=False, WriteFailed=False, Dropped=False)
            events.append(e)
            projected.append(dict(event_id=ident, conn_id=1, query_id=e['QueryID'], binding=copy.deepcopy(b),
                received_ns=at, write_started_ns=at+1, written_ns=at+2, deadline_ms=500,
                injected=False, dropped=False, decoded=True))
        w = dict(ois_pid=101, oic_pid=102, sample_started_ns=s['started_ns'], sample_completed_ns=s['completed_ns'],
            sample_returned_ns=s['completed_ns']+1, wire_event_count=len(events), wire_last_event_id=len(events),
            wire_snapshot_returned_ns=T+(offset+11)*MS, evaluated_ns=T+(offset+12)*MS,
            returned_ns=T+(offset+13)*MS, max_age_ms=300 if stage==2 else 200, result=True,
            queries=list(reversed(projected)))
        if stage != 2:
            w.update(attempt=stage+1, callback_returned_ns=T+(offset+14)*MS)
        if stage == 0:
            w['sample'] = copy.deepcopy(s)
        witnesses.append(w)
    proxy = dict(PID=42, Workers=2, TransportConnections=1, Overflow=False, Failure='',
        Connections=[dict(ID=1, Role='control', ChannelID='', AcceptedNS=1, ClosedNS=0, Workers=2, Closed=False)],
        Events=events)
    raw_history = dict(access_id='lab-a', proxy=proxy)
    anchors = dict(window_start_ns=T, prepare_started_ns=T+MS, prepare_deadline_ns=T+MS+3*T,
        prepared_ns=T+55*MS, slow_input_applied_ns=T+27*MS,
        preparation_strategy='six-source-preload-v1', preload_set_started_ns=T+2*MS,
        preload_set_completed_ns=T+3*MS, preload_accepted_ns=T+25*MS, final_set_started_ns=T+26*MS,
        input_versions={s:2 for s in g2.COMPETITION_SOURCES})
    full = dict(copy.deepcopy(anchors), observed_ns=T+56*MS, sample=sample(T+40*MS), snapshot_drop_cap=32,
                preload_witness=witnesses[0], final_query_witness=witnesses[1])
    end = dict(copy.deepcopy(anchors), window_end_ns=T+27*MS+60*T, observed_ns=T+27*MS+62*T)
    base = dict(ois=dict(pid=101, stats=dict(epoch=1)),
                oics={'lab-a':dict(pid=102, stats=dict(epoch=1, generation=1))})
    return full, end, raw_history, base, witnesses[2]


def health_rows(full, end):
    rows = []
    start = end['slow_input_applied_ns']; finish = end['window_end_ns']
    for i, exposure in enumerate(g2.COMPETITION_SOURCES):
        slow = i >= 2
        r = dict(ID=1, Version=2, Status=200, RequestURI='/', StartedNS=start+T,
            HeaderStartedNS=0 if slow else start+T+1,
            HeaderWrittenNS=0 if slow else start+T+2,
            CancelledNS=start+T+200*MS if slow else 0,
            FinishedNS=start+T+200*MS if slow else start+T+3, WriteFailed=False)
        rows.append(dict(event='competition_target', exposure_id=exposure, version=2,
            observed_ns=finish+2*T,
            preparation_strategy='six-source-preload-v1', exercise_start_ns=start, exercise_end_ns=finish,
            timeout_target=slow, attempts=1, timeouts=int(slow),
            health=dict(PID=42, Version=2, Workers=0, Overflow=False, Records=[r])))
    return rows


class SixSourceDeclarationTests(unittest.TestCase):
    def test_full_exact_declaration_is_required_and_original_inventory_is_preserved(self):
        expected = g2.expected_spec()
        self.assertEqual(expected['competition_preparation'], g2.COMPETITION_PREPARATION)
        self.assertEqual((len(expected['groups']), sum(len(g['leaves']) for g in expected['groups'].values()),
                          sum(g['resource'] for g in expected['groups'].values())), (108, 130, 106))
        for bad in (None, {}, 'six-source-preload-v1', dict(g2.COMPETITION_PREPARATION, extra=True),
                    dict(g2.COMPETITION_PREPARATION, query_witness='selected-query-snapshot-v0')):
            manifest = g2.expected_spec(); manifest['competition_preparation'] = bad
            with self.subTest(value=bad), self.assertRaisesRegex(ValueError, 'preregistration'):
                g2.analyze_g2(manifest, [], {})
        manifest = g2.expected_spec(); manifest.pop('competition_preparation')
        with self.assertRaisesRegex(ValueError, 'preregistration'):
            g2.analyze_g2(manifest, [], {})

    def test_recorded_loader_selects_exact_adjacent_old_module_and_dependencies(self):
        directory = ROOT/'sources/product-pre-six-source'
        mapping = json.loads((directory/'source-map.json').read_text(encoding='utf-8'))
        for key, info in mapping['files'].items():
            raw = (ROOT.parent/info['archived_path']).read_bytes()
            self.assertEqual(len(raw), info['bytes'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), info['sha256'])
        loader_spec = importlib.util.spec_from_file_location('six_source_verify', ROOT/'verify.py')
        verify = importlib.util.module_from_spec(loader_spec)
        exec(compile(Path(loader_spec.origin).read_bytes(), loader_spec.origin, 'exec'), verify.__dict__)
        old_spec = importlib.util.spec_from_file_location('old_g2_preparation', directory/'g2_analyze.py')
        old = importlib.util.module_from_spec(old_spec)
        exec(compile(Path(old_spec.origin).read_bytes(), old_spec.origin, 'exec'), old.__dict__)
        self.assertNotIn('competition_preparation', old.expected_spec())
        with tempfile.TemporaryDirectory() as tmp:
            run = Path(tmp)
            recorded = {k:v['sha256'] for k,v in mapping['files'].items()}
            (run/'source-manifest.json').write_text(json.dumps(recorded), encoding='utf-8')
            (run/'manifest.json').write_text(json.dumps({'case':{'g2_behavior':old.expected_spec()}}), encoding='utf-8')
            selected = verify.recorded_module(run)
            self.assertEqual(Path(selected.__file__).parent, directory)
            loaded = selected._sibling('g2_analyze.py')
            self.assertEqual(loaded.analyze_g2(old.expected_spec(), [], {}), old.analyze_g2(old.expected_spec(), [], {}))
            with self.assertRaisesRegex(ValueError, 'preregistration'):
                loaded.analyze_g2(g2.expected_spec(), [], {})


class SixSourceQueryTests(unittest.TestCase):
    def check(self, full, end, raw, base):
        g2.competition_window(full, end)
        return g2.competition_preparation(full, end, g2.History([raw]), base)

    def test_independent_preload_final_and_critical_boundaries(self):
        full, end, raw, base, critical = fixture()
        self.assertEqual(self.check(full, end, raw, base), 1)
        self.assertEqual(g2.competition_query_witness(critical, sample(T+60*MS), g2.History([raw]),
                                                    300, end['prepared_ns'], T+74*MS, base), 1)
        # Inclusive age equality passes; one nanosecond over does not. The
        # enclosing native start is a distinct bound, not a fresh deadline.
        for budget, name, earliest in ((200, 'final_query_witness', end['slow_input_applied_ns']),):
            w = copy.deepcopy(full[name]); w['evaluated_ns'] = min(q['received_ns'] for q in w['queries'])+budget*MS
            w['returned_ns'] = w['evaluated_ns']; w['callback_returned_ns'] = w['returned_ns']
            limit = w['returned_ns']+1
            g2.competition_query_witness(w, full['sample'], g2.History([raw]), budget, earliest, limit, base, preparation=True)
            w['evaluated_ns']+=1; w['returned_ns']+=1; w['callback_returned_ns']+=1
            with self.assertRaisesRegex(ValueError, 'stale'):
                g2.competition_query_witness(w, full['sample'], g2.History([raw]), budget, earliest, limit, base, preparation=True)

    def test_anchor_order_attempts_and_original_three_seconds_are_strict(self):
        mutations = {
            'old_strategy': lambda f,e,b: f.update(preparation_strategy='healthy-first'),
            'missing_version': lambda f,e,b: f['input_versions'].pop('lab-a/competition-15'),
            'boolean_version': lambda f,e,b: f['input_versions'].update({'lab-a/http':True}),
            'preload_before_sets': lambda f,e,b: f['preload_witness'].update(sample_started_ns=T),
            'final_before_slow': lambda f,e,b: f.update(final_set_started_ns=e['slow_input_applied_ns']+1),
            'attempt_reused': lambda f,e,b: f['final_query_witness'].update(attempt=1),
            'callback_late': lambda f,e,b: f['final_query_witness'].update(callback_returned_ns=e['prepared_ns']+1),
            'renewed_three_seconds': lambda f,e,b: (f.update(prepare_deadline_ns=e['prepare_deadline_ns']+1),e.update(prepare_deadline_ns=e['prepare_deadline_ns']+1)),
            'other_epoch': lambda f,e,b: b['ois']['stats'].update(epoch=2),
            'other_generation': lambda f,e,b: b['oics']['lab-a']['stats'].update(generation=2),
            'full_emitted_before_prepared': lambda f,e,b: f.update(observed_ns=e['prepared_ns']-1),
            'complete_emitted_before_window_end': lambda f,e,b: e.update(observed_ns=e['window_end_ns']-1),
            'no_final': lambda f,e,b: f.pop('final_query_witness'),
            'swapped_witness': lambda f,e,b: f.update(final_query_witness=f['preload_witness']),
        }
        for name, mutate in mutations.items():
            full,end,raw,base,_ = fixture(); mutate(full,end,base)
            with self.subTest(name=name), self.assertRaises((ValueError, KeyError)):
                self.check(full,end,raw,base)

    def test_diagnostic_history_capacity_does_not_lower_the_original_input_limit(self):
        full,end,raw,base,_=fixture()
        full['preload_witness']['attempt']=513
        full['final_query_witness']['attempt']=514
        self.check(full,end,raw,base)
        full['final_query_witness']['callback_returned_ns']=end['prepare_deadline_ns']+1
        with self.assertRaisesRegex(ValueError,'deadline'):
            self.check(full,end,raw,base)

    def test_projection_cannot_forge_identity_prefix_or_forwarding_time(self):
        mutations = {
            'too_few': lambda w: w['queries'].pop(),
            'reverse_scan': lambda w: w['queries'].reverse(),
            'duplicate_id': lambda w: w['queries'].__setitem__(0,copy.deepcopy(w['queries'][1])),
            'different_conn': lambda w: w['queries'][0].update(conn_id=3),
            'different_binding': lambda w: w['queries'][0]['binding'].update(generation=2),
            'false_decode': lambda w: w['queries'][0].update(decoded=False),
            'injected': lambda w: w['queries'][0].update(injected=True),
            'not_yet_written': lambda w: w['queries'][0].update(written_ns=0),
            'forged_written': lambda w: w['queries'][0].update(written_ns=w['wire_snapshot_returned_ns']),
            'future_prefix': lambda w: w.update(wire_event_count=24,wire_last_event_id=24),
            'prefix_count': lambda w: w.update(wire_event_count=15),
            'alias_extra': lambda w: w.update(Result=True),
            'age_budget': lambda w: w.update(max_age_ms=201),
            'future_sample': lambda w: w.update(sample_returned_ns=w['wire_snapshot_returned_ns']+1),
            'fake_query': lambda w: w['queries'][0].update(query_id='f'*32),
            'extra_field': lambda w: w['queries'][0].update(future_write=True),
        }
        for name, mutate in mutations.items():
            full,end,raw,base,_ = fixture(); mutate(full['final_query_witness'])
            with self.subTest(name=name), self.assertRaises((ValueError, KeyError)):
                self.check(full,end,raw,base)

    def test_final_history_does_not_backfill_a_concurrently_completed_unselected_query(self):
        full,end,raw,base,_ = fixture()
        # A final-history event can be inside the snapshot prefix but still
        # have WrittenNS=0 in that copy. Its eventual positive write is not a
        # license for the analyzer to pretend the instrument selected it.
        newer = raw['proxy']['Events'][16]
        newer['ReceivedNS'] = full['final_query_witness']['wire_snapshot_returned_ns']-1
        newer['WriteStartedNS'] = newer['ReceivedNS']
        newer['WrittenNS'] = full['final_query_witness']['wire_snapshot_returned_ns']
        full['final_query_witness'].update(wire_event_count=17, wire_last_event_id=17)
        self.check(full,end,raw,base)
        # Conversely a selected projection cannot borrow a future completion.
        q = full['final_query_witness']['queries'][0]
        e = raw['proxy']['Events'][q['event_id']-1]
        q['written_ns']=e['WrittenNS']=full['final_query_witness']['wire_snapshot_returned_ns']+1
        with self.assertRaisesRegex(ValueError, 'not written'):
            self.check(full,end,raw,base)

    def test_critical_retains_only_its_original_three_hundred_ms_once(self):
        full,end,raw,base,w = fixture(); history=g2.History([raw]); s=sample(T+60*MS)
        for change in ('old','other_sample','future','prep_fields','two_hundred'):
            bad=copy.deepcopy(w)
            if change=='old':bad.update(evaluated_ns=T+363*MS,returned_ns=T+363*MS)
            elif change=='other_sample':bad['sample_started_ns']-=1
            elif change=='future':bad['returned_ns']=T+75*MS
            elif change=='prep_fields':bad.update(attempt=3,callback_returned_ns=bad['returned_ns'])
            else:bad['max_age_ms']=200
            with self.subTest(change=change),self.assertRaises(ValueError):
                g2.competition_query_witness(bad,s,history,300,end['prepared_ns'],T+74*MS,base)

    def test_caller_requires_cycle_zero_witness_and_other_cycles_explicit_null(self):
        full,end,raw,base,w=fixture();history=g2.History([raw])
        row=dict(full_backlog_required=True,critical_query_witness=w,observed_ns=T+100*MS,
                 backlog_before=sample(T+60*MS),native_started_ns=T+74*MS)
        g2.competition_critical_query(row,history,1,end['prepared_ns'],base)
        for value in (None,{},full['final_query_witness']):
            with self.subTest(value=value),self.assertRaises(ValueError):
                g2.competition_critical_query(dict(row,critical_query_witness=value),history,1,end['prepared_ns'],base)
        with self.assertRaisesRegex(ValueError,'control session'):
            g2.competition_critical_query(row,history,99,end['prepared_ns'],base)
        with self.assertRaisesRegex(ValueError,'sampling/acceptance window'):
            g2.competition_critical_query(dict(row,observed_ns=w['returned_ns']-1),history,1,end['prepared_ns'],base)
        row['full_backlog_required']=False
        with self.assertRaisesRegex(ValueError,'unevaluated'):
            g2.competition_critical_query(row,history,1,end['prepared_ns'],base)
        row['critical_query_witness']=None
        g2.competition_critical_query(row,history,1,end['prepared_ns'],base)
        row.pop('critical_query_witness')
        with self.assertRaisesRegex(ValueError,'unevaluated'):
            g2.competition_critical_query(row,history,1,end['prepared_ns'],base)

    def test_each_selected_snapshot_names_its_original_role_process(self):
        for stage in ('preload_witness','final_query_witness','critical'):
            for field in ('ois_pid','oic_pid'):
                for bad in ('missing',None,True,0,42,999,102 if field=='ois_pid' else 101):
                    full,end,raw,base,critical=fixture()
                    witness=critical if stage=='critical' else full[stage]
                    if bad=='missing':witness.pop(field)
                    else:witness[field]=bad
                    with self.subTest(stage=stage,field=field,bad=bad),self.assertRaises((ValueError,KeyError)):
                        if stage=='critical':
                            g2.competition_critical_query(dict(full_backlog_required=True,
                                critical_query_witness=critical,backlog_before=sample(T+60*MS),
                                native_started_ns=T+74*MS,observed_ns=T+100*MS),
                                g2.History([raw]),1,end['prepared_ns'],base)
                        else:
                            self.check(full,end,raw,base)


class SixSourceHealthTests(unittest.TestCase):
    def check(self, rows, full, end, raw):
        g2.competition_targets(rows,g2.History([raw]),full,end)

    def test_warmup_and_postwindow_cancellation_do_not_count(self):
        full,end,raw,_,_=fixture();rows=health_rows(full,end)
        self.check(rows,full,end,raw)
        r=rows[2]; original=r['health']['Records'][0]
        warm=copy.deepcopy(original);warm.update(ID=1,StartedNS=end['slow_input_applied_ns']-1)
        original['ID']=2
        late=copy.deepcopy(original);late.update(ID=3,StartedNS=end['window_end_ns']-1,
            CancelledNS=end['window_end_ns']+1,FinishedNS=end['window_end_ns']+1)
        r['health']['Records']=[warm,original,late];r['attempts']=2
        self.check(rows,full,end,raw)
        r['timeouts']=2
        with self.assertRaisesRegex(ValueError,'timeouts differ'):
            self.check(rows,full,end,raw)

    def test_every_slow_source_needs_actual_in_window_no_header_cancellation(self):
        mutations={
            'warm_only':lambda r,s,e:r['health']['Records'][0].update(StartedNS=s-1),
            'cancel_after_end':lambda r,s,e:r['health']['Records'][0].update(CancelledNS=e+1,FinishedNS=e+1),
            'zero_cancel':lambda r,s,e:r['health']['Records'][0].update(CancelledNS=0),
            'header_released':lambda r,s,e:r['health']['Records'][0].update(HeaderStartedNS=s+2*T),
            'wrong_version':lambda r,s,e:r['health']['Records'][0].update(Version=3),
            'wrong_process':lambda r,s,e:r['health'].update(PID=102),
            'wrong_strategy':lambda r,s,e:r.update(preparation_strategy='legacy'),
            'wrong_window':lambda r,s,e:r.update(exercise_start_ns=s-1),
            'wrong_slow_id':lambda r,s,e:r.update(timeout_target=False),
            'overflow':lambda r,s,e:r['health'].update(Overflow=True),
            'snapshot_version':lambda r,s,e:r['health'].update(Version=3),
            'boolean_count':lambda r,s,e:r.update(attempts=True),
            'future_record':lambda r,s,e:r['health']['Records'][0].update(FinishedNS=r['observed_ns']+1),
            'finished_before_cancel':lambda r,s,e:r['health']['Records'][0].update(FinishedNS=s+T+1),
            'changed_status':lambda r,s,e:r['health']['Records'][0].update(Status=503),
        }
        for name,mutate in mutations.items():
            full,end,raw,_,_=fixture();rows=health_rows(full,end)
            mutate(rows[2],end['slow_input_applied_ns'],end['window_end_ns'])
            with self.subTest(name=name),self.assertRaises((ValueError,KeyError)):
                self.check(rows,full,end,raw)

    def test_extra_or_substituted_source_and_cleanup_headers_are_distinguished(self):
        full,end,raw,_,_=fixture();rows=health_rows(full,end)
        r=rows[2];later=copy.deepcopy(r['health']['Records'][0]);later.update(ID=2,
            StartedNS=end['window_end_ns'], HeaderStartedNS=end['window_end_ns']+1,
            CancelledNS=0,FinishedNS=end['window_end_ns']+2)
        r['health']['Records'].append(later);r['attempts']=2
        self.check(rows,full,end,raw)
        for name in ('missing','extra','substituted'):
            bad=copy.deepcopy(rows)
            if name=='missing':bad.pop()
            elif name=='extra':bad.append(copy.deepcopy(bad[0]))
            else:bad[-1]['exposure_id']='lab-b/competition-15'
            with self.subTest(name=name),self.assertRaises(ValueError):
                self.check(bad,full,end,raw)

    def test_partial_finish_is_valid_but_finished_cannot_precede_header_or_cancel(self):
        full,end,raw,_,_=fixture();rows=health_rows(full,end)
        rows[2]['health']['Records'][0]['FinishedNS']=0
        self.check(rows,full,end,raw)
        record=rows[0]['health']['Records'][0]
        record['FinishedNS']=record['HeaderStartedNS']
        with self.assertRaisesRegex(ValueError,'finished before'):
            self.check(rows,full,end,raw)


if __name__ == '__main__':
    unittest.main()
