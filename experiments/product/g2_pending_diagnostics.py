"""Offline, diagnostic-only pairing of an existing HTTP call and OIS log.

The caller supplies provenance from verified source/artifact/process records.
This module checks that binding; it does not authenticate a filesystem or turn
these optional observations into a G2 verdict. No primary analyzer imports it.
"""
import json
import re

HEADER = "X-OI-Diagnostic-Sample"
MESSAGE = "observation_pending_snapshot"
SCHEMA = "g2-pending-identity-v1"
META_SCHEMA = "g2-pending-request-set-v1"
REASONS = {"capacity", "invalid_identity", "invalid_state", "invalid_time", "payload"}
COUNTS = ("observation_capacity", "observation_backlog", "pending_queries", "dirty_marked")
BINDING = {"access_id", "epoch", "generation", "exposure_id", "revision", "observation_revision", "contract_digest", "source_id", "contract_version"}
PAYLOAD = {"schema", "sample_id", "pid", "run_id", "epoch", "query_slots", "query_ms", "refresh_ms", *COUNTS, "complete", "reasons", "omitted", "pending", "sources", "query_order", "query_cursor"}
PENDING = {"query_id", "binding", "age_ns", "remaining_ns", "binding_current", "session_current", "online"}
SOURCE = {"binding", "query_id", "dirty", "periodic_only", "online", "control_version", "current_generation", "refresh_at_set", "refresh_remaining_ns"}
META = {"schema", "test", "backend", "round", "ois_pid", "requests", "overflow", "omitted"}
REQUEST = {"attempt", "phase", "sample_id", "id_error", "ois_call", "ois_stats"}
CALL = {"started_ns", "returned_ns", "context_deadline_ns", "context_state", "error_class"}
CONTEXT = {"source_sha", "metadata_source_sha", "log_source_sha", "test", "backend", "round", "ois_pid", "run_id", "epoch", "bindings"}
ERRORS = {"none", "deadline", "canceled", "eof", "closed", "timeout", "other"}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def keys(value, expected, name):
    require(type(value) is dict and set(value) == expected, name + " keys")


def integer(value, minimum=-(2**63), maximum=2**63-1):
    return type(value) is int and minimum <= value <= maximum


def text(value, pattern):
    return type(value) is str and re.fullmatch(pattern, value) is not None


def exposure(value):
    return text(value, r"[a-z0-9][a-z0-9-]{0,62}/[a-z0-9][a-z0-9._-]{0,62}")


def binding(value):
    keys(value, BINDING, "binding")
    require(exposure(value["exposure_id"]) and value["exposure_id"].split("/")[0] == value["access_id"], "binding exposure")
    require(integer(value["epoch"], 1) and integer(value["generation"], 1), "binding generation/epoch")
    require(text(value["revision"], r"rv_[a-z2-7]{26}"), "binding revision")
    ob = value["observation_revision"]
    require(text(ob, r"ob_[1-9][0-9]{0,19}") and int(ob[3:]) <= 2**64-1, "binding observation revision")
    require(text(value["contract_digest"], r"[0-9a-f]{64}") and value["source_id"] == "local" and type(value["contract_version"]) is int and value["contract_version"] == 1, "binding contract")
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def decode_log(text_value):
    """Strict JSON lines; retain truncated/duplicate-key failures as issues."""
    rows, problems = [], []
    def unique(pairs):
        out = {}
        for key, value in pairs:
            require(key not in out, "duplicate JSON key")
            out[key] = value
        return out
    for number, line in enumerate(text_value.splitlines(), 1):
        try:
            row = json.loads(line, object_pairs_hook=unique, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("nonfinite JSON")))
            require(type(row) is dict, "log object")
            rows.append(row)
        except (ValueError, TypeError) as exc:
            problems.append({"line": number, "error": str(exc)})
    return rows, problems


def _context(meta, ctx):
    keys(ctx, CONTEXT, "context")
    sha = ctx["source_sha"]
    require(text(sha, r"[0-9a-f]{40}") and sha == ctx["metadata_source_sha"] == ctx["log_source_sha"], "source binding")
    keys(meta, META, "metadata")
    require(meta["schema"] == META_SCHEMA, "metadata schema")
    require(type(ctx["test"]) is str and ctx["test"] and ctx["backend"] in ("sqlite", "postgres") and integer(ctx["round"], 0, 2), "test identity")
    require(integer(ctx["ois_pid"], 1) and integer(ctx["epoch"], 1) and text(ctx["run_id"], r"run_[a-z2-7]{26}"), "process identity")
    for field in ("test", "backend", "round", "ois_pid"):
        require(type(meta[field]) is type(ctx[field]) and meta[field] == ctx[field], "metadata " + field)
    require(type(ctx["bindings"]) is list and ctx["bindings"], "original binding inventory missing")
    allowed = {binding(b) for b in ctx["bindings"]}
    require(type(meta["requests"]) is list and len(meta["requests"]) <= 512, "request capacity")
    require(type(meta["overflow"]) is bool and integer(meta["omitted"], 0) and meta["overflow"] == (meta["omitted"] > 0), "request overflow")
    require(not meta["overflow"] or len(meta["requests"]) == 512, "overflow before capacity")
    return allowed


