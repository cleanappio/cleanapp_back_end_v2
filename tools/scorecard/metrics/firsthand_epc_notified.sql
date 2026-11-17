-- group: public_notice
-- title: First-hand reports with an EPC notice sent
-- kind: ratio
-- window: matured
-- better: higher
-- definition: Share of the eligible first-hand reports received in the matured window (the chain's cohort) for which a production EPC notice has status sent. The public-notice counterpart of chain_2_notified.
SELECT COALESCE(SUM(EXISTS (
         SELECT 1 FROM epc_outbox x
         WHERE x.source_namespace = 'prod' AND x.source_report_seq = c.seq AND x.status = 'sent')), 0), COUNT(*)
FROM ({{eligible_firsthand}}) c
WHERE c.received_at >= @matured_from AND c.received_at < @matured_to
