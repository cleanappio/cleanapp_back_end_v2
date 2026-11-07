"""Trusted data-only validation for the AWS historical import.
No supplied setup code is loaded or executed. This module never connects to a DB.
Runtime use is confined to AWS Linux; local ast/syntax inspection is harmless.
"""
from __future__ import annotations
import base64,collections,datetime as dt,decimal,hashlib,json,math,os,pathlib,platform,re

SOURCE='dataunion-0.1.0'
EXPECTED_COUNT=5639
EXPECTED_SOURCES={'cleanapp':4543,'litterbux':1096}
MAX_PHOTO_BYTES=128*1024*1024

def require_aws_linux():
    if platform.system()!='Linux':raise RuntimeError('Run data processing on the authorized AWS Linux host only')
    vendor=pathlib.Path('/sys/class/dmi/id/sys_vendor').read_text().strip()
    if vendor!='Amazon EC2':raise RuntimeError('Expected Amazon EC2 host; fail closed')

def no_constants(value):raise ValueError('Non-finite JSON constant: '+value)
def unique_pairs(pairs):
    result={}
    for k,v in pairs:
        if k in result:raise ValueError('Duplicate JSON key')
        result[k]=v
    return result

def json_loads(s):return json.loads(s,parse_constant=no_constants,object_pairs_hook=unique_pairs)
def canonical(value):return json.dumps(value,ensure_ascii=False,sort_keys=True,separators=(',',':'),allow_nan=False)
def sha_file(path):
    h=hashlib.sha256()
    with pathlib.Path(path).open('rb') as f:
        for b in iter(lambda:f.read(1024*1024),b''):h.update(b)
    return h.hexdigest()
def sql_string(value):
    if value is None:return 'NULL'
    if not isinstance(value,str):raise TypeError('SQL text literal requires str')
    return "_utf8mb4 X'"+value.encode('utf-8').hex()+"'"
def sql_bytes(value):return "X'"+value.hex()+"'"
def sql_num(value):
    if isinstance(value,bool):raise ValueError('Boolean is not numeric data')
    value=decimal.Decimal(str(value))
    if not value.is_finite():raise ValueError('Non-finite SQL number')
    return format(value,'f')

def load_candidates(path,media_root,expected_sha256=None,verify_photos=True):
    path=pathlib.Path(path).resolve(strict=True);media_root=pathlib.Path(media_root).resolve(strict=True)
    if expected_sha256 and sha_file(path)!=expected_sha256:raise ValueError('Candidate JSONL SHA256 mismatch')
    result=[];ids=set();hashes=set();sources=collections.Counter()
    with path.open(encoding='utf-8') as f:
        for number,line in enumerate(f,1):
            if len(line.encode('utf-8'))>16*1024*1024:raise ValueError('Oversized JSON line')
            row=json_loads(line)
            if not isinstance(row,dict) or not row.get('eligible_projection') or row.get('review_issues'):
                raise ValueError(f'Unreviewed/ineligible candidate at line {number}')
            legacy_id=row.get('legacy_id');source_key=row.get('source_key');source=row.get('legacy_source')
            if row.get('version_label')!='0.1.0' or row.get('legacy_database')!='metadata' or source not in EXPECTED_SOURCES:
                raise ValueError('Unexpected version/database/source')
            if not isinstance(legacy_id,str) or not legacy_id or len(legacy_id)>200 or source_key!=SOURCE+'/metadata/'+legacy_id:
                raise ValueError('Invalid source identity')
            if source_key in ids:raise ValueError('Duplicate source key in candidates')
            ids.add(source_key);sources[source]+=1
            original=row.get('original_record',{}).get('doc',{})
            if original.get('_id')!=legacy_id or original.get('source')!=source or original.get('type')!='image':
                raise ValueError('Original identity/source/type mismatch')
            location=row.get('location',{});selected=location.get('selected') or {}
            lat,lng=selected.get('latitude'),selected.get('longitude')
            if any(isinstance(x,bool) or not isinstance(x,(int,float)) or not math.isfinite(x) for x in (lat,lng)):
                raise ValueError('Invalid numeric coordinate')
            if not -90<=lat<=90 or not -180<=lng<=180 or (lat==0 and lng==0) or location.get('conflicting'):
                raise ValueError('Missing, conflicting, out-of-range or 0,0 coordinate')
            upload=dt.datetime.fromisoformat(row['uploaded_at_utc'].replace('Z','+00:00'))
            if upload.utcoffset()!=dt.timedelta(0):raise ValueError('Upload time is not explicit UTC')
            raw_unix=decimal.Decimal(str(row['uploaded_at_raw']))
            if not raw_unix.is_finite() or abs(raw_unix-decimal.Decimal(str(upload.timestamp())))>decimal.Decimal('0.000002'):
                raise ValueError('Original Unix and normalized upload timestamps disagree')
            if not dt.datetime(2023,1,1,tzinfo=dt.timezone.utc)<=upload<dt.datetime(2024,1,1,tzinfo=dt.timezone.utc):
                raise ValueError('Unexpected historical upload year')
            if original.get('uploaded_at')!=row['uploaded_at_raw'] or not row.get('legacy_uploader_reference'):
                raise ValueError('Missing uploader or altered source timestamp')
            media=row.get('image',{});sha=media.get('sha256');fmt=media.get('format')
            if not re.fullmatch('[0-9a-f]{64}',str(sha)) or sha in hashes:raise ValueError('Invalid/duplicate image SHA256')
            hashes.add(sha)
            if not media.get('exact_photo_join') or not media.get('structural_image_valid') or media.get('zip_member')!=original.get('image_path') or row.get('image_path')!=original.get('image_path'):
                raise ValueError('Photo is not an exact verified original-path match')
            if fmt not in ('JPEG','PNG'):raise ValueError('Unexpected image type')
            expected=media_root/(sha+('.jpg' if fmt=='JPEG' else '.png'))
            locator=expected.resolve(strict=True)
            if expected.is_symlink() or not locator.is_relative_to(media_root) or not locator.is_file():
                raise ValueError('Image locator is not confined content-addressed file')
            size=locator.stat().st_size
            if not 0<size<=MAX_PHOTO_BYTES or size!=media.get('bytes'):raise ValueError('Image byte-size mismatch')
            if verify_photos and sha_file(locator)!=sha:raise ValueError('Actual photo SHA256 mismatch')
            row['_import']={'source_id':'metadata/'+legacy_id,'photo_path':str(locator),'photo_bytes':size,
                'photo_sha256':sha,'uploaded_utc_seconds':upload.replace(microsecond=0,tzinfo=None).isoformat(sep=' '),
                'uploaded_utc_microseconds':upload.replace(tzinfo=None).isoformat(sep=' ',timespec='microseconds'),
                'uploaded_unix_decimal':str(raw_unix),
                'reporter_id':'historical:dataunion-0.1.0:'+hashlib.sha256(row['legacy_uploader_reference'].encode()).hexdigest(),
                'public_id':'rpt_'+base64.urlsafe_b64encode(hashlib.sha256(source_key.encode()).digest()[:16]).decode().rstrip('=')}
            result.append(row)
    if len(result)!=EXPECTED_COUNT or dict(sources)!=EXPECTED_SOURCES:raise ValueError('Confirmed candidate count/source split mismatch')
    return result
