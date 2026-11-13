import test from 'node:test'
import assert from 'node:assert/strict'
import {mkdtempSync, writeFileSync, rmSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
import {loadConfig, reportEventKey} from '../src/config'
import {loadEnvironmentFile} from '../src/environment'

test('source identity never aliases a legacy sequence with a production sequence', () => {
  assert.notEqual(reportEventKey('legacy-dev', 73434), reportEventKey('prod', 73434))
  assert.equal(loadConfig({}).startSeq, 1)
  assert.equal(loadConfig({}).dispatch, false)
  assert.equal(loadConfig({}).existingChatsOnly, false)
  assert.equal(loadConfig({EPC_EXISTING_CHATS_ONLY:'true'}).existingChatsOnly, true)
  assert.throws(() => loadConfig({EPC_SOURCE_NAMESPACE: 'prod;bad'}))
  assert.throws(() => loadConfig({EPC_SEND_INTERVAL_MS: '1'}))
  assert.throws(() => loadConfig({EPC_REPORTS_START_SEQ: 'not-a-number'}))
})

test('protected environment values are literal and existing process values win', () => {
  const dir = mkdtempSync(join(tmpdir(), 'epc-env-'))
  try {
    const path = join(dir, 'worker.env')
    writeFileSync(path, '# literal\nBLOCKSCAN_CHAT_API_KEY=$(touch forbidden)\nDB_PASSWORD=a=b$HOME\nEPC_DISPATCH=true\n')
    const env = {EPC_DISPATCH: 'false'}
    loadEnvironmentFile(path, env)
    assert.deepEqual(env, {EPC_DISPATCH: 'false', BLOCKSCAN_CHAT_API_KEY: '$(touch forbidden)', DB_PASSWORD: 'a=b$HOME'})
    writeFileSync(path, 'PATH=malicious\n')
    assert.throws(() => loadEnvironmentFile(path, {}), /unsupported/)
    writeFileSync(path, 'export EPC_DISPATCH=true\n')
    assert.throws(() => loadEnvironmentFile(path, {}), /unsupported/)
  } finally {rmSync(dir, {recursive: true, force: true})}
})
