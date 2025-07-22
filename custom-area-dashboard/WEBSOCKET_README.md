# Montenegro Areas WebSocket Service

This service now includes a WebSocket endpoint that listens to new reports specifically within the Montenegro country area (admin_level 2, osm_id -53296).

## Features

- **Real-time Report Monitoring**: Continuously monitors for new reports with analysis in Montenegro
- **WebSocket Broadcasting**: Broadcasts new reports with analysis to all connected clients in real-time
- **Spatial Filtering**: Uses WKT conversion to filter reports within Montenegro's geographic boundaries
- **Analysis Integration**: Joins reports with report_analysis table to provide complete analysis data
- **Connection Management**: Handles multiple WebSocket connections with proper cleanup
- **Health Monitoring**: Provides health endpoints with connection statistics

## WebSocket Endpoints

### 1. Montenegro Reports WebSocket
- **URL**: `/ws/montenegro-reports`
- **Method**: GET (WebSocket upgrade)
- **Description**: Establishes a WebSocket connection to receive real-time reports from Montenegro

### 2. WebSocket Health Check
- **URL**: `/ws/health`
- **Method**: GET
- **Description**: Returns WebSocket service health and statistics

## Message Format

### Broadcast Message Structure
```json
{
  "type": "reports",
  "data": {
    "reports": [
      {
        "report": {
          "seq": 12345,
          "timestamp": "2024-01-15T10:30:00Z",
          "id": "user123",
          "team": 1,
          "latitude": 42.123456,
          "longitude": 19.123456,
          "x": 0.5,
          "y": 0.3,
          "action_id": "action_123"
        },
        "analysis": [
          {
            "seq": 12345,
            "source": "ai_analysis",
            "analysis_text": "Detailed analysis text...",
            "title": "Litter Detection",
            "description": "Multiple pieces of litter detected",
            "litter_probability": 0.85,
            "hazard_probability": 0.12,
            "severity_level": 3.5,
            "summary": "Moderate litter detected",
            "language": "en",
            "created_at": "2024-01-15T10:30:00Z"
          },
          {
            "seq": 12345,
            "source": "ai_analysis",
            "analysis_text": "Texto de análisis detallado...",
            "title": "Detección de Basura",
            "description": "Múltiples piezas de basura detectadas",
            "litter_probability": 0.85,
            "hazard_probability": 0.12,
            "severity_level": 3.5,
            "summary": "Basura moderada detectada",
            "language": "es",
            "created_at": "2024-01-15T10:30:00Z"
          }
        ]
      }
    ],
    "count": 1,
    "from_seq": 12345,
    "to_seq": 12345
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Health Response Structure
```json
{
  "status": "healthy",
  "service": "custom-area-dashboard-websocket",
  "timestamp": "2024-01-15T10:30:00Z",
  "connected_clients": 3,
  "last_broadcast_seq": 12345
}
```

## Usage Examples

### JavaScript WebSocket Client
```javascript
const ws = new WebSocket('ws://localhost:8080/ws/montenegro-reports');

ws.onopen = function(event) {
    console.log('Connected to Montenegro reports WebSocket');
};

ws.onmessage = function(event) {
    const message = JSON.parse(event.data);
    if (message.type === 'reports') {
        console.log('Received reports with analysis:', message.data.reports);
        message.data.reports.forEach(reportWithAnalysis => {
            console.log('Report:', reportWithAnalysis.report);
            console.log('Analyses:', reportWithAnalysis.analysis);
            
            // Handle multiple analyses per report
            reportWithAnalysis.analysis.forEach(analysis => {
                console.log(`Analysis in ${analysis.language}:`, analysis);
            });
        });
    }
};

ws.onclose = function(event) {
    console.log('WebSocket connection closed');
};
```

### Health Check
```bash
curl http://localhost:8080/ws/health
```

## Architecture

### Components

1. **WebSocket Hub** (`websocket/hub.go`)
   - Manages WebSocket connections
   - Handles client registration/unregistration
   - Broadcasts messages to all connected clients

2. **WebSocket Client** (`websocket/client.go`)
   - Individual WebSocket connection handler
   - Manages read/write pumps
   - Handles connection lifecycle

3. **WebSocket Service** (`services/websocket_service.go`)
   - Monitors database for new reports in Montenegro
   - Converts Montenegro area geometry to WKT format
   - Filters reports using spatial queries
   - Broadcasts new reports via WebSocket

4. **WebSocket Handler** (`handlers/websocket.go`)
   - HTTP handlers for WebSocket endpoints
   - Handles WebSocket connection upgrades
   - Provides health check endpoint

### Data Flow

1. **Initialization**:
   - Service loads Montenegro areas from GeoJSON
   - Finds Montenegro area (admin_level 2, osm_id -53296)
   - Initializes last processed sequence number

2. **Monitoring Loop**:
   - Polls database every 5 seconds for new reports with analysis
   - Filters reports using spatial query with Montenegro WKT
   - Joins with report_analysis table to get complete analysis data
   - Broadcasts new reports with analysis to all connected WebSocket clients

3. **WebSocket Broadcasting**:
   - Converts reports to JSON format
   - Sends broadcast messages to all connected clients
   - Updates connection statistics

## Configuration

### Environment Variables
- `PORT`: Service port (default: 8080)
- `HOST`: Service host (default: 0.0.0.0)
- `DB_HOST`: Database host (default: localhost)
- `DB_PORT`: Database port (default: 3306)
- `DB_USER`: Database user (default: server)
- `DB_PASSWORD`: Database password (default: secret_app)
- `DB_NAME`: Database name (default: cleanapp)

### Montenegro Area
- **Admin Level**: 2 (country level)
- **OSM ID**: -53296
- **Name**: Montenegro
- **Geometry**: Automatically loaded from GeoJSON file

## Testing

### Test Client
A test client is provided at `test_websocket_client.html` that demonstrates:
- WebSocket connection management
- Real-time report display
- Connection statistics
- Message logging

### Manual Testing
1. Start the service: `go run main.go`
2. Open `test_websocket_client.html` in a browser
3. Click "Connect" to establish WebSocket connection
4. Monitor for new reports in Montenegro

## Performance Considerations

- **Polling Interval**: 5 seconds (configurable)
- **Connection Limits**: No hard limit, but monitor memory usage
- **Message Buffering**: 256 message buffer per client
- **Spatial Queries**: Uses indexed spatial queries for efficiency

## Security Considerations

- **Origin Checking**: Currently allows all origins (configure for production)
- **Message Size**: Limited to 512 bytes per message
- **Connection Timeouts**: 60-second pong timeout
- **Write Timeouts**: 10-second write timeout

## Monitoring

### Logs
The service logs:
- WebSocket connection events
- Report processing statistics
- Error conditions
- Montenegro area detection

### Metrics
Available via `/ws/health`:
- Connected client count
- Last broadcast sequence number
- Service status and timestamp

## Dependencies

- `github.com/gorilla/websocket`: WebSocket implementation
- `github.com/gorilla/mux`: HTTP routing
- `github.com/go-sql-driver/mysql`: MySQL database driver
- `github.com/joho/godotenv`: Environment variable loading

## Integration with Existing Services

This WebSocket service integrates with:
- **Database Service**: For report queries and spatial filtering
- **Areas Service**: For Montenegro area geometry
- **WKT Converter**: For spatial query preparation

The service uses the same WKT conversion functions as the main backend, ensuring consistency in spatial operations. 