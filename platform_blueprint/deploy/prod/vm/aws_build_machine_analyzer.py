#!/usr/bin/env python3
"""Build a secret-free exact-source analyzer archive for first-role registration.

This runs on a CI runner. It never contacts production or changes image pins.
The normal image deployer remains restricted to already registered roles.
"""
import argparse
from datetime import datetime, timezone
import gzip
import hashlib
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

import aws_image_deploy as deploy


def build(ref, output):
    deploy.require(re.fullmatch(r"[a-f0-9]{40}", ref) is not None,
                   "Expected an exact full source commit")
    sha = deploy.command(["git", "rev-parse", "--verify", ref + "^{commit}"],
                         cwd=deploy.ROOT).decode().strip()
    deploy.require(sha == ref, "Source commit differs from requested revision")
    output = Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    archive_path = output / "images.tar.gz"
    build_time = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    with tempfile.TemporaryDirectory(prefix="cleanapp-machine-analyzer-") as temporary:
        temporary = Path(temporary)
        source_tar = temporary / "source.tar"
        deploy.command(["git", "archive", "--format=tar", "--output", source_tar, sha],
                       cwd=deploy.ROOT)
        source = temporary / "source"
        source.mkdir()
        with tarfile.open(source_tar) as archive:
            archive.extractall(source, filter="data")
        context = temporary / "context"
        version, service_dir = deploy.stage_context(
            source, "report-analyze-pipeline", context, sha, build_time)
        image = deploy.build_image(context, service_dir / "Dockerfile",
                                   "cleanapp-aws/report-analyze-pipeline:" + sha[:12],
                                   sha, version, build_time)
        with archive_path.open("wb") as target:
            save = subprocess.Popen(["docker", "image", "save", image["tag"]],
                                    stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
            try:
                with gzip.GzipFile(fileobj=target, mode="wb", mtime=0) as compressed:
                    shutil.copyfileobj(save.stdout, compressed)
            finally:
                save.stdout.close()
                code = save.wait()
            deploy.require(code == 0, "Docker image export failed")
        image["runner_image_id"] = image["image_id"]
        image["image_id"] = deploy.archive_config_ids(archive_path, [image], sha)[image["tag"]]
    with archive_path.open("rb") as stream:
        archive_sha = hashlib.file_digest(stream, "sha256").hexdigest()
    manifest = {"schema": 1, "source_sha": sha, "archive_sha256": archive_sha,
                "purpose": "machine-analyzer-registration", "image": image,
                "production_contacted": False, "migrations": 0}
    deploy.write_json(output / "image-manifest.json", manifest)
    return manifest


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref", required=True)
    parser.add_argument("--output-dir", required=True)
    args = parser.parse_args()
    build(args.ref, args.output_dir)
