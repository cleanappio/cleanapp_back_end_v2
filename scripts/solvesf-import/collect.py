#!/usr/bin/env python3
"""Finite, unauthenticated SolveSF public-feed collection; never publishes reports.

The citywide three-day window is an overlapping discovery snapshot, not complete SF311
history. SQLite pins the first candidate per city case; later content versions
are held for review. Unchanged discovery records need no detail request.
"""
import argparse
import datetime as dt
import decimal
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import sqlite3
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

FEED_URL = "https://solvesf.com/api/live-reports?window=3d"
WINDOWS = ("3d", "24h", "today")
FEED_URLS = tuple("https://solvesf.com/api/live-reports?window=" + window for window in WINDOWS)
PHOTO_HOST = "spot-sf-res.cloudinary.com"
SCHEMA = "cleanapp-solvesf-candidate-v1"
MAX_BODY = 6 * 1024 * 1024
MAX_DETAIL = 256 * 1024
_request_lock = threading.Lock()
_last_request_at = None


class CollectError(Exception):
    pass


class PublicHTTPError(CollectError):
    def __init__(self, status):
        self.status = status
        super().__init__("public API HTTP " + str(status) + "; no retry")


def require(condition, message):
    if not condition:
        raise CollectError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":"))


def digest(value):
    return hashlib.sha256(canonical(value).encode("utf-8")).hexdigest()


def case_id(value):
    require(isinstance(value, str) and re.fullmatch(r"[1-9][0-9]{0,31}", value), "invalid case ID")
    return value


def timestamp(value):
    require(isinstance(value, str) and re.search(r"(?:Z|[+-]\d\d:\d\d)$", value), "timestamp lacks explicit timezone")
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
        require(parsed.tzinfo is not None, "timestamp lacks timezone")
        return parsed.astimezone(dt.timezone.utc).isoformat().replace("+00:00", "Z")
    except ValueError:
        raise CollectError("invalid timestamp") from None


def coordinate(value, low, high):
    require(not isinstance(value, bool) and isinstance(value, (str, int, decimal.Decimal)), "invalid coordinate type")
    try:
        number = decimal.Decimal(value)
        require(number.is_finite() and low <= number <= high, "invalid coordinate range")
        return format(number, "f")
    except decimal.InvalidOperation:
        raise CollectError("invalid coordinate") from None


def photo_url(value):
    require(isinstance(value, str) and len(value) <= 4096, "invalid media URL")
    try:
        parsed = urllib.parse.urlsplit(value)
        require(parsed.scheme == "https" and parsed.hostname == PHOTO_HOST
                and not parsed.username and not parsed.password and parsed.port in (None, 443)
                and not parsed.query and not parsed.fragment and parsed.path.startswith("/"), "unreviewed media URL")
    except ValueError:
        raise CollectError("invalid media URL") from None
    return value


def text(value, maximum=32768):
    require(isinstance(value, str) and len(value) <= maximum
            and not any(ord(char) < 32 and char not in "\t\r\n" for char in value), "invalid public text")
    return value


def public_record(record):
    require(isinstance(record, dict), "invalid source record")
    require(not any(record.get(key) for key in ("deleted", "deleted_at", "nonpublic", "is_deleted"))
            and record.get("is_public") is not False and record.get("public") is not False
            and str(record.get("visibility", "")).lower() not in ("private", "nonpublic", "non-public", "restricted", "hidden"),
            "source record is explicitly private or deleted")


