"""Run the canonical helper against a local, isolated fake build host."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[5]
HELPER = Path("platform_blueprint/deploy/prod/vm/source_build_and_deploy.sh")
VM_SCRIPTS = HELPER.parent


class SourceBuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.repo = self.base / "repo"
        self.repo.mkdir()
        self.bin = self.base / "bin"
        self.bin.mkdir()
        self.events = self.base / "events.jsonl"
        self.pin = self.base / "digest-pin-created"
        self.remote = self.base / "remote"
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        EVENT_LOG=str(self.events), PIN_FILE=str(self.pin),
                        HOST="fake-build-host", REMOTE_BUILD_ROOT=str(self.remote),
                        CLOUDSDK_CONFIG=str(self.base / "empty-cloud-config"),
                        PROJECT_NAME="fixture-project", CLOUD_REGION="fixture-region",
                        SOURCE_SERVICES="report-listener customer-service", DEPLOY_SERVICES="",
                        KEEP_REMOTE_SOURCE="1", DRY_RUN="0", RUN_GO_MIGRATIONS="1")
        self.env.pop("BUILD_ONLY", None)
        self.env.pop("MAKEFLAGS", None)
        self.env.pop("MFLAGS", None)
        for name in (HELPER, VM_SCRIPTS / "source_build_map.sh", Path("Makefile")):
            target = self.repo / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(REPO / name, target)
        self.script(self.bin / "ssh", """
import os,subprocess,sys
assert len(sys.argv)==3 and sys.argv[1]=='fake-build-host'
raise SystemExit(subprocess.run(['bash','-c',sys.argv[2]],env=os.environ).returncode)
""")
        self.script(self.bin / "gcloud", """
import json,os,sys
from pathlib import Path
args=sys.argv[1:]
assert args==['config','set','project','fixture-project'] or args[:4]==['artifacts','docker','tags','add'] or args[:2]==['builds','submit']
with open(os.environ['EVENT_LOG'],'a') as f:f.write(json.dumps(['gcloud',*args])+'\\n')
if args[:2]==['builds','submit']:
 config=next(x.split('=',1)[1] for x in args if x.startswith('--config='))
 with open(os.environ['EVENT_LOG'],'a') as f:
  f.write(json.dumps(['news-context',Path.cwd().name,Path(config).read_text(),Path('src/fixture.rs').read_text(),os.environ['CLEANAPP_GIT_SHA_OVERRIDE'],os.environ['CLEANAPP_BUILD_TIME_OVERRIDE']])+'\\n')
 if os.environ.get('FAIL_NEWS_BUILD')==Path.cwd().name:raise SystemExit(21)
""")
        for service in ("report-listener", "customer-service"):
            directory = self.repo / service
            directory.mkdir()
            self.shell(directory / "build_image.sh", f"""
DOCKER_IMAGE="fixture-images/{service}"
if [[ "${{FAIL_SERVICE:-}}" == "{service}" ]]; then exit 19; fi
python3 - <<'PY'
import json,os
with open(os.environ['EVENT_LOG'],'a') as f:
 f.write(json.dumps(['build','{service}','committed',os.environ['CLEANAPP_GIT_SHA_OVERRIDE']])+'\\n')
PY
printf 'BUILD_VERSION=42\\n' > .version
""")
        self.shell(self.repo / VM_SCRIPTS / "run_go_migrations_remote.sh", """
python3 - "$@" <<'PY'
import json,os,sys
with open(os.environ['EVENT_LOG'],'a') as f:f.write(json.dumps(['migrate',*sys.argv[1:]])+'\\n')
PY
""")
        self.shell(self.repo / VM_SCRIPTS / "deploy_with_digests.sh", """
