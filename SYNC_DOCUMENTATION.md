# Synchronization System Documentation

## Overview

The synchronization system ensures data consistency between the `client_auth` table in the auth-service and the `customers` table in the customer-service. The system supports both manual synchronization via API endpoints and automatic synchronization during regular CRUD operations.

## Architecture

### Tables Structure

Both services maintain synchronized tables with the following structure:

**Auth Service (`client_auth` table):**
```sql
CREATE TABLE client_auth (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email_encrypted TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    sync_version INT DEFAULT 1,
    last_sync_at TIMESTAMP NULL
);
```

**Customer Service (`customers` table):**
```sql
CREATE TABLE customers (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    stripe_customer_id VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    sync_version INT DEFAULT 1,
    last_sync_at TIMESTAMP NULL
);
```

### Synchronization Fields

- `sync_version`: Incremented on each update to track changes
- `last_sync_at`: Timestamp of the last successful synchronization

## Automatic Synchronization

### How It Works

The system automatically triggers synchronization during regular CRUD operations:

1. **Create Operations**: When a new user/customer is created, the system automatically syncs the data to the other service
2. **Update Operations**: When user/customer data is updated, the system automatically syncs the changes
3. **Delete Operations**: When a user/customer is deleted, the system automatically syncs the deletion

### Implementation Details

#### Auth Service Automatic Sync

All CRUD operations in the auth-service automatically trigger sync to customer-service:

```go
// CreateUser - automatically syncs to customer-service
func (s *AuthService) CreateUser(ctx context.Context, req models.CreateUserRequest) (*models.ClientAuth, error) {
    // ... database operations within transaction ...
    
    // Trigger automatic sync to customer service
    go func() {
        if err := s.syncService.SyncToCustomerService(context.Background()); err != nil {
            log.Printf("Failed to sync new user %s to customer service: %v", userID, err)
        }
    }()
    
    return user, nil
}
```

#### Customer Service Automatic Sync

All CRUD operations in the customer-service automatically trigger sync to auth-service:

```go
// CreateCustomer - automatically syncs to auth-service
func (s *CustomerService) CreateCustomer(ctx context.Context, req models.CreateCustomerRequest) (*models.Customer, error) {
    // ... database operations within transaction ...
    
    // Trigger automatic sync to auth service
    go func() {
        if err := s.syncService.SyncToAuthService(context.Background()); err != nil {
            log.Printf("Failed to sync new customer %s to auth service: %v", customerID, err)
        }
    }()
    
    return customer, nil
}
```

### Transaction Safety

All database operations that trigger synchronization are wrapped in database transactions:

```go
// Start transaction
tx, err := s.db.BeginTx(ctx, nil)
if err != nil {
    return fmt.Errorf("failed to begin transaction: %w", err)
}
defer tx.Rollback()

// ... perform database operations ...

// Commit transaction
if err := tx.Commit(); err != nil {
    return fmt.Errorf("failed to commit transaction: %w", err)
}

// Trigger automatic sync (after successful commit)
go func() {
    if err := s.syncService.SyncToCustomerService(context.Background()); err != nil {
        log.Printf("Failed to sync: %v", err)
    }
}()
```

### Asynchronous Sync

Synchronization is performed asynchronously using goroutines to avoid blocking the main operation:

- Sync operations run in the background
- Failures are logged but don't affect the main operation
- This ensures that CRUD operations remain fast and responsive

## Manual Synchronization

### API Endpoints

Both services provide manual sync endpoints:

**Auth Service:**
- `POST /sync/trigger` - Triggers sync from auth-service to customer-service

**Customer Service:**
- `POST /sync/trigger` - Triggers sync from customer-service to auth-service

### Usage

```bash
# Trigger sync from auth-service to customer-service
curl -X POST http://auth-service:8080/sync/trigger

# Trigger sync from customer-service to auth-service
curl -X POST http://customer-service:8080/sync/trigger
```

## Conflict Resolution

The system uses version-based conflict resolution:

1. **Version Comparison**: Each record has a `sync_version` field that increments on updates
2. **Latest Wins**: The record with the higher version number takes precedence
3. **Timestamp Fallback**: If versions are equal, the record with the later `updated_at` timestamp wins

### Conflict Resolution Logic

```go
func (s *SyncService) resolveConflict(local, remote *models.ClientAuth) *models.ClientAuth {
    if local.SyncVersion > remote.SyncVersion {
        return local
    } else if remote.SyncVersion > local.SyncVersion {
        return remote
    } else {
        // Versions are equal, use timestamp
        if local.UpdatedAt.After(remote.UpdatedAt) {
            return local
        }
        return remote
    }
}
```

