#!/usr/bin/env python3
"""Fresh anonymous HTTP contract fixture. Never prints receipt capabilities."""
import argparse
import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

parser = argparse.ArgumentParser()
parser.add_argument("--base-url", default="http://localhost:18082")
parser.add_argument("--state-file", required=True)
parser.add_argument("--phase", choices=["first", "atomic-fail", "atomic-recover", "queue-fail", "queue-recover", "expired"], required=True)
args = parser.parse_args()
base = args.base_url.rstrip("/")
path = "/api/v1/public/agent-reports"
state = {}
if os.path.exists(args.state_file):
    with open(args.state_file, encoding="utf-8") as source:
        state = json.load(source)


def request(method, route, body=None, token=None, headers=None):
    # This harness is explicitly a disposable HTTP fixture. No discovery,
    # external calls, API keys, admin approval, or registration is involved.
    url = urllib.parse.urljoin(base + "/", route)
    assert urllib.parse.urlsplit(url).netloc == urllib.parse.urlsplit(base).netloc
    if method == "POST":
        time.sleep(2.05)  # stay below the separate request-abuse limiter
    req = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    for key, value in (headers or {}).items():
        req.add_header(key, value)
    try:
        response = urllib.request.urlopen(req, timeout=45)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, json.loads(response.read()), response.headers


def accepted(body, status=200):
    code, result, headers = request("POST", path, body)
    assert code == status, ("unexpected submit status", code, list(result))
    assert headers.get("Cache-Control") == "no-store"
    assert result["publication_status"] == "private" and result["status"] == "quarantined"
    assert isinstance(result["idempotency_replay"], bool)
    assert result["source_id"] == body["source_id"]
    assert result["tracking_token"].startswith("gdt_")
    assert result["status_url"].startswith(path + "/receipts/")
    assert not any(key in result for key in ["report_id", "description", "public_id", "canonical_path"])
    return result


if args.phase == "first":
    body = {"description": "Synthetic CI fixture: checkout button does not respond.", "source_id": str(uuid.uuid4())}
    first = accepted(body)
    assert first["idempotency_replay"] is False
    code, tracked, headers = request("GET", first["status_url"], token=first["tracking_token"])
    assert code == 200 and tracked["publication_status"] == "private" and tracked["processing_status"] == "received"
    assert headers.get("Cache-Control") == "no-store" and "tracking_token" not in tracked
    for route, token in [(first["status_url"], None), (first["status_url"], "gdt_wrong"), (first["status_url"] + "?tracking_token=" + first["tracking_token"], None)]:
        assert request("GET", route, token=token)[0] == 401
    # Source identity is independent of transport identity and timestamps.
    code, replay, _ = request("POST", path, body, headers={"User-Agent": "another-client", "X-Forwarded-For": "203.0.113.19"})
    assert code == 200 and replay["receipt_id"] == first["receipt_id"] and replay["idempotency_replay"] is True
    assert replay["tracking_expires_at"] == first["tracking_expires_at"]
    assert request("POST", path, {**body, "description": "Different payload"})[0] == 409
    assert request("POST", path, {"description": "Problem", "source_id": "test123"})[0] == 400
    assert request("POST", path, {"description": "Problem", "tier": 3})[0] == 400
    other = accepted({**body, "source_id": str(uuid.uuid4())})
    assert other["receipt_id"] != first["receipt_id"]
    # Equal descriptions without an explicit intent never share private receipts.
    a = request("POST", path, {"description": "Synthetic identical description"})[1]
    b = request("POST", path, {"description": "Synthetic identical description"})[1]
    assert a["receipt_id"] != b["receipt_id"] and a["source_id"] != b["source_id"]
    state.update(first_body=body, first_receipt=first)
elif args.phase == "atomic-fail":
    state["atomic_body"] = {"description": "Synthetic receipt persistence failure fixture", "source_id": str(uuid.uuid4())}
    code, result, _ = request("POST", path, state["atomic_body"])
    assert code >= 500 and "tracking_token" not in result and "receipt_id" not in result
elif args.phase == "atomic-recover":
    state["atomic_receipt"] = accepted(state["atomic_body"])
elif args.phase == "queue-fail":
    state["queue_body"] = {"description": "Synthetic broker outage fixture", "source_id": str(uuid.uuid4())}
    receipt = accepted(state["queue_body"], status=503)
    assert receipt["processing_status"] == "pending_retry"
    code, tracked, _ = request("GET", receipt["status_url"], token=receipt["tracking_token"])
    assert code == 200 and tracked["processing_status"] == "pending_retry" and tracked["publication_status"] == "private"
    state["queue_receipt"] = receipt
elif args.phase == "queue-recover":
    receipt = accepted(state["queue_body"])
    assert receipt["receipt_id"] == state["queue_receipt"]["receipt_id"] and receipt["idempotency_replay"] is True
    assert receipt["tracking_expires_at"] == state["queue_receipt"]["tracking_expires_at"]
    code, tracked, _ = request("GET", receipt["status_url"], token=receipt["tracking_token"])
    assert code == 200 and tracked["processing_status"] == "received"
elif args.phase == "expired":
    assert request("POST", path, state["first_body"])[0] == 410
    first = state["first_receipt"]
    assert request("GET", first["status_url"], token=first["tracking_token"])[0] == 401

fd = os.open(args.state_file, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w", encoding="utf-8") as target:
    json.dump(state, target)
print("public anonymous fixture passed:", args.phase)
