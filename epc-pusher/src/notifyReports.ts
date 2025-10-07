
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

  const filters = buildFilters()
  const where = ['seq >= ?']
  const params: any[] = [startReportSeq]

  if (filters.onlyValid) {
    where.push('is_valid')
  }
  if (filters.language) {
    where.push('language = ?')
    params.push(filters.language)
  }
  if (filters.source) {
    where.push('source = ?')
    params.push(filters.source)
  }

  const sql = `
    select * from report_analysis
    where ${where.join(' and ')}
    order by seq asc limit 1
  `

  let [r] = await db.query(sql, params)
  return (r as any[])[0]
}

function buildFilters() {
  const onlyValidEnv = process.env.EPC_ONLY_VALID
  const onlyValid = typeof onlyValidEnv === 'undefined' ? true : isTruthyEnv('EPC_ONLY_VALID')
  const language = (process.env.EPC_FILTER_LANGUAGE || '').trim()
  const source = (process.env.EPC_FILTER_SOURCE || '').trim()
  return { onlyValid, language: language || undefined, source: source || undefined }
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

  const dbHost = process.env.DB_HOST || 'cleanapp_db'
  const dbPort = Number(process.env.DB_PORT || 3306)
  const dbUser = process.env.DB_USER || 'server'
  const dbName = process.env.DB_NAME || 'cleanapp'
  const dbPassword = process.env.DB_PASSWORD || process.env.MYSQL_APP_PASSWORD || ''
  const hasPassword = Boolean(dbPassword.trim())

  // Log masked Blockscan API key preview for debugging (first/last 4 chars)
  const blockscanKey = (process.env.BLOCKSCAN_CHAT_API_KEY || '').trim()
  const blockscanPreview = blockscanKey
    ? `${blockscanKey.slice(0, 4)}...${blockscanKey.slice(-4)} len=${blockscanKey.length}`
    : '(empty)'
  console.log(`EPC pusher Blockscan key=${blockscanPreview}`)

  console.log(`EPC pusher connecting to DB host=${dbHost} port=${dbPort} user=${dbUser} db=${dbName} pw_set=${hasPassword}`)

  var db  = mysql.createPool({
    connectionLimit : 10,
    host            : dbHost,
    port            : dbPort,
    user            : dbUser,
    password        : dbPassword,
    database        : dbName
  });

  await waitForDb(db)

  await runSendReports(db)
  await db.end()
}


main()

async function waitForDb(pool: mysql.Pool) {
  const maxAttempts = 60
  const delayMs = 2000
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      await pool.query('select 1')
      return
    } catch (e) {
      console.warn(`DB not ready (attempt ${attempt}/${maxAttempts}). Retrying in ${delayMs}ms...`)
      await new Promise((r) => setTimeout(r, delayMs))
    }
  }
  throw new Error('Database not reachable after multiple attempts')
}
