-- Initial database setup. Run as root@

-- Create the database.
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;
SHOW DATABASES;

-- Create the users table.
CREATE TABLE IF NOT EXISTS users (
  id VARCHAR(255),
  avatar VARCHAR(255),
  team INT, -- 0 UNKNOWN, 1 BLUE, 2 GREEN, see map.go
  privacy varchar(255),
  agree_toc varchar(255),
  referral VARCHAR(32),
  kitns_daily INT DEFAULT 0,
  kitns_disbursed INT DEFAULT 0,
  kitns_ref_daily DECIMAL(18, 6) DEFAULT 0.0,
  kitns_ref_disbursed DECIMAL(18, 6) DEFAULT 0.0,
  kitns_ref_redeemed INT DEFAULT 0,
  action_id VARCHAR(32),
  ts TIMESTAMP default current_timestamp,
  PRIMARY KEY (id),
  UNIQUE INDEX avatar_idx (avatar),
  INDEX action_idx (action_id)
);
SHOW TABLES;
DESCRIBE TABLE users;
SHOW COLUMNS FROM users;

-- Create the users shadow table.
CREATE TABLE IF NOT EXISTS users_shadow (
  id VARCHAR(255),
  avatar VARCHAR(255),
  team INT, -- 0 UNKNOWN, 1 BLUE, 2 GREEN, see map.go
  privacy varchar(255),
  agree_toc varchar(255),
  referral VARCHAR(32),
  kitns_daily INT DEFAULT 0,
  kitns_disbursed INT DEFAULT 0,
  kitns_ref_daily DECIMAL(18, 6) DEFAULT 0.0,
  kitns_ref_disbursed DECIMAL(18, 6) DEFAULT 0.0,
  kitns_ref_redeemed INT DEFAULT 0,
  action_id VARCHAR(32),
  ts TIMESTAMP default current_timestamp,
  PRIMARY KEY (id),
  UNIQUE INDEX avatar_idx (avatar),
  INDEX action_idx (action_id)
);
SHOW TABLES;
DESCRIBE TABLE users_shadow;
SHOW COLUMNS FROM users_shadow;

-- Create the report table.
CREATE TABLE IF NOT EXISTS reports(
  seq INT NOT NULL AUTO_INCREMENT,
  ts TIMESTAMP default current_timestamp,
  id VARCHAR(255) NOT NULL,
  team INT NOT NULL, -- 0 UNKNOWN, 1 BLUE, 2 GREEN, see map.go
  latitude FLOAT NOT NULL,
  longitude FLOAT NOT NULL,
  x FLOAT, # 0.0..1.0
  y FLOAT, # 0.0..1.0
  image LONGBLOB NOT NULL,
  action_id VARCHAR(32),
  description VARCHAR(255),
  PRIMARY KEY (seq),
  INDEX id_index (id),
  INDEX action_idx (action_id),
  INDEX latitude_index (latitude),
  INDEX longitude_index (longitude)
);
SHOW TABLES;
DESCRIBE TABLE reports;
SHOW COLUMNS FROM reports;
-- Migration Statement:
-- SELECT CONCAT('INSERT INTO reports (seq, id, team, latitude, longitude) VALUES(', seq, ',', QUOTE(id), ',', team, ',', latitude, ',', longitude, ');') AS insert_statement FROM reports;

CREATE TABLE IF NOT EXISTS reports_geometry(
  seq INT NOT NULL,
  geom GEOMETRY NOT NULL SRID 4326,
  PRIMARY KEY (seq),
  SPATIAL INDEX(geom)
);
SHOW TABLES;
DESCRIBE TABLE reports_geometry;
SHOW COLUMNS FROM reports_geometry;

CREATE TABLE IF NOT EXISTS referrals(
  refkey CHAR(128) NOT NULL,
  refvalue CHAR(32),
  PRIMARY KEY (refkey),
  INDEX ref_idx (refvalue)
);
SHOW TABLES;
DESCRIBE TABLE referrals;
SHOW COLUMNS FROM referrals;

