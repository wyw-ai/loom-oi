"""Windows host supervision for one explicit, isolated QEMU clock evidence run.

This program never sets a host clock. It only launches the fixed guest runner,
requests the guest's existing recovery entry, preserves original bytes, and can
stop the verified QEMU process HANDLE if recovery cannot be confirmed.
"""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import subprocess
import sys
import time
import types


def sibling(name):
    path = Path(__file__).with_name(name+'.py')
    module = types.ModuleType('_supervisor_'+name)
    module.__file__ = str(path)
    exec(compile(path.read_bytes(), str(path), 'exec'), module.__dict__)
    return module


local = sibling('local_execution')
seal = sibling('local_run_seal')
FILES = ('raw.jsonl', 'guard/guard.jsonl', 'guard/state.json', 'guard/finished.json')
IDENTITY = ('execution_profile', 'run_id', 'source_sha', 'boot_id', 'vm_uuid',
            'attestation_sha256', 'host_nonce_sha256', 'nonce_sha256')
REQUEST_KEYS = {'schema', *IDENTITY, 'wrapper_exit_code', 'files'}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def write_new(path, data):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('xb') as stream:
        stream.write(data)


def fixed_path(value, root):
    require(isinstance(value, str) and '\x00' not in value, 'invalid guest path')
    path, parent = PurePosixPath(value), PurePosixPath(root)
    require(path.is_absolute() and '..' not in path.parts and path != parent
            and parent in path.parents, 'guest path outside dedicated task directory')
    return str(path)


def validate_config(config):
    require(isinstance(config, dict) and config.get('schema') == 'loom-oi-local-supervisor-config-v1',
            'unknown supervisor configuration')
    attestation = local.validate_attestation(config['attestation'])
    root = config['guest_root']
    require(root == '/home/oi-vm/.local/task007', 'unexpected dedicated guest root')
    expected_repo = root+'/source/'+attestation['source_sha']
    expected_run = root+'/runs/'+attestation['run_id']
    require(config['guest_repository'] == expected_repo and config['guest_run'] == expected_run,
            'guest repository/run not bound to source/run identity')
    for key in ('guest_repository', 'guest_run', 'guest_environment_file'):
        fixed_path(config[key], root)
    manifest = PurePosixPath(config['manifest'])
    require(manifest.parts[:2] == ('experiments', 'manifests') and len(manifest.parts) == 3
            and re.fullmatch(r'[A-Za-z0-9_-]+\.json', manifest.name), 'invalid fixed manifest path')
    require(config['guest_attestation'] == '/var/lib/loom-oi/attestations/'+attestation['run_id']+'.json',
            'unexpected root attestation path')
    require(type(config['timeout_seconds']) is int and 1 <= config['timeout_seconds'] <= 86400,
            'invalid outer run timeout')
    vm = config['vm']
    require(type(vm['pid']) is int and vm['pid'] > 0 and type(vm['creation_filetime']) is int
            and vm['creation_filetime'] > 0, 'invalid VM process identity')
    require(vm['vm_uuid'] == attestation['vm_uuid'] and vm['vm_serial'] == attestation['vm_serial'],
            'VM identity differs from attestation')
    require(all(vm[k] == attestation[k] for k in ('launch_sha256', 'qemu_binary_sha256')),
            'VM executable/launch differs from attestation')
    require(local.matches(local.UUID_RE, vm['launch_nonce']), 'missing actual QEMU launch nonce')
    require(vm['name'] == 'loom-oi-clock-'+vm['launch_nonce'], 'wrong QEMU launch name')
    require(sha(vm['launch_nonce'].encode('utf-8')) == attestation['host_nonce_sha256'],
            'attestation host nonce does not name this actual QEMU launch')
    return config


def validate_request(value, attestation):
    require(isinstance(value, dict) and set(value) == REQUEST_KEYS
            and value['schema'] == 'loom-oi-local-supervision-request-v1', 'invalid supervision request schema')
    expected = dict(execution_profile=local.PROFILE, attestation_sha256=local.digest(attestation),
                    **{k: attestation[k] for k in IDENTITY if k in attestation})
    require(all(value.get(k) == v for k, v in expected.items()), 'supervision request identity mismatch')
    require(local.matches('[0-9a-f]{64}', value['nonce_sha256']), 'invalid guard nonce digest')
    require(type(value['wrapper_exit_code']) is int, 'invalid wrapper exit code')
    require(isinstance(value['files'], dict) and set(value['files']) == set(FILES)
            and all(local.matches('[0-9a-f]{64}', h) for h in value['files'].values()),
            'invalid supervision file inventory')
    return value


