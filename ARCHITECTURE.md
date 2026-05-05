# Bakery Service - Architecture Diagrams

## 1. High-Level System Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Bakery Service                                  │
│                                                                         │
│  ┌──────────┐      gRPC       ┌──────────┐      AMQP        ┌────────┐│
│  │  buyers  │ ──────────────▶ │  server  │ ──────────────▶  │ Rabbit ││
│  │  (client)│ ◀────────────── │  (gRPC)  │ ◀──────────────  │  MQ    ││
│  └──────────┘   BuyBreadStream└────┬─────┘   bread-bought   └───┬────┘│
│                                   │                              │      │
│                                   │ PostgreSQL                   │      │
│                                   ▼                              │      │
│                            ┌──────────────┐                      │      │
│                            │   PostgreSQL │                      │      │
│                            │    (data)    │                      │      │
│                            └──────────────┘                      │      │
│                                                                   │      │
│                    buy-bread-order ──▶ make-bread-order           │      │
│                                                                   │      │
│                            ┌──────────────┐                      │      │
│  ┌──────────┐     AMQP    │   broker     │     AMQP     ┌────────┐│
│  │  makers  │ ◀────────── │ (order proc.)│ ◀─────────── │  data  ││
│  └──────────┘ make-bread- └──────────────┘   bread-      └────────┘│
│                  order          │       bought                     │
│                                restock                           │
└─────────────────────────────────────────────────────────────────────────┘
```

## 2. Message Flow (Buy Bread Order)

```
 ┌──────┐        ┌──────┐         ┌────────┐         ┌──────────┐
 │Buyer │        │Server│         │Broker  │         │  Makers  │
 └──┬───┘        └──┬───┘         └───┬────┘         └────┬─────┘
    │               │                  │                    │
    │ BuyBread RPC  │                  │                    │
    │──────────────▶│                  │                    │
    │               │ publish          │                    │
    │               │ ────────────────────────────────────▶ │ buy-bread-order
    │               │                  │                    │
    │               │                  │ consume            │
    │               │                  │ ◀──────────────────│
    │               │                  │ check stock        │
    │               │                  │ adjust qty         │
    │               │                  │ update order       │
    │               │                  │ publish result     │
    │               │                  │ ──────────────────────────────────▶ bread-bought
    │               │                  │                    │
    │               │                  │ (stock < 10)       │
    │               │                  │ publish make req   │
    │               │                  │ ──────────────────────────────────▶ make-bread-order
    │               │                  │                    │
    │               │                  │                    │ consume
    │               │                  │                    │ ◀──────────────────
    │               │                  │                    │ restock DB
    │               │                  │                    │
    │ BuyBreadStream│                  │                    │
    │ ◀─────────────│ stream result    │                    │
    │               │                  │                    │
