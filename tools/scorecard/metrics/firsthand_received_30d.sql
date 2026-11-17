-- group: coverage
-- title: Eligible first-hand reports received
-- kind: count
-- window: last 30 days
-- better: higher
-- definition: Eligible first-hand reports received in the last 30 days.
SELECT COUNT(*)
FROM ({{eligible_firsthand}}) c
WHERE c.received_at >= @activity_from AND c.received_at < @as_of
