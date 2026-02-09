# Platform Manifests (Image Digests)

These manifests pin the *as-deployed* image **digests** captured by xray snapshots.

Why this exists:
- Docker tags like `:prod` and `:dev` are mutable.
- Digests like `@sha256:...` are immutable.
- When we upgrade, we want to be able to answer "exactly what was running?" and roll back deterministically.

## Files

- `prod/<date>.json`: digest pin set for prod at that snapshot date
- `dev/<date>.json`: digest pin set for dev at that snapshot date

## How To Generate

From repo root:

```bash
python3 xray/generate_platform_manifest.py --xray-dir xray/prod/2026-02-07 --out platform_blueprint/manifests/prod/2026-02-07.json
python3 xray/generate_platform_manifest.py --xray-dir xray/dev/2026-02-07 --out platform_blueprint/manifests/dev/2026-02-07.json
```

## How To Use During Upgrades

Treat the latest manifest as the baseline lockfile:
- Before upgrading: capture a new xray snapshot and generate a new manifest.
- During rollout: prefer `image@sha256:...` references when you need deterministic redeploys.
- After rollout: capture another xray snapshot and diff manifests (baseline vs new).

## Turning A Manifest Into A Compose Override

To use a manifest directly with docker compose, generate an override file that replaces each `image:` tag with its pinned digest:

```bash
python3 platform_blueprint/deploy/generate_compose_digests.py \
  --manifest platform_blueprint/manifests/prod/2026-02-09.json \
  --compose platform_blueprint/deploy/prod/docker-compose.yml \
  --compose platform_blueprint/deploy/prod/docker-compose.override.yml \
  --out /tmp/docker-compose.digests.yml
```

