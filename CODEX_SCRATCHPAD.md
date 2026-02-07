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