def validate_recovery(result, attestation, request=None):
    require(isinstance(result, dict) and result.get('schema') == 'g2-local-recovery-v1',
            'invalid independent recovery result')
    identity = local.validate_identity(result.get('identity'))
    expected = dict(execution_profile=local.PROFILE, attestation_sha256=local.digest(attestation),
                    **{k: attestation[k] for k in local.IDENTITY_KEYS if k in attestation and k != 'schema'})
    require(all(identity.get(k) == v for k, v in expected.items()), 'independent recovery identity mismatch')
    if request:
        require(all(identity[k] == request[k] for k in IDENTITY), 'recovery differs from requested guard')
        require(result.get('files') == request['files'], 'recovery changed original clock files')
    require(type(result.get('exit_code')) is int and result['exit_code'] == 0
            and type(result.get('recovery_exit_code')) is int and result['recovery_exit_code'] == 0
            and result.get('test_group_closed') is True and result.get('errors') == [],
            'independent guest recovery did not confirm cleanup')
    require(isinstance(result.get('files'), dict) and set(result['files']) == set(FILES)
            and all(local.matches('[0-9a-f]{64}', v) for v in result['files'].values()),
            'independent recovery file inventory incomplete')
    return result


def validate_launch(launch, expected, port):
    args = launch.get('args')
    require(isinstance(args, list) and all(isinstance(s, str) for s in args), 'invalid recorded QEMU arguments')
    permitted = {'-name', '-machine', '-accel', '-cpu', '-smp', '-m', '-L', '-display',
                 '-monitor', '-serial', '-drive', '-netdev', '-device', '-rtc', '-pidfile', '-uuid', '-smbios'}
    values, i = {}, 0
    while i < len(args):
        flag = args[i]
        if flag == '-no-reboot':
            require(flag not in values, 'duplicate QEMU flag')
            values[flag] = ['']
            i += 1
            continue
        require(flag in permitted and i+1 < len(args), 'unknown or incomplete QEMU option')
        values.setdefault(flag, []).append(args[i+1])
        require(flag == '-drive' or len(values[flag]) == 1, 'duplicate QEMU isolation option')
        i += 2
    for key, value in {'-name': expected['name'], '-uuid': expected['vm_uuid'], '-display': 'none',
                       '-monitor': 'none', '-accel': 'tcg,thread=multi',
                       '-netdev': 'user,id=tasknet,restrict=on,hostfwd=tcp:127.0.0.1:'+str(port)+'-:22',
                       '-device': 'virtio-net-pci,netdev=tasknet,mac=52:54:00:7d:00:01',
                       '-smbios': 'type=1,manufacturer=Loom-OI-TaskLocal,product=Ubuntu24-Clock-Preparation,serial='+expected['vm_serial']}.items():
        require(values.get(key) == [value], 'QEMU isolation/start option differs: '+key)
    require(len(values.get('-drive', [])) == 2, 'unexpected QEMU drives')
    task = Path(expected['task_directory']).resolve()
    formats = []
    for drive in values['-drive']:
        pairs = [part.split('=', 1) for part in drive.split(',')]
        require(all(len(pair) == 2 for pair in pairs), 'invalid QEMU drive options')
        options = dict(pairs)
        require(len(options) == len(pairs) and set(options) <= {'file', 'if', 'format', 'readonly'}
                and options.get('if') == 'virtio' and options.get('format') in ('qcow2', 'raw'),
                'unexpected QEMU drive options')
        require(options.get('readonly') == ('on' if options['format'] == 'raw' else None),
                'seed must be read only and child image writable')
        formats.append(options['format'])
        path = Path(options['file']).resolve()
        require(path.is_relative_to(task) and path != task, 'QEMU drive outside dedicated task directory')
    require(sorted(formats) == ['qcow2', 'raw'], 'expected one child image and one read-only seed')
    for flag in ('-serial', '-L', '-pidfile'):
        require(len(values.get(flag, [])) == 1, 'missing QEMU task path '+flag)
        value = values[flag][0]
        if flag == '-serial':
            require(value.startswith('file:'), 'serial console must not expose another listener')
            value = value[5:]
        path = Path(value).resolve()
        require(path.is_relative_to(task) and path != task, 'QEMU file path outside task directory')
    require(os.path.normcase(launch['executable']) == os.path.normcase(expected['executable']), 'recorded QEMU executable differs')
    return args


