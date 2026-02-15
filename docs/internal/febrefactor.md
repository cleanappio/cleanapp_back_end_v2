# Feb Refactor (Lean + Performance + Agent-Friendliness)

This document tracks the **lean/perf refactors** we agreed to execute to move the backend from ~9.4/10 toward **10/10** on our internal rubric.

Primary KPI: **make the architecture easier for agents (Codex + peers)** to modify safely.

Secondary KPIs:
- Reduce DB load and tail latency.
- Reduce duplicated logic and drift across services.
- Keep all existing user-visible functionality and APIs stable (additive changes, safe fallbacks).

---

## Top 5 Work Items (Execution Order)

### 1) Eliminate `COUNT(DISTINCT ...)` hot paths + N-per-brand loops

**Why:** These queries are DB-heavy and appear in multiple services; they can cause multi-second to multi-minute stalls on large datasets.

**Evidence (prior hot paths, now refactored in PR #133):**
- Global counts:
  - `backend/db/db.go` (legacy helper; previously `COUNT(DISTINCT ra.seq)`)
- Brand dashboard:
  - `brand-dashboard/services/database_service.go` previously looped brands and ran a count query per brand.
- Tags feed count:
  - `report-tags/src/services/feed_service.rs` previously used `COUNT(DISTINCT r.seq)` because joins create duplicates.
- Ownership count:
  - `report-ownership-service/database/service.go` previously used `COUNT(DISTINCT seq)` on `reports_owners`.
- Email pipeline:
  - `email-service/service/email_service.go` previously counted total reports per brand with `COUNT(DISTINCT ...)`.

**New materialized tables (patch SQL):**
- `db/patches/20260215_report_counters.sql`
  - `report_counts_total` (global totals)
  - `brand_report_counts` (per-brand totals)
  - `counters_state` (incremental checkpoints)

**Approach:**
- Introduce small, materialized counters in MySQL:
  - `report_counts_total` (total/physical/digital valid counts; last_seq, updated_at)
  - `brand_report_counts` (per brand totals; last_seq, updated_at)
  - (optional later) `daily_counts` for trend windows
- Update counters incrementally in a single background job (prefer `cleanapp_service`), and have readers query the counters table(s).
- Implement safe fallbacks:
  - If counters tables do not exist yet, readers fall back to prior SQL (no hard failures).

**Expected improvements (order-of-magnitude):**
- Global counts endpoints: from seconds/minutes (full scan) to **<10ms** (cached row) once warm.
- Brand totals (dashboard + email subject/CTA): from repeated per-brand counts to **O(1)** lookup.
- Reduced DB contention during peak usage.

**Rollout:**
1. Add patch SQL to create counters tables.
2. Add background updaters:
   - Global totals persisted by `backend/server/reports_count.go`.
   - Brand totals updated by `backend/server/brand_counts.go` (started from `backend/server/server.go`).
3. Switch read paths to prefer counters tables (with safe fallbacks if tables are missing):
   - `backend/db/db.go`
   - `brand-dashboard/services/database_service.go`
   - `report-tags/src/services/feed_service.rs`
   - `report-ownership-service/database/service.go`
   - `email-service/service/email_service.go`
4. Confirm counts match previous semantics for 24h (log diff if mismatch).

---

### 2) Stop pulling `LONGBLOB` images in selection/list queries (lazy-load / two-step)

**Why:** Selecting `reports.image` in list queries can move tens of MB per call, increasing latency and memory pressure.

**Evidence (examples):**
- Email polling selects `r.image` for up to 500 rows per cycle (`email-service/service/email_service.go`).
- Analyzer and processors select images in scanning queries (`report-analyze-pipeline/database/database.go`, `report-processor/database/database.go`).

**Approach:**
- Two-step selection:
  1. Choose candidate seqs using light joins (no blobs).
  2. Fetch blobs for only the chosen seqs in one `IN (...)` query (bounded).
- Longer-term: split `report_images` table or move images to object storage (GCS).

**Expected improvements:**
- Reduce per-poll bytes moved by **5–20x** in pipelines that currently join blobs early.
- Lower p95 latency on list endpoints.

---

### 3) Standardize outbound HTTP clients/timeouts (and reuse transports)

**Why:** no-timeout HTTP calls can hang forever; new client per request prevents connection reuse.

**Evidence:**
- Multiple `&http.Client{}` with no `Timeout` in analyzer and openai clients.
- `http.Post` / `http.Get` without explicit timeouts in customer-service proxies.

**Approach:**
- Add a shared `common/httpclient` package with sane defaults:
  - `Timeout`
  - tuned Transport keepalives
  - request-scoped context deadlines
- Replace call sites with the shared client.

**Expected improvements:**
- Eliminates hung requests as a failure mode.
- Stabilizes p95/p99 under partial outages of dependencies.

---

### 4) Remove “schema on boot” DDL + drop `multiStatements=true` DSNs

**Why:** startup-time DDL can lock tables and makes deploy behavior non-deterministic; `multiStatements=true` increases blast radius for SQL injection.

**Evidence:**
- `report-analyze-pipeline/database/database.go` creates/migrates tables on boot.
- `email-service/service/email_service.go` creates email tracking tables on boot.
- Multiple services still use `multiStatements=true` DSNs.

**Approach:**
- Convert runtime DDL into explicit patch SQL in `db/patches/`.
- Make services fail-fast in production if required tables/columns are missing.
- Remove `multiStatements=true` once patches cover required schema.

**Expected improvements:**
- Faster, safer restarts; less “mystery prod behavior”.
- Agents can reason about schema via patch history instead of scattered runtime DDL.

---

### 5) Reduce code duplication / drift (Rust `cleanapp_rustlib` vendoring, shared Go helpers)

**Why:** duplicating shared libs increases bugfix cost and leads to subtle behavior drift.

**Evidence:**
- Multiple Rust crates use `vendor/cleanapp_rustlib` as a path dependency.

**Approach:**
- Move to a single workspace crate in-repo or one pinned git dependency.
- Keep one source of truth for RabbitMQ subscriber semantics and reconnect behavior.

**Expected improvements:**
- Lower maintenance latency; fewer regressions.
- Faster agent edits (one place to patch).

---

## Status
- [x] 1) Counters tables + query refactors (implemented in PR #133; pending merge)
- [ ] 2) Blob lazy-load/two-step in pipelines
- [ ] 3) Shared HTTP client/timeouts
- [ ] 4) Replace runtime DDL with patch SQL + remove `multiStatements=true`
- [ ] 5) Rust lib dedupe plan + execution
