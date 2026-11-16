#!/usr/bin/env python3
"""Fixed-host production interlock for the bounded SolveSF canary.

The source producer cannot select code, grant controls, relax privacy, or mutate
OLM state. Generic locking/hash mechanics come from the exact deployed OLM
library; source-specific OLM identity/media reuse is deliberately never invoked.
"""
from __future__ import annotations
import base64
import contextlib
import datetime as dt
import fcntl
import hashlib
import html
import importlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import subprocess
import sys

ROOT = Path('/srv/cleanapp-solvesf-import')
OLM = Path('/srv/cleanapp-olm-import')
LIB = OLM / 'app/native_import/apply'
POLICY = OLM / 'evidence/base-policy.json'
LOCK = Path('/opt/cleanapp/deploy/.deployment-and-backup.lock')
COLLECTION_STATE = ROOT / 'state/canary/ledger.sqlite3'
COLLECTION_LOCK = ROOT / 'state/canary/.collector.lock'
BUCKET = 'cleanapp-production-backups-654574823950'
SERVER = 'f2c484f6-3c5d-11ef-aace-0242ac120003'
FLAGS = ('suppress_email', 'suppress_notifications', 'suppress_rewards', 'suppress_routing', 'suppress_analysis')
SOURCE_COLUMNS = {
    'sf311_source_case_ledger': {
        'source_namespace': ('varchar', 'NO', 64, 'ascii_bin'), 'source_id': ('varchar', 'NO', 64, 'ascii_bin'),
        'payload_sha256': ('char', 'NO', 64, 'ascii_bin'), 'material_sha256': ('char', 'NO', 64, 'ascii_bin'),
        'publisher': ('varchar', 'NO', 64, None), 'source_url': ('varchar', 'NO', 512, None),
        'source_record_json': ('json', 'NO', 0, ''), 'candidate_json': ('json', 'NO', 0, ''),
        'media_json': ('json', 'NO', 0, ''), 'report_seq': ('int', 'YES', 0, ''),
        'recorded_report_seq': ('int', 'NO', 0, ''), 'report_public_id': ('varchar', 'NO', 32, 'ascii_bin'),
        'disposition': ('varchar', 'NO', 32, None), 'created_at': ('timestamp', 'NO', 0, '')},
    'sf311_source_media_ledger': {
        'source_namespace': ('varchar', 'NO', 64, 'ascii_bin'), 'source_id': ('varchar', 'NO', 64, 'ascii_bin'),
        'position': ('tinyint', 'NO', 0, ''), 'original_sha256': ('char', 'NO', 64, 'ascii_bin'),
        'display_sha256': ('char', 'NO', 64, 'ascii_bin'), 'media_json': ('json', 'NO', 0, '')}}

class GuardError(ValueError):
    pass

def require(ok, code):
    if not ok:
        raise GuardError(code)

def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False, allow_nan=False)

def trusted(path, expected=None, private=False):
    path = Path(path)
    require(path.is_absolute(), 'absolute_trusted_path_required')
    for parent in (path, *path.parents):
        info = parent.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0
                and not info.st_mode & 0o022, 'artifact_not_root_trusted')
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and (not private or not info.st_mode & 0o077),
            'private_regular_artifact_required')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        h = hashlib.sha256()
        while block := os.read(fd, 1024 * 1024):
            h.update(block)
        after = os.fstat(fd)
        require((before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns)
                == (after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns), 'artifact_changed')
        digest = h.hexdigest()
        require(expected is None or digest == expected, 'artifact_digest_mismatch')
        return digest
    finally:
        os.close(fd)

def document(path, expected):
    trusted(path, expected, private=True)
    require(Path(path).stat().st_size <= 16 * 1024**2, 'evidence_size_bound')
    def unique(pairs):
        out = {}
        for key, value in pairs:
            require(key not in out, 'duplicate_evidence_key')
            out[key] = value
        return out
    return json.loads(Path(path).read_bytes(), object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(GuardError('nonfinite_evidence')))

def quiet(command):
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    require(result.returncode == 0, 'fixed_runtime_check_failed')
    return result.stdout

def stopped():
    raw = quiet(['/usr/bin/systemctl', 'show', 'olm-native-receiver.service',
                 '-p', 'ActiveState', '-p', 'MainPID', '-p', 'ControlPID'])
    state = dict(line.split('=', 1) for line in raw.decode().splitlines())
    require(state.get('ActiveState') in ('inactive', 'failed')
            and state.get('MainPID') == state.get('ControlPID') == '0', 'olm_receiver_not_quiescent')

