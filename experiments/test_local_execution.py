"""Local identity rejection tests; no subprocess, clock write or VM is used."""
import copy
import hashlib
import importlib.util
import os
from pathlib import Path
import stat
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('local_execution_tested',
                                            Path(__file__).parent/'product/local_execution.py')
local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local)
UUID = '6930ee66-3092-4f01-9039-71b6c89aad46'
BOOT = '64b6f7b7-9d45-43a8-a892-aac8c9fdae5a'


def attestation():
    return dict(schema=local.ATTESTATION_SCHEMA, run_id='local-'+UUID, source_sha='a'*40,
                boot_id=BOOT, vm_uuid=UUID, vm_serial='loom-oi-clock-'+UUID,
                runner_uid=1000, runner_gid=1000, network_mode='isolated', host_shares=False,
                **{k: 'b'*64 for k in local.HASH_KEYS})


def environment():
    return dict(OI_EXECUTION_PROFILE=local.PROFILE, OI_LOCAL_RUN_ID='local-'+UUID,
                OI_LOCAL_SOURCE_SHA='a'*40, OI_IT_CLOCK='1')


def facts():
    return dict(vm_uuid=UUID, vm_serial='loom-oi-clock-'+UUID, boot_id=BOOT,
                sys_vendor='Loom-OI-TaskLocal', product_name='Ubuntu24-Clock-Preparation',
                virtualization='qemu')


