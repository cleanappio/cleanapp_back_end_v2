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

## Checks and limits

Regression checks cover a live isolated broker channel exception/reconnect, missing retry exchange redelivery preservation, no old-delivery retry duplicates after reconnect, isolated MySQL serialization/publication replay, provider diagnostics/fallback, selected digest pins, watchdog failure propagation and human queue stall alerts. Tests use disposable services and do not submit public synthetic reports.

AMQP publication and the database publication marker still have the pre-existing crash window: this repair does not implement a transactional outbox or promise exactly-once delivery across a crash between those operations. Production Alertmanager's existing destination is a no-op endpoint and watchdog has no configured webhook; alerts/status can be detected locally but external notification requires a destination.
