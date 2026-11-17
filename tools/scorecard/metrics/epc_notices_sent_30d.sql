-- group: public_notice
-- title: EPC notices sent
-- kind: count
-- window: last 30 days
-- better: higher
-- definition: EPC notices with status sent whose send time (or, for imported rows without one, creation time) falls in the last 30 days.
SELECT COUNT(*)
FROM epc_outbox
WHERE status = 'sent'
  AND COALESCE(sent_at, created) >= @activity_from AND COALESCE(sent_at, created) < @as_of
