#!/usr/bin/env bash
set -euo pipefail

# Capture public health/version endpoints into an xray folder (no SSH required).
#
# Usage:
#   ./platform_blueprint/tests/smoke/capture_prod_public.sh [YYYY-MM-DD] [OUTDIR]
#
# Defaults:
#   DATE   = today
#   OUTDIR = xray/prod/<DATE>-public

DATE="${1:-$(date +%F)}"
OUTDIR="${2:-$(pwd)/xray/prod/${DATE}-public}"

mkdir -p "${OUTDIR}/responses"

status_tsv="${OUTDIR}/_status.tsv"
printf "url\thttp_code\tbytes\n" > "${status_tsv}"

urls=(
  # Health + contract
  "https://live.cleanapp.io/api/v3/reports/health"
  "https://live.cleanapp.io/api/v4/health"
  "https://live.cleanapp.io/api/v4/openapi.json"

  # Provenance (public)
  "https://live.cleanapp.io/version"
  "https://live.cleanapp.io/api/v4/version"
  "https://api.cleanapp.io/version"
  "https://auth.cleanapp.io/version"
  "https://areas.cleanapp.io/version"
  "https://email.cleanapp.io/version"
  "https://renderer.cleanapp.io/version"
  "https://tags.cleanapp.io/version"
  "https://voice.cleanapp.io/version"

  # Public HTTP listener for api.cleanapp.io -> cleanapp_service (if firewall allows 8080).
  "http://api.cleanapp.io:8080/version"
)

for url in "${urls[@]}"; do
  fname="$(printf '%s' "$url" | sed -E 's#^https?://##; s#[^A-Za-z0-9._-]+#_#g')"
  out="${OUTDIR}/responses/${fname}"
  code="$(curl -sS --max-time 10 -o "${out}" -w "%{http_code}" "${url}" || true)"
  bytes="$(wc -c < "${out}" 2>/dev/null || echo 0)"
  printf "%s\t%s\t%s\n" "${url}" "${code}" "${bytes}" >> "${status_tsv}"
done

echo "[capture] wrote ${status_tsv}"
echo "[capture] wrote ${OUTDIR}/responses/"

