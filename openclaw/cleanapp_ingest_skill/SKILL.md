# SKILL: Report an observed problem to CleanApp

Submit one observed problem privately without registration or an API key. Start at https://www.cleanapp.io/agents; the canonical contract is https://live.cleanapp.io/api/v1/openapi.yaml.

A receipt means the report is stored privately for review. It does not mean publication, routing, resolution or a reward. Submit only facts actually observed. A description alone is enough; optional known URL or coordinates add context.

## First report

Create a private input JSON file containing `description`, optional `title`, `url`, `lat`/`lng`, `agent_name`, and preferably a private random UUIDv4 `source_id`. Generate that UUID once (for example with Python `uuid.uuid4()`), retain it before sending, and reuse only with the same payload for an exact retry. Do not use a static shared ID or public URL. An omitted ID is generated before sending and printed privately to stderr. Keep that ID for timeout recovery; do not blindly create a new report.

```sh
python3 ingest.py --input observed-problem.json --receipt-file private-receipt.json
```

The new file uses mode 0600. It retains the intent before sending and stores the private receipt capability when available. Default JSON stdout without a receipt file includes that capability, so keep it private. A 503 can carry a retained private receipt; the command preserves it while exiting nonzero. Check its state with:

```sh
python3 ingest.py --status-receipt-id RECEIPT_ID_FROM_THE_FILE --tracking-token-file private-receipt.json
```

`pending_retry` means broker enqueue has not been confirmed. Retry the exact original UUID/payload to resume processing. `received` confirms enqueue, not completed analysis/review. Tokens expire 30 days after original receipt creation; retries never extend access. Honor 429/503 Retry-After; changed payload under the same UUID returns 409.

## Preview and data handling

```sh
python3 ingest.py --input observed-problem.json --dry-run
```

Dry-run performs no network call. Optional `--approx-location` rounds supplied coordinates; `--no-location` removes them. Explicit `--no-media` removes media metadata. Public intake rejects unsupported rich/bulk input rather than silently dropping it. No external scripts are fetched at runtime.

## Approved Wire integrations

Optional `CLEANAPP_API_TOKEN` selects the existing authenticated Wire single/batch path and accepts its previous input format. Store this credential in the OpenClaw secret store. Self-registration is disabled by default in production; if enabled it creates a pending identity without submission permission. It is not required for a public first report. An approved key is reusable and its raw value is shown once when issued.

Approved Wire status by source uses `--status-source-id`; public status uses the private receipt capability as above. For Wire examples with media/source metadata, use `examples/sample_items.json` only with approved credentials. Build the self-contained package with `./build_zip.sh`. Version 1.2.0 adds the public single-report path without copying another runtime.
