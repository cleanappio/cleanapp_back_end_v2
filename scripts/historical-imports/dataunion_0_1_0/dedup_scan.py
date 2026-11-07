"""Checkpointed read-only production duplicate scan; artifact writes are host-local.
Root-authorized adaptive limits: <=100 real-photo rows, larger only after measured
empty/tiny batches; <=30MiB/s photo read average; 5s SQL deadline; 20min chunks.
"""
import argparse,collections,datetime as dt,json,math,os,pathlib,re,sqlite3,time
from candidate_data import require_aws_linux,sha_file,sql_string
from dedup_readonly import headers,system_status
from mysql_client import Mysql
EXPECTED_SERVER='f2c484f6-3c5d-11ef-aace-0242ac120003'

def fresh_ref(sha,checked,gdpr):
    return isinstance(sha,str) and bool(re.fullmatch('[0-9a-f]{64}',sha)) and bool(checked) and (not gdpr or checked>=gdpr)

def main():
    require_aws_linux();p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--candidates',required=True);p.add_argument('--candidate-sha256',required=True);p.add_argument('--out-dir',required=True)
    p.add_argument('--budget-seconds',type=int,default=1200);args=p.parse_args()
    if sha_file(args.candidates)!=args.candidate_sha256:raise ValueError('Candidate checksum mismatch')
    candidates=headers(args.candidates);hash_set={x['sha256'] for x in candidates};out=pathlib.Path(args.out_dir);out.mkdir(exist_ok=True,parents=True,mode=0o700)
    cache=sqlite3.connect(out/'coverage.sqlite3');cache.execute('PRAGMA journal_mode=WAL');cache.execute('CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY,value TEXT NOT NULL)')
    cache.execute('CREATE TABLE IF NOT EXISTS coverage (seq INTEGER PRIMARY KEY,ref_sha TEXT,ref_bytes INTEGER,ref_checked TEXT,gdpr_processed TEXT,status TEXT NOT NULL,current_sha TEXT,current_bytes INTEGER)')
    cache.execute('CREATE INDEX IF NOT EXISTS coverage_status_seq ON coverage(status,seq)');cache.commit()
    def put(k,v):cache.execute('INSERT OR REPLACE INTO meta VALUES(?,?)',(k,json.dumps(v)))
    def get(k,default=None):
        r=cache.execute('SELECT value FROM meta WHERE key=?',(k,)).fetchone();return json.loads(r[0]) if r else default
    db=Mysql(readonly=True)
    try:
        server=db.scalar('SELECT @@server_uuid')
        if server!=EXPECTED_SERVER:raise ValueError('Unexpected production DB')
        if get('candidate_sha256',args.candidate_sha256)!=args.candidate_sha256:raise ValueError('Resume belongs to another candidate set')
        if get('server_uuid',server)!=server:raise ValueError('Resume belongs to another server')
        put('server_uuid',server);put('candidate_sha256',args.candidate_sha256)
        cutoff=get('cutoff_seq')
        if cutoff is None:
            cutoff=int(db.scalar('SELECT COALESCE(MAX(seq),0) FROM reports'));put('cutoff_seq',cutoff)
            put('started_at_utc',dt.datetime.now(dt.timezone.utc).isoformat());cache.commit()
        db.query('ROLLBACK;')
        if not get('header_complete',False):
            last=get('header_last_seq',0);count=get('header_count',0);at=time.monotonic()
            while True:
                db.query('START TRANSACTION READ ONLY;')
                sql="SELECT /*+ MAX_EXECUTION_TIME(5000) */ r.seq,COALESCE(LOWER(ca.sha256),''),COALESCE(ca.byte_size,-1),COALESCE(DATE_FORMAT(ca.checked_at,'%Y-%m-%d %H:%i:%s'),''),COALESCE(DATE_FORMAT(g.processed_at,'%Y-%m-%d %H:%i:%s'),'') FROM reports r FORCE INDEX(PRIMARY) LEFT JOIN curation_assets ca ON ca.seq=r.seq LEFT JOIN reports_gdpr g ON g.seq=r.seq WHERE r.seq>"+str(last)+' AND r.seq<='+str(cutoff)+' ORDER BY r.seq LIMIT 5000'
                values=db.query(sql);db.query('ROLLBACK;')
                for value in values:
                    seq,sha,size,checked,gdpr=value.split('\t');seq=int(seq)
                    status='covered_reference' if fresh_ref(sha,checked,gdpr) else 'pending'
                    cache.execute('INSERT OR IGNORE INTO coverage(seq,ref_sha,ref_bytes,ref_checked,gdpr_processed,status) VALUES(?,?,?,?,?,?)',(seq,sha or None,int(size),checked or None,gdpr or None,status));last=seq;count+=1
                put('header_last_seq',last);put('header_count',count);cache.commit()
                if len(values)<5000:break
            put('header_complete',True);cache.commit()
            print(json.dumps({'phase':'headers_complete','headers':count,'seconds':round(time.monotonic()-at,2),'status_counts':dict(cache.execute('SELECT status,COUNT(*) FROM coverage GROUP BY status'))}),flush=True)
        # Cached original hash matches remain a duplicate even if GDPR changed current bytes.
        indexed=[];db.query('START TRANSACTION READ ONLY;')
        for offset in range(0,len(candidates),100):
            literals=','.join(sql_string(x['sha256']) for x in candidates[offset:offset+100])
            for line in db.query('SELECT ca.seq,LOWER(ca.sha256),ca.byte_size,IF(r.seq IS NULL,0,1) FROM curation_assets ca FORCE INDEX(idx_sha) LEFT JOIN reports r ON r.seq=ca.seq WHERE ca.sha256 IN ('+literals+')'):
                seq,sha,size,exists=line.split('\t');indexed.append({'seq':int(seq),'sha256':sha,'bytes':int(size),'basis':'curation_original_or_current','target_exists':exists=='1'})
            for line in db.query('SELECT da.report_seq,LOWER(da.sha256),OCTET_LENGTH(da.image) FROM digital_share_attachments da JOIN reports r ON r.seq=da.report_seq WHERE da.sha256 IN ('+literals+')'):
                seq,sha,size=line.split('\t');indexed.append({'seq':int(seq),'sha256':sha,'bytes':int(size),'basis':'digital_share_attachment','target_exists':True})
        db.query('ROLLBACK;');put('indexed_matches',indexed);cache.commit()
        start=time.monotonic();last_progress=start;last_pressure=start;batch_size=get('next_batch_size',25);tiny_streak=0;chunk_bytes=0;chunk_rows=0;initial_status=system_status(db)
        while time.monotonic()-start<args.budget_seconds:
            pending=[r[0] for r in cache.execute("SELECT seq FROM coverage WHERE status='pending' ORDER BY seq LIMIT ?",(batch_size,))]
            if not pending:break
            begin=time.monotonic()
            sql='SELECT /*+ MAX_EXECUTION_TIME(5000) */ seq,OCTET_LENGTH(image),LOWER(SHA2(image,256)) FROM reports FORCE INDEX(PRIMARY) WHERE seq IN ('+','.join(str(x) for x in pending)+') ORDER BY seq'
            try:
                db.query('START TRANSACTION READ ONLY;');values=db.query(sql,timeout=15);db.query('ROLLBACK;')
            except (RuntimeError,TimeoutError) as error:
                db.close();db=Mysql(readonly=True);db.query('ROLLBACK;')
                if '3024' not in str(error) and 'execution' not in str(error).lower() and not isinstance(error,TimeoutError):raise
                batch_size=max(1,batch_size//2);tiny_streak=0;put('next_batch_size',batch_size);cache.commit();time.sleep(max(0.5,(time.monotonic()-begin)*4))
                print(json.dumps({'phase':'deadline_retry','next_batch_size':batch_size,'same_uncommitted_bounds':True}),flush=True);continue
            elapsed=time.monotonic()-begin;seen=set();byte_count=0
            for line in values:
                seq,size,sha=line.split('\t');seq=int(seq);size=int(size);seen.add(seq);byte_count+=size
                cache.execute("UPDATE coverage SET status='hashed',current_sha=?,current_bytes=? WHERE seq=? AND status='pending'",(sha,size,seq))
            for seq in set(pending)-seen:cache.execute("UPDATE coverage SET status='deleted_during_scan' WHERE seq=?",(seq,))
            chunk_rows+=len(pending);chunk_bytes+=byte_count
            tiny=byte_count<=8*1024**2 and elapsed<0.3
            if tiny:
                tiny_streak+=1
                if tiny_streak>=3:batch_size=min(5000,1000 if batch_size<1000 else 5000)
                pause=0.03
            else:
                tiny_streak=0
                if elapsed>3 or byte_count>128*1024**2:batch_size=max(1,min(100,batch_size//2))
                else:batch_size=min(100,batch_size)
                pause=max(0.5,byte_count/(30*1024**2)-elapsed)
            put('next_batch_size',batch_size);cache.commit()
            if time.monotonic()-last_pressure>=60:
                pressure=system_status(db);threads=int(pressure.get('Threads_running','0'));last_pressure=time.monotonic()
                load=os.getloadavg()[0]
                if threads>max(8,int(initial_status.get('Threads_running','0'))+4) or load>max(2,os.cpu_count() or 1)*1.5:pause=max(pause,5);batch_size=min(batch_size,25)
            if time.monotonic()-last_progress>=60 or chunk_rows%10000<len(pending):
                status=dict(cache.execute('SELECT status,COUNT(*) FROM coverage GROUP BY status'))
                progress={'phase':'hashing','chunk_rows':chunk_rows,'chunk_bytes':chunk_bytes,'chunk_seconds':round(time.monotonic()-start,1),'status_counts':status,'next_batch_size':batch_size,'last_batch_seconds':round(elapsed,3),'last_batch_bytes':byte_count,'pause_seconds':round(pause,2)}
                (out/'progress.json').write_text(json.dumps(progress,indent=2)+'\n');print(json.dumps(progress),flush=True);last_progress=time.monotonic()
            time.sleep(pause)
        status=dict(cache.execute('SELECT status,COUNT(*) FROM coverage GROUP BY status'));pending_count=status.get('pending',0)
        matches=list(indexed)
        for seq,sha,size in cache.execute("SELECT seq,current_sha,current_bytes FROM coverage WHERE status='hashed'"):
            if sha in hash_set:matches.append({'seq':seq,'sha256':sha,'bytes':size,'basis':'current_report_blob','target_exists':True})
        result={'server_uuid':server,'candidate_sha256':args.candidate_sha256,'cutoff_seq':cutoff,'header_count':get('header_count'),'status_counts':status,
            'complete_global_hash_coverage':pending_count==0,'coverage_definition':'All live PK headers through cutoff; exact current hashes for uncovered/stale-GDPR refs plus fresh curation original/current hashes; cached originals preserve duplicate/redaction evidence',
            'matches':matches,'chunk_rows':chunk_rows,'chunk_bytes':chunk_bytes,'chunk_seconds':round(time.monotonic()-start,2),'next_batch_size':batch_size,
            'system_status_before_chunk':initial_status,'system_status_after_chunk':system_status(db),'finished_at_utc':dt.datetime.now(dt.timezone.utc).isoformat()}
        (out/'dedup_manifest.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({k:v for k,v in result.items() if k!='matches'},indent=2),flush=True)
        cache.execute('PRAGMA wal_checkpoint(TRUNCATE)')
    finally:db.close();cache.close()
if __name__=='__main__':main()
