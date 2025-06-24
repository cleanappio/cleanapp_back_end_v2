# Customer Service

A secure Go microservice for managing CleanApp platform customers with subscription management, authentication, and payment processing.

## Features

- **Multi-provider Authentication**: Email/password and OAuth (Google, Apple, Facebook)
- **Flexible Subscription Management**: Customers can exist without subscriptions
- **Subscription Tiers**: Three tiers (Base, Advanced, Exclusive) with monthly/annual billing
- **Secure Payment Processing**: Stripe integration - no credit card data stored locally
- **JWT Bearer Token Authentication**
- **RESTful API with Gin framework**
- **MySQL database with proper schema design**
- **Database migrations for safe schema updates**

## Architecture

### Customer and Subscription Flow

The service separates customer creation from subscription management:

1. **Customer Creation**: Creates a basic customer account without any subscription
2. **Subscription Creation**: Customer can then add a subscription with payment information
3. **Subscription Management**: Customers can update, cancel, or view their subscriptions

### Authentication Design

- **Email Login**: Email stored encrypted in customers table, password hash in login_methods
- **OAuth Login**: OAuth provider ID stored in login_methods, linked to customer
- **No Redundancy**: Email is stored only once (in customers table), not duplicated in login_methods

This separation allows for:
- Free trial periods
- Customer accounts without active subscriptions
- Multiple subscription management strategies
- Better separation of concerns

### Database Schema

The service uses MySQL with the following tables:
- `customers`: Core customer information with encrypted email and Stripe customer ID
- `login_methods`: Authentication methods (one per type per customer)
  - Email login uses password_hash
  - OAuth login uses oauth_id from provider
  - No redundant email storage (uses customers table)
- `customer_areas`: Many-to-many relationship for service areas
- `subscriptions`: Subscription plans with Stripe subscription IDs
- `payment_methods`: Stripe payment method references (no card data stored)
- `billing_history`: Payment transaction records with Stripe payment intent IDs
- `auth_tokens`: JWT token management
- `schema_migrations`: Tracks applied database migrations

### Database Migrations

The service includes an incremental migration system:
- Migrations are automatically applied on service startup
- Migration history is tracked in `schema_migrations` table
- Each migration has a version number and can be rolled back if needed

Current migrations:
1. **Version 1**: Remove redundant `method_id` field from `login_methods` table
2. **Version 2**: Migrate payment methods to use Stripe (removes card data storage)

### Security Features

1. **Encryption**: AES-256-GCM encryption for emails
2. **Password Hashing**: bcrypt for password storage
3. **JWT Tokens**: Secure bearer token authentication
4. **Payment Security**: Stripe integration - no credit card data stored
5. **HTTPS**: Enforced for all sensitive data transmission
6. **Business Logic Separation**: Customer accounts are independent from subscriptions
7. **Optimized Schema**: No redundant data storage (emails stored once)
8. **Migration System**: Safe, incremental database updates with version tracking

## Project Structure

The project follows a clean architecture pattern with clear separation of concerns:

```
├── config/         # Configuration management
├── models/         # Data models and structs
├── database/       # Database operations and business logic
├── handlers/       # HTTP request handlers
├── middleware/     # HTTP middleware (auth, CORS, etc.)
└── utils/          # Utility functions and helpers
    └── encryption/ # AES encryption utilities
```

## Setup

### Prerequisites
- Go 1.21+
- MySQL 8.0+
- Docker & Docker Compose (optional)
- Make (optional, for using Makefile commands)

### Environment Variables

Create a `.env` file from the example:

```bash
cp .env.example .env
```

Then edit the `.env` file with your configuration:

```env
DB_USER=cleanapp_user
DB_PASSWORD=cleanapp_password
DB_HOST=localhost
DB_PORT=3306
ENCRYPTION_KEY=your_64_character_hex_string_for_aes256_encryption
JWT_SECRET=your_super_secret_jwt_key
PORT=8080
```

### Quick Start with Docker

Using Make:
```bash
make setup
```

Or manually:
```bash
docker-compose up -d
```

### Manual Setup

1. Install dependencies:
   ```bash
   make deps
   # or
   go mod download
   ```

2. Set up MySQL database:
   ```sql
   CREATE DATABASE cleanapp;
   ```

3. Run the service:
   ```bash
   make run
   # or
   go run main.go
   ```

