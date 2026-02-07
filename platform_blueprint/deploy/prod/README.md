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
- fix known port reality (`cleanapp_service` should be on host port `8079`)
- add restart policies for long-running services
- bring `cleanapp_bluesky_now` under compose

