#!/usr/bin/env python3
"""Restore only the machine analyzer on the admitted AWS runtime; plan by default.

The existing private human-worker environment is copied in host memory into the
private baseline. No environment values or resolved Compose output are printed.
Run only after the reviewed linux/amd64 image has been loaded on the host.
"""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import stat
import sys
import uuid

from register_epc import (admitted_host, command, digest, json_bytes, private_read,
                          prepare_restore, restore_files, write_file)
from runtime_roles import (CURRENT_SERVICES, MACHINE_ANALYSIS_SERVICE,
                           MACHINE_ANALYSIS_SERVICES, PROJECT, declared_roles,
                           expected_container, require)

PACKAGE = Path(__file__).resolve().parent
BASELINE = Path('/opt/cleanapp/private/compose-target.json')
DEPLOY = Path('/opt/cleanapp/deploy')
LIBRARY = Path('/usr/local/lib/cleanapp-backup')
RUNTIME_ROLES_TARGET = LIBRARY / 'runtime_roles.py'
SCOPE = Path('/etc/cleanapp-backup/weekly-config-scope.json')
LOCK = DEPLOY / '.deployment-and-backup.lock'
RECEIPTS = DEPLOY / 'aws-deploy'
PINS = [DEPLOY / 'docker-compose.aws-backend-images.yml',
        DEPLOY / 'docker-compose.aws-frontend-images.yml']
HUMAN_SERVICE = 'cleanapp_report_analyze_human'
ENV_OVERRIDES = {
    'RABBITMQ_EXCHANGE': 'cleanapp-exchange',
    'RABBITMQ_QUEUE': 'report-analysis-queue',
    'RABBITMQ_RAW_REPORT_ROUTING_KEY': 'report.raw',
    'RABBITMQ_ANALYSED_REPORT_ROUTING_KEY': 'report.analysed',
    'RABBITMQ_PREFETCH_COUNT': '1',
    'RABBITMQ_CONCURRENCY': '1',
    'EVIDENCE_WORKER_ENABLED': 'false',
    'ENRICH_EXTERNAL_DIGITAL_ENABLED': 'false',
}


def validate_identity(image_id, source_sha):
    require(re.fullmatch(r'sha256:[a-f0-9]{64}', image_id or ''), 'Expected immutable image ID')
    require(re.fullmatch(r'[a-f0-9]{40}', source_sha or ''), 'Expected exact reviewed source SHA')


def service_definition(baseline, image_id):
    require(re.fullmatch(r'sha256:[a-f0-9]{64}', image_id or ''), 'Expected immutable image ID')
    human = baseline['services'][HUMAN_SERVICE]
    environment = human.get('environment')
    require(isinstance(environment, dict) and environment and
            all(isinstance(key, str) and isinstance(value, str) for key, value in environment.items()),
            'Expected the existing protected human-worker environment dictionary')
    environment = {**environment, **ENV_OVERRIDES}
    port = environment.get('PORT', '8080')
    require(port.isascii() and port.isdigit() and 0 < int(port) <= 65535,
            'Expected a valid existing analyzer HTTP port')
    require('deployer_default' in baseline.get('networks', {}), 'Reviewed analyzer network is missing')
    health = ('wget -q -T 5 -O - http://127.0.0.1:' + port + '/api/v3/health | '
              'grep -Eq \'"rabbitmq_connected"[[:space:]]*:[[:space:]]*true\'')
    return {
        'image': image_id, 'pull_policy': 'never',
        'container_name': MACHINE_ANALYSIS_SERVICE,
        'restart': 'unless-stopped', 'stop_grace_period': '120s',
        'environment': environment, 'networks': {'deployer_default': {}},
        'healthcheck': {'test': ['CMD-SHELL', health], 'interval': '10s',
                        'timeout': '8s', 'retries': 6, 'start_period': '20s'},
    }


