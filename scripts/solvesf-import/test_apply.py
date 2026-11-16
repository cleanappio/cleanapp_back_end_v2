"""Offline contract tests plus opt-in actual isolated MySQL atomicity tests.

Actual tests require SOLVESF_TEST_MYSQL=1 and the fixed network-none, explicitly
labelled disposable ca-import-test-mysql container. They are not production
coverage, privacy-journal, source freshness, or runtime-guard approval evidence.
"""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import tempfile
import types
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
CANONICAL_SCHEMA = HERE.parents[1] / "go-common/sf311import/schema.sql"
spec = importlib.util.spec_from_file_location("solvesf_apply", HERE / "apply.py")
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)


def candidate(ident="101"):
    row = {"schema": "cleanapp-solvesf-candidate-v1", "source_namespace": "sf311",
           "source_id": ident, "publisher": "SolveSF", "publisher_record_id": ident,
           "source_url": "https://solvesf.com/report/" + ident,
           "requested_at": "2026-11-01T10:00:00Z", "updated_at": None,
           "latitude": "37.7749", "longitude": "-122.4194", "media_urls": [],
           "text": "Dumped litter. City status closed; condition unverified.",
           "source_record": {"service_request_id": ident, "status": "closed",
               "requested_datetime_raw": "2026-11-01T02:00:00-08:00", "updated_datetime_raw": None,
               "description_authorship": "unverified_may_be_ai", "submitter_origin": "unknown",
               "resolution_verified": False}}
    return sign(row)


def sign(row):
    row.pop("candidate_sha256", None)
    row["candidate_sha256"] = app.digest(row)
    return row


class Guard:
    def __init__(self, fail=False):
        self.fail = fail
        self.checks = []

    def preinsert(self, db, c, media):
        self.checks.append("privacy_and_current_boundary")
        if self.fail:
            raise app.Hold("cross_source_photo_or_privacy_hold")

    def verify_replay(self, db, seq, c, media):
        self.checks.append("intact_native_replay")

    def record_insert(self, seq, c, media):
        self.checks.append("committed")


class FakeDB:
    def __init__(self, prior=None, fail_insert=False):
        self.statements, self.prior, self.fail_insert = [], prior, fail_insert

    def query(self, sql):
        self.statements.append(sql)
        if sql.startswith("SELECT disposition"):
            return self.prior or []
        if self.fail_insert and sql.startswith("INSERT INTO report_side_effect_policy"):
            raise ValueError("fixture_failure")
        return []

    def scalar(self, sql):
        return "7"


