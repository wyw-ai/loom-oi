"""Run preregistered product evidence (Implementation level) at HEAD.

Only the fixed steps below run: this repository's Go tests, fuzz targets and
pinned linters; the manifest selects among them and supplies no commands.
The working tree must be clean so that subject_version names the exact code
under test. Linux with cgo (race detector) and PostgreSQL via OI_TEST_PG_DSN
are expected; a missing or timed-out step makes the run inconclusive.
"""
import argparse
import datetime
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import signal
import subprocess
import sys
import tempfile
import threading
import time
import types
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
# Loaded by path: the lab analyzer already owns the module name "analyze".
_spec = importlib.util.spec_from_file_location('oi_product_analyze', HERE / 'analyze.py')
_analyzer = importlib.util.module_from_spec(_spec)
_analyzer_source = HERE / 'analyze.py'
exec(compile(_analyzer_source.read_bytes(), str(_analyzer_source), 'exec'), _analyzer.__dict__)
analyze = _analyzer.analyze

STATICCHECK = 'honnef.co/go/tools/cmd/staticcheck@v0.8.2'
GOVULNCHECK = 'golang.org/x/vuln/cmd/govulncheck@v1.9.0'
# The manifest being run is added to these source paths.
SOURCES = ['cmd', 'internal', 'tests', 'go.mod', 'go.sum', 'experiments/product', '.github/workflows']
CLOCK_ENVELOPES = {'clock_preparation', 'clock_guard', 'clock_result'}
GO_ACTIONS = {'start', 'run', 'pause', 'cont', 'pass', 'bench', 'fail', 'output', 'skip'}


def sibling(name):
    path = HERE / (name+'.py')
    module = types.ModuleType('runner_'+name)
    module.__file__ = str(path)
    exec(compile(path.read_bytes(), str(path), 'exec'), module.__dict__)
    return module


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def dump(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2, sort_keys=True) + '\n', encoding='utf-8', newline='\n')


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def workspace_dirty(manifest_path):
    if git('status', '--porcelain', '--untracked-files=no'):
        return True
    # Include ignored files too: an ignored Go/embedded input can still change
    # what is built. Importing this runner only generates Python bytecode.
    untracked = git('ls-files', '--others', '--', *SOURCES, manifest_path).splitlines()
    return any(not (Path(p).suffix == '.pyc' and '__pycache__' in Path(p).parts) for p in untracked)


