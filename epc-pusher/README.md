# EPC cumulative notice worker

The worker keeps existing Ethereum Plus Code keys and destinations, queues new
public report notices durably, and sends them through Blockscan Chat. It does
not deploy contracts or put report text on Ethereum.

## Install and verify

From `epc-pusher/`:

```sh
corepack yarn install --frozen-lockfile
npm run check
npm test
EPC_TEST_DB_URL=mysql://root:fixture-password@127.0.0.1:3306 npm run test:integration
python3 -m unittest discover -s tools -p 'test_*.py'
```

The MySQL test requires a disposable MySQL 8 instance with permission to create
and drop a uniquely named `epc_worker_test_*` database. Provider calls in that test
are injected fakes. HTTP adapter tests use a local fixture server. Address tests
reproduce six destinations recovered from the actual development archive.

## Apply the schema explicitly

Apply `db/patches/20261113_epc_cumulative_dispatch.sql` before starting the worker.
For example, from `epc-pusher/` with protected database configuration loaded:

```sh
npm run migrate -- ../db/patches/20261113_epc_cumulative_dispatch.sql
```

The explicit migrator requires `DB_NAME` and an administrative migration
identity; the worker never runs startup DDL. Do not grant the worker DDL
privileges. For future AWS code-only releases, use the canonical deployment path
with `--skip-migrations` after explicitly applying any required reviewed schema
changes. The first production migration/import runs under the administrator
with the worker stopped.
The migration creates absent tables and upgrades the legacy outbox additively.
The original four EPC tables retain their original InnoDB/utf8mb4_0900_ai_ci
collation. Original keys, addresses, receipt IDs, templates and metadata survive.
`lib/sql/epc_schema.sql` describes the complete schema for a fresh database.

Use `tools/epc_import_legacy.py` to build and validate the cumulative archive
import. Its importer tests document collision and provenance checks. Import
before enabling dispatch. A legacy receipt retains `source_namespace=legacy-dev`
and the original `source_report_seq`. Only a proven canonical external-source
identity may reserve its `prod:report:<production-sequence>` event key. Equal
sequence numbers or matching brands do not establish report identity.

## Protected configuration

Supply environment variables directly, or mount a protected file read-only and
set `EPC_ENV_FILE` to that file inside the container. The file accepts whitelisted
literal `KEY=value` lines and comments. It performs no shell evaluation,
interpolation, quoting or unescaping. Direct process variables take precedence.
Keep this file outside the source tree and Docker build context.

Recommended initial production values:

```ini
DB_HOST=cleanapp_db
DB_PORT=3306
DB_USER=epc_worker
DB_NAME=cleanapp
EPC_SOURCE_NAMESPACE=prod
EPC_REPORTS_START_SEQ=1
EPC_DISPATCH=false
EPC_CONTRACT_ADDRESS=0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA
EPC_SEND_INTERVAL_MS=1200
EPC_REQUEST_TIMEOUT_MS=30000
EPC_SCAN_BATCH_SIZE=100
EPC_RESCAN_WINDOW_SIZE=1000
```

Also supply `DB_PASSWORD` and `BLOCKSCAN_CHAT_API_KEY` through protected
configuration. Production refuses an empty database password. Set
`EPC_DISPATCH=true` to send after import and verification. The send interval has
a 1000 ms minimum in every environment. Optional settings are `EPC_ONLY_VALID`
(default true), `EPC_FILTER_LANGUAGE`, `EPC_FILTER_SOURCE`, `EPC_POLL_MS`
(default 5000), `EPC_RETRY_BASE_MS` (60000), `EPC_RETRY_MAX_MS` (3600000), and
`EPC_MAX_ATTEMPTS` (8). The initial sequence is a lower bound for forward
scanning, every rescan wrap, and claims of already queued events. Raising it skips
lower report sequences; lowering it lets bounded rescans cover the newly included
earlier range without replacing existing receipts.

If Blockscan accepts established conversations but rejects new recipients, set
`EPC_EXISTING_CHATS_ONLY=true`. Dispatch then requires a previous successful
receipt for the stored destination. Other new notices remain held in the same
cumulative outbox. The default is false; clearing this setting after resolving
the provider restriction releases held notices without replaying sent events.

## Dispatch and recovery

An advisory database lock permits one worker per source namespace. The worker
looks up the stored key/address first and derives an address only for a new key.
An exact template is reused; new notices store their rendered body with encoded
brand URL components. The production cursor is independent of legacy receipt
IDs and legacy report sequences.

Each tick first reads at most `EPC_SCAN_BATCH_SIZE` forward sequence IDs through
the ordered sequence index. Eligibility and preferred-language sorting then run
only on those materialized IDs through indexed point lookups. Forward progress
commits through the source window even if every analysis in it is invalid or
private. The bounded rescan uses the same candidate-driven join for at most
`EPC_RESCAN_WINDOW_SIZE` earlier source sequence IDs. The rescan wraps at the configured sequence floor, so late analysis and reports
that
later become valid/public are picked up below the forward cursor. Unique event
keys deduplicate repeated scans and verified legacy aliases across namespaces.
Queue creation and cursor progress commit together, independently of the single
paced send.

Both queuing and claiming enforce the notification/routing suppression registry
and the public evidence guards: an existing report has a nonempty public ID, raw
visibility is NULL/public, status is active
or absent, and there is no private nonempty owner. Privacy is read exactly from
the latest Wire submission, using matching transactional Wire projection nodes
or the original JSON when that projection is absent. Dirty, stale or missing
aggregate source projections never authorize a send. The runtime role needs
SELECT on `reports`, `wire_submissions_raw` and `wire_evidence_projection` as
well as the analysis/publication/policy tables. The exact analysis language and
brand are rechecked before
send. Revocation after queuing marks that event `error` with
`report_not_eligible`; an operator may requeue it after verifying eligibility.

A committed `sending` intent always precedes the HTTP request. Only `status=1`,
`message=OK` and a positive numeric message ID count as an acknowledged
response, which persists the Blockscan ID in both the dedicated field and legacy JSON
metadata. Explicit rate limits retry with bounded exponential backoff and honor
`Retry-After`. A durable source-namespace dispatch deadline blocks every pending
row during a provider throttle, survives restarts, and allows queue scans to
continue without sleeping through the full cooldown. Permanent rejections become
`error`. Timeouts, ambiguous responses
and interrupted sends become `uncertain`, including a crash after API success
but before receipt persistence. They are never automatically resent because the
provider has no client idempotency key in this API path. Reconcile an uncertain
attempt with Blockscan before manually marking it sent or returning it to
pending. Logs contain outcome codes and receipt IDs, never API key fragments or
provider response bodies.
