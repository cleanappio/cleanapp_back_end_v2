#!/usr/bin/env bash
set -euo pipefail

# Minimal smoke tests against public endpoints.
# Intended to fail fast if prod is down after a deploy.

req() {
  local url="$1"
  local code
  code="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 10 "$url" || true)"
  printf '%s\t%s\n' "$url" "$code"
  [[ "$code" == "200" ]]
}

req "https://live.cleanapp.io/api/v3/reports/health"
req "https://live.cleanapp.io/api/v4/health"
req "https://live.cleanapp.io/api/v4/openapi.json"

