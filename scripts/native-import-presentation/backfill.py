#!/usr/bin/env python3
"""Bounded host-local source presentation repair. Dry run is the default.

Only exact complete legacy analysis presentations are changed. Retained report
description, analysis.description, provenance, identities, media and quiet
policies never change. Avoiding a reports UPDATE also avoids ROW/FULL image
before/after binlog payloads for these image-bearing records.
Apply requires paused importers and an externally held deployment boundary.
Run each finite seq interval once; resume with the receipt's last_seq and a NEW
private journal. A prepared journal entry alone does not prove a commit.
"""
from __future__ import annotations

import argparse
from collections import OrderedDict
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True

OLM_APP = Path('/srv/cleanapp-olm-import/app')
SSF_APP = Path('/srv/cleanapp-solvesf-import/app')
SERVER_UUID = 'f2c484f6-3c5d-11ef-aace-0242ac120003'
FLAGS = ('suppress_email', 'suppress_notifications', 'suppress_rewards',
         'suppress_routing', 'suppress_analysis')
SOURCES = {'olm': ('openlittermap.com', 'olm_source_observation_ledger'),
           'ssf': ('sf311', 'sf311_source_case_ledger')}
MAX_RAW = 256 * 1024
MAX_CATALOGUE = 2 * 1024 * 1024


class Refused(RuntimeError):
    """Sanitized failure: never include database content in the message."""


def require(value, reason):
    if not value:
        raise Refused(reason)


def positive(value, *, zero=False):
    require(type(value) is int and (0 if zero else 1) <= value <= 2147483647,
            'Invalid bounded sequence')
    return str(value)


def sql_string(value):
    # Same introduced UTF8 HEX literal as the reviewed host transport.
    if value is None:
        return 'NULL'
    require(isinstance(value, str) and len(value.encode()) <= MAX_CATALOGUE,
            'Invalid bounded SQL text')
    return "_utf8mb4 X'" + value.encode('utf-8').hex() + "'"


def source_spec(kind):
    require(kind in SOURCES, 'Unreviewed source')
    return SOURCES[kind]


def scope(kind):
    namespace, _ = source_spec(kind)
    checks = ['BINARY l.source_namespace=BINARY ' + sql_string(namespace),
              "BINARY l.disposition=BINARY 'inserted'",
              'l.recorded_report_seq=r.seq',
              'BINARY p.source_namespace=BINARY ' + sql_string(namespace),
              'p.policy_version=1', *(f'p.{flag}=1' for flag in FLAGS)]
    if kind == 'olm':
        checks += ['l.policy_version=1', *(f'l.{flag}=1' for flag in FLAGS)]
    return ' AND '.join(checks)


def joins(kind):
    _, table = source_spec(kind)
    index = 'idx_olm_ledger_report' if kind == 'olm' else 'idx_sf311_ledger_report'
    return (table + ' l FORCE INDEX (' + index + ') JOIN reports r ON r.seq=l.report_seq '
            'JOIN report_side_effect_policy p ON p.report_seq=r.seq '
            'JOIN report_analysis a ON a.seq=r.seq')


def select_sql(kind, after, through, batch, *, locked=False):
    positive(after, zero=True)
    positive(through)
    require(after < through and type(batch) is int and 1 <= batch <= 500,
            'Invalid finite selection bounds')
    # HEX(JSON_ARRAY) has a single unambiguous line per row, retaining NULLs,
    # Unicode, tabs and newlines without exposing them in transport diagnostics.
    fields = ['r.seq', 'l.source_id', 'r.public_id', 'r.description', 'a.title',
              'a.summary', 'a.source', 'a.description']
    if kind == 'olm':
        fields += [f'IF(OCTET_LENGTH(l.original_feature_json_raw)<={MAX_RAW},l.original_feature_json_raw,NULL)',
                   'l.acquisition_evidence', 'l.tag_catalogue_sha256',
                   'l.raw_feature_sha256', 'l.source_feature_sha256']
    else:
        fields += [f'IF(OCTET_LENGTH(l.candidate_json)<={MAX_RAW},l.candidate_json,NULL)',
                   'l.payload_sha256']
    # Actual production EXPLAIN otherwise starts at the policy range and
    # filesorts hundreds of thousands of rows before applying this tiny limit.
    return ('SELECT STRAIGHT_JOIN HEX(JSON_ARRAY(' + ','.join(fields) + ')) FROM ' + joins(kind)
            + ' WHERE l.report_seq>' + str(after) + ' AND l.report_seq<=' + str(through)
            + ' AND ' + scope(kind) + ' ORDER BY l.report_seq LIMIT ' + str(batch)
            + (' FOR UPDATE' if locked else ''))


