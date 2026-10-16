#!/usr/bin/env bash
set -euo pipefail

# Canonical source builds use this fixed subset. The existing build_images.sh
# entrypoints retain their broader, manually invoked build behavior.
ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
if [[ $# != 3 || "$2" != "-e" || "$3" != "dev" ]]; then
  echo "usage: $0 <news-indexer|news-indexer-bluesky> -e dev" >&2
  exit 2
fi
case "$1" in
  news-indexer|news-indexer-bluesky) service="$1" ;;
  *) echo "ERROR: unsupported selected news source" >&2; exit 2 ;;
esac
cd "${ROOT_DIR}/${service}"
version="$(tr -d '\r\n' < .version)"
if [[ "${service}" == "news-indexer" ]]; then
  version="${version#BUILD_VERSION=}"
fi
if [[ ! "${version}" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "ERROR: invalid selected news build version" >&2
  exit 2
fi
version="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((10#${BASH_REMATCH[3]} + 1))"
if [[ "${service}" == "news-indexer" ]]; then
  printf 'BUILD_VERSION=%s\n' "${version}" > .version
else
  printf '%s\n' "${version}" > .version
fi

region="${CLOUD_REGION:-us-central1}"
project="${PROJECT_NAME:-cleanup-mysql-v2}"
repository="${region}-docker.pkg.dev/${project}/cleanapp-docker-repo"
if [[ "${service}" == "news-indexer" ]]; then
  substitutions="_TAG_ANALYZER=${repository}/cleanapp-news-analyzer-twitter-image:${version}"
else
  substitutions="_TAG_INDEXER=${repository}/cleanapp-bluesky-indexer-image:${version},_TAG_ANALYZER=${repository}/cleanapp-bluesky-analyzer-image:${version},_TAG_SUBMITTER=${repository}/cleanapp-bluesky-submitter-image:${version},_TAG_STREAM=${repository}/bluesky-now:${version}"
fi
gcloud builds submit --region="${region}" --project="${project}" \
  --config=cloudbuild.replacements.yaml --substitutions="${substitutions}" .

# Publish only this adapter's fixed image set. Canonical promotion to :prod is
# performed separately by source_build_and_deploy.sh after successful builds.
source "${ROOT_DIR}/platform_blueprint/deploy/prod/vm/source_build_map.sh"
while IFS= read -r image; do
  tag="${region}-docker.pkg.dev/${project}/${image}"
  gcloud artifacts docker tags add "${tag}:${version}" "${tag}:dev"
done < <(news_replacement_images_for_source_service "${service}")
