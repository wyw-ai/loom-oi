"""Recompute bounded conclusions from raw observations; no chosen architecture input."""
import json
import statistics
from pathlib import Path

def analyze(case, rows, repeats):
    problems = []
    def keys_ok(selected, field, variants):
        keys=[(x.get("repeat"),x.get(field)) for x in selected]
        expected={(i,v) for i in range(repeats) for v in variants}
        return len(keys)==len(expected) and set(keys)==expected
    if case == "probe":
        probes = [x for x in rows if x["kind"] == "probe"]
        schedules = [x for x in rows if x["kind"] == "schedule"]
        if len(probes) != 3*repeats or len(schedules) != 2*repeats:
            problems.append("missing observations")
        if not keys_ok(probes,"target",["/ready","/not-ready","/slow"]):
            problems.append("probe trial coverage invalid")
        if not keys_ok(schedules,"mode",["serial","independent"]):
            problems.append("schedule trial coverage invalid")
        # These controls ensure fault injection actually produced the intended conditions.
        controls = all(
            sum(x["target"] == target and x["status"] == status and x["error"] == error
                and x["tcp_reachable"] for x in probes) == repeats
            for target, status, error in [("/ready",200,None),("/not-ready",503,None),("/slow",None,"TimeoutError")]
        )
        if not controls:
            problems.append("healthy/unready/timeout control failed")
        mismatches = sum(x["tcp_reachable"] and x["status"] != 200 for x in probes)
        schedule = {}
        for mode in ["serial", "independent"]:
            values = [x["fast_completion_ms"] for x in schedules if x["mode"] == mode]
            if len(values) != repeats:
                problems.append("missing scheduling variant")
            if any(x["fast_status"] != 200 or x["slow_error"] != "TimeoutError"
                   for x in schedules if x["mode"] == mode):
                problems.append("scheduler control failed")
            schedule[mode] = {"n":len(values), "median_ms":statistics.median(values) if values else None,
                              "min_ms":min(values) if values else None,"max_ms":max(values) if values else None}
        return {"evidence_class":"mechanism_fixture", "status":"inconclusive" if problems else "observed",
                "problems":problems, "tcp_success_without_timely_application_success":mismatches,
                "explicit_http_503":sum(x["status"]==503 for x in probes),
                "timeout_outcome_unknown":sum(x["error"]=="TimeoutError" for x in probes),
                "probe_observations":len(probes), "schedule":schedule,
                "scope":"Loopback HTTP fixtures; does not establish OIC readiness correctness, WAN latency, or HA.",
                "model_rows_excluded":sum(x["kind"]=="freshness_model" for x in rows)}
    if case == "retry":
        mutations = [x for x in rows if x["kind"]=="mutation"]
        if len(mutations) != 2*repeats:
            problems.append("missing observations")
        if not keys_ok(mutations,"mode",["no_replay","blind_replay"]):
            problems.append("mutation trial coverage invalid")
        counts = {}
        for mode, expected in [("no_replay",1),("blind_replay",2)]:
            selected = [x for x in mutations if x["mode"]==mode]
            if len(selected)!=repeats or any(x["counter_status"]!=200 or x["server_counter"]!=expected
                or x["attempts"]!=expected or len(x["client_errors"])!=expected or not all(x["client_errors"]) for x in selected):
                problems.append("fault/effect control failed for "+mode)
            counts[mode] = [x["server_counter"] for x in selected]
        return {"evidence_class":"mechanism_fixture","status":"inconclusive" if problems else "observed",
                "problems":problems,"server_counts":counts,
                "scope":"Response loss after a local HTTP effect. No OI relay, durable dedup, or exactly-once implementation tested."}
    raise ValueError("unknown case")

if __name__ == "__main__":
    import sys
    run=Path(sys.argv[1]);manifest=json.loads((run/"manifest.json").read_text())
    rows=[json.loads(x) for x in (run/"raw.jsonl").read_text().splitlines() if x]
    print(json.dumps(analyze(manifest["case"],rows,manifest["repeats"]),indent=2,sort_keys=True))
