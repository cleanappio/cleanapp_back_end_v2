#!/usr/bin/env python3
"""Finite hourly SolveSF ingestion on the existing AWS host. No DDL or models.

Installation does not enable the timer. Root commissions the schema/canary and
hash-bound test/configuration artifacts before deliberately enabling recurrence.
"""
from __future__ import annotations
import argparse
import contextlib
import datetime as dt
import fcntl
import hashlib
import importlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import sqlite3
import stat
import subprocess
import sys
import time
import uuid

ROOT = Path('/srv/cleanapp-solvesf-import')
OLM = Path('/srv/cleanapp-olm-import')
CONFIG = ROOT / 'evidence/controller.json'
STATE = ROOT / 'state/controller.json'
PAUSE = ROOT / 'state/controller-pause.json'
UNIT = 'olm-native-receiver.service'
TIMER = 'cleanapp-solvesf-import.timer'
LOCK = Path('/opt/cleanapp/deploy/.deployment-and-backup.lock')
MAX_ARCHIVE = 40 * 1024**3

class Deferred(RuntimeError):
    pass

def require(ok, code):
    if not ok:
        raise Deferred(code)

def now():
    return dt.datetime.now(dt.timezone.utc)

def timestamp(value):
    result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(result.tzinfo is not None, 'explicit_utc_time_required')
    return result.astimezone(dt.timezone.utc)

def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temp = path.with_name(path.name + '.new-' + uuid.uuid4().hex)
    with temp.open('x') as stream:
        os.fchmod(stream.fileno(), 0o600)
        json.dump(value, stream, sort_keys=True)
        stream.write('\n'); stream.flush(); os.fsync(stream.fileno())
    os.replace(temp, path)
    fd = os.open(path.parent, os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)

def load_module(name):
    path = ROOT / ('app/' + name + '.py')
    spec = importlib.util.spec_from_file_location('solvesf_controller_' + name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module

def bootstrap_file(path, private=False):
    for item in (path, *path.parents):
        info = item.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
                'controller_bootstrap_not_root_trusted')
    require(stat.S_ISREG(path.lstat().st_mode) and (not private or not path.lstat().st_mode & 0o077),
            'controller_bootstrap_file_permissions')

def validate_limits(config, at):
    start, end = timestamp(config['starts_at']), timestamp(config['expires_at'])
    require(config.get('kind') == 'cleanapp_solvesf_controller.v1' and config.get('window') == 'today'
        and start < end <= start + dt.timedelta(days=7)
        and type(config.get('maximum_native_cases')) is int and 1 <= config['maximum_native_cases'] <= 500
        and type(config.get('cases_per_run')) is int and 1 <= config['cases_per_run'] <= 5
        and type(config.get('interval_seconds')) is int and config['interval_seconds'] >= 3600
        and type(config.get('maximum_archive_bytes')) is int and 0 < config['maximum_archive_bytes'] <= MAX_ARCHIVE,
        'controller_limits_outside_reviewed_envelope')
    return 'not_started' if at < start else 'expired' if at >= end else 'eligible'

def reserve_archive(state, rows, config):
    # Keep reservations on uncertain uploads; this bounds failed/held staging,
    # not merely successfully imported cases. Release excess only after proof.
    budget = sum(len(c['media_urls']) * (20 * 1024**2 + 128 * 1024) + 512 * 1024 for c in rows)
    require(state.get('archive_bytes', 0) + budget <= config['maximum_archive_bytes'], 'archive_budget_exhausted')
    state['archive_bytes'] = state.get('archive_bytes', 0) + budget
    return budget

def controls_deadline(config, at):
    end = timestamp(config['expires_at'])
    require(at < end, 'controller_expired_before_native_apply')
    return min(at + dt.timedelta(minutes=30), end)

def run_child(arguments, timeout=600):
    process = subprocess.Popen(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
    try:
        output, _ = process.communicate(timeout=timeout)
        require(process.returncode == 0 and len(output) <= 2 * 1024**2, 'bounded_child_failed')
        return json.loads(output)
    except BaseException:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL); process.wait()
        raise
    finally:
        process.stdout.close(); process.stderr.close()