python3 - <<'PY'
import json,os
from pathlib import Path
with open(os.environ['EVENT_LOG'],'a') as f:f.write(json.dumps(['pin-up',os.environ['SERVICES'],os.environ['RUN_GO_MIGRATIONS']])+'\\n')
Path(os.environ['PIN_FILE']).write_text('selected services activated')
PY
""")
        self.git("init", "--quiet")
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "--quiet", "-m", "reviewed source")
        self.commit = self.git("rev-parse", "HEAD").stdout.strip()
        self.env["REF"] = self.commit
        # A build from the working directory, instead of REF, would be detected.
        path = self.repo / "report-listener/build_image.sh"
        path.write_text(path.read_text().replace("'committed'", "'uncommitted'"))

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.repo, check=True, text=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def script(self, path, body):
        path.write_text("#!" + sys.executable + "\n" + body.lstrip())
        path.chmod(0o755)

    def shell(self, path, body):
        path.write_text("#!/usr/bin/env bash\nset -euo pipefail\n" + body.lstrip())
        path.chmod(0o755)

    def run_helper(self, value=None, make=False):
        env = dict(self.env)
        if value is not None and not make:
            env["BUILD_ONLY"] = value
        args = (["make", "--no-print-directory", "deploy-prod-source"] if make
                else ["bash", str(self.repo / HELPER)])
        if make and value is not None:
            args.append("BUILD_ONLY=" + value)
        return subprocess.run(args, cwd=self.repo, env=env, text=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)

    def recorded(self):
        return [json.loads(line) for line in self.events.read_text().splitlines()] if self.events.exists() else []

    def assert_source_builds(self, events):
        builds = [event for event in events if event[0] == "build"]
        self.assertEqual(builds, [["build", name, "committed", self.commit[:12]]
                                  for name in ("report-listener", "customer-service")])
        promotions = [event for event in events if event[:5] == ["gcloud", "artifacts", "docker", "tags", "add"]]
        self.assertEqual(promotions, [["gcloud", "artifacts", "docker", "tags", "add",
            f"fixture-region-docker.pkg.dev/fixture-project/fixture-images/{name}:42",
            f"fixture-region-docker.pkg.dev/fixture-project/fixture-images/{name}:prod"]
            for name in ("report-listener", "customer-service")])

    def test_build_only_preserves_exact_build_and_promotion_but_never_migrates_or_pins(self):
        result = self.run_helper("1")
        self.assertEqual(result.returncode, 0, result.stderr)
        events = self.recorded(); self.assert_source_builds(events)
        self.assertFalse(any(event[0] in ("migrate", "pin-up") for event in events))
        self.assertFalse(self.pin.exists())
        self.assertNotIn("deployed pinned services", result.stdout)

    def test_default_still_migrates_then_pins_exact_selected_services(self):
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)
        events = self.recorded(); self.assert_source_builds(events)
        self.assertEqual(events[-2:], [["migrate", "cleanapp_report_listener", "cleanapp_customer_service"],
                                    ["pin-up", "cleanapp_report_listener cleanapp_customer_service", "0"]])
        self.assertTrue(self.pin.exists())
        self.assertIn("deployed pinned services", result.stdout)

    def test_make_forwards_build_only_to_the_same_canonical_helper(self):
        result = self.run_helper("1", make=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        events = self.recorded(); self.assert_source_builds(events)
        self.assertFalse(any(event[0] in ("migrate", "pin-up") for event in events))
        self.assertFalse(self.pin.exists())

    def test_only_literal_zero_or_one_is_allowed_before_any_remote_work(self):
        for value in ("", "true", "false", "2", "01", " 1", "1 "):
            with self.subTest(value=value):
                result = self.run_helper(value)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.events.exists())
                self.assertFalse(self.remote.exists())

    def test_partial_build_failure_cannot_migrate_or_activate_services(self):
        self.env["FAIL_SERVICE"] = "customer-service"
        result = self.run_helper("1")
        self.assertEqual(result.returncode, 19)
        events = self.recorded()
        self.assertEqual([event[1] for event in events if event[0] == "build"], ["report-listener"])
        self.assertEqual(len([event for event in events if event[:5] == ["gcloud", "artifacts", "docker", "tags", "add"]]), 1)
        self.assertFalse(any(event[0] in ("migrate", "pin-up") for event in events))
        self.assertFalse(self.pin.exists())

    def add_news_sources(self):
        adapter = Path("scripts/build/build_news_replacement_images.sh")
        destination = self.repo / adapter
        destination.parent.mkdir(parents=True)
        shutil.copy2(REPO / adapter, destination)
        for service, version in (("news-indexer-bluesky", "1.0.10\n"),
                                 ("news-indexer", "BUILD_VERSION=1.0.33\n")):
            directory = self.repo / service
            (directory / "src").mkdir(parents=True)
            (directory / "src/fixture.rs").write_text("reviewed:" + service)
            (directory / ".version").write_text(version)
            shutil.copy2(REPO / service / "cloudbuild.replacements.yaml", directory)
            # Neither the old broad build nor its targets may be used.
            self.shell(directory / "build_image.sh", "exit 99\n")
            self.shell(directory / "build_images.sh", "exit 98\n")
        for service in ("report-fast-renderer", "report-listener-v4", "replier-twitter"):
            directory = self.repo / service
            directory.mkdir()
            self.shell(directory / "build_image.sh", f"""
