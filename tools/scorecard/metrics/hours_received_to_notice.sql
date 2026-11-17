-- group: latency
-- title: Hours from receiving a report to first sending it
-- kind: median
-- window: last 30 days
-- better: lower
-- definition: Median, over eligible first-hand reports received in the last 30 days that were sent, of the time from receipt to the first email delivery. The second number is how many reports the median is taken over.
WITH delay AS (
  SELECT TIMESTAMPDIFF(MINUTE, c.received_at, s.first_sent) / 60 AS h
  FROM ({{eligible_firsthand}}) c
  JOIN (
    SELECT seq, MIN(sent_at) AS first_sent
    FROM report_email_delivery_events
    WHERE delivery_status = 'sent' AND sent_at IS NOT NULL
    GROUP BY seq
  ) s ON s.seq = c.seq
  WHERE c.received_at >= @activity_from AND c.received_at < @as_of
    AND s.first_sent >= c.received_at
), ranked AS (
  SELECT h, ROW_NUMBER() OVER (ORDER BY h) AS rn, COUNT(*) OVER () AS n FROM delay
)
SELECT AVG(h), COALESCE(MAX(n), 0)
FROM ranked
WHERE rn IN (FLOOR((n + 1) / 2), CEIL((n + 1) / 2))
