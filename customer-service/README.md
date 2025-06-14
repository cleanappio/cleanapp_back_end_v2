# CleanApp Customer Microservice

A secure Go microservice for managing CleanApp platform customers with subscription management, authentication, and payment processing.

## Features

- **Multi-provider Authentication**: Email/password and OAuth (Google, Apple, Facebook)
- **Subscription Management**: Three tiers (Base, Advanced, Exclusive) with monthly/annual billing
- **Secure Data Handling**: AES-256 encryption for sensitive data
- **JWT Bearer Token Authentication**
- **RESTful API with Gin framework**
- **MySQL database with proper schema design**

## Architecture

### Database Schema

The service uses MySQL with the following tables:
- `customers`: Core customer information
- `login_methods`: Multiple authentication methods per customer
- `customer_areas`: Many-to-many relationship for service areas
- `subscriptions`: Subscription plans and billing cycles
- `payment_methods`: Encrypted credit card information
- `billing_history`: Payment transaction records
- `auth_tokens`: JWT token management

### Security Features

1. **Encryption**: AES-256-GCM encryption for emails and payment data
2. **Password Hashing**: bcrypt for password storage
3. **JWT Tokens**: Secure bearer token authentication
4. **HTTPS**: Enforced for all sensitive data transmission

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
TRUSTED_PROXIES=127.0.0.1,::1
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

All endpoints are prefixed with `/api/v1`

### Public Endpoints

#### POST /api/v1/login
Authenticate a customer and receive a JWT token.

```json
{
  "email": "user@example.com",
  "password": "securepassword"
}
```

OR for OAuth:

```json
{
  "provider": "google",
  "token": "oauth-token-from-provider"
}
```

Response:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

#### POST /api/v1/customers
Create a new customer account.

```json
{
  "name": "John Doe",
  "email": "john@example.com",
  "password": "securepassword123",
  "area_ids": [1, 2, 3],
  "plan_type": "advanced",
  "billing_cycle": "annual",
  "card_number": "4111111111111111",
  "card_holder": "John Doe",
  "expiry": "12/25",
  "cvv": "123"
}
```

#### GET /api/v1/health
Health check endpoint.

### Protected Endpoints (Require Bearer Token)

All protected endpoints require the Authorization header:
```
Authorization: Bearer <token>
```

#### Customer Management

- **GET /api/v1/customers/me** - Get current customer information
- **PUT /api/v1/customers/me** - Update customer information
- **DELETE /api/v1/customers/me** - Delete customer account

#### Subscription Management

- **GET /api/v1/subscriptions/me** - Get current subscription
- **PUT /api/v1/subscriptions/me** - Update subscription plan
- **DELETE /api/v1/subscriptions/me** - Cancel subscription
- **GET /api/v1/billing-history** - Get billing history (supports pagination)

#### Payment Methods

- **GET /api/v1/payment-methods** - List payment methods
- **POST /api/v1/payment-methods** - Add new payment method
- **PUT /api/v1/payment-methods/:id** - Update payment method
- **DELETE /api/v1/payment-methods/:id** - Delete payment method

### Webhook Endpoints

- **POST /api/v1/webhooks/payment** - Payment processor webhook

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

5. **OAuth Integration**: Properly validate tokens with OAuth providers

## Subscription Plans

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

The service automatically creates the schema on startup. For production environments, consider using a migration tool like:
- [golang-migrate](https://github.com/golang-migrate/migrate)
- [goose](https://github.com/pressly/goose)

### Monitoring and Logging

The service uses structured logging. In production, consider:
- Adding request ID middleware for tracing
- Integrating with logging services (ELK, CloudWatch, etc.)
- Adding metrics with Prometheus
- Setting up alerts for critical errors

## API Usage Examples

### Create Customer
```bash
curl -X POST http://localhost:8080/api/v1/customers \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test User",
    "email": "test@example.com",
    "password": "password123",
    "area_ids": [1, 2],
    "plan_type": "base",
    "billing_cycle": "monthly",
    "card_number": "4111111111111111",
    "card_holder": "Test User",
    "expiry": "12/25",
    "cvv": "123"
  }'
```

### Login
```bash
curl -X POST http://localhost:8080/api/v1/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "password123"
  }'
```

### Get Customer Info
```bash
curl -X GET http://localhost:8080/api/v1/customers/me \
  -H "Authorization: Bearer <your-token>"
```

### Update Subscription
```bash
curl -X PUT http://localhost:8080/api/v1/subscriptions/me \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "plan_type": "advanced",
    "billing_cycle": "annual"
  }'
```

### Add Payment Method
```bash
curl -X POST http://localhost:8080/api/v1/payment-methods \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "card_number": "5555555555554444",
    "card_holder": "Test User",
    "expiry": "12/26",
    "cvv": "456",
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

## Future Enhancements

- [ ] Add webhook support for payment processing
- [ ] Implement subscription upgrade/downgrade
- [ ] Add email verification
- [ ] Implement 2FA
- [ ] Add API versioning
- [ ] Implement caching layer
- [ ] Add comprehensive logging
- [ ] Create admin endpoints
- [ ] Add metrics and monitoring

## License

This project is proprietary to CleanApp Platform.
