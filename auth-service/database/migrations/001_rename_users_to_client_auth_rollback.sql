-- Rollback Migration: Revert client_auth table back to users
-- This migration should only be used if you need to rollback the table renaming

-- Update foreign key constraints in auth_tokens table
ALTER TABLE auth_tokens 
DROP FOREIGN KEY auth_tokens_ibfk_1;

ALTER TABLE auth_tokens 
ADD CONSTRAINT auth_tokens_ibfk_1 
FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- Update foreign key constraints in login_methods table
ALTER TABLE login_methods 
DROP FOREIGN KEY login_methods_ibfk_1;

ALTER TABLE login_methods 
ADD CONSTRAINT login_methods_ibfk_1 
FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- Rename the client_auth table back to users
RENAME TABLE client_auth TO users;

-- Remove the migration record
DELETE FROM schema_migrations WHERE version = 1; 