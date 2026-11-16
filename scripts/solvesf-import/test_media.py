"""Synthetic network/S3 tests; Pillow-only image tests run when installed."""
import base64
import copy
from email.message import Message
import importlib.util
import io
import json
from pathlib import Path
import sys
import unittest
import urllib.error

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("solvesf_media_test", HERE / "media.py")
media = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = media
spec.loader.exec_module(media)
spec = importlib.util.spec_from_file_location("solvesf_apply_test_fixtures", HERE / "test_apply.py")
fixtures = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixtures)
URL = "https://spot-sf-res.cloudinary.com/photo.jpg"


def headers(**values):
    result = Message()
    for key, value in values.items():
        result[key.replace("_", "-")] = value
    return result


class Response(io.BytesIO):
    status = 200
    def __init__(self, data, content_type="image/jpeg", length=None):
        super().__init__(data)
        self.headers = headers(Content_Type=content_type)
        if length is not None:
            self.headers["Content-Length"] = str(length)

    def geturl(self):
        return URL


class Clock:
    def __init__(self):
        self.value = 0.0

    def now(self):
        return self.value

    def sleep(self, seconds):
        self.value += seconds


class Opener:
    def __init__(self, results, clock):
        self.results, self.clock, self.calls = iter(results), clock, []

    def open(self, req, timeout):
        self.calls.append((self.clock.now(), req))
        result = next(self.results)
        if isinstance(result, Exception):
            raise result
        return result


class Download(io.BytesIO):
    @property
    def stream(self):
        return self

    def finish(self):
        self.close()

    def abort(self):
        self.close()


class Store:
    def __init__(self):
        self.objects, self.puts, self.downloads = {}, 0, []
        self.corrupt = False
        self.version = "version-1"
        self.race_after_read = False

    def run_json(self, args):
        op = args[1]
        assert op in ("put-object", "head-object"), "Bucket management permissions must not be required"
        key = args[args.index("--key") + 1]
        if op == "put-object":
            self.puts += 1
            assert args[args.index("--if-none-match") + 1] == "*"
            assert args[args.index("--server-side-encryption") + 1] == "AES256"
            if key in self.objects:
                raise RuntimeError("conditional existing object")
            self.objects[key] = Path(args[args.index("--body") + 1]).read_bytes()
            return {"VersionId": self.version}
        assert op == "head-object"
        data = self.objects[key]
        return {"VersionId": self.version, "ContentLength": len(data), "ServerSideEncryption": "AES256",
                "ChecksumSHA256": base64.b64encode(bytes.fromhex(media.sha(data))).decode()}

    def download(self, key, version):
        self.downloads.append((key, version))
        body = b"corrupt" if self.corrupt else self.objects[key]
        if self.race_after_read:
            self.objects[key] = b"raced"
        return Download(body)


