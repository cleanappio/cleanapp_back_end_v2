import hashlib
from contextlib import closing
import io
import json
import sqlite3
import tempfile
import unittest
import urllib.error
from decimal import Decimal
from pathlib import Path
from unittest.mock import Mock, patch

import collect

PHOTO = 'https://spot-sf-res.cloudinary.com/image/upload/example.jpg'


def detail(ident='12345678', description='Public case description'):
    return {'service_request_id': ident, 'description': description,
            'requested_datetime': '2026-11-16T01:02:03-08:00',
            'updated_datetime': '2026-11-16T10:03:04Z', 'status': 'closed',
            'status_notes': 'Case closed by city', 'source': 'open311',
            'lat': Decimal('37.765432109876543'), 'long': Decimal('-122.412345678901234'),
            'photos': [PHOTO], 'opened_with_photos': [PHOTO],
            'browser_session_url': 'https://example.invalid/private-session',
            'userUUID': 'private-person', 'access_token': 'private-token',
            'extended_attributes': {'person': 'private-person'},
            'report_classifications': {'title': 'Generated title'}}


def feed(ident='12345678', status='open'):
    return {'reports': [{'id': ident, 'status': status, 'description': 'Public case description',
                        'photoUrls': [PHOTO], 'requestedDatetime': '2026-11-16T09:02:03Z'}]}


class NormalizeTests(unittest.TestCase):
    def test_identity_precision_provenance_and_digest(self):
        candidate = collect.normalize('12345678', detail())
        self.assertEqual(('sf311', '12345678'), (candidate['source_namespace'], candidate['source_id']))
        self.assertEqual('37.765432109876543', candidate['latitude'])
        self.assertEqual('-122.412345678901234', candidate['longitude'])
        self.assertEqual('2026-11-16T09:02:03Z', candidate['requested_at'])
        self.assertEqual('2026-11-16T01:02:03-08:00', candidate['source_record']['requested_datetime_raw'])
        self.assertEqual('2026-11-16T10:03:04Z', candidate['source_record']['updated_datetime_raw'])
        self.assertEqual('https://solvesf.com/report/12345678', candidate['source_url'])
        self.assertEqual('unknown', candidate['source_record']['submitter_origin'])
        self.assertEqual('unverified_may_be_ai', candidate['source_record']['description_authorship'])
        self.assertFalse(candidate['source_record']['resolution_verified'])
        self.assertEqual('closed', candidate['source_record']['status'])
        for excluded in ['private-session', 'private-person', 'private-token', 'Generated title']:
            self.assertNotIn(excluded, json.dumps(candidate))
        signature = candidate.pop('candidate_sha256')
        self.assertEqual(hashlib.sha256(collect.canonical(candidate).encode()).hexdigest(), signature)

    def test_rejects_id_confusion_and_naive_times(self):
        for ident in [12345678, '../123', '12?x=1', ' 123']:
            with self.assertRaises(collect.CollectError):
                collect.normalize(ident, detail())
        with self.assertRaises(collect.CollectError):
            collect.normalize('12345678', detail('87654321'))
        value = detail(); value['requested_datetime'] = '2026-11-16T01:02:03'
        with self.assertRaises(collect.CollectError):
            collect.normalize('12345678', value)

    def test_rejects_unsafe_media_and_invalid_coordinates(self):
        for url in ['https://spot-sf-res.cloudinary.com.evil.test/a',
                    'https://user:pass@spot-sf-res.cloudinary.com/a',
                    PHOTO + '?token=credential', 'http://spot-sf-res.cloudinary.com/a']:
            value = detail(); value['photos'] = [url]
            with self.assertRaises(collect.CollectError):
                collect.normalize('12345678', value)
        for lat in ['NaN', True, '91', None]:
            value = detail(); value['lat'] = lat
            with self.assertRaises(collect.CollectError):
                collect.normalize('12345678', value)

    def test_rejects_duplicate_ids_and_incomplete_snapshots(self):
        for value in [dict(feed(), truncated=True), dict(feed(), refreshing=True),
                      dict(feed(), total=2), {'reports': feed()['reports'] * 2}]:
            with self.assertRaises(collect.CollectError):
                collect.discovery(value)

    def test_rejects_explicitly_private_or_deleted_records(self):
        for flag in [{'deleted': True}, {'deleted_at': '2026-11-16T09:00:00Z'},
                     {'nonpublic': True}, {'is_public': False}, {'visibility': 'private'}]:
            with self.subTest(flag=flag):
                with self.assertRaises(collect.CollectError):
                    collect.normalize('12345678', dict(detail(), **flag))
                snapshot = feed()
                snapshot['reports'][0].update(flag)
                with self.assertRaises(collect.CollectError):
                    collect.discovery(snapshot)


