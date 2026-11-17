-- group: chain
-- title: First-hand reports received
-- kind: count
-- window: matured
-- better: higher
-- definition: Eligible first-hand reports received in the matured window. The denominator of every later step.
SELECT COUNT(*)
FROM ({{eligible_firsthand}}) c
WHERE c.received_at >= @matured_from AND c.received_at < @matured_to