def _request(req, previous):
    keys(req, REQUEST, "request")
    require(integer(req["attempt"], previous + 1) and req["phase"] in ("preload_probe", "pre_dial_full", "slow_full"), "request attempt/phase")
    sid, error, call = req["sample_id"], req["id_error"], req["ois_call"]
    require(sid is None or text(sid, r"[0-9a-f]{32}"), "sample ID")
    require(error in (None, "entropy_error") and not (sid is not None and error is not None), "ID error")
    if call is None:
        require(sid is None and error is None and req["ois_stats"] is None, "uncalled request has result")
        return False
    keys(call, CALL, "OIS call")
    require(integer(call["started_ns"], 0), "OIS start")
    require(call["context_deadline_ns"] is None or integer(call["context_deadline_ns"]), "OIS context deadline")
    require(sid is not None or error == "entropy_error", "called request missing ID outcome")
    if call["returned_ns"] is None:
        require(call["error_class"] is None and call["context_state"] is None and req["ois_stats"] is None, "interrupted call fabricated return")
        return False
    require(integer(call["returned_ns"], call["started_ns"]) and call["error_class"] in ERRORS and call["context_state"] in ("active", "canceled", "deadline"), "OIS return")
    stats = req["ois_stats"]
    require(stats is None or type(stats) is dict, "HTTP stats object")
    if stats is not None:
        require(call["error_class"] == "none", "failed request fabricated complete stats")
    return stats is not None and call["error_class"] == "none" and call["context_state"] == "active" and (call["context_deadline_ns"] is None or call["returned_ns"] <= call["context_deadline_ns"])


def _payload(row, ctx, allowed):
    keys(row, PAYLOAD, "pending payload")
    require(len(json.dumps(row, ensure_ascii=False, separators=(",", ":")).encode()) <= 65536, "payload bound")
    require(row["schema"] == SCHEMA and text(row["sample_id"], r"[0-9a-f]{32}"), "pending schema/ID")
    require(type(row["complete"]) is bool and type(row["reasons"]) is list and all(type(r) is str and r in REASONS for r in row["reasons"]) and len(row["reasons"]) == len(set(row["reasons"])), "completeness reasons")
    complete = row["complete"]
    require(complete == (not row["reasons"]), "complete/reason mismatch")
    require(integer(row["pid"], 1) and row["pid"] == ctx["ois_pid"] and integer(row["epoch"], 1) and row["epoch"] == ctx["epoch"], "log PID/epoch")
    require(row["run_id"] == ctx["run_id"] or (not complete and row["run_id"] is None), "log run ID")
    for field in (*COUNTS, "query_slots", "query_ms", "refresh_ms"):
        require(integer(row[field], 0 if field in COUNTS else (1 if complete else 0)), "counter/config " + field)
    keys(row["omitted"], {"pending", "sources", "query_order"}, "omitted")
    for name, limit in (("pending", 8), ("sources", 16), ("query_order", 16)):
        require(type(row[name]) is list and len(row[name]) <= limit and integer(row["omitted"][name], 0), "array cap " + name)
    require(len(row["pending"]) + row["omitted"]["pending"] == row["pending_queries"] and len(row["sources"]) + row["omitted"]["sources"] == row["observation_capacity"], "omitted/count mismatch")
    require(not complete or not any(row["omitted"].values()), "complete omission")
    order = row["query_order"]
    require(all(exposure(x) or (x is None and not complete) for x in order), "query order identity")
    require(integer(row["query_cursor"]) and (not complete or 0 <= row["query_cursor"] < max(1, len(order))), "query cursor")
    sources, pending = {}, {}
    for item in row["sources"]:
        keys(item, SOURCE, "source")
        b = item["binding"]
        require(b is not None or not complete, "complete source binding null")
        if b is not None:
            require(binding(b) in allowed, "source binding not in original inventory")
            require(b["exposure_id"] not in sources, "duplicate source")
            sources[b["exposure_id"]] = item
        require(item["query_id"] is None or text(item["query_id"], r"[0-9a-f]{32}"), "source query ID")
        require(all(type(item[k]) is bool for k in ("dirty", "periodic_only", "refresh_at_set")), "source flags")
        require(not item["periodic_only"] or item["dirty"], "periodic without dirty")
        require(item["online"] is None or type(item["online"]) is bool, "source online")
        require(item["control_version"] in (None, "oi.control.v1", "oi.control.v2"), "control version")
        require(item["current_generation"] is None or integer(item["current_generation"], 0), "source current generation")
        require(integer(item["refresh_remaining_ns"]) if item["refresh_at_set"] else item["refresh_remaining_ns"] is None, "refresh time")
    for item in row["pending"]:
        keys(item, PENDING, "pending")
        qid, b = item["query_id"], item["binding"]
        require(text(qid, r"[0-9a-f]{32}") or (qid is None and not complete), "query ID")
        require(b is not None or not complete, "complete query binding null")
        if qid is not None:
            require(qid not in pending, "duplicate pending owner")
            pending[qid] = item
        if b is not None:
            require(binding(b) in allowed, "pending binding not in original inventory")
        require(type(item["binding_current"]) is bool and type(item["session_current"]) is bool and (item["online"] is None or type(item["online"]) is bool), "pending relationship flags")
        for field in ("age_ns", "remaining_ns"):
            require(integer(item[field]) or (item[field] is None and not complete), "pending time")
        if complete:
            require(item["age_ns"] >= 0 and item["age_ns"] + item["remaining_ns"] == row["query_ms"] * 1000000, "original query deadline")
            current = sources.get(b["exposure_id"])
            require(item["binding_current"] == (current is not None and current["binding"] == b), "binding_current mismatch")
    if complete:
        require(row["dirty_marked"] == sum(s["dirty"] for s in sources.values()), "dirty count")
        require(row["observation_backlog"] == sum(s["dirty"] or s["query_id"] is not None for s in sources.values()), "backlog union")
    return complete


