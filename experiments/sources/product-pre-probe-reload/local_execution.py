"""Explicit local VM provenance; this module never changes a clock.

The image/launch digests are declarations measured by the trusted host launcher,
not measurements of a backing image made from inside the guest.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

PROFILE = 'local-isolated-vm-v1'
ATTESTATION_SCHEMA = 'loom-oi-local-vm-v1'
ENV_KEYS = ('OI_EXECUTION_PROFILE', 'OI_LOCAL_ATTESTATION', 'OI_LOCAL_RUN_ID',
            'OI_LOCAL_SOURCE_SHA')
HASH_KEYS = ('base_image_sha256', 'qemu_binary_sha256', 'launch_sha256', 'host_nonce_sha256')
ATTESTATION_KEYS = {'schema', 'run_id', 'source_sha', 'boot_id', 'vm_uuid', 'vm_serial',
                    'runner_uid', 'runner_gid', 'network_mode', 'host_shares', *HASH_KEYS}
IDENTITY_KEYS = ('schema', 'run_id', 'job', 'source_sha', 'nonce_sha256', 'boot_id',
                 'execution_profile', 'attestation_sha256', 'vm_uuid', 'vm_serial', *HASH_KEYS)
UUID_RE = r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'


def require(ok, message):
    if not ok:
        raise ValueError(message)


def matches(pattern, value):
    return isinstance(value, str) and re.fullmatch(pattern, value) is not None


def canonical(value):
    return (json.dumps(value, sort_keys=True, indent=2)+'\n').encode('utf-8')


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def selected(env):
    value = env.get('OI_EXECUTION_PROFILE')
    require('OI_EXECUTION_PROFILE' not in env or value == PROFILE, 'unknown explicit execution profile')
    if 'OI_EXECUTION_PROFILE' not in env:
        require(not any(key.startswith('OI_LOCAL_') for key in env), 'local identity without explicit profile')
    return value == PROFILE


def validate_attestation(value):
    require(isinstance(value, dict) and set(value) == ATTESTATION_KEYS,
            'local attestation field inventory mismatch')
    require(value['schema'] == ATTESTATION_SCHEMA, 'unknown local attestation schema')
    require(matches('local-'+UUID_RE, value['run_id']), 'invalid local run identity')
    require(matches('[0-9a-f]{40}', value['source_sha']), 'invalid local source commit')
    for key in ('boot_id', 'vm_uuid'):
        require(matches(UUID_RE, value[key]), 'invalid local '+key)
    require(value['vm_serial'] == 'loom-oi-clock-'+value['vm_uuid'], 'wrong dedicated VM serial')
    for key in HASH_KEYS:
        require(matches('[0-9a-f]{64}', value[key]), 'invalid local '+key)
    require(all(type(value[k]) is int and value[k] > 0 for k in ('runner_uid', 'runner_gid')),
            'local runner must be ordinary user')
    require(value['network_mode'] == 'isolated' and value['host_shares'] is False,
            'local VM is not isolated')
    return value


def _trusted(st, directory=False):
    kind = stat.S_ISDIR(st.st_mode) if directory else stat.S_ISREG(st.st_mode)
    require(kind and st.st_uid == 0 and st.st_mode & 0o022 == 0,
            'attestation path must be root owned and not group/other writable')


def read_root_json(path):
    """Open each path component relative to an already checked directory fd."""
    path = Path(path)
    require(sys.platform == 'linux' and path.is_absolute() and '..' not in path.parts,
            'attestation requires an absolute Linux path')
    flags = os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW
    parent = os.open('/', flags | os.O_DIRECTORY)
    try:
        _trusted(os.fstat(parent), True)
        for name in path.parts[1:-1]:
            child = os.open(name, flags | os.O_DIRECTORY, dir_fd=parent)
            os.close(parent)
            parent = child
            _trusted(os.fstat(parent), True)
        fd = os.open(path.name, flags | os.O_NONBLOCK, dir_fd=parent)
        try:
            _trusted(os.fstat(fd))
            require(os.fstat(fd).st_size <= 8192, 'unbounded local attestation')
            with os.fdopen(fd, 'rb', closefd=False) as stream:
                data = stream.read(8193)
            require(len(data) <= 8192, 'unbounded local attestation')
        finally:
            os.close(fd)
    finally:
        os.close(parent)
    value = json.loads(data)
    require(data == canonical(value), 'attestation must use exact canonical bytes')
    return value


def read_attestation(path):
    return validate_attestation(read_root_json(path))


def platform_facts():
    require(sys.platform == 'linux', 'local execution requires Linux')
    def read(name):
        path = '/sys/class/dmi/id/'+name
        try:
            return Path(path).read_text().strip()
        except PermissionError:
            # Ordinary preparation can inspect these fixed read-only paths;
            # the privileged guard repeats the check without sudo.
            return subprocess.check_output(['sudo', '-n', 'cat', path], text=True, timeout=10).strip()
    return dict(vm_uuid=read('product_uuid').lower(), vm_serial=read('product_serial'),
                sys_vendor=read('sys_vendor'), product_name=read('product_name'),
                virtualization=subprocess.check_output(['systemd-detect-virt', '--vm'],
                                                       text=True, timeout=10).strip(),
                boot_id=Path('/proc/sys/kernel/random/boot_id').read_text().strip())


def validate_environment(env, attestation, facts, uid, gid, clock_scope=True):
    require(selected(env), 'explicit local profile is required')
    if clock_scope:
        require(env.get('OI_IT_CLOCK') == '1', 'local clock scope is not enabled')
    require(not any(key.startswith('GITHUB_') or key == 'RUNNER_ENVIRONMENT' for key in env),
            'local execution must not impersonate a hosted workflow')
    value = validate_attestation(attestation)
    require(value['run_id'] == env.get('OI_LOCAL_RUN_ID')
            and value['source_sha'] == env.get('OI_LOCAL_SOURCE_SHA'), 'local run/source mismatch')
    require(type(uid) is int and type(gid) is int and uid == value['runner_uid']
            and gid == value['runner_gid'], 'local runner identity mismatch')
    require(facts.get('sys_vendor') == 'Loom-OI-TaskLocal'
            and facts.get('product_name') == 'Ubuntu24-Clock-Preparation'
            and facts.get('virtualization') == 'qemu',
            'local execution requires the dedicated QEMU guest')
    require(all(facts.get(k) == value[k] for k in ('vm_uuid', 'vm_serial', 'boot_id')),
            'actual VM/boot differs from local attestation')
    return value


def environment(env, uid=None, gid=None, clock_scope=True):
    value = read_attestation(env.get('OI_LOCAL_ATTESTATION', ''))
    uid = (int(env.get('SUDO_UID', '0')) if os.getuid() == 0 else os.getuid()) if uid is None else uid
    gid = (int(env.get('SUDO_GID', '0')) if os.getgid() == 0 else os.getgid()) if gid is None else gid
    return validate_environment(env, value, platform_facts(), uid, gid, clock_scope=clock_scope)


def identity(value, nonce):
    validate_attestation(value)
    require(matches('[0-9a-f]{64}', nonce), 'invalid private guard nonce')
    return dict(schema='g2-clock-guard-v2', run_id=value['run_id'], job='clock',
                source_sha=value['source_sha'], nonce_sha256=hashlib.sha256(nonce.encode()).hexdigest(),
                boot_id=value['boot_id'], execution_profile=PROFILE, attestation_sha256=digest(value),
                **{k: value[k] for k in ('vm_uuid', 'vm_serial', *HASH_KEYS)})


def validate_identity(value):
    require(isinstance(value, dict) and value.get('schema') == 'g2-clock-guard-v2'
            and value.get('execution_profile') == PROFILE and value.get('job') == 'clock'
            and 'workflow_ref' not in value, 'invalid local guard identity schema/profile')
    require(matches('local-'+UUID_RE, value.get('run_id'))
            and matches('[0-9a-f]{40}', value.get('source_sha')), 'invalid local run/source')
    require(all(matches('[0-9a-f]{64}', value.get(k)) for k in
                ('nonce_sha256', 'attestation_sha256', *HASH_KEYS)), 'invalid local digest')
    require(all(matches(UUID_RE, value.get(k)) for k in ('vm_uuid', 'boot_id'))
            and value.get('vm_serial') == 'loom-oi-clock-'+value['vm_uuid'], 'invalid local VM identity')
    return {k: value[k] for k in IDENTITY_KEYS}
