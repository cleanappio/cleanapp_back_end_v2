# Human report processing: October 7, 2026 repair

## Evidence and failure chain

Production is `cleanapp-prod`, project `cleanup-mysql-v2`, zone `us-central1-a`, SSH `deployer@34.122.15.16`. The human worker was running with Gemini `gemini-flash-latest`, prefetch 1, and no translation override (English only). Its image digest was `sha256:0c994a93da15daabea8546769544423a77d8384996ef70dfed4006a3e2df01cd`; `/version` returned `dev` without a commit or build timestamp, so the historical image's exact commit is unverifiable from its embedded metadata.

At 08:52:52 UTC, the human worker began report 1299589. It failed after 90.46 seconds with a Gemini 404 from the fallback endpoint. At 08:54:23.204801, the RabbitMQ broker logged `basic.publish ... not_found: no exchange cleanapp-retry.report-analysis-human-queue`. AMQP publish and ack returned locally before that asynchronous broker exception arrived. The broker closed only the channel and retained/requeued the delivery. The subscriber checked only whether its connection was closed, then repeatedly attempted QoS against the closed channel.

At approximately 11:46 UTC, the human queue had 42 ready messages, zero unacknowledged messages and zero consumers; the bulk queue had zero ready/unacknowledged messages and one consumer. The human retry/DLQ infrastructure was absent. The old watchdog silently ignored failing checks because its checked brace group disabled Bash errexit, then forwarded one old report to bulk every five minutes. This masked the failed human lane and caused growing receipt-to-analysis delays.

The 11:48:30 UTC Brussels baseline was 59 reports, 50 English analyses/publications and 9 pending, with no invalid or duplicate English analyses. All belonged to the same actor as the incident's report 1299652 (identity omitted). Submission and raw receipt differed by 0–1 seconds; database/server session time was UTC with no reversed timestamps. English creation to publication was 0–1 seconds at database precision. In 26 completed human-worker log traces, hydration median was 20.9 ms (max 493 ms), image-to-English median 10.65 seconds (max 14.54 seconds), and English-to-publication median 3.34 ms (max 26.69 ms). Translation, hydration, database latency and sustained provider throttling were not evidenced causes of the accumulating backlog. The initiating Gemini request's first-endpoint failure was not logged separately; its exact network/provider cause cannot be reconstructed.

## Repair

