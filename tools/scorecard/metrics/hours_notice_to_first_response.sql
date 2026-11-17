-- group: latency
-- title: Hours from a notice to the first thing that came back
-- kind: median
-- window: matured
-- better: lower
-- definition: Median, over notices sent in the matured window that got a view, an automatic acknowledgement or a reply, of the time from sending to the first of those. A bounce is not a response. The second number is how many notices the median is taken over.
WITH first_response AS (
  SELECT source_ref, MIN(recorded_at) AS at
  FROM notify_outcomes
  WHERE outcome_type IN ('viewed', 'acknowledged', 'replied') AND source_ref <> ''
  GROUP BY source_ref
), delay AS (
  SELECT TIMESTAMPDIFF(MINUTE, m.sent_at, f.at) / 60 AS h
  FROM notification_messages m
  JOIN first_response f
    ON f.source_ref = CONVERT(m.notification_id USING utf8mb4) COLLATE utf8mb4_0900_ai_ci
  WHERE m.sent_at >= @matured_from AND m.sent_at < @matured_to AND f.at >= m.sent_at
), ranked AS (
  SELECT h, ROW_NUMBER() OVER (ORDER BY h) AS rn, COUNT(*) OVER () AS n FROM delay
)
SELECT AVG(h), COALESCE(MAX(n), 0)
FROM ranked
WHERE rn IN (FLOOR((n + 1) / 2), CEIL((n + 1) / 2))
