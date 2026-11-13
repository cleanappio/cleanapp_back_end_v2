export interface Config {
  namespace: string
  startSeq: number
  dispatch: boolean
  onlyValid: boolean
  language?: string
  source?: string
  sendIntervalMs: number
  requestTimeoutMs: number
  retryBaseMs: number
  retryMaxMs: number
  maxAttempts: number
  pollMs: number
  scanBatchSize: number
  rescanWindowSize: number
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): Config {
  const integer = (name: string, fallback: number, minimum: number) => {
    const value = Number(env[name] ?? fallback)
    if (!Number.isSafeInteger(value) || value < minimum) {
      throw new Error(`${name} must be an integer >= ${minimum}`)
    }
    return value
  }
  const truthy = (value: string | undefined) => /^(1|true|yes|on)$/i.test(value || '')
  const namespace = env.EPC_SOURCE_NAMESPACE || 'prod'
  if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(namespace)) {
    throw new Error('EPC_SOURCE_NAMESPACE must be 1-64 lowercase ASCII namespace characters')
  }
  const retryBaseMs = integer('EPC_RETRY_BASE_MS', 60_000, 1_000)
  const retryMaxMs = integer('EPC_RETRY_MAX_MS', 3_600_000, retryBaseMs)
  return {
    namespace,
    startSeq: integer('EPC_REPORTS_START_SEQ', 1, 1),
    dispatch: truthy(env.EPC_DISPATCH),
    onlyValid: env.EPC_ONLY_VALID === undefined || truthy(env.EPC_ONLY_VALID),
    language: env.EPC_FILTER_LANGUAGE?.trim() || undefined,
    source: env.EPC_FILTER_SOURCE?.trim() || undefined,
    sendIntervalMs: integer('EPC_SEND_INTERVAL_MS', 1_200, 1_000),
    requestTimeoutMs: integer('EPC_REQUEST_TIMEOUT_MS', 30_000, 1_000),
    retryBaseMs,
    retryMaxMs,
    maxAttempts: integer('EPC_MAX_ATTEMPTS', 8, 1),
    pollMs: integer('EPC_POLL_MS', 5_000, 1_000),
    scanBatchSize: integer('EPC_SCAN_BATCH_SIZE', 100, 1),
    rescanWindowSize: integer('EPC_RESCAN_WINDOW_SIZE', 1_000, 1),
  }
}

export function reportEventKey(namespace: string, seq: number): string {
  if (!Number.isSafeInteger(seq) || seq < 1) throw new Error('Invalid report sequence')
  return `${namespace}:report:${seq}`
}
