"""AWS-only fake-data MySQL integration tests, isolated database/container.
No production credentials or source records are used. Invoked by root/child on
ca-dataunion-build only, through a different MySQL container from production.
"""
import contextlib,copy,datetime as dt,hashlib,json,pathlib,struct,subprocess,sys,tempfile,threading,time,unittest.mock,zlib
import apply_import as importer
import rollback_import as rollback
import candidate_data as data
from mysql_client import Mysql as RealMysql
DB='cleanapp_dataunion_import_test'
CONTAINER='ca-import-test-mysql'
ROOT=pathlib.Path('/srv/cleanapp-build/work/importer')

def mysql():return RealMysql(CONTAINER,DB)
def sql(command):
    p=subprocess.run(['sudo','docker','exec','-i',CONTAINER,'sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot --batch --skip-column-names'],input=command,text=True,capture_output=True)
    if p.returncode:raise RuntimeError(p.stderr)
    return p.stdout
BASE_SCHEMA='''
CREATE TABLE reports (seq INT NOT NULL AUTO_INCREMENT PRIMARY KEY,public_id VARCHAR(32) UNIQUE,ts TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,id VARCHAR(255) NOT NULL,team INT NOT NULL,latitude FLOAT NOT NULL,longitude FLOAT NOT NULL,x FLOAT,y FLOAT,image LONGBLOB NOT NULL,action_id CHAR(32),description VARCHAR(255)) ENGINE=InnoDB;
CREATE TABLE report_raw (report_seq INT NOT NULL PRIMARY KEY,fetcher_id VARCHAR(64),source_id VARCHAR(255),agent_id VARCHAR(255),agent_version VARCHAR(64),collected_at TIMESTAMP NULL,source_type VARCHAR(32),visibility VARCHAR(16) NOT NULL,trust_level VARCHAR(16),spam_score FLOAT,UNIQUE(fetcher_id,source_id),FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE CASCADE) ENGINE=InnoDB;
CREATE TABLE external_ingest_index (source VARCHAR(64),external_id VARCHAR(255),seq INT NOT NULL,source_timestamp DATETIME,source_url VARCHAR(500),PRIMARY KEY(source,external_id)) ENGINE=InnoDB;
CREATE TABLE report_analysis (seq INT NOT NULL,source VARCHAR(255) NOT NULL,title VARCHAR(500),description TEXT,severity_level FLOAT,language VARCHAR(2),brand_name VARCHAR(255),brand_display_name VARCHAR(255),is_valid TINYINT,classification ENUM('physical','digital'),needs_ai_review TINYINT NOT NULL DEFAULT0,inferred_contact_emails TEXT) ENGINE=InnoDB;
CREATE TABLE reports_owners (seq INT NOT NULL,owner VARCHAR(256) NOT NULL,is_public TINYINT,PRIMARY KEY(seq,owner)) ENGINE=InnoDB;
CREATE TABLE report_side_effect_policy (report_seq INT NOT NULL PRIMARY KEY,source_namespace VARCHAR(64) NOT NULL,reason VARCHAR(255) NOT NULL,policy_version INT NOT NULL DEFAULT1,suppress_email BOOLEAN NOT NULL DEFAULT TRUE,suppress_notifications BOOLEAN NOT NULL DEFAULT TRUE,suppress_rewards BOOLEAN NOT NULL DEFAULT TRUE,suppress_routing BOOLEAN NOT NULL DEFAULT TRUE,suppress_analysis BOOLEAN NOT NULL DEFAULT TRUE,created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE CASCADE) ENGINE=InnoDB;
'''.replace('DEFAULT0','DEFAULT 0').replace('DEFAULT1','DEFAULT 1')
def reset():
    sql('DROP DATABASE IF EXISTS '+DB+'; CREATE DATABASE '+DB+' CHARACTER SET utf8mb4; USE '+DB+';'+BASE_SCHEMA+(ROOT/'history_ledger.sql').read_text())

