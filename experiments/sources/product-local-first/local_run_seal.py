"""Local execution provenance. No clock operations, product commands or PID kills."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import time
import types

_path = Path(__file__).with_name('local_execution.py')
local = types.ModuleType('seal_local_execution')
local.__file__ = str(_path)
_local_source = _path.read_bytes()
if '_expected_local_sha256' in globals() and hashlib.sha256(_local_source).hexdigest() != _expected_local_sha256:
    raise ValueError('fixed local_execution source changed before loading')
exec(compile(_local_source, str(_path), 'exec'), local.__dict__)
SCHEMA = 'loom-oi-local-execution-receipt-v1'
HOST_SOURCES = tuple('experiments/product/'+n for n in
                     ('local_supervisor.py', 'local_run_seal.py', 'local_execution.py'))
REQUIRED = {'manifest.json', 'source-manifest.json', 'environment.json', 'execution.json',
            'raw.jsonl', 'stderr.log', 'summary.json', 'report.md'}
ALLOWED = REQUIRED | {'coverage-func.txt', 'vulndb.zip', 'vulndb-provenance.json',
                      'clock-supervisor-receipt.json', 'clock-final-recovery.json',
                      'host-launch.json', 'host-process.json'}
ERRORS = {'runner_nonzero', 'runner_exit_unconfirmed', 'process_identity_unconfirmed',
          'residual_process_group', 'process_inventory_overflow', 'clock_recovery_failed',
          'artifact_collection_failed', 'artifact_changed', 'source_mismatch',
          'vm_identity_mismatch', 'transport_failed', 'deadline_exceeded'}
IDENTITY = ('run_id', 'source_sha', 'boot_id', 'vm_uuid', 'vm_serial', *local.HASH_KEYS)
PROCESS = {'pid', 'start_ticks', 'pgid', 'sid', 'uid', 'gid'}
CHECK = {'step', 'identity', 'exit_code', 'waited_ns', 'checked_ns', 'state', 'remaining_pids', 'overflow'}
KEYS = {'schema', 'execution_profile', *IDENTITY, 'evidence_run_id', 'attestation_sha256',
        'host_sources', 'host_vm', 'runner', 'process_checks', 'clock', 'files',
        'host_started_monotonic_ns', 'host_completed_monotonic_ns', 'errors'}


def require(value, message):
    if not value:
        raise ValueError(message)


def positive(value):
    return type(value) is int and value > 0


def identity(value):
    require(isinstance(value, dict) and set(value) == PROCESS, 'process identity fields differ')
    require(all(positive(value[k]) for k in PROCESS), 'invalid process identity')
    return value


def process_identity(pid=None, proc_root=Path('/proc')):
    """Call immediately after Popen, before poll/wait can reap a short-lived child."""
    pid = os.getpid() if pid is None else pid
    directory = Path(proc_root)/str(pid)
    fields = (directory/'stat').read_text().rsplit(')', 1)[1].split()
    status = dict(line.split(':', 1) for line in (directory/'status').read_text().splitlines() if ':' in line)
    value = dict(pid=pid, pgid=int(fields[2]), sid=int(fields[3]), start_ticks=int(fields[19]),
                 uid=int(status['Uid'].split()[0]), gid=int(status['Gid'].split()[0]))
    return identity(value)


def inspect_process(expected, group=True, proc_root=Path('/proc')):
    identity(expected)
    try:
        current = process_identity(expected['pid'], proc_root)
        state = 'same' if current == expected else 'reused'
    except FileNotFoundError:
        state = 'gone'
    except (OSError, ValueError, IndexError, KeyError):
        state = 'unreadable'
    remaining, overflow = [], False
    if group:
        for directory in Path(proc_root).iterdir():
            if not directory.name.isdigit():
                continue
            try:
                fields = (directory/'stat').read_text().rsplit(')', 1)[1].split()
                if int(fields[2]) != expected['pgid']:
                    continue
                remaining.append(int(directory.name))
                if len(remaining) > 128:
                    overflow = True
                    remaining = remaining[:128]
                    break
            except FileNotFoundError:
                continue
            except (OSError, ValueError, IndexError):
                state = 'unreadable'
    return dict(state=state, remaining_pids=sorted(remaining), overflow=overflow,
                checked_ns=time.monotonic_ns())


def file_hashes(run):
    hashes = {}
    for path in Path(run).iterdir():
        require(not path.is_symlink() and stat.S_ISREG(path.lstat().st_mode), 'nonregular evidence artifact')
        if path.name in ('SHA256SUMS', 'execution-receipt.json'):
            continue
        require(path.name in ALLOWED, 'unknown evidence artifact: '+path.name)
        hashes[path.name] = hashlib.sha256(path.read_bytes()).hexdigest()
    require(REQUIRED <= set(hashes), 'incomplete evidence artifacts')
    return hashes


def _one_fact(rows, name):
    found = [r for r in rows if r.get('kind') == 'fact' and r.get('name') == name]
    require(len(found) == 1 and found[0].get('step') == 'env', 'missing/duplicate '+name)
    return found[0]['value']


def validate_execution_receipt(receipt, manifest, environment, rows, source_manifest, file_hashes):
    """Integrity failures raise; authentic outer failures remain verifiable counterevidence."""
    r, case = receipt, manifest['case']
    require(isinstance(r, dict) and set(r) == KEYS and r['schema'] == SCHEMA, 'execution receipt schema differs')
    require(len(local.canonical(r)) <= 65536, 'execution receipt exceeds 64KiB')
    require(r['execution_profile'] == case.get('execution_profile') == local.PROFILE, 'execution profile differs')
    fact = _one_fact(rows, 'local_execution')
    require(fact == environment.get('local_execution'), 'execution environment differs')
    a = local.validate_attestation(fact['attestation'])
    require(fact['attestation_sha256'] == local.digest(a) == r['attestation_sha256'], 'attestation digest differs')
    require(all(r[k] == a[k] for k in IDENTITY), 'execution receipt identity differs')
    require(r['source_sha'] == manifest['subject_version'] == environment.get('subject_version')
            == _one_fact(rows, 'subject_version'), 'execution source differs')
    require(all(a[k] == v for k, v in case['local_execution'].items()) and not environment.get('ci'), 'manifest VM differs')
    require(re.fullmatch(re.escape(manifest['id'])+r'-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{6}', r['evidence_run_id']) is not None,
            'invalid evidence run identity')
    require(r['host_sources'] == {k: source_manifest.get(k) for k in HOST_SOURCES}
            and all(local.matches('[0-9a-f]{64}', v) for v in r['host_sources'].values()), 'host source differs')
    require(isinstance(file_hashes, dict) and REQUIRED <= set(file_hashes) <= ALLOWED
            and r['files'] == file_hashes
            and all(local.matches('[0-9a-f]{64}', v) for v in file_hashes.values()), 'sealed artifact inventory differs')
    require(file_hashes.get('host-launch.json') == a['launch_sha256']
            and 'host-process.json' in file_hashes, 'measured host launch artifacts absent/different')
    require(({'vulndb.zip', 'vulndb-provenance.json'} & set(file_hashes)) ==
            ({'vulndb.zip', 'vulndb-provenance.json'} if 'vulndb_snapshot' in case else set()),
            'vulnerability database attachments differ from manifest')
    vm = r['host_vm']
    require(isinstance(vm, dict) and set(vm) == {'pid', 'creation_filetime', 'name', 'launch_nonce',
            'command_sha256', 'handle_verified'}, 'host VM record differs')
    require(positive(vm['pid']) and positive(vm['creation_filetime']) and vm['handle_verified'] is True
            and local.matches(local.UUID_RE, vm['launch_nonce'])
            and vm['name'] == 'loom-oi-clock-'+vm['launch_nonce']
            and hashlib.sha256(vm['launch_nonce'].encode()).hexdigest() == a['host_nonce_sha256']
            and local.matches('[0-9a-f]{64}', vm['command_sha256']), 'host VM launch identity differs')
    require(positive(r['host_started_monotonic_ns'])
            and type(r['host_completed_monotonic_ns']) is int
            and r['host_completed_monotonic_ns'] >= r['host_started_monotonic_ns'], 'host time order differs')
    errors = r['errors']
    require(isinstance(errors, list) and len(errors) <= 16 and len(set(errors)) == len(errors)
            and all(e in ERRORS for e in errors), 'unknown/unbounded execution errors')
    failed = bool(errors)
    runner = r['runner']
    require(isinstance(runner, dict) and set(runner) == {'identity', 'started_ns', 'observed_running_ns',
            'checked_ns', 'state', 'exit_code'}, 'runner record differs')
    recorded = _one_fact(rows, 'local_runner')
    require(recorded == environment.get('local_runner')
            and recorded == {k: runner[k] for k in ('identity', 'started_ns')}, 'runner source record differs')
    identity(runner['identity'])
    require(runner['identity']['uid'] == a['runner_uid'] and runner['identity']['gid'] == a['runner_gid'], 'runner user differs')
    require(positive(runner['started_ns']) and positive(runner['checked_ns'])
            and runner['checked_ns'] >= runner['started_ns'], 'runner time order differs')
    seen = runner['observed_running_ns']
    require(seen is None or (type(seen) is int and runner['started_ns'] <= seen <= runner['checked_ns']), 'runner live witness time differs')
    require(runner['state'] in ('gone', 'same', 'reused', 'unreadable'), 'unknown runner state')
    require(runner['exit_code'] is None or type(runner['exit_code']) is int, 'invalid runner exit')
    failed |= seen is None or runner['state'] != 'gone' or runner['exit_code'] not in (0, 1)
    steps = case['steps']
    require(isinstance(steps, list) and 1 <= len(steps) <= 11 and len(set(steps)) == len(steps), 'invalid step inventory')
    require(('clock' in steps) == ('g2_clock' in case), 'clock step declaration differs')
    checks = r['process_checks']
    require(isinstance(checks, list) and len(checks) == len(steps), 'missing process checks')
    identities = set()
    for name, check in zip(steps, checks):
        require(isinstance(check, dict) and set(check) == CHECK and check['step'] == name, 'process check inventory differs')
        require(positive(check['checked_ns']) and check['checked_ns'] >= runner['started_ns'], 'process check time differs')
        require(check['state'] in ('gone', 'same', 'reused', 'unreadable', 'not_started'), 'unknown process state')
        pids = check['remaining_pids']
        require(isinstance(pids, list) and len(pids) <= 128 and pids == sorted(set(pids))
                and all(positive(p) for p in pids) and type(check['overflow']) is bool, 'invalid remaining process inventory')
        starts = [row for row in rows if row.get('step') == name and row.get('kind') == 'start']
        ends = [row for row in rows if row.get('step') == name and row.get('kind') == 'end']
        require(len(starts) <= 1 and len(ends) <= 1, 'duplicate step boundary')
        if check['identity'] is None:
            require(not starts and not ends and check['state'] == 'not_started'
                    and check['exit_code'] is None and check['waited_ns'] is None and not pids
                    and not check['overflow'], 'invented unstarted step')
            failed = True
            continue
        child = identity(check['identity'])
        require(child['pid'] == child['pgid'] == child['sid'] and child['uid'] == a['runner_uid']
                and child['gid'] == a['runner_gid'], 'step is not a dedicated session')
        key = (child['pid'], child['start_ticks'])
        require(key not in identities, 'reused step identity')
        identities.add(key)
        require(len(starts) == 1 and starts[0].get('process') == child, 'step start identity differs')
        if ends:
            require(ends[0].get('process') == {'identity': child, 'waited_ns': check['waited_ns']}
                    and ends[0].get('exit_code') == check['exit_code'], 'step exit identity differs')
            require(type(check['exit_code']) is int and positive(check['waited_ns'])
                    and runner['started_ns'] <= check['waited_ns'] <= check['checked_ns'], 'step wait time differs')
        else:
            require(check['exit_code'] is None and check['waited_ns'] is None, 'invented step exit')
            failed = True
        failed |= check['state'] != 'gone' or bool(pids) or check['overflow']
        if ends:
            failed |= ends[0].get('timed_out') is not False or check['exit_code'] < 0
    if 'g2_clock' not in case:
        require('clock' not in steps and not any(row.get('step')=='clock' for row in rows)
                and r['clock'] is None and not ({'clock-supervisor-receipt.json', 'clock-final-recovery.json'} & set(file_hashes)),
                'ordinary execution must not manufacture clock recovery')
    else:
        clock = r['clock']
        require(isinstance(clock, dict) and set(clock) == {'receipt', 'final_recovery'}, 'clock seal differs')
        for key, filename in (('receipt', 'clock-supervisor-receipt.json'), ('final_recovery', 'clock-final-recovery.json')):
            value = clock[key]
            if value is None:
                require(filename not in file_hashes, 'missing clock object for sealed bytes')
                failed = True
                continue
            require(file_hashes.get(filename) == local.digest(value), 'clock attachment bytes differ')
            require(isinstance(value, dict) and len(local.canonical(value)) <= 8192, 'unbounded clock attachment')
            if key == 'final_recovery':
                require(set(value) == {'schema', 'identity', 'recovery_exit_code', 'errors', 'files',
                        'exit_code', 'test_group_closed'} and value['schema'] == 'g2-local-recovery-v1', 'final recovery schema differs')
                local.validate_identity(value['identity'])
                require(value['recovery_exit_code'] is None or type(value['recovery_exit_code']) is int,
                        'invalid final recovery exit')
                require(type(value['exit_code']) is int and type(value['test_group_closed']) is bool
                        and isinstance(value['errors'], list), 'invalid final recovery outcome')
            else:
                require(value.get('schema') == 'loom-oi-local-supervisor-v1', 'clock receipt schema differs')
            binding = value if key == 'receipt' else value.get('identity', {})
            require(all(binding.get(k) == r[k] for k in ('run_id', 'source_sha', 'boot_id', 'vm_uuid',
                    'attestation_sha256', 'host_nonce_sha256')), 'clock attachment identity differs')
            require(local.matches('[0-9a-f]{64}', binding.get('nonce_sha256')), 'clock nonce missing')
            failed |= value.get('recovery_exit_code') != 0 or value.get('test_group_closed') is not True or value.get('errors') != []
            if key == 'receipt':
                emitted = [x.get('event') for x in rows if x.get('step') == 'clock' and x.get('kind') == 'local_supervisor']
                require(emitted == [value], 'clock receipt not the actual collected receipt')
                require(type(value.get('wrapper_exit_code')) is int, 'invalid clock wrapper exit')
                failed |= value['wrapper_exit_code'] < 0
            else:
                failed |= value.get('exit_code') != 0
        if all(clock.values()):
            require(clock['receipt']['nonce_sha256'] == clock['final_recovery']['identity']['nonce_sha256']
                    and clock['receipt']['files'] == clock['final_recovery']['files'], 'clock final recovery differs')
    return 'failed' if failed else 'complete'


def seal_run(run, receipt):
    run = Path(run)
    require(not (run/'execution-receipt.json').exists() and not (run/'SHA256SUMS').exists(), 'run already sealed')
    hashes = file_hashes(run)
    require(hashes == receipt['files'], 'artifacts changed before sealing')
    manifest = json.loads((run/'manifest.json').read_bytes())
    environment = json.loads((run/'environment.json').read_bytes())
    sources = json.loads((run/'source-manifest.json').read_bytes())
    rows = [json.loads(line) for line in (run/'raw.jsonl').read_text(encoding='utf-8').splitlines() if line]
    require(receipt['evidence_run_id'] == run.name, 'receipt names another evidence directory')
    result = validate_execution_receipt(receipt, manifest, environment, rows, sources, hashes)
    verify_host_materials(run, receipt)
    require(file_hashes(run) == hashes, 'artifacts changed during sealing')
    atomic_new(run/'execution-receipt.json', local.canonical(receipt))
    hashes['execution-receipt.json'] = local.digest(receipt)
    atomic_new(run/'SHA256SUMS', ''.join(h+'  '+n+'\n' for n, h in sorted(hashes.items())).encode())
    return result


def verify_host_materials(run, receipt):
    """Cross-check the trusted host's measured command against original launch bytes."""
    run = Path(run)
    launch_raw = (run/'host-launch.json').read_bytes()
    host_raw = (run/'host-process.json').read_bytes()
    require(len(launch_raw) <= 16384 and len(host_raw) <= 16384, 'unbounded host launch material')
    require(hashlib.sha256(launch_raw).hexdigest() == receipt['launch_sha256'], 'launch original differs from attestation')
    launch, host = json.loads(launch_raw), json.loads(host_raw)
    require(set(host) == {'pid', 'creation_filetime', 'executable', 'command_line', 'argv'}, 'host process material fields differ')
    vm = receipt['host_vm']
    require(host['pid'] == vm['pid'] and host['creation_filetime'] == vm['creation_filetime']
            and hashlib.sha256(host['command_line'].encode()).hexdigest() == vm['command_sha256'], 'host process command differs')
    require(isinstance(host['argv'], list) and 1 <= len(host['argv']) <= 64
            and all(isinstance(x, str) and len(x) <= 8192 for x in host['argv'])
            and host['argv'][1:] == launch['args']
            and host['executable'] == launch['executable'], 'host parsed arguments differ from launch')
    require(host['argv'].count('-name') == 1 and host['argv'][host['argv'].index('-name')+1] == vm['name'], 'launch name differs')