- Add durable human retry exchange/queue (30-second delay) and DLQ/policy/binding. Preserve all existing queue messages and raw records.
- Replace a disconnected subscriber channel after delivery/QoS/bind/consume failure even when its connection remains open. Stop the binding loop cleanly on failure. Never publish an old delivery's retry through a replacement channel.
- Hold a MySQL named lock on a pinned connection per report while analyzing and publishing; acknowledge previously published replays before model/image work. This prevents duplicate processing of messages already recovered by the watchdog. Preserve shadow visibility.
- Log Gemini endpoint version, model, duration and status without report content. Send the key in the `x-goog-api-key` header, which [Google documents](https://ai.google.dev/gemini-api/docs/api-key), to keep it out of URL-bearing transport errors. Preserve model, 90-second timeout and fallback behavior.
- Map analyzer source releases to both bulk and human compose services. Generate pins from effective Compose JSON (including inherited images), require selected digests, and retain unrelated existing pins.
- Check human queue consumer/connectivity and successful progress, fail after 300 seconds without successful completions or 600 seconds of continuous backlog, and include the human analyzer in Prometheus/public status. Capture watchdog child failures explicitly; do not run recovery after a failed prerequisite or forward retained human reports through bulk.

## Deployment and rollback

Use the canonical exact-source build/pin helper:

```sh
HOST=deployer@34.122.15.16 SOURCE_SERVICES=report-analyze-pipeline \
RUN_GO_MIGRATIONS=0 KEEP_REMOTE_SOURCE=1 \
./platform_blueprint/deploy/prod/vm/source_build_and_deploy.sh
```

This repair introduces no schema migration. Keep production compose/env configuration intact. Install only the changed watchdog files and Prometheus config/rules; preserve other operational scripts, cron, secrets and notification destinations.

A mode-700 rollback snapshot is stored on the VM at `/home/deployer/incidents/20261007-human-processing/`, containing compose files, the previous digest override and watchdog/observability files. For an analyzer rollback, use a separate override explicitly pinning **both** analyzers to the old digest above with `docker compose ... up -d --no-deps cleanapp_report_analyze_pipeline cleanapp_report_analyze_human`. The old digest override may omit the human worker, so it alone is insufficient. Restore changed operational files from that snapshot if necessary and reload validated Prometheus config. Retain added queue infrastructure/messages; do not delete or purge it.

## Production rollout and verification

The canonical source build deployed analyzer commit `0dd2846cccbf4b163bfd4099234ada3b805986fb` to both workers at 12:02:31 UTC. Cloud Build was `3e7727b7-3d04-4524-991e-7e26fc35047d`; version is `1.0.2026100701`, build time 11:58:25 UTC, and the shared registry digest is `sha256:ef9f074ba9b764614b65c24e4c3de107a51dc1194cde680177d341b5bdede8f9`. The current digest override is `docker-compose.digests.2026-10-07T120227Z.yml`; both workers are explicitly pinned and unrelated pins are retained. Models, human prefetch/concurrency 1, bulk concurrency 5, and translation configuration were preserved.

Only the changed watchdog files and Prometheus config/rules were installed. Production RabbitMQ management statistics are disabled, so monitoring commit `4f2557ac86256e14fc7ba3977d4aa74532b334aa` reads bounded, read-only `rabbitmqctl list_queues` counts instead of assuming management API statistics exist. Prometheus validation/reload succeeded and both analyzer targets are healthy.

By 12:04:00 UTC all 59 Brussels reports had English analysis and publication, reducing the 11:48:30 baseline backlog from 9 to 0. The 12:14:45 UTC database check still showed zero pending, unpublished, invalid or duplicate English analyses. Both analyzer queues had 0 ready, 0 unacknowledged and 1 consumer at 12:15:09 UTC; retry and dead-letter queues were empty. Existing completed Brussels timestamps were unchanged.

The repaired human worker acknowledged 48 retained/new deliveries: 34 previously published replays were skipped and 14 reports completed analysis/publication without errors or duplicate publications. Hydration median/max were 13/44 ms; image-to-English analysis was 9.16–16.69 seconds (median 11.88); English-to-publication was 1.23–9.37 ms. Five naturally arriving public bulk reports (1299733–1299736 and 1299738) completed receipt-to-publication in 9–18 seconds, and the sixth bulk report (1299737) retained shadow visibility with analysis only. These queue assignments are established by worker logs, not inferred from report source type. No fresh post-repair Boris submission was required; his existing reports demonstrate backlog completion but do not measure fresh receipt-to-publication latency.

Raw report fingerprints before/after matched for a fixed 787-record cohort containing 12,571,156 image bytes, including all 59 Brussels reports. Brussels notification ledger counts remained zero with no duplicate recipient groups; no reports/images/messages were deleted or purged and no public synthetic report was submitted.

The rollback snapshot includes a syntax-checked `rollback-analyzers.sh` and `analyzers-before.yml` explicitly pinning both workers to their prior digest. Keep the added retry/DLQ infrastructure even if reverting application code.

## Validation and remaining limitations

Regression checks cover a live isolated broker channel exception/reconnect, missing retry exchange redelivery preservation, no old-delivery retry duplicates after reconnect, isolated MySQL serialization/publication replay, provider diagnostics/fallback, selected digest pins, watchdog failure propagation and human queue stall alerts. Tests use disposable services and do not submit public synthetic reports.

AMQP publication and the database publication marker still have the pre-existing crash window: this repair does not implement a transactional outbox or promise exactly-once delivery across a crash between those operations. Production Alertmanager's existing destination is a no-op endpoint and watchdog has no configured webhook; alerts/status can be detected locally but external notification requires a destination.

## Existing bulk intake encoding failure

The corrected watchdog exposed a separate pre-existing Bluesky intake failure: the submitter received `REPORT_INSERT_FAILED` HTTP 503 responses before analyzer rollout. The existing analysis-derived description component was valid UTF-8 at 260 bytes/258 characters, but `clampStr(..., 255)` cut through a character. The submitter also appends a source URL; this does not change the invalid prefix. The `reports.description` column is `VARCHAR(255)` with utf8mb4; a session-local temporary table matching that column reproduced MySQL error 1366/HY000 with the invalid projection. The boundary-safe 254-byte projection inserted successfully and rollback left zero rows. This diagnostic touched no real reports, images or AMQP messages.

The repair keeps the existing byte budget and backs up to a complete UTF-8 character boundary; it preserves emoji and the full Wire report/idempotency material. It does not strip characters or alter the database schema. Regression checks cover multilingual cutoffs, ASCII behavior and full-source material beyond the projection limit.

The exact live listener source was recovered from Cloud Build `add49dc3-e009-46e1-a80f-f387a2ef7922` (September 10, 16:54:28 UTC), which produced the live digest `sha256:46c0f2ff60c579e4d8a6e24d676dfd67b9b400e95df406ea45348a6f87de545c`. Its intake code matches the main-branch baseline, but five existing production sorting files differ. Commit `4d4e6e2` records those files verbatim so rebuilding preserves current production behavior. The separate `listener-before.yml` and syntax-checked `rollback-listener.sh` in the private incident directory preserve the old listener digest without reverting the analyzer repair.

The listener-only canonical build/pin rollout deployed commit `4bfa9c6091746302166def0e9082ffc567ae95c5` at 12:25:48 UTC, with version `1.0.2026100701` and build time 12:23:05 UTC. Cloud Build `922ff50e-1cbf-46f4-8241-972a93181415` produced digest `sha256:71047ff823f07be12b497b9eefc22daee65bcbe44ec9137bed3bd4bd3f7a08a0`. Current pins are `docker-compose.digests.2026-10-07T122544Z.yml`; the two analyzer pins remain unchanged. All 101 recovered source/shared files were compared before build: the UTF-8 helper is the sole runtime source difference, with its regression tests added separately. Full listener race tests and vet passed after production source preservation.

At 12:30:10 UTC the submitter's natural retry succeeded: 100 submitted/accepted, 99 recognized existing reports, zero rejected, and no HTTP failures after rollout. One new report completed bulk analysis/publication in 12 seconds, with a 12.286-second handler duration. The 99 duplicates are idempotent acknowledgements of previously stored batch items, not additional report/publication records. Both analyzer queues were empty with one consumer each at the 12:30:58 check, and all three Prometheus targets/eight rules were healthy. The existing submitter database selector still takes approximately 3–4 minutes before building a batch; no submitter restart was used to force verification.

The exact previously failing input was received at 12:30:09 UTC and analyzed/published at 12:30:21 UTC. Its full Wire description remains 339 bytes (including the source URL), while the legacy projection is 254 valid UTF-8 bytes. The 12:30 watchdog run finished honestly `OK`, with its previous failure cleared. The fixed 787-record raw cohort remained unchanged after both deployments; the final Brussels notification check still found zero delivery rows or duplicate recipient groups.
