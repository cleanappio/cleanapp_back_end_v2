FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the entire backend directory
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . ./

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o service ./main.go

# Final image
FROM ubuntu:22.04
RUN echo 'APT::Install-Suggests "0";' >> /etc/apt/apt.conf.d/00-docker
RUN echo 'APT::Install-Recommends "0";' >> /etc/apt/apt.conf.d/00-docker
RUN DEBIAN_FRONTEND=noninteractive apt-get update && rm -rf /var/lib/apt/lists/*

USER root
RUN apt-get update && apt-get install -y ca-certificates

COPY --from=builder /app/service /service

EXPOSE 8080/tcp
CMD ["/service"]
