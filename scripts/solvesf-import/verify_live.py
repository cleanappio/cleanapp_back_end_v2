#!/usr/bin/env python3
"""Read-only first-canary proof against native SQL and public JSON/JPEG/map APIs."""
import base64
import contextlib
import datetime as dt
import fcntl
import gzip
import hashlib
import importlib.util
import io
import json
import math
import re
import struct
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path('/srv/cleanapp-solvesf-import')
API = 'https://api.cleanapp.io/api/v3/reports/'
MAP = 'https://renderer.cleanapp.io/api/v4/reports/points?classification=physical'
OUTPUT = ROOT / 'evidence/live-canary.json'
KIND = 'cleanapp_solvesf_live_canary.v1'


class VerifyError(ValueError):
    pass


def require(ok, code):
    if not ok:
        raise VerifyError(code)


def digest(body):
    return hashlib.sha256(body).hexdigest()


def decoded(body):
    def unique(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, 'duplicate_public_json_key')
            value[key] = item
        return value
    return json.loads(body, object_pairs_hook=unique,
        parse_constant=lambda _: (_ for _ in ()).throw(VerifyError('nonfinite_public_json')))


def coordinates(value, candidate):
    for name in ('latitude', 'longitude'):
        actual = value.get(name)
        original = float(candidate[name])
        bound = 90 if name == 'latitude' else 180
        require(type(actual) in (int, float) and math.isfinite(actual) and math.isfinite(original)
                and -bound <= actual <= bound and -bound <= original <= bound,
                'native_coordinate_not_finite_or_in_range')
        require(struct.pack('!f', float(actual)) == struct.pack('!f', original),
                'native_float32_coordinate_mismatch')


def error_summary(error):
    result = {'error_type': type(error).__name__}
    if isinstance(error, VerifyError) and re.fullmatch(r'[a-z][a-z0-9_]{0,95}', str(error)):
        result['error_code'] = str(error)
    return result


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_args):
        return None


class PublicReader:
    def __init__(self):
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        self.next_at = 0.0

    def get(self, url):
        valid = (url == MAP or re.fullmatch(re.escape(API) + r'(?:by-public-id|rawimage/by-public-id)\?public_id=rpt_[A-Za-z0-9_-]{22}(?:&index=[0-3])?', url))
        require(valid, 'unreviewed_public_endpoint')
        image = '/rawimage/' in url
        limit = 16 * 1024**2 if url == MAP else 128 * 1024 if image else 2 * 1024**2
        time.sleep(max(0.0, self.next_at - time.monotonic()))
        self.next_at = time.monotonic() + 1.0
        request = urllib.request.Request(url, headers={'User-Agent': 'CleanApp-SolveSF-canary-verification',
            'Accept': 'image/jpeg' if image else 'application/json', 'Accept-Encoding': 'gzip' if url == MAP else 'identity'})
        try:
            with self.opener.open(request, timeout=30) as response:
                require(response.status == 200, 'public_http_not_200')
                body = response.read(limit + 1)
                require(len(body) <= limit, 'public_response_byte_limit')
                size = response.headers.get('Content-Length')
                require(size is None or (size.isdigit() and int(size) == len(body)), 'public_response_truncated')
                encoding = response.headers.get('Content-Encoding', '').lower()
                require(encoding in ('', 'identity', 'gzip') and (encoding != 'gzip' or url == MAP), 'unexpected_public_encoding')
                if encoding == 'gzip':
                    with gzip.GzipFile(fileobj=io.BytesIO(body)) as stream:
                        body = stream.read(64 * 1024**2 + 1)
                    require(len(body) <= 64 * 1024**2, 'map_decompression_limit')
                return body, response.headers.get('Content-Type', '').split(';', 1)[0].lower()
        except urllib.error.HTTPError as error:
            status = error.code
            error.close()
            raise VerifyError('public_http_' + str(status)) from None


def json_response(get, url):
    body, content_type = get(url)
    require(content_type == 'application/json', 'public_json_type_required')
    return decoded(body)


