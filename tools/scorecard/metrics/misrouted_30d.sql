-- group: noise
-- title: Notices that went to the wrong party
-- kind: count
-- window: last 30 days
-- better: lower
-- definition: Misrouted outcomes recorded in the last 30 days.
SELECT COUNT(*)
FROM notify_outcomes
WHERE outcome_type = 'misrouted'
  AND recorded_at >= @activity_from AND recorded_at < @as_of
