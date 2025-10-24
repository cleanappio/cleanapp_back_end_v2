# Report Submission with Tags Flow

## Overview

This document explains the updated flow when a new report is submitted with tags in the optimized CleanApp architecture. The system now uses a clean separation between the report processor (Go) and tag service (Rust) with no duplicate functionality.

## Architecture

```
┌─────────────────────┐    HTTP API    ┌─────────────────────┐
│  report-processor   │ ──────────────►│    report-tags      │
│     (Go: 8080)      │                │    (Rust: 8083)     │
│                     │                │                     │
│ • Report submission │                │ • All tag ops       │
│ • Image matching    │                │ • Tag normalization │
│ • Delegates tags    │                │ • Suggestions       │
│ • Tag client        │                │ • Trending          │
└─────────────────────┘                └─────────────────────┘
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

- **If tags are provided**: Calls the tag service via HTTP API
- **If no tags**: Skips tag processing

```go
// Process tags if provided
if len(req.Tags) > 0 {
    addedTags, err := h.tagClient.AddTagsToReport(ctx, response.Seq, req.Tags)
    if err != nil {
        log.Printf("Failed to add tags to report %d: %v", response.Seq, err)
        // Don't fail the whole operation for tag processing
    } else {
        log.Printf("Successfully added %d tags to report %d: %v", len(addedTags), response.Seq, addedTags)
    }
}
```

### 3. Tag Service (Rust Service)

The tag service processes the tags:

#### 3.1 Tag Normalization

For each tag in the request:

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

#### 3.3 Response

Returns the successfully added tags:

```json
{
  "report_seq": 123,
  "tags_added": ["beach", "plastic", "cleanup"]
}
```

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

- If tag service is unavailable, report submission still succeeds
- Tag processing errors are logged but don't fail the entire operation
- Client receives successful response even if tag processing fails

### Error Scenarios

1. **Invalid tags**: Invalid tags are skipped, valid ones are processed
2. **Tag service down**: Report is submitted, tags are logged as failed
3. **Database errors**: Tag operations fail gracefully, report submission continues

## Configuration

### Report Processor Environment Variables

```bash
# Tag service URL
TAG_SERVICE_URL=http://localhost:8083

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

# Server
PORT=8083
RUST_LOG=info
MAX_TAG_FOLLOWS=200
```

## API Endpoints

### Report Submission

- **POST** `/api/v3/match_report` - Submit report with optional tags

### Tag Operations (via Tag Service)

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
- Clean HTTP API communication

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
# Start tag service
cd report-tags
RUST_LOG=info DATABASE_URL="mysql://root:db_password@localhost:3306/cleanapp" PORT=8083 cargo run &

# Start report processor
cd report-processor
TAG_SERVICE_URL=http://localhost:8083 go run main.go &
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
2. **Report-processor** handles report submission and delegates tag processing
3. **Tag service** normalizes, validates, and stores tags
4. **Both services** work together seamlessly via HTTP API
5. **Client** receives successful response with report processed and tags added

This architecture eliminates duplication, provides clear separation of concerns, and enables independent scaling of services while maintaining a seamless user experience.
