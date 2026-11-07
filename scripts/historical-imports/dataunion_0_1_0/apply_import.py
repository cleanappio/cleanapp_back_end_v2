"""Trusted bounded historical importer. Root/maintainer is the sole apply operator.
No HTTP APIs, queues, emails, notifications, account creation, or rewards are used.
This program needs a reviewed live policy deployment and complete dedup manifest.

Do not run in the recovery VM. Run on the trusted AWS production/import host with
photos already transferred as data; database secrets stay in cleanapp_db.
"""
import argparse,datetime as dt,decimal,hashlib,json,math,os,pathlib,time
from candidate_data import require_aws_linux,load_candidates,sha_file,canonical,sql_string,sql_bytes,sql_num,SOURCE,EXPECTED_COUNT
from mysql_client import Mysql

POLICY_VERSION=1
POLICY_FLAGS=('suppress_email','suppress_notifications','suppress_rewards','suppress_routing','suppress_analysis')
EXPECTED_SERVER_UUID='f2c484f6-3c5d-11ef-aace-0242ac120003'

def expected_schema(db):
    needed={'reports':{'seq':'int','ts':'timestamp','id':'varchar','team':'int','latitude':'float','longitude':'float','image':'longblob'},
        'report_raw':{'report_seq':'int','fetcher_id':'varchar','source_id':'varchar','visibility':'varchar'},
        'external_ingest_index':{'source':'varchar','external_id':'varchar','seq':'int','source_timestamp':'datetime'},
        'report_analysis':{'seq':'int','source':'varchar','title':'varchar','description':'text','severity_level':'float','is_valid':'tinyint','inferred_contact_emails':'text'},
        'report_side_effect_policy':{'report_seq':'int','source_namespace':'varchar','policy_version':'int',**{x:'tinyint' for x in POLICY_FLAGS}},
        'historical_report_import_runs':{'run_id':'char','importer_sha256':'char','candidate_sha256':'char','approved_controls_json':'json'},
        'historical_report_import_ledger':{'source_namespace':'varchar','source_id':'varchar','report_seq':'int','photo_sha256':'char','original_record_json':'json'}}
    literal=','.join(sql_string(x) for x in needed)
    rows=db.query('SELECT TABLE_NAME,COLUMN_NAME,DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN ('+literal+')')
    got={}
    for row in rows:
        table,col,typ=row.split('\t');got.setdefault(table,{})[col]=typ
    for table,columns in needed.items():
        for col,typ in columns.items():
            if got.get(table,{}).get(col)!=typ:raise ValueError('Live schema mismatch: '+table+'.'+col)
    # The importer deliberately honors native TIMESTAMP(0); full original precision
    # is retained in DATETIME(6), decimal-string and complete original JSON.
    precision=db.scalar("SELECT COALESCE(DATETIME_PRECISION,-1) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='reports' AND COLUMN_NAME='ts'")
    if precision!='0':raise ValueError('reports.ts precision changed; review timestamp projection')

def check_existing(db,row):
    info=row['_import'];key=sql_string(info['source_id']);ns=sql_string(SOURCE)
    result=db.query('SELECT disposition,COALESCE(report_seq,0),photo_sha256,recorded_report_seq,HEX(original_record_json),HEX(normalized_record_json) FROM historical_report_import_ledger WHERE source_namespace='+ns+' AND source_id='+key)
    if result:
        disposition,seq,sha,recorded,original_hex,normalized_hex=result[0].split('\t')
        if len(result)!=1 or sha!=info['photo_sha256'] or not int(seq):raise ValueError('Conflicting/deleted import ledger identity; no resurrection')
        retained_original=json.loads(bytes.fromhex(original_hex).decode())
        retained_normalized=json.loads(bytes.fromhex(normalized_hex).decode())
        if retained_original!=row['original_record'] or retained_normalized!={k:v for k,v in row.items() if k!='_import'}:
            raise ValueError('Changed source payload under an existing source identity')
        return {'disposition':disposition,'seq':int(seq),'basis':'existing_import_ledger'}
    existing=db.query('SELECT rr.report_seq,LOWER(SHA2(r.image,256)) FROM report_raw rr JOIN reports r ON r.seq=rr.report_seq WHERE rr.fetcher_id='+ns+' AND rr.source_id='+key)
    if existing:raise ValueError('Source identity exists without audit ledger; reconcile rather than overwrite')
    public=db.query('SELECT seq FROM reports WHERE public_id='+sql_string(info['public_id']))
    if public:raise ValueError('Deterministic public ID exists without source ledger; reconcile before apply')
    return None

