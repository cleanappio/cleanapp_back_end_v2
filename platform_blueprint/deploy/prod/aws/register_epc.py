#!/usr/bin/env python3
"""Register only the pinned EPC role on the existing AWS host. Dry-run by default.

Run this reviewed package on production after loading the immutable image and
provisioning the protected env file. Never prints resolved Compose or secrets.
The existing deployment/backup lock serializes apply and rollback with CI.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import uuid

from runtime_roles import (CURRENT_SERVICES, EPC_SERVICE, LEGACY_SERVICES,
                           declared_roles, require)

PACKAGE = Path(__file__).resolve().parent
BASELINE = Path('/opt/cleanapp/private/compose-target.json')
DEPLOY = Path('/opt/cleanapp/deploy')
LIBRARY = Path('/usr/local/lib/cleanapp-backup')
SCOPE = Path('/etc/cleanapp-backup/weekly-config-scope.json')
ENV = Path('/opt/cleanapp/private/epc-pusher.env')
ENV_TARGET = '/run/cleanapp/epc-pusher.env'
LOCK = DEPLOY / '.deployment-and-backup.lock'
RECEIPTS = DEPLOY / 'aws-deploy'
TARGETS = {
    'runtime_boot.py': Path('/opt/cleanapp/host-runtime/runtime_boot.py'),
    'native_restart_policy.py': Path('/opt/cleanapp/host-runtime/native_restart_policy.py'),
    'weekly_recovery.py': LIBRARY / 'weekly_recovery.py',
    'runtime_roles.py': LIBRARY / 'runtime_roles.py',
}
PINS = [DEPLOY / 'docker-compose.aws-backend-images.yml',
        DEPLOY / 'docker-compose.aws-frontend-images.yml']


def digest(data):
    return hashlib.sha256(data).hexdigest()


def private_read(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and not info.st_mode & 0o077,
                'Private runtime input must be a regular owner-only file')
        return stream.read()


def write_file(path, data, mode=0o600, uid=None, gid=None):
    """Atomic replacement; lock inode itself is deliberately never replaced."""
    path = Path(path)
    require(not path.is_symlink(), 'Refusing symlink destination')
    temporary = path.with_name('.' + path.name + '.' + uuid.uuid4().hex)
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data); stream.flush(); os.fsync(stream.fileno())
            os.fchmod(stream.fileno(), mode)
            if uid is not None: os.fchown(stream.fileno(), uid, gid)
        os.replace(temporary, path)
    finally:
        if temporary.exists(): temporary.unlink()


def json_bytes(value):
    return (json.dumps(value, indent=2) + '\n').encode()


def command(args):
    result = subprocess.run([str(x) for x in args], capture_output=True)
    require(result.returncode == 0, 'Runtime command failed; raw diagnostics suppressed')
    return result.stdout


def inspect(names):
    return json.loads(command(['docker', 'inspect', *sorted(names)]))


def identities(names):
    values = inspect(names)
    require(all(c['State']['Running'] for c in values), 'An existing production role is stopped')
    return {c['Name'].lstrip('/'): c['Id'] for c in values}


def compose(baseline):
    args = ['docker', 'compose', '--project-directory', DEPLOY, '--project-name', baseline['name'],
            '-f', BASELINE]
    for path in PINS:
        require(not path.is_symlink(), 'CI image pin must not be a symlink')
        if path.exists(): args += ['-f', path]
    return args


def service_definition(image_id, env_file):
    require(re.fullmatch(r'sha256:[a-f0-9]{64}', image_id), 'Expected immutable image ID')
    require(Path(env_file).is_absolute() and str(env_file) == os.path.normpath(env_file),
            'Protected environment path must be absolute and normalized')
    return {'image': image_id, 'pull_policy': 'never', 'container_name': EPC_SERVICE,
            'restart': 'unless-stopped', 'stop_grace_period': '45s',
            'environment': {'EPC_ENV_FILE': ENV_TARGET},
            'volumes': [{'type': 'bind', 'source': str(env_file), 'target': ENV_TARGET, 'read_only': True}],
            'networks': {'deployer_default': {}}}


def prepare(baseline, scope, image_id, env_file):
    roles = declared_roles(baseline)
    desired = service_definition(image_id, env_file)
    if EPC_SERVICE in roles:
        require(baseline['services'][EPC_SERVICE] == desired,
                'EPC already registered differently; use the ordinary reviewed image-update flow')
    updated = json.loads(json.dumps(baseline))
    updated['services'][EPC_SERVICE] = desired
    require(set(updated['services']) == CURRENT_SERVICES, 'Unexpected expanded role set')
    require(all(updated['services'][s] == baseline['services'][s] for s in LEGACY_SERVICES),
            'An existing role changed while adding EPC')
    require(set(scope) == {'required', 'optional'}, 'Unexpected backup scope format')
    scope = json.loads(json.dumps(scope))
    scope['required'] = sorted(set(scope['required']) | {str(env_file)})
    return updated, scope


def preflight_helpers():
    provenance = json.loads((PACKAGE / 'production-helper-provenance.json').read_text())['files']
    for name, target in TARGETS.items():
        if not target.exists():
            require(name == 'runtime_roles.py', 'Existing runtime helper is missing')
            continue
        current = digest(private_read(target))
        allowed = {digest((PACKAGE / name).read_bytes())}
        if name in provenance: allowed.add(provenance[name]['production_sha256'])
        require(current in allowed, 'Runtime helper changed since review; refresh its source before applying')


def backup(changes, image_id):
    folder = RECEIPTS / ('epc-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8])
    folder.mkdir(mode=0o700)
    manifest = {'schema': 1, 'image_id': image_id, 'state': 'prepared', 'files': []}
    for number, (path, after) in enumerate(changes.items()):
        row = {'path': str(path), 'after_sha256': digest(after), 'existed': path.exists()}
        if row['existed']:
            data = private_read(path); info = path.stat()
            filename = str(number) + '.private'
            write_file(folder / filename, data)
            row.update(backup=filename, before_sha256=digest(data), mode=stat.S_IMODE(info.st_mode),
                       uid=info.st_uid, gid=info.st_gid)
        manifest['files'].append(row)
    write_file(folder / 'manifest.json', json_bytes(manifest))
    return folder, manifest


def prepare_restore(folder, manifest):
    # Preload and hash every original byte before any stop, unlink, or write.
    # Never let a corrupt later backup produce a partial runtime rollback.
    prepared = []
    for row in manifest['files']:
        path = Path(row['path'])
        if path.exists():
            require(digest(private_read(path)) in {row['after_sha256'], row.get('before_sha256')},
                    'Metadata changed after EPC deployment; refusing stale rollback')
        else:
            require(not row['existed'], 'Original runtime metadata disappeared')
        data = None
        if row['existed']:
            require(re.fullmatch(r'[0-9]+[.]private', row['backup']), 'Unexpected rollback backup filename')
            data = private_read(folder / row['backup'])
            require(digest(data) == row['before_sha256'], 'Rollback backup hash mismatch')
        prepared.append((path, row, data))
    return prepared


def restore_files(prepared):
    for path, row, data in prepared:
        if row['existed']:
            write_file(path, data, row['mode'], row['uid'], row['gid'])
        elif path.exists():
            path.unlink()


def remove_epc(image_id):
    found = subprocess.run(['docker', 'inspect', EPC_SERVICE], capture_output=True)
    if found.returncode: return
    container = json.loads(found.stdout)[0]
    labels = container.get('Config', {}).get('Labels') or {}
    require(container['Image'] == image_id and labels.get('com.docker.compose.project') == 'cleanapp-migrated'
            and labels.get('com.docker.compose.service') == EPC_SERVICE,
            'Refusing removal of an unexpected EPC container')
    command(['docker', 'rm', '-f', EPC_SERVICE])


def rollback(folder):
    folder = folder.resolve()
    require(folder.parent == RECEIPTS.resolve() and folder.name.startswith('epc-'), 'Unexpected rollback receipt path')
    manifest = json.loads(private_read(folder / 'manifest.json'))
    allowed = {str(BASELINE), str(SCOPE), *(str(x) for x in TARGETS.values())}
    require({row['path'] for row in manifest['files']} == allowed, 'Rollback receipt has unexpected file scope')
    require(manifest['state'] in {'prepared', 'applied', 'rolled_back'}, 'Unexpected receipt state')
    if manifest['state'] == 'rolled_back':
        # A completed receipt must never be used to undo a later registration,
        # even when its image and rendered metadata happen to match exactly.
        for row in manifest['files']:
            path = Path(row['path'])
            if row['existed']:
                require(path.exists() and digest(private_read(path)) == row['before_sha256'],
                        'Completed rollback receipt is stale; target differs from its original state')
                info = path.stat()
                require((stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid) ==
                        (row['mode'], row['uid'], row['gid']), 'Completed rollback metadata differs from its original state')
            else:
                require(not os.path.lexists(path), 'Completed rollback receipt is stale; added target exists')
        require(not command(['docker', 'ps', '-aq', '--filter', 'name=^/' + EPC_SERVICE + '$']).strip(),
                'Completed rollback receipt is stale; an EPC container exists')
        return {'rolled_back': True, 'already_rolled_back': True, 'receipt': str(folder), 'other_roles_restarted': 0}
    prepared = prepare_restore(folder, manifest)
    remove_epc(manifest['image_id'])
    restore_files(prepared)
    manifest['state'] = 'rolled_back'
    write_file(folder / 'manifest.json', json_bytes(manifest))
    return {'rolled_back': True, 'receipt': str(folder), 'other_roles_restarted': 0}


def apply(image_id, env_file):
    baseline = json.loads(private_read(BASELINE)); scope = json.loads(private_read(SCOPE))
    previous_scope = scope
    updated, scope = prepare(baseline, scope, image_id, env_file)
    require(private_read(env_file).strip(), 'Protected EPC environment file is empty')
    image = json.loads(command(['docker', 'image', 'inspect', image_id]))[0]
    require(image['Id'] == image_id and image['Os'] == 'linux' and image['Architecture'] == 'amd64',
            'Pinned image must already exist as linux/amd64')
    preflight_helpers()
    before = identities(declared_roles(baseline).values())
    if EPC_SERVICE in baseline['services']:
        # Exact replay does not recreate the service or create another rollback point.
        require(inspect([EPC_SERVICE])[0]['Image'] == image_id, 'Registered EPC container has a different image')
        require(all(target.exists() and digest(private_read(target)) == digest((PACKAGE / name).read_bytes())
                    for name, target in TARGETS.items()), 'Registered EPC runtime helpers are incomplete')
        require(str(env_file) in previous_scope['required'], 'Registered EPC environment is absent from backup scope')
        return {'already_registered': True, 'roles': len(before), 'other_roles_restarted': 0}
    absent = subprocess.run(['docker', 'inspect', EPC_SERVICE], capture_output=True)
    require(absent.returncode != 0, 'Unregistered EPC container already exists')
    changes = {**{target: (PACKAGE / name).read_bytes() for name, target in TARGETS.items()},
               BASELINE: json_bytes(updated), SCOPE: json_bytes(scope)}
    folder, manifest = backup(changes, image_id)
    try:
        for path, data in changes.items():
            info = path.stat() if path.exists() else None
            write_file(path, data, stat.S_IMODE(info.st_mode) if info else 0o600,
                       info.st_uid if info else 0, info.st_gid if info else 0)
        resolved = json.loads(command(compose(updated) + ['config', '--format', 'json']))
        require(set(resolved['services']) == CURRENT_SERVICES, 'Resolved Compose role set differs')
        command(compose(updated) + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', EPC_SERVICE])
        require(identities(before) == before, 'An existing production role was replaced')
        epc = inspect([EPC_SERVICE])[0]
        require(epc['State']['Running'] and epc['Image'] == image_id and
                epc['HostConfig']['RestartPolicy']['Name'] == 'unless-stopped', 'EPC startup validation failed')
        manifest['state'] = 'applied'; write_file(folder / 'manifest.json', json_bytes(manifest))
        return {'registered': True, 'roles': len(CURRENT_SERVICES), 'receipt': str(folder), 'other_roles_restarted': 0}
    except Exception:
        prepared = prepare_restore(folder, manifest)
        remove_epc(image_id)
        restore_files(prepared)
        manifest['state'] = 'rolled_back'; write_file(folder / 'manifest.json', json_bytes(manifest))
        raise


def admitted_host():
    require(sys.platform.startswith('linux') and os.geteuid() == 0, 'Execution requires root on AWS Linux')
    require('Amazon' in Path('/sys/devices/virtual/dmi/id/sys_vendor').read_text(), 'Execution requires the reviewed AWS host')
    require(Path('/etc/cleanapp-runtime/ACTIVE').is_file() and
            not Path('/etc/cleanapp-runtime/ISOLATED').exists(), 'Execution requires the admitted active runtime')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image-id'); parser.add_argument('--env-file', type=Path, default=ENV)
    parser.add_argument('--rollback', type=Path); parser.add_argument('--execute', action='store_true')
    args = parser.parse_args()
    require(bool(args.image_id) != bool(args.rollback), 'Choose image registration or a rollback receipt')
    if not args.execute:
        if args.image_id: service_definition(args.image_id, args.env_file)
        print(json.dumps({'plan': True, 'action': 'rollback' if args.rollback else 'register',
                          'service': EPC_SERVICE, 'new_roles': 0 if args.rollback else 1, 'other_roles_restarted': 0,
                          'pulls': 0, 'builds': 0, 'mutations': 0})); return
    admitted_host()
    if str(LIBRARY) not in sys.path: sys.path.insert(0, str(LIBRARY))
    from backup_recovery import shared_deployment_lock
    with shared_deployment_lock(LOCK):
        print(json.dumps(rollback(args.rollback) if args.rollback else apply(args.image_id, args.env_file)))


if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(json.dumps({'success': False, 'error_type': type(error).__name__,
                          'message': str(error) if isinstance(error, ValueError) else 'Registration failed; raw diagnostics suppressed'}), file=sys.stderr)
        sys.exit(1)
