-- group: reach
-- title: Parties notified
-- kind: count
-- window: matured
-- better: higher
-- definition: Distinct recipient mail domains that were sent at least one report in the matured window.
SELECT COUNT(DISTINCT LOWER(SUBSTRING_INDEX(d.recipient_email, '@', -1)))
FROM report_email_deliveries d
WHERE d.sent_at >= @matured_from AND d.sent_at < @matured_to
