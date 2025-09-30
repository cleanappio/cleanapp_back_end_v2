# Voice Assistant Service

A microservice that provides secure access to OpenAI's Realtime API for voice-based interactions. This service acts as a security proxy, minting ephemeral tokens for clients to establish direct WebRTC connections with OpenAI.

## Features

- **OpenAI Realtime Integration**: Secure proxy for OpenAI Realtime API with WebRTC support
- **Ephemeral Token Minting**: Creates short-lived tokens for secure client connections
- **WebRTC Proxy**: Optional proxy for WebRTC offer/answer exchange
- **Rate Limiting**: Built-in rate limiting to prevent abuse
- **Authentication**: JWT-based authentication for secure access
- **CORS Support**: Configurable cross-origin request handling
- **Health Monitoring**: Built-in health check endpoint
- **Docker Support**: Containerized deployment

## API Endpoints

### Health Check

- **GET** `/health` - Service health status (no auth required)

### Session Management

- **POST** `/session` - Create ephemeral OpenAI Realtime session (rate limited)
- **GET** `/session/prewarm` - Prewarm session for faster connection (rate limited)

### WebRTC Proxy (Optional)

- **POST** `/webrtc/proxy-offer` - Proxy WebRTC offer to OpenAI (rate limited)

## API Documentation

### Create Session

**POST** `/session`

Creates an ephemeral OpenAI Realtime session and returns the client secret for direct WebRTC connection.

#### Request Body

```json
{
  "model": "gpt-4o-realtime-preview",
  "voice": "alloy",
  "metadata": {
    "client_app": "mobile-app-v1"
  }
}
```

#### Response

```json
{
  "session_id": "sess_abc123...",
  "client_secret": {
    "value": "ephemeral_token_here",
    "expires_at": "2025-01-26T10:30:00Z"
  },
  "expires_at": "2025-01-26T10:30:00Z",
  "ice_servers": [
    {
      "urls": ["stun:stun.l.google.com:19302"]
    }
  ]
}
```

### Proxy WebRTC Offer

**POST** `/webrtc/proxy-offer`

Proxies a WebRTC offer to OpenAI and returns the answer SDP.

#### Request Body

```json
{
  "session_id": "sess_abc123...",
  "ephemeral_key": "ephemeral_token_here",
  "offer_sdp": "v=0\r\no=- 1234567890 1234567890 IN IP4 127.0.0.1\r\n..."
}
```

#### Response

Returns the answer SDP as plain text:

```
v=0
o=- 9876543210 9876543210 IN IP4 127.0.0.1
...
```

## Environment Variables

- `PORT` - Server port (default: 8080)
- `TRASHFORMER_OPENAI_API_KEY` - OpenAI API key (required)
- `OPENAI_MODEL` - OpenAI model to use (default: gpt-4o-realtime-preview)
- `ALLOWED_ORIGINS` - CORS allowed origins (default: \*)
- `RATE_LIMIT_PER_MINUTE` - Rate limit per user (default: 10)
- `TURN_SERVERS_JSON` - Optional TURN servers configuration

## Development

### Local Development

```bash
# Set environment variables
export TRASHFORMER_OPENAI_API_KEY="your-api-key"
export RATE_LIMIT_PER_MINUTE=10

# Run the service
go run main.go
```

### Run Tests

```bash
go test ./...
```

### Docker Development

```bash
# Build and run with Docker Compose
docker-compose up --build
```

## Deployment

### Build and Deploy

```bash
# Build for dev environment
./build_image.sh -e dev

# Build for prod environment
./build_image.sh -e prod
```

## Usage Examples

### Create Session

```bash
curl -X POST http://localhost:8080/session \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-realtime-preview",
    "voice": "alloy",
    "metadata": {
      "client_app": "mobile-app-v1"
    }
  }'
```

### Health Check

```bash
curl http://localhost:8080/health
```

## Security Considerations

- **Ephemeral Tokens**: Only short-lived tokens are returned to clients
- **No Secret Logging**: Client secrets are never logged or stored
- **Rate Limiting**: Built-in IP-based protection against abuse
- **No Authentication**: Endpoints are accessible without authentication for mobile app compatibility
- **HTTPS**: Use HTTPS in production for secure communication
- **API Key Protection**: OpenAI API key is kept secure on the server

## Integration with React Native

1. **Get Ephemeral Token**: Call `/session` endpoint (no authentication required)
2. **Establish WebRTC**: Use the ephemeral token to connect directly to OpenAI
3. **Handle Audio**: Implement WebRTC audio capture and playback
4. **Error Handling**: Implement token refresh on 401/403 errors

## Monitoring

The service logs structured events for monitoring:

- `session.create.request` - Session creation attempts
- `session.create.success` - Successful session creation
- `session.create.error` - Session creation failures
- `webrtc.proxy_offer.request` - WebRTC proxy requests
- `webrtc.proxy_offer.success` - Successful WebRTC proxy

## Troubleshooting

- **429 Too Many Requests**: Rate limit exceeded, implement backoff
- **502 Bad Gateway**: OpenAI API issues, check API key and network
- **WebRTC Connection Issues**: Check ICE servers and network configuration
