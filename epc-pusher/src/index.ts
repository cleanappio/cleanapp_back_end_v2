
import mysql from 'mysql2/promise'
import {getLatestByCampaign, Queryable} from './queries';
import {getEpcAddress} from './epc';


const CAMPAIGN_SLUG = "reports"
const CAMPAIGN_PREFIX = "cleanapp"


async function initReportsEPC(db: mysql.Pool) {

  await db.query(
    `insert into epc_campaigns (slug, key_prefix, description) values (?, ?, ?)
    on duplicate key update slug=slug`,
    [CAMPAIGN_SLUG, "cleanapp", "automatic notification of new reports from report_analysis table"]
  )
}


async function runSendReports(pool: mysql.Pool) {

  await initReportsEPC(pool)

  let db = await pool.getConnection()

  let startReportSeq = await getStartReportsSeq(db)

  while (true) {
    console.log(`Process reports from ${startReportSeq}...`)

    await db.beginTransaction()
    let report = await getNextReportToProcess(db, startReportSeq)

    if (!report) {
      await db.rollback()
      console.log("No reports to process")
      await new Promise((r) => setTimeout(r, 5000))
      continue
    }

    await processReport(db, report)
    await db.commit()

    startReportSeq = report.seq + 1
  }
}

async function processReport(db: mysql.Connection, report: any) {

  // get the campaign
  let [r1] = await db.query('select * from epc_campaigns where slug = ?', [CAMPAIGN_SLUG])
  let campaign = r1[0]

  // create the contract
  let key: string = report.brand_name.trim()

  if (!key) {
    console.warn(`Report seq ${report.seq} has no brand name, skipping`)
    return
  }

  key = `${campaign.key_prefix}/${key}`

  let address = getEpcAddress(key)
  
  await db.query(
    `insert into epc_contracts (\`key\`, address) values (?, ?)
    on duplicate key update id=id`,
    [key, address]
  )
  let [r0] = await db.query('select id from epc_contracts where address = ?', [address])
  let contract_id = r0[0].id

  await db.query(
    `insert into epc_outbox (campaign, contract_id, metadata, status)
    values (?, ?, ?, ?)`,
    [campaign.slug, contract_id, `{"report_seq":${report.seq}}`, "held"]
  )
}


async function getNextReportToProcess(db: Queryable, startReportSeq: number) {

  let sql = `
  select * from report_analysis
  where seq >= ? and is_valid and language = "en"
  order by seq asc limit 1
  `

  let [r] = await db.query(sql, [startReportSeq])
  return r[0]
}



async function getStartReportsSeq(db: Queryable) {

  let latest = await getLatestByCampaign(db, CAMPAIGN_PREFIX)

  if (latest) {
    return latest.metadata.report_seq + 1
  }


  let key = 'EPC_REPORTS_START_SEQ'
  let v = process.env[key]

  if (typeof v == 'undefined') {
    console.warn(
      `EPC reports process first run; set environment variable ${key} ` +
      `to the starting report sequence number (seq).`
    )
    console.warn("Quitting...")
    await new Promise((r) => setTimeout(r, 10000))
    process.exit(1)
  }

  return parseInt(v)
}





async function main() {
  var db  = mysql.createPool({
    connectionLimit : 10,
    host            : process.env.DB_HOST,
    port            : Number(process.env.DB_PORT || 3306),
    user            : process.env.DB_USER,
    password        : process.env.DB_PASSWORD,
    database        : process.env.DB_NAME
  });

  await runSendReports(db)
  await db.end()
}


main()
