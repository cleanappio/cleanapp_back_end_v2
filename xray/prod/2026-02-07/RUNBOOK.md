# CleanApp Prod Runbook (VM: cleanapp-prod)

Snapshot date: **2026-02-07**  
Prod VM: `deployer@34.122.15.16`

This runbook is designed to be used while SSH’d into the prod VM. It is intentionally **secrets-safe** (no `.env` values).

## 0. Conventions

- Compose project file: `/home/deployer/docker-compose.yml`
- Compose project name: `deployer`
- Nginx config directory: `/etc/nginx/conf.d`
- Docker network: `deployer_default`

## 1. Quick “Is It On Fire?” Checks

### Nginx

```bash
sudo nginx -t
sudo systemctl status nginx --no-pager
```

### Docker + compose state

```bash
sudo docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
cd /home/deployer
sudo docker compose ps
sudo docker compose ps --all
```

### Health endpoints (localhost on VM)

```bash
curl -sS http://127.0.0.1:9081/health
curl -sS http://127.0.0.1:9081/api/v3/reports/health
curl -sS http://127.0.0.1:9097/api/v4/health
curl -sS http://127.0.0.1:9097/api/v4/openapi.json | head
```

## 2. Restart Procedures

### Compose-managed services

```bash
cd /home/deployer
sudo docker compose restart <service>
sudo docker compose logs -f --tail=200 <service>
```

Examples:
```bash
cd /home/deployer
sudo docker compose restart cleanapp_report_listener
sudo docker compose restart cleanapp_report_listener_v4
sudo docker compose restart cleanapp_customer_service
```

### Manual (non-compose) containers

Manual containers at snapshot time:
- `cleanapp_service`
- `cleanapp_report_analyze_pipeline`
- `cleanapp_frontend`
- `cleanapp_frontend_embedded`
- `cleanapp_bluesky_analyzer`
- `cleanapp_bluesky_now`

Restart:
```bash
sudo docker restart cleanapp_service
sudo docker restart cleanapp_report_analyze_pipeline
sudo docker restart cleanapp_frontend
sudo docker restart cleanapp_frontend_embedded
```

Tail logs:
```bash
sudo docker logs -f --tail=200 cleanapp_service
```

## 3. Common Failure Modes

### 502 / Bad Gateway

1. Confirm nginx can reach the upstream locally:
   - Look up the upstream port in `/etc/nginx/conf.d/*.conf`.
   - Then:
     ```bash
     curl -sv http://127.0.0.1:<port>/health 2>&1 | head
     ```
2. Confirm the backing container is running and exposes the expected host port:
   ```bash
   sudo docker ps --format "{{.Names}}\t{{.Ports}}" | grep <port>
   ```

### Container exits immediately

```bash
sudo docker logs --tail=200 <container>
sudo docker inspect <container> --format '{{json .State}}' | jq
```

## 4. RabbitMQ Ops

RabbitMQ is a container: `cleanapp_rabbitmq`.

### Topology

```bash
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_exchanges name type durable auto_delete internal
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name messages_ready messages_unacknowledged consumers
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_bindings source_name destination_name destination_kind routing_key
```

### If queues back up

1. Identify which consumer is stuck (queue consumers count, container logs).
2. Restart that consumer container first.
3. If needed, temporarily reduce input rate (pause producers) rather than purging queues blindly.

## 5. Database Ops (MySQL)

MySQL container: `cleanapp_db`.

### Basic connectivity (inside container)

You’ll need credentials (retrieved via GCP secrets). Prefer using the existing deploy scripts to materialize them temporarily:

```bash
cd /home/deployer
./up.sh   # re-creates .env briefly, runs compose, then removes .env
```

Then use mysql inside the container (example pattern):
```bash
sudo docker exec -it cleanapp_db bash
mysql -u server -p cleanapp
```

## 6. Deploy / Update Procedures (Current Reality)

### Current mechanism

Production deploy is operated via:
- `/home/deployer/up.sh` (fetch secrets via `gcloud secrets ...`, `docker compose up -d`, then remove `.env`)
- `/home/deployer/down.sh`

These scripts exist on the VM under `/home/deployer` (names/metadata captured in `home_deployer_scripts.txt`). Script contents are intentionally not captured/committed because they may contain secrets.

### Caveat: `docker-compose.yml` is not the whole truth

At snapshot time, multiple core services run outside compose (manual `docker run`). If you run `docker compose up -d` without reconciling that drift, you can hit container name conflicts or end up with duplicate services.

Before any big upgrade, reconcile the split control plane (see `UPGRADE_PUSH.md`).

## 7. nginx Changes

```bash
sudo nginx -t
sudo systemctl reload nginx
```

Configs live in `/etc/nginx/conf.d/*.conf` (captured in `nginx_conf_d/` in this snapshot).

## 8. Provenance / “What’s Deployed?”

Use immutable digests:
```bash
sudo docker inspect <container> --format '{{json .Image}}'
sudo docker inspect <container> --format '{{json .Config.Image}}'
sudo docker inspect <container> --format '{{json .RepoDigests}}'
```

Better: see the snapshot inventory in `REPORT.md` which records `repo_digest` per running container at snapshot time.
