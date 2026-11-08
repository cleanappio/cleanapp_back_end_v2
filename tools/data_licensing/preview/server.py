"""Read-only, password-gated projection of one hash-pinned physical evaluation ZIP."""
from __future__ import annotations

import argparse
import base64
from collections import OrderedDict, deque
from contextlib import asynccontextmanager
from dataclasses import dataclass
import getpass
import hashlib
import hmac
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import threading
import time
from urllib.parse import parse_qs, urlsplit
import zipfile

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import FileResponse, HTMLResponse, JSONResponse, RedirectResponse, Response

ROOT = Path(__file__).resolve().parent
PREFIX = '/grepped'
COOKIE = '__Secure-cleanapp_grepped'
HEX = re.compile(r'^[a-f0-9]{64}$')
SAMPLE = re.compile(r'^CA-[0-9]{4}$')
ANNOTATION_FIELDS = ('classification', 'created_at', 'description', 'digital_bug_probability',
                     'hazard_probability', 'is_valid', 'language', 'litter_probability',
                     'needs_ai_review', 'severity_level', 'source', 'summary', 'title', 'updated_at')
RECORD_FIELDS = ('sample_id', 'caption', 'caption_source', 'scene_category', 'scene_category_basis',
                 'report_description', 'report_description_authorship', 'report_timestamp_utc',
                 'timestamp_basis', 'width', 'height', 'image_bytes', 'mime_type', 'sha256',
                 'annotation_status', 'model_revision')
COUNT_FIELDS = ('total_report_records', 'image_bearing_report_records',
                'valid_physical_report_records', 'valid_physical_reports_with_stored_images')


def make_password_hash(password: str) -> str:
    salt = secrets.token_bytes(24)
    digest = hashlib.pbkdf2_hmac('sha256', password.encode(), salt, 600_000)
    return 'pbkdf2_sha256$600000$' + base64.b64encode(salt).decode() + '$' + base64.b64encode(digest).decode()


def parse_password_hash(encoded: str):
    try:
        algorithm, rounds, salt, digest = encoded.split('$')
        rounds = int(rounds)
        salt = base64.b64decode(salt, validate=True)
        digest = base64.b64decode(digest, validate=True)
        if algorithm != 'pbkdf2_sha256' or not 600_000 <= rounds <= 2_000_000 or len(salt) < 16 or len(digest) != 32:
            raise ValueError
        return rounds, salt, digest
    except (ValueError, TypeError):
        raise ValueError('A valid server-side password hash is required') from None


@dataclass(frozen=True)
class Config:
    zip_path: Path
    zip_sha256: str
    password_hash: str
    public_origin: str = 'https://cleanapp.io'
    trusted_proxy_ips: tuple[str, ...] = ()
    session_seconds: int = 7200
    expected_count: int = 500

    @classmethod
    def from_env(cls):
        return cls(Path(os.environ['GREPPED_ZIP']), os.environ['GREPPED_ZIP_SHA256'],
                   os.environ['GREPPED_PASSWORD_HASH'], os.environ.get('GREPPED_PUBLIC_ORIGIN', 'https://cleanapp.io'),
                   tuple(filter(None, os.environ.get('GREPPED_TRUSTED_PROXY_IPS', '').split(','))))