def step_table(work, case):
    """name -> (argv, extra environment, timeout seconds, emits go test -json)."""
    fuzz = case['fuzz_time']
    _analyzer._local_case(case)
    if ('clock' in case.get('steps', [])) != ('g2_clock' in case):
        raise ValueError('clock step requires its explicit G2 clock preregistration')
    # Preregistered execution allowance, never a product deadline.
    vectors_minutes = 90 if 'g2_behavior' in case else 40
    if 'vectors_timeout_minutes' in case:
        value = case['vectors_timeout_minutes']
        if type(value) is not int or value != 90:
            raise ValueError('vectors_timeout_minutes must be the explicit integer 90')
        vectors_minutes = value
    it = {'OI_IT': '1', 'OI_IT_CLOCK': '0', 'OI_IT_BACKENDS': ','.join(case['backends']),
          'OI_IT_RUNDIR': str(work / 'it')}
    if case.get('load', True):
        it['OI_IT_LOAD'] = '1'  # the O3 load profile (TV-B4-06)
    return {
        'unit': (['go', 'test', '-json', '-count=1', '-covermode=atomic', '-coverprofile=' + str(work / 'unit.out'),
                  './cmd/...', './internal/...'], {}, 1800, True),
        'race': (['go', 'test', '-json', '-race', '-count=1', './cmd/...', './internal/...'], {'CGO_ENABLED': '1'},
                 2400, True),
        'vectors': (['go', 'test', '-json', '-count=1', '-timeout',
                     str(vectors_minutes)+'m', './tests/integration'],
                    it, vectors_minutes*60+300, True),
        **({'clock': ([sys.executable, str(HERE / 'clock_run.py'), '--work', str(work / 'clock')],
                    {'OI_IT': '1', 'OI_IT_CLOCK': '1', 'OI_IT_BACKENDS': 'sqlite,postgres'}, 3600, True)}
           if 'g2_clock' in case else {}),
        **({'fixtures': (['go', 'test', '-json', '-count=1', './tests/fixture'], {}, 900, True)}
           if 'g2_behavior' in case else {}),
        'fuzz-frame': (['go', 'test', '-json', '-run', '^$', '-fuzz', '^' + case['fuzz']['fuzz-frame'] + '$',
                        '-fuzztime', fuzz, './internal/protocol'], {}, 900, True),
        'fuzz-control': (['go', 'test', '-json', '-run', '^$', '-fuzz', '^' + case['fuzz']['fuzz-control'] + '$',
                          '-fuzztime', fuzz, './internal/protocol'], {}, 900, True),
        'fuzz-state': (['go', 'test', '-json', '-run', '^$', '-fuzz',
                        '^' + case['fuzz'].get('fuzz-state', 'FuzzObservationState') + '$',
                        '-fuzztime', fuzz, './internal/observation'], {}, 900, True),
        'vet': (['go', 'vet', './...'], {}, 900, False),
        'staticcheck': (['go', 'run', STATICCHECK, './...'], {}, 1200, False),
        'govulncheck': (['go', 'run', GOVULNCHECK, './...'], {}, 1200, False),
    }


def supervise_local_clock(work, context, events, code, deadline, raw):
    local,case,attestation=context
    prepare=[e for kind,e in events if kind=='clock_preparation']
    if len(prepare)!=1 or prepare[0].get('schema')!='g2-clock-preparation-v2':
        raise ValueError('local supervision requires one actual preparation-v2')
    prepared=prepare[0]
    if prepared.get('attestation')!=attestation:
        raise ValueError('local clock changed the runner attestation')
    files={name:digest(work/name) for name in _analyzer.CLOCK_FILES}
    request=_analyzer._local_supervision_request(dict(prepared,schema='g2-clock-guard-v2'),code,files)
    request_path=work/'supervision-request.json'
    if request_path.exists():
        raise ValueError('refusing to overwrite local supervision request')
    temporary=request_path.with_name('.supervision-request.tmp')
    with temporary.open('xb') as output:
        output.write(local.canonical(request));output.flush();os.fsync(output.fileno())
    os.replace(temporary,request_path)
    receipt_path=Path('/var/lib/loom-oi/receipts')/(attestation['run_id']+'.json')
    end=min(deadline,time.monotonic()+120)
    while time.monotonic()<end:
        try:
            receipt=local.read_root_json(receipt_path)
        except FileNotFoundError:
            time.sleep(min(.1,max(0,end-time.monotonic())))
            continue
        (work/'supervisor-receipt.json').write_bytes(local.canonical(receipt))
        raw.write(json.dumps({'step':'clock','kind':'local_supervisor','event':receipt})+'\n')
        _analyzer._local_receipt(receipt,request)
        if time.monotonic()>end:
            raise ValueError('independent local supervisor receipt exceeded original clock deadline')
        return
    raise ValueError('independent local supervisor receipt absent within original clock deadline')


