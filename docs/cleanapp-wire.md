# CleanApp Wire Audit

Status audited against repository state at commit `b013dd1` on 2026-03-07.

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

The current implementation is not yet the canonical ingestion core for all machine-originated reports.

Today, Wire is a thin orchestration layer on top of the older fetcher-v1 ingest path:

- Wire validates and scores the envelope
- Wire assigns a lane
- Wire persists a Wire-specific submission/receipt record
- Wire then calls the older `/v1/reports:bulkIngest` logic to actually create `reports` / `report_raw` rows and publish `report.raw`

That means the protocol is real, but the system architecture is transitional.

The largest implementation gap is architectural, not endpoint-level:

1. several internal machine producers still bypass Wire entirely
2. Wire still depends on the older v1 ingest implementation rather than owning canonical ingest storage/publish directly
3. dedupe clustering, rewards, and integrity controls are mostly scaffolding rather than full production behavior

## Acceptance Audit Matrix

| Spec item | Status | Exact files / modules | Implementation reality |
| --- | --- | --- | --- |
| Purpose | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/main.go`, `report-listener/handlers/openapi/cleanapp-wire.v1.yaml` | Wire exists as the intended machine-ingest surface, but it is not yet the canonical path for all internal and external machine-originated traffic. |
| Design principles | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/database/cleanapp_wire_v1.go`, `report-listener/handlers/ingest_v1.go`, `report-listener/database/migration_helpers.go` | One canonical envelope exists, idempotency exists, provenance metadata is stored, and lanes exist. “All internal agents must use Wire” is not yet true, and rewards are only placeholders. |
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
| Queue and processing architecture | Partial | `report-listener/handlers/cleanapp_wire_v1.go`, `report-listener/handlers/ingest_v1.go`, `report-listener/config/config.go`, `report-analyze-pipeline` consumers | Wire currently publishes into the existing `report.raw` flow through v1 ingest. The dedicated `casp.*` / Wire-native queue graph from the spec does not exist yet. |
| Governance and auditability | Partial | `report-listener/database/cleanapp_wire_v1.go`, `report-listener/database/migration_helpers.go`, `report-listener/handlers/internal_fetcher_admin.go`, `report-listener/handlers/fetcher_promotion_v1.go` | Submission records, receipts, promotion requests, and moderation events exist. Full decision traces, rule-versioning, and reconstruction of every lane decision are not yet implemented. |
| Rollout plan | Partial | `report-listener/main.go`, `cli/cleanapp`, `openclaw/cleanapp_ingest_skill`, `news-indexer-bluesky/src/bin/submitter_bluesky.rs` | Phase 1 and part of Phase 2 exist. Internal-bot migration is incomplete. External agent onboarding exists only partially because CLI and skill packages still target v1 fetcher ingest, not Wire. |
| Operational metrics | Partial | `report-listener/database/fetcher_keys_v1.go`, `report-listener/database/cleanapp_wire_v1.go`, `report-listener/database/ingestion_audit_v1.go` | Basic usage quotas and ingestion audits exist. The richer operational metrics suite from the spec is not fully implemented. |
| Non-negotiable rules | Partial | `report-listener/main.go`, `news-indexer-bluesky/src/bin/submitter_bluesky.rs`, `openclaw/cleanapp_ingest_skill/ingest.py`, `cli/cleanapp/src/commands/reports/submit.ts`, `report-processor/handlers/handlers.go` | Rule 1 is currently false: not all internal agentic ingestion goes through Wire. Rule 3 is mostly true. Rules around rewards, provenance integrity, and duplicate-vs-corroboration are only partial. |

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
5. translate the submission into a single-item v1 fetcher-ingest request
6. insert `reports` + `report_raw`
7. publish to `report.raw`
8. persist Wire submission/receipt rows
9. increment Wire reputation sample count

Files:

- `report-listener/handlers/cleanapp_wire_v1.go`
- `report-listener/handlers/ingest_v1.go`
- `report-listener/database/cleanapp_wire_v1.go`

