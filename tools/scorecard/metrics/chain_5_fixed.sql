-- group: chain
-- title: ... of which fixed, with evidence
-- kind: ratio
-- window: matured
-- better: higher
-- definition: Share with a fixed outcome on record. A fix is never inferred from a send or from silence.
SELECT COALESCE(SUM(EXISTS (
         SELECT 1 FROM notify_outcomes o
         WHERE o.subject_kind = 'report'
           AND o.subject_ref = CONVERT(c.public_id USING utf8mb4) COLLATE utf8mb4_0900_ai_ci
           AND o.outcome_type = 'fixed')), 0), COUNT(*)
FROM ({{eligible_firsthand}}) c
WHERE c.received_at >= @matured_from AND c.received_at < @matured_to
