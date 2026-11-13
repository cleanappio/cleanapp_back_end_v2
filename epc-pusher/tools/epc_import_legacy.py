#!/usr/bin/env python3
"""Validate recovered EPC history and generate a guarded, idempotent MySQL import.

This program never connects to a database or sends notifications. Execute its SQL
with the EPC worker stopped, using a MySQL batch client without --force. Any
validation error aborts the connection and rolls back the target-table transaction.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import re
from pathlib import Path

FILES = {
    "epc_campaigns": "epc_campaigns-decoded.json",
    "epc_contracts": "epc_contracts-decoded.json",
    "epc_sent_message_templates_read_only": "templates-decoded.json",
    "epc_outbox": "epc_outbox-decoded.json",
}
NAMESPACE = "legacy-dev"


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def positive(value: object, name: str) -> int:
    require(type(value) is int and value > 0, f"Invalid {name}")
    return value


def timestamp(value: object) -> str:
    require(isinstance(value, str), "Missing UTC timestamp")
    require(bool(re.fullmatch(r"\d{4}-\d\d-\d\d \d\d:\d\d:\d\d", value)), "Invalid timestamp")
    dt.datetime.strptime(value, "%Y-%m-%d %H:%M:%S")
    return value


def load_history(directory: Path) -> tuple[dict, dict]:
    history, provenance = {}, {}
    for table, filename in FILES.items():
        decoded = json.loads((directory / filename).read_text())
        require(decoded["table"] == table, f"Unexpected table in {filename}")
        require(decoded.get("timestamp_timezone") == "UTC", f"Non-UTC timestamps in {filename}")
        require(decoded["row_count"] == len(decoded["rows"]), f"Row count mismatch in {filename}")
        require(all(p["checksum_ok"] for p in decoded["pages"]), f"Corrupt page in {filename}")
        require(sum(p["header_rows"] for p in decoded["pages"]) == decoded["row_count"], f"Page row counts disagree in {filename}")
        source = Path(decoded["source_file"])
        if not source.exists():
            source = directory / source.name
        require(source.is_file(), f"Missing original .ibd source for {filename}")
        require(sha256(source) == decoded["source_sha256"], f"Source hash mismatch in {filename}")
        require(all(not r.get("physical", {}).get("deleted", False) for r in decoded["rows"]), f"Deleted record in {filename}")
        rows = [{k: v for k, v in r["values"].items() if k not in {"DB_TRX_ID", "DB_ROLL_PTR"}} for r in decoded["rows"]]
        history[table] = rows
        provenance[table] = {"source_sha256": decoded["source_sha256"], "decoded_sha256": sha256(directory / filename), "rows": len(rows)}
    return history, provenance


def prepare(history: dict, provenance: dict, snapshot: str, mappings: list[dict] | None = None) -> tuple[dict, list[dict]]:
    require(bool(re.fullmatch(r"[0-9a-f]{64}", snapshot)), "Expected full restic snapshot ID")
    history = {table: [dict(row) for row in history[table]] for table in FILES}
    lookups = {}
    for table, rows in history.items():
        key = "slug" if table == "epc_campaigns" else "id"
        ids = [row[key] for row in rows]
        require(len(ids) == len(set(ids)), f"Duplicate primary key in {table}")
        for row in rows:
            timestamp(row["created"])
            if key == "id":
                positive(row[key], table + ".id")
        lookups[table] = {row[key]: row for row in rows}
    contracts = lookups["epc_contracts"]
    require(len({r["key"] for r in contracts.values()}) == len(contracts), "Duplicate EPC key")
    require(len({r["address"].lower() for r in contracts.values()}) == len(contracts), "Duplicate EPC address")
    for row in contracts.values():
        require(row["key"].startswith("cleanapp/brand/") and len(row["key"]) <= 255, "Invalid EPC brand key")
        require(bool(re.fullmatch(r"0x[0-9a-fA-F]{40}", row["address"])), "Invalid EPC address")

    mapping_by_seq = {}
    for mapping in mappings or []:
        old = positive(mapping["legacy_report_seq"], "legacy report seq")
        positive(mapping["production_report_seq"], "production report seq")
        require(old not in mapping_by_seq, "Ambiguous canonical mapping for legacy report")
        require(isinstance(mapping["source"], str) and mapping["source"], "Missing canonical source")
        require(isinstance(mapping["external_id"], str) and mapping["external_id"], "Missing canonical external ID")
        mapping_by_seq[old] = mapping

    seen_seq, seen_message, reserved_prod, used_mappings = set(), set(), set(), []
    for row in sorted(history["epc_outbox"], key=lambda r: r["id"]):
        require(row["campaign"] in lookups["epc_campaigns"], "Missing campaign foreign key")
        require(row["contract_id"] in contracts, "Missing contract foreign key")
        require(row["sent_message_template_id"] in lookups["epc_sent_message_templates_read_only"], "Missing template foreign key")
        require(row["status"] == "sent", "Legacy import accepts verified sent records only")
        metadata = dict(row["metadata"])
        seq = positive(metadata.get("report_seq"), "metadata.report_seq")
        message_id = metadata.get("blockscanMessageId")
        require(isinstance(message_id, str) and bool(re.fullmatch(r"\d{1,128}", message_id)), "Missing Blockscan receipt")
        require(seq not in seen_seq, "Duplicate legacy report event")
        require(message_id not in seen_message, "Duplicate Blockscan receipt")
        seen_seq.add(seq); seen_message.add(message_id)
        body = lookups["epc_sent_message_templates_read_only"][row["sent_message_template_id"]]["body"]
        marker = "{{ report.brand_name }}"
        require(marker in body and "{{" not in body.replace(marker, ""), "Unsupported historical template")
        row["message_body"] = body.replace(marker, contracts[row["contract_id"]]["key"].removeprefix("cleanapp/brand/"))
        row["source_namespace"] = NAMESPACE
        row["source_report_seq"] = seq
        row["blockscan_message_id"] = message_id
        row["attempt_count"] = 0  # Historical attempt count is unknown.
        row["sent_at"] = row["created"]
        row["event_key"] = f"{NAMESPACE}:report:{seq}"
        source_provenance = {"snapshot_id": snapshot, "source_table_sha256": provenance["epc_outbox"]["source_sha256"], "source_outbox_id": row["id"],
            "message_body_reconstructed": True,
            "message_body_brand_source": "trimmed_epc_contract_key",
            "message_body_reconstruction": "Stored template rendered with the trimmed brand in epc_contracts.key; original provider message bytes were not captured."}
        if seq in mapping_by_seq:
            mapping = mapping_by_seq[seq]
            prod = mapping["production_report_seq"]
            source_provenance["canonical_source"] = {k: mapping[k] for k in ("source", "external_id", "production_report_seq")}
            used_mappings.append(mapping)
            if prod not in reserved_prod:
                row["event_key"] = f"prod:report:{prod}"
                reserved_prod.add(prod)
        require("_legacy_import" not in metadata, "Unexpected existing import provenance")
        metadata["_legacy_import"] = source_provenance
        row["metadata"] = metadata
    require(set(mapping_by_seq) <= seen_seq, "Canonical mapping references an absent legacy receipt")
    return history, used_mappings


def literal(value: object) -> str:
    if value is None:
        return "NULL"
    if type(value) is int:
        return str(value)
    if isinstance(value, dict):
        value = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    require(isinstance(value, str), "Unsupported SQL value")
    return "CONVERT(0x" + value.encode("utf-8").hex() + " USING utf8mb4)"


COLUMNS = {
    "epc_campaigns": ["slug", "description", "created"],
    "epc_contracts": ["id", "key", "address", "created"],
    "epc_sent_message_templates_read_only": ["id", "body", "created"],
    "epc_outbox": ["id", "campaign", "contract_id", "metadata", "status", "sent_message_template_id", "created", "event_key", "source_namespace", "source_report_seq", "message_body", "blockscan_message_id", "attempt_count", "sent_at"],
}


def generate_sql(history: dict, provenance: dict, mappings: list[dict]) -> str:
    lines = ["-- Generated by epc_import_legacy.py. Requires the EPC production migration.",
             "-- Run with worker stopped and a batch client WITHOUT --force.",
             "-- Source manifest: " + json.dumps(provenance, sort_keys=True),
             "SET NAMES utf8mb4;", "SET time_zone='+00:00';",
             "SET SESSION sql_mode='STRICT_ALL_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO';",
             "DROP TEMPORARY TABLE IF EXISTS _epc_import_assert,_epc_identity," + ",".join("_import_"+table for table in FILES) + ";",
             "CREATE TEMPORARY TABLE _epc_import_assert (ok INT NOT NULL CHECK(ok=1));"]
    for table in FILES:
        stage = "_import_" + table
        columns = COLUMNS[table]
        names = ",".join("`" + c + "`" for c in columns)
        lines.append(f"CREATE TEMPORARY TABLE `{stage}` AS SELECT {names} FROM `{table}` WHERE 0;")
        rows = history[table]
        for start in range(0, len(rows), 250):
            values = ["(" + ",".join(literal(row.get(c)) for c in columns) + ")" for row in rows[start:start+250]]
            lines.append(f"INSERT INTO `{stage}` ({names}) VALUES\n" + ",\n".join(values) + ";")
    if mappings:
        lines.append("CREATE TEMPORARY TABLE _epc_identity (legacy_seq BIGINT UNSIGNED PRIMARY KEY, prod_seq BIGINT UNSIGNED NOT NULL, source VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL, external_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL);")
        for start in range(0, len(mappings), 250):
            values = ["(" + ",".join(literal(r[k]) for k in ("legacy_report_seq", "production_report_seq", "source", "external_id")) + ")" for r in mappings[start:start+250]]
            lines.append("INSERT INTO _epc_identity VALUES\n" + ",\n".join(values) + ";")
    lines.append("START TRANSACTION;")
    if mappings:
        lines.extend(["-- Revalidate canonical identity against current production; bare seq equality is never used.",
          "INSERT INTO _epc_import_assert SELECT IF(NOT EXISTS(SELECT 1 FROM _epc_identity s WHERE NOT EXISTS(SELECT 1 FROM external_ingest_index t WHERE t.source=s.source AND t.external_id=s.external_id AND BINARY t.source=BINARY s.source AND BINARY t.external_id=BINARY s.external_id AND t.seq=s.prod_seq)),1,0);"])
    for table in FILES:
        stage = "_import_" + table
        columns = COLUMNS[table]
        key = "slug" if table == "epc_campaigns" else "id"
        collision_keys = [key]
        if table == "epc_contracts":
            collision_keys += ["key", "address"]
        if table == "epc_outbox":
            collision_keys += ["event_key", "blockscan_message_id"]
        equal = " AND ".join(f"(BINARY t.`{c}` <=> BINARY s.`{c}`)" if c not in {"metadata", "id", "contract_id", "sent_message_template_id", "source_report_seq", "attempt_count"} else f"(t.`{c}` <=> s.`{c}`)" for c in columns)
        names = ",".join("`" + c + "`" for c in columns)
        selected = ",".join("s.`" + c + "`" for c in columns)
        lines.append(f"-- {table}: abort on any ID/natural-key/receipt collision with different content.")
        for collision_key in collision_keys:
            lines.append(f"INSERT INTO _epc_import_assert SELECT IF(NOT EXISTS(SELECT 1 FROM `{stage}` s JOIN `{table}` t ON t.`{collision_key}`=s.`{collision_key}` WHERE NOT({equal})),1,0);")
        lines.extend([
          f"INSERT INTO `{table}` ({names}) SELECT {selected} FROM `{stage}` s LEFT JOIN `{table}` t ON t.`{key}`=s.`{key}` WHERE t.`{key}` IS NULL;",
          f"INSERT INTO _epc_import_assert SELECT IF((SELECT COUNT(*) FROM `{stage}` s JOIN `{table}` t ON t.`{key}`=s.`{key}` WHERE {equal})={len(history[table])},1,0);"])
    lines.extend(["COMMIT;", "SELECT JSON_OBJECT('legacy_import_verified',TRUE,'outbox_rows'," + str(len(history["epc_outbox"])) + ",'canonical_mappings'," + str(len(mappings)) + ",'production_cursor_modified',FALSE);", ""])
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-dir", type=Path, required=True)
    parser.add_argument("--snapshot-id", required=True)
    parser.add_argument("--identity-map", type=Path)
    parser.add_argument("--output-sql", type=Path, required=True)
    args = parser.parse_args()
    history, provenance = load_history(args.input_dir)
    mappings = json.loads(args.identity_map.read_text()) if args.identity_map else []
    prepared, used = prepare(history, provenance, args.snapshot_id, mappings)
    sql = generate_sql(prepared, provenance, used)
    args.output_sql.parent.mkdir(parents=True, exist_ok=True)
    args.output_sql.write_text(sql)
    args.output_sql.chmod(0o600)
    print(json.dumps({"output_sql": str(args.output_sql), "sha256": sha256(args.output_sql), "counts": {t: len(r) for t, r in prepared.items()}, "canonical_mappings": len(used), "prod_event_reservations": sum(r["event_key"].startswith("prod:") for r in prepared["epc_outbox"])}))


if __name__ == "__main__":
    main()
