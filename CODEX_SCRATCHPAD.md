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
