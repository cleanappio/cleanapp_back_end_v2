#!/usr/bin/env bash
#
# Deterministic prod deploys (immutable):
# - Pull tag-based images (e.g. :prod)
# - Resolve to image@sha256 digests from the locally pulled images
# - Write docker-compose digest override on the VM
# - `up -d` with that override
#
# This avoids mutable-tag drift and makes rollbacks predictable.
set -euo pipefail

HOST="${HOST:-deployer@34.122.15.16}"
REMOTE_DIR="${REMOTE_DIR:-/home/deployer}"

# Only pin images that come from our Artifact Registry namespace.
INTERNAL_PREFIX="${INTERNAL_PREFIX:-us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/}"

# Optional: space-separated compose service names to update (pull/pin/up only these).
SERVICES="${SERVICES:-}"

ssh "${HOST}" "REMOTE_DIR='${REMOTE_DIR}' INTERNAL_PREFIX='${INTERNAL_PREFIX}' SERVICES='${SERVICES}' bash -s" << 'REMOTE'
set -euo pipefail

cd "${REMOTE_DIR:-/home/deployer}"

if [[ ! -f docker-compose.yml ]]; then
  echo "ERROR: docker-compose.yml not found in $(pwd)" >&2
  exit 2
fi

compose_args=(-f docker-compose.yml)
if [[ -f docker-compose.override.yml ]]; then
  compose_args+=(-f docker-compose.override.yml)
fi

echo "== docker compose pull =="
if [[ -n "${SERVICES:-}" ]]; then
  # shellcheck disable=SC2086
  docker compose "${compose_args[@]}" pull ${SERVICES}
else
  docker compose "${compose_args[@]}" pull
fi

ts="$(date -u +%Y-%m-%dT%H%M%SZ)"
digest_file="docker-compose.digests.${ts}.yml"
current_link="docker-compose.digests.current.yml"

echo "== generate digest pins =="
INTERNAL_PREFIX="${INTERNAL_PREFIX:-us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/}" \
DIGEST_SERVICES="${SERVICES:-}" \
DIGEST_OUT="${digest_file}" \
python3 - << 'PY'
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone

internal_prefix = os.environ.get("INTERNAL_PREFIX", "").strip()
out_path = os.environ["DIGEST_OUT"]
services_filter_raw = os.environ.get("DIGEST_SERVICES", "").strip()
services_filter = set(services_filter_raw.split()) if services_filter_raw else None

SERVICE_RE = re.compile(r"^  ([A-Za-z0-9_-]+):\s*$")
CONTAINER_NAME_RE = re.compile(r"^    container_name:\s*(.+?)\s*$")
IMAGE_RE = re.compile(r"^    image:\s*(.+?)\s*$")

def strip_quotes(v):
    v = v.strip()
    if len(v) >= 2 and v[0] == v[-1] and v[0] in ("'", '"'):
        return v[1:-1].strip()
    return v

def parse_services(path):
    text = open(path, "r", encoding="utf-8", errors="replace").read().splitlines()
    in_services = False
    current = None
    services = {}
    for raw in text:
        line = raw.rstrip("\n")
        if not in_services:
            if line.strip() == "services:":
                in_services = True
            continue
        if line and not line.startswith(" ") and not line.lstrip().startswith("#"):
            break
        m = SERVICE_RE.match(line)
        if m:
            svc = m.group(1)
            current = services.get(svc, {"name": svc, "container_name": None, "image": None})
            services[svc] = current
            continue
        if current is None:
            continue
        m = CONTAINER_NAME_RE.match(line)
        if m:
            current["container_name"] = strip_quotes(m.group(1))
            continue
        m = IMAGE_RE.match(line)
        if m:
            current["image"] = strip_quotes(m.group(1))
            continue
    return services

def merge_services(*sets):
    merged = {}
    for s in sets:
        for name, v in s.items():
            m = merged.get(name, {"name": name, "container_name": None, "image": None})
            if v.get("container_name") is not None:
                m["container_name"] = v["container_name"]
            if v.get("image") is not None:
                m["image"] = v["image"]
            merged[name] = m
    return merged

def repo_from_image(img):
    if "@" in img:
        return img.split("@", 1)[0]
    # rsplit handles registries with ports.
    if ":" in img:
        return img.rsplit(":", 1)[0]
    return img

def inspect_repo_digests(img):
    raw = subprocess.check_output(["docker", "image", "inspect", "--format", "{{json .RepoDigests}}", img], text=True).strip()
    try:
        v = json.loads(raw)
        if isinstance(v, list):
            return [str(x) for x in v if x]
        return []
    except Exception:
        return []

def pinned_digest_for(img):
    if "@sha256:" in img:
        return img
    repo = repo_from_image(img)
    digests = inspect_repo_digests(img)
    if not digests:
        return None
    for d in digests:
        if d.startswith(repo + "@"):
            return d
    return digests[0]

compose = parse_services("docker-compose.yml")
override = parse_services("docker-compose.override.yml") if os.path.exists("docker-compose.override.yml") else {}
services = merge_services(compose, override)

lines = []
lines.append("# Generated file: docker-compose digest pins (runtime)")
lines.append("# Generated on VM from locally pulled images (no registry lookup).")
lines.append(f"# generated_at_utc: {datetime.now(timezone.utc).isoformat()}")
if internal_prefix:
    lines.append(f"# internal_prefix: {internal_prefix}")
lines.append("#")
lines.append("services:")

missing = []
emitted = 0

for svc_name in sorted(services.keys()):
    if services_filter is not None and svc_name not in services_filter:
        continue
    svc = services[svc_name]
    img = (svc.get("image") or "").strip()
    if not img:
        continue
    if internal_prefix and not img.startswith(internal_prefix):
        continue
    pinned = pinned_digest_for(img)
    if not pinned:
        missing.append(f"{svc_name} (image={img})")
        continue
    lines.append(f"  {svc_name}:")
    lines.append(f"    image: {pinned}")
    emitted += 1

if emitted == 0:
    sys.stderr.write("ERROR: no services were pinned; refusing to write digest file\\n")
    sys.exit(3)

if missing:
    lines.append("")
    lines.append("# WARNING: no digest found for these services (left tag-based):")
    for m in missing:
        lines.append(f"# - {m}")

open(out_path, "w", encoding="utf-8").write("\\n".join(lines) + "\\n")
print(emitted)
PY

ln -sf "${digest_file}" "${current_link}"

echo "== docker compose up (with digest pins) =="
if [[ -n "${SERVICES:-}" ]]; then
  # shellcheck disable=SC2086
  docker compose "${compose_args[@]}" -f "${current_link}" up -d --no-deps ${SERVICES}
else
  docker compose "${compose_args[@]}" -f "${current_link}" up -d --remove-orphans
fi

echo "OK: pinned override=${digest_file} (symlinked to ${current_link})"
REMOTE
