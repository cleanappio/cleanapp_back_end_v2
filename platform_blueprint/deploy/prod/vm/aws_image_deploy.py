#!/usr/bin/env python3
"""Build exact-source images locally and replace explicitly selected running roles.

No registry, cloud CLI, source-host default, or runtime-secret export is used.
The internal --activate command runs on the target over pinned SSH.
"""
import argparse
import contextlib
import fcntl
from datetime import datetime, timezone
import fnmatch
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
import uuid


ROOT = (Path(__file__).resolve().parent / "../../../..").resolve()
PINS_FILE = "docker-compose.aws-backend-images.yml"
FRONTEND_PINS_FILE = "docker-compose.aws-frontend-images.yml"
# Production roles, not the broader legacy source-service map. In particular,
# the human analyzer never expands to the separately registered machine analyzer
# or dormant news workers.
SOURCES = {
    "cleanapp_areas_service": "areas-service",
    "cleanapp_auth_service": "auth-service",
    "cleanapp_red_bull_dashboard": "brand-dashboard",
    "cleanapp_montenegro_areas": "custom-area-dashboard",
    "cleanapp_new_york_areas": "custom-area-dashboard",
    "cleanapp_devconnect_2025_areas": "custom-area-dashboard",
    "cleanapp_edge_city_areas": "custom-area-dashboard",
    "cleanapp_customer_service": "customer-service",
    "cleanapp_service": "docker_backend",
    "cleanapp_pipelines": "docker_pipelines",
    "cleanapp_email_fetcher": "email-fetcher",
    "cleanapp_email_service": "email-service",
    "cleanapp_epc_pusher": "epc-pusher",
    "cleanapp_report_analyze_human": "report-analyze-pipeline",
    "cleanapp_report_analyze_pipeline": "report-analyze-pipeline",
    "cleanapp_report_renderer_service": "report-fast-renderer",
    "cleanapp_report_listener": "report-listener",
    "cleanapp_report_listener_v4": "report-listener-v4",
    "cleanapp_report_ownership_service": "report-ownership-service",
    "cleanapp_report_processor": "report-processor",
    "cleanapp_report_tags_service": "report-tags",
    "cleanapp_voice_assistant_service": "voice-assistant-service",
}
EXCLUDED = (
    ".git", ".aws", ".config", ".env*", "*.pem", "*.key", "*.p8", "*.p12",
    "*.pfx", "id_rsa*", "id_ed25519*", "credentials.json", "*credentials*.json",
    "application_default_credentials.json", "gha-creds-*.json", "service-account*.json",
    "target", "node_modules", "__pycache__", ".DS_Store", "buildinfo.vars",
)
IMAGE_ID = re.compile(r"sha256:[a-f0-9]{64}\Z")
OLM_GUARDED_ROLES = frozenset(("cleanapp_email_service", "cleanapp_report_listener",
                              "cleanapp_report_analyze_human"))
OLM_DEPLOY_GUARD = "/usr/local/sbin/cleanapp-olm-deploy-check"


def check_olm_compatibility(manifest_path, manifest, run=None):
    """A root-installed read-only check must authorize guarded-role changes."""
    guarded = sorted(set(manifest["roles"]) & OLM_GUARDED_ROLES)
    if not guarded:
        return {"decision": "unrelated_roles"}
    run = run or command
    raw_manifest = Path(manifest_path).read_bytes()
    require(json.loads(raw_manifest) == manifest, "release manifest changed before OLM check")
    digest = hashlib.sha256(raw_manifest).hexdigest()
    raw = run(["sudo", "-n", OLM_DEPLOY_GUARD, "--manifest", str(manifest_path)],
              failure="OLM guarded-role compatibility check")
    receipt = json.loads(raw)
    require(receipt.get("kind") == "cleanapp_olm_deploy_check.v1"
            and receipt.get("manifest_sha256") == digest
            and receipt.get("guarded_roles") == guarded
            and receipt.get("decision") in ("not_installed", "unchanged", "reviewed_quiescent"),
            "OLM compatibility check returned an invalid release-bound receipt")
    return receipt


def require(condition, message):
    if not condition:
        raise ValueError(message)


