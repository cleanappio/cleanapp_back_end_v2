#!/usr/bin/env python3
"""Bounded source epoch preparation and guarded native insertion. PLAN is default.

No migrations, queue publication, upstream HTTP calls or original-image downloads.
Production apply requires actual root-owned live evidence, current server/schema/
runtime checks and complete immutable coverage. Fixture code cannot grant apply.
"""
from __future__ import annotations
import argparse
import base64
import contextlib
import datetime as dt
import decimal
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sqlite3
import unicodedata

from mysql_transport import Mysql, sql_string, sql_bytes, sql_num

NAMESPACE = "openlittermap.com"
SOURCE = "OpenLitterMap.com"
BUCKET = "cleanapp-olm-import-20261110-importbucket-qkwbjklbk2d6"
SERVER_UUID = "f2c484f6-3c5d-11ef-aace-0242ac120003"
MAX_SOURCES = 1000
MAX_DISPLAY = 131072
MAX_EPOCH_DISPLAY = 128 * 1024**2
FLAGS = ("suppress_email", "suppress_notifications", "suppress_rewards", "suppress_routing", "suppress_analysis")
MATERIAL_KEYS = ("source_namespace", "source_id", "source_feature_sha256", "source_url", "original_media",
    "display_media", "exact_source_location", "source_timestamp_raw", "source_timestamp_utc",
    "tag_catalogue_sha256", "presentation", "native_text", "side_effect_policy")


# Embedded deterministic display projection; root-bound by this epoch.py digest.
DISPLAY_SOURCE = "OLM"
LEGACY_SOURCE = "OpenLitterMap.com"
LEGACY_PREFIX = "source: OpenLitterMap.com\n"
DISPLAY_PREFIX = "source: OLM\n"
DISPLAY_FIELDS = ("reports_description", "source", "title", "description", "summary")
MAX_TITLE = 240


def _identifier(value):
    if isinstance(value, bool):
        return None
    if isinstance(value, int) and value > 0:
        return str(value)
    if isinstance(value, str) and re.fullmatch(r"[1-9][0-9]{0,15}", value):
        return value
    return None


def _label(value):
    if not isinstance(value, str) or len(value) > 1024:
        return None
    # Match the existing public projection's treatment of markup: retain
    # source-controlled data without executable markup/entity delimiters.
    value = "".join(c for c in value if not unicodedata.category(c).startswith("C"))
    value = " ".join(value.replace("_", " ").split()).strip()
    if not value:
        return None
    return value.replace("&", r"\u0026").replace("<", r"\u003c").replace(">", r"\u003e")


def _labels(summary, catalogue, section):
    result = {}
    rows = catalogue.get(section, []) if isinstance(catalogue, dict) else []
    if isinstance(rows, list):
        for row in rows:
            if not isinstance(row, dict):
                continue
            ident, label = _identifier(row.get("id")), _label(row.get("key"))
            if ident and label:
                result[ident] = label
    keys = summary.get("keys", {})
    keys = keys.get(section, {}) if isinstance(keys, dict) else {}
    if isinstance(keys, dict):
        for ident, value in keys.items():
            ident, label = _identifier(ident), _label(value)
            if ident and label:
                result[ident] = label
    return result


def _quantity(value):
    if value is None:
        return decimal.Decimal(1)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    try:
        result = decimal.Decimal(str(value))
        return result if result.is_finite() and 0 < result <= 1000000000 else None
    except decimal.InvalidOperation:
        return None


def report_title(feature, catalogue=None):
    """Name reported object types, falling back to categories then 'Litter report'."""
    properties = feature.get("properties", {}) if isinstance(feature, dict) else {}
    summary = properties.get("summary", {}) if isinstance(properties, dict) else {}
    if not isinstance(summary, dict):
        summary = {}
    tags = summary.get("tags", [])
    tags = tags if isinstance(tags, list) else []
    for section, field in (("objects", "object_id"), ("categories", "category_id")):
        labels = _labels(summary, catalogue, section)
        counts = {}
        for tag in tags:
            if not isinstance(tag, dict):
                continue
            ident, quantity = _identifier(tag.get(field)), _quantity(tag.get("quantity"))
            if ident in labels and quantity is not None:
                name = labels[ident]
                counts[name] = counts.get(name, decimal.Decimal(0)) + quantity
        if counts:
            pieces = []
            for name, quantity in sorted(counts.items(), key=lambda row: (-row[1], row[0].casefold(), row[0]))[:3]:
                number = format(quantity.normalize(), "f")
                pieces.append(name if quantity == 1 else name + " (" + number + ")")
            title = ", ".join(pieces)
            return (title[:1].upper() + title[1:])[:MAX_TITLE]
    return "Litter report"


def presentation_from_feature(feature, full_description, catalogue=None):
    """Also usable by a reviewed database backfill using retained raw metadata.

    Full factual description, time/status caveats and license attribution stay
    byte-for-byte intact. Source attribution belongs to the source field and
    detailed evidence, while the compact feed text describes the observation.
    """
    if not isinstance(full_description, str):
        raise ValueError("Expected retained OLM factual description")
    if not full_description.startswith(LEGACY_PREFIX):
        raise ValueError("Unexpected OLM attribution prefix")
    title = report_title(feature, catalogue)
    compact = title
    if len(compact) > 255:
        raise ValueError("OLM compact projection exceeds native bound")
    return {"reports_description": compact, "source": DISPLAY_SOURCE,
            "title": title, "description": full_description, "summary": compact}


def derived_presentation(candidate, catalogue=None):
    result = presentation_from_feature(json.loads(candidate["original_feature_json"]),
        candidate["native_text"]["report_analysis_description"], catalogue)
    # Preserve the raw report row exactly. Under ROW/FULL binlogging, even a
    # description-only UPDATE would duplicate its potentially large image BLOB.
    # Feed presentation belongs to report_analysis.title/source/summary.
    result["reports_description"] = candidate["native_text"]["reports_description"]
    return result


