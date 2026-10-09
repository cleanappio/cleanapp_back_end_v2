# Email Service Microservice

This microservice handles sending emails for CleanApp reports. It polls the database for new reports and sends emails to area contacts when reports are submitted within their areas.

## Features

- Polls the `reports` table for reports that have been analyzed
- Only processes reports that exist in the `report_analysis` table
- Finds areas that contain each report's location
- Sends emails to area contacts who have consented to receive reports
- Includes AI analysis data (title, description, probabilities, severity) in emails
- Tracks processed reports in `sent_reports_emails` table
- Uses explicit migrations via `email-service/cmd/migrate` for schema changes
- Handles cases where no areas are found for a report
- **HTTP API for email opt-out management**
- **Health check endpoint for monitoring**

## HTTP API Endpoints

The service now includes HTTP endpoints alongside the existing polling functionality:

### Opt-Out Email
**POST** `/api/v3/optout`
- Allows users to opt out of receiving emails
- Request body: `{"email": "user@example.com"}`
- Returns success/error status with appropriate HTTP codes
- **Built with Gin framework for high performance**

### Opt-Out Link (Email Integration)
**GET** `/opt-out?email=user@example.com`
- Web-based opt-out for email links
- Accepts email parameter via query string
- Returns HTML confirmation pages
- **Integrated into all email templates**

### Health Check
**GET** `/health`
- Returns service status and timestamp
- Useful for monitoring and load balancer health checks

### Configuration
- **Port**: Configurable via `--http_port` flag (default: 8080)
- **Graceful shutdown**: Handles SIGINT/SIGTERM signals
- **Concurrent operation**: HTTP server runs alongside email polling
- **Framework**: Uses Gin for optimal performance and validation
- **HTML templates**: Professional opt-out confirmation pages

For detailed API documentation, see [OPT_OUT_API_ENDPOINT.md](OPT_OUT_API_ENDPOINT.md).

## Architecture

The service follows the same logic as the original `sendAffectedPolygonsEmails()` function:

1. **Polling**: Continuously polls for unprocessed reports
2. **Spatial Query**: Uses MySQL spatial functions to find areas containing report points
3. **Email Lookup**: Finds email addresses for areas with consent
4. **Email Sending**: Sends emails with report image and map through the configured provider
5. **Tracking**: Marks reports as processed to avoid duplicate emails

## Database Schema

### sent_reports_emails table
Create this table via `email-service/cmd/migrate` before running the service:

```sql
CREATE TABLE IF NOT EXISTS sent_reports_emails (
    seq INT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_created_at (created_at),
    INDEX idx_seq (seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Indexes:**
- `PRIMARY KEY` on `seq` - Ensures unique report tracking and fast lookups
- `idx_created_at` - Optimizes queries by creation time
- `idx_seq` - Additional index on seq for performance

### Required Tables
The service expects these tables to exist:
- `reports`: Contains report data
- `report_analysis`: Contains AI analysis results (required for email processing)
- `area_index`: Spatial index for areas
- `areas`: Area definitions with GeoJSON
- `contact_emails`: Email addresses for areas
- `sent_reports_emails`: Tracking table (created by service)

## Configuration

The service uses environment variables for configuration:

### Database
- `MYSQL_HOST`: MySQL host (default: localhost)
- `MYSQL_PORT`: MySQL port (default: 3306)
- `MYSQL_USER`: MySQL user (default: server)
- `MYSQL_PASSWORD`: MySQL password (required outside local dev)
- `MYSQL_DB`: MySQL database (default: cleanapp)

### Outgoing email
- `EMAIL_PROVIDER`: `google_workspace` selects SMTP; `sendgrid` retains the legacy provider for rollback (default: `sendgrid`).
- `EMAIL_FROM_NAME`: From name (default: `SENDGRID_FROM_NAME`, then `CleanApp`).
- `EMAIL_FROM_ADDRESS`: From address (default: `SENDGRID_FROM_EMAIL`, then `info@cleanapp.io`).
- `SMTP_HOST`: SMTP host (default: `smtp-relay.gmail.com`).
- `SMTP_PORT`: SMTP port (default: `587`).
- `SMTP_TIMEOUT`: SMTP timeout (default: `30s`).
- `SMTP_USERNAME`, `SMTP_PASSWORD`: Credentials if the Google Workspace relay requires SMTP authentication. `SMTP_PASSWORD_FILE` can supply a mounted secret instead of `SMTP_PASSWORD`.
- `SENDGRID_API_KEY`: Required only when selecting SendGrid.

For Google Workspace, configure its SMTP relay to authorize the deployment's public IP and the `info@cleanapp.io` sender, or provide credentials for an authorized account. All SMTP connections require TLS. Invalid SMTP configuration prevents service startup; a rejected or failed send remains a failure and does not fall back to SendGrid. Existing opt-outs, recipient selection, schedules, throttles, templates, and inline image references are preserved. New delivery records identify the selected provider as `google_workspace` or `sendgrid`; historical records are unchanged.

### Service
- `POLL_INTERVAL`: How often to poll for new reports (default: 10s)
- `HTTP_PORT`: HTTP server port for API endpoints (default: 8080)
- `OPT_OUT_URL`: URL for email opt-out links (defaults to `CLEANAPP_BASE_URL`/`FRONTEND_URL`-derived public opt-out path; localhost only in dev-like environments)

## Running the Service

### Using Docker Compose
```bash
# Select the Google Workspace relay authorized for this deployment
export EMAIL_PROVIDER=google_workspace
export EMAIL_FROM_ADDRESS=info@cleanapp.io
export SMTP_HOST=smtp-relay.gmail.com
export SMTP_PORT=587

# Set custom configuration (optional)
export POLL_INTERVAL=60s
export HTTP_PORT=9090
export OPT_OUT_URL="https://yourdomain.com/opt-out"

# Start the service
docker-compose up -d
```

### Using Docker
```bash
# Build the image
docker build -t email-service .

# Run the container
docker run -d \
  -e SENDGRID_API_KEY=your_api_key_here \
  -e MYSQL_HOST=your_mysql_host \
  -e MYSQL_PASSWORD=your_mysql_password \
  -e POLL_INTERVAL=60s \
  -e HTTP_PORT=9090 \
  -e OPT_OUT_URL="https://yourdomain.com/opt-out" \
  email-service
```

### Running Locally
```bash
# Install dependencies
go mod download

# Set environment variables
export SENDGRID_API_KEY=your_api_key_here
export MYSQL_HOST=localhost
export MYSQL_PASSWORD=your_password

# Run the service
go run main.go

# Run with custom configuration via environment variables
export POLL_INTERVAL=60s
export HTTP_PORT=9090
export OPT_OUT_URL="https://yourdomain.com/opt-out"
go run main.go
```

## Email Content

The service sends emails containing:
- Report image (attached as inline image)
- Map showing the report location and area boundaries
- AI analysis data including:
  - Report title and description
  - Litter probability score
  - Hazard probability score
  - Severity level assessment
  - Analysis summary
- HTML and plain text versions with styled layout
- **Automatic opt-out links** in all email templates
- **Professional footer** with unsubscribe instructions

## Error Handling

- Database connection errors are logged and the service continues
- Email sending failures are logged but don't stop processing other reports
- Invalid reports are logged and skipped
- The service is resilient to temporary failures

## Monitoring

The service logs:
- Number of unprocessed reports found
- Email sending attempts and results
- Database connection status
- Processing errors

## Dependencies

- Go 1.24+
- MySQL 8.0+ with spatial extensions
- Authorized Google Workspace SMTP relay, or a SendGrid account and API key for rollback
- **Gin framework** for high-performance HTTP API
