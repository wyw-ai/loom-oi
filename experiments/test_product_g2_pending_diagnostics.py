import copy
import json
import unittest

from product import g2_pending_diagnostics as d


def fixture():
    b = {"access_id": "lab-a", "epoch": 3, "generation": 2, "exposure_id": "lab-a/http", "revision": "rv_" + "a"*26,
         "observation_revision": "ob_1", "contract_digest": "a"*64, "source_id": "local", "contract_version": 1}
    ctx = {"source_sha": "a"*40, "metadata_source_sha": "a"*40, "log_source_sha": "a"*40,
           "test": "TestTVG2_11_ControlExportCompetition/sqlite/round_0", "backend": "sqlite", "round": 0,
           "ois_pid": 123, "run_id": "run_" + "a"*26, "epoch": 3, "bindings": [copy.deepcopy(b)]}
    counts = dict(observation_capacity=1, observation_backlog=1, pending_queries=1, dirty_marked=0)
    stats = dict(channels=dict(preparing=0, open=0, closing=0, closed=0), tickets=0, accesses_online=1, relay_bytes=0,
                 max_buffered_bytes=0, store_healthy=True, epoch=3, process={}, observation=copy.deepcopy(counts))
    call = dict(started_ns=1_000_000_000, returned_ns=1_010_000_000, context_deadline_ns=2_000_000_000, context_state="active", error_class="none")
    req = dict(attempt=1, phase="pre_dial_full", sample_id="1"*32, id_error=None, ois_call=call, ois_stats=stats)
    meta = {"schema": d.META_SCHEMA, **{k:ctx[k] for k in ("test", "backend", "round", "ois_pid")}, "requests": [req], "overflow": False, "omitted": 0}
    payload = dict(schema=d.SCHEMA, sample_id="1"*32, pid=123, run_id=ctx["run_id"], epoch=3, query_slots=8, query_ms=500, refresh_ms=500,
                   **counts, complete=True, reasons=[], omitted=dict(pending=0,sources=0,query_order=0), query_order=["lab-a/http"], query_cursor=0,
                   pending=[dict(query_id="2"*32, binding=copy.deepcopy(b), age_ns=600_000_000, remaining_ns=-100_000_000,
                                 binding_current=True, session_current=True, online=True)],
                   sources=[dict(binding=copy.deepcopy(b), query_id="2"*32, dirty=False, periodic_only=False, online=True,
                                 control_version="oi.control.v2", current_generation=2, refresh_at_set=True, refresh_remaining_ns=-90_000_000)])
    return meta, [dict(time="arbitrary-wall-clock", msg=d.MESSAGE, pending_identity=payload)], ctx


def run(meta, logs, ctx):
    return d.analyze(meta, logs, context=ctx)


