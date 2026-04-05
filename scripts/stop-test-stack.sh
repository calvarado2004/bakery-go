#!/bin/bash
# stop-test-stack.sh
# Stops the Docker Compose stack used for integration tests

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "=== Stopping Docker Compose Stack ==="

# Stop all services
docker-compose down

# Optionally remove volumes (uncomment if needed)
# docker-compose down -v

# Remove networks (uncomment if needed)
# docker-compose down --remove-orphans

echo ""
echo "✓ Docker Compose stack stopped successfully"

# Show remaining containers
echo ""
echo "Remaining containers:"
docker ps -a --filter "name=bakery-go" --format "table {{.Names}}\t{{.Status}}"
