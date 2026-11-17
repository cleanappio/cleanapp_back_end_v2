"""Tests for the scorecard.

Run from this directory:

    python3 -m unittest -v

The last class needs a MySQL 8 server. Give it an admin client command with no
database selected, for example:

    SCORECARD_TEST_MYSQL='mysql -uroot --socket=/path/mysql.sock' python3 -m unittest -v

It builds a throwaway database from the table definitions in this repository
(not from a copy of them), loads testdata/rows.sql and checks every metric.
"""
import csv
import datetime as dt
import os
import re
import shlex
import subprocess
import tempfile
import unittest
from pathlib import Path

import scorecard

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
AS_OF = dt.datetime(2026, 11, 17)

# Where each table the metrics read is defined. The fixture database is built
# from these files, so a schema change that breaks a metric fails this test.
SQL_FILE_TABLES = [
    "reports", "report_raw", "report_status", "reports_owners", "report_email_deliveries",
    "report_email_delivery_events", "report_side_effect_policy",
]
GO_TABLES = [
    ("report_evidence", "go-common/evidence/migrate.go"),
    ("report_analysis", "report-analyze-pipeline/database/migration_helpers.go"),
    ("notification_messages", "email-service/service/reply_tracking.go"),
    ("notification_message_reports", "email-service/service/reply_tracking.go"),
    ("opted_out_emails", "email-service/service/email_service.go"),
    ("notify_outcomes", "report-listener/database/migration_helpers.go"),
    ("cases", "report-listener/database/migration_helpers.go"),
    ("case_reports", "report-listener/database/migration_helpers.go"),
]

# What the fixture must produce, with --backlog-min 2.
# count -> number; ratio -> (numerator, denominator); median -> (median, sample).
EXPECTED = {
    "chain_1_received": 4,
    "chain_2_notified": (2, 4),
    "chain_3_seen": (1, 4),
    "chain_4_answered": (1, 4),
    "chain_5_fixed": (1, 4),
    "notices_sent": 4,
    "notices_bounced": (1, 4),
    "notices_seen": (1, 4),
    "notices_answered": (1, 4),
    "notices_auto_acknowledged": (1, 4),
    "notices_silent": (1, 4),
    "parties_notified": 2,
    "parties_seen_ever": 2,
    "hours_received_to_notice": (2.0, 3),
    "hours_notice_to_first_response": (12.5, 2),
    "signal_share_of_sends": (3, 5),
    "opt_outs_30d": 1,
    "misrouted_30d": 1,
    "reports_total": 13,
    "firsthand_total": 10,
    "firsthand_received_30d": 5,
    "parties_with_unsent_evidence": 1,
    "epc_notices_sent_total": 3,
    "firsthand_epc_notified": (2, 4),
    "epc_notices_sent_30d": 2,
    "epc_notices_held": 1,
    "epc_addresses_notified": 2,
    "cases_total": 1,
    "reports_in_cases": 2,
    "fixed_total": 1,
    "reports_resolved_30d": 1,
}


class Done:
    """Stands in for subprocess.CompletedProcess."""

    def __init__(self, stdout="", stderr="", returncode=0):
        self.stdout, self.stderr, self.returncode = stdout, stderr, returncode


def metric(kind, metric_id="example"):
    return scorecard.Metric(id=metric_id, group="chain", title="t", kind=kind, window="w", better="higher",
                            definition="d", sql="SELECT 1")


def measured(kind, **done):
    return scorecard.measure(metric(kind), ["mysql"], AS_OF, 5, run=lambda *a, **k: Done(**done))


