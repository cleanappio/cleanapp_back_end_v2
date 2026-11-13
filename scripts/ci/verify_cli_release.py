#!/usr/bin/env python3
"""Validate the reviewed identity before the manual npm release builds anything."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess


REPOSITORY = "cleanappio/cleanapp_back_end_v2"
PACKAGE_REPOSITORY = {
    "type": "git",
    "url": "git+https://github.com/cleanappio/cleanapp_back_end_v2.git",
    "directory": "cli/cleanapp",
}


def validate_release(package, lock, version_source, *, reviewed_sha, expected_version,
                     actual_sha, workflow_sha, ref, repository):
    if ref != "refs/heads/main" or repository != REPOSITORY:
        raise ValueError("npm releases require the canonical repository's main branch")
    if not re.fullmatch(r"[0-9a-f]{40}", reviewed_sha):
        raise ValueError("reviewed_sha must be an exact lowercase 40-character commit SHA")
    if reviewed_sha != actual_sha or reviewed_sha != workflow_sha:
        raise ValueError("reviewed SHA, workflow SHA and checkout HEAD must match exactly")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", expected_version):
        raise ValueError("expected_version must be an explicit stable x.y.z version")
    if package.get("name") != "@cleanapp/cli" or package.get("private") is not False:
        raise ValueError("release package must be the public @cleanapp/cli package")
    if package.get("repository") != PACKAGE_REPOSITORY:
        raise ValueError("package repository metadata must match the canonical GitHub source")
    match = re.search(r'export const CLI_VERSION\s*=\s*[\"\']([^\"\']+)[\"\']', version_source)
    versions = [package.get("version"), lock.get("version"),
                lock.get("packages", {}).get("", {}).get("version"),
                match.group(1) if match else None]
    if any(version != expected_version for version in versions):
        raise ValueError("package, lockfile and executable versions must equal expected_version")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reviewed-sha", required=True)
    parser.add_argument("--expected-version", required=True)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    package_dir = root / "cli/cleanapp"
    actual_sha = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
    package = json.loads((package_dir / "package.json").read_text())
    validate_release(
        package, json.loads((package_dir / "package-lock.json").read_text()),
        (package_dir / "src/version.ts").read_text(), reviewed_sha=args.reviewed_sha,
        expected_version=args.expected_version, actual_sha=actual_sha,
        workflow_sha=os.environ.get("GITHUB_SHA", ""), ref=os.environ.get("GITHUB_REF", ""),
        repository=os.environ.get("GITHUB_REPOSITORY", ""),
    )
    subprocess.run(["git", "diff", "--exit-code", "HEAD", "--", "cli/cleanapp"],
                   cwd=root, check=True, stdout=subprocess.DEVNULL)
    print(json.dumps({"package": package["name"], "version": args.expected_version,
                      "source_sha": actual_sha, "ref": "refs/heads/main"}))


if __name__ == "__main__":
    main()
