# CleanApp Wire Audit

Status audited against repository state after the canonical-ingest refactor on 2026-03-08.

Note: no standalone audit-matrix file was attached with the request. The matrix below is derived from the acceptance sections in `/Users/anon16/Downloads/casp_spec_for_clean_app.md`, with `CASP` interpreted as `CleanApp Wire`.

## Executive Summary

CleanApp Wire is implemented and live as a public machine-ingest surface on `report-listener`. It provides:

- agent registration
- API-key authentication
- single and batch machine submission
- receipts
- status lookup
- basic reputation profile lookup
- submission-quality scoring
- lane assignment
- transport-level idempotency

The current implementation is now the canonical ingestion core for most machine-originated reports, but it still has compatibility debt at the legacy API boundary.

Today, Wire owns the canonical machine-ingest core:

- Wire validates and scores the envelope
- Wire assigns a lane
- Wire directly creates `reports` / `report_raw` rows and publishes `report.raw`
- Wire persists a Wire-specific submission/receipt record

That means the protocol is real and the core ingest path is now canonical. The remaining transition debt is mostly in legacy compatibility routes.

The largest implementation gap is architectural, not endpoint-level:

1. legacy `/api/v3/reports/bulk_ingest` and `/api/v4/reports/bulk_ingest` compatibility callers still do not receive Wire-native receipts/status semantics directly
2. legacy `/v1/reports:bulkIngest` still exposes an older external response contract even though it now translates into Wire
3. dedupe clustering, rewards, and integrity controls are mostly scaffolding rather than full production behavior

## Acceptance Audit Matrix

