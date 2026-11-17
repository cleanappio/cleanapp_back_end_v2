-- group: effect
-- title: Reports marked resolved
-- kind: count
-- window: last 30 days
-- better: higher
-- definition: Reports whose status became resolved in the last 30 days. For physical reports this is set when a later photo of the same place shows the problem gone, or by hand.
SELECT COUNT(*)
FROM report_status
WHERE status = 'resolved'
  AND updated_at >= @activity_from AND updated_at < @as_of
