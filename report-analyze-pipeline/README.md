# Report Analyze Pipeline

A microservice that analyzes reports from the CleanApp database using OpenAI's vision API to detect litter and hazards in images, with automatic translation to multiple languages.

## Features

- Monitors the `reports` table for new reports
- Analyzes images using OpenAI GPT-4 Vision API
- **Automatically translates analysis results to multiple languages**
- Stores analysis results in the `report_analysis` table with language-specific records
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
    title VARCHAR(500),
    description TEXT,
    litter_probability FLOAT,
    hazard_probability FLOAT,
    severity_level FLOAT,
    summary TEXT,
    language VARCHAR(2) NOT NULL DEFAULT 'en',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX seq_index (seq),
    INDEX source_index (source),
    INDEX idx_report_analysis_language (language)
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
- `TRANSLATION_LANGUAGES` - Comma-separated list of language codes to translate to (default: "en,me")
- `LOG_LEVEL` - Logging level (default: info)

### Translation Languages

The `TRANSLATION_LANGUAGES` environment variable accepts 2-letter language codes that are automatically converted to full language names:

- `en` → English
- `me` → Montenegrin  
- `es` → Spanish
- `fr` → French
- `de` → German
- `it` → Italian
- `pt` → Portuguese
- `ru` → Russian
- `zh` → Chinese
- `ja` → Japanese
- `ko` → Korean
- `ar` → Arabic
- `hi` → Hindi
- `tr` → Turkish
- `nl` → Dutch
- `pl` → Polish
- `sv` → Swedish
- `da` → Danish
- `no` → Norwegian
- `fi` → Finnish

Example: `TRANSLATION_LANGUAGES=en,me,es,fr` will create analysis records in English, Montenegrin, Spanish, and French.

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
export TRANSLATION_LANGUAGES=en,me,es,fr
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
3. Sends images to OpenAI with a configurable prompt for initial analysis in English
4. **Translates the analysis results to all configured languages (except English)**
5. **Stores separate analysis records for each language in the database**
6. Continues monitoring for new reports

### Multi-Language Analysis

For each report, the service creates multiple analysis records:

1. **English Analysis**: Initial analysis performed in English
2. **Translated Analyses**: One record per configured language (except English)

Each record contains:
- The same structured data (title, description, probabilities, severity)
- Translated content appropriate for the target language
- Language field indicating the analysis language
- Same report sequence number (seq) for easy querying

Example: If `TRANSLATION_LANGUAGES=en,me,es` is configured, a single report will generate 3 analysis records:
- One in English (language: "en")
- One in Montenegrin (language: "me") 
- One in Spanish (language: "es")

## Error Handling

- Failed analyses are logged but don't stop the service
- **Translation failures for individual languages are logged but don't prevent other translations**
- Database connection issues are handled gracefully
- OpenAI API errors are logged with retry logic
- The service continues running even if individual reports fail 