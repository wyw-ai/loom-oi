"""Two real registration generations, without filtering the original evidence."""
import base64
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

import test_product_g2_preparation as prep

g2, MS, T, ROOT = prep.g2, prep.MS, prep.T, prep.ROOT


def check(f, e, raw, base):
    g2.competition_window(f, e)
    return g2.competition_preparation(f, e, g2.History([raw]), base)


def mutate_body(raw, index, change):
    event = raw['proxy']['Events'][index]
    body = json.loads(base64.b64decode(event['Raw']))
    change(body)
    event['Raw'] = base64.b64encode(json.dumps(body).encode()).decode()


def sync(f, e):
    e['probe_reload_witness'] = copy.deepcopy(f['probe_reload_witness'])


def append_response(raw, query_index=2, *, late_query=False):
    events = raw['proxy']['Events']
    query = copy.deepcopy(events[query_index])
    body = json.loads(base64.b64decode(query['Raw']))
    if late_query:
        ident = len(events)+1
        body.update(seq=ident, query_id=f'{200:032x}')
        query.update(ID=ident, QueryID=body['query_id'], ReceivedNS=T+80*MS,
            WriteStartedNS=T+80*MS+1, WrittenNS=T+80*MS+2,
            Raw=base64.b64encode(json.dumps(body).encode()).decode())
        events.append(query)
    ident = len(events)+1
    body.update(type='observation_snapshot', seq=ident)
    events.append(dict(query, ID=ident, Direction='oic_to_ois', Type='observation_snapshot',
        ReceivedNS=T+81*MS, WriteStartedNS=0, WrittenNS=0, Dropped=True,
        Raw=base64.b64encode(json.dumps(body).encode()).decode()))


