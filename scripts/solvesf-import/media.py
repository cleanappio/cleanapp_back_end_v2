#!/usr/bin/env python3
"""Archive bounded SolveSF photos privately; derive separate native JPEGs.

PLAN is default. Execute only on the reviewed AWS host as root, with its managed
instance credentials and the fixed existing backup helper/bucket. No model,
database, queue, account, infrastructure, or application-service changes.
"""
from __future__ import annotations

import argparse
import base64
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import warnings

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("solvesf_apply_media", HERE / "apply.py")
apply = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = apply
spec.loader.exec_module(apply)
BUCKET = "cleanapp-production-backups-654574823950"
REGION = "us-east-2"
BACKUP_HELPER = Path("/usr/local/lib/cleanapp-backup/backup_recovery.py")
HOST = "spot-sf-res.cloudinary.com"
MAX_ORIGINAL = 20 * 1024 * 1024
MAX_PIXELS = 50_000_000
MAX_DISPLAY = 128 * 1024
PROJECTION = "solvesf-display-jpeg-v1"


class MediaError(ValueError):
    pass


class MediaHTTPError(MediaError):
    """Safe HTTP status for source-withdrawal decisions; no URL or body."""
    def __init__(self, status):
        self.status = int(status)
        super().__init__("source_http_failure")


def require(value, code):
    if not value:
        raise MediaError(code)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def valid_url(url):
    require(isinstance(url, str) and len(url) <= 4096, "invalid_media_url")
    p = urllib.parse.urlsplit(url)
    require(p.scheme == "https" and p.hostname == HOST and p.port in (None, 443)
            and not p.username and not p.password and not p.query and not p.fragment
            and p.path.startswith("/") and not any(ord(c) < 33 for c in url), "untrusted_media_url")
    return url


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Downloader:
    def __init__(self, opener=None, clock=time.monotonic, sleep=time.sleep):
        self.opener = opener or urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        self.clock, self.sleep, self.next_request = clock, sleep, 0.0

    def _rate(self):
        self.sleep(max(0.0, self.next_request - self.clock()))
        self.next_request = self.clock() + 1.0

    def fetch(self, url):
        current, redirects, failures = valid_url(url), 0, 0
        while True:
            self._rate()
            req = urllib.request.Request(current, headers={
                "User-Agent": "CleanApp-SolveSF-Archive/1.0", "Accept": "image/jpeg,image/png,image/webp"})
            try:
                with self.opener.open(req, timeout=30) as response:
                    require(response.status == 200, "unexpected_media_status")
                    valid_url(response.geturl())
                    content_type = response.headers.get_content_type()
                    require(content_type in ("image/jpeg", "image/png", "image/webp"), "nonphoto_response")
                    length = response.headers.get("Content-Length")
                    if length is not None:
                        require(0 < int(length) <= MAX_ORIGINAL, "original_size_exceeded")
                    data = response.read(MAX_ORIGINAL + 1)
                    require(0 < len(data) <= MAX_ORIGINAL, "original_size_exceeded")
                    if length is not None:
                        require(len(data) == int(length), "truncated_original")
                    return data, content_type
            except urllib.error.HTTPError as error:
                status, location = error.code, error.headers.get("Location")
                retry_after = error.headers.get("Retry-After", "1")
                error.close()
                if status in (301, 302, 303, 307, 308):
                    require(location is not None and redirects < 3, "redirect_limit")
                    current = valid_url(urllib.parse.urljoin(current, location))
                    redirects += 1
                    continue
                if status not in (429, 500, 502, 503, 504) or failures >= 2:
                    raise MediaHTTPError(status) from None
                require(retry_after.isdecimal() and 0 <= int(retry_after) <= 10, "source_backoff_requires_review")
                failures += 1
                self.sleep(max(float(retry_after), float(2 ** (failures - 1))))
            except (TimeoutError, urllib.error.URLError):
                require(failures < 2, "source_transport_failure")
                failures += 1
                self.sleep(float(2 ** (failures - 1)))


