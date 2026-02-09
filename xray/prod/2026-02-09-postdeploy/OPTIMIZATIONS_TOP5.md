# Top 5 Optimization Pushes (Based on Prod As-Deployed Snapshot 2026-02-09)

Source of truth for this list:
- Snapshot folder: `/Users/anon16/Downloads/cleanapp_back_end_v2/xray/prod/2026-02-09-postdeploy`
- Digest-pinned runtime manifest: `/Users/anon16/Downloads/cleanapp_back_end_v2/platform_blueprint/manifests/prod/2026-02-09.json`

## 1) Network Hardening and Surface Area Reduction (Highest ROI)

Why:
- The VM listens on many `0.0.0.0` ports via docker-proxy (see `ss_listening.txt`).
- GCE firewall currently allows public ingress to `3000`, `8080`, `8090`, `8091` (see `gcloud_firewall_rules_default_network.txt` + `gcloud_instance_info.txt`).
- RabbitMQ defaults are set (`cleanapp/cleanapp`) in `prod_docker-compose.yml`.

Push:
- Remove unnecessary public firewall tags/rules (`allow-3000`, `allow-8090`, `allow-8091`) unless there’s a proven external dependency.
- If `api.cleanapp.io:8080` must exist, terminate TLS on `443` and route internally; otherwise close `8080` and replace with a path behind `443`.
- Bind “behind-nginx” container ports to `127.0.0.1` instead of `0.0.0.0` (compose port mappings like `127.0.0.1:9080:8080`).
- Pin RabbitMQ to a known-good version (stop using `rabbitmq:latest`) and rotate the default creds.

Deliverables:
- A “ports policy” table committed under the platform blueprint (what must be public, what must be VPC-only, what must be localhost-only).
- A compose-only change set (no code) that enforces localhost binds for internal ports.

## 2) Deterministic Deploys: Digest-Pinned Releases by Default

Why:
- Tag-based deploys can drift. You want the deployed runtime to be provably identical to a known build.
- We already have the raw data to do this (`containers_manifest.tsv` and `platform_blueprint/manifests/prod/2026-02-09.json`).

Push:
- Standardize prod deploys on the digest-pinned manifest.
- Make the deploy flow “retag is optional”; the canonical mechanism is “compose pulls exact digests”.

Deliverables:
- A generator that turns a digest manifest into a `docker-compose.override.yml` (or equivalent) that pins `image: repo@sha256:...`.
- A smoke script that:
  - pulls the pinned compose set
  - curls all public `/health` + `/version`
  - writes results back into an `xray/prod/<date>/` folder.

## 3) RabbitMQ Pipeline Reliability (DLQ, Idempotency, Backpressure)

Why:
- The report pipeline is event-driven (`report.raw`, `report.analysed`, `twitter.reply`) with multiple consumers (see `rabbitmq_bindings.tsv`).
- Today, a stuck consumer or poison message risks silently halting a queue.

Push:
- Add DLQs and retry policies (dead-letter exchange, TTL retry queues, max redeliveries).
- Ensure consumers are idempotent (dedupe key based on report seq / report id).
- Add backpressure controls (prefetch/QoS) and explicit “queue lag” monitoring.

Deliverables:
- RabbitMQ policy-as-code (definitions export) checked into repo.
- A minimal “pipeline invariant” doc: what each routing key means, who publishes, who consumes.

## 4) Observability and Debuggability (Requests, Traces, and Correlation IDs)

Why:
- We now have `/version` across key services (`VERSIONS.md`), but debugging still needs cross-service correlation.
- The VM already appears to run collectors (ports `20201/20202` in `ss_listening.txt`), so the foundation may exist.

Push:
- Standardize structured logs (JSON), include:
  - request id / trace id
  - user id / report id (when safe)
  - git sha + build version (from `/version`)
- Export minimal metrics:
  - HTTP latency/error rates
  - Rabbit queue depth + consumer lag
  - DB query error rates

Deliverables:
- A single “debug playbook” that starts with:
  - curl `/version`
  - curl `/health`
  - check queue depths
  - trace a report end-to-end

## 5) Platform Integration Harness (Backend + Frontend + Mobile Contract Tests)

Why:
- Dev environments are currently hard to validate without a connected frontend/mobile.
- The v4 API has an OpenAPI contract (`api_v4_openapi.json`), which is perfect for automated integration tests.

Push:
- Make “prod-like” validation automated:
  - run a minimal stack locally or in CI (compose) for v3 + v4 + mysql + rabbit
  - run contract tests against v4 from OpenAPI
  - add a small set of “golden path” tests (submit report -> analyze -> render -> tag).

Deliverables:
- CI job that boots the minimal stack and runs:
  - `/version` sanity
  - `/health` sanity
  - OpenAPI-driven tests for `/api/v4/*`