def parse_rows(raw_rows):
    rows, seen = [], set()
    for raw in raw_rows:
        try:
            require(isinstance(raw, str) and len(raw) <= 2 * (MAX_RAW + 131072),
                    'Oversized presentation row')
            row = json.loads(bytes.fromhex(raw))
        except (ValueError, UnicodeError, TypeError):
            raise Refused('Invalid encoded presentation row') from None
        require(isinstance(row, list) and len(row) in (10, 13), 'Unexpected row shape')
        positive(row[0])
        require(row[0] not in seen, 'Multiple analysis rows for one report; preserve and review')
        require(not rows or row[0] > rows[-1][0], 'Nonmonotonic report sequence')
        require(isinstance(row[1], str) and re.fullmatch(r'[1-9][0-9]{0,31}', row[1]),
                'Invalid retained source identity')
        require(isinstance(row[8], (str, dict)), 'Missing or oversized retained source payload')
        seen.add(row[0])
        rows.append(row)
    return rows


def presentation_tuple(row):
    return tuple(row[3:7])


def decision(current, legacy, new, current_full, expected_full):
    require(len(current) == len(legacy) == len(new) == 4, 'Invalid presentation tuple')
    if current_full != expected_full:
        return 'preserved_other'
    if tuple(current) == tuple(new):
        return 'already_new'
    if tuple(current) == tuple(legacy):
        return 'would_update'
    # Includes partially changed, mixed legacy/new and user-curated records.
    return 'preserved_other'


def exact(column, value):
    return 'BINARY ' + column + ' <=> BINARY ' + sql_string(value)


def update_sql(kind, row, legacy, new, full):
    require(decision(presentation_tuple(row), legacy, new, row[7], full) == 'would_update',
            'CAS repair requires complete exact legacy presentation')
    source_spec(kind)
    require(new[3] == ('OLM' if kind == 'olm' else 'SSF'), 'Wrong display source')
    require(new[0] == legacy[0], 'Retained report description must remain unchanged')
    require(legacy[3] == ('OpenLitterMap.com' if kind == 'olm' else 'SolveSF'),
            'Wrong legacy source')
    where = [scope(kind), 'r.seq=' + positive(row[0]),
             exact('l.source_id', row[1]), exact('r.public_id', row[2]),
             *[exact(column, value) for column, value in zip(
                 ('r.description', 'a.title', 'a.summary', 'a.source'), legacy)],
             exact('a.description', full)]
    setters = ','.join(column + '=' + sql_string(value) for column, value in zip(
        ('a.title', 'a.summary', 'a.source'), new[1:]))
    return 'UPDATE ' + joins(kind) + ' SET ' + setters + ' WHERE ' + ' AND '.join(where)


TEMP = 'tmp_native_import_presentation'
DEPLOYMENT_LOCK = Path('/opt/cleanapp/deploy/.deployment-and-backup.lock')
TEMP_COLUMNS = ('seq', 'source_id', 'public_id', 'old_description', 'old_title',
                'old_summary', 'old_source', 'full_description', 'new_description',
                'new_title', 'new_summary', 'new_source')


def create_temp_sql():
    return ('CREATE TEMPORARY TABLE ' + TEMP + ' ('
            'seq INT PRIMARY KEY,source_id VARCHAR(64),public_id VARCHAR(32),'
            'old_description TEXT,old_title TEXT,old_summary TEXT,old_source VARCHAR(128),'
            'full_description MEDIUMTEXT,new_description TEXT,new_title TEXT,'
            'new_summary TEXT,new_source VARCHAR(128)) ENGINE=InnoDB '
            'DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin')