DOCKER_IMAGE="fixture-images/{service}"
printf 'BUILD_VERSION=42\\n' > .version
""")
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "--quiet", "-m", "exact selected news sources")
        self.commit = self.git("rev-parse", "HEAD").stdout.strip()
        self.env["REF"] = self.commit
        for service in ("news-indexer-bluesky", "news-indexer"):
            (self.repo / service / "src/fixture.rs").write_text("uncommitted")
        self.env["SOURCE_SERVICES"] = "news-indexer-bluesky news-indexer"

    def assert_selected_news(self, events):
        expected = [("news-indexer-bluesky", "1.0.11", [
            "cleanapp-bluesky-indexer-image", "cleanapp-bluesky-analyzer-image",
            "cleanapp-bluesky-submitter-image", "bluesky-now"]),
            ("news-indexer", "1.0.34", ["cleanapp-news-analyzer-twitter-image"])]
        contexts = [event for event in events if event[0] == "news-context"]
        self.assertEqual(len(contexts), 2)
        for event, (service, _, _) in zip(contexts, expected):
            self.assertEqual(event[1], service)
            self.assertEqual(event[3:5], ["reviewed:" + service, self.commit[:12]])
            self.assertRegex(event[5], r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T")
            self.assertEqual(event[2], (REPO / service / "cloudbuild.replacements.yaml").read_text())
            targets = (["indexer", "analyzer", "submitter", "stream"]
                       if service == "news-indexer-bluesky" else ["analyzer"])
            self.assertEqual(re.findall(r"'--target', '([^']+)'", event[2]), targets)
            self.assertEqual(event[2].count("name: 'gcr.io/cloud-builders/docker'"), len(targets))
            substitutions = (["INDEXER", "ANALYZER", "SUBMITTER", "STREAM"]
                             if service == "news-indexer-bluesky" else ["ANALYZER"])
            self.assertEqual(re.findall(r"'-t', '\$\{_TAG_([A-Z]+)\}'", event[2]), substitutions)
            self.assertEqual(re.findall(r"^  - '\$\{_TAG_([A-Z]+)\}'$", event[2], re.MULTILINE), substitutions)
        builds = [event for event in events if event[:3] == ["gcloud", "builds", "submit"]]
        self.assertEqual(len(builds), 2)
        for build, (service, version, images) in zip(builds, expected):
            self.assertIn("--region=fixture-region", build)
            self.assertIn("--config=cloudbuild.replacements.yaml", build)
            self.assertEqual(build[-1], ".")
            substitutions = next(item.split("=", 1)[1] for item in build if item.startswith("--substitutions="))
            tags = dict(item.split("=", 1) for item in substitutions.split(","))
            self.assertEqual(set(tags.values()), {
                f"fixture-region-docker.pkg.dev/fixture-project/cleanapp-docker-repo/{image}:{version}"
                for image in images})
        actual_promotions = [event[-1] for event in events
                             if event[:5] == ["gcloud", "artifacts", "docker", "tags", "add"]]
        self.assertEqual(sorted(actual_promotions), sorted(
            f"fixture-region-docker.pkg.dev/fixture-project/cleanapp-docker-repo/{image}:{tag}"
            for _, _, images in expected for image in images for tag in ("dev", "prod")))
        for forbidden in ("cleanapp-report-analyzer-image", "cleanapp-news-indexer-twitter-image",
                          "cleanapp-news-submitter-twitter-image"):
            self.assertFalse(any(forbidden in item for event in events for item in event if isinstance(item, str)))

    def test_selected_news_build_only_uses_exact_context_and_only_five_images(self):
        self.add_news_sources()
        result = self.run_helper("1")
        self.assertEqual(result.returncode, 0, result.stderr)
        events = self.recorded()
        self.assert_selected_news(events)
        self.assertFalse(any(event[0] in ("migrate", "pin-up") for event in events))
        self.assertFalse(self.pin.exists())

    def test_selected_news_normal_deploy_maps_only_five_runtime_services(self):
        self.add_news_sources()
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)
        events = self.recorded()
        self.assert_selected_news(events)
        services = ["cleanapp_bluesky_indexer", "cleanapp_bluesky_analyzer",
                    "cleanapp_bluesky_submitter", "cleanapp_bluesky_now",
                    "cleanapp_news_analyzer_twitter"]
        self.assertEqual(events[-2:], [["migrate", *services], ["pin-up", " ".join(services), "0"]])

    def test_failed_news_build_does_not_promote_that_source_or_activate_anything(self):
        self.add_news_sources()
        self.env["FAIL_NEWS_BUILD"] = "news-indexer"
        result = self.run_helper("1")
        self.assertEqual(result.returncode, 21, result.stderr)
        events = self.recorded()
        self.assertFalse(any(event[0] in ("migrate", "pin-up") for event in events))
        self.assertFalse(any("cleanapp-news-analyzer-twitter-image" in item for event in events
                             if event[:5] == ["gcloud", "artifacts", "docker", "tags", "add"]
                             for item in event))

    def test_complete_replacement_scope_maps_exactly_eight_services(self):
        self.add_news_sources()
        self.env["SOURCE_SERVICES"] += " report-fast-renderer report-listener-v4 replier-twitter"
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)
        events = self.recorded()
        services = ["cleanapp_bluesky_indexer", "cleanapp_bluesky_analyzer",
                    "cleanapp_bluesky_submitter", "cleanapp_bluesky_now",
                    "cleanapp_news_analyzer_twitter", "cleanapp_report_renderer_service",
                    "cleanapp_report_listener_v4", "cleanapp_replier_twitter"]
        self.assertEqual(events[-2:], [["migrate", *services], ["pin-up", " ".join(services), "0"]])
        promotions = [event for event in events if event[:5] == ["gcloud", "artifacts", "docker", "tags", "add"]
                      and event[-1].endswith(":prod")]
        self.assertEqual(len(promotions), 8)

    def test_news_adapter_rejects_unknown_scope_and_invalid_version_before_cloud_calls(self):
        self.add_news_sources()
        adapter = self.repo / "scripts/build/build_news_replacement_images.sh"
        for args in (("news-indexer-curator", "-e", "dev"), ("news-indexer", "-e", "prod")):
            with self.subTest(args=args):
                result = subprocess.run(["bash", str(adapter), *args], cwd=self.repo, env=self.env,
                                        text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
                self.assertEqual(result.returncode, 2)
                self.assertFalse(self.events.exists())
        (self.repo / "news-indexer/.version").write_text("BUILD_VERSION=not-a-version\n")
        result = subprocess.run(["bash", str(adapter), "news-indexer", "-e", "dev"], cwd=self.repo,
                                env=self.env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
        self.assertEqual(result.returncode, 2)
        self.assertFalse(self.events.exists())


if __name__ == "__main__":
    unittest.main()
