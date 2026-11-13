# Report with an agent

Start at https://www.cleanapp.io/agents. A network-capable agent can submit one observed problem without registration or an API key. Use the supported MCP connector when the client provides MCP tools; a browsing-only GET tool cannot submit a report by itself. Give that client the connector or use an HTTP-capable client.

The public intake stores the original report privately for review. A receipt confirms storage, not publication, routing, resolution, or a reward. Public intake never authorizes outbound email, notifications, routing or rewards. Add only facts actually observed; a description alone is enough, and unknown location stays unknown.

## First report with the CLI

CLI 0.2.0 adds the no-key path:

```sh
npm install -g @cleanapp/cli@0.2.0
cleanapp submit --desc "Describe the problem you actually observed" --receipt-file private-receipt.json
```

The new receipt file is private (mode 0600). It initially retains the intent and is atomically replaced with the receipt when available. Keep it out of public logs. The CLI also prints the private `source_id` to stderr before sending, so a timeout does not erase the retry identity. Retain that UUID and the exact description/options when retrying; do not create a new report to recover a receipt.

```sh
cleanapp status --receipt-id RECEIPT_ID_FROM_THE_FILE --tracking-token-file private-receipt.json
```

JSON output without `--receipt-file` returns the private tracking capability directly to the caller. Human output displays the same capability when no file was requested. Save it privately; changing output format does not require submitting again. A 503 response can carry a retained receipt; the CLI preserves that receipt on stdout or in the requested file while returning a failing exit status.

## Direct HTTP

Production base: `https://live.cleanapp.io`. The canonical machine-readable contract is `/api/v1/openapi.yaml` and the interactive documentation is `/api/v1/docs`.

Send `POST /api/v1/public/agent-reports` with JSON:

- `description`: required, at most 8192 UTF-8 bytes.
- `title`: optional, at most 255 UTF-8 bytes.
- `source_id`: optional private random UUIDv4. Generate it once per report and retain it before sending; use the same UUID and payload for exact retries. Never use a static shared example ID or a public URL.
- `agent_name`: optional self-asserted client label, at most 80 Unicode characters.
- `url`: optional known HTTP(S) URL without embedded credentials, at most 2048 UTF-8 bytes.
- `latitude` and `longitude`: optional known coordinates; supply both together.

No API key, account, images, batch, caller lane or trust settings are accepted. The body limit is 16 KiB. When no `source_id` is supplied the server creates a fresh random intent and returns it; if that response is lost, do not blindly resend without a retained retry identity. Equal descriptions under distinct intent IDs never share a private receipt.

A successful response includes `receipt_id`, the private intent `source_id`, `status: quarantined`, `publication_status: private`, boolean `idempotency_replay`, `tracking_token`, `tracking_expires_at`, and a relative same-origin `status_url`. Resolve `status_url` against the configured API base and send `Authorization: Bearer <tracking_token>` on the receipt GET. Keep the token out of URLs and public logs. Status does not disclose the original description, report sequence or a public report link.

Exact retries retain the same receipt even when client IP, request timestamp or user agent changes. A changed payload under that UUID returns 409. Tracking access expires 30 days after the original receipt creation; retries do not extend it. A retry after expiry returns 410 and retains the original report.

`processing_status: received` means broker enqueue was confirmed, not that analysis or review finished. If the broker is unavailable, POST returns 503 with a durable private receipt and `processing_status: pending_retry`; receipt GET reports that same state. Retry the original UUID/payload to resume enqueueing. A 503 without a receipt leaves acceptance unconfirmed. Honor `Retry-After` on 429 and 503. A 403 means intake is temporarily disabled and no new report was accepted.

## Approved integrations

Existing approved API keys continue to use authenticated Wire submission, batches, rich envelopes and their existing receipt/status paths. Self-registration is disabled by default in production. If enabled, it creates a pending identity without submission permission; it does not unlock the first-report path. An integration key is a reusable bearer credential whose raw value is shown once, not a token consumed after one request.

OpenClaw's included `openclaw/cleanapp_ingest_skill` uses public private-review intake by default for one simple report. Its optional `CLEANAPP_API_TOKEN` enables the existing approved Wire path. Unsupported public rich input is rejected rather than silently discarded; explicit `--no-media` and location-reduction flags remain user-selected transformations.

## Operations and verification

No new tables or runtime DDL are needed. Existing Wire, report, quota and side-effect-policy migrations must already be applied. `PUBLIC_AGENT_INGEST_ENABLED=false` disables new public intake; existing private receipts remain readable until their capabilities expire. The global Wire switch also disables new public submissions.

Default durable budgets are 5 new intents/minute and 50/day per IP, and 30/minute and 1000/day globally. Configure positive `PUBLIC_AGENT_PER_IP_MINUTE_CAP`, `PUBLIC_AGENT_PER_IP_DAILY_CAP`, `PUBLIC_AGENT_GLOBAL_MINUTE_CAP` and `PUBLIC_AGENT_GLOBAL_DAILY_CAP` as needed. Non-positive values retain safe defaults, never unlimited public access. Global and IP budgets are reserved atomically; global capacity is checked before creating an IP bucket. One server-owned actor is used, with no issued backend key and legacy bearer authentication disabled. An eight-request capacity gate and cross-replica intent locks bound concurrent work.

Run `go test ./...` in `report-listener`, `npm test && npm run build` in `cli/cleanapp`, and `python3 -m unittest discover -s openclaw/cleanapp_ingest_skill -p 'test_*.py'`. `make ci-cleanapp-wire` exercises a fresh anonymous first report before any registration/approval, private receipt authorization, exact retry/conflict behavior, atomic rollback on receipt failure, private side-effect controls, broker outage/recovery, expiry and the existing approved Wire flow in a disposable MySQL/RabbitMQ fixture.
