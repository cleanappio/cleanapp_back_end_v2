# Optimized Tag Architecture

## Overview

Successfully optimized the tag implementation by removing duplicate functionality and creating a clean separation of concerns:

1. **report-processor (Go)** - Handles report submission and delegates tag operations to the tag service
2. **report-tags (Rust)** - Full-featured tag microservice with all advanced features
3. **No duplicate functionality** - Each service has a clear, non-overlapping responsibility

## Architecture Benefits

### ✅ **Eliminated Duplication**

- Removed duplicate tag functionality from report-processor
- Single source of truth for all tag operations (Rust service)
- Reduced code maintenance overhead

### ✅ **Clear Separation of Concerns**

- **report-processor**: Report submission, matching, analysis
- **report-tags**: All tag-related functionality
- **Clean interfaces**: HTTP API communication between services

### ✅ **Scalability**

- Tag service can be scaled independently
- Report processor remains lightweight
- Easy to add new tag features without touching report processor

## Service Responsibilities

### Report Processor (Go) - Port 8080

- ✅ Report submission and matching
- ✅ Image comparison and analysis
- ✅ Report status management
- ✅ Delegating tag operations to tag service
- ❌ ~~Direct tag database operations~~
- ❌ ~~Tag normalization logic~~
- ❌ ~~Tag API endpoints~~

### Tag Service (Rust) - Port 8083

- ✅ All tag operations (CRUD)
- ✅ Tag normalization and validation
- ✅ Tag suggestions and autocomplete
- ✅ Trending tags
- ✅ User tag following
- ✅ Location-based feeds
- ✅ Database schema management

## Implementation Details

### 1. Tag Service Client (`services/tag_client.go`)

Created HTTP client to communicate with the Rust tag service:

```go
type TagClient struct {
    baseURL    string
    httpClient *http.Client
}

// AddTagsToReport calls Rust service to add tags
func (tc *TagClient) AddTagsToReport(ctx context.Context, reportSeq int, tags []string) ([]string, error)

// GetTagsForReport calls Rust service to retrieve tags
func (tc *TagClient) GetTagsForReport(ctx context.Context, reportSeq int) ([]TagInfo, error)
```

### 2. Updated Report Submission Flow

**Modified `submitReport` function:**

- Calls Rust tag service for tag processing instead of local operations
- Maintains same API interface for clients
- Graceful error handling (tag failures don't break report submission)

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

### 3. Configuration Updates

**Added to `config/config.go`:**

```go
// Tag service configuration
TagServiceURL string

// Default: http://localhost:8083
TagServiceURL: getEnv("TAG_SERVICE_URL", "http://localhost:8083"),
```

### 4. Removed Duplicate Code

**Deleted files:**

- ❌ `services/tag_service.go` - Duplicate tag service
- ❌ Tag handlers from `handlers/handlers.go`
- ❌ Tag models from `models/report_status.go`
- ❌ Tag routes from `main.go`

## API Usage

### 1. Submit Report with Tags (via report-processor)

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

### 2. Direct Tag Operations (via tag service)

```bash
# Add tags to existing report
curl -X POST http://localhost:8083/api/v3/reports/123/tags \
  -H "Content-Type: application/json" \
  -d '{"tags": ["Ocean", "Pollution"]}'

# Get tags for report
curl http://localhost:8083/api/v3/reports/123/tags

# Get tag suggestions
curl "http://localhost:8083/api/v3/tags/suggest?q=beac&limit=5"

# Get trending tags
curl "http://localhost:8083/api/v3/tags/trending?limit=10"

# Follow a tag
curl -X POST http://localhost:8083/api/v3/users/user123/tags/follow \
  -H "Content-Type: application/json" \
  -d '{"tag": "beach"}'

# Get location feed
curl "http://localhost:8083/api/v3/feed?lat=40.7&lon=-74.0&radius=500&user_id=user123&limit=10"
```

## Environment Variables

### Report Processor

```bash
# Tag service URL
TAG_SERVICE_URL=http://localhost:8083

# Other existing variables...
DB_HOST=localhost
DB_PORT=3306
# etc.
```

### Tag Service

```bash
# Database
DB_HOST=localhost
DB_PORT=3306
DB_USER=server
DB_PASSWORD=secret_app
DB_NAME=cleanapp

# Server
PORT=8083
RUST_LOG=info
MAX_TAG_FOLLOWS=200
```

## Testing

Use the updated test script:

```bash
# Test both services
./test_tags.sh
```

The script tests:

1. Health checks for both services
2. Report submission with tags (via report-processor)
3. Direct tag operations (via tag service)
4. Tag suggestions and trending

## Deployment

### Docker Compose (Recommended)

```yaml
version: "3.8"
services:
  report-processor:
    build: ./report-processor
    ports:
      - "8080:8080"
    environment:
      TAG_SERVICE_URL: http://report-tags:8083
      # ... other config
    depends_on:
      - report-tags

  report-tags:
    build: ./report-tags
    ports:
      - "8083:8083"
    environment:
      # ... tag service config
    depends_on:
      - db

  db:
    image: mysql:8.0
    # ... database config
```

### Individual Services

```bash
# Start tag service first
cd report-tags
docker-compose up -d

# Start report processor
cd report-processor
TAG_SERVICE_URL=http://localhost:8083 go run main.go
```

## Benefits of This Architecture

1. **No Duplication**: Single implementation of tag functionality
2. **Technology Choice**: Each service uses the best technology for its purpose
3. **Independent Scaling**: Services can be scaled based on their specific needs
4. **Clear Boundaries**: Well-defined responsibilities for each service
5. **Maintainability**: Changes to tag features only affect the tag service
6. **Performance**: Rust service provides high-performance tag operations
7. **Future-Proof**: Easy to add new tag features without touching report processor

## Migration Notes

- **Backward Compatible**: Existing report submission API unchanged
- **Gradual Migration**: Can deploy tag service first, then update report processor
- **Fallback Handling**: Report processor gracefully handles tag service failures
- **No Data Loss**: All existing functionality preserved

This optimized architecture provides a clean, scalable, and maintainable solution for tag functionality in CleanApp.
