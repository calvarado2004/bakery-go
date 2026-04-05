#!/bin/bash
# run-integration-tests.sh
# Runs integration tests using docker-compose for PostgreSQL and RabbitMQ

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "=== Starting Docker Compose for Integration Tests ==="

# Start the required services
docker-compose up -d postgres rabbitmq

# Wait for services to be ready
echo "Waiting for PostgreSQL to be ready..."
sleep 3

echo "Waiting for RabbitMQ to be ready..."
sleep 3

# Verify services are up
echo "Checking service health..."
docker-compose ps

echo ""
echo "=== Running Integration Tests ==="

# Run integration tests with coverage
go test ./... \
    -race \
    -count=1 \
    -timeout 120s \
    -coverprofile=cover_all.out \
    -covermode=atomic \
    -v

TEST_EXIT_CODE=$?

echo ""
echo "=== Stopping Docker Compose Stack ==="
docker-compose down

echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "✓ All integration tests passed!"
else
    echo "✗ Some tests failed (exit code: $TEST_EXIT_CODE)"
fi

exit $TEST_EXIT_CODE