def command(args, private_log=None, failure=None, relay_helper_error=False, **kwargs):
    # Capture rather than print inspect/config/migration output, which can
    # contain runtime secrets or a connection string on an error path.
    #
    # private_log: on failure, keep the raw output in this owner-only file on
    #   the machine that ran the command, and name only its path in the error.
    # failure: a safe description (role and stage) to use instead of argv[0].
    # relay_helper_error: the command is this helper running on the target; on
    #   failure, repeat its own final "ERROR: ..." line. Those lines are written
    #   by this file and never contain command output or environment values.
    result = subprocess.run([str(a) for a in args], stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, **kwargs)
    if result.returncode:
        message = f"{failure or str(args[0]) + ' command'} failed (exit {result.returncode})"
        if private_log is not None:
            private_log = Path(private_log)
            descriptor = os.open(private_log, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
            with os.fdopen(descriptor, "wb") as stream:
                stream.write(result.stdout + b"\n--- stderr ---\n" + result.stderr)
            raise RuntimeError(f"{message}; raw output kept privately on the target at {private_log}")
        if relay_helper_error:
            relayed = [line[len("ERROR: "):] for line in result.stderr.decode("utf-8", "replace").splitlines()
                       if line.startswith("ERROR: ")]
            if relayed and relayed[-1].isprintable():
                raise RuntimeError(f"{message}: {relayed[-1][:400]}")
        raise RuntimeError(f"{message}; inspect privately on the host")
    return result.stdout


NETWORK_KEY = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,62}\Z")


def migration_override(service, image_id, resolved_service):
    """Compose override (YAML text) for the one-off migration container.

    The migration reuses the live service definition so it gets the same
    environment, secrets and mounts. Three runtime-only settings must not be
    inherited, because each one stops the container from starting:

    - a fixed network address: the live container already holds it
      ("Address already in use");
    - the runtime user: the migration image is a bare Alpine image without
      that account ("unable to find user");
    - health check and restart policy: meaningless for a run-once command.

    Networks are replaced, not merged: `!override` keeps the same network
    memberships (so the database is reachable) with empty endpoint options,
    which means a dynamic address and no production aliases. A plain mapping
    would merge with the inherited one and keep the fixed address. JSON cannot
    carry the tag, so this file is written as YAML by hand; every scalar is a
    JSON string, which is also valid YAML.
    """
    lines = ["services:", f"  {json.dumps(service)}:", f"    image: {json.dumps(image_id)}",
             "    pull_policy: never", '    user: "0:0"', "    working_dir: /", "    command: []",
             '    restart: "no"', "    healthcheck:", "      disable: true"]
    networks = sorted(resolved_service.get("networks") or [])
    for name in networks:
        require(isinstance(name, str) and NETWORK_KEY.match(name),
                f"unexpected network key in target Compose: {service}")
    if networks:
        lines.append("    networks: !override")
        lines += [f"      {json.dumps(name)}: {{}}" for name in networks]
    return "\n".join(lines) + "\n"


def write_json(path, value):
    path = Path(path)
    path.write_text(json.dumps(value, indent=2) + "\n")
    path.chmod(0o600)


def evidence_roles(document):
    require(bool(document.get("captured_at")), "runtime evidence must include captured_at")
    datetime.fromisoformat(document["captured_at"].replace("Z", "+00:00"))
    roles = {}
    for row in document.get("containers", []):
        service = row.get("compose", {}).get("com.docker.compose.service")
        if service:
            require(service not in roles, "duplicate role in runtime evidence")
            roles[service] = row["name"]
    for row in document.get("roles", []):
        require(row["service"] not in roles, "duplicate role in runtime evidence")
        roles[row["service"]] = row["container"]
    require(bool(roles), "runtime evidence contains no Compose roles")
    return roles


def selected_roles(services, evidence):
    require(bool(services), "at least one explicit service is required")
    require(len(services) == len(set(services)), "duplicate selected service")
    roles = evidence_roles(evidence)
    for service in services:
        require(service in SOURCES, f"unsupported or dormant backend service: {service}")
        require(service in roles, f"selected service absent from captured running roles: {service}")
        require(re.fullmatch(r"[A-Za-z0-9_.-]+", roles[service]) is not None,
                "invalid container name in runtime evidence")
    return {service: roles[service] for service in services}


def ignored(_directory, names):
    return [name for name in names if any(fnmatch.fnmatch(name, rule) for rule in EXCLUDED)]


