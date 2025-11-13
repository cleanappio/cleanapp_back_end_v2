# Report Tags Service

A production-ready Rust microservice for CleanApp's tag feature that provides free-form case-insensitive tags with normalization, autocomplete suggestions, user tag following, and location-based feeds.

## Features

- **Tag Management**: Auto-create and normalize tags with Unicode NFKC normalization
- **Tag Suggestions**: Prefix-based autocomplete with usage-based ranking
- **User Following**: Follow/unfollow tags with 200 tag limit per user
- **Location Feed**: Get reports within 500m radius matching followed tags
- **No Authentication**: Public endpoints for MVP (as per requirements)
- **Auto Schema**: Tables auto-create on startup
- **Production Ready**: Graceful shutdown, health checks, structured logging

## Architecture

- **Framework**: Axum (async HTTP server)
- **Database**: MySQL with SQLx (compile-time checked queries)
- **Runtime**: Tokio (async runtime)
- **Normalization**: Unicode NFKC + case-insensitive canonical tags
- **Spatial Queries**: MySQL ST_Distance_Sphere for location filtering

## API Endpoints

### Health Check

```
GET /health
```

### Tag Management

```
POST /api/v3/reports/:report_seq/tags
GET /api/v3/reports/:report_seq/tags
```

### Tag Suggestions

```
GET /api/v3/tags/suggest?q=beac&limit=10
GET /api/v3/tags/trending?limit=20
```

### User Following

```
POST /api/v3/users/:user_id/tags/follow
DELETE /api/v3/users/:user_id/tags/follow/:tag_id
GET /api/v3/users/:user_id/tags/follows
```

### Location Feed

```
GET /api/v3/feed?lat=40.7&lon=-74.0&radius=500&user_id=abc&limit=20&offset=0
```

## Quick Start

### Using Docker Compose (Recommended)

1. **Start the service with MySQL**:

   ```bash
   docker-compose up -d
   ```

2. **Check health**:

   ```bash
   curl http://localhost:8083/health
   ```

3. **Add tags to a report**:

   ```bash
   curl -X POST http://localhost:8083/api/v3/reports/123/tags \
     -H "Content-Type: application/json" \
     -d '{"tags": ["Beach", "cleanup", "Plastic"]}'
   ```

4. **Get tag suggestions**:

   ```bash
   curl "http://localhost:8083/api/v3/tags/suggest?q=beac&limit=5"
   ```

5. **Follow a tag**:

   ```bash
   curl -X POST http://localhost:8083/api/v3/users/user123/tags/follow \
     -H "Content-Type: application/json" \
     -d '{"tag": "beach"}'
   ```

6. **Get location feed**:
   ```bash
   curl "http://localhost:8083/api/v3/feed?lat=40.7&lon=-74.0&radius=500&user_id=user123&limit=10"
   ```

### Local Development

1. **Setup environment**:

   ```bash
   make env-setup
   # Edit .env with your database settings
   ```

2. **Install dependencies**:

   ```bash
   make deps
   ```

3. **Run the service**:

   ```bash
   make run
   ```

4. **Run tests**:
   ```bash
   make test
   ```

## Configuration

Environment variables (see `env.example`):

| Variable          | Default      | Description                    |
| ----------------- | ------------ | ------------------------------ |
| `DB_HOST`         | `localhost`  | MySQL database host            |
| `DB_PORT`         | `3306`       | MySQL database port            |
| `DB_USER`         | `server`     | MySQL database user            |
| `DB_PASSWORD`     | `secret_app` | MySQL database password        |
| `DB_NAME`         | `cleanapp`   | MySQL database name            |
| `PORT`            | `8083`       | HTTP server port               |
| `RUST_LOG`        | `info`       | Log level                      |
| `MAX_TAG_FOLLOWS` | `200`        | Maximum tags a user can follow |

## Database Schema

The service auto-creates these tables on startup:

### `tags`

- `id` (PRIMARY KEY)
- `canonical_name` (UNIQUE, normalized lowercase)
- `display_name` (original user input)
- `usage_count` (for trending/suggestions)
- `last_used_at` (for recency)
- `created_at`

### `report_tags` (many-to-many)

- `report_seq` + `tag_id` (PRIMARY KEY)
- `created_at`

### `user_tag_follows`

- `user_id` + `tag_id` (PRIMARY KEY)
- `created_at`

## Tag Normalization

Tags are normalized using:

1. Trim whitespace
2. Remove leading `#` if present
3. Unicode NFKC normalization
4. Lowercase conversion
5. Length validation (1-64 characters)
6. Character validation (alphanumeric, spaces, `.-_`)

Examples:

- `"#Beach"` → canonical: `"beach"`, display: `"Beach"`
- `"café"` → canonical: `"cafe"`, display: `"café"`
- `"  Plastic Waste  "` → canonical: `"plastic waste"`, display: `"Plastic Waste"`