class NetworkAndArchive(unittest.TestCase):
    def downloader(self, results):
        clock = Clock()
        opener = Opener(results, clock)
        return media.Downloader(opener, clock.now, clock.sleep), opener

    def test_no_auth_rate_and_bounded_retry(self):
        retry = urllib.error.HTTPError(URL, 429, "rate limit", headers(Retry_After="2"), io.BytesIO(b"private diagnostics"))
        downloader, opener = self.downloader([retry, Response(b"photo"), Response(b"photo")])
        self.assertEqual(downloader.fetch(URL), (b"photo", "image/jpeg"))
        downloader.fetch(URL)
        self.assertTrue(all(b[0] - a[0] >= 1 for a, b in zip(opener.calls, opener.calls[1:])))
        for _, request in opener.calls:
            sent = {key.lower() for key in request.headers}
            self.assertFalse(sent.intersection({"authorization", "cookie", "proxy-authorization"}))
        errors = [urllib.error.HTTPError(URL, 503, "unavailable", headers(), io.BytesIO()) for _ in range(3)]
        downloader, opener = self.downloader(errors)
        with self.assertRaises(media.MediaError):
            downloader.fetch(URL)
        self.assertEqual(len(opener.calls), 3)

    def test_real_downloader_preserves_safe_http_status_without_url_or_body(self):
        for status in (404, 403):
            error = urllib.error.HTTPError(URL, status, "raw private message", headers(), io.BytesIO(b"raw private body"))
            downloader, opener = self.downloader([error])
            with self.assertRaises(media.MediaHTTPError) as raised:
                downloader.fetch(URL)
            self.assertEqual(raised.exception.status, status)
            self.assertEqual(str(raised.exception), "source_http_failure")
            self.assertEqual(len(opener.calls), 1)
        failures = [urllib.error.HTTPError(URL, 429, "rate limited", headers(), io.BytesIO()) for _ in range(3)]
        downloader, opener = self.downloader(failures)
        with self.assertRaises(media.MediaHTTPError) as raised:
            downloader.fetch(URL)
        self.assertEqual(raised.exception.status, 429)
        self.assertEqual(len(opener.calls), 3)

    def test_url_redirect_html_and_size_fail_closed(self):
        for url in ("http://spot-sf-res.cloudinary.com/x", URL + "?token=secret", "https://user:pass@spot-sf-res.cloudinary.com/x", "https://example.com/x"):
            with self.assertRaises(media.MediaError):
                media.valid_url(url)
        redirect = urllib.error.HTTPError(URL, 302, "redirect", headers(Location="http://169.254.169.254/latest/meta-data"), io.BytesIO())
        for response in (redirect, Response(b"<html>oops</html>", "text/html"), Response(b"x", length=media.MAX_ORIGINAL + 1), Response(b"short", length=100)):
            downloader, opener = self.downloader([response])
            with self.assertRaises(media.MediaError):
                downloader.fetch(URL)
            self.assertEqual(len(opener.calls), 1)

    def test_immutable_versioned_full_readback_and_corruption(self):
        store = Store()
        archive = media.Archive(store, private_check=lambda key, version: None)
        key = "import/solvesf/originals/sha256/" + media.sha(b"photo")
        first = archive.put(b"photo", key, "image/jpeg")
        self.assertEqual(first, archive.put(b"photo", key, "image/jpeg"))
        self.assertEqual(len(store.objects), 1)
        self.assertEqual(store.downloads, [(key, "version-1")] * 2)
        store.corrupt = True
        with self.assertRaises(media.MediaError):
            archive.put(b"photo", key, "image/jpeg")

    def test_default_privacy_checker_requires_unsigned_403(self):
        key = "import/solvesf/originals/sha256/" + "a" * 64
        denied = urllib.error.HTTPError(URL, 403, "denied", headers(), io.BytesIO())
        opener = Opener([denied], Clock())
        media.assert_object_private(key, "pinned-version", opener=opener)
        request = opener.calls[0][1]
        self.assertEqual(request.get_header("Range"), "bytes=0-0")
        self.assertEqual(request.host, media.BUCKET + ".s3.us-east-2.amazonaws.com")
        self.assertNotIn("Authorization", request.headers)
        self.assertNotIn("Cookie", request.headers)
        for status in (200, 206):
            response = Response(b"x")
            response.status = status
            with self.assertRaises(media.MediaError):
                media.assert_object_private(key, None, opener=Opener([response], Clock()))

    def test_unversioned_conditional_replay_full_hash_and_readback_race(self):
        store = Store()
        store.version = None
        privacy = []
        archive = media.Archive(store, private_check=lambda key, version: privacy.append((key, version)))
        key = "import/solvesf/originals/sha256/" + media.sha(b"photo")
        first = archive.put(b"photo", key, "image/jpeg")
        self.assertIn("version_id", first)
        self.assertIsNone(first["version_id"])
        self.assertEqual(first, archive.put(b"photo", key, "image/jpeg"))
        self.assertEqual(store.downloads, [(key, None)] * 2)
        self.assertEqual(privacy, [(key, None)] * 2)
        store.race_after_read = True
        with self.assertRaisesRegex(media.MediaError, "race"):
            archive.put(b"photo", key, "image/jpeg")

    def test_whole_case_limits_and_binding_preserve_original(self):
        c = fixtures.candidate()
        c["media_urls"] = [URL]
        fixtures.sign(c)
        downloader, _ = self.downloader([Response(b"exact-original")])
        store = Store()
        projection = lambda data, content_type: (b"derived", {"width": 1, "height": 1, "content_type": "image/jpeg", "metadata_removed": True, "projection_version": media.PROJECTION})
        archive = media.Archive(store, private_check=lambda key, version: None)
        manifest = media.prepare([c], "f" * 64, downloader, archive, projection)
        case = manifest["cases"][c["source_id"]]
        original = case["items"][0]["original"]
        self.assertEqual(store.objects[original["key"]], b"exact-original")
        self.assertEqual(case["candidate_sha256"], c["candidate_sha256"])
        self.assertEqual(json.loads(store.objects[case["source_record_ref"]["key"]]), c)
        frozen_binding = store.objects[case["binding_ref"]["key"]]
        changed, _ = self.downloader([Response(b"changed-source-asset")])
        with self.assertRaises(media.MediaError):
            media.prepare([c], "f" * 64, changed, archive, projection)
        self.assertEqual(store.objects[case["binding_ref"]["key"]], frozen_binding)
        too_many = copy.deepcopy(c)
        too_many["media_urls"] = [URL + str(i) for i in range(5)]
        fixtures.sign(too_many)
        with self.assertRaises(ValueError):
            media.prepare([too_many], "0" * 64, downloader, archive, projection)


@unittest.skipUnless(importlib.util.find_spec("PIL") is not None, "Pillow optional locally; mandatory production fixture")
class ActualImages(unittest.TestCase):
    def test_strip_exif_rotate_bound_and_preserve_exact_original(self):
        from PIL import Image
        original = Image.new("RGB", (2400, 1200), "red")
        exif = Image.Exif()
        exif[274] = 6
        exif[270] = "synthetic private metadata"
        body = io.BytesIO()
        original.save(body, "JPEG", exif=exif)
        raw = body.getvalue()
        before = media.sha(raw)
        preview, info = media.derive(raw, "image/jpeg")
        self.assertEqual(media.sha(raw), before)
        self.assertEqual((info["width"], info["height"]), (800, 1600))
        self.assertLessEqual(len(preview), 128 * 1024)
        with Image.open(io.BytesIO(preview)) as image:
            self.assertFalse(image.getexif())
            self.assertNotIn("synthetic private metadata", str(image.info))
        with self.assertRaises(media.MediaError):
            media.derive(raw, "image/png")
        with self.assertRaises(Exception):
            media.derive(b"<html>not a photo</html>", "image/jpeg")


if __name__ == "__main__":
    unittest.main()
