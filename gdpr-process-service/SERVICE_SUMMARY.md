# GDPR Process Service - Implementation Summary

## Overview
The GDPR Process Service is a microservice designed to continuously monitor and process user and report data for GDPR compliance. It follows the established patterns used in other services within the CleanApp backend ecosystem.

## Architecture Components

### 1. Configuration (`config/config.go`)
- Environment-based configuration management
- Database connection parameters
- Polling interval configuration (default: 60 seconds)
- Follows the same pattern as other services

### 2. Database Layer (`database/`)
- **Schema Management** (`schema.go`): Creates GDPR tracking tables
- **Service Layer** (`service.go`): Handles database operations for GDPR processing
- **Tables Created**:
  - `users_gdpr(id VARCHAR(255))` with unique index on id
  - `reports_gdpr(seq INT)` with unique index on seq

### 3. Processing Logic (`processor/gdpr_processor.go`)
- **User Processing**: `ProcessUser(userID string, avatar string, updateAvatar func(string, string) error)` - implements avatar PII detection and obfuscation via OpenAI API, with automatic database updates
- **Report Processing**: `ProcessReport(seq int)` - placeholder for GDPR logic
- **Extensible Design**: Ready for future GDPR compliance implementation

### 4. Main Service (`main.go`)
- **Continuous Polling**: Main loop that runs indefinitely
- **Batch Processing**: Processes users and reports in batches of 100
- **Error Handling**: Continues processing even if individual records fail
- **Progress Tracking**: Marks successfully processed records

### 5. Database Utilities (`utils/db.go`)
- MySQL connection management
- Connection pooling configuration
- Standard database connection pattern

## Key Features

### Polling Mechanism
- Configurable polling interval via `POLL_INTERVAL` environment variable
- Queries for unprocessed records using LEFT JOINs
- Processes records in chronological order (oldest first)

### Data Processing
- **Users**: Identified by `id` field from `users` table, with automatic avatar PII detection and database updates
- **Reports**: Identified by `seq` field from `reports` table
- Batch size limited to 100 records per cycle for performance

### Progress Tracking
- Successfully processed records are marked in tracking tables
- `processed_at` timestamp automatically recorded
- Prevents duplicate processing of the same records

### Error Resilience
- Individual record failures don't stop the entire batch
- Failed records remain unprocessed for retry in next cycle
- Comprehensive logging for debugging and monitoring

## Database Schema

### Source Tables (Existing)
```sql
users(id VARCHAR(255), ...)           -- User data
reports(seq INT, ...)                 -- Report data
```

### Tracking Tables (Created by Service)
```sql
users_gdpr(
  id VARCHAR(255) PRIMARY KEY,        -- Unique user identifier
  processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

reports_gdpr(
  seq INT PRIMARY KEY,                -- Unique report sequence
  processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## Configuration

### Environment Variables
```bash
DB_HOST=localhost          # Database host
DB_PORT=3306              # Database port
DB_USER=server            # Database username
DB_PASSWORD=secret_app    # Database password
DB_NAME=cleanapp          # Database name
POLL_INTERVAL=60          # Polling interval in seconds
OPENAI_API_KEY=           # OpenAI API key for PII detection
OPENAI_MODEL=gpt-4o       # OpenAI model to use for analysis
```

### Docker Configuration
- Multi-stage Dockerfile for optimized builds
- Docker Compose configuration for easy deployment
- Network integration with existing CleanApp services

## Deployment

### Local Development
```bash
cd gdpr-process-service
go mod tidy
go run main.go
```

### Docker Deployment
```bash
./build_image.sh
docker-compose up -d
```

### Integration
- Service integrates with existing CleanApp database
- Uses same database credentials and network configuration
- Can run alongside other microservices

## Monitoring and Logging

### Log Output
- Service startup and configuration information
- Processing statistics (counts of processed records)
- Individual record processing status
- Error messages for failed operations

### Health Monitoring
- Database connectivity verification
- Processing loop status
- Batch processing results

## Future Implementation Points

### GDPR Processing Functions
The placeholder functions in `processor/gdpr_processor.go` can be extended with:

1. **Data Anonymization**
   - User ID hashing/encryption
   - Location data obfuscation
   - Metadata sanitization

2. **Consent Management**
   - Consent status verification
   - Consent withdrawal processing
   - Consent history tracking

3. **Data Retention**
   - Retention policy enforcement
   - Automatic data deletion
   - Retention period management

4. **Data Export/Deletion**
   - GDPR Article 20 (Data Portability)
   - GDPR Article 17 (Right to Erasure)
   - Data export formatting

5. **Audit Trail**
   - Processing history logging
   - Compliance verification records
   - Regulatory reporting support

## Security Considerations

- **Database Access**: Minimal required permissions
- **No External Endpoints**: Internal service only
- **Environment Configuration**: Secure credential management
- **Data Isolation**: Separate tracking tables for audit purposes

## Performance Characteristics

- **Batch Processing**: 100 records per cycle maximum
- **Polling Interval**: Configurable (default 60 seconds)
- **Database Efficiency**: Uses indexed queries and LEFT JOINs
- **Memory Usage**: Minimal memory footprint
- **Scalability**: Multiple instances can run simultaneously

## Integration Points

- **Database**: MySQL integration with existing CleanApp schema
- **Logging**: Standard Go logging with apex/log
- **Configuration**: Environment-based configuration management
- **OpenAI API**: Integration for PII detection and obfuscation

## OpenAI Integration

### PII Detection and Obfuscation
The service now integrates with OpenAI's API to automatically detect and obfuscate Personally Identifiable Information (PII) in user avatar fields:

- **Detection**: Identifies PII including full names, email addresses, physical addresses, and credit card data
- **Obfuscation**: Replaces detected PII with asterisks (*) using a fixed-length pattern
- **JSON Response**: Returns results in structured JSON format for easy processing
- **Fallback Handling**: Gracefully handles API failures and malformed responses
- **Database Updates**: Automatically updates the users table with obfuscated avatars when PII is detected

### Configuration Requirements
- `OPENAI_API_KEY`: Valid OpenAI API key
- `OPENAI_MODEL`: Model to use (default: gpt-4o)
- Network access to OpenAI API endpoints
- **Docker**: Containerized deployment with existing infrastructure
- **Networking**: Integration with CleanApp service network

This service provides a solid foundation for GDPR compliance processing while maintaining the architectural patterns and deployment strategies used throughout the CleanApp backend ecosystem.
