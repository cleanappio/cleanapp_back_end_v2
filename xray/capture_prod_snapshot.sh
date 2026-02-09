#!/usr/bin/env bash
set -euo pipefail

# Secrets-safe prod "xray" capture script.
#
# What it does:
# - Connects to the prod VM via ssh
# - Captures docker/nginx/compose/rabbitmq + basic gcloud firewall context
# - Writes outputs under xray/prod/<YYYY-MM-DD> (by default)
#
# What it does NOT do:
# - Print or store secret values (it only captures env var *keys* and configs)
#
# Usage:
#   ./xray/capture_prod_snapshot.sh [YYYY-MM-DD] [OUTDIR]
#
# Flags:
#   --env <name>          Environment name (default: prod)
#   --host <user@ip>      SSH target (default: deployer@34.122.15.16)
#   --instance-name <n>   GCE instance name (default: cleanapp-<env>)
#   --prefix <p>          Prefix for output files (default: <env>)
#
# Env overrides:
#   HOST=deployer@34.122.15.16 ./xray/capture_prod_snapshot.sh
#
# Cross-env capture (writes into xray/<ENV_NAME>/<DATE> and prefixes files with <ENV_NAME>):
#   ENV_NAME=dev HOST=deployer@34.132.121.53 ./xray/capture_prod_snapshot.sh

HOST="${HOST:-deployer@34.122.15.16}"
ENV_NAME="${ENV_NAME:-prod}"
INSTANCE_NAME="${INSTANCE_NAME:-}"
PREFIX="${PREFIX:-}"
DEBUG="${DEBUG:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --debug)
      DEBUG=1
      shift 1
      ;;
    --env)
      ENV_NAME="${2:?missing value for --env}"
      shift 2
      ;;
    --host)
      HOST="${2:?missing value for --host}"
      shift 2
      ;;
    --instance-name)
      INSTANCE_NAME="${2:?missing value for --instance-name}"
      shift 2
      ;;
    --prefix)
      PREFIX="${2:?missing value for --prefix}"
      shift 2
      ;;
    --help|-h)
      echo "Usage: $0 [--env <name>] [--host <user@ip>] [--instance-name <name>] [--prefix <p>] [YYYY-MM-DD] [OUTDIR]"
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "Unknown flag: $1" >&2
      exit 2
      ;;
    *)
      break
      ;;
  esac
done

INSTANCE_NAME="${INSTANCE_NAME:-cleanapp-${ENV_NAME}}"
PREFIX="${PREFIX:-${ENV_NAME}}"
DATE="${1:-$(date +%F)}"
OUTDIR="${2:-$(pwd)/xray/${ENV_NAME}/${DATE}}"

if [[ "${DEBUG}" == "1" ]]; then
  set -x
fi

PING_FILE="${OUTDIR}/${PREFIX}_ping.txt"
COMPOSE_FILE="${OUTDIR}/${PREFIX}_docker-compose.yml"
ENV_KEYS_FILE="${OUTDIR}/${PREFIX}_env_keys.txt"

mkdir -p "${OUTDIR}"
mkdir -p "${OUTDIR}/nginx_conf_d"
mkdir -p "${OUTDIR}/container_env_keys" "${OUTDIR}/container_mounts" "${OUTDIR}/container_meta"

echo "[xray] host=${HOST}"
echo "[xray] env=${ENV_NAME} prefix=${PREFIX} instance=${INSTANCE_NAME}"
echo "[xray] outdir=${OUTDIR}"

ssh "${HOST}" "hostname && date -Is && uptime" | tee "${PING_FILE}" >/dev/null

ssh "${HOST}" "date -Is; hostname; uname -a; docker --version; docker compose version; sudo -n nginx -v 2>&1 || true" \
  | tee "${OUTDIR}/host_info.txt" >/dev/null

ssh "${HOST}" "sudo docker ps --format \"{{.Names}}\t{{.Image}}\t{{.ID}}\t{{.CreatedAt}}\t{{.Status}}\t{{.Ports}}\"" \
  | tee "${OUTDIR}/docker_ps.tsv" >/dev/null

ssh "${HOST}" "sudo docker ps -a --format \"{{.Names}}\t{{.Image}}\t{{.Status}}\"" \
  | tee "${OUTDIR}/docker_ps_all.tsv" >/dev/null

