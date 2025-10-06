-- Performance indexes introduced during Oct 2025 refactor
-- MySQL 8.0.13+ supports IF NOT EXISTS for indexes

-- report_analysis composite indexes to accelerate common filters and joins
ALTER TABLE report_analysis 
  ADD INDEX IF NOT EXISTS idx_ra_class_valid_seq (classification, is_valid, seq);

ALTER TABLE report_analysis 
  ADD INDEX IF NOT EXISTS idx_ra_language_seq (language, seq);

ALTER TABLE report_analysis 
  ADD INDEX IF NOT EXISTS idx_ra_seq_class_valid (seq, classification, is_valid);

ALTER TABLE report_analysis 
  ADD INDEX IF NOT EXISTS idx_ra_seq_is_valid (seq, is_valid);

-- users watermark scan support (GDPR processing)
ALTER TABLE users 
  ADD INDEX IF NOT EXISTS idx_users_ts (ts);