def temp_insert_sql(pending):
    require(1 <= len(pending) <= 500, 'Invalid temporary presentation batch')
    values = []
    seen = set()
    for row, legacy, new, full in pending:
        require(row[0] not in seen and decision(presentation_tuple(row), legacy, new,
                row[7], full) == 'would_update', 'Invalid or mixed temporary presentation')
        require(new[0] == legacy[0], 'Retained report description must remain unchanged')
        seen.add(row[0])
        values.append('(' + positive(row[0]) + ',' + ','.join(sql_string(value) for value in
            (row[1], row[2], *legacy, full, *new)) + ')')
    sql = 'INSERT INTO ' + TEMP + ' (' + ','.join(TEMP_COLUMNS) + ') VALUES ' + ','.join(values)
    require(len(sql.encode()) <= 24 * 1024 * 1024, 'Presentation batch exceeds finite SQL packet budget')
    return sql


def temp_match(*, new):
    prefix = 'new_' if new else 'old_'
    fields = [('r.description', prefix + 'description'), ('a.title', prefix + 'title'),
              ('a.summary', prefix + 'summary'), ('a.source', prefix + 'source'),
              ('a.description', 'full_description'), ('l.source_id', 'source_id'),
              ('r.public_id', 'public_id')]
    return ' AND '.join('BINARY ' + column + ' <=> BINARY t.' + temp for column, temp in fields)


def bulk_update_sql(kind):
    expected_new = 'OLM' if kind == 'olm' else 'SSF'
    expected_old = 'OpenLitterMap.com' if kind == 'olm' else 'SolveSF'
    return ('UPDATE ' + joins(kind) + ' JOIN ' + TEMP + ' t ON t.seq=r.seq SET '
            'a.title=t.new_title,'
            'a.summary=t.new_summary,a.source=t.new_source WHERE ' + scope(kind)
            + ' AND ' + temp_match(new=False)
            + ' AND BINARY t.new_description <=> BINARY t.old_description'
            + ' AND BINARY t.new_source=BINARY ' + sql_string(expected_new)
            + ' AND BINARY t.old_source=BINARY ' + sql_string(expected_old))


def postcheck_sql(kind):
    return ('SELECT COUNT(*) FROM ' + joins(kind) + ' JOIN ' + TEMP
            + ' t ON t.seq=r.seq WHERE ' + scope(kind) + ' AND ' + temp_match(new=True))


def trusted(path):
    path = Path(path)
    require(path.is_absolute(), 'Runtime path must be absolute')
    for item in (path, *path.parents):
        info = item.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0
                and info.st_mode & 0o022 == 0, 'Untrusted runtime path')
    return path


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, trusted(path))
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


