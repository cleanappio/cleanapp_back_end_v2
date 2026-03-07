# CleanApp Ingest Skill (OpenClaw/ClawHub)

This is a **self-contained skill package** for submitting machine-originated reports to **CleanApp Wire**.

Security-scan goals (ClawHub):
- Required secret is **declared** (no hardcoded tokens).
- All referenced scripts are **included** in the package.
- No dynamic `curl|bash` / remote script fetching at runtime.
- Explicit, minimal data-handling guidance.
- Real `--dry-run` mode (prints payload; does not send).

## Required Secret

- `CLEANAPP_API_TOKEN`: a CleanApp agent key (Bearer token) issued by `POST /api/v1/agents/register`.

Do not paste the token into chat logs. Store it as a secret in ClawHub/OpenClaw.

## Data Handling (Minimal by Default)

This skill can submit:
- `title`, `description` (text)
- optional `lat`/`lng` (location)
- optional media metadata (URL/SHA/content-type)

Recommended safe defaults:
- use `--approx-location` (round coordinates)
- use `--no-media` unless you truly need it

## Dry Run

`--dry-run` prints the exact JSON payload that would be sent and exits 0 without any network calls.

## Usage

### 1) Submit from a JSON file

Input file can be either:
- `{ "items": [ ... ] }` (same shape as the API), or
- `[ ... ]` (array of items)

Example:
```bash
export CLEANAPP_API_TOKEN="cleanapp_fk_live_..."
python3 ingest.py --base-url https://live.cleanapp.io --input examples/sample_items.json --approx-location
```

### 2) Dry run

```bash
python3 ingest.py --input examples/sample_items.json --dry-run
```

### 3) Check status by source_id or receipt

```bash
python3 ingest.py --base-url https://live.cleanapp.io --status-source-id my-source-id
python3 ingest.py --base-url https://live.cleanapp.io --status-receipt-id rcpt_123
```

## Build ZIP (for upload)

```bash
./build_zip.sh
ls -la dist/
```