def case_media_hashes(refs):
    return {ref[k]['sha256'] for ref in refs for k in ('original', 'display')}

def eligible_collection_case(connection, candidate):
    ident = candidate['source_id']
    require(not connection.execute('SELECT 1 FROM record_holds WHERE id=? LIMIT 1', (ident,)).fetchone(),
            'source_case_has_sticky_collection_hold')
    require(not connection.execute("SELECT 1 FROM versions WHERE id=? AND disposition<>'candidate' LIMIT 1",
                                   (ident,)).fetchone(), 'source_case_has_held_version_history')
    rows = connection.execute('SELECT candidate,disposition FROM versions WHERE id=? AND digest=?',
                              (ident, candidate['candidate_sha256'])).fetchall()
    require(rows == [(canonical(candidate), 'candidate')], 'candidate_not_exactly_authorized_by_collector_state')
    require(connection.execute('SELECT COUNT(*) FROM versions WHERE id=?', (ident,)).fetchone() == (1,),
            'ambiguous_source_version_history')

HOLD_REASONS = frozenset(('guard_nonpublic_or_deleted', 'guard_detail_not_found',
                         'guard_case_material_changed', 'guard_photo_not_found', 'guard_photo_material_changed'))

def save_observed_hold(connection, ident, reason):
    """Metadata-only sticky source history; no candidate/version is rewritten."""
    require(isinstance(ident, str) and re.fullmatch('[1-9][0-9]{0,31}', ident) is not None
            and reason in HOLD_REASONS, 'invalid_observed_hold')
    connection.execute('PRAGMA synchronous=FULL')
    now = dt.datetime.now(dt.timezone.utc).isoformat()
    with connection:
        connection.execute('INSERT INTO record_holds(id,reason,first_seen,last_seen) VALUES(?,?,?,?) '
            'ON CONFLICT(id) DO UPDATE SET last_seen=excluded.last_seen', (ident, reason, now, now))
    require(connection.execute('SELECT COUNT(*) FROM record_holds WHERE id=?', (ident,)).fetchone() == (1,),
            'observed_source_hold_not_durable')

def indexed_media_conflict(db, refs):
    """Any cross-case/source hit holds; deleted mappings are deliberately included."""
    hashes = case_media_hashes(refs)
    require(all(isinstance(h, str) and re.fullmatch('[0-9a-f]{64}', h) is not None for h in hashes),
            'invalid_media_digest')
    for digest in sorted(hashes):
        literal = "_ascii X'" + digest.encode().hex() + "'"
        for table, index, column in (
                ('olm_source_observation_ledger', 'idx_olm_ledger_original_hash', 'original_sha256'),
                ('olm_source_observation_ledger', 'idx_olm_ledger_display_hash', 'display_sha256'),
                ('sf311_source_media_ledger', 'idx_sf311_original_sha', 'original_sha256'),
                ('sf311_source_media_ledger', 'idx_sf311_display_sha', 'display_sha256')):
            if db.query('SELECT 1 FROM ' + table + ' FORCE INDEX(' + index + ') WHERE '
                        + column + '=' + literal + ' LIMIT 1 FOR UPDATE'):
                raise GuardError('existing_source_media_requires_reconciliation')
        if db.query('SELECT 1 FROM historical_report_import_ledger FORCE INDEX(uniq_historical_import_photo) '
                    "WHERE source_namespace=_utf8mb4 X'64617461756e696f6e2d302e312e30' AND photo_sha256="
                    + literal + ' LIMIT 1 FOR UPDATE'):
            raise GuardError('historical_original_requires_reconciliation')

def seed_positive_assets(connection, seq, hashes):
    """Add conservative holds only; never move a journal cursor or erase history."""
    require(type(seq) is int and seq > 0 and all(isinstance(h, str)
            and re.fullmatch('[0-9a-f]{64}', h) for h in hashes), 'invalid_positive_asset')
    columns = [row[1] for row in connection.execute('PRAGMA table_info(assets)')]
    require(columns == ['sha256', 'report_seq', 'protected', 'event_id'], 'unexpected_mutation_assets_schema')
    before = list(connection.execute('SELECT key,value FROM cursor ORDER BY key'))
    connection.execute('BEGIN IMMEDIATE')
    try:
        for digest in sorted(set(hashes)):
            connection.execute('INSERT INTO assets(sha256,report_seq,protected,event_id) VALUES(?,?,1,0) '
                'ON CONFLICT(sha256,report_seq) DO UPDATE SET protected=MAX(protected,1),event_id=MAX(event_id,0)',
                (digest, seq))
        require(list(connection.execute('SELECT key,value FROM cursor ORDER BY key')) == before,
                'mutation_cursor_changed')
        for digest in hashes:
            require(connection.execute('SELECT protected FROM assets WHERE sha256=? AND report_seq=?',
                                       (digest, seq)).fetchone() == (1,), 'positive_asset_readback_failed')
        connection.commit()
    except BaseException:
        connection.rollback()
        raise

