# Report Analyze Pipeline

A microservice that analyzes reports from the CleanApp database using OpenAI's vision API to detect litter and hazards in images.

## Features

- Monitors the `reports` table for new reports
- Analyzes images using OpenAI GPT-4 Vision API
- Stores analysis results in the `report_analysis` table
- Provides HTTP API endpoints for status and results
- Configurable analysis intervals and retry logic

## Database Schema

The service creates and uses the `report_analysis` table:

```sql
CREATE TABLE report_analysis (
    seq INT NOT NULL,
    source VARCHAR(255) NOT NULL,
    analysis_text TEXT,
    analysis_image LONGBLOB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX seq_index (seq),
    INDEX source_index (source)
);
```

## Configuration

Environment variables:

- `DB_HOST` - Database host (default: localhost)
- `DB_PORT` - Database port (default: 3306)
- `DB_USER` - Database user (default: server)
- `DB_PASSWORD` - Database password (default: secret_app)
- `DB_NAME` - Database name (default: cleanapp)
- `PORT` - HTTP server port (default: 8080)
- `OPENAI_API_KEY` - OpenAI API key (required)
- `OPENAI_MODEL` - OpenAI model to use (default: gpt-4o)
- `ANALYSIS_INTERVAL` - Interval between analysis runs (default: 30s)
- `MAX_RETRIES` - Maximum retry attempts (default: 3)
- `ANALYSIS_PROMPT` - Custom prompt for image analysis (default: "What kind of litter or hazard can you see on this image? Please describe the litter or hazard in detail. Also, give a probability that there is a litter or hazard on a photo and a severity level from 0.0 to 1.0.")
- `LOG_LEVEL` - Logging level (default: info)

## API Endpoints

- `GET /api/v1/health` - Health check
- `GET /api/v1/status` - Analysis status
- `GET /api/v1/analysis/:seq` - Get analysis for specific report
- `GET /api/v1/stats` - Analysis statistics

## Usage

### Local Development

1. Create and configure environment variables:
```bash
# Create .env template
make env-create

# Copy template to .env and edit with your values
cp .env.template .env

# Edit .env file with your actual values
# (especially OPENAI_API_KEY and DB_PASSWORD)
```

2. Run the service:
```bash
make run
```

### Alternative: Direct Environment Variables

If you prefer to set environment variables directly:

```bash
export OPENAI_API_KEY=your_openai_api_key
export DB_HOST=localhost
export DB_PASSWORD=your_db_password
make run
```

### Make Commands

- `make run` - Run the service (loads .env if present)
- `make build` - Build the binary
- `make test` - Run tests
- `make clean` - Clean build artifacts
- `make deps` - Download dependencies
- `make env-create` - Create .env.template file
- `make env-show` - Show .env.template contents
- `make docker-build` - Build Docker image
- `make docker-run` - Run with Docker

### Docker

1. Build the image:
```bash
make docker-build
```

2. Run the container:
```bash
make docker-run
```

## Analysis Process

1. The service polls the database every 30 seconds (configurable)
2. Finds reports that haven't been analyzed yet
3. Sends images to OpenAI with a configurable prompt (default: "What kind of litter or hazard can you see on this image? Please describe the litter or hazard in detail. Also, give a probability that there is a litter or hazard on a photo and a severity level from 0.0 to 1.0.")
4. Stores the analysis results in the database
5. Continues monitoring for new reports

## Error Handling

- Failed analyses are logged but don't stop the service
- Database connection issues are handled gracefully
- OpenAI API errors are logged with retry logic
- The service continues running even if individual reports fail 