def stage_context(archive_root, source, destination, sha, build_time):
    # Only committed files reach archive_root. Exclude credential filenames even
    # if a legacy checkout accidentally tracked one; never copy the checkout.
    root_context = source in ("docker_backend", "docker_pipelines")
    shutil.copytree(archive_root if root_context else archive_root / source,
                    destination, ignore=ignored, symlinks=False)
    service_dir = destination / source if root_context else destination
    if not root_context:
        for manifest, dependency, replacement in (
            ("go.mod", "go-common", "/go-common"),
            ("Cargo.toml", "rust-common", "rust-common"),
        ):
            path = destination / manifest
            if path.exists() and f"../{dependency}" in path.read_text():
                shutil.copytree(archive_root / dependency, destination / dependency,
                                ignore=ignored)
                path.write_text(path.read_text().replace(f"../{dependency}", replacement))
    version_path = service_dir / ".version"
    if not version_path.exists():
        version_path = service_dir / "build_version.txt"
    require(version_path.exists(), f"missing tracked version for {source}")
    version = version_path.read_text().strip().removeprefix("BUILD_VERSION=").strip("\"'")
    require(re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version) is not None,
            f"unsupported version format for {source}")
    version += "-aws." + sha[:12]
    (service_dir / "buildinfo.vars").write_text(
        f"CLEANAPP_BUILD_VERSION={version}\nCLEANAPP_GIT_SHA={sha}\nCLEANAPP_BUILD_TIME={build_time}\n")
    return version, service_dir


def build_image(context, dockerfile, tag, sha, version, build_time):
    command(["docker", "build", "--platform", "linux/amd64", "--file", dockerfile,
             "--label", f"org.opencontainers.image.revision={sha}",
             "--label", "org.opencontainers.image.source=https://github.com/cleanappio/cleanapp_back_end_v2",
             "--label", f"org.opencontainers.image.version={version}",
             "--label", f"org.opencontainers.image.created={build_time}",
             "--tag", tag, context])
    data = json.loads(command(["docker", "image", "inspect", tag]))[0]
    require(data.get("Architecture") == "amd64" and data.get("Os") == "linux",
            "built image must be linux/amd64")
    require(IMAGE_ID.fullmatch(data["Id"]) is not None, "invalid built image ID")
    return {"tag": tag, "image_id": data["Id"], "version": version}


def archive_config_ids(path, images, source_sha):
    """Bind exported tags to config-byte digests, independent of runner store IDs.

    Docker's containerd store can report a manifest/index digest as .Id, while
    the production classic store identifies the same image by its config JSON.
    Never substitute labels for identity: hash the exact exported config bytes.
    """
    expected = {item["tag"]: item for item in images}
    require(bool(expected) and len(expected) == len(images), "empty or duplicate expected export tag")
    result = {}

    def document(raw):
        def unique(pairs):
            value = {}
            for key, item in pairs:
                require(key not in value, "duplicate export metadata JSON key")
                value[key] = item
            return value
        return json.loads(raw, object_pairs_hook=unique)

    with tarfile.open(path, "r:gz") as archive:
        members = archive.getmembers()
        by_name = {member.name: member for member in members}
        require(len(by_name) == len(members), "duplicate export archive member")

        def metadata(name):
            member = by_name.get(name)
            require(member is not None and member.isfile() and 0 < member.size <= 10 * 1024 * 1024,
                    "missing or invalid export metadata")
            return archive.extractfile(member).read()

        manifest = document(metadata("manifest.json"))
        require(isinstance(manifest, list) and bool(manifest), "invalid Docker-save manifest")
        for entry in manifest:
            require(isinstance(entry, dict), "invalid Docker-save manifest entry")
            tags = entry.get("RepoTags")
            require(isinstance(tags, list) and bool(tags) and all(isinstance(tag, str) for tag in tags)
                    and len(tags) == len(set(tags)),
                    "ambiguous export tags")
            require(all(tag in expected and tag not in result for tag in tags),
                    "unexpected or duplicate export tag")
            config_name = entry.get("Config")
            require(isinstance(config_name, str) and re.fullmatch(
                r"(?:[a-f0-9]{64}\.json|blobs/sha256/[a-f0-9]{64})", config_name),
                "invalid export config path")
            raw = metadata(config_name)
            digest = hashlib.sha256(raw).hexdigest()
            require(config_name in (digest + ".json", "blobs/sha256/" + digest),
                    "export config digest mismatch")
            config = document(raw)
            require(isinstance(config, dict) and config.get("os") == "linux"
                    and config.get("architecture") == "amd64",
                    "exported config must be linux/amd64")
            runtime = config.get("config")
            labels = runtime.get("Labels") if isinstance(runtime, dict) else None
            require(isinstance(labels, dict), "missing export config labels")
            for tag in tags:
                require(labels.get("org.opencontainers.image.revision") == source_sha
                        and labels.get("org.opencontainers.image.version") == expected[tag]["version"],
                        "export config source/version mismatch")
                result[tag] = "sha256:" + digest
        require(set(result) == set(expected), "missing expected export tag")
    return result


