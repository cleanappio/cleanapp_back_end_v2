-- group: public_notice
-- title: EPC notices sent
-- kind: count
-- window: all time
-- better: higher
-- definition: Rows in the EPC outbox with status sent, including notices imported from the earlier development system.
SELECT COUNT(*) FROM epc_outbox WHERE status = 'sent'
