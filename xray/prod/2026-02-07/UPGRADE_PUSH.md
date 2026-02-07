# Upgrade Push Plan (Based on Prod Xray)

Snapshot date: **2026-02-07**

This is the recommended “big upgrade push” plan, explicitly grounded in what’s deployed today.

## Objectives

1. Make prod deployments deterministic (same inputs -> same running system).
2. Make deployed versions auditable (digest -> git commit -> changelog).
3. Reduce fragility from the current split control plane (compose + manual containers).
4. Enable safe, contract-driven refactors across **mobile + frontend + backend**.
5. Improve security posture and runtime reliability.

## Phase 0: Lock Baseline (Now)

Already done for 2026-02-07:
- Capture running containers, image digests, nginx routing, RabbitMQ topology, and health endpoints.
- Generate `REPORT.md` and `AS_DEPLOYED.md`.

If repeating later, produce a new dated snapshot folder and diff it against this one.

## Phase 1: Unify the Control Plane (Highest ROI)

Problem (as deployed):
- 5 compose-defined services are **not** compose-managed (`cleanapp_service`, `cleanapp_frontend`, `cleanapp_frontend_embedded`, `cleanapp_report_analyze_pipeline`, `cleanapp_bluesky_analyzer`).
- Additional manual container exists (`cleanapp_bluesky_now`) outside compose entirely.

Target state:
- **Everything that should be running is managed by one mechanism** (recommended: docker compose + a systemd unit to bring it up on boot).

Concrete steps:
1. Inventory which containers are manual and why (restart policy, ports, env, network membership).
2. For each manual container, create a compose-managed replacement that matches:
   - image reference
   - port mappings
   - restart policy
   - network
3. Cut over one at a time:
   - stop manual container
   - `docker compose up -d <service>`
   - validate via localhost health + nginx route
4. Add `restart: always` (or `unless-stopped`) to all critical services in compose.
5. Add a systemd unit such as `cleanapp-compose.service` to run:
   - `docker compose up -d` on boot
   - `docker compose down` on stop (optional)

Deliverable from this phase:
- “Single source of truth” deployment config that matches prod reality.

## Phase 2: Provenance (Digest -> Commit)

Current issue:
- Containers in prod can be identified by digest, but most images don’t expose a revision label and the repo doesn’t have a guaranteed mapping from `:prod` -> digest -> commit.

Target state:
- Every image includes OCI labels:
  - `org.opencontainers.image.revision` (git sha)
  - `org.opencontainers.image.source` (repo url)
  - `org.opencontainers.image.created`
- Every service exposes a standard `/version` endpoint returning:
  - service name
  - git sha
  - build time
  - config version
  - runtime mode

Deliverable:
- A “deployed version report” can be generated from the VM without guessing.

## Phase 3: Contracts and Integration (Mobile + Frontend + Backend)

Recommendation:
- Treat `/api/v4` as the contract surface. The Rust report-listener v4 already publishes OpenAPI.

Target state:
1. Store OpenAPI specs in a shared “platform repo” and version them.
2. Generate typed clients for:
   - frontend (TypeScript)
   - mobile (TypeScript/Kotlin/Swift, depending on the stack)
3. Add integration tests that run against staging:
   - health checks
   - auth flows
   - report ingest -> analysis -> render -> query path

Deliverable:
- Breaking changes are caught in CI before they hit prod.

## Phase 4: Refactor + Upgrade (After the Ground Is Stable)

Once Phase 1-3 are complete, upgrades become safe:
- dependency upgrades (Go/Rust/Node)
- refactors across services
- consolidation (optional): gradually migrate remaining v3 usage to v4 where feasible, then retire v3 endpoints deliberately.

## Phase 5: Performance + Cost Optimizations

Grounded opportunities (as deployed):
1. RabbitMQ backpressure and reliability:
   - consumer prefetch tuning
   - DLQs for poison messages
   - metrics for queue depth + processing latency
2. LLM spend controls:
   - caching and idempotency keyed on report content hash
   - explicit max retries with dead-lettering
3. DB query hotspots:
   - add read-optimized endpoints (v4)
   - add indexes and avoid expensive COUNT DISTINCT patterns

## Platform Repo Blueprint (Recommended)

Create a new repo (suggested name: `cleanapp-platform`) that contains:
- `repos/` pointers:
  - `cleanapp-mobile` (submodule or pinned git sha)
  - `cleanapp-frontend` (submodule or pinned git sha)
  - `cleanapp-backend` (this repo, pinned sha)
- `contracts/`:
  - OpenAPI specs (`/api/v4` as canonical)
  - event schema docs (RabbitMQ routing keys, payload shapes)
- `deploy/`:
  - compose files
  - nginx templates
  - migration/run scripts
- `tests/`:
  - staging smoke tests (API + queue-driven flows)
  - canary checks

The platform repo becomes the place where upgrades happen “for real” across all components together.

## Leveraging Chai (ChatGPT) Programmatically

Treat “Chai knowledge” as something we convert into durable artifacts:
1. Create a `docs/decision-log.md` and `docs/incidents/` postmortems for every “pastiche debugging” session.
2. Extract invariants and encode them as:
   - integration tests
   - smoke checks
   - config validation scripts
3. Build a simple RAG index over `docs/` + OpenAPI + runbooks so the model can answer “what breaks if I change X?” from *your actual repository*, not memory.

## Execution Order (Minimal Downtime Bias)

1. Phase 1 (control plane) in small cuts.
2. Phase 2 (provenance) in parallel.
3. Phase 3 (contracts/tests) before any refactor.
4. Only then: Phase 4+5.

