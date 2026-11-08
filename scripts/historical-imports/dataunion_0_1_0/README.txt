CleanApp DataUnion 0.1.0 historical import

Purpose
Import the 5,639 located photo reports recovered from the DataUnion backup while preserving original photos, source timestamps, coordinates and provenance. The 109 unlocated records are excluded. Runtime data processing is restricted to Amazon EC2 Linux. Supplied CouchDB setup/view code is never executed.

The accepted candidate set contains 4,543 cleanapp and 1,096 litterbux records. Candidate JSONL must remain unchanged:
  bytes: 33232097
  SHA-256: ba5ee31e7070da4fdd1ecbb09f1e767e2839d4bb693bd2a1025689fc1bfe2899
Photos are read from MEDIA_ROOT/<sha256>.jpg or .png and verified against every candidate's complete-byte SHA-256. Old media_locator strings are retained as provenance; do not rewrite them when moving the files.

Prerequisites
- Run on the authorized AWS production/import host, with private working directories and no production credentials on the isolated recovery VM.
- The production target is MySQL 8.0.44, database cleanapp, server UUID f2c484f6-3c5d-11ef-aace-0242ac120003. A changed target or schema requires review rather than bypassing the guard.
- Take and verify an appropriate production backup before applying DDL or data.
- Deploy and verify the report_side_effect_policy migration and all email, notification, reward, routing and analysis guards before import. Every imported row must have policy_version=1, source_namespace=dataunion-0.1.0, and all five suppression flags TRUE in the same transaction as its report.
- Apply history_ledger.sql through the reviewed migration path. The importer itself runs no DDL.
- Complete duplicate coverage through an explicit production sequence cutoff. Retain a checksum-pinned dedup_manifest.json with complete_global_hash_coverage=true.
- Record actual deployed revisions/images and live guard test results in the controls attestation. live_guards_verified may become true only after verification, never speculatively. Required fields: server_uuid, policy_version=1, live_guards_verified=true, suppress_email=true, suppress_notifications=true, suppress_rewards=true, suppress_routing=true, suppress_analysis=true.

Credentials and SQL safety
The local MySQL CLI executes inside cleanapp_db; MYSQL_PWD is populated from that container's existing MYSQL_ROOT_PASSWORD. Credentials are not copied into the recovery VM or printed. All data values use UTF-8/binary hex literals, with explicit UTC session settings and bounded lock/query deadlines. No public submission APIs, RabbitMQ publication, account creation, reward writes, fabricated delivery records, or notification calls are used.

Duplicate planning
Run exactly one scanner for a coverage directory. dedup_readonly.py provides an indexed lookup and small probe. dedup_scan.py records all live primary-key headers through a fixed cutoff in a private host-local SQLite cache; production MySQL remains read-only. It includes indexed curation original/current hashes, exact current hashes for uncovered or stale-GDPR references, and report-linked digital-share media. An original-hash match stays a duplicate after redaction; orphan cached media without report mappings is not treated as a live report. Deleted-original matches require review rather than resurrection.

Example scan (AWS host, private WORK_DIR):
python3 dedup_scan.py \
  --candidates WORK_DIR/candidates_5639.jsonl \
  --candidate-sha256 ba5ee31e7070da4fdd1ecbb09f1e767e2839d4bb693bd2a1025689fc1bfe2899 \
  --out-dir WORK_DIR/dedup_scan --budget-seconds 1200

The scanner checkpoints each batch. Resume the same command only after the previous process has exited. Photo-heavy batches stay at or below 100 rows and an average 30MiB/second read budget, with 5-second SQL deadlines and pressure pauses. Larger batches, up to 5,000 IDs, are used only after three measured fast/tiny batches. A chunk may finish with incomplete coverage; never turn that marker true manually.

