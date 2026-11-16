#!/usr/bin/env python3
"""Fixed-path, root-only SolveSF commissioning. No arbitrary host/SQL/commands.

Actions are explicit: stage, test, configure, migrate, cycle, replay, enable,
status. Each prints only controlled metadata; raw test output stays private.
"""
from __future__ import annotations
import argparse
import contextlib
import datetime as dt
import fcntl
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import sys
import tarfile

ROOT = Path('/srv/cleanapp-solvesf-import')
SOURCE = ROOT / 'source'
ARCHIVE = Path('/tmp/solvesf-source-working-20261116.tgz')
PYTHON = ROOT / 'venv/bin/python'
SCRIPTS = 'scripts/solvesf-import/'
HELPERS = 'scripts/historical-imports/dataunion_0_1_0/'
CORE = ('apply.py', 'guard.py', 'media.py', 'collect.py', 'service.py')
APP = (*CORE, 'commission.py', 'verify_live.py')
TESTS = ('test_apply.py', 'test_guard.py', 'test_media.py', 'test_collect.py', 'test_service.py', 'test_commission.py', 'test_verify_live.py')
UNITS = ('cleanapp-solvesf-import.service', 'cleanapp-solvesf-import.timer')
ALLOWED = ({SCRIPTS + p for p in (*APP, *TESTS, *UNITS, 'README.md', 'requirements.txt')}
           | {HELPERS + p for p in ('candidate_data.py', 'mysql_client.py', 'integration_test.py')}
           | {'go-common/sf311import/schema.sql', 'go-common/sf311import/migrate.go', 'report-listener/database/migrate.go'})
REQUIRED = ({SCRIPTS + p for p in (*APP, *TESTS, *UNITS)} | {HELPERS + p for p in ('candidate_data.py', 'mysql_client.py', 'integration_test.py')}
            | {'go-common/sf311import/schema.sql'})
SERVER = 'f2c484f6-3c5d-11ef-aace-0242ac120003'
FIXTURE = 'ca-import-test-mysql'
FIXTURE_IMAGE = 'sha256:b8702932301d5d5e5abc048470ae857ae83aaa1f96c22dd2cb4629ad99a53895'
STEP = '0036_sf311_source_ledgers'
DESCRIPTION = 'create case-based SF311 import provenance and media ledgers'


def require(ok, code):
    if not ok:
        raise ValueError(code)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def trusted(path):
    for part in (Path(path), *Path(path).parents):
        s = part.lstat()
        require(s.st_uid == 0 and not stat.S_ISLNK(s.st_mode) and not s.st_mode & 0o022, 'untrusted_commission_path')


def write(path, data):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    trusted(path.parent)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        os.fchown(stream.fileno(), 0, 0)
        os.fchmod(stream.fileno(), 0o600)
        stream.write(data); stream.flush(); os.fsync(stream.fileno())


def module(name):
    path = ROOT / ('app/' + name + '.py'); trusted(path)
    spec = importlib.util.spec_from_file_location('solvesf_commission_' + name, path)
    result = importlib.util.module_from_spec(spec); sys.modules[spec.name] = result
    spec.loader.exec_module(result)
    return result