def legacy_presentation(candidate):
    return {"reports_description": candidate["native_text"]["reports_description"],
            "source": LEGACY_SOURCE, "title": candidate["presentation"]["title"],
            "description": candidate["native_text"]["report_analysis_description"],
            "summary": candidate["native_text"]["reports_description"]}


def matches_presentation(observed, expected):
    """Compare the entire coordinated display tuple, never independent fields."""
    return (set(observed) == set(DISPLAY_FIELDS)
            and all(observed[key] == expected[key] for key in DISPLAY_FIELDS))


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False)


def sha(value):
    return hashlib.sha256(value.encode() if isinstance(value, str) else value).hexdigest()


def file_sha(path):
    with Path(path).open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def utc_now():
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def read_db(path):
    db = sqlite3.connect(Path(path).resolve(strict=True).as_uri() + "?mode=ro&immutable=1", uri=True)
    db.row_factory = sqlite3.Row
    db.execute("PRAGMA query_only=ON")
    return db


def valid_candidate(c):
    ident = c.get("photo_id")
    if type(ident) is not int or ident <= 0 or c.get("source_id") != str(ident) or c.get("source_namespace") != NAMESPACE:
        raise ValueError("Invalid source identity")
    token = hashlib.sha256((NAMESPACE + "/photo/" + str(ident)).encode()).digest()[:16]
    expected = "rpt_" + base64.urlsafe_b64encode(token).decode().rstrip("=")
    # Exact actual Go IsReportID contract, cross-language tested against its
    # pinned report-listener/publicid implementation, plus deterministic identity.
    if not re.fullmatch(r"rpt_[A-Za-z0-9_-]{22}", c.get("public_id", "")) or c["public_id"] != expected:
        raise ValueError("Invalid native public ID")
    if c.get("schema_version") != "cleanapp-native-olm-staging.v1" or c.get("apply_ready") is not False:
        raise ValueError("Staged input cannot authorize production apply")
    if c.get("review_issues") or c.get("eligible_for_offline_plan") is not True:
        raise ValueError("Candidate is held for missing/invalid source or media evidence")
    if sha(canonical({key: c[key] for key in MATERIAL_KEYS})) != c.get("payload_sha256"):
        raise ValueError("Material payload digest mismatch")
    if sha(c["original_feature_json"]) != c["raw_feature_sha256"]:
        raise ValueError("Raw Feature digest mismatch")
    feature = json.loads(c["original_feature_json"])
    if feature.get("properties", {}).get("id") != ident or sha(canonical(feature)) != c["source_feature_sha256"]:
        raise ValueError("Source Feature identity/hash mismatch")
    precise = json.loads(c["original_feature_json"], parse_float=decimal.Decimal)
    if feature.get("type") != "Feature" or feature.get("geometry", {}).get("type") != "Point":
        raise ValueError("Expected original public point Feature")
    coords = precise["geometry"].get("coordinates")
    if not isinstance(coords, list) or len(coords) < 2 or any(isinstance(x, bool) for x in coords[:2]):
        raise ValueError("Invalid original point coordinates")
    if c.get("exact_source_location") != {"longitude_decimal": str(coords[0]), "latitude_decimal": str(coords[1])}:
        raise ValueError("Native coordinates differ from exact source Feature")
    properties = feature["properties"]
    if properties.get("is_public") is False or properties.get("deleted_at") or properties.get("deleted") is True:
        raise ValueError("Explicit nonpublic/deleted source cannot be republished")
    original_time = properties.get("datetime")
    if not isinstance(original_time, str):
        raise ValueError("Missing source datetime")
    parsed_time = dt.datetime.fromisoformat(original_time.replace("Z", "+00:00"))
    if parsed_time.tzinfo is None:
        raise ValueError("Source timezone must not be guessed")
    parsed_time = parsed_time.astimezone(dt.timezone.utc)
    if (c.get("source_timestamp_raw") != original_time
            or c.get("source_timestamp_utc") != parsed_time.isoformat().replace("+00:00", "Z")
            or c["native_projection"].get("ts_utc_seconds") != parsed_time.replace(microsecond=0, tzinfo=None).isoformat(sep=" ")):
        raise ValueError("Native/source observation time mismatch")
    expected_reporter = "historical:openlittermap.com:" + sha(NAMESPACE + "/" + str(ident))
    if c["native_projection"].get("reporter_id") != expected_reporter or c.get("source_url") != "https://openlittermap.com/global?photo=" + str(ident):
        raise ValueError("Source reporter/link must not impersonate a current account")
    if c.get("original_status") != {"verified": properties.get("verified"), "picked_up": properties.get("picked_up")}:
        raise ValueError("Recorded status differs from original source")
    if c.get("side_effect_policy", {}).get("policy_version") != 1 or any(c["side_effect_policy"].get(flag) is not True for flag in FLAGS):
        raise ValueError("All five quiet policy flags are mandatory")
    for text in (c["native_text"]["reports_description"], c["native_text"]["report_analysis_description"]):
        if not isinstance(text, str) or not text.startswith("source: OpenLitterMap.com\n"):
            raise ValueError("Missing source attribution prefix")
        # The fixed factual attribution contains a harmless literal ampersand;
        # all source-controlled strings are serialized without delimiters.
        if any(character in text.replace("© OpenLitterMap & Contributors.", "") for character in "<>&"):
            raise ValueError("Public display text contains unescaped markup delimiters")
    if (c["presentation"].get("description") != c["native_text"]["report_analysis_description"]
            or any(c["native_projection"].get(key) != value for key, value in c["native_text"].items())):
        raise ValueError("Native display projections disagree")
    verified = properties.get("verified")
    upstream_unverified = verified is None or (type(verified) is int and verified < 2)
    title = "Historical OpenLitterMap observation" + (" (upstream unverified)" if upstream_unverified else "")
    if c["presentation"].get("title") != title:
        raise ValueError("Native title must retain factual historical verification status")
    if len(c["native_text"]["reports_description"]) > 255:
        raise ValueError("Native compact description exceeds deployed VARCHAR255")
    if "derived" not in c["native_text"]["report_analysis_description"].lower():
        raise ValueError("Native presentation must identify the image as a derived preview")
    coords = c.get("exact_source_location") or {}
    for name, low, high in (("latitude_decimal", -90, 90), ("longitude_decimal", -180, 180)):
        number = float(coords.get(name, "nan"))
        if not math.isfinite(number) or not low <= number <= high:
            raise ValueError("Invalid exact source coordinate")
    for media in (c["original_media"], c["display_media"]):
        if (not isinstance(media, dict) or media.get("bucket") != BUCKET
                or not isinstance(media.get("key"), str) or not media["key"].startswith("import/openlittermap/")
                or "?" in media["key"] or any(part in ("", ".", "..") for part in media["key"].split("/"))
                or not re.fullmatch("[0-9a-f]{64}", media.get("sha256", ""))
                or type(media.get("bytes")) is not int or media["bytes"] <= 0):
            raise ValueError("Untrusted media bucket/key/hash/size")
    display = c["display_media"]
    if (display["bytes"] > MAX_DISPLAY or display.get("derived") is not True or display.get("content_type") != "image/jpeg"
            or display.get("projection_version") != "olm-display-jpeg-v1"):
        raise ValueError("Native BLOB must be a bounded derived JPEG")
    if any(type(display.get(name)) is not int or not 0 < display[name] <= 1600 for name in ("width", "height")):
        raise ValueError("Invalid derived dimensions")
    if not re.fullmatch("[0-9a-f]{64}", c.get("tag_catalogue_sha256") or ""):
        raise ValueError("Full source tag catalogue must be retained")
    equivalence = {"original_sha256": c["original_media"]["sha256"], "location": c["exact_source_location"],
        "source_time": c["source_timestamp_utc"], "summary": properties.get("summary"), "status": c["original_status"]}
    if c.get("observation_equivalence_sha256") != sha(canonical(equivalence)):
        raise ValueError("Observation equivalence digest differs from exact source evidence")
    return c


