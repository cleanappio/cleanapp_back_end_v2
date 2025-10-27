FROM rust:1.81 as builder
WORKDIR /app
COPY Cargo.toml .
COPY src ./src
RUN cargo build --release

FROM debian:bullseye-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /srv
COPY --from=builder /app/target/release/report-listener-v4 /usr/local/bin/report-listener-v4
ENV HTTP_PORT=9084
CMD ["/usr/local/bin/report-listener-v4"]


