"""Host supervision failure tests; no real VM, clock, sudo or product is run."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import subprocess
from types import SimpleNamespace
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location('local_supervisor_tested',
                                             Path(__file__).parent/'product/local_supervisor.py')
s = importlib.util.module_from_spec(spec)
spec.loader.exec_module(s)
UUID = '6930ee66-3092-4f01-9039-71b6c89aad46'
BOOT = '64b6f7b7-9d45-43a8-a892-aac8c9fdae5a'


def config():
    a = dict(schema=s.local.ATTESTATION_SCHEMA, run_id='local-'+UUID, source_sha='a'*40,
             boot_id=BOOT, vm_uuid=UUID, vm_serial='loom-oi-clock-'+UUID, runner_uid=1000,
             runner_gid=1000, network_mode='isolated', host_shares=False,
             **{k: 'b'*64 for k in s.local.HASH_KEYS})
    a['host_nonce_sha256'] = s.sha(UUID.encode())
    return dict(schema='loom-oi-local-supervisor-config-v1', attestation=a,
                guest_root='/home/oi-vm/.local/task007',
                guest_repository='/home/oi-vm/.local/task007/source/'+'a'*40,
                guest_run='/home/oi-vm/.local/task007/runs/local-'+UUID,
                guest_environment_file='/home/oi-vm/.local/task007/env.sh',
                guest_attestation='/var/lib/loom-oi/attestations/local-'+UUID+'.json',
                manifest='experiments/manifests/G2-LOCAL.json', timeout_seconds=60,
                vm=dict(pid=123, creation_filetime=999, launch_nonce=UUID, name='loom-oi-clock-'+UUID,
                        vm_uuid=UUID, vm_serial='loom-oi-clock-'+UUID,
                        launch_sha256='b'*64, qemu_binary_sha256='b'*64),
                ssh=dict(port=22761, user='oi-vm'))


def evidence():
    c = config()
    identity = s.local.identity(c['attestation'], 'c'*64)
    files = {name: s.sha(name.encode()) for name in s.FILES}
    request = dict(schema='loom-oi-local-supervision-request-v1',
                   **{k: identity[k] for k in s.IDENTITY}, wrapper_exit_code=0, files=files)
    recovery = dict(schema='g2-local-recovery-v1', identity=identity, recovery_exit_code=0,
                    errors=[], files=files, exit_code=0, test_group_closed=True)
    return c, request, recovery


class FakeBackend:
    def __init__(self):
        self.config, self.req, self.result = evidence()
        self.calls = []
        self.published = None
        self.process = mock.Mock()
        self.process.poll.return_value = 0
        self.request_value = s.local.canonical(self.req)
        self.failures = {}

    def action(self, name):
        self.calls.append(name)
        if name in self.failures:
            raise self.failures[name]

    def verify(self): self.action('verify')
    def start(self, output): self.action('start'); return self.process
    def request(self): self.action('request'); return self.request_value
    def collect(self, output, names): self.action(Path(output).name); return dict(self.req['files'])
    def recover(self, output, final): self.action('final' if final else 'recover'); return copy.deepcopy(self.result)
    def publish(self, value): self.action('publish'); self.published = value
    def preserve(self, output): self.action('preserve')
    def stop_vm(self): self.action('stop_vm')
    def close(self): self.action('close')


class LocalSupervisorIdentityTests(unittest.TestCase):
    def test_exact_request_recovery_and_receipt_match_shared_collector(self):
        c, request, recovered = evidence()
        self.assertEqual(s.validate_config(c), c)
        self.assertEqual(s.validate_request(request, c['attestation']), request)
        self.assertEqual(s.validate_recovery(recovered, c['attestation'], request), recovered)
        value = s.receipt(request, recovered, 1, 2)
        analysis = s.sibling('analyze')
        self.assertEqual(analysis._local_receipt(value, request), value)
        self.assertEqual(analysis._local_supervision_request(recovered['identity'], 0, request['files']), request)

    def test_every_request_identity_and_original_file_is_bound(self):
        c, request, _ = evidence()
        for key in s.IDENTITY:
            with self.subTest(key=key):
                bad = dict(request, **{key: 'x'})
                with self.assertRaises(ValueError): s.validate_request(bad, c['attestation'])
        for mutation in (dict(request, wrapper_exit_code=True), dict(request, files={}),
                         dict(request, extra=True), dict(request, schema='hosted')):
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                s.validate_request(mutation, c['attestation'])

    def test_recovery_cannot_replace_request_or_fabricate_success(self):
        c, request, recovered = evidence()
        for key, value in [('test_group_closed', False), ('test_group_closed', 1), ('exit_code', True),
                           ('recovery_exit_code', 1), ('errors', ['orphan']), ('files', {}), ('schema', 'other')]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                s.validate_recovery(dict(recovered, **{key: value}), c['attestation'], request)
        for key in s.local.IDENTITY_KEYS:
            if key == 'schema': continue
            bad = copy.deepcopy(recovered)
            bad['identity'][key] = 'changed'
            with self.subTest(identity=key), self.assertRaises((ValueError, KeyError)):
                s.validate_recovery(bad, c['attestation'], request)

    def test_config_rejects_guest_path_escape_and_implicit_source(self):
        for key, value in [('guest_repository', '/home/oi-vm/source'), ('guest_run', '/tmp/run'),
                           ('guest_environment_file', '/home/oi-vm/.local/task007/../secret'),
                           ('manifest', 'experiments/manifests/../evil.json'),
                           ('guest_attestation', '/tmp/attestation'), ('timeout_seconds', True)]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                s.validate_config(dict(config(), **{key: value}))
        bad = config(); bad['attestation']['host_nonce_sha256'] = 'd'*64
        with self.assertRaisesRegex(ValueError, 'actual QEMU launch'): s.validate_config(bad)

    def test_launch_exact_network_no_shares_and_real_start_nonce(self):
        with tempfile.TemporaryDirectory() as tmp:
            expected = dict(config()['vm'], task_directory=tmp, executable=str(Path(tmp)/'qemu.exe'))
            args = ['-name', expected['name'], '-uuid', UUID, '-display', 'none', '-monitor', 'none',
                    '-accel', 'tcg,thread=multi', '-netdev', 'user,id=tasknet,restrict=on,hostfwd=tcp:127.0.0.1:22761-:22',
                    '-device', 'virtio-net-pci,netdev=tasknet,mac=52:54:00:7d:00:01',
                    '-smbios', 'type=1,manufacturer=Loom-OI-TaskLocal,product=Ubuntu24-Clock-Preparation,serial='+expected['vm_serial'],
                    '-serial', 'file:'+str(Path(tmp)/'serial.log'), '-L', str(Path(tmp)/'share'),
                    '-pidfile', str(Path(tmp)/'qemu.pid'),
                    '-drive', 'file='+str(Path(tmp)/'disk.qcow2')+',if=virtio,format=qcow2',
                    '-drive', 'file='+str(Path(tmp)/'seed.iso')+',if=virtio,format=raw,readonly=on', '-no-reboot']
            launch = dict(executable=expected['executable'], args=args)
            self.assertEqual(s.validate_launch(launch, expected, 22761), args)
            mutations = [args+['-virtfs', 'local,path=C:/'], args+['-netdev', 'user,id=extra'],
                         [x.replace('restrict=on', 'restrict=off') for x in args],
                         [x.replace('127.0.0.1', '0.0.0.0') for x in args],
                         [x.replace('file:'+str(Path(tmp)/'serial.log'), 'tcp:0.0.0.0:3333,server') for x in args],
                         [x.replace(expected['name'], 'other') for x in args],
                         args[:-1]+['-drive', 'file=/foreign/image,if=virtio,format=raw']]
            for i, changed in enumerate(mutations):
                with self.subTest(i=i), self.assertRaises(ValueError):
                    s.validate_launch(dict(launch, args=changed), expected, 22761)


class LocalSupervisorLifecycleTests(unittest.TestCase):
    def run_fake(self, backend, **kwargs):
        with tempfile.TemporaryDirectory() as tmp:
            ticks = iter(range(1, 1000))
            result = s.supervise(backend, backend.config, Path(tmp)/'result', monotonic=lambda: next(ticks),
                                 sleep=lambda _: None, **kwargs)
            disk = json.loads((Path(tmp)/'result/host-result.json').read_bytes())
            self.assertEqual(result, disk)
            return result

    def test_success_requires_two_independent_recoveries_and_original_hashes(self):
        b = FakeBackend(); result = self.run_fake(b)
        self.assertEqual(result['exit_code'], 0)
        self.assertEqual(b.calls, ['verify', 'start', 'request', 'clock-before', 'recover', 'clock-after',
                                  'publish', 'final', 'clock-final', 'preserve', 'close'])
        self.assertEqual(b.published['files'], b.req['files'])
        self.assertNotIn('summary.json', b.published['files'])

    def test_preexisting_or_failed_runner_still_recovers_and_preserves(self):
        for code in (1, -9):
            with self.subTest(code=code):
                b = FakeBackend(); b.process.poll.return_value = code
                result = self.run_fake(b)
                self.assertEqual(result['exit_code'], 1)
                self.assertIn('final', b.calls); self.assertIn('preserve', b.calls)

    def test_failed_clock_wrapper_is_not_hidden_by_successful_cleanup_or_runner(self):
        b = FakeBackend(); b.req['wrapper_exit_code'] = 1
        b.request_value = s.local.canonical(b.req)
        result = self.run_fake(b)
        self.assertEqual(result['exit_code'], 1)
        self.assertEqual(b.published['wrapper_exit_code'], 1)
        self.assertIn('final', b.calls)

    def test_absent_request_or_early_exit_never_gets_a_passing_receipt(self):
        b = FakeBackend(); b.request_value = None
        result = self.run_fake(b)
        self.assertEqual(result['exit_code'], 1)
        self.assertIsNone(b.published)
        self.assertIn('final', b.calls)

    def test_request_mismatch_preserved_and_recovery_still_attempted(self):
        b = FakeBackend(); b.request_value = s.local.canonical(dict(b.req, boot_id=UUID))
        result = self.run_fake(b)
        self.assertEqual(result['exit_code'], 1)
        self.assertIsNone(b.published); self.assertIn('final', b.calls)

    def test_each_request_phase_failure_runs_final_cleanup_without_passing(self):
        for name in ('start', 'request', 'clock-before', 'recover', 'clock-after', 'publish'):
            with self.subTest(name=name):
                b = FakeBackend(); b.failures[name] = RuntimeError('forced '+name)
                result = self.run_fake(b)
                self.assertEqual(result['exit_code'], 1)
                self.assertIn('final', b.calls); self.assertIn('preserve', b.calls)
                self.assertEqual(b.calls[-1], 'close')

    def test_final_recovery_failure_preserves_before_stopping_verified_vm(self):
        for name in ('final', 'clock-final'):
            with self.subTest(name=name):
                b = FakeBackend(); b.failures[name] = RuntimeError('cannot confirm')
                result = self.run_fake(b)
                self.assertEqual(result['exit_code'], 1)
                self.assertLess(b.calls.index('preserve'), b.calls.index('stop_vm'))

    def test_identity_rejection_never_stops_an_unverified_process(self):
        b = FakeBackend(); b.failures['verify'] = RuntimeError('PID reused')
        result = self.run_fake(b)
        self.assertEqual(result['exit_code'], 1)
        self.assertEqual(b.calls, ['verify', 'close'])

    def test_preservation_close_and_stop_errors_are_not_hidden(self):
        b = FakeBackend(); b.failures = {k: RuntimeError(k) for k in ('final', 'preserve', 'stop_vm', 'close')}
        result = self.run_fake(b)
        self.assertEqual(result['exit_code'], 1)
        self.assertEqual(len(result['errors']), 4)

    def test_deadline_terminates_only_owned_transport_and_verified_vm(self):
        b = FakeBackend(); b.request_value = None; b.process.poll.return_value = None
        with tempfile.TemporaryDirectory() as tmp:
            ticks = iter((1, 61_000_000_002, 61_000_000_003))
            result = s.supervise(b, b.config, Path(tmp)/'out', monotonic=lambda: next(ticks), sleep=lambda _: None)
        self.assertEqual(result['exit_code'], 1)
        self.assertIn('stop_vm', b.calls); b.process.terminate.assert_called_once()

    def test_new_output_is_exclusive(self):
        b = FakeBackend()
        with tempfile.TemporaryDirectory() as tmp, self.assertRaises(FileExistsError):
            s.supervise(b, b.config, tmp)
        self.assertEqual(b.calls, [])


class LocalSupervisorTransportTests(unittest.TestCase):
    def backend(self, tmp):
        c = config()
        key, known = Path(tmp)/'key', Path(tmp)/'known'
        key.write_text('not-a-real-private-key')
        known.write_text('not-a-real-host-key')
        c['ssh'].update(private_key=str(key.resolve()), known_hosts=str(known.resolve()))
        return s.SSHBackend(c)

    def test_only_fixed_loopback_pinned_ssh_and_source_paths_are_used(self):
        with tempfile.TemporaryDirectory() as tmp:
            b = self.backend(tmp)
            self.assertEqual(b.ssh[-1], 'oi-vm@127.0.0.1')
            self.assertIn('StrictHostKeyChecking=yes', b.ssh)
            self.assertIn('ForwardAgent=no', b.ssh)
            self.assertIn('IdentityAgent=none', b.ssh)
            command = b.command(['experiments/product/clock_run.py', '--work', b.work, '--recover-only'], clock=True)
            self.assertIn('OI_IT_CLOCK=1', command)
            self.assertIn('OI_LOCAL_SOURCE_SHA='+'a'*40, command)
            self.assertNotIn('GITHUB_', command)
            self.assertIn('exec env ', command)

    def test_started_command_reaches_real_product_parser_with_all_fixed_arguments(self):
        # Exercise the real entry point's argparse contract, then stop at its
        # first manifest interpretation. No Go, SSH, VM or product is launched.
        class ManifestReached(Exception):
            pass
        with tempfile.TemporaryDirectory() as tmp:
            b = self.backend(tmp)
            b.verified_facts = {'test': 'no real VM'}
            output = Path(tmp)/'output'; output.mkdir()
            with mock.patch.object(b, 'command', return_value='captured') as command, \
                    mock.patch.object(s.subprocess, 'Popen') as popen:
                b.start(output)
            try:
                args = command.call_args.args[0]
                self.assertEqual(args, ['experiments/product/run.py', b.config['manifest'], '--work',
                    b.config['guest_run']+'/work', '--output-root', b.config['guest_run']+'/out'])
                self.assertEqual(popen.call_args.args[0][-1], 'captured')
                entry_path = Path(__file__).parent.parent/args[0]
                entry = s.types.ModuleType('actual_product_entry')
                entry.__file__ = str(entry_path.resolve())
                exec(compile(entry_path.read_bytes(), str(entry_path), 'exec'), entry.__dict__)
                manifest = Path(tmp)/'manifest.json'
                manifest.write_text('{"case":{}}', encoding='utf-8')
                argv = [str(entry_path), str(manifest), *args[2:]]
                with mock.patch.object(entry.sys, 'argv', argv), \
                        mock.patch.object(entry._analyzer, '_local_case', side_effect=ManifestReached):
                    with self.assertRaises(ManifestReached):
                        entry.main()
            finally:
                b.close()

    def test_recovery_preserves_two_distinct_original_records_and_covers_guest_budget(self):
        _, _, result = evidence()
        raw = s.local.canonical(result)
        with tempfile.TemporaryDirectory() as tmp:
            b = self.backend(tmp)
            with mock.patch.object(s.subprocess, 'run', return_value=SimpleNamespace(returncode=0,stdout=b'output',stderr=b'')) as run, \
                    mock.patch.object(b, 'remote', return_value=SimpleNamespace(stdout=raw)):
                for final in (False, True):
                    self.assertEqual(b.recover(Path(tmp), final), result)
                self.assertEqual(run.call_count, 2)
                self.assertTrue(all(c.kwargs['timeout'] >= 90 for c in run.call_args_list))
                self.assertIn('external-final-recovery.json', run.call_args.args[0][-1])
            self.assertEqual((Path(tmp)/'external-recovery.json').read_bytes(), raw)
            self.assertEqual((Path(tmp)/'external-final-recovery.json').read_bytes(), raw)

    def test_lost_transport_does_not_launch_parallel_second_recovery(self):
        with tempfile.TemporaryDirectory() as tmp:
            b = self.backend(tmp)
            failure = subprocess.TimeoutExpired('ssh', 240, output=b'partial', stderr=b'interrupted')
            with mock.patch.object(s.subprocess, 'run', side_effect=failure) as run:
                with self.assertRaises(subprocess.TimeoutExpired): b.recover(Path(tmp), False)
                with self.assertRaisesRegex(ValueError, 'parallel'): b.recover(Path(tmp), True)
                self.assertEqual(run.call_count, 1)
            self.assertEqual((Path(tmp)/'external-recovery.json.stdout').read_bytes(), b'partial')

    def test_result_transport_mismatch_cannot_claim_recovery(self):
        _, _, value = evidence()
        with tempfile.TemporaryDirectory() as tmp:
            b = self.backend(tmp)
            with mock.patch.object(s.subprocess, 'run', return_value=SimpleNamespace(returncode=255,stdout=b'',stderr=b'lost')), \
                    mock.patch.object(b, 'remote', return_value=SimpleNamespace(stdout=s.local.canonical(value))):
                with self.assertRaisesRegex(ValueError, 'exit mismatch'): b.recover(Path(tmp), False)
            self.assertTrue(b.recovery_unconfirmed)

    def test_root_receipt_is_published_atomically_without_overwrite_and_read_back(self):
        _, request, recovered = evidence()
        value = s.receipt(request, recovered, 1, 2)
        with tempfile.TemporaryDirectory() as tmp:
            b = self.backend(tmp)
            with mock.patch.object(b, 'remote', return_value=SimpleNamespace(stdout=s.local.canonical(value))) as remote:
                b.publish(value)
            argv = remote.call_args_list[0].args[0]
            self.assertEqual(argv[:4], ['sudo', '-n', 'python3', '-c'])
            self.assertIn('os.link(temporary,p', argv[4])
            self.assertIn('os.O_EXCL|os.O_NOFOLLOW', argv[4])
            self.assertEqual(remote.call_count, 2)
            with mock.patch.object(b, 'remote', return_value=SimpleNamespace(stdout=b'changed')):
                with self.assertRaisesRegex(ValueError, 'readback'): b.publish(value)


if __name__ == '__main__':
    unittest.main()
