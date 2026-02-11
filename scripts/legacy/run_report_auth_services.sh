#!/usr/bin/env bash
set -euo pipefail

echo "Starting CleanApp services with report-auth microservice..."

# Repo root from this script location
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/conf/compose/docker-compose.report-auth.yml"

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "ERROR: Docker is not running. Please start Docker first."
    exit 1
fi

compose() {
    if docker compose version >/dev/null 2>&1; then
        docker compose "$@"
    else
        docker-compose "$@"
    fi
}

# Build and start services
echo "Building and starting services..."
compose -f "${COMPOSE_FILE}" up --build -d

echo ""
echo "Services started successfully!"
echo ""
echo "Service URLs:"
echo "  Auth Service: http://localhost:8080"
echo "  Report Auth Service: http://localhost:8081"
echo "  Database: localhost:3306"
echo ""
echo "To view logs:"
echo "  compose -f ${COMPOSE_FILE} logs -f"
echo ""
echo "To stop services:"
echo "  compose -f ${COMPOSE_FILE} down"
