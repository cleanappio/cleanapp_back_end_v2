
import mysql from 'mysql2/promise'
import {getLatestByCampaign, Queryable} from './queries'
import {getEpcAddress} from './epc'
import * as blockscan from './blockscan'
import {readFileSync} from 'fs'
import Handlebars from 'handlebars'
import {isTruthyEnv} from './utils'



const CAMPAIGN_SLUG = "reports"


async function initReportsEPC(db: mysql.Pool) {

  await db.query(
    `insert into epc_campaigns (slug, description) values (?, ?)
    on duplicate key update slug=slug`,
    [CAMPAIGN_SLUG, "automatic notification of new reports from report_analysis table"]
  )
}


async function runSendReports(pool: mysql.Pool) {

  await initReportsEPC(pool)

  let db = await pool.getConnection()

  let startReportSeq = await getStartReportsSeq(db)

  while (true) {
    console.log(`Process reports from ${startReportSeq}...`)

    let report: any

    await db.beginTransaction()
    try {
      report = await getNextReportToProcess(db, startReportSeq)

      if (!report) {
        await db.rollback()
        console.log(`Waiting for new reports...`)
        await new Promise((r) => setTimeout(r, 5000))
        continue
      }

      await processReport(db, report)
    } catch (e) {
      await db.rollback()
      console.trace(e)
      console.warn("Sleeping 5s...")
      await new Promise((r) => setTimeout(r, 5000))
    }
    await db.commit()

    startReportSeq = report.seq + 1
  }
}



async function processReport(db: mysql.Connection, report: any) {

  /*
   * create the contract address and key
   */

  let brand: string = report.brand_name.trim()
  if (!brand) {
    console.warn(`Report seq ${report.seq} has no brand name, skipping`)
    return
  }
  let key = `cleanapp/brand/${brand}`
  let address = getEpcAddress(key)

  /*
   * Get contract ID
   */
  
  await db.query(
    `insert into epc_contracts (\`key\`, address) values (?, ?)
    on duplicate key update id=id`,
    [key, address]
  )
  let [r0] = await db.query('select id from epc_contracts where address = ?', [address])
  let contract_id = r0[0].id

  /*
   * Get Sent Template ID
   */

  await db.query(
    `insert into epc_sent_message_templates_read_only (body) values (?)
    on duplicate key update id=id`,
    [reportNotifyTemplate]
  )
  let [r2] = await db.query(
    'select id from epc_sent_message_templates_read_only where body = ?',
    [reportNotifyTemplate])
  let template_id = r2[0].id

  let metadata: any = { report_seq: report.seq }

  /*
   * Render message
   */
  let msg = renderReportNotification(report)

  /*
   * Optionally dispatch notification
   */
  let status = 'held'
  if (isTruthyEnv('EPC_DISPATCH')) {
    /*
     * Send notification
     */
    metadata.blockscanMessageId = await blockscan.sendChat(address, msg)
    console.log(`Dispatched report notification, report=${report.seq}, EPC=${key} (${address}) blockscanMessageId=${metadata.blockscanMessageId}`)
    status = 'sent'

    // Don't firehose messages in DEV
    if (process.env.NODE_ENV == 'development') {
      await new Promise((r) => setTimeout(r, 1000))
    }
  } else {
    // Store message data for when we sent it
    // It can be deleted after sending to save space
    metadata.msg = msg
  }

  /*
   * Insert record
   */
  await db.query(
    `insert into epc_outbox (campaign, contract_id, metadata, status, sent_message_template_id)
    values (?, ?, ?, ?, ?)`,
    [CAMPAIGN_SLUG, contract_id, JSON.stringify(metadata), status, template_id]
  )
}


async function getNextReportToProcess(db: Queryable, startReportSeq: number) {

  let sql = `
  select * from report_analysis
  where seq >= ? and is_valid and language = "en" and source = "ChatGPT"
  order by seq asc limit 1
  `

  let [r] = await db.query(sql, [startReportSeq])
  return r[0]
}


/*
 * Get the sequence offset to start looking for reports to send
 */

async function getStartReportsSeq(db: Queryable) {

  let latest = await getLatestByCampaign(db, CAMPAIGN_SLUG)

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
    console.warn("Quitting in 10s...")
    await new Promise((r) => setTimeout(r, 10000))
    process.exit(1)
  }

  return parseInt(v)
}


const reportNotifyTemplate = readFileSync("./lib/templates/report_notification.tpl", { encoding: 'utf8' })
const _callReportNotifyTemplate = Handlebars.compile(reportNotifyTemplate, { noEscape: true })

function renderReportNotification(report: object) {
  return _callReportNotifyTemplate({ report })
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