def native_rows(db, candidates, app):
    require(1 <= len(candidates) <= 5, 'canary_batch_bound')
    ids = ','.join(app.sql_string(candidate['source_id']) for candidate in candidates)
    flags = ','.join('p.' + name for name in app.FLAGS)
    query = ("SELECT HEX(JSON_OBJECT('source_id',l.source_id,'seq',COALESCE(l.report_seq,0),"
        "'recorded_seq',l.recorded_report_seq,'public_id',l.report_public_id,'payload_sha256',l.payload_sha256,"
        "'material_sha256',l.material_sha256,'candidate',l.candidate_json,'media',l.media_json,"
        "'disposition',l.disposition,'native_public_id',r.public_id,"
        "'latitude',CAST(r.latitude AS DOUBLE),'longitude',CAST(r.longitude AS DOUBLE),"
        "'primary_sha256',SHA2(COALESCE(r.image,_binary''),256),'policy_namespace',p.source_namespace,"
        "'policy_version',p.policy_version,'flags',JSON_ARRAY(" + flags + "),"
        "'raw_public',EXISTS(SELECT 1 FROM report_raw rr WHERE rr.report_seq=r.seq AND rr.fetcher_id='sf311' "
        "AND rr.source_id=l.source_id AND rr.visibility='public'),"
        "'analysis_ok',EXISTS(SELECT 1 FROM report_analysis a WHERE a.seq=r.seq AND a.source IN ('SolveSF','SSF') "
        "AND a.language='en' AND a.classification='physical' AND a.is_valid=1))) "
        "FROM sf311_source_case_ledger l LEFT JOIN reports r ON r.seq=l.report_seq "
        "LEFT JOIN report_side_effect_policy p ON p.report_seq=l.report_seq "
        "WHERE l.source_namespace='sf311' AND l.source_id IN (" + ids + ") ORDER BY l.source_id LIMIT 6")
    rows = [decoded(bytes.fromhex(line)) for line in db.query(query)]
    require(len(rows) == len(candidates) and len({row['source_id'] for row in rows}) == len(rows), 'native_case_mapping_missing_or_duplicated')
    return rows


def verify_rows(rows, candidates, app):
    indexed = {candidate['source_id']: candidate for candidate in candidates}
    for row in rows:
        candidate = indexed[row['source_id']]
        app.valid_candidate(candidate)
        require(row['candidate'] == candidate and row['payload_sha256'] == candidate['candidate_sha256']
            and row['material_sha256'] == app.digest({'candidate_sha256': candidate['candidate_sha256'], 'media': row['media']}), 'native_material_changed')
        require(type(row['seq']) is int and row['seq'] > 0 and row['seq'] == row['recorded_seq']
            and row['disposition'] == 'inserted' and row['public_id'] == row['native_public_id'] == app.public_id(candidate), 'native_mapping_deleted_or_changed')
        require(row['policy_namespace'] == 'sf311' and row['policy_version'] == 1
            and row['flags'] == [1] * 5 and row['raw_public'] == 1 and row['analysis_ok'] == 1, 'native_quiet_or_public_projection_failed')
        require(len(row['media']) == len(candidate['media_urls']) <= 4, 'native_media_count_mismatch')
        coordinates(row, candidate)
        expected = row['media'][0]['display']['sha256'] if row['media'] else digest(b'')
        require(row['primary_sha256'] == expected, 'native_primary_preview_mismatch')


def public_case(row, candidate, app, get):
    pid = row['public_id']
    value = json_response(get, API + 'by-public-id?public_id=' + pid)
    require(isinstance(value, dict) and isinstance(value.get('report'), dict), 'public_report_shape')
    report = value['report']
    _, full = app.presentation(candidate)
    require(report.get('seq') == row['seq'] and report.get('public_id') == pid
        and report.get('source_url') == candidate['source_url'], 'public_report_metadata_mismatch')
    expected_time = app.timestamp(candidate['requested_at']).replace(microsecond=0)
    require(app.timestamp(report.get('source_timestamp')) == expected_time
        and app.timestamp(report.get('timestamp')) == expected_time, 'public_report_timestamp_mismatch')
    coordinates(report, candidate)
    require(any((report.get('description'), analysis.get('title'), analysis.get('summary'), analysis.get('source'))
        in app.presentation_variants(candidate) and analysis.get('classification') == 'physical'
        and analysis.get('description') == full for analysis in value.get('analysis', []) if isinstance(analysis, dict)), 'public_source_attribution_missing')
    refs = row['media']
    require(report.get('extra_image_count', 0) == max(0, len(refs) - 1), 'public_extra_photo_count_mismatch')
    primary = base64.b64decode(report.get('image', ''), validate=True)
    require(digest(primary) == row['primary_sha256'], 'public_metadata_preview_mismatch')
    images = []
    for position, ref in enumerate(refs):
        body, content_type = get(API + 'rawimage/by-public-id?public_id=' + pid + '&index=' + str(position))
        require(content_type == 'image/jpeg' and body.startswith(b'\xff\xd8\xff') and body.endswith(b'\xff\xd9')
            and len(body) == ref['display']['bytes'] and digest(body) == ref['display']['sha256'], 'public_jpeg_preview_mismatch')
        images.append(digest(body))
    return {'public_url': 'https://www.cleanapp.io/physical/report/' + pid, 'preview_sha256': images}


