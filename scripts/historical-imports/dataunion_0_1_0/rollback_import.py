"""Rollback only rows inserted by one reviewed historical import run.
Existing duplicate targets are never deleted or modified. Fail on changed media,
source identity, policy, external side effects, or another analysis source.
"""
import argparse,decimal,hashlib,json,pathlib,struct
from candidate_data import require_aws_linux,sql_string,SOURCE
from mysql_client import Mysql
from apply_import import EXPECTED_SERVER_UUID,POLICY_FLAGS

def rollback_run(db,run_id):
    if db.scalar('SELECT @@server_uuid')!=EXPECTED_SERVER_UUID:raise ValueError('Wrong rollback server')
    rows=db.query('SELECT source_id,report_seq,photo_sha256,COALESCE(report_public_id,\'\'),original_uploaded_unix,original_latitude,original_longitude,HEX(legacy_uploader_reference) FROM historical_report_import_ledger WHERE run_id='+sql_string(run_id)+' AND source_namespace='+sql_string(SOURCE)+" AND disposition='inserted' ORDER BY report_seq")
    result=[];db.query('START TRANSACTION;')
    try:
        for line in rows:
            source_id,seq,sha,public_id,original_unix,original_lat,original_lng,uploader_hex=line.split('\t');seq=int(seq)
            check=db.query('SELECT LOWER(SHA2(r.image,256)),rr.fetcher_id,rr.source_id,COALESCE(r.public_id,\'\'),p.source_namespace,p.policy_version,'+','.join('p.'+x for x in POLICY_FLAGS)+' FROM reports r JOIN report_raw rr ON rr.report_seq=r.seq JOIN report_side_effect_policy p ON p.report_seq=r.seq WHERE r.seq='+str(seq)+' FOR UPDATE')
            if len(check)!=1 or check[0].split('\t')!=[sha,SOURCE,source_id,public_id,SOURCE,'1']+['1']*5:raise ValueError('Rollback target changed or lacks exact policy/source proof')
            native=db.query('SELECT id,UNIX_TIMESTAMP(ts),CAST(latitude AS DOUBLE),CAST(longitude AS DOUBLE) FROM reports WHERE seq='+str(seq))[0].split('\t')
            reporter='historical:dataunion-0.1.0:'+hashlib.sha256(bytes.fromhex(uploader_hex)).hexdigest()
            f32=lambda value:struct.unpack('!f',struct.pack('!f',float(value)))[0]
            if native[0]!=reporter or native[1]!=str(int(decimal.Decimal(original_unix))) or abs(float(native[2])-f32(original_lat))>1e-12 or abs(float(native[3])-f32(original_lng))>1e-12:
                raise ValueError('Rollback target native reporter/time/location was changed')
            if db.scalar('SELECT COUNT(*) FROM report_analysis WHERE seq='+str(seq)+' AND source<>'+sql_string('dataunion-0.1.0-recovery'))!='0':raise ValueError('Rollback target has independent analysis')
            # Existing side effect tables may be absent in staging; on production
            # discover only known report-linked columns, and fail if any row exists.
            for table in ('sent_reports_emails','report_email_deliveries','mobile_push_delivery_events','wire_reward_records','case_reports','reports_owners','report_push_subscriptions','responses','report_details','bounding_boxes','report_tags'):
                columns=db.query('SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='+sql_string(table)+" AND COLUMN_NAME IN ('seq','report_seq')")
                for col in columns:
                    if db.scalar('SELECT COUNT(*) FROM `'+table+'` WHERE `'+col+'`='+str(seq))!='0':raise ValueError('Rollback target has external side effect/reference: '+table)
            db.query('DELETE FROM report_analysis WHERE seq='+str(seq)+' AND source='+sql_string('dataunion-0.1.0-recovery')+'; DELETE FROM external_ingest_index WHERE source='+sql_string(SOURCE)+' AND external_id='+sql_string(source_id)+' AND seq='+str(seq)+'; DELETE FROM reports WHERE seq='+str(seq)+'; UPDATE historical_report_import_ledger SET disposition='+sql_string('rolled_back')+' WHERE source_namespace='+sql_string(SOURCE)+' AND source_id='+sql_string(source_id)+';')
            result.append({'source_id':source_id,'deleted_inserted_seq':seq})
        db.query("UPDATE historical_report_import_runs SET state='rolled_back',completed_at=CURRENT_TIMESTAMP(6) WHERE run_id="+sql_string(run_id)+'; COMMIT;')
    except Exception:
        try:db.query('ROLLBACK;')
        except Exception:pass
        raise
    return result

def main():
    require_aws_linux();p=argparse.ArgumentParser(description=__doc__);p.add_argument('--run-id',required=True);p.add_argument('--out',required=True);args=p.parse_args()
    if len(args.run_id)!=64 or any(x not in '0123456789abcdef' for x in args.run_id):raise ValueError('Invalid run identity')
    with Mysql() as db:result=rollback_run(db,args.run_id)
    pathlib.Path(args.out).write_text(json.dumps({'run_id':args.run_id,'rolled_back_inserted_rows':result},indent=2)+'\n');print(json.dumps({'rolled_back':len(result)}))
if __name__=='__main__':main()