class Deriver:
    def __init__(self, db, kind):
        self.db, self.kind, self.catalogues = db, kind, OrderedDict()
        sys.path.insert(0, str(trusted(OLM_APP / 'native_import/apply')))
        if kind == 'olm':
            self.stage = load('presentation_legacy_candidate', OLM_APP / 'native_import/candidate_stage.py')
            self.display = load('presentation_olm_epoch', OLM_APP / 'native_import/apply/epoch.py')
        else:
            self.display = load('presentation_solvesf_apply', SSF_APP / 'apply.py')

    def catalogue(self, digest):
        require(isinstance(digest, str) and re.fullmatch('[a-f0-9]{64}', digest),
                'Invalid retained catalogue hash')
        if digest not in self.catalogues:
            rows = self.db.query('SELECT HEX(raw_catalogue_json) FROM olm_source_catalogues WHERE '
                'catalogue_sha256=' + sql_string(digest)
                + ' AND BINARY source_namespace=BINARY ' + sql_string(SOURCES['olm'][0])
                + f' AND OCTET_LENGTH(raw_catalogue_json)<={MAX_CATALOGUE}')
            require(len(rows) == 1, 'Retained catalogue missing or oversized')
            raw = bytes.fromhex(rows[0])
            require(hashlib.sha256(raw).hexdigest() == digest, 'Retained catalogue digest mismatch')
            self.catalogues[digest] = json.loads(raw)
            if len(self.catalogues) > 8:
                self.catalogues.popitem(last=False)
        self.catalogues.move_to_end(digest)
        return self.catalogues[digest]

    def derive(self, row):
        if self.kind == 'olm':
            require(len(row) == 13 and isinstance(row[8], str), 'Invalid OLM retained row')
            raw = row[8]
            require(hashlib.sha256(raw.encode()).hexdigest() == row[11], 'Retained OLM Feature digest mismatch')
            acquisition = json.loads(row[9]) if isinstance(row[9], str) else row[9]
            require(isinstance(acquisition, dict) and set(acquisition) == {'first_seen_at', 'last_seen_at'},
                    'Invalid OLM acquisition evidence')
            candidate = self.stage.make_candidate(raw, int(row[1]), acquisition['first_seen_at'],
                acquisition['last_seen_at'], None, {}, catalogue_sha256=row[10])
            require(candidate['raw_feature_sha256'] == row[11]
                    and candidate['source_feature_sha256'] == row[12]
                    and candidate['public_id'] == row[2], 'Retained OLM source binding mismatch')
            compact = candidate['native_text']['reports_description']
            full = candidate['native_text']['report_analysis_description']
            legacy = (compact, candidate['presentation']['title'], compact, 'OpenLitterMap.com')
            new = self.display.presentation_from_feature(json.loads(raw), full, self.catalogue(row[10]))
            require(new['description'] == full, 'Derived OLM body must remain unchanged')
            updated = (new['reports_description'], new['title'], new['summary'], new['source'])
        else:
            require(len(row) == 10, 'Invalid SolveSF retained row')
            candidate = json.loads(row[8]) if isinstance(row[8], str) else row[8]
            self.display.valid_candidate(candidate)
            require(candidate['source_namespace'] == 'sf311' and candidate['source_id'] == row[1]
                    and candidate['candidate_sha256'] == row[9]
                    and self.display.public_id(candidate) == row[2], 'Retained SolveSF source binding mismatch')
            updated, legacy = self.display.presentation_variants(candidate)
            full = self.display.presentation(candidate)[1]
        require(all(isinstance(value, str) for value in (*updated, *legacy, full)),
                'Invalid reconstructed presentation')
        # Keep the retained source-prefixed compact raw report text unchanged;
        # the clean source-derived feed projection belongs to report_analysis.
        updated = (legacy[0], *updated[1:])
        require(len(updated[0]) <= 255 and updated[3] == ('OLM' if self.kind == 'olm' else 'SSF'),
                'Derived presentation exceeds reviewed contract')
        return tuple(legacy), tuple(updated), full


def paused_importers():
    require(os.geteuid() == 0 and sys.platform == 'linux', 'Apply requires root on AWS Linux')
    for unit in ('olm-native-receiver.service', 'cleanapp-solvesf-import.service'):
        result = subprocess.run(['systemctl', 'show', unit, '--property=LoadState,ActiveState,MainPID'],
                                capture_output=True, text=True, timeout=10)
        require(result.returncode == 0, 'Cannot verify importer unit state')
        values = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
        require(values.get('LoadState') == 'loaded' and values.get('MainPID') == '0'
                and values.get('ActiveState') in ('inactive', 'failed'), 'Importer unit still active')
    own = os.getpid()
    for item in Path('/proc').iterdir():
        if not item.name.isdecimal() or int(item.name) == own:
            continue
        try:
            arguments = (item / 'cmdline').read_bytes().split(b'\0')
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            continue
        for argument in arguments:
            # Match executable script arguments, not wrapper shell/Python text
            # that merely mentions these paths during deployment preparation.
            if ((argument.startswith(b'/srv/cleanapp-olm-import/') and argument.endswith(b'/receive.py'))
                    or (argument.startswith(b'/srv/cleanapp-solvesf-import/')
                        and any(argument.endswith(b'/' + name) for name in
                                (b'service.py', b'controller.py', b'apply.py')))):
                raise Refused('Importer process remains active')


