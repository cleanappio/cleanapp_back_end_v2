import {createHash} from 'node:crypto'
import {PoolConnection, ResultSetHeader, RowDataPacket} from 'mysql2/promise'
import Handlebars from 'handlebars'
import {Config, reportEventKey} from './config'
import {NotificationStore, PendingNotification} from './worker'

const CAMPAIGN = 'reports'

interface Report extends RowDataPacket {
  seq: number
  brand_name: string
  language: string | null
}
interface Contract extends RowDataPacket {id: number, key: string, address: string}
interface QueuedRow extends RowDataPacket {
  id: number
  source_report_seq: number
  metadata: Record<string, unknown> | string
  message_body: string | null
  address: string
  attempt_count: number
}

// These are the live publication guards used by the public evidence readers.
// Missing policy/publication tables cause SQL failure, so dispatch fails closed.
export function eligibility(config: Config): {sql: string, params: string[]} {
  const clauses = [
    `a.brand_name IS NOT NULL AND TRIM(a.brand_name)<>''`,
    `EXISTS (SELECT 1 FROM reports r
      WHERE r.seq=a.seq AND r.public_id IS NOT NULL AND r.public_id<>'')`,
    `NOT EXISTS (SELECT 1 FROM report_side_effect_policy effects
      WHERE effects.report_seq=a.seq
        AND (COALESCE(effects.policy_version,0)<>1
          OR COALESCE(effects.suppress_notifications,TRUE)<>FALSE
          OR COALESCE(effects.suppress_routing,TRUE)<>FALSE))`,
    `NOT EXISTS (SELECT 1 FROM report_raw rr
      WHERE rr.report_seq=a.seq AND rr.visibility IS NOT NULL AND rr.visibility<>'public')`,
    `NOT EXISTS (SELECT 1 FROM report_status rs
      WHERE rs.seq=a.seq AND rs.status IS NOT NULL AND rs.status<>'active')`,
    `NOT EXISTS (SELECT 1 FROM reports_owners ro
      WHERE ro.seq=a.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)`,
    // Read exact source privacy on every queue/claim. A false cached result
    // cannot authorize a send while its generation is dirty/stale/missing.
    // The listener selects latest raw submitted_at, then uses a matching
    // transactional Wire projection's nodes (including authoritative NULLs).
    `COALESCE((SELECT CASE WHEN h.submission_id IS NOT NULL
        THEN (h.contains_pii=TRUE OR h.requires_redaction=TRUE)
        ELSE (JSON_EXTRACT(ws.report_json,'$.privacy.contains_pii')=TRUE
          OR JSON_EXTRACT(ws.report_json,'$.privacy.requires_redaction')=TRUE) END
      FROM wire_submissions_raw ws
      LEFT JOIN wire_evidence_projection h
        ON h.submission_id=ws.submission_id AND h.report_seq=ws.report_seq
      WHERE ws.report_seq=a.seq ORDER BY ws.submitted_at DESC LIMIT 1),FALSE)=FALSE`,
  ]
  const params: string[] = []
  if (config.onlyValid) clauses.push('a.is_valid=TRUE')
  if (config.language) {clauses.push('a.language=?'); params.push(config.language)}
  if (config.source) {clauses.push('a.source=?'); params.push(config.source)}
  return {sql: clauses.join(' AND '), params}
}

// JSON_TABLE materializes the already bounded source IDs before any expensive
// eligibility or language ordering. STRAIGHT_JOIN keeps those IDs as the driver
// and performs indexed seq lookups, rather than sorting the entire valid cohort.
export function analysisForCandidates(config: Config, ids: number[], onlyMissing = false): {sql: string, params: (string | number)[]} {
  if (!ids.length || ids.some(seq => !Number.isSafeInteger(seq) || seq < 1)) {
    throw new Error('Invalid bounded EPC source candidates')
  }
  const filter = eligibility(config)
  const missing = onlyMissing ? ` AND NOT EXISTS (SELECT 1 FROM epc_outbox o
    WHERE o.event_key=CONCAT(?,':report:',a.seq))` : ''
  return {
    sql: `SELECT STRAIGHT_JOIN a.seq,a.brand_name,a.language
      FROM JSON_TABLE(?, '$[*]' COLUMNS(seq BIGINT UNSIGNED PATH '$' ERROR ON EMPTY ERROR ON ERROR)) candidates
      JOIN report_analysis a ON a.seq=candidates.seq
      WHERE ${filter.sql}${missing}
      ORDER BY a.seq ASC,(a.language='en') DESC,a.language ASC`,
    params: [JSON.stringify(ids), ...filter.params, ...(onlyMissing ? [config.namespace] : [])],
  }
}

