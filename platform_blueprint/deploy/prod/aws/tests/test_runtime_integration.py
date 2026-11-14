import json
import contextlib
import io
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
import runtime_roles as roles
import runtime_boot as boot
import register_epc as deploy
import native_restart_policy as native

IMAGE = 'sha256:' + 'a' * 64


def baseline(current=False, analyzer=False):
    services = roles.MACHINE_ANALYSIS_SERVICES if analyzer else roles.CURRENT_SERVICES if current else roles.LEGACY_SERVICES
    return {'name': roles.PROJECT, 'services': {
        s: {'container_name': roles.expected_container(s), 'image': IMAGE,
            'environment': {'UNCHANGED': s}}
        for s in services}, 'networks': {'deployer_default': {'external': True}}}


def container(name, running=True):
    return {'Name': '/' + name, 'Id': 'id-' + name, 'Image': IMAGE,
            'State': {'Running': running}, 'Mounts': [],
            'HostConfig': {'RestartPolicy': {'Name': 'unless-stopped'}},
            'Config': {'Labels': {'com.docker.compose.project': roles.PROJECT,
                                  'com.docker.compose.service': name}},
            'NetworkSettings': {'Networks': {'deployer_default': {}}}}


class RoleTests(unittest.TestCase):
    def test_only_exact_reviewed_role_extensions_are_admitted(self):
        self.assertEqual(len(roles.declared_roles(baseline())), 31)
        self.assertEqual(len(roles.declared_roles(baseline(True))), 32)
        self.assertEqual(len(roles.declared_roles(baseline(True, True))), 33)
        wrong = baseline(); wrong['services']['unreviewed_preview'] = wrong['services'].pop('cleanapp_web')
        with self.assertRaises(ValueError): roles.declared_roles(wrong)
        wrong = baseline(); wrong['services']['cleanapp_db']['container_name'] = 'other-db'
        with self.assertRaises(ValueError): roles.declared_roles(wrong)
        wrong = baseline(True, True); del wrong['services'][roles.EPC_SERVICE]
        with self.assertRaises(ValueError): roles.declared_roles(wrong)
        wrong = baseline(True, True)
        wrong['services']['unreviewed_preview'] = wrong['services'].pop(roles.MACHINE_ANALYSIS_SERVICE)
        with self.assertRaises(ValueError): roles.declared_roles(wrong)
        wrong = baseline(True, True); wrong['services'][roles.MACHINE_ANALYSIS_SERVICE]['container_name'] = 'other-analyzer'
        with self.assertRaises(ValueError): roles.declared_roles(wrong)

    def test_capture_targets_declared_roles_and_excludes_unrelated_preview(self):
        for analyzer in (False, True):
            with self.subTest(analyzer=analyzer):
                names = list(roles.declared_roles(baseline(True, analyzer)).values())
                calls = []
                def get(args):
                    calls.append(args)
                    if args[0] == 'inspect':
                        self.assertEqual(set(args[1:]), set(names))
                        return [container(n) for n in args[1:]]
                    return []
                result = roles.capture_declared_runtime(names, get)
                self.assertEqual(len(result['containers']), 33 if analyzer else 32)
                self.assertEqual(roles.MACHINE_ANALYSIS_SERVICE in calls[0], analyzer)
                self.assertFalse(any('ps' in c for c in calls))
                self.assertNotIn('cleanapp_grepped_preview', json.dumps(calls))

    def test_boot_plan_accepts_all_reviewed_sets_and_retains_both_ci_pins(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            target = root / 'compose.json'
            pins = [root / 'backend-pins.json', root / 'frontend-pins.json']
            for pin in pins:
                pin.write_text('{"services":{}}')
            for declared in (baseline(), baseline(True), baseline(True, True)):
                target.write_text(json.dumps(declared)); target.chmod(0o600)
                with self.subTest(roles=len(declared['services'])), \
                     mock.patch.object(boot, 'BASE', target), \
                     mock.patch.object(boot, 'PINS', pins), \
                     mock.patch.object(boot, 'BACKUP_LIBRARY', ROOT), \
                     mock.patch.object(boot, 'run') as docker, \
                     mock.patch.object(sys, 'argv', ['runtime_boot.py', '--action', 'plan']), \
                     contextlib.redirect_stdout(io.StringIO()) as output:
                    names, db, broker, command, present = boot.configuration()
                    self.assertEqual(set(names), set(declared['services']))
                    self.assertEqual(db, ['cleanapp_db'])
                    self.assertEqual(broker, ['cleanapp_rabbitmq'])
                    self.assertEqual(present, pins)
                    self.assertTrue(all(str(pin) in command for pin in pins))
                    boot.main()
                    result = json.loads(output.getvalue())
                    self.assertEqual(result['baseline_services'], len(declared['services']))
                    self.assertEqual(result['existing_ci_pin_files'], 2)
                    docker.assert_not_called()

    def test_snapshot_requires_each_role_running_and_no_duplicates(self):
        for declared in (baseline(), baseline(True), baseline(True, True)):
            names = list(roles.declared_roles(declared).values())
            with self.subTest(roles=len(names)):
                cs = [container(n) for n in names]
                stopped = next((c for c in cs if c['Name'] == '/' + roles.MACHINE_ANALYSIS_SERVICE), cs[0])
                stopped['State']['Running'] = False
                with self.assertRaises(ValueError): roles.snapshot_roles({'containers': cs}, names, True)
                cs = [container(n) for n in names]; cs[-1] = cs[0]
                with self.assertRaises(ValueError): roles.snapshot_roles({'containers': cs})
                cs = [container(n) for n in names[:-1]]
                with self.assertRaises(ValueError): roles.snapshot_roles({'containers': cs}, names)

    def test_native_isolation_snapshot_must_match_current_declared_baseline(self):
        baselines = (baseline(), baseline(True), baseline(True, True))
        snapshots = [{'containers': [container(n) for n in roles.declared_roles(value).values()]}
                     for value in baselines]
        for action in ('fence', 'restore', 'verify-isolated', 'verify-restored'):
            for declared in baselines:
                for snapshot in snapshots:
                    def private(path):
                        return snapshot if str(path) == 'snapshot.private' else declared
                    with self.subTest(action=action, roles=len(declared['services']), snapshot=len(snapshot['containers'])), \
                         mock.patch.object(native, 'private_json', side_effect=private), \
                         mock.patch.object(native, 'run') as docker, \
                         mock.patch.object(sys, 'argv', ['native_restart_policy.py', action, '--snapshot', 'snapshot.private']), \
                         contextlib.redirect_stdout(io.StringIO()) as output:
                        if len(declared['services']) == len(snapshot['containers']):
                            native.main()
                            self.assertTrue(json.loads(output.getvalue())['plan'])
                        else:
                            with self.assertRaisesRegex(ValueError, 'differ from declared'): native.main()
                        docker.assert_not_called()

    def test_addition_preserves_all_original_service_definitions_and_replays(self):
        original = baseline(); scope = {'required': ['/original'], 'optional': []}
        updated, new_scope = deploy.prepare(original, scope, IMAGE, deploy.ENV)
        self.assertEqual(len(updated['services']), 32)
        for s in roles.LEGACY_SERVICES: self.assertEqual(original['services'][s], updated['services'][s])
        service = updated['services'][roles.EPC_SERVICE]
        self.assertTrue(service['volumes'][0]['read_only'])
        self.assertEqual(service['stop_grace_period'], '45s')
        self.assertEqual(service['environment'], {'EPC_ENV_FILE': deploy.ENV_TARGET})
        self.assertIn(str(deploy.ENV), new_scope['required'])
        self.assertEqual(deploy.prepare(updated, new_scope, IMAGE, deploy.ENV), (updated, new_scope))
        with self.assertRaises(ValueError): deploy.prepare(updated, new_scope, 'sha256:' + 'b'*64, deploy.ENV)


class AdapterTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.root = Path(self.temp.name)
        self.package = self.root / 'package'; self.package.mkdir()
        self.host = self.root / 'host'; self.host.mkdir()
        self.baseline_path = self.host / 'compose.json'; self.scope = self.host / 'scope.json'
        self.env = self.host / 'epc.env'; self.env.write_text('DB_PASSWORD=private-test-value\n'); self.env.chmod(0o600)
        self.receipts = self.host / 'receipts'; self.receipts.mkdir()
        self.target = self.host / 'runtime_boot.py'; self.target.write_bytes(b'original helper'); self.target.chmod(0o600)
        (self.package / 'runtime_boot.py').write_bytes(b'reviewed helper')
        (self.package / 'runtime_roles.py').write_bytes(b'reviewed registry')
        self.registry = self.host / 'runtime_roles.py'
        self.original = baseline(); deploy.write_file(self.baseline_path, deploy.json_bytes(self.original))
        deploy.write_file(self.scope, deploy.json_bytes({'required': ['/original'], 'optional': []}))
        self.epc_running = False; self.up_commands = []
        self.patches = [mock.patch.object(deploy.os, 'fchown'),
                        mock.patch.object(deploy, 'PACKAGE', self.package),
                        mock.patch.object(deploy, 'BASELINE', self.baseline_path),
                        mock.patch.object(deploy, 'SCOPE', self.scope),
                        mock.patch.object(deploy, 'RECEIPTS', self.receipts),
                        mock.patch.object(deploy, 'PINS', []),
                        mock.patch.object(deploy, 'TARGETS', {'runtime_boot.py': self.target, 'runtime_roles.py': self.registry}),
                        mock.patch.object(deploy, 'preflight_helpers'),
                        mock.patch.object(deploy, 'inspect', side_effect=self.inspect),
                        mock.patch.object(deploy, 'command', side_effect=self.command),
                        mock.patch.object(deploy.subprocess, 'run', side_effect=self.run_command)]
        for p in self.patches: p.start()
        self.addCleanup(self.temp.cleanup)
        for p in self.patches: self.addCleanup(p.stop)

    def inspect(self, names):
        result = []
        for name in names:
            if name == roles.EPC_SERVICE and not self.epc_running: raise ValueError('absent')
            result.append(container(name))
        return result

    def run_command(self, args, **_kwargs):
        if args[:2] == ['docker', 'inspect'] and args[2] == roles.EPC_SERVICE:
            return subprocess.CompletedProcess(args, 0 if self.epc_running else 1,
                                               json.dumps([container(roles.EPC_SERVICE)]).encode(), b'')
        raise AssertionError(args)

    def command(self, args):
        args = [str(a) for a in args]
        if args[:3] == ['docker', 'ps', '-aq']:
            return b'epc-container-id' if self.epc_running else b''
        if args[:3] == ['docker', 'image', 'inspect']:
            return json.dumps([{'Id': IMAGE, 'Os': 'linux', 'Architecture': 'amd64'}]).encode()
        if 'config' in args:
            return self.baseline_path.read_bytes()
        if 'up' in args:
            self.up_commands.append(args)
            self.assertEqual(args[-1], roles.EPC_SERVICE); self.assertIn('--no-deps', args)
            self.epc_running = True
            return b''
        if args[:3] == ['docker', 'rm', '-f']:
            self.assertEqual(args[-1], roles.EPC_SERVICE); self.epc_running = False
            return b''
        raise AssertionError(args)

    def test_apply_exact_replay_and_rollback_touch_only_epc(self):
        result = deploy.apply(IMAGE, self.env)
        self.assertTrue(result['registered']); self.assertEqual(result['other_roles_restarted'], 0)
        self.assertTrue(self.epc_running); self.assertEqual(len(self.up_commands), 1)
        replay = deploy.apply(IMAGE, self.env)
        self.assertTrue(replay['already_registered']); self.assertEqual(len(self.up_commands), 1)
        deploy.rollback(Path(result['receipt']))
        self.assertFalse(self.epc_running)
        self.assertEqual(json.loads(self.baseline_path.read_text()), self.original)
        self.assertEqual(self.target.read_bytes(), b'original helper'); self.assertFalse(self.registry.exists())
        with mock.patch.object(deploy, 'remove_epc') as stop, mock.patch.object(deploy, 'write_file') as write:
            replay = deploy.rollback(Path(result['receipt']))
            self.assertTrue(replay['already_rolled_back'])
            stop.assert_not_called(); write.assert_not_called()

    def test_old_completed_rollback_cannot_undo_a_later_identical_registration(self):
        first = deploy.apply(IMAGE, self.env)
        deploy.rollback(Path(first['receipt']))
        second = deploy.apply(IMAGE, self.env)
        files_before = {path: path.read_bytes() for path in [self.baseline_path, self.scope, self.target, self.registry]}
        with mock.patch.object(deploy, 'remove_epc') as stop, mock.patch.object(deploy, 'write_file') as write:
            with self.assertRaisesRegex(ValueError, 'Completed rollback receipt is stale'):
                deploy.rollback(Path(first['receipt']))
            stop.assert_not_called(); write.assert_not_called()
        self.assertTrue(self.epc_running)
        self.assertEqual({path: path.read_bytes() for path in files_before}, files_before)
        self.assertEqual(json.loads((Path(second['receipt']) / 'manifest.json').read_text())['state'], 'applied')

    def assert_changed_pin_blocks_rollback(self, created=False):
        backend = self.host / 'backend-pins.json'; frontend = self.host / 'frontend-pins.json'
        deploy.write_file(backend, deploy.json_bytes({'services': {}}))
        with mock.patch.object(deploy, 'PINS', [backend, frontend]):
            result = deploy.apply(IMAGE, self.env)
            folder = Path(result['receipt'])
            manifest = json.loads((folder / 'manifest.json').read_text())
            self.assertTrue(manifest['pin_guards'][str(backend)]['exists'])
            self.assertFalse(manifest['pin_guards'][str(frontend)]['exists'])
            changed = frontend if created else backend
            deploy.write_file(changed, deploy.json_bytes({'services': {roles.EPC_SERVICE: {'image': IMAGE}}}))
            files_before = {Path(row['path']): Path(row['path']).read_bytes() for row in manifest['files']}
            pin_before = changed.read_bytes(); receipt_before = (folder / 'manifest.json').read_bytes()
            with mock.patch.object(deploy, 'remove_epc') as stop, mock.patch.object(deploy, 'write_file') as write:
                with self.assertRaisesRegex(ValueError, 'CI image pins changed'): deploy.rollback(folder)
                stop.assert_not_called(); write.assert_not_called()
            self.assertTrue(self.epc_running)
            self.assertEqual({path: path.read_bytes() for path in files_before}, files_before)
            self.assertEqual(changed.read_bytes(), pin_before)
            self.assertEqual((folder / 'manifest.json').read_bytes(), receipt_before)

    def test_changed_ci_pin_refuses_rollback_with_zero_mutations(self):
        self.assert_changed_pin_blocks_rollback()

    def test_previously_absent_ci_pin_refuses_rollback_after_creation(self):
        self.assert_changed_pin_blocks_rollback(created=True)

    def test_completed_rollback_replay_allows_unrelated_ci_pin_changes(self):
        pin = self.host / 'backend-pins.json'
        deploy.write_file(pin, deploy.json_bytes({'services': {}}))
        with mock.patch.object(deploy, 'PINS', [pin]):
            result = deploy.apply(IMAGE, self.env)
            deploy.rollback(Path(result['receipt']))
            deploy.write_file(pin, deploy.json_bytes({'services': {'cleanapp_frontend': {'image': IMAGE}}}))
            changed = pin.read_bytes()
            with mock.patch.object(deploy, 'remove_epc') as stop, mock.patch.object(deploy, 'write_file') as write:
                replay = deploy.rollback(Path(result['receipt']))
                self.assertTrue(replay['already_rolled_back'])
                stop.assert_not_called(); write.assert_not_called()
            self.assertEqual(pin.read_bytes(), changed)

    def test_replay_rejects_missing_backup_scope_registration(self):
        deploy.apply(IMAGE, self.env)
        deploy.write_file(self.scope, deploy.json_bytes({'required': ['/original'], 'optional': []}))
        with self.assertRaisesRegex(ValueError, 'absent from backup scope'): deploy.apply(IMAGE, self.env)
        self.assertEqual(len(self.up_commands), 1)

    def test_failed_one_service_start_restores_original_metadata(self):
        ordinary = self.command
        def fail_up(args):
            if 'up' in args: raise ValueError('start failed')
            return ordinary(args)
        with mock.patch.object(deploy, 'command', side_effect=fail_up):
            with self.assertRaisesRegex(ValueError, 'start failed'): deploy.apply(IMAGE, self.env)
        self.assertEqual(json.loads(self.baseline_path.read_text()), self.original)
        self.assertEqual(self.target.read_bytes(), b'original helper'); self.assertFalse(self.registry.exists())

    def assert_bad_later_backup_has_no_mutations(self, missing=False):
        result = deploy.apply(IMAGE, self.env)
        folder = Path(result['receipt'])
        manifest = json.loads((folder / 'manifest.json').read_text())
        later = next(row for row in reversed(manifest['files']) if row['existed'])
        backup = folder / later['backup']
        if missing: backup.unlink()
        else: deploy.write_file(backup, b'corrupted later backup')
        files_before = {Path(row['path']): Path(row['path']).read_bytes() for row in manifest['files']}
        receipt_before = (folder / 'manifest.json').read_bytes()
        with mock.patch.object(deploy, 'remove_epc') as stop, mock.patch.object(deploy, 'write_file') as write:
            with self.assertRaises((ValueError, FileNotFoundError)): deploy.rollback(folder)
            stop.assert_not_called(); write.assert_not_called()
        self.assertTrue(self.epc_running)
        self.assertEqual({path: path.read_bytes() for path in files_before}, files_before)
        self.assertEqual((folder / 'manifest.json').read_bytes(), receipt_before)

    def test_corrupt_later_backup_refuses_rollback_with_zero_mutations(self):
        self.assert_bad_later_backup_has_no_mutations()

    def test_missing_later_backup_refuses_rollback_with_zero_mutations(self):
        self.assert_bad_later_backup_has_no_mutations(missing=True)

    def test_stale_rollback_refuses_before_stopping_epc(self):
        result = deploy.apply(IMAGE, self.env)
        deploy.write_file(self.baseline_path, b'{"changed_by_later_deployment":true}')
        with self.assertRaisesRegex(ValueError, 'Metadata changed'): deploy.rollback(Path(result['receipt']))
        self.assertTrue(self.epc_running)
        self.assertEqual(self.target.read_bytes(), b'reviewed helper')


if __name__ == '__main__': unittest.main()
