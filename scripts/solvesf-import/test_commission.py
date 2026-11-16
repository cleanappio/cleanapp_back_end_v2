"""Commissioning archive and fixed fixture safety tests; no live operations."""
import hashlib
import datetime as dt
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import types
import subprocess
import unittest
from unittest.mock import Mock, patch

SPEC = importlib.util.spec_from_file_location('solvesf_commission_test', Path(__file__).with_name('commission.py'))
commission = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(commission)


def archive(extra=None, omit=None):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w:gz') as tar:
        for name in sorted(commission.REQUIRED - ({omit} if omit else set())):
            body = (name + '\n').encode(); member = tarfile.TarInfo(name); member.size = len(body)
            tar.addfile(member, io.BytesIO(body))
        if extra:
            member, body = extra
            tar.addfile(member, io.BytesIO(body) if member.isfile() else None)
    return output.getvalue()


class ArchiveSafety(unittest.TestCase):
    def test_verifier_is_staged_but_core_manifest_stays_exactly_five(self):
        self.assertIn('verify_live.py', commission.APP)
        self.assertIn('test_verify_live.py', commission.TESTS)
        with patch.object(commission, 'sha', return_value='fixed'):
            self.assertEqual(set(commission.source_hashes()), {'service.py', 'guard.py', 'apply.py', 'media.py', 'collect.py'})

    def test_staging_refuses_transitional_units_even_when_is_active_exit_would_be_nonzero(self):
        for state in ('active', 'activating', 'deactivating'):
            result = subprocess.CompletedProcess([], 3, stdout=('LoadState=loaded\nActiveState=' + state + '\n').encode())
            with self.subTest(state=state), patch.object(commission.subprocess, 'run', return_value=result):
                with self.assertRaisesRegex(ValueError, 'before_staging'):
                    commission.stage_idle()
        for load, state, code in (('loaded', 'inactive', 0), ('loaded', 'failed', 0), ('not-found', 'inactive', 1)):
            result = subprocess.CompletedProcess([], code, stdout=('LoadState=' + load + '\nActiveState=' + state + '\n').encode())
            with patch.object(commission.subprocess, 'run', return_value=result):
                commission.stage_idle()

    def test_exact_frozen_bytes_whitelist_and_digest(self):
        blob = archive(); digest = hashlib.sha256(blob).hexdigest()
        result = commission.unpack(blob, digest)
        self.assertEqual(set(result), commission.REQUIRED)
        with self.assertRaisesRegex(ValueError, 'digest'):
            commission.unpack(blob + b'changed', digest)

    def test_traversal_symlink_unknown_and_missing_members_refused(self):
        for name, kind in (('../root-key', tarfile.REGTYPE), ('/etc/shadow', tarfile.REGTYPE),
                           ('scripts/solvesf-import/evil.py', tarfile.REGTYPE),
                           ('scripts/solvesf-import/link.py', tarfile.SYMTYPE)):
            member = tarfile.TarInfo(name); member.type = kind; member.linkname = '/etc/shadow'
            blob = archive((member, b''))
            with self.subTest(name=name), self.assertRaises(ValueError):
                commission.unpack(blob, hashlib.sha256(blob).hexdigest())
        blob = archive(omit='go-common/sf311import/schema.sql')
        with self.assertRaisesRegex(ValueError, 'incomplete'):
            commission.unpack(blob, hashlib.sha256(blob).hexdigest())

    def test_mac_metadata_is_ignored_not_extracted(self):
        member = tarfile.TarInfo('__MACOSX/scripts/._guard.py'); member.size = 4
        blob = archive((member, b'meta'))
        result = commission.unpack(blob, hashlib.sha256(blob).hexdigest())
        self.assertEqual(set(result), commission.REQUIRED)


