# RabbitMQ Refactoring Plan: In-Process Test Harness

## Goal
Refactor RabbitMQ integration to enable in-process integration and E2E tests, bringing coverage above 85%.

## Current Problems
- Makers, broker, and server use global state (`rabbitmqConnection`, `rabbitmqChannel`, `rabbitMQAddress`)
- Services block on `main()` with infinite `select {}` loops — impossible to test in-process
- `rabbitmq.Dial()` called directly throughout business logic — cannot be swapped for testing
- Integration tests are either unit-only (mocked) or skip entirely
- No end-to-end test covering full message flows (buy→match→settle, replenish→make→update)

---

## Phase 1: Abstraction Layer

### 1.1 `server/rabbitmq_dialer.go` — RabbitMQDialer interface
```go
type RabbitMQDialer interface {
    Dial() (*amqp.Connection, error)
}

type realRabbitMQDialer struct{}

func (r realRabbitMQDialer) Dial() (*amqp.Connection, error) {
    return amqp.Dial(os.Getenv("RABBITMQ_SERVICE_ADDR"))
}
```

### 1.2 Inject `RabbitMQDialer` into services
- `RabbitMQBakery` (server): add `RabbitMQDialer` field
- `MakersService` (makers): add `RabbitMQDialer` field
- `BrokerService` (broker): add `RabbitMQDialer` field

Replace all direct `rabbitmq.Dial()` calls with `dialer.Dial()`.

---

## Phase 2: Refactor Makers Service

**File**: `maker/main.go` → instance-based `MakersService` struct

### Changes
- Replace global `rabbitmqConnection`/`rabbitmqChannel` with instance fields
- Create `MakersService` struct with `RabbitMQDialer`
- Add `Start(ctx context.Context, wg *sync.WaitGroup)` method
- Add `Stop()` method with context-based cancellation
- Export `ProcessMakeBreadMessage(body []byte) error` for test verification
- Keep `main()` calling `svc.Start()` for production use

### Before (blocking main + globals):
```go
var rabbitmqConnection *rabbitmq.Connection
var rabbitmqChannel *rabbitmq.Channel

func main() {
    // ... infinite loop
    <-sigCh
}
```

### After (testable instance):
```go
type MakersService struct {
    dialer    RabbitMQDialer
    rmqAddr   string
    stopCh    chan struct{}
}

func (s *MakersService) Start(ctx context.Context, wg *sync.WaitGroup) {
    // ... start consumer loop with ctx cancellation
}

func (s *MakersService) Stop() {
    close(s.stopCh)
}
```

---

## Phase 3: Refactor Broker Service

**File**: `broker/main.go` → instance-based `BrokerService` struct

### Changes
- Create `BrokerService` struct with `RabbitMQDialer`, `grpcConn`, `brokerClient`
- Add `Start(ctx context.Context, wg *sync.WaitGroup)` method
- Add `Stop()` method with context-based cancellation
- Export `processOneOrder` and `processMatchingBatch` for unit testing
- Keep `main()` calling `svc.Start()` for production use

---

## Phase 3b: Refactor Server RabbitMQBakery

**File**: `server/rabbitBakery.go`

### Changes
- Replace all `rabbitmq.Dial(rabbit.rabbitmqURL)` calls with `rabbit.rabbitmqDialer.Dial()`
- `init()`: use dialer
- `checkBread()`: use dialer for make-bread publish
- `listenForBreadMade()`: use dialer

---

## Phase 4: In-Process TestHarness

**File**: `testutils/harness.go`

### Design
```go
type TestHarness struct {
    t       *testing.T
    pgConn  *sql.DB
    rmqConn *amqp.Connection
    rmqURL  string
    grpcURL string

    // Service instances (in-process)
    server *RabbitMQBakery
    broker *BrokerService
    makers *MakersService

    cancel context.CancelFunc
    ctx    context.Context
}

func NewTestHarness(t *testing.T) *TestHarness { ... }
func (h *TestHarness) WaitForReady(timeout time.Duration) error { ... }
func (h *TestHarness) Cleanup() { ... }
func (h *TestHarness) GRPCConn() *grpc.ClientConn { ... }
func (h *TestHarness) RabbitMQConn() *amqp.Connection { ... }
func (h *TestHarness) DB() *sql.DB { ... }
```

### Lifecycle
1. `NewTestHarness(t)` — starts Postgres + RabbitMQ via Docker (if not running)
2. `WaitForReady(30s)` — waits for Postgres, RabbitMQ, gRPC, and all services to be ready
3. Tests run against the in-process services
4. `Cleanup()` — stops all services, stops Docker containers if we started them