def prepare(baseline, image_id):
    declared_roles(baseline)
    require(set(baseline['services']) in (CURRENT_SERVICES, MACHINE_ANALYSIS_SERVICES),
            'Machine analyzer restoration requires all existing 32 production roles')
    desired = service_definition(baseline, image_id)
    if MACHINE_ANALYSIS_SERVICE in baseline['services']:
        require(baseline['services'][MACHINE_ANALYSIS_SERVICE] == desired,
                'Machine analyzer already differs; use the reviewed image-update flow')
    updated = json.loads(json.dumps(baseline))
    updated['services'][MACHINE_ANALYSIS_SERVICE] = desired
    require(set(updated['services']) == MACHINE_ANALYSIS_SERVICES, 'Unexpected restored role set')
    require(all(updated['services'][name] == baseline['services'][name] for name in CURRENT_SERVICES),
            'An existing production role changed')
    return updated


def inspect(names):
    return json.loads(command(['docker', 'inspect', *sorted(names)]))


def identities(names):
    names = set(names)
    values = inspect(names)
    result = {value['Name'].lstrip('/'): value['Id'] for value in values}
    require(len(values) == len(names) and set(result) == names and
            all(value['State']['Running'] for value in values),
            'An existing production role is missing, stopped, or duplicated')
    return result


def machine_container():
    found = command(['docker', 'ps', '-aq', '--filter', 'name=^/' + MACHINE_ANALYSIS_SERVICE + '$']).splitlines()
    require(len(found) <= 1, 'Unexpected machine analyzer container count')
    return inspect([MACHINE_ANALYSIS_SERVICE])[0] if found else None


def compose(baseline):
    args = ['docker', 'compose', '--project-directory', DEPLOY, '--project-name', baseline['name'],
            '-f', BASELINE]
    for path in PINS:
        require(not path.is_symlink(), 'CI image pin must not be a symlink')
        if path.exists(): args += ['-f', path]
    return args


def pin_guards():
    result = {}
    for path in PINS:
        require(not path.is_symlink(), 'CI image pin must not be a symlink')
        exists = path.exists()
        result[str(path)] = {'exists': exists, 'sha256': digest(private_read(path)) if exists else None}
    return result


def validate_guards(manifest):
    require(manifest.get('pin_guards') == pin_guards(),
            'CI image pins changed after analyzer registration; refusing stale rollback')
    require(manifest.get('scope_sha256') == digest(private_read(SCOPE)),
            'Backup scope changed after analyzer registration; refusing stale rollback')


def preflight_helpers():
    record = json.loads((PACKAGE / 'production-helper-provenance.json').read_text())['files']['runtime_roles.py']
    require(record.get('installed_path') == str(RUNTIME_ROLES_TARGET) and
            re.fullmatch(r'[a-f0-9]{64}', record.get('production_sha256', '')),
            'Expected reviewed runtime-role helper provenance')
    packaged = (PACKAGE / 'runtime_roles.py').read_bytes()
    require(digest(private_read(RUNTIME_ROLES_TARGET)) in {record['production_sha256'], digest(packaged)},
            'Runtime-role helper changed since review')
    return packaged


def verify_image(image_id, source_sha):
    values = json.loads(command(['docker', 'image', 'inspect', image_id]))
    require(len(values) == 1, 'Expected one local pinned image')
    image = values[0]
    require(image['Id'] == image_id and image['Os'] == 'linux' and image['Architecture'] == 'amd64' and
            (image.get('Config', {}).get('Labels') or {}).get('org.opencontainers.image.revision') == source_sha,
            'Pinned local image does not match the reviewed linux/amd64 source revision')
    config = image.get('Config') or {}
    require(not config.get('Entrypoint') and config.get('Cmd') == ['./report-analyze-pipeline'] and
            config.get('WorkingDir') == '/root', 'Pinned analyzer image has an unexpected startup command')


def verify_machine(value, image_id, source_sha, healthy=True):
    labels = value.get('Config', {}).get('Labels') or {}
    require(value.get('Name', '').lstrip('/') == MACHINE_ANALYSIS_SERVICE and
            value.get('Image') == image_id and
            labels.get('com.docker.compose.project') == PROJECT and
            labels.get('com.docker.compose.service') == MACHINE_ANALYSIS_SERVICE and
            labels.get('org.opencontainers.image.revision') == source_sha,
            'Refusing an unexpected machine analyzer container')
    if healthy:
        require(value['State']['Running'] and value['State'].get('Health', {}).get('Status') == 'healthy' and
                value['HostConfig']['RestartPolicy']['Name'] == 'unless-stopped',
                'Machine analyzer did not become healthy and connected')