def unit_state():
    raw = subprocess.run(['/usr/bin/systemctl', 'show', UNIT, '-p', 'ActiveState', '-p', 'MainPID', '-p', 'ControlPID'],
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
    require(raw.returncode == 0, 'receiver_inventory_failed')
    return dict(line.split('=', 1) for line in raw.stdout.decode().splitlines())

def running(state):
    return state.get('ActiveState') in ('active', 'activating') and int(state.get('MainPID', '0')) > 0

def stopped(state):
    return state.get('ActiveState') in ('inactive', 'failed') and state.get('MainPID') == state.get('ControlPID') == '0'

def resume_decision(record, current, config_sha, files_equal, integrity_ok):
    require(record.get('kind') == 'cleanapp_solvesf_owned_pause.v1' and record.get('config_sha256') == config_sha,
            'pause_not_owned_by_this_controller_configuration')
    if record['phase'] == 'requested' and running(current) and current['MainPID'] == record['before']['MainPID']:
        return 'already_running'
    require(record['phase'] == 'stopped' and stopped(current) and files_equal and integrity_ok,
            'pause_integrity_uncertain_do_not_resume')
    return 'resume'

@contextlib.contextmanager
def deployment_lock():
    fd = os.open(LOCK, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(before.st_uid == 1003 and before.st_nlink == 1 and before.st_mode & 0o777 == 0o600,
                'deployment_lock_identity_changed')
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(os.lstat(LOCK).st_ino == before.st_ino, 'deployment_lock_replaced')
        yield
    finally:
        os.close(fd)

class Controller:
    def __init__(self):
        require(os.geteuid() == 0 and sys.platform.startswith('linux'), 'aws_linux_root_required')
        os.umask(0o077)
        bootstrap_file(CONFIG, private=True)
        require(CONFIG.stat().st_size <= 1024**2, 'controller_config_size_bound')
        self.config = json.loads(CONFIG.read_bytes())
        self.config_sha = sha(CONFIG)
        validate_limits(self.config, now())
        expected = self.config['source_sha256']
        require(set(expected) == {'service.py', 'guard.py', 'apply.py', 'media.py', 'collect.py'}, 'controller_source_manifest_scope')
        for name, digest in expected.items():
            path = ROOT / ('app/' + name)
            bootstrap_file(path)
            require(sha(path) == digest, 'controller_bootstrap_source_digest_mismatch')
        self.guard = load_module('guard')
        self.config = self.guard.document(CONFIG, self.config_sha)
        tests = self.guard.document(ROOT / 'evidence/controller-tests.json', self.config['controller_test_receipt_sha256'])
        require(tests.get('kind') == 'cleanapp_solvesf_controller_tests.v1' and tests.get('passed') is True
                and tests.get('service_sha256') == expected['service.py'] and tests.get('tests_passed', 0) >= 8,
                'current_controller_tests_missing')
        self.media = load_module('media')
        self.apply = self.media.apply
        checker = self.guard.ProductionGuard({'actual_test_receipt_sha256': self.config['actual_test_receipt_sha256']})
        checker.media_module = self.media
        checker.verify_test_receipt()
        self.policy = self.guard.document(OLM / 'evidence/base-policy.json', self.config['olm_base_policy_sha256'])
        lib = OLM / 'app/native_import/apply'
        for name, digest in self.policy['consumer_sources'].items():
            require(Path(name).name == name, 'invalid_native_library_name')
            self.guard.trusted(lib / name, digest)
        sys.path.insert(0, str(lib))
        try:
            self.driver = importlib.import_module('epoch_driver')
            self.transport = importlib.import_module('mysql_transport')
            self.live = importlib.import_module('live_controls')
        finally:
            sys.path.pop(0)
        self.driver.base_policy(OLM / 'evidence/base-policy.json')
        self.driver.verify_host(self.policy)

    def seen(self):
        with self.transport.Mysql(readonly=True) as db:
            self.driver.server_check(db, self.policy)
            self.guard.verify_source_schema(db)
            return {row for row in db.query("SELECT source_id FROM sf311_source_case_ledger WHERE source_namespace='sf311' ORDER BY source_id LIMIT 501")}

    def identities(self):
        paths = [OLM / 'evidence/base-policy.json', OLM / 'evidence/receiver-config.json', OLM / 'state/receiver/receiver.sqlite3']
        wal = Path(str(paths[-1]) + '-wal')
        if wal.exists():
            paths.append(wal)
        return {str(path): self.guard.trusted(path) for path in paths}

    def integrity(self):
        # Another authorized guarded deployment may run between our parent
        # commissioning lock and the child's lock. Policy bytes alone cannot
        # establish that the currently running applications still match it.
        guards = self.driver.run_reviewed(self.policy['guard_refresh'], timeout=180)
        self.live.validate_guard(guards, now())
        write_json(ROOT / 'evidence/controller-resume-guards.json', guards)
        with self.transport.Mysql() as db:
            require(db.scalar("SELECT GET_LOCK('cleanapp-olm-native-import',0)") == '1', 'native_writer_still_active')
            try:
                row = db.query("SELECT COUNT(*),COALESCE(SUM(p.policy_version=1 AND p.source_namespace='sf311' AND "
                    + ' AND '.join('p.' + f + '=1' for f in self.guard.FLAGS)
                    + "),0) FROM sf311_source_case_ledger l LEFT JOIN report_side_effect_policy p ON p.report_seq=l.report_seq "
                    "WHERE l.source_namespace='sf311' AND l.report_seq IS NOT NULL")[0].split('\t')
                if row[0] != row[1]:
                    return False
                refs = db.query("SELECT l.recorded_report_seq,m.original_sha256,m.display_sha256 FROM sf311_source_case_ledger l "
                    "JOIN sf311_source_media_ledger m USING(source_namespace,source_id) WHERE l.source_namespace='sf311' LIMIT 2001")
                require(len(refs) < 2001, 'source_media_inventory_exceeded')
                path = OLM / 'state/dedup/mutation-state.sqlite3'
                self.guard.trusted(path, private=True)
                with contextlib.closing(sqlite3.connect(path.as_uri() + '?mode=ro', uri=True)) as cache:
                    return all(cache.execute('SELECT protected FROM assets WHERE sha256=? AND report_seq=?',
                        (digest, int(seq))).fetchone() == (1,) for raw in refs for seq, *hashes in [raw.split('\t')] for digest in hashes)
            finally:
                db.query("SELECT RELEASE_LOCK('cleanapp-olm-native-import')")

    def recover_pause(self):
        if not PAUSE.exists():
            return
        with deployment_lock():
            record = self.guard.document(PAUSE, self.guard.trusted(PAUSE, private=True))
            current = unit_state()
            equal = record.get('files') == self.identities() if record.get('phase') == 'stopped' else False
            action = resume_decision(record, current, self.config_sha, equal,
                                     self.integrity() if stopped(current) else False)
            if action == 'resume':
                subprocess.run(['/usr/bin/systemctl', 'start', '--no-block', UNIT], check=True,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
                deadline = time.monotonic() + 15
                while not running(unit_state()):
                    require(time.monotonic() < deadline, 'receiver_did_not_resume')
                    time.sleep(1)
            # Removal follows both a verified decision and an observed live PID.
            PAUSE.unlink()

    def commission(self, candidates, media_manifest, files):
        coverage = self.driver.run_reviewed(self.policy['coverage_refresh'], ('--out-dir', self.policy['coverage_refresh']['out_dir'],
            '--budget-seconds', '1200', '--read-rate-mib', '20', '--tiny-lane'))
        require(coverage.get('complete_global_hash_coverage_at_captured_revisions') is True, 'coverage_refresh_incomplete')
        guards = self.driver.run_reviewed(self.policy['guard_refresh'], timeout=180)
        write_json(ROOT / 'evidence/coverage.json', coverage)
        write_json(ROOT / 'evidence/live-guards.json', guards)
        source = OLM / 'state/dedup/mutation-state.sqlite3'
        target = ROOT / 'state/mutation-state.sqlite3'
        with contextlib.closing(sqlite3.connect(source.as_uri() + '?mode=ro', uri=True)) as old:
            with contextlib.closing(sqlite3.connect(target)) as new:
                old.backup(new)
                cursor = new.execute("SELECT value FROM cursor WHERE key='safe'").fetchone()[0]
        os.chmod(target, 0o600)
        controls = {'kind': 'cleanapp_solvesf_apply_controls.v1', 'server_uuid': self.guard.SERVER,
            'expires_at': controls_deadline(self.config, now()).isoformat(), 'candidate_sha256': sha(candidates),
            'media_manifest_sha256': sha(media_manifest), 'apply_source_sha256': sha(ROOT / 'app/apply.py'),
            'guard_source_sha256': sha(ROOT / 'app/guard.py'), 'media_source_sha256': sha(ROOT / 'app/media.py'),
            'collect_source_sha256': sha(ROOT / 'app/collect.py'), 'sql_helper_sha256': sha(self.apply.HELPERS),
            'actual_test_receipt_sha256': self.config['actual_test_receipt_sha256'],
            'collection_state_sha256': sha(ROOT / 'state/canary/ledger.sqlite3'),
            'olm_base_policy_sha256': self.config['olm_base_policy_sha256'], 'olm_unchanged_files': files,
            'live_guard_receipt_sha256': sha(ROOT / 'evidence/live-guards.json'),
            'coverage_receipt_sha256': sha(ROOT / 'evidence/coverage.json'),
            'mutation_state_initial_sha256': sha(target), 'mutation_safe_cursor': int(cursor)}
        write_json(ROOT / 'evidence/controls.json', controls)

    def guarded_apply(self, candidates, media_manifest):
        try:
            with deployment_lock():
                controls_deadline(self.config, now())
                actual_seen = self.seen()
                proposed = self.apply.read_candidates(candidates, sha(candidates))
                require(len(actual_seen | {c['source_id'] for c in proposed}) <= self.config['maximum_native_cases'],
                        'native_case_budget_changed_before_pause')
                before = unit_state()
                require(running(before), 'receiver_already_paused_by_other_operator')
                write_json(PAUSE, {'kind': 'cleanapp_solvesf_owned_pause.v1', 'config_sha256': self.config_sha,
                                  'phase': 'requested', 'before': before, 'requested_at': now().isoformat()})
                subprocess.run(['/usr/bin/systemctl', 'stop', UNIT], check=True,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
                require(stopped(unit_state()), 'receiver_stop_not_quiescent')
                files = self.identities()
                write_json(PAUSE, {'kind': 'cleanapp_solvesf_owned_pause.v1', 'config_sha256': self.config_sha,
                                  'phase': 'stopped', 'before': before, 'files': files})
                self.commission(candidates, media_manifest, files)
            return run_child([sys.executable, str(ROOT / 'app/apply.py'), '--mode', 'apply', '--candidates', str(candidates),
                '--candidate-sha256', sha(candidates), '--media-manifest', str(media_manifest),
                '--media-manifest-sha256', sha(media_manifest)], timeout=600)
        finally:
            self.recover_pause()

    def cycle(self):
        self.recover_pause()  # Cleanup precedes expiry checks, including a prior interrupted run.
        state = self.guard.document(STATE, self.guard.trusted(STATE, private=True)) if STATE.exists() else {'archive_bytes': 0}
        eligibility = validate_limits(self.config, now())
        if eligibility != 'eligible':
            return {'state': eligibility}
        if state.get('last_attempt_at') and (now() - timestamp(state['last_attempt_at'])).total_seconds() < self.config['interval_seconds']:
            return {'state': 'interval_not_elapsed'}
        seen = self.seen()
        if len(seen) >= self.config['maximum_native_cases']:
            return {'state': 'native_case_budget_complete', 'native_cases': len(seen)}
        require(running(unit_state()), 'receiver_unavailable_before_source_staging')
        state['last_attempt_at'] = now().isoformat(); write_json(STATE, state)
        run_child([sys.executable, str(ROOT / 'app/collect.py'), '--state-dir', str(ROOT / 'state/canary'),
                   '--max-reports', '5', '--window', 'today'], timeout=300)
        source = ROOT / 'state/canary/candidates.jsonl'
        self.guard.trusted(source, private=True)
        require(source.stat().st_size <= 64 * 1024**2, 'candidate_inventory_bound')
        rows = []
        with source.open() as stream:
            for line in stream:
                c = self.apply.valid_candidate(json.loads(line))
                if c['source_id'] not in seen:
                    rows.append(c)
                if len(rows) >= min(self.config['cases_per_run'], self.config['maximum_native_cases'] - len(seen)):
                    break
        if not rows:
            return {'state': 'no_new_eligible_cases'}
        batch = ROOT / ('state/batches/' + now().strftime('%Y%m%dT%H%M%SZ'))
        batch.mkdir(parents=True, mode=0o700)
        candidates = batch / 'candidates.jsonl'
        candidates.write_text(''.join(self.apply.canonical(c) + '\n' for c in rows)); candidates.chmod(0o600)
        reserved = reserve_archive(state, rows, self.config); write_json(STATE, state)
        media_manifest = batch / 'media.json'
        staging_seconds = min(600, max(1, int((timestamp(self.config['expires_at']) - now()).total_seconds())))
        controls_deadline(self.config, now())
        run_child([sys.executable, str(ROOT / 'app/media.py'), '--execute', '--candidates', str(candidates),
                   '--candidate-sha256', sha(candidates), '--output', str(media_manifest)], timeout=staging_seconds)
        manifest = self.guard.document(media_manifest, sha(media_manifest))
        actual = sum(case['source_record_ref']['bytes'] + case['binding_ref']['bytes']
            + sum(ref[k]['bytes'] for ref in case['items'] for k in ('original', 'display')) for case in manifest['cases'].values())
        require(actual <= reserved, 'staged_media_exceeded_reserved_budget')
        state['archive_bytes'] -= reserved - actual; write_json(STATE, state)
        result = self.guarded_apply(candidates, media_manifest)
        state['last_success_at'] = now().isoformat(); write_json(STATE, state)
        return {'state': 'batch_complete', 'cases': len(result['rows']), 'archive_bytes': state['archive_bytes']}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    action = parser.add_mutually_exclusive_group()
    action.add_argument('--run', action='store_true', help='Execute one bounded, guarded cycle')
    action.add_argument('--check', action='store_true', help='Check configuration and native inventory without source collection (default)')
    args = parser.parse_args()
    def terminate(*_):
        raise Deferred('controller_termination_requested')
    signal.signal(signal.SIGTERM, terminate); signal.signal(signal.SIGINT, terminate)
    controller = Controller()
    if not args.run:
        print(json.dumps({'state': 'configuration_verified', 'eligibility': validate_limits(controller.config, now()),
                          'native_cases': len(controller.seen()), 'publication_performed': False})); return
    lockpath = ROOT / 'state/controller.lock'
    with lockpath.open('a+') as lock:
        os.fchmod(lock.fileno(), 0o600); fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        try:
            result = controller.cycle()
        except Exception:
            result = {'state': 'deferred', 'owned_pause_requires_attention': PAUSE.exists()}
        if result.get('state') in ('expired', 'native_case_budget_complete'):
            disabled = subprocess.run(['/usr/bin/systemctl', 'disable', '--now', TIMER],
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
            result['timer_disabled'] = disabled.returncode == 0
        write_json(ROOT / 'evidence/controller-last-run.json', {'at_utc': now().isoformat(), **result})
        print(json.dumps(result))
        if result.get('owned_pause_requires_attention'):
            raise SystemExit(1)

if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'state': 'controller_refused', 'error_type': type(error).__name__,
                          'owned_pause_requires_attention': PAUSE.exists()}))
        raise SystemExit(1)