def receipt(request, recovery, started, completed):
    return dict(schema='loom-oi-local-supervisor-v1', **{k: request[k] for k in IDENTITY},
                request_sha256=local.digest(request), wrapper_exit_code=request['wrapper_exit_code'],
                recovery_exit_code=recovery['recovery_exit_code'],
                test_group_closed=recovery['test_group_closed'], files=dict(request['files']),
                host_started_monotonic_ns=started, host_completed_monotonic_ns=completed,
                errors=list(recovery['errors']))


def supervise(backend, config, output, monotonic=time.monotonic_ns, sleep=time.sleep):
    """Backend is injectable so failure paths are tested without a VM or clock."""
    validate_config(config)
    output = Path(output)
    output.mkdir(parents=True, exist_ok=False)
    started = monotonic()
    deadline = started + config['timeout_seconds']*1_000_000_000
    request, recovered, code, process, errors = None, False, None, None, []
    verified = False
    clock_enabled = False
    final_recovery = None
    supervision = None
    deadline_failed = False

    def within_deadline():
        nonlocal deadline_failed
        if monotonic() >= deadline:
            deadline_failed = True
            raise ValueError('outer guest run deadline exceeded')

    backend.active_deadline = deadline
    try:
        backend.verify()
        within_deadline()
        verified = True
        clock_enabled = backend.clock_enabled
        process = backend.start(output)
        within_deadline()
        while True:
            backend.observe_runner()
            within_deadline()
            raw_request = backend.request() if clock_enabled else None
            within_deadline()
            if raw_request is not None and request is not None:
                require(raw_request == local.canonical(request), 'supervision request changed after publication')
            if raw_request is not None and request is None:
                write_new(output/'supervision-request.json', raw_request)
                request = validate_request(json.loads(raw_request), config['attestation'])
                require(raw_request == local.canonical(request), 'noncanonical supervision request')
                snapshot = backend.collect(output/'clock-before', FILES)
                within_deadline()
                require(snapshot == request['files'], 'request hashes differ from actual original files')
                result = backend.recover(output, final=False)
                within_deadline()
                validate_recovery(result, config['attestation'], request)
                require(backend.collect(output/'clock-after', FILES) == request['files'],
                        'collected recovery bytes differ from request')
                within_deadline()
                value = receipt(request, result, started, monotonic())
                write_new(output/'supervisor-receipt.json', local.canonical(value))
                backend.publish(value)
                within_deadline()
                recovered = True
            code = process.poll()
            within_deadline()
            if code is not None:
                require(not clock_enabled or request is not None and recovered, 'runner exited without independent clock supervision')
                break
            sleep(1)
    except (Exception, KeyboardInterrupt) as exc:
        errors.append(type(exc).__name__+': '+str(exc))
    finally:
        backend.active_deadline = None
        if verified:
            stop = False
            try:
                if clock_enabled:
                    result = backend.recover(output, final=True)
                    final_recovery = result
                    validate_recovery(result, config['attestation'], request)
                # Final recovery is a separate original artifact, never an edit
                # to the already published clock receipt or run summary.
                    final_files = backend.collect(output/'clock-final', FILES)
                    require(final_files == result['files'], 'final recovery files differ from collected bytes')
            except (Exception, KeyboardInterrupt) as exc:
                errors.append('final recovery: '+type(exc).__name__+': '+str(exc))
                stop = True
            try:
                backend.preserve(output)
            except Exception as exc:
                errors.append('preserve: '+str(exc))
            try:
                supervision = backend.finish(output, code, started, monotonic(), final_recovery,
                                             'deadline_exceeded' if deadline_failed else ('transport_failed' if errors else None))
                stop = stop or supervision['stop_required']
                if supervision['result'] != 'complete':
                    errors.append('execution supervision failed')
            except (Exception, KeyboardInterrupt) as exc:
                errors.append('execution seal: '+str(exc))
                stop = True
            if stop or (process is not None and process.poll() is None):
                # The SSH transport is ours, not an arbitrary guest PID. If the
                # entire run did not exit, end the dedicated VM after recovery.
                try:
                    backend.stop_vm()
                except Exception as exc:
                    errors.append('unfinished runner VM stop: '+str(exc))
                if process is not None and process.poll() is None:
                    try:
                        process.terminate()
                        try:
                            process.wait(timeout=10)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait(timeout=10)
                    except Exception as exc:
                        errors.append('close owned SSH transport: '+str(exc))
        try:
            backend.close()
        except Exception as exc:
            errors.append('close host resources: '+str(exc))
        result = dict(schema='loom-oi-local-host-run-v1', run_id=config['attestation']['run_id'],
                      host_started_monotonic_ns=started, host_completed_monotonic_ns=monotonic(),
                      guest_exit_code=code, clock_receipt_written=recovered, errors=errors,
                      execution_supervision=supervision['result'] if supervision else 'unsealed',
                      exit_code=0 if code == 0 and (not clock_enabled or recovered) and not errors else 1)
        write_new(output/'host-result.json', local.canonical(result))
    return result


