# Synchronization System Documentation

## Overview

The synchronization system ensures data consistency between the `client_auth` table in the auth-service and the `customers` table in the customer-service. This is critical for maintaining a unified user experience across both microservices.

## Architecture

### Data Flow

```
┌─────────────────┐    HTTP API    ┌─────────────────┐
│   Auth Service  │ ◄────────────► │ Customer Service│
│                 │                │                 │
│ client_auth     │                │ customers       │
│ table           │                │ table           │
└─────────────────┘                └─────────────────┘
```

### Synchronization Fields

Both tables now include synchronization fields:

- `sync_version`: Incremental version number for conflict resolution
- `last_sync_at`: Timestamp of last successful synchronization
- `updated_at`: Standard timestamp for tracking changes

## Database Schema Changes

### Auth Service (client_auth table)

```sql
CREATE TABLE IF NOT EXISTS client_auth (
    id VARCHAR(256) PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    email_encrypted TEXT NOT NULL,
    sync_version INT DEFAULT 1,
    last_sync_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_email_encrypted (email_encrypted(255)),
    INDEX idx_sync_version (sync_version)
);
```

### Customer Service (customers table)

```sql
CREATE TABLE IF NOT EXISTS customers (
    id VARCHAR(256) PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    email_encrypted TEXT NOT NULL,
    stripe_customer_id VARCHAR(256),
    sync_version INT DEFAULT 1,
    last_sync_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_stripe_customer (stripe_customer_id),
    INDEX idx_sync_version (sync_version)
);
```

## API Endpoints

### Auth Service Sync Endpoints

- `GET /api/v3/auth/sync` - Get all client_auth data for synchronization
- `POST /api/v3/auth/sync` - Receive customer data and sync to client_auth
- `POST /api/v3/auth/sync/trigger` - Trigger full bidirectional sync

### Customer Service Sync Endpoints

- `GET /api/v3/customers/sync` - Get all customer data for synchronization
- `POST /api/v3/customers/sync` - Receive auth data and sync to customers
- `POST /api/v3/customers/sync/trigger` - Trigger full bidirectional sync

## Synchronization Logic

### Conflict Resolution

The system uses a version-based conflict resolution strategy:

1. **Version Comparison**: When syncing, compare `sync_version` fields
2. **Newer Wins**: The record with the higher `sync_version` takes precedence
3. **Incremental Updates**: Each successful sync increments the `sync_version`

### Sync Process

1. **Fetch Data**: Service A fetches data from Service B via HTTP API
2. **Compare Versions**: For each record, compare `sync_version` values
3. **Update if Newer**: Only update if the source version is higher
4. **Increment Version**: After successful sync, increment local `sync_version`
5. **Update Timestamp**: Set `last_sync_at` to current time

### Bidirectional Sync

The system supports bidirectional synchronization:

- **Auth → Customer**: Sync client_auth records to customers table
- **Customer → Auth**: Sync customer records to client_auth table
- **Conflict Resolution**: Version-based resolution prevents data loss

## Configuration

### Environment Variables

#### Auth Service
```env
CUSTOMER_SERVICE_URL=http://customer-service:8081
ENCRYPTION_KEY=your_64_character_hex_string
```

#### Customer Service
```env
AUTH_SERVICE_URL=http://auth-service:8080
ENCRYPTION_KEY=your_64_character_hex_string
```

### Encryption

Both services use AES-256-GCM encryption with the same key to ensure:
- Email addresses are encrypted consistently
- Data can be decrypted by both services
- Security is maintained during transmission

## Implementation Details

### SyncService

Each service includes a `SyncService` that handles:

- **HTTP Communication**: RESTful API calls between services
- **Data Transformation**: Encryption/decryption of sensitive data
- **Version Management**: Incrementing sync versions
- **Error Handling**: Graceful handling of sync failures

### Error Handling

- **Network Failures**: Retry logic with exponential backoff
- **Data Corruption**: Skip corrupted records and log errors
- **Version Conflicts**: Log conflicts for manual resolution
- **Partial Failures**: Continue syncing other records

## Deployment Considerations

### Database Migrations

Run migrations in order:

1. **Auth Service**: `002_add_sync_fields.sql`
2. **Customer Service**: `003_add_sync_fields.sql`

### Service Dependencies

- Both services must be running for sync to work
- Services should be accessible via configured URLs
- Encryption keys must be identical across services

### Monitoring

Monitor sync health via:

- Sync endpoint responses
- Database sync_version increments
- last_sync_at timestamp updates
- Error logs for failed syncs

## Usage Examples

### Manual Sync Trigger

```bash
# Trigger full sync from auth service
curl -X POST http://auth-service:8080/api/v3/auth/sync/trigger

# Trigger full sync from customer service
curl -X POST http://customer-service:8081/api/v3/customers/sync/trigger
```

### Check Sync Status

```bash
# Check unsynced records in auth service
curl http://auth-service:8080/api/v3/auth/sync

# Check unsynced records in customer service
curl http://customer-service:8081/api/v3/customers/sync
```

## Troubleshooting

### Common Issues

1. **Sync Not Working**: Check service URLs and network connectivity
2. **Encryption Errors**: Verify encryption keys are identical
3. **Version Conflicts**: Check sync_version values in both databases
4. **Data Inconsistency**: Trigger manual sync and check logs

### Debug Commands

```sql
-- Check sync status in auth service
SELECT id, name, sync_version, last_sync_at, updated_at 
FROM client_auth 
ORDER BY updated_at DESC;

-- Check sync status in customer service
SELECT id, name, sync_version, last_sync_at, updated_at 
FROM customers 
ORDER BY updated_at DESC;
```

## Security Considerations

- All sensitive data (emails) is encrypted at rest
- HTTP communication should use HTTPS in production
- Encryption keys should be rotated regularly
- Access to sync endpoints should be restricted in production

## Future Enhancements

- **Automated Sync**: Scheduled background sync jobs
- **Real-time Sync**: Event-driven synchronization
- **Conflict Resolution UI**: Web interface for manual conflict resolution
- **Sync Analytics**: Metrics and monitoring dashboard
- **Multi-region Support**: Cross-region synchronization 