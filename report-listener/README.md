# Report Listener Microservice

A Go microservice that listens to the `reports` table in the CleanApp database and broadcasts new reports to connected WebSocket clients in real-time.

## Features

- **Real-time Broadcasting**: Broadcasts new reports to all connected WebSocket clients
- **Batch Processing**: Groups reports within the broadcast interval and sends them as batches
- **Service Recovery**: Tracks the last processed report sequence and resumes from where it left off after service interruption
- **Configurable Broadcast Frequency**: Adjustable broadcast interval (default: 1 second)
- **Health Monitoring**: Provides health check endpoints with service statistics
- **Graceful Shutdown**: Handles shutdown signals and closes connections properly

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   WebSocket     │    │   Report        │    │   MySQL         │
│   Clients       │◄───┤   Listener      │◄───┤   Database      │
│                 │    │   Service       │    │   (reports)     │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## API Endpoints

### WebSocket Endpoint
```
GET /api/v3/reports/listen
```
Establishes a WebSocket connection for real-time report updates.

**Message Format:**
```json
{
  "type": "reports",
  "data": {
    "reports": [
      {
        "seq": 123,
        "timestamp": "2024-01-01T12:00:00Z",
        "id": "user123",
        "team": 1,
        "latitude": 40.7128,
        "longitude": -74.0060,
        "x": 0.5,
        "y": 0.3,
        "action_id": "action123"
      }
    ],
    "count": 1,
    "from_seq": 123,
    "to_seq": 123
  },
  "timestamp": "2024-01-01T12:00:01Z"
}
```

### Health Check Endpoint
```
GET /api/v3/reports/health
```
Returns service health status and statistics.

**Response:**
```json
{
  "status": "healthy",
  "service": "report-listener",
  "timestamp": "2024-01-01T12:00:00Z",
  "connected_clients": 5,
  "last_broadcast_seq": 123
}
```

### Root Health Check
```
GET /health
```
Simple health check endpoint.

## Configuration

The service is configured via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | MySQL database host |
| `DB_PORT` | `3306` | MySQL database port |
| `DB_USER` | `server` | MySQL database user |
| `DB_PASSWORD` | `secret_app` | MySQL database password |
| `DB_NAME` | `cleanapp` | MySQL database name |
| `PORT` | `8080` | HTTP server port |
| `BROADCAST_INTERVAL` | `1s` | Broadcast frequency (e.g., `500ms`, `2s`) |
| `LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |

## Database Schema

The service listens to the `reports` table with the following structure:

```sql
CREATE TABLE reports(
  seq INT NOT NULL AUTO_INCREMENT,
  ts TIMESTAMP default current_timestamp,
  id VARCHAR(255) NOT NULL,
  team INT NOT NULL,
  latitude FLOAT NOT NULL,
  longitude FLOAT NOT NULL,
  x FLOAT,
  y FLOAT,
  image LONGBLOB NOT NULL,
  action_id VARCHAR(32),
  PRIMARY KEY (seq),
  INDEX id_index (id),
  INDEX action_idx (action_id)
);
```

## Running the Service

### Using Docker

```bash
# Build the image
docker build -t report-listener .

# Run the container
docker run -d \
  --name report-listener \
  -p 8080:8080 \
  -e DB_HOST=your-db-host \
  -e DB_PASSWORD=your-db-password \
  report-listener
```

### Using Docker Compose

```yaml
version: '3.8'
services:
  report-listener:
    build: .
    ports:
      - "8080:8080"
    environment:
      - DB_HOST=mysql
      - DB_PASSWORD=secret_app
    depends_on:
      - mysql
    restart: unless-stopped
```

### Running Locally

```bash
# Install dependencies
go mod download

# Run the service
go run main.go
```

## WebSocket Client Example

```javascript
// Connect to the WebSocket endpoint
const ws = new WebSocket('ws://localhost:8080/api/v3/reports/listen');

// Handle connection open
ws.onopen = function() {
    console.log('Connected to report listener');
};

// Handle incoming messages
ws.onmessage = function(event) {
    const message = JSON.parse(event.data);
    
    if (message.type === 'reports') {
        const batch = message.data;
        console.log(`Received ${batch.count} reports (seq ${batch.from_seq}-${batch.to_seq})`);
        
        batch.reports.forEach(report => {
            console.log(`Report ${report.seq}: ${report.id} at (${report.latitude}, ${report.longitude})`);
        });
    }
};

// Handle connection close
ws.onclose = function() {
    console.log('Disconnected from report listener');
};

// Handle errors
ws.onerror = function(error) {
    console.error('WebSocket error:', error);
};
```

## Service Recovery

The service tracks the last processed report sequence number and automatically resumes from where it left off after a restart or interruption. This ensures no reports are missed during service downtime.

### Persistent State Storage

The service uses a `service_state` table in the database to persistently store the last processed sequence number:

```sql
CREATE TABLE service_state (
    service_name VARCHAR(100) PRIMARY KEY,
    last_processed_seq INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
```

### Recovery Process

1. **Service Startup**: 
   - Checks for existing state in `service_state` table
   - If found, resumes from the stored sequence number
   - If not found, starts from the latest report in the database

2. **State Persistence**:
   - Updates the sequence number after each successful broadcast
   - Uses database transactions to ensure consistency
   - Handles database failures gracefully (logs warnings but continues operation)

3. **Fault Tolerance**:
   - If the service crashes or is restarted, it will automatically recover
   - No reports are lost or duplicated
   - Multiple service instances can safely share the same database

### Manual State Inspection

You can check the service state using the provided SQL script:

```bash
mysql -u server -p cleanapp < scripts/check_state.sql
```

This will show:
- Current last processed sequence
- Latest available report sequence
- Number of potentially missed reports
- Time since last update

## Monitoring

The service provides several metrics through the health check endpoint:

- **Connected Clients**: Number of active WebSocket connections
- **Last Broadcast Sequence**: The sequence number of the last broadcasted report
- **Service Status**: Current health status of the service

## Logging

The service logs important events including:

- Service startup/shutdown
- Client connections/disconnections
- Report processing and broadcasting
- Database connection status
- Error conditions

## Security Considerations

- The WebSocket endpoint currently allows all origins (CORS is set to `*`)
- In production, implement proper origin checking
- Consider adding authentication for WebSocket connections
- Use TLS/SSL in production environments

## Performance

- Uses connection pooling for database connections
- Implements efficient WebSocket message broadcasting
- Configurable broadcast intervals to balance latency and throughput
- Graceful handling of client disconnections

## Troubleshooting

### Common Issues

1. **Database Connection Failed**
   - Check database credentials and network connectivity
   - Verify the database is running and accessible

2. **No Reports Being Broadcasted**
   - Check if new reports are being inserted into the database
   - Verify the service has the correct database permissions

3. **WebSocket Connection Issues**
   - Check if the service is running on the expected port
   - Verify firewall settings allow WebSocket connections

### Debug Mode

Enable debug logging by setting `LOG_LEVEL=debug`:

```bash
export LOG_LEVEL=debug
go run main.go
``` 