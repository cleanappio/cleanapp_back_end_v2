#!/usr/bin/env python3
"""CleanApp effectiveness scorecard.

Measures how far a first-hand report travels: received, sent to someone, seen
by a person at the responsible party, answered, fixed. Each metric is one
read-only SELECT in metrics/<id>.sql, so anyone can read exactly what a number
means. This runner executes them through the mysql command-line client and
prints the results, and can append them to a CSV ledger.

Standard library only. See README.md for how to run it.
"""
from __future__ import annotations

import argparse
import csv
import datetime as dt
import os
import re
import shlex
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

METRICS_DIR = Path(__file__).resolve().parent / "metrics"
GROUPS = ["chain", "reach", "latency", "noise", "coverage", "public_notice", "identity", "effect"]
# How many numbers each kind of metric returns: a count; a numerator and a
# denominator; a median and the number of items it was taken over.
KINDS = {"count": 1, "ratio": 2, "median": 2}
HEADER_FIELDS = ("group", "title", "kind", "window", "better", "definition")
LEDGER_COLUMNS = ("measured_at", "as_of", "metric", "value", "numerator", "denominator", "sample", "status", "note")

_HEADER = re.compile(r"^--\s*([a-z_]+):\s*(.*?)\s*$")
_INCLUDE = re.compile(r"\{\{([a-z_]+)\}\}")
_WRITE_WORDS = re.compile(
    r"\b(insert|update|delete|replace|alter|drop|create|truncate|grant|revoke|rename|call|load|handler|lock|set|into)\b",
    re.IGNORECASE,
)
_NUMBER = re.compile(r"^-?\d+(\.\d+)?$")
_ERROR_CODE = re.compile(r"ERROR (\d+)")
_MISSING_TABLE = re.compile(r"Table '(?:[^'.]*\.)?([A-Za-z0-9_]+)' doesn't exist")


@dataclass(frozen=True)
class Metric:
    id: str
    group: str
    title: str
    kind: str
    window: str
    better: str
    definition: str
    sql: str


@dataclass(frozen=True)
class Result:
    metric: Metric
    status: str  # "ok", or why the metric could not be measured
    numerator: float | None = None
    denominator: float | None = None
    sample: float | None = None

    @property
    def value(self) -> float | None:
        if self.status != "ok" or self.numerator is None:
            return None
        if self.metric.kind == "ratio":
            return self.numerator / self.denominator if self.denominator else None
        return self.numerator


def _strip_comments(sql: str) -> str:
    return "\n".join(line for line in sql.splitlines() if not line.lstrip().startswith("--")).strip()


def check_read_only(metric_id: str, sql: str) -> None:
    """Refuse anything but a single SELECT. The session is read-only as well;
    this catches a mistake before it reaches a database."""
    bare = re.sub(r"'[^']*'", "''", _strip_comments(sql))
    if ";" in bare:
        raise ValueError(f"{metric_id}: must be a single statement with no semicolon")
    if not re.match(r"(?i)^(select|with)\b", bare):
        raise ValueError(f"{metric_id}: must start with SELECT or WITH")
    found = _WRITE_WORDS.search(bare)
    if found:
        raise ValueError(f"{metric_id}: '{found.group(1)}' is not allowed in a metric")


def load_metrics(directory: Path = METRICS_DIR) -> list[Metric]:
    shared = {p.stem.lstrip("_"): _strip_comments(p.read_text()) for p in directory.glob("_*.sql")}
    metrics = []
    for path in sorted(directory.glob("*.sql")):
        if path.name.startswith("_"):
            continue
        fields, body = {}, []
        for line in path.read_text().splitlines():
            header = _HEADER.match(line) if not body else None
            if header and header.group(1) in HEADER_FIELDS:
                fields[header.group(1)] = header.group(2)
            elif line.strip() or body:
                body.append(line)
        missing = [name for name in HEADER_FIELDS if not fields.get(name)]
        if missing:
            raise ValueError(f"{path.name}: missing header {', '.join(missing)}")
        if fields["kind"] not in KINDS:
            raise ValueError(f"{path.name}: kind must be one of {', '.join(KINDS)}")
        if fields["group"] not in GROUPS:
            raise ValueError(f"{path.name}: group must be one of {', '.join(GROUPS)}")
        sql = _INCLUDE.sub(lambda m: shared[m.group(1)], "\n".join(body).strip())
        check_read_only(path.stem, sql)
        metrics.append(Metric(id=path.stem, sql=sql, **fields))
    return sorted(metrics, key=lambda m: (GROUPS.index(m.group), m.id))


