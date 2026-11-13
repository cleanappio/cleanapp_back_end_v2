import {BlockscanError} from './blockscan'
import {Config} from './config'

export interface PendingNotification {
  id: number
  address: string
  message: string
  attemptCount: number
}

export interface NotificationStore {
  queueNextReport(): Promise<boolean>
  claimNextNotification(): Promise<PendingNotification | undefined>
  markSent(id: number, messageId: string): Promise<void>
  markFailed(id: number, status: 'pending' | 'error' | 'uncertain', code: string, retryDelayMs?: number, cooldownMs?: number): Promise<void>
  recoverInterrupted(): Promise<void>
  lastAttemptTime(): Promise<number>
}

export class Worker {
  private nextSendAt = 0

  constructor(
    private store: NotificationStore,
    private config: Config,
    private send: (address: string, message: string) => Promise<string>,
    private sleep: (ms: number) => Promise<void> = ms => new Promise(resolve => setTimeout(resolve, ms)),
    private now: () => number = Date.now,
  ) {}

  async initialize(): Promise<void> {
    await this.store.recoverInterrupted()
    this.nextSendAt = await this.store.lastAttemptTime() + this.config.sendIntervalMs
  }

  async tick(): Promise<boolean> {
    const queued = await this.store.queueNextReport()
    if (!this.config.dispatch) return queued
    const delay = Math.max(0, this.nextSendAt - this.now())
    if (delay) await this.sleep(delay)
    // Claim commits the durable sending state before any external request.
    const notification = await this.store.claimNextNotification()
    if (!notification) return queued
    this.nextSendAt = this.now() + this.config.sendIntervalMs
    let messageId: string
    try {
      messageId = await this.send(notification.address, notification.message)
    } catch (error) {
      const failure = error instanceof BlockscanError
        ? error : new BlockscanError('blockscan_unexpected_uncertain', 'uncertain')
      const retry = failure.outcome === 'retryable' && notification.attemptCount < this.config.maxAttempts
      const status = retry ? 'pending' : failure.outcome === 'uncertain' ? 'uncertain' : 'error'
      const backoff = Math.min(this.config.retryMaxMs,
        this.config.retryBaseMs * 2 ** Math.min(notification.attemptCount - 1, 20))
      const throttleDelay = Math.max(backoff, failure.retryAfterMs || 0)
      await this.store.markFailed(notification.id, status, failure.code,
        retry ? throttleDelay : undefined,
        failure.outcome === 'retryable' ? throttleDelay : undefined)
      console.warn(`EPC dispatch outbox=${notification.id} status=${status} code=${failure.code}`)
      return true
    }
    // A database failure after API success leaves sending durable. Restart
    // quarantines it as uncertain instead of automatically duplicating it.
    await this.store.markSent(notification.id, messageId)
    console.log(`EPC dispatch outbox=${notification.id} status=sent blockscanMessageId=${messageId}`)
    return true
  }
}
