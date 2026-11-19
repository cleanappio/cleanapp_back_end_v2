#!/usr/bin/env python3
"""Bounded, quiet native SolveSF import. Offline PLAN is the default.

The production entrypoint requires a separately reviewed root-owned guard which
owns the deployment/OLM boundary and actual indexed privacy/hash coverage. A
candidate, a JSON assertion, or valid SQL schema alone cannot authorize a write.
No model, queue, mail, notification, reward, or source HTTP client exists here.
"""
from __future__ import annotations

import argparse
import base64
import contextlib
import datetime as dt
import decimal
import hashlib
import html
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import sys

# Reuse only the reviewed, data-only SQL encoders; do not parameterize the
# DataUnion importer, its photo-unique ledger, or the OLM source adapter.
HELPERS = Path(__file__).resolve().parents[1] / "historical-imports/dataunion_0_1_0/candidate_data.py"
_spec = importlib.util.spec_from_file_location("solvesf_sql_helpers", HELPERS)
_helpers = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = _helpers
_spec.loader.exec_module(_helpers)
canonical, sql_string, sql_bytes, sql_num = (_helpers.canonical, _helpers.sql_string,
                                            _helpers.sql_bytes, _helpers.sql_num)
NAMESPACE = "sf311"
PUBLISHER = "SolveSF"
SOURCE = "SSF"
LEGACY_TITLE = "Historical SF311 case via SolveSF"
SERVER_UUID = "f2c484f6-3c5d-11ef-aace-0242ac120003"
MAX_BATCH = 5
MAX_DISPLAY = 128 * 1024
FLAGS = ("suppress_email", "suppress_notifications", "suppress_rewards",
         "suppress_routing", "suppress_analysis")
RUNTIME = Path("/srv/cleanapp-solvesf-import")
CONTROL_PATH = RUNTIME / "evidence/controls.json"
GUARD_PATH = RUNTIME / "app/guard.py"
ALLOWED_SOURCE_FIELDS = frozenset((
    "service_request_id", "source", "source_system", "source_attribution",
    "submitter_origin", "description_authorship", "requested_at", "updated_at",
    "requested_datetime_raw", "updated_datetime_raw",
    "status", "status_notes", "service_name", "service_code", "address", "category",
    "description", "photos", "resolved_verified", "resolution_verified",
    "latitude", "longitude", "source_url", "publisher", "publisher_record_id",
))
ALLOWED_CANDIDATE_FIELDS = frozenset(("schema", "source_namespace", "source_id", "publisher",
    "publisher_record_id", "source_url", "requested_at", "updated_at", "latitude", "longitude",
    "text", "media_urls", "source_record", "candidate_sha256"))

# Root applies this additive DDL through the canonical migration path after
# actual-MySQL validation. This module never executes DDL.
LEDGER_DDL = """CREATE TABLE sf311_source_case_ledger (
 source_namespace VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 source_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 payload_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 material_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 publisher VARCHAR(64) NOT NULL,
 source_url VARCHAR(512) NOT NULL,
 source_record_json JSON NOT NULL,
 candidate_json JSON NOT NULL,
 media_json JSON NOT NULL,
 report_seq INT NULL,
 recorded_report_seq INT NOT NULL,
 report_public_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 disposition VARCHAR(32) NOT NULL,
 created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(source_namespace,source_id),
 KEY idx_sf311_ledger_report(report_seq),
 FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE SET NULL
) ENGINE=InnoDB;
CREATE TABLE sf311_source_media_ledger (
 source_namespace VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 source_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 position TINYINT UNSIGNED NOT NULL,
 original_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 display_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 media_json JSON NOT NULL,
 PRIMARY KEY(source_namespace,source_id,position),
 KEY idx_sf311_original_sha(original_sha256),
 KEY idx_sf311_display_sha(display_sha256),
 FOREIGN KEY(source_namespace,source_id)
 REFERENCES sf311_source_case_ledger(source_namespace,source_id)
) ENGINE=InnoDB;"""


class Hold(ValueError):
    """A source must remain unapplied until its explicit conflict is reviewed."""


def digest(value):
    return hashlib.sha256(value if isinstance(value, bytes) else canonical(value).encode()).hexdigest()


