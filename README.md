# Bakery Service

![Bakery Service](./images/bakery-frontend.png)

The Bakery Service is a gRPC server written in Go that manages several operations for a virtual bakery shop. The server provides functionality for checking inventory, making bread, buying bread, and removing old bread. It uses RabbitMQ for asynchronous message passing and PostgreSQL for persistent data storage.

## Author
- [Carlos Alvarado Martínez](https://calvarado04.com)

## Table of Contents
- [Dependencies](#dependencies)
- [Setup](#setup)
- [Architecture](#architecture)
- [Services](#services)
- [gRPC Endpoints](#grpc-endpoints)
- [Recent Changes](#recent-changes)
- [Testing](#testing)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## Dependencies

1. **Go 1.25**: The language used to develop this application.
2. **gRPC / protobuf**: Used for all inter-service communication.
3. **RabbitMQ (amqp091)**: Asynchronous message queuing for bread orders and notifications.
4. **PostgreSQL (pgx/v4)**: Persistent storage for bread inventory, orders, customers, invoices.
5. **JWT (golang-jwt/jwt v5)**: Auth tokens for admin and customer logins.
6. **bcrypt**: Password hashing for admin and customer accounts.

## Setup

Set the following environment variables before running any service:

| Variable | Description | Example |
|---|---|---|
| `BAKERY_SERVICE_ADDR` | gRPC server address | `localhost:50051` |
| `RABBITMQ_SERVICE_ADDR` | RabbitMQ AMQP URL | `amqp://guest:guest@localhost:5672/` |
| `DSN` | PostgreSQL connection string | `host=localhost user=postgres ...` |
| `JWT_SECRET` | Secret key for JWT signing | `change-in-production` |

Install dependencies:

```bash
go mod download
```

Regenerate protobuf (if `proto/bread.proto` changes):

```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/bread.proto
```

Build and push container images:

### Quick build (local architecture)

```bash
docker build . -t docker.io/calvarado2004/bakery-go-buyers   -f buyers.dockerfile   && docker push docker.io/calvarado2004/bakery-go-buyers
docker build . -t docker.io/calvarado2004/bakery-go-frontend -f frontend.dockerfile && docker push docker.io/calvarado2004/bakery-go-frontend
docker build . -t docker.io/calvarado2004/bakery-go-makers   -f makers.dockerfile   && docker push docker.io/calvarado2004/bakery-go-makers
docker build . -t docker.io/calvarado2004/bakery-go-server   -f server.dockerfile   && docker push docker.io/calvarado2004/bakery-go-server
docker build . -t docker.io/calvarado2004/bakery-go-broker   -f broker.dockerfile   && docker push docker.io/calvarado2004/bakery-go-broker
```

### Production build (linux/amd64 multi-arch)

```bash
docker buildx build --platform linux/amd64 -t docker.io/calvarado2004/bakery-go-buyers   -f buyers.dockerfile   --push .
docker buildx build --platform linux/amd64 -t docker.io/calvarado2004/bakery-go-frontend -f frontend.dockerfile --push .
docker buildx build --platform linux/amd64 -t docker.io/calvarado2004/bakery-go-makers   -f makers.dockerfile   --push .
docker buildx build --platform linux/amd64 -t docker.io/calvarado2004/bakery-go-server   -f server.dockerfile   --push .
docker buildx build --platform linux/amd64 -t docker.io/calvarado2004/bakery-go-broker   -f broker.dockerfile   --push .
```

## Architecture

```
┌──────────┐  gRPC  ┌──────────┐  AMQP  ┌──────────┐
│  buyers  │───────▶│  server  │───────▶│ RabbitMQ │
└──────────┘        └──────────┘        └────┬─────┘
                         │                   │
                         │ PostgreSQL    buy-bread-order
                         ▼                   ▼
                    ┌────────┐         ┌──────────┐
                    │  data  │         │  broker  │
                    └────────┘         └──────────┘
                                            │
                                     make-bread-order
                                            ▼
                                       ┌────────┐
                                       │ makers │
                                       └────────┘
```

**Message flow:**
1. `buyers` sends a `BuyBread` gRPC request to `server`.
2. `server` publishes the order to the `buy-bread-order` RabbitMQ queue.
3. `broker` consumes the queue, checks stock, adjusts quantities, updates the order status, and publishes the result to `bread-bought`.
4. `server` streams the result back to `buyers` via `BuyBreadStream`.
5. When stock drops below 10 units, `server` publishes to `make-bread-order`.
6. `makers` consumes the queue and restocks the database.

## Services

| Service | Package | Description |
|---|---|---|
| `server` | `server/` | gRPC server — all RPC endpoints, auth, admin, invoices |
| `broker` | `broker/` | Processes buy orders from RabbitMQ; updates DB, publishes results |
| `makers` | `makers/` | Restocks bread inventory from RabbitMQ make-bread-order queue |
| `buyers` | `buyers/` | gRPC client that buys bread and streams the order status |
| `frontend` | `frontend/` | HTTP web frontend with admin panel and customer portal |
| `data` | `data/` | PostgreSQL repository implementation and data models |

## gRPC Endpoints

### BuyBread
- `BuyBread` — publishes a buy order and returns immediately
- `BuyBreadStream` — streams the settled order result back to the client (retries up to 20×5s = 100 s)

### CheckInventory
- `CheckBreadInventory` — returns current bread stock snapshot
- `CheckBreadInventoryStream` — streams inventory every 15 seconds

### MakeBread
- `BakeBread` — enqueues a bread-making request
- `SendBreadToBakery` — moves baked bread into the bakery queue
- `MadeBreadStream` — streams newly-baked bread to consumers

### RemoveOldBread
- `RemoveBread` — enqueues a bread removal request
- `RemoveBreadStream` — streams removed bread events

### BuyOrderService
- `BuyOrder` — fetches a single order by UUID with total cost
- `BuyOrderStream` — streams one or all orders with cost details

### AdminService
- `GetDashboardStats` — totals for orders, revenue, products, customers, makers, low-stock
- `GetAllCustomers` / `GetAllBreadMakers` / `GetAllBread` — list entities
- `GetBreadById` / `CreateBread` / `UpdateBread` / `DeleteBread` — bread CRUD
- `GetLowStockAlerts` — bread items below 10 units
- `UpdateOrderStatus` — changes order status; auto-generates invoice on `completed`
- `GetAllOrders` / `GetAllMakeOrders` — order lists with bread details
- `GetCustomerOrders` / `GetMakerOrders` — orders scoped to a customer or maker

### AuthService
- `AdminLogin` — bcrypt password check, returns HS256 JWT (24 h TTL)
- `CustomerLogin` — same for customer accounts
- `ValidateToken` — verifies JWT, returns user ID and type
- `CreateAdminUser` — creates a new admin account

### InvoiceService
- `CreateInvoice` — generates invoice for a buy order (8% tax); idempotent
- `GetInvoice` / `GetCustomerInvoices` / `GetAllInvoices` — invoice queries

### CustomerPortalService
- `GetMyOrders` — orders for a specific customer
- `GetMyInvoices` — invoices for a specific customer
- `GetOrderDetails` — full order breakdown with per-bread details

## Recent Changes

### Goroutine leak fix in `BuyBreadStream` (`server/gRPCBakery.go`)
The background goroutine that listens on RabbitMQ's `bread-bought` queue was leaking after the stream handler returned. Fixed by:
- Creating a **parent context** with `context.WithCancel` and `defer parentCancel()` so the goroutine is cancelled when `BuyBreadStream` exits.
- Changing `responseCh` from unbuffered to **buffered (size 1)** so a late send from the goroutine never blocks.
- Adding a `select { case responseCh <- response: … case <-ctx.Done(): … }` in `processBreadsBought` to prevent it blocking forever when the receiver is gone.
- Increasing `maxRetries` from 10 → **20** (100-second window) to accommodate the ~56 s broker processing time in the cluster (DB queries take ~4 s each × 14+ operations).

### RabbitMQ prefetch limit (`broker/main.go`)
Added `channel.Qos(1, 0, false)` before consuming the `buy-bread-order` queue. Without this, RabbitMQ pre-fetched all queued messages into memory on each broker restart. After 62 pod restarts this created a 7,657-message backlog. The fix limits delivery to one message at a time.

### Graceful RabbitMQ init in makers (`makers/main.go`)
`init()` now skips RabbitMQ connection when `RABBITMQ_SERVICE_ADDR` is empty (e.g., during tests) instead of calling `log.Fatalf`.

### New gRPC services (`server/`)
- **AdminService** — full CRUD for bread inventory, order management, dashboard stats, low-stock alerts, invoice auto-generation on order completion.
- **AuthService** — JWT-based login for admin and customer accounts using bcrypt.
- **InvoiceService** — invoice creation with 8% tax, idempotent lookup, customer invoice history.
- **CustomerPortalService** — customer-scoped order and invoice queries.

## Testing

Run the full test suite:

```bash
go test ./... -race -count=1 -timeout 60s
```

Run with coverage:

```bash
go test ./... -coverprofile=cover.out -covermode=atomic && go tool cover -func=cover.out
```

### Test structure

| Package | Test file(s) | What is covered |
|---|---|---|
| `server` | `server_test.go` | `BuyOrder` success/not-found, `retryRepo` retry mechanics |
| `server` | `admin_test.go` | All 19 `AdminService` RPC methods — success + DB error paths |
| `server` | `admin_extra_test.go` | Remaining admin error branches (UpdateBread, GetMakerOrders, etc.) |
| `server` | `auth_test.go` | `AdminLogin`, `CustomerLogin`, `ValidateToken`, `CreateAdminUser` with real bcrypt hashes |
| `server` | `invoice_test.go` | `InvoiceService` (new/existing invoice, not-found) + `CustomerPortalService` |
| `server` | `async_test.go` | Goroutine cleanup via `parentCancel()`, buffered channel semantics, concurrent `BuyOrder` |
| `server` | `bakery_extra_test.go` | `CheckBreadInventory`, `BuyOrderStream`, `initializeBakery` |
| `broker` | `broker_test.go` | `canFulfillOrder` (7 cases), `processOrderItems`, `NewRabbitMQBakery`, map concurrency |
| `makers` | `makers_test.go` | `processMakeBreadMessage` — valid JSON, invalid JSON, repo error, all 7 bread types, concurrent |
| `buyers` | `buyers_test.go` | `buySomeBread` and `buyBreadStream` with mock `pb.BuyBreadClient` — success, error, EOF, multi-response |
| `data` | `models_test.go` | `PostgresTestRepository` stub methods, `ErrNoRows` propagation, null total cost |
| `data` | `test_models_test.go` | All 17 `PostgresTestRepository` methods, struct field coverage |
| `frontend` | `main_test.go` | `staticPageHandler` — all routes, missing template, nav/footer links |

### Coverage summary

| Package | Unit-test coverage | Integration coverage | Ceiling without e2e |
|---|---|---|---|
| `server` (gRPCAdmin, gRPCAuth, gRPCInvoice) | **~90%** | ~95% | ~98% |
| `server` (gRPCBakery / async) | **~85%** | ~92% | ~95% |
| `buyers` | **50%** (business logic 100%, `main()` excluded) | 50% | 50% |
| `broker` helpers | **100%** | ~95% | — |
| `broker` integration | — | **~80%** | — |
| `makers` helpers | **100%** | ~90% | — |
| `makers` integration | — | **~75%** | — |
| `data/test_models.go` | **~90%** | — | — |
| `data/models.go` | **~10%** (40 SQL functions) | ~85% | — |
| `frontend` (web handlers) | **~5%** (main_test.go) | ~40% (integration_test.go) | ~65% |
| **Total (all packages)** | **17.4%** | **24.8%** | ~35% |

> **Note on coverage tiers**:
> - **Unit-test coverage**: Tests with mocked dependencies (no live DB/RabbitMQ).
> - **Integration coverage**: Tests that spin up testcontainers for PostgreSQL + RabbitMQ.
> - **e2e coverage**: Full system tests via `cover_e2e.out` (currently minimal).
>
> The headline 17.4% unit coverage is dominated by `proto/` (527 auto-generated functions at 0%) and `data/models.go` (40 pure SQL functions). Excluding those two, application-logic unit coverage is **38–42%**. Integration tests add **~7.4%** more coverage, primarily in `data/repository.go` and service integration paths. Reaching 85% total requires more e2e tests covering the full request flow.

### Test execution times

| Test suite | Typical duration | Notes |
|---|---|---|
| `go test ./server/...` | 25–35s | Integration tests with testcontainers |
| `go test ./broker/...` | 8–12s | RabbitMQ integration |
| `go test ./makers/...` | 6–10s | RabbitMQ integration |
| `go test ./buyers/...` | 2–4s | Mocked gRPC client |
| `go test ./data/...` | 5–8s | PostgreSQL integration |
| `go test ./frontend/...` | 3–6s | Template + handler tests |
| **Full suite (`./...`)** | **60–90s** | With `-race -count=1 -timeout 60s` |

## Troubleshooting

1. **RabbitMQ message backlog**: If the broker restarts frequently and messages accumulate, scale the broker to 0 replicas, purge the `buy-bread-order` and `bread-bought` queues via the RabbitMQ management UI, then scale back. The prefetch fix (Qos=1) prevents recurrence.
2. **"order not found after N attempts"**: The broker takes ~56 s to process an order in high-latency clusters. `maxRetries` is set to 20 (100 s window). If your cluster is slower, increase `maxRetries` in `server/gRPCBakery.go:BuyBreadStream`.
3. **RabbitMQ Connection**: Ensure `RABBITMQ_SERVICE_ADDR` uses the `amqp://` or `amqps://` scheme.
4. **PostgreSQL Connection**: Verify `DSN` is set and the database is reachable. The server retries up to 10 times with 5 s intervals.
5. **JWT errors**: Set `JWT_SECRET` to a stable value — tokens signed with a different secret will fail validation.

## License
[GPLv3](https://www.gnu.org/licenses/gpl-3.0.en.html)