This means Wire is currently a canonical envelope and receipt layer, but not yet a fully standalone canonical ingest engine.

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

These still bypass Wire entirely or target older ingestion surfaces directly.

### 1. Legacy protected v3/v4 bulk ingest

Files:

- `report-listener/main.go`
- `report-listener/handlers/handlers.go`

Routes:

- `POST /api/v3/reports/bulk_ingest`
- `POST /api/v4/reports/bulk_ingest`

Why it bypasses Wire:

- no Wire receipt
- no Wire quality score
- no Wire lane assignment
- no Wire reputation tracking

### 2. Fetcher v1 ingest surface

Files:

- `report-listener/main.go`
- `report-listener/handlers/ingest_v1.go`
- `report-listener/database/fetcher_keys_v1.go`

Route:

- `POST /v1/reports:bulkIngest`

Why it bypasses Wire:

- no Wire envelope
- no Wire receipt/status model
- no Wire quality scoring
- no Wire-specific reputation update beyond generic fetcher usage

Important nuance:

Wire currently calls this path internally. So v1 is both:

- a bypass path when called directly
- the current underlying ingest implementation used by Wire

### 3. OpenClaw / agent skill package

Files:

- `openclaw/cleanapp_ingest_skill/manifest.json`
- `openclaw/cleanapp_ingest_skill/SKILL.md`
- `openclaw/cleanapp_ingest_skill/README.md`
- `openclaw/cleanapp_ingest_skill/ingest.py`

Why it bypasses Wire:

- still targets `/v1/reports:bulkIngest`
- does not use Wire receipts/reputation/lane semantics

### 4. CleanApp CLI

Files:

- `cli/cleanapp/src/commands/auth/whoami.ts`
- `cli/cleanapp/src/commands/reports/submit.ts`
- `cli/cleanapp/src/commands/reports/bulk_submit.ts`

Why it bypasses Wire:

- `auth whoami` targets `/v1/fetchers/me`
- submit commands target `/v1/reports:bulkIngest`
- no Wire-native commands yet

### 5. Bluesky submitter

Files:

- `news-indexer-bluesky/src/bin/submitter_bluesky.rs`

Why it bypasses Wire:

- posts to `/api/v3/reports/bulk_ingest`
- is a stable machine producer that should have first-class provenance and reputation

### 6. Report processor direct submit + raw publish

Files:

- `report-processor/handlers/handlers.go`

Why it bypasses Wire:

- submits to another report-creation endpoint directly
- publishes `report.raw` directly
- no agent identity
- no Wire receipt
- no Wire lane assignment
- no Wire reputation or promotion path

### 7. Internal admin promotion path

Files:

- `report-listener/handlers/internal_ingest_admin.go`

Why it bypasses Wire:

- it is intentionally internal/admin-only
- it updates visibility/trust directly and can publish `report.analysed` from DB state
- this is not a problem by itself, but it is outside the Wire provenance path

## Internal Producers To Wrap or Migrate First

### Priority 1: Bluesky submitter

Files:

- `news-indexer-bluesky/src/bin/submitter_bluesky.rs`

Why first:

- already batch-oriented
- already has stable external identifiers (`uri`)
- already machine-originated
- high-value provenance win with low migration complexity

What it should gain:

- stable source identity in Wire
- lane assignment
- receipts
- future reputation tracking

### Priority 2: CleanApp CLI

Files:

- `cli/cleanapp/src/commands/auth/whoami.ts`
- `cli/cleanapp/src/commands/reports/submit.ts`
- `cli/cleanapp/src/commands/reports/bulk_submit.ts`

Why second:

- this is the public developer tool
- if Wire is canonical, the CLI should teach Wire, not v1 fetcher ingest

What it should gain:

- immediate alignment between docs and reality
- stable path for external agents and partner automations

### Priority 3: OpenClaw skill package

Files:

- `openclaw/cleanapp_ingest_skill/*`

Why third:

