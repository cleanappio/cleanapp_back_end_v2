-- Explicit additive historical-import controls. Apply before guarded services.
-- Insert the policy row in the same transaction as every imported report.
CREATE TABLE IF NOT EXISTS report_side_effect_policy (
 report_seq INT NOT NULL PRIMARY KEY,
 source_namespace VARCHAR(64) NOT NULL,
 reason VARCHAR(255) NOT NULL,
 policy_version INT NOT NULL DEFAULT 1,
 suppress_email BOOLEAN NOT NULL DEFAULT TRUE,
 suppress_notifications BOOLEAN NOT NULL DEFAULT TRUE,
 suppress_rewards BOOLEAN NOT NULL DEFAULT TRUE,
 suppress_routing BOOLEAN NOT NULL DEFAULT TRUE,
 suppress_analysis BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 KEY idx_report_effect_policy_source (source_namespace, report_seq),
 CONSTRAINT fk_report_effect_policy_report FOREIGN KEY (report_seq) REFERENCES reports(seq) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