Plan and apply
python3 apply_import.py \
  --candidates WORK_DIR/candidates_5639.jsonl \
  --candidate-sha256 ba5ee31e7070da4fdd1ecbb09f1e767e2839d4bb693bd2a1025689fc1bfe2899 \
  --media-root MEDIA_ROOT \
  --dedup-manifest WORK_DIR/dedup_scan/dedup_manifest.json \
  --dedup-manifest-sha256 VERIFIED_MANIFEST_SHA256 \
  --approved-controls VERIFIED_CONTROLS_JSON \
  --mode plan --out-dir WORK_DIR/plan

Review the plan before running the same verified arguments with --mode apply and a private receipt directory. Defaults are five reports and 16MiB of photo bytes per atomic batch. Partial failure preserves committed audit rows; exact replays skip them. A changed payload under an existing source identity, missing mapping, incomplete coverage or unverified guard fails closed.

Each new report, source-index/raw provenance row, factual presentation row, suppression policy and history ledger entry commit together. Existing duplicate targets are left unchanged; only a private ledger mapping is added. Deterministic public IDs and unique namespace/source keys prevent replay duplication. Short insertion-tail locks close concurrent insertion races. The fixed pre-import cutoff is rechecked on every batch, so a transaction that reserved a lower ID and commits after a larger imported ID is still detected. Previously observed non-cohort IDs are not repeatedly rehashed, and imported cohort images are excluded from repeated tail reads.

Time, location and presentation semantics
reports.ts is TIMESTAMP(0), so its projection uses original UTC upload seconds explicitly. The ledger retains original DATETIME(6), exact raw Unix decimal, full DOUBLE coordinates and complete original/normalized JSON. Native FLOAT coordinates retain the current schema's precision. Capture timestamps without an evidenced timezone remain distinct local EXIF values; they are not silently treated as UTC. Legacy uploader IDs are namespaced/pseudonymized and are not associated with current accounts.

Replay compares stored JSON with MySQL-native equality against CAST(exact canonical source JSON AS JSON). This preserves MySQL's JSON number representation without introducing a numeric tolerance. MySQL JSON DOUBLE serialization can differ from a Python round-trip by one ULP; Python float equality is therefore not the replay contract. Changed strings, object keys, array order and meaningful coordinate values still fail closed. The unchanged, SHA-bound candidate JSONL and recovery archive remain the authoritative original source bytes; native JSON equality does not replace those artifacts.

The presentation source is dataunion-0.1.0-recovery, title Historical CleanApp report, and description contains only the original UTC submission date. Physical classification follows the source product. is_valid denotes a valid recovered record, not verified incident truth. Severity0 is explicitly unassessed (projection_method=metadata_only, severity_assessed=false). No AI/human descriptions, contacts, brands or resolution states are invented. inferred_contact_emails is the canonical empty list [] so the existing contact-enrichment fallback does not select these intentionally quiet historical rows.

Verification and rollback
Verify all candidate/source IDs, photos, UTC timestamps, native/ledger coordinates, policy flags and presentation counts after apply, then confirm zero cohort-related email/notification/reward/routing side effects. Keep the committed batch receipts and database/run checksums privately.

python3 rollback_import.py --run-id VERIFIED_RUN_ID --out PRIVATE_ROLLBACK_RECEIPT

Rollback deletes only disposition=inserted rows for that run. Existing duplicate targets are never deleted. It refuses changed photo/source/public-ID/policy/native reporter/time/location, new ownership or user references, independent analysis, or external-send/reward/case references. Precise original source data and recorded report sequence remain in the audit ledger. A rolled-back/deleted source mapping is not silently resurrected by a later replay.

Integration tests
integration_test.py uses generated tiny PNG fixtures only, on a disposable AWS builder with container ca-import-test-mysql and separate database cleanapp_dataunion_import_test. It recreates that test database; never redirect it to production. Trusted sources can be staged in /srv/cleanapp-build/work/importer and run with python3 integration_test.py. Coverage includes BLOB/location/time/precision/policy parity, native JSON replay with a reproduced one-ULP DOUBLE transport difference, changed strings/keys/array order/GPS refusal, existing duplicate preservation, narrow rollback, edited/claimed-row rollback refusal, stale-tail abort, late lower-ID commit handling, and atomic rollback when policy insertion fails.