def media_policy(c, controls, fixture=False):
    for kind, key in (("original_media", "original_prefix"), ("display_media", "display_prefix")):
        prefix = controls.get(key)
        if not isinstance(prefix, str) or not prefix.startswith("import/openlittermap/") or not prefix.endswith("/"):
            raise ValueError("Root controls require scoped canonical media prefixes")
        if not c[kind]["key"].startswith(prefix):
            raise ValueError("Media object is outside the root-reviewed prefix")
        if not fixture and any(part.lower().startswith("canary") for part in c[kind]["key"].split("/")):
            raise ValueError("Canary media references must not create permanent production identities")


def validate_archive_reference(reference, expected_sha, fixture=False):
    if (not isinstance(reference, dict) or reference.get("bucket") != BUCKET
            or not isinstance(reference.get("key"), str) or not reference["key"].startswith("import/openlittermap/")
            or "?" in reference["key"] or any(part in ("", ".", "..") for part in reference["key"].split("/"))
            or reference.get("sha256") != expected_sha or not re.fullmatch("[0-9a-f]{64}", expected_sha or "")
            or type(reference.get("bytes")) is not int or reference["bytes"] <= 0):
        raise ValueError("Immutable metadata/catalogue archive reference is missing or outside the dedicated source archive")
    if not fixture and any(part.lower().startswith("canary") for part in reference["key"].split("/")):
        raise ValueError("Canary metadata references must not become permanent production provenance")
    return reference


def prepare(candidate_db, out_dir, coverage_receipt=None, limit=100, after_source_id=0, fixture=False):
    if not 1 <= limit <= MAX_SOURCES or after_source_id < 0:
        raise ValueError("An epoch is bounded to1..1000 source IDs")
    path = Path(candidate_db).resolve(strict=True)
    digest = file_sha(path)
    selected = []
    total = 0
    with contextlib.closing(read_db(path)) as db:
        metadata = dict(db.execute("SELECT key,value FROM staging_meta"))
        for row in db.execute("SELECT photo_id,source_id,payload_sha256,payload_json FROM candidates WHERE eligible=1 AND photo_id>? ORDER BY photo_id LIMIT ?", (after_source_id, limit)):
            c = valid_candidate(json.loads(row["payload_json"]))
            if row["photo_id"] != c["photo_id"] or row["source_id"] != c["source_id"] or row["payload_sha256"] != c["payload_sha256"]:
                raise ValueError("Candidate SQLite index/source payload identity mismatch")
            total += c["display_media"]["bytes"]
            if total > MAX_EPOCH_DISPLAY:
                raise ValueError("Epoch display byte budget exceeded")
            selected.append({"source_id": c["source_id"], "photo_id": c["photo_id"], "payload_sha256": c["payload_sha256"]})
    if file_sha(path) != digest:
        raise ValueError("Candidate artifact changed while planning")
    record = {"kind": "cleanapp_olm_bounded_epoch.v1", "scope": "isolated_mysql_fixture" if fixture else "production_epoch_plan",
        "candidate_file": str(path), "candidate_sha256": digest,
        "source_metadata_snapshot_sha256": metadata.get("metadata_db_sha256"),
        "tag_catalogue_sha256": metadata.get("tag_catalogue_sha256"),
        "coverage_receipt_sha256": file_sha(coverage_receipt) if coverage_receipt else None,
        "source_namespace": NAMESPACE, "sources": selected, "display_bytes": total,
        "maximum_sources": MAX_SOURCES, "maximum_display_bytes": MAX_EPOCH_DISPLAY,
        "production_writes": 0, "queue_publications": 0}
    record["epoch_sha256"] = sha(canonical(record))
    output = Path(out_dir)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (output / "epoch.json").open("x") as target:
        target.write(json.dumps(record, indent=2, sort_keys=True) + "\n")
    (output / "epoch.json").chmod(0o600)
    return record


