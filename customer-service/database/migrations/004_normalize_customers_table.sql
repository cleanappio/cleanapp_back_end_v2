-- Migration 4: Normalize customers table
-- Remove redundant fields (name, email_encrypted) from customers table
-- These fields are now managed by the auth-service in client_auth table
-- Remove sync fields as they are no longer needed

-- Remove name column from customers table
ALTER TABLE customers DROP COLUMN IF EXISTS name;

-- Remove email_encrypted column from customers table  
ALTER TABLE customers DROP COLUMN IF EXISTS email_encrypted;

-- Remove sync-related columns
ALTER TABLE customers DROP COLUMN IF EXISTS sync_version;
ALTER TABLE customers DROP COLUMN IF EXISTS last_sync_at;

-- Remove sync-related indexes
DROP INDEX IF EXISTS idx_sync_version ON customers;

-- Update customers table structure to be subscription-focused
-- Now customers table only contains subscription-related data
-- The relationship with auth data is maintained via the id field 