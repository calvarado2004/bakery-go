# Bakery-Go — Architecture Refactor Plan

## Goal

Preserve the educational value of this project (Go, gRPC, RabbitMQ, K8s, Docker, Auth, DBs) while fixing
a set of real architectural problems that currently make the system hard to scale, test, and reason about.

**The core principle:** each technology owns exactly one concern.

| Technology | Owns |
|---|---|
| **gRPC** | Synchronous client-facing API |
| **RabbitMQ** | Async workflow orchestration between services |
| **PostgreSQL** | Durable state + push notifications via LISTEN/NOTIFY |
| **HTTP REST** | External surface (API Gateway → frontend) |

---

## What Is Changing and Why

### Problem 1 — Dual-path streaming in `BuyBreadStream`

`BuyBreadStream` simultaneously polls the DB every 5 s *and* spawns a goroutine that consumes the
`bread-bought` RabbitMQ queue. Both paths converge on the same truth. This creates goroutine leak
risk, a consumer race (two goroutines competing on the same queue), and unnecessary DB load.

**Fix:** Replace the polling loop with PostgreSQL `LISTEN/NOTIFY`. The broker calls
`pg_notify('bakery_orders', order_uuid)` after updating order status. The server has a single
persistent listener per connection that wakes the waiting stream. No polling. No RabbitMQ
consumption in the server.

### Problem 2 — Server holds in-memory per-order state

`RabbitMQBakery.orders map[int]*OrderStatus` with a `sync.Mutex` is shared mutable state that is
lost on restart and prevents horizontal scaling of the server. Two server instances cannot share
this map.

**Fix:** Make the server stateless. All order status lives in PostgreSQL. LISTEN/NOTIFY delivers
push updates.

### Problem 3 — Inventory race condition in broker

The broker reads available stock and then deducts it in separate queries. Two concurrent brokers
(or even two messages processed by a worker pool) can both read sufficient stock and both deduct,
causing overselling.

**Fix:** Wrap inventory check + deduction in a single PostgreSQL transaction using `SELECT ... FOR UPDATE`.

### Problem 4 — Makers is single-threaded with an artificial 1 s sleep

Each `AdjustBreadQuantity` call is independent and idempotent. There is no reason to process them
one at a time.

**Fix:** Configurable worker pool in makers. Each message dispatched to a goroutine bounded by a
semaphore.

### Problem 5 — Broker has an unexplained 34 s sleep

This throttles throughput to roughly 100 orders/hour with no documented rationale. Removed and
replaced with the worker pool backpressure model (QoS=1 remains; the pool size is the throttle).

### Problem 6 — Frontend calls gRPC directly

The frontend imports gRPC-generated stubs. Every proto change can break the frontend build. There
is no place to put HTTP-specific concerns (cookies, CSRF, rate limiting, WebSocket upgrades).

**Fix:** Add a thin API Gateway service. The frontend calls REST JSON endpoints. The gateway
translates to gRPC internally. JWT lives at the HTTP layer (HttpOnly cookie). The gRPC server
uses token-in-metadata for internal calls.

### Problem 7 — Flat RabbitMQ queues with default exchange

All queues use the default exchange and named-queue routing. This means you cannot add a second
consumer (analytics, audit, webhook) without modifying every producer.

**Fix:** Switch to topic exchanges with routing keys:
- Exchange `orders` — keys: `buy.requested`, `buy.processed`, `buy.failed`
- Exchange `inventory` — keys: `restock.requested`, `restocked`

### Problem 8 — No graceful shutdown anywhere

Pods receive `SIGTERM` before being killed. Without a signal handler, in-flight gRPC requests,
RabbitMQ messages, and DB transactions are dropped.

**Fix:** Every service gets a `signal.NotifyContext` shutdown path.

---

## Revised Service Map

```
Browser / buyers CLI
    │
    │ HTTP JSON
    ▼
API Gateway (new)          ← JWT cookies, rate limiting, WebSocket
    │
    │ gRPC (internal)
    ▼
BakeryServer               ← stateless; publishes events; streams via LISTEN/NOTIFY
    │ publish             │ LISTEN/NOTIFY
    ▼                     ▼
RabbitMQ (exchanges)    PostgreSQL
    │
    ├──→ Broker (worker pool)    ← SELECT FOR UPDATE; pg_notify on done
    └──→ Makers (worker pool)    ← concurrent AdjustBreadQuantity

Buyers                     ← concurrent gRPC client demo
Frontend                   ← calls API Gateway only
```

