-- Report counters (materialized aggregates)
-- Purpose: eliminate hot COUNT(DISTINCT ...) paths and enable fast O(1) reads.
-- Safe to apply on an existing prod DB (idempotent).

USE cleanapp;

CREATE TABLE IF NOT EXISTS counters_state (
  name VARCHAR(64) NOT NULL PRIMARY KEY,
  last_seq INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS report_counts_total (
  id TINYINT NOT NULL PRIMARY KEY,
  total_valid BIGINT NOT NULL DEFAULT 0,
  physical_valid BIGINT NOT NULL DEFAULT 0,
  digital_valid BIGINT NOT NULL DEFAULT 0,
  last_counted_seq INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB;

-- Single-row invariant: always keep row id=1 present.
INSERT INTO report_counts_total (id, total_valid, physical_valid, digital_valid, last_counted_seq)
VALUES (1, 0, 0, 0, 0)
ON DUPLICATE KEY UPDATE id = id;

CREATE TABLE IF NOT EXISTS brand_report_counts (
  brand_name VARCHAR(255) NOT NULL,
  language VARCHAR(8) NOT NULL DEFAULT 'en',
  total_valid BIGINT NOT NULL DEFAULT 0,
  physical_valid BIGINT NOT NULL DEFAULT 0,
  digital_valid BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (brand_name, language),
  INDEX idx_updated_at (updated_at)
) ENGINE=InnoDB;

