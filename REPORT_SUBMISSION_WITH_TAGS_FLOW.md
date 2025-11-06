# Report Submission with Tags Flow

> **TODO**: This document needs to be updated to reflect the actual RabbitMQ-based event manager implementation. The communication between report-processor and report-tags should be via RabbitMQ message broker, not HTTP API.

## Overview

This document explains the updated flow when a new report is submitted with tags in the optimized CleanApp architecture. The system now uses a clean separation between the report processor (Go) and tag service (Rust) with no duplicate functionality. Communication between services is handled via RabbitMQ event manager.

## Architecture

```
┌─────────────────────┐                    ┌─────────────────────┐
│  report-processor   │                    │    report-tags      │
│     (Go: 8080)      │                    │    (Rust: 8083)     │
│                     │                    │                     │
│ • Report submission │                    │ • All tag ops       │
│ • Image matching    │                    │ • Tag normalization │
│ • Publishes events  │                    │ • Suggestions       │
│                     │                    │ • Trending          │
└──────────┬──────────┘                    └──────────┬──────────┘
           │                                          │
           │  RabbitMQ Event Manager                  │
           │  (Exchange: cleanapp)                    │
           │                                          │
           │  Routing Keys:                           │
           │  • report.raw (publish)                  │
           │  • tag.added (subscribe)                 │
           │                                          │
           ▼                                          ▼
    ┌──────────────────────────────────────────────────┐
    │           RabbitMQ Message Broker                │
    │         (Exchange: cleanapp)                      │
    │         Queue: report-tags                       │
    └──────────────────────────────────────────────────┘
```

## Report Submission Flow

### 1. Client Request

The client submits a report with tags via the `/match_report` endpoint:

```bash
curl -X POST http://localhost:8080/api/v3/match_report \
  -H "Content-Type: application/json" \
  -d '{
    "version": "2.0",
    "id": "report-123",
    "latitude": 40.7128,
    "longitude": -74.0060,
    "x": 0.5,
    "y": 0.5,
    "image": "<base64_image>",
    "annotation": "Beach cleanup needed",
    "tags": ["Beach", "Plastic", "Cleanup"]
  }'
```

### 2. Report Processor (Go Service)

The report-processor service handles the request:

#### 2.1 Validation

- Validates version (must be "2.0")
- Validates coordinates (latitude: -90 to 90, longitude: -180 to 180)
- Validates x,y coordinates (0 to 1)
- Validates image data

#### 2.2 Image Matching

- Compares the submitted image with existing reports within 10m radius
- Uses OpenAI API for image comparison (if configured)
- Determines if the report matches existing reports

#### 2.3 Report Submission

- If no matches found, submits the report to the main reports service
- Gets back a `report_seq` (sequence number) for the new report

#### 2.4 Tag Processing

- **If tags are provided**: Publishes a message to RabbitMQ with routing key `report.raw`
- **If no tags**: Skips tag processing

The message published to RabbitMQ includes:

- `report_seq`: The sequence number of the newly created report
- `tags`: Array of tag strings from the request
- `timestamp`: When the message was created

```go
// Process tags if provided
if len(req.Tags) > 0 {
    // Publish tag processing event to RabbitMQ
    event := TagProcessingEvent{
        ReportSeq: response.Seq,
        Tags:      req.Tags,
        Timestamp: time.Now(),
    }

    err := h.rabbitmqPublisher.Publish(
        ctx,
        "report.raw",  // routing key
        event,
    )
    if err != nil {
        log.Printf("Failed to publish tag event for report %d: %v", response.Seq, err)
        // Don't fail the whole operation for tag processing
    } else {
        log.Printf("Successfully published tag event for report %d with %d tags", response.Seq, len(req.Tags))
    }
}
```

**Note**: The report-processor publishes the event asynchronously and does not wait for tag processing to complete. This ensures the client receives a response quickly while tag processing happens in the background.

### 3. Tag Service (Rust Service)

The tag service subscribes to RabbitMQ messages and processes tags asynchronously:

#### 3.0 Message Consumption

- The tag service subscribes to the `report-tags` queue
- Listens for messages with routing key `report.raw`
- Processes messages asynchronously as they arrive

#### 3.1 Tag Normalization

For each tag in the RabbitMQ message:

- Trim whitespace
- Remove leading `#` if present
- Apply Unicode NFKC normalization
- Convert to lowercase for canonical name
- Validate length (1-64 characters)
- Preserve original as display name