def run_step(name, argv, extra, timeout, is_json, raw, stderr_log, local_context=None, process_record=None):
    env = dict(os.environ)
    env.update(extra)
    start_record = {'step': name, 'kind': 'start', 'command': argv,
                          'env': {k: v for k, v in sorted(extra.items())},
                          'pg_dsn_set': bool(os.environ.get('OI_TEST_PG_DSN'))}
    if process_record is None:
        raw.write(json.dumps(start_record) + '\n')
    stderr_log.write('== ' + name + ' ==\n')
    stderr_log.flush()
    started = time.monotonic()
    kwargs = {'start_new_session': True} if os.name == 'posix' else {}
    proc = subprocess.Popen(argv, cwd=ROOT, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT if not is_json else stderr_log,
                            text=True, encoding='utf-8', errors='replace', **kwargs)
    if process_record is not None:
        try:
            # Never poll/wait before reading a short-lived child's /proc entry.
            process_record['identity'] = sibling('local_run_seal').process_identity(proc.pid)
            start_record['process'] = process_record['identity']
            raw.write(json.dumps(start_record) + '\n')
            raw.flush()
        except Exception:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait()
            raise
    timed_out = threading.Event()

    def kill():
        timed_out.set()
        try:
            if os.name == 'posix':
                os.killpg(proc.pid, signal.SIGKILL)
            else:
                proc.kill()
        except OSError:
            pass

    timer = threading.Timer(max(0, timeout-(time.monotonic()-started)), kill)
    timer.start()
    clock_framing_failed = False
    clock_events=[]
    try:
        for line in proc.stdout:
            terminated = line.endswith('\n')
            line = line.rstrip('\n')
            if is_json and line.startswith('{') and (name != 'clock' or terminated):
                try:
                    event = json.loads(line)
                    if name == 'clock':
                        if (isinstance(event, dict) and set(event) == {'kind', 'event'}
                                and event['kind'] in CLOCK_ENVELOPES and isinstance(event['event'], dict)):
                            raw.write(json.dumps({'step': name, 'kind': event['kind'],
                                                  'event': event['event']}) + '\n')
                            clock_events.append((event['kind'],event['event']))
                            continue
                        if (not isinstance(event, dict) or 'kind' in event or 'event' in event
                                or event.get('Action') not in GO_ACTIONS):
                            raise ValueError('invalid clock stdout envelope')
                    raw.write(json.dumps({'step': name, 'kind': 'go', 'event': event}) + '\n')
                    continue
                except (ValueError, TypeError):
                    pass
            raw.write(json.dumps({'step': name, 'kind': 'line', 'text': line}) + '\n')
            if name == 'clock':
                clock_framing_failed = True
        code = proc.wait()
        if process_record is not None:
            process_record['waited_ns'] = time.monotonic_ns()
    finally:
        timer.cancel()
        proc.stdout.close()
    if name=='clock' and local_context is not None:
        work=Path(argv[argv.index('--work')+1])
        supervise_local_clock(work,local_context,clock_events,code,started+timeout,raw)
    if clock_framing_failed:
        raise ValueError('invalid clock stdout framing; original lines retained; subprocess exit_code='+str(code))
    return code, timed_out.is_set(), int((time.monotonic() - started) * 1000)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('manifest', type=Path)
    parser.add_argument('--output-root', type=Path, default=ROOT / 'experiments/runs')
    parser.add_argument('--work', type=Path, help='keep integration process logs here (default: temporary)')
    parser.add_argument('--allow-dirty', action='store_true',
                        help='development only: the run can never match a gate')
    args = parser.parse_args()
    prereg = args.manifest.resolve()
    manifest = json.loads(prereg.read_text(encoding='utf-8'))
    case = manifest['case']
    local=_analyzer._local_case(case)
    if local and args.work is None:
        print('refusing local execution: --work must preserve independent recovery originals',file=sys.stderr)
        return 2
    assert manifest['evidence_level'] == 'Implementation' and case['kind'] in _analyzer.KINDS
    for name in ['hypothesis', 'alternatives', 'disproof', 'limitations', 'issues']:
        assert manifest.get(name), name
    table_names = set(step_table(Path('.'), case))
    assert set(case['steps']) <= table_names, 'unknown step'
    head = git('rev-parse', 'HEAD')
    try:
        rel = prereg.relative_to(ROOT).as_posix()
    except ValueError:
        print('refusing to run: the manifest must be preregistered in this repository', file=sys.stderr)
        return 2
    dirty = workspace_dirty(rel)
    committed_in = git('log', '-1', '--format=%H', '--', rel)
    if not committed_in:
        dirty = True
    if dirty and not args.allow_dirty:
        print('refusing to run: tracked files differ from HEAD, source files are untracked or the manifest is not committed', file=sys.stderr)
        return 2

    now = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    run = args.output_root.resolve() / (manifest['id'] + '-' + now + '-' + uuid.uuid4().hex[:6])
    run.mkdir(parents=True, exist_ok=False)
    stored = dict(manifest)
    stored['subject_version'] = ('dirty:' if dirty else '') + head
    stored['preregistration'] = {'path': rel, 'sha256': digest(prereg), 'committed_in': committed_in}
    dump(run / 'manifest.json', stored)  # stored before any observation
    files = git('ls-files', '--', *SOURCES, rel).splitlines()
    dump(run / 'source-manifest.json', {f: digest(ROOT / f) for f in files})
    db = sibling('vulndb_snapshot')
    db_context = db.prepare(case, os.environ, run)

    def read(path):
        p = Path(path)
        return p.read_text(encoding='utf-8', errors='replace') if p.exists() else ''

    def first(text, key):
        for line in text.splitlines():
            if line.startswith(key):
                return line.split(':', 1)[1].strip()
        return 'unavailable'

    go_version = subprocess.run(['go', 'version'], cwd=ROOT, capture_output=True, text=True).stdout.strip()
    go_env = json.loads(subprocess.run(['go', 'env', '-json', 'GOOS', 'GOARCH', 'GOAMD64', 'CGO_ENABLED', 'GOTOOLCHAIN',
                                        'GOFLAGS', 'CC', 'GOPROXY', 'GOSUMDB', 'GOMODCACHE'], cwd=ROOT, capture_output=True, text=True).stdout)
    ci_keys = {
        'GITHUB_REPOSITORY', 'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_WORKFLOW', 'GITHUB_REF', 'GITHUB_SHA',
        'GITHUB_SERVER_URL', 'RUNNER_OS', 'RUNNER_ARCH', 'RUNNER_NAME', 'ImageOS', 'ImageVersion'}
    if 'g2_behavior' in case or 'g2_clock' in case:
        ci_keys.update({'GITHUB_WORKFLOW_REF', 'GITHUB_JOB', 'RUNNER_ENVIRONMENT'})
    ci = {k: os.environ[k] for k in sorted(os.environ) if k in ci_keys}
    local_fact=None
    runner_record=None
    if local:
        attestation=local.environment(os.environ,clock_scope=False)
        local_fact=dict(attestation=attestation,attestation_sha256=local.digest(attestation))
        _analyzer._local_fact(case,{'local_execution':local_fact},stored['subject_version'])
        runner_record = dict(identity=sibling('local_run_seal').process_identity(), started_ns=time.monotonic_ns())
    elif any(k.startswith('OI_LOCAL_') or k=='OI_EXECUTION_PROFILE' for k in os.environ):
        raise ValueError('local environment requires an explicit local manifest')
    dump(run / 'environment.json', {
        'os': platform.system(), 'kernel': platform.release(), 'machine': platform.machine(),
        'python': platform.python_version(), 'cpu_count': os.cpu_count(),
        'cpu_model': first(read('/proc/cpuinfo'), 'model name'), 'mem_total': first(read('/proc/meminfo'), 'MemTotal'),
        'go_version': go_version, 'go_env': go_env,
        'postgres': {'image': os.environ.get('OI_PG_IMAGE', 'unrecorded'), 'dsn_set': bool(os.environ.get('OI_TEST_PG_DSN'))},
        'fixtures': {'small_fs': bool(os.environ.get('OI_IT_SMALLFS')),
                     'pg_tools': 'container' if os.environ.get('OI_IT_PG_CONTAINER') else
                     ('bin' if os.environ.get('OI_IT_PG_BIN') else 'none')},
        'ci': ci, 'subject_version': stored['subject_version'],
        **({'local_execution':local_fact} if local else {}),
        **({'local_runner':runner_record} if local else {}),
        **({'vulndb_snapshot':db.fact(db_context)} if db_context else {}),
        'isolation': {'mode': manifest.get('isolation', 'unrecorded'),
                      'network': 'loopback topology of B4 §4; PostgreSQL service on 127.0.0.1',
                      'not_suitable_for': 'untrusted code; production services'}})

    work_ctx = tempfile.TemporaryDirectory(prefix='oi-' + manifest['id'] + '-') if args.work is None else None
    work = Path(work_ctx.name) if work_ctx else args.work.resolve()
    work.mkdir(parents=True, exist_ok=True)
    if local:
        sibling('local_run_seal').atomic_new(work/'collector.json', local.canonical(dict(
            schema='loom-oi-local-collector-v1', run_id=attestation['run_id'], source_sha=head,
            boot_id=attestation['boot_id'], evidence_run_id=run.name, runner=runner_record)))
    table = step_table(work, case)
    steps, timed_out, failure = [], False, None
    try:
        with (run / 'raw.jsonl').open('w', encoding='utf-8', newline='\n') as raw, \
                (run / 'stderr.log').open('w', encoding='utf-8', newline='\n') as err:
            raw.write(json.dumps({'step': 'env', 'kind': 'fact', 'name': 'go_version', 'value': go_version}) + '\n')
            raw.write(json.dumps({'step': 'env', 'kind': 'fact', 'name': 'goos', 'value': go_env.get('GOOS')}) + '\n')
            raw.write(json.dumps({'step': 'env', 'kind': 'fact', 'name': 'goarch', 'value': go_env.get('GOARCH')}) + '\n')
            if 'g2_behavior' in case or 'g2_clock' in case or local:
                raw.write(json.dumps({'step': 'env', 'kind': 'fact', 'name': 'subject_version',
                                      'value': stored['subject_version']}) + '\n')
            if local:
                raw.write(json.dumps({'step':'env','kind':'fact','name':'local_execution','value':local_fact})+'\n')
                raw.write(json.dumps({'step':'env','kind':'fact','name':'local_runner','value':runner_record})+'\n')
            if db_context:
                raw.write(json.dumps({'step':'env','kind':'fact','name':'vulndb_snapshot','value':db.fact(db_context)})+'\n')
            for name in case['steps']:
                argv, extra, timeout, is_json = table[name]
                if name == 'govulncheck' and db_context:
                    before = db.verify_directory(db_context)
                    argv = db.command(db_context)
                    raw.write(json.dumps({'step':name,'kind':'fact','name':'vulndb_scan_before','value':before})+'\n')
                kwargs={'local_context':(local,case,attestation)} if local and name=='clock' else {}
                process_record = {} if local else None
                if local:
                    kwargs['process_record'] = process_record
                code, out, ms = run_step(name, argv, extra, timeout, is_json, raw, err,**kwargs)
                if name == 'unit' and (work / 'unit.out').exists():
                    cover = subprocess.run(['go', 'tool', 'cover', '-func=' + str(work / 'unit.out')], cwd=ROOT,
                                           capture_output=True, text=True)
                    lines = cover.stdout.splitlines()
                    raw.write(json.dumps({'step': 'unit', 'kind': 'coverage', 'lines': lines}) + '\n')
                    (run / 'coverage-func.txt').write_text(cover.stdout, encoding='utf-8', newline='\n')
                raw.write(json.dumps({'step': name, 'kind': 'end', 'exit_code': code, 'timed_out': out,
                                      'duration_ms': ms, **({'process':process_record} if local else {})}) + '\n')
                if name == 'govulncheck' and db_context:
                    after = db.verify_directory(db_context)
                    raw.write(json.dumps({'step':name,'kind':'fact','name':'vulndb_scan_after','value':after})+'\n')
                raw.flush()
                steps.append({'step': name, 'command': argv, 'exit_code': code, 'timed_out': out, 'duration_ms': ms})
                timed_out = timed_out or out
    except (OSError, ValueError) as exc:
        failure = type(exc).__name__ + ': ' + str(exc)
    finally:
        if work_ctx:
            work_ctx.cleanup()
    dump(run / 'execution.json', {'exit_code': 0 if failure is None else 1, 'timed_out': timed_out,
                                  'runner': 'experiments/product/run.py', 'steps': steps, 'runner_error': failure})
    try:
        rows = [json.loads(x) for x in (run / 'raw.jsonl').read_text(encoding='utf-8').splitlines() if x]
        summary = analyze(case, rows, manifest['repeats'])
    except (ValueError, KeyError, TypeError) as exc:
        summary = {'status': 'inconclusive', 'problems': [type(exc).__name__ + ': ' + str(exc)]}
    if failure is not None or timed_out:
        summary['status'] = 'inconclusive'
        summary.setdefault('problems', []).append('runner or step execution incomplete')
    dump(run / 'summary.json', summary)
    (run / 'report.md').write_text(report(stored, summary, steps), encoding='utf-8', newline='\n')
    if not local:
        (run / 'SHA256SUMS').write_text(''.join(digest(p) + '  ' + p.name + '\n' for p in sorted(run.iterdir())
                                                if p.is_file() and p.name != 'SHA256SUMS'), encoding='utf-8', newline='\n')
    print(json.dumps({'run': str(run), 'status': summary['status'], 'verdict': summary.get('verdict'),
                      'problems': summary.get('problems', [])[:20]}, ensure_ascii=False))
    return 0 if summary['status'] == 'observed' and summary.get('verdict') == 'pass' else 1