class PendingDiagnosticsTests(unittest.TestCase):
    def test_success_uses_request_interval_not_wall_clock_or_combined_sample(self):
        meta, logs, ctx = fixture()
        result = run(meta, logs, ctx)
        self.assertEqual(result["status"], "complete", result)
        self.assertEqual(result["business_verdict"], "not_evaluated")
        q = result["requests"][0]["pending"][0]
        self.assertEqual(q["started_interval_ns"], [400_000_000, 410_000_000])
        self.assertEqual(q["deadline_interval_ns"], [900_000_000, 910_000_000])
        self.assertEqual(result["requests"][0]["sources"][0]["refresh_interval_ns"], [910_000_000, 920_000_000])
        logs[0]["time"] = "9999-01-01T00:00:00Z"
        self.assertEqual(result, run(meta, logs, ctx))

    def test_failure_or_late_log_never_gains_success_window(self):
        for mode in ("error", "partial_body", "canceled", "deadline", "late_return", "goexit", "not_called", "entropy_failure"):
            with self.subTest(mode=mode):
                meta, logs, ctx = fixture(); req = meta["requests"][0]; call = req["ois_call"]
                if mode in ("error", "partial_body"):
                    call["error_class"] = "eof"; req["ois_stats"] = None
                elif mode in ("canceled", "deadline"):
                    call["context_state"] = mode
                elif mode == "late_return":
                    call["context_deadline_ns"] = call["returned_ns"]-1
                elif mode == "goexit":
                    call.update(returned_ns=None, error_class=None, context_state=None); req["ois_stats"] = None
                elif mode == "not_called":
                    req.update(sample_id=None, ois_call=None, ois_stats=None); logs=[]
                elif mode == "entropy_failure":
                    req.update(sample_id=None, id_error="entropy_error"); logs=[]
                result = run(meta, logs, ctx)
                self.assertEqual(result["status"], "incomplete", result)
                self.assertIsNone(result["requests"][0]["request_window_ns"])
                for q in result["requests"][0]["pending"]:
                    self.assertNotIn("deadline_interval_ns", q)

    def test_missing_duplicate_unknown_and_identity_conflicts(self):
        def apply(mode, m, logs, c):
            p=logs[0]["pending_identity"];r=m["requests"][0]
            if mode == "missing_log": logs.clear()
            elif mode == "duplicate_log": logs.append(copy.deepcopy(logs[0]))
            elif mode == "duplicate_request": m["requests"].append({**copy.deepcopy(r), "attempt":2})
            elif mode == "wrong_id": p["sample_id"]="f"*32
            elif mode == "pid": p["pid"]+=1
            elif mode == "epoch": p["epoch"]+=1
            elif mode == "run": p["run_id"]="run_"+"b"*26
            elif mode == "round": m["round"]=1
            elif mode == "backend": m["backend"]="postgres"
            elif mode == "test": m["test"]="another test"
            elif mode == "source": c["log_source_sha"]="b"*40
            elif mode == "metadata_source": c["metadata_source_sha"]="b"*40
            elif mode == "schema": p["schema"]="future"
            elif mode == "unknown_key": p["extra"]=0
            elif mode == "missing_key": del p["pending"]
            elif mode == "count": p["dirty_marked"]=1
            elif mode == "http_count": r["ois_stats"]["observation"]["pending_queries"]+=1
            elif mode == "http_epoch": r["ois_stats"]["epoch"]+=1
            elif mode == "observation_missing": del r["ois_stats"]["observation"]
            elif mode == "duplicate_owner": p["pending"].append(copy.deepcopy(p["pending"][0])); p["pending_queries"]=2; r["ois_stats"]["observation"]["pending_queries"]=2
            elif mode == "source_binding": p["sources"][0]["binding"]["generation"]=999
            elif mode == "query_binding": p["pending"][0]["binding"]["revision"]="rv_"+"b"*26
            elif mode == "binding_bool": p["pending"][0]["binding"]["contract_version"]=True
            elif mode == "missing_deadline": del r["ois_call"]["context_deadline_ns"]
            elif mode == "returned_before_start": r["ois_call"]["returned_ns"]=0
            elif mode == "failed_complete": r["ois_call"]["error_class"]="eof"
            elif mode == "float_time": p["pending"][0]["age_ns"]=1.0
            elif mode == "bool_count": p["pending_queries"]=True
            elif mode == "negative_age": p["pending"][0]["age_ns"]=-1
            elif mode == "changed_deadline": p["pending"][0]["remaining_ns"]+=1
            elif mode == "wrong_current": p["pending"][0]["binding_current"]=False
        modes=("missing_log duplicate_log duplicate_request wrong_id pid epoch run round backend test source metadata_source schema unknown_key missing_key count http_count http_epoch observation_missing duplicate_owner source_binding query_binding binding_bool missing_deadline returned_before_start failed_complete float_time bool_count negative_age changed_deadline wrong_current").split()
        for mode in modes:
            with self.subTest(mode=mode):
                m,l,c=fixture();apply(mode,m,l,c);result=run(m,l,c)
                self.assertEqual(result["status"], "incomplete" if mode=="missing_log" else "conflict", result)

    def test_incomplete_cap_and_placeholders_do_not_fabricate_missing(self):
        for mode in ("capacity", "identity", "zero_refresh", "payload", "invalid_time"):
            with self.subTest(mode=mode):
                m,l,c=fixture();p=l[0]["pending_identity"]
                if mode=="zero_refresh":
                    p["sources"][0].update(refresh_at_set=False,refresh_remaining_ns=None)
                else:
                    p["complete"]=False
                    if mode=="capacity":
                        p["reasons"]=["capacity"];p["omitted"]["pending"]=1;p["pending_queries"]=2;m["requests"][0]["ois_stats"]["observation"]["pending_queries"]=2
                    elif mode=="identity":
                        p["reasons"]=["invalid_identity"];p["pending"][0].update(query_id=None,binding=None);p["query_order"]=[None]
                    elif mode=="invalid_time":
                        p["reasons"]=["invalid_time"];p["pending"][0].update(age_ns=None,remaining_ns=None)
                    else:
                        p["reasons"]=["payload"];p.update(pending=[],sources=[],query_order=[],omitted=dict(pending=1,sources=1,query_order=1))
                result=run(m,l,c)
                self.assertEqual(result["status"], "complete" if mode=="zero_refresh" else "incomplete",result)
                if mode=="zero_refresh":self.assertIsNone(result["requests"][0]["sources"][0]["refresh_interval_ns"])

    def test_old_binding_offline_is_a_fact_not_repaired(self):
        m,l,c=fixture();p=l[0]["pending_identity"]
        old=copy.deepcopy(p["pending"][0]["binding"]);old["generation"]=1;c["bindings"].append(old)
        p["pending"][0].update(binding=old,binding_current=False,session_current=False,online=False)
        p["sources"][0].update(online=False,current_generation=0,control_version=None)
        result=run(m,l,c)
        self.assertEqual(result["status"],"complete",result)
        self.assertFalse(result["requests"][0]["pending"][0]["session_current"])

    def test_finite_schema_and_unbounded_input_rejected(self):
        for mode in ("request513","pending9","sources17","order17","unknown_reason","huge_identity","int64","nan","incomplete_no_reason","complete_omission","periodic_not_dirty","refresh_missing","query_cursor"):
            with self.subTest(mode=mode):
                m,l,c=fixture();p=l[0]["pending_identity"]
                if mode=="request513":m["requests"]*=513
                elif mode=="pending9":p["pending"]*=9
                elif mode=="sources17":p["sources"]*=17
                elif mode=="order17":p["query_order"]*=17
                elif mode=="unknown_reason":p.update(complete=False,reasons=["anything"])
                elif mode=="huge_identity":p["pending"][0]["binding"]["revision"]="x"*65536
                elif mode=="int64":p["pending"][0]["remaining_ns"]=2**63
                elif mode=="nan":p["pending"][0]["age_ns"]=float("nan")
                elif mode=="incomplete_no_reason":p["complete"]=False
                elif mode=="complete_omission":p["omitted"]["query_order"]=1
                elif mode=="periodic_not_dirty":p["sources"][0]["periodic_only"]=True
                elif mode=="refresh_missing":p["sources"][0]["refresh_remaining_ns"]=None
                elif mode=="query_cursor":p["query_cursor"]=1
                self.assertEqual(run(m,l,c)["status"],"conflict")

    def test_log_decode_truncation_duplicate_key_and_nonfinite(self):
        for raw in ('{"msg":', '{"msg":"x","msg":"y"}', '{"value":NaN}', '[]'):
            with self.subTest(raw=raw):
                rows, problems=d.decode_log(raw)
                self.assertFalse(rows);self.assertTrue(problems)
                m,l,c=fixture()
                self.assertNotEqual(d.analyze(m,l,context=c,decode_problems=problems)["status"],"complete")

    def test_optional_metadata_does_not_replace_primary_preparation(self):
        import test_product_g2_preparation as original
        full, end, raw, base, _ = original.fixture()
        history = original.g2.History([raw])
        expected = original.g2.competition_preparation(full, end, history, base)
        meta, _, _ = fixture()
        full["pending_identity_diagnostics"] = copy.deepcopy(meta)
        end["pending_identity_diagnostics"] = copy.deepcopy(meta)
        self.assertEqual(original.g2.competition_preparation(full, end, history, base), expected)
        # A complete optional log cannot repair a failed original young input.
        full["pre_dial_query_witness"]["max_age_ms"] = 200
        with self.assertRaises(ValueError):
            original.g2.competition_preparation(full, end, history, base)


if __name__ == "__main__":
    unittest.main()
