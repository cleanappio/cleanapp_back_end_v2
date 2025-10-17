#!/usr/bin/env bash
# xtrabackup_gen_key_and_secret.sh
set -euo pipefail

ENV=""
PROJECT="cleanup-mysql-v2"

usage() {
  echo "Usage: $0 -e <dev|prod> [-p <gcp-project>]"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -e|--env) ENV="$2"; shift 2 ;;
    -p|--project) PROJECT="$2"; shift 2 ;;
    *) usage ;;
  esac
done

[[ -z "${ENV}" ]] && usage
ENV_UPPER=$(echo "${ENV}" | tr '[:lower:]' '[:upper:]')
[[ "${ENV_UPPER}" != "DEV" && "${ENV_UPPER}" != "PROD" ]] && usage

SECRET_NAME="XTRABACKUP_SSH_KEY_${ENV_UPPER}"
KEY_BASE="/tmp/xtrabackup_${ENV}"

echo "Generating ED25519 keypair at ${KEY_BASE}..."
rm -f "${KEY_BASE}" "${KEY_BASE}.pub"
ssh-keygen -t ed25519 -N "" -C "xtrabackup-${ENV}" -f "${KEY_BASE}" >/dev/null

echo "Ensuring secret ${SECRET_NAME} exists in project ${PROJECT}..."
if ! gcloud secrets describe "${SECRET_NAME}" --project "${PROJECT}" >/dev/null 2>&1; then
  gcloud secrets create "${SECRET_NAME}" --replication-policy=automatic --project "${PROJECT}"
fi

echo "Uploading private key to Secret Manager as a new version..."
gcloud secrets versions add "${SECRET_NAME}" --data-file="${KEY_BASE}" --project "${PROJECT}"

echo
echo "Public key for authorized_keys (copy the entire line below):"
cat "${KEY_BASE}.pub"
echo
echo "Done. Add the public key to prod2 ~deployer/.ssh/authorized_keys with the forced-command and from= restrictions we discussed."