def report(manifest, summary, steps):
    out = ['# ' + manifest['id'] + ' run record', '',
           'Gate ' + manifest['gate'] + ', evidence level ' + manifest['evidence_level'] + ', subject `'
           + manifest['subject_version'] + '`.', '',
           'Status: ' + summary['status'] + '; verdict: ' + str(summary.get('verdict')) + '.', '',
           'Every number below is recomputed from raw.jsonl by experiments/product/analyze.py; '
           '`python experiments/verify.py <run>` repeats that check.', '', '## Steps', '',
           '| Step | Exit | Timed out | Duration (ms) |', '| --- | --- | --- | --- |']
    out += ['| %s | %s | %s | %s |' % (s['step'], s['exit_code'], s['timed_out'], s['duration_ms']) for s in steps]
    if summary.get('verdict') is None:
        out += ['', 'Problems: ' + '; '.join(summary.get('problems', []))]
        return '\n'.join(out) + '\n'
    out += ['', 'Unit coverage of the delivery unit: ' + str(summary['coverage_total_pct']) + '%. Engines: '
            + json.dumps(summary['engines'], ensure_ascii=False) + '.', '', '## ' + manifest['gate'] + ' criteria', '',
            '| Criterion | Vectors | Result |', '| --- | --- | --- |']
    for name, ids in manifest['case']['criteria'].items():
        out.append('| %s | %s | %s |' % (name, ', '.join(ids), summary['criteria'][name]))
    out += ['', '## Vectors', '', '| Vector | Result | Checked |', '| --- | --- | --- |']
    for vid, v in summary['vectors'].items():
        out.append('| %s | %s | %s |' % (vid, v['result'], '<br>'.join(v['checked'])))
    out += ['', '## Observations (recorded, no pass lines; B4 §6)', '']
    out += ['- %s: %d records' % (k, len(v)) for k, v in summary['observations'].items()]
    out += ['', '## Problems', '']
    out += ['- ' + p for p in summary['problems']] or ['- none']
    out += ['', '## Limitations', ''] + ['- ' + x for x in manifest['limitations']]
    return '\n'.join(out) + '\n'


if __name__ == '__main__':
    sys.exit(main())
