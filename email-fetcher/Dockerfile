FROM rust:1.82 as builder
ARG CACHE_BUST=0
RUN echo "CACHE_BUST=${CACHE_BUST}"
WORKDIR /app
# Copy full source (avoid dummy main to prevent stale binaries)
COPY . .
RUN cargo build --release --locked

# Use a minimal Debian runtime to ensure glibc, stdout/stderr, and certs are present
FROM debian:bookworm-slim
WORKDIR /
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/target/release/email-fetcher /email-fetcher
ENV RUST_LOG=info
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]


