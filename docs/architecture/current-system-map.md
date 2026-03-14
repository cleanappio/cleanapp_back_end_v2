# Current System Map

This document connects the canonical CleanApp philosophy docs to the system that is actually live today.

It is intentionally not a replacement for:

- `/Users/anon16/Downloads/cleanapp_back_end_v2/WHY.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/THEORY.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/INVARIANTS.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/ARCHITECTURE.md`

Those remain the canonical mission and system-design backbone.

Use this file when you need to answer:

- what is live today
- which subsystems are canonical vs transitional
- where the main logic currently lives in code

## Reading order

For the full system model:

1. `/Users/anon16/Downloads/cleanapp_back_end_v2/WHY.md`
2. `/Users/anon16/Downloads/cleanapp_back_end_v2/THEORY.md`
3. `/Users/anon16/Downloads/cleanapp_back_end_v2/INVARIANTS.md`
4. `/Users/anon16/Downloads/cleanapp_back_end_v2/ARCHITECTURE.md`
5. this file

## What is canonical today

### Ingest

Canonical machine-ingest core:

- CleanApp Wire on `report-listener`
- documented in `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/cleanapp-wire.md`

Canonical human-ingest facade:

- `POST /api/v1/human-reports/submit`
- implemented on top of the same Wire-backed ingest core
- documented in `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/security-hardening-2026-03-11.md`

Implementation center:

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/cleanapp_wire_v1.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/human_ingest.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/cleanapp_wire_v1.go`

### Public read model

Canonical public report identity:

- `public_id`

Canonical public report detail path:

- report pages keyed by `public_id`

Canonical read-plane design principle:

- public browsing stays open
- export-like bulk surfaces should be constrained
- visualization and stable identifiers are separated where possible

Implementation center:

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/main.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener-v4/src/main.rs`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/security-hardening-2026-03-11.md`

### Cases and clusters

Canonical case model:

- a case is a durable aggregate of one or more cluster snapshots
- a cluster is not automatically identical to a case
- new clusters may attach to an existing case rather than creating duplicates

Implementation center:

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/cases.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/cases.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/case_accumulation.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/cleanapp-cases.md`

### Contact routing and escalation

Canonical routing posture:

- defect-general, not physical-only
- shared routing engine for cases and solo reports
- responsibility routing is distinct from raw contact discovery
- notify plans are wave-based and multi-party
- outcome memory should improve routing over time

Implementation center:

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/case_contact_discovery.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/case_notify_routing.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/contact_routing_context.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/report_contact_strategy.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/subject_routing_strategy.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/case-contact-discovery-2026-03-11.md`

## Transitional areas

These parts of the system exist, but should be treated as transitional or compatibility surfaces rather than the cleanest expression of the architecture:

- legacy v2 backend compatibility routes
- older v3/v4 bulk-ingest compatibility callers
- older notification flows centered only on email-service polling
- docs that describe first shipped slices before later refactors

When a topical doc and code disagree:

- philosophy and intent: root docs win
- implementation reality: code wins
- this file should be updated to explain the gap

## Service map by responsibility

### `report-listener`

Primary responsibility:

- ingest edge
- public report APIs
- case APIs
- contact routing
- escalation plans and delivery records

### `report-listener-v4`

Primary responsibility:

- fast public renderer/read surfaces
- globe and lightweight public consumption paths

### `report-analyze-pipeline`

Primary responsibility:

- AI enrichment
- analysis generation
- report-level structure extraction

### `email-service`

Primary responsibility:

- actual email execution
- provider integration and delivery status

### Frontend and mobile

Primary responsibility:

- public browsing, report detail, case review, escalation workflows
- should reflect the shared backend routing model, not invent parallel logic

## Current high-level product loop

1. Ingest raw signal.
2. Preserve the raw report.
3. Enrich it with analysis and structure.
4. Compound signals into clusters and cases where appropriate.
5. Route to the best responsible and interested actors.
6. Execute outreach through the best available channels.
7. Learn from delivery, acknowledgment, and outcome signals.

## Supporting docs

- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/cleanapp-wire.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/cleanapp-cases.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/security-hardening-2026-03-11.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/case-contact-discovery-2026-03-11.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/architecture/domain-model.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/decisions/decision-log.md`