def deployment_entry(info, *, lock=False):
    require(not stat.S_ISLNK(info.st_mode) and info.st_uid == (1003 if lock else 0)
            and info.st_mode & 0o022 == 0, 'Untrusted deployment boundary entry')
    if lock:
        require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600
                and info.st_nlink == 1, 'Deployment lock must be private regular single-link file')
    else:
        require(stat.S_ISDIR(info.st_mode), 'Deployment boundary ancestor must be a directory')


def lock_identity(info):
    deployment_entry(info, lock=True)
    return (info.st_dev, info.st_ino, info.st_uid, stat.S_IMODE(info.st_mode), info.st_nlink)


def deployment_boundary_held():
    path = DEPLOYMENT_LOCK
    # This existing deployment directory and lock intentionally belong to the
    # dedicated deployer (UID1003); higher ancestors must remain root trusted.
    parent = path.parent.lstat()
    require(stat.S_ISDIR(parent.st_mode) and not stat.S_ISLNK(parent.st_mode)
            and parent.st_uid == 1003 and parent.st_mode & 0o022 == 0,
            'Untrusted deployer-owned deployment directory')
    for ancestor in path.parent.parents:
        deployment_entry(ancestor.lstat())
    before = lock_identity(path.lstat())
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        require(lock_identity(os.fstat(fd)) == before, 'Deployment lock changed while opening')
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            require(lock_identity(path.lstat()) == before
                    and lock_identity(os.fstat(fd)) == before,
                    'Deployment lock changed during held-boundary check')
            return
        fcntl.flock(fd, fcntl.LOCK_UN)
        raise Refused('External deployment boundary is not held')
    finally:
        os.close(fd)


class Journal:
    def __init__(self, path):
        path = Path(path)
        require(path.is_absolute() and path.parent.is_dir(), 'Journal requires existing absolute private directory')
        trusted(path.parent)
        require(path.parent.stat().st_mode & 0o077 == 0, 'Journal directory must be root private')
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        self.stream = os.fdopen(fd, 'w', encoding='utf-8')
        os.fsync(fd)
        directory = os.open(path.parent, os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)

    def append(self, record):
        self.append_many([record])

    def append_many(self, records):
        for record in records:
            self.stream.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + '\n')
        self.stream.flush()
        os.fsync(self.stream.fileno())

    def close(self):
        self.stream.close()


