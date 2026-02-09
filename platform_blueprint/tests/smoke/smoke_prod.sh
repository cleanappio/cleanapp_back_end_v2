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

# Build provenance (must be safe to expose publicly).
req "https://live.cleanapp.io/version"               # report-listener (v3)
req "https://live.cleanapp.io/api/v4/version"        # report-listener-v4 (v4 alias)
req "https://api.cleanapp.io/version"                # customer-service
req "https://auth.cleanapp.io/version"               # auth-service
req "https://areas.cleanapp.io/version"              # areas-service
req "https://email.cleanapp.io/version"              # email-service
req "https://renderer.cleanapp.io/version"           # report-fast-renderer
req "https://tags.cleanapp.io/version"               # report-tags
req "https://voice.cleanapp.io/version"              # voice-assistant-service
