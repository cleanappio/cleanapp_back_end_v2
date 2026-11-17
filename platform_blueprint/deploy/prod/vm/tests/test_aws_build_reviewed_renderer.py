"""Exercise build-only source/archive boundaries without Docker or production."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch


VM = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("renderer_export_deploy_fixture", VM / "aws_image_deploy.py")
DEPLOY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DEPLOY)
SPEC = importlib.util.spec_from_file_location("renderer_export_fixture", VM / "aws_build_reviewed_renderer.py")
BUILD = importlib.util.module_from_spec(SPEC)
with patch.dict(sys.modules, {"aws_image_deploy": DEPLOY}):
    SPEC.loader.exec_module(BUILD)


class SaveProcess:
    def __init__(self, payload, code):
        self.stdout = io.BytesIO(payload)
        self.code = code
        self.returncode = None
        self.terminated = False

    def wait(self, timeout):
        self.returncode = self.code if self.returncode is None else self.returncode
        return self.returncode

    def poll(self):
        return self.returncode

    def terminate(self):
        self.terminated = True
        self.returncode = -15

    def kill(self):
        self.returncode = -9


class ReviewedRendererBuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repo = Path(self.temporary.name) / "repo"
        self.service = self.repo / "report-fast-renderer"
        self.service.mkdir(parents=True)
        (self.service / "Dockerfile").write_text("FROM fixture\nCOPY . .\n")
        (self.service / "Cargo.toml").write_text('[dependencies]\ncommon = { path = "../rust-common" }\n')
        (self.service / ".version").write_text("BUILD_VERSION=1.2.3\n")
        (self.service / "source.rs").write_text("committed source\n")
        (self.service / ".env.production").write_text("FIXTURE=not-a-credential\n")
        common = self.repo / "rust-common"
        common.mkdir()
        (common / "Cargo.toml").write_text("committed common\n")
        (common / "fixture.key").write_text("not-a-credential\n")
        helpers = self.repo / BUILD.HELPER_DIR
        helpers.mkdir(parents=True)
        for name in ("aws_image_deploy.py", "aws_build_reviewed_renderer.py"):
            shutil.copyfile(VM / name, helpers / name)
        self.git("init", "--quiet")
        self.commit()
        (self.service / "source.rs").write_text("dirty source\n")
        (self.service / "untracked.rs").write_text("untracked source\n")
        self.output = Path(self.temporary.name) / "artifact"
        self.events = []
        self.code = 0
        self.tamper_config = False
        self.wrong_source = False
        self.real_command = DEPLOY.command
        self.real_popen = subprocess.Popen

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.repo, check=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout

    def commit(self):
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "--quiet", "-m", "reviewed source")
        self.sha = self.git("rev-parse", "HEAD").decode().strip()

    def command(self, raw, **kwargs):
        args = [str(arg) for arg in raw]
        self.events.append(args)
        if args[0] == "git":
            return self.real_command(args, **kwargs)
        if args[:2] == ["docker", "build"]:
            self.assertEqual(args[args.index("--platform") + 1], "linux/amd64")
            context = Path(args[-1])
            self.assertEqual((context / "source.rs").read_text(), "committed source\n")
            self.assertEqual((context / "rust-common/Cargo.toml").read_text(), "committed common\n")
            self.assertNotIn("../rust-common", (context / "Cargo.toml").read_text())
            for filename in (".env.production", "untracked.rs", "rust-common/fixture.key"):
                self.assertFalse((context / filename).exists(), filename)
            self.assertIn("CLEANAPP_GIT_SHA=" + self.sha, (context / "buildinfo.vars").read_text())
            self.tag = args[args.index("--tag") + 1]
            self.labels = dict(args[index + 1].split("=", 1) for index, arg in enumerate(args) if arg == "--label")
            return b""
        if args[:3] == ["docker", "image", "inspect"]:
            return json.dumps([{"Architecture": "amd64", "Os": "linux", "Id": "sha256:" + "a" * 64}]).encode()
        self.fail("Unexpected external command: " + repr(args))

    def popen(self, raw, **kwargs):
        args = [str(arg) for arg in raw]
        if args[0] == "git":
            return self.real_popen(raw, **kwargs)
        self.assertEqual(args, ["docker", "image", "save", self.tag])
        self.events.append(args)
        labels = dict(self.labels)
        if self.wrong_source:
            labels["org.opencontainers.image.revision"] = "b" * 40
        config = json.dumps({"architecture": "amd64", "os": "linux", "config": {"Labels": labels}}).encode()
        digest = hashlib.sha256(config).hexdigest()
        self.config_id = "sha256:" + digest
        name = "blobs/sha256/" + digest
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode="w") as archive:
            for filename, data in ((name, config + b" " if self.tamper_config else config),
                                   ("manifest.json", json.dumps([{"Config": name, "RepoTags": [self.tag], "Layers": []}]).encode())):
                member = tarfile.TarInfo(filename)
                member.size = len(data)
                archive.addfile(member, io.BytesIO(data))
        self.save = SaveProcess(stream.getvalue(), self.code)
        return self.save

    def build(self, ref=None):
        with patch.object(DEPLOY, "ROOT", self.repo), patch.object(DEPLOY, "command", self.command), \
                patch.object(subprocess, "Popen", self.popen):
            return BUILD.build(ref or self.sha, self.output)

    def assert_no_artifact(self):
        self.assertFalse((self.output / "image-manifest.json").exists())
        self.assertFalse((self.output / "images.tar.gz").exists())
        self.assertFalse((self.output / "images.tar.gz.partial").exists())

    def test_exact_source_dependencies_and_verified_export_are_retained_without_production_calls(self):
        proof = self.build()
        self.assertEqual(proof["role"], "cleanapp_report_renderer_service")
        self.assertEqual(proof["source_sha"], self.sha)
        self.assertEqual(proof["image"]["image_id"], self.config_id)
        self.assertNotEqual(proof["image"]["runner_image_id"], self.config_id)
        self.assertEqual(proof["image"]["version"], "1.2.3-aws." + self.sha[:12])
        self.assertEqual(proof["archive_sha256"], BUILD.digest(self.output / "images.tar.gz"))
        self.assertEqual(proof["archive_bytes"], (self.output / "images.tar.gz").stat().st_size)
        self.assertEqual(proof["migrations"], 0)
        self.assertFalse(proof["production_contacted"])
        self.assertFalse(proof["deployment_authorized_by_artifact"])
        self.assertEqual(len(proof["source_helper_sha256"]), 2)
        self.assertEqual(proof, json.loads((self.output / "image-manifest.json").read_text()))

    def test_short_or_symbolic_ref_refuses_before_commands(self):
        for ref in ("HEAD", self.sha[:12]):
            with self.subTest(ref=ref), self.assertRaisesRegex(ValueError, "exact full source commit"):
                self.build(ref)
        self.assertEqual(self.events, [])
        self.assertFalse(self.output.exists())

    def test_requested_commit_must_bind_the_running_build_helpers(self):
        helper = self.repo / BUILD.HELPER_DIR / "aws_build_reviewed_renderer.py"
        helper.write_bytes(helper.read_bytes() + b"\n# differing committed helper\n")
        self.commit()
        with self.assertRaisesRegex(ValueError, "Build helper differs"):
            self.build()
        self.assertFalse(any(event[0] == "docker" for event in self.events))
        self.assert_no_artifact()

    def test_export_config_tampering_refuses_without_artifact(self):
        self.tamper_config = True
        with self.assertRaisesRegex(ValueError, "export config digest mismatch"):
            self.build()
        self.assert_no_artifact()

    def test_wrong_export_source_refuses_without_artifact(self):
        self.wrong_source = True
        with self.assertRaisesRegex(ValueError, "export config source/version mismatch"):
            self.build()
        self.assert_no_artifact()

    def test_failed_export_refuses_without_artifact(self):
        self.code = 7
        with self.assertRaisesRegex(ValueError, "Docker image export failed"):
            self.build()
        self.assert_no_artifact()

    def test_size_bound_terminates_export_and_removes_partial_archive(self):
        with patch.object(BUILD, "MAX_ARCHIVE_BYTES", 1):
            with self.assertRaisesRegex(ValueError, "Image archive exceeds size bound"):
                self.build()
        self.assertTrue(self.save.terminated)
        self.assert_no_artifact()

    def test_existing_output_is_not_reused_or_overwritten(self):
        self.output.mkdir()
        sentinel = self.output / "unrelated.txt"
        sentinel.write_text("retain")
        with self.assertRaises(FileExistsError):
            self.build()
        self.assertEqual(sentinel.read_text(), "retain")
        self.assertFalse(any(event[0] == "docker" for event in self.events))


if __name__ == "__main__":
    unittest.main()
