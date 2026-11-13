import mysql from 'mysql2/promise'
import {readFileSync} from 'node:fs'
import {loadEnvironmentFile} from './environment'
import {applyMigration} from './migration'

async function main(): Promise<void> {
  if (process.env.EPC_ENV_FILE) loadEnvironmentFile(process.env.EPC_ENV_FILE)
  const path = process.argv[2]
  if (!path || !process.env.DB_NAME) throw new Error('Explicit migration file and DB_NAME are required')
  const db = await mysql.createConnection({
    host: process.env.DB_HOST || 'cleanapp_db', port: Number(process.env.DB_PORT || 3306),
    user: process.env.DB_USER || 'server', database: process.env.DB_NAME,
    password: process.env.DB_PASSWORD || process.env.MYSQL_APP_PASSWORD || '',
    timezone: 'Z',
  })
  try {
    await db.query("SET SESSION time_zone='+00:00'")
    await applyMigration(db, readFileSync(path, 'utf8'))
  }
  finally {await db.end()}
  console.log('EPC migration completed')
}
main().catch(() => {console.error('EPC migration failed; check reviewed migration and protected database configuration'); process.exitCode = 1})
