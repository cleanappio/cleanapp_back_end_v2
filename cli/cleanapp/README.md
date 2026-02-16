# @cleanapp/cli

Production-grade CleanApp CLI (thin wrapper around the CleanApp API).

- npm package name: `@cleanapp/cli`
- installed command: `cleanapp`
- default output: JSON (agent-friendly)
- `--human` for human-readable output
- `--dry-run` and `--trace` supported on all commands

## Install

```bash
npm i -g @cleanapp/cli
```

## Getting Started (Humans)

```bash
cleanapp init
cleanapp auth whoami
```

`cleanapp init` writes local config to:

- `~/.cleanapp/config.json`

If you choose to store a token locally, the CLI will attempt to chmod the config file to `0600` on unix-like systems.

## Headless / Agents (Recommended)

Use env vars only (no local config needed):

```bash
export CLEANAPP_API_URL="https://live.cleanapp.io"
export CLEANAPP_API_TOKEN="cleanapp_fk_live_..."

cleanapp auth whoami
```

Security note: never paste tokens into chat logs or shell history. Prefer env vars or masked prompts.

## API Base URL Resolution

For every request, the CLI resolves API base URL in this order:

1. `--api-url`
2. `CLEANAPP_API_URL`
3. `~/.cleanapp/config.json`
4. default: `https://live.cleanapp.io`

Token resolution order:

1. `CLEANAPP_API_TOKEN`
2. `~/.cleanapp/config.json` (if you stored a token)

## Commands (v1 MVP)

### Auth

```bash
cleanapp auth whoami
```

Calls `GET /v1/fetchers/me`.

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

### Bulk Submit From File

```bash
cleanapp bulk-submit --file reports.ndjson
```

Supported input formats:

- `.ndjson` / `.jsonl`: one JSON object per line
- `.json`: either an array of items, or `{ "items": [...] }`
- `.csv`: header row, maps common fields like `source_id,title,description,lat,lng,...`

### Status

```bash
cleanapp status --report-id 123456
```

Currently maps to `GET /api/v3/reports/by-seq?seq=...` (report listener).

`--source-id` is not supported yet because there is no API endpoint to query by `(fetcher_id, source_id)`.

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

