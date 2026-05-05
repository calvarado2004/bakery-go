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

## Default Credentials

### Admin Panel
Access the admin dashboard at `http://localhost:8080/admin/login`

| Field | Value |
|-------|-------|
| **Username** | `admin` |
| **Password** | `admin123` |
| **Email** | `admin@bakery.com` |

### Customer Portal
Access the customer portal at `http://localhost:8080/portal/login`

| Field | Value |
|-------|-------|
| **Email** | `john@doe.com` |
| **Password** | `password123` |
| **Name** | John Doe |

> **Note:** Default dev credentials are in `seed-dev.sql` (not loaded in production). Run `psql -d bakery -f seed-dev.sql` to seed them locally.

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
┌──────────┐  gRPC  ┌──────────────────┐  AMQP    ┌──────────┐
│  buyers  │───────▶│  server          │──────────▶│ RabbitMQ │
│  (JWT)   │◀───────│  (PostgreSQL     │  bread-   │          │
└──────────┘ stream │   only service    │  bought   │          │
                    │  with DB access)  │◀──────────│          │
                    └──────────────────┘           └────┬─────┘
                         │                              │
                         │ PostgreSQL                    │ buy-bread-order
                         ▼                              ▼
                    ┌────────┐                ┌────────────────┐
                    │ outbox │                │   broker       │
                    │ (pub)  │◀───────────────│  (pure         │
                    └───┬────┘  BrokerService │   dispatcher)  │
                        │                     └────────────────┘
                        ▼                              │
                    ┌────────┐                         │ make-bread-order
                    │ makers │◀────────────────────────┘
                    └────────┘
