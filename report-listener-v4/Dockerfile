FROM rust:1.82-bullseye AS builder

WORKDIR /app

# Build with full sources (avoid accidental stub carryover)
COPY . .
RUN set -a; \
    if [ -f buildinfo.vars ]; then . ./buildinfo.vars; else CLEANAPP_BUILD_VERSION=dev; CLEANAPP_GIT_SHA=; CLEANAPP_BUILD_TIME=; fi; \
    set +a; \
    cargo build --release --bins

FROM debian:bullseye-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /srv
COPY --from=builder /app/target/release/report-listener-v4 /usr/local/bin/report-listener-v4
ENV HTTP_PORT=9084
ENV RUST_LOG=info
ENV RUST_BACKTRACE=1
ENTRYPOINT ["/usr/local/bin/report-listener-v4"]

