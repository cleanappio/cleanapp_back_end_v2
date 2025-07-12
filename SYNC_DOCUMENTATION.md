# Normalized Database Architecture Documentation

## Overview

The system has been normalized to eliminate data duplication and synchronization complexity. The `client_auth` table in the auth-service now exclusively manages authentication data (name, email, password), while the `customers` table in the customer-service focuses solely on subscription and billing data.

## Architecture

### Data Flow

```
┌─────────────────┐    HTTP API    ┌─────────────────┐
│   Auth Service  │ ◄────────────► │ Customer Service│
│                 │                │                 │
│ client_auth     │                │ customers       │
│ (auth data)     │                │ (billing data)  │
└─────────────────┘                └─────────────────┘
```

### Table Structure

**Auth Service (`client_auth` table):**
```sql
CREATE TABLE client_auth (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email_encrypted TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_email_encrypted (email_encrypted(255))
);
```

**Customer Service (`customers` table):**
```sql
CREATE TABLE customers (
    id VARCHAR(255) PRIMARY KEY,
    stripe_customer_id VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_stripe_customer (stripe_customer_id)
);
```

## Key Benefits

### 1. **No Data Duplication**
- Authentication data (name, email) is stored only in `client_auth`
- Billing data is stored only in `customers`
- No synchronization required between services

### 2. **Clear Separation of Concerns**
- **Auth Service**: Handles user authentication, registration, and profile management
- **Customer Service**: Handles subscriptions, billing, and payment processing

### 3. **Simplified Architecture**
- No complex synchronization logic
- No version conflict resolution
- No data consistency issues

### 4. **Flexible User States**
- Users can exist in `client_auth` without having a `customers` record
- Customer records are created only when subscription is needed
- Supports free users who don't need billing

## Data Relationships

### User Registration Flow

1. **User registers** → Record created in `client_auth` (auth-service)
2. **User subscribes** → Record created in `customers` (customer-service)
3. **Both records share the same `id`** for relationship

### API Integration

The customer-service can fetch user data from auth-service when needed:

```go
// Example: Get user profile for billing
func (s *CustomerService) GetCustomerWithProfile(ctx context.Context, customerID string) (*CustomerWithProfile, error) {
    // Get customer data from local database
    customer, err := s.GetCustomer(ctx, customerID)
    if err != nil {
        return nil, err
    }
    
    // Get user profile from auth-service
    userProfile, err := s.fetchUserProfileFromAuthService(ctx, customerID)
    if err != nil {
        return nil, err
    }
    
    return &CustomerWithProfile{
        Customer: customer,
        Profile:  userProfile,
    }, nil
}
```

## Database Migrations

### Auth Service Migration

The auth-service maintains its original structure with authentication data:

```sql
-- No changes needed - auth-service keeps its structure
-- client_auth table remains focused on authentication
```

### Customer Service Migration

The customer-service has been normalized to remove redundant fields:

```sql
-- Migration 6: Normalize customers table
-- Remove redundant fields (name, email_encrypted) from customers table
-- These fields are now managed by the auth-service in client_auth table

-- Remove name column from customers table
ALTER TABLE customers DROP COLUMN IF EXISTS name;

-- Remove email_encrypted column from customers table  
ALTER TABLE customers DROP COLUMN IF EXISTS email_encrypted;

-- Remove sync-related columns
ALTER TABLE customers DROP COLUMN IF EXISTS sync_version;
ALTER TABLE customers DROP COLUMN IF EXISTS last_sync_at;

-- Remove sync-related indexes
DROP INDEX IF EXISTS idx_sync_version ON customers;
```

## API Changes

### Customer Service API Updates

**Create Customer:**
```go
// Before: Required name, email, password
type CreateCustomerRequest struct {
    Name     string `json:"name" binding:"required,max=256"`
    Email    string `json:"email" binding:"required,email"`
    Password string `json:"password" binding:"required,min=8"`
    AreaIDs  []int  `json:"area_ids" binding:"required,min=1"`
}

// After: Only requires area IDs (auth data handled by auth-service)
type CreateCustomerRequest struct {
    AreaIDs []int `json:"area_ids" binding:"required,min=1"`
}
```

**Update Customer:**
```go
// Before: Could update name and email
type UpdateCustomerRequest struct {
    Name    *string `json:"name,omitempty" binding:"omitempty,max=256"`
    Email   *string `json:"email,omitempty" binding:"omitempty,email"`
    AreaIDs []int   `json:"area_ids,omitempty"`
}

// After: Only area updates (name/email handled by auth-service)
type UpdateCustomerRequest struct {
    AreaIDs []int `json:"area_ids,omitempty"`
}
```

### Customer Model Updates

```go
// Before: Included name and email
type Customer struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// After: Focused on subscription data
type Customer struct {
    ID        string    `json:"id"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

