-- Fixture for test_scorecard.py. Measured as of 2026-11-17 00:00 UTC, so:
--   last 30 days  = [2026-10-18, 2026-11-17)
--   matured window = [2026-10-11, 2026-11-10)
-- Each report below exists to exercise one rule. Expected values are in the test.
SET time_zone = '+00:00';

INSERT INTO reports (seq, public_id, ts, id, team, latitude, longitude, image) VALUES
  (1,  'p1',  '2026-10-20 10:00:00', 'u', 0, 0, 0, ''),  -- first-hand; sent, seen, answered, fixed
  (2,  'p2',  '2026-10-25 00:00:00', 'u', 0, 0, 0, ''),  -- first-hand; sent, bounced
  (3,  'p3',  '2026-10-28 00:00:00', 'u', 0, 0, 0, ''),  -- first-hand; never sent
  (4,  'p4',  '2026-10-22 00:00:00', 'u', 0, 0, 0, ''),  -- coverage; sent (noise)
  (5,  'p5',  '2026-11-12 00:00:00', 'u', 0, 0, 0, ''),  -- first-hand; too recent for the matured window
  (6,  'p6',  '2026-10-21 00:00:00', 'u', 0, 0, 0, ''),  -- first-hand; import barred from notices
  (7,  'p7',  '2026-10-21 00:00:00', 'u', 0, 0, 0, ''),  -- first-hand; quarantined
  (8,  'p8',  '2026-09-01 00:00:00', 'u', 0, 0, 0, ''),  -- first-hand; before every window; seen
  (9,  'p9',  '2026-10-21 00:00:00', 'u', 0, 0, 0, ''),  -- first-hand; analysis invalid
  (10, 'p10', '2026-10-05 00:00:00', 'u', 0, 0, 0, ''),  -- unknown kind; marked resolved
  (11, 'p11', '2026-10-29 00:00:00', 'u', 0, 0, 0, ''),  -- coverage; sent, silence
  (12, 'p12', '2026-10-21 00:00:00', 'u', 0, 0, 0, ''),  -- first-hand; private to an owner
  (13, 'p13', '2020-01-01 00:00:00', 'u', 0, 0, 0, '');  -- first-hand; old post imported 2026-10-23

INSERT INTO report_raw (report_seq, fetcher_id, source_id, visibility, created_at) VALUES
  (7,  'f', 's7',  'shadow', '2026-10-21 00:00:00'),
  (13, 'f', 's13', 'public', '2026-10-23 00:00:00');

INSERT INTO report_evidence (seq, report_kind) VALUES
  (1, 'firsthand'), (2, 'firsthand'), (3, 'firsthand'), (4, 'coverage'), (5, 'firsthand'),
  (6, 'firsthand'), (7, 'firsthand'), (8, 'firsthand'), (9, 'firsthand'), (10, 'unknown'),
  (11, 'coverage'), (12, 'firsthand'), (13, 'firsthand');

INSERT INTO report_analysis (seq, source, brand_name, language, is_valid, classification) VALUES
  (1, 't', 'alpha', 'en', TRUE, 'digital'), (1, 't', 'alpha', 'de', TRUE, 'digital'),
  (2, 't', 'beta', 'en', TRUE, 'digital'), (3, 't', 'gamma', 'en', TRUE, 'digital'),
  (4, 't', 'alpha', 'en', TRUE, 'digital'), (5, 't', 'epsilon', 'en', TRUE, 'digital'),
  (6, 't', 'gamma', 'en', TRUE, 'digital'), (7, 't', 'gamma', 'en', TRUE, 'digital'),
  (8, 't', 'delta', 'en', TRUE, 'digital'), (9, 't', 'gamma', 'en', FALSE, 'digital'),
  (10, 't', '', 'en', TRUE, 'physical'), (11, 't', 'beta', 'en', TRUE, 'digital'),
  (12, 't', 'gamma', 'en', TRUE, 'digital'), (13, 't', 'gamma', 'en', TRUE, 'digital');

INSERT INTO report_side_effect_policy (report_seq, source_namespace, reason) VALUES (6, 'import', 'fixture');

-- An empty owner marks a report as processed with no owner; it is not private.
INSERT INTO reports_owners (seq, owner, is_public) VALUES (1, '', FALSE), (12, 'customer-1', FALSE);

INSERT INTO report_email_deliveries (seq, recipient_email, delivery_status, delivery_source, provider, sent_at) VALUES
  (1,  'support@alpha.example',   'sent', 'aggregate_brand', 'smtp', '2026-10-20 12:00:00'),
  (2,  'help@beta.example',       'sent', 'aggregate_brand', 'smtp', '2026-10-25 06:00:00'),
  (4,  'support@alpha.example',   'sent', 'aggregate_brand', 'smtp', '2026-10-22 09:00:00'),
  (11, 'help@beta.example',       'sent', 'aggregate_brand', 'smtp', '2026-10-30 09:00:00'),
  (5,  'support@epsilon.example', 'sent', 'aggregate_brand', 'smtp', '2026-11-12 01:00:00'),
  (8,  'team@delta.example',      'sent', 'aggregate_brand', 'smtp', '2026-09-02 00:00:00');