export class MysqlNotificationStore implements NotificationStore {
  private templateId = 0
  private render: Handlebars.TemplateDelegate
  private lockName: string

  constructor(
    private db: PoolConnection,
    private config: Config,
    private template: string,
    private deriveAddress: (key: string) => string,
  ) {
    this.render = Handlebars.compile(template, {noEscape: true})
    this.lockName = `cleanapp:epc:${createHash('sha256').update(config.namespace).digest('hex').slice(0,40)}`
  }

  async initialize(): Promise<void> {
    await this.db.query("SET SESSION time_zone='+00:00'")
    const [locks] = await this.db.query<RowDataPacket[]>('SELECT GET_LOCK(?,0) AS acquired', [this.lockName])
    if (Number(locks[0]?.acquired) !== 1) throw new Error('EPC worker already running for this namespace')
    await this.db.query(`INSERT INTO epc_campaigns (slug,description) VALUES (?,?)
      ON DUPLICATE KEY UPDATE slug=slug`, [CAMPAIGN, 'automatic notification of new reports from report_analysis table'])
    await this.db.query(`INSERT IGNORE INTO epc_report_cursors
      (campaign,source_namespace,last_report_seq,rescan_report_seq)
      VALUES (?,?,?,?)`, [CAMPAIGN, this.config.namespace, this.config.startSeq - 1, this.config.startSeq - 1])
    // Reuse the earliest exact legacy template; do not append one per report.
    const [templates] = await this.db.query<RowDataPacket[]>(
      'SELECT id FROM epc_sent_message_templates_read_only WHERE BINARY body=BINARY ? ORDER BY id LIMIT 1', [this.template])
    if (templates.length) this.templateId = Number(templates[0]!.id)
    else {
      const [result] = await this.db.query<ResultSetHeader>(
        'INSERT INTO epc_sent_message_templates_read_only (body) VALUES (?)', [this.template])
      this.templateId = result.insertId
    }
  }

  async close(): Promise<void> {
    await this.db.query('SELECT RELEASE_LOCK(?)', [this.lockName])
  }

  private async inTransaction<T>(operation: () => Promise<T>): Promise<T> {
    await this.db.beginTransaction()
    try {
      const result = await operation()
      await this.db.commit()
      return result
    } catch (error) {
      await this.db.rollback()
      throw error
    }
  }

  private async queueReport(report: Report): Promise<boolean> {
    const eventKey = reportEventKey(this.config.namespace, Number(report.seq))
    // Cross-namespace aliases reserved by a provenance-verified legacy import
    // intentionally suppress another send of the same production source event.
    const [existing] = await this.db.query<RowDataPacket[]>(
      'SELECT id FROM epc_outbox WHERE event_key=? LIMIT 1', [eventKey])
    if (existing.length) return false
    const brand = report.brand_name.trim()
    const key = `cleanapp/brand/${brand}`
    const [found] = await this.db.query<Contract[]>(
      'SELECT id,`key`,address FROM epc_contracts WHERE `key`=? LIMIT 1', [key])
    let contract = found[0]
    if (!contract) {
      const address = this.deriveAddress(key)
      const [insert] = await this.db.query<ResultSetHeader>(
        'INSERT INTO epc_contracts (`key`,address) VALUES (?,?)', [key, address])
      contract = {id: insert.insertId, key, address} as Contract
    }
    const message = this.render({report: {brand_name: encodeURIComponent(brand)}})
    const metadata = {
      report_seq: Number(report.seq), source_namespace: this.config.namespace,
      report_brand: brand, report_language: report.language,
    }
    await this.db.query(`INSERT INTO epc_outbox
      (campaign,contract_id,metadata,status,sent_message_template_id,event_key,
       source_namespace,source_report_seq,message_body,next_attempt_at)
      VALUES (?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(3))`,
    [CAMPAIGN, contract.id, JSON.stringify(metadata), this.config.dispatch ? 'pending' : 'held',
      this.templateId, eventKey, this.config.namespace, Number(report.seq), message])
    return true
  }

