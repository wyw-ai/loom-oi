"""Fixed real-clock step for the dedicated G2 evidence workflow.

Preparation and Go binaries run as the runner user. Only clock_guard is elevated.
Stdout contains original test2json lines and three explicitly named envelopes;
the product runner must preserve these envelopes as clock_* raw record kinds.
No dry-run or externally supplied evidence can turn this step into a pass.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
SOURCE_PATHS = ('cmd', 'internal', 'tests', 'go.mod', 'go.sum',
                'experiments/product', '.github/workflows', 'third_party/websocket')
PRESERVE = ('OI_IT', 'OI_IT_CLOCK', 'OI_IT_BACKENDS', 'OI_IT_BIN', 'OI_IT_RUNDIR',
            'OI_TEST_PG_DSN', 'OI_PG_IMAGE', 'GOTOOLCHAIN', 'GITHUB_ACTIONS',
            'RUNNER_ENVIRONMENT', 'GITHUB_JOB', 'GITHUB_REPOSITORY',
            'GITHUB_WORKFLOW_REF', 'GITHUB_RUN_ID', 'GITHUB_SHA',
            'OI_EXECUTION_PROFILE', 'OI_LOCAL_ATTESTATION', 'OI_LOCAL_RUN_ID', 'OI_LOCAL_SOURCE_SHA')
GUARD_FILES = ('guard.jsonl', 'state.json', 'finished.json')
GUARD_BUDGET = 3120  # Fixed test timeout is 50 minutes; allow bounded cleanup.


def load_guard():
    spec = importlib.util.spec_from_file_location('oi_clock_guard', HERE/'clock_guard.py')
    module = importlib.util.module_from_spec(spec)
    source = HERE/'clock_guard.py'
    exec(compile(source.read_bytes(), str(source), 'exec'), module.__dict__)
    return module


def require(ok, message):
    if not ok:
        raise ValueError(message)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def dump(path, value):
    path.write_text(json.dumps(value, sort_keys=True, indent=2)+'\n',
                    encoding='utf-8', newline='\n')


def envelope(kind, event, output):
    output.write(json.dumps({'kind': kind, 'event': event}, sort_keys=True)+'\n')
    output.flush()


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def sources():
    require(not git('status', '--porcelain', '--untracked-files=no'), 'tracked source is dirty')
    extras = git('ls-files', '--others', '--', *SOURCE_PATHS).splitlines()
    require(not any(not (Path(p).suffix == '.pyc' and '__pycache__' in Path(p).parts)
                    for p in extras), 'untracked or ignored source inputs exist')
    return {p: sha(ROOT/p) for p in git('ls-files', '--', *SOURCE_PATHS).splitlines()}


def prepare(work, guard):
    local_attestation = guard.execution_environment(os.environ)
    if local_attestation is None:
        require('/.github/workflows/g2-evidence.yml@refs/heads/evidence/G2-' in
                os.environ['GITHUB_WORKFLOW_REF'], 'formal clock wrapper requires the fixed evidence workflow')
    require(os.geteuid() == os.getuid() > 0 and os.getgid() > 0,
            'preparation must run as the ordinary runner user')
    require(os.environ.get('OI_IT') == '1' and os.environ.get('OI_IT_BACKENDS') == 'sqlite,postgres',
            'real clock requires both explicit backends')
    require(bool(os.environ.get('OI_TEST_PG_DSN')), 'PostgreSQL DSN is required')
    subject = local_attestation['source_sha'] if local_attestation else os.environ['GITHUB_SHA']
    require(git('rev-parse', 'HEAD') == subject, 'checkout differs from fixed execution subject')
    before = sources()
    work.mkdir(parents=True, exist_ok=False)
    (work/'bin').mkdir()
    (work/'it').mkdir()
    nonce = secrets.token_hex(32)
    fd = os.open(work/'nonce', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w', encoding='ascii') as stream:
        stream.write(nonce+'\n')
    go = shutil.which('go')
    require(go is not None, 'Go executable missing')
    go = str(Path(go).resolve())
    go_env = json.loads(subprocess.check_output([go, 'env', '-json', 'GOOS', 'GOARCH',
                                               'GOTOOLCHAIN', 'GOFLAGS'], cwd=ROOT, text=True))
    require(go_env['GOOS'] == 'linux', 'clock binary must execute on Linux')
    require(not any(flag in go_env['GOFLAGS'] for flag in ('-overlay', '-modfile')),
            'external build input override is forbidden')
    env = dict(os.environ, GOTOOLCHAIN='local')
    with (work/'prepare.log').open('wb') as log:
        commands = [[go, 'test', '-c', '-o', str(work/'integration.test'), './tests/integration']]
        commands += [[go, 'build', '-trimpath', '-o', str(work/'bin'/name), './cmd/'+name]
                     for name in ('ois', 'oic', 'oi')]
        for index, command in enumerate(commands):
            build_env = dict(env)
            if index:
                build_env['CGO_ENABLED'] = '0'
            subprocess.run(command, cwd=ROOT, env=build_env, stdout=log, stderr=subprocess.STDOUT,
                           check=True, timeout=300)
    require(before == sources(), 'source changed while preparing clock binaries')
    provenance = (dict(guard.local_module().identity(local_attestation, nonce),
                       schema='g2-clock-preparation-v2', attestation=local_attestation,
                       runner_environment=guard.local_module().PROFILE) if local_attestation else
                  dict(schema='g2-clock-preparation-v1', source_sha=subject,
                       run_id=os.environ['GITHUB_RUN_ID'], workflow_ref=os.environ['GITHUB_WORKFLOW_REF'],
                       runner_environment=os.environ['RUNNER_ENVIRONMENT']))
    preparation = dict(provenance, runner_uid=os.getuid(),
                       runner_gid=os.getgid(), nonce_sha256=hashlib.sha256(nonce.encode()).hexdigest(),
                       source_hashes=before, go_version=subprocess.check_output([go, 'version'], text=True).strip(),
                       go_env=go_env, binaries={p.relative_to(work).as_posix(): sha(p)
                       for p in [work/'integration.test', *(work/'bin'/n for n in ('ois', 'oic', 'oi'))]})
    dump(work/'preparation.json', preparation)
    return go, nonce, preparation


def sudo_guard(command, work, *args):
    return ['sudo', '-n', '--preserve-env='+','.join(PRESERVE), 'python3', str(HERE/'clock_guard.py'),
            command, '--uid', str(os.getuid()), '--nonce-file', str(work/'nonce'), *args]


def stop_guard(proc):
    # Its own process group contains sudo and the root guard, never the runner.
    # sudo's forwarding is not assumed; explicitly signal this exact owned group.
    for name, wait in (('TERM', 90), ('KILL', 10)):
        if proc.poll() is not None:
            return False
        subprocess.run(['sudo', '-n', 'kill', '-'+name, '--', '-'+str(proc.pid)],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10, check=False)
        try:
            proc.wait(timeout=wait)
            return name == 'KILL'
        except subprocess.TimeoutExpired:
            pass
    raise RuntimeError('clock guard process group did not exit')


def privileged_bytes(path):
    try:
        return path.read_bytes()
    except PermissionError:
        return subprocess.check_output(['sudo', '-n', 'cat', str(path)], timeout=10)


def close_orphaned_test(guard, nonce):
    """All exit paths may leave the separately grouped test behind."""
    audit = guard.guard_directory(nonce)/'guard.jsonl'
    if not audit.exists():
        return  # No guard started; this invocation has a fresh, unique nonce.
    events = [json.loads(line) for line in privileged_bytes(audit).splitlines()]
    expected = guard.identity(nonce)
    require(events and all(all(event.get(k) == v for k, v in expected.items()) for event in events),
            'cannot identify interrupted guard child')
    children = [e['child_pid'] for e in events if e.get('action') == 'child_started']
    require(len(children) <= 1, 'ambiguous guard child')
    if not children:
        return
    pid = children[0]
    require(type(pid) is int and pid > 1 and pid != os.getpgrp(), 'invalid detached child group')
    if expected.get('schema') == 'g2-clock-guard-v2':
        start = next(e.get('child_start_ticks') for e in events if e.get('action') == 'child_started')
        require(type(start) is int and start > 0, 'missing detached child start identity')
        try:
            actual = guard.process_start_ticks(pid)
        except FileNotFoundError:
            # A leader may have exited while its descendants keep its PGID.
            # Do not signal an unidentifiable group; supervisor must fail/stop VM.
            try:
                os.killpg(pid, 0)
            except ProcessLookupError:
                return
            raise ValueError('orphaned group leader missing; cannot safely identify group')
        require(actual == start and os.getsid(pid) == pid and os.getpgid(pid) == pid,
                'detached child PID/group was reused')
    for sig, budget in ((signal.SIGTERM, 2), (signal.SIGKILL, 1)):
        try:
            # The guard demoted every process in this group to our original UID.
            os.killpg(pid, sig)
        except ProcessLookupError:
            return
        deadline = time.monotonic()+budget
        while time.monotonic() < deadline:
            try:
                os.killpg(pid, 0)
            except ProcessLookupError:
                return
            time.sleep(.025)
    raise RuntimeError('detached clock test group remains after termination')


def collect(work, guard, nonce):
    directory = guard.guard_directory(nonce)
    target = work/'guard'
    target.mkdir(exist_ok=True)
    for name in GUARD_FILES:
        source = directory/name
        if not source.exists():
            continue
        data = privileged_bytes(source)
        destination = target/name
        if destination.exists():
            require(destination.read_bytes() == data, 'collected guard bytes changed: '+name)
        else:
            destination.write_bytes(data)


def recover(work, guard, nonce, env, log):
    directory = guard.guard_directory(nonce)
    if not (directory/'state.json').exists():
        return None  # No privileged restoration anchor means the guard never armed.
    result = subprocess.run(sudo_guard('recover', work), cwd=ROOT, env=env,
                            stdout=log, stderr=subprocess.STDOUT, timeout=90, check=False)
    return result.returncode


def emit_raw(work, output):
    # No fabricated Go event: preserve the actual test2json lines. Guard objects
    # have distinct kinds and cannot satisfy any Go test terminal check.
    errors = []
    raw = work/'raw.jsonl'
    if raw.exists():
        with raw.open('rb') as stream:
            for data in stream:
                try:
                    line = data.decode('utf-8')
                    require(line.endswith('\n'), 'truncated clock test2json line')
                    value = json.loads(line)
                    require(isinstance(value, dict) and 'Action' in value, 'invalid clock test2json record')
                    output.write(line)
                except ValueError as exc:
                    errors.append(str(exc))
    audit = work/'guard'/'guard.jsonl'
    if audit.exists():
        with audit.open('rb') as stream:
            for data in stream:
                try:
                    line = data.decode('utf-8')
                    require(line.endswith('\n'), 'truncated clock audit line')
                    envelope('clock_guard', json.loads(line), output)
                except ValueError as exc:
                    errors.append(str(exc))
    output.flush()
    require(not errors, 'invalid original raw (preserved in work): '+'; '.join(errors))


def run(work, output=sys.stdout):
    guard = load_guard()
    go, nonce, preparation = prepare(work, guard)
    envelope('clock_preparation', preparation, output)
    env = dict(os.environ, OI_IT_BIN=str(work/'bin'), OI_IT_RUNDIR=str(work/'it'), GOTOOLCHAIN='local')
    command = sudo_guard('run', work, '--gid', str(os.getgid()), '--parent-pid', str(os.getpid()),
                         '--go', go, '--binary', str(work/'integration.test'), '--work', str(ROOT),
                         '--raw', str(work/'raw.jsonl'))
    interrupted, errors = [], []
    old_handlers = {}
    proc, code, restored, verified = None, None, None, False
    def interrupt(signum, _frame):
        interrupted.append(signum)
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        old_handlers[sig] = signal.signal(sig, interrupt)
    with (work/'guard-stderr.log').open('wb') as log:
        try:
            proc = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT,
                                    start_new_session=True)
            deadline = time.monotonic()+GUARD_BUDGET
            while proc.poll() is None:
                require(not interrupted, 'clock wrapper interrupted')
                require(time.monotonic() < deadline, 'clock guard exceeded wrapper deadline')
                time.sleep(.05)
            code = proc.returncode
        except (OSError, ValueError, RuntimeError) as exc:
            errors.append(type(exc).__name__+': '+str(exc))
        finally:
            # Each cleanup is independent: failure cannot skip clock recovery or
            # raw retention. Workflow always recovery handles a killed wrapper.
            if proc is not None and proc.poll() is None:
                try:
                    if stop_guard(proc):
                        errors.append('guard required SIGKILL; finally could not be relied on')
                except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as exc:
                    errors.append('guard termination: '+str(exc))
            # A guard already exited with -9, or a failed stop operation, is not
            # evidence that its detached test group was cleaned up.
            try:
                close_orphaned_test(guard, nonce)
            except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as exc:
                errors.append('detached test cleanup: '+str(exc))
            try:
                restored = recover(work, guard, nonce, env, log)
            except (OSError, ValueError, subprocess.SubprocessError) as exc:
                errors.append('clock recovery: '+str(exc))
            try:
                collect(work, guard, nonce)
            except (OSError, ValueError, subprocess.SubprocessError) as exc:
                errors.append('clock audit collection: '+str(exc))
            for sig, handler in old_handlers.items():
                signal.signal(sig, handler)
    try:
        guard.verify(work/'raw.jsonl', work/'guard'/'guard.jsonl')
        verified = True
    except (OSError, ValueError, KeyError, TypeError) as exc:
        errors.append('clock verification: '+str(exc))
    try:
        emit_raw(work, output)
    except (OSError, ValueError) as exc:
        errors.append('clock raw retention: '+str(exc))
    passed = code == 0 and restored == 0 and verified and not errors and not interrupted
    result = dict(schema='g2-clock-result-v1', source_sha=preparation['source_sha'],
                  run_id=preparation['run_id'], guard_exit_code=code, recovery_exit_code=restored,
                  verified=verified, interrupted=interrupted, errors=errors, exit_code=0 if passed else 1,
                  files={p.relative_to(work).as_posix(): sha(p) for p in
                  [work/'raw.jsonl', *(work/'guard'/name for name in GUARD_FILES)] if p.is_file()})
    if preparation['schema'] == 'g2-clock-preparation-v2':
        result.update({k: preparation[k] for k in guard.local_module().IDENTITY_KEYS})
        result['schema'] = 'g2-clock-result-v2'
    dump(work/'result.json', result)
    envelope('clock_result', result, output)
    return result['exit_code']


def recover_only(work, record='external-recovery.json'):
    """Independent host supervisor entry; never converts emergency recovery to success."""
    guard = load_guard()
    require(record in ('external-recovery.json', 'external-final-recovery.json'),
            'unknown independent recovery record')
    attestation = guard.execution_environment(os.environ)
    require(attestation is not None, 'external local recovery requires explicit local profile')
    nonce = guard.nonce_from(str(work/'nonce'), os.getuid())
    expected = guard.identity(nonce)
    errors, restored = [], None
    with (work/'external-recovery.log').open('ab') as log:
        for name, operation in (
                ('orphan_cleanup', lambda: close_orphaned_test(guard, nonce)),
                ('recover', lambda: recover(work, guard, nonce, dict(os.environ), log)),
                ('collect', lambda: collect(work, guard, nonce))):
            try:
                value = operation()
                if name == 'recover':
                    restored = value
            except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as exc:
                errors.append(name+': '+str(exc))
    result = dict(schema='g2-local-recovery-v1', identity=expected,
                  recovery_exit_code=restored, errors=errors,
                  files={p.relative_to(work).as_posix(): sha(p) for p in
                         [work/'raw.jsonl', *(work/'guard'/n for n in GUARD_FILES)] if p.is_file()},
                  exit_code=0 if restored == 0 and not errors else 1)
    if set(result['files']) != {'raw.jsonl', 'guard/guard.jsonl', 'guard/state.json', 'guard/finished.json'}:
        result['errors'].append('independent recovery file inventory incomplete')
        result['exit_code'] = 1
    result['test_group_closed'] = not any(e.startswith('orphan_cleanup:') for e in errors)
    destination = work/record
    require(not destination.exists(), 'external recovery receipt already exists')
    dump(destination, result)
    print(json.dumps(result, sort_keys=True))
    return result['exit_code']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, required=True, help='new directory; never overwrite a previous step')
    parser.add_argument('--recover-only', action='store_true', help='independent local supervisor cleanup')
    parser.add_argument('--recovery-record', choices=('external-recovery.json', 'external-final-recovery.json'),
                        default='external-recovery.json', help='exclusive record for this supervision phase')
    args = parser.parse_args()
    try:
        return recover_only(args.work.resolve(), args.recovery_record) if args.recover_only else run(args.work.resolve())
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        print('clock wrapper: '+str(exc), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