# Container manifest (running containers + immutable repo digests).
ssh "${HOST}" 'bash -s' <<'REMOTE' | tee "${OUTDIR}/containers_manifest.tsv" >/dev/null
set -euo pipefail
printf "name\tconfig_image\tcontainer_id\timage_id\tcreated\tstarted_at\tstate\thealth\tports\tnetworks\trepo_digests_json\n"
for name in $(sudo docker ps --format "{{.Names}}"); do
  config_image="$(sudo docker inspect "$name" --format '{{.Config.Image}}')"
  container_id="$(sudo docker inspect "$name" --format '{{.Id}}')"
  image_id="$(sudo docker inspect "$name" --format '{{.Image}}')"
  created="$(sudo docker inspect "$name" --format '{{.Created}}')"
  started_at="$(sudo docker inspect "$name" --format '{{.State.StartedAt}}')"
  state="$(sudo docker inspect "$name" --format '{{.State.Status}}')"
  health="$(sudo docker inspect "$name" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}')"
  ports="$(sudo docker ps --filter "name=^/${name}$" --format '{{.Ports}}')"
  networks="$(sudo docker ps --filter "name=^/${name}$" --format '{{.Networks}}')"
  repo_digests_json="$(sudo docker image inspect --format '{{json .RepoDigests}}' "$image_id" 2>/dev/null || echo '[]')"
  printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n" \
    "$name" "$config_image" "$container_id" "$image_id" "$created" "$started_at" "$state" "$health" "$ports" "$networks" "$repo_digests_json"
done
REMOTE

ssh "${HOST}" "cd /home/deployer && sudo docker compose ls" \
  | tee "${OUTDIR}/docker_compose_ls.txt" >/dev/null

ssh "${HOST}" "cd /home/deployer && sudo docker compose ps --format json" \
  | tee "${OUTDIR}/docker_compose_ps.json" >/dev/null

ssh "${HOST}" "cd /home/deployer && sudo docker compose ps --all --format json" \
  | tee "${OUTDIR}/docker_compose_ps_all.json" >/dev/null

ssh "${HOST}" "sudo docker network ls --format \"{{.Name}}\t{{.Driver}}\t{{.Scope}}\"" \
  | tee "${OUTDIR}/docker_networks.tsv" >/dev/null

ssh "${HOST}" "sudo docker network inspect deployer_default" \
  | tee "${OUTDIR}/docker_network_deployer_default.json" >/dev/null

ssh "${HOST}" "sudo docker volume ls --format \"{{.Name}}\t{{.Driver}}\"" \
  | tee "${OUTDIR}/docker_volumes.tsv" >/dev/null

ssh "${HOST}" "sudo docker volume inspect \$(sudo docker volume ls -q)" \
  | tee "${OUTDIR}/docker_volume_inspect.json" >/dev/null

ssh "${HOST}" "sudo nginx -T 2>/dev/null | head -n 200" \
  | tee "${OUTDIR}/nginx_T_head.txt" >/dev/null

# Pull all nginx *.conf files (no cert/key files).
ssh "${HOST}" "bash -lc 'cd /etc/nginx/conf.d; tar -czf - *.conf'" \
  | tar -xz -C "${OUTDIR}/nginx_conf_d"

# Save compose file separately (easy diffing).
ssh "${HOST}" "sudo cat /home/deployer/docker-compose.yml" \
  | tee "${COMPOSE_FILE}" >/dev/null

# Deployer directory: capture filenames/metadata only (NOT script contents).
# Rationale: deploy scripts have previously contained hard-coded credentials.
ssh "${HOST}" "ls -la /home/deployer" \
  | tee "${OUTDIR}/home_deployer_ls.txt" >/dev/null || true

ssh "${HOST}" "ls -la /home/deployer/*.sh 2>/dev/null || true" \
  | tee "${OUTDIR}/home_deployer_scripts.txt" >/dev/null || true

ssh "${HOST}" "sha256sum /home/deployer/*.sh 2>/dev/null || true" \
  | tee "${OUTDIR}/home_deployer_scripts_sha256.txt" >/dev/null || true

ssh "${HOST}" "ls -la /home/deployer/*.env* 2>/dev/null || true" \
  | tee "${OUTDIR}/home_deployer_compose_env_files.txt" >/dev/null || true

# Extract env var names referenced in compose/up scripts (keys only).
(
  grep -hoE '\\$\\{[A-Z0-9_]+\\}' "${COMPOSE_FILE}" || true
) | sed 's/[${}]//g' | sort -u | tee "${ENV_KEYS_FILE}" >/dev/null

# RabbitMQ topology (safe).
ssh "${HOST}" "sudo docker exec cleanapp_rabbitmq rabbitmqctl list_exchanges name type durable auto_delete internal" \
  | tee "${OUTDIR}/rabbitmq_exchanges.tsv" >/dev/null || true

ssh "${HOST}" "sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name durable auto_delete arguments messages_ready messages_unacknowledged consumers" \
  | tee "${OUTDIR}/rabbitmq_queues.tsv" >/dev/null || true

ssh "${HOST}" "sudo docker exec cleanapp_rabbitmq rabbitmqctl list_bindings source_name destination_name destination_kind routing_key arguments" \
  | tee "${OUTDIR}/rabbitmq_bindings.tsv" >/dev/null || true

