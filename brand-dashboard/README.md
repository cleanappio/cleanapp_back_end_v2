# Brand Dashboard Service

A microservice for monitoring and analyzing reports related to specific brands. This service provides a dashboard view that displays reports whose analysis recognizes brand names configured for the dashboard.

## Features

- **Brand-based Report Filtering**: Displays reports that match configured brand names
- **Soft Brand Matching**: Handles variations like "coca cola" → "coca-cola", "Red Bull" → "redbull"
- **Real-time WebSocket Updates**: Live updates for new brand-related reports
- **Authentication Integration**: Bearer token authentication via auth-service
- **RESTful API**: Clean API endpoints for brand and report data
- **Multi-language Support**: Handles reports with analyses in different languages

## Configuration

### Environment Variables

Create a `.env` file or set environment variables:

```env
# Database Configuration
DB_USER=server
DB_PASSWORD=secret_app
DB_HOST=localhost
DB_PORT=3306
DB_NAME=cleanapp

# Server Configuration
PORT=8080
HOST=0.0.0.0

# Auth Service
AUTH_SERVICE_URL=http://auth-service:8080

# Brand Dashboard Configuration
BRAND_NAMES=coca-cola,redbull,nike,adidas,pepsi,mcdonalds,starbucks,apple,samsung,microsoft
```

### Brand Names Configuration

The `BRAND_NAMES` environment variable accepts a comma-separated list of brand names. The service will:

1. **Normalize brand names** for matching (lowercase, remove punctuation)
2. **Perform soft matching** to handle variations
3. **Support common abbreviations** and alternative spellings

#### Example Brand Matching

| Input | Matches | Display Name |
|-------|---------|--------------|
| "coca cola" | "coca-cola" | "Coca-Cola" |
| "Red Bull" | "redbull" | "Red Bull" |
| "NIKE" | "nike" | "Nike" |
| "adidas shoes" | "adidas" | "Adidas" |

## API Endpoints

### Authentication
All protected endpoints require a valid Bearer token in the Authorization header.

### Public Endpoints

#### Health Check
```
GET /health
```

**Response:**
```json
{
  "status": "healthy",
  "message": "Brand Dashboard service is running",
  "service": "brand-dashboard"
}
```

### Protected Endpoints

#### Get Available Brands
```
GET /brands
Authorization: Bearer <token>
```

**Response:**
```json
{
  "brands": [
    {
      "name": "coca-cola",
      "display_name": "Coca-Cola",
      "count": 42
    },
    {
      "name": "redbull",
      "display_name": "Red Bull",
      "count": 15
    }
  ],
  "count": 2
}
```

#### Get Reports by Brand
```
GET /reports?brand=coca-cola&n=10
Authorization: Bearer <token>
```

**Parameters:**
- `brand` (required): Brand name to filter by
- `n` (required): Number of reports to return

**Response:**
```json
{
  "reports": [
    {
      "report": {
        "seq": 12345,
        "timestamp": "2024-01-15T10:30:00Z",
        "id": "report_12345",
        "team": 1,
        "latitude": 42.123456,
        "longitude": -71.123456,
        "x": 123.45,
        "y": 67.89,
        "action_id": "action_123"
      },
      "analysis": [
        {
          "seq": 12345,
          "source": "openai",
          "analysis_text": "Found Coca-Cola bottle...",
          "title": "Coca-Cola Bottle Found",
          "description": "A red Coca-Cola bottle was found...",
          "brand_name": "coca-cola",
          "litter_probability": 0.95,
          "hazard_probability": 0.1,
          "severity_level": 0.8,
          "summary": "Coca-Cola bottle litter",
          "language": "en",
          "created_at": "2024-01-15T10:30:00Z"
        }
      ]
    }
  ],
  "count": 1,
  "brand": "coca-cola"
}
```

#### WebSocket Health Check
```
GET /ws/health
Authorization: Bearer <token>
```

**Response:**
```json
{
  "status": "healthy",
  "message": "Brand Dashboard WebSocket service is running",
  "service": "brand-dashboard-websocket",
  "connected_clients": 5,
  "last_broadcast_seq": 42
}
```

### WebSocket Endpoints

#### Connect to Brand Reports Stream
```
GET /ws/brand-reports
Authorization: Bearer <token>
```

**WebSocket Messages:**