class WindowsVM:
    """Hold an identity-checked process object; never terminate by a reused PID."""
    def __init__(self, expected, port):
        require(os.name == 'nt', 'host supervisor currently requires Windows')
        from ctypes import wintypes
        self.kernel = ctypes.WinDLL('kernel32', use_last_error=True)
        self.kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        self.kernel.OpenProcess.restype = wintypes.HANDLE
        self.kernel.GetProcessTimes.argtypes = [wintypes.HANDLE, *([ctypes.POINTER(wintypes.FILETIME)]*4)]
        self.kernel.QueryFullProcessImageNameW.argtypes = [wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR, ctypes.POINTER(wintypes.DWORD)]
        self.kernel.TerminateProcess.argtypes = [wintypes.HANDLE, wintypes.UINT]
        self.kernel.CloseHandle.argtypes = [wintypes.HANDLE]
        self.handle = self.kernel.OpenProcess(0x00100000 | 0x1000 | 0x0001, False, expected['pid'])
        require(bool(self.handle), 'cannot open dedicated VM process')
        try:
            launch_bytes = Path(expected['launch_file']).read_bytes()
            require(sha(launch_bytes) == expected['launch_sha256'], 'actual launch record digest mismatch')
            launch_args = validate_launch(json.loads(launch_bytes), expected, port)
            times = [wintypes.FILETIME() for _ in range(4)]
            require(self.kernel.GetProcessTimes(self.handle, *(ctypes.byref(t) for t in times)), 'cannot read VM creation identity')
            created = (times[0].dwHighDateTime << 32) | times[0].dwLowDateTime
            require(created == expected['creation_filetime'], 'recorded VM PID was reused')
            size = wintypes.DWORD(32768)
            buf = ctypes.create_unicode_buffer(size.value)
            require(self.kernel.QueryFullProcessImageNameW(self.handle, 0, buf, ctypes.byref(size)), 'cannot read VM executable')
            require(os.path.normcase(buf.value) == os.path.normcase(expected['executable']), 'VM executable path mismatch')
            require(sha(Path(buf.value).read_bytes()) == expected['qemu_binary_sha256'], 'VM executable digest mismatch')
            script = '$p=Get-CimInstance Win32_Process -Filter "ProcessId='+str(expected['pid'])+'"; $p.CommandLine'
            command = subprocess.check_output(['powershell', '-NoProfile', '-NonInteractive', '-Command', script],
                                              text=True, creationflags=subprocess.CREATE_NO_WINDOW).strip()
            require(command == expected['command_line'], 'VM command line changed')
            shell = ctypes.WinDLL('shell32', use_last_error=True)
            shell.CommandLineToArgvW.argtypes = [wintypes.LPCWSTR, ctypes.POINTER(ctypes.c_int)]
            shell.CommandLineToArgvW.restype = ctypes.POINTER(wintypes.LPWSTR)
            self.kernel.LocalFree.argtypes = [wintypes.HLOCAL]
            count = ctypes.c_int()
            parsed = shell.CommandLineToArgvW(command, ctypes.byref(count))
            require(bool(parsed), 'cannot parse actual QEMU command')
            try:
                actual = [parsed[i] for i in range(count.value)]
                require(actual[1:] == launch_args, 'actual QEMU arguments differ from measured launch')
            finally:
                self.kernel.LocalFree(parsed)
            self.launch_bytes = launch_bytes
            self.process_record = dict(pid=expected['pid'], creation_filetime=created,
                                       executable=buf.value, command_line=command, argv=actual)
        except BaseException:
            self.close()
            raise

    def stop(self):
        # This handle continues to name the original kernel process object even
        # after its PID has been recycled. It cannot kill the replacement.
        from ctypes import wintypes
        code = wintypes.DWORD()
        self.kernel.GetExitCodeProcess.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]
        require(self.kernel.GetExitCodeProcess(self.handle, ctypes.byref(code)), 'cannot inspect VM exit')
        if code.value == 259:
            require(self.kernel.TerminateProcess(self.handle, 1), 'failed to stop dedicated VM')
            self.kernel.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
            require(self.kernel.WaitForSingleObject(self.handle, 10000) == 0,
                    'dedicated VM termination not confirmed')

    def close(self):
        if getattr(self, 'handle', None):
            self.kernel.CloseHandle(self.handle)
            self.handle = None