def jpeg_dimensions(data):
    if not 0 < len(data) <= MAX_DISPLAY or not data.startswith(b"\xff\xd8\xff") or not data.endswith(b"\xff\xd9"):
        raise ValueError("Invalid/big/truncated derived JPEG")
    index = 2
    dimensions = None
    while index + 4 <= len(data):
        if data[index] != 255:
            raise ValueError("Invalid JPEG marker")
        while index < len(data) and data[index] == 255:
            index += 1
        marker = data[index]
        index += 1
        if marker in (0xD8, 0xD9) or 0xD0 <= marker <= 0xD7:
            continue
        length = int.from_bytes(data[index:index+2], "big")
        if length < 2 or index + length > len(data):
            raise ValueError("Invalid JPEG segment length")
        if 0xE1 <= marker <= 0xEF or marker == 0xFE:
            raise ValueError("Derived display still contains non-JFIF metadata")
        if marker == 0xC0:
            if length < 8:
                raise ValueError("Short JPEG frame")
            height = int.from_bytes(data[index+3:index+5], "big")
            width = int.from_bytes(data[index+5:index+7], "big")
            if not 0 < width <= 1600 or not 0 < height <= 1600:
                raise ValueError("JPEG dimensions exceed native bound")
            if dimensions is not None or data[index+2] != 8 or data[index+7] != 3:
                raise ValueError("Expected a single8bitRGB baseline JPEG frame")
            dimensions = width, height
        if marker == 0xDA:
            if dimensions is None:
                raise ValueError("JPEG scan precedes frame")
            cursor = index + length
            entropy_bytes = 0
            while cursor < len(data):
                if data[cursor] != 255:
                    entropy_bytes += 1
                    cursor += 1
                    continue
                if cursor + 1 >= len(data):
                    raise ValueError("Truncated JPEG entropy marker")
                following = data[cursor+1]
                if following == 0:
                    entropy_bytes += 1
                    cursor += 2
                elif 0xD0 <= following <= 0xD7:
                    cursor += 2
                elif following == 0xD9 and cursor == len(data)-2 and entropy_bytes:
                    return dimensions
                else:
                    raise ValueError("Unexpected metadata/scan marker inside baseline entropy")
            raise ValueError("JPEG entropy or final EOI missing")
        if marker not in (0xE0, 0xDB, 0xC0, 0xC4, 0xDD):
            raise ValueError("Unexpected marker outside frozen baseline JPEG policy")
        index += length
    raise ValueError("JPEG frame dimensions missing")


def media_client():
    """One trusted SDK client may serve a bounded epoch; no proofs are cached."""
    import boto3
    from botocore.config import Config
    return boto3.client("s3", region_name="us-east-2",
        config=Config(connect_timeout=10, read_timeout=30, retries={"mode": "standard", "max_attempts": 3}))


def display_bytes(c, controls, fixture=False, *, s3=None):
    media_policy(c, controls, fixture)
    # Injected by the reviewed root driver only; producer input cannot choose
    # SDK clients. Every per-source HEAD/privacy/body check still executes.
    s3 = media_client() if s3 is None else s3
    privacy = s3.get_public_access_block(Bucket=BUCKET)["PublicAccessBlockConfiguration"]
    if any(privacy.get(key) is not True for key in ("BlockPublicAcls", "IgnorePublicAcls", "BlockPublicPolicy", "RestrictPublicBuckets")):
        raise ValueError("Source archive bucket is not fully blocked from public access")
    original = c["original_media"]
    original_head = s3.head_object(Bucket=BUCKET, Key=original["key"], ChecksumMode="ENABLED")
    original_checksum = base64.b64encode(bytes.fromhex(original["sha256"])).decode()
    if (original_head.get("ContentLength") != original["bytes"] or original_head.get("ChecksumSHA256") != original_checksum
            or original_head.get("ServerSideEncryption") != "AES256"):
        raise ValueError("Actual full-size archived source HEAD differs; original body was not fetched")
    ref = c["display_media"]
    args = {"Bucket": BUCKET, "Key": ref["key"]}
    head = s3.head_object(**args, ChecksumMode="ENABLED")
    checksum = base64.b64encode(bytes.fromhex(ref["sha256"])).decode()
    if head.get("ContentLength") != ref["bytes"] or head.get("ChecksumSHA256") != checksum or head.get("ServerSideEncryption") != "AES256":
        raise ValueError("Actual S3 display HEAD/checksum/encryption differs")
    response = s3.get_object(**args)
    with contextlib.closing(response["Body"]) as body:
        data = body.read(MAX_DISPLAY + 1)
    if len(data) != ref["bytes"] or sha(data) != ref["sha256"] or jpeg_dimensions(data) != (ref["width"], ref["height"]):
        raise ValueError("Downloaded derived JPEG differs from reviewed reference")
    return data


def insert_statement(table, values):
    allowed = {"reports", "report_analysis", "report_raw", "external_ingest_index", "report_side_effect_policy",
               "olm_source_catalogues", "olm_source_import_runs", "olm_source_observation_ledger"}
    if table not in allowed or any(not re.fullmatch("[a-z_][a-z0-9_]*", key) for key in values):
        raise ValueError("Invalid fixed importer SQL target")
    return "INSERT INTO " + table + " (" + ",".join(values) + ") VALUES (" + ",".join(values.values()) + ");"



_DISPLAY_CATALOGUE_CACHE = {}


