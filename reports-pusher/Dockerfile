FROM rust:1.83 AS builder

WORKDIR /usr/src/reports-pusher
# Install protoc for tonic-build
RUN apt-get update && apt-get install -y protobuf-compiler && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo install --path .

FROM --platform=linux/amd64 debian:12-slim

RUN echo 'APT::Install-Suggests "0";' >> /etc/apt/apt.conf.d/00-docker
RUN echo 'APT::Install-Recommends "0";' >> /etc/apt/apt.conf.d/00-docker
RUN DEBIAN_FRONTEND=noninteractive \
   apt-get update \
   && apt-get install -y --no-install-recommends ca-certificates \
   && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/cargo/bin/reports-pusher /usr/local/bin/reports-pusher

CMD \
  "reports-pusher" \
  "--mysql-url=${MYSQL_URL}" \
  "--request-registrator-url=${REQUEST_REGISTRATOR_URL}" \
  "--app-id-hex=${APP_ID_HEX}" \
  "--chain-id=${CHAIN_ID}" \
  "--poll-secs=${POLL_SECS}" 