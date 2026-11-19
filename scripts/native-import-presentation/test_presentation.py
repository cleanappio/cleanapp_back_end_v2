"""Offline presentation boundaries; no database, network or production access."""
import copy
import inspect
import json
from pathlib import Path
import struct
import unittest

import epoch


CATALOGUE = {"objects": [{"id": 7, "key": "bottle"}, {"id": 8, "key": "food_wrapper"}],
             "categories": [{"id": 1, "key": "plastic"}]}
SEEN = "2026-11-10T12:00:00Z"


def candidate():
    feature = {"type": "Feature", "properties": {"id": 11, "verified": 2,
        "name": "PRIVATE_PERSON", "username": "PRIVATE_USER", "team": "PRIVATE_TEAM",
        "summary": {"tags": [{"object_id": 7, "category_id": 1, "quantity": 2}],
                    "keys": {"objects": {"7": "bottle"}}}}}
    return {"original_feature_json": json.dumps(feature), "source_id": "11",
        "source_url": "https://openlittermap.com/global?photo=11",
        "tag_catalogue_sha256": epoch.sha(epoch.canonical(CATALOGUE)),
        "native_text": {"reports_description": "source: OpenLitterMap.com\nHistorical observation 11.",
            "report_analysis_description": "source: OpenLitterMap.com\nOriginal date/status retained.\n© OpenLitterMap & Contributors."},
        "presentation": {"title": "Historical OpenLitterMap observation"},
        "native_projection": {"reporter_id": "historical:openlittermap.com:fixture",
            "ts_utc_seconds": "2020-01-02 02:04:05"},
        "exact_source_location": {"latitude_decimal": "53.1234567890123", "longitude_decimal": "-7.1234567890123"}}


class ReplayDatabase:
    def __init__(self, c, display):
        self.c = c
        f32 = lambda value: struct.unpack("!f", struct.pack("!f", float(value)))[0]
        self.report = {"id": c["native_projection"]["reporter_id"],
            "ts": c["native_projection"]["ts_utc_seconds"],
            "latitude": f32(c["exact_source_location"]["latitude_decimal"]),
            "longitude": f32(c["exact_source_location"]["longitude_decimal"]),
            "description": display["reports_description"]}
        self.analysis = {key: display[key] for key in ("source", "title", "description", "summary")}
        self.analysis.update(severity=0, language="en", brand="", display="", valid=1,
            classification="physical", review=0, contacts="[]", analysis_text_null=1, analysis_image_null=1)
        self.raw = "\t".join([epoch.NAMESPACE, c["source_id"], "cleanapp-historical-import", "olm-native-v1",
            "2026-11-10 12:00:00", "historical_import", "public", "unverified", "0"])
        self.index = "\t".join([epoch.SOURCE, c["source_id"], c["native_projection"]["ts_utc_seconds"],
                                   c["source_url"].encode().hex().upper()])
        self.catalogue = epoch.canonical(CATALOGUE)
        self.catalogue_reads = 0

    def query(self, sql):
        if sql.startswith("SELECT JSON_OBJECT('id'"):
            return [json.dumps(self.report)]
        if " FROM report_raw " in sql:
            return [self.raw]
        if " FROM external_ingest_index " in sql:
            return [self.index]
        if " FROM report_analysis " in sql:
            assert sql.endswith(" FOR UPDATE")
            return [json.dumps(self.analysis)]
        if " FROM olm_source_catalogues " in sql:
            self.catalogue_reads += 1
            return [self.catalogue.encode().hex()]
        raise AssertionError("Unexpected query")


