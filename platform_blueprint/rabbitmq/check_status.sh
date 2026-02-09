#!/usr/bin/env bash
set -euo pipefail

# RabbitMQ status snapshot (VM-local).

RABBIT_CONTAINER="${RABBIT_CONTAINER:-cleanapp_rabbitmq}"
VHOST="${VHOST:-/}"

echo "== rabbitmq version =="
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl version

echo
echo "== exchanges (filtered) =="
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl list_exchanges name type | egrep "^(name|cleanapp-exchange|cleanapp-dlx)\\b" || true

echo
echo "== queues (filtered) =="
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl list_queues name messages_ready messages_unacknowledged consumers | egrep "^(name|(report-(analysis|renderer|tags)-queue|twitter-reply-queue)(\\.dlq)?\\b)" || true

echo
echo "== policies (vhost=${VHOST}) =="
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl list_policies -p "${VHOST}"
