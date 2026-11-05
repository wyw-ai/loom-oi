"""Early-arm evidence uses an actual pre-arm prefix, not receive-time guessing."""
import base64
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

import test_product_g2_preparation as prep

g2 = prep.g2
MS, T, ROOT = prep.MS, prep.T, prep.ROOT


def fixture_with_drop():
    """Complete preparation with a real matching query and preload-prefix Drop.

    The query was already published in the pre-arm prefix. The response's
    receive timestamp precedes arm, but publication follows the saved cursor.
    """
    full, end, raw, base, critical = prep.fixture()
    query = raw['proxy']['Events'][2]
    query.update(ReceivedNS=T+MS+1, WriteStartedNS=T+MS+2, WrittenNS=T+MS+3)
    body = json.loads(base64.b64decode(query['Raw']))
    body.update(type='observation_snapshot', seq=4)
    dropped = dict(query, ID=4, Type='observation_snapshot', Direction='oic_to_ois',
        ReceivedNS=T+MS+8, WriteStartedNS=0, WrittenNS=0, Dropped=True,
        Raw=base64.b64encode(json.dumps(body).encode()).decode())
    raw['proxy']['Events'][3] = dropped
    for row in (full, end):
        row.update(drop_arm_before_event_id=3, drop_arm_before_snapshot_returned_ns=T+MS+5,
                   drop_arm_started_ns=T+MS+10, drop_arm_completed_ns=T+MS+11)
    return full, end, raw, base, critical


def check(full, end, raw, base):
    g2.competition_window(full, end)
    return g2.competition_preparation(full, end, g2.History([raw]), base)