class Contracts(unittest.TestCase):
    def test_compact_card_leads_with_reported_issue_and_full_preserves_context(self):
        row = candidate()
        issue = "Broken sidewalk blocks wheelchair access at the crossing."
        row["text"] = issue + " More observed detail." * 20
        compact, full = app.presentation(row)
        self.assertTrue(compact.startswith("source: SolveSF.com\n" + issue))
        self.assertLessEqual(len(compact), 255)
        self.assertIn("Original city status: closed; this is not a verified resolution.", full)
        self.assertIn("Description authorship is unverified and may be AI generated.", full)
        self.assertIn(row["text"], full)
        row["text"] = "Broken & blocked <crossing>"
        compact, _ = app.presentation(row)
        self.assertIn("Broken &amp; blocked &lt;crossing&gt;", compact)

    def test_actual_collector_output_matches_writer_contract(self):
        import collect
        row = collect.normalize("12345678", {"service_request_id": "12345678",
            "description": "Public case, source wording retained", "source": "open311",
            "requested_datetime": "2026-11-16T01:02:03-08:00",
            "updated_datetime": "2026-11-16T10:03:04Z", "status": "closed",
            "lat": "37.765432109876543", "long": "-122.412345678901234",
            "photos": ["https://spot-sf-res.cloudinary.com/image/upload/example.jpg"]})
        self.assertEqual(app.valid_candidate(row), row)
        self.assertEqual(row["source_record"]["requested_datetime_raw"], "2026-11-16T01:02:03-08:00")

    def test_writer_ledger_contract_matches_canonical_migration(self):
        def normalized(sql):
            return " ".join(re.sub(r"\bIF\s+NOT\s+EXISTS\b", "", sql, flags=re.IGNORECASE).split())
        canonical = CANONICAL_SCHEMA.read_text()
        self.assertEqual(normalized(app.LEDGER_DDL), normalized(canonical))
        self.assertEqual(len(re.findall(r"CREATE TABLE IF NOT EXISTS", canonical)), 2)

    def test_identity_digest_original_time_and_source_allowlist(self):
        row = candidate()
        self.assertEqual(app.valid_candidate(row), row)
        for mutate in (
            lambda c: c.update(source_id="solvesf:101"),
            lambda c: c.update(requested_at="2026-11-01T10:00:00"),
            lambda c: c.update(latitude=37.7),
            lambda c: c["source_record"].update(submitter_email="private@example.invalid"),
            lambda c: c["source_record"].update(resolution_verified=True),
            lambda c: c["source_record"].update(requested_datetime_raw="2026-11-01T02:01:00-08:00"),
            lambda c: c["source_record"].update(updated_datetime_raw="2026-11-02T10:00:00Z"),
        ):
            bad = copy.deepcopy(row)
            mutate(bad)
            with self.assertRaises((ValueError, app.Hold)):
                app.valid_candidate(sign(bad))
        changed = copy.deepcopy(row)
        changed["text"] += "changed"
        with self.assertRaisesRegex(app.Hold, "digest"):
            app.valid_candidate(changed)

    def test_plan_offline_bounded_and_public_id_case_based(self):
        rows = [candidate("101"), candidate("102")]
        self.assertNotEqual(app.public_id(rows[0]), app.public_id(rows[1]))
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "candidates.jsonl"
            path.write_text("\n".join(app.canonical(r) for r in rows) + "\n")
            self.assertEqual(app.read_candidates(path, app.file_sha(path)), rows)
            path.write_text((app.canonical(rows[0]) + "\n") * 6)
            with self.assertRaises(app.Hold):
                app.read_candidates(path, app.file_sha(path))
        self.assertFalse(app.plan(rows)["apply_ready"])
        self.assertFalse(app.plan(rows)["database_connected"])

    def test_atomic_quiet_insert_and_rollback_on_partial_failure(self):
        db, guard = FakeDB(), Guard()
        self.assertEqual(app.write_one(db, guard, candidate(), [], [])["action"], "inserted")
        text = "\n".join(db.statements)
        for table in ("reports", "report_raw", "external_ingest_index", "report_analysis",
                      "report_side_effect_policy", "sf311_source_case_ledger"):
            self.assertIn("INSERT INTO " + table, text)
        policy = next(s for s in db.statements if s.startswith("INSERT INTO report_side_effect_policy"))
        self.assertTrue(all(flag in policy for flag in app.FLAGS))
        self.assertTrue(policy.endswith(",1,1,1,1,1,1)"))
        self.assertIn("privacy_and_current_boundary", guard.checks)
        self.assertEqual(db.statements[-1], "COMMIT")
        bad = FakeDB(fail_insert=True)
        with self.assertRaises(ValueError):
            app.write_one(bad, Guard(), candidate(), [], [])
        self.assertEqual(bad.statements[-1], "ROLLBACK")
        self.assertNotIn("COMMIT", bad.statements)

    def test_cross_source_or_private_hit_never_creates_native_row(self):
        db = FakeDB()
        with self.assertRaises(app.Hold):
            app.write_one(db, Guard(fail=True), candidate(), [], [])
        self.assertFalse(any(s.startswith("INSERT") for s in db.statements))

    def test_deleted_changed_and_intact_replay(self):
        c = candidate()
        material = app.digest({"candidate_sha256": c["candidate_sha256"], "media": []})
        base = ["inserted", "7", "7", c["candidate_sha256"], material, app.public_id(c)]
        db = FakeDB(["\t".join(base)])
        self.assertEqual(app.write_one(db, Guard(), c, [], [])["action"], "replay_skip")
        self.assertFalse(any(s.startswith("INSERT") for s in db.statements))
        for field, value in ((0, "held"), (1, "0"), (3, "0" * 64), (4, "f" * 64)):
            row = base.copy()
            row[field] = value
            with self.assertRaises(app.Hold):
                app.write_one(FakeDB(["\t".join(row)]), Guard(), c, [], [])

    def test_media_is_required_when_source_has_photos(self):
        c = candidate()
        c["media_urls"] = ["https://spot-sf-res.cloudinary.com/photo.jpg"]
        with self.assertRaisesRegex(app.Hold, "coverage"):
            app.write_one(FakeDB(), Guard(), sign(c), [], [])
        self.assertNotIn("UNIQUE", app.LEDGER_DDL)
        self.assertIn("ON DELETE SET NULL", app.LEDGER_DDL)

    def test_hash_bound_media_allows_explicit_null_version_but_not_missing_version(self):
        c = candidate()
        c["media_urls"] = ["https://spot-sf-res.cloudinary.com/photo.jpg"]
        body = b"SQL fixture bytes; codec verified separately"
        ref = {"source_url": c["media_urls"][0],
            "original": {"bucket": "fixed-private-bucket", "key": "import/solvesf/originals/fixture",
                "version_id": None, "sha256": "a" * 64, "bytes": 10},
            "display": {"bucket": "fixed-private-bucket", "key": "import/solvesf/previews/fixture",
                "version_id": None, "sha256": hashlib.sha256(body).hexdigest(), "bytes": len(body),
                "width": 1, "height": 1, "content_type": "image/jpeg", "metadata_removed": True}}
        app.validate_media(c, [ref], [body])
        del ref["display"]["version_id"]
        with self.assertRaises(app.Hold):
            app.validate_media(c, [ref], [body])


