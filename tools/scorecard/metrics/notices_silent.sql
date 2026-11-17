-- group: reach
-- title: Notices silent
-- kind: ratio
-- window: matured
-- better: lower
-- definition: Share of notices sent in the matured window where nothing came back within the waiting period, recorded as no_response.
SELECT COALESCE(SUM(x.hit), 0), COUNT(*)
FROM notification_messages m
LEFT JOIN (
  SELECT source_ref, 1 AS hit
  FROM notify_outcomes
  WHERE outcome_type = 'no_response' AND source_ref <> ''
  GROUP BY source_ref
) x ON x.source_ref = CONVERT(m.notification_id USING utf8mb4) COLLATE utf8mb4_0900_ai_ci
WHERE m.sent_at >= @matured_from AND m.sent_at < @matured_to