  private async sourceWindow(after: number, limit: number): Promise<number[]> {
    const [rows] = await this.db.query<RowDataPacket[]>(
      'SELECT DISTINCT seq FROM report_analysis WHERE seq>? ORDER BY seq LIMIT ?', [after, limit])
    return rows.map(row => Number(row.seq))
  }

  private async preferredCandidates(ids: number[], onlyMissing = false): Promise<Report[]> {
    if (!ids.length) return []
    const query = analysisForCandidates(this.config, ids, onlyMissing)
    const [rows] = await this.db.query<Report[]>(query.sql, query.params)
    const seen = new Set<number>()
    // The bounded SQL sorts eligible English first, then language ascending.
    // One report event gets exactly that first eligible analysis per sequence.
    return rows.filter(row => {
      const seq = Number(row.seq)
      if (seen.has(seq)) return false
      seen.add(seq)
      return true
    })
  }

  async queueNextReport(): Promise<boolean> {
    return this.inTransaction(async () => {
      const [cursors] = await this.db.query<RowDataPacket[]>(
        'SELECT last_report_seq,rescan_report_seq FROM epc_report_cursors WHERE campaign=? AND source_namespace=? FOR UPDATE',
        [CAMPAIGN, this.config.namespace])
      const cursor = Math.max(Number(cursors[0]?.last_report_seq), this.config.startSeq - 1)
      const rescan = Math.max(Number(cursors[0]?.rescan_report_seq), this.config.startSeq - 1)
      if (!Number.isSafeInteger(cursor) || !Number.isSafeInteger(rescan)) throw new Error('Invalid EPC source cursor')

      // Bound source IDs BEFORE filtering/ordering by eligible language. This
      // advances through invalid-only windows and avoids a full valid-cohort
      // filesort even when no eligible report can be found near this cursor.
      const forwardWindow = await this.sourceWindow(cursor, this.config.scanBatchSize)
      const forward = await this.preferredCandidates(forwardWindow)
      for (const report of forward) await this.queueReport(report)
      const nextCursor = forwardWindow.length ? forwardWindow[forwardWindow.length - 1]! : cursor

      // Repeated bounded source windows also cover late analyses and reports
      // which become valid/public below the forward cursor. The same exact
      // candidate-driven join protects this rescan from optimizer range drift.
      const window = await this.sourceWindow(rescan, this.config.rescanWindowSize)
      let nextRescan = this.config.startSeq - 1
      let queued = false
      if (window.length) {
        const earlier = await this.preferredCandidates(window, true)
        const batch = earlier.slice(0, this.config.scanBatchSize)
        for (const report of batch) queued = await this.queueReport(report) || queued
        nextRescan = earlier.length > batch.length
          ? Number(batch[batch.length - 1]!.seq) : window[window.length - 1]!
      }
      // Intent and both source cursors commit together. Imported outbox IDs
      // and legacy sequence maxima never initialize either production cursor.
      await this.db.query(`UPDATE epc_report_cursors SET last_report_seq=?,rescan_report_seq=?
        WHERE campaign=? AND source_namespace=?`, [nextCursor, nextRescan, CAMPAIGN, this.config.namespace])
      return forwardWindow.length > 0 || queued
    })
  }