def compose_base(remote_dir, files):
    require(isinstance(files, list) and bool(files), "explicit Compose file list is required")
    for name in files:
        require(isinstance(name, str) and Path(name).is_absolute(),
                "each explicit Compose file must have an absolute target path")
        require(Path(name).name not in (PINS_FILE, FRONTEND_PINS_FILE),
                "managed backend/frontend image pins must not appear in the caller Compose file list")
        # The verified production baseline can be root-readable while release
        # state/pins are owned by the dedicated deployment user. Do not require
        # that user to traverse or read the private baseline directory.
        command(["sudo", "-n", "test", "-f", name])
    # Include both managed overrides after caller baseline/runtime overlays.
    # The sibling is first and our own pins last; neither helper overwrites it.
    files = [*files, *[str(remote_dir / name) for name in (FRONTEND_PINS_FILE, PINS_FILE)
                      if (remote_dir / name).exists()]]
    args = ["sudo", "-n", "docker", "compose", "--project-directory", str(remote_dir)]
    for name in files:
        args += ["-f", name]
    return args


def checked_container(container, service, project):
    data = json.loads(command(["sudo", "-n", "docker", "inspect", container]))[0]
    labels = data.get("Config", {}).get("Labels") or {}
    require(data.get("State", {}).get("Running") is True, f"target role is not running: {service}")
    require(labels.get("com.docker.compose.service") == service,
            f"target container is not the selected Compose role: {service}")
    require(labels.get("com.docker.compose.project") == project,
            f"target container is not in the configured Compose project: {service}")
    require(IMAGE_ID.fullmatch(data["Image"]) is not None, "invalid running image ID")
    return data


@contextlib.contextmanager
def deployment_lock(directory):
    """Share one stable private inode with weekly backup and cold restore."""
    directory = Path(directory)
    if not directory.is_absolute():
        raise ValueError("Deployment lock parent must be absolute")
    parent = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    descriptor = None
    try:
        info = os.fstat(parent)
        if info.st_uid != os.geteuid() or info.st_mode & 0o022:
            raise ValueError("Deployment lock parent must be deployer-owned and not publicly writable")
        name = ".deployment-and-backup.lock"
        descriptor = os.open(name, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=parent)
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o600:
            raise ValueError("Deployment lock must be a private single-link deployer-owned regular file")
        fcntl.flock(descriptor, fcntl.LOCK_EX)
        current = os.stat(name, dir_fd=parent, follow_symlinks=False)
        if (current.st_dev, current.st_ino) != (info.st_dev, info.st_ino):
            raise ValueError("Deployment lock inode changed while waiting")
        try:
            yield
        finally:
            fcntl.flock(descriptor, fcntl.LOCK_UN)
    finally:
        if descriptor is not None:
            os.close(descriptor)
        os.close(parent)


def require_active_runtime(run):
    """A CI trigger must not bypass the one-writer migration/restore fence."""
    isolated = "/etc/cleanapp-runtime/ISOLATED"
    active = "/etc/cleanapp-runtime/ACTIVE"
    run(["sudo", "-n", "test", "!", "-e", isolated])
    run(["sudo", "-n", "test", "!", "-L", isolated])
    run(["sudo", "-n", "test", "-f", active])
    run(["sudo", "-n", "test", "!", "-L", active])


def activate(manifest_path):
    manifest_path = Path(manifest_path).resolve()
    manifest = json.loads(manifest_path.read_text())
    with deployment_lock(Path(manifest["remote_dir"])):
        require_active_runtime(command)
        return _activate_locked(manifest_path)


