#!/usr/bin/env python3
import argparse
import json
import os
import sys
import urllib.request
import urllib.parse
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def approx_coord(x: float, decimals: int) -> float:
    return round(x, decimals)


def load_items(path: Optional[str]) -> List[Dict[str, Any]]:
    raw = ""
    if path:
        with open(path, "r", encoding="utf-8") as f:
            raw = f.read()
    else:
        raw = sys.stdin.read()

    raw = raw.strip()
    if not raw:
        raise SystemExit("input is empty (provide --input or pipe JSON to stdin)")

    obj = json.loads(raw)
    if isinstance(obj, dict) and isinstance(obj.get("items"), list):
        return list(obj["items"])
    if isinstance(obj, list):
        return list(obj)
    raise SystemExit("input must be a JSON array of items, or an object with an 'items' array")


def redact_media(items: List[Dict[str, Any]]) -> None:
    for it in items:
        it.pop("media", None)


def apply_location_policy(items: List[Dict[str, Any]], *, no_location: bool, approx_decimals: Optional[int]) -> None:
    for it in items:
        if no_location:
            it.pop("lat", None)
            it.pop("lng", None)
            continue
        if approx_decimals is not None:
            if "lat" in it and isinstance(it["lat"], (int, float)):
                it["lat"] = approx_coord(float(it["lat"]), approx_decimals)
            if "lng" in it and isinstance(it["lng"], (int, float)):
                it["lng"] = approx_coord(float(it["lng"]), approx_decimals)


def is_wire_submission(item: Dict[str, Any]) -> bool:
    return isinstance(item.get("schema_version"), str) and isinstance(item.get("agent"), dict) and isinstance(item.get("report"), dict)


def wrap_wire_submission(item: Dict[str, Any]) -> Dict[str, Any]:
    if is_wire_submission(item):
        return item

    source_id = str(item.get("source_id") or item.get("sourceId") or "").strip()
    title = str(item.get("title") or "").strip()
    description = str(item.get("description") or item.get("desc") or "").strip()
    collected_at = str(item.get("collected_at") or item.get("collectedAt") or "").strip()
    source_type = str(item.get("source_type") or item.get("sourceType") or "text").strip() or "text"
    lat = item.get("lat")
    lng = item.get("lng")
    has_location = isinstance(lat, (int, float)) and isinstance(lng, (int, float))

    evidence_bundle = []
    for idx, media in enumerate(item.get("media") or []):
        if not isinstance(media, dict):
            continue
        evidence_bundle.append({
            "evidence_id": f"{source_id or 'wire'}_ev_{idx+1}",
            "type": "media",
            "uri": media.get("url"),
            "sha256": media.get("sha256"),
            "mime_type": media.get("content_type") or media.get("contentType"),
            "captured_at": collected_at or utc_now_iso(),
        })

    if has_location:
        domain = "physical"
        problem_type = "physical_issue"
        location = {
            "kind": "coordinate",
            "lat": float(lat),
            "lng": float(lng),
            "place_confidence": 0.7,
        }
        digital_context = None
    else:
        domain = "digital"
        problem_type = "digital_issue" if source_type in ("web", "text") else "general_issue"
        location = None
        digital_context = {"submitted_via": "openclaw_skill", "source_type": source_type}

    return {
        "schema_version": "cleanapp-wire.v1",
        "source_id": source_id,
        "submitted_at": utc_now_iso(),
        "observed_at": collected_at or None,
        "agent": {
            "agent_id": "openclaw-cleanapp-ingest",
            "agent_name": "OpenClaw CleanApp Ingest Skill",
            "agent_type": "agent",
            "operator_type": "openclaw",
            "auth_method": "api_key",
            "software_version": "1.1.0",
            "execution_mode": "skill",
        },
        "provenance": {
            "generation_method": "openclaw_skill",
            "chain_of_custody": ["openclaw", "cleanapp_ingest_skill"],
        },
        "report": {
            "domain": domain,
            "problem_type": problem_type,
            "title": title,
            "description": description,
            "confidence": 0.7,
            "location": location,
            "digital_context": digital_context,
            "evidence_bundle": evidence_bundle or None,
        },
        "delivery": {"requested_lane": "auto"},
    }