CREATE TABLE IF NOT EXISTS users_refcodes(
  referral CHAR(32) NOT NULL,
  id VARCHAR(255) NOT NULL,
  PRIMARY KEY (id)
);
SHOW TABLES;
DESCRIBE TABLE users_refcodes;
SHOW COLUMNS FROM users_refcodes;

CREATE TABLE IF NOT EXISTS actions(
  id CHAR(32) NOT NULL,
  name VARCHAR(255) NOT NULL,
  is_active INT NOT NULL DEFAULT 0,
  expiration_date DATE,
  PRIMARY KEY (id)
);
SHOW TABLES;
DESCRIBE TABLE actions;
SHOW COLUMNS FROM actions;

CREATE TABLE IF NOT EXISTS areas(
  id INT NOT NULL AUTO_INCREMENT,
  name VARCHAR(255) NOT NULL,
  description VARCHAR(255),
  is_custom BOOL NOT NULL DEFAULT false,
  contact_name VARCHAR(255),
  area_json JSON,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  PRIMARY KEY (id)
);
-- Migration Statement:
-- SELECT CONCAT('INSERT INTO areas (id, name, description, is_custom, contact_name, area_json) VALUES(', id, ',', QUOTE(name), ',', QUOTE(description), ',', is_custom, ',', QUOTE(contact_name), ',', QUOTE(area_json), ');') AS insert_statement FROM areas;

CREATE TABLE IF NOT EXISTS contact_emails(
  area_id INT NOT NULL,
  email CHAR(64) NOT NULL,
  consent_report BOOL NOT NULL DEFAULT true,
  INDEX area_id_index (area_id),
  INDEX email_index (email)
);
-- Migration Statement:
-- SELECT CONCAT('INSERT INTO contact_emails (area_id, email, consent_report) VALUES(', area_id, ',', QUOTE(email), ',', consent_report, ');') AS insert_statement FROM contact_emails;

CREATE TABLE IF NOT EXISTS area_index(
  area_id INT NOT NULL,
  geom GEOMETRY NOT NULL SRID 4326,
  SPATIAL INDEX(geom)
);

CREATE TABLE IF NOT EXISTS sent_emails (
  seq INT PRIMARY KEY
);

-- Create GDPR tracking tables
CREATE TABLE IF NOT EXISTS users_gdpr(
  id VARCHAR(255) NOT NULL,
  processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE INDEX id_unique (id)
);

CREATE TABLE IF NOT EXISTS reports_gdpr(
  seq INT NOT NULL,
  processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (seq),
  UNIQUE INDEX seq_unique (seq)
);

-- Create the responses table with identical structure to reports plus status enum
CREATE TABLE IF NOT EXISTS responses(
  seq INT NOT NULL AUTO_INCREMENT,
  ts TIMESTAMP default current_timestamp,
  id VARCHAR(255) NOT NULL,
  team INT NOT NULL, -- 0 UNKNOWN, 1 BLUE, 2 GREEN, see map.go
  latitude FLOAT NOT NULL,
  longitude FLOAT NOT NULL,
  x FLOAT, # 0.0..1.0
  y FLOAT, # 0.0..1.0
  image LONGBLOB NOT NULL,
  action_id VARCHAR(32),
  description VARCHAR(255),
  status ENUM('resolved', 'verified') NOT NULL DEFAULT 'resolved',
  report_seq INT NOT NULL, -- Reference to the resolved report
  PRIMARY KEY (seq),
  INDEX id_index (id),
  INDEX action_idx (action_id),
  INDEX latitude_index (latitude),
  INDEX longitude_index (longitude),
  INDEX status_index (status),
  INDEX report_seq_index (report_seq),
  FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
);
SHOW TABLES;
DESCRIBE TABLE responses;
SHOW COLUMNS FROM responses;

-- Migration Statement:
-- SELECT CONCAT('INSERT INTO area_index (area_id, geom) VALUES(', area_id, ',', ST_AsText(geom), ');') AS insert_statement FROM area_index;

