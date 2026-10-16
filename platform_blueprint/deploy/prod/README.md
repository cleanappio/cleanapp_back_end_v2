# Prod Deploy (Blueprint)

This directory is where the platform repo would keep prod deployment artifacts:
- `docker-compose.yml` (single source of truth)
- `nginx_conf_d/*.conf` (public routing)
- scripts (upgrade/rollback/smoke)

For the current real-world deployed versions (2026-02-09 snapshot), see:
- `xray/prod/2026-02-09-postdlq2/prod_docker-compose.yml`
- `xray/prod/2026-02-09-postdlq2/nginx_conf_d/`

The first integration milestone is to make the platform repo’s `docker-compose.yml` match prod reality (no manual containers).

## Override File

`docker-compose.override.yml` is a small overlay intended to:
- add restart policies for long-running services
- bring `cleanapp_bluesky_now` under compose

Note: keep `cleanapp_service`'s host port mapping (`8079:8080`) in `docker-compose.yml` (not the override), because compose file merges append list fields like `ports`.

## Digest Pins (Deterministic Deploys)

For deterministic rollouts/rollbacks, prefer `image@sha256:...` pins rather than mutable tags.

### Canonical source-build + deploy helper
For code changes, the canonical production path is now:

```bash
HOST=deployer@34.122.15.16 SOURCE_SERVICES="report-listener customer-service" \
  ./platform_blueprint/deploy/prod/vm/source_build_and_deploy.sh
```

This helper:
1. stages the exact git ref (`HEAD` by default) to the prod VM
2. builds the selected service images from that staged source on the VM
3. promotes each resulting image to `:prod`
4. runs explicit Go migrations from the same staged source
5. deploys via digest pins

Optional inputs:
- `REF=<git-ref>`
- `HOST=deployer@34.122.15.16`
- `RUN_GO_MIGRATIONS=0|1`
- `BUILD_ONLY=0|1` (default `0`; only these literal values are accepted)
- `KEEP_REMOTE_SOURCE=1`

To build and verify replacement images while the current containers keep running:

```bash
make deploy-prod-source REF=<reviewed-commit> SOURCE_SERVICES="report-fast-renderer report-listener-v4" BUILD_ONLY=1
```

Build-only uses the same exact-source staging, image builds and `:prod` tag promotion. It skips explicit migrations and the digest deploy helper, so it does not write compose pins or restart containers. Tag promotion is a registry change; it is not a release. Capture and verify the selected Build digests before a separately authorized deployment. At deployment, recheck that the selected tags still resolve to those exact digests, preserve every unselected pin, and use the normal scoped `make deploy-prod SERVICES="<selected compose services>"` path with explicit migrations.

The command contract is covered locally with `python3 -m unittest discover -s platform_blueprint/deploy/prod/vm/tests` using an isolated fake build host; it makes no production calls.

The canonical news adapters have a fixed replacement scope. `news-indexer-bluesky` builds and promotes only the indexer, analyzer, submitter and `bluesky-now` images. `news-indexer` builds and promotes only the Twitter analyzer. Their `cloudbuild.replacements.yaml` files select these targets from the existing Dockerfiles; the original broader `build_images.sh` commands remain available and unchanged. Neither adapter publishes the Bluesky report analyzer or Twitter indexer/submitter images.

The eight-service shutdown replacement build can therefore be prepared with:

```bash
make deploy-prod-source REF=<reviewed-commit> BUILD_ONLY=1 SOURCE_SERVICES="news-indexer-bluesky news-indexer report-fast-renderer report-listener-v4 replier-twitter"
```

The two news source versions increment in the staged archive using their existing file formats. Cloud Build receives that exact source directory and a fixed selected-image recipe. Build-only still promotes the selected tags; verify their exact Build digests before activation. The source mappings for this command name only the eight selected runtime services, and leave migrations and digest activation to the normal, separately authorized deployment.

Some existing production Bluesky services use shared custom image repositories (`cleanapp-bluesky-diverse` or `cleanapp-bluesky-submitter-evidence`). Tagging the canonical replacement repositories does not update those image references. The reviewed activation must compare the current effective compose image references and pins, replace only the selected entries with the verified replacement digests, and preserve every unselected entry. The ordinary tag-based deploy must not be assumed to perform that repository change.

Supported `SOURCE_SERVICES` currently include the services in this repo that have compose mappings, including:
- `areas-service`
- `auth-service`
- `brand-dashboard`
- `custom-area-dashboard`
- `customer-service`
- `docker_backend`
- `docker_pipelines`
- `email-fetcher`
- `email-service`
- `email-service-v3`
- `gdpr-process-service`
- `news-indexer` (Twitter analyzer only)
- `news-indexer-bluesky` (four selected Bluesky services)
- `replier-twitter`
- `report-analyze-pipeline`
- `report-fast-renderer`
- `report-listener`
- `report-listener-v4`
- `report-ownership-service`
- `report-processor`
- `report-tags`
- `reports-pusher`
- `stxn_kickoff`
- `voice-assistant-service`

### Digest pin helper (already-built tags)
If you already have the desired `:prod` tags in Artifact Registry and only want a pinned rollout/restart, use:

```bash
HOST=deployer@34.122.15.16 ./platform_blueprint/deploy/prod/vm/deploy_with_digests.sh
```

By default this runs the explicit Go service migrations before the deploy. The helper stages the local source for the Go migration-backed services to the VM, runs each `cmd/migrate` entrypoint in a transient `golang:1.24-alpine` container on the VM, and then proceeds with the digest-pinned compose rollout.

To skip Go migrations for a no-schema-change restart, opt out explicitly:

```bash
HOST=deployer@34.122.15.16 RUN_GO_MIGRATIONS=0 ./platform_blueprint/deploy/prod/vm/deploy_with_digests.sh
```

This runs `docker compose pull` on the VM, resolves the locally pulled images to `RepoDigests`, writes:
- `~/docker-compose.digests.<timestamp>.yml`
- `~/docker-compose.digests.current.yml` (symlink)

and then deploys with the digest override.

Inputs:
- A captured digest manifest from xray: `platform_blueprint/manifests/prod/<date>.json`
- Optionally, a checked-in digest override under: `platform_blueprint/deploy/prod/digests/*.digests.yml`

Generate a digest-pinned override:

```bash
python3 platform_blueprint/deploy/generate_compose_digests.py \
  --manifest platform_blueprint/manifests/prod/2026-02-09-postdlq2.json \
  --compose platform_blueprint/deploy/prod/docker-compose.yml \
  --compose platform_blueprint/deploy/prod/docker-compose.override.yml \
  --out /tmp/docker-compose.digests.yml
```

Use it during deploy:

```bash
docker compose -f docker-compose.yml -f docker-compose.override.yml -f /tmp/docker-compose.digests.yml pull
docker compose -f docker-compose.yml -f docker-compose.override.yml -f /tmp/docker-compose.digests.yml up -d
```