class EarlyArmPreparationTests(unittest.TestCase):
    def test_complete_preload_drop_and_received_before_arm_are_legal(self):
        f,e,r,b,critical = fixture_with_drop()
        self.assertLess(r['proxy']['Events'][3]['ReceivedNS'], f['drop_arm_started_ns'])
        self.assertLess(f['drop_arm_before_event_id'], r['proxy']['Events'][3]['ID'])
        self.assertLessEqual(r['proxy']['Events'][3]['ID'], f['preload_queue_witness']['wire_last_event_id'])
        # Preload has no Full/young requirement; the later two still do.
        f['preload_queue_witness']['sample']['ois']['observation'].update(
            pending_queries=0, observation_backlog=0, dirty_marked=0)
        self.assertEqual(check(f,e,r,b), 1)
        g2.competition_critical_query(dict(full_backlog_required=True,
            critical_query_witness=critical, backlog_before=prep.sample(T+60*MS),
            native_started_ns=T+74*MS, observed_ns=T+100*MS),
            g2.History([r]), 1, e['prepared_ns'], b, f['preload_queue_witness']['bindings'])

    def test_empty_and_nonempty_real_pre_arm_prefixes(self):
        for cursor in (0, 1, 2):
            f,e,r,b,_ = prep.fixture()
            for row in (f,e): row['drop_arm_before_event_id'] = cursor
            with self.subTest(cursor=cursor): self.assertEqual(check(f,e,r,b), 1)

    def test_new_scalars_are_required_typed_and_identical_in_both_events(self):
        for field, invalid in (
            ('drop_arm_before_event_id', (None, True, 2.0, '2', -1, 1 << 63)),
            ('drop_arm_before_snapshot_returned_ns', (None, False, float(T+MS), str(T+MS), 0, -1))):
            for owner in ('full','complete','both'):
                for value in invalid:
                    f,e,r,b,_ = prep.fixture()
                    for row in ([f,e] if owner=='both' else [f if owner=='full' else e]): row[field]=value
                    with self.subTest(field=field,owner=owner,value=value),self.assertRaises((ValueError,KeyError)):
                        check(f,e,r,b)
                f,e,r,b,_ = prep.fixture()
                for row in ([f,e] if owner=='both' else [f if owner=='full' else e]): row.pop(field)
                with self.subTest(field=field,owner=owner,missing=True),self.assertRaises((ValueError,KeyError)):
                    check(f,e,r,b)
        for field in ('drop_arm_before_event_id','drop_arm_before_snapshot_returned_ns'):
            f,e,r,b,_=prep.fixture();e[field]+=1
            with self.subTest(field=field,mismatch=True),self.assertRaises(ValueError): check(f,e,r,b)

    def test_prefix_is_not_future_truncated_or_borrowed_from_final_history(self):
        changes = {
            'future_receive': {'drop_arm_before_event_id': 3},
            'after_preload': {'drop_arm_before_event_id': 9},
            'before_prepare': {'drop_arm_before_snapshot_returned_ns': T},
            'after_arm': {'drop_arm_before_snapshot_returned_ns': T+MS+2},
            'after_set': {'drop_arm_started_ns': T+3*MS, 'drop_arm_completed_ns': T+3*MS+1},
            'old_probe_first_order': {'drop_arm_started_ns': T+25*MS, 'drop_arm_completed_ns': T+25*MS+1},
            'partial_arm': {'drop_arm_count': 31},
            'over_arm': {'drop_arm_count': 33},
        }
        for name, change in changes.items():
            f,e,r,b,_=prep.fixture();f.update(change);e.update(change)
            with self.subTest(name=name),self.assertRaises(ValueError): check(f,e,r,b)
        f,e,r,b,_=prep.fixture();r['proxy']['Events'][0]['ID']=2
        with self.assertRaises(ValueError): check(f,e,r,b)

    def test_pre_arm_drop_cannot_be_reclassified_as_early_arm_input(self):
        f,e,r,b,_=fixture_with_drop()
        for row in (f,e):
            row.update(drop_arm_before_event_id=4,drop_arm_before_snapshot_returned_ns=T+MS+9)
        with self.assertRaisesRegex(ValueError,'pre-arm prefix'): check(f,e,r,b)

    def test_drop_cannot_be_critical_injected_forwarded_or_another_binding(self):
        for name in ('critical','injected','write_failed','written','other_connection','other_binding',
                     'null_binding','list_binding','boolean_contract'):
            f,e,r,b,_=fixture_with_drop();event=r['proxy']['Events'][3]
            body=json.loads(base64.b64decode(event['Raw']))
            if name=='critical': body['type']=event['Type']='close_channel'
            elif name=='injected':event.update(Injected=True,ReceivedNS=0)
            elif name=='write_failed':event['WriteFailed']=True
            elif name=='written':event.update(WriteStartedNS=T+MS+12,WrittenNS=T+MS+13)
            elif name=='other_connection':event['ConnID']=99
            else:
                if name=='null_binding':body['binding']=None
                elif name=='list_binding':body['binding']=[]
                elif name=='boolean_contract':body['binding']['contract_version']=True
                else:body['binding']['contract_digest']='b'*64
                query=r['proxy']['Events'][2];qb=json.loads(base64.b64decode(query['Raw']))
                qb['binding']=copy.deepcopy(body['binding'])
                query['Raw']=base64.b64encode(json.dumps(qb).encode()).decode()
            event['Raw']=base64.b64encode(json.dumps(body).encode()).decode()
            with self.subTest(name=name),self.assertRaises(ValueError): check(f,e,r,b)

    def test_actual_unused_drop_capacity_is_not_fabricated_consumption(self):
        for count in (1, 32, 33):
            f,e,r,b,_=fixture_with_drop()
            template=r['proxy']['Events'][3]
            for index in range(count-1):
                body=json.loads(base64.b64decode(template['Raw']));body['seq']=5+index
                r['proxy']['Events'].append(dict(template,ID=25+index,ReceivedNS=T+20*MS+index,
                    Raw=base64.b64encode(json.dumps(body).encode()).decode()))
            with self.subTest(count=count):
                if count==33:
                    with self.assertRaisesRegex(ValueError,'Drop history exceeds'):check(f,e,r,b)
                else:self.assertEqual(check(f,e,r,b),1)

    def test_dropped_query_and_snapshot_cannot_borrow_integer_binding_types(self):
        for event_index in (2,3):
            for field in ('epoch','generation','contract_version'):
                for value in (True,1.0):
                    f,e,r,b,_=fixture_with_drop();event=r['proxy']['Events'][event_index]
                    body=json.loads(base64.b64decode(event['Raw']))
                    body['binding'][field]=value
                    event['Raw']=base64.b64encode(json.dumps(body).encode()).decode()
                    with self.subTest(event_index=event_index,field=field,value=value),self.assertRaises(ValueError):
                        check(f,e,r,b)

    def test_dropped_query_original_forwarding_is_required(self):
        for name in ('direction','unwritten','write_failed','after_response','wrong_role',
                     'wrong_connection','injected','dropped','wrong_type'):
            f,e,r,b,_=fixture_with_drop();query=r['proxy']['Events'][2];response=r['proxy']['Events'][3]
            if name=='direction':query['Direction']='oic_to_ois'
            elif name=='unwritten':query.update(WriteStartedNS=0,WrittenNS=0)
            elif name=='write_failed':query.update(WrittenNS=0,WriteFailed=True)
            elif name=='after_response':query.update(WriteStartedNS=response['ReceivedNS']+1,WrittenNS=response['ReceivedNS']+2)
            elif name=='wrong_role':query['Role']='provider'
            elif name=='wrong_connection':query['ConnID']=99
            elif name=='injected':query.update(Injected=True,ReceivedNS=0)
            elif name=='dropped':query.update(Dropped=True,WriteStartedNS=0,WrittenNS=0)
            else:
                body=json.loads(base64.b64decode(query['Raw']));body['type']='observation_changed'
                query['Type']=body['type'];query['Raw']=base64.b64encode(json.dumps(body).encode()).decode()
            with self.subTest(name=name),self.assertRaises(ValueError):check(f,e,r,b)

    def test_query_final_write_return_may_follow_response_and_preload_snapshot(self):
        f,e,r,b,_=fixture_with_drop();query=r['proxy']['Events'][2]
        query['WrittenNS']=T+80*MS
        self.assertGreater(query['WrittenNS'],r['proxy']['Events'][3]['ReceivedNS'])
        self.assertGreater(query['WrittenNS'],f['preload_queue_witness']['wire_snapshot_returned_ns'])
        self.assertEqual(check(f,e,r,b),1)

    def test_early_arm_does_not_replace_final_or_critical_full_and_age(self):
        for stage in ('pre_dial_query_witness','final_query_witness','critical'):
            f,e,r,b,c=fixture_with_drop()
            w=c if stage=='critical' else f[stage]
            w['queries'][0]['written_ns']=0
            with self.subTest(stage=stage),self.assertRaises(ValueError):
                if stage!='critical':check(f,e,r,b)
                else:g2.competition_critical_query(dict(full_backlog_required=True,
                    critical_query_witness=c,backlog_before=prep.sample(T+60*MS),
                    native_started_ns=T+74*MS,observed_ns=T+100*MS),g2.History([r]),1,e['prepared_ns'],b)

    def test_exporter_baseline_still_follows_preload_acceptance(self):
        f,e,first=prep.PreDialOrderTests().inputs()
        # Before the strategy change, arm followed preload. Its old baseline
        # lower bound no longer suffices after moving arm earlier.
        first['before_export'].update(started_ns=T+4*MS,completed_ns=T+5*MS)
        self.assertLess(e['drop_arm_completed_ns'],first['before_export']['started_ns'])
        self.assertLess(first['before_export']['completed_ns'],e['preload_accepted_ns'])
        with self.assertRaisesRegex(ValueError,'input/final escaped'):
            g2.competition_export_first(f,e,first)


