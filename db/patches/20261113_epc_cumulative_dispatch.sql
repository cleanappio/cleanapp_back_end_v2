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

-- Additive upgrade for an existing legacy EPC schema. Receipt IDs, keys,
-- addresses, timestamps and metadata are deliberately not rewritten.
DELIMITER //
DROP PROCEDURE IF EXISTS epc_add_column_20261113//
CREATE PROCEDURE epc_add_column_20261113(IN table_name_arg VARCHAR(64), IN column_name_arg VARCHAR(64), IN column_definition TEXT)
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
    WHERE table_schema=DATABASE() AND table_name=table_name_arg AND column_name=column_name_arg) THEN
    SET @epc_ddl=CONCAT('ALTER TABLE `',table_name_arg,'` ADD COLUMN `', column_name_arg, '` ', column_definition);
    PREPARE epc_stmt FROM @epc_ddl;
    EXECUTE epc_stmt;
    DEALLOCATE PREPARE epc_stmt;
  END IF;
END//
DROP PROCEDURE IF EXISTS epc_add_index_20261113//
CREATE PROCEDURE epc_add_index_20261113(IN index_name_arg VARCHAR(64), IN index_definition TEXT)
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.statistics
    WHERE table_schema=DATABASE() AND table_name='epc_outbox' AND index_name=index_name_arg) THEN
    SET @epc_ddl=CONCAT('ALTER TABLE epc_outbox ADD ', index_definition);
    PREPARE epc_stmt FROM @epc_ddl;
    EXECUTE epc_stmt;
    DEALLOCATE PREPARE epc_stmt;
  END IF;
END//
DELIMITER ;
CALL epc_add_column_20261113('epc_outbox', 'event_key', 'VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NULL');
CALL epc_add_column_20261113('epc_outbox', 'source_namespace', 'VARCHAR(64) NULL');
CALL epc_add_column_20261113('epc_outbox', 'source_report_seq', 'BIGINT UNSIGNED NULL');
CALL epc_add_column_20261113('epc_outbox', 'message_body', 'TEXT NULL');
CALL epc_add_column_20261113('epc_outbox', 'blockscan_message_id', 'VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL');
CALL epc_add_column_20261113('epc_outbox', 'attempt_count', 'INT UNSIGNED NOT NULL DEFAULT 0');
CALL epc_add_column_20261113('epc_outbox', 'next_attempt_at', 'DATETIME(3) NULL');
CALL epc_add_column_20261113('epc_outbox', 'last_attempt_at', 'DATETIME(3) NULL');
CALL epc_add_column_20261113('epc_outbox', 'sent_at', 'DATETIME(3) NULL');
CALL epc_add_column_20261113('epc_outbox', 'last_error_code', 'VARCHAR(64) NULL');
CALL epc_add_column_20261113('epc_report_cursors', 'rescan_report_seq', 'BIGINT UNSIGNED NOT NULL DEFAULT 0');
CALL epc_add_column_20261113('epc_report_cursors', 'dispatch_not_before', 'DATETIME(3) NULL');
ALTER TABLE epc_contracts MODIFY COLUMN `key` VARCHAR(512) NOT NULL;
ALTER TABLE epc_outbox MODIFY COLUMN status ENUM('held','pending','sending','sent','error','uncertain') NOT NULL DEFAULT 'pending';
CALL epc_add_index_20261113('epc_outbox_event', 'UNIQUE KEY epc_outbox_event (event_key)');
CALL epc_add_index_20261113('epc_outbox_blockscan_message', 'UNIQUE KEY epc_outbox_blockscan_message (blockscan_message_id)');
CALL epc_add_index_20261113('epc_outbox_dispatch', 'KEY epc_outbox_dispatch (source_namespace,status,next_attempt_at,id)');
CALL epc_add_index_20261113('epc_outbox_source_report', 'KEY epc_outbox_source_report (source_namespace,source_report_seq)');
DROP PROCEDURE epc_add_column_20261113;
DROP PROCEDURE epc_add_index_20261113;
