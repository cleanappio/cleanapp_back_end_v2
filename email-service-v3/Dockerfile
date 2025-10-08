# Multi-stage build for Rust email-service-v3

FROM rust:1.82-bullseye AS builder

WORKDIR /app

# Cache deps
COPY Cargo.toml Cargo.lock ./
RUN mkdir -p src && echo "fn main(){}" > src/main.rs && cargo build --release || true

# Build actual sources
COPY src ./src
RUN cargo build --release --bins

FROM debian:bullseye-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app

# Copy binaries
COPY --from=builder /app/target/release/email-service-v3 /usr/local/bin/email-service-v3
COPY --from=builder /app/target/release/email-service-v3-backfill /usr/local/bin/email-service-v3-backfill

ENV RUST_LOG=info
ENV ENABLE_EMAIL_V3=false

# Default command runs the service; backfill can be invoked by overriding CMD
CMD ["/usr/local/bin/email-service-v3"]