def normalize(ident, record):
    ident = case_id(ident)
    public_record(record)
    require(isinstance(record, dict) and record.get("service_request_id") == ident, "detail case ID mismatch")
    requested = timestamp(record.get("requested_datetime"))
    updated = timestamp(record["updated_datetime"]) if record.get("updated_datetime") else None
    latitude = coordinate(record.get("lat"), -90, 90)
    longitude = coordinate(record.get("long"), -180, 180)
    require(not (decimal.Decimal(latitude) == decimal.Decimal(longitude) == 0), "zero source coordinate")
    description = text(record.get("description") or "")
    photos = []
    for name in ("opened_with_photos", "photos"):
        values = record.get(name) or []
        require(isinstance(values, list) and len(values) <= 64, "invalid source photos")
        photos.extend(photo_url(value) for value in values)
    if record.get("media_url"):
        photos.append(photo_url(record["media_url"]))
    photos = list(dict.fromkeys(photos))
    source = record.get("source") if record.get("source") in ("open311", "socrata") else "unknown"
    raw = {"service_request_id": ident, "source": source,
           "source_attribution": "SolveSF public feed; city API provenance does not identify the submitting app",
           "submitter_origin": "unknown", "description_authorship": "unverified_may_be_ai",
           "resolution_verified": False, "requested_at": requested, "updated_at": updated,
           "requested_datetime_raw": record["requested_datetime"],
           "updated_datetime_raw": record.get("updated_datetime"),
           "description": description, "photos": photos}
    for name in ("status", "status_notes", "service_name", "service_code", "address", "category"):
        if record.get(name) is not None:
            raw[name] = text(record[name])
    candidate = {"schema": SCHEMA, "source_namespace": "sf311", "source_id": ident,
                 "publisher": "SolveSF", "publisher_record_id": ident,
                 "source_url": "https://solvesf.com/report/" + ident,
                 "requested_at": requested, "updated_at": updated,
                 "latitude": latitude, "longitude": longitude, "text": description,
                 "media_urls": photos, "source_record": raw}
    candidate["candidate_sha256"] = digest(candidate)
    return candidate


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


def throttle():
    """Space all default client request starts by at least one second."""
    global _last_request_at
    with _request_lock:
        if _last_request_at is not None:
            delay = 1.0 - (time.monotonic() - _last_request_at)
            if delay > 0:
                time.sleep(delay)
        _last_request_at = time.monotonic()


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON object key")
        result[key] = value
    return result


def public_get(url, byte_limit=MAX_BODY):
    parsed = urllib.parse.urlsplit(url)
    require(url in FEED_URLS or (parsed.scheme == "https" and parsed.netloc == "solvesf.com"
            and re.fullmatch(r"/api/report/[1-9][0-9]{0,31}", parsed.path)
            and not parsed.query and not parsed.fragment), "unreviewed API URL")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(url, headers={"Accept": "application/json", "User-Agent": "CleanApp-SolveSF-public-feed/1.0"})
    try:
        throttle()
        with opener.open(request, timeout=30) as response:
            require(response.headers.get("Content-Type", "").split(";", 1)[0].lower() == "application/json", "non-JSON API response")
            length = response.headers.get("Content-Length")
            if length is not None:
                require(length.isdigit() and int(length) <= byte_limit, "oversized or invalid Content-Length")
            body = response.read(byte_limit + 1)
            require(len(body) <= byte_limit, "API response exceeds byte limit")
            require(length is None or int(length) == len(body), "truncated API response")
        return json.loads(body.decode("utf-8"), parse_float=decimal.Decimal,
                          object_pairs_hook=unique_object,
                          parse_constant=lambda value: (_ for _ in ()).throw(CollectError("non-finite JSON number")))
    except urllib.error.HTTPError as error:
        status = error.code
        error.close()
        raise PublicHTTPError(status) from None
    except (urllib.error.URLError, TimeoutError, OSError, http.client.HTTPException):
        raise CollectError("public API network failure; diagnostics suppressed") from None
    except (ValueError, UnicodeError):
        raise CollectError("invalid API JSON") from None


