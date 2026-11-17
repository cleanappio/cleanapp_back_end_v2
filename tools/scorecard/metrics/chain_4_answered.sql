-- group: chain
-- title: ... of which answered by a person
-- kind: ratio
-- window: matured
-- better: higher
-- definition: Share with a replied outcome: a message with no automatic markers came back to a notice that carried the report.
SELECT COALESCE(SUM(EXISTS (
         SELECT 1 FROM notify_outcomes o
         WHERE o.subject_kind = 'report'
           AND o.subject_ref = CONVERT(c.public_id USING utf8mb4) COLLATE utf8mb4_0900_ai_ci
           AND o.outcome_type = 'replied')), 0), COUNT(*)
FROM ({{eligible_firsthand}}) c
WHERE c.received_at >= @matured_from AND c.received_at < @matured_to