class HttpTests(unittest.TestCase):
    def setUp(self):
        self.throttle_patch = patch.object(collect, 'throttle')
        self.throttle_patch.start()
        self.addCleanup(self.throttle_patch.stop)

    class Response(io.BytesIO):
        def __init__(self, body, content_type='application/json', length=None):
            super().__init__(body)
            self.headers = {'Content-Type': content_type}
            if length is not None:
                self.headers['Content-Length'] = str(length)

    def test_bounds_types_json_and_no_auth(self):
        opener = Mock()
        for response in [self.Response(b'{}' * 20), self.Response(b'{}', 'text/html'),
                         self.Response(b'{broken'), self.Response(b'{}', length=3),
                         self.Response(b'{"a":1,"a":2}')]:
            opener.open.return_value = response
            with patch.object(collect.urllib.request, 'build_opener', return_value=opener):
                with self.assertRaises(collect.CollectError):
                    collect.public_get(collect.FEED_URL, byte_limit=20)
        opener.open.return_value = self.Response(b'{}', length=2)
        with patch.object(collect.urllib.request, 'build_opener', return_value=opener):
            self.assertEqual({}, collect.public_get(collect.FEED_URL))
        request = opener.open.call_args.args[0]
        self.assertFalse(any(key.lower() in ['authorization', 'cookie'] for key in request.headers))
        with self.assertRaises(collect.CollectError):
            collect.public_get('https://elsewhere.invalid/api/report/123')

    def test_rate_limit_fails_without_retry(self):
        opener = Mock()
        body = io.BytesIO(b'limited')
        opener.open.side_effect = urllib.error.HTTPError(collect.FEED_URL, 429, 'rate limited', {}, body)
        with patch.object(collect.urllib.request, 'build_opener', return_value=opener):
            with self.assertRaisesRegex(collect.CollectError, 'HTTP 429'):
                collect.public_get(collect.FEED_URL)
        self.assertEqual(1, opener.open.call_count)
        self.assertTrue(body.closed)


class ThrottleTests(unittest.TestCase):
    def test_default_client_spaces_request_starts_by_one_second(self):
        clock = [100.0]
        delays = []
        def sleep(delay):
            delays.append(delay)
            clock[0] += delay
        with patch.object(collect, '_last_request_at', None), \
                patch.object(collect.time, 'monotonic', side_effect=lambda: clock[0]), \
                patch.object(collect.time, 'sleep', side_effect=sleep):
            collect.throttle()
            clock[0] += 0.25
            collect.throttle()
            self.assertEqual([0.75], delays)
            clock[0] += 2.0
            collect.throttle()
            self.assertEqual([0.75], delays)


