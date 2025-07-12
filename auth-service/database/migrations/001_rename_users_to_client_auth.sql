-- Migration: Rename users table to client_auth to avoid conflicts with main backend
-- This migration should be run on existing deployments that have the old table structure

-- Rename the users table to client_auth
RENAME TABLE users TO client_auth;

-- Update foreign key constraints in login_methods table
ALTER TABLE login_methods 
DROP FOREIGN KEY login_methods_ibfk_1;

ALTER TABLE login_methods 
ADD CONSTRAINT login_methods_ibfk_1 
FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE;

-- Update foreign key constraints in auth_tokens table
ALTER TABLE auth_tokens 
DROP FOREIGN KEY auth_tokens_ibfk_1;

ALTER TABLE auth_tokens 
ADD CONSTRAINT auth_tokens_ibfk_1 
FOREIGN KEY (user_id) REFERENCES client_auth(id) ON DELETE CASCADE;

-- Record this migration
INSERT INTO schema_migrations (version, applied_at) VALUES (1, NOW()); 