Example: `"#Beach"` → canonical: `"beach"`, display: `"Beach"`

#### 3.2 Database Operations

For each normalized tag:

1. **Upsert Tag**:

   ```sql
   INSERT INTO tags (canonical_name, display_name, usage_count, last_used_at)
   VALUES (?, ?, 0, NULL)
   ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)
   ```

2. **Link to Report**:

   ```sql
   INSERT IGNORE INTO report_tags (report_seq, tag_id)
   VALUES (?, ?)
   ```

3. **Increment Usage**:
   ```sql
   UPDATE tags
   SET usage_count = usage_count + 1, last_used_at = NOW()
   WHERE id = ?
   ```

#### 3.3 Event Publishing

After successfully processing tags, the tag service publishes a `TagAddedEvent` to RabbitMQ:

```json
{
  "report_seq": 123,
  "tags": ["beach", "plastic", "cleanup"],
  "timestamp": "2024-01-15T10:30:00Z"
}
```

This event is published with routing key `tag.added` and can be consumed by other services that need to react to tag additions.

### 4. Final Response

The report-processor returns the final response to the client:

```json
{
  "success": true,
  "message": "Report matching completed. 0 reports resolved out of 0 compared.",
  "results": []
}
```

## Database Schema

### Tables Created by Tag Service

#### `tags` table

```sql
CREATE TABLE IF NOT EXISTS tags (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    canonical_name VARCHAR(255) NOT NULL UNIQUE,
    display_name VARCHAR(255) NOT NULL,
    usage_count INT UNSIGNED DEFAULT 0,
    last_used_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_canonical_name (canonical_name),
    INDEX idx_usage_count (usage_count DESC),
    INDEX idx_last_used (last_used_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### `report_tags` table (many-to-many)

```sql
CREATE TABLE IF NOT EXISTS report_tags (
    report_seq INT NOT NULL,
    tag_id INT UNSIGNED NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (report_seq, tag_id),
    INDEX idx_tag_id (tag_id),
    INDEX idx_report_seq (report_seq),
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
) ENGINE=InnoDB;
```

## Error Handling

### Graceful Degradation

- If RabbitMQ is unavailable, report submission still succeeds
- Tag processing errors are logged but don't fail the entire operation
- Client receives successful response even if tag processing fails
- Messages are persisted in RabbitMQ queue, so tag processing can resume when the service is available

### Error Scenarios

1. **Invalid tags**: Invalid tags are skipped, valid ones are processed
2. **RabbitMQ down**: Report is submitted, tag event publishing fails gracefully
3. **Tag service down**: Messages accumulate in RabbitMQ queue and are processed when service recovers
4. **Database errors**: Tag operations fail gracefully, messages can be retried
5. **Message processing errors**: Failed messages are rejected and can be handled by dead letter queue (if configured)

## Configuration

### Report Processor Environment Variables

```bash
# RabbitMQ Configuration (for publishing tag events)
AMQP_HOST=localhost
AMQP_PORT=5672
AMQP_USER=guest
AMQP_PASSWORD=guest
RABBITMQ_EXCHANGE=cleanapp
RABBITMQ_RAW_REPORT_ROUTING_KEY=report.raw

# Database (for report processing)
DB_HOST=localhost
DB_PORT=3306
DB_USER=root
DB_PASSWORD=db_password
DB_NAME=cleanapp

# Other existing variables...
```

### Tag Service Environment Variables

```bash
# Database
DB_HOST=localhost
DB_PORT=3306
DB_USER=root
DB_PASSWORD=db_password
DB_NAME=cleanapp

# RabbitMQ Configuration (for consuming tag events and publishing tag.added events)
AMQP_HOST=localhost
AMQP_PORT=5672
AMQP_USER=guest
AMQP_PASSWORD=guest
RABBITMQ_EXCHANGE=cleanapp
RABBITMQ_QUEUE=report-tags
RABBITMQ_RAW_REPORT_ROUTING_KEY=report.raw
RABBITMQ_TAG_EVENT_ROUTING_KEY=tag.added

