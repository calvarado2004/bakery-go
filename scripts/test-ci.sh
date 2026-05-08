#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export JWT_SECRET="${JWT_SECRET:-change-in-production}"
export CSRF_KEY="${CSRF_KEY:-change-in-production}"
export BAKERY_TEST_USE_EXISTING_INFRA=1

wait_for_port() {
  local host="$1"
  local port="$2"
  local timeout_secs="$3"
  local start_ts
  start_ts="$(date +%s)"

  while true; do
    if (echo >"/dev/tcp/${host}/${port}") >/dev/null 2>&1; then
      return 0
    fi
    if (( "$(date +%s)" - start_ts >= timeout_secs )); then
      echo "Timed out waiting for ${host}:${port} after ${timeout_secs}s" >&2
      return 1
    fi
    sleep 1
  done
}

cleanup() {
  if [[ "${KEEP_TEST_STACK:-0}" != "1" ]]; then
    docker compose down --remove-orphans >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# Reset stack state to avoid stale/conflicting containers between CI runs.
docker compose down --remove-orphans >/dev/null 2>&1 || true

# Start infra + server for integration/E2E tests that require localhost:50051.
docker compose up -d postgres rabbitmq server
wait_for_port localhost 5432 90
wait_for_port localhost 5672 90
wait_for_port localhost 50051 120

# Deterministic run: no test cache and single package at a time.
go test -p 1 -count=1 ./... -covermode=atomic -coverprofile=cover.out

go tool cover -func=cover.out | tail -n 1
