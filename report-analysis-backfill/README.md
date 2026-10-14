# Report Analysis Backfill Service

This microservice continuously polls the database for unanalyzed reports and sends them to the report analysis API for processing.

## Features

- Fetches unanalyzed reports from the `reports` table
- **Image compression**: Automatically compresses JPEG images to max height of 1000px before sending
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
- `DB_PASSWORD`: Database password (required outside local dev)
- `DB_NAME`: Database name (default: cleanapp)

### Analysis API Configuration
- `REPORT_ANALYSIS_URL`: URL of the report analysis API (required)

### Polling Configuration
- `POLL_INTERVAL`: Duration between polling cycles (default: 1m)
- `BATCH_SIZE`: Number of reports to process per batch (default: 20)
- `SEQ_END_TO`: Upper sequence bound for backfill processing (default: 0, meaning include all)

### Logging
- `LOG_LEVEL`: Log level (default: info)

## API Endpoints

- `GET /health`: Root health check endpoint
- `GET /version`: Build and git provenance
- `GET /api/v1/health`: Legacy API health check endpoint
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
SEQ_END_TO=0
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
2. Every `POLL_INTERVAL` (default: 1 minute), it queries for unanalyzed reports **without images**
3. It fetches up to `BATCH_SIZE` reports (default: 20) that haven't been analyzed
4. For each report, the service:
   - Fetches the image separately by sequence number
   - **Compresses the image** to max height of 1000px while preserving aspect ratio
   - Sends the complete report (with compressed image) to the analysis API
5. Each report is sent to the analysis API at `REPORT_ANALYSIS_URL/api/v3/analysis`
6. The process repeats until the service is stopped

## Architecture Benefits

- **Efficient memory usage**: Images are only loaded when needed for sending
- **Faster report fetching**: Initial query excludes large image data
- **On-demand image processing**: Images are fetched and compressed just before sending
- **Better error handling**: If image fetch fails, the report is still sent without image

## Image Compression

The service automatically compresses JPEG images before sending them to the analysis API:

- **Max height**: 1000 pixels (aspect ratio preserved)
- **Quality**: 85% JPEG quality
- **EXIF handling**: Corrects image orientation based on EXIF data
- **Fallback**: If compression fails, the original image is sent
- **Logging**: Compression details are logged for monitoring

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

- Check service health: `curl http://localhost:8080/health`
- Check service version: `curl http://localhost:8080/version`
- Check service status: `curl http://localhost:8080/api/v1/status`

The status endpoint returns:
- `running`: Whether the service is currently running
- `poll_interval`: Current polling interval
- `batch_size`: Current batch size
- `last_processed_seq`: Last processed sequence number
- `seq_end_to`: Configured upper sequence bound

## Evidence-only backfill

The standalone command reuses the production stored-original resolver and narrow
Gemini/OpenAI classifier. Run the explicit report-analyze-pipeline or
report-listener migrations before using it. Configure the existing `DB_*`
environment variables plus `ANALYZER_LLM_PROVIDER` and the corresponding API key
and model. It does not contact the full analysis service or publish routing events.

A bounded dry run is the default and must be reviewed before applying a broader
cohort:

```bash
go run ./cmd/evidence-backfill --dry-run --brand grok --limit 50
go run ./cmd/evidence-backfill --dry-run --brands claude,claudecode,anthropic,grok,openai,chatgpt --limit 200
```

Each JSON output line identifies the report (`seq`, `brand`, and its existing
`title`), proposed evidence fields, `source_type`, `original_text_available`, and
`original_media_available`. The title comes deterministically from an existing
English analysis when available, otherwise an existing translated title; each
report appears once. Media availability requires a valid original image and
rejects placeholder blobs. Full raw original bodies, media bytes, source IDs, and
contact metadata are not printed. When intake privacy flags require redaction,
the review output withholds titles and extracted text while preserving source
availability flags. The
final line contains `examined`, `written`, `failed`, `last_seq`, and `label_counts`.
These are label counts, **not classifier precision**. Evaluate precision against
manually reviewed originals, including journalism quoting first-person accounts,
third-party integration issues, incomplete originals, and original photo reports.
Dry runs call the configured model but perform no database writes.

Writing requires explicit `--apply`; raw input and existing analysis content
remain unchanged. All language rows receive the same original evidence fields:

```bash
go run ./cmd/evidence-backfill --apply --brands claude,anthropic,grok,openai --limit 200
```

`--after-seq` resumes after the returned sequence; `--force` reclassifies a reviewed
cohort with existing evidence labels. `--limit` is required to remain between 1
and 10,000 (default 50). `--interval` defaults to three seconds and cannot be less
than 250 milliseconds. Missing stored originals stay `unknown`; they never use a
generated summary as an original. Classification failures are reported and not
written. The container also includes `/root/evidence-backfill` for this command.

The shared evidence provider also enforces a process-wide request budget:
`EVIDENCE_REQUESTS_PER_MINUTE` defaults to 20 and accepts 1–120. The CLI interval
cannot bypass that provider budget. A backfill process has its own budget, so
allocate combined quota deliberately if running beside the production worker.

Candidates include every analysis language, and translations are grouped by report
sequence so one source produces one classifier call and one label count.