def verify_source_schema(db):
    for table in ('sf311_source_case_ledger', 'sf311_source_media_ledger', 'report_extra_images'):
        require(db.scalar("SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='"
                          + table + "'") == 'InnoDB', 'source_atomic_tables_must_be_innodb')
    for table, expected in (('sf311_source_case_ledger', 'source_namespace,source_id'),
                            ('sf311_source_media_ledger', 'source_namespace,source_id,position')):
        rows = db.query("SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ',') FROM information_schema.STATISTICS "
                        "WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='" + table + "' AND INDEX_NAME='PRIMARY'")
        require(rows == [expected], 'source_case_identity_key_changed')
    rows = db.query("SELECT INDEX_NAME,COLUMN_NAME,NON_UNIQUE FROM information_schema.STATISTICS "
                    "WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='sf311_source_media_ledger' "
                    "AND INDEX_NAME IN ('idx_sf311_original_sha','idx_sf311_display_sha') ORDER BY INDEX_NAME,SEQ_IN_INDEX")
    require(rows == ['idx_sf311_display_sha\tdisplay_sha256\t1',
                     'idx_sf311_original_sha\toriginal_sha256\t1'], 'source_media_hash_indexes_changed')
    require(db.scalar("SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() "
                      "AND TABLE_NAME='sf311_source_media_ledger' AND NON_UNIQUE=0 AND INDEX_NAME<>'PRIMARY'") == '0',
            'image_hash_must_not_be_source_identity')
    for table, expected in SOURCE_COLUMNS.items():
        rows = db.query("SELECT COLUMN_NAME,DATA_TYPE,IS_NULLABLE,COALESCE(CHARACTER_MAXIMUM_LENGTH,0),"
                        "COALESCE(COLLATION_NAME,'') FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() "
                        "AND TABLE_NAME='" + table + "'")
        actual = {}
        for raw in rows:
            name, typ, nullable, length, collation = raw.split('\t')
            actual[name] = (typ, nullable, int(length), collation)
        require(set(actual) == set(expected), 'source_columns_changed')
        require(all(actual[name][:3] == spec[:3] and (spec[3] is None or actual[name][3] == spec[3])
                    for name, spec in expected.items()), 'source_column_type_or_identity_collation_changed')
    require(db.scalar("SELECT COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() "
                      "AND TABLE_NAME='sf311_source_media_ledger' AND COLUMN_NAME='position'") == 'tinyint unsigned',
            'source_media_position_type_changed')
    foreign = db.query("SELECT k.TABLE_NAME,k.COLUMN_NAME,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,r.DELETE_RULE "
        "FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r "
        "ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME AND r.TABLE_NAME=k.TABLE_NAME "
        "WHERE k.TABLE_SCHEMA=DATABASE() AND k.TABLE_NAME IN ('sf311_source_case_ledger','sf311_source_media_ledger') "
        "AND k.REFERENCED_TABLE_NAME IS NOT NULL ORDER BY k.TABLE_NAME,k.COLUMN_NAME")
    parts = [row.split('\t') for row in foreign]
    require(all(len(row) == 5 for row in parts), 'source_foreign_key_metadata_shape')
    # InnoDB reports canonical default media FKs as NO ACTION on this server;
    # RESTRICT has identical immediate enforcement. The report deletion FK
    # must retain SET NULL, and metadata result order carries no semantics.
    normalized = sorted(tuple(row[:4]) + ('RESTRICT' if row[0] == 'sf311_source_media_ledger'
        and row[4] in ('NO ACTION', 'RESTRICT') else row[4],) for row in parts)
    require(normalized == sorted([
        ('sf311_source_case_ledger', 'report_seq', 'reports', 'seq', 'SET NULL'),
        ('sf311_source_media_ledger', 'source_id', 'sf311_source_case_ledger', 'source_id', 'RESTRICT'),
        ('sf311_source_media_ledger', 'source_namespace', 'sf311_source_case_ledger', 'source_namespace', 'RESTRICT')]),
        'source_delete_and_identity_foreign_keys_changed')

