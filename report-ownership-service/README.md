# Report Ownership Service

A microservice that determines ownership of every report by analyzing location and brand information, then stores the results in a MySQL table.

## Overview

This service continuously polls for new reports and determines their ownership based on:
- **Location-based ownership**: Reports located within user-owned areas
- **Brand-based ownership**: Reports associated with user-owned brands

One report can have multiple owners, and ownership information is stored in the `reports_owners` table.

## Features

- **Continuous Processing**: Polls for new reports at configurable intervals
- **Batch Processing**: Processes reports in configurable batch sizes
- **Spatial Queries**: Uses MySQL spatial functions for efficient geographic lookups
- **Brand Normalization**: Normalizes brand names for consistent comparison
- **Duplicate Prevention**: Uses INSERT IGNORE to prevent duplicate ownership entries
- **Status Monitoring**: HTTP endpoints for health checks and service status

## Database Schema

### New Table: `reports_owners`
```sql
CREATE TABLE reports_owners (
    seq INT NOT NULL,
    owner VARCHAR(256) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (seq, owner),
    INDEX idx_seq (seq)
);
```

### Dependencies on Existing Tables
- `reports` - Report metadata and location
- `report_analysis` - Brand information for reports
- `area_index` - Spatial index for areas
- `customer_areas` - User area ownership
- `customer_brands` - User brand ownership

## Configuration

Environment variables:
- `DB_HOST` - Database host (default: localhost)
- `DB_PORT` - Database port (default: 3306)
- `DB_USER` - Database username (default: server)
- `DB_PASSWORD` - Database password (default: secret_app)
- `DB_NAME` - Database name (default: cleanapp)
- `POLL_INTERVAL` - Polling interval (default: 30s)
- `BATCH_SIZE` - Batch size for processing (default: 100)
- `LOG_LEVEL` - Logging level (default: info)

## API Endpoints

### GET /health
Health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "service": "report-ownership-service",
  "time": "2024-01-01T00:00:00Z"
}
```

### GET /status
Service status and statistics.

**Response:**
```json
{
  "status": "running",
  "last_processed_seq": 12345,
  "total_reports": 50000,
  "last_update": "2024-01-01T00:00:00Z"
}
```

## How It Works

### 1. **Polling Loop**
- Service polls for unprocessed reports every `POLL_INTERVAL`
- Queries for reports not yet in `reports_owners` table

### 2. **Ownership Determination**
For each report:
- **Location Analysis**: Uses spatial queries to find areas containing the report coordinates
- **Brand Analysis**: Normalizes brand names and finds associated customers
- **Owner Combination**: Combines location and brand owners, removing duplicates

### 3. **Storage**
- Stores ownership information in `reports_owners` table
- Uses `INSERT IGNORE` to prevent duplicate entries
- Each owner gets a separate row for the same report

### 4. **Spatial Queries**
```sql
SELECT DISTINCT ca.customer_id 
FROM customer_areas ca
JOIN areas a ON ca.area_id = a.id
JOIN area_index ai ON a.id = ai.area_id
WHERE ST_Contains(ai.geom, ST_GeomFromText(CONCAT('POINT(', ?, ' ', ?, ')'), 4326))
```

### 5. **Brand Normalization**
- Converts to lowercase
- Removes punctuation and common words
- Ensures consistent brand matching

## Running the Service

### Local Development
```bash
go mod tidy
go run main.go
```

### Docker
```bash
# Build the image
./build_image.sh

# Run with docker-compose
docker-compose up

# Run in background
docker-compose up -d
```

### Build Image
```bash
./build_image.sh
```

## Architecture

The service follows a clean architecture pattern:
- **Models**: Data structures for reports and ownership
- **Database**: Data access and ownership determination logic
- **Service**: Main business logic and processing loop
- **Main**: Application entry point and HTTP server setup

## Dependencies

- Go 1.23+
- MySQL 8.0+ (for spatial functions)
- MySQL driver for Go

## Monitoring

The service provides several monitoring points:
- **Health checks** via `/health` endpoint
- **Status information** via `/status` endpoint
- **Detailed logging** for debugging and monitoring
- **Batch processing metrics** in logs

## Performance Considerations

- **Batch processing** to handle large numbers of reports efficiently
- **Spatial indexing** for fast geographic queries
- **Connection pooling** for database connections
- **Configurable intervals** to balance processing speed and resource usage

## Error Handling

- **Graceful degradation** when individual reports fail to process
- **Detailed error logging** for debugging
- **Context timeouts** to prevent hanging operations
- **Database connection resilience** with proper connection management
