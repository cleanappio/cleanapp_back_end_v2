-- group: chain
-- title: ... of which sent to anyone
-- kind: ratio
-- window: matured
-- better: higher
-- definition: Share of those reports that appear in at least one email delivery, to any recipient, at any time.
SELECT COALESCE(SUM(EXISTS (SELECT 1 FROM report_email_deliveries d WHERE d.seq = c.seq)), 0), COUNT(*)
FROM ({{eligible_firsthand}}) c
WHERE c.received_at >= @matured_from AND c.received_at < @matured_to