class LocalExecutionTests(unittest.TestCase):
    def test_exact_environment_and_canonical_digest(self):
        value = local.validate_environment(environment(), attestation(), facts(), 1000, 1000)
        self.assertEqual(value, attestation())
        self.assertEqual(local.digest(value), hashlib.sha256(local.canonical(value)).hexdigest())
        identity = local.identity(value, 'c'*64)
        self.assertEqual(local.validate_identity(identity), identity)
        self.assertEqual(identity['nonce_sha256'], hashlib.sha256(('c'*64).encode()).hexdigest())
        self.assertNotIn('workflow_ref', identity)

    def test_no_implicit_local_fallback(self):
        self.assertFalse(local.selected({}))
        for value in (None, '', 'github-hosted', False, 1, 'local', 'local-isolated-vm-v2'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                local.selected({'OI_EXECUTION_PROFILE': value})
        for key in local.ENV_KEYS[1:]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                local.selected({key: 'value'})

    def test_actual_vm_and_runner_must_match(self):
        changes = {'vm_uuid': '0'*36, 'vm_serial': 'production', 'boot_id': UUID,
                   'sys_vendor': 'QEMU', 'product_name': 'another-VM', 'virtualization': 'none'}
        for key, value in changes.items():
            with self.subTest(key=key), self.assertRaises(ValueError):
                local.validate_environment(environment(), attestation(), dict(facts(), **{key: value}), 1000, 1000)
        for uid, gid in ((0, 1000), (1000, 0), (1001, 1000), (1000, 1001), (True, 1000)):
            with self.subTest(uid=uid, gid=gid), self.assertRaises(ValueError):
                local.validate_environment(environment(), attestation(), facts(), uid, gid)

    def test_local_environment_cannot_impersonate_hosted(self):
        for key in ('GITHUB_ACTIONS', 'GITHUB_SHA', 'GITHUB_WORKFLOW_REF', 'RUNNER_ENVIRONMENT'):
            with self.subTest(key=key), self.assertRaises(ValueError):
                local.validate_environment(dict(environment(), **{key: ''}), attestation(), facts(), 1000, 1000)
        for key in ('OI_LOCAL_RUN_ID', 'OI_LOCAL_SOURCE_SHA', 'OI_IT_CLOCK'):
            with self.subTest(key=key), self.assertRaises(ValueError):
                local.validate_environment(dict(environment(), **{key: 'wrong'}), attestation(), facts(), 1000, 1000)

    def test_ordinary_scope_does_not_enable_clock(self):
        env = environment(); env.pop('OI_IT_CLOCK')
        self.assertEqual(local.validate_environment(env, attestation(), facts(), 1000, 1000,
                                                    clock_scope=False), attestation())
        with self.assertRaises(ValueError):
            local.validate_environment(env, attestation(), facts(), 1000, 1000)

    def test_attestation_inventory_types_and_isolation(self):
        changes = {'schema': 'other', 'run_id': '123', 'source_sha': 'a'*39,
                   'boot_id': UUID.upper(), 'vm_uuid': '', 'vm_serial': UUID,
                   'runner_uid': True, 'runner_gid': 0, 'network_mode': 'nat', 'host_shares': 0,
                   **{k: 'z'*64 for k in local.HASH_KEYS}}
        for key, value in changes.items():
            with self.subTest(key=key), self.assertRaises(ValueError):
                local.validate_attestation(dict(attestation(), **{key: value}))
        for key in local.ATTESTATION_KEYS:
            value = attestation(); del value[key]
            with self.subTest(missing=key), self.assertRaises(ValueError):
                local.validate_attestation(value)
        with self.assertRaises(ValueError):
            local.validate_attestation(dict(attestation(), unexpected=True))

    def test_root_path_permissions_and_file_type(self):
        local._trusted(SimpleNamespace(st_mode=stat.S_IFREG | 0o644, st_uid=0))
        local._trusted(SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=0), True)
        for kind in (stat.S_IFLNK, stat.S_IFIFO, stat.S_IFDIR):
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                local._trusted(SimpleNamespace(st_mode=kind | 0o644, st_uid=0))
        for mode, uid in ((0o666, 0), (0o664, 0), (0o644, 1000)):
            with self.subTest(mode=mode, uid=uid), self.assertRaises(ValueError):
                local._trusted(SimpleNamespace(st_mode=stat.S_IFREG | mode, st_uid=uid))
        with self.assertRaises(ValueError):
            local._trusted(SimpleNamespace(st_mode=stat.S_IFDIR | 0o777, st_uid=0), True)

    def test_local_identity_never_accepts_mixed_or_missing_keys(self):
        value = local.identity(attestation(), 'c'*64)
        for key in local.IDENTITY_KEYS:
            changed = copy.deepcopy(value); del changed[key]
            with self.subTest(missing=key), self.assertRaises(ValueError):
                local.validate_identity(changed)
        for key, bad in (('schema', 'g2-clock-guard-v1'), ('execution_profile', 'github-hosted'),
                         ('workflow_ref', ''), ('job', 'unit'), ('attestation_sha256', 'g'*64)):
            with self.subTest(key=key), self.assertRaises(ValueError):
                local.validate_identity(dict(value, **{key: bad}))

    def test_non_linux_path_rejected_before_open(self):
        with mock.patch.object(local.sys, 'platform', 'win32'), mock.patch.object(local.os, 'open') as opened:
            with self.assertRaises(ValueError):
                local.read_attestation('/etc/loom-oi/local-vm.json')
            opened.assert_not_called()

    @unittest.skipUnless(sys.platform == 'linux' and os.geteuid() == 0,
                         'scope: secure path syscall checks need the isolated Linux guest root')
    def test_real_secure_open_rejects_symlinks_writable_paths_and_fifo(self):
        # /run has root-owned parents; /tmp is deliberately rejected by policy.
        with tempfile.TemporaryDirectory(prefix='loom-oi-profile-', dir='/run') as tmp:
            root = Path(tmp)
            path = root/'identity.json'
            path.write_bytes(local.canonical(attestation()))
            self.assertEqual(local.read_attestation(path), attestation())
            link = root/'link.json'; link.symlink_to(path)
            with self.assertRaises((OSError, ValueError)):
                local.read_attestation(link)
            os.mkfifo(root/'fifo', 0o600)
            with self.assertRaises(ValueError):
                local.read_attestation(root/'fifo')
            path.chmod(0o664)
            with self.assertRaises(ValueError):
                local.read_attestation(path)
            path.chmod(0o644)
            root.chmod(0o775)
            with self.assertRaises(ValueError):
                local.read_attestation(path)
            root.chmod(0o700)
            os.chown(path, 1000, 1000)
            with self.assertRaises(ValueError):
                local.read_attestation(path)
            os.chown(path, 0, 0)
            path.write_bytes(local.canonical(attestation()).rstrip())
            with self.assertRaises(ValueError):
                local.read_attestation(path)


if __name__ == '__main__':
    unittest.main()
