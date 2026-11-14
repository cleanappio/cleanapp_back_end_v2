"""Exercise the first-role build artifact using only a temporary Git repository."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch


VM = Path(__file__).resolve().parents[1]
DEPLOY_SPEC = importlib.util.spec_from_file_location("machine_build_deploy_fixture", VM / "aws_image_deploy.py")
DEPLOY = importlib.util.module_from_spec(DEPLOY_SPEC)
DEPLOY_SPEC.loader.exec_module(DEPLOY)
BUILD_SPEC = importlib.util.spec_from_file_location("machine_build_fixture", VM / "aws_build_machine_analyzer.py")
BUILD = importlib.util.module_from_spec(BUILD_SPEC)
with patch.dict(sys.modules, {"aws_image_deploy": DEPLOY}):
    BUILD_SPEC.loader.exec_module(BUILD)


class SaveProcess:
    def __init__(self, data, code):
        self.stdout = io.BytesIO(data)
        self.code = code

    def wait(self):
        return self.code


class MachineAnalyzerBuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.repo = self.base / "repo"
        service = self.repo / "report-analyze-pipeline"
        (service / "nested").mkdir(parents=True)
        (service / "Dockerfile").write_text("FROM fixture\nCOPY . .\n")
        (service / "go.mod").write_text("replace cleanapp-common => ../go-common\n")
        (service / ".version").write_text("BUILD_VERSION=1.0.42\n")
        (service / "source.txt").write_text("reviewed committed source\n")
        # Even accidentally tracked credential-shaped files must be excluded.
        (service / ".env.production").write_text("FIXTURE=not-a-credential\n")
        (service / "fixture.key").write_text("not-a-credential\n")
        (service / "nested/.env.worker").write_text("FIXTURE=not-a-credential\n")
        (service / "nested/identity.pem").write_text("not-a-credential\n")
        common = self.repo / "go-common"
        common.mkdir()
        (common / "common.go").write_text("package common\n")
        (common / "credentials.json").write_text('{"fixture": true}\n')
        self.git("init", "--quiet")
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "--quiet", "-m", "reviewed source")
        self.sha = self.git("rev-parse", "HEAD").decode().strip()
        (service / "source.txt").write_text("dirty source must not be built\n")
        (service / ".version").write_text("BUILD_VERSION=9.9.9\n")
        (service / "untracked.go").write_text("package unreviewed\n")
        (service / ".env.local").write_text("FIXTURE=not-a-credential\n")
        (common / "untracked.go").write_text("package unreviewed\n")
        self.output = self.base / "output"
        self.events = []
        self.build_count = 0
        self.export_failure = False
        self.tamper_export = False
        self.wrong_export_source = False
        self.runner_id = "sha256:" + hashlib.sha256(b"containerd runner manifest").hexdigest()
        self.original_command = DEPLOY.command
        self.original_popen = subprocess.Popen

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.repo, check=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout

    def command(self, raw_args, **kwargs):
        args = [str(arg) for arg in raw_args]
        self.events.append(args)
        if args[0] == "git":
            return self.original_command(args, **kwargs)
        if args[:2] == ["docker", "build"]:
            self.build_count += 1
            context = Path(args[-1])
            self.assertEqual((context / "source.txt").read_text(), "reviewed committed source\n")
            self.assertEqual((context / "go-common/common.go").read_text(), "package common\n")
            self.assertEqual((context / "go.mod").read_text(), "replace cleanapp-common => /go-common\n")
            for name in (".env.production", ".env.local", "fixture.key", "nested/.env.worker",
                         "nested/identity.pem", "untracked.go", "go-common/credentials.json",
                         "go-common/untracked.go"):
                self.assertFalse((context / name).exists(), name)
            metadata = (context / "buildinfo.vars").read_text()
            self.version = "1.0.42-aws." + self.sha[:12]
            self.assertIn("CLEANAPP_GIT_SHA=" + self.sha + "\n", metadata)
            self.assertIn("CLEANAPP_BUILD_VERSION=" + self.version + "\n", metadata)
            self.assertEqual(args[args.index("--platform") + 1], "linux/amd64")
            self.assertEqual(Path(args[args.index("--file") + 1]), context / "Dockerfile")
            self.tag = args[args.index("--tag") + 1]
            self.assertEqual(self.tag, "cleanapp-aws/report-analyze-pipeline:" + self.sha[:12])
            self.labels = dict(arg.split("=", 1) for index, arg in enumerate(args)
                               if index and args[index - 1] == "--label")
            self.assertEqual(self.labels["org.opencontainers.image.revision"], self.sha)
            self.assertEqual(self.labels["org.opencontainers.image.version"], self.version)
            return b""
        if args[:3] == ["docker", "image", "inspect"]:
            self.assertEqual(args[-1], self.tag)
            return json.dumps([{"Id": self.runner_id, "Architecture": "amd64", "Os": "linux",
                                "Config": {"Labels": self.labels}}]).encode()
        self.fail("Unexpected external command: " + repr(args))

    def popen(self, raw_args, **kwargs):
        args = [str(arg) for arg in raw_args]
        if args[0] == "git":
            return self.original_popen(raw_args, **kwargs)
        self.assertEqual(args, ["docker", "image", "save", self.tag])
        self.events.append(args)
        labels = dict(self.labels)
        if self.wrong_export_source:
            labels["org.opencontainers.image.revision"] = "b" * 40
        raw = json.dumps({"architecture": "amd64", "os": "linux",
                          "config": {"Labels": labels}}).encode()
        digest = hashlib.sha256(raw).hexdigest()
        self.exported_config_id = "sha256:" + digest
        config_name = "blobs/sha256/" + digest
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode="w") as archive:
            for name, data in ((config_name, raw + b" " if self.tamper_export else raw),
                               ("manifest.json", json.dumps([{"Config": config_name,
                                 "RepoTags": [self.tag], "Layers": []}]).encode())):
                member = tarfile.TarInfo(name)
                member.size = len(data)
                archive.addfile(member, io.BytesIO(data))
        return SaveProcess(stream.getvalue(), 7 if self.export_failure else 0)

    def build(self, ref=None):
        with patch.object(DEPLOY, "ROOT", self.repo), \
             patch.object(DEPLOY, "command", self.command), \
             patch.object(subprocess, "Popen", self.popen):
            return BUILD.build(ref or self.sha, self.output)

    def test_committed_source_excludes_credentials_and_manifest_binds_exported_config(self):
        manifest = self.build()
        self.assertEqual(self.build_count, 1)
        self.assertEqual(manifest["source_sha"], self.sha)
        self.assertEqual(manifest["image"]["runner_image_id"], self.runner_id)
        self.assertEqual(manifest["image"]["image_id"], self.exported_config_id)
        self.assertNotEqual(self.runner_id, self.exported_config_id)
        archive = self.output / "images.tar.gz"
        self.assertEqual(manifest["archive_sha256"], hashlib.sha256(archive.read_bytes()).hexdigest())
        self.assertEqual(manifest, json.loads((self.output / "image-manifest.json").read_text()))
        self.assertFalse(manifest["production_contacted"])
        self.assertEqual(manifest["migrations"], 0)
        self.assertTrue(any(args[:2] == ["git", "archive"] and args[-1] == self.sha
                            for args in self.events))

    def test_exported_config_digest_tampering_rejects_artifact(self):
        self.tamper_export = True
        with self.assertRaisesRegex(ValueError, "export config digest mismatch"):
            self.build()
        self.assertFalse((self.output / "image-manifest.json").exists())

    def test_different_exported_source_rejects_artifact(self):
        self.wrong_export_source = True
        with self.assertRaisesRegex(ValueError, "export config source/version mismatch"):
            self.build()
        self.assertFalse((self.output / "image-manifest.json").exists())

    def test_failed_docker_save_does_not_publish_manifest(self):
        self.export_failure = True
        with self.assertRaisesRegex(ValueError, "Docker image export failed"):
            self.build()
        self.assertFalse((self.output / "image-manifest.json").exists())

    def test_nonexact_revision_fails_before_any_external_command(self):
        for ref in ("HEAD", self.sha[:12]):
            with self.subTest(ref=ref), self.assertRaisesRegex(ValueError, "exact full source commit"):
                self.build(ref)
        self.assertEqual(self.events, [])
        self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()
