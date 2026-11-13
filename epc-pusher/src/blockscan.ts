const API_URL = 'https://chatapi.blockscan.com/v1/api'

export class BlockscanError extends Error {
  constructor(
    readonly code: string,
    readonly outcome: 'retryable' | 'permanent' | 'uncertain',
    readonly retryAfterMs?: number,
  ) {
    // Provider response bodies and credentials must never enter logs/errors.
    super(code)
    this.name = 'BlockscanError'
  }
}

export interface SendOptions {
  apiKey?: string
  timeoutMs?: number
  fetch?: typeof fetch
}

export async function sendChat(address: string, msg: string, options: SendOptions = {}): Promise<string> {
  const apikey = (options.apiKey ?? process.env.BLOCKSCAN_CHAT_API_KEY ?? '').trim()
  if (!apikey) throw new BlockscanError('blockscan_key_missing', 'permanent')
  let response: Response
  try {
    response = await (options.fetch ?? fetch)(API_URL, {
      method: 'POST',
      headers: {'User-Agent': 'CleanApp-EPC/1.1 (+https://cleanapp.io)'},
      body: new URLSearchParams({method: 'sendchat', apikey, to: address, msg, replytoid: '0'}),
      signal: AbortSignal.timeout(options.timeoutMs ?? 30_000),
    })
  } catch {
    // A connection failure/timeout may follow provider acceptance.
    throw new BlockscanError('blockscan_transport_uncertain', 'uncertain')
  }
  if (response.status === 429) {
    const header = response.headers.get('retry-after')
    const delay = header && /^\d+$/.test(header)
      ? Number(header) * 1_000
      : header ? Math.max(0, Date.parse(header) - Date.now()) : undefined
    throw new BlockscanError('blockscan_rate_limited', 'retryable', Number.isFinite(delay) ? delay : undefined)
  }
  if ([400, 401, 403, 404, 422].includes(response.status)) {
    throw new BlockscanError(`blockscan_http_${response.status}`, 'permanent')
  }
  if (response.status !== 200) {
    throw new BlockscanError(`blockscan_http_${response.status}_uncertain`, 'uncertain')
  }
  let data: {status?: unknown, message?: unknown, result?: unknown}
  try {
    const parsed: unknown = await response.json()
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Invalid acknowledgment')
    data = parsed as typeof data
  } catch {
    throw new BlockscanError('blockscan_response_uncertain', 'uncertain')
  }
  if (data.status === 0 || data.status === '0') {
    const rejection = `${typeof data.message === 'string' ? data.message : ''} ${typeof data.result === 'string' ? data.result : ''}`
    if (/rate.?limit|too many|temporar|busy/i.test(rejection)) {
      throw new BlockscanError('blockscan_rejected_retryable', 'retryable')
    }
    throw new BlockscanError('blockscan_rejected', 'permanent')
  }
  if ((data.status !== 1 && data.status !== '1') || data.message !== 'OK') {
    throw new BlockscanError('blockscan_acknowledgment_uncertain', 'uncertain')
  }
  const messageId = String(data.result)
  if ((typeof data.result !== 'string' && typeof data.result !== 'number')
      || (typeof data.result === 'number' && !Number.isSafeInteger(data.result))
      || !/^[1-9][0-9]{0,127}$/.test(messageId)) {
    throw new BlockscanError('blockscan_acknowledgment_uncertain', 'uncertain')
  }
  return messageId
}
