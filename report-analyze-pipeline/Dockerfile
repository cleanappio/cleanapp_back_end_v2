FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o report-analyze-pipeline .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/report-analyze-pipeline .

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./report-analyze-pipeline"] 