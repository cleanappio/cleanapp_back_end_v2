#!/usr/bin/env python3
"""Read-only phone queue monitoring; never requeue reports or restart workers."""

import base64
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.request


def evaluate(queue, connected, completed, previous, now, stall_seconds, backlog_seconds):
    ready = int(queue["messages_ready"])
    outstanding = ready + int(queue["messages_unacknowledged"])
    last_progress = previous.get("last_progress_at", now)
    backlog_since = previous.get("backlog_since", now)
    # A process restart resets its counter. Allow one grace window for recovery.
    if not outstanding or completed != previous.get("completed", completed):
        last_progress = now
    # A busy worker with no waiting messages is healthy when completions advance.
    # The latency budget applies to reports waiting in the ready queue.
    if not ready or not previous.get("ready", 0):
        backlog_since = now
    state = {
        "observed_at": now, "completed": completed, "outstanding": outstanding, "ready": ready,
        "last_progress_at": last_progress, "backlog_since": backlog_since,
    }
    details = "ready={} unacked={} consumers={} completed={} stalled_seconds={} backlog_seconds={}".format(
        queue["messages_ready"], queue["messages_unacknowledged"], queue["consumers"],
        completed, now - last_progress, now - backlog_since,
    )
    if not connected or int(queue["consumers"]) < 1:
        return state, "human analyzer disconnected or missing consumer; " + details
    if outstanding and now - last_progress >= stall_seconds:
        return state, "human queue has no successful completions; " + details
    if ready and now - backlog_since >= backlog_seconds:
        return state, "human backlog persists beyond latency budget; " + details
    return state, None


def snapshot():
    rabbit = os.environ.get("RABBIT_CONTAINER", "cleanapp_rabbitmq")
    worker = os.environ.get("HUMAN_ANALYZER_CONTAINER", "cleanapp_report_analyze_human")
    # Inspect output contains secrets. Keep it in memory and print only queue counts.
    container = json.loads(subprocess.check_output(
        ["sudo", "-n", "docker", "inspect", rabbit], text=True, timeout=10,
        stderr=subprocess.DEVNULL,
    ))[0]
    env = dict(item.split("=", 1) for item in container["Config"]["Env"] if "=" in item)
    auth = base64.b64encode((env["RABBITMQ_DEFAULT_USER"] + ":" + env["RABBITMQ_DEFAULT_PASS"]).encode()).decode()
    api = os.environ.get("API_BASE", "http://127.0.0.1:15672/api")
    request = urllib.request.Request(api + "/queues/%2F/report-analysis-human-queue",
                                     headers={"Authorization": "Basic " + auth})
    with urllib.request.urlopen(request, timeout=10) as response:
        queue = json.load(response)
    metrics = subprocess.check_output(
        ["sudo", "-n", "docker", "exec", worker, "wget", "-qO-", "http://127.0.0.1:8080/metrics"],
        text=True, timeout=10, stderr=subprocess.DEVNULL,
    )
    connected = re.search(r"^cleanapp_analyzer_rabbitmq_connected ([0-9.e+-]+)$", metrics, re.M)
    completed = re.search(r'^cleanapp_analyzer_rabbitmq_processed_total\{result="success"\} ([0-9.e+-]+)$', metrics, re.M)
    if connected is None:
        raise ValueError("analyzer connection metric missing")
    return queue, float(connected[1]) == 1, float(completed[1]) if completed else 0


def main():
    state_path = Path(os.environ.get("HUMAN_QUEUE_STATE_FILE", str(Path.home() / "cleanapp_watchdog/human_queue_state.json")))
    previous = json.loads(state_path.read_text()) if state_path.exists() else {}
    queue, connected, completed = snapshot()
    state, failure = evaluate(queue, connected, completed, previous, int(time.time()),
                              int(os.environ.get("HUMAN_QUEUE_STALL_SECONDS", "300")),
                              int(os.environ.get("HUMAN_QUEUE_BACKLOG_SECONDS", "600")))
    state["healthy"] = failure is None
    temporary = state_path.with_suffix(".tmp")
    temporary.write_text(json.dumps(state) + "\n")
    temporary.replace(state_path)
    if failure:
        print("[watchdog human] FAIL: " + failure, file=sys.stderr)
        return 1
    print("[watchdog human] OK: ready={} unacked={} consumers={}".format(
        queue["messages_ready"], queue["messages_unacknowledged"], queue["consumers"]))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        # Avoid including command output/credentials in an operational error.
        print("[watchdog human] FAIL: snapshot/state unavailable ({})".format(type(error).__name__), file=sys.stderr)
        sys.exit(1)
