#!/bin/sh
set -e

echo "email-fetcher container starting at $(date -u +%FT%TZ)"
echo "ENABLE_EMAIL_FETCHER=${ENABLE_EMAIL_FETCHER:-}"
echo "DB_HOST=${DB_HOST:-} DB_PORT=${DB_PORT:-} DB_USER=${DB_USER:-} DB_NAME=${DB_NAME:-}"
echo "OPENAI_MODEL=${OPENAI_MODEL:-} OPENAI_API_KEY_SET=$([ -n "$OPENAI_API_KEY" ] && echo yes || echo no)"

# Replace the shell so Docker stop signals reach the Rust process.
exec /email-fetcher "$@"
