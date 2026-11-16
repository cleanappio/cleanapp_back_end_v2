"""Meaningful negative tests; offline evidence never authorizes production."""
import importlib.util
from pathlib import Path
import types
import sqlite3
import copy
import hashlib
import base64
import io
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('solvesf_guard_tested', Path(__file__).with_name('guard.py'))
guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guard)
COLLECT_SPEC = importlib.util.spec_from_file_location('solvesf_guard_collect_tested', Path(__file__).with_name('collect.py'))
collect = importlib.util.module_from_spec(COLLECT_SPEC)
COLLECT_SPEC.loader.exec_module(collect)

REFS = [{'original': {'sha256': 'a' * 64}, 'display': {'sha256': 'b' * 64}}]

class Database:
    def __init__(self, hit=None):
        self.statements = []
        self.hit = hit
    def query(self, query):
        self.statements.append(query)
        return ['1'] if self.hit and self.hit in query else []

class GuardTests(unittest.TestCase):
    def object_fixture(self, version):
        body = b'archived exact source bytes'
        ref = {'bucket': guard.BUCKET, 'key': 'import/solvesf/originals/sha256/' + hashlib.sha256(body).hexdigest(),
               'version_id': version, 'sha256': hashlib.sha256(body).hexdigest(), 'bytes': len(body)}
        calls = []
        g = guard.ProductionGuard({})
        head = {'ContentLength': len(body), 'ServerSideEncryption': 'AES256', 'VersionId': version,
                'ChecksumSHA256': base64.b64encode(hashlib.sha256(body).digest()).decode()}
        g.archive_store = types.SimpleNamespace(run_json=lambda args: calls.append(('head', args)) or head,
            download=lambda key, v: calls.append(('download', key, v)) or types.SimpleNamespace(
                stream=io.BytesIO(body), finish=lambda: None, abort=lambda: None))
        g.media_module = types.SimpleNamespace(assert_object_private=lambda key, v: calls.append(('private', key, v)))
        return g, ref, body, calls

    def test_versioned_and_unversioned_objects_have_exact_authenticated_and_privacy_proofs(self):
        for version in (None, 'actual-provider-version'):
            g, ref, body, calls = self.object_fixture(version)
            self.assertEqual(g._object(ref, 1000), body)
            self.assertEqual('--version-id' in calls[0][1], version is not None)
            self.assertEqual(calls[1], ('private', ref['key'], version))
            self.assertEqual(calls[2], ('download', ref['key'], version))

    def test_public_object_is_rejected_before_body_use(self):
        g, ref, _body, calls = self.object_fixture(None)
        def public(*_args):
            raise guard.GuardError('anonymous_object_is_public')
        g.media_module.assert_object_private = public
        with self.assertRaisesRegex(guard.GuardError, 'public'):
            g._object(ref, 1000)
        self.assertFalse(any(call[0] == 'download' for call in calls))

    def test_unversioned_object_still_requires_exact_full_bytes(self):
        g, ref, body, _calls = self.object_fixture(None)
        g.archive_store.download = lambda *_args: types.SimpleNamespace(stream=io.BytesIO(b'x' * len(body)),
            finish=lambda: None, abort=lambda: None)
        with self.assertRaisesRegex(guard.GuardError, 'readback_mismatch'):
            g._object(ref, 1000)

    def schema_db(self, engine='InnoDB', extra_unique='0', foreign_rule='SET NULL', collation='ascii_bin', media_rule='RESTRICT'):
        class SchemaDB:
            def scalar(self, query):
                if 'SELECT COLUMN_TYPE' in query:
                    return 'tinyint unsigned'
                return engine if 'SELECT ENGINE' in query else extra_unique
            def query(self, query):
                if 'information_schema.COLUMNS' in query:
                    table = 'sf311_source_media_ledger' if 'source_media' in query else 'sf311_source_case_ledger'
                    return ['\t'.join([name, spec[0], spec[1], str(spec[2]),
                        collation if spec[3] == 'ascii_bin' else 'utf8mb4_0900_ai_ci' if spec[3] is None else spec[3]])
                        for name, spec in guard.SOURCE_COLUMNS[table].items()]
                if 'REFERENTIAL_CONSTRAINTS' in query:
                    return ['sf311_source_case_ledger\treport_seq\treports\tseq\t' + foreign_rule,
                        'sf311_source_media_ledger\tsource_namespace\tsf311_source_case_ledger\tsource_namespace\t' + media_rule,
                        'sf311_source_media_ledger\tsource_id\tsf311_source_case_ledger\tsource_id\t' + media_rule]
                if 'GROUP_CONCAT' in query:
                    return ['source_namespace,source_id,position' if 'source_media' in query else 'source_namespace,source_id']
                return ['idx_sf311_display_sha\tdisplay_sha256\t1', 'idx_sf311_original_sha\toriginal_sha256\t1']
        return SchemaDB()

    def test_nontransactional_source_table_refused(self):
        with self.assertRaisesRegex(guard.GuardError, 'innodb'):
            guard.verify_source_schema(self.schema_db(engine='MyISAM'))

    def test_image_unique_constraint_refused_even_with_correct_source_primary_key(self):
        with self.assertRaisesRegex(guard.GuardError, 'source_identity'):
            guard.verify_source_schema(self.schema_db(extra_unique='1'))
        guard.verify_source_schema(self.schema_db())

    def test_tombstone_fk_and_binary_identity_collation_required(self):
        with self.assertRaisesRegex(guard.GuardError, 'foreign_keys'):
            guard.verify_source_schema(self.schema_db(foreign_rule='CASCADE'))
        with self.assertRaisesRegex(guard.GuardError, 'collation'):
            guard.verify_source_schema(self.schema_db(collation='ascii_general_ci'))

    def test_actual_mysql_no_action_media_fk_metadata_is_accepted(self):
        guard.verify_source_schema(self.schema_db(media_rule='NO ACTION'))
        with self.assertRaisesRegex(guard.GuardError, 'foreign_keys'):
            guard.verify_source_schema(self.schema_db(media_rule='CASCADE'))
        with self.assertRaisesRegex(guard.GuardError, 'foreign_keys'):
            guard.verify_source_schema(self.schema_db(foreign_rule='NO ACTION', media_rule='NO ACTION'))

    def fresh_fixture(self):
        record = {'service_request_id': '123', 'requested_datetime': '2026-11-16T09:00:00Z',
                  'lat': '37.77', 'long': '-122.41', 'description': 'Public case text', 'status': 'open'}
        candidate = collect.normalize('123', record)
        g = guard.ProductionGuard({})
        g.collect_module = collect
        g.unchanged = lambda: None
        g.collection = self.collection_db(candidate)
        g.persist_source_hold = lambda ident, reason: guard.save_observed_hold(g.collection, ident, reason)
        return g, candidate, record

    def collection_db(self, candidate):
        db = sqlite3.connect(':memory:')
        self.addCleanup(db.close)
        db.executescript('CREATE TABLE versions(id TEXT,digest TEXT,candidate TEXT,disposition TEXT,PRIMARY KEY(id,digest));'
                         'CREATE TABLE record_holds(id TEXT PRIMARY KEY,reason TEXT,first_seen TEXT,last_seen TEXT);')
        db.execute('INSERT INTO versions VALUES(?,?,?,?)', (candidate['source_id'], candidate['candidate_sha256'],
                                                         guard.canonical(candidate), 'candidate'))
        db.commit()
        return db

    def test_exact_candidate_collector_binding(self):
        g, candidate, _ = self.fresh_fixture()
        guard.eligible_collection_case(g.collection, candidate)
        changed = copy.deepcopy(candidate)
        changed['text'] = 'changed outside collector'
        with self.assertRaisesRegex(guard.GuardError, 'exactly_authorized'):
            guard.eligible_collection_case(g.collection, changed)

    def test_old_candidate_never_escapes_sticky_private_or_held_history(self):
        g, candidate, _ = self.fresh_fixture()
        g.collection.execute("INSERT INTO record_holds VALUES('123','nonpublic_or_deleted','t0','t0')")
        with self.assertRaisesRegex(guard.GuardError, 'sticky'):
            guard.eligible_collection_case(g.collection, candidate)
        g.collection.execute('DELETE FROM record_holds')
        g.collection.execute('INSERT INTO versions VALUES(?,?,?,?)', ('123', 'f' * 64, '{}', 'held_revision'))
        with self.assertRaisesRegex(guard.GuardError, 'held_version'):
            guard.eligible_collection_case(g.collection, candidate)

    def test_current_public_case_requested_at_exact_fixed_url(self):
        g, candidate, record = self.fresh_fixture()
        with patch.object(collect, 'public_get', return_value=record) as request:
            g.fresh_case(candidate)
        request.assert_called_once_with('https://solvesf.com/api/report/123', collect.MAX_DETAIL)

    def test_changed_or_private_source_refused_before_archive_or_insert(self):
        for update in ({'description': 'Changed'}, {'is_public': False}, {'deleted': True}):
            g, candidate, record = self.fresh_fixture()
            record.update(update)
            g._object = lambda *_: self.fail('archive must not be read after source hold')
            with patch.object(collect, 'public_get', return_value=record):
                with self.assertRaises((guard.GuardError, collect.CollectError)):
                    g.verified_media(candidate, {})
            self.assertEqual(g.collection.execute('SELECT id FROM record_holds').fetchall(), [('123',)])

    def test_observed_hold_is_metadata_only_and_preserves_existing_history(self):
        g, candidate, _ = self.fresh_fixture()
        before = g.collection.execute('SELECT * FROM versions').fetchall()
        guard.save_observed_hold(g.collection, '123', 'guard_nonpublic_or_deleted')
        initial = g.collection.execute('SELECT reason,first_seen FROM record_holds WHERE id=?', ('123',)).fetchone()
        guard.save_observed_hold(g.collection, '123', 'guard_case_material_changed')
        self.assertEqual(g.collection.execute('SELECT reason,first_seen FROM record_holds WHERE id=?', ('123',)).fetchone(), initial)
        self.assertEqual(g.collection.execute('SELECT * FROM versions').fetchall(), before)
        self.assertEqual(g.collection.execute('SELECT COUNT(*) FROM record_holds').fetchone(), (1,))

    def test_only_source_404_is_sticky_transient_statuses_and_network_are_not(self):
        for status in (403, 404, 429, 500, 503):
            g, candidate, _ = self.fresh_fixture()
            with patch.object(collect, 'public_get', side_effect=collect.PublicHTTPError(status)):
                with self.assertRaises(collect.PublicHTTPError):
                    g.fresh_case(candidate)
            self.assertEqual(g.collection.execute('SELECT COUNT(*) FROM record_holds').fetchone(),
                             (1,) if status == 404 else (0,))
        g, candidate, _ = self.fresh_fixture()
        with patch.object(collect, 'public_get', side_effect=collect.CollectError('network failure')):
            with self.assertRaises(collect.CollectError):
                g.fresh_case(candidate)
        self.assertEqual(g.collection.execute('SELECT COUNT(*) FROM record_holds').fetchone(), (0,))

    def test_current_photo_bytes_are_checked_and_changes_held(self):
        g, candidate, _ = self.fresh_fixture()
        original = b'published photo bytes'
        ref = {'source_url': 'https://spot-sf-res.cloudinary.com/image.jpg',
               'original': {'sha256': hashlib.sha256(original).hexdigest(), 'bytes': len(original)}}
        calls = []
        g.downloader = types.SimpleNamespace(fetch=lambda url: (calls.append(url) or original, 'image/jpeg'))
        self.assertEqual(g.fresh_photo(candidate, ref), 'image/jpeg')
        self.assertEqual(calls, [ref['source_url']])
        g.downloader.fetch = lambda _: (b'changed or redacted', 'image/jpeg')
        with self.assertRaisesRegex(guard.GuardError, 'photo_changed'):
            g.fresh_photo(candidate, ref)
        self.assertEqual(g.collection.execute('SELECT reason FROM record_holds').fetchall(), [('guard_photo_material_changed',)])

    def test_photo_404_is_sticky_but_403_is_not(self):
        class PublicFailure(Exception):
            def __init__(self, status):
                self.status = status
        for status in (403, 404, 429, 503):
            g, candidate, _ = self.fresh_fixture()
            def fail(_url):
                raise PublicFailure(status)
            g.downloader = types.SimpleNamespace(fetch=fail)
            with self.assertRaises(PublicFailure):
                g.fresh_photo(candidate, {'source_url': 'https://spot-sf-res.cloudinary.com/photo.jpg'})
            self.assertEqual(g.collection.execute('SELECT COUNT(*) FROM record_holds').fetchone(),
                             (1,) if status == 404 else (0,))

    def test_actual_test_receipt_must_bind_current_source_bundle(self):
        g = guard.ProductionGuard({'actual_test_receipt_sha256': 'a' * 64})
        g.media_module = types.SimpleNamespace(apply=types.SimpleNamespace(HELPERS=guard.ROOT / 'helper.py'))
        value = lambda p, *_args, **_kw: hashlib.sha256(str(p).encode()).hexdigest()
        sources = {name: value(guard.ROOT / ('app/' + name)) for name in ('apply.py', 'guard.py', 'media.py', 'collect.py')}
        sources.update(sql_helper=value(g.media_module.apply.HELPERS), **{'schema.sql': value(guard.ROOT / 'app/schema.sql')})
        receipt = {'kind': 'cleanapp_solvesf_actual_tests.v1', 'actual_mysql_passed': True,
                   'guard_tests_passed': True, 'media_tests_passed': True, 'source_sha256': sources}
        with patch.object(guard, 'trusted', side_effect=value), patch.object(guard, 'document', return_value=receipt):
            g.verify_test_receipt()
            receipt['source_sha256']['guard.py'] = '0' * 64
            with self.assertRaisesRegex(guard.GuardError, 'test_evidence'):
                g.verify_test_receipt()

    def mutation_db(self):
        db = sqlite3.connect(':memory:')
        self.addCleanup(db.close)
        db.executescript('CREATE TABLE cursor(key TEXT PRIMARY KEY,value INTEGER NOT NULL);'
            'CREATE TABLE assets(sha256 TEXT NOT NULL,report_seq INTEGER NOT NULL,protected INTEGER NOT NULL,'
            'event_id INTEGER NOT NULL,PRIMARY KEY(sha256,report_seq));'
            "INSERT INTO cursor VALUES('safe',123);")
        return db

    def test_positive_seed_is_idempotent_and_preserves_cursor_and_prior_history(self):
        db = self.mutation_db()
        db.execute('INSERT INTO assets VALUES(?,?,?,?)', ('c' * 64, 77, 1, 999))
        db.commit()
        for _ in range(2):
            guard.seed_positive_assets(db, 88, {'a' * 64, 'b' * 64})
        self.assertEqual(db.execute('SELECT * FROM cursor').fetchall(), [('safe', 123)])
        self.assertEqual(db.execute('SELECT * FROM assets WHERE report_seq=77').fetchall(), [('c' * 64, 77, 1, 999)])
        self.assertEqual(db.execute('SELECT COUNT(*),MIN(protected),MAX(event_id) FROM assets WHERE report_seq=88').fetchone(), (2, 1, 0))

    def test_positive_seed_never_unprotects_or_rewinds_existing_event(self):
        db = self.mutation_db()
        db.execute('INSERT INTO assets VALUES(?,?,?,?)', ('a' * 64, 88, 0, 555))
        db.commit()
        guard.seed_positive_assets(db, 88, {'a' * 64})
        self.assertEqual(db.execute('SELECT protected,event_id FROM assets').fetchone(), (1, 555))

    def test_cross_source_queries_use_all_covering_hash_indexes(self):
        db = Database()
        guard.indexed_media_conflict(db, REFS)
        self.assertEqual(len(db.statements), 10)
        for column in ('original_sha256', 'display_sha256'):
            self.assertTrue(any('sf311_source_media_ledger' in q and column in q for q in db.statements))
            self.assertTrue(any('olm_source_observation_ledger' in q and column in q for q in db.statements))
        self.assertTrue(all('LIMIT 1 FOR UPDATE' in q for q in db.statements))
        self.assertTrue(any('uniq_historical_import_photo' in q for q in db.statements))

    def test_deleted_media_mapping_is_still_a_conflict(self):
        db = Database('sf311_source_media_ledger')
        with self.assertRaisesRegex(guard.GuardError, 'requires_reconciliation'):
            guard.indexed_media_conflict(db, REFS)
        # A null report_seq / deleted mapping is not filtered out.
        self.assertTrue(all('report_seq IS NOT NULL' not in q for q in db.statements))

    def test_original_hit_holds_even_if_display_would_be_unique(self):
        with self.assertRaises(guard.GuardError):
            guard.indexed_media_conflict(Database('idx_olm_ledger_original_hash'), REFS)

    def test_untrusted_hash_never_becomes_sql(self):
        db = Database()
        with self.assertRaises(guard.GuardError):
            guard.indexed_media_conflict(db, [{'original': {'sha256': "x' OR 1"}, 'display': {'sha256': 'b' * 64}}])
        self.assertEqual(db.statements, [])

    def test_text_only_still_calls_actual_tail_boundary_without_fake_asset(self):
        g = guard.ProductionGuard({})
        calls = []
        g.unchanged = lambda: None
        g.tail = lambda db, candidate: calls.append(candidate)
        g.preinsert(Database(), {'source_id': '123'}, [])
        self.assertEqual(calls, [{'source_id': '123', 'original_media': {'sha256': None},
                                 'display_media': {'sha256': None}}])

    def test_every_photo_passes_tail_guard(self):
        g = guard.ProductionGuard({})
        calls = []
        g.unchanged = lambda: None
        g.tail = lambda db, candidate: calls.append(candidate)
        g.preinsert(Database(), {'source_id': '123'}, REFS + REFS)
        self.assertEqual(len(calls), 2)

    def test_active_or_starting_receiver_refused(self):
        for state in ('active', 'activating', 'deactivating'):
            with self.subTest(state=state), patch.object(guard, 'quiet', return_value=(
                    'ActiveState=' + state + '\nMainPID=0\nControlPID=0\n').encode()):
                with self.assertRaises(guard.GuardError):
                    guard.stopped()

    def test_stopped_state_requires_no_control_process(self):
        with patch.object(guard, 'quiet', return_value=b'ActiveState=inactive\nMainPID=0\nControlPID=77\n'):
            with self.assertRaises(guard.GuardError):
                guard.stopped()

    def test_clean_stopped_receiver_accepted(self):
        with patch.object(guard, 'quiet', return_value=b'ActiveState=inactive\nMainPID=0\nControlPID=0\n'):
            guard.stopped()

if __name__ == '__main__':
    unittest.main()
