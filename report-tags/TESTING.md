# Testing Guide for Report Tags Service

This guide covers all the ways to test the report-tags microservice.

## Prerequisites

1. **Database**: MySQL running and accessible
2. **Service Running**: Either locally or via Docker
3. **Optional**: RabbitMQ (service works without it, but won't publish events)

## Quick Start

### 1. Start the Service

**Option A: Local Development**
```bash
# Set up environment
make env-setup
# Edit .env with your database credentials

# Run the service
make run
# or
cargo run
```

**Option B: Docker Compose**
```bash
# Start service with MySQL and RabbitMQ
docker compose up -d

# Check logs
docker compose logs -f report-tags
```

### 2. Verify Service is Running

```bash
# Health check
curl http://localhost:8083/health

# Or use make
make health
```

Expected response:
```json
{
  "status": "healthy",
  "service": "report-tags"
}
```

## Testing Methods

### 1. Unit Tests

Run the existing unit tests:

```bash
# Run all unit tests
cargo test

# Run specific test
cargo test test_normalize_tag

# Run with output
cargo test -- --nocapture

# Run tests in a specific module
cargo test normalization
```

**Existing Tests:**
- Tag normalization tests in `src/utils/normalization.rs`
  - Tests for Unicode normalization
  - Tests for tag trimming and validation
  - Tests for canonical name generation

### 2. Integration Tests (Manual API Testing)

#### Using the Test Script

```bash
# Make sure service is running on localhost:8083
./test_api.sh
```

This script tests:
1. Health check
2. Add tags to a report
3. Get tags for a report
4. Get tag suggestions
5. Get trending tags
6. Follow a tag
7. Get user follows
8. Get location feed

#### Manual API Testing

**Health Check:**
```bash
curl http://localhost:8083/health | jq .
```

**Add Tags to Report:**
```bash
curl -X POST http://localhost:8083/api/v3/reports/123/tags \
  -H "Content-Type: application/json" \
  -d '{"tags": ["Beach", "cleanup", "Plastic"]}' | jq .
```

**Get Tags for Report:**
```bash
curl http://localhost:8083/api/v3/reports/123/tags | jq .
```

**Get Tag Suggestions:**
```bash
curl "http://localhost:8083/api/v3/tags/suggest?q=beac&limit=5" | jq .
```

**Get Trending Tags:**
```bash
curl "http://localhost:8083/api/v3/tags/trending?limit=10" | jq .
```

**Follow a Tag:**
```bash
curl -X POST http://localhost:8083/api/v3/users/user123/tags/follow \
  -H "Content-Type: application/json" \
  -d '{"tag": "beach"}' | jq .
```

**Get User Follows:**
```bash
curl "http://localhost:8083/api/v3/users/user123/tags/follows" | jq .
```

**Unfollow a Tag:**
```bash
curl -X DELETE "http://localhost:8083/api/v3/users/user123/tags/follow/1" | jq .
```

**Get Location Feed:**
```bash
curl "http://localhost:8083/api/v3/feed?lat=40.7&lon=-74.0&radius=500&user_id=user123&limit=10&offset=0" | jq .
```

### 3. Testing with Docker

**Start Services:**
```bash
cd report-tags
docker compose up -d

# Check service status
docker compose ps

# View logs
docker compose logs -f report-tags
```

**Run Tests Against Docker Service:**
```bash
# The service will be on localhost:8083
./test_api.sh
```

**Stop Services:**
```bash
docker compose down
```

### 4. Testing RabbitMQ Integration

**Check if RabbitMQ is Available:**
```bash
# Check logs for RabbitMQ initialization
docker compose logs report-tags | grep -i rabbitmq

# Or if running locally, check stderr logs
```

**Expected Logs:**
- Success: `INFO - RabbitMQ publisher initialized successfully`
- Failure (graceful): `WARN - Failed to initialize RabbitMQ publisher: ... Continuing without RabbitMQ.`

**Test Tag Event Publishing:**

1. Start RabbitMQ management UI:
   ```bash
   # Access at http://localhost:15672
   # Login: guest/guest
   ```

2. Add tags to a report:
   ```bash
   curl -X POST http://localhost:8083/api/v3/reports/123/tags \
     -H "Content-Type: application/json" \
     -d '{"tags": ["Beach", "cleanup"]}'
   ```

3. Check RabbitMQ management UI:
   - Go to Exchanges → `cleanapp` (or configured exchange)
   - Check the `tag.added` routing key
   - You should see messages published

### 5. Database Testing

**Verify Tables Created:**
```sql
USE cleanapp;
SHOW TABLES LIKE '%tag%';

-- Should show:
-- tags
-- report_tags  
-- user_tag_follows
```

**Check Data:**
```sql
-- View tags
SELECT * FROM tags ORDER BY usage_count DESC LIMIT 10;

-- View report tags
SELECT rt.report_seq, t.canonical_name, t.usage_count 
FROM report_tags rt
JOIN tags t ON rt.tag_id = t.id
ORDER BY rt.report_seq;

-- View user follows
SELECT utf.user_id, t.canonical_name, utf.created_at
FROM user_tag_follows utf
JOIN tags t ON utf.tag_id = t.id
ORDER BY utf.user_id;
```

### 6. Load Testing

**Simple Load Test with Apache Bench:**
```bash
# Install ab if needed
# macOS: already installed
# Ubuntu: sudo apt-get install apache2-utils

# Test health endpoint
ab -n 1000 -c 10 http://localhost:8083/health

# Test tag suggestions
ab -n 100 -c 5 "http://localhost:8083/api/v3/tags/suggest?q=beac&limit=5"
```

**Using curl in a loop:**
```bash
# Test tag suggestions endpoint
for i in {1..10}; do
  curl -s "http://localhost:8083/api/v3/tags/suggest?q=beac&limit=5" > /dev/null
  echo "Request $i completed"
done
```

### 7. Error Testing

**Test Invalid Input:**
```bash
# Invalid tag (empty)
curl -X POST http://localhost:8083/api/v3/reports/123/tags \
  -H "Content-Type: application/json" \
  -d '{"tags": [""]}'

# Invalid report_seq (non-numeric)
curl http://localhost:8083/api/v3/reports/abc/tags

# Missing required fields
curl -X POST http://localhost:8083/api/v3/reports/123/tags \
  -H "Content-Type: application/json" \
  -d '{}'
```

**Test Follow Limits:**
```bash
# Follow 201 tags (should fail with limit 200)
for i in {1..201}; do
  curl -X POST http://localhost:8083/api/v3/users/user123/tags/follow \
    -H "Content-Type: application/json" \
    -d "{\"tag\": \"tag$i\"}"
done
```

## Testing Scenarios

### Scenario 1: Complete Tag Lifecycle

```bash
# 1. Add tags to a report
curl -X POST http://localhost:8083/api/v3/reports/1001/tags \
  -H "Content-Type: application/json" \
  -d '{"tags": ["Beach", "Cleanup", "Plastic"]}'

# 2. Get tags for the report
curl http://localhost:8083/api/v3/reports/1001/tags | jq .

# 3. Get suggestions (should include "beach")
curl "http://localhost:8083/api/v3/tags/suggest?q=beac&limit=5" | jq .

# 4. Follow the tag
curl -X POST http://localhost:8083/api/v3/users/alice/tags/follow \
  -H "Content-Type: application/json" \
  -d '{"tag": "beach"}'

# 5. Get user follows
curl "http://localhost:8083/api/v3/users/alice/tags/follows" | jq .
```

### Scenario 2: Tag Normalization

```bash
# Test various tag formats that should normalize to the same canonical tag
curl -X POST http://localhost:8083/api/v3/reports/1002/tags \
  -H "Content-Type: application/json" \
  -d '{"tags": ["Beach", "beach", "BEACH", "#Beach", "  Beach  "]}'

# All should normalize to canonical "beach"
curl http://localhost:8083/api/v3/reports/1002/tags | jq .
```

### Scenario 3: Trending Tags

```bash
# Add tags to multiple reports
for seq in 2001 2002 2003 2004 2005; do
  curl -X POST http://localhost:8083/api/v3/reports/$seq/tags \
    -H "Content-Type: application/json" \
    -d '{"tags": ["PopularTag"]}'
done

# Check trending (should show PopularTag)
curl "http://localhost:8083/api/v3/tags/trending?limit=5" | jq .
```

## Debugging Tips

### View Service Logs

**Local:**
- Logs go to stderr by default
- Set `RUST_LOG=debug` for more verbose logging

**Docker:**
```bash
docker compose logs -f report-tags
```

### Check Database Connection

```bash
# Test database connection from service logs
docker compose logs report-tags | grep -i "database\|mysql\|connection"
```

### Check RabbitMQ Connection

```bash
# Check if RabbitMQ is connected
docker compose logs report-tags | grep -i rabbitmq
```

### Enable Debug Logging

```bash
# In .env file
RUST_LOG=debug

# Or when running
RUST_LOG=debug cargo run
```

## Common Issues

### Service Won't Start

1. **Database not accessible:**
   ```bash
   # Check database connection
   mysql -h localhost -u server -p cleanapp
   ```

2. **Port already in use:**
   ```bash
   # Check what's using port 8083
   lsof -i :8083
   # Or change PORT in .env
   ```

### Tests Fail

1. **Database not initialized:**
   - Service auto-creates tables on startup
   - Check logs for schema initialization

2. **Missing dependencies:**
   ```bash
   cargo test --no-run  # Check if tests compile
   ```

### API Returns Errors

1. **Check service logs** for detailed error messages
2. **Verify database tables exist**
3. **Check request format** matches API documentation

## Continuous Testing

### With Cargo Watch

```bash
# Install cargo-watch
cargo install cargo-watch

# Run tests on file changes
cargo watch -x test

# Run service with auto-reload
make dev
```

### CI/CD Integration

Add to your CI pipeline:

```yaml
# Example GitHub Actions
- name: Run tests
  run: |
    cd report-tags
    cargo test
    
- name: Build Docker image
  run: |
    cd report-tags
    docker build -t report-tags:test .
```

## Performance Testing

### Benchmark Tag Operations

```bash
# Time tag addition
time curl -X POST http://localhost:8083/api/v3/reports/9999/tags \
  -H "Content-Type: application/json" \
  -d '{"tags": ["TestTag"]}'

# Time suggestions query
time curl "http://localhost:8083/api/v3/tags/suggest?q=test&limit=10"
```

### Database Query Performance

```sql
-- Check query performance
EXPLAIN SELECT * FROM tags WHERE canonical_name LIKE 'beac%';
EXPLAIN SELECT * FROM tags ORDER BY usage_count DESC LIMIT 10;
```

## Next Steps

1. **Add Integration Tests**: Create integration tests that use a test database
2. **Add Performance Tests**: Benchmark critical endpoints
3. **Add Contract Tests**: Verify API contracts match expectations
4. **Add End-to-End Tests**: Test complete user workflows


