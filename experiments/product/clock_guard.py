"""D3 clock instrument. Real writes are restricted to the dedicated hosted job.

The state machine is independently testable with an injected clock. No dry-run
result is accepted by the dedicated workflow as real clock evidence.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import struct
import subprocess
import sys
import time

STEP_NS = 30_000_000_000
TOLERANCE_NS = 100_000_000
MAX_LINE = 8192
IDENTITY_KEYS = ('schema','run_id','job','source_sha','nonce_sha256','boot_id','workflow_ref')
UNITS = ('systemd-timesyncd.service', 'chrony.service', 'chronyd.service',
         'ntp.service', 'ntpd.service', 'openntpd.service')


def require(ok, message):
    if not ok:
        raise ValueError(message)


def allowed_workflow(value):
    return isinstance(value,str) and re.fullmatch(
        r'wyw-ai/loom-oi/\.github/workflows/(?:g2-clock\.yml@refs/heads/clock/[A-Za-z0-9._/-]+'
        r'|g2-evidence\.yml@refs/heads/evidence/G2-[A-Za-z0-9._/-]+)',value) is not None


def hosted_environment(env):
    require(sys.platform == 'linux', 'clock instrument requires Linux')
    for key, value in {'OI_IT_CLOCK': '1', 'GITHUB_ACTIONS': 'true',
                       'RUNNER_ENVIRONMENT': 'github-hosted', 'GITHUB_JOB': 'clock',
                       'GITHUB_REPOSITORY': 'wyw-ai/loom-oi'}.items():
        require(env.get(key) == value, 'dedicated hosted workflow gate: '+key)
    require(allowed_workflow(env.get('GITHUB_WORKFLOW_REF')), 'wrong workflow source')
    require(re.fullmatch(r'[1-9][0-9]*', env.get('GITHUB_RUN_ID', '')) is not None,
            'missing workflow run identity')
    require(re.fullmatch(r'[0-9a-f]{40}', env.get('GITHUB_SHA', '')) is not None,
            'missing immutable source commit')


class SystemClock:
    def sample(self):
        # Bracket REALTIME with MONOTONIC, retaining the acquisition uncertainty.
        before = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
        real = time.clock_gettime_ns(time.CLOCK_REALTIME)
        after = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
        return dict(realtime_ns=real, monotonic_ns=(before+after)//2,
                    monotonic_before_ns=before, monotonic_after_ns=after)

    def set(self, value):
        time.clock_settime_ns(time.CLOCK_REALTIME, value)


class ClockState:
    def __init__(self, clock, nonce, emit, anchor=None):
        self.clock, self.nonce, self.emit = clock, nonce, emit
        self.anchor = anchor or clock.sample()
        self.active = None
        self.completed = 0
        self.offset_ns = 0
        self.failed = False

    def offset(self, sample):
        return (sample['realtime_ns']-self.anchor['realtime_ns']
                -sample['monotonic_ns']+self.anchor['monotonic_ns'])

    def witness(self):
        sample = self.clock.sample()
        sample['offset_ns'] = self.offset(sample)
        require(abs(sample['offset_ns']-self.offset_ns) <= TOLERANCE_NS,
                'unexpected realtime drift or asynchronous clock correction')
        return sample

    def restore(self, cause='request'):
        before = self.clock.sample()
        target = self.anchor['realtime_ns']+before['monotonic_ns']-self.anchor['monotonic_ns']
        self.clock.set(target)
        after = self.clock.sample()
        require(abs(self.offset(after)) <= TOLERANCE_NS, 'clock restoration outside tolerance')
        event = dict(action='restore', cause=cause, round_id=self.active, before=before,
                     after=after, actual_delta_ns=(after['realtime_ns']-before['realtime_ns'])
                     -(after['monotonic_ns']-before['monotonic_ns']))
        if self.active is not None:
            self.completed += 1
        self.active, self.offset_ns = None, 0
        self.emit(event)
        return event

    def request(self, request):
        require(isinstance(request, dict), 'request is not an object')
        require(request.get('nonce') == self.nonce, 'wrong job nonce')
        action = request.get('action')
        if action == 'witness':
            require(set(request) == {'nonce', 'action'}, 'unknown witness field')
            return dict(action=action, sample=self.witness(), active_round=self.active,
                        completed=self.completed)
        require(set(request) == ({'nonce', 'action', 'round_id', 'delta_ns'} if action == 'shift'
                                 else {'nonce', 'action', 'round_id'}), 'unknown command fields')
        ident = request.get('round_id')
        require(type(ident) is int and ident >= 1, 'invalid round identity')
        if action == 'shift':
            delta = request.get('delta_ns')
            require(type(delta) is int and delta in (-STEP_NS, STEP_NS), 'only +/-30s allowed')
            require(self.active is None and ident == self.completed+1, 'overlapping/reused/out-of-order round')
            before = self.witness()
            # Set active first: even a failing/partially applied syscall requires restoration.
            self.active = ident
            self.clock.set(before['realtime_ns']+delta)
            self.offset_ns = delta
            after = self.witness()
            actual = (after['realtime_ns']-before['realtime_ns'])-(after['monotonic_ns']-before['monotonic_ns'])
            require(abs(actual-delta) <= TOLERANCE_NS, 'requested clock step was not applied')
            event = dict(action=action, round_id=ident, requested_delta_ns=delta,
                         actual_delta_ns=actual, before=before, after=after)
            self.emit(event)
            return event
        require(action == 'restore' and self.active == ident, 'restore does not match active round')
        self.witness()
        return self.restore()


def unit_states():
    states = []
    for unit in UNITS:
        result = subprocess.run(['systemctl', 'show', unit, '--property=LoadState,ActiveState,UnitFileState',
                                 '--no-pager'], capture_output=True, text=True, timeout=5)
        values = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
        if values.get('LoadState') == 'not-found':
            continue
        # A successful runtime mask deliberately changes LoadState to masked.
        # Retain known, inactive masked units in both the stopped snapshot and
        # the original restoration anchor; they are not ordinary loaded units.
        load, active, enabled = (values.get(name) for name in ('LoadState', 'ActiveState', 'UnitFileState'))
        require(result.returncode == 0 and load in ('loaded', 'masked'),
                'cannot inventory '+unit+' (load='+str(load)+', status='+str(result.returncode)+')')
        if load == 'masked':
            require(active == 'inactive' and enabled in ('masked', 'masked-runtime'),
                    'masked NTP unit is not safely inactive: '+unit)
        require(values.get('ActiveState') in ('active', 'inactive'), 'unstable NTP unit '+unit)
        states.append(dict(unit=unit, active=values['ActiveState'], enabled=values.get('UnitFileState', '')))
    return states


def stop_sync(states):
    # Runtime masks prevent a service dependency/timer from silently restarting it;
    # persistent enabled/disabled state is not changed.
    for state in states:
        unit = state['unit']
        if not state['enabled'].startswith('masked'):
            subprocess.run(['systemctl', 'mask', '--runtime', '--now', unit], check=True, timeout=10,
                           stdout=subprocess.DEVNULL)
        require(subprocess.run(['systemctl', 'is-active', '--quiet', unit]).returncode != 0,
                'time synchronization remained active: '+unit)


def restore_sync(states):
    # Preserve every original name, including service aliases. Runtime masks
    # belong to names, so deduplicating aliases would lose a mask to restore.
    for state in states:
        unit = state['unit']
        if not state['enabled'].startswith('masked'):
            subprocess.run(['systemctl', 'unmask', '--runtime', unit], check=True, timeout=10,
                           stdout=subprocess.DEVNULL)
        if state['active'] == 'active':
            subprocess.run(['systemctl', 'start', unit], check=True, timeout=10)
    actual = unit_states()
    require({s['unit']: (s['active'], s['enabled']) for s in actual}
            == {s['unit']: (s['active'], s['enabled']) for s in states}, 'NTP state restoration mismatch')
    return actual


def restore_environment(state, services, emit, cause):
    # A failed clock syscall must not leave the original synchronization masked.
    errors=[]
    try:
        state.restore(cause)
    except Exception as exc:
        errors.append('clock: '+str(exc))
    restored=None
    try:
        restored=restore_sync(services)
    except Exception as exc:
        errors.append('synchronization: '+str(exc))
    require(not errors,'; '.join(errors))
    return restored


def nonce_from(path, uid):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'r', encoding='ascii') as stream:
        st = os.fstat(stream.fileno())
        require(st.st_uid == uid and st.st_mode & 0o077 == 0, 'nonce file ownership/mode')
        nonce = stream.read(128).strip()
    require(re.fullmatch('[0-9a-f]{64}', nonce) is not None, 'invalid private job nonce')
    return nonce


def guard_directory(nonce):
    return Path('/run/loom-oi-clock-'+hashlib.sha256(nonce.encode()).hexdigest()[:24])


def identity(nonce):
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    require(re.fullmatch(r'[0-9a-f-]{36}', boot) is not None, 'missing boot identity')
    return dict(schema='g2-clock-guard-v1', run_id=os.environ['GITHUB_RUN_ID'],
                job=os.environ['GITHUB_JOB'], source_sha=os.environ['GITHUB_SHA'],
                nonce_sha256=hashlib.sha256(nonce.encode()).hexdigest(), boot_id=boot,
                workflow_ref=os.environ['GITHUB_WORKFLOW_REF'])


def check_saved_identity(saved, expected, uid):
    require(isinstance(saved,dict) and saved.get('identity') == expected and saved.get('uid') == uid,
            'restoration anchor belongs to another boot/job/source/nonce/user')


def terminate_group(child, emit):
    """The group can outlive test2json. Every path is bounded and auditable."""
    if child is None:
        return
    def exists():
        try:
            os.killpg(child.pid, 0)
            return True
        except ProcessLookupError:
            return False
    for sig,budget in ((signal.SIGTERM,2),(signal.SIGKILL,1)):
        try:
            os.killpg(child.pid,sig)
        except ProcessLookupError:
            break
        deadline=time.monotonic()+budget
        while time.monotonic() < deadline:
            child.poll()  # reap the leader even while a grandchild remains
            if not exists():
                break
            time.sleep(.025)
        if not exists():
            break
    child.wait(timeout=1)
    require(not exists(), 'test process group survived bounded termination')
    emit(dict(action='child_cleanup', child_pid=child.pid, group_closed=True))


def write_json(path, obj):
    path=Path(path)
    temporary=path.with_name(path.name+'.tmp')
    with open(temporary, 'w', encoding='utf-8', newline='\n') as stream:
        json.dump(obj, stream, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary,path)


def serve(args):
    hosted_environment(os.environ)
    import pwd
    require(os.geteuid() == 0 and args.uid > 0 and args.gid > 0, 'guard root/test nonroot required')
    nonce = nonce_from(args.nonce_file, args.uid)
    provenance = identity(nonce)
    directory = guard_directory(nonce)
    directory.mkdir(mode=0o711)  # never reuse a previous guard's privileged state
    log = open(directory/'guard.jsonl', 'w', encoding='utf-8', buffering=1)
    log_failed=False
    def emit(event):
        nonlocal log_failed
        event.update(provenance, guard_pid=os.getpid())
        try:
            log.write(json.dumps(event, sort_keys=True)+'\n')
        except OSError:
            log_failed=True  # diagnostic failure cannot interrupt restoration
    state = ClockState(SystemClock(), nonce, emit)
    services = unit_states()
    write_json(directory/'state.json', dict(anchor=state.anchor, services=services, uid=args.uid,
                                          gid=args.gid, identity=provenance))
    emit(dict(action='start', anchor=state.anchor, ntp_before=services, runner_uid=args.uid,
              runner_gid=args.gid))
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    child = None
    interrupted = []
    def interrupt(signum, _frame):
        interrupted.append(signum)
    for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(sig, interrupt)
    code = 1
    supervisor_parent = os.getppid()
    try:
        require(not log_failed,'guard audit unavailable')
        stop_sync(services)
        emit(dict(action='sync_stopped', ntp_stopped=unit_states(), sample=state.witness()))
        sock.bind(str(directory/'control.sock'))
        os.chown(directory/'control.sock', args.uid, args.gid)
        os.chmod(directory/'control.sock', 0o600)
        sock.listen(1)
        sock.settimeout(.05)
        user = pwd.getpwuid(args.uid)
        env = dict(os.environ, HOME=user.pw_dir, USER=user.pw_name, LOGNAME=user.pw_name,
                   PATH=str(Path(args.go).parent)+os.pathsep+os.environ.get('PATH',''),
                   OI_CLOCK_SOCKET=str(directory/'control.sock'), OI_CLOCK_NONCE=nonce,
                   GOPROXY='off', GOSUMDB='off')
        def demote():
            os.initgroups(user.pw_name, args.gid)
            os.setgid(args.gid)
            os.setuid(args.uid)
        with open(args.raw, 'wb') as output:
            os.chown(args.raw, args.uid, args.gid)
            child = subprocess.Popen([args.go, 'tool', 'test2json', '-p',
                'github.com/wyw-ai/loom-oi/tests/integration', '-t', args.binary,
                '-test.v', '-test.run=^TestTVG2_04_RealClock$', '-test.timeout=50m'],
                cwd=args.work, env=env, stdout=output, stderr=subprocess.STDOUT,
                preexec_fn=demote, start_new_session=True)
            emit(dict(action='child_started', child_pid=child.pid))
            last_witness = 0
            test_pid = None
            while child.poll() is None:
                require(not log_failed,'guard audit write failed')
                require(not interrupted, 'guard interrupted '+str(interrupted))
                require(os.getppid() == supervisor_parent and Path('/proc/'+str(args.parent_pid)).exists(),
                        'guard parent lost')
                sample = state.witness()
                if sample['monotonic_ns']-last_witness >= 100_000_000:
                    emit(dict(action='witness', active_round=state.active, sample=sample))
                    last_witness = sample['monotonic_ns']
                try:
                    conn, _ = sock.accept()
                except socket.timeout:
                    continue
                with conn:
                    conn.settimeout(.5)
                    pid, uid, _gid = struct.unpack('3i', conn.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
                    require(uid == args.uid and os.getsid(pid) == child.pid, 'control peer outside test process group')
                    if test_pid is None:
                        test_pid = pid
                        emit(dict(action='test_peer', test_pid=pid, test_uid=uid))
                    require(pid == test_pid, 'test process identity changed')
                    raw = b''
                    while not raw.endswith(b'\n') and len(raw) <= MAX_LINE:
                        part = conn.recv(min(1024, MAX_LINE+1-len(raw)))
                        require(part, 'incomplete control request')
                        raw += part
                    require(len(raw) <= MAX_LINE and raw.endswith(b'\n'), 'unbounded control request')
                    result = state.request(json.loads(raw))
                    result.update(guard_pid=os.getpid(), peer_pid=pid, ok=True)
                    if result.get('action') == 'witness':
                        emit(result)
                    conn.sendall(json.dumps(result).encode()+b'\n')
            code = child.returncode
            require(state.active is None, 'test exited with an active clock shift')
            require(code == 0 and state.completed == 400, 'expected 400 completed real clock rounds')
    except Exception as exc:
        state.failed = True
        emit(dict(action='failure', error=str(exc)))
        print('clock guard:', exc, file=sys.stderr)
        code = 1
    finally:
        try:
            terminate_group(child,emit)
        except Exception as exc:
            code=1
            emit(dict(action='child_cleanup_failed',error=str(exc)))
        # Process cleanup failures must NEVER bypass the independent restoration.
        try:
            restored = restore_environment(state,services,emit,'guard_finally')
            if log_failed:
                code=1
            emit(dict(action='finished', ok=code == 0 and not state.failed,
                      completed=state.completed, ntp_after=restored, sample=state.clock.sample()))
            write_json(directory/'finished.json', dict(restored=True, completed=state.completed,
                       exit_code=code, identity=provenance, uid=args.uid))
        except Exception as exc:
            emit(dict(action='restoration_failed', error=str(exc)))
            code = 1
        if log_failed:
            code=1
        sock.close()
        log.close()
    return code


def recover(args):
    hosted_environment(os.environ)
    require(os.geteuid() == 0, 'recovery requires root')
    nonce = nonce_from(args.nonce_file, args.uid)
    expected = identity(nonce)
    directory = guard_directory(nonce)
    saved = json.loads((directory/'state.json').read_text())
    require(directory.stat().st_uid == 0 and (directory/'state.json').stat().st_uid == 0,
            'untrusted recovery anchor')
    check_saved_identity(saved,expected,args.uid)
    if (directory/'finished.json').exists():
        finished=json.loads((directory/'finished.json').read_text())
        check_saved_identity(finished,expected,args.uid)
        require(finished.get('restored') is True, 'invalid restoration completion')
        return 0
    with open(directory/'guard.jsonl', 'a', encoding='utf-8', buffering=1) as stream:
        def emit(event):
            event.update(expected, guard_pid=os.getpid())
            stream.write(json.dumps(event, sort_keys=True)+'\n')
        state = ClockState(SystemClock(), nonce, emit, anchor=saved['anchor'])
        restored = restore_environment(state,saved['services'],emit,'workflow_always')
        emit(dict(action='emergency_recovered', ntp_after=restored))
    write_json(directory/'finished.json', dict(restored=True, exit_code=1, emergency=True,
                                             identity=expected,uid=args.uid))
    return 1  # successful emergency restoration never turns failed evidence green


def verify_audit(events):
    require(events and events[0].get('action') == 'start', 'guard anchor missing')
    anchor = events[0]['anchor']
    active, complete, shifts, restores, previous_mono = None, 0, {}, {}, 0
    provenance={k:events[0].get(k) for k in IDENTITY_KEYS}
    require(provenance['schema']=='g2-clock-guard-v1' and provenance['job']=='clock'
            and re.fullmatch('[0-9a-f]{40}',provenance.get('source_sha') or '')
            and re.fullmatch('[0-9a-f]{64}',provenance.get('nonce_sha256') or '')
            and re.fullmatch('[0-9a-f-]{36}',provenance.get('boot_id') or '')
            and re.fullmatch('[1-9][0-9]*',provenance.get('run_id') or '')
            and allowed_workflow(provenance['workflow_ref']), 'invalid guard provenance')
    require(type(events[0].get('guard_pid')) is int and events[0]['guard_pid']>0,'missing guard process')
    require(sum(e.get('action')=='start' for e in events)==1 and sum(e.get('action')=='finished' for e in events)==1,'repeated/missing guard lifecycle')
    def sample(value, expected):
        nonlocal previous_mono
        require(isinstance(value, dict), 'missing clock sample')
        keys = ('realtime_ns','monotonic_ns','monotonic_before_ns','monotonic_after_ns')
        require(all(type(value.get(k)) is int and value[k] > 0 for k in keys), 'invalid sampled clock fields')
        require(value['monotonic_before_ns'] <= value['monotonic_ns'] <= value['monotonic_after_ns'], 'clock sampling bracket reversed')
        require(value['monotonic_before_ns']>=previous_mono,'guard monotonic timeline regressed')
        previous_mono=value['monotonic_after_ns']
        offset=value['realtime_ns']-anchor['realtime_ns']-value['monotonic_ns']+anchor['monotonic_ns']
        require(abs(offset-expected) <= TOLERANCE_NS, 'raw clock offset drift')
    sample(anchor,0)
    for event in events:
        require(all(event.get(k)==v for k,v in provenance.items()) and event.get('guard_pid')==events[0]['guard_pid'], 'mixed guard provenance/process')
        action=event.get('action')
        require(action in ('start','sync_stopped','child_started','test_peer','witness','shift','restore','child_cleanup','finished'), 'guard reported failure/unknown action')
        if action == 'shift':
            ident,delta=event.get('round_id'),event.get('requested_delta_ns')
            require(active is None and type(ident) is int and ident == complete+1, 'raw clock round reused/out of order')
            require(type(delta) is int and delta in (-STEP_NS,STEP_NS), 'raw clock step missing')
            sample(event['before'],0);sample(event['after'],delta)
            actual=event['after']['realtime_ns']-event['before']['realtime_ns']-event['after']['monotonic_ns']+event['before']['monotonic_ns']
            require(event.get('actual_delta_ns') == actual and abs(actual-delta) <= TOLERANCE_NS, 'raw syscall delta differs')
            active=ident;shifts[ident]=event
        elif action == 'restore':
            require(event.get('round_id') == active, 'restoration belongs to another round')
            sample(event['before'],shifts[active]['requested_delta_ns'] if active else 0)
            sample(event['after'],0)
            actual=event['after']['realtime_ns']-event['before']['realtime_ns']-event['after']['monotonic_ns']+event['before']['monotonic_ns']
            require(type(event.get('actual_delta_ns')) is int and event['actual_delta_ns']==actual,'raw restore delta differs')
            if event.get('cause') == 'request':
                require(active is not None, 'request restored an inactive round')
                restores[active]=event;complete+=1
            else:
                require(event.get('cause') == 'guard_finally' and active is None, 'emergency cleanup is not passing evidence')
            active=None
        elif action == 'witness':
            require(event.get('active_round') == active, 'witness round mismatch')
            sample(event['sample'],shifts[active]['requested_delta_ns'] if active else 0)
        elif action in ('sync_stopped','finished'):
            sample(event['sample'],0)
    last=events[-1]
    require(last.get('action') == 'finished' and last.get('ok') is True and last.get('completed') == complete == 400
            and active is None and len(shifts) == len(restores) == 400, 'guard did not complete 400 restored rounds')
    require(events[0].get('ntp_before') == last.get('ntp_after'), 'raw NTP restoration differs')
    return shifts,restores


def verify(raw, audit):
    """Workflow scope guard, additional to the versioned behavioral analyzer."""
    terminal, buffers, rounds, observations = {}, {}, {}, []
    for line in Path(raw).read_text(encoding='utf-8').splitlines():
        event = json.loads(line)
        test = event.get('Test', '')
        if event.get('Action') in ('pass', 'fail', 'skip') and test:
            terminal.setdefault(test, []).append(event['Action'])
        if event.get('Action') == 'output':
            buffers[test] = buffers.get(test, '')+event.get('Output', '')
            while '\n' in buffers[test]:
                text, buffers[test] = buffers[test].split('\n', 1)
                if 'OBS ' in text:
                    row = json.loads(text.split('OBS ', 1)[1])
                    if row.get('event') == 'real_clock_round':
                        require(row.get('test') == test, 'clock raw test provenance mismatch')
                        require(type(row.get('round')) is int, 'missing/invalid clock round')
                        rounds.setdefault(test, []).append(row['round'])
                        observations.append(row)
    require(not any('OBS' in value for value in buffers.values()), 'truncated clock observation')
    expected = {'TestTVG2_04_RealClock/'+scenario+'/'+direction+'/'+backend
                for scenario in ('ready','not_ready','preflight','final','total_prepare')
                for direction in ('forward','backward') for backend in ('sqlite','postgres')}
    require(set(rounds) == expected, 'missing/unexpected real-clock leaves')
    require(all(terminal.get(test) == ['pass'] and sorted(rounds[test]) == list(range(20))
                for test in expected), 'clock leaf skipped/failed/incomplete')
    events = [json.loads(line) for line in Path(audit).read_text(encoding='utf-8').splitlines()]
    shifts,restores=verify_audit(events)
    used=set()
    for row in observations:
        shift,restore=row['shift']['guard'],row['restore']['guard']
        ident=shift.get('round_id')
        require(ident not in used and ident in shifts and restore.get('round_id') == ident, 'raw round borrowed another clock operation')
        used.add(ident)
        for original,copy_event in ((shifts[ident],shift),(restores[ident],restore)):
            require(all(copy_event.get(k) == original.get(k) for k in (*IDENTITY_KEYS,'guard_pid','action','round_id','before','after','actual_delta_ns')), 'test/guard clock boundaries differ')
        require(shift.get('requested_delta_ns') == (STEP_NS if '/forward/' in row['test'] else -STEP_NS), 'clock direction not applied')
        before,after=row['shift']['test_before'],row['shift']['test_after']
        actual=after['realtime_ns']-before['realtime_ns']-after['monotonic_ns']+before['monotonic_ns']
        require(row['shift'].get('actual_delta_ns')==actual and abs(actual-shift['requested_delta_ns'])<=TOLERANCE_NS,'independent clock observer did not see requested delta')
        require(before['monotonic_after_ns']<=shift['before']['monotonic_before_ns']
                and shift['after']['monotonic_after_ns']<=after['monotonic_before_ns'],'syscall outside independent observer bracket')
        restored=row['restore']['test_after']
        offset=restored['realtime_ns']-before['realtime_ns']-restored['monotonic_ns']+before['monotonic_ns']
        require(abs(offset)<=TOLERANCE_NS and row['restore'].get('offset_ns')==offset,'independent observer restoration failed')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    run = sub.add_parser('run')
    for name in ('nonce-file','go','binary','work','raw'):
        run.add_argument('--'+name, required=True)
    for name in ('uid','gid','parent-pid'):
        run.add_argument('--'+name, type=int, required=True)
    recovery = sub.add_parser('recover')
    recovery.add_argument('--nonce-file', required=True)
    recovery.add_argument('--uid', type=int, required=True)
    check = sub.add_parser('verify')
    check.add_argument('--raw', required=True)
    check.add_argument('--audit', required=True)
    args = parser.parse_args()
    if args.command == 'run':
        return serve(args)
    if args.command == 'recover':
        return recover(args)
    verify(args.raw, args.audit)
    return 0


if __name__ == '__main__':
    sys.exit(main())
