import contextlib
import copy
import io
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
import register_machine_analyzer as deploy
import runtime_roles as roles

IMAGE = 'sha256:' + 'a' * 64
OTHER_IMAGE = 'sha256:' + 'b' * 64
SOURCE = 'c' * 40
SERVICE = roles.MACHINE_ANALYSIS_SERVICE


def baseline():
    value = {'name': roles.PROJECT, 'services': {
        name: {'container_name': roles.expected_container(name), 'image': IMAGE,
               'environment': {'UNCHANGED': name}}
        for name in roles.CURRENT_SERVICES},
        'networks': {'deployer_default': {'external': True}}}
    value['services'][deploy.HUMAN_SERVICE].update({
        'environment': {'DB_PASSWORD': 'synthetic-$$-private-value', 'PORT': '8080',
                        'RABBITMQ_QUEUE': 'human-queue', 'RABBITMQ_RAW_REPORT_ROUTING_KEY': 'report.human',
                        'RABBITMQ_PREFETCH_COUNT': '5', 'RABBITMQ_CONCURRENCY': '4',
                        'EVIDENCE_WORKER_ENABLED': 'true', 'ENRICH_EXTERNAL_DIGITAL_ENABLED': 'true'},
        'ports': ['18080:8080'],
        'networks': {'deployer_default': {'ipv4_address': '172.18.0.25'}},
    })
    return value


def container(name, identifier=None):
    return {'Name': '/' + name, 'Id': identifier or 'id-' + name, 'Image': IMAGE,
            'State': {'Running': True, 'Health': {'Status': 'healthy'}},
            'HostConfig': {'RestartPolicy': {'Name': 'unless-stopped'}},
            'Config': {'Labels': {'com.docker.compose.project': roles.PROJECT,
                                  'com.docker.compose.service': name,
                                  'org.opencontainers.image.revision': SOURCE}}}


class DefinitionTests(unittest.TestCase):
    def test_only_machine_consumer_settings_change_and_no_human_network_endpoint_is_cloned(self):
        original = baseline(); saved = copy.deepcopy(original)
        service = deploy.service_definition(original, IMAGE)
        expected = {**original['services'][deploy.HUMAN_SERVICE]['environment'], **deploy.ENV_OVERRIDES}
        self.assertEqual(service['environment'], expected)
        self.assertEqual(original, saved)
        self.assertNotIn('ports', service)
        self.assertNotIn('privileged', service)
        self.assertEqual(service['networks'], {'deployer_default': {}})
        self.assertEqual(service['stop_grace_period'], '120s')
        self.assertEqual(service['restart'], 'unless-stopped')
        self.assertEqual(service['pull_policy'], 'never')
        self.assertIn('wget', service['healthcheck']['test'][1])
        self.assertIn('http://127.0.0.1:8080/api/v3/health', service['healthcheck']['test'][1])
        self.assertIn('rabbitmq_connected', service['healthcheck']['test'][1])
        self.assertIn('true', service['healthcheck']['test'][1])
        for key, expected_value in {
            'RABBITMQ_EXCHANGE': 'cleanapp-exchange', 'RABBITMQ_QUEUE': 'report-analysis-queue',
            'RABBITMQ_RAW_REPORT_ROUTING_KEY': 'report.raw', 'RABBITMQ_ANALYSED_REPORT_ROUTING_KEY': 'report.analysed',
            'RABBITMQ_PREFETCH_COUNT': '1', 'RABBITMQ_CONCURRENCY': '1',
            'EVIDENCE_WORKER_ENABLED': 'false', 'ENRICH_EXTERNAL_DIGITAL_ENABLED': 'false',
        }.items():
            self.assertEqual(service['environment'][key], expected_value)

    def test_prepare_preserves_all_32_definitions_and_allows_only_exact_replay(self):
        original = baseline(); saved = copy.deepcopy(original)
        updated = deploy.prepare(original, IMAGE)
        self.assertEqual(original, saved)
        self.assertEqual(set(updated['services']), roles.MACHINE_ANALYSIS_SERVICES)
        for name in roles.CURRENT_SERVICES:
            self.assertEqual(original['services'][name], updated['services'][name])
        self.assertEqual(deploy.prepare(updated, IMAGE), updated)
        with self.assertRaises(ValueError): deploy.prepare(updated, OTHER_IMAGE)
        old = baseline(); del old['services'][roles.EPC_SERVICE]
        with self.assertRaises(ValueError): deploy.prepare(old, IMAGE)

    def test_malformed_environment_network_and_port_fail_before_any_registration(self):
        for change in ('list', 'port', 'network'):
            value = baseline()
            if change == 'list': value['services'][deploy.HUMAN_SERVICE]['environment'] = ['PRIVATE=x']
            elif change == 'port': value['services'][deploy.HUMAN_SERVICE]['environment']['PORT'] = '8080; command'
            else: value['networks'] = {}
            with self.subTest(change=change), self.assertRaises(ValueError):
                deploy.prepare(value, IMAGE)


class RegistrationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(); self.root = Path(self.temporary.name)
        self.package = self.root / 'package'; self.package.mkdir()
        self.host = self.root / 'host'; self.host.mkdir()
        self.library = self.host / 'library'; self.library.mkdir()
        self.receipts = self.host / 'receipts'; self.receipts.mkdir()
        self.baseline_path = self.host / 'compose.json'
        self.target = self.library / 'runtime_roles.py'
        self.scope = self.host / 'scope.json'
        self.pins = [self.host / 'backend-pins.json', self.host / 'frontend-pins.json']
        self.original = baseline()
        deploy.write_file(self.baseline_path, deploy.json_bytes(self.original))
        deploy.write_file(self.target, b'original reviewed registry\n')
        deploy.write_file(self.scope, deploy.json_bytes({'required': ['/original'], 'optional': []}))
        for path in self.pins: deploy.write_file(path, deploy.json_bytes({'services': {}}))
        (self.package / 'runtime_roles.py').write_bytes(b'expanded reviewed registry\n')
        (self.package / 'production-helper-provenance.json').write_text(json.dumps({'files': {
            'runtime_roles.py': {'installed_path': str(self.target),
                                 'production_sha256': deploy.digest(self.target.read_bytes())}}}))
        self.original_bytes = self.metadata()
        self.existing = {roles.expected_container(name): container(roles.expected_container(name))
                         for name in roles.CURRENT_SERVICES}
        self.original_ids = {name: value['Id'] for name, value in self.existing.items()}
        self.machine = None; self.generation = 0; self.events = []; self.fail_start = False
        self.resolved_environments = None
        self.image = {'Id': IMAGE, 'Os': 'linux', 'Architecture': 'amd64',
                      'Config': {'Labels': {'org.opencontainers.image.revision': SOURCE},
                                 'Entrypoint': None, 'Cmd': ['./report-analyze-pipeline'], 'WorkingDir': '/root'}}
        replacements = {'PACKAGE': self.package, 'BASELINE': self.baseline_path, 'DEPLOY': self.host,
                        'LIBRARY': self.library, 'RUNTIME_ROLES_TARGET': self.target,
                        'SCOPE': self.scope, 'LOCK': self.host / 'lock', 'RECEIPTS': self.receipts,
                        'PINS': self.pins}
        self.patches = [mock.patch.object(deploy, name, value) for name, value in replacements.items()]
        self.patches.append(mock.patch.object(deploy, 'command', side_effect=self.command))
        for patch in self.patches: patch.start()
        self.addCleanup(self.temporary.cleanup)
        for patch in self.patches: self.addCleanup(patch.stop)

    def metadata(self):
        return {str(path): path.read_bytes() for path in
                [self.baseline_path, self.target, self.scope, *self.pins]}

    def command(self, args):
        args = [str(arg) for arg in args]; self.events.append(args)
        if args[:3] == ['docker', 'image', 'inspect']:
            return json.dumps([self.image]).encode()
        if args[:3] == ['docker', 'ps', '-aq']:
            self.assertEqual(args[-1], 'name=^/' + SERVICE + '$')
            return (self.machine['Id'].encode() if self.machine else b'')
        if args[:2] == ['docker', 'inspect']:
            values = []
            for name in args[2:]:
                value = self.machine if name == SERVICE else self.existing[name]
                if value is None: raise ValueError('synthetic absent machine')
                values.append(value)
            return json.dumps(values).encode()
        if args[:2] == ['docker', 'compose']:
            for path in self.pins:
                self.assertIn(str(path), args)
            if 'config' in args:
                resolved = json.loads(self.baseline_path.read_bytes())
                for definition in resolved['services'].values():
                    definition['environment'] = {key: value.replace('$$', '$')
                                                 for key, value in definition.get('environment', {}).items()}
                self.resolved_environments = (resolved['services'][deploy.HUMAN_SERVICE]['environment'],
                                              resolved['services'][SERVICE]['environment'])
                return json.dumps(resolved).encode()
            if 'up' in args:
                self.assertEqual(args[-1], SERVICE)
                for flag in ('--no-deps', '--no-build', '--wait'): self.assertIn(flag, args)
                self.assertEqual(args[args.index('--pull') + 1], 'never')
                self.generation += 1; self.machine = container(SERVICE, 'machine-' + str(self.generation))
                if self.fail_start:
                    self.machine['State']['Health']['Status'] = 'unhealthy'
                    raise ValueError('synthetic startup failure')
                return b''
        if args[:2] == ['docker', 'stop']:
            self.assertEqual(args, ['docker', 'stop', '--time', '120', SERVICE])
            self.machine['State']['Running'] = False
            return b''
        if args[:2] == ['docker', 'rm']:
            self.assertEqual(args, ['docker', 'rm', SERVICE])
            self.machine = None; return b''
        raise AssertionError(args)

    def stops(self):
        return [event for event in self.events if event[:2] in (['docker', 'stop'], ['docker', 'rm'])]

    def assert_existing_untouched(self):
        self.assertEqual({name: value['Id'] for name, value in self.existing.items()}, self.original_ids)
        self.assertTrue(all(value['State']['Running'] for value in self.existing.values()))
        for path in [self.scope, *self.pins]: self.assertEqual(path.read_bytes(), self.original_bytes[str(path)])

    def test_success_exact_replay_and_rollback_preserve_existing_32_ids_scope_and_both_pins(self):
        result = deploy.apply(IMAGE, SOURCE)
        self.assertTrue(result['registered']); self.assertEqual(result['roles'], 33)
        self.assertEqual(result['other_roles_restarted'], 0)
        self.assert_existing_untouched()
        human, machine = self.resolved_environments
        self.assertEqual(machine, {**human, **deploy.ENV_OVERRIDES})
        self.assertEqual(machine['DB_PASSWORD'], 'synthetic-$-private-value')
        raw = json.loads(self.baseline_path.read_bytes())
        self.assertEqual(raw['services'][deploy.HUMAN_SERVICE], self.original['services'][deploy.HUMAN_SERVICE])
        self.assertEqual(raw['services'][SERVICE]['environment']['DB_PASSWORD'], 'synthetic-$$-private-value')
        after = self.metadata(); receipt_count = len(list(self.receipts.iterdir()))
        replay = deploy.apply(IMAGE, SOURCE)
        self.assertTrue(replay['already_registered'])
        self.assertEqual(self.metadata(), after)
        self.assertEqual(len(list(self.receipts.iterdir())), receipt_count)
        self.assertEqual(len([event for event in self.events if 'up' in event]), 1)
        manifest = json.loads((Path(result['receipt']) / 'manifest.json').read_text())
        self.assertEqual({row['path'] for row in manifest['files']}, {str(self.baseline_path), str(self.target)})
        deploy.rollback(Path(result['receipt']))
        self.assertIsNone(self.machine)
        self.assertEqual(self.metadata(), self.original_bytes)
        self.assert_existing_untouched()
        before = len(self.stops())
        self.assertTrue(deploy.rollback(Path(result['receipt']))['already_rolled_back'])
        self.assertEqual(len(self.stops()), before)

    def assert_rejected_without_metadata_writes(self):
        with self.assertRaises(ValueError): deploy.apply(IMAGE, SOURCE)
        self.assertEqual(self.metadata(), self.original_bytes)
        self.assertEqual(list(self.receipts.iterdir()), [])
        self.assertFalse(any('up' in event for event in self.events))
        self.assertFalse(self.stops())

    def test_wrong_image_revision_or_identity_rejected_before_writes(self):
        self.image['Config']['Labels']['org.opencontainers.image.revision'] = 'd' * 40
        self.assert_rejected_without_metadata_writes()
        self.image['Config']['Labels']['org.opencontainers.image.revision'] = SOURCE
        self.image['Id'] = OTHER_IMAGE
        self.assert_rejected_without_metadata_writes()

    def test_unexpected_image_startup_command_rejected_before_writes(self):
        for key, wrong in [('Cmd', ['./unexpected']), ('WorkingDir', '/other'), ('Entrypoint', ['/unexpected'])]:
            with self.subTest(field=key):
                original = self.image['Config'][key]
                self.image['Config'][key] = wrong
                self.assert_rejected_without_metadata_writes()
                self.image['Config'][key] = original

    def test_wrong_declared_container_name_rejected_before_writes(self):
        value = baseline(); value['services']['cleanapp_db']['container_name'] = 'other-db'
        deploy.write_file(self.baseline_path, deploy.json_bytes(value)); self.original_bytes = self.metadata()
        self.assert_rejected_without_metadata_writes()

    def test_unreviewed_role_helper_rejected_before_writes(self):
        deploy.write_file(self.target, b'unreviewed registry drift\n'); self.original_bytes = self.metadata()
        self.assert_rejected_without_metadata_writes()

    def assert_changed_guard_blocks_rollback(self, path):
        result = deploy.apply(IMAGE, SOURCE); identifier = self.machine['Id']
        deploy.write_file(path, path.read_bytes() + b'\n')
        after = self.metadata(); before = len(self.stops())
        with self.assertRaises(ValueError): deploy.rollback(Path(result['receipt']))
        self.assertEqual(self.metadata(), after); self.assertEqual(len(self.stops()), before)
        self.assertEqual(self.machine['Id'], identifier); self.assertTrue(self.machine['State']['Running'])

    def test_changed_backend_pin_blocks_rollback_before_stop(self):
        self.assert_changed_guard_blocks_rollback(self.pins[0])

    def test_changed_frontend_pin_blocks_rollback_before_stop(self):
        self.assert_changed_guard_blocks_rollback(self.pins[1])

    def test_changed_backup_scope_blocks_rollback_before_stop(self):
        self.assert_changed_guard_blocks_rollback(self.scope)

    def test_corrupted_later_backup_never_partially_restores_or_stops(self):
        result = deploy.apply(IMAGE, SOURCE); folder = Path(result['receipt'])
        manifest = json.loads((folder / 'manifest.json').read_text())
        deploy.write_file(folder / manifest['files'][-1]['backup'], b'corrupt later backup\n')
        after = self.metadata(); before = len(self.stops())
        with self.assertRaises(ValueError): deploy.rollback(folder)
        self.assertEqual(self.metadata(), after); self.assertEqual(len(self.stops()), before)
        self.assertTrue(self.machine['State']['Running'])

    def test_startup_failure_restores_both_files_and_removes_only_new_role(self):
        self.fail_start = True
        with self.assertRaisesRegex(ValueError, 'synthetic startup failure'): deploy.apply(IMAGE, SOURCE)
        self.assertIsNone(self.machine); self.assertEqual(self.metadata(), self.original_bytes)
        self.assert_existing_untouched()
        self.assertEqual(len(self.stops()), 2)

    def test_completed_old_receipt_cannot_remove_later_same_image_registration(self):
        first = deploy.apply(IMAGE, SOURCE); deploy.rollback(Path(first['receipt']))
        deploy.apply(IMAGE, SOURCE); after = self.metadata(); identifier = self.machine['Id']; before = len(self.stops())
        with self.assertRaises(ValueError): deploy.rollback(Path(first['receipt']))
        self.assertEqual(self.metadata(), after); self.assertEqual(len(self.stops()), before)
        self.assertEqual(self.machine['Id'], identifier)

    def test_recreated_machine_id_blocks_applied_receipt_before_stop(self):
        result = deploy.apply(IMAGE, SOURCE); self.machine['Id'] = 'later-recreated-machine'
        after = self.metadata(); before = len(self.stops())
        with self.assertRaises(ValueError): deploy.rollback(Path(result['receipt']))
        self.assertEqual(self.metadata(), after); self.assertEqual(len(self.stops()), before)

    def test_default_plan_does_not_touch_host_docker_or_files(self):
        before = self.metadata()
        argv = ['register_machine_analyzer.py', '--image-id', IMAGE, '--source-sha', SOURCE]
        with mock.patch.object(sys, 'argv', argv), \
             mock.patch.object(deploy, 'admitted_host', side_effect=AssertionError('must not touch host')) as host, \
             contextlib.redirect_stdout(io.StringIO()) as output:
            deploy.main()
        self.assertTrue(json.loads(output.getvalue())['plan'])
        self.assertEqual(self.events, []); self.assertEqual(self.metadata(), before)
        self.assertEqual(list(self.receipts.iterdir()), []); host.assert_not_called()

    def test_admission_is_rechecked_under_lock_before_registration_or_rollback(self):
        for action in ('register', 'rollback'):
            with self.subTest(action=action):
                state = {'active': True}

                def admitted():
                    if not state['active']:
                        raise ValueError('synthetic runtime became isolated')

                @contextlib.contextmanager
                def lock(_path):
                    # Model another operator isolating the host after the
                    # initial admission check and before this lock is acquired.
                    state['active'] = False
                    yield

                argv = ['register_machine_analyzer.py', '--execute']
                if action == 'register':
                    argv += ['--image-id', IMAGE, '--source-sha', SOURCE]
                else:
                    argv += ['--rollback', str(self.receipts / 'machine-analyzer-fixture')]
                before = self.metadata()
                helper = types.SimpleNamespace(shared_deployment_lock=lock)
                with mock.patch.object(sys, 'argv', argv), \
                     mock.patch.dict(sys.modules, {'backup_recovery': helper}), \
                     mock.patch.object(deploy, 'admitted_host', side_effect=admitted) as host, \
                     mock.patch.object(deploy, 'apply') as apply, \
                     mock.patch.object(deploy, 'rollback') as rollback:
                    with self.assertRaisesRegex(ValueError, 'runtime became isolated'):
                        deploy.main()
                self.assertEqual(host.call_count, 2)
                apply.assert_not_called(); rollback.assert_not_called()
                self.assertEqual(self.events, [])
                self.assertEqual(self.metadata(), before)
                self.assertEqual(list(self.receipts.iterdir()), [])


if __name__ == '__main__':
    unittest.main()
