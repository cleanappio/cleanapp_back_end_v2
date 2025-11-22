Analyzer manual reconciliation tool

This small Rust CLI re-submits reports to the CleanApp backend so they are re-queued for analysis.

It reads a CSV export with header:

seq,ts,id,team,latitude,longitude,x,y,image,action_id,description

- Only id, latitude, longitude, x, y are required.
- image is expected to be a base64 string if present.
- action_id and description (used as annotation) are optional.

Build

1. cd cleanapp_back_end_v2/scripts/analyzer-manual-reconciliation
2. cargo build --release

Run

By default it looks for cleanapp_reports.csv in the current directory and posts to http://api.cleanapp.io:8080/report

Example:

```
./target/release/analyzer-manual-reconciliation \
  --csv cleanapp_reports.csv \
  --api-url http://api.cleanapp.io:8080 \
  --concurrency 2 \
  --max-retries 3 \
  --initial-backoff 500ms
```

Safety (dry-run)

Use --dry-run to print what would be submitted without sending anything:

```
./target/release/analyzer-manual-reconciliation --dry-run
```

Notes

- The endpoint is POST /report (version must be "2.0"), response includes {"seq": <number>}.
- The tool retries on 5xx and transient network errors with exponential backoff and jitter.
- Concurrency is modest by default (2) to avoid overwhelming the API.
- Images: if a CSV image field starts with a data URL prefix (e.g. "data:image/jpeg;base64,..."), the tool strips the prefix. It then validates base64; invalid base64 is skipped to avoid 400 errors from the API.
- By default, rows with missing/invalid images or missing id are skipped (see --skip-on-image-error; defaults to true). The summary prints Skipped count. 


