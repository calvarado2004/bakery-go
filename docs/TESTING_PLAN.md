# Bakery Service — Testing Plan

## Table of Contents

1. [Philosophy and Goals](#philosophy-and-goals)
2. [Test Pyramid](#test-pyramid)
3. [Unit Tests](#unit-tests)
   - [Data Layer](#data-layer-unit-tests)
   - [gRPC Services](#grpc-service-unit-tests)
   - [Authentication](#authentication-unit-tests)
   - [Broker Logic](#broker-logic-unit-tests)
   - [Frontend Handlers](#frontend-handler-unit-tests)
4. [Integration Tests](#integration-tests)
   - [gRPC + Database Integration](#grpc--database-integration)
   - [Messaging Integration (RabbitMQ)](#messaging-integration)
5. [End-to-End Tests](#end-to-end-tests)
   - [Test Infrastructure](#e2e-test-infrastructure)
   - [E2E Scenarios](#e2e-scenarios)
6. [Test Tooling](#test-tooling)
7. [Test Directory Layout](#test-directory-layout)
8. [CI Pipeline Integration](#ci-pipeline-integration)
9. [Mock Strategy](#mock-strategy)
10. [Coverage Targets](#coverage-targets)

---

## Philosophy and Goals

The testing strategy for the Bakery Service is guided by the following principles:

- **Testability over retrofitting.** All new code must be written with testability as a first-class concern — interfaces, dependency injection, and small functions.
- **Speed at the base, confidence at the top.** Unit tests should be fast and numerous; integration and E2E tests should be fewer but comprehensive.
- **No shared mutable state.** Each test must be self-contained and must not depend on execution order.
- **Real infrastructure for integration and E2E.** Integration tests use real PostgreSQL and RabbitMQ instances (via Docker containers managed by `testcontainers-go`). No mocked infrastructure at the integration level.
- **Explicit, readable assertions.** Use `testify/assert` and `testify/require` for clear failure messages.

---

## Test Pyramid

```
                        ┌──────────────────────────────┐
                        │      End-to-End Tests        │  ← Fewest tests, highest confidence
                        │   (Full stack via browser /  │    (~5–10 scenarios)
                        │    CLI + live services)       │
                        └──────────────┬───────────────┘
                  ┌────────────────────┴──────────────────────┐
                  │           Integration Tests               │  ← Medium count
                  │   (Service + DB + RabbitMQ, real infra)   │    (~30–50 tests)
                  └────────────────────┬──────────────────────┘
       ┌────────────────────────────────┴──────────────────────────────────┐
       │                        Unit Tests                                 │  ← Most tests
       │  (Single functions/methods, mocked dependencies, no I/O)          │    (~100–200 tests)
       └───────────────────────────────────────────────────────────────────┘
```

---

## Unit Tests

Unit tests live alongside the source file they test, following Go conventions: `foo_test.go` in the same package. All external dependencies (database, RabbitMQ) are replaced with mocks or stubs.

### Data Layer Unit Tests

**File:** `data/repository_test.go`

The existing `TestRepository` (in `data/test_models.go`) is a static mock. It must be evolved into a **configurable mock** that can simulate both happy-path and error conditions.

#### Recommended: Interface-based Mock with `testify/mock`

```go
// data/mocks/repository_mock.go
type MockRepository struct {
    mock.Mock
}

func (m *MockRepository) GetBreadByID(id int) (*Bread, error) {
    args := m.Called(id)
    return args.Get(0).(*Bread), args.Error(1)
}
// ... all interface methods
```

#### Test Cases

| Test Name | Method | Scenario | Expected |
|-----------|--------|----------|----------|
| `TestGetBreadByID_Found` | `GetBreadByID` | Bread exists | Returns correct bread |
| `TestGetBreadByID_NotFound` | `GetBreadByID` | ID does not exist | Returns `nil, nil` |
| `TestInsertBuyOrder_Success` | `InsertBuyOrder` | Valid order | Returns new ID, no error |
| `TestInsertBuyOrder_Error` | `InsertBuyOrder` | DB error | Returns `0, error` |
| `TestAdjustBreadQuantity_Decrement` | `AdjustBreadQuantity` | Subtract 2 from qty 10 | qty = 8 |
| `TestAdjustBreadQuantity_NegativeBlock` | `AdjustBreadQuantity` | Subtract 15 from qty 10 | Error (constraint violation) |
| `TestPasswordMatches_Correct` | `PasswordMatches` | Valid password | Returns `true, nil` |
| `TestPasswordMatches_Wrong` | `PasswordMatches` | Wrong password | Returns `false, nil` |
| `TestGetUnprocessedOutbox_Empty` | `GetUnprocessedOutboxMessages` | No pending msgs | Returns empty slice |
| `TestGetDashboardStats` | `GetDashboardStats` | Populated DB | Returns correct aggregates |

---

### gRPC Service Unit Tests

Each gRPC server struct has its repository injected via the `RabbitMQBakery.Repo` field. Tests inject a `MockRepository` and a mock AMQP channel.

#### AdminService — `server/gRPCAdmin_test.go`

| Test Name | Method | Scenario | Expected |
|-----------|--------|----------|----------|
| `TestGetDashboardStats_Success` | `GetDashboardStats` | Repo returns stats | Returns correct proto |
| `TestGetDashboardStats_RepoError` | `GetDashboardStats` | Repo returns error | Returns `codes.Internal` |
| `TestCreateBread_Success` | `CreateBread` | Valid request | Returns created bread proto |
| `TestCreateBread_EmptyName` | `CreateBread` | Name is empty string | Returns `codes.InvalidArgument` |
| `TestDeleteBread_Success` | `DeleteBread` | Valid ID | Returns `Empty`, no error |
| `TestDeleteBread_NotFound` | `DeleteBread` | ID does not exist | Returns `codes.NotFound` |
| `TestUpdateOrderStatus_CompletedGeneratesInvoice` | `UpdateOrderStatus` | Status = "completed" | Calls `InsertInvoice` |
| `TestGetLowStockAlerts_ReturnsCorrectItems` | `GetLowStockAlerts` | 3 items below 10 qty | Returns list of 3 |
| `TestGetCustomerOrders_Success` | `GetCustomerOrders` | Valid customer ID | Returns customer + orders |

#### BuyBreadServer — `server/gRPCBakery_test.go`

| Test Name | Scenario | Expected |
|-----------|----------|----------|
| `TestBuyBread_PublishesMessage` | Valid order with inventory | Publishes to buy-bread-order |
| `TestBuyBread_InsertOrderBeforePublish` | Valid order | `InsertBuyOrder` called before publish |
| `TestBuyBread_OutboxInserted` | Valid order | `InsertOutboxMessage` called |
| `TestCheckBreadInventory_ReturnsAll` | Available bread in repo | Returns all items in response |
| `TestCheckBreadInventory_EmptyDB` | No bread available | Returns empty `BreadResponse` |

#### AuthService — `server/gRPCAuth_test.go`

| Test Name | Scenario | Expected |
|-----------|----------|----------|
| `TestAdminLogin_Success` | Valid credentials | Returns JWT token |
| `TestAdminLogin_WrongPassword` | Incorrect password | Returns `codes.Internal` (or `Unauthenticated`) |
| `TestAdminLogin_UserNotFound` | Username not in DB | Returns error |
| `TestCustomerLogin_Success` | Valid email/password | Returns JWT token |
| `TestValidateToken_Valid` | Unexpired token | Returns `valid = true` |
| `TestValidateToken_Expired` | Expired token | Returns `valid = false` |
| `TestValidateToken_WrongSecret` | Token signed with different key | Returns `valid = false` |
| `TestCreateAdminUser_HashesPassword` | Any input | Stored password is bcrypt hash |

---

### Authentication Unit Tests

**File:** `frontend/cmd/web/auth_test.go`

| Test Name | Scenario | Expected |
|-----------|----------|----------|
| `TestValidateToken_AdminType` | Valid admin JWT | Returns claims, no error |
| `TestValidateToken_CustomerType` | Valid customer JWT | Returns claims, no error |
| `TestValidateToken_WrongType` | Customer token on admin route | Returns error |
| `TestValidateToken_Malformed` | Random string | Returns error |
| `TestRequireAdminAuth_NoToken` | No cookie set | Redirects to `/admin/login` |
| `TestRequireAdminAuth_ValidToken` | Valid cookie | Calls wrapped handler |
| `TestRequireCustomerAuth_NoToken` | No cookie set | Redirects to `/portal/login` |

---

### Broker Logic Unit Tests

**File:** `broker/main_test.go`

| Test Name | Scenario | Expected |
|-----------|----------|----------|
| `TestPerformBuyBread_AllItemsAvailable` | All bread quantities sufficient | Decrements quantities, sets "Processed" |
| `TestPerformBuyBread_OneItemInsufficient` | One bread out of stock | Sets "Failed", no quantity changes |
| `TestPerformBuyBread_PublishesBreadBought` | Successful order | Publishes to bread-bought queue |
| `TestPerformBuyBread_DBError` | `GetBreadByID` returns error | Nacks message, returns error |
| `TestPerformBuyBread_Idempotency` | Same order processed twice | Second call detects already-processed, skips |

---

### Frontend Handler Unit Tests

**File:** `frontend/cmd/web/admin_handlers_test.go`, `auth_handlers_test.go`

Use `net/http/httptest` to create test servers and `httptest.NewRecorder` for response capture.

| Test Name | Handler | Scenario | Expected |
|-----------|---------|----------|----------|
| `TestAdminDashboardHandler_Authenticated` | `AdminDashboardHandler` | Valid admin cookie | Returns `200 OK` |
| `TestAdminDashboardHandler_Unauthenticated` | `AdminDashboardHandler` | No cookie | Redirects `302` to `/admin/login` |
| `TestAdminBreadCreateHandler_ValidForm` | `AdminBreadCreateHandler` | Valid form data | Calls gRPC `CreateBread`, redirects |
| `TestAdminBreadCreateHandler_EmptyName` | `AdminBreadCreateHandler` | Empty name field | Returns `400` or re-renders form with error |
| `TestAdminLoginHandler_ValidCredentials` | `AdminLoginHandler` | Correct username/password | Sets `admin_token` cookie, redirects to `/admin` |
| `TestAdminLoginHandler_InvalidCredentials` | `AdminLoginHandler` | Wrong password | Re-renders login with error message |
| `TestCustomerPortalDashboard_Authenticated` | `CustomerPortalDashboardHandler` | Valid customer cookie | Returns `200 OK` |
| `TestCustomerInvoiceDetail_WrongCustomer` | `CustomerInvoiceDetailHandler` | Invoice belongs to other customer | Returns `403` or redirects |

---

## Integration Tests

Integration tests verify that multiple components work correctly together using real infrastructure. They are placed in `tests/integration/` and use build tags to prevent them from running during normal `go test ./...`.

```go
//go:build integration
```

Run with:
```bash
go test -tags=integration ./tests/integration/... -v
```

### Infrastructure via `testcontainers-go`

Each integration test suite spins up fresh Docker containers:

```go
import "github.com/testcontainers/testcontainers-go"

func setupPostgres(t *testing.T) *pgx.Conn {
    container, _ := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "postgres:15-alpine",
            ExposedPorts: []string{"5432/tcp"},
            Env: map[string]string{
                "POSTGRES_DB":       "bakery_test",
                "POSTGRES_USER":     "test",
                "POSTGRES_PASSWORD": "test",
            },
            WaitingFor: wait.ForListeningPort("5432/tcp"),
        },
        Started: true,
    })
    // Apply schema
    applySchema(conn, "../../bakery.sql")
    return conn
}

func setupRabbitMQ(t *testing.T) *amqp.Connection {
    container, _ := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "rabbitmq:3-alpine",
            ExposedPorts: []string{"5672/tcp"},
            WaitingFor:   wait.ForListeningPort("5672/tcp"),
        },
        Started: true,
    })
    // ...
}
```

### gRPC + Database Integration

**File:** `tests/integration/server_test.go`

| Test Name | Description |
|-----------|-------------|
| `TestIntegration_CreateAndGetBread` | Creates a bread via `AdminService.CreateBread`, fetches via `AdminService.GetBreadById`, asserts equality |
| `TestIntegration_BreadCRUD` | Full create → update → delete cycle; verifies DB state at each step |
| `TestIntegration_AdminLogin_JWT` | Inserts admin user, calls `AuthService.AdminLogin`, verifies returned JWT contains correct claims |
| `TestIntegration_CustomerLogin_JWT` | Same for customer login flow |
| `TestIntegration_InsertInvoice_Idempotent` | Calls `InvoiceService.CreateInvoice` twice for the same order; verifies only one invoice is created |
| `TestIntegration_GetDashboardStats` | Inserts known data, calls `GetDashboardStats`, verifies counts match |
| `TestIntegration_UpdateOrderStatus_GeneratesInvoice` | Updates order to "completed", verifies invoice row in DB |
| `TestIntegration_GetCustomerOrders_Authorization` | Customer A cannot retrieve Customer B's orders |

### Messaging Integration

**File:** `tests/integration/messaging_test.go`

| Test Name | Description |
|-----------|-------------|
| `TestIntegration_BuyBread_PublishToQueue` | Calls `BuyBread`, verifies `buy-bread-order` queue receives a correctly-formed message |
| `TestIntegration_Broker_ProcessOrder_Success` | Publishes a buy order to `buy-bread-order`, broker processes it, verifies `bread-bought` is published |
| `TestIntegration_Broker_ProcessOrder_InsufficientInventory` | Publishes order for more bread than in stock, verifies status = "Failed" |
| `TestIntegration_InventoryReplenishment` | Inserts bread with qty 5, triggers `checkBread`, verifies `make-bread-order` published |
| `TestIntegration_Makers_IncrementsQuantity` | Publishes to `make-bread-order`, Makers consumes, verifies DB quantity increased |
| `TestIntegration_OutboxRelay` | Simulates failed initial publish (mark as unsent), trigger outbox job, verify message eventually published |

---

## End-to-End Tests

E2E tests verify the entire system behaves correctly from the perspective of an external user (browser, API client). They run against a fully deployed environment — ideally the local `docker-compose` stack or a dedicated test Kubernetes namespace.

### E2E Test Infrastructure

```
tests/e2e/
├── setup_test.go          # Compose/K8s bootstrap, wait for readiness
├── admin_e2e_test.go      # Admin portal scenarios
├── customer_e2e_test.go   # Customer portal scenarios
├── purchase_e2e_test.go   # End-to-end purchase flow
└── helpers/
    ├── http.go            # HTTP client helpers
    ├── grpc.go            # gRPC client helpers
    └── db.go              # Direct DB assertion helpers
```

Build tag: `//go:build e2e`

Run with:
```bash
docker-compose up -d
go test -tags=e2e ./tests/e2e/... -v -timeout 5m
```

### Readiness Check

Before any E2E test runs, the suite waits for all services to be healthy:

```go
func waitForService(url string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        resp, err := http.Get(url + "/healthz")
        if err == nil && resp.StatusCode == 200 {
            return nil
        }
        time.Sleep(2 * time.Second)
    }
    return fmt.Errorf("service at %s did not become healthy in %s", url, timeout)
}
```

> **Prerequisite:** All services must expose a `/healthz` endpoint (see `docs/IMPROVEMENTS.md`).

---

### E2E Scenarios

#### Scenario 1: Admin Full Bread Lifecycle

```
1. POST /admin/login           → credentials valid → admin_token cookie set
2. GET /admin/bread            → bread list rendered with seeded items
3. POST /admin/bread/create    → new bread "Test Loaf" created
4. GET /admin/bread/{id}/edit  → edit form pre-populated
5. POST /admin/bread/{id}/update → bread name updated
6. GET /admin/bread            → updated bread visible in list
7. POST /admin/bread/{id}/delete → bread removed
8. GET /admin/bread            → deleted bread no longer visible
```

#### Scenario 2: Full Purchase Flow

```
1. Buyers service sends BuyBread gRPC call
   - Bread: [Sourdough x2, Baguette x1]
   - Customer ID: 1
2. Assert: buy_order row inserted in DB with status "Pending"
3. Assert: buy-bread-order queue contains the message
4. Broker processes the order
5. Assert: bread quantities decremented in DB
6. Assert: bread-bought queue contains confirmation
7. Assert: buy_order status = "Processed"
8. Admin marks order "completed"
9. Assert: invoice row created in DB
10. Assert: invoice items match order details
```

#### Scenario 3: Customer Portal Order Visibility

```
1. POST /portal/login           → customer credentials valid → cookie set
2. GET /portal                  → dashboard renders with customer name
3. GET /portal/orders           → order list shows orders for this customer only
4. GET /portal/orders/{id}      → order detail shows correct line items
5. GET /portal/invoices         → invoice list for this customer
6. GET /portal/invoices/{id}    → invoice detail matches expected totals
7. Attempt GET /portal/invoices/{other_customer_invoice_id} → 403 or redirect
```

#### Scenario 4: Inventory Replenishment

```
1. Update bread quantity to 5 (below threshold)
2. Wait for checkBread goroutine tick (≤ 30 seconds)
3. Assert: make-bread-order queue receives message for that bread
4. Makers consumes the message
5. Assert: bread quantity increased in DB
```

#### Scenario 5: Admin Authentication Guard

```
1. GET /admin → redirects to /admin/login (no cookie)
2. POST /admin/login with wrong password → stays on login page with error
3. POST /admin/login with correct credentials → redirected to /admin
4. GET /admin/logout → cookie cleared
5. GET /admin → redirected to /admin/login again
```

#### Scenario 6: Live Inventory Stream

```
1. Connect to GET /stream (SSE)
2. Assert: receives "bread" events within 20 seconds
3. Admin updates bread quantity
4. Assert: subsequent SSE event reflects new quantity
```

#### Scenario 7: Tax Rate and Invoice Calculation

```
1. Create order with known bread prices
2. Mark order "completed" via admin portal
3. Assert: invoice subtotal = sum(quantity * price)
4. Assert: invoice tax = subtotal * 0.10
5. Assert: invoice total = subtotal + tax
6. Assert: due_date = created_at + 30 days
```

---

## Test Tooling

| Tool | Purpose | Import |
|------|---------|--------|
| `testing` | Go standard test runner | stdlib |
| `testify/assert` | Non-fatal assertions | `github.com/stretchr/testify/assert` |
| `testify/require` | Fatal assertions (stops test on failure) | `github.com/stretchr/testify/require` |
| `testify/mock` | Interface mocking | `github.com/stretchr/testify/mock` |
| `testcontainers-go` | Docker-based test infrastructure | `github.com/testcontainers/testcontainers-go` |
| `net/http/httptest` | HTTP server/recorder for handler tests | stdlib |
| `google.golang.org/grpc/test/bufconn` | In-memory gRPC transport for unit tests | grpc |
| `gomock` (optional alternative) | Code-generated mocks from interfaces | `go.uber.org/mock/gomock` |

### In-Memory gRPC Testing with `bufconn`

Instead of spinning up a real TCP listener for gRPC unit tests, use `bufconn`:

```go
import "google.golang.org/grpc/test/bufconn"

const bufSize = 1024 * 1024

func startTestServer(t *testing.T, repo data.Repository) *grpc.ClientConn {
    lis := bufconn.Listen(bufSize)
    s := grpc.NewServer()
    proto.RegisterAdminServiceServer(s, &AdminServiceServer{bakery: &RabbitMQBakery{Repo: repo}})
    go s.Serve(lis)

    conn, _ := grpc.DialContext(ctx, "bufnet",
        grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
            return lis.Dial()
        }),
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    return conn
}
```

---

## Test Directory Layout

```
bakery-go/
├── data/
│   ├── repository_test.go       # Unit tests for PostgresRepository (requires DB)
│   └── mocks/
│       └── repository_mock.go   # testify/mock implementation of Repository
├── server/
│   ├── gRPCAdmin_test.go
│   ├── gRPCAuth_test.go
│   ├── gRPCBakery_test.go
│   └── gRPCInvoice_test.go
├── broker/
│   └── main_test.go
├── makers/
│   └── main_test.go
├── frontend/
│   └── cmd/web/
│       ├── admin_handlers_test.go
│       └── auth_handlers_test.go
└── tests/
    ├── integration/
    │   ├── server_test.go         # gRPC + DB integration
    │   ├── messaging_test.go      # RabbitMQ integration
    │   └── helpers/
    │       └── containers.go      # testcontainers setup functions
    └── e2e/
        ├── setup_test.go
        ├── admin_e2e_test.go
        ├── customer_e2e_test.go
        ├── purchase_e2e_test.go
        └── helpers/
            ├── http.go
            ├── grpc.go
            └── db.go
```

---

## CI Pipeline Integration

Recommended GitHub Actions workflow:

```yaml
# .github/workflows/test.yml
jobs:
  unit:
    name: Unit Tests
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go test ./... -count=1 -race -timeout 60s

  integration:
    name: Integration Tests
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15-alpine
        env: { POSTGRES_DB: bakery_test, POSTGRES_USER: test, POSTGRES_PASSWORD: test }
        ports: ['5432:5432']
      rabbitmq:
        image: rabbitmq:3-alpine
        ports: ['5672:5672']
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go test -tags=integration ./tests/integration/... -v -timeout 5m
        env:
          DSN: postgres://test:test@localhost:5432/bakery_test
          RABBITMQ_SERVICE_ADDR: amqp://guest:guest@localhost:5672/

  e2e:
    name: End-to-End Tests
    runs-on: ubuntu-latest
    needs: [unit, integration]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: docker-compose up -d --build
      - run: go test -tags=e2e ./tests/e2e/... -v -timeout 10m
      - run: docker-compose down
```

---

## Mock Strategy

| Layer | Mock Type | Rationale |
|-------|-----------|-----------|
| `data.Repository` in gRPC unit tests | `testify/mock` or `gomock` | Control exact return values and errors |
| RabbitMQ channel in unit tests | Manual stub (`amqp.Channel` wrapper interface) | Avoid AMQP connection in unit tests |
| gRPC clients in frontend tests | `testify/mock` or `bufconn` server | Avoid running real gRPC server |
| PostgreSQL in integration tests | Real container via `testcontainers-go` | Verify actual SQL correctness |
| RabbitMQ in integration tests | Real container via `testcontainers-go` | Verify actual AMQP behaviour |

> **Note:** To make RabbitMQ channel mockable, a thin wrapper interface must be introduced:
> ```go
> type AMQPChannel interface {
>     PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
>     Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
>     QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
> }
> ```

---

## Coverage Targets

| Package | Minimum Coverage Target | Priority |
|---------|------------------------|----------|
| `data/` | 80% | High — critical business logic |
| `server/` | 75% | High — all service handlers |
| `broker/` | 85% | High — financial-critical purchase processing |
| `makers/` | 70% | Medium |
| `frontend/cmd/web/` | 65% | Medium — UI logic |
| `proto/` (generated) | Excluded | Generated code — do not test |

Run coverage report:
```bash
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -html=coverage.out -o coverage.html
```
