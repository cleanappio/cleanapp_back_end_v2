# Email Service Implementation Notes

## Overview

This microservice replicates the functionality of the `sendAffectedPolygonsEmails()` function from the main backend, but as a standalone service that polls the database for new reports.

## Key Implementation Details

### 1. Database Polling Strategy

The service polls the `reports` table every 30 seconds (configurable) to find reports that have been analyzed but haven't been processed for email sending. It uses an INNER JOIN with the `report_analysis` table to ensure only analyzed reports are processed, and a LEFT JOIN with the `sent_reports_emails` table to identify unprocessed reports.

```sql
SELECT r.seq, r.id, r.latitude, r.longitude, r.image, r.ts
FROM reports r
INNER JOIN report_analysis ra ON r.seq = ra.seq
LEFT JOIN sent_reports_emails sre ON r.seq = sre.seq
WHERE sre.seq IS NULL
ORDER BY r.ts ASC
LIMIT 100
```

### 2. Spatial Query Logic

The service uses the same spatial query logic as the original implementation:

1. **Point to WKT**: Converts report coordinates to WKT format
2. **Spatial Intersection**: Uses `MBRWithin` to find areas containing the report point
3. **Area Features**: Retrieves GeoJSON features for matching areas
4. **Email Lookup**: Finds email addresses for areas with consent

### 3. Email Processing Flow

For each report:
1. Fetch analysis data from `report_analysis` table
2. Find areas containing the report location
3. If no areas found → mark report as processed (no emails sent)
4. If areas found → send emails to area contacts with analysis data
5. Mark report as processed regardless of email success/failure

### 4. Error Handling

- **Database errors**: Logged but don't stop processing
- **Email failures**: Logged but continue with other recipients
- **Invalid reports**: Logged and skipped
- **Service resilience**: Continues running despite individual failures

### 5. Tracking Mechanism

The `sent_reports_emails` table tracks which reports have been processed:
- `seq`: Primary key (report sequence number)
- `created_at`: Timestamp when processing completed
- Multiple indexes for optimal performance:
  - `PRIMARY KEY` on `seq` for unique constraint and fast lookups
  - `idx_created_at` for time-based queries
  - `idx_seq` for additional performance on seq field

The table is automatically created on service startup if it doesn't exist.

## Differences from Original Implementation

### 1. Asynchronous Processing
- **Original**: Email sending happens synchronously during report save
- **Microservice**: Email sending happens asynchronously via polling

### 2. Deduplication
- **Original**: No deduplication mechanism
- **Microservice**: Uses `sent_reports_emails` table to prevent duplicate emails

### 3. Scalability
- **Original**: Tied to main application
- **Microservice**: Can be scaled independently

### 4. Fault Tolerance
- **Original**: Email failures could affect report saving
- **Microservice**: Email failures don't affect main application

## Configuration

### Environment Variables
```bash
# Database
MYSQL_HOST=localhost
MYSQL_PORT=3306
MYSQL_USER=server
MYSQL_PASSWORD=secret
MYSQL_DB=cleanapp

# SendGrid
SENDGRID_API_KEY=your_api_key_here
SENDGRID_FROM_NAME=CleanApp
SENDGRID_FROM_EMAIL=info@cleanapp.io

# Service
--poll_interval=30s
```

## Database Schema

### Required Tables
The service expects these tables to exist:
- `reports`: Contains report data
- `report_analysis`: Contains AI analysis results (required for email processing)
- `area_index`: Spatial index for areas
- `areas`: Area definitions with GeoJSON
- `contact_emails`: Email addresses for areas
- `sent_reports_emails`: Tracking table (created by service)

### Schema Creation
The service automatically creates the table on startup:

```sql
CREATE TABLE IF NOT EXISTS sent_reports_emails (
    seq INT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_created_at (created_at),
    INDEX idx_seq (seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

## Deployment

### Docker Compose (Recommended)
```bash
export SENDGRID_API_KEY=your_api_key_here
./deploy.sh
```

### Manual Deployment
```bash
# Build
docker build -t email-service .

# Run
docker run -d \
  -e SENDGRID_API_KEY=your_api_key_here \
  -e MYSQL_HOST=your_host \
  -e MYSQL_PASSWORD=your_password \
  email-service
```

## Monitoring

### Logs
The service logs:
- Number of unprocessed reports found
- Email sending attempts and results
- Database connection status
- Processing errors

### Health Checks
- Database connectivity
- SendGrid API connectivity
- Processing loop status

## Performance Considerations

### Batch Processing
- Processes up to 100 reports per polling cycle
- Configurable polling interval
- Non-blocking email sending

### Database Optimization
- Uses indexes on `sent_reports_emails.created_at`
- Limits query results to prevent memory issues
- Uses prepared statements for efficiency

### Email Rate Limiting
- Respects SendGrid rate limits
- Continues processing on individual email failures
- Logs rate limit errors

## Future Enhancements

1. **Retry Logic**: Implement exponential backoff for failed emails
2. **Metrics**: Add Prometheus metrics for monitoring
3. **Webhooks**: Add webhook support for real-time processing
4. **Templates**: Make email templates configurable
5. **Queue**: Use message queue instead of polling
6. **Caching**: Cache area data to reduce database queries 