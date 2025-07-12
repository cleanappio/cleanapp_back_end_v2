-- Migration: Add sync fields to client_auth table
-- This migration adds sync_version and last_sync_at fields for synchronization

-- Add sync_version column
ALTER TABLE client_auth 
ADD COLUMN sync_version INT DEFAULT 1,
ADD COLUMN last_sync_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
ADD INDEX idx_sync_version (sync_version);

-- Record this migration
INSERT INTO schema_migrations (version, applied_at) VALUES (2, NOW()); 