def presentation_catalogue(db, c, staged=None):
    """Read a bounded, hash-verified retained catalogue; cache immutable bytes only."""
    digest = c["tag_catalogue_sha256"]
    if not isinstance(digest, str) or not re.fullmatch("[0-9a-f]{64}", digest):
        raise ValueError("Invalid presentation catalogue digest")
    if digest in _DISPLAY_CATALOGUE_CACHE:
        return _DISPLAY_CATALOGUE_CACHE[digest]
    if staged is not None:
        row = staged.execute("SELECT raw_json FROM catalogues WHERE sha256=?", (digest,)).fetchone()
        raw = row[0] if row is not None else None
    else:
        rows = db.query("SELECT HEX(raw_catalogue_json) FROM olm_source_catalogues WHERE catalogue_sha256="
            + sql_string(digest) + " AND OCTET_LENGTH(raw_catalogue_json)<=4194304")
        raw = bytes.fromhex(rows[0]).decode("utf-8") if len(rows) == 1 else None
    if not isinstance(raw, str) or len(raw.encode()) > 4 * 1024**2 or sha(raw) != digest:
        raise ValueError("Retained presentation catalogue missing or hash mismatch")
    value = json.loads(raw)
    if not isinstance(value, dict):
        raise ValueError("Invalid retained presentation catalogue")
    if len(_DISPLAY_CATALOGUE_CACHE) >= 8:
        _DISPLAY_CATALOGUE_CACHE.pop(next(iter(_DISPLAY_CATALOGUE_CACHE)))
    _DISPLAY_CATALOGUE_CACHE[digest] = value
    return value


def ensure_run(db, run_id, candidate_sha, controls_sha, actual_evidence=None):
    values = {"run_id": sql_string(run_id), "source_namespace": sql_string(NAMESPACE),
        "candidate_artifact_sha256": sql_string(candidate_sha), "input_snapshot_sha256": sql_string(controls_sha),
        "importer_source_sha256": sql_string(file_sha(__file__)),
        "actual_controls_json": sql_string(canonical({"controls_sha256": controls_sha,
            "validated_nonsecret_evidence": actual_evidence or {"kind": "isolated_fixture_only"}})),
        "state": sql_string("applying"), "started_at": "UTC_TIMESTAMP(6)"}
    existing = db.query("SELECT candidate_artifact_sha256,input_snapshot_sha256 FROM olm_source_import_runs WHERE run_id=" + sql_string(run_id))
    if existing:
        if existing != [candidate_sha + "\t" + controls_sha]:
            raise ValueError("Import run identity conflict")
        return
    db.query(insert_statement("olm_source_import_runs", values))


def _catalogue(db, staged, c, archive_ref):
    row = staged.execute("SELECT raw_json,bytes FROM catalogues WHERE sha256=?", (c["tag_catalogue_sha256"],)).fetchone()
    if row is None or sha(row[0]) != c["tag_catalogue_sha256"] or len(row[0].encode()) != row[1]:
        raise ValueError("Complete tag catalogue bytes/hash missing")
    prior = db.query("SELECT LOWER(SHA2(raw_catalogue_json,256)) FROM olm_source_catalogues WHERE catalogue_sha256=" + sql_string(c["tag_catalogue_sha256"]))
    if prior:
        if prior != [c["tag_catalogue_sha256"]]:
            raise ValueError("Retained catalogue changed")
        return
    db.query(insert_statement("olm_source_catalogues", {"catalogue_sha256": sql_string(c["tag_catalogue_sha256"]),
        "source_namespace": sql_string(NAMESPACE), "raw_catalogue_json": sql_string(row[0]), "byte_count": str(row[1]),
        "archive_ref_and_verification_json": sql_string(canonical(archive_ref)), "acquired_at": "UTC_TIMESTAMP(6)"}))


def existing_source(db, c):
    key = "source_namespace=" + sql_string(NAMESPACE) + " AND source_id=" + sql_string(c["source_id"])
    rows = db.query("SELECT payload_sha256,COALESCE(report_seq,0),disposition,display_sha256,display_bytes,source_feature_sha256,raw_feature_sha256,HEX(original_feature_json_raw),HEX(acquisition_evidence) FROM olm_source_observation_ledger WHERE " + key + " FOR UPDATE")
    if rows:
        payload, seq, disposition, display_sha, display_size, feature_sha, raw_sha, raw_hex, acquired_hex = rows[0].split("\t")
        if payload != c["payload_sha256"]:
            raise ValueError("Changed source payload; automatic overwrite refused")
        if disposition != "inserted" or not int(seq):
            raise ValueError("Deleted/rolled-back/aliased source requires explicit reconciliation")
        stored_raw = bytes.fromhex(raw_hex).decode()
        if (display_sha != c["display_media"]["sha256"] or int(display_size) != c["display_media"]["bytes"]
                or feature_sha != c["source_feature_sha256"] or sha(stored_raw) != raw_sha
                or sha(canonical(json.loads(stored_raw))) != feature_sha):
            raise ValueError("Ledger material hashes/bytes differ from source candidate")
        flags = ",".join("p." + name for name in FLAGS)
        check = db.query("SELECT r.public_id,LOWER(SHA2(r.image,256)),OCTET_LENGTH(r.image),p.source_namespace,p.policy_version," + flags
            + " FROM reports r JOIN report_side_effect_policy p ON p.report_seq=r.seq WHERE r.seq=" + str(int(seq)) + " FOR UPDATE")
        if not check:
            raise ValueError("Source target deleted; no resurrection")
        parts = check[0].split("\t")
        if parts[0] != c["public_id"] or parts[1] != display_sha or int(parts[2]) != c["display_media"]["bytes"] or parts[3] != NAMESPACE or parts[4:] != ["1"] * 6:
            raise ValueError("Prior native report/media/quiet policy changed")
        stored_acquisition = json.loads(bytes.fromhex(acquired_hex))
        verify_written_fields(db, c, int(seq), stored_acquisition)
        return int(seq)
    for sql in (
        "SELECT seq FROM external_ingest_index WHERE source=" + sql_string(SOURCE) + " AND external_id=" + sql_string(c["source_id"]) + " FOR UPDATE",
        "SELECT report_seq FROM report_raw WHERE fetcher_id=" + sql_string(NAMESPACE) + " AND source_id=" + sql_string(c["source_id"]) + " FOR UPDATE"):
        if db.query(sql):
            raise ValueError("Existing source mapping outside OLM ledger; reconcile without overwrite")
    if db.query("SELECT seq FROM reports WHERE public_id=" + sql_string(c["public_id"]) + " FOR UPDATE"):
        raise ValueError("Native public-ID collision")
    return None


