# Build stage
FROM golang:1.24-alpine AS builder

# Install git and ca-certificates
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN if [ -f buildinfo.vars ]; then . ./buildinfo.vars; else CLEANAPP_BUILD_VERSION=dev; CLEANAPP_GIT_SHA=; CLEANAPP_BUILD_TIME=; fi && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X report-listener/version.BuildVersion=${CLEANAPP_BUILD_VERSION} -X report-listener/version.GitSHA=${CLEANAPP_GIT_SHA} -X report-listener/version.BuildTime=${CLEANAPP_BUILD_TIME}" -a -installsuffix cgo -o report-listener .

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/report-listener .

# Change ownership to non-root user
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./report-listener"] 