def map_matches(value, rows, candidates):
    if isinstance(value, dict):
        lists = [value[key] for key in ('points', 'reports', 'data') if isinstance(value.get(key), list)]
        require(len(lists) == 1, 'unknown_map_shape')
        value = lists[0]
    require(isinstance(value, list), 'unknown_map_shape')
    indexed = {candidate['source_id']: candidate for candidate in candidates}
    for row in rows:
        matches = [point for point in value if isinstance(point, dict) and point.get('seq') == row['seq']]
        if not matches:
            return False
        require(len(matches) == 1, 'duplicate_canary_map_point')
        coordinates(matches[0], indexed[row['source_id']])
    return True


def public_checks(rows, candidates, app, get, sleep=time.sleep, monotonic=time.monotonic):
    indexed = {candidate['source_id']: candidate for candidate in candidates}
    results = [public_case(row, indexed[row['source_id']], app, get) for row in rows]
    require(sum(len(result['preview_sha256']) for result in results) > 0, 'canary_has_no_servable_jpeg_evidence')
    deadline = monotonic() + 180
    for attempt in range(3):
        if map_matches(json_response(get, MAP), rows, candidates):
            return results
        if attempt < 2:
            require(monotonic() + 30 < deadline, 'map_refresh_wait_exhausted')
            sleep(30)
    raise VerifyError('canary_absent_from_public_map')


def main():
    spec = importlib.util.spec_from_file_location('solvesf_canary_service', Path(__file__).with_name('service.py'))
    service = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(service)
    controller = service.Controller()
    guard, app = controller.guard, controller.apply
    guard.trusted(Path(__file__).resolve())
    result = {'kind': KIND, 'passed': False, 'config_sha256': controller.config_sha,
              'public_reports_verified': False, 'quiet_effects_verified': False}
    try:
        with (ROOT / 'state/controller.lock').open('a+') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            require(service.validate_limits(controller.config, service.now()) == 'eligible', 'controller_not_eligible')
            path = ROOT / 'evidence/controller-last-run.json'
            run_sha = guard.trusted(path, private=True)
            run = guard.document(path, run_sha)
            result['controller_run_sha256'] = run_sha
            require(run.get('state') == 'batch_complete' and type(run.get('cases')) is int and 1 <= run['cases'] <= 5, 'successful_first_cycle_required')
            batches = sorted(path for path in (ROOT / 'state/batches').iterdir() if re.fullmatch(r'[0-9]{8}T[0-9]{6}Z', path.name))
            require(batches, 'canary_batch_missing')
            batch = batches[-1]
            candidate_path, media_path = batch / 'candidates.jsonl', batch / 'media.json'
            controls_path = ROOT / 'evidence/controls.json'
            controls = guard.document(controls_path, guard.trusted(controls_path, private=True))
            require(guard.trusted(candidate_path, private=True) == controls['candidate_sha256']
                and guard.trusted(media_path, private=True) == controls['media_manifest_sha256'], 'latest_canary_inputs_not_bound')
            candidates = app.read_candidates(candidate_path, controls['candidate_sha256'])
            require(len(candidates) == run['cases'], 'canary_run_batch_count_mismatch')
            def inventory():
                with controller.transport.Mysql(readonly=True) as db:
                    controller.driver.server_check(db, controller.policy)
                    guard.verify_source_schema(db)
                    return native_rows(db, candidates, app)
            rows = inventory()
            verify_rows(rows, candidates, app)
            checks = public_checks(rows, candidates, app, PublicReader().get)
            require(inventory() == rows, 'native_inventory_changed_during_public_check')
            require(guard.trusted(service.CONFIG, private=True) == controller.config_sha
                and guard.trusted(path, private=True) == run_sha, 'canary_configuration_or_run_changed')
            require(not service.PAUSE.exists() and service.running(service.unit_state()), 'receiver_not_resumed')
            result.update(passed=True, public_reports_verified=True, quiet_effects_verified=True,
                public_jpeg_previews_verified=True, public_map_verified=True, receiver_resumed=True,
                cases_verified=len(rows), images_verified=sum(len(check['preview_sha256']) for check in checks),
                coordinate_projection='native_float32', reports=checks,
                verifier_sha256=guard.trusted(Path(__file__).resolve()))
    except Exception as error:
        result.update(error_summary(error))
    result['at_utc'] = dt.datetime.now(dt.timezone.utc).isoformat()
    service.write_json(OUTPUT, result)
    print(json.dumps(result, sort_keys=True))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