```

## 3. Service Details & Endpoints

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                                server/                                       │
│                            (gRPC Server)                                     │
│──────────────────────────────────────────────────────────────────────────────│
│                                                                              │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐             │
│  │  BakeryService   │  │  AdminService   │  │  AuthService     │             │
│  ├─────────────────┤  ├─────────────────┤  ├─────────────────┤             │
│  │ BuyBread         │  │ GetDashboardStats│  │ AdminLogin      │             │
│  │ BuyBreadStream   │  │ GetAllCustomers  │  │ CustomerLogin   │             │
│  │ CheckBreadInv    │  │ GetAllBread      │  │ ValidateToken   │             │
│  │ CheckInvStream   │  │ GetBreadById     │  │ CreateAdminUser │             │
│  │ BakeBread        │  │ CreateBread      │  └─────────────────┘             │
│  │ SendBreadToBakery│  │ UpdateBread      │                                  │
│  │ MadeBreadStream  │  │ DeleteBread      │  ┌─────────────────┐             │
│  │ RemoveBread      │  │ GetLowStockAlerts│  │ InvoiceService  │             │
│  │ RemoveBreadStream│  │ UpdateOrderStat  │  ├─────────────────┤             │
│  │ BuyOrder         │  │ GetAllOrders     │  │ CreateInvoice   │             │
│  │ BuyOrderStream   │  │ GetAllMakeOrders │  │ GetInvoice      │             │
│  │                  │  │ GetCustOrders    │  │ GetCustInvoices │             │
│  └─────────────────┘  │ GetMakerOrders   │  │ GetAllInvoices  │             │
│                        └─────────────────┘  └─────────────────┘             │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐     │
│  │                      data/ (PostgreSQL Repository)                   │     │
│  │  Models: Bread, Order, BuyOrder, MakeOrder, Customer, Maker, Invoice│     │
│  │  CRUD operations, transactions, query builders                       │     │
│  └─────────────────────────────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────────────────────────────┘


┌──────────────────────────────────────────────────────────────────────────────┐
│                             broker/                                          │
│                         (RabbitMQ Consumer)                                  │
│──────────────────────────────────────────────────────────────────────────────│
│                                                                              │
│  Queue: buy-bread-order ──▶ processOrderItems ──▶ publish bread-bought       │
│                              • check canFulfillOrder                         │
│                              • adjust quantity                               │
│                              • update order status                           │
│                              • check stock < 10 → publish make-bread-order   │
│                                                                              │
│  QoS: channel.Qos(1, 0, false) — one message at a time                       │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘


┌──────────────────────────────────────────────────────────────────────────────┐
│                             makers/                                          │
│                       (Restock Consumer)                                     │
│──────────────────────────────────────────────────────────────────────────────│
│                                                                              │
│  Queue: make-bread-order ──▶ processMakeBreadMessage ──▶ restock DB          │
│                              • parse bread type                              │
│                              • add inventory to PostgreSQL                   │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘


┌──────────────────────────────────────────────────────────────────────────────┐
│                            buyers/                                           │
│                        (gRPC Client)                                         │
│──────────────────────────────────────────────────────────────────────────────│
│                                                                              │
│  • buySomeBread()     — sends BuyBread RPC, returns order ID                 │
│  • buyBreadStream()   — subscribes to BuyBreadStream, streams results        │
│                                                                              │
│  Retry: up to 20 × 5s = 100s window for broker processing                    │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘


┌──────────────────────────────────────────────────────────────────────────────┐
│                           frontend/                                          │
│                        (HTTP Web Server)                                     │
│──────────────────────────────────────────────────────────────────────────────│
│                                                                              │
│  Routes:                                                                     │
│  /                        — home page                                        │
│  /admin/*                  — admin panel (login, dashboard, CRUD)            │
│  /portal/*                 — customer portal (login, orders, invoices)       │
│  /static/*                 — CSS, JS assets                                  │
│                                                                              │
│  Auth: JWT-based (admin + customer)                                          │
│  Templates: Go html/template                                                 │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## 4. Data Flow — Complete Order Lifecycle

```
  BUYER                          SERVER                        BROKER                    MAKERS
   │                               │                              │                        │
   │ 1. BuyBread(id, qty)          │                              │                        │
   │────────────────────────────▶  │                              │                        │
   │                               │ 2. Save order (PENDING)      │                        │
   │                               │──────────────────────────▶ PostgreSQL                 │
   │                               │                              │                        │
   │                               │ 3. Publish to                │                        │
   │                               │──────────────────────────────────────────────────────▶│
   │                               │    buy-bread-order queue                          make-bread-order
   │                               │                              │                        │
   │                               │                              │ 4. Consume message     │
   │                               │                              │◀───────────────────────│
   │                               │                              │                        │
   │                               │                              │ 5. canFulfillOrder?    │
   │                               │                              │ (check PostgreSQL)     │
   │                               │                              │                        │
   │                               │        ┌──── YES ───────────┤                        │
   │                               │        │                     │                        │
   │                               │        ▼                     │                        │
   │                               │ 6. Adjust qty, set COMPLETED │                        │
   │                               │──────────────────────────▶ PostgreSQL                 │
   │                               │                              │                        │
   │                               │ 7. Publish bread-bought     │                        │
   │                               │──────────────────────────────────────────────────────▶│
   │                               │                              │   (only if stock < 10) │
   │                               │                              │                        │
   │        ┌───── NO ─────────────┤                             │                        │
   │        │                       │ 8. Set INSUFFICIENT_STOCK   │                        │
   │        ▼                       │──────────────────────────▶ PostgreSQL                 │
   │                               │                              │                        │
   │                               │ 9. Publish bread-bought     │                        │
   │                               │──────────────────────────────────────────────────────▶│
   │                               │                              │                        │
   │ 10. Stream result back        │                              │                        │
   │◀─────────────────────────────  │                              │                        │
   │                               │                              │                        │
   │                               │                              │ 11. Consume             │
   │                               │                              │◀───────────────────────│
   │                               │                              │    make-bread-order     │
   │                               │                              │                        │
   │                               │                              │ 12. Parse bread type    │
   │                               │                              │ 13. Restock PostgreSQL  │
   │                               │                              │─────────────────────▶  │