| Spec item | Status | Exact files / modules | Implementation reality |
| --- | --- | --- | --- |
| Purpose | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/main.go`, `report-listener/handlers/openapi/cleanapp-wire.v1.yaml`, `report-listener/handlers/ingest_v1.go` | Wire is now the canonical ingest core for machine-originated reports in practice, but not every external caller receives Wire-native response semantics yet because legacy v1/v3/v4 compatibility routes remain. |
| Design principles | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/cleanapp_wire_v1.go`, `report-listener/handlers/ingest_v1.go`, `report-listener/database/migration_helpers.go` | One canonical envelope exists, idempotency exists, provenance metadata is stored, lanes exist, and the canonical persistence/publish path now lives inside Wire. “All internal agents must use Wire” is substantially true for the migrated producers, but rewards are still placeholders. |
| Scope | Partial | `report-listener/main.go`, `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/cleanapp_wire_v1.go` | Submission, batch submission, auth, provenance capture, receipts, status, lane assignment, and reputation hooks are implemented. Reward accounting and deeper governance/review flows are only partial. |
| Core resources | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/cleanapp_wire_v1.go`, `report-listener/database/fetcher_keys_v1.go`, `report-listener/database/migration_helpers.go` | Agent, Submission, Report, EvidenceBundle, Receipt, and ReputationProfile exist. RewardRecord exists as a table, but not as a working lifecycle. |
| API surface: primary endpoints | Complete | `report-listener/main.go`, `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/handlers/openapi/cleanapp-wire.v1.yaml` | All primary endpoints from the spec are implemented and documented, including colon-style aliases for submit/batchSubmit. |
| API surface: optional future endpoints | Not applicable yet | `report-listener/handlers/openapi/cleanapp-wire.v1.yaml` | Corroborate, amend, withdraw, cluster lookup, and rewards lookup are not implemented and are not required for current v1. |
| Submission contract | Partial | `report-listener/handlers/cleanapp_wire_v1.go` | Envelope and payload are modeled, but integrity headers and a more formal transport contract are not yet first-class. |
| Canonical JSON schema | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/handlers/openapi/cleanapp-wire.v1.yaml` | The schema is close to the provided spec but not identical. It uses `cleanapp-wire.v1`, not `casp.v1`, and some optional fields are present without full downstream semantics. |
| Required fields | Complete | `report-listener/handlers/cleanapp_wire_v1.go` | Required field validation exists, including schema version, required agent/report fields, confidence bounds, and location-or-digital-context requirement. |
| Idempotency rules | Complete | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/cleanapp_wire_v1.go`, `report-listener/database/migration_helpers.go` | `(fetcher_id, source_id)` uniqueness is enforced. Same payload returns idempotent replay, materially different payload returns conflict. |
| Trust tiers | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/fetcher_keys_v1.go`, `report-listener/handlers/fetcher_promotion_v1.go`, `report-listener/handlers/internal_fetcher_admin.go` | Tiers exist numerically on fetchers and can be promoted administratively, but the full tier semantics from the spec are not fully encoded in policy. |
| Lane architecture | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/handlers/ingest_v1.go`, `report-listener/config/config.go` | Reject/quarantine/shadow/publish/priority lanes exist, but lane assignment is still a simple heuristic over tier, quality, evidence count, and config toggles. |
| Reputation model | Partial | `report-listener/database/cleanapp_wire_v1.go`, `report-listener/database/migration_helpers.go`, `report-listener/handlers/cleanapp_wire_v1.go` | Reputation metrics table exists and a profile endpoint exists, but most sub-scores are placeholders and only `sample_size` is actively incremented in current flow. |
| Submission-level quality score | Partial | `report-listener/handlers/cleanapp_wire_v1.go` | A real quality score is computed from confidence, evidence completeness, place certainty, target certainty, novelty, category fit, policy risk, and anomaly. It is still a simplified heuristic, not a mature scoring system. |
| Economic and reward model | Missing | `report-listener/database/migration_helpers.go` | `wire_reward_records` exists as a table only. No reward computation, issuance, or lookup flow is implemented. |
| Dedupe and clustering | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/cleanapp_wire_v1.go`, `report-listener/database/migration_helpers.go` | Transport-level dedupe is implemented. Semantic dedupe, corroboration-vs-duplicate classification, and clustering are not implemented beyond placeholder tables/fields. |
| Receipts and statuses | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/cleanapp_wire_v1.go` | Immediate receipts and status lookup are implemented and working. The broader lifecycle states in the spec (`clustered`, `routed`, `validated`, `resolved`, `rewarded`) are not wired through. |
| Authentication and integrity | Partial | `report-listener/main.go`, `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/fetcher_keys_v1.go`, `report-listener/config/config.go`, `report-listener/middleware/fetcher_key_auth_v1.go` | API-key auth, scopes, status checks, and quotas exist. Strict signature enforcement is optional and disabled by default. Nonce/timestamp replay protection via headers is not implemented. |
| Validation rules | Partial | `report-listener/handlers/cleanapp_wire_v1.go` | Core schema/field/confidence validation is implemented with machine-readable codes. MIME allowlists, timestamp drift checks, and richer category compatibility validation are not yet present. |
| Queue and processing architecture | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/handlers/ingest_v1.go`, `report-listener/config/config.go`, `report-analyze-pipeline` consumers | Wire now publishes into the existing `report.raw` flow directly through its own ingest core. The dedicated `casp.*` / Wire-native queue graph from the spec does not exist yet. |
| Governance and auditability | Partial | `report-listener/database/cleanapp_wire_v1.go`, `report-listener/database/migration_helpers.go`, `report-listener/handlers/internal_fetcher_admin.go`, `report-listener/handlers/fetcher_promotion_v1.go` | Submission records, receipts, promotion requests, and moderation events exist. Full decision traces, rule-versioning, and reconstruction of every lane decision are not yet implemented. |
| Rollout plan | Partial | `report-listener/main.go`, `report-listener/handlers/ingest_v1.go`, `cli/cleanapp`, `openclaw/cleanapp_ingest_skill`, `news-indexer-bluesky/src/bin/submitter_bluesky.rs`, `news-indexer/src/bin/submitter_twitter.rs`, `news-indexer/src/bin/submitter_github.rs`, `tools/reddit_dump_reader/src/main.rs`, `report-processor/handlers/handlers.go` | Wire is now the default or preferred path for the Bluesky submitter, twitter submitter, github submitter, the Reddit dump reader, the npm CLI, the OpenClaw ingest skill, and `report-processor`. Legacy v1 ingest now translates into Wire semantics internally, and Wire owns canonical persistence/publish directly. The remaining migration gap is mainly legacy v3/v4 machine-ingest callers, which are mirrored into Wire provenance but do not yet receive Wire-native receipts directly. |
| Operational metrics | Partial | `report-listener/database/fetcher_keys_v1.go`, `report-listener/database/cleanapp_wire_v1.go`, `report-listener/database/ingestion_audit_v1.go` | Basic usage quotas and ingestion audits exist. The richer operational metrics suite from the spec is not fully implemented. |
| Non-negotiable rules | Partial | `report-listener/main.go`, `news-indexer-bluesky/src/bin/submitter_bluesky.rs`, `openclaw/cleanapp_ingest_skill/ingest.py`, `cli/cleanapp/src/commands/reports/submit.ts`, `report-processor/handlers/handlers.go`, `report-listener/handlers/handlers.go` | Rule 1 is substantially true for the major machine producers now migrated onto Wire. The remaining exception is compatibility traffic through legacy v3/v4 bulk-ingest routes, which still return legacy responses even though provenance is mirrored into Wire internally. Rules around rewards, provenance integrity, and duplicate-vs-corroboration are still only partial. |

