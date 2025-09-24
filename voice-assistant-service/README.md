# Voice Assistant Service

A minimal microservice that acts as a security proxy for OpenAI API calls, providing voice assistant functionality for mobile applications.

## Features

- **OpenAI Integration**: Secure proxy for OpenAI Chat Completions API
- **Streaming Responses**: Real-time streaming of AI responses
- **CORS Support**: Cross-origin requests enabled
- **Health Monitoring**: Built-in health check endpoint
- **Docker Support**: Containerized deployment

## API Endpoints

### Health Check

- **GET** `/health` - Service health status

### Voice Assistant

- **POST** `/api/assistant` - Main voice assistant endpoint with streaming

#### Request Body

```json
{
  "prompt": "How do I upload my documents?"
}
```

#### Response

Streams text chunks in real-time:

```
{"content": "To upload your documents, "}
{"content": "navigate to the Files section "}
{"content": "and tap the upload button."}
{"done": true}
```

## Environment Variables

- `PORT` - Server port (default: 8080)
- `OPENAI_API_KEY` - OpenAI API key (required)
- `OPENAI_MODEL` - OpenAI model to use (default: gpt-4o)

## Development

### Local Development
```bash
# Set environment variables
export OPENAI_API_KEY="your-api-key"

# Run the service
go run main.go
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

## Usage Example

```bash
curl -X POST http://localhost:8080/api/assistant \
  -H "Content-Type: application/json" \
  -d '{"prompt": "How do I change my password?"}'
```
```

### **Step 12: Initialize Dependencies**

**File: `go.mod`**
```go
module voice-assistant-service

go 1.24.0

require (
    github.com/apex/log v1.9.0
    github.com/gin-gonic/gin v1.10.0
)
```

Run:
```bash
go mod tidy
```

## 🚀 **Quick Start Commands**

1. **Create the service**:
   ```bash
   mkdir voice-assistant-service
   cd voice-assistant-service
   ```

2. **Initialize and build**:
   ```bash
   go mod init voice-assistant-service
   # Add the files above
   go mod tidy
   ```

3. **Test locally**:
   ```bash
   export OPENAI_API_KEY="your-api-key"
   go run main.go
   ```

4. **Test the API**:
   ```bash
   curl -X POST http://localhost:8080/api/assistant \
     -H "Content-Type: application/json" \
     -d '{"prompt": "Hello, how can you help me?"}'
   ```

5. **Build and deploy**:
   ```bash
   chmod +x build_image.sh
   ./build_image.sh -e dev
   ```

## 🔧 **Key Simplifications Made**

- ❌ **Removed**: Database layer (no conversation history)
- ❌ **Removed**: Authentication middleware (no auth required)
- ❌ **Removed**: Complex models (minimal request/response)
- ❌ **Removed**: Database utilities (no DB needed)
- ✅ **Kept**: Core OpenAI integration with streaming
- ✅ **Kept**: CORS support for mobile app
- ✅ **Kept**: Health check endpoint
- ✅ **Kept**: Docker containerization
- ✅ **Kept**: Build and deployment scripts

This minimal implementation focuses purely on being a secure proxy for OpenAI API calls with streaming support, perfect for your mobile app integration.