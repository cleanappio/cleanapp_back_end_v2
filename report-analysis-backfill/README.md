# Report Analysis Backfill Service

This microservice continuously polls the database for unanalyzed reports and sends them to the report analysis API for processing.

## Features

- Fetches unanalyzed reports from the `reports` table
- Sends reports to the analysis API at configurable intervals
- Rate limiting: processes 20 reports per minute by default
- Configurable polling interval and batch size
- Health check and status endpoints
- Graceful shutdown handling

## Configuration

The service can be configured using environment variables:

### Database Configuration
- `DB_HOST`: Database host (default: localhost)
- `DB_PORT`: Database port (default: 3306)
- `DB_USER`: Database username (default: server)
- `DB_PASSWORD`: Database password (default: secret_app)
- `DB_NAME`: Database name (default: cleanapp)

### Analysis API Configuration
- `REPORT_ANALYSIS_URL`: URL of the report analysis API (required)

### Polling Configuration
- `POLL_INTERVAL`: Duration between polling cycles (default: 1m)
- `BATCH_SIZE`: Number of reports to process per batch (default: 20)
- `SEQ_START_FROM`: Starting sequence number for processing (default: 0)

### Logging
- `LOG_LEVEL`: Log level (default: info)

## API Endpoints

- `GET /api/v1/health`: Health check endpoint
- `GET /api/v1/status`: Service status and statistics

## Usage

### Running with Docker Compose

1. Set environment variables in a `.env` file:
```bash
DB_HOST=your-db-host
DB_PASSWORD=your-db-password
REPORT_ANALYSIS_URL=http://your-analysis-api:8080
POLL_INTERVAL=1m
BATCH_SIZE=20
SEQ_START_FROM=0
```

2. Run the service:
```bash
docker-compose up -d
```

### Running Locally

1. Install dependencies:
```bash
go mod download
```

2. Set environment variables and run:
```bash
export REPORT_ANALYSIS_URL=http://localhost:8080
export POLL_INTERVAL=1m
export BATCH_SIZE=20
go run main.go
```

### Building Docker Image

```bash
./build_image.sh
```

## How It Works

1. The service starts and connects to the database
2. Every `POLL_INTERVAL` (default: 1 minute), it queries for unanalyzed reports
3. It fetches up to `BATCH_SIZE` reports (default: 20) that haven't been analyzed
4. Each report is sent to the analysis API at `REPORT_ANALYSIS_URL/api/v3/analysis`
5. The process repeats until the service is stopped

## Database Schema

The service expects the following tables:
- `reports`: Contains the reports to be analyzed
- `report_analysis`: Contains the analysis results (used to determine which reports are already analyzed)

## Rate Limiting

The service implements rate limiting by:
- Processing only `BATCH_SIZE` reports per polling cycle
- Waiting `POLL_INTERVAL` between cycles
- Default: 20 reports per minute (20 reports every 1 minute)

## Monitoring

- Check service health: `curl http://localhost:8081/api/v1/health`
- Check service status: `curl http://localhost:8081/api/v1/status`

The status endpoint returns:
- `running`: Whether the service is currently running
- `poll_interval`: Current polling interval
- `batch_size`: Current batch size
- `last_processed_seq`: Last processed sequence number
- `start_from_seq`: Starting sequence number
