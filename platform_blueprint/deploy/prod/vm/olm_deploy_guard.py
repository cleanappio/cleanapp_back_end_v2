#!/usr/bin/python3 -I
"""Root-installed read-only interlock for deployments during the finite OLM job.

No stopping, migrations, pin updates, probes or resume occur here. Unknown new
images require a root-owned exact review plan AND an already quiescent receiver.
"""
import argparse
import ast
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess

ROOT = Path('/srv/cleanapp-olm-import')
ROLES = {'cleanapp_email_service': 'email-service',
         'cleanapp_report_listener': 'report-listener',
         'cleanapp_report_analyze_human': 'report-analyze-pipeline'}
PAIR_IMAGE = re.compile(r'sha256:[0-9a-f]{64}\Z')
PAIR_SOURCE = re.compile(r'[0-9a-f]{40}\Z')
HASH = re.compile(r'[0-9a-f]{64}\Z')


def require(ok, message):
    if not ok:
        raise ValueError(message)


def read_file(path, *, trusted):
    if trusted:
        trusted_parents(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_size <= 1024**2,
                'Expected bounded regular artifact')
        if trusted:
            require(before.st_uid == 0 and before.st_mode & 0o022 == 0,
                    'OLM artifact must be root-owned and not group/world writable')
        raw = b''
        while chunk := os.read(fd, 65536):
            raw += chunk
            require(len(raw) <= 1024**2, 'Artifact exceeded bound')
        after = os.fstat(fd)
        require((before.st_size, before.st_mtime_ns) == (after.st_size, after.st_mtime_ns),
                'Artifact changed while reading')
        return raw
    finally:
        os.close(fd)


def trusted_parents(path):
    for parent in reversed(Path(path).parents):
        try:
            info = parent.lstat()
        except FileNotFoundError:
            break
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0,
                'OLM artifact ancestors must be root-owned nonsymlink private-write directories')


def document(raw):
    def unique(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, 'Duplicate artifact key')
            value[key] = item
        return value
    return json.loads(raw, object_pairs_hook=unique)


def validate_pairs(pairs):
    require(isinstance(pairs, dict) and set(pairs) == set(ROLES), 'Wrong guarded role set')
    for value in pairs.values():
        require(isinstance(value, (tuple, list)) and len(value) == 2
                and isinstance(value[0], str) and PAIR_IMAGE.fullmatch(value[0])
                and isinstance(value[1], str) and PAIR_SOURCE.fullmatch(value[1]),
                'Invalid exact runtime identity')
    return {role: list(pair) for role, pair in pairs.items()}


def reviewed_pins(root):
    policy = document(read_file(root / 'evidence/base-policy.json', trusted=True))
    helper = root / 'app/native_import/guard_review/refresh_live_guards.py'
    spec = policy['guard_refresh']
    raw = read_file(helper, trusted=True)
    require(spec['path'] == str(helper) and hashlib.sha256(raw).hexdigest() == spec['sha256'],
            'Reviewed guard helper does not match the base policy')
    assignments = [node.value for node in ast.parse(raw).body if isinstance(node, ast.Assign)
                   and any(isinstance(target, ast.Name) and target.id == 'EXPECTED'
                           for target in node.targets)]
    require(len(assignments) == 1, 'Reviewed guard identity map is missing or ambiguous')
    return validate_pairs(ast.literal_eval(assignments[0])), spec['sha256']


def receiver_state():
    raw = subprocess.check_output(['/usr/bin/systemctl', 'show', 'olm-native-receiver.service',
                                  '-p', 'ActiveState', '-p', 'MainPID', '-p', 'ControlPID'],
                                  text=True, timeout=15)
    return dict(line.split('=', 1) for line in raw.splitlines())


def current_runtime():
    pairs = {}
    template = '{{json .Image}}\n{{json (index .Config.Labels "org.opencontainers.image.revision")}}\n{{json .State.Running}}'
    for role in ROLES:
        raw = subprocess.check_output(['/usr/bin/docker', 'inspect', '--format', template, role],
                                      text=True, timeout=15)
        values = [json.loads(line) for line in raw.splitlines()]
        require(len(values) == 3 and values[2] is True, 'Guarded runtime is not running')
        pairs[role] = values[:2]
    return validate_pairs(pairs)


def decision(manifest, pins, actual, guard_sha, manifest_sha, state, plan, now):
    pins = validate_pairs(pins)
    require(validate_pairs(actual) == pins, 'Actual guarded runtime differs from reviewed pins')
    proposed = dict(pins)
    for role in sorted(set(manifest['roles']) & set(ROLES)):
        proposed[role] = [manifest['images'][ROLES[role]]['runtime']['image_id'], manifest['source_sha']]
    proposed = validate_pairs(proposed)
    if proposed == pins:
        return 'unchanged'
    require(plan is not None, 'Unfinished OLM import: unknown guarded images need a root-reviewed plan')
    expires = dt.datetime.fromisoformat(plan['valid_until_utc'].replace('Z', '+00:00'))
    require(expires.tzinfo is not None and now <= expires <= now + dt.timedelta(hours=24),
            'OLM review plan is expired or exceeds the 24-hour bound')
    require(plan.get('kind') == 'cleanapp_olm_deploy_review.v1'
            and plan.get('manifest_sha256') == manifest_sha
            and plan.get('previous_guard_sha256') == guard_sha
            and isinstance(plan.get('source_review_sha256'), str)
            and HASH.fullmatch(plan['source_review_sha256'])
            and plan.get('manual_post_deploy_probes_and_resume') is True
            and validate_pairs(plan['approved_runtime']) == proposed,
            'OLM review plan does not authorize this exact release')
    require(state.get('ActiveState') in ('inactive', 'failed')
            and state.get('MainPID') == '0' and state.get('ControlPID') == '0',
            'Reviewed guarded-image change requires an already stopped OLM receiver')
    return 'reviewed_quiescent'


def check(manifest_path, *, root=ROOT):
    raw = read_file(manifest_path, trusted=False)
    manifest = document(raw)
    guarded = sorted(set(manifest['roles']) & set(ROLES))
    result = {'kind': 'cleanapp_olm_deploy_check.v1', 'manifest_sha256': hashlib.sha256(raw).hexdigest(),
              'guarded_roles': guarded}
    config = root / 'evidence/receiver-config.json'
    try:
        config_raw = read_file(config, trusted=True)
    except FileNotFoundError:
        return {**result, 'decision': 'not_installed'}
    # Presence means unfinished, including failed/stopped/activating services.
    # Retirement is an explicit root action after terminal drain verification.
    document(config_raw)
    pins, guard_sha = reviewed_pins(root)
    plan_path = root / 'evidence/deployment-review-plan.json'
    try:
        plan = document(read_file(plan_path, trusted=True))
    except FileNotFoundError:
        plan = None
    state = receiver_state()
    outcome = decision(manifest, pins, current_runtime(), guard_sha, result['manifest_sha256'], state, plan,
                       dt.datetime.now(dt.timezone.utc))
    return {**result, 'decision': outcome, 'guard_sha256': guard_sha,
            'manual_post_deploy_probes_and_resume': outcome == 'reviewed_quiescent'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    args = parser.parse_args()
    try:
        require(os.geteuid() == 0, 'Root-installed OLM check required')
        require(args.manifest.is_absolute(), 'Absolute release manifest required')
        print(json.dumps(check(args.manifest)))
    except (OSError, ValueError, KeyError, TypeError, SyntaxError, subprocess.SubprocessError):
        raise SystemExit('OLM compatibility check refused; inspect root-owned review plan and guard state')
