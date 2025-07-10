-- Create the sent_reports_emails table to track which reports have had emails sent
CREATE TABLE IF NOT EXISTS sent_reports_emails (
    seq INT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_created_at (created_at),
    INDEX idx_seq (seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci; 