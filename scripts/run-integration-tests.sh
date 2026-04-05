#!/bin/bash
# run-integration-tests.sh
# Runs integration tests using docker-compose for PostgreSQL and RabbitMQ
#
# Usage:
#   ./run-integration-tests.sh              # Run all integration tests
#   ./run-integration-tests ./broker/...    # Run tests for specific package

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

# Export JWT_SECRET for tests (matches docker-compose fallback)
export JWT_SECRET="${JWT_SECRET:-change-in-production}"

# Determine which packages to test
TEST_PACKAGES="${1:-./...}"

echo "=== Starting Docker Compose for Integration Tests ==="

# Stop and remove existing containers to ensure clean state
echo "Cleaning up existing containers..."
docker-compose down --remove-orphans 2>/dev/null || true

# Determine which services to start based on test packages
START_ALL=false
START_BROKER=false
START_MAKERS=false
START_SERVER=false

case "$TEST_PACKAGES" in
    "./broker/..."*|*"broker"* )
        START_BROKER=true
        # Broker needs makers to create customers for foreign key constraint
        START_MAKERS=true
        ;;
    "./makers/..."*|*"makers"* )
        START_MAKERS=true
        ;;
    "./server/..."*|*"server"*|*"frontend"* )
        START_SERVER=true
        ;;
    * )
        START_ALL=true
        ;;
esac

# Start the required services
if [ "$START_ALL" = true ]; then
    echo "Starting all services (postgres, rabbitmq, server, broker, makers)..."
    docker-compose up -d postgres rabbitmq server broker makers
elif [ "$START_BROKER" = true ]; then
    echo "Starting broker services (postgres, rabbitmq, broker, makers)..."
    docker-compose up -d postgres rabbitmq broker makers
elif [ "$START_MAKERS" = true ]; then
    echo "Starting makers services (postgres, rabbitmq, makers)..."
    docker-compose up -d postgres rabbitmq makers
elif [ "$START_SERVER" = true ]; then
    echo "Starting server services (postgres, rabbitmq, server)..."
    docker-compose up -d postgres rabbitmq server
else
    echo "Starting base services (postgres, rabbitmq)..."
    docker-compose up -d postgres rabbitmq
fi

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

# Wait for optional services
if [ "$START_SERVER" = true ] || [ "$START_ALL" = true ]; then
    echo "Waiting for gRPC server to be ready..."
    for i in {1..60}; do
        if docker-compose exec -T server nc -z 0.0.0.0 50051 2>/dev/null; then
            echo "gRPC server is ready"
            break
        fi
        sleep 1
    done
fi

if [ "$START_BROKER" = true ] || [ "$START_ALL" = true ]; then
    echo "Waiting for broker to be ready..."
    for i in {1..30}; do
        if docker-compose logs broker 2>&1 | grep -q "Listening for buy bread orders"; then
            echo "Broker is ready"
            break
        fi
        sleep 1
    done
    # Also wait for makers since broker depends on it
    if [ "$START_MAKERS" = true ]; then
        echo "Waiting for makers to be ready..."
        for i in {1..30}; do
            if docker-compose logs makers 2>&1 | grep -q "Listening for make bread orders"; then
                echo "Makers is ready"
                break
            fi
            sleep 1
        done
    fi
fi

# Additional wait for services to fully initialize and register all handlers
sleep 3

# Verify services are up
echo "Checking service health..."
docker-compose ps

echo ""
echo "=== Running Integration Tests ==="
echo "Testing packages: $TEST_PACKAGES"

# Run integration tests with coverage
go test $TEST_PACKAGES \
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
