
import mysql from 'mysql2/promise'
import {EPCCampaigns, Outbox} from './types'


export type Queryable = mysql.Connection | mysql.Pool


export async function getLatestByCampaign<K extends keyof EPCCampaigns>(db: Queryable, campaign: K) {
  let sql = `
  select o.* from epc_outbox o
  where o.campaign = ?
  order by o.id desc
  limit 1
  `

  let [r, _] = await db.query<Outbox<K>[]>(sql, [campaign])
  return r[0]
}