def preamble(as_of: dt.datetime, backlog_min: int) -> str:
    """Session setup shared by every metric: UTC, a time limit, the windows,
    and a read-only transaction."""
    return "\n".join([
        "SET time_zone = '+00:00';",
        "SET SESSION max_execution_time = 60000;",
        f"SET @as_of = '{as_of:%Y-%m-%d %H:%M:%S}';",
        "SET @activity_from = @as_of - INTERVAL 30 DAY;",
        "SET @matured_from = @as_of - INTERVAL 37 DAY;",
        "SET @matured_to = @as_of - INTERVAL 7 DAY;",
        f"SET @backlog_min = {int(backlog_min)};",
        "START TRANSACTION READ ONLY;",
    ])


def script_for(metric: Metric, as_of: dt.datetime, backlog_min: int) -> str:
    return f"{preamble(as_of, backlog_min)}\n{metric.sql};\n"


def reduce_error(stderr: str) -> str:
    """Turn a client error into a short fixed phrase. The ledger is public, so
    no text from the server (hosts, users, paths) is copied into it."""
    table = _MISSING_TABLE.search(stderr)
    if table:
        return f"unavailable: table {table.group(1)} missing"
    code = _ERROR_CODE.search(stderr)
    if not code:
        return "unavailable: client failed"
    number = int(code.group(1))
    if number == 3024:
        return "unavailable: timed out"
    if number in (1044, 1045, 1142, 1143, 1227):
        return "unavailable: access denied"
    if number in (2002, 2003, 2005, 2013):
        return "unavailable: cannot connect"
    return f"unavailable: error {number}"


def parse_numbers(stdout: str) -> list[float | None] | None:
    """The result row: the first line whose cells are all numbers or NULL.
    Column-name lines, which the client prints in some modes, are skipped."""
    for line in stdout.splitlines():
        cells = line.strip().split("\t")
        if cells and all(cell == "NULL" or _NUMBER.match(cell) for cell in cells):
            return [None if cell == "NULL" else float(cell) for cell in cells]
    return None


def measure(metric: Metric, client: list[str], as_of: dt.datetime, backlog_min: int, run=subprocess.run) -> Result:
    try:
        done = run(client, input=script_for(metric, as_of, backlog_min), capture_output=True, text=True, timeout=180)
    except FileNotFoundError:
        return Result(metric, "unavailable: client not found")
    except subprocess.TimeoutExpired:
        return Result(metric, "unavailable: timed out")
    if done.returncode != 0:
        return Result(metric, reduce_error(done.stderr))
    numbers = parse_numbers(done.stdout)
    if numbers is None or len(numbers) != KINDS[metric.kind]:
        return Result(metric, "unavailable: unexpected output")
    if metric.kind == "count":
        return Result(metric, "ok", numerator=numbers[0])
    if metric.kind == "ratio":
        return Result(metric, "ok", numerator=numbers[0], denominator=numbers[1])
    return Result(metric, "ok", numerator=numbers[0], sample=numbers[1])


def _plain(number: float | None) -> str:
    if number is None:
        return ""
    return str(int(number)) if float(number).is_integer() else f"{number:.6g}"


def describe(result: Result) -> str:
    """One human-readable cell for a result."""
    if result.status != "ok":
        return result.status
    kind = result.metric.kind
    if kind == "count":
        return f"{int(result.numerator):,}"
    if kind == "ratio":
        if not result.denominator:
            return "none to measure (0 of 0)"
        share = 100 * result.numerator / result.denominator
        return f"{share:.1f} per 100 ({int(result.numerator):,} of {int(result.denominator):,})"
    if result.numerator is None:
        return "none to measure"
    return f"{result.numerator:.1f} (median of {int(result.sample or 0):,})"


