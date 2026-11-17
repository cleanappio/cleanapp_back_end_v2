-- group: coverage
-- title: Reports labelled first-hand
-- kind: count
-- window: all time
-- better: higher
-- definition: Reports whose evidence label is first-hand, before any eligibility filter.
SELECT COUNT(*) FROM report_evidence WHERE report_kind = 'firsthand'
