# Bakery Service — System Architecture

## Table of Contents

1. [Overview](#overview)
2. [Component Topology](#component-topology)
3. [Inter-Service Communication](#inter-service-communication)
4. [Data Flow](#data-flow)
5. [Deployment Model](#deployment-model)
6. [Environment Configuration](#environment-configuration)
7. [Design Patterns](#design-patterns)
8. [Known Architectural Limitations](#known-architectural-limitations)

---

## Overview

The Bakery Service is a microservices application written in Go that simulates the operations of a virtual bakery shop. It is composed of five independently deployable services that communicate via **gRPC** (synchronous, strongly-typed RPC) and **RabbitMQ** (asynchronous message passing). Persistent state is stored in a **PostgreSQL** relational database. The web-facing layer is rendered server-side using **Go templates**.

The system is designed to demonstrate:

- Event-driven architecture with message queuing
- gRPC-based service-to-service communication
- Transactional reliability via the Outbox Pattern
- Role-based access control (admin and customer portals)
- Real-time UI updates using Server-Sent Events (SSE) and gRPC streaming

---

## Component Topology

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│                                   External Clients                               │
│                         Browser (Admin Portal / Customer Portal)                 │
└──────────────────────────────────┬───────────────────────────────────────────────┘
                                   │ HTTP (port 8080)
                                   ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              Frontend Service                                   │
│                    (Go Templates + gorilla/mux + SSE)                           │
│                                                                                 │
│  Public:  /  /stream  /order-stream  /orders                                   │
│  Admin:   /admin/**   (JWT middleware — HttpOnly cookie)                        │
│  Portal:  /portal/**  (JWT middleware — HttpOnly cookie)                        │
└──────────────────────────────────┬──────────────────────────────────────────────┘
                                   │ gRPC (port 50051)
                                   ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                               Server Service                                    │
│                      (gRPC — all service implementations)                       │
│                                                                                 │
│  Services:  MakeBread | CheckInventory | BuyBread | BuyOrderService             │
│             RemoveOldBread | MakeOrderService | AdminService                    │
│             AuthService | InvoiceService | CustomerPortalService                │
└────────────┬────────────────────────────────────────────────┬───────────────────┘
             │ AMQP publish/consume                           │ pgx
             ▼                                               ▼
┌─────────────────────────────┐              ┌───────────────────────────┐
│         RabbitMQ            │              │        PostgreSQL          │
│                             │              │                           │
│  Queues:                    │              │  Tables:                  │
│  • make-bread-order         │              │  • bread                  │
│  • buy-bread-order          │              │  • bread_maker            │
│  • bread-bought             │              │  • customer               │
│  • bread-to-make            │              │  • buy_order              │
│  • bread-in-bakery          │              │  • order_details          │
│  • bread-removed            │              │  • make_order             │
│                             │              │  • orders_processed       │
└────────────┬────────────────┘              │  • outbox                 │
             │                              │  • admin_users            │
     ┌───────┴────────┐                     │  • invoices               │
     │                │                     │  • invoice_items          │
     ▼                ▼                     └───────────────────────────┘
┌──────────┐   ┌────────────┐
│  Broker  │   │   Makers   │
│ Service  │   │  Service   │
│          │   │            │
│ Consumes │   │ Consumes   │
│ buy-     │   │ make-      │
│ bread-   │   │ bread-     │
│ order    │   │ order      │
│          │   │            │
│ Publishes│   │ Updates DB │
│ bread-   │   │ quantity   │
│ bought   │   │ (+)        │
└──────────┘   └────────────┘

┌────────────────────────────────────────────┐
│               Buyers Service               │
│   (Simulates customer purchase requests)   │
│   • Sends BuyBread gRPC call               │
│   • Streams BuyBreadStream for results     │
│   • Cycles every 35 seconds               │
└────────────────────────────────────────────┘
```

---

## Inter-Service Communication

### gRPC (Synchronous)

All synchronous calls go through the **Server Service** on port `50051`. gRPC uses Protocol Buffers (defined in `proto/bread.proto`) for strongly-typed message serialization.

| Client       | Server Service       | Method                        | Purpose                                  |
|--------------|---------------------|-------------------------------|------------------------------------------|
| Frontend     | AdminService        | GetDashboardStats             | Admin dashboard aggregates               |
| Frontend     | AdminService        | GetAllBread / CRUD            | Bread product management                 |
| Frontend     | AdminService        | GetAllOrders / UpdateStatus   | Order lifecycle management               |
| Frontend     | AdminService        | GetCustomerOrders             | Per-customer order history               |
| Frontend     | AdminService        | GetMakerOrders                | Per-maker production history             |
| Frontend     | AdminService        | GetLowStockAlerts             | Inventory alert monitoring               |
| Frontend     | AuthService         | AdminLogin / CustomerLogin    | Credential validation and token issuance |
| Frontend     | AuthService         | ValidateToken                 | JWT integrity verification               |
| Frontend     | InvoiceService      | GetAllInvoices / GetInvoice   | Invoice listing and detail               |
| Frontend     | CustomerPortalService | GetMyOrders / GetMyInvoices | Customer self-service portal             |
| Frontend     | CheckInventory      | CheckBreadInventoryStream     | SSE live inventory feed                  |
| Buyers       | BuyBread            | BuyBread / BuyBreadStream     | Purchase initiation and confirmation     |

### RabbitMQ (Asynchronous)

| Queue              | Publisher         | Consumer        | Purpose                                           |
|--------------------|-------------------|-----------------|---------------------------------------------------|
| `make-bread-order` | Server (checkBread) | Makers        | Trigger bread production when inventory < 10      |
| `buy-bread-order`  | Server (BuyBread) | Broker          | Initiate a purchase transaction                   |
| `bread-bought`     | Broker            | Server          | Confirm successful purchase to streaming clients  |
| `bread-to-make`    | Server (BakeBread)| —               | MakeBread service internal make queue             |
| `bread-in-bakery`  | Server (SendBreadToBakery) | Server (MadeBreadStream) | Signal bread is ready      |
| `bread-removed`    | Server (RemoveBread) | Server (RemoveBreadStream) | Signal stale bread removed         |

---

## Data Flow

### Purchase Flow (Happy Path)

```
Buyers Service
    │
    │  1. BuyBread(BreadRequest) gRPC
    ▼
Server Service
    │
    │  2. Publish JSON BuyOrder → "buy-bread-order" queue
    │  3. Create Outbox record (not yet sent)
    ▼
RabbitMQ [buy-bread-order]
    │
    │  4. Deliver message
    ▼
Broker Service
    │
    │  5. Validate bread availability
    │  6. Decrease bread quantities in DB
    │  7. Set order status: Pending → Processed (or Failed)
    │  8. Insert Outbox message for confirmation
    │  9. Publish BuyOrder → "bread-bought" queue
    ▼
RabbitMQ [bread-bought]
    │
    │  10. Deliver confirmation
    ▼
Server Service (getBuyResponse)
    │
    │  11. Update OrderStatus channel (unblocks BuyBreadStream)
    │  12. Stream BreadResponse to Buyers / Frontend
    ▼
Buyers Service / Frontend
    │
    │  13. Display confirmation
```

### Inventory Replenishment Flow

```
Server Service (background goroutine — every 30s)
    │
    │  1. Query DB for bread with qty < 10
    │  2. Publish Bread JSON → "make-bread-order" queue
    ▼
RabbitMQ [make-bread-order]
    │
    │  3. Deliver production request
    ▼
Makers Service
    │
    │  4. Increment bread quantity in DB
    │  5. Acknowledge message
```

### Admin Order Completion Flow

```
Admin User (HTTP POST /admin/orders/{id}/status)
    │
    │  1. UpdateOrderStatus gRPC call (status = "completed")
    ▼
Server (AdminService.UpdateOrderStatus)
    │
    │  2. Update order status in DB
    │  3. Auto-invoke generateInvoiceForOrder()
    │  4. CreateInvoice with 10% tax
    │  5. Return updated BuyOrder proto
    ▼
Frontend
    │
    │  6. Redirect to orders list
```

---

## Deployment Model

Each service has its own Dockerfile enabling independent containerisation.

| Service    | Dockerfile               | Main Entry Point                        |
|------------|--------------------------|------------------------------------------|
| Server     | `server.dockerfile`      | `server/main.go`                        |
| Broker     | `broker.dockerfile`      | `broker/main.go`                        |
| Buyers     | `buyers.dockerfile`      | `buyers/main.go`                        |
| Makers     | `makers.dockerfile`      | `makers/main.go`                        |
| Frontend   | `frontend.dockerfile`    | `frontend/cmd/web/main.go`              |

Infrastructure dependencies (PostgreSQL, RabbitMQ) are expected to be provided externally (e.g., via Docker Compose or Kubernetes).

> **Note:** No `docker-compose.yml` currently exists in the repository. This is a gap to be addressed (see `docs/IMPROVEMENTS.md`).

---

## Environment Configuration

| Variable               | Default                              | Used By              | Description                           |
|------------------------|--------------------------------------|----------------------|---------------------------------------|
| `BAKERY_SERVICE_ADDR`  | `localhost:50051`                    | Frontend, Buyers     | gRPC server address                   |
| `RABBITMQ_SERVICE_ADDR`| `amqp://guest:guest@localhost:5672/` | Server, Broker, Makers | RabbitMQ connection string           |
| `DSN`                  | _(none — required)_                  | Server, Broker, Makers | PostgreSQL DSN                       |
| `JWT_SECRET`           | *(required, no default)* | Server, Frontend | JWT signing key                 |

> **Security Warning:** The default value of `JWT_SECRET` is hardcoded in source. It must be overridden in all non-development environments.

---

## Design Patterns

### Repository Pattern

The `data.Repository` interface abstracts all database operations. The production implementation (`PostgresRepository`) uses `pgx/v4`. The test implementation (`TestRepository`) returns deterministic mock data. This enables unit testing of service logic without a live database.

### Outbox Pattern (Message Reliability)

Before publishing a buy-bread order to RabbitMQ, the Server Service persists the payload to the `outbox` table. A background goroutine in the Broker service re-publishes any messages with `sent = false` every 45 seconds, ensuring at-least-once delivery even when the message broker is temporarily unavailable.

### gRPC Streaming

Multiple services expose server-streaming RPCs (e.g., `BuyBreadStream`, `CheckBreadInventoryStream`, `MadeBreadStream`). These are used by both the Buyers simulation service and the Frontend's SSE endpoints to deliver real-time updates to connected clients.

### Server-Sent Events (SSE)

The Frontend service exposes `/stream`, `/order-stream`, `/admin/dashboard-stream`, and `/admin/alerts-stream` as SSE endpoints. Handlers forward data received from gRPC streams over the HTTP response writer using the `text/event-stream` content type.

### JWT Authentication

Both admin and customer sessions are authenticated using JWT tokens stored in `HttpOnly`, `SameSite=Strict` cookies. Token claims carry `UserID`, `Username`, `UserType`, and `Role`. Middleware (`RequireAdminAuth`, `RequireCustomerAuth`) validates tokens on every protected request.

### Factory Pattern

`NewRabbitMQBakery()` centralises the construction of the `RabbitMQBakery` struct, ensuring all dependencies (repository, RabbitMQ connection, configuration) are injected in a consistent manner.

---

## Known Architectural Limitations

| #  | Limitation                                                                  | Severity | Resolution                                          |
|----|-----------------------------------------------------------------------------|----------|-----------------------------------------------------|
| 1  | No `docker-compose.yml` for local orchestration                             | Medium   | Add compose file with all services + infra          |
| 2  | `JWT_SECRET` defaults to a hardcoded string                                 | High     | Enforce env var at startup; panic if not set        |
| 3  | No TLS on gRPC connections                                                  | High     | Implement mutual TLS or at minimum server TLS       |
| 4  | Broker and Server each define identical structs (`RabbitMQBakery`, `Config`)| Medium   | Extract to a shared internal package                |
| 5  | `initializeBakery()` seeds data unconditionally; may duplicate records      | Low      | Guard with existence check before insert            |
| 6  | No structured error types; plain `fmt.Errorf` strings throughout            | Medium   | Define sentinel errors and domain error types       |
| 7  | No health check endpoints for any service                                   | Medium   | Implement `/healthz` and gRPC health protocol       |
| 8  | No distributed tracing or metrics instrumentation                           | Medium   | Add OpenTelemetry tracing and Prometheus metrics     |
| 9  | Polling in `BuyBreadStream` (5-second intervals, 10 retries)                | Low      | Replace with event-driven notification via channel  |
| 10 | No rate limiting on public endpoints                                        | Medium   | Add middleware for rate limiting                    |