def _activate_locked(manifest_path):
    manifest_path = Path(manifest_path).resolve()
    release_dir = manifest_path.parent
    archive = release_dir / "images.tar.gz"
    try:
        manifest = json.loads(manifest_path.read_text())
        services = list(manifest["roles"])
        selected_roles(services, {"captured_at": manifest["runtime_captured_at"],
                                "roles": [{"service": s, "container": c}
                                          for s, c in manifest["roles"].items()]})
        remote_dir = Path(manifest["remote_dir"])
        require(remote_dir.is_absolute() and remote_dir.is_dir(), "target deployment directory must exist")
        architecture = command(["sudo", "-n", "docker", "info", "--format", "{{.Architecture}}"])
        require(architecture.decode().strip() in ("amd64", "x86_64"), "target Docker host must be x86/amd64")
        base = compose_base(remote_dir, manifest["compose_files"])
        # config includes effective secret environment values: keep it in memory.
        resolved = json.loads(command([*base, "config", "--format", "json"], cwd=remote_dir))
        previous = {}
        for service, container in manifest["roles"].items():
            require(service in resolved["services"], f"selected role absent from target Compose: {service}")
            require(resolved["services"][service].get("container_name") == container,
                    f"target Compose container name differs from runtime evidence: {service}")
            previous[service] = checked_container(container, service, resolved["name"])["Image"]
        old_path = remote_dir / PINS_FILE
        old_pins = json.loads(old_path.read_text()) if old_path.exists() else {"services": {}}
        require(set(old_pins) == {"services"}, "unexpected backend pin-file keys")
        for service, pin in old_pins["services"].items():
            require(service in SOURCES and set(pin) == {"image", "pull_policy"}
                    and pin["pull_policy"] == "never" and IMAGE_ID.fullmatch(pin["image"]),
                    "existing backend pins must contain supported roles and imported image IDs only")
        rollback = {"services": {**old_pins["services"],
                                 **{s: {"image": image, "pull_policy": "never"}
                                    for s, image in previous.items()}}}
        write_json(release_dir / "rollback.yml", rollback)
        write_json(release_dir / "rollback.json", {"source_sha": manifest["source_sha"],
                   "roles": manifest["roles"], "previous_images": previous,
                   "compose_files": manifest["compose_files"], "remote_dir": str(remote_dir)})
        with archive.open("rb") as stream:
            checksum = hashlib.file_digest(stream, "sha256").hexdigest()
        require(checksum == manifest["archive_sha256"], "transferred image archive checksum mismatch")
        items = [item for image in manifest["images"].values() for item in image.values()]
        config_ids = archive_config_ids(archive, items, manifest["source_sha"])
        require(all(config_ids[item["tag"]] == item["image_id"] for item in items),
                "exported config ID differs from release manifest")
        command(["sudo", "-n", "docker", "image", "load", "--input", archive])
        for image in manifest["images"].values():
            for item in (image["runtime"], image.get("migration")):
                if not item:
                    continue
                loaded = json.loads(command(["sudo", "-n", "docker", "image", "inspect", item["tag"]]))[0]
                require(loaded["Id"] == item["image_id"], "loaded image ID differs from runner build")
                require(loaded.get("Os") == "linux" and loaded.get("Architecture") == "amd64",
                        "loaded image is not linux/amd64")
                labels = loaded.get("Config", {}).get("Labels") or {}
                require(labels.get("org.opencontainers.image.revision") == manifest["source_sha"]
                        and labels.get("org.opencontainers.image.version") == item["version"],
                        "loaded image source/version labels differ from runner build")
        # A failed/activating receiver is still an unfinished import. Never
        # infer compatibility from process state or accept unknown live images.
        # This runs before every migration and role replacement, under the
        # existing deployment/backup lock. Unrelated services bypass it.
        olm_check = check_olm_compatibility(manifest_path, manifest)
        migrated = set()
        for service in services:
            source = SOURCES[service]
            image = manifest["images"][source]
            if source in migrated or not image.get("migration"):
                continue
            migrated.add(source)
            override_path = release_dir / "migration.yml"
            override_path.write_text(migration_override(
                service, image["migration"]["image_id"], resolved["services"][service]))
            command([*base, "-f", override_path, "run", "--rm", "--no-deps", "--pull", "never",
                     "--entrypoint", "/cleanapp-migrate", service], cwd=remote_dir,
                    failure=f"migration for {service}",
                    private_log=release_dir / f"migration-{source}.log")
        pins = {"services": {**old_pins["services"], **{
            s: {"image": manifest["images"][SOURCES[s]]["runtime"]["image_id"], "pull_policy": "never"}
            for s in services}}}
        temporary = old_path.with_suffix(".tmp")
        write_json(temporary, pins)
        temporary.replace(old_path)
        # Explicit services and --no-deps are mandatory; never start the source map.
        up_base = compose_base(remote_dir, manifest["compose_files"])
        command([*up_base, "up", "-d", "--no-deps", "--no-build", "--pull", "never",
                 "--wait", "--wait-timeout", "180", *services], cwd=remote_dir)
        for service, container in manifest["roles"].items():
            running = checked_container(container, service, resolved["name"])
            require(running["Image"] == pins["services"][service]["image"],
                    f"deployed image ID mismatch: {service}")
        result = {"source_sha": manifest["source_sha"], "services": services,
                  "pins_file": str(old_path), "rollback_file": str(release_dir / "rollback.yml"),
                  "verified_running_image_ids": {s: pins["services"][s]["image"] for s in services},
                  "olm_compatibility": olm_check}
        write_json(release_dir / "result.json", result)
        return result
    finally:
        # Recovery keeps previous images and pins; the extra transport archive
        # need not consume permanent host disk space on success or failure.
        archive.unlink(missing_ok=True)