### Development with Hot Reload

Install Air for hot reload:
```bash
go install github.com/cosmtrek/air@latest
```

Then run:
```bash
make dev
# or
air
```

## API Endpoints

All endpoints are prefixed with `/api/v3`

### Public Endpoints

#### POST /api/v3/login
Authenticate a customer and receive a JWT token.

**Email/Password Login:**
```json
{
  "email": "user@example.com",
  "password": "securepassword"
}
```
Note: Do not include the `provider` field for email/password login.

**OAuth Login:**
```json
{
  "provider": "google",
  "token": "oauth-user-id-from-provider"
}
```
Note: The `provider` must be one of: `google`, `apple`, `facebook`

Response:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

#### POST /api/v3/customers
Create a new customer account (without subscription).

```json
{
  "name": "John Doe",
  "email": "john@example.com",
  "password": "securepassword123",
  "area_ids": [1, 2, 3]
}
```

#### GET /api/v3/health
Health check endpoint.

### Protected Endpoints (Require Bearer Token)

All protected endpoints require the Authorization header:
```
Authorization: Bearer <token>
```

#### Customer Management

- **GET /api/v3/customers/me** - Get current customer information
- **PUT /api/v3/customers/me** - Update customer information
- **DELETE /api/v3/customers/me** - Delete customer account

#### Subscription Management

- **POST /api/v3/subscriptions** - Create a new subscription (requires Stripe payment method)

```json
{
  "plan_type": "base",
  "billing_cycle": "monthly",
  "stripe_payment_method_id": "pm_1234567890abcdef"
}
```

- **GET /api/v3/subscriptions/me** - Get current subscription
- **PUT /api/v3/subscriptions/me** - Update subscription plan
- **DELETE /api/v3/subscriptions/me** - Cancel subscription
- **GET /api/v3/billing-history** - Get billing history (supports pagination)

#### Payment Methods

- **GET /api/v3/payment-methods** - List payment methods
- **POST /api/v3/payment-methods** - Add new payment method
- **PUT /api/v3/payment-methods/:id** - Update payment method
- **DELETE /api/v3/payment-methods/:id** - Delete payment method

### Webhook Endpoints

- **POST /api/v3/webhooks/payment** - Payment processor webhook

## Security Considerations

1. **Encryption Key**: Generate a secure 32-byte key for production:
   ```bash
   openssl rand -hex 32
   ```

2. **JWT Secret**: Use a strong, random secret for JWT signing

3. **HTTPS**: Always use HTTPS in production

4. **Database Security**: 
   - Use strong passwords
   - Restrict database access
   - Regular backups

5. **OAuth Integration**: 
   - Properly validate tokens with OAuth providers
   - Store OAuth provider IDs in oauth_id field
   - Each customer can link one account per OAuth provider

## Subscription Plans

### Business Rules
- A customer can be created without a subscription
- Each customer can have only one active subscription at a time
- Payment information is required when creating a subscription
- Subscriptions can be updated or canceled independently

### Pricing Tiers
- **Base**: Entry-level features
- **Advanced**: Enhanced features
- **Exclusive**: Premium features with priority support

### Billing Cycles
- **Monthly**: Standard monthly billing
- **Annual**: 12-month billing with discount

## Development

### Running Tests
```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run specific package tests
go test ./handlers/...
```

### Code Quality
```bash
# Format code
make fmt

# Run linter (requires golangci-lint)
make lint

# Security check (requires gosec)
make security
```

### Building for Production
```bash
# Build for current platform
make build

# Build for Linux
make build-linux

# Build Docker image
make docker-build
```

### Database Migrations

The service automatically creates the schema on startup and runs any pending migrations.

To add a new migration:
1. Edit `database/schema.go`
2. Add a new Migration struct to the `Migrations` slice
3. Increment the version number
4. Provide Up and Down SQL statements
5. The migration will run automatically on next startup

Example:
```go
{
    Version: 2,
    Name:    "add_user_preferences",
    Up:      "ALTER TABLE customers ADD COLUMN preferences JSON;",
    Down:    "ALTER TABLE customers DROP COLUMN preferences;",
}
```

Check migration status:
```bash
make migrate-status
# Or directly in MySQL:
# SELECT * FROM cleanapp.schema_migrations;
```

### Monitoring and Logging

