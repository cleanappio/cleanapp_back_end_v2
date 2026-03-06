#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"

services=(
  "auth-service"
  "customer-service"
  "report-listener"
  "report-analyze-pipeline"
  "report-processor"
  "gdpr-process-service"
)

if [[ $# -gt 0 ]]; then
  services=("$@")
fi

for service in "${services[@]}"; do
  echo "== migrate: ${service} =="
  (
    cd "${ROOT_DIR}/${service}"
    go run ./cmd/migrate
  )
  echo
done