# Server
PORT=8083
RUST_LOG=info
MAX_TAG_FOLLOWS=200
```

## API Endpoints

### Report Submission

- **POST** `/api/v3/match_report` - Submit report with optional tags
  - Tags are processed asynchronously via RabbitMQ (routing key: `report.raw`)

### Tag Operations (via Tag Service HTTP API)

**Note**: While tag processing during report submission uses RabbitMQ, the tag service also exposes HTTP endpoints for querying and managing tags:

- **POST** `/api/v3/reports/:report_seq/tags` - Add tags to existing report
- **GET** `/api/v3/reports/:report_seq/tags` - Get tags for report
- **GET** `/api/v3/tags/suggest?q=beac&limit=5` - Tag suggestions
- **GET** `/api/v3/tags/trending?limit=10` - Trending tags
- **POST** `/api/v3/users/:user_id/tags/follow` - Follow a tag
- **DELETE** `/api/v3/users/:user_id/tags/follow/:tag_id` - Unfollow a tag
- **GET** `/api/v3/users/:user_id/tags/follows` - List followed tags
- **GET** `/api/v3/feed?lat=40.7&lon=-74.0&radius=500&user_id=user123&limit=10` - Location feed

## Benefits of This Architecture

### 1. **No Duplication**

- Single implementation of tag functionality
- Reduced maintenance overhead
- Consistent tag behavior across the system

### 2. **Clear Separation of Concerns**

- Report processor: Report submission, matching, analysis
- Tag service: All tag-related functionality
- Event-driven communication via RabbitMQ message broker

### 3. **Scalability**

- Services can be scaled independently
- Tag service can handle high tag processing load
- Report processor remains lightweight

### 4. **Technology Optimization**

- Go: Excellent for HTTP services and report processing
- Rust: High performance for tag operations and database queries

### 5. **Maintainability**

- Changes to tag features only affect the tag service
- Easy to add new tag functionality
- Clear boundaries between services

## Testing the Flow

### 1. Start Services

```bash
# Start RabbitMQ (if not already running)
docker run -d --name rabbitmq \
  -p 5672:5672 \
  -p 15672:15672 \
  -e RABBITMQ_DEFAULT_USER=guest \
  -e RABBITMQ_DEFAULT_PASS=guest \
  rabbitmq:3-management-alpine

# Start tag service
cd report-tags
RUST_LOG=info \
  DATABASE_URL="mysql://root:db_password@localhost:3306/cleanapp" \
  AMQP_HOST=localhost \
  AMQP_PORT=5672 \
  AMQP_USER=guest \
  AMQP_PASSWORD=guest \
  RABBITMQ_EXCHANGE=cleanapp \
  RABBITMQ_QUEUE=report-tags \
  RABBITMQ_RAW_REPORT_ROUTING_KEY=report.raw \
  RABBITMQ_TAG_EVENT_ROUTING_KEY=tag.added \
  PORT=8083 \
  cargo run &

# Start report processor
cd report-processor
AMQP_HOST=localhost \
  AMQP_PORT=5672 \
  AMQP_USER=guest \
  AMQP_PASSWORD=guest \
  RABBITMQ_EXCHANGE=cleanapp \
  RABBITMQ_RAW_REPORT_ROUTING_KEY=report.raw \
  go run main.go &
```

### 2. Test Report Submission

```bash
curl -X POST http://localhost:8080/api/v3/match_report \
  -H "Content-Type: application/json" \
  -d '{
    "version": "2.0",
    "id": "test-report-123",
    "latitude": 40.7128,
    "longitude": -74.0060,
    "x": 0.5,
    "y": 0.5,
    "image": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
    "annotation": "Test litter report",
    "tags": ["Beach", "Plastic", "Cleanup"]
  }'
```

### 3. Verify Tags

```bash
# Check tags were added
curl "http://localhost:8083/api/v3/reports/1/tags"

# Check tag suggestions
curl "http://localhost:8083/api/v3/tags/suggest?q=beac&limit=5"

# Check trending tags
curl "http://localhost:8083/api/v3/tags/trending?limit=5"
```

## Summary

The optimized architecture provides a clean, efficient flow for report submission with tags:

1. **Client** submits report with tags to report-processor
2. **Report-processor** handles report submission and publishes tag processing event to RabbitMQ
3. **Tag service** consumes the event from RabbitMQ, normalizes, validates, and stores tags
4. **Both services** work together seamlessly via RabbitMQ event manager (asynchronous, decoupled)
5. **Client** receives successful response immediately; tag processing happens asynchronously in the background

This architecture eliminates duplication, provides clear separation of concerns, and enables independent scaling of services while maintaining a seamless user experience. The event-driven approach ensures loose coupling and high availability.