  async claimNextNotification(): Promise<PendingNotification | undefined> {
    const filter = eligibility(this.config)
    return this.inTransaction(async () => {
      const [limits] = await this.db.query<RowDataPacket[]>(`SELECT
        COALESCE(dispatch_not_before>UTC_TIMESTAMP(3),FALSE) AS cooling
        FROM epc_report_cursors WHERE campaign=? AND source_namespace=? FOR UPDATE`,
      [CAMPAIGN, this.config.namespace])
      if (!limits[0]) throw new Error('Missing EPC dispatch cursor')
      if (Number(limits[0].cooling)) return undefined
      const [rows] = await this.db.query<QueuedRow[]>(`SELECT o.*,c.address FROM epc_outbox o
        JOIN epc_contracts c ON c.id=o.contract_id
        WHERE o.source_namespace=? AND o.campaign=? AND o.status IN ('pending','held')
          AND o.source_report_seq>=?
          AND (o.next_attempt_at IS NULL OR o.next_attempt_at<=UTC_TIMESTAMP(3))
        ORDER BY o.id LIMIT 1 FOR UPDATE`, [this.config.namespace, CAMPAIGN, this.config.startSeq])
      const row = rows[0]
      if (!row) return undefined
      const metadata = typeof row.metadata === 'string' ? JSON.parse(row.metadata) : row.metadata
      const [reports] = await this.db.query<Report[]>(`SELECT a.seq,a.brand_name,a.language
        FROM report_analysis a WHERE a.seq=? AND a.language <=> ? AND ${filter.sql} LIMIT 1`,
      [row.source_report_seq, metadata.report_language ?? null, ...filter.params])
      if (!reports[0] || reports[0].brand_name.trim() !== metadata.report_brand || !row.message_body) {
        await this.db.query(`UPDATE epc_outbox SET status='error',last_error_code='report_not_eligible'
          WHERE id=?`, [row.id])
        return undefined
      }
      await this.db.query(`UPDATE epc_outbox SET status='sending',attempt_count=attempt_count+1,
        last_attempt_at=UTC_TIMESTAMP(3),last_error_code=NULL WHERE id=?`, [row.id])
      return {id: row.id, address: row.address, message: row.message_body, attemptCount: row.attempt_count + 1}
    })
  }

  async markSent(id: number, messageId: string): Promise<void> {
    const [result] = await this.db.query<ResultSetHeader>(`UPDATE epc_outbox SET status='sent',
      blockscan_message_id=?,metadata=JSON_SET(COALESCE(metadata,JSON_OBJECT()),'$.blockscanMessageId',?),
      sent_at=UTC_TIMESTAMP(3),next_attempt_at=NULL,last_error_code=NULL
      WHERE id=? AND source_namespace=? AND status='sending'`, [messageId, messageId, id, this.config.namespace])
    if (result.affectedRows !== 1) throw new Error('EPC sent receipt was not persisted')
  }

  async markFailed(id: number, status: 'pending' | 'error' | 'uncertain', code: string, retryDelayMs?: number, cooldownMs?: number): Promise<void> {
    await this.inTransaction(async () => {
      if (cooldownMs) {
        await this.db.query(`UPDATE epc_report_cursors SET dispatch_not_before=GREATEST(
          COALESCE(dispatch_not_before,UTC_TIMESTAMP(3)),TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(3)))
          WHERE campaign=? AND source_namespace=?`, [cooldownMs * 1000, CAMPAIGN, this.config.namespace])
      }
      const [result] = await this.db.query<ResultSetHeader>(`UPDATE epc_outbox SET status=?,last_error_code=?,
        next_attempt_at=CASE WHEN ? IS NULL THEN NULL ELSE TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(3)) END
        WHERE id=? AND source_namespace=? AND status='sending'`,
      [status, code, retryDelayMs ?? null, (retryDelayMs ?? 0) * 1000, id, this.config.namespace])
      if (result.affectedRows !== 1) throw new Error('EPC failed attempt was not persisted')
    })
  }

  async recoverInterrupted(): Promise<void> {
    await this.db.query(`UPDATE epc_outbox SET status='uncertain',last_error_code='interrupted_during_send',
      next_attempt_at=NULL WHERE source_namespace=? AND status='sending'`, [this.config.namespace])
  }

  async lastAttemptTime(): Promise<number> {
    const [rows] = await this.db.query<RowDataPacket[]>(`SELECT UNIX_TIMESTAMP(MAX(last_attempt_at))*1000 AS attempted
      FROM epc_outbox WHERE source_namespace=?`, [this.config.namespace])
    return Number(rows[0]?.attempted) || 0
  }
}
