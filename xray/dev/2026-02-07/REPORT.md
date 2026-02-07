# Dev Xray Snapshot (2026-02-07)

Source host: `deployer@34.132.121.53` (GCE instance `cleanapp-dev`)

This folder is a secrets-safe snapshot of the *as-deployed* dev VM state on **2026-02-07**.

## Key Takeaways

- Dev is running both report listener services:
  - `cleanapp_report_listener` (v3) on `:9081` (health `200`)
  - `cleanapp_report_listener_v4` (v4) on `:9097` (health `200`)
- The v3 and v4 endpoints are both reachable on localhost:
  - `http://127.0.0.1:9081/api/v3/reports/health` -> `200`
  - `http://127.0.0.1:9097/api/v4/health` -> `200`
  - `http://127.0.0.1:9097/api/v4/openapi.json` -> `200`

## Evidence Index

- Containers + immutable image digests: `containers_manifest.tsv`
- Compose state:
  - `docker_compose_ps.json`
  - `docker_compose_ps_all.json`
- Current compose file: `dev_docker-compose.yml`
- Nginx conf snapshot (no certs/keys): `nginx_conf_d/`
- Health checks captured by the script: `http_health_status.tsv`

## Notes (Dev vs Prod)

- Dev exposes `cleanapp_service` directly on host port `:8080`. Prod uses `:8079` because nginx binds host `:8080` on prod and proxies to `:8079`.
- On dev, `nginx` is not on the `deployer` PATH (it exists under root/system paths). The capture script uses `sudo nginx ...` accordingly.

