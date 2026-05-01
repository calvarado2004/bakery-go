# Bakery-Go — Architectural Improvements

> **Goal:** Keep gRPC and RabbitMQ but make the architecture cleaner, more maintainable, and less duplicated.

---

## Table of Contents

1. [Current State](#current-state)
2. [Problem 1: Each Service Connects to Both PostgreSQL and RabbitMQ](#problem-1-each-service-connects-to-both-postgresql-and-rabbitmq)
3. [Problem 2: The gRPC Server Is a Monolith with 9 Services](#problem-2-the-grpc-server-is-a-monolith-with-9-services)
4. [Problem 3: Sync Bridge + Async Events in the Same System](#problem-3-sync-bridge--async-events-in-the-same-system)
5. [Problem 4: Proto File Naming and Design Inconsistencies](#problem-4-proto-file-naming-and-design-inconsistencies)
6. [Problem 5: The Buyers Service Is a Test Harness](#problem-5-the-buyers-service-is-a-test-harness)
7. [Problem 6: Outbox Lives on the Broker, But the Server Should Own It](#problem-6-outbox-lives-on-the-broker-but-the-server-should-own-it)
8. [Proposed New Architecture](#proposed-new-architecture)
9. [Recommended Refactoring Order](#recommended-refactoring-order)

---

## Current State

You have **5 binaries** (server, broker, makers, buyers, frontend), each running as a separate container. The `server` is a monolithic gRPC process hosting all 9 services. Each service that needs infrastructure (server, broker, makers) independently connects to **both** PostgreSQL and RabbitMQ, with duplicated structs (`RabbitMQBakery`, `OrderStatus`, `Config`), duplicated DB connect logic (`connectToDB`/`openDB`), and duplicated logging setup.

The messaging layer uses RabbitMQ as a **synchronous request-reply** bridge between server and broker (each `BuyBread` call publishes to `buy-bread-order` and polls for a confirmation on `bread-bought`), but also uses RabbitMQ for **asynchronous** events (makers, removals, bakery-ready). These two patterns fight each other.

---

## Problem 1: Each Service Connects to Both PostgreSQL and RabbitMQ

Every service (server, broker, makers) opens its own DB connection pool AND its own RabbitMQ connection. This means 3 DB pools and 3 RMQ connections doing essentially the same thing: talk to the same tables. The broker and makers don't need direct RabbitMQ access from the server's perspective, and the server doesn't need its own separate connection from what the broker uses.

Each service duplicates:
- `connectToDB()` / `openDB()` retry logic
- `RabbitMQBakery`, `OrderStatus`, `Config` structs
- Logrus formatter setup

### Suggested Change

Let services choose the right dependency based on their actual role:

- **Server** → owns gRPC. Connects to PostgreSQL (read/write) and publishes to RabbitMQ. Does NOT need to consume from `buy-bread-order`.
- **Broker** → owns purchase processing. Connects to PostgreSQL (read/write) and RabbitMQ (`buy-bread-order` in, `bread-bought` out). Does NOT serve gRPC.
- **Makers** → owns inventory restocking. Connects to PostgreSQL (read/write) and RabbitMQ (`make-bread-order`). A thin worker.

Extract shared infrastructure into `internal/`:

```
internal/
├── db/
│   ├── connect.go       # connectToDB(), openDB() with retry logic
│   └── repository.go    # Repository interface, PostgresRepository
├── mq/
│   ├── connection.go    # NewRabbitMQ(), channel setup, QoS
│   └── publisher.go     # Publish(), with outbox helper
├── config.go            # Config, RabbitMQBakery, OrderStatus structs
└── log/
    └── setup.go         # Logrus formatter setup (single function)
```

---

## Problem 2: The gRPC Server Is a Monolith with 9 Services

All 9 gRPC services (`MakeBread`, `CheckInventory`, `BuyBread`, `BuyOrderService`, `RemoveOldBread`, `AdminService`, `AuthService`, `InvoiceService`, `CustomerPortalService`) live in one binary with one dependency graph. They all embed `RabbitMQBakery` even though most never touch RabbitMQ (e.g., `AdminService` only reads from Postgres).

### Suggested Change

Group services by concern and give each group only the dependencies it actually needs:

```
server/
├── main.go              # Bootstrap: DB + RMQ + register all services
├── bakery/
│   ├── server.go        # MakeBread, CheckInventory, BuyBread, BuyOrderService,
│   │                     RemoveOldBread  (these need RabbitMQ)
│   └── handlers.go
├── commerce/
│   ├── server.go        # InvoiceService, CustomerPortalService (Postgres only)
│   └── handlers.go
├── admin/
│   ├── server.go        # AdminService (Postgres only)
│   └── handlers.go
└── auth/
    ├── server.go        # AuthService (Postgres only)
    └── handlers.go
```

Each group gets only its dependencies. `AdminService` no longer holds a RabbitMQ connection. Each group is testable in isolation and the dependency injection is explicit.

---

## Problem 3: Sync Bridge + Async Events in the Same System

The `BuyBread` flow is synchronous request-reply:

1. Server publishes to `buy-bread-order`
2. Server calls `getBuyResponse()` which polls with exponential backoff (1s, 2s, 4s, 8s, 16s) for a `bread-bought` message
3. Broker processes and publishes to `bread-bought`
4. Server receives confirmation

But `checkBread` is fire-and-forget:

1. Server publishes to `make-bread-order` every 30s
2. Makers consume and update DB
3. No confirmation

These are two different patterns with no clear separation. The `bread-bought` queue has **two consumers** (`getBuyResponse` and `processBreadsBought`) with round-robin dispatch, causing a race condition where a confirmation message may be consumed by the wrong goroutine.

### Suggested Change

Separate the messaging concerns cleanly:

```
Queue                Direction          Pattern          Purpose
──────────────────── ────────────────── ────────────── ────────────────────
buy-bread-order      Server → Broker    Request        Async purchase processing
bread-bought         Broker → Server    Response       Confirmation
make-bread-order     Server → Makers    Fire-and-forget Restock trigger
bread-in-bakery      Server → Server    Internal       Make → ready lifecycle
bread-removed        Server → Server    Internal       Stale bread removal
```

Key changes:

- `getBuyResponse` registers a consumer on `bread-bought` per order (current approach). Replace with a **dedicated reply queue per order** using each order's UUID as the queue name and routing key. This eliminates the race condition.
- `processBreadsBought` should be the **only** consumer of `bread-bought`. It dispatches to the in-memory `OrderStatus` channels.
- The 30s polling loop in `BuyBreadStream` should read from the `OrderStatus` channel instead of polling the DB.

---

## Problem 4: Proto File Naming and Design Inconsistencies

The `proto/bread.proto` file has several issues:

- `package bread;` but `option go_package = "github.com/calvarado2004/bakery-go/bread"` — the package name and go_package path don't reflect the actual module structure
- Services are fragmented across 9 separate service blocks when they could be grouped by domain
- Messages like `BreadRequest` are reused for 5+ different RPCs with different meanings
- Timestamps use `string` instead of `google.protobuf.Timestamp`
- `float` used for monetary values (use `double` for precision)

### Suggested Change

Restructure the proto file:

```protobuf
package bakery.v1;
option go_package = "github.com/calvarado2004/bakery-go/proto/bakeryv1";

import "google/protobuf/timestamp.proto";
import "google/protobuf/empty.proto";

// Group services by domain
service BakeryService {
    rpc BakeBread(BakeBreadRequest) returns (BakeBreadResponse);
    rpc CheckInventory(CheckInventoryRequest) returns (CheckInventoryResponse);
    rpc BuyBread(BuyBreadRequest) returns (BuyBreadResponse);
    rpc StreamInventory(Empty) returns (stream InventoryUpdate);
    rpc StreamOrders(StreamOrdersRequest) returns (stream OrderUpdate);
}

service CommerceService {
    rpc CreateInvoice(CreateInvoiceRequest) returns (Invoice);
    rpc GetMyOrders(Empty) returns (OrderList);
}

service AdminService {
    rpc GetDashboardStats(Empty) returns (DashboardStats);
    rpc GetAllBread(Empty) returns (BreadList);
    rpc CreateBread(CreateBreadRequest) returns (Bread);
    rpc UpdateBread(UpdateBreadRequest) returns (Bread);
    rpc DeleteBread(DeleteBreadRequest) returns (Empty);
    rpc GetAllOrders(Empty) returns (OrderList);
    rpc GetAllCustomers(Empty) returns (CustomerList);
    rpc GetAllBreadMakers(Empty) returns (BreadMakerList);
}

service AuthService {
    rpc AdminLogin(LoginRequest) returns (LoginResponse);
    rpc CustomerLogin(CustomerLoginRequest) returns (CustomerLoginResponse);
    rpc ValidateToken(ValidateTokenRequest) returns (ValidateTokenResponse);
    rpc CreateAdminUser(CreateAdminUserRequest) returns (AdminUser);
}

// Use google.protobuf.Timestamp instead of string
message Bread {
    int32 id = 1;
    string name = 2;
    double price = 3;                       // float → double for precision
    int32 quantity = 4;
    string description = 5;
    string type = 6;
    string status = 7;
    google.protobuf.Timestamp created_at = 8;
    google.protobuf.Timestamp updated_at = 9;
    string image = 10;
}

message BuyOrder {
    int32 id = 1;
    int32 customer_id = 2;
    string buy_order_uuid = 3;
    double total_cost = 4;                  // float → double
    string status = 5;
    repeated BreadItem breads = 6;
    google.protobuf.Timestamp created_at = 7;
}

message BreadItem {
    int32 bread_id = 1;
    string name = 2;
    double price = 3;
    int32 quantity = 4;
}
```

Benefits:
- Domain-grouped services (Bakery, Commerce, Admin, Auth)
- Standard protobuf types (`Timestamp`, `Empty`)
- Clear request/response message naming per RPC
- `double` for monetary precision
- Consistent snake_case field names

---

## Problem 5: The Buyers Service Is a Test Harness

The buyers service runs every 35 seconds, sends hardcoded bread types with `customerId = 1` (John Doe), and sleeps. It's a simulator, not a production service.

### Suggested Change

Either:

- **Option A:** Keep it but make it a proper client library (`internal/client/`) that the frontend or an external API gateway calls
- **Option B:** Mark it clearly as a test fixture, rename to `test-buyers`, add a `TEST_MODE` env var that gates it, and remove from production `docker-compose.yml`

---

## Problem 6: Outbox Lives on the Broker, But the Server Should Own It

The outbox pattern is split: the server inserts outbox messages when publishing to `buy-bread-order`, but the broker has the outbox re-publishing goroutine. If the server crashes after inserting to outbox but before publishing, the broker's goroutine eventually re-publishes. But the broker's outbox goroutine does **not** mark messages as sent after publishing (duplicate delivery every 45s).

### Suggested Change

Move outbox responsibility to the **publisher** (server) and have the broker use a simpler pattern:

```
Server (publisher):
  1. BEGIN transaction
  2. Insert BuyOrder + outbox row (sent=false)
  3. Publish to RabbitMQ
  4. If publish succeeds: UPDATE outbox SET sent=true
  5. COMMIT

Server bg goroutine (every 30s):
  1. SELECT * FROM outbox WHERE sent = false AND created_at < NOW() - 5m
  2. Re-publish each message
  3. UPDATE sent = true on success, or mark as dead after N retries
  4. Periodically DELETE old rows where sent = true AND created_at < NOW() - 7d
```

The broker doesn't need an outbox — it's the only consumer of `buy-bread-order`, so RabbitMQ's own ack/nack handles recovery. This simplifies the broker significantly.

---

## Proposed New Architecture

```
┌─────────────┐     gRPC      ┌─────────────────────────────┐
│  Frontend   │──────────────▶│   Server (gRPC)              │
│  (HTTP/SSE) │               │   ├── BakeryService (DB+RMQ) │
└─────────────┘               │   ├── CommerceService (DB)   │
                              │   ├── AdminService (DB)      │
│  Buyers (test) │──gRPC─────▶│   └── AuthService (DB)       │
└─────────────┘               └──────────┬──────────────────┘
                                         │ RabbitMQ
                              ┌──────────▼──────────────────┐
                              │   RabbitMQ                  │
                              │   ├── buy-bread-order       │
                              │   ├── bread-bought          │
                              │   └── make-bread-order      │
                              └──────────┬──────────────────┘
                                         │
                    ┌────────────────────┼────────────────────┐
                    │                    │                     │
              ┌─────▼─────┐      ┌──────▼──────┐    ┌────────▼────────┐
              │   Broker   │      │   Makers    │    │   PostgreSQL    │
              │ (buy orders)│      │ (restock)   │    │   (single DB)   │
              └────────────┘      └─────────────┘    └─────────────────┘
```

Each service in `docker-compose.yml` stays, but the **server** gets cleaner internal structure with per-domain gRPC services, and the **broker**/**makers** become thinner consumers with explicit responsibilities.

---

## Recommended Refactoring Order

| Phase | What | Why |
|-------|------|-----|
| **1** | Extract `internal/db/`, `internal/mq/`, `internal/config.go`, `internal/log/` | Remove duplication across server/broker/makers |
| **2** | Restructure `proto/bread.proto` — domain-grouped services, standard types, consistent naming | Clean API contract |
| **3** | Group gRPC services into sub-packages (`bakery/`, `admin/`, `auth/`, `commerce/`) | Clearer dependency injection per service group |
| **4** | Fix `bread-bought` consumer race: single consumer + dispatch map | Eliminate race condition |
| **5** | Move outbox to server, simplify broker (no outbox needed) | Cleaner message recovery |
| **6** | Fix `BuyBreadStream` to read from channels, not DB polling | Remove wasteful polling |
| **7** | Add graceful shutdown, health checks, structured logging | Production readiness |
| **8** | Add pagination, input validation, consistent tax rate, DB constraints | Correctness |
| **9** | Move outbox cleanup, add DLX for dead letters, add metrics | Observability |

---

## Appendix: Quick Reference — What Each Service Needs

| Service | PostgreSQL | RabbitMQ | gRPC Server | gRPC Client |
|---------|-----------|----------|-------------|-------------|
| **server** | Read/Write | Publish + consume (internal queues) | All 9 services | None |
| **broker** | Read/Write | Consume `buy-bread-order`, publish `bread-bought` | None | None |
| **makers** | Read/Write | Consume `make-bread-order` | None | None |
| **buyers** | None | None | None | Connect to server |
| **frontend** | None | None | None | Connect to server |