def acquisition_seconds(acquisition):
    value = dt.datetime.fromisoformat(acquisition["first_seen_at"].replace("Z", "+00:00"))
    if value.tzinfo is None:
        raise ValueError("Acquisition timezone missing")
    return value.astimezone(dt.timezone.utc).replace(microsecond=0, tzinfo=None).isoformat(sep=" ")


def verify_written_fields(db, c, seq, stored_acquisition):
    """Accept exactly legacy or v1 display, preserving all insertion-owned evidence."""
    row = json.loads(db.query("SELECT JSON_OBJECT('id',r.id,'ts',DATE_FORMAT(r.ts,'%Y-%m-%d %H:%i:%s'),"
        "'latitude',CAST(r.latitude AS DOUBLE),'longitude',CAST(r.longitude AS DOUBLE),'description',r.description) FROM reports r WHERE seq=" + str(seq))[0])
    import struct
    f32 = lambda value: struct.unpack("!f", struct.pack("!f", float(value)))[0]
    if (row["id"] != c["native_projection"]["reporter_id"] or row["ts"] != c["native_projection"]["ts_utc_seconds"]
            or abs(row["latitude"] - f32(c["exact_source_location"]["latitude_decimal"])) > 1e-12
            or abs(row["longitude"] - f32(c["exact_source_location"]["longitude_decimal"])) > 1e-12):
        raise ValueError("Native insertion-owned identity/time/GPS changed")
    raw = db.query("SELECT fetcher_id,source_id,agent_id,agent_version,DATE_FORMAT(collected_at,'%Y-%m-%d %H:%i:%s'),source_type,visibility,trust_level,spam_score FROM report_raw WHERE report_seq=" + str(seq))
    expected = [NAMESPACE, c["source_id"], "cleanapp-historical-import", "olm-native-v1", acquisition_seconds(stored_acquisition),
                "historical_import", "public", "unverified", "0"]
    if len(raw) != 1 or raw[0].split("\t") != expected:
        raise ValueError("Raw source/acquisition/publication fields changed")
    index = db.query("SELECT source,external_id,DATE_FORMAT(source_timestamp,'%Y-%m-%d %H:%i:%s'),HEX(source_url) FROM external_ingest_index WHERE seq=" + str(seq))
    if len(index) != 1 or index[0].split("\t") != [SOURCE, c["source_id"], c["native_projection"]["ts_utc_seconds"], c["source_url"].encode().hex().upper()]:
        raise ValueError("External source mapping changed")
    analyses = db.query("SELECT JSON_OBJECT('source',source,'title',title,'description',description,'summary',summary,"
        "'severity',severity_level,'language',language,'brand',brand_name,'display',brand_display_name,'valid',is_valid,"
        "'classification',classification,'review',needs_ai_review,'contacts',inferred_contact_emails,"
        "'analysis_text_null',analysis_text IS NULL,'analysis_image_null',analysis_image IS NULL) FROM report_analysis WHERE seq=" + str(seq) + " FOR UPDATE")
    fixed = {"severity": 0, "language": "en", "brand": "", "display": "", "valid": 1,
        "classification": "physical", "review": 0, "contacts": "[]", "analysis_text_null": 1, "analysis_image_null": 1}
    if len(analyses) != 1:
        raise ValueError("Analysis was curated/extended after import")
    analysis = json.loads(analyses[0])
    if (set(analysis) != set(fixed) | {"source", "title", "description", "summary"}
            or any(analysis[key] != value for key, value in fixed.items())):
        raise ValueError("Analysis was curated/extended after import")
    observed = {"reports_description": row["description"],
                **{key: analysis[key] for key in ("source", "title", "description", "summary")}}
    legacy = legacy_presentation(c)
    if matches_presentation(observed, legacy):
        return legacy
    projected = derived_presentation(c, presentation_catalogue(db, c))
    if matches_presentation(observed, projected):
        return projected
    raise ValueError("Analysis was curated/extended after import (uncoordinated presentation)")