def png(red,green,blue):
    def chunk(kind,payload):return struct.pack('>I',len(payload))+kind+payload+struct.pack('>I',zlib.crc32(kind+payload)&0xffffffff)
    return b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',1,1,8,2,0,0,0))+chunk(b'IDAT',zlib.compress(bytes((0,red,green,blue))))+chunk(b'IEND',b'')
def fixtures(directory):
    media=directory/'media';media.mkdir();rows=[]
    for index,source in enumerate(('cleanapp','litterbux')):
        ident='fake-test-'+str(index);photo=png(255 if index==0 else 0,0,255 if index==1 else 0);sha=hashlib.sha256(photo).hexdigest();path=media/(sha+'.png');path.write_bytes(photo)
        utc=dt.datetime(2023,7,4,3,52,48,191301+index,tzinfo=dt.timezone.utc);lat,lng=(0.0,12.3456789012345) if index==0 else (45.1234567890123,-123.456789012345)
        original={'_id':ident,'_rev':'1-fake','source':source,'type':'image','image_path':'data/fake/'+ident+'.png','uploaded_at':utc.timestamp(),'uploaded_by':'fake-account-'+str(index),'annotations':[{'type':'Location','latitude':lat,'longitude':lng}]}
        row={'source_key':data.SOURCE+'/metadata/'+ident,'version_label':'0.1.0','legacy_database':'metadata','legacy_id':ident,'legacy_source':source,'legacy_record_type':'image','legacy_uploader_reference':data.SOURCE+'/uploader/fake-account-'+str(index),'uploaded_at_raw':utc.timestamp(),'uploaded_at_utc':utc.isoformat().replace('+00:00','Z'),'location':{'selected':{'latitude':lat,'longitude':lng},'conflicting':False},'image_path':original['image_path'],'image':{'sha256':sha,'format':'PNG','bytes':len(photo),'zip_member':original['image_path'],'exact_photo_join':True,'structural_image_valid':True,'media_locator':str(path)},'review_issues':[],'eligible_projection':True,'original_record':{'legacy_database':'metadata','doc':original}}
        rows.append(row)
    return rows,media

def run_import(directory,rows,media,matches=None,cutoff=0,expect_error=None,mysql_factory=None):
    candidate=directory/'candidates.jsonl';candidate.write_text(''.join(data.canonical(x)+'\n' for x in rows));digest=data.sha_file(candidate)
    with mysql() as db:uuid=db.scalar('SELECT @@server_uuid')
    manifest=directory/'dedup.json';manifest.write_text(json.dumps({'candidate_sha256':digest,'server_uuid':uuid,'complete_global_hash_coverage':True,'cutoff_seq':cutoff,'matches':matches or []}))
    controls=directory/'controls.json';controls.write_text(json.dumps({'server_uuid':uuid,'policy_version':1,'live_guards_verified':True,**{x:True for x in importer.POLICY_FLAGS}}))
    argv=['apply_import.py','--candidates',str(candidate),'--candidate-sha256',digest,'--media-root',str(media),'--dedup-manifest',str(manifest),'--dedup-manifest-sha256',data.sha_file(manifest),'--approved-controls',str(controls),'--mode','apply','--batch-size','1','--out-dir',str(directory/'out')]
    with unittest.mock.patch.object(importer,'Mysql',mysql_factory or mysql),unittest.mock.patch.object(importer,'EXPECTED_SERVER_UUID',uuid),unittest.mock.patch.object(data,'EXPECTED_COUNT',2),unittest.mock.patch.object(data,'EXPECTED_SOURCES',{'cleanapp':1,'litterbux':1}),unittest.mock.patch.object(sys,'argv',argv):
        try:importer.main()
        except Exception as error:
            if expect_error and expect_error in str(error):return str(error)
            raise
    if expect_error:raise AssertionError('Expected importer refusal: '+expect_error)
    return json.loads((directory/'out/import_plan.json').read_text())['run_id']

