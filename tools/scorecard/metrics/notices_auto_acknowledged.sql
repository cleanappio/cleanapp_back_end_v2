-- group: reach
-- title: Notices auto acknowledged
-- kind: ratio
-- window: matured
-- better: context
-- definition: Share of notices sent in the matured window where an automatic acknowledgement came back, such as a help-desk ticket number.
SELECT COALESCE(SUM(x.hit), 0), COUNT(*)
FROM notification_messages m
LEFT JOIN (
  SELECT source_ref, 1 AS hit
  FROM notify_outcomes
  WHERE outcome_type = 'acknowledged' AND source_ref <> ''
  GROUP BY source_ref
) x ON x.source_ref = CONVERT(m.notification_id USING utf8mb4) COLLATE utf8mb4_0900_ai_ci
WHERE m.sent_at >= @matured_from AND m.sent_at < @matured_to
