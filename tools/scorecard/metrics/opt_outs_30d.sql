-- group: noise
-- title: Recipients who opted out
-- kind: count
-- window: last 30 days
-- better: lower
-- definition: Addresses added to the opt-out list in the last 30 days.
SELECT COUNT(*)
FROM opted_out_emails
WHERE opted_out_at >= @activity_from AND opted_out_at < @as_of
