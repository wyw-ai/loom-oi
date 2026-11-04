"""Clock wrapper tests: no sudo, service manager, real clock or Go process runs."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import py_compile
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('clock_run_tested', Path(__file__).parent/'product/clock_run.py')
clock = importlib.util.module_from_spec(spec)
spec.loader.exec_module(clock)

ENV = dict(OI_IT='1', OI_IT_CLOCK='1', OI_IT_BACKENDS='sqlite,postgres', OI_TEST_PG_DSN='test-only',
           GITHUB_SHA='a'*40, GITHUB_RUN_ID='123', RUNNER_ENVIRONMENT='github-hosted',
           GITHUB_WORKFLOW_REF='wyw-ai/loom-oi/.github/workflows/g2-evidence.yml@refs/heads/evidence/G2-test')


class ClockRunTests(unittest.TestCase):
    def test_guard_loader_executes_hashed_source_not_same_timestamp_bytecode(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); path = root/'clock_guard.py'
            path.write_bytes(b'MARK = "old"\n')
            original = path.stat()
            py_compile.compile(str(path), doraise=True)
            path.write_bytes(b'MARK = "new"\n')
            os.utime(path, ns=(original.st_atime_ns, original.st_mtime_ns))
            stale_spec = importlib.util.spec_from_file_location('stale_guard_proof', path)
            stale = importlib.util.module_from_spec(stale_spec); stale_spec.loader.exec_module(stale)
            self.assertEqual(stale.MARK, 'old')
            with mock.patch.object(clock, 'HERE', root):
                self.assertEqual(clock.load_guard().MARK, 'new')

    def test_local_preparation_uses_attestation_without_hosted_identifiers(self):
        local = clock.load_guard().local_module()
        uuid = '6930ee66-3092-4f01-9039-71b6c89aad46'
        attestation = dict(schema=local.ATTESTATION_SCHEMA, run_id='local-'+uuid,
                           source_sha='a'*40, boot_id='64b6f7b7-9d45-43a8-a892-aac8c9fdae5a',
                           vm_uuid=uuid, vm_serial='loom-oi-clock-'+uuid, runner_uid=1000,
                           runner_gid=1000, network_mode='isolated', host_shares=False,
                           **{k: 'c'*64 for k in local.HASH_KEYS})
        env = dict(OI_IT='1', OI_IT_CLOCK='1', OI_IT_BACKENDS='sqlite,postgres',
                   OI_TEST_PG_DSN='test-only', OI_EXECUTION_PROFILE=local.PROFILE,
                   OI_LOCAL_SOURCE_SHA='a'*40, OI_LOCAL_RUN_ID='local-'+uuid)
        api = SimpleNamespace(execution_environment=lambda env: attestation, local_module=lambda: local)
        with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
            stack.enter_context(mock.patch.dict(clock.os.environ, env, clear=True))
            for name in ('getuid', 'geteuid', 'getgid'):
                stack.enter_context(mock.patch.object(clock.os, name, return_value=1000, create=True))
            stack.enter_context(mock.patch.object(clock, 'git', return_value='a'*40))
            stack.enter_context(mock.patch.object(clock, 'sources', return_value={'input.go': 'b'*64}))
            stack.enter_context(mock.patch.object(clock.shutil, 'which', return_value='/tools/go'))
            def output(argv, **kwargs):
                if argv[1] == 'env':
                    return json.dumps(dict(GOOS='linux', GOARCH='amd64', GOTOOLCHAIN='local', GOFLAGS=''))
                return 'go version go1.27.3 linux/amd64'
            stack.enter_context(mock.patch.object(clock.subprocess, 'check_output', side_effect=output))
            def build(argv, **kwargs):
                self.assertNotIn('sudo', argv)
                Path(argv[argv.index('-o')+1]).write_bytes(b'binary')
            stack.enter_context(mock.patch.object(clock.subprocess, 'run', side_effect=build))
            _, nonce, prepared = clock.prepare(Path(tmp)/'clock', api)
            self.assertEqual(prepared['schema'], 'g2-clock-preparation-v2')
            self.assertEqual(prepared['attestation'], attestation)
            self.assertEqual(prepared['attestation_sha256'], local.digest(attestation))
            self.assertEqual(prepared['runner_environment'], local.PROFILE)
            self.assertEqual(prepared['nonce_sha256'], local.identity(attestation, nonce)['nonce_sha256'])
            self.assertNotIn('workflow_ref', prepared)
            self.assertEqual(len(prepared['binaries']), 4)

    def test_external_supervision_attempts_every_cleanup_and_preserves_failure(self):
        for failing in (None, 'orphan', 'recover', 'collect'):
            with self.subTest(failing=failing), tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
                work = Path(tmp)
                (work/'guard').mkdir()
                for name in ('raw.jsonl', 'guard/guard.jsonl', 'guard/state.json', 'guard/finished.json'):
                    (work/name).write_bytes(b'original\n')
                guard = SimpleNamespace(execution_environment=lambda env: {'source_sha': 'a'*40},
                                        nonce_from=lambda *args: 'b'*64,
                                        identity=lambda nonce: {'source_sha': 'a'*40})
                stack.enter_context(mock.patch.object(clock, 'load_guard', return_value=guard))
                stack.enter_context(mock.patch.object(clock.os, 'getuid', return_value=1000, create=True))
                calls = []
                def invoke(name):
                    def operation(*args):
                        calls.append(name)
                        if name == failing:
                            raise ValueError(name+' failed')
                        return 0
                    return operation
                for function, name in (('close_orphaned_test', 'orphan'), ('recover', 'recover'), ('collect', 'collect')):
                    stack.enter_context(mock.patch.object(clock, function, side_effect=invoke(name)))
                stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
                code = clock.recover_only(work)
                self.assertEqual(calls, ['orphan', 'recover', 'collect'])
                self.assertEqual(code, 0 if failing is None else 1)
                result = json.loads((work/'external-recovery.json').read_text())
                self.assertEqual(result['test_group_closed'], failing != 'orphan')
                self.assertEqual(len(result['files']), 4)
                self.assertEqual(bool(result['errors']), failing is not None)

    def test_local_orphan_reused_process_is_not_signaled(self):
        with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
            work = Path(tmp)
            identity = {'schema': 'g2-clock-guard-v2', 'source_sha': 'a'*40}
            row = dict(identity, action='child_started', child_pid=321, child_start_ticks=12345)
            (work/'guard.jsonl').write_text(json.dumps(row)+'\n')
            guard = SimpleNamespace(guard_directory=lambda nonce: work, identity=lambda nonce: identity,
                                    process_start_ticks=lambda pid: 54321)
            stack.enter_context(mock.patch.object(clock.os, 'getpgrp', return_value=100, create=True))
            signal_group = stack.enter_context(mock.patch.object(clock.os, 'killpg', create=True))
            with self.assertRaisesRegex(ValueError, 'reused'):
                clock.close_orphaned_test(guard, 'nonce')
            signal_group.assert_not_called()

    def test_prepare_is_unprivileged_before_elevation_and_retains_source(self):
        with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
            work = Path(tmp)/'clock'
            stack.enter_context(mock.patch.dict(clock.os.environ, ENV))
            for name in ('getuid', 'geteuid', 'getgid'):
                stack.enter_context(mock.patch.object(clock.os, name, return_value=1001, create=True))
            stack.enter_context(mock.patch.object(clock, 'git', return_value='a'*40))
            snapshots = {'internal/example.go': 'digest'}
            stack.enter_context(mock.patch.object(clock, 'sources', return_value=snapshots))
            stack.enter_context(mock.patch.object(clock.shutil, 'which', return_value='/tools/go'))
            def output(argv, **kwargs):
                if argv[1] == 'env':
                    return json.dumps(dict(GOOS='linux', GOARCH='amd64', GOTOOLCHAIN='local', GOFLAGS=''))
                return 'go version go1.27.3 linux/amd64'
            stack.enter_context(mock.patch.object(clock.subprocess, 'check_output', side_effect=output))
            commands = []
            def build(argv, **kwargs):
                commands.append((argv, kwargs))
                Path(argv[argv.index('-o')+1]).write_bytes(b'binary')
            stack.enter_context(mock.patch.object(clock.subprocess, 'run', side_effect=build))
            go, nonce, preparation = clock.prepare(work, SimpleNamespace(execution_environment=mock.Mock(return_value=None)))
            self.assertEqual(len(commands), 4)
            self.assertTrue(all(command[0][0] == go for command in commands))
            self.assertTrue(all('sudo' not in command[0] for command in commands))
            self.assertEqual(preparation['source_hashes'], snapshots)
            self.assertEqual(len(preparation['binaries']), 4)
            self.assertEqual(len(nonce), 64)
            self.assertEqual((work/'nonce').read_text().strip(), nonce)
            self.assertNotIn(nonce, (work/'preparation.json').read_text())
            self.assertEqual(preparation['runner_uid'], 1001)
            self.assertEqual(preparation['source_sha'], ENV['GITHUB_SHA'])

    def test_wrong_workflow_root_and_dirty_source_cannot_arm_guard(self):
        for case in ('workflow', 'root', 'dirty'):
            with self.subTest(case=case), tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as stack:
                stack.enter_context(mock.patch.dict(clock.os.environ, ENV))
                if case == 'workflow':
                    clock.os.environ['GITHUB_WORKFLOW_REF'] = 'wyw-ai/loom-oi/.github/workflows/ci.yml@refs/heads/main'
                for name in ('getuid', 'geteuid', 'getgid'):
                    stack.enter_context(mock.patch.object(clock.os, name, return_value=0 if case == 'root' else 1001, create=True))
                stack.enter_context(mock.patch.object(clock, 'git', return_value='a'*40))
                stack.enter_context(mock.patch.object(clock, 'sources', side_effect=ValueError('dirty')))
                process = stack.enter_context(mock.patch.object(clock.subprocess, 'run'))
                with self.assertRaises(ValueError):
                    clock.prepare(Path(tmp)/'clock', SimpleNamespace(execution_environment=mock.Mock(return_value=None)))
                process.assert_not_called()

    def run_fixture(self, *, code=0, recovery=0, verify_error=None, stop_error=None,
                    force_stop=False, orphan_error=None):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        work = Path(tmp.name)/'clock'
        work.mkdir()
        (work/'raw.jsonl').write_text('{"Action":"pass","Package":"test-package"}\n', encoding='utf-8')
        (work/'guard').mkdir()
        audit = b'{"action":"start","source_sha":"subject"}\n'
        (work/'guard'/'guard.jsonl').write_bytes(audit)
        preparation = dict(schema='g2-clock-preparation-v1', source_sha='a'*40, run_id='123')
        guard = SimpleNamespace(verify=mock.Mock(side_effect=verify_error))
        proc = mock.Mock(returncode=code)
        if stop_error or force_stop:
            proc.poll.side_effect = [ValueError('interrupted wait'), None]
        else:
            proc.poll.return_value = code
        signals = SimpleNamespace(SIGTERM=15, SIGINT=2, SIGHUP=1, signal=mock.Mock())
        calls = []
        with contextlib.ExitStack() as stack:
            stack.enter_context(mock.patch.object(clock, 'load_guard', return_value=guard))
            stack.enter_context(mock.patch.object(clock, 'prepare', return_value=('go', 'b'*64, preparation)))
            stack.enter_context(mock.patch.object(clock, 'signal', signals))
            for name in ('getuid', 'getgid'):
                stack.enter_context(mock.patch.object(clock.os, name, return_value=1001, create=True))
            stack.enter_context(mock.patch.object(clock.subprocess, 'Popen', return_value=proc))
            stop = stack.enter_context(mock.patch.object(clock, 'stop_guard', side_effect=stop_error,
                                                         return_value=force_stop))
            def clean_orphan(*args):
                calls.append('orphan')
                if orphan_error:
                    raise orphan_error
            orphan = stack.enter_context(mock.patch.object(clock, 'close_orphaned_test', side_effect=clean_orphan))
            recover = stack.enter_context(mock.patch.object(clock, 'recover',
                side_effect=lambda *args: calls.append('recover') or recovery))
            collect = stack.enter_context(mock.patch.object(clock, 'collect',
                side_effect=lambda *args: calls.append('collect')))
            output = io.StringIO()
            result = clock.run(work, output)
        self.assertEqual(calls, ['orphan', 'recover', 'collect'])
        recover.assert_called_once()
        collect.assert_called_once()
        if stop_error or force_stop:
            stop.assert_called_once()
        orphan.assert_called_once()
        self.assertEqual((work/'guard'/'guard.jsonl').read_bytes(), audit)
        return result, [json.loads(line) for line in output.getvalue().splitlines()], guard

    def test_actual_go_and_guard_records_remain_distinct_and_failure_is_not_repaired(self):
        for code in (0, 1, 7, -9):
            with self.subTest(code=code):
                result, rows, guard = self.run_fixture(code=code)
                self.assertEqual(result, 0 if code == 0 else 1)
                self.assertEqual(rows[0]['kind'], 'clock_preparation')
                self.assertEqual(rows[1], dict(Action='pass', Package='test-package'))
                self.assertEqual(rows[2]['kind'], 'clock_guard')
                self.assertEqual(rows[-1]['kind'], 'clock_result')
                self.assertEqual(rows[-1]['event']['guard_exit_code'], code)
                self.assertEqual(rows[-1]['event']['recovery_exit_code'], 0)
                guard.verify.assert_called_once()

    def test_failed_recovery_verification_and_termination_all_stay_failed(self):
        cases = [dict(recovery=1), dict(recovery=None), dict(verify_error=ValueError('missing rounds')),
                 dict(stop_error=RuntimeError('cannot join guard'))]
        for case in cases:
            with self.subTest(case=case):
                result, rows, _ = self.run_fixture(**case)
                self.assertEqual(result, 1)
                self.assertEqual(rows[-1]['event']['exit_code'], 1)
                self.assertTrue(any(r.get('kind') == 'clock_guard' for r in rows))

    def test_malformed_go_line_cannot_hide_complete_guard_audit(self):
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            (work/'raw.jsonl').write_bytes(b'{"Action":"pass"}\n\xff\ntruncated')
            (work/'guard').mkdir()
            original = b'{"action":"start"}\n{"action":"failure"}\n'
            (work/'guard'/'guard.jsonl').write_bytes(original)
            output = io.StringIO()
            with self.assertRaisesRegex(ValueError, 'truncated'):
                clock.emit_raw(work, output)
            rows = [json.loads(line) for line in output.getvalue().splitlines()]
            self.assertEqual([r['event']['action'] for r in rows if r.get('kind') == 'clock_guard'], ['start', 'failure'])
            self.assertEqual((work/'guard'/'guard.jsonl').read_bytes(), original)

    def test_hard_kill_and_invalid_child_identity_cannot_skip_clock_restoration(self):
        for error in (None, ValueError('wrong provenance'), KeyError('missing child identity')):
            with self.subTest(error=error):
                result, rows, _ = self.run_fixture(force_stop=True, orphan_error=error)
                self.assertEqual(result, 1)
                self.assertEqual(rows[-1]['event']['recovery_exit_code'], 0)
                self.assertTrue(any('SIGKILL' in e for e in rows[-1]['event']['errors']))

    def test_orphan_cleanup_never_signals_a_foreign_job(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            (directory/'guard.jsonl').write_text('{"action":"child_started","child_pid":55,"source_sha":"other"}\n')
            guard = SimpleNamespace(guard_directory=lambda nonce: directory,
                                    identity=lambda nonce: dict(source_sha='current'))
            with mock.patch.object(clock.os, 'killpg', create=True) as kill:
                with self.assertRaisesRegex(ValueError, 'identify'):
                    clock.close_orphaned_test(guard, 'nonce')
                kill.assert_not_called()

    def test_never_started_guard_has_no_detached_group_to_kill(self):
        with tempfile.TemporaryDirectory() as tmp:
            guard = SimpleNamespace(guard_directory=lambda nonce: Path(tmp)/'not-created')
            with mock.patch.object(clock.os, 'killpg', create=True) as kill:
                clock.close_orphaned_test(guard, 'nonce')
                kill.assert_not_called()

    def test_existing_collected_audit_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            source, work = base/'privileged', base/'work'
            source.mkdir(); work.mkdir()
            (source/'guard.jsonl').write_bytes(b'first\n')
            guard = SimpleNamespace(guard_directory=lambda nonce: source)
            clock.collect(work, guard, 'nonce')
            (source/'guard.jsonl').write_bytes(b'changed\n')
            with self.assertRaisesRegex(ValueError, 'changed'):
                clock.collect(work, guard, 'nonce')
            self.assertEqual((work/'guard'/'guard.jsonl').read_bytes(), b'first\n')

    def test_stop_targets_only_detached_guard_group_and_reports_hard_kill(self):
        proc = mock.Mock(pid=1234)
        proc.poll.return_value = None
        proc.wait.side_effect = [subprocess.TimeoutExpired('guard', 90), 1]
        with mock.patch.object(clock.subprocess, 'run') as run:
            self.assertTrue(clock.stop_guard(proc))
        self.assertEqual([call.args[0] for call in run.call_args_list],
                         [['sudo', '-n', 'kill', '-TERM', '--', '-1234'],
                          ['sudo', '-n', 'kill', '-KILL', '--', '-1234']])


if __name__ == '__main__':
    unittest.main()