def derive(data, declared_type):
    """Original bytes are unchanged. EXIF orientation affects only the preview."""
    from PIL import Image, ImageOps
    require(0 < len(data) <= MAX_ORIGINAL, "original_size_exceeded")
    with warnings.catch_warnings():
        warnings.simplefilter("error", Image.DecompressionBombWarning)
        with Image.open(io.BytesIO(data)) as source:
            require(source.format in ("JPEG", "PNG", "WEBP"), "unsupported_photo_format")
            require({"JPEG": "image/jpeg", "PNG": "image/png", "WEBP": "image/webp"}[source.format] == declared_type,
                    "photo_content_type_mismatch")
            require(source.width * source.height <= MAX_PIXELS and getattr(source, "n_frames", 1) == 1,
                    "oversized_or_animated_photo")
            source.verify()
        with Image.open(io.BytesIO(data)) as source:
            oriented = ImageOps.exif_transpose(source)
            oriented.load()
            if "A" in oriented.getbands() or "transparency" in oriented.info:
                rgba = oriented.convert("RGBA")
                rgb = Image.new("RGB", rgba.size, "white")
                rgb.paste(rgba, mask=rgba.getchannel("A"))
            else:
                rgb = oriented.convert("RGB")
            rgb.thumbnail((1600, 1600), Image.Resampling.LANCZOS)
            # Creating a fresh image drops EXIF/XMP/ICC/comment fields rather
            # than relying on the decoder's inherited info dictionary.
            clean = Image.new("RGB", rgb.size)
            clean.paste(rgb)
            for _ in range(8):
                for quality in (90, 80, 70, 60, 50, 40):
                    output = io.BytesIO()
                    clean.save(output, format="JPEG", quality=quality, optimize=True, progressive=False)
                    preview = output.getvalue()
                    if len(preview) <= MAX_DISPLAY:
                        with Image.open(io.BytesIO(preview)) as check:
                            require(not check.getexif() and not any(k in check.info for k in ("exif", "xmp", "icc_profile", "comment")),
                                    "display_metadata_not_stripped")
                            check.verify()
                        return preview, {"width": clean.width, "height": clean.height,
                            "content_type": "image/jpeg", "metadata_removed": True, "projection_version": PROJECTION}
                clean = clean.resize((max(1, int(clean.width * .8)), max(1, int(clean.height * .8))), Image.Resampling.LANCZOS)
    raise MediaError("display_budget_unachievable")


def assert_object_private(key, version_id=None, opener=None):
    """Prove anonymous GET is denied from this host for the exact known object."""
    require(key.startswith("import/solvesf/") and not any(p in ("", ".", "..") for p in key.split("/")), "invalid_archive_key")
    url = "https://" + BUCKET + ".s3." + REGION + ".amazonaws.com/" + urllib.parse.quote(key, safe="/")
    if version_id is not None:
        url += "?" + urllib.parse.urlencode({"versionId": version_id})
    opener = opener or urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(url, headers={"Range": "bytes=0-0"})
    try:
        with opener.open(request, timeout=30) as response:
            status = response.status
    except urllib.error.HTTPError as error:
        status = error.code
        error.close()
    require(status == 403, "anonymous_object_get_not_denied")


class Archive:
    """Conditional content-addressed puts and complete hash-bound readback."""
    def __init__(self, store, private_check=assert_object_private):
        self.store, self.private_check = store, private_check

    def put(self, data, key, content_type):
        require(key.startswith("import/solvesf/") and not any(p in ("", ".", "..") for p in key.split("/")), "invalid_archive_key")
        checksum = base64.b64encode(bytes.fromhex(sha(data))).decode()
        put = None
        with tempfile.NamedTemporaryFile(prefix="solvesf-private-") as body:
            os.chmod(body.name, 0o600)
            body.write(data)
            body.flush()
            try:
                put = self.store.run_json(["s3api", "put-object", "--bucket", BUCKET, "--key", key,
                    "--body", body.name, "--content-type", content_type, "--server-side-encryption", "AES256",
                    "--checksum-sha256", checksum, "--if-none-match", "*"])
            except Exception:
                # Existing immutable content or an uncertain PUT is reconciled
                # only by independent HEAD plus complete versioned readback.
                pass
        head = self.store.run_json(["s3api", "head-object", "--bucket", BUCKET, "--key", key, "--checksum-mode", "ENABLED"])
        version = head.get("VersionId") or None
        version = None if version == "null" else version
        require((version is None or isinstance(version, str)) and head.get("ContentLength") == len(data)
                and head.get("ServerSideEncryption") == "AES256" and head.get("ChecksumSHA256") == checksum,
                "archive_head_mismatch")
        if put and put.get("VersionId") not in (None, "", "null"):
            require(put["VersionId"] == version, "archive_put_head_race")
        remote = self.store.download(key, version)
        digest, size = hashlib.sha256(), 0
        try:
            while block := remote.stream.read(1024 * 1024):
                size += len(block)
                require(size <= len(data), "archive_readback_oversize")
                digest.update(block)
            remote.finish()
        except BaseException:
            remote.abort()
            raise
        require(size == len(data) and digest.hexdigest() == sha(data), "archive_readback_mismatch")
        if version is None:
            after = self.store.run_json(["s3api", "head-object", "--bucket", BUCKET, "--key", key, "--checksum-mode", "ENABLED"])
            require(after.get("VersionId") in (None, "", "null") and after.get("ContentLength") == len(data)
                    and after.get("ChecksumSHA256") == checksum and after.get("ServerSideEncryption") == "AES256",
                    "unversioned_archive_readback_race")
        self.private_check(key, version)
        return {"bucket": BUCKET, "key": key, "version_id": version, "sha256": sha(data), "bytes": size}


