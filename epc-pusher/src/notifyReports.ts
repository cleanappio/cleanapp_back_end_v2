import mysql from 'mysql2/promise'
import {readFileSync} from 'node:fs'
import {getEpcAddress} from './epc'
import {loadConfig} from './config'
import {loadEnvironmentFile} from './environment'
import {MysqlNotificationStore} from './repository'
import {sendChat} from './blockscan'
import {Worker} from './worker'

async function main(): Promise<void> {
  if (process.env.EPC_ENV_FILE) loadEnvironmentFile(process.env.EPC_ENV_FILE)
  const config = loadConfig()
  if (config.dispatch && !(process.env.BLOCKSCAN_CHAT_API_KEY || '').trim()) {
    throw new Error('BLOCKSCAN_CHAT_API_KEY is required for dispatch')
  }
  if (!/^0x[0-9a-fA-F]{40}$/.test(process.env.EPC_CONTRACT_ADDRESS || '')) {
    throw new Error('EPC_CONTRACT_ADDRESS must be an explicit factory address')
  }
  const dbPassword = process.env.DB_PASSWORD || process.env.MYSQL_APP_PASSWORD || ''
  if (process.env.NODE_ENV === 'production' && !dbPassword.trim()) {
    throw new Error('A protected database password is required in production')
  }
  const pool = mysql.createPool({
    connectionLimit: 2,
    host: process.env.DB_HOST || 'cleanapp_db',
    port: Number(process.env.DB_PORT || 3306),
    user: process.env.DB_USER || 'server',
    password: dbPassword,
    database: process.env.DB_NAME || 'cleanapp',
    timezone: 'Z',
    supportBigNumbers: true,
  })
  const db = await pool.getConnection()
  const template = readFileSync(new URL('../lib/templates/report_notification.tpl', import.meta.url), 'utf8')
  const store = new MysqlNotificationStore(db, config, template, getEpcAddress)
  let stopping = false
  process.once('SIGTERM', () => {stopping = true})
  process.once('SIGINT', () => {stopping = true})
  try {
    await store.initialize()
    const worker = new Worker(store, config, (address, message) =>
      sendChat(address, message, {timeoutMs: config.requestTimeoutMs}))
    await worker.initialize()
    console.log(`EPC worker ready namespace=${config.namespace} dispatch=${config.dispatch}`)
    while (!stopping) {
      const active = await worker.tick()
      if (!active) await new Promise(resolve => setTimeout(resolve, config.pollMs))
    }
  } finally {
    await store.close().catch(() => {})
    db.release()
    await pool.end()
  }
}

main().catch(() => {
  // Driver/network errors can contain connection or request details. Operational
  // outcome codes above are safe; unclassified failures stop without dumping them.
  console.error('EPC worker stopped; check database, migration and protected configuration')
  process.exitCode = 1
})