class EarlyArmArchiveTests(unittest.TestCase):
    def test_recorded_loader_selects_the_exact_eight_module_old_closure(self):
        directory=ROOT/'sources/product-pre-early-arm'
        mapping=json.loads((directory/'source-map.json').read_text(encoding='utf-8'))
        self.assertEqual(mapping['subject'],'dd62daab4f86db940be2c3d1752b7efc008398fb')
        self.assertEqual(set(Path(p).name for p in mapping['files']),{
            'analyze.py','g2_analyze.py','g2_clock_analyze.py','clock_guard.py','local_execution.py',
            'local_run_seal.py','dependency_quality.py','vulndb_snapshot.py'})
        for info in mapping['files'].values():
            raw=(ROOT.parent/info['archived_path']).read_bytes()
            self.assertEqual((len(raw),hashlib.sha256(raw).hexdigest()),(info['bytes'],info['sha256']))
            self.assertEqual(hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest(),info['git_blob'])
        def load(name,path):
            spec=importlib.util.spec_from_file_location(name,path);module=importlib.util.module_from_spec(spec)
            exec(compile(path.read_bytes(),str(path),'exec'),module.__dict__);return module
        verify=load('early_arm_verify',ROOT/'verify.py')
        old=load('old_pre_dial',directory/'g2_analyze.py')
        case={'g2_behavior':old.expected_spec(),'g2_clock':{},'local_execution':{},
              'vulndb_snapshot':{},'dependency_quality':{}}
        with tempfile.TemporaryDirectory() as temp:
            run=Path(temp)
            (run/'source-manifest.json').write_text(json.dumps({k:v['sha256'] for k,v in mapping['files'].items()}),encoding='utf-8')
            (run/'manifest.json').write_text(json.dumps({'case':case}),encoding='utf-8')
            selected=verify.recorded_module(run)
            self.assertEqual(Path(selected.__file__).parent,directory)
            self.assertEqual(len(selected._recorded_dependency_hashes),7)
            for name in selected._recorded_dependency_hashes:
                self.assertEqual(Path(verify.recorded_helper(selected,name,case).__file__).parent,directory)
            recorded=selected._sibling('g2_analyze.py')
            self.assertEqual(recorded.analyze_g2(recorded.expected_spec(),[],{}),old.analyze_g2(old.expected_spec(),[],{}))
            for current,other in ((g2,recorded),(recorded,g2)):
                with self.assertRaisesRegex(ValueError,'preregistration'):current.analyze_g2(other.expected_spec(),[],{})
        expected=copy.deepcopy(old.expected_spec())
        expected['competition_preparation']['strategy']='six-source-early-arm-pre-dial-fresh-v1'
        self.assertEqual(expected,g2.expected_spec())
        self.assertEqual((len(expected['groups']),sum(len(g['leaves']) for g in expected['groups'].values()),
            sum(g['resource'] for g in expected['groups'].values())),(108,130,106))


if __name__=='__main__':unittest.main()
