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
- `users`: Core user information with encrypted email
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
DB_PASSWORD=cleanapp_password
DB_HOST=localhost
DB_PORT=3306
ENCRYPTION_KEY=your_64_character_hex_string_for_aes256_encryption
JWT_SECRET=your_super_secret_jwt_key
PORT=8080
```

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
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

#### POST /api/v3/auth/refresh
Refresh an access token using a refresh token.

```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

#### POST /api/v3/validate-token
Validate a JWT token (for other services).

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs..."
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

Other services can validate tokens by calling the `/api/v3/validate-token` endpoint:

```go
// Example integration in another service
func validateTokenWithAuthService(token string) (string, error) {
    resp, err := http.PostJSON("http://auth-service:8080/api/v3/validate-token", 
        map[string]string{"token": token})
    if err != nil {
        return "", err
    }
    
    var result struct {
        Valid  bool   `json:"valid"`
        UserID string `json:"user_id"`
        Error  string `json:"error"`
    }
    
    if err := json.Unmarshal(resp.Body, &result); err != nil {
        return "", err
    }
    
    if !result.Valid {
        return "", errors.New(result.Error)
    }
    
    return result.UserID, nil
}
```

## Development

### Running Tests
```bash
go test ./...
```

### Database Migrations
Migrations are automatically applied on service startup. To add a new migration:

1. Add a new migration to `database/schema.go`
2. The service will automatically apply it on startup

### Building for Production
```bash
go build -o auth-service main.go
``` 