The service uses structured logging. In production, consider:
- Adding request ID middleware for tracing
- Integrating with logging services (ELK, CloudWatch, etc.)
- Adding metrics with Prometheus
- Setting up alerts for critical errors

## API Usage Examples

### Create Customer (Step 1)
```bash
curl -X POST http://localhost:8080/api/v3/customers \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test User",
    "email": "test@example.com",
    "password": "password123",
    "area_ids": [1, 2]
  }'
```

### Login Examples
```bash
# Email/Password login (no provider field needed)
curl -X POST http://localhost:8080/api/v3/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "password123"
  }'

# OAuth login (no email/password needed)
curl -X POST http://localhost:8080/api/v3/login \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "google",
    "token": "google-user-id-123456"
  }'
```

### Create Subscription (Step 2 - Requires Authentication)
```bash
# First, create a payment method in Stripe and get the payment method ID
# Then use that ID to create the subscription:
curl -X POST http://localhost:8080/api/v3/subscriptions \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "plan_type": "base",
    "billing_cycle": "monthly",
    "stripe_payment_method_id": "pm_1234567890abcdef"
  }'
```

### Get Customer Info
```bash
curl -X GET http://localhost:8080/api/v3/customers/me \
  -H "Authorization: Bearer <your-token>"
```

### Get Subscription Info
```bash
curl -X GET http://localhost:8080/api/v3/subscriptions/me \
  -H "Authorization: Bearer <your-token>"
```

### Update Subscription
```bash
curl -X PUT http://localhost:8080/api/v3/subscriptions/me \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "plan_type": "advanced",
    "billing_cycle": "annual"
  }'
```

### Cancel Subscription
```bash
curl -X DELETE http://localhost:8080/api/v3/subscriptions/me \
  -H "Authorization: Bearer <your-token>"
```

### Add Payment Method
```bash
# First create a payment method in Stripe, then attach it:
curl -X POST http://localhost:8080/api/v3/payment-methods \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "stripe_payment_method_id": "pm_1234567890abcdef",
    "is_default": true
  }'
```

## Production Deployment

1. Use environment-specific configuration
2. Enable HTTPS with valid SSL certificates
3. Set up database replicas for high availability
4. Implement rate limiting
5. Add monitoring and logging
6. Regular security audits
7. Implement backup strategies
8. Review and test migrations before applying to production

## Future Enhancements

- [ ] Complete Stripe integration (subscriptions, webhooks)
- [ ] Implement OAuth customer registration
- [ ] Add free trial period support
- [ ] Implement subscription pause/resume
- [ ] Add webhook support for payment processing
- [ ] Implement subscription upgrade/downgrade with proration
- [ ] Add email verification
- [ ] Implement 2FA
- [ ] Add API versioning strategy (currently v3)
- [ ] Implement caching layer
- [ ] Add comprehensive logging
- [ ] Create admin endpoints
- [ ] Add metrics and monitoring
- [ ] Support multiple payment methods per customer
- [ ] Add subscription renewal notifications
- [ ] Add migration rollback commands

## Stripe Integration

The service integrates with Stripe for secure payment processing:

### Setting Up Stripe

1. **Create Stripe Products and Prices**:
   - Create products for each plan tier (Base, Advanced, Exclusive)
   - Create prices for monthly and annual billing cycles
   - Add the price IDs to your `.env` file

2. **Frontend Integration**:
   - Use Stripe Elements or Checkout to collect payment information
   - Create PaymentMethod in Stripe
   - Send the `pm_xxx` ID to your API

3. **Webhook Configuration**:
   - Set up webhook endpoint: `https://your-domain.com/api/v3/webhooks/payment`
   - Subscribe to events: `payment_intent.succeeded`, `payment_intent.failed`, etc.
   - Add webhook secret to `.env`

### Payment Flow

1. **Customer creates payment method in Stripe** (frontend)
2. **Customer sends payment method ID to create subscription**
3. **Backend creates/updates Stripe customer**
4. **Backend attaches payment method to customer**
5. **Backend creates subscription in Stripe** (in production)
6. **Stripe processes recurring payments automatically**

### Security Benefits

- **PCI Compliance**: No credit card data touches your servers
- **SCA Ready**: Supports Strong Customer Authentication
- **Secure Storage**: Payment methods stored and managed by Stripe
- **Tokenization**: Only store Stripe reference IDs

## License

This project is proprietary to CleanApp Platform.