class FrozenBundle:
    """Never extract arbitrary members; serve only validated image members and allowlisted data."""
    def __init__(self, cfg: Config):
        if not HEX.fullmatch(cfg.zip_sha256):
            raise ValueError('An exact frozen ZIP SHA-256 is required')
        path = cfg.zip_path
        if path.is_symlink() or not path.is_file():
            raise ValueError('ZIP must be a regular, nonsymlink file')
        # Retain the verified file descriptor; replacing the pathname cannot redirect reads.
        self.file = open(path, 'rb')
        digest = hashlib.file_digest(self.file, 'sha256').hexdigest()
        if not hmac.compare_digest(digest, cfg.zip_sha256):
            self.file.close()
            raise ValueError('Frozen ZIP hash mismatch')
        self.stat = os.fstat(self.file.fileno())
        self.file.seek(0)
        self.zip = zipfile.ZipFile(self.file)
        self.lock = threading.Lock()
        infos = self.zip.infolist()
        if len(infos) > 2000 or len({i.filename for i in infos}) != len(infos):
            raise ValueError('Invalid bundle membership')
        members = {i.filename: i for i in infos}
        for name in ('records.jsonl', 'manifest.json', 'corpus_inventory.json'):
            if name not in members or members[name].file_size > 16_000_000:
                raise ValueError('Required bundle metadata is missing or too large')
        rows = [json.loads(x) for x in self.zip.read('records.jsonl').splitlines() if x.strip()]
        if len(rows) != cfg.expected_count or len({r['sample_id'] for r in rows}) != len(rows):
            raise ValueError('Frozen physical sample count or membership mismatch')
        self.items, self.images = [], {}
        for row in rows:
            sid, image = row['sample_id'], row['image_path']
            if not SAMPLE.fullmatch(sid) or image != f'images/{sid}.jpg' or image not in members:
                raise ValueError('Unsafe or unexpected image member')
            if not HEX.fullmatch(row['sha256']) or row['mime_type'] != 'image/jpeg':
                raise ValueError('Unexpected original image type or hash')
            size = row['image_bytes']
            if type(size) is not int or not 0 < size <= 50_000_000 or members[image].file_size != size:
                raise ValueError('Original image size mismatch')
            public = {k: row.get(k) for k in RECORD_FIELDS}
            public['annotations'] = [{k: a.get(k) for k in ANNOTATION_FIELDS} for a in row.get('annotations', [])]
            public['image_url'] = f'{PREFIX}/media/{sid}'
            self.items.append(public)
            self.images[sid] = (image, size, row['sha256'])
        manifest = json.loads(self.zip.read('manifest.json'))
        if manifest.get('total') != len(rows):
            raise ValueError('Bundle manifest count mismatch')
        inventory = json.loads(self.zip.read('corpus_inventory.json'))
        counts = {k: inventory[k] for k in COUNT_FIELDS if type(inventory.get(k)) is int and inventory[k] >= 0}
        self.summary = {'title': 'CleanApp physical image evaluation', 'total': len(rows),
                        'selection_type': 'Curated diversity showcase; not a statistical corpus sample',
                        'image_format': 'Exact stored original JPEG bytes', 'corpus_counts': counts,
                        'corpus_as_of_utc': inventory.get('as_of_utc'),
                        'corpus_count_basis': 'Image-bearing report records include sequestered records and exact-file duplicates; not a unique licensed-image count.',
                        'annotation_basis': 'Existing source-labeled AI analysis; not independently verified ground truth. Report timestamps are not verified capture times.'}

    def close(self):
        if archive := getattr(self, 'zip', None):
            archive.close()
        if handle := getattr(self, 'file', None):
            handle.close()

    def __del__(self):
        self.close()

    def original(self, sid):
        if sid not in self.images:
            raise HTTPException(404, 'Image not found')
        name, size, sha = self.images[sid]
        with self.lock:
            now = os.fstat(self.file.fileno())
            if (now.st_size, now.st_mtime_ns, now.st_ctime_ns) != (self.stat.st_size, self.stat.st_mtime_ns, self.stat.st_ctime_ns):
                raise HTTPException(409, 'Frozen bundle changed')
            try:
                raw = self.zip.read(name)
            except (OSError, RuntimeError, zipfile.BadZipFile):
                raise HTTPException(409, 'Frozen original unavailable') from None
        if len(raw) != size or not hmac.compare_digest(hashlib.sha256(raw).hexdigest(), sha):
            raise HTTPException(409, 'Frozen original verification failed')
        return raw


