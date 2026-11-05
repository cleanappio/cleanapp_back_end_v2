"""Exercise exact-source build/import/activation against isolated fake commands."""
import argparse
import hashlib
import importlib.util
import inspect
import io
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch


HELPER = Path(__file__).resolve().parents[1] / "aws_image_deploy.py"
SPEC = importlib.util.spec_from_file_location("aws_deploy", HELPER)
AWS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AWS)


def image_id(value):
    return "sha256:" + hashlib.sha256(value.encode()).hexdigest()


def export_tar(configs, oci=False):
    output = io.BytesIO()
    entries = []
    with tarfile.open(fileobj=output, mode="w") as archive:
        for tag, raw in configs.items():
            digest = hashlib.sha256(raw.encode()).hexdigest()
            name = "blobs/sha256/" + digest if oci else digest + ".json"
            data = raw.encode()
            member = tarfile.TarInfo(name)
            member.size = len(data)
            archive.addfile(member, io.BytesIO(data))
            entries.append({"Config": name, "RepoTags": [tag], "Layers": []})
        data = json.dumps(entries).encode()
        member = tarfile.TarInfo("manifest.json")
        member.size = len(data)
        archive.addfile(member, io.BytesIO(data))
    return output.getvalue()


class PortableDeployTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name).resolve()
        self.repo = self.base / "repo"
        self.repo.mkdir()
        for source in ("report-listener", "report-analyze-pipeline"):
            directory = self.repo / source
            (directory / "cmd/migrate").mkdir(parents=True)
            (directory / "cmd/migrate/main.go").write_text("package main\n")
            (directory / "Dockerfile").write_text("FROM fixture\nCOPY . .\n")
            (directory / "go.mod").write_text("replace cleanapp-common => ../go-common\n")
            (directory / ".version").write_text("BUILD_VERSION=1.0.42\n")
            (directory / "source.txt").write_text("committed source")
            (directory / ".env.production").write_text("FIXTURE_EXCLUDED=fixture-only")
            (directory / "fixture.key").write_text("fixture-only")
        (self.repo / "go-common").mkdir()
        (self.repo / "go-common/common.go").write_text("package common\n")
        self.git("init", "--quiet")
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "--quiet", "-m", "reviewed source")
        self.sha = self.git("rev-parse", "HEAD").decode().strip()
        (self.repo / "report-listener/source.txt").write_text("uncommitted source")
        (self.repo / "report-listener/.env.local").write_text("UNTRACKED=fixture-only")
        self.target = self.base / "target"
        self.target.mkdir()
        self.compose = self.target / "portable.yml"
        self.compose.write_text("services: {}\n")
        self.frontend_pins = self.target / "docker-compose.aws-frontend-images.yml"
        self.frontend_pins.write_text("frontend owned fixture\n")
        self.previous = {service: image_id("previous:" + service)
                         for service in ("cleanapp_report_listener", "cleanapp_report_analyze_human")}
        self.running = dict(self.previous)
        self.pins = self.target / AWS.PINS_FILE
        self.unselected = {"image": image_id("unselected"), "pull_policy": "never"}
        AWS.write_json(self.pins, {"services": {"cleanapp_report_processor": self.unselected}})
        self.evidence = self.base / "runtime.json"
        AWS.write_json(self.evidence, {"captured_at": "2026-11-05T09:43:15Z", "roles": [
            {"service": service, "container": service} for service in self.previous]})
        self.key = self.base / "identity"
        self.key.write_text("fixture-only")
        self.known_hosts = self.base / "known_hosts"
        self.known_hosts.write_text("fixture-only")
        self.bin = self.base / "bin"
        self.bin.mkdir()
        docker = self.bin / "docker"
        docker.write_text("#!" + sys.executable + "\nimport sys,json,hashlib,tarfile,io\nfrom pathlib import Path\n"
                          + inspect.getsource(export_tar) + "\n"
                          "assert sys.argv[1:3] == ['image', 'save']\n"
                          "base=Path(__file__).parent\n"
                          "configs=json.loads((base/'configs.json').read_text())\n"
                          "assert set(sys.argv[3:]) == set(configs)\n"
                          "data=export_tar(configs,oci=True)\n"
                          "(base/'last-export.tar').write_bytes(data)\n"
                          "sys.stdout.buffer.write(data)\n")
        docker.chmod(0o755)
        self.events = []
        self.images = {}
        self.configs = {}
        self.containerd_runner = False
        self.builds = []
        self.corrupt_transfer = False
        self.mismatch_loaded_id = False
        self.fail_migration = False
        self.fail_build = False
        self.stopped = False
        self.active = True
        self.isolated = False
        self.architecture = b"x86_64\n"
        self.original_command = AWS.command
        self.args = argparse.Namespace(ref=self.sha, runtime_manifest=str(self.evidence),
            host="deployer@aws.example.invalid", remote_dir=str(self.target),
            compose_files_json=json.dumps([str(self.compose)]),
            ssh_key=str(self.key), known_hosts=str(self.known_hosts), ssh_port=22,
            output_dir=str(self.base / "output"), dry_run=False, skip_migrations=False,
            services=list(self.previous))

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.repo, check=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout

    def execute(self):
        with patch.object(AWS, "ROOT", self.repo), patch.object(AWS, "command", self.fake_command), \
             patch.dict(os.environ, {"PATH": str(self.bin) + os.pathsep + os.environ["PATH"]}):
            return AWS.deploy(self.args)

    def fake_command(self, raw_args, **kwargs):
        args = [str(arg) for arg in raw_args]
        self.events.append(args)
        if args[0] == "git":
            return self.original_command(args, **kwargs)
        if args[0] == "ssh":
            self.assertIn("StrictHostKeyChecking=yes", args)
            self.assertIn("IdentitiesOnly=yes", args)
            self.assertIn("UserKnownHostsFile=" + str(self.known_hosts), args)
            self.assertEqual(args[-2], "deployer@aws.example.invalid")
            remote = args[-1]
            if remote.startswith("umask 077; cat > "):
                path = Path(shlex.split(remote.split(";", 1)[1])[2])
                content = kwargs["stdin"].read()
                if self.corrupt_transfer and path.name == "images.tar.gz":
                    content += b"corrupt"
                path.write_bytes(content)
            else:
                parts = shlex.split(remote)
                if parts[0] == "mkdir":
                    Path(parts[-1]).mkdir(parents=True, mode=0o700)
                elif parts[0] == "rm":
                    Path(parts[-1]).unlink(missing_ok=True)
                elif parts[0] == "python3":
                    return json.dumps(AWS.activate(parts[-1])).encode()
                else:
                    self.fail("unexpected remote command: " + remote)
            return b""
        if args[:2] == ["docker", "build"]:
            if self.fail_build:
                raise RuntimeError("fixture build failed")
            context = Path(args[-1])
            self.assertEqual((context / "source.txt").read_text(), "committed source")
            self.assertFalse((context / ".env.local").exists())
            self.assertFalse((context / ".env.production").exists())
            self.assertFalse((context / "fixture.key").exists())
            self.assertEqual((context / "go.mod").read_text(), "replace cleanapp-common => /go-common\n")
            self.assertTrue((context / "go-common/common.go").is_file())
            metadata = (context / "buildinfo.vars").read_text()
            self.assertIn("CLEANAPP_GIT_SHA=" + self.sha, metadata)
            self.assertIn("CLEANAPP_BUILD_VERSION=1.0.42-aws." + self.sha[:12], metadata)
            self.builds.append((args, metadata))
            tag = args[args.index("--tag") + 1]
            labels = dict(arg.split("=", 1) for index, arg in enumerate(args)
                          if index and args[index - 1] == "--label")
            raw_config = json.dumps({"architecture": "amd64", "os": "linux",
                                     "config": {"Labels": labels, "Cmd": [tag]}})
            self.configs[tag] = raw_config
            (self.bin / "configs.json").write_text(json.dumps(self.configs))
            config_id = "sha256:" + hashlib.sha256(raw_config.encode()).hexdigest()
            self.images[tag] = {"Id": image_id(tag) if self.containerd_runner else config_id,
                                "Architecture": "amd64", "Os": "linux",
                                "Config": {"Labels": labels}}
            return b""
        if args[:3] == ["docker", "image", "inspect"]:
            return json.dumps([self.images[args[-1]]]).encode()
        if args[:3] == ["sudo", "-n", "test"] and args[-1].startswith("/etc/cleanapp-runtime/"):
            if args[-1].endswith("ISOLATED") and self.isolated:
                raise RuntimeError("fixture runtime is isolated")
            if args[3] == "-f" and args[-1].endswith("ACTIVE") and not self.active:
                raise RuntimeError("fixture runtime is not active")
            return b""
        if args[:4] == ["sudo", "-n", "test", "-f"]:
            self.assertTrue(Path(args[-1]).exists())
            return b""
        if args[:3] == ["sudo", "-n", "docker"]:
            docker = args[3:]
            if docker[0] == "info":
                return self.architecture
            if docker[:2] == ["image", "load"]:
                for tag, raw_config in self.configs.items():
                    self.images[tag]["Id"] = "sha256:" + hashlib.sha256(raw_config.encode()).hexdigest()
                if self.mismatch_loaded_id:
                    next(iter(self.images.values()))["Id"] = image_id("wrong loaded ID")
                return b""
            if docker[:2] == ["image", "inspect"]:
                return json.dumps([self.images[docker[-1]]]).encode()
            if docker[0] == "inspect":
                service = docker[1]
                return json.dumps([{"Image": self.running[service], "State": {"Running": not self.stopped},
                    "Config": {"Labels": {"com.docker.compose.service": service,
                               "com.docker.compose.project": "fixture"}}}]).encode()
            if docker[0] == "compose":
                if docker[-3:] == ["config", "--format", "json"]:
                    return json.dumps({"name": "fixture", "services": {
                        s: {"container_name": s, "environment": {"SECRET_FIXTURE": "never export this value"}}
                        for s in self.previous}}).encode()
                if "run" in docker:
                    self.assertIn("--no-deps", docker)
                    self.assertEqual(docker[docker.index("--pull") + 1], "never")
                    self.assertEqual(docker[docker.index("--entrypoint") + 1], "/cleanapp-migrate")
                    migration = json.loads((Path(docker[docker.index("--project-directory") + 1]) /
                        ".aws-releases" / self.release_name() / "migration.yml").read_text())
                    self.assertEqual(migration["services"][docker[-1]]["command"], [])
                    if self.fail_migration:
                        raise RuntimeError("fixture migration failed")
                    return b""
                if "up" in docker:
                    self.assertEqual(docker[docker.index("up") + 1:],
                        ["-d", "--no-deps", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "180",
                         *self.args.services])
                    pins = json.loads(self.pins.read_text())["services"]
                    self.running.update({s: pins[s]["image"] for s in self.args.services})
                    return b""
        self.fail("unexpected command: " + repr(args))

    def command_events(self, name):
        return [args for args in self.events if args[:4] == ["sudo", "-n", "docker", name]]

    def release_name(self):
        return json.loads((self.base / "output/image-manifest.json").read_text())["release_dir"].rsplit("/", 1)[1]

    def assert_no_activation(self):
        self.assertFalse(any("run" in args or "up" in args for args in self.command_events("compose")))
        self.assertEqual(json.loads(self.pins.read_text()), {"services": {"cleanapp_report_processor": self.unselected}})

    def test_exact_source_import_migrations_and_only_selected_running_roles(self):
        result = self.execute()
        self.assertEqual(result["services"], self.args.services)
        self.assertEqual(len(self.builds), 4)  # Two runtime and two migration images.
        self.assertEqual(len([a for a in self.command_events("compose") if "run" in a]), 2)
        pins = json.loads(self.pins.read_text())["services"]
        self.assertEqual(pins["cleanapp_report_processor"], self.unselected)
        self.assertNotIn("cleanapp_report_analyze_pipeline", pins)
        self.assertEqual(self.frontend_pins.read_text(), "frontend owned fixture\n")
        self.assertTrue(all(pin["pull_policy"] == "never" for pin in pins.values()))
        self.assertFalse(any("gcloud" in arg or "--remove-orphans" in arg for event in self.events for arg in event))
        rollback = json.loads(Path(result["rollback_file"]).read_text())["services"]
        self.assertEqual({s: rollback[s]["image"] for s in self.previous}, self.previous)
        self.assertEqual(rollback["cleanapp_report_processor"], self.unselected)
        manifest = json.loads((self.base / "output/image-manifest.json").read_text())
        self.assertEqual(manifest["source_sha"], self.sha)
        self.assertEqual(manifest["archive_sha256"], hashlib.sha256(
            gzip_bytes((self.bin / "last-export.tar").read_bytes())).hexdigest())
        self.assertFalse((Path(manifest["release_dir"]) / "images.tar.gz").exists())
        self.assertNotIn("never export this value", json.dumps(result))
        self.assertEqual((self.repo / "report-listener/.version").read_text(), "BUILD_VERSION=1.0.42\n")

    def test_containerd_runner_id_is_not_confused_with_classic_target_config_id(self):
        self.containerd_runner = True
        self.args.skip_migrations = True
        result = self.execute()
        manifest = json.loads((self.base / "output/image-manifest.json").read_text())
        for source, images in manifest["images"].items():
            image = images["runtime"]
            self.assertNotEqual(image["runner_image_id"], image["image_id"])
            expected = "sha256:" + hashlib.sha256(self.configs[image["tag"]].encode()).hexdigest()
            self.assertEqual(image["image_id"], expected)
        self.assertEqual(set(result["verified_running_image_ids"].values()),
                         {image["runtime"]["image_id"] for image in manifest["images"].values()})

    def test_isolated_runtime_cannot_migrate_or_restart(self):
        self.isolated = True
        with self.assertRaisesRegex(RuntimeError, "isolated"):
            self.execute()
        self.assert_no_activation()

    def test_inactive_runtime_cannot_migrate_or_restart(self):
        self.active = False
        with self.assertRaisesRegex(RuntimeError, "not active"):
            self.execute()
        self.assert_no_activation()

    def test_dry_run_uses_exact_ref_and_never_contacts_docker_or_ssh(self):
        self.args.dry_run = True
        result = self.execute()
        self.assertTrue(result["dry_run"])
        self.assertEqual(result["source_sha"], self.sha)
        self.assertEqual([a[0] for a in self.events], ["git"])
        self.assertFalse((self.base / "output").exists())

    def test_dormant_empty_and_unevidenced_services_fail_before_any_commands(self):
        for services in ([], ["cleanapp_report_analyze_pipeline"], ["cleanapp_news_analyzer_twitter"],
                         ["cleanapp_auth_service"], ["cleanapp_report_listener"] * 2):
            with self.subTest(services=services):
                self.args.services = services
                with self.assertRaises(ValueError):
                    self.execute()
                self.assertFalse(self.events)

    def test_build_failure_cannot_contact_target(self):
        self.fail_build = True
        with self.assertRaises(RuntimeError):
            self.execute()
        self.assertFalse(any(a[0] == "ssh" for a in self.events))
        self.assert_no_activation()

    def test_stopped_target_role_fails_before_image_load(self):
        self.stopped = True
        with self.assertRaisesRegex(ValueError, "not running"):
            self.execute()
        self.assertFalse(any(a[3:5] == ["image", "load"] for a in self.events))
        self.assert_no_activation()

    def test_non_x86_target_fails_before_image_load(self):
        self.architecture = b"aarch64\n"
        with self.assertRaisesRegex(ValueError, "must be x86"):
            self.execute()
        self.assertFalse(any(a[3:5] == ["image", "load"] for a in self.events))
        self.assert_no_activation()

    def test_caller_cannot_reorder_managed_pins_and_sibling_is_automatically_included(self):
        with patch.object(AWS, "command", self.fake_command):
            for managed in (self.pins, self.frontend_pins):
                with self.assertRaisesRegex(ValueError, "must not appear"):
                    AWS.compose_base(self.target, [str(managed), str(self.compose)])
            base = AWS.compose_base(self.target, [str(self.compose)])
        self.assertEqual(base[-4:], ["-f", str(self.frontend_pins), "-f", str(self.pins)])
        self.assertEqual(base.count(str(self.pins)), 1)

    def test_private_baseline_existence_uses_sudo_without_unprivileged_file_stat(self):
        with patch.object(AWS, "command", self.fake_command), \
             patch.object(Path, "is_file", side_effect=AssertionError("unprivileged file stat")):
            AWS.compose_base(self.target, [str(self.compose)])
        self.assertIn(["sudo", "-n", "test", "-f", str(self.compose)], self.events)

    def test_missing_target_configuration_and_non_exact_ref_fail_before_commands(self):
        for field, value in (("host", ""), ("host", "-oProxyCommand=invalid@aws.invalid"),
                             ("remote_dir", "relative"), ("compose_files_json", "[]"),
                             ("ssh_port", 0), ("ref", "main")):
            with self.subTest(field=field, value=value):
                original = getattr(self.args, field)
                setattr(self.args, field, value)
                with self.assertRaises(ValueError):
                    self.execute()
                setattr(self.args, field, original)
                self.assertFalse(self.events)

    def test_transfer_checksum_mismatch_cannot_load_or_activate(self):
        self.corrupt_transfer = True
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            self.execute()
        self.assertFalse(any(a[3:5] == ["image", "load"] for a in self.events))
        self.assert_no_activation()

    def test_wrong_loaded_image_cannot_migrate_or_write_pins(self):
        self.mismatch_loaded_id = True
        with self.assertRaisesRegex(ValueError, "loaded image ID"):
            self.execute()
        self.assert_no_activation()

    def test_migration_failure_preserves_existing_pins_and_never_restarts(self):
        self.fail_migration = True
        with self.assertRaises(RuntimeError):
            self.execute()
        self.assertTrue(any("run" in a for a in self.command_events("compose")))
        self.assertFalse(any("up" in a for a in self.command_events("compose")))
        self.assertEqual(json.loads(self.pins.read_text()), {"services": {"cleanapp_report_processor": self.unselected}})

    def test_explicit_no_schema_change_skips_migration_builds_and_runs(self):
        self.args.skip_migrations = True
        self.execute()
        self.assertEqual(len(self.builds), 2)
        self.assertFalse(any("run" in a for a in self.command_events("compose")))

    def test_raw_runtime_capture_format_preserves_only_compose_roles(self):
        evidence = {"captured_at": "2026-11-05T09:43:15Z", "containers": [
            {"name": "cleanapp_report_analyze_human", "compose": {
                "com.docker.compose.service": "cleanapp_report_analyze_human"}},
            {"name": "cleanapp_frontend", "compose": {}}]}
        self.assertEqual(AWS.selected_roles(["cleanapp_report_analyze_human"], evidence),
                         {"cleanapp_report_analyze_human": "cleanapp_report_analyze_human"})


class ArchiveConfigIdentityTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / "images.tar.gz"
        self.sha = "a" * 40
        self.image = {"tag": "cleanapp-aws/fixture:" + self.sha[:12],
                      "image_id": image_id("containerd manifest"),
                      "version": "1.0.80-aws." + self.sha[:12]}
        self.config = {"architecture": "amd64", "os": "linux", "config": {"Labels": {
            "org.opencontainers.image.revision": self.sha,
            "org.opencontainers.image.version": self.image["version"]}}}

    def write(self, *, config=None, tag=None, oci=False):
        raw = json.dumps(config or self.config)
        self.path.write_bytes(gzip_bytes(export_tar({tag or self.image["tag"]: raw}, oci)))
        return "sha256:" + hashlib.sha256(raw.encode()).hexdigest()

    def test_classic_and_containerd_exports_bind_exact_config_bytes(self):
        for oci in (False, True):
            with self.subTest(oci=oci):
                expected = self.write(oci=oci)
                self.assertEqual(AWS.archive_config_ids(self.path, [self.image], self.sha),
                                 {self.image["tag"]: expected})
                self.assertNotEqual(expected, self.image["image_id"])

    def test_config_tampering_is_rejected_even_with_same_labels(self):
        self.write()
        import gzip
        data = gzip.decompress(self.path.read_bytes()).replace(b'"amd64"', b'"arm64"')
        self.path.write_bytes(gzip_bytes(data))
        with self.assertRaisesRegex(ValueError, "config digest mismatch"):
            AWS.archive_config_ids(self.path, [self.image], self.sha)

    def test_wrong_tag_platform_source_and_version_are_rejected(self):
        self.write(tag="cleanapp-aws/other:unselected")
        with self.assertRaisesRegex(ValueError, "unexpected or duplicate export tag"):
            AWS.archive_config_ids(self.path, [self.image], self.sha)
        for field, value in (("architecture", "arm64"), ("os", "windows")):
            self.write(config={**self.config, field: value})
            with self.assertRaisesRegex(ValueError, "linux/amd64"):
                AWS.archive_config_ids(self.path, [self.image], self.sha)
        for field in ("org.opencontainers.image.revision", "org.opencontainers.image.version"):
            config = json.loads(json.dumps(self.config))
            config["config"]["Labels"][field] = "unreviewed"
            self.write(config=config)
            with self.assertRaisesRegex(ValueError, "source/version mismatch"):
                AWS.archive_config_ids(self.path, [self.image], self.sha)

    def test_missing_expected_tag_is_rejected(self):
        self.write()
        extra = {**self.image, "tag": "cleanapp-aws/missing:fixture"}
        with self.assertRaisesRegex(ValueError, "missing expected export tag"):
            AWS.archive_config_ids(self.path, [self.image, extra], self.sha)

    def test_duplicate_archive_members_and_config_json_keys_are_rejected(self):
        raw = json.dumps(self.config)
        data = export_tar({self.image["tag"]: raw, "other:tag": raw})
        self.path.write_bytes(gzip_bytes(data))
        with self.assertRaisesRegex(ValueError, "duplicate export archive member"):
            AWS.archive_config_ids(self.path, [self.image], self.sha)
        raw = raw.replace('"architecture": "amd64"', '"architecture": "amd64", "architecture": "arm64"')
        self.path.write_bytes(gzip_bytes(export_tar({self.image["tag"]: raw})))
        with self.assertRaisesRegex(ValueError, "duplicate export metadata JSON key"):
            AWS.archive_config_ids(self.path, [self.image], self.sha)


def gzip_bytes(value):
    # Match the deterministic mtime=0 stream used by the exporter.
    import gzip
    import io
    output = io.BytesIO()
    with gzip.GzipFile(fileobj=output, mode="wb", mtime=0, filename="images.tar") as stream:
        stream.write(value)
    return output.getvalue()


if __name__ == "__main__":
    unittest.main()
