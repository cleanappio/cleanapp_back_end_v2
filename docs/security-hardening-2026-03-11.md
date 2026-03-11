# CleanApp Security Hardening

Date: March 11, 2026

## Design Principle

Keep the public product experience intact:

- the full-history globe remains dense and visually unchanged
- canonical public report URLs remain stable via `public_id`
- search, browse, and manual report reading remain public

Harden the system underneath that experience by separating:

- visualization from stable identifiers
- public read paths from export-like bulk APIs
- human submission from machine submission
- provenance-aware ingest from legacy compatibility routes

## Implemented In This Slice

### Write plane

- Added a `human-reports` ingest facade to `report-listener`:
  - `POST /api/v1/human-reports/submit`
  - `GET /api/v1/human-reports/receipts/:receipt_id`
  - aliased under `api/v3` and `api/v4` for compatibility
- Reused the existing CleanApp Wire ingest core rather than creating a second submission pipeline.
- Extended `wire_submissions_raw` with generalized provenance fields:
  - `actor_kind`
  - `channel`
  - `auth_method`
  - `risk_score`
- Added migration `0013_wire_submission_actor_columns`.
- Legacy `/report` no longer writes directly to the database; it proxies into the canonical human-ingest path.
- Public `/update_or_create_user` no longer mints direct reward side effects through the old route.

### Machine ingest

- Self-registration for fetchers can now be disabled with `FETCHER_SELF_REGISTRATION_ENABLED`.
- When self-registration is enabled, newly created fetchers are provisioned as:
  - `pending`
  - read-only (`fetcher:read`)
  - no immediate submit scope
- Removed the `report-processor` fail-open Wire-to-legacy fallback. If Wire is selected and fails, submission now fails closed.

### Read plane

- Added `GET /api/v3/public/resolve-physical-point` and `GET /api/v4/public/resolve-physical-point`.
- Globe clicks can now resolve from map coordinates rather than depending on stable IDs in the point feed.
- Added `public_id` image endpoints:
  - `/api/v3/reports/image/by-public-id`
  - `/api/v3/reports/rawimage/by-public-id`
  - `/api/v4/reports/image/by-public-id`
  - `/api/v4/reports/rawimage/by-public-id`
- Applied detail-class rate limits to public image endpoints.
- Split websocket behavior by trust level:
  - `GET /api/v3/public/listen`
  - `GET /api/v4/public/listen`
  - `GET /api/v3/reports/listen/full`
  - `GET /api/v4/reports/listen/full`
- Anonymous websocket listeners now receive a public-lite live batch with:
  - timestamp
  - lat/lon for physical incidents
  - minimal localized titles/summaries
  - severity
  - short-lived discovery tokens
- The old `/reports/listen` path now downgrades anonymous callers to the public-lite stream instead of exposing the full analysis firehose by default.
- Public modal/list widgets now use tokenized discovery endpoints instead of the older anonymous `last` / `by-brand` / `by-latlng` report batch APIs.

### Edge hardening

- `report-listener` and the legacy backend now accept explicit trusted proxy configuration through `TRUSTED_PROXIES`.
- Public websocket origin checks no longer accept empty `Origin` headers.
- `report-listener-v4` detail throttling now keys off the actual socket peer instead of trusting spoofable forwarding headers.

### Frontend and mobile

- Globe physical pin clicks now resolve by map point coordinates, preserving the current map rendering while removing reliance on point-feed IDs.
- Mobile report submission now targets the canonical human-ingest endpoint.
- The mobile wallet settings screen no longer renders the private key or mnemonic phrase in the normal UI.

## Operational Outcome

This preserves the current visual product while materially improving:

- write-plane provenance
- resistance to naive point-feed scraping
- resistance to legacy route abuse
- resistance to public image walking
- resistance to spoofed detail-rate evasion

## Still Worth Doing Next

- finish migrating any remaining non-public/internal widgets that still rely on legacy bulk report reads
- move mobile secrets from AsyncStorage into platform secure storage
- bind privileged/full websocket access to stronger actor identity beyond admin/JWT fallback
- tighten anonymous Clean Intelligence session binding further if abuse pressure increases
- remove remaining public `seq` dependencies after all clients are off them