class AuthState:
    def __init__(self, cfg, clock=time.monotonic):
        self.cfg, self.clock = cfg, clock
        self.rounds, self.salt, self.digest = parse_password_hash(cfg.password_hash)
        self.sessions, self.attempts, self.global_attempts = {}, OrderedDict(), deque()
        self.lock = threading.Lock()

    def valid(self, token):
        if not token or len(token) > 128:
            return False
        with self.lock:
            key = hashlib.sha256(token.encode()).hexdigest()
            expiration = self.sessions.get(key, 0)
            if expiration <= self.clock():
                self.sessions.pop(key, None)
                return False
            return True

    def login(self, address, password):
        with self.lock:
            now = self.clock()
            self.sessions = {k: v for k, v in self.sessions.items() if v > now}
            self.attempts = OrderedDict((k, v) for k, v in self.attempts.items() if v and v[-1] > now - 900)
            while self.global_attempts and self.global_attempts[0] <= now - 300:
                self.global_attempts.popleft()
            if len(self.global_attempts) >= 30:
                return None, 429
            attempts = self.attempts.setdefault(address, deque())
            if len(attempts) >= 5:
                return None, 429
            # Reserve before password verification so concurrent attempts cannot bypass the cap.
            attempts.append(now)
            self.global_attempts.append(now)
            if len(self.attempts) > 4096:
                self.attempts.popitem(last=False)
        got = hashlib.pbkdf2_hmac('sha256', password.encode(), self.salt, self.rounds)
        if not hmac.compare_digest(got, self.digest):
            return None, 401
        token = secrets.token_urlsafe(32)
        with self.lock:
            if len(self.sessions) >= 1024:
                return None, 429
            self.sessions[hashlib.sha256(token.encode()).hexdigest()] = self.clock() + self.cfg.session_seconds
        return token, 200

    def logout(self, token):
        if token:
            with self.lock:
                self.sessions.pop(hashlib.sha256(token.encode()).hexdigest(), None)


LOGIN_STYLE = 'body{margin:0;background:#f4f6ef;color:#21382a;font:15px system-ui,sans-serif;display:grid;min-height:100svh;place-items:center}main{width:min(360px,80vw);padding:50px 0}h1{font-size:43px;letter-spacing:-2px;margin:0 0 40px}h1 span{color:#256345}h2{font-size:21px;font-weight:550;margin:0 0 12px}p{font-size:13px;line-height:1.7;color:#667463}label{display:block;font-size:12px;margin:35px 0 10px}input{box-sizing:border-box;width:100%;border:0;border-bottom:1px solid #9eaf99;background:transparent;padding:12px 2px;font:16px system-ui;color:#21382a}button{margin-top:25px;background:#235b40;color:white;border:0;padding:13px 21px;font:13px system-ui;cursor:pointer}input:focus-visible,button:focus-visible{outline:2px solid #235b40;outline-offset:4px}.error{color:#8c4737;min-height:23px}'
LOGIN_STYLE_HASH = base64.b64encode(hashlib.sha256(LOGIN_STYLE.encode()).digest()).decode()


def login_page(error=''):
    return HTMLResponse('<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>CleanApp · Private evaluation</title><style>' + LOGIN_STYLE + '</style><main><h1>CleanApp<span>.</span></h1><h2>Private image evaluation</h2><p>500 original physical-world images and their existing source-labeled annotations.</p><form action="/grepped/login" method="post"><label for="password">Access password</label><input id="password" name="password" type="password" autocomplete="current-password" required maxlength="256" autofocus><p class="error" role="status">' + error + '</p><button type="submit">Open evaluation →</button></form></main></html>')


