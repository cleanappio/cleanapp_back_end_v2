import hashlib
import importlib.util
import json
from pathlib import Path
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('presentation_backfill', Path(__file__).with_name('backfill.py'))
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)

LEGACY = ('source: OpenLitterMap.com\nHistorical observation 123',
          'Historical OpenLitterMap observation',
          'source: OpenLitterMap.com\nHistorical observation 123', 'OpenLitterMap.com')
NEW = (LEGACY[0], 'Plastic bottles (3)', 'Plastic bottles (3)', 'OLM')
FULL = 'source: OpenLitterMap.com\nRetained factual body\nOriginal timestamp unchanged.'


def row(seq=100, presentation=LEGACY, full=FULL):
    return [seq, '123', 'rpt_public123', *presentation, full, '{}',
            {'first_seen_at': '2026-11-10T00:00:00Z', 'last_seen_at': '2026-11-10T00:00:00Z'},
            'a' * 64, hashlib.sha256(b'{}').hexdigest(), 'b' * 64]


class Derived:
    def derive(self, value):
        return LEGACY, NEW, FULL


class FakeDB:
    def __init__(self, changed='1', matched='1'):
        self.queries = []
        self.changed, self.matched = changed, matched

    def query(self, sql):
        self.queries.append(sql)
        if sql.startswith('UPDATE ') and sql.endswith('; SELECT ROW_COUNT()'):
            return [self.changed]
        return []

    def scalar(self, sql):
        self.queries.append(sql)
        # Model the real transport's trailing SELECT marker: a separate
        # SELECT ROW_COUNT() would observe -1, not the previous UPDATE count.
        if sql == 'SELECT ROW_COUNT()':
            return '-1'
        return self.matched


class FakeJournal:
    def __init__(self, order):
        self.order = order
        self.records = []

    def append_many(self, records):
        self.order.append('journal_fsynced')
        self.records.extend(records)