```

## 5. Technology Stack

```
┌────────────────────────────────────────────────────────────────┐
│                      Frontend                                   │
│  Go (html/template) + Bootstrap CSS + Vanilla JS               │
│  Admin Panel: Dashboard, CRUD, Order Mgmt                      │
│  Customer Portal: Orders, Invoices                             │
└──────────────────────┬─────────────────────────────────────────┘
                       │ HTTP/REST
┌──────────────────────▼─────────────────────────────────────────┐
│                      gRPC Layer                                 │
│  Go + protobuf (proto/bread.proto)                              │
│  - gRPC server on :50051                                        │
│  - JWT auth (golang-jwt/jwt v5)                                 │
│  - bcrypt password hashing                                      │
└──────────────────────┬─────────────────────────────────────────┘
                       │ gRPC    AMQP
┌──────────────────────┼──────────────────────────────────────────┐
│                      │                                          │
│              ┌───────▼───────┐               ┌─────────────────▼───────┐
│              │   PostgreSQL  │               │      RabbitMQ           │
│              │   (data/)     │               │   (amqp091)             │
│              │               │               │   Queues:               │
│              │ • bread       │               │   • buy-bread-order     │
│              │ • orders      │               │   • bread-bought        │
│              │ • customers   │               │   • make-bread-order    │
│              │ • makers      │               └─────────────────────────┘
│              │ • invoices    │
│              └───────────────┘
└────────────────────────────────────────────────────────────────┘
```

## 6. Container Images

```
┌────────────────────────────────────────────────────────────────┐
│                  Docker Images (Docker Hub)                     │
│                  docker.io/calvarado2004/                       │
│                                                                 │
│  bakery-go-server   — server.dockerfile  (gRPC server)         │
│  bakery-go-broker   — broker.dockerfile  (RabbitMQ broker)     │
│  bakery-go-makers   — makers.dockerfile  (restock consumer)    │
│  bakery-go-buyers   — buyers.dockerfile  (gRPC client)         │
│  bakery-go-frontend — frontend.dockerfile  (HTTP web server)   │
└────────────────────────────────────────────────────────────────┘
```

## 7. Environment Configuration

```
┌────────────────────────────────────────────────────────────────┐
│                     .env / .env.local                          │
│                                                                 │
│  BAKERY_SERVICE_ADDR   — gRPC server address   (localhost:50051)│
│  RABBITMQ_SERVICE_ADDR — AMQP URL          (amqp://...:5672/)  │
│  DSN                   — PostgreSQL conn string                  │
│  JWT_SECRET            — JWT signing secret                      │
└────────────────────────────────────────────────────────────────┘
```

## 8. Service Boundary Analysis

### 8.1 Current State — Decoupled Boundary (Phase 10 Complete)

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  Buyer #1    │  │  Buyer #2    │  │  Buyer #N    │  ← External, independent
│  (gRPC + JWT)│  │  (gRPC + JWT)│  │  (gRPC + JWT)│
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │ gRPC (typed)     │ gRPC (typed)     │ gRPC (typed)
       │ Rate limited     │ Rate limited     │ Rate limited
       │ RBAC enforced    │ RBAC enforced    │ RBAC enforced
       └────────┬─────────┴────────┬─────────┘
                │                  │
                ▼                  ▼
        ┌─────────────────────────────────┐
        │         SERVER (gRPC)           │  ← Foundation: API gateway + DB
        │  - Auth + RBAC                  │     (only service with PostgreSQL)
        │  - Writes to PostgreSQL only    │
        │  - No AMQP knowledge            │
        │  - Async: returns immediately   │
        └────────────┬────────────────────┘
                     │ PostgreSQL INSERT
                     │ (status = pending)
                     ▼
        ┌─────────────────────────────────┐
        │     PostgreSQL                  │  ← Source of truth
        │     (no DSN for buyers)         │
        └────────┬───────────────────────┘
                 │ PG NOTIFY / LISTEN
                 │ or: polling buy_order
                 ▼
        ┌─────────────────────────────────┐
        │         BROKER                  │  ← Internal: pure dispatcher
        │  - Declares buy-bread-order     │     (zero DB access)
        │  - Declares bread-bought        │     (via gRPC BrokerService)
        │  - Batch matching engine        │
        │  - Circuit breaker + retry      │
        └────────────┬────────────────────┘
                     │ AMQP: MatchingResult
                     ▼
        ┌─────────────────────────────────┐
        │     bread-bought queue          │
        │  Consumed by:                   │
        │  - Server (stream dispatch)     │
        │  - External services (loyalty,  │
        │    analytics, notifications)     │
        └─────────────────────────────────┘

        ┌─────────────────────────────────┐
        │  make-bread-order (external)    │  ← Declared by makers
        │  Consumed by:                   │
        │  - External makers (AMQP only)  │
        │  - Separate: pending_make_orders│
        │    (server auto-replenish)      │
        └─────────────────────────────────┘

        ┌──────────────┐  ┌──────────────┐
        │  Maker #1    │  │  Maker #N    │  ← External, independent
        │  (AMQP only) │  │  (AMQP only) │
        └──────────────┘  └──────────────┘
```

**Key boundary principles (enforced):**

| Principle | Enforcement |
|-----------|-------------|
| **Ownership** | Broker declares `buy-bread-order` + `bread-bought`; makers declare `make-bread-order` |
| **Broker has zero DB** | Broker communicates via `BrokerService` gRPC (Phase 10.1) |
| **Server is foundation** | Only service with PostgreSQL access |
| **Separation** | Auto-replenishment writes to `pending_make_orders` table with `source=auto` (Phase 10.7) |
| **Independent contracts** | AMQP uses `MatchingResult` proto type, not internal `data.BuyOrder` (Phase 10.5) |
| **Typed external API** | Buyers use gRPC + JWT (no raw AMQP); makers use AMQP |
| **No direct DB** | Buyers have no DSN (Phase 10.9) |
| **Resilience** | Rate limiter (10 req/s), circuit breakers, RBAC (Phase 10.10) |

### 8.2 Phase 10 Architecture Summary

Phase 10 decoupled the external/internal boundary of the bakery platform:

| Component | Role | External Access |
|-----------|------|----------------|
| **Server** | Foundation: gRPC API + PostgreSQL owner | Buyers (gRPC + JWT), Frontend (HTTP) |
| **Broker** | Pure dispatcher: RabbitMQ consume/publish | Server (gRPC BrokerService only) |
| **Makers** | External providers: consume `make-bread-order` | RabbitMQ only |
| **Buyers** | External clients: gRPC + JWT | gRPC only (no DB) |
| **Frontend** | HTTP interface for admin + customers | HTTP + gRPC |

See [ARCHITECTURE_AUDIT.md §10](ARCHITECTURE_AUDIT.md#10-externalinternal-boundary-coupling-issues) for the full audit and remediation plan.