-- Create the report_clusters table
CREATE TABLE IF NOT EXISTS report_clusters(
  primary_seq INT NOT NULL,
  related_seq INT NOT NULL,
  PRIMARY KEY (primary_seq, related_seq),
  INDEX primary_seq_index (primary_seq),
  UNIQUE INDEX related_seq_unique (related_seq),
  FOREIGN KEY (primary_seq) REFERENCES reports(seq) ON DELETE CASCADE,
  FOREIGN KEY (related_seq) REFERENCES reports(seq) ON DELETE CASCADE
);
SHOW TABLES;
DESCRIBE TABLE report_clusters;
SHOW COLUMNS FROM report_clusters;

-- Email pipeline marker: tracks which reports have been processed for outbound notifications.
CREATE TABLE IF NOT EXISTS sent_reports_emails (
  seq INT PRIMARY KEY,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_created_at (created_at),
  INDEX idx_seq (seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Fetcher Key System + Quarantine Ingest (v1)
-- These tables support external agent swarms submitting reports safely with
-- hashed API keys, quotas, audit logs, and a shadow visibility lane.

CREATE TABLE IF NOT EXISTS fetchers (
  id INT UNSIGNED AUTO_INCREMENT,
  fetcher_id VARCHAR(64) NOT NULL UNIQUE,
  name VARCHAR(255) NOT NULL,
  token_hash VARBINARY(64) NOT NULL, -- legacy v3 bulk_ingest token hash (sha256); retained for compatibility
  scopes JSON NULL,
  active BOOL NOT NULL DEFAULT TRUE, -- legacy flag; status is the canonical field for v1+
  owner_type VARCHAR(32) NOT NULL DEFAULT 'unknown',
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  tier INT NOT NULL DEFAULT 0,
  reputation_score INT NOT NULL DEFAULT 50,
  daily_cap_items INT NOT NULL DEFAULT 200,
  per_minute_cap_items INT NOT NULL DEFAULT 20,
  last_used_at TIMESTAMP NULL,
  last_seen_at TIMESTAMP NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  INDEX idx_active (active),
  INDEX idx_fetchers_status (status),
  INDEX idx_fetchers_owner (owner_type),
  INDEX idx_fetchers_last_seen (last_seen_at)
);

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
  FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
);

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

-- Legacy bulk_ingest idempotency mapping (also used for backfills/tools).
CREATE TABLE IF NOT EXISTS external_ingest_index (
  source VARCHAR(64) NOT NULL,
  external_id VARCHAR(255) NOT NULL,
  seq INT NOT NULL,
  source_timestamp DATETIME NULL,
  source_url VARCHAR(512) NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (source, external_id),
  INDEX idx_seq (seq)
);

-- Structured metadata for digital reports (company/product/url).
CREATE TABLE IF NOT EXISTS report_details (
  seq INT NOT NULL,
  company_name VARCHAR(255),
  product_name VARCHAR(255),
  url VARCHAR(512),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (seq),
  FOREIGN KEY (seq) REFERENCES reports(seq) ON DELETE CASCADE,
  INDEX idx_company (company_name),
  INDEX idx_product (product_name)
);

-- Create the user.
-- 1. Remove '%' user
--    if the server and mysql run on the same instance.
--    (still needed if run from two images)
CREATE USER IF NOT EXISTS 'server'@'localhost' IDENTIFIED BY 'secret_app';
CREATE USER IF NOT EXISTS 'server'@'%' IDENTIFIED BY 'secret_app';
CREATE USER IF NOT EXISTS 'importer'@'%' IDENTIFIED BY 'secret_importer';
SELECT User, Host FROM mysql.user;

-- Grant rights to the user.
GRANT ALL ON cleanapp.* TO 'server'@'localhost';
GRANT ALL ON cleanapp.* TO 'server'@'%';
GRANT SELECT ON cleanapp.* TO 'importer'@'%';  -- We don't make secret out of reports, so that's safe.
