#!/usr/bin/env python3
"""
Generate a concise "as-deployed" report from the captured prod xray artifacts.

This script is intentionally secrets-safe: it only consumes already-sanitized
files (env keys, labels, mounts, manifests) and nginx/rabbitmq configs.
"""

from __future__ import annotations

import csv
import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple


BASE = Path(__file__).resolve().parent


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="replace")


def load_tsv_dicts(path: Path) -> List[Dict[str, str]]:
    with path.open("r", encoding="utf-8", errors="replace", newline="") as f:
        reader = csv.DictReader(f, delimiter="\t")
        return [dict(r) for r in reader]


def load_json(path: Path) -> Any:
    return json.loads(read_text(path))


def load_jsonl(path: Path) -> List[Any]:
    items: List[Any] = []
    with path.open("r", encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            items.append(json.loads(line))
    return items


def parse_ports_field(ports: str) -> List[Tuple[int, int, str]]:
    """
    Parse docker ps 'Ports' string into (host_port, container_port, proto).
    Example:
      "0.0.0.0:9097->8080/tcp, [::]:9097->8080/tcp"
    """
    if not ports:
        return []
    out: List[Tuple[int, int, str]] = []
    # Find "host:PORT->CONTAINER/proto"
    for m in re.finditer(r":(\d+)->(\d+)/(tcp|udp)", ports):
        out.append((int(m.group(1)), int(m.group(2)), m.group(3)))
    return out


@dataclass(frozen=True)
class NginxRoute:
    server_names: Tuple[str, ...]
    location: str
    upstream_host: str
    upstream_port: int
    file: str


def parse_nginx_routes(conf_text: str, filename: str) -> List[NginxRoute]:
    routes: List[NginxRoute] = []
    server_names: List[str] = []
    current_location: Optional[str] = None
    brace_depth = 0
    server_depth: Optional[int] = None
    location_depth: Optional[int] = None

    for raw in conf_text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            # ignore comments/blank lines
            brace_depth += line.count("{") - line.count("}")
            continue

        # Update depths after processing so "location ... {" is seen with current depth.
        if re.match(r"^server\s*\{$", line):
            server_names = []
            current_location = None
            server_depth = brace_depth + 1

        m_sn = re.search(r"server_name\s+([^;]+);", line)
        if m_sn and server_depth is not None:
            # Nginx allows multiple names separated by spaces.
            names = [n for n in m_sn.group(1).split() if n]
            server_names.extend(names)

        m_loc = re.match(r"^location\s+(\S+)\s*\{$", line)
        if m_loc and server_depth is not None:
            current_location = m_loc.group(1)
            location_depth = brace_depth + 1

        m_pp = re.search(r"proxy_pass\s+http://([^:/]+):(\d+)\s*;", line)
        if m_pp and server_depth is not None:
            loc = current_location or "/"
            routes.append(
                NginxRoute(
                    server_names=tuple(sorted(set(server_names))),
                    location=loc,
                    upstream_host=m_pp.group(1),
                    upstream_port=int(m_pp.group(2)),
                    file=filename,
                )
            )

        brace_depth += line.count("{") - line.count("}")

        # Leaving blocks
        if location_depth is not None and brace_depth < location_depth:
            current_location = None
            location_depth = None
        if server_depth is not None and brace_depth < server_depth:
            server_depth = None
            server_names = []
            current_location = None
            location_depth = None

    return routes


def main() -> int:
    out_lines: List[str] = []

    # Snapshot header
    out_lines.append("# CleanApp Prod Xray (As Deployed)")
    out_lines.append("")
    out_lines.append(f"Snapshot folder: `{BASE}`")
    out_lines.append("")

    host_info_path = BASE / "host_info.txt"
    if host_info_path.exists():
        out_lines.append("## Host")
        out_lines.append("")
        out_lines.append("```")
        out_lines.append(read_text(host_info_path).rstrip("\n"))
        out_lines.append("```")
        out_lines.append("")

    # Containers
    manifest_path = BASE / "containers_manifest.tsv"
    docker_ps_path = BASE / "docker_ps_refresh.tsv"
    compose_labels_path = BASE / "container_compose_labels.tsv"

    manifest_rows = load_tsv_dicts(manifest_path) if manifest_path.exists() else []

    # Map host_port -> container name (best-effort, using docker ps output)
    port_to_container: Dict[int, str] = {}
    if docker_ps_path.exists():
        with docker_ps_path.open("r", encoding="utf-8", errors="replace") as f:
            for line in f:
                parts = line.rstrip("\n").split("\t")
                if len(parts) < 6:
                    continue
                name = parts[0]
                ports = parts[5]
                for host_p, _container_p, _proto in parse_ports_field(ports):
                    port_to_container[host_p] = name

    compose_info: Dict[str, Tuple[Optional[str], Optional[str]]] = {}
    if compose_labels_path.exists():
        with compose_labels_path.open("r", encoding="utf-8", errors="replace") as f:
            for raw in f:
                raw = raw.rstrip("\n")
                if not raw:
                    continue
                parts = raw.split("\t")
                name = parts[0]
                if len(parts) == 2 and parts[1] == "(no-compose)":
                    compose_info[name] = (None, None)
                else:
                    project = parts[1] if len(parts) > 1 else None
                    service = parts[2] if len(parts) > 2 else None
                    compose_info[name] = (project, service)

    if manifest_rows:
        out_lines.append("## Running Containers (Snapshot)")
        out_lines.append("")
        out_lines.append(
            "Columns: name, compose_service, health, published_ports, image, repo_digest"
        )
        out_lines.append("")
        out_lines.append("| name | compose | health | ports | image | repo digest |")
        out_lines.append("|---|---|---|---|---|---|")
        for r in sorted(manifest_rows, key=lambda x: x.get("name", "")):
            name = r.get("name", "")
            _project, service = compose_info.get(name, (None, None))
            compose_cell = service or "(no-compose)"
            health = r.get("health", "")
            ports = r.get("ports", "")
            image = r.get("config_image", "")
            digests: List[str] = []
            try:
                digests = json.loads(r.get("repo_digests_json", "[]") or "[]")
            except Exception:
                digests = []
            digest = digests[0] if digests else ""
            out_lines.append(
                f"| `{name}` | `{compose_cell}` | `{health}` | `{ports}` | `{image}` | `{digest}` |"
            )
        out_lines.append("")

        # Compose drift summary
        no_compose = [n for (n, (p, s)) in compose_info.items() if p is None]
        out_lines.append("### Compose Drift")
        out_lines.append("")
        out_lines.append(
            f"- Compose project containers: {sum(1 for (_n, (p, _s)) in compose_info.items() if p is not None)}"
        )
        out_lines.append(f"- Non-compose (manually started) containers: {len(no_compose)}")
        if no_compose:
            out_lines.append("")
            out_lines.append("Non-compose container names:")
            out_lines.append("")
            for n in sorted(no_compose):
                out_lines.append(f"- `{n}`")
        out_lines.append("")

    # Nginx routing
    nginx_dir = BASE / "nginx_conf_d"
    routes: List[NginxRoute] = []
    if nginx_dir.exists():
        for p in sorted(nginx_dir.glob("*.conf")):
            routes.extend(parse_nginx_routes(read_text(p), p.name))

    if routes:
        out_lines.append("## Public Routing (nginx)")
        out_lines.append("")
        out_lines.append("| hostname | path | upstream | container | source |")
        out_lines.append("|---|---|---|---|---|")

        # Flatten server_names so each hostname shows rows
        flat: List[Tuple[str, NginxRoute]] = []
        for rt in routes:
            for hn in rt.server_names:
                flat.append((hn, rt))
        for hn, rt in sorted(flat, key=lambda t: (t[0], t[1].location, t[1].upstream_port)):
            upstream = f"{rt.upstream_host}:{rt.upstream_port}"
            container = port_to_container.get(rt.upstream_port, "")
            container_cell = f"`{container}`" if container else "(unknown)"
            out_lines.append(
                f"| `{hn}` | `{rt.location}` | `{upstream}` | {container_cell} | `{rt.file}` |"
            )
        out_lines.append("")

        out_lines.append("Notes:")
        out_lines.append("")
        out_lines.append("- Upstream ports are host ports on the VM (nginx proxies to `127.0.0.1:<port>`).")
        out_lines.append("- Some routes proxy to frontends (`3001`, `3002`) and multiple API backends.")
        out_lines.append("")

    # RabbitMQ topology
    ex_path = BASE / "rabbitmq_exchanges.tsv"
    q_path = BASE / "rabbitmq_queues.tsv"
    b_path = BASE / "rabbitmq_bindings.tsv"
    if ex_path.exists() or q_path.exists() or b_path.exists():
        out_lines.append("## RabbitMQ (Topology Snapshot)")
        out_lines.append("")
        if ex_path.exists():
            out_lines.append("### Exchanges")
            out_lines.append("")
            out_lines.append("```")
            out_lines.append(read_text(ex_path).rstrip("\n"))
            out_lines.append("```")
            out_lines.append("")
        if q_path.exists():
            out_lines.append("### Queues")
            out_lines.append("")
            out_lines.append("```")
            out_lines.append(read_text(q_path).rstrip("\n"))
            out_lines.append("```")
            out_lines.append("")
        if b_path.exists():
            out_lines.append("### Bindings")
            out_lines.append("")
            out_lines.append("```")
            out_lines.append(read_text(b_path).rstrip("\n"))
            out_lines.append("```")
            out_lines.append("")

    # Report listeners: health + API v4 OpenAPI summary
    out_lines.append("## Report Listener Services (Why Multiple Versions)")
    out_lines.append("")
    out_lines.append("- `cleanapp_report_listener` (Go/Gin) handles `/api/v3/*` and live websocket style usage.")
    out_lines.append("- `cleanapp_report_listener_v4` (Rust/Axum) handles `/api/v4/*` read-oriented endpoints and publishes OpenAPI.")
    out_lines.append("")

    health_map_path = BASE / "http_health_status.tsv"
    if health_map_path.exists():
        out_lines.append("### Health Checks (localhost)")
        out_lines.append("")
        out_lines.append("```")
        out_lines.append(read_text(health_map_path).rstrip("\n"))
        out_lines.append("```")
        out_lines.append("")

    openapi_path = BASE / "api_v4_openapi.json"
    if openapi_path.exists():
        try:
            openapi = load_json(openapi_path)
            paths = sorted(list((openapi.get("paths") or {}).keys()))
        except Exception:
            paths = []
        if paths:
            out_lines.append("### `/api/v4` Surface Area (from OpenAPI)")
            out_lines.append("")
            for p in paths:
                out_lines.append(f"- `{p}`")
            out_lines.append("")

    # Security + operational risks (high signal)
    out_lines.append("## High-Signal Findings")
    out_lines.append("")
    out_lines.append("- Multiple critical services are running outside `docker compose` (no compose labels), which makes upgrades/rollbacks harder.")
    out_lines.append("- RabbitMQ, MySQL, and RabbitMQ management ports are published on `0.0.0.0` (host-wide). This is risky unless the VM firewall restricts access.")
    out_lines.append("- Container images in prod are pinned by registry digest, but do not expose build provenance (no OCI revision labels), so digest-to-git mapping is currently manual.")
    out_lines.append("")

    # Top 5 optimizations (summary)
    out_lines.append("## Top 5 Optimizations (Recommended Next Upgrade Push)")
    out_lines.append("")
    out_lines.append("1. **Build provenance and version endpoints**: add `org.opencontainers.image.revision` labels and a standard `/version` endpoint in every service (git sha, build date, config version).")
    out_lines.append("2. **Deployment determinism**: bring *all* running containers under one orchestrated definition (compose + systemd), remove snowflake `docker run` containers, and document rollback procedure by image digest.")
    out_lines.append("3. **Network hardening**: bind MySQL/RabbitMQ/management to localhost or internal-only, and put any admin UIs behind auth/VPN; remove default RabbitMQ credentials.")
    out_lines.append("4. **Contract-driven integration**: treat `/api/v4` OpenAPI as the contract, generate clients for frontend/mobile, and add integration tests against staging (catches breaking changes before prod).")
    out_lines.append("5. **Pipeline efficiency**: introduce backpressure/visibility on RabbitMQ consumers (prefetch, DLQ, metrics) and reduce LLM spend with caching/idempotency around analysis and tagging jobs.")
    out_lines.append("")

    report_path = BASE / "REPORT.md"
    report_path.write_text("\n".join(out_lines) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