@unittest.skipUnless(os.environ.get("SOLVESF_TEST_MYSQL") == "1", "opt-in actual isolated MySQL fixture")
class ActualMysql(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        metadata = json.loads(subprocess.check_output(["docker", "inspect", "ca-import-test-mysql"], text=True))[0]
        if (metadata["HostConfig"]["NetworkMode"] != "none"
                or metadata["Config"].get("Labels", {}).get("cleanapp.solvesf.fixture") != "true"):
            raise RuntimeError("Only the labelled network-none disposable fixture is allowed")
        spec = importlib.util.spec_from_file_location("fixture_mysql", app.HELPERS.parent / "mysql_client.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        cls.fixture_factory = staticmethod(lambda: module.Mysql("ca-import-test-mysql", "cleanapp_dataunion_import_test"))
        cls.db = module.Mysql("ca-import-test-mysql", "cleanapp_dataunion_import_test")
        if cls.db.scalar("SELECT @@server_uuid") == app.SERVER_UUID:
            raise RuntimeError("Production server refused by fixture")
        ddl = (app.HELPERS.parent / "integration_test.py").read_text()
        # The existing fixture's explicit synthetic DDL is data, never executed
        # as Python or confused with captured production schema approval.
        schema = ddl.split("BASE_SCHEMA='''", 1)[1].split("'''", 1)[0]
        cls.db.query(schema.replace("DEFAULT0", "DEFAULT 0").replace("DEFAULT1", "DEFAULT 1"))
        cls.db.query("ALTER TABLE report_analysis ADD COLUMN summary TEXT")
        cls.db.query("CREATE TABLE report_extra_images (seq INT NOT NULL,position TINYINT UNSIGNED NOT NULL,image MEDIUMBLOB NOT NULL,sha256 CHAR(64) NOT NULL,PRIMARY KEY(seq,position),FOREIGN KEY(seq) REFERENCES reports(seq) ON DELETE CASCADE) ENGINE=InnoDB")
        cls.db.query("CREATE TABLE reports_gdpr (seq INT NOT NULL PRIMARY KEY) ENGINE=InnoDB")
        cls.db.query("CREATE TABLE report_evidence_source_projection (seq INT NOT NULL PRIMARY KEY,privacy_restricted BOOLEAN NOT NULL DEFAULT FALSE) ENGINE=InnoDB")
        canonical = CANONICAL_SCHEMA.read_text()
        cls.db.query(canonical)
        cls.db.query(canonical)  # Canonical additive migration is idempotent.

    @classmethod
    def tearDownClass(cls):
        cls.db.close()

    def real_sql_guard(self):
        spec = importlib.util.spec_from_file_location("solvesf_actual_sql_guard", HERE / "guard.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        g = module.ProductionGuard({})
        journal = sqlite3.connect(":memory:")
        self.addCleanup(journal.close)
        journal.execute("CREATE TABLE restricted_reports(report_seq INTEGER PRIMARY KEY)")
        # This fixture exercises REAL MySQL projection/privacy/schema queries.
        # Host/cloud/stopped-OLM/journal evidence are explicitly out of scope;
        # these mocks can never be used as production runtime approval.
        g.unchanged = mock.Mock()
        g.seed_olm_holds = mock.Mock()
        g.mutations = types.SimpleNamespace(db=journal, locked_changes=mock.Mock())
        g.tail = types.SimpleNamespace(journal_cutoff=0)
        g.coverage = None
        g.sql = app.sql_string
        g.media_module = types.SimpleNamespace(apply=app)
        return module, g

    def assert_sql_replay(self, guard, row, refs, seq, mutation=None, error=None):
        self.db.query("START TRANSACTION")
        try:
            if mutation:
                self.db.query(mutation)
            if error:
                with self.assertRaises(error):
                    guard.verify_replay(self.db, seq, row, refs)
            else:
                guard.verify_replay(self.db, seq, row, refs)
        finally:
            self.db.query("ROLLBACK")

    def test_atomic_insert_replay_change_and_no_resurrection(self):
        c = candidate("400001")
        result = app.write_one(self.db, Guard(), c, [], [])
        seq = result["seq"]
        self.assertEqual(app.write_one(self.db, Guard(), c, [], [])["action"], "replay_skip")
        self.assertEqual(self.db.scalar("SELECT COUNT(*) FROM report_side_effect_policy WHERE report_seq=" + str(seq) + " AND " + " AND ".join(f + "=1" for f in app.FLAGS)), "1")
        changed = copy.deepcopy(c)
        changed["text"] += " new material"
        with self.assertRaises(app.Hold):
            app.write_one(self.db, Guard(), sign(changed), [], [])
        self.db.query("DELETE FROM reports WHERE seq=" + str(seq))
        self.assertEqual(self.db.scalar("SELECT recorded_report_seq FROM sf311_source_case_ledger WHERE source_id='400001'"), str(seq))
        with self.assertRaises(app.Hold):
            app.write_one(self.db, Guard(), c, [], [])
        # A constraint failure after the report INSERT must remove the report,
        # source rows and case ledger together. No partial public report exists.
        before = self.db.scalar("SELECT COUNT(*) FROM reports")
        self.db.query("ALTER TABLE report_side_effect_policy ADD CONSTRAINT fixture_quiet_failure CHECK(suppress_email=0)")
        with self.assertRaises(Exception):
            app.write_one(self.db, Guard(), candidate("400002"), [], [])
        # The batch mysql client exits on SQL failure; its disconnected
        # transaction is rolled back. Inspect using a fresh fixture connection.
        self.db.close()
        type(self).db = self.fixture_factory()
        self.assertEqual(self.db.scalar("SELECT COUNT(*) FROM reports"), before)
        self.assertEqual(self.db.scalar("SELECT COUNT(*) FROM sf311_source_case_ledger WHERE source_id='400002'"), "0")
        self.db.query("ALTER TABLE report_side_effect_policy DROP CHECK fixture_quiet_failure")
        # Distinct canonical cases can share the same zero-image representation.
        app.write_one(self.db, Guard(), candidate("400003"), [], [])
        app.write_one(self.db, Guard(), candidate("400004"), [], [])
        self.assertEqual(self.db.scalar("SELECT COUNT(*) FROM sf311_source_case_ledger WHERE source_id IN ('400003','400004')"), "2")

    def test_real_guard_replay_sql_and_material_privacy_drift(self):
        module, g = self.real_sql_guard()
        row = candidate("400005")
        row["media_urls"] = ["https://spot-sf-res.cloudinary.com/sql-fixture.jpg"]
        sign(row)
        # BLOB shape only: actual photo decode/projection is covered separately
        # by the Pillow fixture and must also pass verified_media in production.
        body = b"synthetic-native-sql-blob"
        ref = {"source_url": row["media_urls"][0],
            "original": {"bucket": "cleanapp-production-backups-654574823950", "key": "import/solvesf/originals/sql-fixture",
                "version_id": "fixture-version", "sha256": hashlib.sha256(b"synthetic-original").hexdigest(), "bytes": 18},
            "display": {"bucket": "cleanapp-production-backups-654574823950", "key": "import/solvesf/previews/sql-fixture",
                "version_id": "fixture-version", "sha256": hashlib.sha256(body).hexdigest(), "bytes": len(body),
                "width": 1, "height": 1, "content_type": "image/jpeg", "metadata_removed": True}}
        seq = app.write_one(self.db, Guard(), row, [ref], [body])["seq"]
        self.assert_sql_replay(g, row, [ref], seq)
        self.assertTrue(g.mutations.locked_changes.called)
        self.assertTrue(g.seed_olm_holds.called)
        mutations = [
            "UPDATE report_analysis SET description='curated' WHERE seq=" + str(seq),
            "UPDATE external_ingest_index SET source_url='https://example.invalid/changed' WHERE seq=" + str(seq),
            "UPDATE sf311_source_media_ledger SET original_sha256=" + app.sql_string("0" * 64) + " WHERE source_id='400005'",
            "UPDATE sf311_source_case_ledger SET source_record_json=JSON_OBJECT('changed',TRUE) WHERE source_id='400005'",
            "INSERT INTO reports_gdpr(seq) VALUES(" + str(seq) + ")",
            "INSERT INTO report_evidence_source_projection(seq,privacy_restricted) VALUES(" + str(seq) + ",TRUE)",
        ]
        for statement in mutations:
            with self.subTest(mutation=statement.split()[1]):
                self.assert_sql_replay(g, row, [ref], seq, statement, module.GuardError)
        self.assert_sql_replay(g, row, [ref], seq)

    def test_real_guard_schema_sql_rejects_tombstone_drift(self):
        module, _ = self.real_sql_guard()
        module.verify_source_schema(self.db)
        name = self.db.scalar("SELECT CONSTRAINT_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='sf311_source_case_ledger' AND COLUMN_NAME='report_seq' AND REFERENCED_TABLE_NAME='reports'")
        self.assertRegex(name, r"^[A-Za-z0-9_]+$")
        drift_name = "fixture_sf311_cascade_drift"
        self.db.query("ALTER TABLE sf311_source_case_ledger DROP FOREIGN KEY " + name)
        try:
            self.db.query("ALTER TABLE sf311_source_case_ledger ADD CONSTRAINT " + drift_name + " FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE CASCADE")
            with self.assertRaises(module.GuardError):
                module.verify_source_schema(self.db)
        finally:
            present = self.db.query("SELECT CONSTRAINT_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='sf311_source_case_ledger' AND CONSTRAINT_NAME='" + drift_name + "' AND REFERENCED_TABLE_NAME='reports'")
            if present:
                self.db.query("ALTER TABLE sf311_source_case_ledger DROP FOREIGN KEY " + drift_name)
            self.db.query("ALTER TABLE sf311_source_case_ledger ADD CONSTRAINT " + name + " FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE SET NULL")
        module.verify_source_schema(self.db)


if __name__ == "__main__":
    unittest.main()