## Current Implementation Reality

### Public Wire endpoints

Implemented in `report-listener`:

- `POST /api/v1/agents/register`
- `GET /api/v1/agents/me`
- `GET /api/v1/agents/reputation/:agent_id`
- `POST /api/v1/agent-reports:submit`
- `POST /api/v1/agent-reports:batchSubmit`
- `GET /api/v1/agent-reports/receipts/:receipt_id`
- `GET /api/v1/agent-reports/status/:source_id`

Files:

- `report-listener/main.go`
- `report-listener/handlers/cleanapp_wire_v1.go`
- `report-listener/handlers/openapi/cleanapp-wire.v1.yaml`
- `report-listener/handlers/openapi_cleanapp_wire_v1.go`

### Actual ingest path today

The true path for a successful Wire submission is:

1. authenticate with fetcher-key middleware
2. validate and normalize the Wire envelope
3. compute submission quality
4. assign a lane
5. persist the canonical machine-ingest record directly from the normalized Wire envelope
6. insert `reports` + `report_raw`
7. publish to `report.raw`
8. persist Wire submission/receipt rows
9. increment Wire reputation sample count

Files:

- `report-listener/handlers/cleanapp_wire_v1.go`
- `report-listener/database/cleanapp_wire_v1.go`

This means Wire is now a canonical machine-ingest engine, not just an envelope and receipt layer.

### Current lane behavior

Implemented lane outcomes:

- `reject`
- `quarantine`
- `shadow`
- `publish`
- `priority`

Current lane policy:

- tier `<= 0` -> `quarantine`
- tier `< CLEANAPP_WIRE_PUBLISH_LANE_MIN_TIER` -> `shadow`
- otherwise `publish` if `quality >= 0.65` and evidence exists
- `priority` only if explicitly requested and enabled by config and tier/quality threshold is met

Files:

- `report-listener/handlers/cleanapp_wire_v1.go`
- `report-listener/config/config.go`

### Current reputation behavior

Implemented:

- reputation profile table
- profile endpoint
- sample-size tracking
- fetcher tier/status visible through `/agents/me`

Not yet implemented:

- real rolling reputation updates
- dynamic promotions/demotions from observed quality
- reward linkage

Files:

- `report-listener/database/cleanapp_wire_v1.go`
- `report-listener/database/migration_helpers.go`
- `report-listener/handlers/fetcher_promotion_v1.go`
- `report-listener/handlers/internal_fetcher_admin.go`

## Legacy Ingestion Paths That Bypass Wire

These no longer all bypass Wire entirely. Some are now compatibility routes that translate into or mirror into Wire semantics internally, while others remain intentionally outside Wire.