def deploy(args):
    evidence = json.loads(Path(args.runtime_manifest).read_text())
    roles = selected_roles(args.services, evidence)
    require(re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]*@[A-Za-z0-9][A-Za-z0-9_.:-]*", args.host) is not None,
            "host must be an explicit user@hostname or user@IP")
    require(1 <= args.ssh_port <= 65535, "SSH port must be in 1..65535")
    require(Path(args.remote_dir).is_absolute(), "remote deployment directory must be absolute")
    files = json.loads(args.compose_files_json)
    require(isinstance(files, list) and bool(files) and
            all(isinstance(p, str) and Path(p).is_absolute() for p in files),
            "explicit absolute target Compose file list is required")
    require(all(Path(p).name not in (PINS_FILE, FRONTEND_PINS_FILE) for p in files),
            "managed backend/frontend image pins must not appear in the caller Compose file list")
    require(Path(args.ssh_key).is_file() and Path(args.known_hosts).is_file(),
            "explicit SSH identity and pinned known-hosts files are required")
    require(re.fullmatch(r"[a-f0-9]{40}", args.ref) is not None, "ref must be the exact full commit SHA")
    sha = command(["git", "rev-parse", "--verify", args.ref + "^{commit}"], cwd=ROOT).decode().strip()
    require(re.fullmatch(r"[a-f0-9]{40}", sha) is not None, "invalid exact source commit")
    manifest = {"source_sha": sha, "runtime_captured_at": evidence["captured_at"],
                "roles": roles, "remote_dir": args.remote_dir, "compose_files": files}
    if args.dry_run:
        return {**manifest, "dry_run": True, "sources": sorted({SOURCES[s] for s in roles})}
    output = Path(args.output_dir).resolve()
    output.mkdir(parents=True, exist_ok=True)
    ssh = ["ssh", "-i", args.ssh_key, "-p", str(args.ssh_port), "-o", "BatchMode=yes",
           "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=yes", "-o",
           "UserKnownHostsFile=" + str(Path(args.known_hosts).resolve()), "-o", "ConnectTimeout=20", args.host]
    build_time = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    with tempfile.TemporaryDirectory(prefix="cleanapp-aws-build-") as temporary:
        temporary = Path(temporary)
        source_tar = temporary / "source.tar"
        command(["git", "archive", "--format=tar", "--output", source_tar, sha], cwd=ROOT)
        source_root = temporary / "source"
        source_root.mkdir()
        with tarfile.open(source_tar) as source_archive:
            source_archive.extractall(source_root, filter="data")
        images = {}
        for source in sorted({SOURCES[s] for s in roles}):
            context = temporary / (source + "-context")
            version, service_dir = stage_context(source_root, source, context, sha, build_time)
            tag = f"cleanapp-aws/{source}:{sha[:12]}"
            dockerfile = service_dir / "Dockerfile"
            images[source] = {"runtime": build_image(context, dockerfile, tag, sha, version, build_time)}
            if not args.skip_migrations and (service_dir / "cmd/migrate").is_dir():
                migration_dockerfile = context / "Dockerfile.aws-migration"
                migration_dockerfile.write_text(
                    "FROM golang:1.24-alpine AS builder\nWORKDIR /app\n"
                    "COPY go-common /go-common\nCOPY . .\n"
                    "RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /cleanapp-migrate ./cmd/migrate\n"
                    "FROM alpine:3.22\nRUN apk add --no-cache ca-certificates\n"
                    "COPY --from=builder /cleanapp-migrate /cleanapp-migrate\nENTRYPOINT [\"/cleanapp-migrate\"]\n")
                images[source]["migration"] = build_image(context, migration_dockerfile,
                    tag + "-migration", sha, version, build_time)
        manifest["images"] = images
        archive_path = temporary / "images.tar.gz"
        tags = [item["tag"] for image in images.values() for item in image.values()]
        with archive_path.open("wb") as archive_file:
            save = subprocess.Popen(["docker", "image", "save", *tags], stdout=subprocess.PIPE,
                                    stderr=subprocess.DEVNULL)
            try:
                with gzip.GzipFile(fileobj=archive_file, mode="wb", mtime=0) as compressed:
                    shutil.copyfileobj(save.stdout, compressed)
            finally:
                save.stdout.close()
                code = save.wait()
            require(code == 0, "docker image save failed")
        items = [item for image in images.values() for item in image.values()]
        config_ids = archive_config_ids(archive_path, items, sha)
        for item in items:
            item["runner_image_id"] = item["image_id"]
            item["image_id"] = config_ids[item["tag"]]
        with archive_path.open("rb") as stream:
            manifest["archive_sha256"] = hashlib.file_digest(stream, "sha256").hexdigest()
        release_dir = str(Path(args.remote_dir) / ".aws-releases" / (sha[:12] + "-" + uuid.uuid4().hex[:12]))
        manifest["release_dir"] = release_dir
        manifest_path = output / "image-manifest.json"
        write_json(manifest_path, manifest)
        command([*ssh, shlex.join(["mkdir", "-p", "-m", "700", release_dir])])
        try:
            for local, remote_name in ((Path(__file__), "aws_image_deploy.py"),
                                       (manifest_path, "manifest.json"), (archive_path, "images.tar.gz")):
                with local.open("rb") as stream:
                    command([*ssh, "umask 077; cat > " + shlex.quote(release_dir + "/" + remote_name)], stdin=stream)
            raw = command([*ssh, shlex.join(["python3", release_dir + "/aws_image_deploy.py",
                                            "--activate", release_dir + "/manifest.json"])],
                          failure="activation on the target", relay_helper_error=True)
        finally:
            try:
                command([*ssh, shlex.join(["rm", "-f", release_dir + "/images.tar.gz"])])
            except RuntimeError:
                # A network outage may prevent cleanup; the saved manifest names
                # the exact partial archive to remove privately after reconnecting.
                pass
        result = json.loads(raw)
        write_json(output / "deployment-result.json", result)
        return result


def main():
    if len(sys.argv) == 3 and sys.argv[1] == "--activate":
        return activate(sys.argv[2])
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref", required=True, help="exact commit selected by the workflow event")
    parser.add_argument("--runtime-manifest", required=True, help="non-secret captured running-role evidence JSON")
    parser.add_argument("--host", required=True)
    parser.add_argument("--remote-dir", required=True)
    parser.add_argument("--compose-files-json", required=True, help="explicit absolute target Compose paths in merge order")
    parser.add_argument("--ssh-key", required=True)
    parser.add_argument("--known-hosts", required=True)
    parser.add_argument("--ssh-port", type=int, default=22)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--dry-run", action="store_true", help="validate/print scope without Docker or SSH calls")
    parser.add_argument("--skip-migrations", action="store_true", help="explicit no-schema-change release only")
    parser.add_argument("services", nargs="+")
    return deploy(parser.parse_args())


if __name__ == "__main__":
    os.umask(0o077)
    try:
        print(json.dumps(main(), indent=2))
    except (ValueError, KeyError, OSError, RuntimeError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        sys.exit(1)