def print_table(results: list[Result], out=sys.stdout) -> None:
    width = max(len(r.metric.id) for r in results)
    group = None
    for result in results:
        if result.metric.group != group:
            group = result.metric.group
            print(f"\n[{group}]", file=out)
        print(f"  {result.metric.id.ljust(width)}  {describe(result)}", file=out)


def append_ledger(path: Path, results: list[Result], measured_at: dt.datetime, as_of: dt.datetime, note: str) -> None:
    new = not path.exists() or path.stat().st_size == 0
    with path.open("a", newline="") as handle:
        writer = csv.writer(handle, lineterminator="\n")
        if new:
            writer.writerow(LEDGER_COLUMNS)
        for result in results:
            writer.writerow([
                f"{measured_at:%Y-%m-%dT%H:%M:%SZ}", f"{as_of:%Y-%m-%dT%H:%M:%SZ}", result.metric.id,
                _plain(result.value), _plain(result.numerator), _plain(result.denominator), _plain(result.sample),
                result.status, note,
            ])


def catalogue(metrics: list[Metric]) -> str:
    """The metric list as a Markdown table, for the wiki."""
    rows = ["| Group | Metric | What it measures | Kind | Window |", "| --- | --- | --- | --- | --- |"]
    for m in metrics:
        rows.append(f"| {m.group} | `{m.id}` | {m.definition} | {m.kind} | {m.window} |")
    return "\n".join(rows)


def parse_as_of(text: str | None) -> dt.datetime:
    if not text:
        return dt.datetime.now(dt.timezone.utc).replace(microsecond=0, tzinfo=None)
    return dt.datetime.fromisoformat(text.replace("Z", "")).replace(tzinfo=None)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    commands = parser.add_subparsers(dest="command", required=True)
    run = commands.add_parser("run", help="measure every metric")
    run.add_argument("--mysql", default=os.environ.get("SCORECARD_MYSQL", ""),
                     help="the mysql client command, database included; reads SQL on standard input "
                          "(default: $SCORECARD_MYSQL). Never put a password in it.")
    run.add_argument("--as-of", help="measure as of this UTC time, e.g. 2026-11-17T00:00:00 (default: now)")
    run.add_argument("--backlog-min", type=int, default=5,
                     help="first-hand reports a brand needs to count in parties_with_unsent_evidence")
    run.add_argument("--ledger", type=Path, help="append the results to this CSV file")
    run.add_argument("--note", default="", help="free text stored with each ledger row")
    commands.add_parser("catalogue", help="print the metric list as a Markdown table")
    show = commands.add_parser("script", help="print the SQL that 'run' would execute, for review")
    show.add_argument("--as-of")
    show.add_argument("--backlog-min", type=int, default=5)
    args = parser.parse_args(argv)

    metrics = load_metrics()
    if args.command == "catalogue":
        print(catalogue(metrics))
        return 0
    as_of = parse_as_of(args.as_of)
    if args.command == "script":
        for metric in metrics:
            print(f"-- {metric.id}\n{script_for(metric, as_of, args.backlog_min)}")
        return 0

    client = shlex.split(args.mysql)
    if not client:
        parser.error("give the mysql client command with --mysql or $SCORECARD_MYSQL")
    results = [measure(metric, client, as_of, args.backlog_min) for metric in metrics]
    print(f"CleanApp scorecard as of {as_of:%Y-%m-%d %H:%M} UTC")
    print_table(results)
    if not any(result.status == "ok" for result in results):
        print("\nNothing could be measured. Check the client command and its access.", file=sys.stderr)
        return 2
    if args.ledger:
        measured_at = dt.datetime.now(dt.timezone.utc).replace(microsecond=0, tzinfo=None)
        append_ledger(args.ledger, results, measured_at, as_of, args.note)
        print(f"\nAppended {len(results)} rows to {args.ledger}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