class ProbeReloadTests(unittest.TestCase):
    def test_raw_bindings_are_independently_typed_before_projection_equality(self):
        # Mutate only the actual wire bytes: Python equality must not let a
        # bool/float borrow the unchanged integer projection's validation.
        for name, index in (('old_register', 0), ('new_register', 6), ('selected_query', 8)):
            for field in ('epoch', 'generation', 'contract_version'):
                for value in (True, 1.0):
                    f, e, raw, base, _ = prep.fixture()
                    def change(body):
                        actual = body['exposures'][0]['binding'] if name.endswith('register') else body['binding']
                        actual[field] = value
                    mutate_body(raw, index, change)
                    with self.subTest(wire=name, field=field, value=value), self.assertRaises(ValueError):
                        check(f, e, raw, base)

    def test_actual_two_generations_and_full_target_window_pass(self):
        f,e,r,b,c=prep.fixture()
        self.assertEqual(check(f,e,r,b),1)
        g2.competition_targets(prep.health_rows(f,e),g2.History([r]),f,e)
        g2.competition_critical_query(dict(full_backlog_required=True,critical_query_witness=c,
            backlog_before=prep.sample(T+60*MS),native_started_ns=T+74*MS,observed_ns=T+100*MS),
            g2.History([r]),1,e['prepared_ns'],b,registered=f['probe_reload_witness']['new_bindings'])
        self.assertNotEqual(f['preload_queue_witness']['bindings'],f['probe_reload_witness']['new_bindings'])
        for round_ in (1,2):
            f,e,r,b,c=prep.fixture();f['round']=e['round']=round_;prep.add_reload_fixture(f,e,r,c)
            self.assertEqual(check(f,e,r,b),1)
            g2.competition_targets(prep.health_rows(f,e),g2.History([r]),f,e)

    def test_all_success_fields_are_required_and_complete_types_are_not_equality_aliases(self):
        template=prep.fixture()[0]['probe_reload_witness']
        for key in template:
            for side in ('full','end','both'):
                for missing in (False,True):
                    f,e,r,b,_=prep.fixture()
                    owners=[f,e] if side=='both' else [f if side=='full' else e]
                    for owner in owners:
                        if missing:owner['probe_reload_witness'].pop(key)
                        else:owner['probe_reload_witness'][key]=None
                    with self.subTest(key=key,side=side,missing=missing),self.assertRaises((ValueError,TypeError,KeyError)):
                        check(f,e,r,b)
        for key,value in (('request_index',True),('request_index',1.0),('observation_attempts',True),
                          ('ois_pid',101.0),('oic_pid',102.0),('accepted_ns',float(T+25*MS+26)),
                          ('result',1),('schema','unknown'),('notify_method','ignored'),
                          ('write_error_class','deadline'),('notify_error_class','interrupted')):
            f,e,r,b,_=prep.fixture();f['probe_reload_witness'][key]=value;sync(f,e)
            with self.subTest(key=key,value=value),self.assertRaises(ValueError):check(f,e,r,b)

    def test_snapshot_prefixes_are_original_contiguous_and_bound_to_the_write(self):
        changes=[('before_prefix','event_count',7),('before_prefix','last_event_id',True),
                 ('before_prefix','returned_ns',T+17*MS-1),('before_prefix','returned_ns',T+25*MS+6),
                 ('accepted_prefix','event_count',7),('accepted_prefix','last_event_id',7),
                 ('accepted_prefix','returned_ns',T+25*MS+19),('last_prefix','last_event_id',9)]
        for key,field,value in changes:
            f,e,r,b,_=prep.fixture();f['probe_reload_witness'][key][field]=value;sync(f,e)
            with self.subTest(key=key,field=field,value=value),self.assertRaises(ValueError):check(f,e,r,b)
        f,e,r,b,_=prep.fixture();w=f['probe_reload_witness'];w['observation_attempts']=513;sync(f,e)
        self.assertEqual(check(f,e,r,b),1)  # 512 is diagnostic capacity, not a new input deadline.

    def test_request_notification_acceptance_and_original_deadline_are_not_renewed(self):
        for field,value in (
            ('write_started_ns',T+25*MS+3),('write_returned_ns',T+25*MS+4),
            ('notify_started_ns',T+25*MS+5),('notify_returned_ns',T+25*MS+26),
            ('accepted_ns',T+25*MS+24),('accepted_ns',T+26*MS+1),
            ('accepted_ns',T+4*T),('request_index',2),('config_after_sha256','a'*64)):
            f,e,r,b,_=prep.fixture();f['probe_reload_witness'][field]=value;sync(f,e)
            with self.subTest(field=field,value=value),self.assertRaises(ValueError):check(f,e,r,b)
        for method in ('sighup','poll'):
            f,e,r,b,_=prep.fixture();f['probe_reload_witness']['notify_method']=method;sync(f,e)
            self.assertEqual(check(f,e,r,b),1)
        f,e,first=prep.PreDialOrderTests().inputs()
        first['before_export']['completed_ns']=f['probe_reload_witness']['write_started_ns']+1
        with self.assertRaises(ValueError):g2.competition_export_first(f,e,first)

    def test_probe_canonical_digest_is_independent_and_all_fixed_parameters_remain(self):
        f,e,r,b,_=prep.fixture();plan=f['probe_reload_witness']['probes'][0]
        self.assertEqual(g2.competition_probe_digest(plan['old'],'/')[0],plan['old_digest'])
        for field,value in (('kind','other'),('timeout_ms',201),('interval_ms',400),
             ('validity_ms',1600),('jitter_ms',0),('timeout_ms',200.0),('ready_statuses',[True,204]),
             ('allowed_cidrs',['0.0.0.0/0']),('tls_ca_sha256','a'*64),('not_ready_statuses',[500]),
             ('url','http://127.0.0.1:30000/g2-competition-reload/1'),
             ('url','http://127.0.0.1:30000/g2-competition-reload/0?x'),
             ('url','http://127.0.0.1:30000/g2-competition-reload/0#'),
             ('url','http://localhost:30000/g2-competition-reload/0'),
             ('url','http://127.0.0.1:030000/g2-competition-reload/0')):
            f,e,r,b,_=prep.fixture();f['probe_reload_witness']['probes'][0]['new'][field]=value;sync(f,e)
            with self.subTest(field=field,value=value),self.assertRaises(ValueError):check(f,e,r,b)
        for side in ('old','new'):
            f,e,r,b,_=prep.fixture();w=f['probe_reload_witness'];w['probes'][0][side+'_digest']='f'*64
            w[side+'_bindings']['lab-a/http']['contract_digest']='f'*64
            if side=='old':f['preload_queue_witness']['bindings']['lab-a/http']['contract_digest']='f'*64
            mutate_body(r,0 if side=='old' else 6,lambda body:body['exposures'][0]['binding'].update(contract_digest='f'*64))
            sync(f,e)
            with self.subTest(side=side),self.assertRaisesRegex(ValueError,'compilation'):
                g2.competition_probe_reload(f,e,g2.History([r]),b)

    def test_registered_observation_must_increment_once_and_keep_target_session(self):
        for field,value in (('observation_revision','ob_1'),('observation_revision','ob_3'),
                            ('revision','rv_'+'b'*26),('epoch',2),('generation',2),
                            ('contract_version',True),('contract_version',1.0),('source_id','remote')):
            f,e,r,b,_=prep.fixture();w=f['probe_reload_witness'];w['new_bindings']['lab-a/http'][field]=value
            def mutate(body):
                exposure=body['exposures'][0];exposure['binding'][field]=value
                if field=='revision':exposure['revision']=value
            mutate_body(r,6,mutate);sync(f,e)
            with self.subTest(field=field,value=value),self.assertRaises(ValueError):
                g2.competition_probe_reload(f,e,g2.History([r]),b)
        for field in ('old_register','old_register_ack','new_register','new_register_ack'):
            for value in (True,1.0):
                f,e,r,b,_=prep.fixture();f['probe_reload_witness'][field]['conn_id']=value;sync(f,e)
                with self.subTest(field=field,value=value),self.assertRaises(ValueError):check(f,e,r,b)

    def test_extra_registration_or_reconnect_is_not_hidden_by_last_witness(self):
        for typ,index in (('register',6),('register_ack',7)):
            f,e,r,b,_=prep.fixture();event=copy.deepcopy(r['proxy']['Events'][index])
            body=json.loads(base64.b64decode(event['Raw']));body['seq']=25
            event.update(ID=25,ReceivedNS=T+90*MS,WriteStartedNS=T+90*MS+1,WrittenNS=T+90*MS+2,
                         Raw=base64.b64encode(json.dumps(body).encode()).decode())
            r['proxy']['Events'].append(event)
            with self.subTest(typ=typ),self.assertRaisesRegex(ValueError,'extra registration'):check(f,e,r,b)

    def test_registration_write_return_may_follow_the_reply_but_not_original_snapshot(self):
        f,e,r,b,_=prep.fixture();reg=r['proxy']['Events'][6]
        reg['WrittenNS']=T+25*MS+24
        f['probe_reload_witness']['new_register']['written_ns']=reg['WrittenNS'];sync(f,e)
        self.assertGreater(reg['WrittenNS'],r['proxy']['Events'][7]['ReceivedNS'])
        self.assertEqual(check(f,e,r,b),1)
        reg['WrittenNS']=T+25*MS+26
        f['probe_reload_witness']['new_register']['written_ns']=reg['WrittenNS'];sync(f,e)
        with self.assertRaisesRegex(ValueError,'not written'):check(f,e,r,b)

    def test_old_query_and_snapshot_can_cross_new_register_and_still_consume_quota(self):
        for index,late_query in ((2,False),(2,True),(8,False)):
            f,e,r,b,_=prep.fixture();append_response(r,index,late_query=late_query)
            self.assertGreater(r['proxy']['Events'][-1]['ReceivedNS'],f['probe_reload_witness']['new_register']['written_ns'])
            with self.subTest(query_index=index,late_query=late_query):self.assertEqual(check(f,e,r,b),1)
        f,e,r,b,_=prep.fixture();append_response(r,2)
        r['proxy']['Events'][2]['WrittenNS']=T+90*MS
        self.assertEqual(check(f,e,r,b),1)  # eventual query completion is not backfilled into the original Snapshot.

    def test_unknown_third_generation_and_mixed_drop_binding_are_rejected(self):
        for which in ('query','snapshot','both'):
            f,e,r,b,_=prep.fixture();append_response(r,2)
            for index in ([2,24] if which=='both' else [2 if which=='query' else 24]):
                mutate_body(r,index,lambda body:body['binding'].update(observation_revision='ob_3'))
            with self.subTest(which=which),self.assertRaises(ValueError):check(f,e,r,b)
        for count in (32,33):
            f,e,r,b,_=prep.fixture()
            for i in range(count):append_response(r,2 if i%2 else 8)
            with self.subTest(count=count):
                if count==32:self.assertEqual(check(f,e,r,b),1)
                else:
                    with self.assertRaisesRegex(ValueError,'Drop history exceeds'):check(f,e,r,b)

    def test_latest_eight_cannot_borrow_old_inventory_or_filter_an_old_source(self):
        for stage in ('pre_dial_query_witness','final_query_witness'):
            f,e,r,b,_=prep.fixture();q=f[stage]['queries'][0]
            old=copy.deepcopy(f['preload_queue_witness']['bindings'][q['binding']['exposure_id']])
            q['binding']=old
            mutate_body(r,q['event_id']-1,lambda body:body.update(binding=old))
            with self.subTest(stage=stage),self.assertRaises(ValueError):check(f,e,r,b)

    def test_window_health_is_the_entire_original_set_not_a_filtered_new_path_subset(self):
        for uri in ('/','/other','/g2-competition-reload/1'):
            f,e,r,b,_=prep.fixture();rows=prep.health_rows(f,e)
            original=rows[2]['health']['Records'][0]
            old=dict(original,ID=2,RequestURI=uri,StartedNS=original['StartedNS']+1,
                     CancelledNS=original['CancelledNS']+1,FinishedNS=original['FinishedNS']+1)
            rows[2]['health']['Records'].append(old);rows[2].update(attempts=2,timeouts=2)
            with self.subTest(uri=uri),self.assertRaisesRegex(ValueError,'old/other path'):
                g2.competition_targets(rows,g2.History([r]),f,e)
        f,e,r,b,_=prep.fixture();rows=prep.health_rows(f,e)
        rows[2]['health']['Records'].append(dict(rows[2]['health']['Records'][0],ID=2,
            RequestURI='/',StartedNS=e['slow_input_applied_ns']-1))
        g2.competition_targets(rows,g2.History([r]),f,e)  # pre-window work remains visible, uncounted as before.


