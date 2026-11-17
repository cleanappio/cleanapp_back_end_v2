-- group: noise
-- title: Share of what was sent that is first-hand
-- kind: ratio
-- window: last 30 days
-- better: higher
-- definition: Of report-recipient pairs sent in the last 30 days, the share whose report is labelled first-hand. The rest is coverage, unknown or unlabelled: material a recipient can fairly call noise.
SELECT COALESCE(SUM(e.report_kind = 'firsthand'), 0), COUNT(*)
FROM report_email_deliveries d
LEFT JOIN report_evidence e ON e.seq = d.seq
WHERE d.sent_at >= @activity_from AND d.sent_at < @as_of