**Report Update:**
```json
{
  "type": "brand_report",
  "data": {
    "reports": [...],
    "count": 1,
    "from_seq": 12340,
    "to_seq": 12345,
    "brand": "coca-cola"
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

**Connection Status:**
```json
{
  "type": "connection_status",
  "data": {
    "status": "connected",
    "user_id": "user_123",
    "connected_clients": 5
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

## Brand Matching Logic

The service implements sophisticated brand matching to handle various input formats:

### Normalization Process
1. Convert to lowercase
2. Remove punctuation (`-`, `_`, `.`, `,`, `&`)
3. Remove common words (`and`)
4. Remove extra spaces

### Soft Matching Examples

| Input | Normalized | Matches Brand |
|-------|------------|---------------|
| "Coca-Cola" | "cocacola" | "coca-cola" |
| "Red Bull Energy" | "redbullenergy" | "redbull" |
| "Nike Shoes" | "nikeshoes" | "nike" |
| "McDonald's" | "mcdonalds" | "mcdonalds" |

### Common Variations Supported

- **Coca-Cola**: "coca", "cola", "coke"
- **Red Bull**: "red", "bull", "redbullenergy"
- **Nike**: "nikeshoes", "nikeinc"
- **Adidas**: "adidasshoes", "adidasgroup"
- **Pepsi**: "pepsico", "pepsicola"
- **McDonald's**: "mcd", "mcdonaldsrestaurant"

## Database Schema

The service connects to the existing CleanApp database and queries:

- `reports` table: Report metadata and location
- `report_analysis` table: Analysis results including brand names

### Key Fields Used

**Reports Table:**
- `seq`: Report sequence number
- `ts`: Timestamp
- `id`: Report ID
- `team`: Team number
- `latitude`, `longitude`: GPS coordinates
- `x`, `y`: Additional coordinates
- `action_id`: Action identifier

**Report Analysis Table:**
- `seq`: Report sequence number (foreign key)
- `brand_name`: Detected brand name
- `analysis_text`: Full analysis text
- `title`, `description`: Analysis summary
- `litter_probability`, `hazard_probability`: AI probabilities
- `severity_level`: Calculated severity
- `language`: Analysis language
- `created_at`: Analysis timestamp

## Usage Examples

### Frontend Integration

```javascript
// Connect to WebSocket for real-time updates
const ws = new WebSocket('ws://localhost:8080/ws/brand-reports');
ws.onmessage = function(event) {
  const message = JSON.parse(event.data);
  if (message.type === 'brand_report') {
    // Update map with new reports
    updateMapWithReports(message.data.reports);
  }
};

// Fetch available brands
fetch('/brands', {
  headers: {
    'Authorization': 'Bearer ' + token
  }
})
.then(response => response.json())
.then(data => {
  // Display brand list with counts
  displayBrands(data.brands);
});

// Fetch reports for specific brand
fetch('/reports?brand=coca-cola&n=20', {
  headers: {
    'Authorization': 'Bearer ' + token
  }
})
.then(response => response.json())
.then(data => {
  // Display reports on map
  displayReportsOnMap(data.reports);
});
```

### Docker Deployment

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o brand-dashboard .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/brand-dashboard .
EXPOSE 8080
CMD ["./brand-dashboard"]
```

### Docker Compose

```yaml
version: '3.8'
services:
  brand-dashboard:
    build: .
    ports:
      - "8080:8080"
    environment:
      - DB_USER=server
      - DB_PASSWORD=secret_app
      - DB_HOST=mysql
      - DB_PORT=3306
      - DB_NAME=cleanapp
      - AUTH_SERVICE_URL=http://auth-service:8080
      - BRAND_NAMES=coca-cola,redbull,nike,adidas
    depends_on:
      - mysql
      - auth-service
```

## Development

### Prerequisites
- Go 1.21+
- MySQL 8.0+
- Access to CleanApp database

### Local Development

1. **Clone and setup:**
   ```bash
   cd brand-dashboard
   go mod download
   ```

2. **Configure environment:**
   ```bash
   cp .env.example .env
   # Edit .env with your configuration
   ```

3. **Run the service:**
   ```bash
   go run main.go
   ```

4. **Test endpoints:**
   ```bash
   # Health check
   curl http://localhost:8080/health
   
   # Get brands (requires auth token)
   curl -H "Authorization: Bearer <token>" http://localhost:8080/brands
   ```

### Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...
```

## Monitoring and Logging

The service provides comprehensive logging:

- **INFO**: Service startup, configuration, client connections
- **DEBUG**: WebSocket messages, database queries
- **WARNING**: Authentication failures, invalid requests
- **ERROR**: Database errors, service failures

### Health Monitoring

Monitor the `/health` endpoint for service status and the `/ws/health` endpoint for WebSocket service status.

## Security Considerations

1. **Authentication**: All protected endpoints require valid Bearer tokens
2. **CORS**: Configured to allow cross-origin requests
3. **Input Validation**: All user inputs are validated and sanitized
4. **Database Security**: Uses parameterized queries to prevent SQL injection
5. **WebSocket Security**: Validates connections and implements rate limiting

## Performance Considerations

1. **Database Connection Pool**: Configured with optimal connection settings
2. **WebSocket Efficiency**: Implements efficient message broadcasting
3. **Query Optimization**: Uses indexed queries for report retrieval
4. **Memory Management**: Proper cleanup of WebSocket connections

## Future Enhancements

- [ ] Brand analytics and trends
- [ ] Geographic clustering of brand reports
- [ ] Brand comparison features
- [ ] Advanced filtering options
- [ ] Export functionality
- [ ] Brand-specific alerts
- [ ] Integration with external brand databases
- [ ] Machine learning for improved brand detection 