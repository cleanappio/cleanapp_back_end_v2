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
WORKDIR /srv
COPY --from=builder /app/target/release/report-listener-v4 /usr/local/bin/report-listener-v4
ENV HTTP_PORT=9084
ENV RUST_LOG=info
ENV RUST_BACKTRACE=1
CMD ["/usr/local/bin/report-listener-v4"]



