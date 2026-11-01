"""Recompute bounded conclusions from raw observations; no chosen architecture input."""
import json
import statistics
from pathlib import Path

def analyze(case, rows, repeats):
    problems = []
    if case == "probe":
        probes = [x for x in rows if x["kind"] == "probe"]
        schedules = [x for x in rows if x["kind"] == "schedule"]
        if len(probes) != 3*repeats or len(schedules) != 2*repeats:
            problems.append("missing observations")
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
                "problems":problems, "tcp_success_but_application_not_ready":mismatches,
                "probe_observations":len(probes), "schedule":schedule,
                "scope":"Loopback HTTP fixtures; does not establish OIC readiness correctness, WAN latency, or HA.",
                "model_rows_excluded":sum(x["kind"]=="freshness_model" for x in rows)}
    if case == "retry":
        mutations = [x for x in rows if x["kind"]=="mutation"]
        if len(mutations) != 2*repeats:
            problems.append("missing observations")
        counts = {}
        for mode, expected in [("no_replay",1),("blind_replay",2)]:
            selected = [x for x in mutations if x["mode"]==mode]
            if len(selected)!=repeats or any(x["counter_status"]!=200 or x["server_counter"]!=expected
                or len(x["client_errors"])!=expected or not all(x["client_errors"]) for x in selected):
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
