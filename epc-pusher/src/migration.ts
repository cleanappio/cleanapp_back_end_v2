import {Connection} from 'mysql2/promise'

// MySQL CLI DELIMITER is a client directive, not SQL. Keep procedure bodies
// together when applying the explicit, reviewed migration through mysql2.
export async function applyMigration(db: Connection, sql: string): Promise<void> {
  let delimiter = ';'
  let statement = ''
  for (const line of sql.split('\n')) {
    const change = /^DELIMITER\s+(\S+)\s*$/i.exec(line)
    if (change) {delimiter = change[1]!; continue}
    statement += `${line}\n`
    if (statement.trimEnd().endsWith(delimiter)) {
      const query = statement.trimEnd().slice(0, -delimiter.length).trim()
      if (query) await db.query(query)
      statement = ''
    }
  }
  if (statement.replace(/--[^\n]*/g, '').trim()) throw new Error('Unterminated migration statement')
}
