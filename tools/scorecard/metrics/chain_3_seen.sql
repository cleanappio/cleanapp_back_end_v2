-- group: chain
-- title: ... of which seen by a person at the party
-- kind: ratio
-- window: matured
-- better: higher
-- definition: Share with a viewed outcome (a tracked link in a notice was followed by a person) or a replied outcome (a person wrote back). An automatic acknowledgement does not count.
SELECT COALESCE(SUM(EXISTS (
         SELECT 1 FROM notify_outcomes o
         WHERE o.subject_kind = 'report'
           AND o.subject_ref = CONVERT(c.public_id USING utf8mb4) COLLATE utf8mb4_0900_ai_ci
           AND o.outcome_type IN ('viewed', 'replied'))), 0), COUNT(*)
FROM ({{eligible_firsthand}}) c
WHERE c.received_at >= @matured_from AND c.received_at < @matured_to