-- Report 1 was sent twice; latency uses the first send.
INSERT INTO report_email_delivery_events (seq, recipient_email, delivery_status, delivery_source, provider, sent_at) VALUES
  (1,  'support@alpha.example',   'sent', 'aggregate_brand', 'smtp', '2026-10-20 12:00:00'),
  (1,  'support@alpha.example',   'sent', 'aggregate_brand', 'smtp', '2026-10-21 12:00:00'),
  (2,  'help@beta.example',       'sent', 'aggregate_brand', 'smtp', '2026-10-25 06:00:00'),
  (4,  'support@alpha.example',   'sent', 'aggregate_brand', 'smtp', '2026-10-22 09:00:00'),
  (11, 'help@beta.example',       'sent', 'aggregate_brand', 'smtp', '2026-10-30 09:00:00'),
  (5,  'support@epsilon.example', 'sent', 'aggregate_brand', 'smtp', '2026-11-12 01:00:00'),
  (8,  'team@delta.example',      'sent', 'aggregate_brand', 'smtp', '2026-09-02 00:00:00');

INSERT INTO notification_messages (notification_id, recipient_email, sent_at) VALUES
  ('n0', 'team@delta.example',      '2026-09-02 00:00:00'),
  ('n1', 'support@alpha.example',   '2026-10-20 12:00:00'),
  ('n2', 'help@beta.example',       '2026-10-25 06:00:00'),
  ('n3', 'support@epsilon.example', '2026-11-12 01:00:00'),
  ('n4', 'support@alpha.example',   '2026-10-22 09:00:00'),
  ('n5', 'help@beta.example',       '2026-10-30 09:00:00');

INSERT INTO notification_message_reports (notification_id, seq) VALUES
  ('n0', 8), ('n1', 1), ('n2', 2), ('n3', 5), ('n4', 4), ('n5', 11);

INSERT INTO notify_outcomes (subject_kind, subject_ref, endpoint_key, outcome_type, source_type, source_ref, recorded_at) VALUES
  ('report', 'p1',  'support@alpha.example', 'viewed',       'notification_link', 'n1', '2026-10-21 12:00:00'),
  ('report', 'p1',  'support@alpha.example', 'replied',      'inbox',             'n1', '2026-10-22 12:00:00'),
  ('report', 'p1',  'support@alpha.example', 'fixed',        'manual',            '',   '2026-11-05 00:00:00'),
  ('report', 'p2',  'help@beta.example',     'bounced',      'inbox',             'n2', '2026-10-25 06:05:00'),
  ('report', 'p4',  'support@alpha.example', 'acknowledged', 'inbox',             'n4', '2026-10-22 10:00:00'),
  ('report', 'p4',  'support@alpha.example', 'misrouted',    'manual',            '',   '2026-11-02 00:00:00'),
  ('report', 'p11', 'help@beta.example',     'no_response',  'silence',           'n5', '2026-11-06 09:00:00'),
  ('report', 'p8',  'team@delta.example',    'viewed',       'notification_link', 'n0', '2026-09-03 00:00:00'),
  ('case',   'c1',  'ops@zeta.example',      'sent',         'case_escalation',   '',   '2026-10-15 00:00:00');

INSERT INTO opted_out_emails (email, opted_out_at) VALUES
  ('gone@beta.example', '2026-11-01 00:00:00'), ('old@old.example', '2026-09-01 00:00:00');

INSERT INTO report_status (seq, status, created_at, updated_at) VALUES
  (10, 'resolved', '2026-10-05 00:00:00', '2026-11-01 00:00:00'),
  (1,  'active',   '2026-10-20 10:00:00', '2026-11-03 00:00:00');

INSERT INTO cases (case_id, slug, title, created_by_user_id) VALUES ('c1', 'c1', 'Fixture case', 'user-1');
INSERT INTO case_reports (case_id, seq) VALUES ('c1', 1), ('c1', 3);

INSERT INTO epc_campaigns (slug, description) VALUES ('fixture', 'fixture');
INSERT INTO epc_contracts (id, `key`, address) VALUES
  (1, 'cleanapp/alpha', '0x0000000000000000000000000000000000000001'),
  (2, 'cleanapp/beta',  '0x0000000000000000000000000000000000000002'),
  (3, 'cleanapp/gamma', '0x0000000000000000000000000000000000000003');
-- The third row is an imported notice with no send time; its creation date stands in.
-- Its report number belongs to the old system and must not match report 13 here.
INSERT INTO epc_outbox (campaign, contract_id, status, event_key, source_namespace, source_report_seq, created, sent_at) VALUES
  ('fixture', 1, 'sent', 'prod:report:1', 'prod',       1,  '2026-11-01 00:00:00', '2026-11-01 00:00:00'),
  ('fixture', 1, 'sent', 'prod:report:2', 'prod',       2,  '2026-11-10 00:00:00', '2026-11-10 00:00:00'),
  ('fixture', 2, 'sent', 'legacy:13',     'legacy-dev', 13, '2025-10-10 00:00:00', NULL),
  ('fixture', 3, 'held', 'prod:report:3', 'prod',       3,  '2026-11-12 00:00:00', NULL);