---

## Task List

Each task has: a short goal, the files it touches, expected unit test coverage, and whether it needs
an integration test updated.

---

### Phase 1 — Correctness (fix bugs before adding features)

#### TASK-01 — Fix inventory race condition with `SELECT FOR UPDATE`

**Goal:** Ensure two concurrent brokers cannot both deduct the same bread stock.

**Files:**
- `data/repository.go` — new method `TryFulfillOrder(ctx, tx, order) error`
- `data/postgres_repository.go` — implementation inside a transaction
- `broker/main.go` — call `TryFulfillOrder` instead of `canFulfillOrder` + `processOrderItems`
- `broker/helpers.go` — `canFulfillOrder` and `processOrderItems` become pure unit-testable helpers
  called inside the transaction (or removed if logic moves to SQL)

**Unit tests:**
- `broker/helpers_test.go` — test `canFulfillOrder` with table-driven cases (sufficient stock,
  partial stock, zero stock, unknown bread) — target 100 %
- `data/repository_test.go` — test `TryFulfillOrder` with mock tx — target 90 %

**Integration test:** Update `broker/broker_integration_test.go` to run two concurrent brokers
against the same Testcontainer DB and assert no oversell.

**Coverage target:** broker helpers 100 %, data layer 90 %

---

#### TASK-02 — Add `CHECK (quantity >= 0)` constraint to schema

**Goal:** Database enforces non-negative bread quantity as a last line of defense.

**Files:**
- `bakery.sql` — add constraint to `bread` table
- `data/repository.go` — `AdjustBreadQuantity` handles the new constraint error and returns a
  typed `ErrInsufficientStock` sentinel

**Unit tests:**
- `data/errors_test.go` — test that constraint violation maps to `ErrInsufficientStock`

**Integration test:** Add a case in `broker/broker_integration_test.go` that attempts to deduct
more than available and asserts `ErrInsufficientStock` is returned.

**Coverage target:** error mapping 100 %

---

#### TASK-03 — Fix broker idempotency (duplicate message handling)

**Goal:** If RabbitMQ redelivers a message, the broker skips it instead of double-processing.

**Files:**
- `broker/main.go` — check `GetBuyOrderByUUID` before processing; ack-and-skip if found

**Unit tests:**
- `broker/broker_test.go` — add case: message with already-processed UUID is acked without
  touching DB — target 100 % of the dedup path

**Integration test:** Simulate redelivery in Testcontainer test by publishing same UUID twice.

**Coverage target:** dedup path 100 %

---

#### TASK-04 — Fix outbox publisher (mark messages as sent after publish) ✅ DONE

**Goal:** Outbox messages are deleted after successful publish; the table does not grow unbounded.

**Files:**
- `broker/main.go` — outbox goroutine calls `DeleteOutboxMessage` after each successful publish

**Unit tests:**
- `broker/broker_test.go` — mock repo verifies `DeleteOutboxMessage` is called on success, not on
  failure — target 100 %

**Coverage target:** outbox goroutine 100 %

---

### Phase 2 — Architecture (single responsibility per technology)

#### TASK-05 — Replace `BuyBreadStream` polling with PostgreSQL LISTEN/NOTIFY ✅ DONE

**Goal:** Eliminate the 5 s polling loop and the RabbitMQ consumer inside the server.
Server becomes a pure gRPC publisher; PostgreSQL delivers push updates.

**Files:**
- `bakery.sql` — add trigger on `buy_orders` status update that calls `pg_notify('bakery_orders', buy_order_uuid)`
- `data/repository.go` — new `WaitForOrderNotification(ctx, uuid) error` using `pgconn.Conn.WaitForNotification`
- `server/gRPCBakery.go` — `BuyBreadStream` calls `WaitForOrderNotification` instead of the polling loop
- `server/rabbitBakery.go` — remove `getBuyResponse`, `processBreadsBought`, `bread-bought` consumer
- `broker/main.go` — after `UpdateOrderStatus`, call `pg_notify` (or rely on the DB trigger)

**Unit tests:**
- `data/repository_test.go` — test `WaitForOrderNotification` with a mock `pgconn.Conn` that delivers
  a notification — target 90 %
- `server/grpc_test.go` — test `BuyBreadStream` with a mock repo that immediately resolves
  the notification — target 85 %

