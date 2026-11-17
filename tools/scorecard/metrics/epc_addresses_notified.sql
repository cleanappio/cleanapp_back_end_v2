-- group: public_notice
-- title: EPC addresses that have received a notice
-- kind: count
-- window: all time
-- better: higher
-- definition: Distinct EPC addresses with at least one sent notice.
SELECT COUNT(DISTINCT contract_id) FROM epc_outbox WHERE status = 'sent'