def analyze(metadata, log_records, *, context, decode_problems=()):
    """Return diagnostics, never a business PASS. Missing/late facts stay unknown.

    `context.bindings` is the full original registered/wire binding inventory,
    including legitimate older bindings; it must not be derived from this log.
    Time intervals use only that request's actual OISCall, never a slog time.
    """
    out = {"schema": "g2-pending-diagnosis-v1", "business_verdict": "not_evaluated", "status": "incomplete", "problems": list(decode_problems), "requests": [], "orphan_sample_ids": []}
    try:
        allowed = _context(metadata, context)
        require(type(log_records) is list, "log records")
        indexed = {}
        for row in log_records:
            require(type(row) is dict, "log record object")
            if row.get("msg") != MESSAGE:
                continue
            payload = row.get("pending_identity")
            require(type(payload) is dict and text(payload.get("sample_id"), r"[0-9a-f]{32}"), "diagnostic log missing ID/object")
            indexed.setdefault(payload["sample_id"], []).append(payload)
        seen, previous = set(), 0
        for req in metadata["requests"]:
            window = _request(req, previous)
            previous = req["attempt"]
            sid = req["sample_id"]
            result = {"attempt": req["attempt"], "sample_id": sid, "status": "missing", "id_error": req["id_error"], "ois_call": req["ois_call"], "snapshot": None, "request_window_ns": None, "pending": [], "sources": []}
            out["requests"].append(result)
            if sid is None:
                continue
            require(sid not in seen, "duplicate request sample ID")
            seen.add(sid)
            matches = indexed.get(sid, [])
            if not matches:
                continue
            require(len(matches) == 1, "duplicate log sample ID")
            row = matches[0]
            complete = _payload(row, context, allowed)
            result["snapshot"] = {k: v for k, v in row.items() if k not in ("pending", "sources")}
            stats = req["ois_stats"]
            if stats is not None:
                require(integer(stats.get("epoch"), 1) and stats["epoch"] == row["epoch"], "HTTP epoch")
                obs = stats.get("observation")
                require(type(obs) is dict, "complete HTTP observation missing")
                for name in COUNTS:
                    require(integer(obs.get(name), 0) and obs[name] == row[name], "same-call HTTP count " + name)
            result["status"] = "complete" if complete and window else "partial"
            if window and complete:
                start, end = req["ois_call"]["started_ns"], req["ois_call"]["returned_ns"]
                result["request_window_ns"] = [start, end]
                for q in row["pending"]:
                    result["pending"].append({**q, "started_interval_ns": [start-q["age_ns"], end-q["age_ns"]], "deadline_interval_ns": [start+q["remaining_ns"], end+q["remaining_ns"]]})
                for s in row["sources"]:
                    r = s["refresh_remaining_ns"]
                    result["sources"].append({**s, "refresh_interval_ns": None if r is None else [start+r, end+r]})
            else:
                result["pending"], result["sources"] = row["pending"], row["sources"]
        out["orphan_sample_ids"] = sorted(set(indexed) - seen)
        require(not out["orphan_sample_ids"], "unassociated diagnostic log")
        if not out["problems"] and not metadata["overflow"] and out["requests"] and all(x["status"] == "complete" for x in out["requests"]):
            out["status"] = "complete"
    except (ValueError, TypeError, KeyError) as exc:
        out["problems"].append(str(exc))
        out["status"] = "conflict"
    return out