class FixedFixtureSafety(unittest.TestCase):
    def valid(self):
        return {'HostConfig': {'NetworkMode': 'none', 'Memory': 1024**3},
                'Image': commission.FIXTURE_IMAGE, 'Config': {'Labels': {'cleanapp.solvesf.fixture': 'true'}}}

    def test_only_exact_disposable_container_and_nonproduction_server_can_reset(self):
        calls = []
        def command(args, **kwargs):
            calls.append((args, kwargs))
            if args[:2] == ['docker', 'inspect']:
                return json.dumps([self.valid()]).encode()
            if kwargs['input'].startswith(b'SELECT'):
                return b'fixture-server-uuid\t8.0.44\n'
            return b''
        with patch.object(commission, 'command', side_effect=command):
            commission.fixture_reset()
        self.assertTrue(all(commission.FIXTURE in args for args, _ in calls))
        self.assertEqual(calls[-1][1]['input'], b'DROP DATABASE IF EXISTS cleanapp_dataunion_import_test; CREATE DATABASE cleanapp_dataunion_import_test CHARACTER SET utf8mb4;\n')
        with patch.object(commission, 'command', side_effect=[json.dumps([self.valid()]).encode(),
                (commission.SERVER + '\t8.0.44\n').encode()]) as command:
            with self.assertRaisesRegex(ValueError, 'identity'):
                commission.fixture_reset()
            self.assertEqual(command.call_count, 2)

    def test_wrong_network_label_image_or_memory_never_queries_database(self):
        for section, field, value in (('HostConfig', 'NetworkMode', 'bridge'), ('HostConfig', 'Memory', 0),
                                       ('Config', 'Labels', {}), (None, 'Image', 'another-image')):
            info = self.valid()
            (info if section is None else info[section])[field] = value
            with self.subTest(field=field), patch.object(commission, 'command', return_value=json.dumps([info]).encode()) as command:
                with self.assertRaisesRegex(ValueError, 'fixture'):
                    commission.fixture_reset()
                self.assertEqual(command.call_count, 1)


class FailureDiagnostics(unittest.TestCase):
    def test_dead_transport_cleanup_preserves_primary_and_records_recovery_failure(self):
        primary = RuntimeError('private diagnostic must not be emitted')
        primary.code = 1205
        recovery = ValueError('recovery_not_verified')
        db = types.SimpleNamespace(_closed=True, query=Mock(side_effect=AssertionError('closed transport reused')))
        controller = types.SimpleNamespace(recover_pause=Mock(side_effect=recovery))
        with patch.object(commission, 'write') as write:
            with self.assertRaises(RuntimeError) as raised:
                try:
                    raise primary
                finally:
                    commission.release_migration_lock(db, primary)
                    commission.recover_migration(controller, primary)
            self.assertIs(raised.exception, primary)
            db.query.assert_not_called()
            receipt = json.loads(write.call_args.args[1])
            self.assertEqual(receipt['primary'], {'error_type': 'RuntimeError', 'mysql_code': 1205})
            self.assertEqual(receipt['recovery']['error_code'], 'recovery_not_verified')
            self.assertFalse(receipt['recovery_verified'])

    def test_cleanup_failure_is_visible_without_a_primary_error(self):
        failure = RuntimeError('cleanup failed')
        db = types.SimpleNamespace(_closed=False, query=Mock(side_effect=failure))
        with self.assertRaises(RuntimeError) as raised:
            commission.release_migration_lock(db, None)
        self.assertIs(raised.exception, failure)
        with self.assertRaises(RuntimeError):
            commission.recover_migration(types.SimpleNamespace(recover_pause=Mock(side_effect=failure)), None)

    def test_safe_diagnostics_exclude_arbitrary_messages_and_codes(self):
        for code in ('1205', True, None):
            error = RuntimeError('https://example.invalid/?token=private'); error.code = code
            self.assertEqual(commission.error_summary(error), {'error_type': 'RuntimeError'})


