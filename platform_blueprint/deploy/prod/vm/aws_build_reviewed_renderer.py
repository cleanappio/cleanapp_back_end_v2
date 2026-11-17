#!/usr/bin/env python3
"""Build one exact-source renderer archive without contacting production.

Only Git and the runner's Docker daemon are used. This command has no registry
push, AWS, SSH, staging or activation path. A separately approved release must
review the exported archive/config identities before any production mutation.
"""
import argparse
from datetime import datetime, timezone
import gzip
import hashlib
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

import aws_image_deploy as deploy


SOURCE = "report-fast-renderer"
ROLE = "cleanapp_report_renderer_service"
HELPER_DIR = Path("platform_blueprint/deploy/prod/vm")
MAX_ARCHIVE_BYTES = 2 * 1024**3
MAX_EXPORT_BYTES = 8 * 1024**3


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def export_image(tag, path):
    """Bound retained size and clean up Docker-save children on any failure."""
    save = subprocess.Popen(["docker", "image", "save", tag],
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    try:
        with path.open("xb") as output:
            with gzip.GzipFile(fileobj=output, mode="wb", mtime=0) as compressed:
                exported = 0
                while data := save.stdout.read(1024**2):
                    exported += len(data)
                    deploy.require(exported <= MAX_EXPORT_BYTES, "Docker export exceeds size bound")
                    compressed.write(data)
                    deploy.require(output.tell() <= MAX_ARCHIVE_BYTES, "Image archive exceeds size bound")
            deploy.require(output.tell() <= MAX_ARCHIVE_BYTES, "Image archive exceeds size bound")
        deploy.require(save.wait(timeout=30) == 0, "Docker image export failed")
    finally:
        save.stdout.close()
        if save.poll() is None:
            save.terminate()
            try:
                save.wait(timeout=10)
            except subprocess.TimeoutExpired:
                save.kill()
                save.wait(timeout=10)


def build(ref, output):
    deploy.require(re.fullmatch(r"[a-f0-9]{40}", ref) is not None,
                   "Expected an exact full source commit")
    sha = deploy.command(["git", "rev-parse", "--verify", ref + "^{commit}"],
                         cwd=deploy.ROOT).decode().strip()
    deploy.require(sha == ref, "Source commit differs from requested revision")
    output = Path(output).resolve()
    output.mkdir(parents=True, exist_ok=False, mode=0o700)
    partial = output / "images.tar.gz.partial"
    archive_path = output / "images.tar.gz"
    build_time = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    try:
        with tempfile.TemporaryDirectory(prefix="cleanapp-reviewed-renderer-") as temporary:
            temporary = Path(temporary)
            source_tar = temporary / "source.tar"
            deploy.command(["git", "archive", "--format=tar", "--output", source_tar, sha],
                           cwd=deploy.ROOT)
            source = temporary / "source"
            source.mkdir()
            with tarfile.open(source_tar) as archive:
                archive.extractall(source, filter="data")
            helpers = {}
            for live_path in (Path(__file__).resolve(), Path(deploy.__file__).resolve()):
                relative = HELPER_DIR / live_path.name
                committed = source / relative
                deploy.require(committed.is_file() and digest(live_path) == digest(committed),
                               "Build helper differs from requested source commit")
                helpers[str(relative)] = digest(committed)
            context = temporary / "context"
            version, service_dir = deploy.stage_context(source, SOURCE, context, sha, build_time)
            image = deploy.build_image(context, service_dir / "Dockerfile",
                                       "cleanapp-aws/" + SOURCE + ":" + sha[:12],
                                       sha, version, build_time)
            export_image(image["tag"], partial)
            image["runner_image_id"] = image["image_id"]
            image["image_id"] = deploy.archive_config_ids(partial, [image], sha)[image["tag"]]
            proof = {"schema": 1, "kind": "cleanapp_reviewed_renderer_image.v1",
                     "source_sha": sha, "platform": "linux/amd64", "role": ROLE,
                     "source": SOURCE, "build_time_utc": build_time,
                     "archive_filename": archive_path.name,
                     "archive_sha256": digest(partial), "archive_bytes": partial.stat().st_size,
                     "image": image, "source_helper_sha256": helpers,
                     "dockerfile_sha256": digest(source / SOURCE / "Dockerfile"),
                     "archive_size_limit_bytes": MAX_ARCHIVE_BYTES,
                     "export_size_limit_bytes": MAX_EXPORT_BYTES,
                     "production_contacted": False, "migrations": 0,
                     "deployment_authorized_by_artifact": False}
            partial.replace(archive_path)
            deploy.write_json(output / "image-manifest.json", proof)
            return proof
    except BaseException:
        partial.unlink(missing_ok=True)
        archive_path.unlink(missing_ok=True)
        (output / "image-manifest.json").unlink(missing_ok=True)
        raise


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref", required=True, help="exact commit supplied by the workflow event")
    parser.add_argument("--output-dir", required=True, help="new directory for the reviewed image archive")
    args = parser.parse_args()
    build(args.ref, args.output_dir)