### 1. Legacy protected v3/v4 bulk ingest

Files:

- `report-listener/main.go`
- `report-listener/handlers/handlers.go`

Routes:

- `POST /api/v3/reports/bulk_ingest`
- `POST /api/v4/reports/bulk_ingest`

Current state:

- legacy `/api/v3` and `/api/v4` bulk ingest still execute their existing external contract
- accepted machine-originated submissions are mirrored into Wire provenance/receipt state internally
- callers still do not receive Wire-native receipt/status semantics directly

Why it still matters:

- external callers still experience the legacy contract
- migration debt can remain hidden because internal provenance exists even when external semantics do not

### 2. Fetcher v1 ingest surface

Files:

- `report-listener/main.go`
- `report-listener/handlers/ingest_v1.go`
- `report-listener/database/fetcher_keys_v1.go`

Route:

- `POST /v1/reports:bulkIngest`

Current state:

- direct `/v1/reports:bulkIngest` calls are now translated item-by-item into Wire submissions internally
- callers still receive the legacy v1 response shape

Important nuance:

v1 is no longer a true producer-side bypass path and is no longer an implementation dependency underneath Wire. It is now a compatibility facade over the canonical Wire ingest core.

### 3. OpenClaw / agent skill package

Files:

- `openclaw/cleanapp_ingest_skill/manifest.json`
- `openclaw/cleanapp_ingest_skill/SKILL.md`
- `openclaw/cleanapp_ingest_skill/README.md`
- `openclaw/cleanapp_ingest_skill/ingest.py`

Current state:

- now targets:
  - `POST /api/v1/agent-reports:submit`
  - `POST /api/v1/agent-reports:batchSubmit`
  - `GET /api/v1/agent-reports/status/{source_id}`
  - `GET /api/v1/agent-reports/receipts/{receipt_id}`
- wraps legacy simple items into `cleanapp-wire.v1`
- no longer teaches `/v1/reports:bulkIngest` as the default path

### 4. CleanApp CLI

Files:

- `cli/cleanapp/src/commands/auth/whoami.ts`
- `cli/cleanapp/src/commands/reports/submit.ts`
- `cli/cleanapp/src/commands/reports/bulk_submit.ts`

Current state:

- `auth whoami` now targets `/api/v1/agents/me` with fallback to `/v1/fetchers/me`
- submit and bulk-submit now target Wire endpoints by default
- status supports:
  - `--source-id`
  - `--receipt-id`
  - legacy `--report-id` fallback
- the CLI still keeps legacy metrics/fetcher introspection compatibility because Wire-native metrics do not exist yet

### 5. Bluesky submitter

Files:

- `news-indexer-bluesky/src/bin/submitter_bluesky.rs`

Current state:

- now supports `SUBMIT_PROTOCOL=wire|legacy|auto`
- defaults to `auto`, which resolves to Wire for fetcher-key style tokens
- uses stable `source_id = Bluesky URI`
- stores Wire receipts in a local submission ledger:
  - `indexer_bluesky_wire_submission`
- preserves safe rollback via legacy mode

### 6. Twitter submitter

Files:

- `news-indexer/src/bin/submitter_twitter.rs`
- `platform_blueprint/deploy/prod/docker-compose.yml`

Current state:

- now supports `SUBMIT_PROTOCOL=wire|legacy|auto`
- uses stable `source_id = twitter:<external_id>`
- maps twitter-originated items into `cleanapp-wire.v1` envelopes
- preserves idempotent retries through stable `source_id`
- is now running in production with `SUBMIT_PROTOCOL=auto`, resolving to Wire through a dedicated Wire fetcher key
- rollout remains safe because `legacy` is still available as an override

### 7. GitHub submitter

Files:

- `news-indexer/src/bin/submitter_github.rs`

Current state:

- now supports `SUBMIT_PROTOCOL=wire|legacy|auto`
- uses stable `source_id = github_issue:<external_id>`
- maps GitHub issue items into `cleanapp-wire.v1` envelopes
- preserves idempotent retries through stable `source_id`
- currently migrated in code; no prod runtime cutover was required because this submitter is not part of the active prod compose set

