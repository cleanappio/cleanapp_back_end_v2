import contextlib
import io
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
import unittest
from unittest import mock
import uuid

import ingest


class PublicFirstReportTest(unittest.TestCase):
    def run_main(self, argv, source, token=""):
        output = io.StringIO()
        with mock.patch.object(sys, "argv", ["ingest.py"] + argv), mock.patch.object(sys, "stdin", io.StringIO(json.dumps(source))), mock.patch.dict(os.environ, {"CLEANAPP_API_TOKEN": token}), contextlib.redirect_stdout(output):
            result = ingest.main()
        return result, output.getvalue()

    def test_description_only_dry_run_needs_no_token_and_does_no_network(self):
        with mock.patch.object(ingest, "post_json") as post:
            code, out = self.run_main(["--dry-run"], {"description": "Observed broken checkout"})
            self.assertEqual(code, 0)
            payload = json.loads(out)
            self.assertTrue(payload["url"].endswith("/api/v1/public/agent-reports"))
            self.assertEqual(uuid.UUID(payload["payload"]["source_id"]).version, 4)
            self.assertNotIn("delivery", payload["payload"])
            post.assert_not_called()

    def test_retry_retains_private_uuid_and_rich_media_is_rejected(self):
        source_id = str(uuid.uuid4())
        item = {"description": "Observed problem", "source_id": source_id}
        self.assertEqual(ingest.wrap_public_submission(item), ingest.wrap_public_submission(item))
        for changed in [{**item, "source_id": "test123"}, {**item, "media": []}, {**item, "delivery": {"requested_lane": "publish"}}]:
            with self.assertRaises(SystemExit):
                ingest.wrap_public_submission(changed)

    def test_broker_failure_preserves_private_receipt_without_logging_token(self):
        token = "gdt_synthetic_private_capability"
        receipt = {"receipt_id": "rcpt_fixture", "tracking_token": token, "publication_status": "private", "processing_status": "pending_retry"}
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp) / "receipt.json"
            with mock.patch.object(ingest, "post_json", return_value=(503, json.dumps(receipt))) as post:
                code, output = self.run_main(["--receipt-file", str(target)], {"description": "Observed problem", "source_id": str(uuid.uuid4())})
            self.assertEqual(code, 2)
            self.assertNotIn(token, output)
            self.assertEqual(json.loads(target.read_text())["tracking_token"], token)
            self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o600)
            self.assertEqual(post.call_args.args[1], "")
            self.assertTrue(post.call_args.args[0].endswith("/api/v1/public/agent-reports"))
            with self.assertRaises(FileExistsError):
                ingest.save_private_receipt(str(target), "overwrite")

    def test_tracking_receipt_uses_capability_not_integration_token(self):
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp) / "receipt.json"
            target.write_text(json.dumps({"tracking_token": "gdt_private"}))
            with mock.patch.object(ingest, "get_json", return_value=(200, '{"publication_status":"private"}')) as get:
                code, _ = self.run_main(["--status-receipt-id", "rcpt_fixture", "--tracking-token-file", str(target)], {}, token="approved-integration-key")
            self.assertEqual(code, 0)
            self.assertIn("/api/v1/public/agent-reports/receipts/", get.call_args.args[0])
            self.assertEqual(get.call_args.args[1], "gdt_private")

    def test_approved_wire_batch_stays_on_existing_path(self):
        with mock.patch.object(ingest, "post_json", return_value=(200, '{}')) as post:
            code, _ = self.run_main([], [{"source_id": "old-a", "title": "A", "description": "Observed A"}, {"source_id": "old-b", "title": "B", "description": "Observed B"}], token="approved-key")
        self.assertEqual(code, 0)
        self.assertTrue(post.call_args.args[0].endswith("/api/v1/agent-reports:batchSubmit"))
        self.assertEqual(post.call_args.args[1], "approved-key")
        self.assertEqual(len(post.call_args.args[2]["items"]), 2)


if __name__ == "__main__":
    unittest.main()