## Development

### Project Structure

```
src/
├── main.rs              # Entry point
├── config.rs            # Configuration
├── models.rs            # Data structures
├── database/
│   ├── mod.rs          # Connection pool
│   └── schema.rs       # Table creation
├── services/
│   ├── tag_service.rs  # Tag business logic
│   └── feed_service.rs # Feed queries
├── handlers/
│   ├── tags.rs         # Tag endpoints
│   ├── suggestions.rs  # Autocomplete
│   ├── follows.rs      # User following
│   ├── feed.rs         # Location feed
│   └── health.rs       # Health check
└── utils/
    └── normalization.rs # Tag normalization
```

### Available Commands

```bash
make help              # Show all commands
make build             # Build release binary
make run               # Run locally
make test              # Run tests
make clean             # Clean build artifacts
make docker-build      # Build Docker image
make docker-run        # Start with Docker Compose
make docker-stop       # Stop Docker Compose
make logs              # Show service logs
make health            # Health check
make fmt               # Format code
make lint              # Run clippy
make dev               # Development with hot reload
```

### Testing

```bash
# Unit tests
cargo test

# Integration test with real database
# (requires MySQL running)
cargo test --features integration

# Manual API testing
make health
curl "http://localhost:8083/api/v3/tags/suggest?q=test"
```

## Performance

### Indexes

- `tags.canonical_name` (UNIQUE) for fast lookups
- `tags.usage_count` (DESC) for trending queries
- `report_tags (tag_id, report_seq)` for joins
- `reports_geometry.geom` (SPATIAL) for location queries

### Query Optimization

- Connection pooling (SQLx default)
- Prepared statements via sqlx macros
- Pagination on all list endpoints
- Spatial indexes for location queries

## Deployment

### Docker

```bash
# Build image
./build_image.sh v1.0.0

# Run with Docker Compose
docker-compose up -d

# Scale service
docker-compose up -d --scale report-tags=3
```

### Production Considerations

- Use environment variables for configuration
- Set up MySQL connection pooling
- Configure log levels appropriately
- Consider Redis for caching (future enhancement)
- Monitor health endpoint for orchestration

## API Examples

### Add Tags to Report

```bash
curl -X POST http://localhost:8083/api/v3/reports/123/tags \
  -H "Content-Type: application/json" \
  -d '{"tags": ["Beach", "cleanup", "Plastic"]}'

# Response:
{
  "report_seq": 123,
  "tags_added": ["beach", "cleanup", "plastic"]
}
```

### Get Tag Suggestions

```bash
curl "http://localhost:8083/api/v3/tags/suggest?q=beac&limit=5"

# Response:
{
  "suggestions": [
    {
      "id": 1,
      "display_name": "Beach",
      "canonical_name": "beach",
      "usage_count": 42
    }
  ]
}
```

### Follow Tag

```bash
curl -X POST http://localhost:8083/api/v3/users/user123/tags/follow \
  -H "Content-Type: application/json" \
  -d '{"tag": "beach"}'

# Response:
{
  "followed": true,
  "tag_id": 1
}
```

### Get Location Feed

```bash
curl "http://localhost:8083/api/v3/feed?lat=40.7&lon=-74.0&radius=500&user_id=user123&limit=10&offset=0"

# Response:
{
  "reports": [
    {
      "seq": 123,
      "id": "report_123",
      "team": 1,
      "latitude": 40.7,
      "longitude": -74.0,
      "ts": "2024-01-01T12:00:00Z",
      "tags": [
        {
          "id": 1,
          "canonical_name": "beach",
          "display_name": "Beach",
          "usage_count": 42,
          "last_used_at": "2024-01-01T12:00:00Z",
          "created_at": "2024-01-01T10:00:00Z"
        }
      ],
      "analysis": {
        "seq": 123,
        "source": "openai",
        "analysis_text": "Beach cleanup needed",
        "title": "Beach Litter",
        "description": "Plastic waste on beach",
        "brand_name": "Coca-Cola",
        "brand_display_name": "Coca-Cola",
        "litter_probability": 0.95,
        "hazard_probability": 0.1,
        "digital_bug_probability": 0.0,
        "severity_level": 0.7,
        "summary": "High litter probability",
        "language": "en",
        "classification": "physical",
        "is_valid": true,
        "created_at": "2024-01-01T12:05:00Z",
        "updated_at": "2024-01-01T12:05:00Z"
      }
    }
  ],
  "total": 50,
  "limit": 10,
  "offset": 0
}
```

## Future Enhancements

- Redis caching for suggestions and trending
- Admin endpoints for tag management
- Tag moderation and curation
- Geographic trending (per-tile popularity)
- Authentication and authorization
- Rate limiting
- Metrics and observability
- Tag hierarchies and categories

## License

This project is part of the CleanApp backend services.
