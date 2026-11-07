"""Read-only production duplicate planning; never creates/writes MySQL objects.
Run only on trusted AWS host, with the existing local Docker MySQL client.
"""
import argparse,collections,datetime as dt,hashlib,json,pathlib,time
from candidate_data import require_aws_linux,json_loads,sha_file,SOURCE,EXPECTED_COUNT,sql_string
from mysql_client import Mysql

def headers(path):
    rows=[];keys=set();hashes=set();sources=collections.Counter()
    with pathlib.Path(path).open() as f:
        for line in f:
            r=json_loads(line);im=r['image'];sha=im['sha256']
            if not r['eligible_projection'] or r['review_issues'] or not im['structural_image_valid'] or not im['exact_photo_join']:raise ValueError('Unverified candidate')
            if r['source_key'] in keys or sha in hashes or r['source_key']!=SOURCE+'/metadata/'+r['legacy_id']:raise ValueError('Duplicate/invalid candidate identity')
            keys.add(r['source_key']);hashes.add(sha);sources[r['legacy_source']]+=1
            rows.append({'source_key':r['source_key'],'source_id':'metadata/'+r['legacy_id'],'sha256':sha,'bytes':im['bytes']})
    if len(rows)!=EXPECTED_COUNT or dict(sources)!={'cleanapp':4543,'litterbux':1096}:raise ValueError('Candidate count/source mismatch')
    return rows

def system_status(db):
    rows=db.query("SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_running','Threads_connected','Innodb_data_read','Innodb_buffer_pool_reads','Questions')")
    return dict(r.split('\t',1) for r in rows)

