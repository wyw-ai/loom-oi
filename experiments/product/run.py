"""Run the preregistered P1 product evidence (Implementation level) at HEAD.

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
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
# Loaded by path: the lab analyzer already owns the module name "analyze".
_spec = importlib.util.spec_from_file_location('oi_product_analyze', HERE / 'analyze.py')
_analyzer = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_analyzer)
analyze = _analyzer.analyze

STATICCHECK = 'honnef.co/go/tools/cmd/staticcheck@v0.8.2'
GOVULNCHECK = 'golang.org/x/vuln/cmd/govulncheck@v1.9.0'
SOURCES = ['cmd', 'internal', 'tests', 'go.mod', 'go.sum', 'experiments/product', 'experiments/manifests/G1-P1.json',
           '.github/workflows']


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def dump(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2, sort_keys=True) + '\n', encoding='utf-8', newline='\n')


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def step_table(work, case):
    """name -> (argv, extra environment, timeout seconds, emits go test -json)."""
    fuzz = case['fuzz_time']
    it = {'OI_IT': '1', 'OI_IT_BACKENDS': ','.join(case['backends']), 'OI_IT_LOAD': '1',
          'OI_IT_RUNDIR': str(work / 'it')}
    return {
        'unit': (['go', 'test', '-json', '-count=1', '-covermode=atomic', '-coverprofile=' + str(work / 'unit.out'),
                  './cmd/...', './internal/...'], {}, 1800, True),
        'race': (['go', 'test', '-json', '-race', '-count=1', './cmd/...', './internal/...'], {'CGO_ENABLED': '1'},
                 2400, True),
        'vectors': (['go', 'test', '-json', '-count=1', '-timeout', '40m', './tests/integration'], it, 2700, True),
        'fuzz-frame': (['go', 'test', '-json', '-run', '^$', '-fuzz', '^' + case['fuzz']['fuzz-frame'] + '$',
                        '-fuzztime', fuzz, './internal/protocol'], {}, 900, True),
        'fuzz-control': (['go', 'test', '-json', '-run', '^$', '-fuzz', '^' + case['fuzz']['fuzz-control'] + '$',
                          '-fuzztime', fuzz, './internal/protocol'], {}, 900, True),
        'vet': (['go', 'vet', './...'], {}, 900, False),
        'staticcheck': (['go', 'run', STATICCHECK, './...'], {}, 1200, False),
        'govulncheck': (['go', 'run', GOVULNCHECK, './...'], {}, 1200, False),
    }


def run_step(name, argv, extra, timeout, is_json, raw, stderr_log):
    env = dict(os.environ)
    env.update(extra)
    raw.write(json.dumps({'step': name, 'kind': 'start', 'command': argv,
                          'env': {k: v for k, v in sorted(extra.items())},
                          'pg_dsn_set': bool(os.environ.get('OI_TEST_PG_DSN'))}) + '\n')
    stderr_log.write('== ' + name + ' ==\n')
    stderr_log.flush()
    started = time.monotonic()
    kwargs = {'start_new_session': True} if os.name == 'posix' else {}
    proc = subprocess.Popen(argv, cwd=ROOT, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT if not is_json else stderr_log,
                            text=True, encoding='utf-8', errors='replace', **kwargs)
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

    timer = threading.Timer(timeout, kill)
    timer.start()
    try:
        for line in proc.stdout:
            line = line.rstrip('\n')
            if is_json and line.startswith('{'):
                try:
                    raw.write(json.dumps({'step': name, 'kind': 'go', 'event': json.loads(line)}) + '\n')
                    continue
                except ValueError:
                    pass
            raw.write(json.dumps({'step': name, 'kind': 'line', 'text': line}) + '\n')
        code = proc.wait()
    finally:
        timer.cancel()
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
    assert manifest['evidence_level'] == 'Implementation' and case['kind'] == 'product-p1'
    for name in ['hypothesis', 'alternatives', 'disproof', 'limitations', 'issues']:
        assert manifest.get(name), name
    table_names = set(step_table(Path('.'), case))
    assert set(case['steps']) <= table_names, 'unknown step'
    head = git('rev-parse', 'HEAD')
    dirty = git('status', '--porcelain', '--untracked-files=no') != ''
    try:
        rel = prereg.relative_to(ROOT).as_posix()
    except ValueError:
        print('refusing to run: the manifest must be preregistered in this repository', file=sys.stderr)
        return 2
    committed_in = git('log', '-1', '--format=%H', '--', rel)
    if not committed_in:
        dirty = True
    if dirty and not args.allow_dirty:
        print('refusing to run: tracked files differ from HEAD or the manifest is not committed', file=sys.stderr)
        return 2

    now = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    run = args.output_root.resolve() / (manifest['id'] + '-' + now + '-' + uuid.uuid4().hex[:6])
    run.mkdir(parents=True, exist_ok=False)
    stored = dict(manifest)
    stored['subject_version'] = ('dirty:' if dirty else '') + head
    stored['preregistration'] = {'path': rel, 'sha256': digest(prereg), 'committed_in': committed_in}
    dump(run / 'manifest.json', stored)  # stored before any observation
    files = git('ls-files', '--', *SOURCES).splitlines()
    dump(run / 'source-manifest.json', {f: digest(ROOT / f) for f in files})

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
                                        'GOFLAGS', 'CC'], cwd=ROOT, capture_output=True, text=True).stdout)
    ci = {k: os.environ[k] for k in sorted(os.environ) if k in {
        'GITHUB_REPOSITORY', 'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_WORKFLOW', 'GITHUB_REF', 'GITHUB_SHA',
        'GITHUB_SERVER_URL', 'RUNNER_OS', 'RUNNER_ARCH', 'RUNNER_NAME', 'ImageOS', 'ImageVersion'}}
    dump(run / 'environment.json', {
        'os': platform.system(), 'kernel': platform.release(), 'machine': platform.machine(),
        'python': platform.python_version(), 'cpu_count': os.cpu_count(),
        'cpu_model': first(read('/proc/cpuinfo'), 'model name'), 'mem_total': first(read('/proc/meminfo'), 'MemTotal'),
        'go_version': go_version, 'go_env': go_env,
        'postgres': {'image': os.environ.get('OI_PG_IMAGE', 'unrecorded'), 'dsn_set': bool(os.environ.get('OI_TEST_PG_DSN'))},
        'ci': ci, 'subject_version': stored['subject_version'],
        'isolation': {'mode': manifest.get('isolation', 'unrecorded'),
                      'network': 'loopback topology of B4 §4; PostgreSQL service on 127.0.0.1',
                      'not_suitable_for': 'untrusted code; production services'}})

    work_ctx = tempfile.TemporaryDirectory(prefix='oi-g1-') if args.work is None else None
    work = Path(work_ctx.name) if work_ctx else args.work.resolve()
    work.mkdir(parents=True, exist_ok=True)
    table = step_table(work, case)
    steps, timed_out, failure = [], False, None
    try:
        with (run / 'raw.jsonl').open('w', encoding='utf-8', newline='\n') as raw, \
                (run / 'stderr.log').open('w', encoding='utf-8', newline='\n') as err:
            raw.write(json.dumps({'step': 'env', 'kind': 'fact', 'name': 'go_version', 'value': go_version}) + '\n')
            raw.write(json.dumps({'step': 'env', 'kind': 'fact', 'name': 'goos', 'value': go_env.get('GOOS')}) + '\n')
            raw.write(json.dumps({'step': 'env', 'kind': 'fact', 'name': 'goarch', 'value': go_env.get('GOARCH')}) + '\n')
            for name in case['steps']:
                argv, extra, timeout, is_json = table[name]
                code, out, ms = run_step(name, argv, extra, timeout, is_json, raw, err)
                if name == 'unit' and (work / 'unit.out').exists():
                    cover = subprocess.run(['go', 'tool', 'cover', '-func=' + str(work / 'unit.out')], cwd=ROOT,
                                           capture_output=True, text=True)
                    lines = cover.stdout.splitlines()
                    raw.write(json.dumps({'step': 'unit', 'kind': 'coverage', 'lines': lines}) + '\n')
                    (run / 'coverage-func.txt').write_text(cover.stdout, encoding='utf-8', newline='\n')
                raw.write(json.dumps({'step': name, 'kind': 'end', 'exit_code': code, 'timed_out': out,
                                      'duration_ms': ms}) + '\n')
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
            + json.dumps(summary['engines'], ensure_ascii=False) + '.', '', '## G1 criteria', '',
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
