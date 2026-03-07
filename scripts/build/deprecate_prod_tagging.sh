#!/usr/bin/env bash

warn_deprecated_prod_tagging() {
  local opt="${1:-}"
  local service_dir="${2:-$(basename "$PWD")}"
  if [[ "${opt}" != "prod" ]]; then
    return 0
  fi

  cat >&2 <<MSG
ERROR: ./build_image.sh -e prod is deprecated.

Reason:
- the old prod path only re-tagged an existing image
- it did not guarantee a fresh source build
- it drifted from the digest-pinned deploy flow

Use the canonical source-build-and-pin path instead:
  HOST=deployer@34.122.15.16 SOURCE_SERVICES="${service_dir}" \
    ./platform_blueprint/deploy/prod/vm/source_build_and_deploy.sh

Or, for already-built images only:
  HOST=deployer@34.122.15.16 SERVICES="<compose_service>" \
    ./platform_blueprint/deploy/prod/vm/deploy_with_digests.sh
MSG
  exit 2
}