def write_one(db, staged, c, data, run_id, catalogue_ref, *, metadata_archive_ref=None, preinsert_check=None, after_report_hook=None):
    """One atomic source transaction. Generic ONLY for explicit fixture tests or
    the production CLI after independent live controls/race/dedup verification.
    The hook is in-process fixture fault injection, never CLI/input configurable.
    """
    valid_candidate(c)
    if db.target == "production" and (preinsert_check is None or metadata_archive_ref is None):
        raise ValueError("Production requires reviewed in-transaction race/dedup checks and immutable metadata archive reference")
    if db.target == "production":
        validate_archive_reference(metadata_archive_ref, metadata_archive_ref.get("sha256"))
        validate_archive_reference(catalogue_ref, c["tag_catalogue_sha256"])
    if len(data) != c["display_media"]["bytes"] or sha(data) != c["display_media"]["sha256"] or jpeg_dimensions(data) != (c["display_media"]["width"], c["display_media"]["height"]):
        raise ValueError("Only exact bounded display bytes may enter native storage")
    db.query("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ; START TRANSACTION;")
    try:
        prior = existing_source(db, c)
        if prior:
            db.query("ROLLBACK;")
            return {"source_id": c["source_id"], "action": "replay_skip", "seq": prior}
        if preinsert_check is not None:
            preinsert_check(db, c)
        _catalogue(db, staged, c, catalogue_ref)
        public_presentation = derived_presentation(c, presentation_catalogue(db, c, staged))
        p = c["native_projection"]
        db.query(insert_statement("reports", {"public_id": sql_string(c["public_id"]), "ts": sql_string(p["ts_utc_seconds"]),
            "id": sql_string(p["reporter_id"]), "team": "0", "latitude": sql_num(c["exact_source_location"]["latitude_decimal"]),
            "longitude": sql_num(c["exact_source_location"]["longitude_decimal"]), "x": "0", "y": "0", "image": sql_bytes(data),
            "action_id": "NULL", "description": sql_string(public_presentation["reports_description"])}))
        seq = int(db.scalar("SELECT LAST_INSERT_ID()"))
        if after_report_hook is not None:
            after_report_hook(db, seq)
        db.query(insert_statement("report_side_effect_policy", {"report_seq": str(seq), "source_namespace": sql_string(NAMESPACE),
            "reason": sql_string("Historical public OLM import; no outbound or automated side effects"),
            "policy_version": "1", **{flag: "1" for flag in FLAGS}}))
        db.query(insert_statement("report_raw", {"report_seq": str(seq), "fetcher_id": sql_string(NAMESPACE),
            "source_id": sql_string(c["source_id"]), "agent_id": sql_string("cleanapp-historical-import"),
            "agent_version": sql_string("olm-native-v1"), "collected_at": sql_string(acquisition_seconds(c["acquisition"])),
            "source_type": sql_string("historical_import"), "visibility": sql_string("public"),
            "trust_level": sql_string("unverified"), "spam_score": "0"}))
        db.query(insert_statement("external_ingest_index", {"source": sql_string(SOURCE), "external_id": sql_string(c["source_id"]),
            "seq": str(seq), "source_timestamp": sql_string(p["ts_utc_seconds"]), "source_url": sql_string(c["source_url"])}))
        db.query(insert_statement("report_analysis", {"seq": str(seq), "source": sql_string(public_presentation["source"]),
            "title": sql_string(public_presentation["title"]), "description": sql_string(public_presentation["description"]),
            "summary": sql_string(public_presentation["summary"]), "severity_level": "0", "language": sql_string("en"),
            "brand_name": sql_string(""), "brand_display_name": sql_string(""), "is_valid": "1",
            "classification": sql_string("physical"), "needs_ai_review": "0", "inferred_contact_emails": sql_string("[]")}))
        original, display = c["original_media"], c["display_media"]
        values = {"source_namespace": sql_string(NAMESPACE), "source_id": sql_string(c["source_id"]), "run_id": sql_string(run_id),
            "payload_sha256": sql_string(c["payload_sha256"]), "source_feature_sha256": sql_string(c["source_feature_sha256"]),
            "raw_feature_sha256": sql_string(c["raw_feature_sha256"]), "original_feature_json_raw": sql_string(c["original_feature_json"]),
            "original_feature_archive_ref": sql_string(canonical(metadata_archive_ref or {"kind": "isolated_fixture_archive_not_production", "source_feature_sha256": c["source_feature_sha256"], "run_id": run_id})),
            "tag_catalogue_sha256": sql_string(c["tag_catalogue_sha256"]), "source_url": sql_string(c["source_url"]),
            "acquisition_evidence": sql_string(canonical(c["acquisition"])), "source_timestamp_raw": sql_string(c["source_timestamp_raw"]),
            "source_timestamp_utc": sql_string(c["source_timestamp_utc"].replace("T", " ").removesuffix("Z")),
            "exact_latitude_decimal": sql_string(c["exact_source_location"]["latitude_decimal"]),
            "exact_longitude_decimal": sql_string(c["exact_source_location"]["longitude_decimal"]),
            "original_status_json": sql_string(canonical(c["original_status"])),
            "original_bucket": sql_string(BUCKET), "original_key": sql_string(original["key"]), "original_version_id": "NULL",
            "original_sha256": sql_string(original["sha256"]), "original_bytes": str(original["bytes"]), "original_is_camera_file": "0",
            "display_bucket": sql_string(BUCKET), "display_key": sql_string(display["key"]), "display_version_id": "NULL",
            "display_sha256": sql_string(display["sha256"]), "display_bytes": str(display["bytes"]),
            "display_width": str(display["width"]), "display_height": str(display["height"]),
            "projection_version": sql_string(display["projection_version"]),
            "media_verification_evidence": sql_string(canonical(c.get("object_verification_evidence", {}))),
            "observation_equivalence_sha256": sql_string(c["observation_equivalence_sha256"]),
            "proposed_public_id": sql_string(c["public_id"]), "report_seq": str(seq), "recorded_report_seq": str(seq),
            "actual_report_public_id": sql_string(c["public_id"]), "disposition": sql_string("inserted"), "duplicate_evidence": "NULL",
            "licence_and_attribution_json": sql_string(canonical(c["license"])), "policy_version": "1",
            **{flag: "1" for flag in FLAGS}, "created_at": "UTC_TIMESTAMP(6)"}
        db.query(insert_statement("olm_source_observation_ledger", values))
        flags = ",".join("p." + flag for flag in FLAGS)
        check = db.query("SELECT LOWER(SHA2(r.image,256)),OCTET_LENGTH(r.image),p.policy_version," + flags
            + " FROM reports r JOIN report_side_effect_policy p ON p.report_seq=r.seq WHERE r.seq=" + str(seq))[0].split("\t")
        if check != [display["sha256"], str(display["bytes"])] + ["1"] * 6:
            raise ValueError("Atomic native media/policy verification failed before commit")
        db.query("COMMIT;")
        return {"source_id": c["source_id"], "action": "inserted", "seq": seq, "display_bytes": display["bytes"]}
    except BaseException:
        with contextlib.suppress(Exception):
            db.query("ROLLBACK;")
        raise


