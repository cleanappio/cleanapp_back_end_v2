FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN if [ -f buildinfo.vars ]; then . ./buildinfo.vars; else CLEANAPP_BUILD_VERSION=dev; CLEANAPP_GIT_SHA=; CLEANAPP_BUILD_TIME=; fi && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X report-analyze-pipeline/version.BuildVersion=${CLEANAPP_BUILD_VERSION} -X report-analyze-pipeline/version.GitSHA=${CLEANAPP_GIT_SHA} -X report-analyze-pipeline/version.BuildTime=${CLEANAPP_BUILD_TIME}" -a -installsuffix cgo -o report-analyze-pipeline .

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
