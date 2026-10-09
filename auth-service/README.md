# Authentication Service

A secure Go microservice for handling user authentication, JWT token management, and user management for the CleanApp platform.

## Features

- **User Registration**: Email/password registration with encrypted email storage
- **User Authentication**: Email/password login with JWT token generation
- **Token Management**: Access and refresh token generation, validation, and invalidation
- **User Management**: CRUD operations for user profiles
- **Token Validation**: Service-to-service token validation endpoint
- **JWT Bearer Token Authentication**
- **RESTful API with Gin framework**
- **MySQL database with proper schema design**
- **Database migrations for safe schema updates**

## Architecture

### Authentication Flow

1. **User Registration**: Creates a user account with encrypted email and hashed password
2. **User Login**: Authenticates credentials and returns JWT token pair
3. **Token Validation**: Validates tokens for protected endpoints
4. **Token Refresh**: Uses refresh token to generate new access token
5. **User Management**: Update and delete user profiles

### Database Schema

The service uses MySQL with the following tables:
- `client_auth`: Core user information with encrypted email (renamed from `users` to avoid conflicts)
- `login_methods`: Authentication methods (email/password, OAuth)
- `auth_tokens`: JWT token management with access/refresh token types
- `schema_migrations`: Tracks applied database migrations

### Security Features

1. **Encryption**: AES-256-GCM encryption for emails
2. **Password Hashing**: bcrypt for password storage
3. **JWT Tokens**: Secure bearer token authentication
4. **HTTPS**: Enforced for all sensitive data transmission
5. **Token Expiration**: Configurable token lifetimes
6. **Migration System**: Safe, incremental database updates

## Project Structure

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
- Go 1.23+
- MySQL 8.0+
- Docker & Docker Compose (optional)

### Environment Variables

Create a `.env` file:

```env
DB_USER=cleanapp_user
DB_PASSWORD=<required-db-password>
DB_HOST=localhost
DB_PORT=3306
ENCRYPTION_KEY=<required-64-character-hex-aes256-key>
JWT_SECRET=<required-jwt-secret>
PORT=8080
```

### Password reset emails through Google Workspace

Set the provider explicitly to send password reset emails from the Workspace
account. The production SMTP relay configuration is:

```env
EMAIL_PROVIDER=google_workspace
EMAIL_FROM_NAME=CleanApp
EMAIL_FROM_ADDRESS=info@cleanapp.io
SMTP_HOST=smtp-relay.gmail.com
SMTP_PORT=587
SMTP_TIMEOUT=30s
FRONTEND_URL=https://cleanapp.io
```

Workspace's SMTP relay must allow the server's outbound IP and require TLS.
The transport uses STARTTLS and verifies the server certificate. IP-authorized
relay needs no SMTP username or password. For authenticated SMTP, set
`SMTP_USERNAME=info@cleanapp.io` together with either `SMTP_PASSWORD` or
`SMTP_PASSWORD_FILE` pointing to a mounted secret; use `smtp.gmail.com` when
authenticating directly to the mailbox. Keep credentials out of checked-in files.

Selecting `google_workspace` initializes the password reset sender even when
`SENDGRID_API_KEY` is absent. Invalid SMTP configuration fails service startup.
SMTP delivery failures keep the existing password reset response that avoids
revealing whether the account exists, and are logged for operators. The reset
link, subject, and text/HTML email content are unchanged.

`EMAIL_FROM_NAME` and `EMAIL_FROM_ADDRESS` fall back to `SENDGRID_FROM_NAME`
and `SENDGRID_FROM_EMAIL`, then to `CleanApp` and `info@cleanapp.io`.
For rollback, set `EMAIL_PROVIDER=sendgrid` and provide `SENDGRID_API_KEY`.
An unset provider retains the legacy SendGrid behavior, including disabled email
when its API key is absent.

### Quick Start with Docker

```bash
docker-compose up -d
```

### Manual Setup

1. Install dependencies:
   ```bash
   go mod download
   ```

2. Set up MySQL database:
   ```sql
   CREATE DATABASE cleanapp;
   ```

3. Run the service:
   ```bash
   go run main.go
   ```

## API Endpoints

All endpoints are prefixed with `/api/v3`

### Public Endpoints

#### POST /api/v3/auth/register
Register a new user account.

```json
{
  "name": "John Doe",
  "email": "john@example.com",
  "password": "securepassword123"
}
```

#### POST /api/v3/auth/login
Authenticate a user and receive JWT tokens.

```json
{
  "email": "john@example.com",
  "password": "securepassword123"
}
```

Response:
```json
{
  "token": "<access_token>",
  "refresh_token": "<refresh_token>",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

#### POST /api/v3/auth/refresh
Refresh an access token using a refresh token.

```json
{
  "refresh_token": "<refresh_token>"
}
```

#### POST /api/v3/validate-token
Validate a JWT token (for other services).

```json
{
  "token": "<access_token>"
}
```

Response:
```json
{
  "valid": true,
  "user_id": "user_1234567890"
}
```

#### GET /api/v3/users/exists?email=user@example.com
Check if a user exists by email address.

#### GET /api/v3/health
Health check endpoint.

### Protected Endpoints (Require Bearer Token)

All protected endpoints require the Authorization header:
```
Authorization: Bearer <token>
```

#### User Management

- **GET /api/v3/users/me** - Get current user information
- **PUT /api/v3/users/me** - Update user information
- **DELETE /api/v3/users/me** - Delete user account

#### Authentication

- **POST /api/v3/auth/logout** - Logout and invalidate token

## Service Integration

Other Go services should prefer local JWT verification against the shared `auth_tokens` table via `cleanapp-common/authx`. The `/api/v3/validate-token` endpoint remains available for compatibility and non-Go callers.

## Development

### Running Tests
```bash
go test ./...
```

### Database Migrations
Run explicit migrations instead of relying on service startup mutation:

```bash
go run ./cmd/migrate
```

Add new migration steps in `database/migrate.go`.

#### Important: Table Renaming Migration
If you're upgrading from a previous version that used the `users` table, you'll need to run the migration to rename it to `client_auth`:

```sql
-- Run this migration on existing deployments
RENAME TABLE users TO client_auth;
```

See `database/migrations/001_rename_users_to_client_auth.sql` for the complete migration script.

### Building for Production
```bash
go build -o auth-service main.go
``` 