def command(args, timeout=30, input=None):
    r = subprocess.run(args, input=input, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    require(r.returncode == 0, 'fixed_commission_command_failed')
    return r.stdout


def unpack(blob, expected):
    require(re.fullmatch('[0-9a-f]{64}', expected or '') and hashlib.sha256(blob).hexdigest() == expected, 'source_archive_digest_mismatch')
    require(len(blob) <= 8 * 1024**2, 'source_archive_size_bound')
    files = {}
    with tarfile.open(fileobj=io.BytesIO(blob), mode='r:gz') as archive:
        for member in archive:
            name = member.name.removeprefix('./'); parts = PurePosixPath(name).parts
            require(not name.startswith('/') and '..' not in parts, 'unsafe_archive_path')
            if any(p.startswith('._') for p in parts) or (parts and parts[0] == '__MACOSX'):
                continue
            if member.isdir():
                require(name in ('', '.') or any(p.startswith(name.rstrip('/') + '/') for p in ALLOWED), 'unexpected_archive_directory')
                continue
            require(member.isfile() and name in ALLOWED and name not in files and 0 <= member.size <= 2 * 1024**2, 'unexpected_archive_member')
            with archive.extractfile(member) as stream:
                files[name] = stream.read(member.size + 1)
            require(len(files[name]) == member.size, 'source_archive_short_read')
    require(REQUIRED <= set(files), 'incomplete_source_archive')
    return files


def source_hashes():
    return {name: sha(ROOT / ('app/' + name)) for name in CORE}


def actual_hashes():
    result = {name: sha(ROOT / ('app/' + name)) for name in ('apply.py', 'guard.py', 'media.py', 'collect.py')}
    result.update(sql_helper=sha(ROOT / 'historical-imports/dataunion_0_1_0/candidate_data.py'), **{'schema.sql': sha(ROOT / 'app/schema.sql')})
    return result


def stage_idle():
    for unit in UNITS:
        r = subprocess.run(['/usr/bin/systemctl', 'show', unit, '-p', 'LoadState', '-p', 'ActiveState'],
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
        state = dict(line.split('=', 1) for line in r.stdout.decode().splitlines() if '=' in line)
        require((r.returncode == 0 or state.get('LoadState') == 'not-found')
                and state.get('ActiveState') in ('inactive', 'failed'), 'stop_solvesf_controller_before_staging')


def stage(expected):
    require(not (ROOT / 'state/controller-pause.json').exists(), 'owned_pause_requires_recovery_before_staging')
    stage_idle()
    files = unpack(ARCHIVE.read_bytes(), expected)
    for name, data in files.items():
        write(SOURCE / name, data)
    for name in APP:
        write(ROOT / ('app/' + name), files[SCRIPTS + name])
    write(ROOT / 'app/schema.sql', files['go-common/sf311import/schema.sql'])
    for name in ('candidate_data.py', 'mysql_client.py'):
        write(ROOT / ('historical-imports/dataunion_0_1_0/' + name), files[HELPERS + name])
    return {'staged': len(files), 'source_archive_sha256': expected}


def fixture_reset():
    info = json.loads(command(['docker', 'inspect', FIXTURE]))[0]
    require(info['HostConfig']['NetworkMode'] == 'none' and info['Image'] == FIXTURE_IMAGE
            and 0 < info['HostConfig']['Memory'] <= 1024**3
            and info['Config'].get('Labels', {}).get('cleanapp.solvesf.fixture') == 'true', 'wrong_isolated_fixture')
    shell = 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot --batch --skip-column-names'
    base = ['docker', 'exec', '-i', FIXTURE, 'sh', '-c', shell]
    identity = command(base, input=b'SELECT @@server_uuid,VERSION();\n').decode().strip().split('\t')
    require(len(identity) == 2 and identity[0] != SERVER and identity[1] == '8.0.44', 'fixture_server_identity_changed')
    command(base, input=b'DROP DATABASE IF EXISTS cleanapp_dataunion_import_test; CREATE DATABASE cleanapp_dataunion_import_test CHARACTER SET utf8mb4;\n')


def tests():
    fixture_reset(); before = actual_hashes(); services = source_hashes()
    for name in (*APP, *TESTS):
        trusted(SOURCE / (SCRIPTS + name))
        if name in APP:
            require(sha(SOURCE / (SCRIPTS + name)) == sha(ROOT / ('app/' + name)), 'app_source_tree_drift')
    for name in ('candidate_data.py', 'mysql_client.py'):
        require(sha(SOURCE / (HELPERS + name)) == sha(ROOT / ('historical-imports/dataunion_0_1_0/' + name)), 'tested_sql_helper_drift')
    require(sha(SOURCE / 'go-common/sf311import/schema.sql') == sha(ROOT / 'app/schema.sql'), 'tested_schema_drift')
    log = ROOT / 'evidence/actual-test.log'; log.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    env = os.environ.copy(); env.update(SOLVESF_TEST_MYSQL='1', PYTHONDONTWRITEBYTECODE='1')
    env.pop('PYTHONPATH', None); env.pop('PYTHONHOME', None)
    with log.open('wb') as output:
        os.fchmod(output.fileno(), 0o600)
        r = subprocess.run([str(PYTHON), '-W', 'error', '-m', 'unittest', 'discover', '-s', SCRIPTS.rstrip('/'), '-p', 'test_*.py', '-v'],
                           cwd=SOURCE, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=900)
    text = log.read_text(errors='replace'); count = re.search(r'Ran (\d+) tests', text)
    require(r.returncode == 0 and count and '\nOK\n' in text and 'skipped' not in text
            and actual_hashes() == before and source_hashes() == services, 'actual_tests_failed_or_source_changed')
    service = module('service')
    service.write_json(ROOT / 'evidence/actual-tests.json', {'kind': 'cleanapp_solvesf_actual_tests.v1',
        'actual_mysql_passed': True, 'guard_tests_passed': True, 'media_tests_passed': True,
        'source_sha256': before, 'tests_passed': int(count[1]), 'log_sha256': sha(log)})
    service.write_json(ROOT / 'evidence/controller-tests.json', {'kind': 'cleanapp_solvesf_controller_tests.v1',
        'passed': True, 'service_sha256': services['service.py'], 'tests_passed': int(count[1]), 'log_sha256': sha(log)})
    return {'tests_passed': int(count[1]), 'actual_mysql_passed': True}


def configure(revision):
    require(re.fullmatch('[0-9a-f]{40}', revision or ''), 'exact_main_revision_required')
    service = module('service'); require(not service.PAUSE.exists(), 'owned_pause_blocks_reconfiguration')
    start = service.now()
    value = {'kind': 'cleanapp_solvesf_controller.v1', 'source_revision': revision, 'window': 'today',
        'starts_at': start.isoformat(), 'expires_at': (start + dt.timedelta(days=7)).isoformat(),
        'maximum_native_cases': 500, 'cases_per_run': 5, 'interval_seconds': 3600, 'maximum_archive_bytes': 40 * 1024**3,
        'source_sha256': source_hashes(), 'actual_test_receipt_sha256': sha(ROOT / 'evidence/actual-tests.json'),
        'controller_test_receipt_sha256': sha(ROOT / 'evidence/controller-tests.json'),
        'olm_base_policy_sha256': sha(service.OLM / 'evidence/base-policy.json')}
    if service.CONFIG.exists():
        old = json.loads(service.CONFIG.read_bytes())
        require(old.get('source_revision') == revision and old.get('source_sha256') == value['source_sha256'], 'configuration_already_exists_do_not_extend_lifetime')
        require(old.get('olm_base_policy_sha256') == value['olm_base_policy_sha256'], 'existing_policy_binding_requires_review')
        for name in ('actual_test_receipt_sha256', 'controller_test_receipt_sha256'):
            old[name] = value[name]
        service.write_json(service.CONFIG, old)
        service.Controller()
        return {'configured': True, 'existing_deadline_preserved': True, 'test_receipt_bindings_refreshed': True}
    service.write_json(service.CONFIG, value)
    service.Controller()  # Verify host, exact sources and actual test receipts; no seen()/DDL yet.
    return {'configured': True, 'expires_at': value['expires_at']}


def error_summary(error):
    result = {'error_type': type(error).__name__}
    if isinstance(error, ValueError) and len(error.args) == 1 and isinstance(error.args[0], str) and re.fullmatch('[a-z0-9_]{1,120}', error.args[0]):
        result['error_code'] = error.args[0]
    code = getattr(error, 'code', None)
    if type(code) is int and 100 <= code <= 99999:
        result['mysql_code'] = code
    return result


def release_migration_lock(db, primary):
    # The reviewed transport closes itself after SQL errors, releasing locks.
    if not getattr(db, '_closed', False):
        try:
            db.query("SELECT RELEASE_LOCK('cleanapp-olm-native-import')")
        except BaseException:
            if primary is None:
                raise


def recover_migration(c, primary):
    try:
        c.recover_pause()
    except BaseException as recovery:
        if primary is None:
            raise
        # Preserve the primary failure while retaining safe recovery diagnostics.
        with contextlib.suppress(Exception):
            write(ROOT / 'evidence/migration-failure.json', json.dumps({
                'primary': error_summary(primary), 'recovery': error_summary(recovery),
                'recovery_verified': False}, sort_keys=True).encode() + b'\n')


def migrate():
    service = module('service'); c = service.Controller()
    primary = None
    try:
        with service.deployment_lock():
            require(not service.PAUSE.exists(), 'owned_pause_already_exists')
            before = service.unit_state(); require(service.running(before), 'receiver_not_owned_running')
            record = {'kind': 'cleanapp_solvesf_owned_pause.v1', 'config_sha256': c.config_sha,
                      'phase': 'requested', 'before': before, 'requested_at': service.now().isoformat()}
            service.write_json(service.PAUSE, record)
            command(['/usr/bin/systemctl', 'stop', service.UNIT], timeout=180)
            require(service.stopped(service.unit_state()), 'receiver_stop_not_quiescent')
            service.write_json(service.PAUSE, {**record, 'phase': 'stopped', 'files': c.identities()})
            with c.transport.Mysql() as db:
                c.driver.server_check(db, c.policy)
                require(db.scalar("SELECT GET_LOCK('cleanapp-olm-native-import',0)") == '1', 'native_advisory_lock_busy')
                sql_failure = None
                try:
                    schema = (ROOT / 'app/schema.sql').read_text()
                    norm = lambda x: ' '.join(re.sub(r'\bIF\s+NOT\s+EXISTS\b', '', x, flags=re.I).split())
                    require(norm(schema) == norm(c.apply.LEDGER_DDL), 'canonical_schema_writer_drift')
                    for statement in schema.split(';'):
                        if statement.strip(): db.query(statement)
                    c.guard.verify_source_schema(db)
                    q = c.apply.sql_string
                    found = db.query('SELECT description FROM go_schema_migrations WHERE service_name=' + q('report-listener') + ' AND step_id=' + q(STEP))
                    require(not found or found == [DESCRIPTION], 'migration_record_changed')
                    if not found:
                        require(db.scalar('SELECT COUNT(*) FROM sf311_source_case_ledger') == '0'
                                and db.scalar('SELECT COUNT(*) FROM sf311_source_media_ledger') == '0', 'unowned_existing_native_import')
                        db.query('INSERT INTO go_schema_migrations(service_name,step_id,description) VALUES(' + ','.join(map(q, ('report-listener', STEP, DESCRIPTION))) + ')')
                except BaseException as error:
                    sql_failure = error
                    raise
                finally:
                    release_migration_lock(db, sql_failure)
    except BaseException as error:
        primary = error
        raise
    finally:
        recover_migration(c, primary)
    service.write_json(ROOT / 'evidence/migration.json', {'step_id': STEP, 'schema_sha256': sha(ROOT / 'app/schema.sql'), 'verified': True})
    return {'migration_verified': True, 'step_id': STEP}


def execute(action):
    service = module('service'); c = service.Controller()
    lock = ROOT / 'state/controller.lock'; lock.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with lock.open('a+') as stream:
        os.fchmod(stream.fileno(), 0o600); fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if action == 'cycle':
            result = c.cycle()
            service.write_json(ROOT / 'evidence/controller-last-run.json', {'at_utc': service.now().isoformat(), **result})
            return result
        batches = sorted(p for p in (ROOT / 'state/batches').iterdir() if re.fullmatch('[0-9]{8}T[0-9]{6}Z', p.name))
        require(batches, 'no_batch_available_for_replay'); batch = batches[-1]
        for path in (batch, batch / 'candidates.jsonl', batch / 'media.json'): trusted(path)
        result = c.guarded_apply(batch / 'candidates.jsonl', batch / 'media.json')
        result['actions'] = {key: sum(row['action'] == key for row in result['rows']) for key in ('inserted', 'replay_skip')}
        service.write_json(ROOT / 'evidence/replay.json', result)
        return {'replayed': len(result['rows']), 'actions': result['actions']}


def sandbox_command(unit_text):
    """Mirror every reviewed service restriction, changing only --run to --check."""
    import shlex
    allowed = {'Type', 'User', 'Group', 'UMask', 'WorkingDirectory', 'TimeoutStartSec', 'TimeoutStopSec',
        'KillMode', 'MemoryMax', 'CPUQuota', 'Nice', 'NoNewPrivileges', 'PrivateTmp', 'ProtectSystem',
        'ProtectHome', 'ReadWritePaths', 'ProtectKernelTunables', 'ProtectKernelModules',
        'ProtectControlGroups', 'RestrictSUIDSGID', 'LockPersonality'}
    properties, environment, section, executable = {}, {}, None, None
    for raw in unit_text.splitlines():
        line = raw.strip()
        if not line or line.startswith(('#', ';')): continue
        if line.startswith('[') and line.endswith(']'):
            section = line[1:-1]; continue
        if section != 'Service': continue
        require('=' in line, 'unsupported_sandbox_unit_line')
        key, value = line.split('=', 1)
        if key == 'Environment':
            for assignment in shlex.split(value):
                require('=' in assignment, 'invalid_sandbox_environment')
                name, setting = assignment.split('=', 1)
                require(name not in environment, 'duplicate_sandbox_environment')
                environment[name] = setting
        elif key == 'ExecStart':
            require(executable is None, 'duplicate_sandbox_executable')
            executable = shlex.split(value)
        else:
            require(key in allowed and key not in properties, 'unreviewed_sandbox_service_setting')
            properties[key] = value
    require(set(properties) == allowed and executable == [str(PYTHON), str(ROOT / 'app/service.py'), '--run'],
            'sandbox_service_profile_incomplete')
    require(properties['Type'] == 'oneshot' and properties['User'] == properties['Group'] == 'root'
            and properties['ProtectSystem'] == 'strict' and properties['PrivateTmp'] == 'true'
            and properties['NoNewPrivileges'] == 'false' and properties['MemoryMax'] == '1G'
            and properties['CPUQuota'] == '25%' and properties['UMask'] == '0077', 'sandbox_limits_changed')
    require(environment == {'PYTHONUNBUFFERED': '1', 'PYTHONDONTWRITEBYTECODE': '1',
        'AWS_DEFAULT_REGION': 'us-east-2', 'AWS_SHARED_CREDENTIALS_FILE': '/dev/null',
        'AWS_CONFIG_FILE': '/dev/null'}, 'sandbox_credential_environment_changed')
    args = ['/usr/bin/systemd-run', '--unit=cleanapp-solvesf-import-sandbox-check', '--wait', '--pipe', '--collect']
    args.extend('--property=' + key + '=' + value for key, value in properties.items())
    args.extend('--setenv=' + key + '=' + value for key, value in environment.items())
    return args + [str(PYTHON), str(ROOT / 'app/service.py'), '--check']


def sandbox_check():
    service = module('service'); c = service.Controller()
    source = SOURCE / (SCRIPTS + UNITS[0]); trusted(source)
    args = sandbox_command(source.read_text())
    target = 'cleanapp-solvesf-import-sandbox-check.service'
    present = subprocess.run(['/usr/bin/systemctl', 'show', target, '-p', 'LoadState', '--value'],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
    require(present.returncode in (0, 1, 4) and present.stdout.strip() == b'not-found', 'sandbox_check_unit_already_exists')
    try:
        output = command(args, timeout=120)
        result = json.loads(output)
        require(result.get('state') == 'configuration_verified' and result.get('publication_performed') is False
                and type(result.get('native_cases')) is int and 0 <= result['native_cases'] <= 500,
                'sandbox_service_check_failed')
    except BaseException:
        # This name was absent before our fixed start. Cleanup never targets
        # an application, OLM, the real timer, or another preexisting unit.
        subprocess.run(['/usr/bin/systemctl', 'stop', target], stdout=subprocess.PIPE,
                       stderr=subprocess.PIPE, timeout=30)
        raise
    proof = {'kind': 'cleanapp_solvesf_sandbox_check.v1', 'passed': True, 'config_sha256': c.config_sha,
        'service_sha256': sha(ROOT / 'app/service.py'), 'unit_sha256': sha(source),
        'checked_at_utc': service.now().isoformat(), 'result': result}
    service.write_json(ROOT / 'evidence/sandbox-check.json', proof)
    return {'sandbox_check_passed': True, 'publication_performed': False, 'native_cases': result['native_cases']}


def enable():
    service = module('service'); c = service.Controller()
    require(service.validate_limits(c.config, service.now()) == 'eligible', 'controller_not_eligible_for_enable')
    require(not service.PAUSE.exists(), 'owned_pause_blocks_timer_enable')
    sandbox = c.guard.document(ROOT / 'evidence/sandbox-check.json', sha(ROOT / 'evidence/sandbox-check.json'))
    checked = dt.datetime.fromisoformat(sandbox['checked_at_utc'].replace('Z', '+00:00'))
    require(sandbox.get('kind') == 'cleanapp_solvesf_sandbox_check.v1' and sandbox.get('passed') is True
        and sandbox.get('config_sha256') == c.config_sha
        and sandbox.get('service_sha256') == sha(ROOT / 'app/service.py')
        and sandbox.get('unit_sha256') == sha(SOURCE / (SCRIPTS + UNITS[0]))
        and checked.tzinfo is not None and dt.timedelta(0) <= dt.datetime.now(dt.timezone.utc) - checked <= dt.timedelta(minutes=30)
        and sandbox.get('result', {}).get('publication_performed') is False,
        'actual_current_systemd_sandbox_check_required')
    last = c.guard.document(ROOT / 'evidence/controller-last-run.json', sha(ROOT / 'evidence/controller-last-run.json'))
    proof = c.guard.document(ROOT / 'evidence/live-canary.json', sha(ROOT / 'evidence/live-canary.json'))
    require(last.get('state') == 'batch_complete' and last.get('cases', 0) > 0
            and proof.get('kind') == 'cleanapp_solvesf_live_canary.v1' and proof.get('passed') is True
            and proof.get('config_sha256') == c.config_sha
            and proof.get('controller_run_sha256') == sha(ROOT / 'evidence/controller-last-run.json')
            and proof.get('public_reports_verified') is True and proof.get('quiet_effects_verified') is True,
            'actual_first_cycle_and_live_public_canary_proof_required')
    # cycle() already proved stopped-only integrity before its owned resume.
    # Reacquiring the native writer lock now would race the running OLM epoch.
    require(service.running(service.unit_state()) and c.seen(), 'running_receiver_and_native_inventory_required')
    for name in UNITS:
        source = SOURCE / (SCRIPTS + name); trusted(source)
        write(Path('/etc/systemd/system') / name, source.read_bytes())
    command(['/usr/bin/systemctl', 'daemon-reload'])
    command(['/usr/bin/systemctl', 'enable', '--now', service.TIMER])
    return {'timer_enabled': True, 'expires_at': c.config['expires_at']}


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('action', choices=('stage', 'test', 'configure', 'migrate', 'cycle', 'replay', 'sandbox-check', 'enable', 'status', 'check'))
    p.add_argument('--sha256'); p.add_argument('--source-revision')
    args = p.parse_args(); require(os.geteuid() == 0 and sys.platform.startswith('linux'), 'root_linux_operator_required')
    trusted(Path(__file__).resolve())
    if args.action == 'stage': result = stage(args.sha256)
    elif args.action == 'test': result = tests()
    elif args.action == 'configure': result = configure(args.source_revision)
    elif args.action == 'migrate': result = migrate()
    elif args.action in ('cycle', 'replay'): result = execute(args.action)
    elif args.action == 'sandbox-check': result = sandbox_check()
    elif args.action == 'enable': result = enable()
    else:
        service = module('service'); c = service.Controller()
        result = {'configuration_verified': True, 'native_cases': len(c.seen()), 'owned_pause': service.PAUSE.exists()}
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    try: main()
    except Exception as error:
        result = {'commission_refused': True, **error_summary(error)}
        print(json.dumps(result))
        raise SystemExit(1)