def rollback_one(db, c, run_id):
    """Narrow reversal of an unchanged, unclaimed inserted source only."""
    valid_candidate(c)
    db.query("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ; START TRANSACTION;")
    try:
        key = "source_namespace=" + sql_string(NAMESPACE) + " AND source_id=" + sql_string(c["source_id"])
        row = db.query("SELECT run_id,COALESCE(report_seq,0),disposition FROM olm_source_observation_ledger WHERE " + key + " FOR UPDATE")
        if not row or row[0].split("\t")[0] != run_id or row[0].split("\t")[2] != "inserted":
            raise ValueError("Rollback does not own this inserted source")
        seq = existing_source(db, c)
        native = db.query("SELECT id,CAST(latitude AS DOUBLE),CAST(longitude AS DOUBLE),DATE_FORMAT(ts,'%Y-%m-%d %H:%i:%s'),HEX(description) FROM reports WHERE seq=" + str(seq))[0].split("\t")
        import struct
        f32 = lambda value: struct.unpack("!f", struct.pack("!f", float(value)))[0]
        owned_source = db.scalar("SELECT source FROM report_analysis WHERE seq=" + str(seq) + " FOR UPDATE")
        owned_display = (legacy_presentation(c) if owned_source == SOURCE else
                         derived_presentation(c, presentation_catalogue(db, c)))
        if owned_source not in (SOURCE, DISPLAY_SOURCE):
            raise ValueError("Independent analysis prevents rollback")
        if (native[0] != c["native_projection"]["reporter_id"]
                or abs(float(native[1]) - f32(c["exact_source_location"]["latitude_decimal"])) > 1e-12
                or abs(float(native[2]) - f32(c["exact_source_location"]["longitude_decimal"])) > 1e-12
                or native[3] != c["native_projection"]["ts_utc_seconds"]
                or bytes.fromhex(native[4]).decode() != owned_display["reports_description"]):
            raise ValueError("Native source/time/location/text changed; rollback refused")
        if db.scalar("SELECT COUNT(*) FROM report_analysis WHERE seq=" + str(seq) + " AND source<>" + sql_string(owned_source)) != "0":
            raise ValueError("Independent analysis prevents rollback")
        for table in ("sent_reports_emails", "report_email_deliveries", "report_email_delivery_events", "email_report_retry", "mobile_push_delivery_events", "wire_reward_records",
                      "case_reports", "reports_owners", "report_push_subscriptions", "responses", "report_details", "bounding_boxes", "report_tags",
                      "report_escalation_targets", "physical_contact_lookup_state", "physical_contact_candidates", "report_sort_events", "report_sort_metrics"):
            columns = db.query("SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME="
                + sql_string(table) + " AND COLUMN_NAME IN ('seq','report_seq')")
            for column in columns:
                if db.scalar("SELECT COUNT(*) FROM `" + table + "` WHERE `" + column + "`=" + str(seq)) != "0":
                    raise ValueError("User/transport/reward/reference prevents rollback")
        for table, column, values in (("cases", "anchor_report_seq", (str(seq),)),
            ("notify_execution_tasks", "subject_ref", (str(seq), c["public_id"])),
            ("notify_outcomes", "subject_ref", (str(seq), c["public_id"]))):
            exists = db.scalar("SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME="
                + sql_string(table) + " AND COLUMN_NAME=" + sql_string(column))
            if exists != "0":
                for value in values:
                    if db.scalar("SELECT COUNT(*) FROM `" + table + "` WHERE BINARY `" + column + "`=BINARY " + sql_string(value)) != "0":
                        raise ValueError("Case anchor/queued subject prevents rollback")
        if db.scalar("SELECT COUNT(*) FROM olm_source_observation_ledger WHERE report_seq=" + str(seq)) != "1":
            raise ValueError("Other source observations reference this target")
        db.query("DELETE FROM report_analysis WHERE seq=" + str(seq) + " AND source=" + sql_string(owned_source)
            + "; DELETE FROM external_ingest_index WHERE source=" + sql_string(SOURCE) + " AND external_id="
            + sql_string(c["source_id"]) + " AND seq=" + str(seq) + "; DELETE FROM reports WHERE seq=" + str(seq)
            + "; UPDATE olm_source_observation_ledger SET disposition='rolled_back' WHERE " + key + "; COMMIT;")
        return {"source_id": c["source_id"], "deleted_inserted_seq": seq, "source_identity_retained": True}
    except BaseException:
        with contextlib.suppress(Exception):
            db.query("ROLLBACK;")
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("plan", "apply"), default="plan")
    parser.add_argument("--candidates-db", type=Path, required=True)
    parser.add_argument("--out-dir", type=Path, required=True)
    parser.add_argument("--coverage-receipt", type=Path)
    parser.add_argument("--limit", type=int, default=100)
    parser.add_argument("--after-source-id", type=int, default=0)
    parser.add_argument("--controls", type=Path)
    parser.add_argument("--guard-receipt", type=Path)
    args = parser.parse_args()
    if args.mode == "apply":
        # Production execution is intentionally unavailable until the bounded
        # race/dedup/rollback integration contract has passed isolated MySQL.
        raise SystemExit("Apply execution not enabled in this reviewed preparation revision; no production connection made")
    result = prepare(args.candidates_db, args.out_dir, args.coverage_receipt, args.limit, args.after_source_id)
    print(canonical({"epoch_sha256": result["epoch_sha256"], "source_count": len(result["sources"]),
        "display_bytes": result["display_bytes"], "production_writes": 0, "mode": "plan"}))


if __name__ == "__main__":
    main()