class ProductionGuard:
    def __init__(self, controls):
        self.controls = controls
        self.resources = contextlib.ExitStack()
        self.db = None
        self.pending_source = None

    def unchanged(self):
        stopped()
        trusted(COLLECTION_STATE, self.controls['collection_state_sha256'], private=True)
        wal = Path(str(COLLECTION_STATE) + '-wal')
        require(not wal.exists() and not wal.is_symlink(), 'collector_must_be_closed_and_checkpointed')
        allowed = {str(POLICY), str(OLM / 'evidence/receiver-config.json'),
                   str(OLM / 'state/receiver/receiver.sqlite3'),
                   str(OLM / 'state/receiver/receiver.sqlite3-wal')}
        files = self.controls['olm_unchanged_files']
        require(set(files) <= allowed and {str(POLICY), str(OLM / 'evidence/receiver-config.json'),
                str(OLM / 'state/receiver/receiver.sqlite3')} <= set(files), 'incomplete_olm_stop_identity')
        for path, digest in files.items():
            trusted(path, digest)
        expires = dt.datetime.fromisoformat(self.controls['expires_at'].replace('Z', '+00:00'))
        now = dt.datetime.now(dt.timezone.utc)
        require(expires.tzinfo is not None and now < expires <= now + dt.timedelta(minutes=30),
                'bounded_controls_expired_or_overlong')
        require(os.statvfs(ROOT).f_bavail * os.statvfs(ROOT).f_frsize >= 40 * 1024**3,
                'production_capacity_reserve')

    def __enter__(self):
        try:
            require(os.geteuid() == 0 and sys.platform.startswith('linux'), 'aws_linux_root_required')
            os.umask(0o077)
            trusted(ROOT / 'evidence/controls.json', private=True)
            active = Path('/etc/cleanapp-runtime/ACTIVE')
            isolated = Path('/etc/cleanapp-runtime/ISOLATED')
            trusted(active)
            require(not isolated.exists() and not isolated.is_symlink(), 'isolated_runtime_refused')
            info = LOCK.lstat()
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == 1003
                    and stat.S_IMODE(info.st_mode) == 0o600, 'shared_lock_identity')
            fd = os.open(LOCK, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
            self.lock = self.resources.enter_context(os.fdopen(fd, 'r+'))
            opened = os.fstat(fd)
            require((info.st_dev, info.st_ino) == (opened.st_dev, opened.st_ino), 'shared_lock_replaced')
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            current = LOCK.lstat()
            require((current.st_dev, current.st_ino) == (opened.st_dev, opened.st_ino), 'shared_lock_replaced')
            trusted(COLLECTION_LOCK, private=True)
            collection_fd = os.open(COLLECTION_LOCK, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            self.collection_lock = self.resources.enter_context(os.fdopen(collection_fd, 'r'))
            fcntl.flock(self.collection_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.unchanged()
            self.collection = self.resources.enter_context(contextlib.closing(sqlite3.connect(
                COLLECTION_STATE.as_uri() + '?mode=ro&immutable=1', uri=True)))
            self.collection.execute('PRAGMA query_only=ON')
            policy = document(POLICY, self.controls['olm_base_policy_sha256'])
            require(policy.get('kind') == 'cleanapp_olm_receiver_base_policy.v1'
                    and policy.get('source_namespace') == 'openlittermap.com', 'unexpected_deployed_policy')
            for name, digest in policy['consumer_sources'].items():
                require(Path(name).name == name and name.endswith('.py'), 'invalid_deployed_library_name')
                trusted(LIB / name, digest)
            protocol = policy['journal_protocol']
            require(Path(protocol['path']) == OLM / 'app/native_import/journal/cursor_protocol.py',
                    'unexpected_journal_library_path')
            trusted(protocol['path'], protocol['sha256'])
            # The library location is fixed, root-owned and digest checked. The
            # CLI and source input cannot select another module implementation.
            for name in ('epoch_driver', 'epoch', 'live_controls', 'mutation_state', 'mysql_transport'):
                require(name not in sys.modules, 'unexpected_preloaded_native_library')
            sys.path.insert(0, str(LIB))
            try:
                driver = importlib.import_module('epoch_driver')
                self.native = importlib.import_module('epoch')
                self.live = importlib.import_module('live_controls')
                mutation = importlib.import_module('mutation_state')
                transport = importlib.import_module('mysql_transport')
            finally:
                sys.path.pop(0)
            driver.verify_host(policy)
            driver.base_policy(POLICY)  # Existing actual-MySQL/source/backup bindings.
            guard = document(ROOT / 'evidence/live-guards.json', self.controls['live_guard_receipt_sha256'])
            self.live.validate_guard(guard, dt.datetime.now(dt.timezone.utc))
            coverage = document(ROOT / 'evidence/coverage.json', self.controls['coverage_receipt_sha256'])
            self.live.validate_coverage(coverage, dt.datetime.now(dt.timezone.utc))
            self.coverage = self.resources.enter_context(contextlib.closing(self.native.read_db(coverage['snapshot_file'])))
            state = ROOT / 'state/mutation-state.sqlite3'
            trusted(state, self.controls['mutation_state_initial_sha256'], private=True)
            self.mutations = mutation.Mutations(state, mutation.load_protocol(protocol))
            self.resources.callback(self.mutations.close)
            require(self.mutations.cursor() == self.controls['mutation_safe_cursor'], 'mutation_snapshot_cursor_mismatch')
            self.db = self.resources.enter_context(transport.Mysql())
            driver.server_check(self.db, policy)
            verify_source_schema(self.db)
            require(self.db.scalar("SELECT GET_LOCK('cleanapp-olm-native-import',0)") == '1', 'olm_advisory_lock_busy')
            self.resources.callback(lambda: self.db.query("SELECT RELEASE_LOCK('cleanapp-olm-native-import')"))
            baseline = self.mutations.baseline(self.db, self.coverage)
            # No image-only equivalence exemption is available across sources.
            class StrictTail(driver.TailCheck):
                def known_olm_media_reuse(self, db, seq, candidate):
                    return False
            self.tail = StrictTail(coverage['cutoff_seq'], coverage['snapshot_file'],
                                  dt.datetime.now(dt.timezone.utc).isoformat(), self.mutations, baseline)
            self.resources.callback(self.tail.close)
            self.sql = transport.sql_string
            self.media_module = self._sibling('media', self.controls['media_source_sha256'])
            self.collect_module = self._sibling('collect', self.controls['collect_source_sha256'])
            self.verify_test_receipt()
            self.archive_store = self.media_module.production_archive().store
            self.downloader = self.media_module.Downloader()
            return self
        except BaseException:
            self.resources.close()
            raise

    def _sibling(self, name, expected):
        path = ROOT / ('app/' + name + '.py')
        trusted(path, expected)
        spec = importlib.util.spec_from_file_location('solvesf_guard_' + name, path)
        module = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = module
        spec.loader.exec_module(module)
        return module

    def verify_test_receipt(self):
        expected = {name: trusted(ROOT / ('app/' + name)) for name in ('apply.py', 'guard.py', 'media.py', 'collect.py')}
        expected['sql_helper'] = trusted(self.media_module.apply.HELPERS)
        expected['schema.sql'] = trusted(ROOT / 'app/schema.sql')
        receipt = document(ROOT / 'evidence/actual-tests.json', self.controls['actual_test_receipt_sha256'])
        require(receipt.get('kind') == 'cleanapp_solvesf_actual_tests.v1'
                and receipt.get('actual_mysql_passed') is True and receipt.get('guard_tests_passed') is True
                and receipt.get('media_tests_passed') is True and receipt.get('source_sha256') == expected,
                'current_solvesf_actual_test_evidence_missing')

    def fresh_case(self, candidate):
        ident = candidate['source_id']
        try:
            record = self.collect_module.public_get('https://solvesf.com/api/report/' + ident,
                                                    self.collect_module.MAX_DETAIL)
        except Exception as error:
            if getattr(error, 'status', None) == 404:
                self.persist_source_hold(ident, 'guard_detail_not_found')
            raise
        try:
            self.collect_module.public_record(record)
        except self.collect_module.CollectError:
            # public_record's only rejection of a dictionary is an explicit
            # privacy/deletion flag. A malformed non-object is a protocol error.
            if isinstance(record, dict):
                self.persist_source_hold(ident, 'guard_nonpublic_or_deleted')
            raise
        try:
            current = self.collect_module.normalize(ident, record)
        except self.collect_module.CollectError:
            self.persist_source_hold(ident, 'guard_case_material_changed')
            raise
        if current.get('candidate_sha256') != candidate['candidate_sha256']:
            self.persist_source_hold(ident, 'guard_case_material_changed')
            raise GuardError('source_case_changed_or_withdrawn')

    def persist_source_hold(self, ident, reason):
        # This deliberate source-history write invalidates the controls hash.
        # The caller always raises immediately; exception exit performs cleanup
        # without trying to bless the changed collection state as current.
        fcntl.flock(self.collection_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        trusted(COLLECTION_STATE, self.controls['collection_state_sha256'], private=True)
        with contextlib.closing(sqlite3.connect(COLLECTION_STATE.as_uri() + '?mode=rw', uri=True, timeout=2)) as connection:
            save_observed_hold(connection, ident, reason)

    def fresh_photo(self, candidate, ref):
        try:
            current, current_type = self.downloader.fetch(ref['source_url'])
        except Exception as error:
            if getattr(error, 'status', None) == 404:
                self.persist_source_hold(candidate['source_id'], 'guard_photo_not_found')
            raise
        if (hashlib.sha256(current).hexdigest() != ref['original']['sha256']
                or len(current) != ref['original']['bytes']):
            self.persist_source_hold(candidate['source_id'], 'guard_photo_material_changed')
            raise GuardError('source_photo_changed_or_withdrawn')
        return current_type

    def _object(self, ref, maximum):
        version = ref.get('version_id')
        require(ref.get('bucket') == BUCKET and 'version_id' in ref
                and (version is None or isinstance(version, str) and version not in ('', 'null'))
                and isinstance(ref.get('key'), str) and ref['key'].startswith('import/solvesf/')
                and not any(x in ('', '.', '..') for x in ref['key'].split('/'))
                and re.fullmatch('[0-9a-f]{64}', ref.get('sha256', '')) is not None
                and type(ref.get('bytes')) is int and 0 < ref['bytes'] <= maximum, 'untrusted_archive_reference')
        arguments = ['s3api', 'head-object', '--bucket', BUCKET, '--key', ref['key'], '--checksum-mode', 'ENABLED']
        if version is not None:
            arguments.extend(['--version-id', version])
        head = self.archive_store.run_json(arguments)
        actual_version = head.get('VersionId')
        if actual_version == 'null':
            actual_version = None
        require(head.get('ContentLength') == ref['bytes'] and head.get('ServerSideEncryption') == 'AES256'
                and actual_version == version
                and head.get('ChecksumSHA256') == base64.b64encode(bytes.fromhex(ref['sha256'])).decode(),
                'archived_media_head_mismatch')
        # Effective privacy of this authenticated-existing object is checked
        # afresh without requiring bucket-management permissions or changing IAM.
        self.media_module.assert_object_private(ref['key'], version)
        download = self.archive_store.download(ref['key'], version)
        try:
            body = download.stream.read(ref['bytes'] + 1)
            download.finish()
        except BaseException:
            download.abort()
            raise
        require(len(body) == ref['bytes'] and hashlib.sha256(body).hexdigest() == ref['sha256'],
                'archived_media_readback_mismatch')
        return body

    def verified_media(self, candidate, manifest):
        self.unchanged()
        eligible_collection_case(self.collection, candidate)
        # Current public source visibility/material is checked again, not
        # inferred from an archived historical candidate or its stored URL.
        self.fresh_case(candidate)
        require(manifest.get('schema') == 'cleanapp-solvesf-media-v1'
                and manifest.get('candidate_file_sha256') == self.controls['candidate_sha256'], 'media_manifest_binding')
        case = manifest['cases'][candidate['source_id']]
        require(case['candidate_sha256'] == candidate['candidate_sha256']
                and case['source_id'] == candidate['source_id'] and case['source_namespace'] == 'sf311', 'case_media_binding')
        require(self._object(case['source_record_ref'], 256 * 1024) == canonical(candidate).encode(), 'archived_source_mismatch')
        binding = {k: v for k, v in case.items() if k != 'binding_ref'}
        require(self._object(case['binding_ref'], 256 * 1024) == canonical(binding).encode(), 'archived_binding_mismatch')
        refs, bodies = case['items'], []
        require(len(refs) <= 4 and [r['source_url'] for r in refs] == candidate['media_urls'], 'photo_order_mismatch')
        from PIL import Image
        for ref in refs:
            require(ref['original']['key'] == 'import/solvesf/originals/sha256/' + ref['original']['sha256']
                    and ref['display']['key'] == 'import/solvesf/previews/solvesf-display-jpeg-v1/sha256/'
                        + ref['display']['sha256'] + '.jpg', 'noncanonical_media_object_path')
            original = self._object(ref['original'], 20 * 1024**2)
            current_type = self.fresh_photo(candidate, ref)
            display = self._object(ref['display'], 128 * 1024)
            d = ref['display']
            require(d.get('projection_version') == 'solvesf-display-jpeg-v1'
                    and d.get('metadata_removed') is True, 'unreviewed_display_projection')
            with Image.open(io.BytesIO(display)) as image:
                require(image.format == 'JPEG' and image.size == (d['width'], d['height'])
                        and not image.getexif() and not any(k in image.info for k in ('exif', 'xmp', 'icc_profile', 'comment')),
                        'native_display_verification')
                image.verify()
            with Image.open(io.BytesIO(original)) as source:
                declared = {'JPEG': 'image/jpeg', 'PNG': 'image/png', 'WEBP': 'image/webp'}.get(source.format)
            require(current_type == declared, 'source_photo_type_changed')
            reproduced, details = self.media_module.derive(original, declared)
            require(display == reproduced and all(d.get(k) == value for k, value in details.items()),
                    'preview_not_reproducible_from_archived_source')
            bodies.append(display)
        return refs, bodies

    def preinsert(self, db, candidate, refs):
        self.unchanged()
        indexed_media_conflict(db, refs)
        # SQL/SQLite NULL cannot equal an asset digest. A text-only observation
        # therefore runs the complete reviewed boundary/suffix protocol with no
        # candidate media, without falsely matching other empty native images.
        pairs = refs or [{'original': {'sha256': None}, 'display': {'sha256': None}}]
        for ref in pairs:
            self.tail(db, {'source_id': candidate['source_id'],
                          'original_media': ref['original'], 'display_media': ref['display']})
        self.pending_source = candidate['source_id']

    def seed_olm_holds(self, seq, refs):
        self.unchanged()
        path = OLM / 'state/dedup/mutation-state.sqlite3'
        trusted(path, private=True)
        with contextlib.closing(sqlite3.connect(path, timeout=2)) as connection:
            seed_positive_assets(connection, seq, case_media_hashes(refs))

    def record_insert(self, seq, candidate, refs):
        self.tail.owned.add(seq)
        for ref in refs:
            self.tail.record_insert(seq, {'original_media': ref['original'], 'display_media': ref['display']})
        # Called only AFTER native SQL commit. These sticky positive holds make
        # the existing OLM writer see the new source's full original hashes.
        # A failure is fatal; the operator must repair/verify before OLM resume.
        self.seed_olm_holds(seq, refs)
        self.pending_source = None

    def verify_replay(self, db, seq, candidate, refs):
        self.unchanged()
        q = self.sql
        # Replay has no preinsert call. Consume the same actual locking journal
        # suffix here too, so a private->public toggle since capture stays held.
        is_pending_insert = self.pending_source == candidate['source_id']
        if not is_pending_insert:
            self.mutations.locked_changes(db, self.tail.journal_cutoff, self.coverage)
        require(not self.mutations.db.execute('SELECT 1 FROM restricted_reports WHERE report_seq=?',
                                              (int(seq),)).fetchone(), 'historically_restricted_case')
        public = 'rpt_' + base64.urlsafe_b64encode(hashlib.sha256(
            ('sf311/case/' + candidate['source_id']).encode()).digest()[:16]).decode().rstrip('=')
        reporter = 'historical:sf311:' + hashlib.sha256(candidate['source_id'].encode()).hexdigest()
        compact, full = self.media_module.apply.presentation(candidate)
        require(compact == html.escape('source: SolveSF.com\n' + candidate['text'], quote=True)[:255],
                'compact_issue_projection_changed')
        material = hashlib.sha256(canonical({'candidate_sha256': candidate['candidate_sha256'], 'media': refs}).encode()).hexdigest()
        observed = dt.datetime.fromisoformat(candidate['requested_at'].replace('Z', '+00:00'))
        unix_seconds = int(observed.timestamp())
        flags = ' AND '.join('p.' + flag + '=1' for flag in FLAGS)
        rows = db.query('SELECT r.public_id,LOWER(SHA2(r.image,256)),p.policy_version FROM reports r '
            'JOIN report_raw rr ON rr.report_seq=r.seq JOIN report_side_effect_policy p ON p.report_seq=r.seq '
            'JOIN report_analysis a ON a.seq=r.seq LEFT JOIN reports_gdpr g ON g.seq=r.seq '
            'LEFT JOIN report_evidence_source_projection e ON e.seq=r.seq '
            'WHERE r.seq=' + str(int(seq)) + ' AND BINARY rr.fetcher_id=BINARY ' + q('sf311')
            + ' AND BINARY rr.source_id=BINARY ' + q(candidate['source_id'])
            + " AND BINARY rr.visibility=BINARY 'public' AND BINARY p.source_namespace=BINARY " + q('sf311')
            + ' AND p.policy_version=1 AND ' + flags + ' AND g.seq IS NULL AND COALESCE(e.privacy_restricted,0)=0'
            + ' AND EXISTS(SELECT 1 FROM external_ingest_index x WHERE x.seq=r.seq AND BINARY x.source=BINARY '
            + q('sf311') + ' AND BINARY x.external_id=BINARY ' + q(candidate['source_id'])
            + ' AND BINARY x.source_url=BINARY ' + q(candidate['source_url'])
            + ' AND x.source_timestamp=' + q(observed.replace(tzinfo=None).isoformat(sep=' ', timespec='seconds')) + ')'
            + ' AND BINARY r.public_id=BINARY ' + q(public) + ' AND BINARY r.id=BINARY ' + q(reporter)
            + ' AND r.team=0 AND r.x=0 AND r.y=0 AND r.action_id IS NULL'
            + ' AND UNIX_TIMESTAMP(r.ts)=' + str(unix_seconds)
            + ' AND CAST(r.latitude AS DOUBLE)=CAST(CAST(' + q(candidate['latitude']) + ' AS FLOAT) AS DOUBLE)'
            + ' AND CAST(r.longitude AS DOUBLE)=CAST(CAST(' + q(candidate['longitude']) + ' AS FLOAT) AS DOUBLE)'
            + ' AND BINARY r.description=BINARY ' + q(compact)
            + " AND BINARY a.title=BINARY 'Historical SF311 case via SolveSF' AND BINARY a.description=BINARY " + q(full)
            + ' AND BINARY a.summary=BINARY ' + q(compact)
            + " AND a.needs_ai_review=0 AND BINARY a.inferred_contact_emails=BINARY '[]' AND a.severity_level=0"
            + " AND BINARY a.language=BINARY 'en' AND BINARY a.brand_name=BINARY '' AND BINARY a.brand_display_name=BINARY ''"
            + ' AND EXISTS(SELECT 1 FROM sf311_source_case_ledger l WHERE l.report_seq=r.seq AND l.recorded_report_seq=r.seq'
            + ' AND l.source_namespace=' + q('sf311') + ' AND l.source_id=' + q(candidate['source_id'])
            + ' AND l.payload_sha256=' + q(candidate['candidate_sha256']) + ' AND l.material_sha256=' + q(material)
            + ' AND BINARY l.report_public_id=BINARY r.public_id AND BINARY l.publisher=BINARY ' + q('SolveSF')
            + ' AND BINARY l.source_url=BINARY ' + q(candidate['source_url']) + " AND l.disposition='inserted'"
            + ' AND l.candidate_json=CAST(' + q(canonical(candidate)) + ' AS JSON)'
            + ' AND l.source_record_json=CAST(' + q(canonical(candidate['source_record'])) + ' AS JSON)'
            + ' AND l.media_json=CAST(' + q(canonical(refs)) + ' AS JSON))'
            + " AND a.is_valid=1 AND BINARY a.source=BINARY 'SolveSF' AND BINARY a.classification=BINARY 'physical' FOR UPDATE")
        require(len(rows) == 1, 'changed_or_private_native_case')
        values = rows[0].split('\t')
        primary = refs[0]['display']['sha256'] if refs else hashlib.sha256(b'').hexdigest()
        require(values[1:] == [primary, '1'], 'native_primary_or_policy_drift')
        extra = db.query('SELECT position,LOWER(SHA2(image,256)),sha256 FROM report_extra_images WHERE seq='
                         + str(int(seq)) + ' ORDER BY position FOR UPDATE')
        require(extra == [str(i) + '\t' + ref['display']['sha256'] + '\t' + ref['display']['sha256']
                          for i, ref in enumerate(refs[1:], 1)], 'native_extra_media_drift')
        require(db.scalar('SELECT COUNT(*) FROM sf311_source_media_ledger WHERE source_namespace='
                    + q('sf311') + ' AND source_id=' + q(candidate['source_id'])) == str(len(refs)),
                'case_media_ledger_count_drift')
        for index, ref in enumerate(refs):
            rows = db.query('SELECT media_json=CAST(' + q(canonical(ref)) + ' AS JSON) AND original_sha256='
                + q(ref['original']['sha256']) + ' AND display_sha256=' + q(ref['display']['sha256']) + ' FROM sf311_source_media_ledger '
                'WHERE source_namespace=' + q('sf311') + ' AND source_id=' + q(candidate['source_id'])
                + ' AND position=' + str(index) + ' FOR UPDATE')
            require(rows == ['1'], 'case_media_ledger_drift')
        if not is_pending_insert:
            # A replay is already committed before this read-only transaction.
            # Repairing a missing conservative hold is safe and idempotent.
            self.seed_olm_holds(int(seq), refs)

    def __exit__(self, exc_type, *_):
        try:
            if exc_type is None:
                self.unchanged()
        finally:
            self.resources.close()

def production_guard(controls):
    return ProductionGuard(controls)
