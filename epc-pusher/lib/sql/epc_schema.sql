-- Cumulative EPC notice history and durable, namespaced production dispatch.
CREATE TABLE IF NOT EXISTS epc_campaigns (
  slug VARCHAR(16) NOT NULL PRIMARY KEY,
  description VARCHAR(255),
  created TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE IF NOT EXISTS epc_contracts (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  `key` VARCHAR(512) NOT NULL UNIQUE,
  address VARCHAR(42) NOT NULL UNIQUE,
  created TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE IF NOT EXISTS epc_sent_message_templates_read_only (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  body TEXT NOT NULL,
  created TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE IF NOT EXISTS epc_outbox (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  campaign VARCHAR(16) NOT NULL,
  contract_id INT UNSIGNED NOT NULL,
  metadata JSON,
  status ENUM('held','pending','sending','sent','error','uncertain') NOT NULL DEFAULT 'pending',
  sent_message_template_id INT UNSIGNED,
  created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  event_key VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NULL,
  source_namespace VARCHAR(64) NULL,
  source_report_seq BIGINT UNSIGNED NULL,
  message_body TEXT NULL,
  blockscan_message_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
  attempt_count INT UNSIGNED NOT NULL DEFAULT 0,
  next_attempt_at DATETIME(3) NULL,
  last_attempt_at DATETIME(3) NULL,
  sent_at DATETIME(3) NULL,
  last_error_code VARCHAR(64) NULL,
  UNIQUE KEY epc_outbox_event (event_key),
  UNIQUE KEY epc_outbox_blockscan_message (blockscan_message_id),
  KEY epc_outbox_dispatch (source_namespace, status, next_attempt_at, id),
  KEY epc_outbox_source_report (source_namespace, source_report_seq),
  FOREIGN KEY (campaign) REFERENCES epc_campaigns(slug),
  FOREIGN KEY (contract_id) REFERENCES epc_contracts(id),
  FOREIGN KEY (sent_message_template_id) REFERENCES epc_sent_message_templates_read_only(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- This cursor belongs only to the named source database. Imported outbox IDs
-- and legacy report sequence numbers must never initialize it.
CREATE TABLE IF NOT EXISTS epc_report_cursors (
  campaign VARCHAR(16) NOT NULL,
  source_namespace VARCHAR(64) NOT NULL,
  last_report_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  rescan_report_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  dispatch_not_before DATETIME(3) NULL,
  updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (campaign, source_namespace),
  FOREIGN KEY (campaign) REFERENCES epc_campaigns(slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