class MetricFiles(unittest.TestCase):
    def test_every_metric_loads_and_has_an_expected_value(self):
        ids = [m.id for m in scorecard.load_metrics()]
        self.assertEqual(sorted(ids), sorted(EXPECTED), "a metric file and EXPECTED must be added together")

    def test_metrics_are_ordered_by_group_then_name(self):
        ids = [m.id for m in scorecard.load_metrics()]
        self.assertEqual(ids[:5], ["chain_1_received", "chain_2_notified", "chain_3_seen", "chain_4_answered", "chain_5_fixed"])

    def test_anything_but_a_single_select_is_refused(self):
        for sql in [
            "DELETE FROM reports",
            "SELECT 1; SELECT 2",
            "SELECT 1 INTO OUTFILE '/tmp/x'",
            "WITH x AS (SELECT 1) UPDATE reports SET team = 1",
            "SET @a = 1",
            "SELECT * FROM reports FOR UPDATE; DROP TABLE reports",
        ]:
            with self.assertRaises(ValueError, msg=sql):
                scorecard.check_read_only("bad", sql)

    def test_column_names_that_contain_write_words_are_allowed(self):
        scorecard.check_read_only("ok", "SELECT COUNT(*) FROM report_status WHERE updated_at > created_at AND status = 'update'")

    def test_every_run_is_utc_time_limited_and_read_only(self):
        script = scorecard.script_for(metric("count"), AS_OF, 5)
        for line in ["SET time_zone = '+00:00';", "SET SESSION max_execution_time = 60000;",
                     "SET @as_of = '2026-11-17 00:00:00';", "START TRANSACTION READ ONLY;"]:
            self.assertIn(line, script)
        self.assertTrue(script.rstrip().endswith("SELECT 1;"))

    def test_the_wiki_lists_every_metric(self):
        page = (REPO / "docs/wiki/11-scorecard.md").read_text()
        missing = [m.id for m in scorecard.load_metrics() if f"`{m.id}`" not in page]
        self.assertEqual(missing, [], "run 'scorecard.py catalogue' and update docs/wiki/11-scorecard.md")


class Measuring(unittest.TestCase):
    def test_count(self):
        result = measured("count", stdout="COUNT(*)\n42\n")
        self.assertEqual((result.status, result.value), ("ok", 42))

    def test_ratio_and_its_share(self):
        result = measured("ratio", stdout="a\tb\n1\t4\n")
        self.assertEqual((result.numerator, result.denominator, result.value), (1, 4, 0.25))
        self.assertEqual(scorecard.describe(result), "25.0 per 100 (1 of 4)")

    def test_ratio_with_nothing_to_measure_has_no_value(self):
        result = measured("ratio", stdout="0\t0\n")
        self.assertIsNone(result.value)
        self.assertEqual(scorecard.describe(result), "none to measure (0 of 0)")

    def test_median_and_sample(self):
        result = measured("median", stdout="AVG(h)\tn\n12.50000000\t2\n")
        self.assertEqual((result.value, result.sample), (12.5, 2))

    def test_median_of_nothing(self):
        result = measured("median", stdout="NULL\t0\n")
        self.assertEqual(result.status, "ok")
        self.assertIsNone(result.value)

    def test_wrong_number_of_columns_is_not_a_result(self):
        self.assertEqual(measured("ratio", stdout="7\n").status, "unavailable: unexpected output")

    def test_a_missing_table_is_named(self):
        result = measured("count", returncode=1,
                          stderr="ERROR 1146 (42S02) at line 9: Table 'cleanapp.notification_messages' doesn't exist\n")
        self.assertEqual(result.status, "unavailable: table notification_messages missing")
        self.assertIsNone(result.value)

    def test_server_text_never_reaches_the_ledger(self):
        result = measured("count", returncode=1,
                          stderr="ERROR 1045 (28000): Access denied for user 'ops'@'10.0.0.7' (using password: YES)\n")
        self.assertEqual(result.status, "unavailable: access denied")

    def test_other_failures(self):
        self.assertEqual(measured("count", returncode=1, stderr="ERROR 3024 (HY000): Query execution was interrupted").status,
                         "unavailable: timed out")
        self.assertEqual(measured("count", returncode=1, stderr="ERROR 2002 (HY000): Can't connect").status,
                         "unavailable: cannot connect")
        self.assertEqual(measured("count", returncode=1, stderr="ERROR 1054 (42S22): Unknown column").status,
                         "unavailable: error 1054")
        self.assertEqual(measured("count", returncode=127, stderr="sh: mysql: not found").status,
                         "unavailable: client failed")

    def test_a_missing_client_is_reported(self):
        def absent(*args, **kwargs):
            raise FileNotFoundError("mysql")
        self.assertEqual(scorecard.measure(metric("count"), ["mysql"], AS_OF, 5, run=absent).status,
                         "unavailable: client not found")


