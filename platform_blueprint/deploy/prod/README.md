# Prod Deploy (Blueprint)

This directory is where the platform repo would keep prod deployment artifacts:
- `docker-compose.yml` (single source of truth)
- `nginx_conf_d/*.conf` (public routing)
- scripts (upgrade/rollback/smoke)

For the current real-world deployed versions (2026-02-09 snapshot), see:
- `xray/prod/2026-02-09-postdlq2/prod_docker-compose.yml`
- `xray/prod/2026-02-09-postdlq2/nginx_conf_d/`

The first integration milestone is to make the platform repo’s `docker-compose.yml` match prod reality (no manual containers).

## Override File

`docker-compose.override.yml` is a small overlay intended to:
- add restart policies for long-running services
- bring `cleanapp_bluesky_now` under compose

Note: keep `cleanapp_service`'s host port mapping (`8079:8080`) in `docker-compose.yml` (not the override), because compose file merges append list fields like `ports`.

## Digest Pins (Deterministic Deploys)

For deterministic rollouts/rollbacks, prefer `image@sha256:...` pins rather than mutable tags.

Inputs:
- A captured digest manifest from xray: `platform_blueprint/manifests/prod/<date>.json`
- Optionally, a checked-in digest override under: `platform_blueprint/deploy/prod/digests/*.digests.yml`

Generate a digest-pinned override:

```bash
python3 platform_blueprint/deploy/generate_compose_digests.py \
  --manifest platform_blueprint/manifests/prod/2026-02-09-postdlq2.json \
  --compose platform_blueprint/deploy/prod/docker-compose.yml \
  --compose platform_blueprint/deploy/prod/docker-compose.override.yml \
  --out /tmp/docker-compose.digests.yml
```

Use it during deploy:

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml -f /tmp/docker-compose.digests.yml pull
docker compose -f docker-compose.yml -f docker-compose.override.yml -f /tmp/docker-compose.digests.yml up -d
```
