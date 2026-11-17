-- group: public_notice
-- title: EPC notices held back
-- kind: count
-- window: now
-- better: lower
-- definition: EPC notices with status held: queued, and waiting because the chat provider does not yet accept the recipient.
SELECT COUNT(*) FROM epc_outbox WHERE status = 'held'
