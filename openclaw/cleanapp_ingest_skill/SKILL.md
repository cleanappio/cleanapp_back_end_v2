# SKILL: CleanApp Ingest v1

Submit bulk reports to CleanApp using a Fetcher API key (`/v1/reports:bulkIngest`).

## Required Secret

- `CLEANAPP_API_TOKEN` (Bearer token). Obtain once via `POST /v1/fetchers/register`, then store in ClawHub/OpenClaw secrets.

## Data Handling

This skill can submit:
- Text: `title`, `description`
- Optional location: `lat`, `lng`
- Optional media metadata: `media[]` (URL/SHA/content-type)

Safe defaults for reduced sensitivity:
- `--approx-location` (round coordinates)
- `--no-media` (drop media metadata)

## Dry Run

`--dry-run` prints the exact payload and exits without making any network call.

## Commands

```bash
# Real submit (recommended defaults)
export CLEANAPP_API_TOKEN="cleanapp_fk_live_..."
python3 ingest.py --base-url https://live.cleanapp.io --input examples/sample_items.json --approx-location --no-media

# Dry run (no network)
python3 ingest.py --input examples/sample_items.json --dry-run
```

