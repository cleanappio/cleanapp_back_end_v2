#!/usr/bin/env python3
"""
Generate a docker-compose override that pins images by digest.

Why:
  - Tags like :prod are mutable; digests are immutable.
  - This script converts a captured runtime manifest (image@sha256) into a
    compose override you can use for deterministic deploys/rollbacks.

Typical usage (platform repo blueprint):
  python3 platform_blueprint/deploy/generate_compose_digests.py \
    --manifest platform_blueprint/manifests/prod/2026-02-09.json \
    --compose platform_blueprint/deploy/prod/docker-compose.yml \
    --compose platform_blueprint/deploy/prod/docker-compose.override.yml \
    --out /tmp/docker-compose.digests.yml

Then on the VM:
  docker compose -f docker-compose.yml -f docker-compose.override.yml -f /tmp/docker-compose.digests.yml pull
  docker compose -f docker-compose.yml -f docker-compose.override.yml -f /tmp/docker-compose.digests.yml up -d

Notes:
  - No external dependencies (no PyYAML). Compose YAML is parsed via indentation heuristics
    that match our current compose style (2-space service keys, 4-space fields).
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Tuple


_SERVICE_RE = re.compile(r"^  ([A-Za-z0-9_-]+):\s*$")
_CONTAINER_NAME_RE = re.compile(r"^    container_name:\s*(.+?)\s*$")
_IMAGE_RE = re.compile(r"^    image:\s*(.+?)\s*$")


@dataclass
class ComposeService:
    name: str
    container_name: Optional[str] = None
    image: Optional[str] = None


def _strip_quotes(v: str) -> str:
    v = v.strip()
    if len(v) >= 2 and v[0] == v[-1] and v[0] in ("'", '"'):
        return v[1:-1].strip()
    return v


def _parse_compose_services(path: Path) -> Dict[str, ComposeService]:
    """
    Extremely small YAML-ish parser:
      - finds services under a top-level 'services:' key
      - captures 'container_name' and 'image' fields (when present)
    """
    text = path.read_text(encoding="utf-8", errors="replace").splitlines()
    in_services = False
    current: Optional[ComposeService] = None
    services: Dict[str, ComposeService] = {}

    for raw in text:
        line = raw.rstrip("\n")
        if not in_services:
            if line.strip() == "services:":
                in_services = True
            continue

        m = _SERVICE_RE.match(line)
        if m:
            svc = m.group(1)
            current = services.get(svc) or ComposeService(name=svc)
            services[svc] = current
            continue

        if current is None:
            continue

        m = _CONTAINER_NAME_RE.match(line)
        if m:
            current.container_name = _strip_quotes(m.group(1))
            continue

        m = _IMAGE_RE.match(line)
        if m:
            current.image = _strip_quotes(m.group(1))
            continue

    return services


def _merge_services(services_sets: Iterable[Dict[str, ComposeService]]) -> Dict[str, ComposeService]:
    merged: Dict[str, ComposeService] = {}
    for services in services_sets:
        for name, s in services.items():
            m = merged.get(name) or ComposeService(name=name)
            # Later files win (compose override semantics).
            if s.container_name is not None:
                m.container_name = s.container_name
            if s.image is not None:
                m.image = s.image
            merged[name] = m
    return merged


def _load_manifest(manifest_path: Path) -> Tuple[dict, Dict[str, str]]:
    payload = json.loads(manifest_path.read_text(encoding="utf-8"))
    containers = payload.get("containers") or {}
    out: Dict[str, str] = {}
    if isinstance(containers, dict):
        for cname, cinfo in containers.items():
            if not cname or not isinstance(cinfo, dict):
                continue
            pinned = cinfo.get("pinned_image") or ""
            pinned = str(pinned).strip()
            if pinned:
                out[str(cname)] = pinned
    return payload, out


def _render_override(
    *,
    manifest_path: Path,
    manifest: dict,
    services: Dict[str, ComposeService],
    pinned_by_container: Dict[str, str],
    strict: bool,
) -> str:
    captured_date = str(manifest.get("captured_date") or "")
    env = str(manifest.get("env") or "")
    schema_version = str(manifest.get("schema_version") or "")

    lines: List[str] = []
    lines.append("# Generated file: docker-compose digest pins")
    lines.append(f"# manifest: {manifest_path}")
    if schema_version:
        lines.append(f"# schema_version: {schema_version}")
    if env:
        lines.append(f"# env: {env}")
    if captured_date:
        lines.append(f"# captured_date: {captured_date}")
    lines.append("#")
    lines.append("# Usage:")
    lines.append("#   docker compose -f docker-compose.yml -f docker-compose.override.yml -f <this file> pull")
    lines.append("#   docker compose -f docker-compose.yml -f docker-compose.override.yml -f <this file> up -d")
    lines.append("")
    lines.append("services:")

    missing: List[str] = []
    emitted = 0

    for svc_name in sorted(services.keys()):
        svc = services[svc_name]
        container_name = svc.container_name or svc.name
        pinned = pinned_by_container.get(container_name)
        if not pinned:
            missing.append(f"{svc_name} (container={container_name})")
            continue

        lines.append(f"  {svc_name}:")
        lines.append(f"    image: {pinned}")
        emitted += 1

    if strict and missing:
        raise ValueError(
            "Missing pinned digests for some services:\n  - " + "\n  - ".join(missing)
        )

    if emitted == 0:
        raise ValueError("No services were pinned. Is the manifest empty or mismatched?")

    if missing:
        # Write warnings into the YAML as comments to make drift visible.
        lines.append("")
        lines.append("# WARNING: no pinned digest found for these services (skipped):")
        for m in missing:
            lines.append(f"# - {m}")

    return "\n".join(lines) + "\n"


def main(argv: List[str]) -> int:
    parser = argparse.ArgumentParser(description="Generate a docker-compose override that pins images by digest.")
    parser.add_argument("--manifest", required=True, help="Path to platform manifest JSON (digest pins).")
    parser.add_argument(
        "--compose",
        action="append",
        default=[],
        help="docker-compose YAML to parse (repeatable). Order matters (later wins).",
    )
    parser.add_argument("--out", default="-", help="Output path (default: stdout). Use '-' for stdout.")
    parser.add_argument("--strict", action="store_true", help="Fail if any compose service is missing a pin.")

    args = parser.parse_args(argv)
    manifest_path = Path(args.manifest).resolve()
    if not manifest_path.exists():
        parser.error(f"manifest not found: {manifest_path}")

    compose_paths = [Path(p).resolve() for p in (args.compose or [])]
    if not compose_paths:
        parser.error("--compose must be provided at least once")
    for p in compose_paths:
        if not p.exists():
            parser.error(f"compose file not found: {p}")

    manifest, pinned_by_container = _load_manifest(manifest_path)
    merged_services = _merge_services(_parse_compose_services(p) for p in compose_paths)

    payload = _render_override(
        manifest_path=manifest_path,
        manifest=manifest,
        services=merged_services,
        pinned_by_container=pinned_by_container,
        strict=bool(args.strict),
    )

    if args.out == "-" or args.out == "":
        sys.stdout.write(payload)
    else:
        out_path = Path(args.out).resolve()
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(payload, encoding="utf-8")

    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))

