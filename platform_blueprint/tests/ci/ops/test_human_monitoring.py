#!/usr/bin/env python3
"""Regression checks for human-lane deploy pins and passive monitoring."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[4]
WATCHDOG = ROOT / "platform_blueprint/ops/watchdog/vm"
DEPLOY = ROOT / "platform_blueprint/deploy/prod/vm"
spec = importlib.util.spec_from_file_location("human_queue", WATCHDOG / "human_queue.py")
human_queue = importlib.util.module_from_spec(spec)
spec.loader.exec_module(human_queue)


class HumanQueueTests(unittest.TestCase):
    def evaluate(self, ready=1, unacked=1, consumers=1, connected=True, completed=10, previous=None, now=1000):
        return human_queue.evaluate({"messages_ready": ready, "messages_unacknowledged": unacked, "consumers": consumers},
                                    connected, completed, previous or {}, now, 300, 600)

    def test_disconnected_consumer_fails_even_with_zero_backlog(self):
        self.assertIn("disconnected", self.evaluate(ready=0, unacked=0, consumers=0)[1])

    def test_connected_but_stuck_work_fails(self):
        previous = {"last_progress_at": 700, "backlog_since": 700, "completed": 10, "outstanding": 2, "ready": 1}
        self.assertIn("no successful completions", self.evaluate(previous=previous)[1])

    def test_slow_persistent_backlog_fails_despite_progress(self):
        previous = {"last_progress_at": 900, "backlog_since": 400, "completed": 9, "outstanding": 2, "ready": 1}
        self.assertIn("latency budget", self.evaluate(previous=previous)[1])

    def test_idle_and_recently_progressing_work_are_healthy(self):
        previous = {"last_progress_at": 100, "backlog_since": 100, "completed": 9, "outstanding": 2, "ready": 1}
        self.assertIsNone(self.evaluate(ready=0, unacked=0, previous=previous)[1])
        self.assertIsNone(self.evaluate(previous={**previous, "backlog_since": 900})[1])

    def test_restart_resets_progress_grace_without_erasing_backlog_age(self):
        previous = {"last_progress_at": 100, "backlog_since": 900, "completed": 90, "outstanding": 2, "ready": 1}
        state, failure = self.evaluate(previous=previous)
        self.assertIsNone(failure)
        self.assertEqual(state["last_progress_at"], 1000)
        self.assertEqual(state["backlog_since"], 900)

    def test_busy_worker_without_waiting_reports_does_not_fail_backlog_budget(self):
        previous = {"last_progress_at": 100, "backlog_since": 100, "completed": 9, "outstanding": 1, "ready": 0}
        self.assertIsNone(self.evaluate(ready=0, previous=previous)[1])


class WatchdogRunnerTests(unittest.TestCase):
    def test_child_failure_is_recorded_and_recovery_is_skipped(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            (directory / "rabbitmq_ensure.sh").write_text("#!/bin/bash\nexit 7\n")
            (directory / "rabbitmq_ensure.sh").chmod(0o700)
            (directory / "golden_path.sh").write_text("#!/bin/bash\ntouch \"${CLEANAPP_WATCHDOG_DIR}/unexpected_recovery\"\n")
            (directory / "golden_path.sh").chmod(0o700)
            env = {**os.environ, "CLEANAPP_WATCHDOG_DIR": str(directory),
                   "CLEANAPP_WATCHDOG_LOCK_DIR": str(directory / "lock"),
                   "CLEANAPP_ALERT_WEBHOOK_URL": "", "CLEANAPP_WATCHDOG_WEBHOOK_URL": ""}
            result = subprocess.run(["bash", str(WATCHDOG / "run.sh")], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 7)
            status = json.loads((directory / "status.json").read_text())
            self.assertEqual(status["last_ok"], "")
            self.assertIn("rabbitmq_ensure.sh", status["last_error"])
            self.assertFalse((directory / "unexpected_recovery").exists())
            self.assertFalse((directory / "lock").exists())


class DeployPinTests(unittest.TestCase):
    def test_source_release_includes_both_analyzers(self):
        result = subprocess.check_output(["bash", str(DEPLOY / "source_build_and_deploy.sh"), "report-analyze-pipeline"],
                                         env={**os.environ, "DRY_RUN": "1"}, text=True)
        self.assertIn("deploy_services=cleanapp_report_analyze_pipeline cleanapp_report_analyze_human", result)

    def run_pin_generation(self, directory, digest_available, human_image_id=False):
        prefix = "us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/"
        analyzer_image = prefix + "cleanapp-report-analyze-pipeline-image:prod"
        resolved = {"services": {
            "cleanapp_report_analyze_pipeline": {"image": analyzer_image},
            "cleanapp_report_analyze_human": {"image": "sha256:local" if human_image_id else analyzer_image},
            "cleanapp_report_listener": {"image": prefix + "listener:prod"},
        }}
        script = (DEPLOY / "deploy_with_digests.sh").read_text().split("python3 - << 'PY'\n", 1)[1].split("\nPY\n", 1)[0]
        def docker_output(command, **kwargs):
            if "compose" in command:
                self.assertIn("--format", command)
                return json.dumps(resolved)
            return json.dumps([prefix + "cleanapp-report-analyze-pipeline-image@sha256:new"] if digest_available else [])
        previous_cwd = Path.cwd()
        try:
            os.chdir(directory)
            Path("docker-compose.digests.current.yml").write_text("services:\n  cleanapp_report_listener:\n    image: " + prefix + "listener@sha256:rollback\n")
            with patch.dict(os.environ, {"INTERNAL_PREFIX": prefix, "DIGEST_OUT": "pins.yml", "DOCKER_CMD": "docker",
                                         "SERVICES": "cleanapp_report_analyze_pipeline cleanapp_report_analyze_human"}), \
                 patch("subprocess.check_output", side_effect=docker_output), contextlib.redirect_stdout(io.StringIO()), \
                 contextlib.redirect_stderr(io.StringIO()):
                exec(compile(script, "digest_generator", "exec"), {})
            return Path("pins.yml").read_text()
        finally:
            os.chdir(previous_cwd)

    def test_inherited_human_image_gets_same_pin_and_unselected_pin_is_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            pins = self.run_pin_generation(directory, True)
            self.assertIn("cleanapp_report_analyze_human:", pins)
            self.assertIn("cleanapp_report_analyze_pipeline:", pins)
            self.assertEqual(pins.count("pipeline-image@sha256:new"), 2)
            self.assertIn("listener@sha256:rollback", pins)

    def test_missing_selected_digest_refuses_rollout(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(SystemExit) as error:
                self.run_pin_generation(directory, False)
            self.assertEqual(error.exception.code, 3)
            self.assertFalse((Path(directory) / "pins.yml").exists())

    def test_selected_local_human_image_id_resolves_to_registry_pin(self):
        with tempfile.TemporaryDirectory() as directory:
            pins = self.run_pin_generation(directory, True, human_image_id=True)
            self.assertIn("cleanapp_report_analyze_human:", pins)
            self.assertEqual(pins.count("pipeline-image@sha256:new"), 2)


if __name__ == "__main__":
    unittest.main()
