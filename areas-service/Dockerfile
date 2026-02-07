FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN if [ -f buildinfo.vars ]; then . ./buildinfo.vars; else CLEANAPP_BUILD_VERSION=dev; CLEANAPP_GIT_SHA=; CLEANAPP_BUILD_TIME=; fi && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X areas-service/version.BuildVersion=${CLEANAPP_BUILD_VERSION} -X areas-service/version.GitSHA=${CLEANAPP_GIT_SHA} -X areas-service/version.BuildTime=${CLEANAPP_BUILD_TIME}" -a -installsuffix cgo -o areas-service .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/areas-service .

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./areas-service"] 
