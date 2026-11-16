"""Lifecycle/budget tests: uncertain state must never restart an unrelated job."""
import contextlib
import datetime as dt
import importlib.util
from pathlib import Path
import subprocess
import sys
import types
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('solvesf_controller_tested', Path(__file__).with_name('service.py'))
service = importlib.util.module_from_spec(spec)
spec.loader.exec_module(service)
RUNNING = {'ActiveState': 'activating', 'MainPID': '123', 'ControlPID': '0'}
STOPPED = {'ActiveState': 'inactive', 'MainPID': '0', 'ControlPID': '0'}

def config():
    start = service.now() - dt.timedelta(hours=1)
    return {'kind': 'cleanapp_solvesf_controller.v1', 'starts_at': start.isoformat(),
            'expires_at': (start + dt.timedelta(days=7)).isoformat(), 'window': 'today',
            'maximum_native_cases': 500, 'maximum_archive_bytes': service.MAX_ARCHIVE,
            'cases_per_run': 5, 'interval_seconds': 3600}

class PausePath:
    def __init__(self, records):
        self.records = records
    def exists(self):
        return 'pause' in self.records
    def unlink(self):
        self.records.pop('pause')

class Lifecycle(unittest.TestCase):
    def test_absolute_lifetime_case_archive_and_hourly_envelopes(self):
        good = config()
        self.assertEqual(service.validate_limits(good, service.now()), 'eligible')
        for change in ({'maximum_native_cases': 501}, {'cases_per_run': 6}, {'interval_seconds': 3599},
                       {'window': '3d'}, {'maximum_archive_bytes': service.MAX_ARCHIVE + 1},
                       {'expires_at': (service.timestamp(good['starts_at']) + dt.timedelta(days=8)).isoformat()}):
            with self.subTest(change=change), self.assertRaises(service.Deferred):
                service.validate_limits({**good, **change}, service.now())

    def test_uncertain_archive_reservation_stays_spent_and_stops_next_stage(self):
        cfg = config(); state = {'archive_bytes': 0}
        rows = [{'media_urls': ['photo'] * 4}] * 5
        reserved = service.reserve_archive(state, rows, cfg)
        self.assertGreater(reserved, 400 * 1024**2)
        self.assertEqual(state['archive_bytes'], reserved)
        cfg['maximum_archive_bytes'] = reserved
        with self.assertRaises(service.Deferred):
            service.reserve_archive(state, rows, cfg)
        self.assertEqual(state['archive_bytes'], reserved)

    def test_native_controls_cannot_extend_the_seven_day_job_deadline(self):
        cfg = config(); at = service.now()
        cfg['expires_at'] = (at + dt.timedelta(seconds=60)).isoformat()
        self.assertEqual(service.controls_deadline(cfg, at), at + dt.timedelta(seconds=60))
        with self.assertRaises(service.Deferred):
            service.controls_deadline(cfg, at + dt.timedelta(seconds=61))

    def test_foreign_pause_and_missing_integrity_never_authorize_resume(self):
        record = {'kind': 'cleanapp_solvesf_owned_pause.v1', 'config_sha256': 'current',
                  'phase': 'stopped', 'before': RUNNING}
        self.assertEqual(service.resume_decision(record, STOPPED, 'current', True, True), 'resume')
        for sha, files, integrity in (('foreign', True, True), ('current', False, True), ('current', True, False)):
            with self.assertRaises(service.Deferred):
                service.resume_decision(record, STOPPED, sha, files, integrity)

    def test_failed_stop_cannot_turn_unproven_inactive_state_into_resume(self):
        record = {'kind': 'cleanapp_solvesf_owned_pause.v1', 'config_sha256': 'current',
                  'phase': 'requested', 'before': RUNNING}
        self.assertEqual(service.resume_decision(record, RUNNING, 'current', False, False), 'already_running')
        with self.assertRaises(service.Deferred):
            service.resume_decision(record, STOPPED, 'current', True, True)

    def fake_controller(self, records, integrity=True):
        obj = object.__new__(service.Controller)
        obj.config_sha = 'current'; obj.config = config()
        obj.guard = types.SimpleNamespace(document=lambda *_args, **_kw: records['pause'], trusted=lambda *_args, **_kw: 'digest')
        obj.identities = lambda: {'receiver': 'same'}
        obj.integrity = lambda: integrity
        obj.commission = lambda *_args: None
        obj.seen = lambda: set()
        obj.apply = types.SimpleNamespace(read_candidates=lambda *_args: [{'source_id': '123'}])
        return obj

    def run_guarded(self, integrity, stop_failure=False):
        records, events = {}, []
        pause = PausePath(records)
        obj = self.fake_controller(records, integrity)
        current = dict(RUNNING)
        def systemctl(command, **_kw):
            operation = command[1]; events.append(operation)
            if operation == 'stop':
                current.update(STOPPED)
                if stop_failure:
                    raise subprocess.CalledProcessError(1, command)
            if operation == 'start':
                current.update({**RUNNING, 'MainPID': '456'})
            return types.SimpleNamespace(returncode=0)
        def save(path, data):
            self.assertIs(path, pause)
            records['pause'] = data
        with patch.object(service, 'PAUSE', pause), patch.object(service, 'deployment_lock', contextlib.nullcontext), \
             patch.object(service, 'unit_state', side_effect=lambda: dict(current)), patch.object(service, 'write_json', side_effect=save), \
             patch.object(service.subprocess, 'run', side_effect=systemctl), patch.object(service, 'sha', return_value='digest'), \
             patch.object(service, 'run_child', side_effect=service.Deferred('source_guard_refused')):
            with self.assertRaises((service.Deferred, subprocess.CalledProcessError)):
                obj.guarded_apply(Path('/candidate'), Path('/media'))
        return records, events

    def test_guard_refusal_can_resume_owned_pause_only_after_complete_integrity(self):
        records, events = self.run_guarded(True)
        self.assertEqual(events, ['stop', 'start'])
        self.assertFalse(records)

    def test_missing_positive_seed_keeps_owned_pause_and_never_calls_start(self):
        records, events = self.run_guarded(False)
        self.assertEqual(events, ['stop'])
        self.assertEqual(records['pause']['phase'], 'stopped')

    def test_stop_failure_never_calls_apply_or_blind_start(self):
        records, events = self.run_guarded(True, stop_failure=True)
        self.assertEqual(events, ['stop'])
        self.assertEqual(records['pause']['phase'], 'requested')

    def test_source_failure_occurs_before_pause_and_keeps_attempt_cadence(self):
        obj = object.__new__(service.Controller)
        obj.config = config(); obj.recover_pause = lambda: None; obj.seen = lambda: set()
        obj.guarded_apply = lambda *_: self.fail('source failure must not pause OLM')
        state_path = types.SimpleNamespace(exists=lambda: False)
        writes = []
        with patch.object(service, 'STATE', state_path), patch.object(service, 'unit_state', return_value=RUNNING), \
             patch.object(service, 'write_json', side_effect=lambda p, s: writes.append(dict(s))), \
             patch.object(service, 'run_child', side_effect=service.Deferred('source503')):
            with self.assertRaises(service.Deferred):
                obj.cycle()
        self.assertEqual(len(writes), 1)
        self.assertIn('last_attempt_at', writes[0])

    def test_owned_pause_cleanup_precedes_expired_budget_without_source_calls(self):
        obj = object.__new__(service.Controller)
        obj.config = config()
        obj.config['starts_at'] = (service.now() - dt.timedelta(days=8)).isoformat()
        obj.config['expires_at'] = (service.now() - dt.timedelta(days=1, minutes=1)).isoformat()
        events = []; obj.recover_pause = lambda: events.append('recover')
        obj.seen = lambda: self.fail('expired job must not collect or inspect candidates')
        with patch.object(service, 'STATE', types.SimpleNamespace(exists=lambda: False)), \
             patch.object(service, 'run_child', side_effect=AssertionError('no source call')):
            self.assertEqual(obj.cycle()['state'], 'expired')
        self.assertEqual(events, ['recover'])

    def test_timed_out_child_group_is_reaped(self):
        created = []
        original = subprocess.Popen
        def spawn(*args, **kwargs):
            process = original(*args, **kwargs); created.append(process); return process
        with patch.object(service.subprocess, 'Popen', side_effect=spawn):
            with self.assertRaises(subprocess.TimeoutExpired):
                service.run_child([sys.executable, '-c', 'import time; time.sleep(20)'], timeout=0.02)
        self.assertIsNotNone(created[0].poll())

if __name__ == '__main__':
    unittest.main()
