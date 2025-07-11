# Montenegro Areas Microservice

A Go microservice for handling Montenegro area data.

## Features

- Health check endpoint (`/health`)
- GeoJSON data loading from OSMB file
- Areas querying by administrative level
- Available admin levels endpoint
- JSON API responses
- Configurable port via environment variable

## Running Locally

### Prerequisites

- Go 1.21 or later

### Setup

1. Navigate to the montenegro-areas directory:
   ```bash
   cd montenegro-areas
   ```

2. Download dependencies:
   ```bash
   go mod tidy
   ```

3. Run the service:
   ```bash
   make run-dev
   ```
   
   Or run directly with environment variables:
   ```bash
   go run main.go
   ```

The service will start on port 8080 by default.

### Environment Variables

The service uses a `.env` file for configuration. Copy `.env.example` to `.env` and modify as needed:

```bash
cp .env.example .env
```

**Available Environment Variables:**

- `PORT`: Port to run the service on (default: 8080)
- `HOST`: Host to bind the service to (default: 0.0.0.0)
- `LOG_LEVEL`: Logging level (default: info)
- `LOG_FORMAT`: Log format (default: json)
- `GEOJSON_FILE`: Path to the GeoJSON file (default: OSMB-e0b412fe96a2a2c5d8e7eb33454a21d971bea620.geojson)

## API Endpoints

### GET /health

Returns the health status of the service.

**Response:**
```json
{
  "status": "healthy",
  "message": "Montenegro Areas service is running"
}
```

### GET /areas?admin_level={level}

Returns all areas for a given administrative level.

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

### GET /admin-levels

Returns all available administrative levels in the dataset.

**Response:**
```json
{
  "admin_levels": [2, 4, 6, 8, 10],
  "count": 5
}
```

### GET /reports?osm_id={id}&n={number}

Returns the last N reports within a specific Montenegro area.

**Parameters:**
- `osm_id` (required): The OSM ID of the area (integer)
- `n` (required): Number of reports to return (integer, 1-100)

**Response:**
```json
{
  "reports": [
    {
      "seq": 123,
      "timestamp": "2024-01-01T12:00:00Z",
      "id": "user123",
      "team": 1,
      "latitude": 42.2580593,
      "longitude": 18.8975984,
      "x": 0.5,
      "y": 0.3,
      "action_id": "action123"
    }
  ],
  "count": 1
}
```

### GET /reports_aggr

Returns aggregated reports data for all areas of AdminLevel 6.

**Response:**
```json
{
  "areas": [
    {
      "osm_id": -18945986,
      "name": "Đenjaši Česminovo",
      "reports_count": 15,
      "reports_median": 12.5,
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
- `reports_median`: Median reports count across all AdminLevel 6 areas
- `mean_severity`: Mean severity level (0.0-1.0) for all reports in this area
- `mean_litter_probability`: Mean litter probability (0.0-1.0) for all reports in this area
- `mean_hazard_probability`: Mean hazard probability (0.0-1.0) for all reports in this area

## Docker

### Build the image:
```bash
docker build -t montenegro-areas .
```

### Run the container:
```bash
docker run -p 8080:8080 montenegro-areas
```

## Development

This is a skeleton service that can be extended with additional endpoints for handling Montenegro area data. 