class Ledger(unittest.TestCase):
    def test_rows_are_appended_under_one_header(self):
        ratio = scorecard.Result(metric("ratio", "chain_2_notified"), "ok", numerator=1, denominator=4)
        gone = scorecard.Result(metric("count", "notices_sent"), "unavailable: table notification_messages missing")
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "ledger.csv"
            scorecard.append_ledger(path, [ratio, gone], dt.datetime(2026, 11, 18, 9), AS_OF, "first")
            scorecard.append_ledger(path, [ratio], dt.datetime(2026, 11, 25, 9), AS_OF, "")
            with path.open() as handle:
                rows = list(csv.DictReader(handle))
        self.assertEqual(len(rows), 3)
        self.assertEqual(rows[0], {
            "measured_at": "2026-11-18T09:00:00Z", "as_of": "2026-11-17T00:00:00Z", "metric": "chain_2_notified",
            "value": "0.25", "numerator": "1", "denominator": "4", "sample": "", "status": "ok", "note": "first",
        })
        self.assertEqual((rows[1]["value"], rows[1]["status"]), ("", "unavailable: table notification_messages missing"))


@unittest.skipUnless(os.environ.get("SCORECARD_TEST_MYSQL"), "set SCORECARD_TEST_MYSQL to run against MySQL")
class AgainstMySQL(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.admin = shlex.split(os.environ["SCORECARD_TEST_MYSQL"])
        cls.database = f"scorecard_test_{os.getpid()}"
        cls.sql(f"DROP DATABASE IF EXISTS {cls.database}; CREATE DATABASE {cls.database};")
        cls.addClassCleanup(cls.sql, f"DROP DATABASE IF EXISTS {cls.database};")
        cls.sql(cls.schema() + (HERE / "testdata/rows.sql").read_text(), database=cls.database)

    @classmethod
    def sql(cls, script, database=None):
        command = cls.admin + ([database] if database else [])
        done = subprocess.run(command, input=script, capture_output=True, text=True, timeout=120)
        if done.returncode != 0:
            raise AssertionError(f"mysql failed: {done.stderr.strip()}")
        return done.stdout

    @staticmethod
    def schema():
        statements = []
        init = (REPO / "db/init.sql").read_text()
        for table in SQL_FILE_TABLES:
            found = re.search(rf"CREATE TABLE IF NOT EXISTS {table}\s*\(.*?;", init, re.S)
            assert found, f"{table} not found in db/init.sql"
            statements.append(found.group(0))
        for table, source in GO_TABLES:
            found = re.search(rf"CREATE TABLE (?:IF NOT EXISTS )?{table} \(.*?(?=`)", (REPO / source).read_text(), re.S)
            assert found, f"{table} not found in {source}"
            statements.append(found.group(0).rstrip() + ";")
        statements.append((REPO / "epc-pusher/lib/sql/epc_schema.sql").read_text())
        return "\n".join(statements) + "\n"

    def measure_all(self, backlog_min=2):
        client = self.admin + [self.database]
        return {m.id: scorecard.measure(m, client, AS_OF, backlog_min) for m in scorecard.load_metrics()}

    def test_every_metric_matches_the_fixture(self):
        got = {}
        for metric_id, result in self.measure_all().items():
            self.assertEqual(result.status, "ok", metric_id)
            if result.metric.kind == "count":
                got[metric_id] = result.numerator
            elif result.metric.kind == "ratio":
                got[metric_id] = (result.numerator, result.denominator)
            else:
                got[metric_id] = (result.numerator, result.sample)
        self.assertEqual(got, EXPECTED)

    def test_the_backlog_threshold_is_applied(self):
        self.assertEqual(self.measure_all(backlog_min=3)["parties_with_unsent_evidence"].numerator, 0)

    def test_a_metric_cannot_write(self):
        attempt = scorecard.Metric(id="x", group="chain", title="t", kind="count", window="w", better="higher",
                                   definition="d", sql="DELETE FROM opted_out_emails")
        result = scorecard.measure(attempt, self.admin + [self.database], AS_OF, 5)
        self.assertEqual(result.status, "unavailable: error 1792")  # read-only transaction
        self.assertEqual(self.sql("SELECT COUNT(*) FROM opted_out_emails;", database=self.database).split()[-1], "2")

    def test_a_missing_table_costs_only_the_metrics_that_need_it(self):
        self.sql("RENAME TABLE notification_messages TO notification_messages_away;", database=self.database)
        try:
            results = self.measure_all()
        finally:
            self.sql("RENAME TABLE notification_messages_away TO notification_messages;", database=self.database)
        self.assertEqual(results["notices_sent"].status, "unavailable: table notification_messages missing")
        self.assertEqual(results["chain_2_notified"].status, "ok")


if __name__ == "__main__":
    unittest.main()
