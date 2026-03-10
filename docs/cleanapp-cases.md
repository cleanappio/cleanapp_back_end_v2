# CleanApp Cases

Implementation-reality notes for the first shipped backend slice of Cases.

## What exists now

Cases are a durable backend object for grouping multiple reports around one incident and tracking:

- linked reports
- saved cluster snapshots
- suggested escalation targets
- status changes
- audit events

This first slice is intentionally conservative:

- polygon/geometry and nearby-report analysis are implemented
- case creation and report linking are implemented
- escalation target suggestion is implemented
- case-specific outbound email drafting, sending, and delivery tracking are implemented
- automatic case matching/reopen logic is **not** implemented yet

## Data model

Owned by `report-listener` migrations:

- `saved_clusters`
- `cases`
- `case_reports`
- `case_clusters`
- `case_escalation_targets`
- `case_escalation_actions`
- `case_email_deliveries`
- `case_resolution_signals`
- `case_audit_events`

Migration step:

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/migrate.go`
- step `0009_case_tables`

## API

### Public cluster analysis

These endpoints analyze a scope and return:

- reports in scope
- severity/time stats
- incident hypotheses
- suggested escalation targets

Routes:

- `POST /api/v3/clusters/analyze`
- `POST /api/v3/clusters/from-report`
- `POST /api/v4/clusters/analyze`
- `POST /api/v4/clusters/from-report`

### Protected case routes

These routes require a bearer access token. `report-listener` now validates JWTs locally using the shared auth/token store.

Routes:

- `POST /api/v3/cases`
- `GET /api/v3/cases/:case_id`
- `POST /api/v3/cases/:case_id/reports`
- `POST /api/v3/cases/:case_id/status`
- `GET /api/v3/cases/:case_id/escalations`
- `POST /api/v3/cases/:case_id/escalations/draft`
- `POST /api/v3/cases/:case_id/escalations/send`
- `POST /api/v4/cases`
- `GET /api/v4/cases/:case_id`
- `POST /api/v4/cases/:case_id/reports`
- `POST /api/v4/cases/:case_id/status`
- `GET /api/v4/cases/:case_id/escalations`
- `POST /api/v4/cases/:case_id/escalations/draft`
- `POST /api/v4/cases/:case_id/escalations/send`

### Case escalation flow

The report-listener now acts as the public/authenticated case facade.

It:

- drafts a case escalation email from case title/summary/reports
- creates durable `case_escalation_actions`
- calls the email-service internal endpoint
- records per-recipient delivery truth in `case_email_deliveries`

The email-service remains the actual SendGrid executor. It is not bypassed.

Internal email-service route:

- `POST /internal/case-escalations/send`

Protected by:

- `INTERNAL_ADMIN_TOKEN`

Case escalation API responses now expose:

- targets
- actions
- deliveries
- provider message ids
- sent timestamps
- per-recipient failures

## Authentication

Case writes are protected by:

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/middleware/auth.go`

This middleware expects:

- `Authorization: Bearer <access_token>`

And validates using:

- `cleanapp-common/authx`

`report-listener` must receive:

- `JWT_SECRET`

## Cluster analysis behavior

The first clustering heuristic is intentionally explainable, not magical.

Reports are grouped using weighted similarity from:

- same classification
- similar severity
- shared incident language
- near-identical physical location
- same organization/brand when present

The response returns incident hypotheses with:

- representative report
- confidence
- severity
- urgency
- rationale list

## Current limitations

1. A polygon is a workspace scope, not automatically a case.
2. New reports are not yet auto-matched into open cases.
3. `report_clusters` is not the canonical case backbone; the new case tables are.
4. Case escalation currently sends simple text/html summaries; richer attachment/memo generation is still future work.

## Intended next steps

1. Add case detail views on web.
2. Add automatic case matching/reopen suggestions during analysis.
3. Make mobile case-aware after web workspace flow is stable.
4. Improve case escalation drafting with richer memo/attachment generation.