def file_sha(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def timestamp(value):
    if not isinstance(value, str):
        raise Hold("missing_source_timestamp")
    parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None or parsed.utcoffset() != dt.timedelta(0):
        raise Hold("source_timestamp_must_be_explicit_utc")
    return parsed


def valid_candidate(c):
    if not isinstance(c, dict) or set(c) - ALLOWED_CANDIDATE_FIELDS:
        raise Hold("unreviewed_candidate_fields")
    ident = c.get("source_id")
    if (c.get("schema") != "cleanapp-solvesf-candidate-v1"
            or c.get("source_namespace") != NAMESPACE
            or not isinstance(ident, str) or not re.fullmatch(r"[1-9][0-9]{0,31}", ident)
            or c.get("publisher") != PUBLISHER or c.get("publisher_record_id") != ident
            or c.get("source_url") != "https://solvesf.com/report/" + ident):
        raise Hold("invalid_canonical_city_case_identity")
    material = {k: v for k, v in c.items() if k != "candidate_sha256"}
    if digest(material) != c.get("candidate_sha256"):
        raise Hold("candidate_digest_mismatch")
    raw = c.get("source_record")
    if not isinstance(raw, dict) or set(raw) - ALLOWED_SOURCE_FIELDS:
        raise Hold("source_record_not_allowlisted")
    if (raw.get("service_request_id") != ident
            or raw.get("description_authorship") != "unverified_may_be_ai"
            or raw.get("submitter_origin") != "unknown"
            or raw.get("resolution_verified", raw.get("resolved_verified")) is not False):
        raise Hold("source_provenance_mismatch")
    observed = timestamp(c.get("requested_at"))
    if raw.get("requested_at") is not None:
        original_time = dt.datetime.fromisoformat(raw["requested_at"].replace("Z", "+00:00"))
        if original_time.tzinfo is None or original_time != observed:
            raise Hold("original_and_projection_timestamps_disagree")
    if c.get("updated_at") is not None:
        timestamp(c["updated_at"])
    for original_key, projected_key in (("requested_datetime_raw", "requested_at"),
                                        ("updated_datetime_raw", "updated_at")):
        if raw.get(original_key) is not None:
            value = raw[original_key]
            if not isinstance(value, str):
                raise Hold("original_timestamp_must_be_retained_text")
            original = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
            if original.tzinfo is None or original != timestamp(c.get(projected_key)):
                raise Hold("original_and_projection_timestamps_disagree")
    for name, low, high in (("latitude", -90, 90), ("longitude", -180, 180)):
        value = c.get(name)
        if not isinstance(value, str):
            raise Hold("coordinates_must_preserve_decimal_strings")
        number = decimal.Decimal(value)
        if not number.is_finite() or not low <= number <= high:
            raise Hold("invalid_source_coordinate")
    if decimal.Decimal(c["latitude"]) == decimal.Decimal(c["longitude"]) == 0:
        raise Hold("zero_source_coordinate")
    if not isinstance(c.get("text"), str) or not 0 < len(c["text"]) <= 32768:
        raise Hold("missing_or_oversized_source_text")
    urls = c.get("media_urls")
    if not isinstance(urls, list) or len(urls) > 4 or len(set(urls)) != len(urls):
        raise Hold("invalid_source_media_list")
    from urllib.parse import urlsplit
    for url in urls:
        p = urlsplit(url)
        if (p.scheme != "https" or p.hostname != "spot-sf-res.cloudinary.com"
                or p.username or p.password or p.port not in (None, 443) or p.fragment):
            raise Hold("unreviewed_source_media_origin")
    return c


def public_id(c):
    raw = hashlib.sha256((NAMESPACE + "/case/" + c["source_id"]).encode()).digest()[:16]
    return "rpt_" + base64.urlsafe_b64encode(raw).decode().rstrip("=")


def read_candidates(path, expected_sha):
    if file_sha(path) != expected_sha:
        raise Hold("candidate_file_digest_mismatch")
    rows, identities = [], set()
    with Path(path).open("rb") as stream:
        for line in stream:
            if len(line) > 256 * 1024 or len(rows) >= MAX_BATCH:
                raise Hold("first_batch_is_bounded_to_five")
            c = valid_candidate(_helpers.json_loads(line))
            if c["source_id"] in identities:
                raise Hold("duplicate_case_in_batch")
            identities.add(c["source_id"])
            rows.append(c)
    if not rows:
        raise Hold("empty_candidate_batch")
    return rows


def feed_text(c):
    """Original issue wording, without an acquisition-label prefix."""
    text = c["text"].strip()
    text = re.sub(r"^(?:from\s+solvesf(?:\.com)?|source:\s*solvesf(?:\.com)?)(?=\s|[:–—-]|$)\s*[:–—-]?\s*",
                  "", text, count=1, flags=re.IGNORECASE)
    if not text.strip():
        source = c["source_record"]
        text = next((value for value in (source.get("service_name"), source.get("category"))
                     if isinstance(value, str) and value.strip()), "SF311 case " + c["source_id"])
    return " ".join(text.split())


def report_title(c):
    """Extract a bounded title; never invent a classification or call a model."""
    title = re.split(r"(?<=[.!?])\s+", feed_text(c), maxsplit=1)[0]
    if len(title) > 180:
        title = title[:179].rsplit(" ", 1)[0].rstrip() + "…"
    return title


def presentation(c):
    status = str(c["source_record"].get("status", "not supplied"))
    text = ("source: SolveSF public feed (SF311 case " + c["source_id"] + ")\n"
            + "Original city status: " + status + "; this is not a verified resolution.\n"
            + "Description authorship is unverified and may be AI generated.\n"
            + c["text"] + "\nSource: " + c["source_url"])
    safe = html.escape(text, quote=True)
    compact = html.escape(feed_text(c), quote=True)[:255]
    return compact, safe


def raw_description(c):
    """Retain the original report compact; feed text lives in analysis.summary."""
    return html.escape("source: SolveSF.com\n" + c["text"], quote=True)[:255]


def presentation_variants(c):
    """Only complete legacy or curated display tuples may survive replay.

    Raw report text, source identity and source JSON remain unchanged. Only the
    analysis metadata changes; the old tuple stays replayable for safe rollback.
    Tuple order: reports.description, analysis.title, analysis.summary, source.
    """
    compact, _ = presentation(c)
    legacy = raw_description(c)
    return ((legacy, report_title(c), compact, SOURCE),
            (legacy, LEGACY_TITLE, legacy, PUBLISHER))


def plan(rows):
    return {"mode": "plan", "apply_ready": False, "database_connected": False,
            "model_calls": 0, "queue_publications": 0,
            "source_namespace": NAMESPACE,
            "rows": [{"source_id": c["source_id"], "public_id": public_id(c),
                      "candidate_sha256": c["candidate_sha256"],
                      "source_media_count": len(c["media_urls"])} for c in rows],
            "required_runtime_proofs": ["root_reviewed_current_guard_and_actual_mysql_tests",
                "same_olm_cursor_after_quiescence", "fresh_complete_indexed_media_privacy_coverage",
                "in_transaction_mutation_and_bounded_suffix_fence", "private_original_and_display_head",
                "additive_case_ledger_and_exact_schema", "current_source_privacy_and_material_digest"]}


def insert(table, values):
    allowed = {"reports", "report_extra_images", "report_analysis", "report_raw",
               "external_ingest_index", "report_side_effect_policy", "sf311_source_case_ledger", "sf311_source_media_ledger"}
    if table not in allowed or any(not re.fullmatch(r"[a-z_][a-z0-9_]*", key) for key in values):
        raise ValueError("Unreviewed SQL target")
    return "INSERT INTO " + table + " (" + ",".join(values) + ") VALUES (" + ",".join(values.values()) + ")"


def validate_media(c, media, data):
    """Actual S3 HEAD/body proofs belong to the fixed guard, not input booleans."""
    if len(media) != len(c["media_urls"]) or len(data) != len(media):
        raise Hold("source_photo_coverage_incomplete")
    for i, (m, body) in enumerate(zip(media, data)):
        if m.get("source_url") != c["media_urls"][i]:
            raise Hold("source_photo_binding_changed")
        for kind in ("original", "display"):
            ref = m.get(kind, {})
            if (not re.fullmatch(r"[0-9a-f]{64}", ref.get("sha256", ""))
                    or type(ref.get("bytes")) is not int or ref["bytes"] <= 0
                    or not ref.get("bucket") or "version_id" not in ref
                    or not (ref["version_id"] is None or (isinstance(ref["version_id"], str) and ref["version_id"] not in ("", "null")))
                    or not str(ref.get("key", "")).startswith("import/solvesf/")
                    or any(p in ("", ".", "..") for p in ref["key"].split("/"))):
                raise Hold("invalid_private_archived_media_reference")
        d = m["display"]
        if (d["bytes"] > MAX_DISPLAY or len(body) != d["bytes"]
                or hashlib.sha256(body).hexdigest() != d["sha256"]
                or d.get("content_type") != "image/jpeg"
                or d.get("metadata_removed") is not True
                or any(type(d.get(k)) is not int or not 0 < d[k] <= 1600 for k in ("width", "height"))):
            raise Hold("invalid_derived_native_display")


def write_one(db, guard, c, media, data):
    """One case transaction; guard is trusted in-process code, never a CLI hook."""
    valid_candidate(c)
    validate_media(c, media, data)
    material_sha = digest({"candidate_sha256": c["candidate_sha256"], "media": media})
    key = "source_namespace=" + sql_string(NAMESPACE) + " AND source_id=" + sql_string(c["source_id"])
    db.query("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ; START TRANSACTION")
    try:
        prior = db.query("SELECT disposition,COALESCE(report_seq,0),recorded_report_seq,payload_sha256,material_sha256,report_public_id FROM sf311_source_case_ledger WHERE " + key + " FOR UPDATE")
        if prior:
            state, seq, recorded, payload, material, pid = prior[0].split("\t")
            if len(prior) != 1 or state != "inserted" or seq == "0" or seq != recorded:
                raise Hold("deleted_or_withheld_case_no_resurrection")
            if payload != c["candidate_sha256"] or material != material_sha or pid != public_id(c):
                raise Hold("changed_case_material_requires_review")
            guard.verify_replay(db, int(seq), c, media)
            db.query("ROLLBACK")
            return {"source_id": c["source_id"], "action": "replay_skip", "seq": int(seq)}
        for table, condition in (
            ("report_raw", "fetcher_id=" + sql_string(NAMESPACE) + " AND source_id=" + sql_string(c["source_id"])),
            ("external_ingest_index", "source=" + sql_string(NAMESPACE) + " AND external_id=" + sql_string(c["source_id"])),
            ("reports", "public_id=" + sql_string(public_id(c))),
        ):
            if db.query("SELECT 1 FROM " + table + " WHERE " + condition + " LIMIT 1 FOR UPDATE"):
                raise Hold("unowned_existing_city_case_requires_reconciliation")
        # Must close the actual concurrent old-row/privacy and insertion-tail
        # race. Cross-source photo hits are held, never merged just by pixels.
        guard.preinsert(db, c, media)
        compact, full = presentation(c)
        observed = timestamp(c["requested_at"]).replace(tzinfo=None).isoformat(sep=" ", timespec="seconds")
        primary = data[0] if data else b""
        reporter = "historical:sf311:" + hashlib.sha256(c["source_id"].encode()).hexdigest()
        db.query(insert("reports", {"public_id": sql_string(public_id(c)), "ts": sql_string(observed),
            "id": sql_string(reporter), "team": "0", "latitude": sql_num(c["latitude"]),
            "longitude": sql_num(c["longitude"]), "x": "0", "y": "0", "image": sql_bytes(primary),
            "action_id": "NULL", "description": sql_string(raw_description(c))}))
        seq = int(db.scalar("SELECT LAST_INSERT_ID()"))
        for position, body in enumerate(data[1:], 1):
            db.query(insert("report_extra_images", {"seq": str(seq), "position": str(position),
                "image": sql_bytes(body), "sha256": sql_string(hashlib.sha256(body).hexdigest())}))
        db.query(insert("report_side_effect_policy", {"report_seq": str(seq), "source_namespace": sql_string(NAMESPACE),
            "reason": sql_string("Historical SolveSF public feed; quiet native import"),
            "policy_version": "1", **{flag: "1" for flag in FLAGS}}))
        db.query(insert("report_raw", {"report_seq": str(seq), "fetcher_id": sql_string(NAMESPACE),
            "source_id": sql_string(c["source_id"]), "agent_id": sql_string("cleanapp-historical-import"),
            "agent_version": sql_string("solvesf-native-v1"), "collected_at": "UTC_TIMESTAMP()",
            "source_type": sql_string("historical_import"), "visibility": sql_string("public"),
            "trust_level": sql_string("unverified"), "spam_score": "0"}))
        db.query(insert("external_ingest_index", {"source": sql_string(NAMESPACE), "external_id": sql_string(c["source_id"]),
            "seq": str(seq), "source_timestamp": sql_string(observed), "source_url": sql_string(c["source_url"])}))
        db.query(insert("report_analysis", {"seq": str(seq), "source": sql_string(SOURCE),
            "title": sql_string(report_title(c)), "description": sql_string(full),
            "summary": sql_string(compact), "severity_level": "0", "language": sql_string("en"),
            "brand_name": sql_string(""), "brand_display_name": sql_string(""), "is_valid": "1",
            "classification": sql_string("physical"), "needs_ai_review": "0", "inferred_contact_emails": sql_string("[]")}))
        db.query(insert("sf311_source_case_ledger", {"source_namespace": sql_string(NAMESPACE),
            "source_id": sql_string(c["source_id"]), "payload_sha256": sql_string(c["candidate_sha256"]),
            "material_sha256": sql_string(material_sha), "publisher": sql_string(PUBLISHER),
            "source_url": sql_string(c["source_url"]), "source_record_json": sql_string(canonical(c["source_record"])),
            "candidate_json": sql_string(canonical(c)), "media_json": sql_string(canonical(media)),
            "report_seq": str(seq), "recorded_report_seq": str(seq), "report_public_id": sql_string(public_id(c)),
            "disposition": sql_string("inserted")}))
        for position, ref in enumerate(media):
            db.query(insert("sf311_source_media_ledger", {"source_namespace": sql_string(NAMESPACE),
                "source_id": sql_string(c["source_id"]), "position": str(position),
                "original_sha256": sql_string(ref["original"]["sha256"]),
                "display_sha256": sql_string(ref["display"]["sha256"]),
                "media_json": sql_string(canonical(ref))}))
        guard.verify_replay(db, seq, c, media)
        db.query("COMMIT")
        guard.record_insert(seq, c, media)
        return {"source_id": c["source_id"], "action": "inserted", "seq": seq}
    except BaseException:
        with contextlib.suppress(Exception):
            db.query("ROLLBACK")
        raise


def trusted_file(path):
    path = Path(path)
    for parent in (path, *path.parents):
        s = parent.lstat()
        if stat.S_ISLNK(s.st_mode) or s.st_uid != 0 or s.st_mode & 0o022:
            raise Hold("runtime_evidence_or_code_not_root_trusted")
        if parent == RUNTIME:
            break
    return path


def apply_batch(rows, candidate_sha, media_path, media_sha):
    if os.geteuid() != 0:
        raise Hold("production_apply_requires_root_operator")
    _helpers.require_aws_linux()
    controls = _helpers.json_loads(trusted_file(CONTROL_PATH).read_bytes())
    if (controls.get("kind") != "cleanapp_solvesf_apply_controls.v1"
            or controls.get("candidate_sha256") != candidate_sha
            or controls.get("media_manifest_sha256") != media_sha
            or controls.get("server_uuid") != SERVER_UUID
            or controls.get("apply_source_sha256") != file_sha(__file__)
            or controls.get("sql_helper_sha256") != file_sha(HELPERS)
            or timestamp(controls["expires_at"]) <= dt.datetime.now(dt.timezone.utc)):
        raise Hold("root_controls_do_not_bind_current_inputs_runtime_and_deadline")
    guard_path = trusted_file(GUARD_PATH)
    if file_sha(guard_path) != controls.get("guard_source_sha256"):
        raise Hold("reviewed_native_guard_code_mismatch")
    if file_sha(media_path) != media_sha:
        raise Hold("media_manifest_digest_mismatch")
    media = _helpers.json_loads(Path(media_path).read_bytes())
    spec = importlib.util.spec_from_file_location("solvesf_native_guard", guard_path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    results = []
    # Guard owns actual EC2/server/schema/backup/test receipts, deployment lock,
    # unchanged stopped OLM cursor, shared native advisory lock, fresh indexed
    # immutable coverage and reviewed mutation protocol. It must not trust
    # producer booleans. No arbitrary endpoint or code path is accepted by CLI.
    with module.production_guard(controls) as guard:
        if guard.db.scalar("SELECT @@server_uuid") != SERVER_UUID:
            raise Hold("wrong_actual_server")
        for c in rows:
            refs, bodies = guard.verified_media(c, media)
            results.append(write_one(guard.db, guard, c, refs, bodies))
    return {"mode": "apply", "rows": results, "queue_publications": 0, "model_calls": 0}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidates", required=True)
    parser.add_argument("--candidate-sha256", required=True)
    parser.add_argument("--mode", choices=("plan", "apply"), default="plan")
    parser.add_argument("--media-manifest")
    parser.add_argument("--media-manifest-sha256")
    args = parser.parse_args(argv)
    rows = read_candidates(args.candidates, args.candidate_sha256)
    if args.mode == "apply":
        if not args.media_manifest or not args.media_manifest_sha256:
            raise Hold("actual_private_media_manifest_required")
        result = apply_batch(rows, args.candidate_sha256, args.media_manifest, args.media_manifest_sha256)
    else:
        result = plan(rows)
    print(canonical(result))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # No candidate text, URLs, SQL, credentials or raw transport errors.
        # Each case commits atomically. An interrupted batch may have earlier
        # committed cases, whose durable ledger must be checked before retry.
        print(canonical({"completed": False, "check_case_ledger_before_retry": True,
                         "error_type": type(error).__name__}), file=sys.stderr)
        raise SystemExit(1)