**Integration test:**
- `server/server_integration_test.go` — full buy flow through Testcontainer Postgres + RabbitMQ;
  assert stream receives result within 15 s without any polling

**Coverage target:** notification path 90 %, BuyBreadStream 85 %

---

#### TASK-06 — Remove in-memory `orders` map; make server stateless ✅ DONE

**Goal:** Server holds no per-request mutable state. Two server replicas behave identically.

**Files:**
- `server/main.go` — remove `orders map[int]*OrderStatus` and `sync.Mutex` from `RabbitMQBakery`
- `server/gRPCBakery.go` — remove all references to `s.RabbitMQBakery.orders`
- `server/rabbitBakery.go` — remove `OrderStatus` struct; remove dispatcher goroutine

**Unit tests:**
- `server/grpc_test.go` — confirm no shared state between two concurrent `BuyBreadStream` calls
  (use race detector: `go test -race`)

**Coverage target:** no regressions; race detector clean

---

#### TASK-07 — Switch RabbitMQ to topic exchanges

**Goal:** Producers publish to named exchanges with routing keys. Adding a new consumer
(audit log, analytics) requires zero changes to producers.

**Files:**
- `server/main.go` — declare `orders` and `inventory` exchanges on startup
- `server/gRPCBakery.go` — publish to `orders` exchange with key `buy.requested`
- `server/gRPCBakery.go` — publish to `inventory` exchange with key `restock.requested`
- `broker/main.go` — bind to `orders` exchange, key `buy.requested`; publish result to
  `orders` exchange, key `buy.processed` or `buy.failed`
- `makers/main.go` — bind to `inventory` exchange, key `restock.requested`
- `docker-compose.yml` — add RabbitMQ definitions JSON for exchange pre-declaration
- `kubernetes/rabbitmq.yaml` — update config

**Unit tests:**
- `server/rabbitmq_test.go` — mock channel verifies correct exchange + routing key on each publish
- `broker/broker_test.go` — same for broker publishes

**Integration test:** Update all Testcontainer-based tests to pre-declare the new exchanges.

**Coverage target:** publish paths 100 %

---

#### TASK-08 — Add worker pool to Broker

**Goal:** Broker processes N orders concurrently (configurable). Removes the arbitrary 34 s sleep.

**Files:**
- `broker/main.go` — replace sequential loop with semaphore-bounded goroutine pool
- `broker/main.go` — remove `time.Sleep(34 * time.Second)`
- `broker/main.go` — read `BROKER_WORKERS` env var (default 3)

**Unit tests:**
- `broker/broker_test.go` — test that N concurrent messages are all processed when pool size = N;
  test that N+1 messages queue correctly — target 90 %

**Integration test:** Send 10 orders concurrently; assert all 10 are processed; assert no oversell.

**Coverage target:** worker pool logic 90 %

---

#### TASK-09 — Add worker pool to Makers

**Goal:** Makers processes M bread production tasks concurrently.

**Files:**
- `makers/main.go` — replace sequential `listenForMakeBread` loop with worker pool
- `makers/main.go` — remove `time.Sleep(1 * time.Second)` inside handler
- `makers/main.go` — read `MAKERS_WORKERS` env var (default 5)

**Unit tests:**
- `makers/makers_test.go` — test concurrent invocations of `AdjustBreadQuantity` via pool;
  assert all are called — target 90 %

**Coverage target:** worker pool logic 90 %

---

#### TASK-10 — Add API Gateway service

**Goal:** Frontend calls REST JSON. Gateway translates to gRPC. JWT lives at HTTP layer only.

**Files (new service):**
- `gateway/cmd/main.go` — HTTP server setup, graceful shutdown
- `gateway/handlers/auth.go` — `POST /api/auth/admin/login`, `POST /api/auth/customer/login`
- `gateway/handlers/inventory.go` — `GET /api/bread`, `GET /api/bread/{id}`
- `gateway/handlers/orders.go` — `POST /api/orders`, `GET /api/orders/{uuid}/status` (SSE)
- `gateway/handlers/admin.go` — `GET /api/admin/dashboard`, CRUD bread, orders, customers
- `gateway/handlers/invoices.go` — `GET /api/invoices`, `GET /api/invoices/{id}`
- `gateway/middleware/auth.go` — JWT cookie validation; injects token into gRPC metadata
- `gateway/middleware/ratelimit.go` — token bucket per IP
- `gateway.dockerfile` — new Dockerfile
- `docker-compose.yml` — add gateway service
- `kubernetes/gateway.yaml` — Deployment + Service