class ProbeReloadArchiveTests(unittest.TestCase):
    def test_real_recorded_loader_uses_exact_cbd_eight_module_closure(self):
        directory=ROOT/'sources/product-pre-probe-reload'
        mapping=json.loads((directory/'source-map.json').read_text(encoding='utf-8'))
        self.assertEqual(mapping['subject'],'cbd94ce9c47fdc1e4b82b6bdb0d4bb8589517073')
        self.assertEqual(len(mapping['files']),8)
        for info in mapping['files'].values():
            raw=(ROOT.parent/info['archived_path']).read_bytes()
            self.assertEqual((len(raw),hashlib.sha256(raw).hexdigest()),(info['bytes'],info['sha256']))
            self.assertEqual(hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest(),info['git_blob'])
        def load(name,path):
            spec=importlib.util.spec_from_file_location(name,path);mod=importlib.util.module_from_spec(spec)
            exec(compile(path.read_bytes(),str(path),'exec'),mod.__dict__);return mod
        verify=load('probe_reload_verify',ROOT/'verify.py');old=load('probe_reload_old',directory/'g2_analyze.py')
        case={'g2_behavior':old.expected_spec(),'g2_clock':{},'local_execution':{},'vulndb_snapshot':{},'dependency_quality':{}}
        with tempfile.TemporaryDirectory() as temp:
            run=Path(temp)
            (run/'source-manifest.json').write_text(json.dumps({k:v['sha256'] for k,v in mapping['files'].items()}),encoding='utf-8')
            (run/'manifest.json').write_text(json.dumps({'case':case}),encoding='utf-8')
            chosen=verify.recorded_module(run)
            self.assertEqual(Path(chosen.__file__).parent,directory)
            self.assertEqual(len(chosen._recorded_dependency_hashes),7)
            for name in chosen._recorded_dependency_hashes:
                self.assertEqual(Path(verify.recorded_helper(chosen,name,case).__file__).parent,directory)
            recorded=chosen._sibling('g2_analyze.py')
            self.assertEqual(recorded.analyze_g2(old.expected_spec(),[],{}),old.analyze_g2(old.expected_spec(),[],{}))
            for current,other in ((g2,recorded),(recorded,g2)):
                with self.assertRaisesRegex(ValueError,'preregistration'):current.analyze_g2(other.expected_spec(),[],{})
        expected=old.expected_spec()
        expected['competition_preparation']['strategy']='six-source-probe-reload-pre-dial-fresh-v1'
        expected['competition_preparation']['reload_witness']='path-only-register-transition-v1'
        self.assertEqual(expected,g2.expected_spec())


if __name__=='__main__':unittest.main()