def inspect_run(run):
    """Run in the fixed guest source after the collector has exited."""
    run = Path(run)
    rows = [json.loads(line) for line in (run/'raw.jsonl').read_text(encoding='utf-8').splitlines() if line]
    manifest = json.loads((run/'manifest.json').read_bytes())
    runner = _one_fact(rows, 'local_runner')
    checks = []
    for name in manifest['case']['steps']:
        starts = [r for r in rows if r.get('step') == name and r.get('kind') == 'start']
        ends = [r for r in rows if r.get('step') == name and r.get('kind') == 'end']
        require(len(starts) <= 1 and len(ends) <= 1, 'duplicate process step boundary')
        if not starts:
            checks.append(dict(step=name, identity=None, exit_code=None, waited_ns=None,
                               checked_ns=time.monotonic_ns(), state='not_started', remaining_pids=[], overflow=False))
            continue
        child = identity(starts[0]['process'])
        check = dict(step=name, identity=child, exit_code=ends[0]['exit_code'] if ends else None,
                     waited_ns=ends[0]['process']['waited_ns'] if ends else None, **inspect_process(child))
        checks.append(check)
    return dict(evidence_run_id=run.name, files=file_hashes(run), process_checks=checks,
                runner=runner, runner_check=inspect_process(runner['identity'], group=False))


def atomic_new(path, data):
    """Exclusive atomic publication; never replace an existing artifact."""
    path = Path(path)
    temporary = path.with_name('.'+path.name+'.publishing')
    with temporary.open('xb') as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    os.link(temporary, path, follow_symlinks=False)
    temporary.unlink()
