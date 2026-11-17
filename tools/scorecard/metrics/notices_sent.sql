-- group: reach
-- title: Notices sent
-- kind: count
-- window: matured
-- better: context
-- definition: Notification messages sent in the matured window. Only messages sent with reply tracking on are counted, because only those can be tied to what came back.
SELECT COUNT(*)
FROM notification_messages m
WHERE m.sent_at >= @matured_from AND m.sent_at < @matured_to