def main():
    data.require_aws_linux();ROOT.mkdir(parents=True,exist_ok=True);results=[]
    with tempfile.TemporaryDirectory(prefix='fixture-',dir=ROOT) as tmp:
        d=pathlib.Path(tmp);rows,media=fixtures(d);reset()
        run=run_import(d,rows,media)
        with mysql() as db:
            assert db.scalar('SELECT COUNT(*) FROM reports')=='2';assert db.scalar('SELECT COUNT(*) FROM report_side_effect_policy WHERE '+ ' AND '.join(x+'=1' for x in importer.POLICY_FLAGS))=='2'
            assert db.scalar('SELECT COUNT(*) FROM historical_report_import_ledger WHERE MICROSECOND(original_uploaded_at)>0')=='2'
            assert db.scalar("SELECT COUNT(*) FROM report_analysis WHERE is_valid=1 AND classification='physical' AND (inferred_contact_emails IS NULL OR inferred_contact_emails='')")=='0'
            assert db.scalar("SELECT COUNT(*) FROM report_analysis WHERE inferred_contact_emails='[]'")=='2'
            assert db.scalar('SELECT COUNT(*) FROM historical_report_import_ledger WHERE projection_method=\'metadata_only\' AND severity_assessed=0')=='2'
            for row in rows:
                actual=db.query('SELECT LOWER(SHA2(r.image,256)),UNIX_TIMESTAMP(r.ts),l.original_uploaded_unix,l.original_latitude,l.original_longitude FROM reports r JOIN historical_report_import_ledger l ON l.report_seq=r.seq WHERE l.source_id='+data.sql_string('metadata/'+row['legacy_id']))[0].split('\t')
                assert actual[0]==row['image']['sha256'];assert actual[1]==str(int(row['uploaded_at_raw']));assert actual[2]==str(row['uploaded_at_raw']);assert abs(float(actual[3])-row['location']['selected']['latitude'])<1e-12
        results.append('first_apply_blob_location_utc_fractional_ledger_and_all_policy_flags')
        results.append('metadata_rows_do_not_match_contact_enrichment_fallback')
        with mysql() as db:
            db.query("INSERT INTO reports_owners(seq,owner,is_public) VALUES(1,'new-user-claim',1)")
            with unittest.mock.patch.object(rollback,'EXPECTED_SERVER_UUID',db.scalar('SELECT @@server_uuid')):
                try:rollback.rollback_run(db,run);raise AssertionError('Ownership claim must block rollback')
                except ValueError as e:assert 'external side effect/reference' in str(e)
            assert db.scalar('SELECT COUNT(*) FROM reports')=='2'
            db.query('DELETE FROM reports_owners WHERE seq=1; UPDATE reports SET latitude=1 WHERE seq=1')
            with unittest.mock.patch.object(rollback,'EXPECTED_SERVER_UUID',db.scalar('SELECT @@server_uuid')):
                try:rollback.rollback_run(db,run);raise AssertionError('Native edit must block rollback')
                except ValueError as e:assert 'native reporter/time/location' in str(e)
            assert db.scalar('SELECT COUNT(*) FROM reports')=='2';db.query('UPDATE reports SET latitude=0 WHERE seq=1')
        results.extend(['rollback_refuses_new_ownership_claim','rollback_refuses_changed_native_location'])
        run_import(d,rows,media)
        with mysql() as db:assert db.scalar('SELECT COUNT(*) FROM reports')=='2'
        results.append('replay_adds_no_reports')
        changed=copy.deepcopy(rows);changed[0]['original_record']['doc']['_rev']='2-changed'
        run_import(d,changed,media,expect_error='Changed source payload');results.append('changed_source_payload_refused')
        reset()
        with mysql() as db:
            db.query("INSERT INTO reports(public_id,ts,id,team,latitude,longitude,image) VALUES('existing','2024-01-15 10:11:12','existing-user',0,12,13,"+data.sql_bytes((media/(rows[0]['image']['sha256']+'.png')).read_bytes())+');')
        run=run_import(d,rows,media,matches=[{'seq':1,'sha256':rows[0]['image']['sha256'],'basis':'curation_original_or_current','target_exists':True}],cutoff=1)
        with mysql() as db:
            assert db.scalar('SELECT COUNT(*) FROM reports')=='2';assert db.scalar("SELECT COUNT(*) FROM historical_report_import_ledger WHERE disposition='duplicate_existing'")=='1';assert db.scalar('SELECT COUNT(*) FROM report_side_effect_policy')=='1'
            before=db.query('SELECT public_id,UNIX_TIMESTAMP(ts),LOWER(SHA2(image,256)) FROM reports WHERE seq=1')
            with unittest.mock.patch.object(rollback,'EXPECTED_SERVER_UUID',db.scalar('SELECT @@server_uuid')):deleted=rollback.rollback_run(db,run)
            assert len(deleted)==1;assert db.scalar('SELECT COUNT(*) FROM reports')=='1';assert db.query('SELECT public_id,UNIX_TIMESTAMP(ts),LOWER(SHA2(image,256)) FROM reports WHERE seq=1')==before
        results.extend(['existing_exact_duplicate_skipped_without_mutation','rollback_deletes_only_new_cohort'])
        # Simulate a concurrent ingestion reserving a lower sequence while the
        # import holds the insertion gap, then committing after our larger ID.
        reset()
        with mysql() as db:db.query('ALTER TABLE reports AUTO_INCREMENT=2')
        late_errors=[];late_ready=threading.Event()
        late_photo=(media/(rows[1]['image']['sha256']+'.png')).read_bytes()
        def late_insert():
            try:
                with mysql() as b:
                    late_ready.set()
                    b.query("INSERT INTO reports(seq,public_id,ts,id,team,latitude,longitude,image) VALUES(1,'late-existing','2024-02-01 01:02:03','other-user',0,10,11,"+data.sql_bytes(late_photo)+')')
            except Exception as e:late_errors.append(str(e));late_ready.set()
        class HookMysql(RealMysql):
            def __init__(self):
                self.late_thread=None;self.late_triggered=False
                super().__init__(CONTAINER,DB)
            def query(self,statement,timeout=60):
                result=super().query(statement,timeout)
                if 'SELECT seq FROM reports FORCE INDEX(PRIMARY) WHERE seq>' in statement and 'FOR UPDATE' in statement and not self.late_triggered:
                    self.late_triggered=True;self.late_thread=threading.Thread(target=late_insert);self.late_thread.start();assert late_ready.wait(3);time.sleep(0.05)
                if statement.strip()=='COMMIT;' and self.late_thread:
                    self.late_thread.join(5);assert not self.late_thread.is_alive();assert not late_errors;self.late_thread=None
                return result
        run_import(d,rows,media,mysql_factory=HookMysql)
        with mysql() as db:
            assert db.scalar('SELECT COUNT(*) FROM reports')=='2'
            assert db.scalar("SELECT COUNT(*) FROM historical_report_import_ledger WHERE source_id='metadata/fake-test-1' AND disposition='duplicate_existing' AND report_seq=1")=='1'
        results.append('lower_sequence_late_commit_is_detected_as_duplicate')
        reset()
        with mysql() as db:
            seeds=','.join('('+data.sql_string('seed-'+str(x))+",0,1,2,X'01')" for x in range(1001))
            db.query('INSERT INTO reports(id,team,latitude,longitude,image) VALUES '+seeds)
        run_import(d,rows,media,expect_error='tail is stale')
        with mysql() as db:assert db.scalar('SELECT COUNT(*) FROM reports')=='1001';assert db.scalar('SELECT COUNT(*) FROM report_raw')=='0'
        results.append('stale_live_tail_aborts_without_new_reports')
        reset()
        with mysql() as db:db.query('ALTER TABLE report_side_effect_policy ADD CONSTRAINT fixture_force_failure CHECK(suppress_email=0)')
        run_import(d,rows,media,expect_error='Check constraint')
        with mysql() as db:assert db.scalar('SELECT COUNT(*) FROM reports')=='0';assert db.scalar('SELECT COUNT(*) FROM report_raw')=='0';assert db.scalar('SELECT COUNT(*) FROM report_analysis')=='0'
        results.append('policy_insert_failure_rolls_back_report_atomically')
    receipt={'environment':'AWS disposable builder','database':DB,'production_accessed':False,'passed':True,'tests':results}
    (ROOT/'integration_test_result.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt,indent=2))
if __name__=='__main__':main()