def ledger_insert(row,run_id,seq_expr,disposition,duplicate_basis=None,public_expr=None):
    i=row['_import'];clean={k:v for k,v in row.items() if k!='_import'}
    return 'INSERT INTO historical_report_import_ledger (source_namespace,source_id,run_id,disposition,report_seq,recorded_report_seq,report_public_id,photo_sha256,photo_bytes,original_uploaded_at,original_latitude,original_longitude,original_uploaded_unix,legacy_uploader_reference,projection_method,severity_assessed,duplicate_basis,original_record_json,normalized_record_json) VALUES ('+','.join([
        sql_string(SOURCE),sql_string(i['source_id']),sql_string(run_id),sql_string(disposition),seq_expr,seq_expr,public_expr or 'NULL',
        sql_string(i['photo_sha256']),str(i['photo_bytes']),sql_string(i['uploaded_utc_microseconds']),sql_num(row['location']['selected']['latitude']),sql_num(row['location']['selected']['longitude']),sql_string(i['uploaded_unix_decimal']),
        sql_string(row['legacy_uploader_reference']),sql_string('metadata_only'),'0',sql_string(duplicate_basis),
        sql_string(canonical(row['original_record'])),sql_string(canonical(clean))])+');'

def insert_sql(row,run_id):
    i=row['_import'];location=row['location']['selected'];photo=pathlib.Path(i['photo_path']).read_bytes()
    if len(photo)!=i['photo_bytes'] or hashlib.sha256(photo).hexdigest()!=i['photo_sha256']:raise ValueError('Photo changed immediately before insert')
    upload=i['uploaded_utc_seconds'];date_label=row['uploaded_at_utc']
    statements=['INSERT INTO reports (public_id,ts,id,team,latitude,longitude,x,y,image,action_id,description) VALUES ('+','.join([
        sql_string(i['public_id']),sql_string(upload),sql_string(i['reporter_id']),'0',sql_num(location['latitude']),sql_num(location['longitude']),'0','0',sql_bytes(photo),'NULL','NULL'])+');',
        'SET @historical_seq=LAST_INSERT_ID();',
        'INSERT INTO report_side_effect_policy (report_seq,source_namespace,reason,policy_version,'+','.join(POLICY_FLAGS)+') VALUES (@historical_seq,'+sql_string(SOURCE)+','+sql_string('User-authorized historical recovery; no email, notification, routing, analysis, or reward side effects')+',1,1,1,1,1,1);',
        'INSERT INTO report_raw (report_seq,fetcher_id,source_id,agent_id,agent_version,collected_at,source_type,visibility,trust_level,spam_score) VALUES (@historical_seq,'+','.join([
            sql_string(SOURCE),sql_string(i['source_id']),sql_string('cleanapp-historical-recovery'),sql_string('0.1.0'),sql_string(upload),sql_string('historical_import'),sql_string('public'),sql_string('unverified'),'0'])+');',
        'INSERT INTO external_ingest_index (source,external_id,seq,source_timestamp,source_url) VALUES ('+sql_string(SOURCE)+','+sql_string(i['source_id'])+',@historical_seq,'+sql_string(upload)+',NULL);',
        'INSERT INTO report_analysis (seq,source,title,description,severity_level,language,brand_name,brand_display_name,is_valid,classification,needs_ai_review,inferred_contact_emails) VALUES (@historical_seq,'+','.join([
            sql_string('dataunion-0.1.0-recovery'),sql_string('Historical CleanApp report'),sql_string('Original photo report submitted on '+date_label+'.'),'0',sql_string('en'),sql_string(''),sql_string(''),'1',sql_string('physical'),'0',sql_string('[]')])+');',
        ledger_insert(row,run_id,'@historical_seq','inserted',public_expr=sql_string(i['public_id']))]
    return '\n'.join(statements)

