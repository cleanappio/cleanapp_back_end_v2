#!/usr/bin/env python3
import argparse
import json
import os
import uuid
import urllib.error
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
    if isinstance(obj, dict) and "description" in obj:
        return [obj]
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


def wrap_public_submission(item: Dict[str, Any]) -> Dict[str, Any]:
    allowed = {"source_id", "title", "description", "desc", "lat", "lng", "url", "agent_name"}
    unsupported = set(item) - allowed
    if unsupported:
        raise SystemExit("public single-report intake does not support these fields: " + ", ".join(sorted(unsupported)) + "; use approved Wire credentials for rich/bulk input")
    description = str(item.get("description") or item.get("desc") or "").strip()
    if not description or len(description.encode("utf-8")) > 8192:
        raise SystemExit("description is required and must be at most 8192 UTF-8 bytes")
    source_id = str(item.get("source_id") or uuid.uuid4()).strip().lower()
    try:
        parsed = uuid.UUID(source_id)
        if parsed.version != 4 or str(parsed) != source_id:
            raise ValueError()
    except ValueError:
        raise SystemExit("source_id must be a private random UUIDv4; retain it and reuse only for the same report retry")
    payload: Dict[str, Any] = {"description": description, "source_id": source_id, "agent_name": str(item.get("agent_name") or "OpenClaw CleanApp Ingest")}
    if item.get("title"):
        if len(str(item["title"]).encode("utf-8")) > 255:
            raise SystemExit("title must be at most 255 UTF-8 bytes")
        payload["title"] = item["title"]
    if (item.get("lat") is None) != (item.get("lng") is None):
        raise SystemExit("lat and lng must be supplied together")
    if item.get("lat") is not None:
        payload["latitude"], payload["longitude"] = item["lat"], item["lng"]
    if item.get("url"):
        payload["url"] = item["url"]
    return payload


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def request_json(url: str, token: str, timeout_sec: int, payload: Optional[Dict[str, Any]] = None) -> Tuple[int, str]:
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8") if payload is not None else None
    req = urllib.request.Request(url, data=data, method="POST" if payload is not None else "GET")
    if payload is not None:
        req.add_header("content-type", "application/json")
    if token:
        req.add_header("authorization", f"Bearer {token}")
    req.add_header("user-agent", "cleanapp-ingest-skill/1.2 (+https://cleanapp.io/agents)")
    try:
        with urllib.request.build_opener(NoRedirect).open(req, timeout=timeout_sec) as resp:
            return int(resp.status), resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        return int(exc.code), exc.read().decode("utf-8", errors="replace")


def post_json(url: str, token: str, payload: Dict[str, Any], timeout_sec: int) -> Tuple[int, str]:
    return request_json(url, token, timeout_sec, payload)


def get_json(url: str, token: str, timeout_sec: int) -> Tuple[int, str]:
    return request_json(url, token, timeout_sec)


def save_private_receipt(path: str, body: str, replace: bool = False) -> None:
    # An explicit output file is never silently overwritten or made readable
    # by other users. Receipts contain bearer capabilities, not public links.
    destination = path + "." + str(uuid.uuid4()) + ".tmp" if replace else path
    try:
        fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as out:
            out.write(body + "\n")
        if replace:
            os.replace(destination, path)
    finally:
        if replace and os.path.exists(destination):
            os.unlink(destination)


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
    ap.add_argument("--receipt-file", help="Save private receipt JSON to a new mode-0600 file")
    ap.add_argument("--tracking-token-file", help="Read private tracking token (or receipt JSON) for public receipt lookup")
    ap.add_argument("--status-source-id", help="Fetch Wire status by source_id instead of submitting")
    ap.add_argument("--status-receipt-id", help="Fetch Wire receipt by receipt_id instead of submitting")

    args = ap.parse_args()

    token = os.environ.get("CLEANAPP_API_TOKEN", "").strip()

    if args.status_source_id and args.status_receipt_id:
        raise SystemExit("provide only one of --status-source-id or --status-receipt-id")

    if args.tracking_token_file and not args.status_receipt_id:
        raise SystemExit("--tracking-token-file requires --status-receipt-id")

    if args.status_source_id:
        if not token:
            raise SystemExit("source-id status requires approved Wire credentials; public intake uses receipt ID and private tracking token")
        url = args.base_url.rstrip("/") + "/api/v1/agent-reports/status/" + urllib.parse.quote(args.status_source_id, safe="")
        status, body = get_json(url, token, timeout_sec=args.timeout)
        print(body)
        return 0 if 200 <= status < 300 else 2

    if args.status_receipt_id:
        prefix = "/api/v1/agent-reports/receipts/"
        if args.tracking_token_file:
            with open(args.tracking_token_file, encoding="utf-8") as secret_file:
                raw = secret_file.read(8192).strip()
            token = str(json.loads(raw).get("tracking_token") or "") if raw.startswith("{") else raw
            if not token.startswith("gdt_") or len(token) > 4096:
                raise SystemExit("invalid tracking token file")
            prefix = "/api/v1/public/agent-reports/receipts/"
        elif not token:
            raise SystemExit("public receipt lookup needs --tracking-token-file; use the private token returned on submission")
        url = args.base_url.rstrip("/") + prefix + urllib.parse.quote(args.status_receipt_id, safe="")
        status, body = get_json(url, token, timeout_sec=args.timeout)
        print(body)
        return 0 if 200 <= status < 300 else 2

    items = load_items(args.input)

    if args.no_media:
        redact_media(items)
    approx_decimals = args.approx_decimals if args.approx_location else None
    apply_location_policy(items, no_location=args.no_location, approx_decimals=approx_decimals)

    if not items:
        raise SystemExit("provide at least one report")
    if not token:
        if len(items) != 1:
            raise SystemExit("public intake accepts one report; bulk input requires approved Wire credentials")
        payload = wrap_public_submission(items[0])
        url = args.base_url.rstrip("/") + "/api/v1/public/agent-reports"
    else:
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

    if not token:
        print("source_id=" + payload["source_id"] + " (keep private; retain for exact retries)", file=sys.stderr)
    if args.receipt_file:
        save_private_receipt(args.receipt_file, json.dumps({"processing_status": "unconfirmed", "request": payload}, ensure_ascii=False))
    status, body = post_json(url, token, payload, timeout_sec=args.timeout)
    if args.receipt_file:
        parsed = json.loads(body)
        if parsed.get("tracking_token"):
            try:
                save_private_receipt(args.receipt_file, body, replace=True)
            except OSError:
                print(body)
                raise SystemExit("Could not save private receipt; retain the JSON returned on stdout, and do not create a new report")
            parsed.pop("tracking_token", None)
            parsed["private_receipt_file"] = args.receipt_file
            print(json.dumps(parsed, ensure_ascii=False))
        else:
            print(body)
    else:
        print(body)
    return 0 if 200 <= status < 300 else 2


if __name__ == "__main__":
    raise SystemExit(main())
