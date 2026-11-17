-- group: identity
-- title: Reports attached to a case
-- kind: count
-- window: all time
-- better: higher
-- definition: Distinct reports attached to at least one case.
SELECT COUNT(DISTINCT seq) FROM case_reports
