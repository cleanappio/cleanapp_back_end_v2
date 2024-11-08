FROM rust:1.81 AS builder

WORKDIR /usr/src/stxn-poc
COPY . .
RUN cargo install --path .

FROM --platform=linux/amd64 ubuntu:22.04

RUN echo 'APT::Install-Suggests "0";' >> /etc/apt/apt.conf.d/00-docker
RUN echo 'APT::Install-Recommends "0";' >> /etc/apt/apt.conf.d/00-docker
RUN DEBIAN_FRONTEND=noninteractive \
   apt-get update \
   && rm -rf /var/lib/apt/lists/*
 
USER root

RUN apt-get update
RUN apt-get install -y ca-certificates

# Copy certificates to connect to the ethereum network
COPY certificates/* /usr/local/share/ca-certificates/
RUN update-ca-certificates

COPY --from=builder /usr/local/cargo/bin/stxn_kickoff /usr/local/bin/stxn_kickoff

CMD \
  "stxn_kickoff" \
  "--chain-id=${CHAIN_ID}" \
  "--ws-chain-url=${WS_CHAIN_URL}" \
  "--laminator-address=${LAMINATOR_ADDRESS}" \
  "--call-breaker-address=${CALL_BREAKER_ADDRESS}" \
  "--kitn-disbursement-scheduler-address=${KITN_DISBURSEMENT_SCHEDULER_ADDRESS}" \
  "--cleanapp-wallet-private-key=${CLEANAPP_WALLET_PRIVATE_KEY}" \
  "--cron-schedule=${DISBURSEMENT_SHADOW_SCHEDULE}"

