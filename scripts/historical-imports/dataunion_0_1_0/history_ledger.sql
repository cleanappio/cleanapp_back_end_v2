-- Trusted import audit/provenance. Apply through the reviewed migration path only.
-- The generic report_side_effect_policy DDL is maintained separately.
CREATE TABLE IF NOT EXISTS historical_report_import_runs (
    run_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    source_namespace VARCHAR(64) NOT NULL,
    importer_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    candidate_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    dedup_manifest_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    candidate_count INT NOT NULL,
    suppression_policy_version INT NOT NULL,
    approved_controls_json JSON NOT NULL,
    state VARCHAR(24) NOT NULL DEFAULT 'applying',
    started_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    completed_at TIMESTAMP(6) NULL,
    INDEX idx_historical_import_namespace (source_namespace,started_at)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS historical_report_import_ledger (
    source_namespace VARCHAR(64) NOT NULL,
    source_id VARCHAR(255) NOT NULL,
    run_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    disposition VARCHAR(32) NOT NULL,
    report_seq INT NULL,
    recorded_report_seq INT NOT NULL,
    report_public_id VARCHAR(32) NULL,
    photo_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    photo_bytes BIGINT NOT NULL,
    original_uploaded_at DATETIME(6) NOT NULL,
    original_latitude DOUBLE NOT NULL,
    original_longitude DOUBLE NOT NULL,
    original_uploaded_unix VARCHAR(64) CHARACTER SET ascii NOT NULL,
    legacy_uploader_reference VARCHAR(512) NOT NULL,
    projection_method VARCHAR(32) NOT NULL DEFAULT 'metadata_only',
    severity_assessed BOOLEAN NOT NULL DEFAULT FALSE,
    duplicate_basis VARCHAR(255) NULL,
    original_record_json JSON NOT NULL,
    normalized_record_json JSON NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (source_namespace,source_id),
    UNIQUE KEY uniq_historical_import_photo (source_namespace,photo_sha256),
    INDEX idx_historical_import_run (run_id,disposition,report_seq),
    FOREIGN KEY (run_id) REFERENCES historical_report_import_runs(run_id),
    FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE SET NULL
) ENGINE=InnoDB;
