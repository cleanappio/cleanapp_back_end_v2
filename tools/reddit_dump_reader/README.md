# reddit_dump_reader

A lightweight tool to stream Reddit monthly dumps (comments + submissions) and push them to CleanApp's `/api/v3/reports/bulk_ingest` endpoint.

The tool only applies the optional allowlist/keyword filters described below; it does **not** attempt to pre-screen for CleanApp-style reports. Classification and downstream analysis remain the responsibility of the backend after ingestion.

## Usage

```bash
# Dry run to inspect converted items
CLEANAPP_BACKEND_URL=https://backend.example.com \
CLEANAPP_FETCHER_TOKEN=secret \
./target/release/reddit_dump_reader \
  --inputs https://files.pushshift.io/reddit/comments/RC_2024-05.zst \
  --inputs https://files.pushshift.io/reddit/submissions/RS_2024-05.zst \
  --mode both \
  --max-items 5 \
  --dry-run

# Live ingest with batching and filtering
./target/release/reddit_dump_reader \
  --inputs RC_2024-06.zst RS_2024-06.zst \
  --backend-url https://backend.example.com \
  --fetcher-token secret \
  --batch-size 1000 \
  --concurrency 8 \
  --subreddit-allowlist allow.txt \
  --keyword-file keywords.txt
```

Flags:
- `--inputs <path-or-url>` (repeatable) accepts gzip/zstd/xz or plain NDJSON.
- `--mode comments|submissions|both` to pick record types.
- `--max-items` to cap ingestion (helpful for smoke tests).
- `--batch-size` (default 1000) and `--concurrency` (default 8).
- `--subreddit-allowlist` and `--keyword-file` provide simple gating.
- `--dry-run` prints converted items instead of posting.

Environment variables:
- `CLEANAPP_BACKEND_URL`
- `CLEANAPP_FETCHER_TOKEN`
