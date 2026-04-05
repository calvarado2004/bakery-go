#!/bin/bash
# run-integration-tests.sh
# Runs integration tests using docker-compose for PostgreSQL and RabbitMQ

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

# Export JWT_SECRET for tests (matches docker-compose fallback)
export JWT_SECRET="${JWT_SECRET:-change-in-production}"

echo "=== Starting Docker Compose for Integration Tests ==="

# Stop and remove existing containers to ensure clean state
echo "Cleaning up existing containers..."
docker-compose down --remove-orphans 2>/dev/null || true

# Start the required services
docker-compose up -d postgres rabbitmq server broker makers

# Wait for services to be ready
echo "Waiting for PostgreSQL to be ready..."
for i in {1..30}; do
    if docker-compose exec -T postgres pg_isready -U postgres -d bakery > /dev/null 2>&1; then
        echo "PostgreSQL is ready"
        break
    fi
    sleep 1
done

echo "Waiting for RabbitMQ to be ready..."
for i in {1..30}; do
    if docker-compose exec -T rabbitmq rabbitmq-diagnostics -q ping > /dev/null 2>&1; then
        echo "RabbitMQ is ready"
        break
    fi
    sleep 1
done

echo "Waiting for gRPC server to be ready..."
for i in {1..60}; do
    # Try to connect to the gRPC port using nc (netcat)
    if docker-compose exec -T server nc -z 0.0.0.0 50051 2>/dev/null; then
        echo "gRPC server is ready"
        break
    fi
    sleep 1
done

# Wait for broker and makers to be ready
echo "Waiting for broker to be ready..."
for i in {1..30}; do
    if docker-compose logs broker 2>&1 | grep -q "Listening for buy bread orders"; then
        echo "Broker is ready"
        break
    fi
    sleep 1
done

echo "Waiting for makers to be ready..."
for i in {1..30}; do
    if docker-compose logs makers 2>&1 | grep -q "Listening for make bread orders"; then
        echo "Makers is ready"
        break
    fi
    sleep 1
done

# Additional wait for services to fully initialize and register all handlers
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
    -timeout 180s \
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
