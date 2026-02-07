# Multi-stage build for Rust report-tags service

FROM rust:1.85-bullseye AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config \
    libssl-dev \
    ca-certificates \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy full sources so we can optionally ingest buildinfo.vars produced by build_image.sh
COPY . .

# Build binary (embed provenance if buildinfo.vars is present)
RUN set -a; \
    if [ -f buildinfo.vars ]; then . ./buildinfo.vars; else CLEANAPP_BUILD_VERSION="$(cat build_version.txt 2>/dev/null || echo dev)"; CLEANAPP_GIT_SHA=; CLEANAPP_BUILD_TIME=; fi; \
    set +a; \
    cargo build --release --bin report-tags

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
