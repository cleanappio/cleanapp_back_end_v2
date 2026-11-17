-- Shared definition, included by other metrics as {{eligible_firsthand}}.
-- A first-hand report that CleanApp may put on notice: the evidence says the
-- author experienced the problem, the analysis is valid, and the report is
-- not quarantined, not an import barred from notices, and not private to an
-- owner. received_at is when CleanApp got it: machine intake time when there
-- is one, otherwise the time it was submitted.
SELECT r.seq, r.public_id, COALESCE(rr.created_at, r.ts) AS received_at
FROM report_evidence e
JOIN reports r ON r.seq = e.seq
LEFT JOIN report_raw rr ON rr.report_seq = e.seq
WHERE e.report_kind = 'firsthand'
  AND (rr.report_seq IS NULL OR rr.visibility IS NULL OR rr.visibility = 'public')
  AND EXISTS (SELECT 1 FROM report_analysis a WHERE a.seq = e.seq AND a.language = 'en' AND a.is_valid)
  AND NOT EXISTS (SELECT 1 FROM report_side_effect_policy p WHERE p.report_seq = e.seq AND p.suppress_notifications)
  AND NOT EXISTS (SELECT 1 FROM reports_owners ro WHERE ro.seq = e.seq AND ro.owner <> '' AND ro.is_public = FALSE)
