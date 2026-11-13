import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {randomUUID} from 'node:crypto'
import mysql, {PoolConnection, RowDataPacket} from 'mysql2/promise'
import {loadConfig} from '../src/config'
import {MysqlNotificationStore} from '../src/repository'
import {Worker} from '../src/worker'
import {BlockscanError} from '../src/blockscan'

import {applyMigration as script} from '../src/migration'

const databaseUrl = process.env.EPC_TEST_DB_URL

test('MySQL migration, cumulative receipts and durable worker lifecycle', {skip: !databaseUrl, timeout: 60_000}, async () => {
  const url = new URL(databaseUrl!)
  const options = {host: url.hostname, port: Number(url.port || 3306), user: decodeURIComponent(url.username),
    password: decodeURIComponent(url.password), timezone: 'Z', multipleStatements: true}
  const db = await mysql.createConnection(options)
  const name = `epc_worker_test_${randomUUID().replaceAll('-', '')}`
  let store: MysqlNotificationStore | undefined
  const migration = readFileSync(new URL('../../db/patches/20261113_epc_cumulative_dispatch.sql', import.meta.url), 'utf8')
  const template = readFileSync(new URL('../lib/templates/report_notification.tpl', import.meta.url), 'utf8')
  const legacyAddress = '0x863BA6CD3EeAe109f6fD68DfBa10A0501Ce023Cd'
  try {
    await db.query(`CREATE DATABASE ${name} CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci`)
    await db.query(`USE ${name}`)
    await script(db, readFileSync(new URL('../../db/patches/3.3.1-2025-10-07-epc.sql', import.meta.url), 'utf8'))
    await script(db, migration); await script(db, migration)
    await db.query(`
      CREATE TABLE report_analysis (seq BIGINT UNSIGNED NOT NULL,language VARCHAR(16) NOT NULL DEFAULT 'en',
        brand_name VARCHAR(255),source VARCHAR(32) DEFAULT 'fixture',is_valid BOOLEAN DEFAULT TRUE,PRIMARY KEY(seq,language));
      CREATE TABLE reports (seq BIGINT UNSIGNED PRIMARY KEY,public_id VARCHAR(64));
      CREATE TRIGGER fixture_parent AFTER INSERT ON report_analysis FOR EACH ROW
        INSERT IGNORE INTO reports VALUES (NEW.seq,CONCAT('public-',NEW.seq));
      CREATE TABLE report_side_effect_policy (report_seq BIGINT UNSIGNED PRIMARY KEY,policy_version INT,
        suppress_notifications BOOLEAN,suppress_routing BOOLEAN);
      CREATE TABLE report_raw (report_seq BIGINT UNSIGNED PRIMARY KEY,visibility VARCHAR(16));
      CREATE TABLE report_status (seq BIGINT UNSIGNED PRIMARY KEY,status VARCHAR(16));
      CREATE TABLE reports_owners (seq BIGINT UNSIGNED PRIMARY KEY,owner VARCHAR(64),is_public BOOLEAN);
      CREATE TABLE report_evidence_source_projection (seq BIGINT UNSIGNED PRIMARY KEY,privacy_restricted BOOLEAN,
        dirty BOOLEAN DEFAULT TRUE,normalizer_version INT DEFAULT 1,computed_revision INT DEFAULT 0,source_revision INT DEFAULT 1);
      CREATE TABLE wire_submissions_raw (submission_id VARCHAR(64) PRIMARY KEY,report_seq BIGINT UNSIGNED,
        submitted_at DATETIME(6),report_json JSON,KEY report_seq(report_seq));
      CREATE TABLE wire_evidence_projection (submission_id VARCHAR(64) PRIMARY KEY,report_seq BIGINT UNSIGNED,
        contains_pii JSON,requires_redaction JSON);
      INSERT INTO epc_campaigns(slug) VALUES ('reports');
      INSERT INTO epc_contracts(id,\`key\`,address) VALUES (1103,'cleanapp/brand/apple',?);
      INSERT INTO epc_sent_message_templates_read_only(id,body) VALUES (1103,?);
      INSERT INTO epc_outbox(id,campaign,contract_id,metadata,status,sent_message_template_id,
        event_key,source_namespace,source_report_seq,blockscan_message_id)
        VALUES (500,'reports',1103,JSON_OBJECT('report_seq',9000,'blockscanMessageId','legacy-receipt'),
          'sent',1103,'prod:report:10','legacy-dev',9000,'legacy-receipt');
      INSERT INTO report_analysis(seq,brand_name) VALUES
        (1,'Apple'),(2,'new brand'),(3,'invalid'),(4,'suppressed-notification'),(5,'suppressed-routing'),
        (6,'unknown-policy'),(7,'private-raw'),(8,'inactive'),(9,'private-owner'),(10,'apple'),
        (11,'restricted-projection'),(12,'empty-owner'),(13,'nullable-visibility'),(14,'explicit-allow');
      UPDATE report_analysis SET is_valid=FALSE WHERE seq=3;
      INSERT INTO report_side_effect_policy VALUES (4,1,TRUE,FALSE),(5,1,FALSE,TRUE),(6,2,FALSE,FALSE),(14,1,FALSE,FALSE);
      INSERT INTO report_raw VALUES (7,'private'),(13,NULL);
      INSERT INTO report_status VALUES (8,'deleted');
      INSERT INTO reports_owners VALUES (9,'private-owner',FALSE),(12,'',FALSE);
      INSERT INTO report_evidence_source_projection(seq,privacy_restricted) VALUES (11,TRUE);
      INSERT INTO wire_submissions_raw VALUES ('private-11',11,UTC_TIMESTAMP(6),JSON_OBJECT('privacy',JSON_OBJECT('requires_redaction',TRUE)));
    `, [legacyAddress, template])
    const config = loadConfig({EPC_DISPATCH: 'true',EPC_SEND_INTERVAL_MS: '1000'})
    let deriveBlocked = false
    const derive = (key: string) => {
      assert.notEqual(key.toLowerCase(), 'cleanapp/brand/apple', 'legacy mapping must override another factory')
      if (deriveBlocked) throw new Error('fixture address derivation failed')
      return `0x${Buffer.from(key).toString('hex').padEnd(40,'0').slice(0,40)}`
    }
    store = new MysqlNotificationStore(db as PoolConnection, config, template, derive)
    await store.initialize()
    const contenderDb = await mysql.createConnection(options)
    try {
      await contenderDb.query(`USE ${name}`)
      const contender = new MysqlNotificationStore(contenderDb as PoolConnection, config, template, derive)
      await assert.rejects(contender.initialize(), /already running/)
    } finally {await contenderDb.end()}
    let clock = Date.now(); let sent = 0
    let behavior: 'ok' | 'uncertain' | 'throttle' = 'ok'
    const send = async () => {
      sent++
      if (behavior === 'uncertain') throw new BlockscanError('fixture_timeout','uncertain')
      if (behavior === 'throttle') throw new BlockscanError('fixture_throttle','retryable')
      return `fixture-receipt-${sent}`
    }
    const worker = new Worker(store, config, send, async ms => {clock += ms}, () => clock)
    await worker.initialize()
    for (let i=0;i<12;i++) await worker.tick()
    const [outbox] = await db.query<RowDataPacket[]>(`SELECT source_report_seq,status FROM epc_outbox WHERE source_namespace='prod' ORDER BY source_report_seq`)
    assert.deepEqual(outbox.map(r => Number(r.source_report_seq)), [1,2,12,13,14])
    assert.ok(outbox.every(r => r.status==='sent'))
    assert.equal(sent,5,'canonical legacy alias must prevent another send')
    const [contracts] = await db.query<RowDataPacket[]>('SELECT address FROM epc_contracts WHERE id=1103')
    assert.equal(contracts[0]!.address,legacyAddress)
    const [cursor] = await db.query<RowDataPacket[]>(`SELECT last_report_seq FROM epc_report_cursors WHERE source_namespace='prod'`)
    assert.equal(Number(cursor[0]!.last_report_seq),14,'legacy seq9000 must not become the production cursor')
    const [templates] = await db.query<RowDataPacket[]>('SELECT COUNT(*) AS n FROM epc_sent_message_templates_read_only')
    assert.equal(Number(templates[0]!.n),1)
    await db.query('UPDATE report_analysis SET is_valid=TRUE WHERE seq=3')
    for (let i=0;i<4;i++) await worker.tick()
    const [late] = await db.query<RowDataPacket[]>(`SELECT status FROM epc_outbox WHERE event_key='prod:report:3'`)
    assert.equal(late[0]!.status,'sent','bounded rescan must recover analysis below forward cursor')
    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (15,'apple')`)
    behavior='uncertain'; await worker.tick()
    const [uncertain] = await db.query<RowDataPacket[]>(`SELECT status FROM epc_outbox WHERE event_key='prod:report:15'`)
    assert.equal(uncertain[0]!.status,'uncertain')
    const before=sent; await worker.tick(); assert.equal(sent,before)
    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (16,'apple')`)
    behavior='throttle'; await worker.tick()
    const [retry] = await db.query<RowDataPacket[]>(`SELECT status,attempt_count,next_attempt_at FROM epc_outbox WHERE event_key='prod:report:16'`)
    assert.equal(retry[0]!.status,'pending'); assert.equal(Number(retry[0]!.attempt_count),1); assert.ok(retry[0]!.next_attempt_at)
    behavior='ok'
    await db.query(`UPDATE epc_outbox SET next_attempt_at=UTC_TIMESTAMP(3) WHERE event_key='prod:report:16'; UPDATE epc_report_cursors SET dispatch_not_before=NULL WHERE source_namespace='prod'`)
    await worker.tick()
    const [retried] = await db.query<RowDataPacket[]>(`SELECT status,attempt_count FROM epc_outbox WHERE event_key='prod:report:16'`)
    assert.equal(retried[0]!.status,'sent'); assert.equal(Number(retried[0]!.attempt_count),2)
    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (17,'apple')`)
    const markSent=store.markSent.bind(store)
    store.markSent=async () => {throw new Error('fixture database failure')}
    await assert.rejects(worker.tick(),/fixture database failure/)
    store.markSent=markSent; await worker.initialize()
    const [interrupted] = await db.query<RowDataPacket[]>(`SELECT status FROM epc_outbox WHERE event_key='prod:report:17'`)
    assert.equal(interrupted[0]!.status,'uncertain')
    const afterRecovery=sent; await worker.tick(); assert.equal(sent,afterRecovery)
    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (18,'derivation-failure')`)
    deriveBlocked=true; await assert.rejects(store.queueNextReport(),/derivation failed/)
    const [rolledBack] = await db.query<RowDataPacket[]>(`SELECT last_report_seq FROM epc_report_cursors WHERE source_namespace='prod'`)
    assert.equal(Number(rolledBack[0]!.last_report_seq),17)
    deriveBlocked=false; await worker.tick()
    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (19,'apple')`)
    await store.queueNextReport(); await db.query(`INSERT INTO report_raw VALUES (19,'private')`)
    assert.equal(await store.claimNextNotification(),undefined)
    const [revoked] = await db.query<RowDataPacket[]>(`SELECT status,last_error_code FROM epc_outbox WHERE event_key='prod:report:19'`)
    assert.equal(revoked[0]!.status,'error'); assert.equal(revoked[0]!.last_error_code,'report_not_eligible')
    // A false/dirty aggregate projection cannot override current Wire privacy.
    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (20,'apple');
      INSERT INTO wire_submissions_raw VALUES ('wire-20',20,UTC_TIMESTAMP(6),JSON_OBJECT());`)
    await store.queueNextReport()
    await db.query(`UPDATE wire_submissions_raw SET report_json=JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',TRUE)) WHERE report_seq=20;
      INSERT INTO report_evidence_source_projection(seq,privacy_restricted,dirty,computed_revision,source_revision) VALUES (20,FALSE,TRUE,1,2);`)
    assert.equal(await store.claimNextNotification(),undefined,'dirty false projection must not leak after queuing')
    const [privateAfterQueue] = await db.query<RowDataPacket[]>(`SELECT status FROM epc_outbox WHERE event_key='prod:report:20'`)
    assert.equal(privateAfterQueue[0]!.status,'error')
    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (21,'apple'),(22,'apple'),(23,'apple'),(24,'apple');
      INSERT INTO wire_submissions_raw VALUES
        ('wire-21',21,UTC_TIMESTAMP(6),JSON_OBJECT('privacy',JSON_OBJECT('requires_redaction',TRUE))),
        ('wire-22',22,UTC_TIMESTAMP(6),JSON_OBJECT()),
        ('wire-23-old',23,'2025-01-01',JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',TRUE))),
        ('wire-23-new',23,'2026-01-01',JSON_OBJECT()),
        ('wire-24',24,UTC_TIMESTAMP(6),JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',TRUE)));
      INSERT INTO wire_evidence_projection VALUES ('wire-22',22,CAST('true' AS JSON),NULL),('wire-24',999,CAST('false' AS JSON),NULL);
      INSERT INTO report_evidence_source_projection(seq,privacy_restricted,dirty,normalizer_version) VALUES (22,FALSE,FALSE,0);`)
    await worker.tick()
    const [privateEvents] = await db.query<RowDataPacket[]>(`SELECT source_report_seq FROM epc_outbox WHERE source_namespace='prod' AND source_report_seq BETWEEN 21 AND 24`)
    assert.deepEqual(privateEvents.map(r=>Number(r.source_report_seq)),[23], 'missing/stale cache uses exact latest Wire + matching projection')

    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (25,'apple'),(26,'apple');
      UPDATE reports SET public_id='' WHERE seq=25; DELETE FROM reports WHERE seq=26;`)
    await store.queueNextReport()
    const [unpublished]=await db.query<RowDataPacket[]>(`SELECT id FROM epc_outbox WHERE event_key IN ('prod:report:25','prod:report:26')`)
    assert.equal(unpublished.length,0,'empty public identity and missing parent must block notices')

    await db.query(`INSERT INTO report_analysis(seq,brand_name) VALUES (27,'apple')`)
    await store.queueNextReport() // Leave one eligible, unsent event below the new floor.
    // Start sequence is a lower bound for initial/wrapped scans and claims.
    await store.close(); store=undefined
    await db.query(`UPDATE report_side_effect_policy SET suppress_notifications=FALSE,suppress_routing=FALSE WHERE report_seq=4;
      INSERT INTO report_analysis(seq,brand_name) VALUES (100,'apple'),(101,'apple');`)
    const cooldownConfig=loadConfig({EPC_DISPATCH:'true',EPC_SEND_INTERVAL_MS:'1000',EPC_REPORTS_START_SEQ:'100'})
    store=new MysqlNotificationStore(db as PoolConnection,cooldownConfig,template,derive)
    await store.initialize()
    let cooldownSends=0
    let throttled=true
    const cooldownSend=async()=>{cooldownSends++; if(throttled)throw new BlockscanError('fixture_account_throttle','retryable',90_000); return `cooldown-${cooldownSends}`}
    const cooldownWorker=new Worker(store,cooldownConfig,cooldownSend,async ms=>{clock+=ms},()=>clock)
    await cooldownWorker.initialize(); await cooldownWorker.tick()
    assert.equal(cooldownSends,1)
    await cooldownWorker.tick()
    assert.equal(cooldownSends,1,'a second pending row must honor the account cooldown')
    await cooldownWorker.initialize(); await cooldownWorker.tick()
    assert.equal(cooldownSends,1,'cooldown must remain in force after restart')
    const [belowFloor]=await db.query<RowDataPacket[]>(`SELECT id FROM epc_outbox WHERE event_key='prod:report:4'`)
    assert.equal(belowFloor.length,0,'rescan must never queue below startSeq, including after wrap')
    const [oldPending]=await db.query<RowDataPacket[]>(`SELECT attempt_count,status FROM epc_outbox WHERE event_key='prod:report:27'`)
    assert.equal(Number(oldPending[0]!.attempt_count),0); assert.equal(oldPending[0]!.status,'pending')
    const [globalLimit]=await db.query<RowDataPacket[]>(`SELECT dispatch_not_before>UTC_TIMESTAMP(3) AS cooling FROM epc_report_cursors WHERE source_namespace='prod'`)
    assert.equal(Number(globalLimit[0]!.cooling),1)
    throttled=false
    await db.query(`UPDATE epc_report_cursors SET dispatch_not_before=NULL WHERE source_namespace='prod';
      UPDATE epc_outbox SET next_attempt_at=UTC_TIMESTAMP(3) WHERE event_key='prod:report:100';`)
    await cooldownWorker.tick(); await cooldownWorker.tick()
    assert.equal(cooldownSends,3)
    const longBrand='x'.repeat(255)
    await db.query('INSERT INTO report_analysis(seq,brand_name) VALUES (102,?)',[longBrand])
    await cooldownWorker.tick()
    const [longKey]=await db.query<RowDataPacket[]>(`SELECT CHAR_LENGTH(c.\`key\`) AS n,o.status FROM epc_outbox o JOIN epc_contracts c ON c.id=o.contract_id WHERE o.event_key='prod:report:102'`)
    assert.equal(Number(longKey[0]!.n),270); assert.equal(longKey[0]!.status,'sent')
    // Invalid-only source windows must advance without an unbounded search for
    // an eligible row. A later rescan recovers a newly valid older analysis.
    await store.close(); store=undefined
    await db.query(`INSERT INTO report_analysis(seq,brand_name,is_valid) VALUES
      (200,'apple',FALSE),(201,'apple',FALSE),(202,'apple',TRUE),(203,'apple',TRUE);
      INSERT INTO report_analysis(seq,language,brand_name) VALUES (202,'fr','wrong-language');`)
    const boundedConfig=loadConfig({EPC_SOURCE_NAMESPACE:'bounded-forward',EPC_REPORTS_START_SEQ:'200',
      EPC_DISPATCH:'true',EPC_SCAN_BATCH_SIZE:'2',EPC_RESCAN_WINDOW_SIZE:'2'})
    store=new MysqlNotificationStore(db as PoolConnection,boundedConfig,template,derive)
    await store.initialize()
    assert.equal(await store.queueNextReport(),true)
    const [invalidWindow]=await db.query<RowDataPacket[]>(`SELECT last_report_seq FROM epc_report_cursors WHERE source_namespace='bounded-forward'`)
    assert.equal(Number(invalidWindow[0]!.last_report_seq),201)
    const [emptyBatch]=await db.query<RowDataPacket[]>(`SELECT id FROM epc_outbox WHERE source_namespace='bounded-forward'`)
    assert.equal(emptyBatch.length,0)
    await store.queueNextReport()
    const [validWindow]=await db.query<RowDataPacket[]>(`SELECT source_report_seq,metadata FROM epc_outbox WHERE source_namespace='bounded-forward' ORDER BY source_report_seq`)
    assert.deepEqual(validWindow.map(r=>Number(r.source_report_seq)),[202,203])
    assert.equal(validWindow[0]!.metadata.report_language,'en')
    assert.equal(validWindow[0]!.metadata.report_brand,'apple')
    await db.query('UPDATE report_analysis SET is_valid=TRUE WHERE seq=200')
    await store.queueNextReport(); await store.queueNextReport()
    const [lateWindow]=await db.query<RowDataPacket[]>(`SELECT source_report_seq FROM epc_outbox WHERE source_namespace='bounded-forward' ORDER BY source_report_seq`)
    assert.deepEqual(lateWindow.map(r=>Number(r.source_report_seq)),[200,202,203])
    await script(db,migration)
    const [legacy] = await db.query<RowDataPacket[]>('SELECT metadata,source_namespace,status FROM epc_outbox WHERE id=500')
    assert.equal(legacy[0]!.source_namespace,'legacy-dev'); assert.equal(legacy[0]!.status,'sent')
    assert.equal(legacy[0]!.metadata.report_seq,9000)
  } finally {
    if (store) await store.close().catch(() => {})
    await db.query(`DROP DATABASE IF EXISTS ${name}`); await db.end()
  }
})
