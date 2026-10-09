# Codex Scratchpad (CleanApp)

This file is a persistent, repo-local scratchpad to compound improvements over time.

Rules:
- No secrets, ever.
- Keep entries factual and actionable.
- At session start: read this file before making changes.
- At session end: append a new entry (what I got wrong, what you corrected, what worked, what didn't).

## What You Like (Stable Preferences)

- Be decisive and keep momentum (minimize questions).
- Xray the *as-deployed* system before upgrades/refactors.
- Turn “tribal knowledge” into durable artifacts (runbooks, manifests, tests) committed to git.
- Security-first posture: never commit secrets; avoid copying VM scripts that might contain creds; prefer Secret Manager and env vars.
- Deterministic deploys: one control plane (docker compose) and auditable versions (digests).

## Operating Checklist (Per Session)

- Read latest xray reports and manifests before changing anything.
- Confirm what is deployed using image digests (not tags).
- Make changes in small, reversible cuts; keep rollback options.
- Update docs/manifests in the same PR as the change when possible.
- Run smoke checks (localhost health + externally routed endpoints where relevant).

## Session Log

### 2026-02-07 (Prod+Dev Xray, Control Plane, Baseline Lock)

Got wrong:
- The local sandbox can block `ssh` from scripts unless run with escalation.
- Dev host did not have `nginx` available for non-root PATH checks.

Corrected by:
- Allow escalation for xray capture when needed.
- Use `sudo -n nginx -v || true` in capture scripts.

What worked:
- Moving prod to a single compose-managed control plane reduced ambiguity.
- Capturing secrets-safe xray snapshots (configs + env keys, not values) created an auditable baseline.

What didn't:
- `setup/setup.sh` generated VM scripts that could embed secrets due to unescaped command substitutions.

User preferences learned:
- "CTO mode": act without asking; you handle on-the-ground testing.

Next time:
- Always run secret scanning before push/merge.
- Prefer explicit image-digest manifests to make redeploys deterministic.

### 2026-02-07 (Provenance + /version Endpoints)

Got wrong:
- Started with a `buildinfo.env` file name; this repo ignores `*.env`, which likely means `gcloud builds submit` would also exclude it from the build context.

Corrected by:
- Switched to `buildinfo.vars` and updated Dockerfiles + build scripts to consume it; added `trap` cleanup so the file doesn’t linger locally.

What worked:
- Standard `/version` endpoints (and version-in-health where helpful) across deployed Go + Rust services.
- Embedded build metadata into binaries (Go via `-ldflags -X`, Rust via `option_env!`).

What didn't:
- No local `go`/`cargo` toolchains available here, so this needs validation via a dev deploy + endpoint smoke checks.

Next time:
- Validate build-context ignore rules (gitignore vs cloud build upload) before choosing file names/extensions.
- Add a post-deploy smoke script to curl all public `/version` endpoints and save the results to `xray/`.

### 2026-02-09 (Prod Rollout + Provenance Capture)

Got wrong:
- Shipped a small Go syntax bug in `voice-assistant-service/version` (missing `range` in a `for` loop) that broke Cloud Build.
- `report-fast-renderer` builds were non-reproducible because `.dockerignore` excluded `Cargo.lock` and the Dockerfile used `cargo install` without `--locked`, which allowed dependency/MSRV drift.
- First `/version` capture pass wrote `.err` artifacts that were noise once services stabilized; cleaned up and re-captured.

Corrected by:
- Fixing the `for _, s := range info.Settings` loop and rebuilding the image.
- Keeping `Cargo.lock` in the Docker build context and using `cargo install --locked`.
- Re-running the `/version` capture after services were fully started and committing only the clean results.

What worked:
- Promoting the already-built version tags to `:prod` in Artifact Registry, then `docker compose pull && docker compose up -d` on prod to roll out safely.
- Capturing `/version` responses into `xray/prod/2026-02-09/version/` and generating `xray/prod/2026-02-09/VERSIONS.md` for a commit-level provenance map.

Next time:
- Always ensure build lockfiles are included and enforced (`--locked`) for deterministic Rust builds.
- When collecting endpoint snapshots during restarts, wait for health checks to settle before recording final artifacts.

### 2026-02-09 (Digest-Pinned Deploy Tooling)

Got wrong:
- Assumed `PyYAML` might exist for compose parsing; it isn’t installed here.

Corrected by:
- Writing a small indentation-based parser for our current compose style (services + container_name/image fields only).

What worked:
- Generating a third compose overlay that replaces tags with `image@sha256:...` pins from `platform_blueprint/manifests/*`, enabling deterministic `docker compose ... pull/up`.
- Expanding the blueprint prod smoke checks to include public `/version` endpoints, plus a capture script that records responses into `xray/`.

Next time:
- If we want strict pinning for *all* services (including currently-exited ones), generate the manifest from `docker ps -a` or from `docker compose config` rather than only running containers.

### 2026-02-09 (Prod Network Hardening: Localhost Port Binds)

Got wrong:
- Changed `cleanapp_service`'s port mapping to `127.0.0.1:8079:8080` in `docker-compose.yml` but forgot `docker-compose.override.yml` also defined `8079:8080`. Compose merges port lists by appending, which created conflicting duplicate host binds and prevented `cleanapp_service` from starting.

Corrected by:
- Removing the `ports:` override for `cleanapp_service` from `/home/deployer/docker-compose.override.yml` on prod so the base compose file is the single source of truth for that mapping.

What worked:
- Rebinding internal service ports (908x/909x/8079/3306/5672/15672/3001/3002) to `127.0.0.1` while keeping intentionally-public ports `3000` and `8090` unchanged.
- Verifying nginx-routed public health + `/version` endpoints from the VM (all 200).
- Capturing and committing a post-change prod xray snapshot: `xray/prod/2026-02-09-postharden1/`.

Next time:
- Always check for duplicate `ports:` across compose files before changing host binds (because list-merge semantics can create conflicts).

### 2026-02-09 (Nginx Port Visibility + Secret Hygiene)

Got wrong:
- Xray capture claimed “no secret values” but could still capture secret-like literals from `/home/deployer/docker-compose.yml` (e.g., password fields).
- Gitleaks flags token-shaped placeholders in docs/tests (Bearer headers, JWT-looking strings, long hex keys), even when they are examples.

Corrected by:
- Enabling a separate nginx debug access log on prod that records `host`, `server_port`, and `upstream` so we can quantify real `:8080` usage before closing it.
- Removing hardcoded RabbitMQ/AMQP creds (`cleanapp`) from repo compose/templates and switching to env placeholders.
- Redacting token-like examples in docs/tests to avoid gitleaks false positives and accidental copy/paste of real creds.
- Updating `xray/capture_prod_snapshot.sh` to redact secret-like values from the captured compose file stream.

What worked:
- Running `gitleaks detect --no-git` before commit/push to guarantee the current tree is clean.

Next time:
- Decide early whether we want to purge historic leaks (history rewrite) vs. start a clean-slate repo for the next upgrade push.

### 2026-02-09 (RabbitMQ Consumers: Bounded Concurrency + Ack Semantics)

Got wrong:
- Rust RabbitMQ consumers used `tokio::spawn` inside `Callback::on_message`, so the subscriber acked immediately and we could lose messages on async failure.
- `report-tags` ran the subscriber on a throwaway runtime thread; once `Subscriber::start()` returned, the runtime dropped and the consumer likely stopped.
- Could not push upstream `cleanappio/cleanapp-rustlib` changes due to GitHub permissions, so “bump tag and redeploy” wasn’t possible immediately.

Corrected by:
- Enforcing bounded concurrency + post-processing ack/nack semantics in the Rust subscriber implementation, and removing per-message async spawning from consumer callbacks.
- Making `report-tags` start its subscriber on the main runtime and keeping the task alive for the process lifetime.
- Vendoring a patched `cleanapp_rustlib` into this repo (`vendor/cleanapp_rustlib`) and switching Rust services to a local `path` dependency to unblock deploys without upstream access.

What worked:
- Running `gitleaks detect --no-git --redact` after vendoring and before staging/commits.

Next time:
- Before relying on a library’s `start()` semantics, confirm whether it blocks or spawns-and-returns; it changes how you keep the runtime alive.

### 2026-02-09 (RabbitMQ Consumer Rollout: report-tags + replier-twitter)

Got wrong:
- `replier-twitter` Cloud Build failed because `anyhow::Error` in this crate didn’t satisfy `std::error::Error` at the subscriber callback boundary (so it couldn’t be returned as `Box<dyn Error>` or wrapped with `subscriber::permanent(...)`).
- Forgot that `replier-twitter/build_image.sh` only builds on `-e dev`; `-e prod` is just tag promotion.

Corrected by:
- Wrapping `anyhow::Error` into `std::io::Error` (stringified) before returning from `Callback::on_message`.
- Building with `./build_image.sh -e dev`, then tagging with `./build_image.sh -e prod`, then deploying with `docker compose up -d --no-deps`.

What worked:
- Deploying `cleanapp_report_tags_service` and `cleanapp_replier_twitter` with `--no-deps` avoided restarting DB/RabbitMQ.
- Verifying `report-tags` and `report-fast-renderer` via localhost `/health` + `/version` confirmed the correct versions were running.

Next time:
- Avoid `anyhow::Error` at trait boundaries; normalize to a std error type (or a small custom error) before returning.
- Consider setting `RABBITMQ_CONCURRENCY` explicitly per consumer in compose for predictable throughput tuning.

### 2026-02-09 (RabbitMQ DLQs + Blueprinted Prod Deploy Config)

Got wrong:
- Assumed we could pipe scripts into `ssh` (stdin redirection/heredocs); sandbox restrictions block that path.
- Earlier xray compose redaction emitted literal `\\1\\2` backreference strings, making the captured compose hard to read.

Corrected by:
- Applying DLX/DLQ config via direct `ssh` command strings (no stdin piping) and verifying via `rabbitmqctl list_policies/list_queues`.
- Fixing compose redaction in `xray/capture_prod_snapshot.sh` and re-capturing `xray/prod/2026-02-09-postdlq2/`.

What worked:
- Enabling DLQs (`cleanapp-dlx` + `<queue>.dlq` queues) for:
  - `report-tags-queue`
  - `report-renderer-queue`
  - `twitter-reply-queue`
- Copying redacted prod compose + nginx conf into `platform_blueprint/deploy/prod/` and generating a new digest manifest.

Next time:
- Prefer repo-shipped ops scripts (like `platform_blueprint/rabbitmq/*`) for prod changes: repeatable and auditable.

### 2026-02-10 (Analyzer Observability: /metrics + CI Golden Path)

Got wrong:
- Added a CI golden-path script that used `curl -u user:pass ...`; gitleaks flagged it even though it was only for localhost RabbitMQ.
- Assumed we could keep `go.mod` "close enough" without a local Go toolchain; Cloud Build treated the module as read-only and failed builds until the module graph was tidied.

Corrected by:
- Switching CI RabbitMQ mgmt calls to `curl --netrc-file ...` with the credential material created at runtime (not committed).
- Adding a deterministic `stub` LLM provider to let CI run end-to-end without real API keys.
- Exposing Prometheus `/metrics` on `report-analyze-pipeline` and adding basic RabbitMQ processing metrics.
- Updating the analyzer Dockerfile to run `go mod tidy` during the build so Cloud Build doesn't choke on go.mod drift.

What worked:
- Gitleaks as a hard gate before commits prevented "harmless" but policy-breaking auth patterns from landing.
- Shipping the stub provider plus the docker-compose CI harness gave us an automated regression net for the analyzer pipeline.

Next time:
- When adding new Go deps in a module without local Go tooling, assume Cloud Build will enforce tidy behavior: either keep module files tidy in-repo or run tidy in Docker build.

### 2026-10-07 (Human analyzer queue delay)

Evidence:
- A Gemini failure triggered publication to a missing human retry exchange, closing only the AMQP channel; the subscriber kept its open connection and retried QoS forever.
- The human container stayed running with zero consumers. Five-minute watchdog recovery through bulk masked the outage and increased delays.
- Mapping analyzer releases to human is insufficient unless digest generation resolves Compose `extends`; manual YAML field parsing omitted the human image.

What worked:
- Confirming broker-side channel exception and matching worker/database UTC traces before changing model settings or concurrency.
- Testing channel-only failures on a disposable broker and published replay serialization on a disposable database.
- Preserving raw reports/queue messages and protecting already published backlog copies from duplicate model work/publication.
- Saving private production rollback configs on the VM and deploying from a clean, exact-source worktree.

Next time:
- Treat running containers and listener health as insufficient pipeline health; monitor human queue consumers, completions and persistent backlog.
- Explicitly capture child exit status in watchdogs; a brace group checked with `||` disables Bash errexit inside it.
- Preserve per-service digest pins during partial releases, include every analyzer consumer and verify embedded commit provenance after rollout.

### 2026-10-10 (Outgoing mail migration to Workspace)

What worked:
- Confirmed live production uses email-service 1.0.38 and auth-service; Rust V3 is inactive.
- Verified info@cleanapp.io is an active licensed Workspace user and production egress is 34.122.15.16.
- Added shared verified STARTTLS SMTP transport and provider selection for both active senders.
- Retained templates, inline CIDs, recipient rules and explicit SendGrid rollback; new deliveries record the selected provider.
- Prepared a Workspace relay rule restricted to registered users, one production IP and required TLS.

Limitations at preparation time:
- Google rejected the production IP before relay authorization; source readiness is not a live cutover.
- Saving new SMTP relay authorization requires user confirmation in browser policy.

Next time:
- Verify Google acceptance and recipient authentication headers before completing a provider cutover.
- Keep production image digests and main source aligned; preserve rollback configuration privately.

Session-end update:

- Saved the approved Workspace SMTP relay rule for registered users, production
  egress `34.122.15.16` and required TLS.
- Added apex SPF `v=spf1 include:_spf.google.com ~all` through Namecheap,
  preserving the 49 existing DNS records; authoritative DNS exposes the record.
- PR #143 merged as `d205fee705eac9bfd87eec6cccbdfb7bb575bb5c` after checks passed.
- Source-built and deployed both active services from the merged commit through
  the canonical digest workflow. Health, embedded commit, immutable digests,
  `google_workspace`, sender `info@cleanapp.io` and verified TLS all passed.
  Critical auth configuration and all 35 unrelated containers/pins were unchanged;
  no mailbox password or SMTP credential was added. V3 remains inactive.
- Enabled 2048-bit domain DKIM with selector `cleanapp20261010`, publishing its
  public TXT record while preserving the original 49 DNS records. Externally
  received production messages passed aligned DKIM and DMARC. Google MX still
  cached old SPF data; public DNS already exposes the correct SPF record.
- The deployed application and password-reset tests reached the owned Gmail
  inbox. Later domain-signed tests were placed in Spam; Gmail explicitly attributed
  this to previous cleanapp.io messages being marked as spam. Kept mailbox
  classification untouched rather than changing test results.
- At 22:51:19 UTC, natural notifications recorded 7,101 report-recipient delivery
  links for 1,951 reports and 132 distinct recipients using `google_workspace`.
  No fresh SMTP/provider errors were observed. The first candidate query took
  5m44s; candidate SQL, scheduling and recipient selection are byte-for-byte
  unchanged from the previous deployed email commit.
- Rollback snapshots remain private on the VM. Preserve the old auth image locally
  because it had no registry digest; do not prune it until rollback is unnecessary.