**Update:**
- `frontend/cmd/web/main.go` — replace all direct gRPC calls with HTTP calls to gateway
- `frontend/cmd/web/` — remove all gRPC stub imports

**Unit tests:**
- `gateway/handlers/*_test.go` — each handler tested with `httptest.Server` and a mock gRPC
  client — target 85 %
- `gateway/middleware/auth_test.go` — JWT validation cases — target 100 %
- `gateway/middleware/ratelimit_test.go` — rate limit enforcement — target 90 %

**Integration test:**
- `gateway/gateway_integration_test.go` — Testcontainer: gateway + real server + Postgres;
  full HTTP request → DB assertion

**Coverage target:** handlers 85 %, middleware 95 %

---

### Phase 3 — Reliability and Production Readiness

#### TASK-11 — Graceful shutdown for all services

**Goal:** All services handle `SIGTERM` / `SIGINT`; in-flight work completes before exit.

**Files:**
- `server/main.go` — `signal.NotifyContext`; `grpcServer.GracefulStop()`
- `broker/main.go` — signal handler; drain worker pool; close RabbitMQ channel before exit
- `makers/main.go` — signal handler; drain worker pool
- `gateway/cmd/main.go` — `http.Server.Shutdown(ctx)`
- `frontend/cmd/web/main.go` — `http.Server.Shutdown(ctx)`

**Unit tests:** N/A (shutdown paths are integration-level concerns)

**Integration test:** Send `SIGTERM` to a running service during active processing;
assert in-flight messages complete and are acked.

**Coverage target:** shutdown paths verified in integration suite

---

#### TASK-12 — Shared internal packages (eliminate struct duplication)

**Goal:** `RabbitMQBakery`, `Config`, DB connection helpers live in one place.

**Files (new):**
- `internal/bakery/config.go` — shared `Config` struct
- `internal/bakery/rmq.go` — `RabbitMQBakery`, `NewRabbitMQBakery`, connection helpers
- `internal/db/connect.go` — `connectToDB`, `openDB` with retry logic

**Update:** `server/main.go`, `broker/main.go`, `makers/main.go` — import from `internal/`

**Unit tests:**
- `internal/db/connect_test.go` — retry logic with mock dialer — target 90 %

**Coverage target:** internal packages 90 %

---

#### TASK-13 — gRPC health check + HTTP health endpoints

**Goal:** Kubernetes liveness/readiness probes work correctly.

**Files:**
- `server/main.go` — register `grpc_health_v1.HealthServer`; set service status `SERVING` after
  DB + RabbitMQ connections are established
- `gateway/cmd/main.go` — `GET /healthz` (liveness), `GET /readyz` (checks gRPC connectivity)
- `frontend/cmd/web/main.go` — `GET /healthz`
- `kubernetes/*.yaml` — update liveness/readiness probe definitions

**Unit tests:**
- `gateway/handlers/health_test.go` — readyz returns 503 when gRPC is unreachable — target 100 %

**Coverage target:** health handlers 100 %

---

#### TASK-14 — Input validation on all gRPC handlers

**Goal:** No invalid data reaches the database.

**Files:**
- `server/gRPCAdmin.go` — validate `CreateBread`, `UpdateBread` (name required, price > 0, qty >= 0)
- `server/gRPCAuth.go` — validate `CreateAdminUser` (username, password required)
- `server/gRPCBakery.go` — validate `BuyBread` (customer ID > 0, at least one bread item)

**Unit tests:**
- `server/grpc_validation_test.go` — table-driven tests for each handler's validation cases;
  assert correct `codes.InvalidArgument` returned — target 100 %

**Coverage target:** validation logic 100 %

---

#### TASK-15 — Fix `InsertInvoice` transaction

**Goal:** Invoice header + items inserted atomically; partial state impossible.

**Files:**
- `data/postgres_repository.go` — `InsertInvoice` wrapped in `BeginTx`/`Commit`/`Rollback`

**Unit tests:**
- `data/repository_test.go` — mock that fails on item insert; assert header is not committed

**Coverage target:** transaction path 90 %

---

#### TASK-16 — Structured logging with context fields

**Goal:** Every log line carries `order_uuid`, `customer_id`, `service`, `trace_id` fields.
Debugging a request across services becomes grep-able.