## Data Encryption

### Auth Service Encryption

The auth-service encrypts sensitive data before storage:

- **Email**: Encrypted using AES-256-GCM
- **Passwords**: Hashed using bcrypt

### Customer Service Encryption

The customer-service stores data in plain text but uses encryption for sync communication:

- **Sync Payload**: Encrypted using AES-256-GCM for secure transmission
- **API Communication**: Uses HTTPS for secure communication

## Error Handling

### Sync Failures

When automatic sync fails:

1. **Logging**: Errors are logged with context
2. **Non-blocking**: Main operations continue unaffected
3. **Retry**: Manual sync can be triggered to retry failed operations

### Common Error Scenarios

- **Network Issues**: Service unavailable or timeout
- **Data Conflicts**: Version conflicts during sync
- **Encryption Errors**: Key mismatches or corrupted data
- **Database Errors**: Constraint violations or connection issues

## Monitoring and Debugging

### Logging

Both services log sync operations:

```
2024/01/15 10:30:45 Failed to sync new user abc123 to customer service: connection refused
2024/01/15 10:30:46 Successfully synced updated user def456 to customer service
```

### Health Checks

Monitor sync health by checking:

1. **Sync Endpoints**: Verify both services are reachable
2. **Database Consistency**: Compare record counts and versions
3. **Error Logs**: Monitor for sync failures

### Debugging Tips

1. **Check Service URLs**: Ensure `AUTH_SERVICE_URL` and `CUSTOMER_SERVICE_URL` are correct
2. **Verify Encryption Keys**: Ensure both services use the same encryption key
3. **Monitor Network**: Check connectivity between services
4. **Review Logs**: Look for sync-related error messages

## Configuration

### Environment Variables

**Auth Service:**
```bash
CUSTOMER_SERVICE_URL=http://customer-service:8080
ENCRYPTION_KEY=your-32-byte-encryption-key
```

**Customer Service:**
```bash
AUTH_SERVICE_URL=http://auth-service:8080
ENCRYPTION_KEY=your-32-byte-encryption-key
```

### Database Migrations

Run migrations to add sync fields:

```bash
# Auth service
mysql -u root -p auth_db < database/migrations/002_add_sync_fields.sql

# Customer service
mysql -u root -p customer_db < database/migrations/003_add_sync_fields.sql
```

## Best Practices

### Development

1. **Always Use Transactions**: Wrap all database operations in transactions
2. **Handle Sync Errors**: Log sync failures but don't block main operations
3. **Test Sync Scenarios**: Test both automatic and manual sync
4. **Monitor Performance**: Ensure sync doesn't impact response times

### Production

1. **Monitor Sync Health**: Set up alerts for sync failures
2. **Regular Manual Sync**: Schedule periodic manual syncs as backup
3. **Backup Strategies**: Ensure both databases are backed up
4. **Load Testing**: Test sync performance under load

### Security

1. **Secure Communication**: Use HTTPS between services
2. **Encryption Keys**: Rotate encryption keys regularly
3. **Access Control**: Restrict sync endpoints to internal network
4. **Audit Logging**: Log all sync operations for audit purposes

## Troubleshooting

### Common Issues

1. **Sync Not Working**: Check service URLs and network connectivity
2. **Data Inconsistencies**: Run manual sync to resolve conflicts
3. **Performance Issues**: Monitor sync frequency and optimize if needed
4. **Encryption Errors**: Verify encryption keys are identical

### Recovery Procedures

1. **Manual Sync**: Use API endpoints to trigger manual sync
2. **Database Reset**: In extreme cases, reset sync versions and re-sync
3. **Service Restart**: Restart services if sync gets stuck
4. **Log Analysis**: Review logs to identify root causes

## Future Enhancements

### Planned Features

1. **Real-time Sync**: WebSocket-based real-time synchronization
2. **Batch Sync**: Optimize for large datasets
3. **Sync Metrics**: Detailed metrics and monitoring
4. **Conflict Resolution UI**: Web interface for resolving conflicts
5. **Multi-service Sync**: Extend to support more services

### Performance Optimizations

1. **Incremental Sync**: Only sync changed records
2. **Compression**: Compress sync payloads
3. **Connection Pooling**: Optimize database connections
4. **Caching**: Cache frequently accessed data 