- same reason as CLI, but specifically for autonomous agent ecosystems
- should align with the canonical machine-ingest story

What it should gain:

- direct Wire receipts and status
- cleaner policy story for external swarms

### Priority 4: Legacy v3 bulk-ingest producers

Files:

- `report-listener/handlers/handlers.go`
- `news-indexer-bluesky/src/bin/submitter_bluesky.rs`
- any other submitters still posting to `/api/v3/reports/bulk_ingest`

Why fourth:

- these are still machine-ingest paths, but they use the older route
- they should either:
  - be migrated to Wire, or
  - be wrapped into Wire internally

### Priority 5: Report processor direct path

Files:

- `report-processor/handlers/handlers.go`

Why fifth:

- this is the most architecturally important bypass, but also the highest-risk migration
- it likely needs an explicit internal-Wire adapter, not a naive endpoint swap

## Legacy Migration Summary

Current state:

- Wire exists and works.
- v1 fetcher ingest still exists and is the actual persistence/publish core.
- several internal and external machine producers still bypass Wire.

Recommended migration order:

1. `news-indexer-bluesky` -> move to Wire
2. `cli/cleanapp` -> add Wire-native auth/submit/status commands and make them the default machine path
3. `openclaw/cleanapp_ingest_skill` -> retarget to Wire
4. selected legacy `/api/v3/reports/bulk_ingest` producers -> migrate or wrap
5. `report-processor` -> design an internal Wire adapter and migrate last

Migration policy recommendation:

- Do not delete v1 or v3 ingest immediately.
- First migrate callers.
- Then convert old machine-ingest routes into thin wrappers over Wire.
- Only after that should Wire become the sole canonical machine-ingest core.

## Top 5 Production Risks

### 1. Wire is not yet the canonical machine-ingest path

Risk:

- reputation, lane, receipt, and provenance behavior will remain fragmented
- future policies can silently apply only to part of machine traffic

### 2. Wire still depends on v1 ingest internals

Risk:

- any logic drift in `/v1/reports:bulkIngest` affects Wire behavior
- there are effectively two sources of truth for ingest semantics

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

### 5. Public tooling still teaches the old path

Risk:

- developers and agents will keep integrating against v1 fetcher ingest instead of Wire
- this locks in the bypass problem

## Next 3 Smallest Production-Safe PRs

### PR 1: Make CLI Wire-native

Scope:

- add Wire-native commands or flip existing machine-submit commands to:
  - `POST /api/v1/agent-reports:submit`
  - `POST /api/v1/agent-reports:batchSubmit`
  - `GET /api/v1/agents/me`
  - `GET /api/v1/agent-reports/status/{source_id}`

Why this is safe:

- low blast radius
- no runtime ingestion-core changes
- immediately aligns public developer tooling with Wire

### PR 2: Retarget OpenClaw skill package to Wire

Scope:

- change `openclaw/cleanapp_ingest_skill` from `/v1/reports:bulkIngest` to Wire endpoints
- update docs and dry-run examples

Why this is safe:

- no backend behavior change
- closes a major ecosystem/documentation mismatch

### PR 3: Migrate Bluesky submitter to Wire

Scope:

- have `news-indexer-bluesky/src/bin/submitter_bluesky.rs` emit a Wire envelope
- preserve stable `source_id` via Bluesky URI
- keep retry behavior idempotent

Why this is safe:

- one contained internal producer
- high-value provenance gain
- minimal risk if rolled behind a config flag or alternate endpoint variable

## Recommended Canonical Direction

If CleanApp Wire is intended to become the canonical ingestion layer for all machine-originated and machine-assisted traffic, the architectural end state should be:

1. all machine producers submit Wire envelopes
2. Wire owns canonical machine-ingest persistence and receipt generation directly
3. legacy v1/v3 machine-ingest routes either disappear or become thin compatibility wrappers into Wire
4. reputation, lane assignment, dedupe, and later rewards are computed from one shared machine-ingest path

That end state is not yet true today, but the current implementation is a credible Phase 1/2 base to get there.
