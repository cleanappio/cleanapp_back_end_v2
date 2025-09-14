CREATE TABLE IF NOT EXISTS social_posts (
  post_id VARCHAR(255) NOT NULL,
  platform VARCHAR(50) NOT NULL,
  url VARCHAR(255),
  content TEXT,
  likes INT,
  reposts INT,
  replies INT,
  post_timestamp TIMESTAMP,
  processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  submitted_to_cleanapp BOOL DEFAULT FALSE,
  cleanapp_report_seq INT,
  PRIMARY KEY (post_id, platform)
);

CREATE TABLE IF NOT EXISTS indexing_state (
  platform VARCHAR(50) PRIMARY KEY,
  last_indexed_time TIMESTAMP
);