**Files:**
- All `*.go` files using `log.Info(...)` — replace with `log.WithFields(logrus.Fields{...})`
- `internal/logging/logger.go` — shared logger factory with default fields (service name, version)

**Unit tests:** N/A — logging is a cross-cutting concern; verified by inspection

---

### Phase 4 — Buyers Concurrency

#### TASK-17 — Make Buyers run concurrent orders

**Goal:** Buyers submits N orders concurrently (configurable) to demonstrate real load.

**Files:**
- `buyers/main.go` — replace sequential buy loop with `sync.WaitGroup` + goroutine fan-out
- `buyers/main.go` — read `BUYERS_CONCURRENT_ORDERS` env var (default 3)
- `buyers/main.go` — each goroutine creates its own UUID and independent stream

**Unit tests:**
- `buyers/buyers_test.go` — mock gRPC server; assert N concurrent calls are made simultaneously
  (use `sync.WaitGroup` + channel to synchronize test assertions) — target 85 %

**Integration test:** Run buyers against a real server + broker stack; assert N orders all complete.

**Coverage target:** concurrency logic 85 %

---

### Phase 5 — Developer Experience

#### TASK-18 — Makefile for common tasks

**Goal:** `make test`, `make build`, `make up`, `make proto` work out of the box.

**Files:**
- `Makefile` — targets: `build`, `test`, `test-integration`, `up`, `down`, `proto`, `lint`

---

#### TASK-19 — `.env.example` and environment variable audit

**Goal:** New developer can copy `.env.example` and have a working local setup.

**Files:**
- `.env.example` — all variables with placeholder values and comments
- `docs/ARCHITECTURE.md` — update environment variable table

---

#### TASK-20 — Enforce `JWT_SECRET` at startup (no fallback default)

**Goal:** Server fails fast if `JWT_SECRET` is not set; eliminates the hardcoded fallback.

**Files:**
- `server/gRPCAuth.go` — `log.Fatal` if env var empty
- `docker-compose.yml` — set `JWT_SECRET` from `.env`
- `kubernetes/bakery-secrets.yaml` — document required secret

**Unit tests:**
- `server/auth_test.go` — test that empty `JWT_SECRET` triggers fatal log (use `os.Setenv` + recover)

---

## End-to-End Integration Test Suite

All integration tests use **Testcontainers** (Docker). The E2E suite lives in `e2e/`.

### TASK-21 — E2E test: full buy flow

**Scenario:** Buyer submits order → broker processes → server stream receives result.

```
e2e/
├── setup_test.go      # Start Postgres, RabbitMQ, server, broker containers
├── buy_flow_test.go   # BuyBread gRPC call → assert order in DB → assert stream response
├── inventory_test.go  # CheckBreadInventory → buy → assert quantity reduced
└── concurrent_test.go # 5 concurrent buyers → assert 5 orders processed, no oversell
```

**Coverage target:** happy path 100 %, concurrent 1 race-free run

---

### TASK-22 — E2E test: maker restock flow

**Scenario:** Bread stock drops below 10 → server publishes restock → makers increases quantity.

```
e2e/restock_test.go    # Drain bread to < 10 → assert make-bread-order published → assert qty increased
```

---

### TASK-23 — E2E test: API Gateway → frontend flow

**Scenario:** HTTP POST to gateway → gateway calls gRPC → assert DB state.

```
e2e/gateway_test.go    # Admin login → create bread → buy bread → get invoice
```

---

## Coverage Targets Summary

| Package | Unit | Integration |
|---|---|---|
| `data/` | 90 % | 85 % |
| `server/` | 85 % | 80 % |
| `broker/` | 90 % | 85 % |
| `makers/` | 90 % | 80 % |
| `buyers/` | 85 % | 70 % |
| `gateway/` (new) | 85 % | 80 % |
| `internal/` (new) | 90 % | — |
| `e2e/` | — | happy path + race-free |

---

## Execution Order

```
Phase 1 (Correctness)  → TASK-01 through TASK-04
Phase 2 (Architecture) → TASK-05 through TASK-10
Phase 3 (Reliability)  → TASK-11 through TASK-16
Phase 4 (Concurrency)  → TASK-17
Phase 5 (DX)           → TASK-18 through TASK-20
E2E Suite              → TASK-21 through TASK-23
```

Each task is independently mergeable. Start every task from a clean branch off `main`.
Run `go test -race ./...` before marking any task done.
