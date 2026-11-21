import copy
import hashlib
import json
from pathlib import Path
import sys
import subprocess
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from sorter import (adapt_saved_review_pool, annotation_consensus, disposition, freeze_originals,
                    index_holds, load_config, select_review_set)


def candidate(seq, category="litter", sha=None):
    sha = sha or hashlib.sha256(str(seq).encode()).hexdigest()
    return {
        "seq": seq, "original_sha256": sha, "current_original_sha256": sha,
        "source_snapshot_sha256": "a" * 64, "visibility": "public",
        "hold_snapshot_sha256": "f" * 64, "holds_complete": True,
        "active_actions": 0, "shared_holds": 0, "width": 1200, "height": 900,
        "image_bytes": 200000, "mime_type": "image/jpeg", "header_hex": "FFD8FFE0",
        "latitude": 47 + seq / 100, "longitude": 8 + seq / 100,
        "source_group": str(seq), "stratum": category, "base_score": 100,
        "quality_evidence": {"original_sha256": sha, "status": "eligible",
                             "artifact_sha256": "b" * 64},
        "visible_review": {"original_sha256": sha,
                           "status": "clear_for_private_analysis",
                           "artifact_sha256": "c" * 64},
        "annotations": [{"source": "Gemini", "language": "en", "title": "Litter",
                         "description": "A plastic bottle lies on pavement."}],
    }


def config(n=3):
    cfg = load_config()
    cfg["quotas"] = {"litter": n}
    return cfg