class PresentationTests(unittest.TestCase):
    def setUp(self):
        epoch._DISPLAY_CATALOGUE_CACHE.clear()

    def test_taxonomy_title_and_private_names_never_used(self):
        c = candidate()
        before = copy.deepcopy(c)
        display = epoch.derived_presentation(c, CATALOGUE)
        self.assertEqual(display["title"], "Bottle (2)")
        self.assertEqual(display["source"], "OLM")
        self.assertEqual(display["summary"], display["title"])
        self.assertEqual(display["reports_description"], c["native_text"]["reports_description"])
        self.assertEqual(display["description"], c["native_text"]["report_analysis_description"])
        self.assertNotIn("PRIVATE_", json.dumps(display))
        self.assertEqual(c, before)

    def test_catalogue_fallback_category_and_no_fabricated_name(self):
        f = json.loads(candidate()["original_feature_json"])
        f["properties"]["summary"].pop("keys")
        self.assertEqual(epoch.report_title(f, CATALOGUE), "Bottle (2)")
        f["properties"]["summary"]["tags"][0]["object_id"] = 999
        self.assertEqual(epoch.report_title(f, CATALOGUE), "Plastic (2)")
        f["properties"].pop("summary")
        self.assertEqual(epoch.report_title(f, CATALOGUE), "Litter report")

    def test_quantity_and_order_are_deterministic_and_bounded(self):
        f = json.loads(candidate()["original_feature_json"])
        tags = f["properties"]["summary"]["tags"]
        tags += [{"object_id": 8, "quantity": 3}, {"object_id": 7, "quantity": 2},
                 {"object_id": 7, "quantity": True}, {"object_id": 7, "quantity": -1}]
        self.assertEqual(epoch.report_title(f, CATALOGUE), "Bottle (4), food wrapper (3)")
        tags.reverse()
        self.assertEqual(epoch.report_title(f, CATALOGUE), "Bottle (4), food wrapper (3)")
        f["properties"]["summary"]["keys"]["objects"]["7"] = "x" * 1000
        self.assertLessEqual(len(epoch.report_title(f, CATALOGUE)), 240)

    def test_markup_is_inert_and_unknown_attribution_refused(self):
        f = json.loads(candidate()["original_feature_json"])
        f["properties"]["summary"]["keys"]["objects"]["7"] = "<img>&copy;"
        title = epoch.report_title(f)
        self.assertFalse(any(character in title for character in "<>&"))
        self.assertIn(r"\u003c", title)
        with self.assertRaisesRegex(ValueError, "attribution"):
            epoch.presentation_from_feature(f, "arbitrary unreviewed description")

    def test_exact_legacy_replay_uses_no_catalogue_or_material_change(self):
        c = candidate(); display = epoch.legacy_presentation(c)
        db = ReplayDatabase(c, display)
        self.assertEqual(epoch.verify_written_fields(db, c, 12, {"first_seen_at": SEEN}), display)
        self.assertEqual(db.catalogue_reads, 0)

    def test_exact_new_replay_uses_hash_verified_retained_catalogue(self):
        c = candidate(); display = epoch.derived_presentation(c, CATALOGUE)
        db = ReplayDatabase(c, display)
        self.assertEqual(epoch.verify_written_fields(db, c, 12, {"first_seen_at": SEEN}), display)
        self.assertEqual(db.catalogue_reads, 1)

    def test_mixed_or_curated_display_is_rejected(self):
        c = candidate(); old = epoch.legacy_presentation(c); new = epoch.derived_presentation(c, CATALOGUE)
        for field in ("source", "title", "summary"):
            mixed = dict(new); mixed[field] = old[field]
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "curated"):
                epoch.verify_written_fields(ReplayDatabase(c, mixed), c, 12, {"first_seen_at": SEEN})
        for field, value in (("reports_description", new["title"]), ("title", "Editor title"), ("description", new["description"] + " changed")):
            changed = dict(new); changed[field] = value
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "curated"):
                epoch.verify_written_fields(ReplayDatabase(c, changed), c, 12, {"first_seen_at": SEEN})

    def test_evidence_source_policy_and_catalogue_tamper_still_rejected(self):
        c = candidate(); new = epoch.derived_presentation(c, CATALOGUE)
        for field in ("severity", "review", "analysis_text_null", "contacts"):
            db = ReplayDatabase(c, new); db.analysis[field] = "tampered"
            with self.subTest(field=field), self.assertRaises(ValueError):
                epoch.verify_written_fields(db, c, 12, {"first_seen_at": SEEN})
        db = ReplayDatabase(c, new); db.index = db.index.replace("OpenLitterMap.com", "OLM")
        with self.assertRaisesRegex(ValueError, "External source"):
            epoch.verify_written_fields(db, c, 12, {"first_seen_at": SEEN})
        db = ReplayDatabase(c, new); db.catalogue += " "
        with self.assertRaisesRegex(ValueError, "hash mismatch"):
            epoch.verify_written_fields(db, c, 12, {"first_seen_at": SEEN})

    def test_immutable_candidate_validation_and_material_fields_are_unchanged(self):
        original = Path(__file__).parent.parent / "native-presentation-runtime/epoch.py"
        if original.exists():
            import ast
            old = ast.parse(original.read_text())
            fresh = ast.parse(inspect.getsource(epoch))
            select = lambda tree: next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == "valid_candidate")
            self.assertEqual(ast.dump(select(old)), ast.dump(select(fresh)))
        self.assertEqual(epoch.MATERIAL_KEYS, ("source_namespace", "source_id", "source_feature_sha256", "source_url", "original_media",
            "display_media", "exact_source_location", "source_timestamp_raw", "source_timestamp_utc", "tag_catalogue_sha256", "presentation", "native_text", "side_effect_policy"))


if __name__ == "__main__":
    unittest.main()
