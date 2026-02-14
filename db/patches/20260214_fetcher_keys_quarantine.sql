-- Fetcher Key System + Quarantine Ingest Lane (v1)
-- Applied once on production to introduce hashed API keys, quotas, audit logs,
-- and a shadow visibility lane for new external ingest sources.

-- Extend legacy fetchers table with governance + quota fields.
ALTER TABLE fetchers
  ADD COLUMN owner_type VARCHAR(32) NOT NULL DEFAULT 'unknown',
  ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active',
  ADD COLUMN tier INT NOT NULL DEFAULT 0,
  ADD COLUMN reputation_score INT NOT NULL DEFAULT 50,
  ADD COLUMN daily_cap_items INT NOT NULL DEFAULT 200,
  ADD COLUMN per_minute_cap_items INT NOT NULL DEFAULT 20,
  ADD COLUMN last_seen_at TIMESTAMP NULL;

-- Backfill status based on legacy active flag (best-effort).
UPDATE fetchers SET status = IF(active, 'active', 'suspended');

-- Helpful indexes for governance queries.
ALTER TABLE fetchers
  ADD INDEX idx_fetchers_status (status),
  ADD INDEX idx_fetchers_owner (owner_type),
  ADD INDEX idx_fetchers_last_seen (last_seen_at);

-- Fetcher API keys (Stripe-style: plaintext shown once; DB stores only hash).
CREATE TABLE IF NOT EXISTS fetcher_keys (
  key_id CHAR(36) NOT NULL,
  fetcher_id VARCHAR(64) NOT NULL,
  key_prefix VARCHAR(32) NOT NULL,
  key_hash VARCHAR(128) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  scopes JSON NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  last_used_at TIMESTAMP NULL,
  per_minute_cap_items INT NULL,
  daily_cap_items INT NULL,
  PRIMARY KEY (key_id),
  INDEX idx_fetcher (fetcher_id),
  INDEX idx_status (status),
  CONSTRAINT fk_fetcher_keys_fetcher FOREIGN KEY (fetcher_id) REFERENCES fetchers(fetcher_id) ON DELETE CASCADE
);

-- Append-only ingest audit log (secrets-safe).
CREATE TABLE IF NOT EXISTS ingestion_audit (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  ts TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  fetcher_id VARCHAR(64) NULL,
  key_id CHAR(36) NULL,
  endpoint VARCHAR(128) NOT NULL,
  items_submitted INT NOT NULL DEFAULT 0,
  items_accepted INT NOT NULL DEFAULT 0,
  items_rejected INT NOT NULL DEFAULT 0,
  reject_reasons JSON NULL,
  latency_ms INT NOT NULL DEFAULT 0,
  remote_ip VARCHAR(64) NULL,
  user_agent VARCHAR(255) NULL,
  request_id VARCHAR(64) NULL,
  PRIMARY KEY (id),
  INDEX idx_audit_ts (ts),
  INDEX idx_audit_fetcher_ts (fetcher_id, ts),
  INDEX idx_audit_key_ts (key_id, ts)
);

-- Raw ingest metadata (quarantine lane control + idempotency).
CREATE TABLE IF NOT EXISTS report_raw (
  report_seq INT NOT NULL,
  fetcher_id VARCHAR(64) NULL,
  source_id VARCHAR(255) NULL,
  agent_id VARCHAR(255) NULL,
  agent_version VARCHAR(64) NULL,
  collected_at TIMESTAMP NULL,
  source_type VARCHAR(32) NULL,
  visibility VARCHAR(16) NOT NULL DEFAULT 'public',
  trust_level VARCHAR(16) NOT NULL DEFAULT 'unverified',
  spam_score FLOAT NOT NULL DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (report_seq),
  UNIQUE KEY uniq_fetcher_source (fetcher_id, source_id),
  INDEX idx_visibility (visibility),
  INDEX idx_fetcher_visibility (fetcher_id, visibility),
  CONSTRAINT fk_report_raw_seq FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
);

-- Quota enforcement helpers (buckets).
CREATE TABLE IF NOT EXISTS fetcher_usage_minute (
  fetcher_id VARCHAR(64) NOT NULL,
  key_id CHAR(36) NOT NULL,
  bucket_minute DATETIME NOT NULL,
  items INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (fetcher_id, key_id, bucket_minute),
  INDEX idx_bucket (bucket_minute)
);

CREATE TABLE IF NOT EXISTS fetcher_usage_daily (
  fetcher_id VARCHAR(64) NOT NULL,
  key_id CHAR(36) NOT NULL,
  bucket_date DATE NOT NULL,
  items INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (fetcher_id, key_id, bucket_date),
  INDEX idx_bucket (bucket_date)
);