class EvidencePolicyTests(unittest.TestCase):
    def test_adapter_pins_holds_and_does_not_borrow_stale_dimensions_or_paths(self):
        original = "1" * 64
        pool = [{"seq": 1, "original_sha256": original}]
        report = {"kind": "report", "seq": 1, "original_sha256": original}
        dimensions = [{"seq": 1, "original_sha256": "0" * 64,
                       "old_width": 4000, "old_height": 3000, "path": "/private/original"}]
        adapted = list(adapt_saved_review_pool(pool, [report], dimensions, "a" * 64,
                       [{"id": "privacy", "kind": "privacy", "original_sha256": original}], "b" * 64))[0]
        self.assertIsNone(adapted["width"])
        self.assertNotIn("path", adapted)
        self.assertTrue(adapted["holds_complete"])
        self.assertEqual(disposition(adapted)["status"], "hold_privacy")

    def test_incomplete_hold_snapshot_is_not_eligibility(self):
        row = candidate(1)
        row["holds_complete"] = False
        self.assertEqual(disposition(row)["status"], "hold_policy_snapshot")

    def test_flat_alias_id_and_hash_entries_never_become_a_pair(self):
        by_seq, by_sha = index_holds([
            {"report_seq": "1", "original_sha256": None, "hold_types": ["privacy_review"]},
            {"report_seq": None, "original_sha256": "1" * 64, "hold_types": ["historical_alias_review"]}])
        self.assertIsNone(by_seq["1"][0]["original_sha256"])
        self.assertIsNone(by_sha["1" * 64][0]["seq"])

    def test_stale_original_or_source_binding_never_admitted(self):
        row = candidate(1)
        row["current_original_sha256"] = "0" * 64
        self.assertEqual(disposition(row)["status"], "hold_binding")
        row = candidate(1)
        row["quality_evidence"]["original_sha256"] = "0" * 64
        self.assertEqual(disposition(row)["status"], "hold_quality_evidence")

    def test_caption_absence_never_releases_privacy(self):
        row = candidate(1)
        row["holds"] = [{"id": "privacy1", "kind": "privacy", "original_sha256": row["original_sha256"]}]
        row["annotations"][0]["description"] = "A clean road with no people."
        self.assertEqual(disposition(row)["status"], "hold_privacy")

    def test_image_hold_follows_exact_hash_alias_not_cartesian_pairing(self):
        row = candidate(2)
        row["holds"] = [{"id": "p", "kind": "privacy", "seq": 1,
                         "original_sha256": row["original_sha256"]}]
        self.assertEqual(disposition(row)["status"], "hold_privacy")
        row["holds"][0]["original_sha256"] = "0" * 64
        self.assertEqual(disposition(row)["status"], "eligible_for_owner_review")

    def test_report_only_hold_remains_unpaired(self):
        row = candidate(1)
        row["holds"] = [{"id": "hint", "kind": "caption_hint", "seq": 1}]
        self.assertEqual(disposition(row)["status"], "hold_caption_hint")

    def test_only_explicit_same_image_reason_bound_review_retires_hint(self):
        row = candidate(1)
        hold = {"id": "hint", "kind": "caption_hint", "seq": 1,
                "reason_code": "black_or_blank_image_claim", "observed_at": "2026-10-18T17:29:00Z"}
        row["holds"] = [hold]
        release = {"supersedes_id": "hint", "kind": "caption_hint",
                   "reason_code": hold["reason_code"], "original_sha256": row["original_sha256"],
                   "source_snapshot_sha256": row["source_snapshot_sha256"],
                   "evidence_role": "explicit_saved_review", "artifact_sha256": "d" * 64,
                   "reviewed_at": "2026-11-08T00:00:00Z",
                   "decision": "retire_specific_hint"}
        row["supersessions"] = [release]
        self.assertEqual(disposition(row)["status"], "eligible_for_owner_review")
        for field, value in [("original_sha256", "0" * 64),
                             ("source_snapshot_sha256", "0" * 64),
                             ("evidence_role", "generated_caption"),
                             ("reviewed_at", "2026-10-15T00:00:00Z"),
                             ("reviewed_at", "2026-11-08T00:00:00")]:
            changed = copy.deepcopy(row)
            changed["supersessions"][0][field] = value
            self.assertEqual(disposition(changed)["status"], "hold_caption_hint")

    def test_hint_retirement_cannot_remove_another_privacy_or_hash_hold(self):
        row = candidate(1)
        row["holds"] = [{"id": "p", "kind": "privacy", "original_sha256": row["original_sha256"]},
                        {"id": "h", "kind": "caption_hint", "seq": 1}]
        self.assertEqual(disposition(row)["status"], "hold_privacy")
        self.assertFalse(disposition(row)["privacy_or_rights_cleared"])

    def test_urgency_coupled_spam_is_not_binary_spam(self):
        row = candidate(1)
        row["manual_vote"] = {"verdict": "spam", "urgency": 7}
        result = disposition(row)
        self.assertEqual(result["status"], "eligible_for_owner_review")
        self.assertIn("positive_urgency_vote_is_ambiguous", result["notes"])
        row["manual_vote"]["urgency"] = 0
        self.assertEqual(disposition(row)["status"], "hold_manual_candidate")

    def test_text_consensus_uses_saved_english_variants_not_translation_votes(self):
        base = {"source": "Gemini", "language": "en", "title": "Black image",
                "description": "The image is completely black with no visible content."}
        translated = dict(base, language="de")
        self.assertEqual(annotation_consensus([base, translated])["status"], "insufficient_saved_variants")
        other = dict(base, source="ChatGPT")
        self.assertEqual(annotation_consensus([base, other])["status"], "nominate_quality_review")
        other["description"] = "Visible litter is present on the pavement."
        self.assertEqual(annotation_consensus([base, other])["status"], "conflicting_saved_text")

    def test_missing_model_text_is_unknown_not_clean(self):
        self.assertEqual(annotation_consensus([])["status"], "missing_saved_text")

    def test_imported_post_in_analysis_table_is_not_an_ai_variant(self):
        rows = [{"source": source, "language": "en",
                 "description": "The image is completely black with no visible content."}
                for source in ("Gemini", "bluesky")]
        self.assertEqual(annotation_consensus(rows)["status"], "insufficient_saved_variants")

    def test_source_purpose_and_digital_failure_prevent_junk_nomination(self):
        rows = [{"source": name, "language": "en", "description": "The image is completely black with no visible content."}
                for name in ("ChatGPT", "Gemini")]
        self.assertEqual(annotation_consensus(rows, original_report_text="The app opens to a black screen.")["status"],
                         "source_purpose_requires_review")
        for row in rows:
            row["description"] += " This could be a display failure."
        self.assertEqual(annotation_consensus(rows)["status"], "conflicting_saved_text")

    def test_black_object_local_blur_and_actual_litter_are_preserved(self):
        for description in ("A black bag lies on the pavement; litter is visible.",
                            "The background is blurry, but a plastic bottle is visible.",
                            "The sign text is illegible; visible debris is present.",
                            "A selfie shows a loose rope on the floor, a potential trip hazard."):
            rows = [{"source": name, "language": "en", "description": description}
                    for name in ("ChatGPT", "Gemini")]
            self.assertNotEqual(annotation_consensus(rows)["status"], "nominate_quality_review")