def create_app(cfg: Config, *, clock=time.monotonic):
    origin = urlsplit(cfg.public_origin)
    if origin.scheme != 'https' or not origin.netloc or origin.path or origin.query or origin.fragment:
        raise ValueError('Public origin must be an HTTPS origin without a path')
    if not 60 <= cfg.session_seconds <= 28800:
        raise ValueError('Invalid session lifetime')
    for address in cfg.trusted_proxy_ips:
        ipaddress.ip_address(address)
    auth, bundle = AuthState(cfg, clock), FrozenBundle(cfg)
    @asynccontextmanager
    async def lifespan(app):
        yield
        bundle.close()
    app = FastAPI(docs_url=None, redoc_url=None, openapi_url=None, redirect_slashes=False, lifespan=lifespan)
    app.state.bundle, app.state.auth = bundle, auth

    @app.middleware('http')
    async def gate(request: Request, call_next):
        path = request.url.path
        if request.headers.get('host', '').lower() != origin.netloc.lower():
            response = Response('Invalid host', status_code=403)
        elif request.method in ('POST', 'PUT', 'DELETE', 'PATCH') and request.headers.get('origin') != cfg.public_origin:
            response = Response('Origin mismatch', status_code=403)
        elif path == PREFIX + '/login' and request.method in ('GET', 'HEAD', 'POST'):
            response = await call_next(request)
        elif not auth.valid(request.cookies.get(COOKIE)):
            response = RedirectResponse(PREFIX + '/login', status_code=303) if path in (PREFIX, PREFIX + '/') else JSONResponse({'error': 'Authentication required'}, status_code=401)
        else:
            response = await call_next(request)
        response.headers.update({'Cache-Control': 'no-store, private', 'Pragma': 'no-cache',
            'X-Content-Type-Options': 'nosniff', 'Referrer-Policy': 'no-referrer',
            'X-Frame-Options': 'DENY', 'X-Robots-Tag': 'noindex, nofollow, noarchive',
            'Content-Security-Policy': "default-src 'none'; img-src 'self'; style-src 'self' 'sha256-" + LOGIN_STYLE_HASH + "'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"})
        return response

    @app.get(PREFIX + '/login')
    def login_get(request: Request):
        return RedirectResponse(PREFIX + '/', status_code=303) if auth.valid(request.cookies.get(COOKIE)) else login_page()

    @app.post(PREFIX + '/login')
    async def login_post(request: Request):
        if request.headers.get('content-type', '').split(';')[0] != 'application/x-www-form-urlencoded':
            raise HTTPException(415, 'Form required')
        body = bytearray()
        async for part in request.stream():
            body.extend(part)
            if len(body) > 2048:
                raise HTTPException(413, 'Request too large')
        try:
            data = parse_qs(body.decode('utf-8'), max_num_fields=2, strict_parsing=True)
            password = data['password'][0]
            if set(data) != {'password'} or len(data['password']) != 1 or len(password) > 256:
                raise ValueError
        except (UnicodeDecodeError, KeyError, ValueError):
            raise HTTPException(400, 'Invalid form') from None
        peer = request.client.host if request.client else 'unknown'
        if peer in cfg.trusted_proxy_ips:
            try:
                peer = str(ipaddress.ip_address(request.headers.get('x-real-ip', '')))
            except ValueError:
                raise HTTPException(400, 'Trusted proxy must supply a valid X-Real-IP') from None
        token, status = auth.login(peer, password)
        if token is None:
            response = login_page('Please wait before trying again.' if status == 429 else 'Password not recognized.')
            response.status_code = status
            if status == 429:
                response.headers['Retry-After'] = '900'
            return response
        auth.logout(request.cookies.get(COOKIE))
        response = RedirectResponse(PREFIX + '/', status_code=303)
        response.set_cookie(COOKIE, token, max_age=cfg.session_seconds, path=PREFIX,
                            secure=True, httponly=True, samesite='strict')
        return response

    @app.post(PREFIX + '/logout')
    def logout(request: Request):
        auth.logout(request.cookies.get(COOKIE))
        response = RedirectResponse(PREFIX + '/login', status_code=303)
        response.delete_cookie(COOKIE, path=PREFIX, secure=True, httponly=True, samesite='strict')
        return response

    @app.get(PREFIX)
    @app.get(PREFIX + '/')
    def index():
        return FileResponse(ROOT / 'static/index.html')

    @app.get(PREFIX + '/static/{name}')
    def static(name: str):
        if name not in ('app.js', 'style.css'):
            raise HTTPException(404)
        return FileResponse(ROOT / 'static' / name)

    @app.get(PREFIX + '/api/summary')
    def summary():
        return bundle.summary

    @app.get(PREFIX + '/api/items')
    def items():
        return {'items': bundle.items}

    @app.get(PREFIX + '/media/{sample_id}')
    def media(sample_id: str):
        return Response(bundle.original(sample_id), media_type='image/jpeg',
                        headers={'Content-Disposition': f'inline; filename="{sample_id}.jpg"'})

    return app


def create_from_env():
    return create_app(Config.from_env())


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--hash-password', action='store_true')
    args = parser.parse_args()
    if args.hash_password:
        print(make_password_hash(getpass.getpass('Preview password: ')))
    else:
        import uvicorn
        uvicorn.run(create_app(Config.from_env()), host='127.0.0.1',
                    port=int(os.environ.get('GREPPED_PORT', '8790')), access_log=False, proxy_headers=False)