def process_batch(db, deriver, kind, rows, *, apply=False, journal=None):
    counts = {'scanned': len(rows), 'would_update': 0, 'updated': 0,
              'already_new': 0, 'preserved_other': 0}
    pending = []
    for row in rows:
        legacy, new, full = deriver.derive(row)
        require(new[3] == ('OLM' if kind == 'olm' else 'SSF')
                and legacy[3] == ('OpenLitterMap.com' if kind == 'olm' else 'SolveSF')
                and new[0] == legacy[0],
                'Unreviewed presentation source')
        state = decision(presentation_tuple(row), legacy, new, row[7], full)
        counts[state] += 1
        if state == 'would_update' and apply:
            require(journal is not None, 'Apply requires private rollback journal')
            pending.append((row, legacy, new, full))
    if pending:
        # Prebuild and validate all bounded SQL before recording durable intent.
        insert = temp_insert_sql(pending)
        journal.append_many({'kind': 'prepared', 'namespace': source_spec(kind)[0],
            'seq': row[0], 'source_id': row[1], 'old': legacy, 'new': new}
            for row, legacy, new, full in pending)
        db.query('DELETE FROM ' + TEMP)
        db.query(insert)
        # Mysql.query appends a SELECT end-marker, which itself resets
        # ROW_COUNT(). Capture the count before that marker in the SAME call.
        changed_rows = db.query(bulk_update_sql(kind) + '; SELECT ROW_COUNT()')
        require(len(changed_rows) == 1 and re.fullmatch('[0-9]+', changed_rows[0]),
                'Invalid presentation CAS affected-row receipt')
        changed = int(changed_rows[0])
        require(changed == len(pending), 'Presentation CAS changed unexpected rows')
        require(db.scalar(postcheck_sql(kind)) == str(len(pending)),
                'Presentation CAS postimage mismatch')
        counts['updated'] = len(pending)
    return counts, [row[0] for row, *_ in pending]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', choices=tuple(SOURCES), required=True)
    parser.add_argument('--through-seq', type=int, required=True)
    parser.add_argument('--after-seq', type=int, default=0)
    parser.add_argument('--batch-size', type=int, default=250)
    parser.add_argument('--max-rows', type=int, default=10000)
    parser.add_argument('--max-seconds', type=int, default=900)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--journal')
    args = parser.parse_args(argv)
    select_sql(args.source, args.after_seq, args.through_seq, args.batch_size)
    require(1 <= args.max_rows <= 1000000 and 1 <= args.max_seconds <= 3600,
            'Invalid finite work budget')
    require(bool(args.journal) == args.apply, 'Journal is required only for apply')
    require(sys.platform == 'linux', 'Host-local operation requires AWS Linux')
    if args.apply:
        paused_importers()
        deployment_boundary_held()
    transport = load('presentation_mysql_transport', OLM_APP / 'native_import/apply/mysql_transport.py')
    totals = {'scanned': 0, 'would_update': 0, 'updated': 0, 'already_new': 0, 'preserved_other': 0}
    receipt = {'kind': 'native_import_presentation_backfill.v1', 'mode': 'apply' if args.apply else 'dry_run',
               'source': args.source, 'through_seq': args.through_seq, 'after_seq': args.after_seq,
               'last_seq': args.after_seq, 'counts': totals, 'complete_interval': False,
               'model_calls': 0, 'queue_publications': 0, 'credential_values_included': False}
    journal = Journal(args.journal) if args.apply else None
    started = time.monotonic()
    reported_rows = 0
    try:
        with transport.Mysql(readonly=not args.apply) as db:
            require(db.scalar('SELECT @@server_uuid') == SERVER_UUID, 'Unexpected actual MySQL server')
            deriver = Deriver(db, args.source)
            if args.apply:
                db.query(create_temp_sql())
            while totals['scanned'] < args.max_rows and time.monotonic() - started < args.max_seconds:
                if args.apply:
                    paused_importers()
                    deployment_boundary_held()
                limit = min(args.batch_size, args.max_rows - totals['scanned'])
                db.query('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ; START TRANSACTION'
                         + ('' if args.apply else ' READ ONLY'))
                try:
                    rows = parse_rows(db.query(select_sql(args.source, receipt['last_seq'],
                        args.through_seq, limit, locked=args.apply)))
                    counts, pending = process_batch(db, deriver, args.source, rows,
                        apply=args.apply, journal=journal)
                    db.query('COMMIT')
                    if journal and pending:
                        journal.append({'kind': 'committed', 'seqs': pending})
                except Exception:
                    try:
                        db.query('ROLLBACK')
                    except Exception:
                        pass
                    raise
                for key, value in counts.items():
                    totals[key] += value
                if rows:
                    receipt['last_seq'] = rows[-1][0]
                if len(rows) < limit or receipt['last_seq'] == args.through_seq:
                    receipt['complete_interval'] = True
                    break
                if totals['scanned'] - reported_rows >= 5000:
                    print(json.dumps({**receipt, 'progress': True}), flush=True)
                    reported_rows = totals['scanned']
            print(json.dumps(receipt), flush=True)
    finally:
        if journal:
            journal.close()
    return receipt


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Imported code/driver exceptions may contain raw data. Never echo them.
        print(json.dumps({'kind': 'native_import_presentation_backfill_error.v1',
            'error': str(error) if isinstance(error, Refused) else 'Operation failed; private diagnostics suppressed',
            'credential_values_included': False}), file=sys.stderr)
        raise SystemExit(1)
