import hashlib
import io
import json
from pathlib import Path
import tempfile
import sys
import unittest
import zipfile

from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from server import Config, COOKIE, create_app, make_password_hash


class PreviewSecurityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.password_hash = make_password_hash('test-only-evaluation-password')

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.path = Path(self.temp.name) / 'frozen.zip'
        self.raw = b'\xff\xd8synthetic-unchanged-original\xff\xd9'
        self.now = [1000.0]
        self.write_bundle()
        self.cfg = Config(self.path, self.sha, self.password_hash, expected_count=1)
        self.app = create_app(self.cfg, clock=lambda: self.now[0])
        self.client = TestClient(self.app, base_url='https://cleanapp.io')

    def tearDown(self):
        self.app.state.bundle.zip.close()
        self.app.state.bundle.file.close()
        self.temp.cleanup()

    def write_bundle(self, **changes):
        row = {'sample_id':'CA-0001','image_path':'images/CA-0001.jpg','sha256':hashlib.sha256(self.raw).hexdigest(),
               'image_bytes':len(self.raw),'mime_type':'image/jpeg','caption':'<script>bad()</script>',
               'caption_source':'Saved AI','scene_category':'infrastructure','width':12,'height':8,
               'report_timestamp_utc':'2026-10-01 00:00:00','report_description':None,
               'seq':123456,'latitude':1.23456789,'coarse_location':{'latitude_rounded_degrees':1.23},
               'private_provenance':{'file':'secret-path'},'review':{'note':'private-note'},
               'annotations':[{'source':'Saved AI','language':'en','title':'Safe title','description':'Saved context',
                               'seq':123456,'private_note':'never expose'}]}
        row.update(changes)
        with zipfile.ZipFile(self.path,'w') as z:
            z.writestr('records.jsonl',json.dumps(row)+'\n')
            z.writestr('manifest.json',json.dumps({'total':1,'owner_review':'private-owner-status'}))
            z.writestr('corpus_inventory.json',json.dumps({'total_report_records':1000,'image_bearing_report_records':120,
                'valid_physical_report_records':200,'valid_physical_reports_with_stored_images':90,
                'website_cached_physical_report_count':999,'count_components':{'private':7}}))
            z.writestr('images/CA-0001.jpg',self.raw)
            z.writestr('notes.txt','private zip member')
        self.sha = hashlib.sha256(self.path.read_bytes()).hexdigest()

    def login(self, password='test-only-evaluation-password', **kwargs):
        return self.client.post('/grepped/login', data={'password':password},
                                headers={'Origin':'https://cleanapp.io'},follow_redirects=False,**kwargs)

    def test_every_data_static_and_unknown_route_requires_session(self):
        for route in ['/grepped/api/items','/grepped/api/summary','/grepped/media/CA-0001',
                      '/grepped/static/app.js','/grepped/static/style.css','/grepped/notes.txt',
                      '/grepped/api/reviews/123','/grepped/download-all','/docs','/openapi.json']:
            self.assertEqual(self.client.get(route).status_code,401,route)
        self.assertEqual(self.client.get('/grepped/',follow_redirects=False).status_code,303)
        login=self.client.get('/grepped/login')
        self.assertEqual(login.status_code,200)
        self.assertNotIn('test-only-evaluation-password',login.text)

    def test_html_form_policy_preserves_same_origin_without_relaxing_csrf(self):
        page=self.client.get('/grepped/login')
        self.assertEqual(page.headers['referrer-policy'],'same-origin')
        # Browsers may send Origin:null for no-referrer HTML form POSTs.
        # Keep null/missing/cross-site origins rejected; fix the page policy instead.
        for origin in ['null','https://evil.example']:
            response=self.client.post('/grepped/login',data={'password':'test-only-evaluation-password'},
                                      headers={'Origin':origin},follow_redirects=False)
            self.assertEqual(response.status_code,403)
        self.assertEqual(self.login().status_code,303)
        app_page=self.client.get('/grepped/')
        self.assertEqual(app_page.headers['referrer-policy'],'same-origin')
        logout=self.client.post('/grepped/logout',headers={'Origin':'https://evil.example'},follow_redirects=False)
        self.assertEqual(logout.status_code,403)
        self.assertEqual(self.client.get('/grepped/api/items').status_code,200)

    def test_cookie_flags_opacity_and_logout_revocation(self):
        response=self.login()
        self.assertEqual(response.status_code,303)
        cookie=response.headers['set-cookie']
        for attr in ['HttpOnly','Secure','SameSite=strict','Path=/grepped']:
            self.assertIn(attr,cookie)
        self.assertNotIn('Domain=',cookie)
        self.assertNotIn('password',cookie)
        token=self.client.cookies.get(COOKIE)
        self.assertGreaterEqual(len(token),40)
        self.assertNotIn(token,self.app.state.auth.sessions)
        self.assertEqual(self.client.get('/grepped/').status_code,200)
        self.assertEqual(self.client.post('/grepped/logout',headers={'Origin':'https://cleanapp.io'},follow_redirects=False).status_code,303)
        self.client.cookies.set(COOKIE,token)
        self.assertEqual(self.client.get('/grepped/api/items').status_code,401)

    def test_metadata_allowlist_and_original_unchanged_bytes(self):
        self.login()
        response=self.client.get('/grepped/api/items')
        self.assertEqual(response.status_code,200)
        content=response.text
        for value in ['private-note','secret-path','private_provenance','latitude','coarse_location','123456','never expose']:
            self.assertNotIn(value,content)
        self.assertEqual(response.json()['items'][0]['caption'],'<script>bad()</script>')
        original=self.client.get('/grepped/media/CA-0001')
        self.assertEqual(original.content,self.raw)
        self.assertEqual(original.headers['content-type'],'image/jpeg')
        self.assertIn('no-store',original.headers['cache-control'])
        summary=self.client.get('/grepped/api/summary').json()
        self.assertEqual(summary['corpus_counts']['total_report_records'],1000)
        self.assertNotIn('website_cached',json.dumps(summary))
        self.assertNotIn('private-owner-status',json.dumps(summary))

    def test_no_review_download_all_or_arbitrary_zip_member_routes(self):
        self.login()
        for route in ['/grepped/download-all','/grepped/notes.txt','/grepped/static/notes.txt','/grepped/media/CA-9999']:
            self.assertEqual(self.client.get(route).status_code,404)
        response=self.client.post('/grepped/api/reviews/123',json={},headers={'Origin':'https://cleanapp.io'})
        self.assertEqual(response.status_code,404)
        self.assertEqual(self.client.post('/grepped/api/items',headers={'Origin':'https://cleanapp.io'}).status_code,405)

    def test_host_origin_and_forwarded_headers_cannot_bypass_checks(self):
        self.assertEqual(self.client.get('/grepped/login',headers={'Host':'evil.example'}).status_code,403)
        self.assertEqual(self.client.post('/grepped/login',data={'password':'x'},headers={'Origin':'https://evil.example'}).status_code,403)
        self.assertEqual(self.client.post('/grepped/login',data={'password':'x'}).status_code,403)
        self.assertEqual(self.client.get('/grepped/api/items',headers={'X-Forwarded-For':'127.0.0.1','X-Real-IP':'127.0.0.1'}).status_code,401)

    def test_login_rate_limit_and_expiration(self):
        for _ in range(5):
            self.assertEqual(self.login('wrong').status_code,401)
        self.assertEqual(self.login().status_code,429)
        self.now[0]+=901
        self.assertEqual(self.login().status_code,303)
        self.now[0]+=7201
        self.assertEqual(self.client.get('/grepped/api/items').status_code,401)

    def test_global_throttle_remains_bounded_and_recovers(self):
        auth=self.app.state.auth
        for i in range(30):
            self.assertEqual(auth.login(str(i),'wrong')[1],401)
        self.assertEqual(auth.login('new-address','wrong')[1],429)
        self.assertEqual(auth.login('another-address','wrong')[1],429)
        self.assertLessEqual(len(auth.attempts),30)
        self.now[0]+=901
        self.assertEqual(auth.login('new-address','test-only-evaluation-password')[1],200)

    def test_forged_cookie_and_http_transport_do_not_authenticate(self):
        self.client.cookies.set(COOKIE,'forged')
        self.assertEqual(self.client.get('/grepped/api/items').status_code,401)
        self.login()
        self.assertEqual(self.client.get('http://cleanapp.io/grepped/api/items').status_code,401)

    def test_login_body_validation_and_security_headers(self):
        self.assertEqual(self.client.post('/grepped/login',content='x'*2200,headers={'Origin':'https://cleanapp.io','Content-Type':'application/x-www-form-urlencoded'}).status_code,413)
        self.assertEqual(self.client.post('/grepped/login',json={'password':'x'},headers={'Origin':'https://cleanapp.io'}).status_code,415)
        self.assertEqual(self.client.post('/grepped/login',content='password=x&password=y',headers={'Origin':'https://cleanapp.io','Content-Type':'application/x-www-form-urlencoded'}).status_code,400)
        r=self.client.get('/grepped/api/items')
        self.assertEqual(r.headers['x-robots-tag'],'noindex, nofollow, noarchive')
        self.assertIn("frame-ancestors 'none'",r.headers['content-security-policy'])
        self.assertIn('no-store',r.headers['cache-control'])

    def test_wrong_archive_hash_and_unsafe_image_path_fail_closed(self):
        with self.assertRaisesRegex(ValueError,'hash mismatch'):
            create_app(Config(self.path,'0'*64,self.password_hash,expected_count=1))
        self.app.state.bundle.zip.close();self.app.state.bundle.file.close()
        self.write_bundle(image_path='../secret.jpg')
        with self.assertRaisesRegex(ValueError,'Unsafe'):
            create_app(Config(self.path,self.sha,self.password_hash,expected_count=1))

    def test_changed_bundle_not_served_after_login(self):
        self.login()
        with self.path.open('ab') as handle:
            handle.write(b'changed')
        self.assertEqual(self.client.get('/grepped/media/CA-0001').status_code,409)

    def test_wrong_saved_image_digest_fails_on_original_route(self):
        self.app.state.bundle.zip.close();self.app.state.bundle.file.close()
        self.write_bundle(sha256='0'*64)
        self.app=create_app(Config(self.path,self.sha,self.password_hash,expected_count=1))
        self.client=TestClient(self.app,base_url='https://cleanapp.io')
        self.login()
        self.assertEqual(self.client.get('/grepped/media/CA-0001').status_code,409)


if __name__=='__main__':
    unittest.main()