class SelectionTests(unittest.TestCase):
    def test_determinism_deduplication_and_config_unchanged(self):
        rows = [candidate(n) for n in range(1, 9)]
        rows.append(candidate(99, sha=rows[0]["original_sha256"]))
        cfg = config()
        before = copy.deepcopy(cfg)
        a = select_review_set(rows, cfg)
        b = select_review_set(list(reversed(rows)), cfg)
        self.assertEqual([r["seq"] for r in a["selected"]], [r["seq"] for r in b["selected"]])
        self.assertEqual(len({r["original_sha256"] for r in a["selected"]}), 3)
        self.assertEqual(cfg, before)

    def test_saved_dhash_only_filters_near_duplicate_in_same_coarse_cell(self):
        rows = [candidate(n) for n in range(1, 5)]
        for r in rows:
            r.update(latitude=47.01, longitude=8.01, dhash="0" * 16)
        with self.assertRaisesRegex(ValueError, "Insufficient"):
            select_review_set(rows, config(2))

    def test_shortage_reallocation_is_finite_and_audited(self):
        cfg = config(2)
        cfg["quotas"] = {"water_utilities": 1, "litter": 2}
        result = select_review_set([candidate(n) for n in range(1, 17)], cfg)
        self.assertEqual(len(result["selected"]), 3)
        self.assertTrue(result["quota_adjustments"])
        with self.assertRaisesRegex(ValueError, "Insufficient"):
            select_review_set([candidate(1)], cfg)

    def test_unknown_geography_and_unsafe_text_never_pass_showcase_gates(self):
        rows = [candidate(n) for n in range(1, 5)]
        rows[0]["latitude"] = float("nan")
        rows[1]["annotations"][0]["description"] = "A child by a damaged wall."
        rows[2]["annotations"] = []
        with self.assertRaisesRegex(ValueError, "Insufficient"):
            select_review_set(rows, config(2))

    def test_plain_no_litter_and_people_are_never_global_junk_verdicts(self):
        row = candidate(1)
        row["annotations"][0]["description"] = "People beside a clean road with no litter."
        self.assertEqual(disposition(row)["status"], "eligible_for_owner_review")
        self.assertFalse(disposition(row)["global_junk_confirmed"])
        with self.assertRaises(ValueError):
            select_review_set([row], config(1))


class OriginalFreezeTests(unittest.TestCase):
    def test_copy_is_byte_exact_and_mismatch_refuses_before_copy(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            original = root / "original"
            original.write_bytes(b"fixture original bytes")
            row = candidate(1, sha=hashlib.sha256(original.read_bytes()).hexdigest())
            rows = freeze_originals([row], {1: str(original)}, root / "copies")
            self.assertEqual((root / "copies" / rows[0]["file_name"]).read_bytes(), original.read_bytes())
            changed = dict(row, original_sha256="0" * 64)
            with self.assertRaisesRegex(ValueError, "hash"):
                freeze_originals([changed], {1: str(original)}, root / "other")
            self.assertFalse((root / "other").exists())

    def test_destination_symlink_never_writes_elsewhere(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            original = root / "original"
            original.write_bytes(b"fixture")
            row = candidate(1, sha=hashlib.sha256(b"fixture").hexdigest())
            out = root / "copies"
            out.mkdir()
            (out / "CA-0001-S1.jpg").symlink_to(root / "unrelated")
            with self.assertRaisesRegex(ValueError, "symlink"):
                freeze_originals([row], {1: str(original)}, out)
            self.assertFalse((root / "unrelated").exists())


class CommandTests(unittest.TestCase):
    def test_failed_selection_publishes_no_partial_output(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source, cfg, output = root / "input.jsonl", root / "config.json", root / "out.jsonl"
            source.write_text(json.dumps(candidate(1)) + "\n")
            cfg.write_text(json.dumps(config(2)))
            command = [sys.executable, str(Path(__file__).resolve().parents[1] / "sorter.py"),
                       "select", "--input", str(source), "--output", str(output), "--config", str(cfg)]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(output.exists())
            self.assertEqual(sorted(p.name for p in root.iterdir()), ["config.json", "input.jsonl"])

    def test_existing_input_or_output_cannot_be_overwritten(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "input.jsonl"
            original = json.dumps(candidate(1)) + "\n"
            path.write_text(original)
            result = subprocess.run([sys.executable, str(Path(__file__).resolve().parents[1] / "sorter.py"),
                                     "reconcile", "--input", str(path), "--output", str(path)], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(path.read_text(), original)


if __name__ == "__main__":
    unittest.main()
