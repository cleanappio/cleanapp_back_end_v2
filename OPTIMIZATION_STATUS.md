# CleanApp Optimization Status (Rolling)

This is the rolling task/status tracker for the “big upgrade push” workstream.

Source of truth inputs:
- Latest prod xray snapshot: `/Users/anon16/Downloads/cleanapp_back_end_v2/xray/prod/2026-02-09-postdlq2/`
- Latest prod digest manifest: `/Users/anon16/Downloads/cleanapp_back_end_v2/platform_blueprint/manifests/prod/2026-02-09-postdlq2.json`

## 1) Network Hardening + Surface Area Reduction

Status: **In progress**

Done:
- Most internal service ports on prod are now bound to `127.0.0.1` (reduces exposure even if firewall rules are permissive).
- Prod host ports `3000` (cleanapp_web) and `8090` (cleanapp_pipelines) are now bound to `127.0.0.1` (external access removed even if firewall rules still allow them).
- Pinned RabbitMQ image in prod compose (stopped relying on `rabbitmq:latest`).

Next:
- Rotate/replace AMQP creds (stop relying on defaults).
- Reduce GCE firewall tags/rules to only what must be public.

Evidence:
- `xray/prod/2026-02-09-postdlq2/ss_listening.txt`
- `xray/prod/2026-02-09-postdlq2/gcloud_firewall_rules_relevant.txt`

## 2) Deterministic Deploys (Digest-Pinned By Default)

Status: **In progress**

Done:
- Captured redacted prod deploy config into the blueprint:
  - `platform_blueprint/deploy/prod/docker-compose.yml`
  - `platform_blueprint/deploy/prod/nginx_conf_d/`
- Captured and committed digest pins from prod:
  - `platform_blueprint/manifests/prod/2026-02-09-postdlq2.json`
- Generated and committed a digest-pinned compose overlay:
  - `platform_blueprint/deploy/prod/digests/2026-02-09-postdlq2.digests.yml`

Next:
- Decide whether we want the manifest to cover *only running containers* or *all compose services* (including stopped ones), and adjust xray capture accordingly.

## 3) RabbitMQ Pipeline Reliability (Backpressure, Ack Semantics, DLQs)

Status: **Mostly complete (core safety)**

Done:
- Bounded concurrency + correct ack/nack semantics for key Rust consumers (no per-message goroutine spawning, ack only after success).
- DLQs enabled on prod (DLX `cleanapp-dlx` + `<queue>.dlq` + policies) for:
  - `report-tags-queue`
  - `report-renderer-queue`
  - `twitter-reply-queue`
  - `report-analysis-queue` (policy + DLQ queue present for future)

Next:
- Add retry queues / max redelivery policy (so transient errors don’t spin forever).
- Add a DLQ replay/runbook (how to inspect + requeue after fixes).

Evidence:
- `xray/prod/2026-02-09-postdlq2/rabbitmq_policies.txt`
- `xray/prod/2026-02-09-postdlq2/rabbitmq_queues.tsv`

## 4) Observability + Debuggability (Correlation IDs, Metrics)

Status: **Early**

Done:
- `/version` endpoints broadly deployed; xray captures include provenance.

Next:
- Standardize structured logs + correlation id propagation.
- Minimal metrics for queue depth/lag + consumer health.

## 5) Platform Integration Harness (Contracts + Smoke + Golden Paths)

Status: **In progress**

Done:
- Public smoke checks exist (nginx endpoints):
  - `platform_blueprint/tests/smoke/smoke_prod.sh`
  - `platform_blueprint/tests/smoke/capture_prod_public.sh`
- v4 OpenAPI contract snapshot is stored:
  - `platform_blueprint/contracts/openapi/api_v4_openapi.json`
- Prod VM-local smoke checks exist (localhost ports + RabbitMQ invariants):
  - `platform_blueprint/tests/smoke/smoke_prod_vm.sh`
- v4 contract checks (quick) are exercised in the public smoke:
  - `platform_blueprint/tests/smoke/smoke_prod.sh`

Next:
- Optionally make the contract smoke OpenAPI-driven (validate endpoint coverage/schema drift).
