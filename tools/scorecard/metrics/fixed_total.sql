-- group: effect
-- title: Reports with a fix on record
-- kind: count
-- window: all time
-- better: higher
-- definition: Distinct reports with a fixed outcome since records began.
SELECT COUNT(DISTINCT subject_ref)
FROM notify_outcomes
WHERE subject_kind = 'report' AND outcome_type = 'fixed'
