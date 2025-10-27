#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME=${IMAGE_NAME:-report-listener-v4}
IMAGE_TAG=${IMAGE_TAG:-latest}

docker build -t "${IMAGE_NAME}:${IMAGE_TAG}" .