## Service Integration

### Cross-Service Data Access

When the customer-service needs user profile data, it can make HTTP requests to the auth-service:

```go
// Example: Get user profile for invoice generation
func (s *CustomerService) GetUserProfileForInvoice(ctx context.Context, customerID string) (*UserProfile, error) {
    resp, err := http.Get(fmt.Sprintf("%s/api/v3/users/%s", s.authServiceURL, customerID))
    if err != nil {
        return nil, fmt.Errorf("failed to fetch user profile: %w", err)
    }
    defer resp.Body.Close()
    
    var profile UserProfile
    if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
        return nil, fmt.Errorf("failed to decode user profile: %w", err)
    }
    
    return &profile, nil
}
```

### Authentication Integration

The customer-service continues to proxy authentication requests to the auth-service:

```go
// Login is proxied to auth-service
func (h *Handlers) Login(c *gin.Context) {
    // Forward request to auth-service
    resp, err := http.Post(fmt.Sprintf("%s/api/v3/auth/login", h.authServiceURL), 
        "application/json", c.Request.Body)
    // ... handle response
}
```

## Configuration

### Environment Variables

**Auth Service:**
```bash
# Database
DB_USER=root
DB_PASSWORD=password
DB_HOST=localhost
DB_PORT=3306

# Security
JWT_SECRET=your-secret-key-here
ENCRYPTION_KEY=your-32-byte-encryption-key

# Server
PORT=8080
```

**Customer Service:**
```bash
# Database
DB_USER=root
DB_PASSWORD=password
DB_HOST=localhost
DB_PORT=3306

# Server
PORT=8081

# Auth Service Integration
AUTH_SERVICE_URL=http://auth-service:8080

# Stripe
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
```

## Migration Strategy

### Production Migration

1. **Backup both databases**
2. **Run customer-service migration** to remove redundant fields
3. **Update application code** to use new API structure
4. **Deploy updated services**
5. **Verify data integrity**

### Rollback Plan

If issues arise, the migration can be rolled back:

```sql
-- Rollback: Add back the removed columns
ALTER TABLE customers 
    ADD COLUMN name VARCHAR(256) NOT NULL DEFAULT '',
    ADD COLUMN email_encrypted TEXT NOT NULL DEFAULT '',
    ADD COLUMN sync_version INT DEFAULT 1,
    ADD COLUMN last_sync_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ADD INDEX idx_sync_version (sync_version);
```

## Best Practices

### Development

1. **Clear API Boundaries**: Keep auth and billing concerns separate
2. **Cross-Service Calls**: Use HTTP APIs for data that spans services
3. **Error Handling**: Handle network failures gracefully
4. **Caching**: Consider caching frequently accessed user data

### Production

1. **Service Discovery**: Use proper service discovery for inter-service communication
2. **Circuit Breakers**: Implement circuit breakers for cross-service calls
3. **Monitoring**: Monitor cross-service API calls and response times
4. **Caching**: Implement caching strategies for user profile data

### Security

1. **Service-to-Service Auth**: Implement proper authentication between services
2. **Data Validation**: Validate all data received from other services
3. **Rate Limiting**: Apply rate limits to cross-service API calls
4. **Audit Logging**: Log all cross-service data access

## Troubleshooting

### Common Issues

1. **Service Unavailable**: Handle auth-service downtime gracefully
2. **Data Inconsistency**: Ensure proper error handling for missing user data
3. **Performance**: Monitor cross-service API call performance
4. **Authentication**: Verify service-to-service authentication

### Debugging Tips

1. **Check Service URLs**: Ensure `AUTH_SERVICE_URL` is correct
2. **Monitor Logs**: Check for cross-service API call errors
3. **Verify Data**: Ensure user IDs match between services
4. **Test APIs**: Verify auth-service APIs are accessible

## Future Enhancements

### Planned Features

1. **Caching Layer**: Implement Redis caching for user profiles
2. **Event-Driven Updates**: Use message queues for real-time updates
3. **GraphQL Federation**: Consider GraphQL for unified data access
4. **Service Mesh**: Implement service mesh for better inter-service communication

### Performance Optimizations

1. **Connection Pooling**: Optimize HTTP client connection pools
2. **Batch Requests**: Implement batch API calls where possible
3. **Compression**: Enable HTTP compression for cross-service calls
4. **CDN**: Use CDN for static user profile data

## Conclusion

The normalized architecture provides:

- **Simplified Data Management**: No synchronization complexity
- **Clear Service Boundaries**: Each service has a focused responsibility
- **Better Scalability**: Services can scale independently
- **Reduced Maintenance**: Less complex codebase to maintain
- **Flexible User States**: Supports users with and without subscriptions

This architecture is more maintainable, scalable, and follows microservices best practices by eliminating data duplication and complex synchronization logic. 