### 8. Reddit dump reader

Files:

- `tools/reddit_dump_reader/src/main.rs`
- `tools/reddit_dump_reader/README.md`

Current state:

- now supports `--submit-protocol auto|wire|legacy`
- uses stable `source_id` values derived from Reddit record ids
- defaults to `auto`, which resolves to Wire for fetcher-key style tokens
- posts batches directly to `POST /api/v1/agent-reports:batchSubmit` in Wire mode
- preserves safe rollback through explicit `legacy` mode
- currently migrated in code/tooling; there is no standing prod service to cut over

### 9. Report processor match flow (migrated)

Files:

- `report-processor/handlers/handlers.go`

Current state:

- `report-processor` now submits newly created reports through Wire by default
- it uses a dedicated internal Wire fetcher identity and stable `source_id`
- it receives lane assignment and receipt semantics through Wire
- it still emits downstream `report.raw` follow-on events in its own processing flow where appropriate

Why it still matters:

- this is no longer an ingest bypass
- it is now a Wire-native internal producer, but it still participates in downstream event publication outside the receipt layer

### 10. Internal admin promotion path

Files:

- `report-listener/handlers/internal_ingest_admin.go`

Why it bypasses Wire:

- it is intentionally internal/admin-only
- it updates visibility/trust directly and can publish `report.analysed` from DB state
- this is not a problem by itself, but it is outside the Wire provenance path

## Internal Producers To Wrap or Migrate First

### Completed migrations

- `news-indexer-bluesky/src/bin/submitter_bluesky.rs` -> Wire-native by default
- `news-indexer/src/bin/submitter_twitter.rs` -> Wire-native in production via `SUBMIT_PROTOCOL=auto` and a dedicated Wire fetcher key
- `news-indexer/src/bin/submitter_github.rs` -> Wire-capable with safe `SUBMIT_PROTOCOL` rollout
- `tools/reddit_dump_reader/src/main.rs` -> Wire-capable with safe protocol override
- `cli/cleanapp/*` machine submission flows -> Wire-native by default
- `openclaw/cleanapp_ingest_skill/*` -> Wire-native
- `report-processor/handlers/handlers.go` -> Wire-native by default for report creation

### Priority 1: Legacy v3/v4 compatibility callers

Files:

- `report-listener/handlers/handlers.go`
- any remaining callers posting to:
  - `POST /api/v3/reports/bulk_ingest`
  - `POST /api/v4/reports/bulk_ingest`

Why first now:

- they now have a legacy-to-Wire mirroring path available
- they still do not receive Wire responses directly
- they are the next obvious population to either wrap or migrate explicitly

### Priority 2: Direct v1 fetcher ingest callers

Files:

- `report-listener/handlers/ingest_v1.go`
- any producers still posting to `/v1/reports:bulkIngest`

Why second now:

- Wire still uses v1 internally, so this cannot be deleted yet
- direct external use of v1 now gains Wire semantics internally, but still keeps a legacy response contract

What it should gain:

- eventual collapse behind a pure Wire ingest core once Wire no longer delegates to v1

### Priority 3: Internal admin promotion path

Files:

- `report-listener/handlers/internal_ingest_admin.go`

Why fourth:

- this is intentionally outside Wire, but should eventually emit provenance-compatible moderation events if the Wire model becomes canonical system-wide

## Legacy Migration Summary

Current state after migration PRs:

- Wire exists and works.
- v1 fetcher ingest still exists and is still the actual persistence/publish core used under Wire.
- the following real producers are now Wire-native by default or Wire-capable with explicit rollout controls:
  - `news-indexer-bluesky`
  - `news-indexer` twitter submitter
  - `news-indexer` github submitter
  - `reddit_dump_reader`
  - `@cleanapp/cli`
  - `openclaw/cleanapp_ingest_skill`
  - `report-processor`
