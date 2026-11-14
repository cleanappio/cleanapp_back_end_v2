"""Offline compatibility/refusal cases; no sudo, Docker, AWS or DB commands."""
import copy
import datetime as dt
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

BASE = Path(__file__).resolve().parents[1]


def load(name):
    spec = importlib.util.spec_from_file_location(name, BASE / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


G = load('olm_deploy_guard')
A = load('aws_image_deploy')


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.now = dt.datetime(2026, 11, 14, tzinfo=dt.timezone.utc)
        self.pins = {role: ['sha256:' + str(i) * 64, 'a' * 40]
                     for i, role in enumerate(G.ROLES, 1)}
        self.role = 'cleanapp_report_listener'
        self.manifest = {'source_sha': 'b' * 40, 'roles': {self.role: self.role},
                         'images': {'report-listener': {'runtime': {'image_id': 'sha256:' + 'd' * 64}}}}
        self.state = {'ActiveState': 'failed', 'MainPID': '0', 'ControlPID': '0'}
        proposed = copy.deepcopy(self.pins)
        proposed[self.role] = ['sha256:' + 'd' * 64, 'b' * 40]
        self.plan = {'kind': 'cleanapp_olm_deploy_review.v1', 'manifest_sha256': 'e' * 64,
                     'previous_guard_sha256': 'f' * 64, 'source_review_sha256': 'c' * 64,
                     'valid_until_utc': '2026-11-14T01:00:00Z',
                     'manual_post_deploy_probes_and_resume': True, 'approved_runtime': proposed}

    def decide(self, plan=None):
        return G.decision(self.manifest, self.pins, self.pins, 'f' * 64, 'e' * 64,
                          self.state, plan, self.now)

    def test_unreviewed_actual_runtime_is_never_adopted(self):
        actual = copy.deepcopy(self.pins)
        actual[self.role][0] = 'sha256:' + '8' * 64
        with self.assertRaisesRegex(ValueError, 'Actual guarded runtime'):
            G.decision(self.manifest, self.pins, actual, 'f' * 64, 'e' * 64,
                       self.state, self.plan, self.now)

    def test_actual_inspection_is_bounded_to_fixed_roles_without_environment_output(self):
        def inspect(args, **kwargs):
            self.assertEqual(args[:3], ['/usr/bin/docker', 'inspect', '--format'])
            self.assertEqual(kwargs['timeout'], 15)
            self.assertNotIn('Env', args[3])
            pair = self.pins[args[-1]]
            return '\n'.join(json.dumps(value) for value in [*pair, True])
        with patch.object(G.subprocess, 'check_output', inspect):
            self.assertEqual(G.current_runtime(), self.pins)
        raw = '\n'.join(json.dumps(value) for value in [*self.pins[self.role], False])
        with patch.object(G.subprocess, 'check_output', return_value=raw), self.assertRaises(ValueError):
            G.current_runtime()

    def test_missing_registration_skips_and_failed_installed_job_does_not(self):
        manifest_raw = json.dumps(self.manifest).encode()
        def absent(path, **kwargs):
            if str(path) == '/release/manifest.json':
                return manifest_raw
            raise FileNotFoundError()
        with patch.object(G, 'read_file', absent), patch.object(G, 'receiver_state',
                side_effect=AssertionError('uninstalled job queried service')):
            self.assertEqual(G.check('/release/manifest.json')['decision'], 'not_installed')
        def installed(path, **kwargs):
            if str(path) == '/release/manifest.json':
                return manifest_raw
            if str(path).endswith('receiver-config.json'):
                return b'{}'
            raise FileNotFoundError()
        with patch.object(G, 'read_file', installed), patch.object(G, 'reviewed_pins',
                return_value=(self.pins, 'f' * 64)), patch.object(G, 'current_runtime', return_value=self.pins), \
                patch.object(G, 'receiver_state', return_value=self.state), self.assertRaisesRegex(ValueError, 'root-reviewed plan'):
            G.check('/release/manifest.json')

    def test_unknown_image_is_refused_for_failed_inactive_active_and_activating_receivers(self):
        for state in ('failed', 'inactive', 'active', 'activating'):
            with self.subTest(state=state):
                self.state['ActiveState'] = state
                with self.assertRaisesRegex(ValueError, 'root-reviewed plan'):
                    self.decide()

    def test_known_reviewed_images_pass_without_pausing_running_import(self):
        self.manifest['source_sha'] = 'a' * 40
        self.manifest['images']['report-listener']['runtime']['image_id'] = self.pins[self.role][0]
        self.state = {'ActiveState': 'activating', 'MainPID': '42', 'ControlPID': '0'}
        self.assertEqual(self.decide(), 'unchanged')

    def test_exact_root_review_plan_allows_change_only_after_receiver_is_stopped(self):
        self.assertEqual(self.decide(self.plan), 'reviewed_quiescent')
        for key, value in (('ActiveState', 'activating'), ('MainPID', '42'), ('ControlPID', '42')):
            original = self.state[key]
            self.state[key] = value
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, 'already stopped'):
                self.decide(self.plan)
            self.state[key] = original

    def test_stale_wrong_release_guard_or_review_and_changed_unselected_role_are_refused(self):
        changes = [('manifest_sha256', '1' * 64), ('previous_guard_sha256', '2' * 64),
                   ('source_review_sha256', ''), ('manual_post_deploy_probes_and_resume', False),
                   ('valid_until_utc', '2026-11-13T23:59:59Z'),
                   ('valid_until_utc', '2026-11-16T00:00:00Z')]
        for key, value in changes:
            plan = copy.deepcopy(self.plan)
            plan[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ValueError):
                self.decide(plan)
        plan = copy.deepcopy(self.plan)
        plan['approved_runtime']['cleanapp_email_service'][0] = 'sha256:' + '9' * 64
        with self.assertRaises(ValueError):
            self.decide(plan)

    def test_malformed_runtime_identity_or_extra_role_is_refused(self):
        for pair in (['latest', 'a' * 40], ['sha256:' + 'a' * 64, 'main']):
            pins = copy.deepcopy(self.pins)
            pins[self.role] = pair
            with self.assertRaises(ValueError):
                G.validate_pairs(pins)
        with self.assertRaises(ValueError):
            G.validate_pairs({**self.pins, 'unrelated': ['sha256:' + 'a' * 64, 'a' * 40]})

    def test_unrelated_services_never_call_privileged_check(self):
        def forbidden(*args, **kwargs):
            self.fail('unrelated role called OLM check')
        self.assertEqual(A.check_olm_compatibility('/missing', {'roles': {'cleanapp_auth_service': 'auth'}},
                                                forbidden), {'decision': 'unrelated_roles'})

    def test_root_check_receipt_binds_exact_release_and_scope(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'manifest.json'
            raw = json.dumps(self.manifest).encode()
            path.write_bytes(raw)
            receipt = {'kind': 'cleanapp_olm_deploy_check.v1', 'guarded_roles': [self.role],
                       'manifest_sha256': hashlib.sha256(raw).hexdigest(), 'decision': 'unchanged'}
            def run(args, **kwargs):
                self.assertEqual(args[:3], ['sudo', '-n', A.OLM_DEPLOY_GUARD])
                return json.dumps(receipt).encode()
            A.check_olm_compatibility(path, self.manifest, run)
            for key, value in (('manifest_sha256', '0' * 64), ('guarded_roles', []), ('decision', 'accept_any_image')):
                original = receipt[key]
                receipt[key] = value
                with self.subTest(key=key), self.assertRaises(ValueError):
                    A.check_olm_compatibility(path, self.manifest, run)
                receipt[key] = original
            path.write_text(json.dumps({**self.manifest, 'source_sha': 'c' * 40}))
            with self.assertRaisesRegex(ValueError, 'changed before'):
                A.check_olm_compatibility(path, self.manifest, run)

    def test_missing_gate_is_not_treated_as_no_installed_import(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'manifest.json'
            path.write_text(json.dumps(self.manifest))
            def missing(*args, **kwargs):
                raise RuntimeError('fixed root check unavailable')
            with self.assertRaisesRegex(RuntimeError, 'unavailable'):
                A.check_olm_compatibility(path, self.manifest, missing)

    def test_symlink_and_duplicate_json_artifacts_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'target'
            path.write_text('{}')
            alias = Path(directory) / 'link'
            alias.symlink_to(path)
            with self.assertRaises((OSError, ValueError)):
                G.read_file(alias, trusted=True)
        with self.assertRaises(ValueError):
            G.document('{"roles":{},"roles":{}}')

    def test_installed_pin_map_is_parsed_without_executing_helper(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            helper = root / 'app/native_import/guard_review/refresh_live_guards.py'
            raw = ('raise RuntimeError("must never execute")\nEXPECTED=' + repr(self.pins)).encode()
            policy = {'guard_refresh': {'path': str(helper), 'sha256': hashlib.sha256(raw).hexdigest()}}
            def read(path, **kwargs):
                return raw if path == helper else json.dumps(policy).encode()
            with patch.object(G, 'read_file', read):
                pins, digest = G.reviewed_pins(root)
            self.assertEqual(pins, self.pins)
            self.assertEqual(digest, policy['guard_refresh']['sha256'])


if __name__ == '__main__':
    unittest.main()
