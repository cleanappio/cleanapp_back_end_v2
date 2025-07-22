# Montenegro Areas Microservice

A Go microservice for handling Montenegro area data with bearer token authentication.

## Features

- Bearer token authentication via auth-service
- Health check endpoint (`/health`)
- GeoJSON data loading from OSMB file
- Areas querying by administrative level
- Available admin levels endpoint
- Reports data endpoints
- WebSocket support for real-time updates
- JSON API responses
- Configurable port via environment variable

## Authentication

All endpoints except `/health` require a valid Bearer token in the Authorization header. The service validates tokens by calling the auth-service.

**Example:**
```bash
curl -H "Authorization: Bearer your-jwt-token" http://localhost:8080/areas?admin_level=8
```

## Running Locally

### Prerequisites

- Go 1.21 or later
- Access to auth-service for token validation

### Setup

1. Navigate to the custom-area-dashboard directory:
   ```bash
   cd custom-area-dashboard
   ```

2. Download dependencies:
   ```bash
   go mod tidy
   ```

3. Set up environment variables (see Environment Variables section below)

4. Run the service:
   ```bash
   make run-dev
   ```
   
   Or run directly with environment variables:
   ```bash
   go run main.go
   ```

The service will start on port 8080 by default.

### Environment Variables

The service uses environment variables for configuration. Create a `.env` file or set environment variables directly:

**Required Environment Variables:**

- `AUTH_SERVICE_URL`: URL of the auth-service for token validation (default: http://auth-service:8080)

**Optional Environment Variables:**

- `PORT`: Port to run the service on (default: 8080)
- `HOST`: Host to bind the service to (default: 0.0.0.0)
- `DB_HOST`: Database host (default: localhost)
- `DB_PORT`: Database port (default: 3306)
- `DB_USER`: Database user (default: root)
- `DB_PASSWORD`: Database password (default: password)

**Example .env file:**
```bash
# Server Configuration
PORT=8080
HOST=0.0.0.0

# Database Configuration
DB_HOST=localhost
DB_PORT=3306
DB_USER=root
DB_PASSWORD=password

# Auth Service Configuration
AUTH_SERVICE_URL=http://auth-service:8080
```

## API Endpoints

### Public Endpoints

#### GET /health

Returns the health status of the service. **No authentication required.**

**Response:**
```json
{
  "status": "healthy",
  "message": "Montenegro Areas service is running"
}
```

### Protected Endpoints

All endpoints below require a valid Bearer token in the Authorization header.

#### GET /areas?admin_level={level}

Returns all areas for a given administrative level.

**Headers:**
- `Authorization: Bearer <jwt-token>` (required)

**Parameters:**
- `admin_level` (required): The administrative level to query (integer)

**Response:**
```json
{
  "admin_level": 8,
  "count": 25,
  "areas": [
    {
      "admin_level": 8,
      "area": {
        "type": "Polygon",
        "coordinates": [[[18.8975984, 42.2580593], ...]]
      },
      "name": "Đenjaši Česminovo",
      "osm_id": -18945986
    }
  ]
}
```

#### GET /admin-levels

Returns all available administrative levels in the dataset.

**Headers:**
- `Authorization: Bearer <jwt-token>` (required)

**Response:**
```json
{
  "admin_levels": [2, 4, 6, 8, 10],
  "count": 5
}
```

#### GET /reports?osm_id={id}&n={number}

Returns the last N reports with analysis within a specific Montenegro area.

**Headers:**
- `Authorization: Bearer <jwt-token>` (required)

**Parameters:**
- `osm_id` (required): The OSM ID of the area (integer)
- `n` (required): Number of reports to return (integer, 1-100)

**Response:**
```json
{
  "reports": [
    {
      "report": {
        "seq": 123,
        "timestamp": "2024-01-01T12:00:00Z",
        "id": "user123",
        "team": 1,
        "latitude": 42.2580593,
        "longitude": 18.8975984,
        "x": 0.5,
        "y": 0.3,
        "action_id": "action123"
      },
      "analysis": [
        {
          "seq": 123,
          "source": "openai",
          "analysis_text": "This image shows litter in a public area...",
          "title": "Litter Analysis",
          "description": "Multiple pieces of litter detected",
          "litter_probability": 0.85,
          "hazard_probability": 0.15,
          "severity_level": 0.7,
          "summary": "Moderate litter situation",
          "language": "en",
          "created_at": "2024-01-01T12:00:00Z"
        },
        {
          "seq": 123,
          "source": "openai",
          "analysis_text": "Esta imagen muestra basura en un área pública...",
          "title": "Análisis de Basura",
          "description": "Múltiples piezas de basura detectadas",
          "litter_probability": 0.85,
          "hazard_probability": 0.15,
          "severity_level": 0.7,
          "summary": "Situación moderada de basura",
          "language": "es",
          "created_at": "2024-01-01T12:00:00Z"
        }
      ]
    }
  ],
  "count": 1
}
```

**Response Fields:**
- `report`: The report data including location and metadata
- `analysis`: Array of analysis results in different languages
  - `seq`: Report sequence number
  - `source`: Analysis source (e.g., "openai")
  - `analysis_text`: Detailed analysis text
  - `title`: Analysis title
  - `description`: Analysis description
  - `litter_probability`: Probability of litter (0.0-1.0)
  - `hazard_probability`: Probability of hazard (0.0-1.0)
  - `severity_level`: Severity level (0.0-1.0)
  - `summary`: Analysis summary
  - `language`: Language code (e.g., "en", "es", "fr")
  - `created_at`: Analysis creation timestamp

#### GET /reports_aggr

Returns aggregated reports data for all areas of AdminLevel 6.

**Headers:**
- `Authorization: Bearer <jwt-token>` (required)

**Response:**
```json
{
  "areas": [
    {
      "osm_id": -18945986,
      "name": "Đenjaši Česminovo",
      "reports_count": 15,
      "reports_mean": 12.5,
      "reports_max": 25,
      "mean_severity": 0.7,
      "mean_litter_probability": 0.8,
      "mean_hazard_probability": 0.2
    }
  ],
  "count": 1
}
```

**Response Fields:**
- `osm_id`: The OSM ID of the area
- `name`: The name of the area
- `reports_count`: Number of reports in this area
- `reports_mean`: Mean reports count across all AdminLevel 6 areas
- `reports_max`: Maximum reports count across all AdminLevel 6 areas
- `mean_severity`: Mean severity level (0.0-1.0) for all reports in this area
- `mean_litter_probability`: Mean litter probability (0.0-1.0) for all reports in this area
- `mean_hazard_probability`: Mean hazard probability (0.0-1.0) for all reports in this area

### WebSocket Endpoints

#### GET /ws/montenegro-reports

WebSocket endpoint for real-time Montenegro reports updates.

**Headers:**
- `Authorization: Bearer <jwt-token>` (required)

#### GET /ws/health

WebSocket health check endpoint.

**Headers:**
- `Authorization: Bearer <jwt-token>` (required)

## Error Responses

### Authentication Errors

**401 Unauthorized:**
```json
{
  "error": "missing authorization header"
}
```

```json
{
  "error": "invalid authorization format"
}
```

```json
{
  "error": "invalid or expired token"
}
```

## Docker

### Build the image:
```bash
docker build -t custom-area-dashboard .
```

### Run the container:
```bash
docker run -p 8080:8080 \
  -e AUTH_SERVICE_URL=http://auth-service:8080 \
  custom-area-dashboard
```

## Development

This service integrates with the auth-service for token validation and provides protected access to Montenegro area data and reports. 