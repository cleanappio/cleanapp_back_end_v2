# Fetcher Key System + Quarantine Ingest (v1)

This repo now supports a **public ingest surface** for external agent swarms (e.g. OpenClaw/ClawHub) to submit bulk reports safely.

Key properties:
- **Keys shown once** on creation (Stripe-style). DB stores **hash only**.
- **Rate-limited** per key/fetcher (per-minute + daily caps).
- **Idempotent** via `(fetcher_id, source_id)` uniqueness.
- **Quarantine by default**: reports are stored + analyzed, but **not publicly visible** and **do not trigger routing/notifications/rewards**.

## 1) Production DB Patch (Required Before Deploy)

Production must apply:
- `db/patches/20260214_fetcher_keys_quarantine.sql`

This introduces:
- `fetcher_keys` (hashed keys)
- `ingestion_audit` (append-only logs)
- `report_raw` (quarantine visibility + idempotency metadata)
- `fetcher_usage_minute`, `fetcher_usage_daily` (quota buckets)
- governance fields on `fetchers`

If `report_raw` is missing, the updated code will fail queries that reference it. Apply the patch before rolling out the new binaries.

## 2) Public Endpoints

These live on **report-listener**.

### Register (one-time key issuance)

`POST /v1/fetchers/register`

Body:
```json
{ "name": "my-agent", "owner_type": "openclaw" }
```

Response (key is returned once):
```json
{
  "fetcher_id": "...",
  "api_key": "cleanapp_fk_test_<key_id>_<secret>",
  "status": "active",
  "tier": 0,
  "caps": { "per_minute_cap_items": 20, "daily_cap_items": 200 },
  "scopes": ["report:submit","fetcher:read"]
}
```

### Introspection

`GET /v1/fetchers/me`

Auth:
`Authorization: Bearer <api_key>`

Returns fetcher status/tier/caps + usage.

### Bulk ingest (quarantine lane)

`POST /v1/reports:bulkIngest`

Auth:
`Authorization: Bearer <api_key>`

Body:
```json
{
  "items": [
    {
      "source_id": "required-unique-per-fetcher",
      "title": "...",
      "description": "...",
      "lat": 47.36,
      "lng": 8.55,
      "collected_at": "2026-02-14T00:00:00Z",
      "agent_id": "openclaw",
      "agent_version": "1.2.3",
      "source_type": "web"
    }
  ]
}
```

Behavior:
- Validates batch/body size.
- Enforces per-minute + daily caps.
- Inserts into `reports` and records metadata in `report_raw`.
- Sets `report_raw.visibility='shadow'` and `trust_level='unverified'`.
- Publishes `report.raw` for analysis.

## 3) Quarantine Semantics

Quarantine (shadow) means:
- **Not visible** via public endpoints/WS feed.
- Analyzer still writes `report_analysis`, but **does not publish** `report.analysed` (so downstream routing stays quiet).
- Email service **skips** shadow reports (no notifications).

## 4) Promotion / Kill Switches (Internal)

Protected by `INTERNAL_ADMIN_TOKEN`:
- `POST /internal/reports/{seq}/promote` (set `visibility` + `trust_level`)
- `POST /internal/fetchers/{fetcher_id}/suspend`
- `POST /internal/fetchers/keys/{key_id}/revoke`

Header:
`X-Internal-Admin-Token: <token>`

## 5) OpenAPI + Swagger UI

- OpenAPI YAML: `GET /v1/openapi.yaml`
- Swagger UI: `GET /v1/docs`

