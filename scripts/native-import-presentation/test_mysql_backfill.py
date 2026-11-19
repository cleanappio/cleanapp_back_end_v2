"""Opt-in actual MySQL bulk projection/CAS tests; fixed isolated fixture only."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent / 'solvesf-import'))
import test_apply as fixtures

spec = importlib.util.spec_from_file_location('actual_presentation_backfill', HERE / 'backfill.py')
backfill = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backfill)
app = fixtures.app


class DurableJournal:
    def __init__(self, path):
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        self.stream = os.fdopen(fd, 'w')
        self.records = []

    def append_many(self, records):
        for record in records:
            self.records.append(record)
            self.stream.write(json.dumps(record) + '\n')
        self.stream.flush()
        os.fsync(self.stream.fileno())

    def close(self):
        self.stream.close()


@unittest.skipUnless(os.environ.get('PRESENTATION_BACKFILL_TEST_MYSQL') == '1',
                     'opt-in fixed isolated MySQL fixture')
class ActualBulkCAS(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Reuse the existing network-none / explicit-label / nonproduction UUID
        # contract and synthetic DDL; never instantiate a production transport.
        fixtures.ActualMysql.setUpClass()
        cls.db = fixtures.ActualMysql.db

    @classmethod
    def tearDownClass(cls):
        fixtures.ActualMysql.tearDownClass()

    def test_actual_bulk_cas_idempotence_preservation_and_postimage_rollback(self):
        candidates = [fixtures.candidate('710001'), fixtures.candidate('710002')]
        candidates[0]['text'] = 'Blocked crossing & broken curb. Original issue detail.'
        candidates[1]['text'] = 'Dumped furniture near the corner. Additional source detail.'
        candidates = [fixtures.sign(candidate) for candidate in candidates]
        seqs = [app.write_one(self.db, fixtures.Guard(), candidate, [], [])['seq']
                for candidate in candidates]
        through = max(seqs)
        deriver = object.__new__(backfill.Deriver)
        deriver.kind, deriver.display = 'ssf', app

        def rows():
            return backfill.parse_rows(self.db.query(backfill.select_sql('ssf', 0, through, 2, locked=True)))

        def rewrite(seq, values):
            raw = bytes.fromhex(self.db.scalar('SELECT HEX(description) FROM reports WHERE seq=' + str(seq))).decode()
            self.assertEqual(values[0], raw)
            self.db.query('UPDATE report_analysis a SET '
                + ','.join(column + '=' + app.sql_string(value) for column, value in zip(
                    ('a.title', 'a.summary', 'a.source'), values[1:]))
                + ' WHERE a.seq=' + str(seq))

        def projections():
            return [backfill.presentation_tuple(row) for row in rows()]

        def protected_snapshot():
            # Read real SQL values, including retained payload/media identity,
            # immutable body, canonical source names, and every quiet flag.
            fields = ['l.source_namespace', 'l.source_id', 'l.publisher', 'l.payload_sha256',
                'l.candidate_json', 'l.source_record_json', 'l.media_json', 'r.public_id',
                'r.id', 'r.ts', 'r.latitude', 'r.longitude', 'r.description', 'SHA2(r.image,256)', 'a.description',
                'p.source_namespace', 'p.policy_version', *['p.' + flag for flag in app.FLAGS],
                'e.source', 'e.external_id', 'e.source_url', 'e.source_timestamp']
            return self.db.query('SELECT HEX(JSON_ARRAY(' + ','.join(fields) + ')) FROM '
                'sf311_source_case_ledger l JOIN reports r ON r.seq=l.report_seq '
                'JOIN report_analysis a ON a.seq=r.seq JOIN report_side_effect_policy p ON p.report_seq=r.seq '
                'JOIN external_ingest_index e ON e.seq=r.seq WHERE r.seq IN ('
                + ','.join(map(str, seqs)) + ') ORDER BY r.seq')

        current = [app.presentation_variants(candidate)[0] for candidate in candidates]
        legacy = [app.presentation_variants(candidate)[1] for candidate in candidates]
        for seq, projection in zip(seqs, legacy):
            rewrite(seq, projection)
        protected = protected_snapshot()
        self.assertEqual(projections(), legacy)
        # An UPDATE targeting reports would rewrite its large image row under
        # FULL row binlogging even if its description value stayed unchanged.
        # Make that a REAL SQL failure, not merely a generated-SQL assertion.
        self.db.query("CREATE TRIGGER fixture_raw_report_immutable BEFORE UPDATE ON reports "
                      "FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='raw fixture report is immutable'")
        self.addCleanup(lambda: self.db.query('DROP TRIGGER IF EXISTS fixture_raw_report_immutable'))
        self.db.query(backfill.create_temp_sql())
        with tempfile.TemporaryDirectory() as directory:
            journal = DurableJournal(Path(directory) / 'rollback.private.jsonl')
            self.addCleanup(journal.close)
            self.db.query('START TRANSACTION')
            counts, pending = backfill.process_batch(self.db, deriver, 'ssf', rows(),
                                                    apply=True, journal=journal)
            self.assertEqual((counts['updated'], pending), (2, seqs))
            self.db.query('COMMIT')
            self.assertEqual(projections(), current)
            self.assertEqual(protected_snapshot(), protected)

            self.db.query('START TRANSACTION')
            repeat, pending = backfill.process_batch(self.db, deriver, 'ssf', rows(),
                                                    apply=True, journal=journal)
            self.db.query('COMMIT')
            self.assertEqual((repeat['already_new'], repeat['updated'], pending), (2, 0, []))
            self.assertEqual(len(journal.records), 2)

            curated = list(current[0]); curated[1] = 'Human-curated issue title'
            mixed = list(current[1]); mixed[3] = legacy[1][3]
            rewrite(seqs[0], curated); rewrite(seqs[1], mixed)
            before = projections()
            self.db.query('START TRANSACTION')
            preserved, pending = backfill.process_batch(self.db, deriver, 'ssf', rows(),
                                                       apply=True, journal=journal)
            self.db.query('COMMIT')
            self.assertEqual((preserved['preserved_other'], preserved['updated'], pending), (2, 0, []))
            self.assertEqual(projections(), before)
            self.assertEqual(protected_snapshot(), protected)

            for seq, projection in zip(seqs, legacy):
                rewrite(seq, projection)
            self.db.query("CREATE TRIGGER fixture_bad_postimage BEFORE UPDATE ON report_analysis "
                          "FOR EACH ROW SET NEW.title=CONCAT(NEW.title,' [fixture drift]')")
            try:
                self.db.query('START TRANSACTION')
                with self.assertRaisesRegex(backfill.Refused, 'postimage'):
                    backfill.process_batch(self.db, deriver, 'ssf', rows(), apply=True, journal=journal)
                self.db.query('ROLLBACK')
                self.assertEqual(projections(), legacy)
                self.assertEqual(protected_snapshot(), protected)
            finally:
                self.db.query('ROLLBACK; DROP TRIGGER fixture_bad_postimage')

        result = {'passed': True, 'database': 'cleanapp_dataunion_import_test', 'rows': 2,
            'checks': {'bulk_old_to_new_complete_tuple': True, 'actual_affected_row_receipt_parsed': True,
                'idempotent_replay': True, 'full_bodies_unchanged': True,
                'raw_report_row_never_updated': True,
                'canonical_source_payload_media_and_quiet_flags_unchanged': True,
                'mixed_and_curated_presentations_preserved': True,
                'actual_bad_postimage_rolls_back_entire_batch': True},
            'backfill_sha256': hashlib.sha256((HERE / 'backfill.py').read_bytes()).hexdigest(),
            'production_connections': 0, 'production_writes': 0}
        if os.environ.get('PRESENTATION_BACKFILL_RESULT'):
            Path(os.environ['PRESENTATION_BACKFILL_RESULT']).write_text(json.dumps(result, indent=2) + '\n')


if __name__ == '__main__':
    unittest.main()
