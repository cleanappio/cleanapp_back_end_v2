import {readFileSync} from 'node:fs'

const ALLOWED = new Set([
  'DB_HOST','DB_PORT','DB_USER','DB_NAME','DB_PASSWORD','MYSQL_APP_PASSWORD',
  'BLOCKSCAN_CHAT_API_KEY','EPC_CONTRACT_ADDRESS','EPC_SOURCE_NAMESPACE',
  'EPC_REPORTS_START_SEQ','EPC_DISPATCH','EPC_ONLY_VALID','EPC_FILTER_LANGUAGE',
  'EPC_FILTER_SOURCE','EPC_SEND_INTERVAL_MS','EPC_REQUEST_TIMEOUT_MS',
  'EPC_RETRY_BASE_MS','EPC_RETRY_MAX_MS','EPC_MAX_ATTEMPTS','EPC_POLL_MS',
  'EPC_SCAN_BATCH_SIZE','EPC_RESCAN_WINDOW_SIZE',
])

export function loadEnvironmentFile(path: string, env: NodeJS.ProcessEnv = process.env): void {
  for (const line of readFileSync(path, 'utf8').split(/\r?\n/)) {
    if (!line.trim() || line.trimStart().startsWith('#')) continue
    const match = /^([A-Z][A-Z0-9_]*)=(.*)$/.exec(line)
    if (!match || !ALLOWED.has(match[1]!)) throw new Error('Invalid or unsupported EPC environment entry')
    // Literal values: no shell, interpolation, quote handling or escape parsing.
    if (env[match[1]!] === undefined) env[match[1]!] = match[2]!
  }
}
