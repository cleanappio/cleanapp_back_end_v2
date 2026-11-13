import test from 'node:test'
import assert from 'node:assert/strict'
import {createServer, IncomingMessage, ServerResponse} from 'node:http'
import {AddressInfo} from 'node:net'
import {BlockscanError, sendChat} from '../src/blockscan'

async function localProvider(handler: (req: IncomingMessage, res: ServerResponse) => void) {
  const server = createServer(handler)
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  const url = `http://127.0.0.1:${(server.address() as AddressInfo).port}/api`
  return {fetch: ((_input, init) => fetch(url, init)) as typeof fetch,
    close: () => new Promise<void>(resolve => {server.closeAllConnections(); server.close(() => resolve())})}
}

test('real HTTP request uses identifiable agent, exact destination and literal form values', async () => {
  let received = ''
  const server = await localProvider((req, res) => {
    assert.equal(req.headers['user-agent'], 'CleanApp-EPC/1.1 (+https://cleanapp.io)')
    req.on('data', chunk => {received += chunk})
    req.on('end', () => {res.end(JSON.stringify({status: '1', message: 'OK', result: '123456'}))})
  })
  try {
    assert.equal(await sendChat('0xabc', 'notice & detail', {apiKey: 'test-key', fetch: server.fetch}), '123456')
    assert.deepEqual(Object.fromEntries(new URLSearchParams(received)),
      {method: 'sendchat', apikey: 'test-key', to: '0xabc', msg: 'notice & detail', replytoid: '0'})
  } finally {await server.close()}
})

test('HTTP throttling is retryable and honors Retry-After', async () => {
  const server = await localProvider((_req, res) => {res.writeHead(429, {'Retry-After': '7'}); res.end('limited')})
  try {
    await assert.rejects(sendChat('a', 'b', {apiKey: 'test-key', fetch: server.fetch}),
      (error: unknown) => error instanceof BlockscanError && error.outcome === 'retryable' && error.retryAfterMs === 7000)
  } finally {await server.close()}
})

test('known authorization rejection is permanent and never exposes provider body', async () => {
  const server = await localProvider((_req, res) => {res.writeHead(403); res.end('private test-key')})
  try {
    await assert.rejects(sendChat('a', 'b', {apiKey: 'test-key', fetch: server.fetch}),
      (error: unknown) => error instanceof BlockscanError && error.outcome === 'permanent' && !error.message.includes('test-key'))
  } finally {await server.close()}
})

test('ambiguous acknowledgment and transport timeout are quarantined', async () => {
  const invalid = await localProvider((_req, res) => {res.end('null')})
  try {
    await assert.rejects(sendChat('a', 'b', {apiKey: 'test-key', fetch: invalid.fetch}),
      (error: unknown) => error instanceof BlockscanError && error.outcome === 'uncertain')
  } finally {await invalid.close()}
  const stalled = await localProvider(() => {})
  try {
    await assert.rejects(sendChat('a', 'b', {apiKey: 'test-key', fetch: stalled.fetch, timeoutMs: 20}),
      (error: unknown) => error instanceof BlockscanError && error.outcome === 'uncertain')
  } finally {await stalled.close()}
})


test('only status1, OK and a positive numeric message ID count as acknowledgment', async () => {
  for (const payload of [
    {message: 'OK', result: '123'},
    {status: '1', message: 'OK', result: 'not-a-message-id'},
    {status: '1', message: 'OK', result: '0'},
    {status: '1', message: 'OK', result: 9007199254740992},
    {status: '1', message: 'NOTOK', result: '123'},
  ]) {
    await assert.rejects(sendChat('a', 'b', {apiKey: 'test-key',
      fetch: (async () => new Response(JSON.stringify(payload))) as typeof fetch}),
    (error: unknown) => error instanceof BlockscanError && error.outcome === 'uncertain')
  }
  await assert.rejects(sendChat('a', 'b', {apiKey: 'test-key',
    fetch: (async () => new Response(JSON.stringify({status: '0',message: 'NOTOK',result: 'Rate limit reached'}))) as typeof fetch}),
  (error: unknown) => error instanceof BlockscanError && error.outcome === 'retryable')
})
