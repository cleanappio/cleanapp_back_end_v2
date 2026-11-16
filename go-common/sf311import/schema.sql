CREATE TABLE IF NOT EXISTS sf311_source_case_ledger (
 source_namespace VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 source_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 payload_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 material_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 publisher VARCHAR(64) NOT NULL,
 source_url VARCHAR(512) NOT NULL,
 source_record_json JSON NOT NULL,
 candidate_json JSON NOT NULL,
 media_json JSON NOT NULL,
 report_seq INT NULL,
 recorded_report_seq INT NOT NULL,
 report_public_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 disposition VARCHAR(32) NOT NULL,
 created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(source_namespace,source_id),
 KEY idx_sf311_ledger_report(report_seq),
 FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE SET NULL
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS sf311_source_media_ledger (
 source_namespace VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 source_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 position TINYINT UNSIGNED NOT NULL,
 original_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 display_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 media_json JSON NOT NULL,
 PRIMARY KEY(source_namespace,source_id,position),
 KEY idx_sf311_original_sha(original_sha256),
 KEY idx_sf311_display_sha(display_sha256),
 FOREIGN KEY(source_namespace,source_id)
 REFERENCES sf311_source_case_ledger(source_namespace,source_id)
) ENGINE=InnoDB;
