# GDPR Process Service

A microservice that polls the `users` and `reports` tables for new rows and processes them for GDPR compliance.

## Overview

This service continuously monitors the database for unprocessed users and reports, applies GDPR processing logic (currently placeholder), and tracks which records have been processed.

## Features

- **Continuous Polling**: Automatically checks for new users and reports at configurable intervals
- **GDPR Processing**: Placeholder functions for GDPR compliance processing
- **Progress Tracking**: Maintains tables to track processed records
- **Parallel Processing**: Processes users in concurrent batches of 10 for optimal performance
- **Batch Processing**: Processes records in batches for efficiency
- **Error Handling**: Continues processing even if individual records fail

## Database Tables

### Input Tables
- `users`: Source table containing user data
- `reports`: Source table containing report data

### Tracking Tables
- `users_gdpr`: Tracks processed users with unique ID index
- `reports_gdpr`: Tracks processed reports with unique sequence index

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | Database host |
| `DB_PORT` | `3306` | Database port |
| `DB_USER` | `server` | Database username |
| `DB_PASSWORD` | `secret` | Database password |
| `DB_NAME` | `cleanapp` | Database name |
| `POLL_INTERVAL` | `60` | Polling interval in seconds |
| `OPENAI_API_KEY` | `` | OpenAI API key for PII detection |
| `OPENAI_MODEL` | `gpt-4o` | OpenAI model to use for analysis |
| `BATCH_SIZE` | `10` | Number of users to process in each batch |
| `MAX_WORKERS` | `10` | Maximum number of concurrent OpenAI API calls |

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Main Loop     │───▶│  Database Query  │───▶│  Process Users  │
│   (Polling)     │    │  (Unprocessed)   │    │   (Placeholder) │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Sleep Timer   │    │  Process Reports │    │ Mark Processed  │
│   (Configurable)│    │   (Placeholder)  │    │   (Tracking)    │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

## Processing Logic

### Current Implementation
- **Users**: Fetches avatar field, processes it through OpenAI API for PII detection and obfuscation in parallel batches of 10, generates unique avatar names to prevent conflicts, and updates the database with obfuscated values when changes are detected
- **Reports**: Placeholder function that logs processing (to be implemented)

### Future Implementation
The placeholder functions can be extended to include:
- **User Processing**: Currently implements avatar PII detection and obfuscation via OpenAI API in parallel batches, with automatic database updates and unique avatar name generation to prevent conflicts
- Data anonymization
- Consent management
- Data retention policy enforcement
- Data export functionality
- Data deletion requests
- Location data handling
- Image data processing

## Running the Service

### Local Development
```bash
cd gdpr-process-service
go mod tidy
go run main.go
```

### Docker
```bash
# Build the image
./build_image.sh

# Run with docker-compose
docker-compose up -d
```

### Environment Setup
```bash
export DB_HOST=your_db_host
export DB_PORT=3306
export DB_USER=your_db_user
export DB_PASSWORD=your_db_password
export DB_NAME=cleanapp
export POLL_INTERVAL=60
```

## Monitoring

The service logs:
- Startup information
- Processing statistics
- Individual record processing status
- Error messages for failed processing

## Dependencies

- Go 1.24+
- MySQL database
- `github.com/apex/log` for logging
- `github.com/go-sql-driver/mysql` for database connectivity

## Security Considerations

- Database credentials should be provided via environment variables
- Service runs with minimal required database permissions
- No external HTTP endpoints (internal service only)

## Scaling

- Multiple instances can run simultaneously
- Each instance will process different records due to database locking
- Consider adjusting batch sizes and polling intervals for production loads
