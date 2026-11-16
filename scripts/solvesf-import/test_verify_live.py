import base64
import copy
import importlib.util
import io
import json
import struct
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import test_apply
import verify_live as verify

app = test_apply.app
JPEG = b'\xff\xd8\xffsynthetic-public-wire-preview\xff\xd9'


def fixtures():
    candidate = test_apply.candidate('123')
    candidate['media_urls'] = ['https://spot-sf-res.cloudinary.com/preview.jpg']
    test_apply.sign(candidate)
    media = [{'display': {'sha256': verify.digest(JPEG), 'bytes': len(JPEG)}}]
    lat = struct.unpack('!f', struct.pack('!f', float(candidate['latitude'])))[0]
    lon = struct.unpack('!f', struct.pack('!f', float(candidate['longitude'])))[0]
    row = {'source_id': '123', 'seq': 77, 'recorded_seq': 77, 'public_id': app.public_id(candidate),
        'native_public_id': app.public_id(candidate), 'payload_sha256': candidate['candidate_sha256'],
        'material_sha256': app.digest({'candidate_sha256': candidate['candidate_sha256'], 'media': media}),
        'candidate': candidate, 'media': media, 'disposition': 'inserted', 'latitude': lat, 'longitude': lon,
        'primary_sha256': verify.digest(JPEG), 'policy_namespace': 'sf311', 'policy_version': 1,
        'flags': [1] * 5, 'raw_public': 1, 'analysis_ok': 1}
    compact, full = app.presentation(candidate)
    public = {'report': {'seq': 77, 'public_id': row['public_id'], 'source_url': candidate['source_url'],
        'description': compact, 'timestamp': candidate['requested_at'], 'source_timestamp': candidate['requested_at'],
        'latitude': lat, 'longitude': lon, 'image': base64.b64encode(JPEG).decode()},
        'analysis': [{'source': 'SolveSF', 'classification': 'physical', 'description': full}]}
    point = {'seq': 77, 'latitude': lat, 'longitude': lon}
    return candidate, row, public, point


class CanaryProof(unittest.TestCase):
    def test_shortest_float32_decimal_is_valid_but_different_bits_fail(self):
        candidate = {'latitude': '37.779449462890625', 'longitude': '-122.4123456789'}
        verify.coordinates({'latitude': 37.77945, 'longitude': -122.412346}, candidate)
        for actual in ({'latitude': 37.77946, 'longitude': -122.412346},
                       {'latitude': float('nan'), 'longitude': -122.412346},
                       {'latitude': True, 'longitude': -122.412346},
                       {'latitude': 91, 'longitude': -122.412346}):
            with self.subTest(actual=actual), self.assertRaises(verify.VerifyError):
                verify.coordinates(actual, candidate)

    def test_error_diagnostics_allow_only_static_safe_codes(self):
        self.assertEqual('public_http_429', verify.error_summary(verify.VerifyError('public_http_429'))['error_code'])
        self.assertNotIn('error_code', verify.error_summary(verify.VerifyError('https://example.invalid/?token=sensitive')))
        self.assertNotIn('error_code', verify.error_summary(ValueError('arbitrary detail')))

    def test_native_mapping_flags_and_material_are_checked(self):
        candidate, row, _, _ = fixtures()
        verify.verify_rows([row], [candidate], app)
        for mutation in ({'seq': 0}, {'flags': [1, 1, 0, 1, 1]}, {'raw_public': 0},
                         {'primary_sha256': '0' * 64}, {'payload_sha256': '0' * 64}):
            with self.subTest(mutation=mutation), self.assertRaises(verify.VerifyError):
                verify.verify_rows([{**row, **mutation}], [candidate], app)

    def test_actual_public_metadata_and_jpeg_digest_pass(self):
        candidate, row, public, _ = fixtures()
        def get(url):
            return (JPEG, 'image/jpeg') if '/rawimage/' in url else (json.dumps(public).encode(), 'application/json')
        result = verify.public_case(row, candidate, app, get)
        self.assertEqual([verify.digest(JPEG)], result['preview_sha256'])
        self.assertIn('/physical/report/rpt_', result['public_url'])

    def test_frontend_html_and_wrong_preview_cannot_pass(self):
        candidate, row, public, _ = fixtures()
        with self.assertRaises(verify.VerifyError):
            verify.public_case(row, candidate, app, lambda _: (b'<html>200</html>', 'text/html'))
        def get(url):
            return (b'wrong', 'image/jpeg') if '/rawimage/' in url else (json.dumps(public).encode(), 'application/json')
        with self.assertRaises(verify.VerifyError):
            verify.public_case(row, candidate, app, get)

    def test_metadata_attribution_coordinates_and_timestamp_drift_fail(self):
        candidate, row, public, _ = fixtures()
        for field, value in [('source_url', 'https://example.invalid/changed'), ('latitude', 1),
                             ('source_timestamp', '2025-01-01T00:00:00Z')]:
            altered = copy.deepcopy(public); altered['report'][field] = value
            with self.subTest(field=field), self.assertRaises(verify.VerifyError):
                verify.public_case(row, candidate, app, lambda _: (json.dumps(altered).encode(), 'application/json'))

    def test_map_presence_and_native_float32_projection_required(self):
        candidate, row, _, point = fixtures()
        self.assertTrue(verify.map_matches([point], [row], [candidate]))
        self.assertFalse(verify.map_matches([], [row], [candidate]))
        with self.assertRaises(verify.VerifyError):
            verify.map_matches([{**point, 'longitude': 0}], [row], [candidate])
        with self.assertRaises(verify.VerifyError):
            verify.map_matches([point, point], [row], [candidate])

    def test_map_refresh_retry_is_bounded_and_not_a_false_pass(self):
        candidate, row, public, point = fixtures()
        maps = [[], [point]]; waits = []
        def get(url):
            if url == verify.MAP:
                return json.dumps(maps.pop(0)).encode(), 'application/json'
            return (JPEG, 'image/jpeg') if '/rawimage/' in url else (json.dumps(public).encode(), 'application/json')
        result = verify.public_checks([row], [candidate], app, get, sleep=waits.append, monotonic=lambda: 0)
        self.assertEqual([30], waits)
        self.assertEqual(len(result), 1)
        with self.assertRaises(verify.VerifyError):
            verify.public_checks([row], [candidate], app,
                lambda url: (b'[]', 'application/json') if url == verify.MAP else get(url),
                sleep=lambda _: None, monotonic=lambda: 0)

    def test_native_query_is_bounded_to_exact_batch_and_reads_only(self):
        candidate, row, _, _ = fixtures()
        db = Mock(); db.query.return_value = [json.dumps(row).encode().hex()]
        self.assertEqual([row], verify.native_rows(db, [candidate], app))
        query = db.query.call_args.args[0]
        self.assertTrue(query.startswith('SELECT '))
        self.assertIn('LIMIT 6', query)
        self.assertIn("l.source_namespace='sf311'", query)

    def test_public_reader_rejects_other_hosts_without_network(self):
        reader = verify.PublicReader()
        reader.opener = Mock()
        with self.assertRaises(verify.VerifyError):
            reader.get('https://example.invalid/api/v3/reports/by-public-id?public_id=rpt_bad')
        reader.opener.open.assert_not_called()


if __name__ == '__main__':
    unittest.main()