class CheckpointTests(unittest.TestCase):
    def test_explicit_supported_citywide_window_is_recorded(self):
        with tempfile.TemporaryDirectory() as directory:
            calls = []
            def fetch(url):
                calls.append(url)
                return feed() if url in collect.FEED_URLS else detail()
            stats = collect.run(Path(directory), 1, fetch, window='today')
            self.assertEqual('https://solvesf.com/api/live-reports?window=today', calls[0])
            self.assertEqual('today', stats['window'])
            with self.assertRaises(collect.CollectError):
                collect.run(Path(directory), 1, fetch, window='7d')

    def test_invalid_first_case_is_sticky_metadata_hold_and_valid_second_progresses(self):
        with tempfile.TemporaryDirectory() as directory:
            snapshot = {'reports': feed()['reports'] + feed('23456789')['reports']}
            def fetch(url):
                if url == collect.FEED_URL:
                    return snapshot
                value = detail(url.rsplit('/', 1)[1])
                if url.endswith('12345678'):
                    value['requested_datetime'] = 'missing timezone'
                return value
            stats = collect.run(Path(directory), 2, fetch)
            self.assertEqual(1, stats['candidate_count'])
            self.assertEqual({'invalid_public_record': 1}, stats['held_record_reasons'])
            with closing(sqlite3.connect(Path(directory) / 'ledger.sqlite3')) as ledger:
                row = ledger.execute('SELECT * FROM record_holds').fetchone()
                self.assertEqual(('12345678', 'invalid_public_record'), row[:2])
                self.assertEqual(4, len(row))
                self.assertEqual(0, ledger.execute("SELECT count(*) FROM versions WHERE id='12345678'").fetchone()[0])
            self.assertEqual(0, collect.run(Path(directory), 2, fetch)['details_fetched'])

    def test_404_is_held_but_access_rate_and_server_errors_remain_fatal(self):
        for status in (404, 403, 429, 500):
            with self.subTest(status=status), tempfile.TemporaryDirectory() as directory:
                def fetch(url):
                    if url == collect.FEED_URL:
                        return feed()
                    raise collect.PublicHTTPError(status)
                if status == 404:
                    self.assertEqual({'detail_not_found': 1}, collect.run(Path(directory), 1, fetch)['held_record_reasons'])
                else:
                    with self.assertRaises(collect.PublicHTTPError):
                        collect.run(Path(directory), 1, fetch)
                    with closing(sqlite3.connect(Path(directory) / 'ledger.sqlite3')) as ledger:
                        self.assertEqual(0, ledger.execute('SELECT count(*) FROM discoveries').fetchone()[0])
                        self.assertEqual(0, ledger.execute('SELECT count(*) FROM record_holds').fetchone()[0])

    def test_resume_replay_and_revision_hold(self):
        with tempfile.TemporaryDirectory() as directory:
            snapshot = {'reports': feed()['reports'] + feed('23456789')['reports']}
            def fetch(url):
                return snapshot if url == collect.FEED_URL else detail(url.rsplit('/', 1)[1])
            first = collect.run(Path(directory), 1, fetch)
            self.assertEqual(1, first['details_fetched'])
            self.assertEqual(1, first['pending_discoveries'])
            second = collect.run(Path(directory), 1, fetch)
            self.assertEqual(2, second['candidate_count'])
            self.assertEqual(0, collect.run(Path(directory), 1, fetch)['details_fetched'])
            snapshot['reports'][0]['status'] = 'closed'
            def changed(url):
                return snapshot if url == collect.FEED_URL else detail(url.rsplit('/', 1)[1], 'Changed public case')
            fourth = collect.run(Path(directory), 1, changed)
            self.assertEqual(1, fourth['held_revision_count'])
            candidates = [json.loads(line) for line in (Path(directory) / 'candidates.jsonl').read_text().splitlines()]
            self.assertEqual(['23456789'], [candidate['source_id'] for candidate in candidates])
            self.assertEqual('Public case description', candidates[0]['text'])
            self.assertFalse(fourth['whole_history_complete'])
            self.assertEqual(hashlib.sha256((Path(directory) / 'candidates.jsonl').read_bytes()).hexdigest(), fourth['candidates_sha256'])

    def test_more_than_four_photos_is_retained_but_held(self):
        with tempfile.TemporaryDirectory() as directory:
            value = detail(); value['photos'] = [PHOTO + str(i) for i in range(5)]
            stats = collect.run(Path(directory), 1, lambda url: feed() if url == collect.FEED_URL else value)
            self.assertEqual(0, stats['candidate_count'])
            held = json.loads((Path(directory) / 'held-revisions.jsonl').read_text())
            self.assertEqual('held_media_count', held['disposition'])
            self.assertEqual(6, len(held['candidate']['media_urls']))

    def test_interruption_checkpoints_without_publishing_partial_snapshot(self):
        with tempfile.TemporaryDirectory() as directory:
            snapshot = {'reports': feed()['reports'] + feed('23456789')['reports']}
            def interrupted(url):
                if url == collect.FEED_URL:
                    return snapshot
                if url.endswith('23456789'):
                    raise collect.CollectError('HTTP 429')
                return detail()
            with self.assertRaises(collect.CollectError):
                collect.run(Path(directory), 2, interrupted)
            self.assertFalse((Path(directory) / 'candidates.jsonl').exists())
            def resumed(url):
                return snapshot if url == collect.FEED_URL else detail(url.rsplit('/', 1)[1])
            stats = collect.run(Path(directory), 2, resumed)
            self.assertEqual(1, stats['details_fetched'])
            self.assertEqual(2, stats['candidate_count'])


if __name__ == '__main__':
    unittest.main()
