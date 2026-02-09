# cleanapp-platform (Blueprint)

This folder is a **starter blueprint** for the repo I recommend creating as the single integration point for:
- `cleanapp-mobile`
- `cleanapp-frontend`
- `cleanapp-backend` (this repo)

Goal: upgrades/refactors happen against the **real integrated system** (contracts + deploy config + end-to-end tests), not in isolated repos.

This is not a production-ready repo by itself; it’s a scaffold you can lift into a new repo when you’re ready.

## Suggested Layout

```
cleanapp-platform/
  repos/
    README.md
  contracts/
    openapi/
  deploy/
    prod/
      docker-compose.yml
      nginx_conf_d/
  manifests/
    README.md
    prod/
    dev/
  tests/
    smoke/
```

## Source-of-Truth Inputs

As of the prod xray snapshot (2026-02-07), the canonical as-deployed artifacts are under:
- `xray/prod/2026-02-07/`

As of the latest prod xray snapshot (2026-02-09-postdlq2), the canonical as-deployed artifacts are under:
- `xray/prod/2026-02-09-postdlq2/`

We also keep a redacted copy of prod deploy config here (for future migration into `cleanapp-platform/`):
- `platform_blueprint/deploy/prod/docker-compose.yml`
- `platform_blueprint/deploy/prod/nginx_conf_d/`

For deterministic upgrades/rollbacks, pin the baseline using image digest manifests:
- `platform_blueprint/manifests/`

The recommended bootstrap path is:
1. Copy the prod compose + nginx configs into the platform repo.
2. Add OpenAPI + event schemas as explicit contracts.
3. Add smoke tests that validate the real deployed flows.
