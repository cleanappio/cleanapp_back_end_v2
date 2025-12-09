#!/bin/bash

# Restart email-service to pick up new image
# Usage: ./restart_email_service.sh -e <dev|prod> [--ssh-keyfile <keyfile>]

OPT=""
SSH_KEYFILE=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-e"|"--env")
      OPT="$2"
      shift 2
      ;;
    "--ssh-keyfile")
      SSH_KEYFILE="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

if [ -z "${OPT}" ]; then
  echo "Usage: $0 -e|--env <dev|prod> [--ssh-keyfile <ssh_keyfile>]"
  exit 1
fi

case ${OPT} in
  "dev")
      echo "Using dev environment"
      TARGET_VM_IP="34.132.121.53"
      ;;
  "prod")
      echo "Using prod environment"
      TARGET_VM_IP="34.122.15.16"
      ;;
  *)
    echo "Usage: $0 -e|--env <dev|prod> [--ssh-keyfile <ssh_keyfile>]"
    exit 1
    ;;
esac

SSH_CMD="ssh"
if [ -n "${SSH_KEYFILE}" ]; then
  SSH_CMD="ssh -i ${SSH_KEYFILE}"
fi

echo "🔄 Restarting email-service on ${TARGET_VM_IP}..."

# SSH to VM and restart the email-service container
${SSH_CMD} ${TARGET_VM_IP} << 'REMOTE'
# Login to Artifact Registry
ACCESS_TOKEN=$(gcloud auth print-access-token)
echo "${ACCESS_TOKEN}" | docker login -u oauth2accesstoken --password-stdin https://us-central1-docker.pkg.dev

# Pull the latest image
docker pull us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-email-service-image:${OPT:-prod}

# Restart the email service container
cd /path/to/compose && sudo docker compose pull cleanapp_email_service && sudo docker compose up -d cleanapp_email_service

echo "✅ Email service restarted"
REMOTE

echo "Done!"
