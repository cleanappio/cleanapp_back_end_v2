# Multi-stage build for Rust report-tags service

FROM rust:1.85-bullseye AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config \
    libssl-dev \
    ca-certificates \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy cache-busting build version file (forces rebuild on each new version)
COPY build_version.txt ./build_version.txt

# Copy project files
COPY Cargo.toml Cargo.lock ./
COPY src ./src

# Build binary
RUN cargo build --release --bin report-tags

# Sanity-check the produced binary is our server (and not a tiny placeholder)
RUN test -f target/release/report-tags \
  && SIZE=$(stat -c%s target/release/report-tags) \
  && echo "Built report-tags size: $SIZE bytes" \
  && if [ "$SIZE" -lt 3000000 ]; then \
       echo "ERROR: report-tags binary too small; likely wrong build"; \
       ls -l target/release/; \
       ldd target/release/report-tags || true; \
       exit 1; \
     fi \
  && grep -a "Report Tags Service Starting" target/release/report-tags >/dev/null || (echo "ERROR: Expected startup string not found in binary"; exit 1)

FROM debian:bullseye-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
WORKDIR /app

# Copy binary
COPY --from=builder /app/target/release/report-tags /usr/local/bin/report-tags

ENV RUST_LOG=info

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS http://localhost:8080/health || exit 1

# Run the application
CMD ["/usr/local/bin/report-tags"]
