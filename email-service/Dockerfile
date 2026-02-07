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
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X email-service/version.BuildVersion=${CLEANAPP_BUILD_VERSION} -X email-service/version.GitSHA=${CLEANAPP_GIT_SHA} -X email-service/version.BuildTime=${CLEANAPP_BUILD_TIME}" -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Copy templates for opt-out pages
COPY --from=builder /app/templates ./templates

# Run the application
EXPOSE 8080
CMD ["./main"] 