# RabbitMQ policies (safe; no credentials).
ssh "${HOST}" "sudo docker exec cleanapp_rabbitmq rabbitmqctl list_policies -p /" \
  | tee "${OUTDIR}/rabbitmq_policies.txt" >/dev/null || true

# Health endpoints on localhost (safe).
ssh "${HOST}" 'set -e; urls="http://127.0.0.1:9081/health http://127.0.0.1:9081/api/v3/reports/health http://127.0.0.1:9097/api/v4/health http://127.0.0.1:9097/api/v4/openapi.json"; for u in $urls; do code=$(curl -sS --max-time 5 -o /dev/null -w "%{http_code}" "$u" || true); echo -e "$u\t$code"; done' \
  | tee "${OUTDIR}/http_health_status.tsv" >/dev/null || true

ssh "${HOST}" "curl -sS --max-time 5 http://127.0.0.1:9081/health" \
  | tee "${OUTDIR}/report_listener_health.txt" >/dev/null || true

ssh "${HOST}" "curl -sS --max-time 5 http://127.0.0.1:9081/api/v3/reports/health" \
  | tee "${OUTDIR}/api_v3_reports_health.txt" >/dev/null || true

ssh "${HOST}" "curl -sS --max-time 5 http://127.0.0.1:9097/api/v4/health" \
  | tee "${OUTDIR}/api_v4_health.txt" >/dev/null || true

ssh "${HOST}" "curl -sS --max-time 10 http://127.0.0.1:9097/api/v4/openapi.json" \
  | tee "${OUTDIR}/api_v4_openapi.json" >/dev/null || true

# Listening ports + firewall (safe).
ssh "${HOST}" "sudo ss -lntup" | tee "${OUTDIR}/ss_listening.txt" >/dev/null || true
ssh "${HOST}" "sudo iptables -S 2>/dev/null || sudo nft list ruleset 2>/dev/null || true" \
  | tee "${OUTDIR}/firewall_rules.txt" >/dev/null

# Gcloud context (safe).
ssh "${HOST}" "gcloud config list --format=\"text(core.project)\" 2>/dev/null || true" \
  | tee "${OUTDIR}/gcloud_config.txt" >/dev/null

ssh "${HOST}" "gcloud compute instances list --filter=\"name=${INSTANCE_NAME}\" --format=\"value(name,zone,networkInterfaces[0].networkIP,tags.items.list())\" 2>/dev/null || true" \
  | tee "${OUTDIR}/gcloud_instance_info.txt" >/dev/null

ssh "${HOST}" "gcloud compute firewall-rules list --filter=\"targetTags:(allow-3000 OR allow-8080 OR allow-8090 OR allow-8091 OR http-server OR https-server)\" --format=\"table(name,network,direction,priority,sourceRanges.list(),allowed[].map().firewall_rule().list():label=ALLOWED,targetTags.list():label=TAGS)\" 2>/dev/null || true" \
  | tee "${OUTDIR}/gcloud_firewall_rules_relevant.txt" >/dev/null

ssh "${HOST}" "gcloud compute firewall-rules list --filter=\"network:default\" --format=\"table(name,direction,sourceRanges.list():label=SRC,allowed[].map().firewall_rule().list():label=ALLOWED,targetTags.list():label=TAGS)\" 2>/dev/null || true" \
  | tee "${OUTDIR}/gcloud_firewall_rules_default_network.txt" >/dev/null

# Per-container metadata dump (env keys, mounts, labels, meta) without leaking env values.
ssh "${HOST}" "bash -lc 'set -euo pipefail; out=/tmp/cleanapp_xray_export_${DATE}; rm -rf \"\$out\"; mkdir -p \"\$out/container_env_keys\" \"\$out/container_mounts\" \"\$out/container_meta\"; for name in \$(sudo docker ps -a --format \"{{.Names}}\"); do sudo docker inspect \"\$name\" --format \"{{range .Config.Env}}{{println .}}{{end}}\" | cut -d= -f1 | sort -u > \"\$out/container_env_keys/\$name.txt\"; sudo docker inspect \"\$name\" --format \"{{json .Mounts}}\" > \"\$out/container_mounts/\$name.mounts.json\"; sudo docker inspect \"\$name\" --format \"{{json .Config.Labels}}\" > \"\$out/container_meta/\$name.labels.json\"; sudo docker inspect \"\$name\" --format \"image={{.Config.Image}};created={{.Created}};restart={{.HostConfig.RestartPolicy.Name}};health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}};workdir={{.Config.WorkingDir}};user={{.Config.User}};entrypoint={{json .Config.Entrypoint}};cmd={{json .Config.Cmd}}\" > \"\$out/container_meta/\$name.meta.txt\"; done; tar -C \"\$out\" -czf - container_env_keys container_mounts container_meta'" \
  | tar -xz -C "${OUTDIR}"

echo "[xray] capture complete: ${OUTDIR}"
