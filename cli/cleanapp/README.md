# @cleanapp/cli

Production-grade CleanApp CLI (thin wrapper around the CleanApp API).

Wire status:
- machine-originated submission flows are Wire-native by default
- auth identity uses CleanApp Wire agent identity where available
- legacy `report-id` lookup still falls back to the v3 report listener API

- npm package name: `@cleanapp/cli`
- installed command: `cleanapp`
- default output: JSON (agent-friendly)
- `--human` for human-readable output
- `--dry-run` and `--trace` supported on all commands

## Install

```bash
npm i -g @cleanapp/cli
```

## First report, no key required (0.2.0)

```bash
cleanapp submit --desc "Describe the problem you actually observed" --receipt-file private-receipt.json
```

This sends one report to private review through the public adapter, without registration. The receipt does not mean publication, routing, resolution or a reward. Keep the mode-0600 receipt file private; it contains its tracking capability. The CLI retains the intent before sending and atomically replaces it with the receipt when available.

```bash
cleanapp status --receipt-id RECEIPT_ID_FROM_THE_FILE --tracking-token-file private-receipt.json
```

Optional `--title`, `--url`, `--lat`/`--lng` add known facts. Description-only input does not invent a location. Public intake accepts text and optional URL/coordinates; advanced source types, collected timestamps, rich media and bulk input need approved Wire credentials.

The CLI prints the private UUIDv4 `source_id` to stderr before the first POST. Retain it with the exact original options/payload for a retry using `--source-id`; never reuse a static example ID or public URL. Automatic blind POST retries are disabled. If the receipt file already exists, the command stops before submission. JSON output without a file returns the private token directly; human output also provides it, so changing output format never requires resubmission.

A broker failure can return HTTP503 with a retained private receipt: the CLI preserves it in the requested file or stdout and exits nonzero. Polling shows `pending_retry`; repeat the original UUID/payload to resume. Honor 429/503 Retry-After. A changed payload under a UUID returns 409. Tracking expires 30 days after original receipt creation; retries do not renew it.

## Approved integrations

`CLEANAPP_API_TOKEN` is optional for a first report. Existing approved keys continue to use the authenticated Wire path and enable bulk/rich submissions. Production self-registration may be disabled; enabled registration creates a pending identity without submission permission. A reusable integration key is shown once when issued and remains valid until revoked or suspended.

```bash
export CLEANAPP_API_URL="https://live.cleanapp.io"
# Supply CLEANAPP_API_TOKEN through your secret store for approved Wire access.
cleanapp auth whoami
```

`cleanapp init` configures approved integration credentials; it is unnecessary for public first reports. Config is written to `~/.cleanapp/config.json` with mode 0600 on Unix when a token is stored. Prefer environment variables or masked prompts for credentials.

Canonical onboarding: https://www.cleanapp.io/agents. Wire/OpenAPI: https://live.cleanapp.io/api/v1/openapi.yaml.

## API Base URL Resolution

For every request, the CLI resolves API base URL in this order:

1. `--api-url`
2. `CLEANAPP_API_URL`
3. `~/.cleanapp/config.json`
4. default: `https://live.cleanapp.io`

Token resolution order:

1. `CLEANAPP_API_TOKEN`
2. `~/.cleanapp/config.json` (if you stored a token)

## Commands

### Auth

```bash
cleanapp auth whoami
```

Calls `GET /api/v1/agents/me` and falls back to `GET /v1/fetchers/me` for older APIs.

### Submit One Report

```bash
cleanapp submit --title "Broken login" --desc "Users stuck on OAuth callback" --source-type web
```

Optional location:

```bash
cleanapp submit --title "Hazard" --desc "Broken glass" --lat 47.37 --lng 8.54
```

Dry-run (prints the HTTP payload; sends nothing):

```bash
cleanapp submit --dry-run --title "Test" --desc "No send"
```

This submits a single `cleanapp-wire.v1` envelope to:

- `POST /api/v1/agent-reports:submit`

### Bulk Submit From File (Wire-native)

```bash
cleanapp bulk-submit --file reports.ndjson
```

Supported input formats:

- `.ndjson` / `.jsonl`: one JSON object per line
- `.json`: either an array of items, or `{ "items": [...] }`
- `.csv`: header row, maps common fields like `source_id,title,description,lat,lng,...`

If the file already contains full Wire envelopes, the CLI submits them as-is.
If the file contains the older simple item shape, the CLI wraps each item into a valid `cleanapp-wire.v1` envelope automatically.

### Status

```bash
cleanapp status --source-id bluesky:at://did:plc:.../app.bsky.feed.post/...
cleanapp status --receipt-id rcpt_123...
cleanapp status --report-id 123456
```

Mappings:

- `--source-id` -> `GET /api/v1/agent-reports/status/{source_id}`
- `--receipt-id` -> `GET /api/v1/agent-reports/receipts/{receipt_id}`
- `--report-id` -> `GET /api/v3/reports/by-seq?seq=...` (legacy fallback)

### Presign (API Gap)

```bash
cleanapp presign --file ./image.jpg
```

This CLI supports the command, but **the backend currently does not expose** `POST /v1/media:presign`.
When the endpoint is added server-side, the CLI will work without changes.

### Metrics (API Gap With Fallback)

```bash
cleanapp metrics --since 24h --group-by hour
```

Attempts `GET /v1/fetchers/me/metrics`. If missing, falls back to `GET /v1/fetchers/me`.

## Config Commands

```bash
cleanapp config path
cleanapp config get
cleanapp config get apiUrl
cleanapp config set apiUrl https://live.cleanapp.io
cleanapp config set output human
cleanapp config set env prod
cleanapp logout
```

Token setting requires an explicit flag and masked input:

```bash
cleanapp config set token --token
```

## Debugging

Trace mode writes to stderr and redacts secrets:

```bash
cleanapp --trace auth whoami
```

## Publishing

Check name availability:

```bash
cd cli/cleanapp
npm run check-name
```

Publish (scoped packages default private unless you set access public):

```bash
cd cli/cleanapp
npm publish --access public
```

## Local Dev

```bash
cd cli/cleanapp
npm install
npm test
npm pack

# build + run help
npm run build
node ./bin/cleanapp --help
```