def discovery(snapshot, check_public=True):
    require(isinstance(snapshot, dict) and isinstance(snapshot.get("reports"), list), "invalid feed snapshot")
    records = snapshot["reports"]
    require(len(records) <= 10000, "feed exceeds discovery bound")
    require(not any(snapshot.get(name) for name in ("truncated", "hasMore", "nextCursor", "refreshing")), "incomplete feed snapshot")
    for name in ("total", "totalCount"):
        require(name not in snapshot or snapshot[name] == len(records), "feed count indicates truncation")
    found = {}
    for record in records:
        if check_public:
            public_record(record)
        require(isinstance(record, dict), "invalid feed record")
        ident = case_id(record.get("id"))
        require(ident not in found, "duplicate feed case ID")
        retained = {key: record[key] for key in ("id", "title", "description", "photoUrl", "photoUrls",
                    "lat", "lng", "address", "requestedDatetime", "status", "cityResponse", "category", "serviceCode") if key in record}
        # Decimal coordinates remain exact, even in the discovery fingerprint.
        encoded = json.dumps(retained, sort_keys=True, ensure_ascii=False, separators=(",", ":"), default=str)
        found[ident] = hashlib.sha256(encoded.encode()).hexdigest()
    return found


def atomic_write(path, body):
    descriptor, name = tempfile.mkstemp(prefix="." + path.name, dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(body)
            output.flush()
            os.fsync(output.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def run(state_dir, max_reports=100, fetch=None, window="3d"):
    require(isinstance(max_reports, int) and 1 <= max_reports <= 1000, "max-reports must be between 1 and 1000")
    require(window in WINDOWS, "unsupported citywide feed window")
    feed_url = "https://solvesf.com/api/live-reports?window=" + window
    state_dir = Path(state_dir)
    state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    fetch = fetch or (lambda url: public_get(url, MAX_BODY if url == feed_url else MAX_DETAIL))
    with (state_dir / ".collector.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise CollectError("another collector owns this state directory") from None
        connection = sqlite3.connect(state_dir / "ledger.sqlite3")
        try:
            connection.executescript("CREATE TABLE IF NOT EXISTS discoveries (id TEXT PRIMARY KEY, feed_digest TEXT NOT NULL);"
                "CREATE TABLE IF NOT EXISTS versions (id TEXT NOT NULL, digest TEXT NOT NULL, candidate TEXT NOT NULL,"
                " disposition TEXT NOT NULL, first_seen TEXT NOT NULL, last_seen TEXT NOT NULL, PRIMARY KEY(id,digest));"
                "CREATE TABLE IF NOT EXISTS record_holds (id TEXT PRIMARY KEY, reason TEXT NOT NULL, first_seen TEXT NOT NULL, last_seen TEXT NOT NULL);")
            def hold(ident, reason):
                now = dt.datetime.now(dt.timezone.utc).isoformat()
                with connection:
                    connection.execute("INSERT INTO record_holds VALUES (?,?,?,?) ON CONFLICT(id) DO UPDATE SET last_seen=excluded.last_seen", (ident, reason, now, now))
            snapshot = fetch(feed_url)
            discovered = discovery(snapshot, check_public=False)
            # Privacy flags matter even when the publication fingerprint is unchanged.
            for record in snapshot["reports"]:
                try:
                    public_record(record)
                except CollectError:
                    hold(record["id"], "nonpublic_or_deleted")
            held_ids = {row[0] for row in connection.execute("SELECT id FROM record_holds")}
            observed_at = dt.datetime.now(dt.timezone.utc).isoformat()
            with connection:
                connection.executemany("UPDATE record_holds SET last_seen=? WHERE id=?",
                    ((observed_at, ident) for ident in held_ids.intersection(discovered)))
            seen = dict(connection.execute("SELECT id,feed_digest FROM discoveries"))
            pending = [(ident, fingerprint) for ident, fingerprint in discovered.items() if ident not in held_ids and seen.get(ident) != fingerprint]
            pending.sort(key=lambda row: (len(row[0]), row[0]))
            details_fetched = 0
            for ident, fingerprint in pending[:max_reports]:
                try:
                    record = fetch("https://solvesf.com/api/report/" + ident)
                except PublicHTTPError as error:
                    if error.status != 404:
                        raise
                    hold(ident, "detail_not_found")
                    continue
                details_fetched += 1
                try:
                    candidate = normalize(ident, record)
                except CollectError:
                    hold(ident, "invalid_public_record")
                    continue
                now = dt.datetime.now(dt.timezone.utc).isoformat()
                existing = connection.execute("SELECT 1 FROM versions WHERE id=?", (ident,)).fetchone()
                disposition = "held_revision" if existing else "candidate"
                if not existing and len(candidate["media_urls"]) > 4:
                    disposition = "held_media_count"
                if not existing and not candidate["text"].strip():
                    disposition = "held_missing_public_text"
                with connection:
                    connection.execute("INSERT INTO versions VALUES (?,?,?,?,?,?) ON CONFLICT(id,digest) DO UPDATE SET last_seen=excluded.last_seen",
                        (ident, candidate["candidate_sha256"], canonical(candidate), disposition, now, now))
                    connection.execute("INSERT INTO discoveries VALUES (?,?) ON CONFLICT(id) DO UPDATE SET feed_digest=excluded.feed_digest", (ident, fingerprint))
            candidates, held = [], []
            held_ids = {row[0] for row in connection.execute("SELECT id FROM record_holds")}
            revised_ids = {row[0] for row in connection.execute("SELECT DISTINCT id FROM versions WHERE disposition='held_revision'")}
            revision_count = 0
            for ident, body, disposition in connection.execute("SELECT id,candidate,disposition FROM versions ORDER BY length(id),id,first_seen,digest"):
                if disposition == "candidate":
                    if ident not in revised_ids and ident not in held_ids:
                        candidates.append(body)
                else:
                    held.append(canonical({"disposition": disposition, "candidate": json.loads(body)}))
                    revision_count += disposition == "held_revision"
            candidate_bytes = ("\n".join(candidates) + ("\n" if candidates else "")).encode()
            held_bytes = ("\n".join(held) + ("\n" if held else "")).encode()
            stats = {"schema": "cleanapp-solvesf-collection-v1", "feed_url": feed_url,
                     "window": window, "max_reports": max_reports,
                     "whole_history_complete": False, "publication_performed": False,
                     "discovered_count": len(discovered), "details_fetched": details_fetched,
                     "unchanged_discoveries": sum(ident not in held_ids and seen.get(ident) == fingerprint
                         for ident, fingerprint in discovered.items()),
                     "pending_discoveries": max(0, len(pending) - max_reports),
                     "candidate_count": len(candidates), "held_count": len(held),
                     "held_record_count": len(held_ids),
                     "held_record_reasons": dict(connection.execute("SELECT reason,count(*) FROM record_holds GROUP BY reason")),
                     "held_revision_count": revision_count,
                     "candidates_sha256": hashlib.sha256(candidate_bytes).hexdigest(),
                     "held_sha256": hashlib.sha256(held_bytes).hexdigest()}
            atomic_write(state_dir / "candidates.jsonl", candidate_bytes)
            atomic_write(state_dir / "held-revisions.jsonl", held_bytes)
            # Manifest is replaced last; readers must check its file digests.
            atomic_write(state_dir / "stats.json", (canonical(stats) + "\n").encode())
            return stats
        finally:
            connection.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state-dir", type=Path, required=True)
    parser.add_argument("--max-reports", type=int, default=100)
    parser.add_argument("--window", choices=WINDOWS, default="3d")
    args = parser.parse_args()
    try:
        print(canonical(run(args.state_dir, args.max_reports, window=args.window)))
        return 0
    except (CollectError, sqlite3.Error, OSError):
        print(canonical({"success": False, "publication_performed": False,
                         "error": "collection failed; verify snapshot manifest before use; resume from ledger after review"}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
