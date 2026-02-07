# Prod Deploy (Blueprint)

This directory is where the platform repo would keep prod deployment artifacts:
- `docker-compose.yml` (single source of truth)
- `nginx_conf_d/*.conf` (public routing)
- scripts (upgrade/rollback/smoke)

For the current real-world deployed versions (2026-02-07 snapshot), see:
- `xray/prod/2026-02-07/prod_docker-compose.yml`
- `xray/prod/2026-02-07/nginx_conf_d/`

The first integration milestone is to make the platform repo’s `docker-compose.yml` match prod reality (no manual containers).

## Override File

`docker-compose.override.yml` is a small overlay intended to:
- add restart policies for long-running services
- bring `cleanapp_bluesky_now` under compose

Note: keep `cleanapp_service`'s host port mapping (`8079:8080`) in `docker-compose.yml` (not the override), because compose file merges append list fields like `ports`.
