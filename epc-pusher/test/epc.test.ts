import test from 'node:test'
import assert from 'node:assert/strict'
import {getEpcAddress} from '../src/epc'

test('configured factory reproduces actual archived EPC destinations exactly', () => {
  const oldFactory = process.env.EPC_CONTRACT_ADDRESS
  process.env.EPC_CONTRACT_ADDRESS = '0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA'
  const fixtures = [
    ['youtube', '0x4aCD44cB30286a5254CfeBD7f79C57D2F9B8217f'],
    ['google ads', '0xDBCf8e3651c79f39517c810b899BD0FB3e9fC496'],
    ['apple', '0x863BA6CD3EeAe109f6fD68DfBa10A0501Ce023Cd'],
    ['openai', '0x05BCff538DD502Db27c0696A7cEc0e2a97f08488'],
    ['bluesky', '0x6D496AEA1d8461b9eB256437256b8E3937BBC490'],
    ['microsoft', '0xE3290D5f65b0fcdA75B74556655fa876C8646457'],
  ]
  try {
    for (const [brand, expected] of fixtures) assert.equal(getEpcAddress(`cleanapp/brand/${brand}`), expected)
  } finally {
    if (oldFactory === undefined) delete process.env.EPC_CONTRACT_ADDRESS
    else process.env.EPC_CONTRACT_ADDRESS = oldFactory
  }
})
