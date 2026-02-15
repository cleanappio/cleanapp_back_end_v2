-- Fetcher Promotion Workflow + Moderation Events
--
-- Adds:
-- - Fetcher-level defaults (visibility/trust) and feature toggles (routing/rewards)
-- - Promotion request workflow (self-serve request + internal approve/deny)
-- - Append-only moderation events for auditability
-- - report_raw.analysed_published_at to make promotion-triggered publish idempotent

-- Fetcher defaults + toggles.
ALTER TABLE fetchers
  ADD COLUMN default_visibility VARCHAR(16) NOT NULL DEFAULT 'shadow',
  ADD COLUMN default_trust_level VARCHAR(16) NOT NULL DEFAULT 'unverified',
  ADD COLUMN routing_enabled BOOL NOT NULL DEFAULT FALSE,
  ADD COLUMN rewards_enabled BOOL NOT NULL DEFAULT FALSE,
  ADD COLUMN verified_domain VARCHAR(255) NULL,
  ADD COLUMN owner_user_id VARCHAR(255) NULL,
  ADD COLUMN notes TEXT NULL;

ALTER TABLE fetchers
  ADD INDEX idx_fetchers_default_visibility (default_visibility),
  ADD INDEX idx_fetchers_verified_domain (verified_domain);

-- Promotion publish idempotency marker (used by internal promote and analyzer).
ALTER TABLE report_raw
  ADD COLUMN promoted_to_public_at TIMESTAMP NULL,
  ADD COLUMN analysed_published_at TIMESTAMP NULL,
  ADD INDEX idx_report_raw_promoted_public (promoted_to_public_at),
  ADD INDEX idx_report_raw_analysed_published (analysed_published_at);

-- Self-serve promotion request queue.
CREATE TABLE IF NOT EXISTS fetcher_promotion_requests (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  fetcher_id VARCHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'pending', -- pending|approved|denied|needs_info|cancelled
  contact_email VARCHAR(255) NULL,
  verified_domain VARCHAR(255) NULL,
  requested_tier INT NULL,
  requested_daily_cap_items INT NULL,
  requested_per_minute_cap_items INT NULL,
  requested_default_visibility VARCHAR(16) NULL,
  requested_default_trust_level VARCHAR(16) NULL,
  requested_routing_enabled BOOL NULL,
  requested_rewards_enabled BOOL NULL,
  notes TEXT NULL,
  decision_notes TEXT NULL,
  reviewed_by VARCHAR(255) NULL,
  reviewed_at TIMESTAMP NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  INDEX idx_status_created (status, created_at),
  INDEX idx_fetcher_created (fetcher_id, created_at),
  CONSTRAINT fk_promo_req_fetcher FOREIGN KEY (fetcher_id) REFERENCES fetchers(fetcher_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Append-only moderation event log (secrets-safe).
CREATE TABLE IF NOT EXISTS moderation_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  actor VARCHAR(255) NULL,
  actor_ip VARCHAR(64) NULL,
  action VARCHAR(64) NOT NULL,
  target_type VARCHAR(64) NOT NULL,
  target_id VARCHAR(255) NOT NULL,
  details JSON NULL,
  request_id VARCHAR(64) NULL,
  PRIMARY KEY (id),
  INDEX idx_ts (ts),
  INDEX idx_action_ts (action, ts),
  INDEX idx_target (target_type, target_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
