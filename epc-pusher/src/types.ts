
import mysql from 'mysql2/promise'


/*
 * This type maps EPC campaigns to JSON metadata
 */
export type EPCCampaigns = {
  reports: {
    report_seq: number
  }
}



export interface Outbox<K extends keyof EPCCampaigns> extends mysql.RowDataPacket {
  id: number
  campaign_id: number
  contract_id: number
  metadata: EPCCampaigns[K] & {}
  status: string
  error: string | null
  created: any
}
