# CleanApp Backend v3 (Future Roadmap)

Status: **future optimization roadmap** (aspirational). This is **not** the current production architecture.

If you're looking for what is deployed today and what we're actively improving in v2, start here:
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/internal/OPTIMIZATION_STATUS.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/internal/febrefactor.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/ARCHITECTURE.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/xray/` (as-deployed snapshots)

## Objective

Define a "10/10 agent-native" target backend and a realistic migration strategy that gets us there **without breaking prod**.

Primary KPI: make the architecture **easier for coding agents (Codex + peers)** to reason about and change safely.

## Target: Fewer Deployables, Clearer Boundaries

Goal: collapse the current microservice sprawl into ~5 deployable units, while preserving the modular pipeline invariant.

- `edge-api`: public HTTP entrypoint. Responsibilities: auth, CORS, rate limiting, request validation, timeouts, OpenAPI, `/version`, consistent error model.
- `ingest`: accepts reports, enforces idempotency/quarantine, writes raw, publishes canonical events.
- `pipeline-workers`: analysis/tags/render/email as separate workers, but all using one shared worker framework (same retry/DLQ/idempotency semantics).
- `read-api`: dashboards/search/query built on explicit read models (materialized aggregates), optimized for p95/p99.
- `ops-admin`: promotion/quarantine tooling, fetcher keys, audit/backfills/replays.

## Contract-First Everywhere

HTTP:
- Canonical, versioned OpenAPI specs per surface (`ingest v1`, `read v1`, `ops v1`).
- Generated clients/types from OpenAPI so code stays aligned.

Events:
- Versioned event schemas for RabbitMQ (or future bus), ideally tracked as AsyncAPI or an equivalent schema doc.
- One shared consumer library that enforces bounded concurrency, ack-after-success, retries, DLQs, and idempotency hooks.

## DB Discipline and Migrations

Target state:
- One migration system and one authoritative patch history.
- No runtime DDL at service boot.
- Least-privilege DB users per component.
- Read models (counters/aggregates) are first-class and updated by workers.

This turns deploys from "hope it boots" into deterministic procedures: migrate once, then start services.

## Observability as a Product Feature

Target state:
- JSON logs everywhere with correlation IDs.
- Tracing across HTTP and queue boundaries (OpenTelemetry).
- Minimal SLO dashboards and alerts that map to user impact (ingest stale, analysis lag, email backlog, DLQ growth).
- Every deployable exposes `/health`, `/ready`, `/version`.

## One Way to Run, Test, and Deploy

Target state:
- Single entrypoints: `make dev-up`, `make test`, `make e2e`, `make deploy-prod`.
- CI gates include unit tests, contract checks (OpenAPI + event schemas), and a golden-path E2E that proves "ingest -> analyze -> read -> email decision".
- Production deploys are digest-pinned by default with a practiced rollback.

## Default Safety Posture

Target state:
- Secrets are injected at runtime (never in repo/VM scripts).
- Public surfaces enforce size limits, timeouts, and rate limits.
- Quarantine/shadow lane is the default for new external actors (fetchers/agents), with auditable promotion.

---

## Migration Strategy (Recommended): In-Prod Strangler

Do not "big-bang rewrite and flip the frontends".

Instead, run v3 components alongside v2 and cut over one surface at a time with shadowing and rollback.

### Why This Works for CleanApp

- Each cutover is reversible (route traffic back to v2).
- We can ship incremental value quickly (less complexity and fewer incidents without a flag day).
- We preserve uptime and user trust while changing internals.

### Mechanics: How We Cut Over Safely

1. Define and freeze external contracts (OpenAPI for HTTP, schemas for events) and encode them as tests.
2. Introduce `edge-api` first as a thin wrapper in front of existing services (centralize auth/cors/rate-limits/timeouts/versioning).
3. Migrate one worker lane at a time behind stable queues (tags, renderer, email, analysis), keeping idempotency and retry/DLQ semantics constant.
4. Move read endpoints to `read-api` backed by materialized read models (counters/aggregates), one endpoint-group at a time.
5. Move ingest to v3 only when needed (or for new external keys first), preserving backwards compatibility.

### Cutover Techniques

- Shadow mode: v3 computes/consumes but does not publish effects; compare outputs/metrics to v2.
- Canary: route small % of traffic or specific orgs/paths to v3.
- Rollback: switch routing back to v2 immediately if p95/errors regress.

### What "Done" Looks Like

- New work happens only in v3 surfaces.
- v2 surfaces are still running but see near-zero traffic.
- We delete v2 modules/services in controlled steps, guided by real traffic and contract coverage.