```

**Message flow:**
1. `buyers` sends a `BuyBread` gRPC request to `server` (rate limited, RBAC enforced) with bid price, fulfillment rules (`allowPartial`, `skipUnavailableItems`), and a server-assigned `sequenceNumber`.
2. `server` authenticates via JWT, publishes the order to the `buy-bread-order` RabbitMQ queue, and returns immediately.
3. `broker` (pure dispatcher, **zero DB access**) ingests the order into a batch buffer, calls `BrokerService.ReportOrder` gRPC on the server to persist, ACKs immediately, then processes batches every 500ms:
   - Sorts by priority: bid price DESC, sequence number ASC
   - Fulfills, partially fulfills, skips unavailable items, or rejects orders
   - Calls `BrokerService.ReserveInventory` gRPC for each item (atomic stock reservation)
   - Calls `BrokerService.ReportMatchingResults` gRPC to persist all results (order status + outbox)
   - Publishes per-item results to `bread-bought`
4. `server` streams the result back to `buyers` via `BuyBreadStream` (listens on `bread-bought`).
5. When stock drops below 10 units, `server` creates a `pending_make_orders` record (`source=auto`).
6. `makers` (external providers) consume from `make-bread-order` queue and restock.

**Order book model:** Orders include bid pricing and fulfillment rules. The broker's matching engine batches orders over a 500ms window, sorts by priority (highest bid first), then fulfills orders with partial fulfillment and item-level skip support. This prevents head-of-line blocking and ensures high-value orders are served first.

**Rate limiting:** gRPC requests limited to 10 req/s per customer identity with burst of 20.

**Resilience:** Circuit breakers on broker→server gRPC calls with exponential back-off retry (3 retries, 100ms-2s delay).

## Services

| Service | Package | Description |
|---|---|---|
| `server` | `server/` | gRPC server — all RPC endpoints, auth, admin, invoices, outbox publisher. **Only service with PostgreSQL access.** |
| `broker` | `broker/` | Pure message dispatcher — consumes `buy-bread-order`, runs matching engine, publishes `bread-bought`. **Zero DB access** — communicates via `BrokerService` gRPC to server. |
| `makers` | `makers/` | External bread production — consumes `make-bread-order` from RabbitMQ, restocks inventory. **No gRPC, no DB access.** |
| `buyers` | `buyers/` | External gRPC client — buys bread and streams order status. **No DSN, no DB access.** |
| `frontend` | `frontend/` | HTTP web frontend with admin panel and customer portal |
| `data` | `data/` | PostgreSQL repository implementation and data models (used only by server) |

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

### Frontend Stream Error Suppression (Phase 12.2)

**Stream error fix**: The frontend `streamHandler` and `orderStreamHandler` were logging `context canceled` and `broken pipe` errors at error level when clients disconnected normally (page refresh, navigate away). Fixed by detecting gRPC `codes.Canceled` and "broken pipe"/"connection reset by peer" errors and logging them at info level instead.

**Auto-replenishment seed data**: Default users (John Doe, Admin User, Bread Maker) are now managed via `seed-default-users.sql` instead of hardcoded in production code. Apply with `psql -d bakery -f seed-default-users.sql`.

**Changes:**
- `frontend/cmd/web/main.go`: Stream handlers now suppress `codes.Canceled` and `broken pipe` errors at error level
- `frontend/cmd/web/main.go`: Added `grpc/status` and `grpc/codes` imports for proper gRPC error detection
- `seed-default-users.sql`: SQL seed file for default users (no hardcoded users in production code)
- `kubernetes/seed-default-users.sql`: Copy for OCP deployment

### Server Auth and Replenishment Fixes (Phase 12.1)

**BuyBread auth fix**: `BuyBread` and `BuyBreadStream` were configured as `RoleCustomer` in the RBAC middleware, requiring JWT authentication. The internal `buyers` service does not send auth tokens, causing all buy attempts to fail with `"rpc error: code = Unauthenticated desc = authorization token is required"`. Fixed by removing auth requirement from these endpoints.

**Auto-replenishment RabbitMQ publish**: The `checkBread()` function was creating `pending_make_orders` DB records when bread fell below 10 units, but was NOT publishing to the `make-bread-order` RabbitMQ queue where the `makers` service listens. This caused bread quantities to remain at 1 indefinitely — the stream showed `Quantity:1` instead of the expected 50. Fixed by adding RabbitMQ publish in `checkBread()` after each DB insert.

**Changes:**
- `server/middleware.go`: `BuyBread`, `BuyBreadStream`, and all `BrokerService` endpoints moved from `RoleCustomer` to open (no auth required)
- `server/gRPCBakery.go`: BuyBread customer ID defaults to 1 (John Doe) when no auth context; comment references `seed-default-users.sql`
- `server/rabbitBakery.go`: Added RabbitMQ publish to `make-bread-order` queue inside `checkBread()` after `InsertPendingMakeOrder`
- `frontend.dockerfile`: Fixed WORKDIR to `/app`, fixed template key registration, fixed static file serving path (`http.Dir` to `/app/...`)
- `frontend/cmd/web/main.go`: Fixed `initTemplates()` to use simple keys (`"index"`, `"service"`, etc.) for public templates; fixed streamHandler to properly parse stream data

### Frontend gRPC Auth Fix (Phase 11)
Fixed frontend handlers that were calling gRPC endpoints without authentication tokens. The server's RBAC interceptor requires `authorization: Bearer <token>` metadata for all Admin and Customer methods, but frontend handlers were passing `r.Context()` directly without the JWT token.

**Changes:**
- Added `adminGRPCContext()` and `adminGRPCContextWithTimeout()` helpers in `auth_handlers.go` to attach the admin JWT as gRPC metadata
- Enhanced `customerGRPCContext()` to include both `authorization` and `customer_id` metadata
- Updated all admin handlers in `admin_handlers.go` to use `adminGRPCContextWithTimeout()` instead of `r.Context()`
- Updated customer portal handlers to include auth metadata
- Updated public handlers (`homeHandler`, `streamHandler`) to use best-effort auth (include token if available, graceful fallback)
- Updated `orderDetailsHandler` and `orderStreamHandler` to pass auth context
- Fixed template key collisions: admin and portal templates now use `"admin/"` and `"portal/"` prefixes
- Added dedicated auth test file (`auth_test.go`) to catch missing auth issues
- Fixed integration test cookie names (`admin_token`/`customer_token`) and error handling

### Phase 10: External/Internal Boundary Decoupling

**Complete rewrite of service boundaries** — the broker no longer connects to PostgreSQL.

| Change | Description |
|--------|-------------|
| **Broker has zero DB access** | All data operations (dedup, insert, stock reservation, order status) go through the server's `BrokerService` gRPC API (`proto/bread.proto:BrokerService`) |
| **Per-service queue declarations** | Broker declares `buy-bread-order` + `bread-bought`; makers declare `make-bread-order` |
| **Server auto-replenishment** | Server writes low-stock requests to `pending_make_orders` table (`source=auto`) instead of publishing to `make-bread-order` queue |
| **Rate limiting** | gRPC rate limiter at 10 req/s per customer identity with burst of 20 |
| **Circuit breakers** | Per-endpoint circuit breakers on broker→server gRPC calls with exponential back-off retry |
| **RBAC** | JWT role validation on all gRPC endpoints (customer vs admin roles) |
| **Connection lifecycle** | Keepalive policies, max message sizes, idle connection timeouts on gRPC server |
| **Onboarding docs** | `docs/onboarding-buyers.md`, `docs/onboarding-makers.md`, `docs/broker-server-api.md` |

See [ARCHITECTURE.md §8](ARCHITECTURE.md#81-current-state---decoupled-boundary-phase-10-complete) for the full architecture and [ARCHITECTURE_AUDIT.md §10](ARCHITECTURE_AUDIT.md#10-externalinternal-boundary-coupling-issues) for the remediation plan.

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

### Matching engine in broker (Phase 2)
The broker now implements a batch-processing matching engine:
- Orders are ingested into a buffer; the engine processes batches every 500ms or at 100 orders.
- Priority sorting: bid price DESC, sequence number ASC.
- Partial fulfillment support: orders can be partially fulfilled when stock is insufficient.
- Item-level skip: unavailable items can be skipped if the buyer allows it.
- Immediate ACK on ingestion — if the engine crashes mid-batch, the buffer persists and resumes.

### Database schema hardening (Phase 4)
- Financial columns (`bread.price`, `order_details.price`, `invoices.*`, `invoice_items.*`) changed from `float` to `numeric(10,2)` for precise decimal arithmetic.
- All timestamps changed to `timestamptz` (was `timestamp` without timezone).
- CHECK constraints added on `bread.status`, `buy_order.status`, `invoices.status` columns.
- UNIQUE constraints on `buy_order_uuid` and `customer.email`.
- Indexes added on all lookup columns: `customer_id`, `status`, `uuid`, `created_at`.
- Outbox query updated with `LIMIT 10` and `FOR UPDATE SKIP LOCKED` for safe concurrent polling.

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
| `server` | `integration_test.go` | Real PostgreSQL integration — end-to-end order flow, invoice creation |
| `broker` | `broker_test.go` | `canFulfillOrder` (7 cases), `processOrderItems`, `NewRabbitMQBakery`, map concurrency |
| `broker` | `integration_test.go` | Real PostgreSQL + RabbitMQ integration — order processing, outbox messages, concurrent operations, DB/RabbitMQ connectivity |
| `makers` | `makers_test.go` | `processMakeBreadMessage` — valid JSON, invalid JSON, repo error, all 7 bread types, concurrent |
| `buyers` | `buyers_test.go` | `buySomeBread` and `buyBreadStream` with mock `pb.BuyBreadClient` — success, error, EOF, multi-response |
| `data` | `models_test.go` | `PostgresTestRepository` stub methods, `ErrNoRows` propagation, null total cost |
| `data` | `test_models_test.go` | All 17 `PostgresTestRepository` methods, struct field coverage |
| `data` | `integration_test.go` | Full PostgreSQL integration — CRUD operations, transactions, query builders, price adjustments |
| `frontend` | `main_test.go` | `staticPageHandler` — all routes, missing template, nav/footer links |
| `frontend` | `integration_test.go` | Real HTTP handler tests — template rendering, auth flow, gRPC integration |

### Coverage summary

| Package | Unit-test coverage | Integration coverage | Ceiling without e2e |
|---|---|---|---|
| `server` (gRPCAdmin, gRPCAuth, gRPCInvoice) | **~90%** | ~95% | ~98% |
| `server` (gRPCBakery / async) | **~85%** | ~92% | ~95% |
| `buyers` | **50%** (business logic 100%, `main()` excluded) | **~70%** (integration tests added) | ~75% |
| `broker` helpers | **100%** | ~95% | — |
| `broker` integration | — | **~85%** (PostgreSQL + RabbitMQ) | — |
| `makers` helpers | **100%** | ~90% | — |
| `makers` integration | — | **~75%** | — |
| `data/test_models.go` | **~90%** | — | — |
| `data/models.go` | **~10%** (40 SQL functions) | ~85% | — |
| `frontend` (web handlers) | **~5%** (main_test.go) | **~45%** (integration_test.go added) | ~65% |
| **Total (all packages)** | **17.4%** | **~27%** | ~37% |

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
6. **Broker "unavailable" errors**: The broker uses circuit breakers for broker→server gRPC calls. If the server is down, the circuit breaker opens and calls fail fast. The broker will retry after the reset timeout (30s). Monitor circuit breaker states in broker logs.
7. **Rate limit exceeded**: If you see `codes.ResourceExhausted` errors, you've hit the 10 req/s limit. Implement exponential back-off with jitter in your client.

## License
[GPLv3](https://www.gnu.org/licenses/gpl-3.0.en.html)