class EnableSafety(unittest.TestCase):
    def test_sandbox_profile_uses_real_restrictions_and_only_readonly_check(self):
        text = Path(__file__).with_name(commission.UNITS[0]).read_text()
        args = commission.sandbox_command(text)
        self.assertEqual(args[-3:], [str(commission.PYTHON), str(commission.ROOT / 'app/service.py'), '--check'])
        self.assertNotIn('--run', args)
        self.assertIn('--property=ProtectSystem=strict', args)
        self.assertIn('--property=NoNewPrivileges=false', args)
        for old, changed in (('NoNewPrivileges=false', 'NoNewPrivileges=true'),
                             ('ProtectSystem=strict', 'ProtectSystem=false'),
                             ('MemoryMax=1G', 'MemoryMax=2G'), ('CPUQuota=25%', 'CPUQuota=100%')):
            with self.subTest(setting=old), self.assertRaisesRegex(ValueError, 'limits'):
                commission.sandbox_command(text.replace(old, changed))
        for setting in ('ExecStartPre=/bin/true', 'UnreviewedProperty=true'):
            with self.subTest(setting=setting), self.assertRaisesRegex(ValueError, 'unreviewed'):
                commission.sandbox_command(text + '\n' + setting + '\n')

    def test_successful_owned_resume_proof_never_relocks_running_native_writer(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); source = root / 'source'
            for unit in commission.UNITS:
                path = source / (commission.SCRIPTS + unit)
                path.parent.mkdir(parents=True, exist_ok=True); path.write_text('reviewed fixture unit')
            last = {'state': 'batch_complete', 'cases': 1}
            proof = {'kind': 'cleanapp_solvesf_live_canary.v1', 'passed': True,
                'config_sha256': 'config', 'controller_run_sha256': 'run',
                'public_reports_verified': True, 'quiet_effects_verified': True}
            sandbox = {'kind': 'cleanapp_solvesf_sandbox_check.v1', 'passed': True,
                'config_sha256': 'config', 'service_sha256': 'run', 'unit_sha256': 'run',
                'checked_at_utc': dt.datetime.now(dt.timezone.utc).isoformat(),
                'result': {'state': 'configuration_verified', 'publication_performed': False, 'native_cases': 1}}
            def document(path, _digest):
                return {'controller-last-run.json': last, 'live-canary.json': proof, 'sandbox-check.json': sandbox}[path.name]
            controller = types.SimpleNamespace(config={}, config_sha='config', seen=Mock(return_value={'123'}),
                integrity=Mock(side_effect=AssertionError('must not compete with running OLM writer')),
                guard=types.SimpleNamespace(document=document))
            controller.config['expires_at'] = 'finite configured deadline'
            service = types.SimpleNamespace(Controller=lambda: controller, PAUSE=root / 'no-pause',
                validate_limits=lambda *_: 'eligible', now=lambda: None, unit_state=lambda: 'running',
                running=lambda _: True, TIMER=commission.UNITS[1])
            with patch.object(commission, 'ROOT', root), patch.object(commission, 'SOURCE', source), \
                    patch.object(commission, 'module', return_value=service), patch.object(commission, 'sha', return_value='run'), \
                    patch.object(commission, 'trusted'), patch.object(commission, 'write'), patch.object(commission, 'command') as command:
                self.assertTrue(commission.enable()['timer_enabled'])
                controller.integrity.assert_not_called()
                command.assert_called_with(['/usr/bin/systemctl', 'enable', '--now', service.TIMER])
                last['state'] = 'deferred'; command.reset_mock()
                with self.assertRaisesRegex(ValueError, 'first_cycle'):
                    commission.enable()
                command.assert_not_called()
                last['state'] = 'batch_complete'; sandbox['service_sha256'] = 'changed'
                with self.assertRaisesRegex(ValueError, 'sandbox_check'):
                    commission.enable()
                command.assert_not_called()


if __name__ == '__main__':
    unittest.main()
