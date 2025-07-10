# Email Service Microservice

This microservice handles sending emails for CleanApp reports. It polls the database for new reports and sends emails to area contacts when reports are submitted within their areas.

## Features

- Polls the `reports` table for reports that have been analyzed
- Only processes reports that exist in the `report_analysis` table
- Finds areas that contain each report's location
- Sends emails to area contacts who have consented to receive reports
- Includes AI analysis data (title, description, probabilities, severity) in emails
- Tracks processed reports in `sent_reports_emails` table
- Automatically creates required tables and indexes on startup
- Handles cases where no areas are found for a report

## Architecture

The service follows the same logic as the original `sendAffectedPolygonsEmails()` function:

1. **Polling**: Continuously polls for unprocessed reports
2. **Spatial Query**: Uses MySQL spatial functions to find areas containing report points
3. **Email Lookup**: Finds email addresses for areas with consent
4. **Email Sending**: Sends emails with report image and map via SendGrid
5. **Tracking**: Marks reports as processed to avoid duplicate emails

## Database Schema

### sent_reports_emails table
The service automatically creates this table on startup if it doesn't exist:

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
- `MYSQL_PASSWORD`: MySQL password (default: secret)
- `MYSQL_DB`: MySQL database (default: cleanapp)

### SendGrid
- `SENDGRID_API_KEY`: SendGrid API key (required)
- `SENDGRID_FROM_NAME`: From name (default: CleanApp)
- `SENDGRID_FROM_EMAIL`: From email (default: info@cleanapp.io)

### Service
- `--poll_interval`: How often to poll for new reports (default: 30s)

## Running the Service

### Using Docker Compose
```bash
# Set your SendGrid API key
export SENDGRID_API_KEY=your_api_key_here

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
- SendGrid account and API key 