- direct `/v1/reports:bulkIngest` now translates submissions into Wire semantics internally while preserving its legacy response contract
- legacy `/api/v3/reports/bulk_ingest` now mirrors new ingests into Wire submission/receipt records for provenance, without changing its legacy response contract
- the largest remaining compatibility gap is legacy `/api/v3` and `/api/v4` compatibility callers that still return legacy-only response contracts

Recommended migration order:

1. migrate or wrap all remaining `/api/v3/reports/bulk_ingest` and `/api/v4/reports/bulk_ingest` callers so they receive Wire receipts or explicit receipt-compatible metadata
2. keep `/v1/reports:bulkIngest` compatible for now, then collapse once external callers have migrated off the legacy response contract
3. optionally emit Wire-compatible moderation/provenance events for internal admin/moderation flows

Migration policy recommendation:

- Do not delete v1 or v3 ingest immediately.
- First migrate remaining legacy callers.
- Keep the legacy v3 machine route mirrored into Wire for auditability.
- Only delete v1 direct usage once external callers no longer depend on the legacy response contract.

## Top 5 Production Risks

### 1. Wire is still not the sole external machine-ingest contract

Risk:

- external callers can still remain on legacy response contracts
- reputation, lane, receipt, and provenance behavior can remain partially fragmented at the API boundary

### 2. Legacy compatibility surfaces still fragment external semantics

Risk:

- some callers still see only legacy response bodies even though their submissions are mirrored or translated into Wire internally
- that slows migration because provenance and receipt behavior remain partially hidden at the API boundary

### 3. Integrity/auth protections are incomplete

Risk:

- strict signatures are optional and disabled by default
- nonce/timestamp replay headers are not implemented
- this limits confidence for higher-trust external automation onboarding

### 4. Dedupe/clustering and rewards are mostly scaffolding

Risk:

- reputation can look more mature than it really is
- reward/economics cannot yet be trusted for production incentives
- corroboration vs duplicate distinction is still absent

### 5. Compatibility layers still hide migration debt

Risk:

- legacy `/api/v3` and `/v1` callers now create or translate into Wire provenance internally
- but they still do not uniformly receive Wire-native receipt/status semantics directly
- this can hide migration debt if not tracked explicitly

## Next 3 Smallest Production-Safe PRs

### PR 1: Add a first-class migration map for remaining legacy callers

Scope:

- enumerate all remaining direct callers of `/api/v3/reports/bulk_ingest`, `/api/v4/reports/bulk_ingest`, and `/v1/reports:bulkIngest`
- assign each to:
  - migrate
  - wrap
  - retire

Why this is safe:

- no runtime behavior change
- reduces hidden bypass risk

### PR 2: Add optional Wire-native receipt/status metadata to legacy compatibility responses

Scope:

- enrich legacy `/api/v3` and `/v1` compatibility responses with optional receipt/status metadata where it does not break existing callers
- make migration progress visible to integrators without deleting old routes yet

Why this is safe:

- incremental compatibility improvement
- reduces silent dependence on legacy-only semantics

### PR 3: Migrate or wrap remaining legacy v3/v4 machine callers

Scope:

- enumerate and migrate the remaining real callers of `/api/v3/reports/bulk_ingest` and `/api/v4/reports/bulk_ingest`
- where migration is risky, wrap them so they at least receive receipt-compatible metadata

Why this is safe:

- preserves production safety while shrinking the last real compatibility gap

## Recommended Canonical Direction

If CleanApp Wire is intended to become the canonical ingestion layer for all machine-originated and machine-assisted traffic, the architectural end state should be:

1. all machine producers submit Wire envelopes
2. Wire owns canonical machine-ingest persistence and receipt generation directly
3. legacy v1/v3 machine-ingest routes either disappear or become thin compatibility wrappers into Wire
4. reputation, lane assignment, dedupe, and later rewards are computed from one shared machine-ingest path

That end state is now substantially true in the ingest core. The remaining work is mainly at the external compatibility boundary and in the unfinished reputation/dedupe/reward layers.
