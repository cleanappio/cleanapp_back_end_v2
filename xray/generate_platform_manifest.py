#!/usr/bin/env python3
"""
Generate a platform manifest that pins image digests from an xray snapshot.

Input:
  xray/<env>/<date>/containers_manifest.tsv

Output:
  JSON mapping container -> image tag + repo digests + runtime metadata.
"""

from __future__ import annotations

import argparse
import csv
import datetime as dt
import json
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple


EXPECTED_COLUMNS = [
    "name",
    "config_image",
    "container_id",
    "image_id",
    "created",
    "started_at",
    "state",
    "health",
    "ports",
    "networks",
    "repo_digests_json",
]


def _infer_env_and_date(xray_dir: Path) -> Tuple[Optional[str], Optional[str]]:
    parts = list(xray_dir.parts)
    try:
        i = parts.index("xray")
    except ValueError:
        return None, None

    env = parts[i + 1] if i + 1 < len(parts) else None
    date = parts[i + 2] if i + 2 < len(parts) else None
    return env, date


def _load_rows(tsv_path: Path) -> List[Dict[str, str]]:
    with tsv_path.open("r", encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f, delimiter="\t")
        if reader.fieldnames is None:
            raise ValueError("Missing TSV header row")
        missing_cols = [c for c in EXPECTED_COLUMNS if c not in reader.fieldnames]
        if missing_cols:
            raise ValueError(
                f"Unexpected TSV header. Missing columns: {missing_cols}. "
                f"Got: {reader.fieldnames}"
            )
        return list(reader)


def _parse_repo_digests(repo_digests_json: str) -> List[str]:
    try:
        v: Any = json.loads(repo_digests_json or "[]")
    except json.JSONDecodeError:
        return []

    if not isinstance(v, list):
        return []
    return [str(x) for x in v if x]


def _build_manifest(*, env: str, date: str, xray_dir: Path, rows: List[Dict[str, str]]) -> Dict[str, Any]:
    containers: Dict[str, Dict[str, Any]] = {}
    containers_missing_digests: List[str] = []

    for r in rows:
        name = (r.get("name") or "").strip()
        if not name:
            continue

        repo_digests = _parse_repo_digests(r.get("repo_digests_json", "[]"))
        pinned = repo_digests[0] if repo_digests else None
        if not repo_digests:
            containers_missing_digests.append(name)

        containers[name] = {
            "config_image": r.get("config_image", ""),
            "repo_digests": repo_digests,
            "pinned_image": pinned,
            "container_id": r.get("container_id", ""),
            "image_id": r.get("image_id", ""),
            "created": r.get("created", ""),
            "started_at": r.get("started_at", ""),
            "state": r.get("state", ""),
            "health": r.get("health", ""),
            "ports": r.get("ports", ""),
            "networks": r.get("networks", ""),
        }

    generated_at = dt.datetime.now(dt.timezone.utc).isoformat()

    # Stable output ordering for diffs.
    containers_sorted = {k: containers[k] for k in sorted(containers.keys())}
    containers_missing_digests_sorted = sorted(set(containers_missing_digests))

    return {
        "schema_version": 1,
        "env": env,
        "captured_date": date,
        "generated_at": generated_at,
        "xray_dir": str(xray_dir),
        "containers": containers_sorted,
        "containers_missing_repo_digests": containers_missing_digests_sorted,
        "summary": {
            "container_count": len(containers_sorted),
            "missing_repo_digests_count": len(containers_missing_digests_sorted),
        },
    }


def main(argv: List[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Generate a platform manifest (digest pins) from an xray snapshot.",
    )
    parser.add_argument(
        "--xray-dir",
        required=True,
        help="Path to xray/<env>/<date> directory",
    )
    parser.add_argument(
        "--env",
        default=None,
        help="Environment name (default: inferred from xray-dir)",
    )
    parser.add_argument(
        "--date",
        default=None,
        help="Snapshot date (default: inferred from xray-dir)",
    )
    parser.add_argument(
        "--out",
        default=None,
        help="Output JSON path (default: stdout)",
    )

    args = parser.parse_args(argv)
    xray_dir = Path(args.xray_dir).resolve()
    tsv_path = xray_dir / "containers_manifest.tsv"

    inferred_env, inferred_date = _infer_env_and_date(xray_dir)
    env = args.env or inferred_env
    date = args.date or inferred_date
    if not env:
        parser.error("--env not provided and could not be inferred from --xray-dir")
    if not date:
        parser.error("--date not provided and could not be inferred from --xray-dir")

    if not tsv_path.exists():
        raise FileNotFoundError(f"Missing {tsv_path}")

    rows = _load_rows(tsv_path)
    manifest = _build_manifest(env=env, date=date, xray_dir=xray_dir, rows=rows)
    payload = json.dumps(manifest, indent=2, sort_keys=True) + "\n"

    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(payload, encoding="utf-8")
    else:
        sys.stdout.write(payload)

    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))

