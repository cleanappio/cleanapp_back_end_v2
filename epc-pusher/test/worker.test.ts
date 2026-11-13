import test from 'node:test'
import assert from 'node:assert/strict'
import {BlockscanError} from '../src/blockscan'
import {loadConfig} from '../src/config'
import {NotificationStore, PendingNotification, Worker} from '../src/worker'

class Store implements NotificationStore {
  state = 'pending'
  attempts = 0
  delay: number | undefined
  cooldown: number | undefined
  last = 0
  async queueNextReport() {return false}
  async claimNextNotification(): Promise<PendingNotification | undefined> {
    if (this.state !== 'pending') return undefined
    this.state = 'sending'; this.attempts++
    return {id: 1, address: 'legacy-address', message: 'notice', attemptCount: this.attempts}
  }
  async markSent() {this.state = 'sent'}
  async markFailed(_id: number, state: 'pending' | 'error' | 'uncertain', _code: string, delay?: number, cooldown?: number) {this.state = state; this.delay = delay; this.cooldown = cooldown}
  async recoverInterrupted() {if (this.state === 'sending') this.state = 'uncertain'}
  async lastAttemptTime() {return this.last}
}
const config = loadConfig({EPC_DISPATCH: 'true'})

test('durable sending precedes HTTP and ambiguous transport never auto-resends', async () => {
  const store = new Store()
  let sends = 0
  const worker = new Worker(store, config, async () => {
    assert.equal(store.state, 'sending'); sends++
    throw new BlockscanError('timeout', 'uncertain')
  }, async () => {})
  await worker.initialize(); await worker.tick(); await worker.tick()
  assert.equal(store.state, 'uncertain'); assert.equal(sends, 1)
})

test('explicit rejection retries with bounded exponential delay and attempt limit', async () => {
  const store = new Store()
  const worker = new Worker(store, {...config, maxAttempts: 2}, async () => {
    throw new BlockscanError('throttle', 'retryable', 90_000)
  }, async () => {})
  await worker.initialize(); await worker.tick()
  assert.equal(store.state, 'pending'); assert.equal(store.delay, 90_000); assert.equal(store.cooldown, 90_000)
  await worker.tick()
  assert.equal(store.state, 'error'); assert.equal(store.attempts, 2); assert.equal(store.cooldown, 120_000)
})

test('database failure after provider success leaves uncertain on restart', async () => {
  const store = new Store()
  store.markSent = async () => {throw new Error('database disconnected')}
  let sends = 0
  const worker = new Worker(store, config, async () => {sends++; return 'provider-receipt'}, async () => {})
  await worker.initialize()
  await assert.rejects(worker.tick(), /database disconnected/)
  assert.equal(store.state, 'sending')
  await worker.initialize(); await worker.tick()
  assert.equal(store.state, 'uncertain'); assert.equal(sends, 1)
})

test('pacing continues from the last durable attempt across restart', async () => {
  const store = new Store(); store.last = 5000
  let clock = 5500; const delays: number[] = []
  const worker = new Worker(store, config, async () => 'receipt', async ms => {delays.push(ms); clock += ms}, () => clock)
  await worker.initialize(); await worker.tick()
  assert.deepEqual(delays, [700])
})
