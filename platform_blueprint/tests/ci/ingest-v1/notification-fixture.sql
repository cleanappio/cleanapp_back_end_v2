-- Disposable producer-owned schema needed by private outcome projections.
-- This matches email-service/service/email_service.go's retry-table schema.
-- No email or EPC producer/provider runs in these CI stacks.
USE cleanapp;
CREATE TABLE IF NOT EXISTS email_report_retry (
 seq INT PRIMARY KEY,
 reason VARCHAR(64) NOT NULL,
 next_attempt_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 retry_count INT NOT NULL DEFAULT 1,
 created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
 updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
 INDEX idx_retry_next_attempt (next_attempt_at),
 INDEX idx_retry_reason (reason),
 INDEX idx_retry_updated_at (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
