-- group: reach
-- title: Parties where a person has seen a notice, ever
-- kind: count
-- window: all time
-- better: higher
-- definition: Distinct recipient mail domains with at least one viewed or replied outcome since records began. This number can only go up.
SELECT COUNT(DISTINCT LOWER(SUBSTRING_INDEX(o.endpoint_key, '@', -1)))
FROM notify_outcomes o
WHERE o.outcome_type IN ('viewed', 'replied')
  AND o.endpoint_key LIKE '%@%'
  AND o.recorded_at < @as_of
