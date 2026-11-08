#!/usr/bin/env python3
"""Offline evidence reconciliation and deterministic review-set selection.

No database, network, model, image decoder, upload or delete operation exists in
this module. JSONL adapters supply current facts and pinned saved evidence.
"""
from __future__ import annotations

import argparse
from collections import Counter, defaultdict
import copy
import datetime
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import tempfile

if __package__:
    from .text_rules import classify_existing_text
else:
    from text_rules import classify_existing_text

SHA = re.compile(r"[0-9a-f]{64}\Z")
HOLD_PRIORITY = ("privacy", "unresolved", "manual_candidate", "quality_relevance",
                 "quality", "relevance", "caption_hint", "alias", "curator")


def later_review(hold, release):
    """Unknown/naive clocks never prove review supersession."""
    try:
        prior = datetime.datetime.fromisoformat(hold["observed_at"].replace("Z", "+00:00"))
        later = datetime.datetime.fromisoformat(release["reviewed_at"].replace("Z", "+00:00"))
        return prior.tzinfo is not None and later.tzinfo is not None and later > prior
    except (KeyError, ValueError, TypeError, AttributeError):
        return False


def valid_sha(value):
    return isinstance(value, str) and SHA.fullmatch(value) is not None


def file_sha(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_config(path=None):
    return json.loads(Path(path or Path(__file__).with_name("review500.json")).read_text())


def index_holds(rows):
    """Index normalized events or typed review-mask rows without invented pairs."""
    by_seq, by_sha = defaultdict(list), defaultdict(list)
    names = {"privacy_review": "privacy", "unresolved_review": "unresolved",
             "spam_vote_candidate_review": "manual_candidate", "caption_hint_review": "caption_hint",
             "quality_or_relevance_review": "quality_relevance", "quality_review": "quality",
             "relevance_review": "relevance", "historical_alias_review": "alias",
             "historical_image_control_review": "quality_relevance", "curator_review": "curator"}
    for row in rows:
        kinds = row.get("hold_types", [row.get("kind", "unknown_evidence")])
        for kind in kinds:
            event = {"seq": row.get("seq", row.get("report_seq")),
                     "original_sha256": row.get("original_sha256"),
                     "kind": names.get(kind, kind), "reason_code": row.get("reason_code", kind),
                     "observed_at": row.get("observed_at")}
            if event["seq"] is None and not valid_sha(event["original_sha256"]):
                raise ValueError("A hold requires a report sequence or original SHA")
            if event["seq"] is not None and (isinstance(event["seq"], bool) or not str(event["seq"]).isdigit() or int(event["seq"]) <= 0):
                raise ValueError("Invalid report sequence in hold")
            event["id"] = row.get("id") or hashlib.sha256(json.dumps(event, sort_keys=True).encode()).hexdigest()
            if event["original_sha256"]:
                if not valid_sha(event["original_sha256"]):
                    raise ValueError("Invalid original hash in hold")
                by_sha[event["original_sha256"]].append(event)
            elif event["seq"] is not None:
                by_seq[str(event["seq"])].append(event)
    return by_seq, by_sha


def adapt_saved_review_pool(pool, metadata_rows, dimension_rows, snapshot_sha256,
                           hold_rows, hold_snapshot_sha256):
    """Adapter for the saved-review pool + current report/analysis JSONL format.

    File locations come from CLI arguments. Local paths are intentionally not
    copied into the normalized selection input. Fresh byte-hash verification is
    represented by the current report's original_sha256, not fabricated here.
    """
    if not valid_sha(snapshot_sha256) or not valid_sha(hold_snapshot_sha256):
        raise ValueError("Metadata and complete hold snapshot hashes are required")
    holds_by_seq, holds_by_sha = index_holds(hold_rows)
    reports, annotations = {}, defaultdict(list)
    for row in metadata_rows:
        if row.get("kind") == "report":
            if row["seq"] in reports:
                raise ValueError("Duplicate current report row")
            reports[row["seq"]] = row
        elif row.get("kind") == "analysis":
            annotations[row["seq"]].append(row)
    dimensions = {r["seq"]: r for r in dimension_rows}
    for old in pool:
        seq = old["seq"]
        report, dimension = reports.get(seq, {}), dimensions.get(seq, {})
        if dimension.get("original_sha256", dimension.get("sha256")) != old["original_sha256"]:
            dimension = {}  # Never borrow dimensions from another image version.
        quality, visible = old.get("saved_quality_proof", {}), old.get("saved_visible_review_proof", {})
        yield {
            "seq": seq, "original_sha256": old["original_sha256"],
            "current_original_sha256": report.get("original_sha256"),
            "source_snapshot_sha256": snapshot_sha256,
            "hold_snapshot_sha256": hold_snapshot_sha256, "holds_complete": True,
            "visibility": report.get("visibility"), "active_actions": report.get("active_actions"),
            "shared_holds": report.get("shared_holds"),
            "width": report.get("width") or dimension.get("old_width"),
            "height": report.get("height") or dimension.get("old_height"),
            "image_bytes": report.get("image_bytes"), "header_hex": report.get("header_hex"),
            "mime_type": "image/jpeg" if str(report.get("header_hex", "")).upper().startswith("FFD8FF") else "unknown",
            "latitude": report.get("latitude"), "longitude": report.get("longitude"),
            "source_group": old.get("source_account_group_internal"),
            "stratum": old.get("selection_stratum_from_saved_text"), "base_score": old.get("rank_score", 0),
            "dhash": report.get("dhash"), "annotations": annotations[seq],
            "report_description": report.get("report_description"),
            "quality_evidence": {"original_sha256": old["original_sha256"],
                                 "status": quality.get("quality_status"),
                                 "artifact_sha256": quality.get("feature_binding_file_sha256")},
            "visible_review": {"original_sha256": visible.get("original_sha256"),
                               "status": visible.get("status"),
                               "artifact_sha256": visible.get("source_file_sha256")},
            "holds": holds_by_seq[str(seq)] + holds_by_sha[old["original_sha256"]] + old.get("holds", []),
            "supersessions": old.get("supersessions", []),
        }


def annotation_consensus(annotations, original_report_text="", curator_notes=(),
                         allowed_sources=("ChatGPT", "Gemini", "DeepSeek", "Qwen")):
    """Nominations from distinct saved English variants; never a junk verdict.

    Translations and duplicate rows are not independent observations/model runs.
    Distinct provider/text variants still are not calibrated independent truth.
    """
    if str(original_report_text or "").strip() or curator_notes:
        return {"status": "source_purpose_requires_review", "category": None,
                "global_junk_confirmed": False, "new_model_calls": 0}
    variants = {}
    digital_purpose = False
    for row in annotations:
        if row.get("source") not in allowed_sources or str(row.get("language", "")).lower() != "en":
            continue
        fields = {key: str(row.get(key) or "") for key in ("title", "description", "summary")}
        if not any(value.strip() for value in fields.values()):
            continue
        key = (row.get("source"), *fields.values())
        variants[key] = classify_existing_text(**fields)
        text = " ".join(fields.values())
        if re.search(r"\b(?:screen|display|rendering)\b.{0,45}\b(?:fail\w*|malfunction\w*|error|defect|blackout|crash|nonresponsive)\b", text, re.I):
            digital_purpose = True
    values = list(variants.values())
    status = "missing_saved_text"
    category = None
    if values:
        categories = {r["category"] for r in values if r["decision"] == "clear"}
        if digital_purpose or any(r["blocking_evidence"] for r in values) or (categories and any(r["decision"] != "clear" for r in values)):
            status = "conflicting_saved_text"
        elif len(values) < 2:
            status = "insufficient_saved_variants"
        elif len(categories) == 1 and all(r["decision"] == "clear" for r in values):
            status, category = "nominate_quality_review", next(iter(categories))
        else:
            status = "no_consensus_for_narrow_quality_rules"
    return {"status": status, "category": category, "saved_English_variants": len(values),
            "independent_observations_or_model_accuracy_certified": False,
            "global_junk_confirmed": False, "new_model_calls": 0}


def disposition(row):
    """Resolve typed saved holds for one current original, without applying them.

    A SHA-only image hold follows aliases. A seq-only hold stays report-only.
    Flat ID/SHA arrays must be adapted as separate events, never zipped.
    Only an explicit saved review of the current source/original can retire the
    named reason. Retiring a caption hint does not clear privacy or another hold.
    """
    result = {"seq": row.get("seq"), "original_sha256": row.get("original_sha256"),
              "global_junk_confirmed": False, "privacy_or_rights_cleared": False,
              "owner_review_required_before_upload": True, "notes": [], "active_holds": []}
    original = row.get("original_sha256")
    if not valid_sha(original) or row.get("current_original_sha256") != original or not valid_sha(row.get("source_snapshot_sha256")):
        return dict(result, status="hold_binding")
    if row.get("holds_complete") is not True or not valid_sha(row.get("hold_snapshot_sha256")):
        return dict(result, status="hold_policy_snapshot")
    for hold in row.get("holds", []):
        if hold.get("original_sha256"):
            applies = hold["original_sha256"] == original
        else:
            applies = str(hold.get("seq")) == str(row.get("seq"))
        if not applies:
            continue
        retired = any(
            release.get("decision") == "retire_specific_hint"
            and release.get("supersedes_id") == hold.get("id")
            and bool(hold.get("id"))
            and release.get("kind") == hold.get("kind")
            and release.get("reason_code") == hold.get("reason_code")
            and release.get("original_sha256") == original
            and release.get("source_snapshot_sha256") == row["source_snapshot_sha256"]
            and release.get("evidence_role") in ("explicit_saved_review", "explicit_owner_review")
            and valid_sha(release.get("artifact_sha256"))
            and later_review(hold, release)
            # This specific retirement operation is intentionally narrower than
            # privacy/rights clearance. An adapter cannot use it for those axes.
            and hold.get("kind") == "caption_hint"
            for release in row.get("supersessions", [])
        )
        if not retired:
            result["active_holds"].append(hold)
    vote = row.get("manual_vote", {})
    if vote.get("verdict") == "spam":
        if vote.get("urgency") == 0:
            result["active_holds"].append({"kind": "manual_candidate", "reason_code": "explicit_zero_urgency_spam_vote"})
        elif isinstance(vote.get("urgency"), (int, float)) and vote["urgency"] > 0:
            result["notes"].append("positive_urgency_vote_is_ambiguous")
    if result["active_holds"]:
        kinds = {h.get("kind") for h in result["active_holds"]}
        kind = next((k for k in HOLD_PRIORITY if k in kinds), "unknown_evidence")
        return dict(result, status="hold_" + kind)
    if row.get("visibility") != "public" or row.get("active_actions") or row.get("shared_holds"):
        return dict(result, status="hold_current_source")
    for field, accepted in (("quality_evidence", "eligible"),
                            ("visible_review", "clear_for_private_analysis")):
        evidence = row.get(field, {})
        if evidence.get("original_sha256") != original or evidence.get("status") != accepted or not valid_sha(evidence.get("artifact_sha256")):
            return dict(result, status="hold_" + field)
    return dict(result, status="eligible_for_owner_review")


def _geography(row, cfg):
    lat, lon = row.get("latitude"), row.get("longitude")
    if (isinstance(lat, bool) or isinstance(lon, bool)
            or not isinstance(lat, (int, float)) or not isinstance(lon, (int, float))
            or not math.isfinite(lat) or not math.isfinite(lon)
            or not -90 <= lat <= 90 or not -180 <= lon <= 180 or (lat == lon == 0)):
        return None
    # Multiplication preserves the original review500 floor(latitude * 50)
    # convention at binary floating-point boundaries.
    return tuple((math.floor(lat * (1.0 / cfg[key])), math.floor(lon * (1.0 / cfg[key])))
                 for key in ("coarse_grid_degrees", "fine_grid_degrees"))


def select_review_set(candidates, config):
    """Preserve the review500 quota/score/cap ladder with explicit audit output.

    Greedy O(eligible * selected) selection is suitable for bounded candidate
    pools. Whole-database adapters should stream reconciliation and materialize
    a bounded, source-qualified candidate projection before this step.
    """
    cfg = copy.deepcopy(config)
    quotas = dict(cfg["quotas"])
    if not quotas or any(not isinstance(n, int) or isinstance(n, bool) or n < 0 for n in quotas.values()):
        raise ValueError("Quotas must be nonnegative integers")
    target = sum(quotas.values())
    if target <= 0:
        raise ValueError("Requested review-set size must be positive")
    if any(cfg[k] <= 0 for k in ("coarse_grid_degrees", "fine_grid_degrees")):
        raise ValueError("Geographic grid sizes must be positive")
    if (not cfg["source_geo_cap_ladder"] or any(len(caps) != 3 or any(not isinstance(v, int) or isinstance(v, bool) or v <= 0 for v in caps)
                                               for caps in cfg["source_geo_cap_ladder"])):
        raise ValueError("Each source/geography cap must contain three positive integers")
    privacy = re.compile(cfg["privacy_text_pattern"], re.I)
    quality = re.compile(cfg["quality_text_pattern"], re.I)
    eligible, excluded, bycat = [], Counter(), defaultdict(list)
    seen_sequences = set()
    for source in sorted(candidates, key=lambda r: r["seq"]):
        row = copy.deepcopy(source)
        seq = row.get("seq")
        if not isinstance(seq, int) or isinstance(seq, bool) or seq <= 0 or seq in seen_sequences:
            raise ValueError("Each candidate requires a unique positive integer seq")
        seen_sequences.add(seq)
        policy = disposition(row)
        # No model text can replace the required saved visible-review evidence.
        if not cfg.get("require_saved_visible_review", True):
            raise ValueError("Disabling saved visible review requires a separately reviewed profile/adapter")
        if policy["status"] != "eligible_for_owner_review":
            excluded[policy["status"]] += 1
            continue
        if row.get("visibility") != "public" or row.get("active_actions") or row.get("shared_holds"):
            excluded["current_source_hold"] += 1
            continue
        width, height, size = row.get("width"), row.get("height"), row.get("image_bytes")
        if (not all(isinstance(v, int) and not isinstance(v, bool) for v in (width, height, size))
                or min(width, height) < cfg["min_short_edge"] or max(width, height) < cfg["min_long_edge"]
                or size < cfg["min_image_bytes"]):
            excluded["technical_showcase_threshold"] += 1
            continue
        if row.get("mime_type") not in cfg["allowed_mime_types"] or not str(row.get("header_hex", "")).upper().startswith("FFD8FF"):
            excluded["unsupported_original_format_for_browser_review"] += 1
            continue
        ai = [a for a in row.get("annotations", []) if a.get("source") in cfg["allowed_annotation_sources"]
              and a.get("language") == cfg["annotation_language"] and str(a.get("description") or "").strip()]
        if not ai:
            excluded["no_current_English_AI_description"] += 1
            continue
        text = " ".join(str(a.get(k) or "") for a in ai for k in ("title", "description")) + " " + str(row.get("report_description") or "")
        if privacy.search(text):
            excluded["conservative_text_privacy_cue"] += 1
            continue
        if quality.search(text):
            excluded["current_annotation_quality_warning"] += 1
            continue
        if any(a.get("classification") in cfg["excluded_annotation_classifications"] for a in ai):
            excluded["showcase_scope"] += 1
            continue
        cells = _geography(row, cfg)
        if cells is None or not row.get("source_group"):
            excluded["missing_geography_or_source_group"] += 1
            continue
        if row.get("stratum") not in quotas:
            excluded["stratum_not_requested"] += 1
            continue
        if not isinstance(row.get("base_score"), (int, float)) or isinstance(row["base_score"], bool) or not math.isfinite(row["base_score"]):
            raise ValueError("base_score must be finite")
        if row.get("dhash") is not None and not re.fullmatch(r"[0-9a-fA-F]{16}", row["dhash"]):
            raise ValueError("dhash must be an existing saved 64-bit hexadecimal value")
        row["coarse_cell"], row["fine_cell"] = cells
        technical = cfg["technical_score"]
        row["technical_score"] = (min(technical["byte_cap"], math.log2(max(1, size / technical["byte_base"])) * technical["byte_weight"])
                                  + min(technical["edge_cap"], min(width, height) / technical["short_edge_divisor"]))
        eligible.append(row)
        bycat[row["stratum"]].append(row)
    selected, used_seqs, used_sha, saved_dhash = [], set(), set(), []
    accounts, coarse, fine, category_counts, adjustments = Counter(), Counter(), Counter(), Counter(), []

    def choices(category):
        for acap, gcap, fcap in cfg["source_geo_cap_ladder"]:
            values = []
            for row in bycat[category]:
                if (row["seq"] in used_seqs or row["original_sha256"] in used_sha
                        or accounts[row["source_group"]] >= acap or coarse[row["coarse_cell"]] >= gcap
                        or fine[row["fine_cell"]] >= fcap):
                    continue
                dhash = row.get("dhash")
                if dhash and any(cell == row["coarse_cell"] and (int(dhash, 16) ^ value).bit_count() <= cfg["saved_dhash_max_hamming_distance"]
                                 for value, cell in saved_dhash):
                    continue
                values.append(row)
            if values:
                return values
        return []

    while len(selected) < target:
        pending = [cat for cat, n in quotas.items() if category_counts[cat] < n]
        category = min(pending, key=lambda cat: (category_counts[cat] / quotas[cat], list(quotas).index(cat)))
        available = choices(category)
        if not available:
            deficit = quotas[category] - category_counts[category]
            alternatives = [cat for cat in quotas if cat != category
                            and len(bycat[cat]) > quotas[cat] + cfg["quota_redistribution_spare"]
                            and choices(cat)]
            if not alternatives:
                raise ValueError(f"Insufficient distinct source-qualified candidates: selected {len(selected)} of {target}")
            quotas[category] = category_counts[category]
            for n in range(deficit):
                quotas[alternatives[n % len(alternatives)]] += 1
            adjustments.append({"exhausted_stratum": category, "redistributed": deficit, "to": alternatives})
            continue
        penalty = cfg["diversity_penalties"]
        score = lambda r: r["base_score"] + r["technical_score"] - penalty["source_group"] * accounts[r["source_group"]] - penalty["coarse_cell"] * coarse[r["coarse_cell"]] - penalty["fine_cell"] * fine[r["fine_cell"]]
        row = max(available, key=lambda r: (score(r), -r["seq"]))
        row["score_at_selection"] = score(row)
        selected.append(row)
        used_seqs.add(row["seq"])
        used_sha.add(row["original_sha256"])
        accounts[row["source_group"]] += 1
        coarse[row["coarse_cell"]] += 1
        fine[row["fine_cell"]] += 1
        category_counts[category] += 1
        if row.get("dhash"):
            saved_dhash.append((int(row["dhash"], 16), row["coarse_cell"]))
    return {"selected": selected, "eligible": len(eligible), "excluded": dict(excluded),
            "requested_quotas": config["quotas"], "effective_quotas": quotas,
            "category_counts": dict(category_counts), "quota_adjustments": adjustments,
            "source_groups": len(accounts), "coarse_cells": len(coarse), "fine_cells": len(fine),
            "selection_type": "curated diversity showcase, not a random representative sample",
            "privacy_or_rights_certified": False, "new_vision_or_pixels": False}


def freeze_originals(selected, local_paths, output):
    """Optional explicit local byte-copy operation; no decoding or new pixels.

    Verify every input before writing any output. Rehash each copy; never replace
    different destination bytes or modify the original. Does not upload anything.
    """
    output = Path(output)
    plan = []
    for index, row in enumerate(selected, 1):
        if not isinstance(row.get("seq"), int) or isinstance(row["seq"], bool) or row["seq"] <= 0:
            raise ValueError("Each original requires a positive integer seq")
        source = Path(local_paths[row["seq"]])
        if file_sha(source) != row["original_sha256"]:
            raise ValueError(f"Original hash mismatch for seq {row['seq']}")
        name = f"CA-{index:04d}-S{row['seq']}.jpg"
        target = output / name
        if target.is_symlink():
            raise ValueError("Refusing a symlink destination")
        if target.exists() and file_sha(target) != row["original_sha256"]:
            raise ValueError("Different destination bytes already exist")
        plan.append((source, target, row))
    output.mkdir(parents=True, exist_ok=True)
    receipts = []
    for source, target, row in plan:
        if not target.exists():
            shutil.copyfile(source, target)
        if file_sha(target) != row["original_sha256"]:
            raise ValueError("Copied original hash mismatch")
        receipts.append({"seq": row["seq"], "file_name": target.name, "sha256": row["original_sha256"]})
    return receipts


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("adapt", "reconcile", "select"))
    parser.add_argument("--input", type=Path, required=True, help="Normalized offline JSONL; never a database URL")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--config", type=Path)
    parser.add_argument("--metadata", type=Path, help="adapt only: current report/analysis JSONL")
    parser.add_argument("--dimensions", type=Path, help="adapt only: saved width/height JSONL, not feature-vector dimensions")
    parser.add_argument("--holds", type=Path, help="adapt only: complete current typed hold snapshot JSONL")
    args = parser.parse_args()
    if args.input.resolve() == args.output.resolve():
        parser.error("Output must not overwrite input")
    if args.output.exists():
        parser.error("Refusing to overwrite an existing output")
    cfg = load_config(args.config)
    if args.command == "adapt" and (not args.metadata or not args.dimensions or not args.holds):
        parser.error("adapt requires --metadata, --dimensions and --holds")
    temp_path = None
    try:
        with args.input.open() as stream, tempfile.NamedTemporaryFile(mode="w", dir=args.output.parent, delete=False) as output:
            temp_path = Path(output.name)
            records = (json.loads(line) for line in stream if line.strip())
            if args.command == "adapt":
                with args.metadata.open() as metadata, args.dimensions.open() as dimensions, args.holds.open() as holds:
                    adapted = adapt_saved_review_pool(records,
                        (json.loads(line) for line in metadata if line.strip()),
                        (json.loads(line) for line in dimensions if line.strip()), file_sha(args.metadata),
                        (json.loads(line) for line in holds if line.strip()), file_sha(args.holds))
                    for row in adapted:
                        output.write(json.dumps(row, sort_keys=True) + "\n")
            elif args.command == "reconcile":
                for row in records:
                    output.write(json.dumps(disposition(row), sort_keys=True) + "\n")
            else:
                result = select_review_set(list(records), cfg)
                for row in result.pop("selected"):
                    output.write(json.dumps(row, sort_keys=True) + "\n")
        # Hard-link creation is exclusive even if another writer creates the
        # destination after preflight. It never replaces an existing output.
        os.link(temp_path, args.output)
        if args.command == "select":
            print(json.dumps(result, sort_keys=True))
    finally:
        if temp_path is not None:
            temp_path.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
