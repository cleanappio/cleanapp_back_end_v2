FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN if [ -f buildinfo.vars ]; then . ./buildinfo.vars; else CLEANAPP_BUILD_VERSION=dev; CLEANAPP_GIT_SHA=; CLEANAPP_BUILD_TIME=; fi && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X custom-area-dashboard/version.BuildVersion=${CLEANAPP_BUILD_VERSION} -X custom-area-dashboard/version.GitSHA=${CLEANAPP_GIT_SHA} -X custom-area-dashboard/version.BuildTime=${CLEANAPP_BUILD_TIME}" -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./main"]