def backup(changes, image_id, source_sha, before):
    folder = RECEIPTS / ('machine-analyzer-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8])
    folder.mkdir(mode=0o700)
    manifest = {'schema': 1, 'service': MACHINE_ANALYSIS_SERVICE, 'image_id': image_id,
                'source_sha': source_sha, 'state': 'prepared', 'files': [],
                'original_container_ids': before, 'pin_guards': pin_guards(),
                'scope_sha256': digest(private_read(SCOPE))}
    for number, (path, after) in enumerate(changes.items()):
        data = private_read(path); info = path.stat()
        filename = str(number) + '.private'
        write_file(folder / filename, data)
        manifest['files'].append({'path': str(path), 'after_sha256': digest(after), 'existed': True,
                                  'backup': filename, 'before_sha256': digest(data),
                                  'mode': stat.S_IMODE(info.st_mode), 'uid': info.st_uid, 'gid': info.st_gid})
    write_file(folder / 'manifest.json', json_bytes(manifest))
    return folder, manifest


def remove_machine(manifest):
    value = machine_container()
    if value is None: return
    verify_machine(value, manifest['image_id'], manifest['source_sha'], healthy=False)
    if manifest.get('registered_container_id'):
        require(value['Id'] == manifest['registered_container_id'], 'Machine analyzer was replaced after registration')
    command(['docker', 'stop', '--time', '120', MACHINE_ANALYSIS_SERVICE])
    command(['docker', 'rm', MACHINE_ANALYSIS_SERVICE])


def rollback(folder):
    require(not folder.is_symlink(), 'Rollback receipt must not be a symlink')
    folder = folder.resolve()
    require(folder.parent == RECEIPTS.resolve() and folder.name.startswith('machine-analyzer-'),
            'Unexpected rollback receipt path')
    manifest = json.loads(private_read(folder / 'manifest.json'))
    rows = manifest.get('files', [])
    require(manifest.get('schema') == 1 and manifest.get('service') == MACHINE_ANALYSIS_SERVICE and
            len(rows) == 2 and {row['path'] for row in rows} == {str(BASELINE), str(RUNTIME_ROLES_TARGET)} and
            all(row.get('existed') is True for row in rows), 'Rollback receipt has unexpected file scope')
    validate_identity(manifest.get('image_id'), manifest.get('source_sha'))
    require(manifest.get('state') in {'prepared', 'applied', 'rolled_back'}, 'Unexpected receipt state')
    original = manifest.get('original_container_ids', {})
    require(set(original) == {expected_container(name) for name in CURRENT_SERVICES},
            'Rollback receipt has unexpected original container scope')
    if manifest['state'] == 'rolled_back':
        for row in rows:
            path = Path(row['path'])
            require(digest(private_read(path)) == row['before_sha256'], 'Completed rollback receipt is stale')
            info = path.stat()
            require((stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid) == (row['mode'], row['uid'], row['gid']),
                    'Completed rollback metadata differs from its original state')
        require(machine_container() is None, 'Completed rollback receipt is stale; machine analyzer exists')
        return {'rolled_back': True, 'already_rolled_back': True, 'receipt': str(folder), 'other_roles_restarted': 0}
    validate_guards(manifest)
    prepared = prepare_restore(folder, manifest)
    require(identities(original) == original, 'An existing production role changed after registration')
    remove_machine(manifest)
    restore_files(prepared)
    manifest['state'] = 'rolled_back'; write_file(folder / 'manifest.json', json_bytes(manifest))
    return {'rolled_back': True, 'receipt': str(folder), 'other_roles_restarted': 0}


def apply(image_id, source_sha):
    validate_identity(image_id, source_sha)
    baseline = json.loads(private_read(BASELINE))
    scope = json.loads(private_read(SCOPE))
    require(set(scope) == {'required', 'optional'}, 'Unexpected backup scope format')
    updated = prepare(baseline, image_id)
    verify_image(image_id, source_sha)
    packaged_roles = preflight_helpers()
    original_names = {name for service, name in declared_roles(baseline).items() if service in CURRENT_SERVICES}
    before = identities(original_names)
    existing = machine_container()
    if MACHINE_ANALYSIS_SERVICE in baseline['services']:
        require(existing is not None, 'Registered machine analyzer is missing')
        verify_machine(existing, image_id, source_sha)
        require(private_read(RUNTIME_ROLES_TARGET) == packaged_roles, 'Registered analyzer role helper is incomplete')
        return {'already_registered': True, 'roles': 33, 'other_roles_restarted': 0}
    require(existing is None, 'Unregistered machine analyzer container already exists')
    changes = {BASELINE: json_bytes(updated), RUNTIME_ROLES_TARGET: packaged_roles}
    folder, manifest = backup(changes, image_id, source_sha, before)
    try:
        for path, data in changes.items():
            info = path.stat()
            write_file(path, data, stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid)
        resolved = json.loads(command(compose(updated) + ['config', '--format', 'json']))
        desired = updated['services'][MACHINE_ANALYSIS_SERVICE]
        worker = resolved['services'].get(MACHINE_ANALYSIS_SERVICE, {})
        # Compose resolves escaped literal dollars in both environment copies.
        # Compare the resolved worker with the resolved human source, not with
        # the raw protected baseline's interpolation syntax.
        expected_environment = {**resolved['services'][HUMAN_SERVICE]['environment'], **ENV_OVERRIDES}
        require(set(resolved['services']) == MACHINE_ANALYSIS_SERVICES and
                worker.get('image') == image_id and worker.get('environment') == expected_environment and
                worker.get('healthcheck', {}).get('test') == desired['healthcheck']['test'] and
                not worker.get('ports') and not worker.get('privileged') and
                set(worker.get('networks', {})) == {'deployer_default'} and
                all(not (value or {}).get('ipv4_address') and not (value or {}).get('ipv6_address')
                    for value in worker.get('networks', {}).values()), 'Resolved machine analyzer differs from review')
        validate_guards(manifest)
        command(compose(updated) + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never',
                                    '--wait', '--wait-timeout', '180', MACHINE_ANALYSIS_SERVICE])
        require(identities(before) == before, 'An existing production role was replaced')
        value = machine_container()
        require(value is not None, 'Machine analyzer container is missing after startup')
        verify_machine(value, image_id, source_sha)
        manifest['registered_container_id'] = value['Id']
        manifest['state'] = 'applied'; write_file(folder / 'manifest.json', json_bytes(manifest))
        return {'registered': True, 'roles': 33, 'receipt': str(folder), 'other_roles_restarted': 0}
    except Exception:
        # Validate all prior bytes and guards before stopping or restoring anything.
        validate_guards(manifest)
        prepared = prepare_restore(folder, manifest)
        remove_machine(manifest)
        restore_files(prepared)
        manifest['state'] = 'rolled_back'; write_file(folder / 'manifest.json', json_bytes(manifest))
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image-id'); parser.add_argument('--source-sha')
    parser.add_argument('--rollback', type=Path); parser.add_argument('--execute', action='store_true')
    args = parser.parse_args()
    require(bool(args.image_id) != bool(args.rollback), 'Choose image registration or a rollback receipt')
    if args.image_id: validate_identity(args.image_id, args.source_sha)
    else: require(not args.source_sha, 'Source SHA is only valid for image registration')
    if not args.execute:
        print(json.dumps({'plan': True, 'action': 'rollback' if args.rollback else 'register',
                          'service': MACHINE_ANALYSIS_SERVICE, 'new_roles': 0 if args.rollback else 1,
                          'other_roles_restarted': 0, 'pulls': 0, 'builds': 0, 'mutations': 0})); return
    admitted_host()
    if str(LIBRARY) not in sys.path: sys.path.insert(0, str(LIBRARY))
    from backup_recovery import shared_deployment_lock
    with shared_deployment_lock(LOCK):
        admitted_host()
        print(json.dumps(rollback(args.rollback) if args.rollback else apply(args.image_id, args.source_sha)))


if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(json.dumps({'success': False, 'error_type': type(error).__name__,
                          'message': str(error) if isinstance(error, ValueError) else 'Registration failed; raw diagnostics suppressed'}),
              file=sys.stderr)
        sys.exit(1)