def main():
    require_aws_linux();p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--candidates',required=True);p.add_argument('--expected-candidates-sha256',required=True)
    p.add_argument('--out-dir',required=True);p.add_argument('--probe-rows',type=int,default=100)
    p.add_argument('--scan-uncovered',action='store_true');p.add_argument('--batch-size',type=int,default=250)
    p.add_argument('--sleep-seconds',type=float,default=0.25);p.add_argument('--max-seconds',type=int,default=600)
    args=p.parse_args();out=pathlib.Path(args.out_dir);out.mkdir(parents=True,exist_ok=True,mode=0o700)
    if sha_file(args.candidates)!=args.expected_candidates_sha256:raise ValueError('Candidates digest mismatch')
    candidates=headers(args.candidates);candidate_hashes={x['sha256'] for x in candidates}
    with Mysql(readonly=True) as db:
        server=db.query('SELECT @@server_uuid,@@version,DATABASE(),@@transaction_isolation')[0].split('\t')
        cutoff=int(db.scalar('SELECT COALESCE(MAX(seq),0) FROM reports'))
        before=system_status(db);started=time.monotonic();matches=[]
        for offset in range(0,len(candidates),100):
            hashes=[x['sha256'] for x in candidates[offset:offset+100]]
            literals=','.join(sql_string(x) for x in hashes)
            for line in db.query('SELECT ca.seq,LOWER(ca.sha256),ca.byte_size FROM curation_assets ca FORCE INDEX(idx_sha) JOIN reports r ON r.seq=ca.seq WHERE ca.sha256 IN ('+literals+')'):
                seq,sha,size=line.split('\t');matches.append({'seq':int(seq),'sha256':sha,'bytes':int(size),'basis':'curation_assets_original_or_current','candidate_match':True})
            # Digital share attachments carry a direct report_seq; cached blobs without report links are not duplicate reports.
            for line in db.query('SELECT da.report_seq,LOWER(da.sha256),OCTET_LENGTH(da.image) FROM digital_share_attachments da JOIN reports r ON r.seq=da.report_seq WHERE da.sha256 IN ('+literals+')'):
                seq,sha,size=line.split('\t');matches.append({'seq':int(seq),'sha256':sha,'bytes':int(size),'basis':'digital_share_attachment','candidate_match':True})
        lookup_seconds=time.monotonic()-started
        predicate="(ca.sha256 IS NULL OR ca.sha256='')"
        sql='SELECT r.seq FROM reports r FORCE INDEX(PRIMARY) LEFT JOIN curation_assets ca ON ca.seq=r.seq AND ca.sha256 IS NOT NULL WHERE r.seq>0 AND r.seq<='+str(cutoff)+' AND ca.seq IS NULL ORDER BY r.seq LIMIT '+str(args.probe_rows)
        began=time.monotonic();probe=[]
        probe_ids=[int(x) for x in db.query(sql)]
        hash_sql='SELECT seq,OCTET_LENGTH(image),LOWER(SHA2(image,256)) FROM reports FORCE INDEX(PRIMARY) WHERE seq IN ('+','.join(str(x) for x in probe_ids)+') ORDER BY seq' if probe_ids else None
        for line in (db.query(hash_sql) if hash_sql else []):
            seq,size,sha=line.split('\t');probe.append({'seq':int(seq),'bytes':int(size),'sha256':sha})
        probe_seconds=time.monotonic()-began
        for row in probe:
            if row['sha256'] in candidate_hashes:matches.append({**row,'basis':'current_report_blob_probe','candidate_match':True})
        result={'read_only':True,'server_uuid':server[0],'server_version':server[1],'database':server[2],'isolation':server[3],
            'cutoff_seq':cutoff,'candidate_count':len(candidates),'candidate_sha256':args.expected_candidates_sha256,
            'indexed_lookup_seconds':round(lookup_seconds,3),'indexed_match_count':len(matches),
            'probe_rows':len(probe),'probe_bytes':sum(x['bytes'] for x in probe),'probe_seconds':round(probe_seconds,3),
            'probe_max_bytes':max((x['bytes'] for x in probe),default=0),'probe_last_seq':probe[-1]['seq'] if probe else 0,
            'system_status_before':before,'system_status_after':system_status(db),
            'complete_global_hash_coverage':False,'created_at_utc':dt.datetime.now(dt.timezone.utc).isoformat()}
        (out/'indexed_and_probe_matches.json').write_text(json.dumps(matches,indent=2)+'\n')
        (out/'probe_rows.json').write_text(json.dumps(probe,indent=2)+'\n')
        (out/'dedup_probe_summary.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps(result,indent=2),flush=True)
        db.query('ROLLBACK;')
        if not args.scan_uncovered:return
        scan_started=time.monotonic();last=0;seen=0;scanned_bytes=0;scan_matches=[];complete=False
        with (out/'uncovered_report_hashes.jsonl').open('x') as w:
            while time.monotonic()-scan_started<args.max_seconds:
                # Short read-only transactions keep snapshot/undo lifetime bounded.
                db.query('START TRANSACTION READ ONLY;')
                sql='SELECT r.seq FROM reports r FORCE INDEX(PRIMARY) LEFT JOIN curation_assets ca ON ca.seq=r.seq AND ca.sha256 IS NOT NULL WHERE r.seq>'+str(last)+' AND r.seq<='+str(cutoff)+' AND ca.seq IS NULL ORDER BY r.seq LIMIT '+str(args.batch_size)
                start=time.monotonic();ids=[int(x) for x in db.query(sql)]
                hash_sql='SELECT seq,OCTET_LENGTH(image),LOWER(SHA2(image,256)) FROM reports FORCE INDEX(PRIMARY) WHERE seq IN ('+','.join(str(x) for x in ids)+') ORDER BY seq' if ids else None
                rows=db.query(hash_sql) if hash_sql else [];db.query('ROLLBACK;')
                for line in rows:
                    seq,size,sha=line.split('\t');row={'seq':int(seq),'bytes':int(size),'sha256':sha};w.write(json.dumps(row)+'\n');seen+=1;scanned_bytes+=int(size);last=int(seq)
                    if sha in candidate_hashes:scan_matches.append({**row,'basis':'current_report_blob_uncovered','candidate_match':True})
                w.flush()
                if len(rows)<args.batch_size:complete=True;break
                if seen%10000<args.batch_size:print(json.dumps({'scanned_rows':seen,'scanned_bytes':scanned_bytes,'last_seq':last,'seconds':round(time.monotonic()-scan_started,1),'last_batch_seconds':round(time.monotonic()-start,3)}),flush=True)
                time.sleep(args.sleep_seconds)
        result.update({'uncovered_scan_complete':complete,'uncovered_scanned_rows':seen,'uncovered_scanned_bytes':scanned_bytes,'uncovered_last_seq':last,'uncovered_seconds':round(time.monotonic()-scan_started,3),'uncovered_match_count':len(scan_matches)})
        (out/'uncovered_matches.json').write_text(json.dumps(scan_matches,indent=2)+'\n');(out/'dedup_scan_summary.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps(result,indent=2),flush=True)
if __name__=='__main__':main()