class BackfillTests(unittest.TestCase):
    def test_selection_is_fenced_keyset_and_excludes_blobs(self):
        for source in ('olm', 'ssf'):
            sql = b.select_sql(source, 42, 900, 500, locked=True)
            self.assertTrue(sql.startswith('SELECT STRAIGHT_JOIN HEX(JSON_ARRAY('))
            self.assertIn('l.report_seq>42 AND l.report_seq<=900', sql)
            self.assertIn('FORCE INDEX', sql)
            self.assertIn('ORDER BY l.report_seq LIMIT 500 FOR UPDATE', sql)
            self.assertIn("BINARY l.disposition=BINARY 'inserted'", sql)
            for flag in b.FLAGS:
                self.assertIn('p.' + flag + '=1', sql)
            self.assertNotIn('r.image', sql)
            self.assertNotIn('report_extra_images', sql)
            self.assertNotIn('OFFSET', sql)

    def test_invalid_bounds_and_namespace_refused(self):
        for args in [('other', 0, 20, 5), ('olm', 20, 20, 5), ('olm', -1, 20, 5),
                     ('olm', 0, 20, 501), ('olm', True, 20, 5)]:
            with self.assertRaises(b.Refused):
                b.select_sql(*args)

    def test_hex_sql_encodes_quotes_and_preserves_null(self):
        value = "Robert'); DROP TABLE reports; --\nΩ"
        sql = b.sql_string(value)
        self.assertNotIn('DROP TABLE', sql)
        self.assertEqual(bytes.fromhex(sql.split("'")[1]).decode(), value)
        self.assertEqual(b.sql_string(None), 'NULL')

    def test_complete_tuple_and_body_required(self):
        self.assertEqual(b.decision(LEGACY, LEGACY, NEW, FULL, FULL), 'would_update')
        self.assertEqual(b.decision(NEW, LEGACY, NEW, FULL, FULL), 'already_new')
        for mixed in [(LEGACY[0], NEW[1], *LEGACY[2:]), (*LEGACY[:3], NEW[3]),
                      ('Curated human wording', *LEGACY[1:])]:
            self.assertEqual(b.decision(mixed, LEGACY, NEW, FULL, FULL), 'preserved_other')
        self.assertEqual(b.decision(LEGACY, LEGACY, NEW, FULL + ' human edit', FULL),
                         'preserved_other')

    def test_dry_run_has_zero_sql_or_journal_writes(self):
        db = FakeDB()
        counts, pending = b.process_batch(db, Derived(), 'olm', [row(), row(101, NEW),
            row(102, ('Curated', *LEGACY[1:]))])
        self.assertEqual(db.queries, [])
        self.assertEqual(pending, [])
        self.assertEqual(counts['would_update'], 1)
        self.assertEqual(counts['already_new'], 1)
        self.assertEqual(counts['preserved_other'], 1)

    def test_bulk_apply_journals_before_cas_and_checks_postimage(self):
        order = []
        db = FakeDB(changed='2', matched='2')
        original_query = db.query

        def query(sql):
            order.append('query')
            return original_query(sql)

        db.query = query
        journal = FakeJournal(order)
        counts, pending = b.process_batch(db, Derived(), 'olm', [row(), row(101)],
                                          apply=True, journal=journal)
        self.assertEqual(order[0], 'journal_fsynced')
        self.assertEqual(counts['updated'], 2)
        self.assertEqual(pending, [100, 101])
        self.assertEqual(len([q for q in db.queries if q.startswith('UPDATE ')]), 1)
        self.assertEqual(len([q for q in db.queries if q.startswith('INSERT ')]), 1)
        self.assertEqual(journal.records[0]['old'], LEGACY)
        self.assertEqual(journal.records[0]['new'], NEW)
        self.assertNotIn('full_description', journal.records[0])
        self.assertNotIn('SELECT ROW_COUNT()', db.queries)
        self.assertIn('; SELECT ROW_COUNT()', next(q for q in db.queries if q.startswith('UPDATE ')))

    def test_failed_cas_or_postcheck_refuses(self):
        for changed, matched in [('0', '1'), ('2', '1'), ('3', '1'), ('1', '0')]:
            with self.assertRaises(b.Refused):
                b.process_batch(FakeDB(changed, matched), Derived(), 'olm', [row()],
                    apply=True, journal=FakeJournal([]))

    def test_update_only_analysis_presentation_fields(self):
        sql = b.bulk_update_sql('olm')
        assignments = sql.split(' SET ', 1)[1].split(' WHERE ', 1)[0]
        self.assertEqual(assignments, 'a.title=t.new_title,'
            'a.summary=t.new_summary,a.source=t.new_source')
        for field in ('r.description', 'a.title', 'a.summary', 'a.source', 'a.description',
                      'l.source_id', 'r.public_id'):
            self.assertIn('BINARY ' + field + ' <=> BINARY t.', sql)
        self.assertNotIn('r.image', sql)
        self.assertNotIn('r.description=', assignments)
        single = b.update_sql('olm', row(), LEGACY, NEW, FULL)
        self.assertNotIn('r.description=', single.split(' SET ', 1)[1].split(' WHERE ', 1)[0])
        self.assertEqual(NEW[0], LEGACY[0])

    def test_wrong_display_namespace_refuses_before_writes(self):
        class Wrong:
            def derive(self, value):
                return LEGACY, (*NEW[:3], 'sf311'), FULL
        db = FakeDB()
        with self.assertRaises(b.Refused):
            b.process_batch(db, Wrong(), 'olm', [row()], apply=True, journal=FakeJournal([]))
        self.assertEqual(db.queries, [])

    def test_encoded_rows_preserve_tabs_unicode_and_null(self):
        value = row(presentation=('Ω\t\n', None, '', 'OpenLitterMap.com'))
        encoded = json.dumps(value).encode().hex()
        self.assertEqual(b.parse_rows([encoded]), [value])
        with self.assertRaises(b.Refused):
            b.parse_rows([encoded, encoded])

    def test_tampered_retained_feature_refuses_before_stage_or_catalogue(self):
        deriver = object.__new__(b.Deriver)
        deriver.kind = 'olm'
        deriver.stage = mock.Mock()
        value = row()
        value[8] = '{"tampered":true}'
        with self.assertRaises(b.Refused):
            deriver.derive(value)
        deriver.stage.make_candidate.assert_not_called()

    def test_catalogue_hash_and_namespace_are_verified(self):
        deriver = object.__new__(b.Deriver)
        deriver.kind = 'olm'
        deriver.catalogues = b.OrderedDict()
        deriver.db = mock.Mock()
        deriver.db.query.return_value = [b'{"objects":[]}'.hex()]
        digest = hashlib.sha256(b'{"objects":[]}').hexdigest()
        self.assertEqual(deriver.catalogue(digest), {'objects': []})
        self.assertEqual(deriver.catalogue(digest), {'objects': []})
        self.assertEqual(deriver.db.query.call_count, 1)
        self.assertIn(b.sql_string('openlittermap.com'), deriver.db.query.call_args.args[0])
        with self.assertRaises(b.Refused):
            deriver.catalogue('a' * 64)

    def test_lock_stat_accepts_only_exact_deployer_private_inode(self):
        good = dict(st_mode=stat.S_IFREG | 0o600, st_uid=1003, st_nlink=1,
                    st_dev=10, st_ino=20)
        self.assertEqual(b.lock_identity(SimpleNamespace(**good)), (10, 20, 1003, 0o600, 1))
        for replacement in ({'st_uid': 0}, {'st_mode': stat.S_IFREG | 0o640},
                            {'st_mode': stat.S_IFLNK | 0o600}, {'st_nlink': 2}):
            with self.assertRaises(b.Refused):
                b.lock_identity(SimpleNamespace(**{**good, **replacement}))

    def test_lock_probe_rejects_changed_inode(self):
        before = SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=1003,
                                 st_nlink=1, st_dev=10, st_ino=20)
        changed = SimpleNamespace(**{**vars(before), 'st_ino': 21})
        parent = SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=1003)
        ancestor = SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=0)
        fake_path = mock.Mock()
        fake_path.parent.lstat.return_value = parent
        fake_path.parent.parents = [mock.Mock()]
        fake_path.parent.parents[0].lstat.return_value = ancestor
        fake_path.lstat.return_value = before
        with mock.patch.object(b, 'DEPLOYMENT_LOCK', fake_path), \
             mock.patch.object(b.os, 'open', return_value=7), \
             mock.patch.object(b.os, 'fstat', return_value=changed), \
             mock.patch.object(b.os, 'close'):
            with self.assertRaises(b.Refused):
                b.deployment_boundary_held()

    def test_lock_probe_requires_another_holder(self):
        info = SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=1003,
                               st_nlink=1, st_dev=10, st_ino=20)
        fake_path = mock.Mock()
        fake_path.parent.lstat.return_value = SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=1003)
        fake_path.parent.parents = []
        fake_path.lstat.return_value = info
        with mock.patch.object(b, 'DEPLOYMENT_LOCK', fake_path), \
             mock.patch.object(b.os, 'open', return_value=7), \
             mock.patch.object(b.os, 'fstat', return_value=info), \
             mock.patch.object(b.os, 'close'), mock.patch.object(b.fcntl, 'flock') as flock:
            with self.assertRaises(b.Refused):
                b.deployment_boundary_held()
            flock.side_effect = BlockingIOError()
            b.deployment_boundary_held()

    def test_active_importer_unit_refuses(self):
        result = mock.Mock(returncode=0, stdout='LoadState=loaded\nActiveState=activating\nMainPID=42\n')
        with mock.patch.object(b.os, 'geteuid', return_value=0), mock.patch.object(b.sys, 'platform', 'linux'), \
             mock.patch.object(b.subprocess, 'run', return_value=result):
            with self.assertRaises(b.Refused):
                b.paused_importers()

    def test_temp_insert_refuses_mixed_or_duplicate_sequence(self):
        with self.assertRaises(b.Refused):
            b.temp_insert_sql([(row(presentation=NEW), LEGACY, NEW, FULL)])
        with self.assertRaises(b.Refused):
            b.temp_insert_sql([(row(), LEGACY, NEW, FULL), (row(), LEGACY, NEW, FULL)])
        with self.assertRaises(b.Refused):
            b.temp_insert_sql([(row(), LEGACY, ('Changed raw description', *NEW[1:]), FULL)])


if __name__ == '__main__':
    unittest.main()
