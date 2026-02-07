# Cutover: Single Control Plane (Compose Owns Everything)

Snapshot baseline: `xray/prod/2026-02-07/`

Goal: eliminate the split between:
- compose-managed containers, and
- manual `docker run` containers that “shadow” compose services

So upgrades become predictable and rollbackable.

## Preconditions

1. Take a fresh snapshot with `/Users/anon16/Downloads/cleanapp_back_end_v2/xray/capture_prod_snapshot.sh`.
2. Confirm current drift:
   - `/home/deployer/docker-compose.yml` defines 36 services.
   - `docker compose ps --all` shows only 31 have containers.
   - Missing (as of snapshot): `cleanapp_service`, `cleanapp_frontend`, `cleanapp_frontend_embedded`, `cleanapp_report_analyze_pipeline`, `cleanapp_bluesky_analyzer`.
3. Confirm nginx routes that depend on manual containers:
   - `cleanapp.io` -> `cleanapp_frontend` on host port `3001`
   - `embed.cleanapp.io` -> `cleanapp_frontend_embedded` on host port `3002`
   - `api.cleanapp.io:8080` -> `cleanapp_service` on host port `8079`

## Step 1: Make Compose Safe to Own Core Services

Before moving containers under compose, update `/home/deployer/docker-compose.yml` so the compose versions match the manual versions:

1. Port mappings:
   - Ensure `cleanapp_service` uses `8079:8080` (matches current manual container and nginx `api.cleanapp.io:8080`).
2. Restart policies:
   - Add `restart: unless-stopped` to all critical services.
3. Healthchecks:
   - Add healthchecks for the key HTTP services (report listeners, auth, areas, customer service, report processor).

Do this first so the compose-created replacements behave well after cutover.

Alternative (often safer): add a `docker-compose.override.yml` next to `/home/deployer/docker-compose.yml` that contains only the overrides (ports/restart/new services), and run:
```bash
cd /home/deployer
sudo docker compose -f docker-compose.yml -f docker-compose.override.yml up -d
```

Important: compose file merge behavior appends list fields like `ports`. If you define `cleanapp_service` ports in both files, it will try to bind both host ports (and you will hit the `8080` vs nginx conflict). Prefer changing `docker-compose.yml` (or remove the base `ports:` entry and define ports only in the override).

## Step 2: Cut Over One Service at a Time

Run on the prod VM:

### 2.1 cleanapp_frontend (cleanapp.io)

```bash
sudo docker stop cleanapp_frontend
sudo docker rm cleanapp_frontend
cd /home/deployer
sudo docker compose up -d cleanapp_frontend
```

Validate:
```bash
curl -sS -o /dev/null -w "%{http_code}\n" https://cleanapp.io/
```

### 2.2 cleanapp_frontend_embedded (embed.cleanapp.io)

```bash
sudo docker stop cleanapp_frontend_embedded
sudo docker rm cleanapp_frontend_embedded
cd /home/deployer
sudo docker compose up -d cleanapp_frontend_embedded
```

Validate:
```bash
curl -sS -o /dev/null -w "%{http_code}\n" https://embed.cleanapp.io/
```

### 2.3 cleanapp_service (api.cleanapp.io:8080 -> 8079)

```bash
sudo docker stop cleanapp_service
sudo docker rm cleanapp_service
cd /home/deployer
sudo docker compose up -d cleanapp_service
```

Validate:
```bash
curl -sS -o /dev/null -w "%{http_code}\n" http://api.cleanapp.io:8080/
```

### 2.4 cleanapp_report_analyze_pipeline (async worker)

```bash
sudo docker stop cleanapp_report_analyze_pipeline
sudo docker rm cleanapp_report_analyze_pipeline
cd /home/deployer
sudo docker compose up -d cleanapp_report_analyze_pipeline
```

Validate:
- RabbitMQ queues stay near zero / consumers stay 1
- logs show steady processing

### 2.5 cleanapp_bluesky_analyzer (async worker)

```bash
sudo docker stop cleanapp_bluesky_analyzer
sudo docker rm cleanapp_bluesky_analyzer
cd /home/deployer
sudo docker compose up -d cleanapp_bluesky_analyzer
```

Validate:
- container stays up
- downstream submitter continues to submit to `cleanapp_report_listener`

## Step 3: Verify Drift Is Gone

After the 5 cutovers:
```bash
cd /home/deployer
sudo docker compose ps --all
```

Expected:
- All 36 services either have a running container, or are intentionally stopped (and are clearly documented as such).

## Step 4: Deal With bluesky_now

`cleanapp_bluesky_now` is currently a manual container not represented in compose.

Options:
1. Add it to compose explicitly (recommended).
2. Remove it if no longer needed (only after verifying it’s not required).

## Rollback Strategy

Rollback is per-service:
- stop the compose-created container
- re-run the known-good manual `docker run` (using the image digest recorded in the latest snapshot)

Longer term: pin compose images by digest to make rollback deterministic without rebuilding.
