-- Run once before deploying the ingestion recovery release.
-- Verify SHOW INDEX FROM indexer_bluesky_analysis before retrying.
-- Online index avoids scanning analysis TEXT/JSON for every submission batch.
SET SESSION lock_wait_timeout = 20;
ALTER TABLE indexer_bluesky_analysis
  ADD INDEX idx_relevant_uri (is_relevant, uri), ALGORITHM=INPLACE, LOCK=NONE;