### What it provides
- **Real** RabbitMQ + **real** PostgreSQL (shared Docker containers)
- **Real** server, broker, makers running in-process (goroutines)
- gRPC client connection to server
- RabbitMQ connection for publishing test messages
- Direct DB access for assertions
- Clean isolation between tests (DB cleanup, queue purge)

---

## Phase 5: Integration + E2E Tests

### 5a. E2E: Full Buy Order Flow
**File**: `server/buy_order_e2e_test.go`

Flow:
1. Publish `buy-bread-order` message to RabbitMQ
2. Broker consumes, calls gRPC `ReportOrder` (dedup + insert)
3. Broker matches batch (priority ordering)
4. Broker publishes `bread-bought` to RabbitMQ
5. Server's `SettlementDispatcher` routes to waiting gRPC stream
6. Buyer receives settlement via `BuyBreadStream`

Assertions:
- Order in `buy_order` table
- Bread quantity deducted
- Settlement delivered to stream within timeout
- Order status = "processed" or "partially_processed"

### 5b. E2E: Maker Replenishment Flow
**File**: `server/maker_flow_e2e_test.go`

Flow:
1. Set bread quantity to 5 (below threshold of 10)
2. Trigger `checkBread()` manually
3. Server publishes `make-bread-order` to RabbitMQ
4. Makers consumes, simulates baking, publishes `bread-made`
5. Server consumes `bread-made`, updates inventory

Assertions:
- `pending_make_orders` row created
- `bread-made` message consumed
- Bread quantity increased

### 5c. E2E: Settlement Dispatcher Delivery
**File**: `server/settlement_e2e_test.go`

Flow:
1. Start `BuyBreadStream` (creates waiter in SettlementDispatcher)
2. Publish `buy-bread-order` to RabbitMQ
3. Broker matches, publishes `bread-bought`
4. SettlementDispatcher routes to stream

Assertions:
- Stream receives order within timeout
- Order matches published order

### 5d. Integration: Broker Matching
**File**: `broker/broker_integration_test.go`

Tests:
- Batch processing (multiple orders matched in single batch)
- Priority ordering (highest bid wins)
- FIFO within same bid price
- Partial fulfillment (some items available, some not)
- Insufficient stock (all items unavailable)
- Dedup (same UUID rejected)
- Skip unavailable items flag

### 5e. Rewrite Makers Integration Tests
**File**: `makers/integration_test.go`

Replace skipped tests with harness-based tests:
- Publish `make-bread-order` → Makers consumes → `bread-made` published
- Verify inventory update
- Multi-message flow
- Invalid JSON rejection
- Zero quantity handling

---

## Phase 6: Cleanup

- Remove stale `broker/integration_test.go` comment-only file
- Update skipped makers tests to use harness
- Update `docker-compose.yml` if needed (health checks, etc.)
- Verify all tests pass: `go test ./...`
- Check coverage: `go test -cover ./...`

---

## Files to Modify

| File | Action |
|------|--------|
| `server/rabbitmq_dialer.go` | **NEW** — RabbitMQDialer interface + impl |
| `server/main.go` | Add `RabbitMQDialer` to `RabbitMQBakery`, use it |
| `server/rabbitBakery.go` | Replace `rabbitmq.Dial()` with dialer |
| `makers/main.go` | Refactor to instance-based `MakersService` |
| `broker/main.go` | Refactor to instance-based `BrokerService` |
| `broker/matching.go` | Minor: use broker's dialer for `bread-bought` publish |
| `testutils/harness.go` | **NEW** — TestHarness |
| `server/buy_order_e2e_test.go` | **NEW** — Full buy order E2E |
| `server/maker_flow_e2e_test.go` | **NEW** — Maker replenishment E2E |
| `server/settlement_e2e_test.go` | **NEW** — Settlement dispatcher E2E |
| `broker/broker_integration_test.go` | **NEW** — Broker matching integration |
| `makers/integration_test.go` | **REWRITE** — Use harness |
| `broker/integration_test.go` | **DELETE** — Stale comment only |

## Files to Keep
- `server/broker_service_integration_test.go` — Already good (real gRPC + DB)
- `server/broker_service_test.go` — Unit tests, already good
- `server/settlement_dispatcher_test.go` — Unit tests, already good
- `broker/broker_test.go` — Unit tests, already good
- `testutils/fixtures.go` — Used by harness for infrastructure management
- `testutils/db_helper.go` — Used by tests for DB cleanup