def main():
    require_aws_linux();p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--candidates',required=True);p.add_argument('--candidate-sha256',required=True);p.add_argument('--media-root',required=True)
    p.add_argument('--dedup-manifest',required=True);p.add_argument('--dedup-manifest-sha256',required=True)
    p.add_argument('--approved-controls',required=True,help='Root-reviewed deployment/tests attestation, no secrets')
    p.add_argument('--mode',choices=('plan','apply'),default='plan');p.add_argument('--batch-size',type=int,default=5)
    p.add_argument('--max-batch-photo-bytes',type=int,default=16*1024*1024);p.add_argument('--out-dir',required=True)
    args=p.parse_args();out=pathlib.Path(args.out_dir);out.mkdir(parents=True,exist_ok=True,mode=0o700)
    if not 1<=args.batch_size<=10 or not 1<=args.max_batch_photo_bytes<=24*1024*1024:raise ValueError('Unbounded batch settings rejected')
    rows=load_candidates(args.candidates,args.media_root,args.candidate_sha256,verify_photos=True)
    if sha_file(args.dedup_manifest)!=args.dedup_manifest_sha256:raise ValueError('Dedup manifest digest mismatch')
    manifest=json.loads(pathlib.Path(args.dedup_manifest).read_text())
    if manifest.get('candidate_sha256')!=args.candidate_sha256 or manifest.get('server_uuid')!=EXPECTED_SERVER_UUID or not manifest.get('complete_global_hash_coverage'):
        raise ValueError('Incomplete/wrong-server duplicate evidence; fail closed')
    controls=json.loads(pathlib.Path(args.approved_controls).read_text())
    if controls.get('policy_version')!=1 or not controls.get('live_guards_verified') or not all(controls.get(x) is True for x in POLICY_FLAGS):
        raise ValueError('Live side-effect suppression was not explicitly verified')
    if controls.get('server_uuid')!=EXPECTED_SERVER_UUID:raise ValueError('Suppression attestation targets another DB')
    existing_hashes={}
    for hit in manifest.get('matches',[]):
        if hit.get('sha256'):
            existing_hashes.setdefault(hit['sha256'],hit)
    run_id=hashlib.sha256(canonical({'source':SOURCE,'candidates':args.candidate_sha256,'dedup':args.dedup_manifest_sha256,'controls':controls,'importer_sha256':sha_file(__file__)}).encode()).hexdigest()
    with Mysql() as db:
        if db.scalar('SELECT @@server_uuid')!=EXPECTED_SERVER_UUID:raise ValueError('Wrong production server')
        expected_schema(db)
        if db.scalar("SELECT GET_LOCK('cleanapp-dataunion-0.1.0-import',0)")!='1':raise RuntimeError('Another importer holds the run lock')
        plan=[]
        for row in rows:
            prior=check_existing(db,row);hit=existing_hashes.get(row['_import']['photo_sha256'])
            if prior:plan.append({'source_id':row['_import']['source_id'],**prior,'action':'replay_skip'})
            elif hit:plan.append({'source_id':row['_import']['source_id'],'action':'duplicate_skip','seq':int(hit['seq']),'basis':hit['basis']})
            else:plan.append({'source_id':row['_import']['source_id'],'action':'insert'})
        (out/'import_plan.json').write_text(json.dumps({'run_id':run_id,'mode':args.mode,'rows':plan},indent=2)+'\n')
        if args.mode=='plan':print(json.dumps({'run_id':run_id,'insert':sum(x['action']=='insert' for x in plan),'duplicate_skip':sum(x['action']=='duplicate_skip' for x in plan),'replay_skip':sum(x['action']=='replay_skip' for x in plan)}));return
        # No DDL is run here. Root must deploy reviewed DDL/guards before apply.
        db.query('INSERT INTO historical_report_import_runs (run_id,source_namespace,importer_sha256,candidate_sha256,dedup_manifest_sha256,candidate_count,suppression_policy_version,approved_controls_json) VALUES ('+','.join([sql_string(run_id),sql_string(SOURCE),sql_string(sha_file(__file__)),sql_string(args.candidate_sha256),sql_string(args.dedup_manifest_sha256),str(len(rows)),'1',sql_string(canonical(controls))])+') ON DUPLICATE KEY UPDATE run_id=run_id;')
        snapshot_cutoff=int(manifest['cutoff_seq']);observed_max_seq=snapshot_cutoff;done=0;observed_tail_ids=set()
        receipt=(out/'committed_batches.jsonl').open('a')
        try:
            at=0
            while at<len(rows):
                batch=[];total_bytes=0
                while at<len(rows) and len(batch)<args.batch_size:
                    row=rows[at];photo_bytes=row['_import']['photo_bytes']
                    if batch and total_bytes+photo_bytes>args.max_batch_photo_bytes:break
                    if photo_bytes>24*1024*1024:raise ValueError('Photo exceeds reviewed SQL packet bound')
                    batch.append(row);total_bytes+=photo_bytes;at+=1
                db.query('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ; START TRANSACTION;')
                try:
                    # Keep the pre-import cutoff fixed: other transactions can
                    # reserve lower auto-increment IDs, wait on our gap lock, and
                    # commit after our larger ID. Advancing past our own IDs
                    # would miss such late commits. Thin locked headers include
                    # the complete post-snapshot interval on every batch.
                    tail_ids=[int(x) for x in db.query('SELECT seq FROM reports FORCE INDEX(PRIMARY) WHERE seq>'+str(snapshot_cutoff)+' ORDER BY seq LIMIT '+str(EXPECTED_COUNT+1001)+' FOR UPDATE')]
                    if len(tail_ids)>=EXPECTED_COUNT+1001:raise ValueError('Duplicate snapshot tail is stale; rescan before apply')
                    owned_ids={int(x) for x in db.query('SELECT report_seq FROM historical_report_import_ledger WHERE source_namespace='+sql_string(SOURCE)+" AND disposition='inserted' AND report_seq>"+str(snapshot_cutoff))}
                    normal_ids=[seq for seq in tail_ids if seq not in owned_ids]
                    if len(normal_ids)>1000:raise ValueError('Duplicate snapshot tail is stale; rescan before apply')
                    unseen=[seq for seq in normal_ids if seq not in observed_tail_ids]
                    if unseen:
                        tail=db.query('SELECT seq,OCTET_LENGTH(image),LOWER(SHA2(image,256)) FROM reports FORCE INDEX(PRIMARY) WHERE seq IN ('+','.join(str(seq) for seq in unseen)+') ORDER BY seq')
                        for line in tail:
                            seq,size,sha=line.split('\t');seq=int(seq);observed_max_seq=max(observed_max_seq,seq);observed_tail_ids.add(seq)
                            existing_hashes.setdefault(sha,{'seq':seq,'bytes':int(size),'basis':'live_tail_before_insert'})
                    dispositions=[]
                    for row in batch:
                        info=row['_import'];prior=check_existing(db,row)
                        if prior:dispositions.append({'source_id':info['source_id'],'action':'replay_skip','seq':prior['seq']});continue
                        hit=existing_hashes.get(info['photo_sha256'])
                        if hit:
                            seq=int(hit['seq'])
                            if db.scalar('SELECT COUNT(*) FROM reports WHERE seq='+str(seq))!='1':raise ValueError('Duplicate target was deleted; retain no-resurrection decision for review')
                            db.query(ledger_insert(row,run_id,str(seq),'duplicate_existing',hit['basis'],public_expr='(SELECT public_id FROM reports WHERE seq='+str(seq)+')'))
                            dispositions.append({'source_id':info['source_id'],'action':'duplicate_skip','seq':seq});continue
                        db.query(insert_sql(row,run_id),timeout=60)
                        seq=int(db.scalar('SELECT @historical_seq'));observed_max_seq=max(observed_max_seq,seq)
                        stored=db.query('SELECT LOWER(SHA2(r.image,256)),UNIX_TIMESTAMP(r.ts),p.policy_version,'+','.join('p.'+x for x in POLICY_FLAGS)+' FROM reports r JOIN report_side_effect_policy p ON p.report_seq=r.seq WHERE r.seq='+str(seq))[0].split('\t')
                        if stored[0]!=info['photo_sha256'] or stored[1]!=str(int(decimal.Decimal(info['uploaded_unix_decimal']))) or stored[2:]!=['1']*6:raise ValueError('Inserted photo/policy validation failed before commit')
                        existing_hashes[info['photo_sha256']]={'seq':seq,'basis':'this_import_run','bytes':info['photo_bytes']}
                        dispositions.append({'source_id':info['source_id'],'action':'inserted','seq':seq})
                    db.query('COMMIT;')
                    done+=len(batch);receipt.write(json.dumps({'run_id':run_id,'done':done,'rows':dispositions,'committed_at_utc':dt.datetime.now(dt.timezone.utc).isoformat()})+'\n');receipt.flush();os.fsync(receipt.fileno())
                    print(json.dumps({'done':done,'total':len(rows),'last_seq':observed_max_seq}),flush=True)
                except Exception:
                    try:db.query('ROLLBACK;')
                    except Exception:pass
                    raise
            db.query("UPDATE historical_report_import_runs SET state='applied',completed_at=CURRENT_TIMESTAMP(6) WHERE run_id="+sql_string(run_id))
        finally:receipt.close()
if __name__=='__main__':main()
