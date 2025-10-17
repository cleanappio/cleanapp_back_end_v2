# Multi-stage build for Rust report-tags service

FROM rust:1.85-alpine AS builder

# Install build dependencies
RUN apk add --no-cache musl-dev pkgconfig openssl-dev

WORKDIR /app

# Cache deps
COPY Cargo.toml ./
RUN mkdir -p src && echo "fn main(){}" > src/main.rs && cargo build --release || true

# Build actual sources
COPY src ./src
RUN cargo build --release --bin report-tags

FROM debian:bullseye-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app

# Copy binary
COPY --from=builder /app/target/release/report-tags /usr/local/bin/report-tags

ENV RUST_LOG=info

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["/usr/local/bin/report-tags"]