class SSHBackend:
    def __init__(self, config):
        self.config, self.vm, self.streams = config, None, []
        self.recovery_unconfirmed = False
        self.clock_enabled = False
        self.runner_witness = None
        self.active_deadline = None
        s = config['ssh']
        require(type(s['port']) is int and 1 <= s['port'] <= 65535 and s['user'] == 'oi-vm', 'invalid task SSH identity')
        for key in ('private_key', 'known_hosts'):
            require(Path(s[key]).is_absolute() and Path(s[key]).is_file(), 'missing fixed task SSH '+key)
        self.ssh = ['ssh', '-F', 'NUL', '-i', s['private_key'], '-p', str(s['port']),
                    '-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes', '-o', 'IdentityAgent=none',
                    '-o', 'ForwardAgent=no', '-o', 'StrictHostKeyChecking=yes',
                    '-o', 'HostKeyAlias=oi-task007-local-vm', '-o', 'UserKnownHostsFile='+s['known_hosts'],
                    '-o', 'GlobalKnownHostsFile=NUL', '-o', 'ConnectTimeout=8', 'oi-vm@127.0.0.1']
        self.work = config['guest_run']+'/work/clock'

    def remote(self, argv, **kwargs):
        timeout = 45
        if self.active_deadline is not None:
            timeout = min(timeout, (self.active_deadline-time.monotonic_ns())/1_000_000_000)
            require(timeout > 0, 'outer guest run deadline exceeded')
        return subprocess.run(self.ssh+[shlex.join(argv)], check=True, timeout=timeout,
                              creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0), **kwargs)

    def command(self, args, clock=False):
        c, a = self.config, self.config['attestation']
        env = dict(OI_EXECUTION_PROFILE=local.PROFILE, OI_LOCAL_RUN_ID=a['run_id'],
                   OI_LOCAL_SOURCE_SHA=a['source_sha'], OI_LOCAL_ATTESTATION=c['guest_attestation'])
        if clock:
            env['OI_IT_CLOCK'] = '1'
        return 'set -eu; . '+shlex.quote(c['guest_environment_file'])+'; cd '+shlex.quote(c['guest_repository'])+'; exec '+shlex.join(['env', *(k+'='+v for k,v in env.items()), 'python3', *args])

    def verify(self):
        self.vm = WindowsVM(self.config['vm'], self.config['ssh']['port'])
        c, a = self.config, self.config['attestation']
        script = """import pathlib,subprocess,sys,json,types,hashlib
r=pathlib.Path(sys.argv[1]); repo=pathlib.Path(sys.argv[2]); run=pathlib.Path(sys.argv[3]); env=pathlib.Path(sys.argv[4])
for p in (repo,run,env):
 assert p.resolve().is_relative_to(r.resolve()) and p.resolve()!=r.resolve(), 'guest realpath escaped task root'
assert not run.exists(), 'guest run directory already exists'
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip()==sys.argv[5], 'guest source mismatch'
assert not subprocess.check_output(['git','status','--porcelain','--untracked-files=no'],cwd=repo,text=True), 'dirty guest source'
p=repo/'experiments/product/local_execution.py';m=types.ModuleType('fixed_local');m.__file__=str(p);exec(compile(p.read_bytes(),str(p),'exec'),m.__dict__)
a=m.read_attestation(sys.argv[6]); assert a==json.loads(sys.argv[7]), 'root attestation differs'
f=m.platform_facts(); assert all(f[k]==a[k] for k in ('vm_uuid','vm_serial','boot_id')), 'guest boot mismatch'
assert f['virtualization']=='qemu' and f['sys_vendor']=='Loom-OI-TaskLocal' and f['product_name']=='Ubuntu24-Clock-Preparation'
case=json.loads((repo/sys.argv[8]).read_bytes())['case']
print(json.dumps({'facts':f,'case':case,'host_sources':{n:hashlib.sha256((repo/n).read_bytes()).hexdigest() for n in ('experiments/product/local_supervisor.py','experiments/product/local_run_seal.py','experiments/product/local_execution.py')}},sort_keys=True))
"""
        result = self.remote(['python3', '-c', script, c['guest_root'], c['guest_repository'], c['guest_run'],
                              c['guest_environment_file'], a['source_sha'], c['guest_attestation'], json.dumps(a), c['manifest']], capture_output=True)
        self.verified_facts = json.loads(result.stdout)
        require(self.verified_facts['host_sources'] == {n: sha(Path(__file__).with_name(Path(n).name).read_bytes())
                for n in seal.HOST_SOURCES}, 'host supervisor differs from fixed guest source')
        self.clock_enabled = 'g2_clock' in self.verified_facts['case']

    def fixed_helper(self, body, *args):
        script = """import pathlib,sys,types,json,hashlib
repo=pathlib.Path(sys.argv[1]);p=repo/'experiments/product/local_run_seal.py'
"""+"expected="+repr(self.verified_facts['host_sources'])+"\nboot="+repr(self.config['attestation']['boot_id'])+"\n"+\
            "task=pathlib.Path("+repr(self.config['guest_root'])+")\nrun=pathlib.Path("+repr(self.config['guest_run'])+")\n"+"""
assert task.resolve()==task and repo.resolve()==repo and repo.is_relative_to(task), 'fixed repository path changed'
assert run.resolve()==run and run.is_relative_to(task), 'fixed run path changed'
for arg in sys.argv[2:]:
 q=pathlib.Path(arg)
 assert q.is_absolute() and q.resolve()==q and q.is_relative_to(run), 'fixed helper input escaped run'
assert pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip()==boot, 'guest boot changed'
sources={n:(repo/n).read_bytes() for n in expected}
assert all(hashlib.sha256(sources[n]).hexdigest()==h for n,h in expected.items()), 'fixed host/guest source changed'
assert p.resolve()==repo.resolve()/'experiments/product/local_run_seal.py', 'fixed helper path escaped repository'
m=types.ModuleType('fixed_seal');m.__file__=str(p);m._expected_local_sha256=expected['experiments/product/local_execution.py']
exec(compile(sources['experiments/product/local_run_seal.py'],str(p),'exec'),m.__dict__)
"""+body
        return self.remote(['python3', '-c', script, self.config['guest_repository'], *args], capture_output=True).stdout

    def observe_runner(self):
        data = self.fixed_helper("""p=pathlib.Path(sys.argv[2])
if p.exists():
 a=json.loads(p.read_bytes()); c=m.inspect_process(a['runner']['identity'],group=False)
 print(json.dumps({'anchor':a,'check':c}))
""", self.config['guest_run']+'/work/collector.json')
        if not data:
            return
        observation = json.loads(data)
        anchor, check = observation['anchor'], observation['check']
        a = self.config['attestation']
        require(anchor['schema'] == 'loom-oi-local-collector-v1'
                and all(anchor[k] == a[k] for k in ('run_id', 'source_sha', 'boot_id')), 'collector anchor differs')
        if self.runner_witness:
            require(self.runner_witness['anchor'] == anchor, 'collector identity changed')
        elif check['state'] == 'same':
            self.runner_witness = observation

    def start(self, output):
        write_new(output/'verified-host-guest.json', local.canonical(self.verified_facts))
        write_new(output/'host-launch.json', self.vm.launch_bytes)
        write_new(output/'host-process.json', local.canonical(self.vm.process_record))
        for name in ('runner.stdout', 'runner.stderr'):
            self.streams.append((output/name).open('xb'))
        command = self.command(['experiments/product/run.py', self.config['manifest'], '--work',
                                self.config['guest_run']+'/work', '--output-root', self.config['guest_run']+'/out'])
        write_new(output/'command.json', local.canonical({'command': command, 'vm': self.config['vm']}))
        return subprocess.Popen(self.ssh+[command], stdout=self.streams[0], stderr=self.streams[1],
                                creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))

    def request(self):
        script = "from pathlib import Path;import sys;p=Path(sys.argv[1]);sys.stdout.buffer.write(p.read_bytes() if p.is_file() else b'')"
        data = self.remote(['python3', '-c', script, self.work+'/supervision-request.json'], capture_output=True).stdout
        require(len(data) <= 8192, 'oversized supervision request')
        return data or None

    def collect(self, output, names):
        hashes = {}
        for name in names:
            require(name in FILES, 'unknown clock artifact')
            destination = Path(output)/name
            destination.parent.mkdir(parents=True, exist_ok=True)
            with destination.open('xb') as stream:
                self.remote(['cat', '--', self.work+'/'+name], stdout=stream, stderr=subprocess.PIPE)
            hashes[name] = sha(destination.read_bytes())
        return hashes

    def recover(self, output, final):
        require(not self.recovery_unconfirmed, 'prior recovery transport unconfirmed; refusing parallel recovery')
        self.recovery_unconfirmed = True
        name = 'external-final-recovery.json' if final else 'external-recovery.json'
        command = self.command(['experiments/product/clock_run.py', '--work', self.work,
                                '--recover-only', '--recovery-record', name], clock=True)
        # This cleanup transport includes the existing 90s guest recovery plus
        # orphan cleanup and provenance collection. It does not extend the
        # runner's original 120s receipt wait or any product deadline. A lost
        # transport cannot trigger a concurrent second recovery invocation.
        try:
            timeout = 240 if self.active_deadline is None else min(240,(self.active_deadline-time.monotonic_ns())/1_000_000_000)
            require(timeout > 0, 'outer guest run deadline exceeded')
            result = subprocess.run(self.ssh+[command], capture_output=True, timeout=timeout,
                                    creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))
        except subprocess.TimeoutExpired as exc:
            write_new(Path(output)/(name+'.stdout'), exc.stdout or b'')
            write_new(Path(output)/(name+'.stderr'), exc.stderr or b'')
            raise
        write_new(Path(output)/(name+'.stdout'), result.stdout)
        write_new(Path(output)/(name+'.stderr'), result.stderr)
        # Retain the exact guest artifact even when its exit/result is failure.
        raw = self.remote(['cat', '--', self.work+'/'+name], capture_output=True).stdout
        write_new(Path(output)/name, raw)
        value = json.loads(raw)
        require(result.returncode == value.get('exit_code'), 'recovery transport/result exit mismatch')
        require(raw == local.canonical(value), 'noncanonical independent recovery artifact')
        self.recovery_unconfirmed = False
        return value

    def publish(self, value):
        path = '/var/lib/loom-oi/receipts/'+self.config['attestation']['run_id']+'.json'
        script = """import os,pathlib,stat,sys
p=pathlib.Path(sys.argv[1]); parent=p.parent
assert str(parent)=='/var/lib/loom-oi/receipts' and not parent.is_symlink()
parent.mkdir(parents=True,mode=0o755,exist_ok=True)
for d in (pathlib.Path('/var'),pathlib.Path('/var/lib'),pathlib.Path('/var/lib/loom-oi'),parent):
 s=d.lstat(); assert stat.S_ISDIR(s.st_mode) and s.st_uid==0 and not s.st_mode&0o022
data=sys.stdin.buffer.read(8193); assert len(data)<=8192
temporary=parent/('.'+p.name+'.publishing')
fd=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o644)
with os.fdopen(fd,'wb') as f: f.write(data);f.flush();os.fsync(f.fileno())
os.link(temporary,p,follow_symlinks=False)
temporary.unlink()
directory=os.open(parent,os.O_RDONLY|os.O_DIRECTORY)
try: os.fsync(directory)
finally: os.close(directory)
"""
        self.remote(['sudo', '-n', 'python3', '-c', script, path], input=local.canonical(value), capture_output=True)
        raw = self.remote(['cat', '--', path], capture_output=True).stdout
        require(raw == local.canonical(value), 'root receipt readback differs')

    def preserve(self, output):
        # The original final run bytes are archived, never rewritten by host.
        with (Path(output)/'guest-run.tar').open('xb') as stream:
            self.remote(['tar', '-C', self.config['guest_run'], '-cf', '-', 'out', 'work'], stdout=stream, stderr=subprocess.PIPE)

    def finish(self, output, code, started, completed, final_recovery, earlier_error):
        """Seal a host copy after independent guest process/file observations."""
        snapshot_script = """out=pathlib.Path(sys.argv[2]);runs=list(out.iterdir())
assert len(runs)==1 and runs[0].is_dir() and not runs[0].is_symlink(), 'ambiguous evidence directory'
print(json.dumps(m.inspect_run(runs[0])))
"""
        snapshot = json.loads(self.fixed_helper(snapshot_script, self.config['guest_run']+'/out'))
        run = Path(output)/'evidence'/snapshot['evidence_run_id']
        require(Path(snapshot['evidence_run_id']).name == snapshot['evidence_run_id'], 'invalid evidence directory name')
        run.mkdir(parents=True, exist_ok=False)
        for name, expected in snapshot['files'].items():
            require(name in seal.ALLOWED, 'unknown original evidence file')
            with (run/name).open('xb') as stream:
                self.remote(['cat', '--', self.config['guest_run']+'/out/'+run.name+'/'+name],
                            stdout=stream, stderr=subprocess.PIPE)
            require(sha((run/name).read_bytes()) == expected, 'downloaded original artifact differs')
        after = json.loads(self.fixed_helper(snapshot_script, self.config['guest_run']+'/out'))
        require(after['files'] == snapshot['files'] and after['runner'] == snapshot['runner'], 'guest artifacts changed during preservation')
        errors = []
        if code is None:
            errors.append('runner_exit_unconfirmed')
        elif code not in (0, 1):
            errors.append('runner_nonzero')
        if earlier_error:
            errors.append(earlier_error)
        witness = self.runner_witness
        recorded = after['runner']
        if witness:
            require(witness['anchor']['runner'] == recorded and witness['anchor']['evidence_run_id'] == run.name,
                    'live collector witness differs from preserved run')
        if not witness or after['runner_check']['state'] != 'gone':
            errors.append('process_identity_unconfirmed')
        checks = after['process_checks']
        if any(c['remaining_pids'] for c in checks):
            errors.append('residual_process_group')
        if any(c['overflow'] for c in checks):
            errors.append('process_inventory_overflow')
        if any(c['state'] not in ('gone', 'not_started') for c in checks) and 'process_identity_unconfirmed' not in errors:
            errors.append('process_identity_unconfirmed')
        clock = None
        if self.clock_enabled:
            clock = dict(receipt=None, final_recovery=final_recovery)
            receipt_path = Path(output)/'supervisor-receipt.json'
            if receipt_path.is_file():
                clock['receipt'] = json.loads(receipt_path.read_bytes())
            for key, filename in (('receipt', 'clock-supervisor-receipt.json'), ('final_recovery', 'clock-final-recovery.json')):
                if clock[key] is not None:
                    write_new(run/filename, local.canonical(clock[key]))
            if not all(clock.values()):
                errors.append('clock_recovery_failed')
        a, vm = self.config['attestation'], self.config['vm']
        for name in ('host-launch.json', 'host-process.json'):
            write_new(run/name, (Path(output)/name).read_bytes())
        receipt_value = dict(schema=seal.SCHEMA, execution_profile=local.PROFILE,
            **{k:a[k] for k in seal.IDENTITY}, evidence_run_id=run.name, attestation_sha256=local.digest(a),
            host_sources=self.verified_facts['host_sources'],
            host_vm=dict(pid=vm['pid'], creation_filetime=vm['creation_filetime'], name=vm['name'],
                         launch_nonce=vm['launch_nonce'], command_sha256=sha(self.vm.process_record['command_line'].encode()), handle_verified=True),
            runner=dict(recorded, observed_running_ns=witness['check']['checked_ns'] if witness else None,
                        checked_ns=after['runner_check']['checked_ns'], state=after['runner_check']['state'], exit_code=code),
            process_checks=checks, clock=clock, files=seal.file_hashes(run), errors=errors,
            host_started_monotonic_ns=started, host_completed_monotonic_ns=time.monotonic_ns())
        result = seal.seal_run(run, receipt_value)
        return dict(result=result, stop_required=(after['runner_check']['state'] != 'gone'
                    or any(c['state'] not in ('gone', 'not_started') or c['remaining_pids'] or c['overflow'] for c in checks)))

    def stop_vm(self):
        require(self.vm is not None, 'VM was not verified')
        self.vm.stop()

    def close(self):
        for stream in self.streams:
            stream.close()
        if self.vm:
            self.vm.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    config = validate_config(json.loads(args.config.read_bytes()))
    return supervise(SSHBackend(config), config, args.output)['exit_code']


if __name__ == '__main__':
    sys.exit(main())