def post_json(url: str, token: str, payload: Dict[str, Any], timeout_sec: int) -> Tuple[int, str]:
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("content-type", "application/json")
    req.add_header("authorization", f"Bearer {token}")
    req.add_header("user-agent", "cleanapp-ingest-skill/1.0")
    with urllib.request.urlopen(req, timeout=timeout_sec) as resp:
        body = resp.read().decode("utf-8", errors="replace")
        return int(resp.status), body


def get_json(url: str, token: str, timeout_sec: int) -> Tuple[int, str]:
    req = urllib.request.Request(url, method="GET")
    req.add_header("authorization", f"Bearer {token}")
    req.add_header("user-agent", "cleanapp-ingest-skill/1.1")
    with urllib.request.urlopen(req, timeout=timeout_sec) as resp:
        body = resp.read().decode("utf-8", errors="replace")
        return int(resp.status), body


def main() -> int:
    ap = argparse.ArgumentParser(description="Submit or inspect reports through CleanApp Wire.")
    ap.add_argument("--base-url", default=os.environ.get("CLEANAPP_BASE_URL", "https://live.cleanapp.io"),
                    help="Base URL for CleanApp report-listener (default: https://live.cleanapp.io)")
    ap.add_argument("--input", help="Path to JSON file (or omit to read stdin)")
    ap.add_argument("--dry-run", action="store_true", help="Print payload and exit without sending")
    ap.add_argument("--no-media", action="store_true", help="Drop 'media' entries (recommended default)")
    ap.add_argument("--no-location", action="store_true", help="Remove lat/lng entirely")
    ap.add_argument("--approx-location", action="store_true", help="Round lat/lng to reduce precision (recommended default)")
    ap.add_argument("--approx-decimals", type=int, default=2, help="Decimals for --approx-location (default: 2)")
    ap.add_argument("--timeout", type=int, default=20, help="HTTP timeout seconds (default: 20)")
    ap.add_argument("--status-source-id", help="Fetch Wire status by source_id instead of submitting")
    ap.add_argument("--status-receipt-id", help="Fetch Wire receipt by receipt_id instead of submitting")

    args = ap.parse_args()

    token = os.environ.get("CLEANAPP_API_TOKEN", "").strip()
    if not token:
        raise SystemExit("missing required secret env CLEANAPP_API_TOKEN")

    if args.status_source_id and args.status_receipt_id:
        raise SystemExit("provide only one of --status-source-id or --status-receipt-id")

    if args.status_source_id:
        url = args.base_url.rstrip("/") + "/api/v1/agent-reports/status/" + urllib.parse.quote(args.status_source_id, safe="")
        status, body = get_json(url, token, timeout_sec=args.timeout)
        print(body)
        return 0 if status < 400 else 2

    if args.status_receipt_id:
        url = args.base_url.rstrip("/") + "/api/v1/agent-reports/receipts/" + urllib.parse.quote(args.status_receipt_id, safe="")
        status, body = get_json(url, token, timeout_sec=args.timeout)
        print(body)
        return 0 if status < 400 else 2

    items = load_items(args.input)

    if args.no_media:
        redact_media(items)
    approx_decimals = args.approx_decimals if args.approx_location else None
    apply_location_policy(items, no_location=args.no_location, approx_decimals=approx_decimals)

    # Ensure required source_id exists for each item.
    missing = [i for i, it in enumerate(items) if not str(it.get("source_id", "")).strip()]
    if missing:
        raise SystemExit(f"missing source_id for items at indexes: {missing}")

    wire_items = [wrap_wire_submission(it) for it in items]
    is_batch = len(wire_items) > 1
    payload: Dict[str, Any]
    if is_batch:
        payload = {"items": wire_items}
        url = args.base_url.rstrip("/") + "/api/v1/agent-reports:batchSubmit"
    else:
        payload = wire_items[0]
        url = args.base_url.rstrip("/") + "/api/v1/agent-reports:submit"

    if args.dry_run:
        out = {
            "url": url,
            "ts": utc_now_iso(),
            "payload": payload,
        }
        print(json.dumps(out, indent=2, ensure_ascii=False))
        return 0

    status, body = post_json(url, token, payload, timeout_sec=args.timeout)
    print(body)
    if status >= 400:
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
