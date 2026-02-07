FROM golang:1.24-alpine AS builder

WORKDIR /src

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Full source required for module build
COPY . .

# Optional build metadata injected by build_image.sh via docker_backend/buildinfo.vars
RUN if [ -f docker_backend/buildinfo.vars ]; then . ./docker_backend/buildinfo.vars; else CLEANAPP_BUILD_VERSION=dev; CLEANAPP_GIT_SHA=; CLEANAPP_BUILD_TIME=; fi && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X cleanapp/common/version.BuildVersion=${CLEANAPP_BUILD_VERSION} -X cleanapp/common/version.GitSHA=${CLEANAPP_GIT_SHA} -X cleanapp/common/version.BuildTime=${CLEANAPP_BUILD_TIME}" -o /out/service backend/main.go

FROM --platform=linux/amd64 ubuntu:22.04
RUN echo 'APT::Install-Suggests "0";' >> /etc/apt/apt.conf.d/00-docker
RUN echo 'APT::Install-Recommends "0";' >> /etc/apt/apt.conf.d/00-docker
RUN DEBIAN_FRONTEND=noninteractive \
   apt-get update \
   && rm -rf /var/lib/apt/lists/*

USER root

RUN apt-get update
RUN apt-get install -y ca-certificates

COPY --from=builder /out/service ./service

# Copy certificates to connect to the ethereum network
COPY docker_backend/certificates/* /usr/local/share/ca-certificates/
RUN update-ca-certificates

EXPOSE 8080/tcp
CMD "./service" \
   "--mysql_password=${MYSQL_APP_PASSWORD}" \
   "--mysql_host=cleanapp_db" \
   "--eth_network_url_main=${ETH_NETWORK_URL_MAIN}" \
   "--eth_private_key_main=${KITN_PRIVATE_KEY_MAIN}" \
   "--contract_address_main=${CONTRACT_ADDRESS_MAIN}" \
   "--solver_url=${SOLVER_URL}"

