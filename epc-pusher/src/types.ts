
import mysql from 'mysql2/promise'


/*
 * This type maps EPC contract sources to JSON metadata
 */
export type EPCSources = {
  cleanapp: {
    report_seq: number
  }
}



export interface Outbox<K extends keyof EPCSources> extends mysql.RowDataPacket {
  id: number
  campaign_id: number
  contract_id: number
  metadata: EPCSources[K] & {}
  status: string
  error: string | null
  created: any
}
