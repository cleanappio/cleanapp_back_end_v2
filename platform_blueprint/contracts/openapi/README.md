# OpenAPI Contracts

The recommended contract-first integration approach is:
1. Treat `/api/v4` OpenAPI as canonical.
2. Commit the spec here.
3. Generate clients for frontend/mobile.
4. Run smoke tests against staging/prod.

Baseline spec captured from prod xray snapshot:
- `api_v4_openapi.json` (copied from `xray/prod/2026-02-07/api_v4_openapi.json`)

