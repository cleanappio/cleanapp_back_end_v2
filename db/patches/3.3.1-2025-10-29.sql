-- Performance indexes for report-listener-v4 endpoints
-- Note: Some MySQL versions don't support IF NOT EXISTS on ADD INDEX.
-- Apply once; reruns may fail if indexes already exist.

-- Accelerate brand summary and language-filtered grouping
ALTER TABLE report_analysis
  ADD INDEX idx_ra_class_valid_lang_brand (language, classification, is_valid, brand_name, brand_display_name);

-- Accelerate points endpoint: aggregate severity by seq filtered by classification/is_valid
ALTER TABLE report_analysis
  ADD INDEX idx_ra_class_valid_seq_sev (classification, is_valid, seq, severity_level);

-- Speed filters on active status
ALTER TABLE report_status
  ADD INDEX idx_rs_seq_status (seq, status);

-- Speed public-owner visibility checks
ALTER TABLE reports_owners
  ADD INDEX idx_ro_seq_public (seq, is_public, owner);