def prepare(rows, candidate_file_sha256, downloader, archive, projector=derive):
    require(0 < len(rows) <= 5, "canary_case_limit")
    require(re.fullmatch(r"[0-9a-f]{64}", candidate_file_sha256 or ""), "invalid_candidate_file_digest")
    require(len({c.get("source_id") for c in rows}) == len(rows), "duplicate_case_in_batch")
    for c in rows:
        apply.valid_candidate(c)
        require(len(c["media_urls"]) <= 4, "whole_case_photo_limit")
        for url in c["media_urls"]:
            valid_url(url)
    cases = {}
    for c in rows:
        items = []
        for url in c["media_urls"]:
            original, declared = downloader.fetch(url)
            preview, details = projector(original, declared)
            require(len(original) <= MAX_ORIGINAL and len(preview) <= MAX_DISPLAY, "media_budget_exceeded")
            original_ref = archive.put(original, "import/solvesf/originals/sha256/" + sha(original), declared)
            display_ref = archive.put(preview, "import/solvesf/previews/" + PROJECTION + "/sha256/" + sha(preview) + ".jpg", "image/jpeg")
            items.append({"source_url": url, "original": {**original_ref, "source_asset_as_published": True,
                "camera_original_verified": False}, "display": {**display_ref, **details}})
        snapshot = apply.canonical(c).encode()
        prefix = "import/solvesf/cases/" + c["source_id"] + "/" + c["candidate_sha256"]
        source_ref = archive.put(snapshot, prefix + "/source.json", "application/json")
        binding = {"source_namespace": "sf311", "source_id": c["source_id"],
            "candidate_sha256": c["candidate_sha256"], "source_record_ref": source_ref, "items": items}
        binding_ref = archive.put(apply.canonical(binding).encode(), prefix + "/media.json", "application/json")
        cases[c["source_id"]] = {**binding, "binding_ref": binding_ref}
    return {"schema": "cleanapp-solvesf-media-v1", "candidate_file_sha256": candidate_file_sha256, "cases": cases}


def production_archive():
    require(os.geteuid() == 0, "root_operator_required")
    apply._helpers.require_aws_linux()
    for path in (BACKUP_HELPER, *BACKUP_HELPER.parents):
        metadata = path.lstat()
        require(not stat.S_ISLNK(metadata.st_mode) and metadata.st_uid == 0 and not metadata.st_mode & 0o022,
                "untrusted_backup_helper")
    spec = importlib.util.spec_from_file_location("solvesf_backup", BACKUP_HELPER)
    helper = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = helper
    spec.loader.exec_module(helper)
    return Archive(helper.S3(BUCKET, REGION))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidates", required=True)
    parser.add_argument("--candidate-sha256", required=True)
    parser.add_argument("--output")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    rows = apply.read_candidates(args.candidates, args.candidate_sha256)
    if not args.execute:
        print(apply.canonical({"mode": "plan", "cases": len(rows), "photos": sum(len(c["media_urls"]) for c in rows),
                              "download_requests": 0, "archive_writes": 0}))
        return
    require(args.output is not None, "private_output_path_required")
    output = Path(args.output)
    require(not output.exists() and not output.is_symlink(), "output_already_exists")
    manifest = prepare(rows, args.candidate_sha256, Downloader(), production_archive())
    with output.open("x", encoding="utf-8") as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(apply.canonical(manifest) + "\n")
        stream.flush()
        os.fsync(stream.fileno())
    print(apply.canonical({"mode": "execute", "cases": len(rows), "photos": sum(len(c["media_urls"]) for c in rows),
                          "manifest_sha256": apply.file_sha(output), "full_readback_verified": True}))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"completed": False, "error_type": type(error).__name__}), file=sys.stderr)
        raise SystemExit(1)
