import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from epc_import_legacy import FILES, generate_sql, literal, load_history, prepare

STAMP = "2025-10-07 18:05:33"
SNAPSHOT = "a" * 64


def fixture():
    template = "Report https://cleanapp.io/digital/{{ report.brand_name }}\n"
    history = {
        "epc_campaigns": [{"slug": "reports", "description": "Reports", "created": STAMP}],
        "epc_contracts": [{"id": 10, "key": "cleanapp/brand/brand's `name`", "address": "0x" + "1" * 40, "created": STAMP}],
        "epc_sent_message_templates_read_only": [{"id": i, "body": template, "created": STAMP} for i in [20, 21]],
        "epc_outbox": [{"id": i, "campaign": "reports", "contract_id": 10, "metadata": {"report_seq": 100 + i, "blockscanMessageId": str(200 + i)}, "status": "sent", "sent_message_template_id": 20, "created": STAMP} for i in [1, 2]],
    }
    provenance = {table: {"source_sha256": "b" * 64, "decoded_sha256": "c" * 64, "rows": len(rows)} for table, rows in history.items()}
    return history, provenance


class LegacyImportTests(unittest.TestCase):
    def test_preserves_all_ids_templates_and_original_receipts(self):
        history, provenance = fixture()
        prepared, mapping = prepare(history, provenance, SNAPSHOT)
        self.assertEqual([r["id"] for r in prepared["epc_sent_message_templates_read_only"]], [20, 21])
        self.assertEqual([r["id"] for r in prepared["epc_outbox"]], [1, 2])
        row = prepared["epc_outbox"][0]
        self.assertEqual(row["metadata"]["blockscanMessageId"], "201")
        self.assertEqual(row["source_report_seq"], 101)
        self.assertEqual(row["event_key"], "legacy-dev:report:101")
        self.assertEqual(row["message_body"], "Report https://cleanapp.io/digital/brand's `name`\n")
        self.assertTrue(row["metadata"]["_legacy_import"]["message_body_reconstructed"])
        self.assertEqual(row["metadata"]["_legacy_import"]["message_body_brand_source"], "trimmed_epc_contract_key")
        self.assertEqual(history["epc_outbox"][0]["metadata"], {"report_seq": 101, "blockscanMessageId": "201"})

    def test_verified_identity_reserves_prod_event_without_rewriting_legacy_seq(self):
        history, provenance = fixture()
        mapping = [{"legacy_report_seq": 101, "production_report_seq": 999, "source": "github_issue", "external_id": "repo/issue/7"}]
        prepared, used = prepare(history, provenance, SNAPSHOT, mapping)
        row = prepared["epc_outbox"][0]
        self.assertEqual((row["event_key"], row["source_namespace"], row["source_report_seq"]), ("prod:report:999", "legacy-dev", 101))
        self.assertEqual(row["metadata"]["report_seq"], 101)
        sql = generate_sql(prepared, provenance, used)
        self.assertIn("BINARY t.external_id=BINARY s.external_id AND t.seq=s.prod_seq", sql)
        self.assertNotIn("UPDATE epc_report_cursors", sql)

    def test_multiple_historical_receipts_for_one_prod_event_remain_preserved(self):
        history, provenance = fixture()
        mapping = [{"legacy_report_seq": seq, "production_report_seq": 999, "source": "github_issue", "external_id": str(seq)} for seq in [101, 102]]
        prepared, _ = prepare(history, provenance, SNAPSHOT, mapping)
        self.assertEqual([r["event_key"] for r in prepared["epc_outbox"]], ["prod:report:999", "legacy-dev:report:102"])

    def test_rejects_orphan_duplicate_receipt_and_non_sent_records(self):
        for field, value in [("contract_id", 999), ("sent_message_template_id", 999), ("status", "pending")]:
            with self.subTest(field=field):
                history, provenance = fixture(); history["epc_outbox"][0][field] = value
                with self.assertRaises(ValueError):
                    prepare(history, provenance, SNAPSHOT)
        history, provenance = fixture()
        history["epc_outbox"][1]["metadata"]["blockscanMessageId"] = "201"
        with self.assertRaisesRegex(ValueError, "Duplicate Blockscan"):
            prepare(history, provenance, SNAPSHOT)

    def test_conflicts_abort_instead_of_replacing_target_history(self):
        history, provenance = fixture()
        prepared, mappings = prepare(history, provenance, SNAPSHOT)
        sql = generate_sql(prepared, provenance, mappings)
        self.assertIn("CHECK(ok=1)", sql)
        self.assertIn("ON t.`event_key`=s.`event_key`", sql)
        self.assertIn("ON t.`blockscan_message_id`=s.`blockscan_message_id`", sql)
        self.assertNotIn("INSERT IGNORE", sql)
        self.assertNotIn("REPLACE INTO", sql)
        self.assertNotIn("ON DUPLICATE KEY UPDATE", sql)
        self.assertEqual(sql.count("START TRANSACTION;"), 1)
        self.assertEqual(sql.count("COMMIT;"), 1)

    def test_values_use_utf8_hex_including_quotes_backslashes_and_unicode(self):
        value = "O'Reilly\\path\n日本語 `quoted`"
        encoded = literal(value)
        self.assertEqual(encoded, "CONVERT(0x" + value.encode().hex() + " USING utf8mb4)")
        self.assertNotIn("O'Reilly", encoded)

    def test_rejects_changed_source_bytes_before_generating_import(self):
        history, _ = fixture()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for table, name in FILES.items():
                source = root / (table + ".ibd"); source.write_bytes(b"original source")
                rows = history[table]
                document = {"table": table, "timestamp_timezone": "UTC", "row_count": len(rows), "rows": [{"values": row} for row in rows], "pages": [{"checksum_ok": True, "header_rows": len(rows)}], "source_file": str(source), "source_sha256": hashlib.sha256(source.read_bytes()).hexdigest()}
                (root / name).write_text(json.dumps(document))
            loaded, _ = load_history(root)
            self.assertEqual(len(loaded["epc_outbox"]), 2)
            (root / "epc_outbox.ibd").write_bytes(b"modified source")
            with self.assertRaisesRegex(ValueError, "Source hash mismatch"):
                load_history(root)


if __name__ == "__main__":
    unittest.main()
