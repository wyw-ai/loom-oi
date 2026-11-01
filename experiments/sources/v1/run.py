"""Run only this repository's reviewed fixtures, never arbitrary code or remote targets."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import subprocess
import sys
import tempfile
import uuid
from analyze import analyze

ROOT=Path(__file__).resolve().parent
def digest(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def dump(path,data):path.write_text(json.dumps(data,ensure_ascii=False,indent=2,sort_keys=True)+"\n")
def probe(command):
    try:
        p=subprocess.run(command,capture_output=True,text=True,timeout=3,env={"PATH":os.defpath,"LANG":"C.UTF-8"})
        return {"returncode":p.returncode,"stderr":p.stderr.strip()[:500]}
    except (OSError,subprocess.TimeoutExpired) as exc:
        return {"unavailable":type(exc).__name__}
def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("manifest",type=Path)
    parser.add_argument("--output-root",type=Path,default=ROOT/"runs")
    args=parser.parse_args()
    manifest=json.loads(args.manifest.read_text())
    assert manifest["case"] in ["probe","retry"]
    assert 2 <= manifest["repeats"] <= 20 and isinstance(manifest["seed"],int)
    assert manifest["isolation"]=="trusted_process_loopback"
    for name in ["hypothesis","alternatives","disproof","limitations","issues"]:
        assert manifest.get(name),name
    now=datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    run=args.output_root.resolve()/(manifest["id"]+"-"+now+"-"+uuid.uuid4().hex[:6])
    run.mkdir(parents=True,exist_ok=False)
    dump(run/"manifest.json",manifest) # Stored before executing observations.
    source_files=[ROOT/"run.py",ROOT/"analyze.py",ROOT/"cases/protocol_fixture.py"]
    dump(run/"source-manifest.json",{str(p.relative_to(ROOT.parent)):digest(p) for p in source_files})
    def limit(name):
        p=Path("/sys/fs/cgroup")/name
        return p.read_text().strip() if p.exists() else "unavailable"
    env={"os":platform.system(),"kernel":platform.release(),"machine":platform.machine(),
         "python":platform.python_version(),"cpu_count":os.cpu_count(),
         "cgroup_cpu_max":limit("cpu.max"),"cgroup_memory_max":limit("memory.max"),
         "go":shutil.which("go"),"rustc":shutil.which("rustc"),"cargo":shutil.which("cargo"),
         "namespace_probe":probe(["unshare","--user","--map-root-user","--net","true"]),
         "isolation":{"mode":"trusted_process_loopback","network":"hardcoded 127.0.0.1; no network namespace enforcement",
                      "filesystem":"temporary cwd; host filesystem remains accessible",
                      "environment":"allowlist only; no inherited user credentials",
                      "limits":{"cpu_seconds":10,"address_space_bytes":536870912,"open_files":128,
                                "file_bytes":8388608,"wall_seconds":30},
                      "not_suitable_for":"untrusted plugins, production services, hostile code"},
         "source_base":subprocess.check_output(["git","rev-parse","HEAD"],cwd=ROOT,text=True).strip(),
         "source_worktree":"source-manifest hashes are authoritative for this uncommitted experiment version"}
    dump(run/"environment.json",env)
    timed_out=False
    with tempfile.TemporaryDirectory(prefix="oi-evidence-") as work:
        # Files are bounded by the child's RLIMIT_FSIZE. No shell or user command.
        with (run/"raw.jsonl").open("w") as stdout,(run/"stderr.log").open("w") as stderr:
            process=subprocess.Popen([sys.executable,"-I",str(ROOT/"cases/protocol_fixture.py"),
                                      str(run/"manifest.json")],cwd=work,
                env={"PATH":os.defpath,"LANG":"C.UTF-8","PYTHONHASHSEED":"0"},
                stdout=stdout,stderr=stderr,start_new_session=True)
            try: code=process.wait(timeout=30)
            except subprocess.TimeoutExpired:
                timed_out=True
                os.killpg(process.pid,signal.SIGKILL)
                code=process.wait()
            finally:
                # Kill any descendants remaining in this run's process group.
                try:os.killpg(process.pid,signal.SIGKILL)
                except ProcessLookupError:pass
    dump(run/"execution.json",{"exit_code":code,"timed_out":timed_out,
         "command":["python","-I","experiments/cases/protocol_fixture.py","manifest.json"],
         "temp_workdir_removed":True})
    rows=[]
    try:
        rows=[json.loads(x) for x in (run/"raw.jsonl").read_text().splitlines() if x]
        summary=analyze(manifest["case"],rows,manifest["repeats"])
    except (ValueError,KeyError,TypeError) as exc:
        summary={"status":"inconclusive","problems":[type(exc).__name__+": "+str(exc)]}
    if code!=0 or timed_out:
        summary["status"]="inconclusive"
        summary.setdefault("problems",[]).append("worker execution failed")
    dump(run/"summary.json",summary)
    (run/"report.md").write_text("# "+manifest["id"]+" run record\n\n"
        "Status: "+summary["status"]+". Evidence: mechanism fixture, not OI implementation.\n\n"
        "See manifest.json for preregistered question, raw.jsonl for observations, summary.json for recomputed results, "
        "environment.json for isolation and machine constraints. Numeric timings are local fixture observations, not product SLOs.\n")
    (run/"SHA256SUMS").write_text("".join(digest(p)+"  "+p.name+"\n"
        for p in sorted(run.iterdir()) if p.is_file() and p.name!="SHA256SUMS"))
    print(json.dumps({"run":str(run),"summary":summary},ensure_ascii=False))
    return 0 if summary["status"]=="observed" else 1
if __name__=="__main__":sys.exit(main())
