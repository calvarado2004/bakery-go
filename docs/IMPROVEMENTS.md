# Bakery Service — Code Quality and Professionalism Improvements

## Table of Contents

1. [Overview](#overview)
2. [Critical (Security)](#critical-security)
3. [High Priority](#high-priority)
4. [Medium Priority](#medium-priority)
5. [Low Priority / Polish](#low-priority--polish)
6. [Refactoring Roadmap](#refactoring-roadmap)

---

## Overview

This document catalogues all identified issues across the codebase, organised by severity. Each item includes a description of the current state, its risk, and a concrete recommended fix. This list should be reviewed and items promoted to a task tracker (e.g., GitHub Issues) before work begins.

---

## Critical (Security)

These issues create immediate security exposure and must be resolved before any non-development deployment.

### C-1: Hardcoded JWT Secret

**File:** `server/gRPCAuth.go`

**Current:** Falls back to `"bakery-go-secret-key-change-in-production"` when `JWT_SECRET` is not set.

**Risk:** Anyone who knows this default secret can forge valid JWT tokens for any user or role.

**Fix:**
```go
func getJWTSecret() []byte {
    secret := os.Getenv("JWT_SECRET")
    if secret == "" {
        log.Fatal("JWT_SECRET environment variable is not set")
    }
    return []byte(secret)
}
```

---

### C-2: Plain HTTP gRPC — No TLS

**Files:** `frontend/cmd/web/main.go`, `buyers/main.go`

**Current:** `grpc.WithTransportCredentials(insecure.NewCredentials())` used everywhere.

**Risk:** All inter-service communication (including auth tokens, order data, customer PII) travels unencrypted.

**Fix:** Implement server TLS on the gRPC server and use `credentials.NewTLS(tlsConfig)` on clients. Use a self-signed certificate for internal Kubernetes traffic or mutual TLS.

---

### C-3: Seed Credentials in Version Control

**File:** `bakery.sql`

**Current:** The SQL schema includes bcrypt hashes of known default passwords (`password123`, `admin123`).

**Risk:** While bcrypt hashes are not reversible trivially, the known plaintexts are documented in `README.md`. Any deployed instance that retains seed data is compromised.

**Fix:** Remove seed credentials from `bakery.sql`. Provide a separate `seed-dev.sql` file that is explicitly excluded from production `docker-compose` or Kubernetes Job manifests.

---

### C-4: No CSRF Protection on Forms

**File:** `frontend/cmd/web/main.go`

**Current:** POST handlers (`/admin/bread/create`, `/admin/orders/{id}/status`, etc.) have no CSRF token validation.

**Risk:** An attacker can craft a malicious page that submits forms to the admin portal on behalf of an authenticated admin user.

**Fix:** Add `gorilla/csrf` middleware:
```go
csrfMiddleware := csrf.Protect(
    []byte(os.Getenv("CSRF_KEY")),
    csrf.Secure(true),
)
router.Use(csrfMiddleware)
```
Add `{{ .csrfField }}` to all POST forms in templates.

---

### C-5: `CreateAdminUser` Has No Authorization Check

**File:** `server/gRPCAuth.go`

**Current:** Any gRPC client can call `CreateAdminUser` without presenting any credentials.

**Risk:** Unauthenticated actor can create admin accounts.

**Fix:** Add a gRPC interceptor that validates an admin JWT before allowing `CreateAdminUser` to proceed. Alternatively, restrict this RPC to be called only from `localhost` or a trusted internal address.

---

## High Priority

These issues will cause bugs or degrade reliability in a production workload.

### H-1: `InsertInvoice` Is Not Transactional

**File:** `data/repository.go`

**Current:** Inserts the invoice header row, then inserts each `InvoiceItem` in a loop with individual `INSERT` statements — no transaction.

**Risk:** If the process crashes after inserting the header but before all items, the database is left in a partially consistent state.

**Fix:**
```go
tx, err := r.DB.BeginTx(ctx, pgx.TxOptions{})
// ... all inserts
tx.Commit(ctx)
```

---

### H-2: Race Condition in `bread-bought` Consumer

**File:** `server/rabbitBakery.go`

**Current:** Both `getBuyResponse()` and `processBreadsBought()` register consumers on the same `"bread-bought"` queue. With AMQP round-robin dispatch, a confirmation message may be consumed by the wrong goroutine.

**Fix:** Consolidate into a single consumer goroutine that dispatches received messages to the correct `OrderStatus` channel by `BuyOrderUUID`:
```go
// Central dispatcher
go func() {
    for delivery := range breadBoughtChan {
        var order data.BuyOrder
        json.Unmarshal(delivery.Body, &order)
        if status, ok := bakery.OrderStatuses[order.BuyOrderUUID]; ok {
            status.OrderChan <- &order
        }
        delivery.Ack(false)
    }
}()
```

---

### H-3: Broker Is Not Idempotent

**File:** `broker/main.go`

**Current:** If the same `buy-bread-order` message is delivered twice (due to AMQP redelivery), the Broker will decrement bread quantities a second time, causing incorrect inventory.

**Fix:** Before processing, check `orders_processed` table for the `BuyOrderUUID`:
```go
if _, err := repo.GetBuyOrderByUUID(order.BuyOrderUUID); err == nil {
    // Already processed — acknowledge and skip
    msg.Ack(false)
    return
}
```

---

### H-4: Outbox Re-Publish Does Not Mark Messages as Sent

**File:** `broker/main.go` (outbox goroutine)

**Current:** The background goroutine calls `GetUnprocessedOutboxMessages` and re-publishes each, but does not call `DeleteOutboxMessage` or update `sent = true` afterwards.

**Risk:** Duplicate message delivery on every 45-second cycle. The `outbox` table will grow without bound.

**Fix:**
```go
for _, msg := range unprocessed {
    if err := ch.PublishWithContext(...); err == nil {
        repo.DeleteOutboxMessage(msg.ID)
    }
}
```

---

### H-5: Duplicated Structs Across Services

**Files:** `server/main.go`, `broker/main.go`

**Current:** `RabbitMQBakery`, `OrderStatus`, and `Config` are defined identically in both `server/` and `broker/`.

**Risk:** Any change to these structs must be made in two places; inconsistencies will cause subtle bugs.

**Fix:** Extract to `internal/bakery/`:
```
internal/
└── bakery/
    ├── config.go        # Config, RabbitMQBakery, OrderStatus structs
    └── connection.go    # connectToDB, openDB functions
```

---

### H-6: No Graceful Shutdown

**Files:** `server/main.go`, `broker/main.go`, `makers/main.go`, `frontend/cmd/web/main.go`

**Current:** Services do not handle `SIGTERM` or `SIGINT`. In Kubernetes, pods receive `SIGTERM` before being killed; if not handled, in-flight requests and messages may be dropped.

**Fix:**
```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

log.Info("Shutting down gracefully...")
grpcServer.GracefulStop()
httpServer.Shutdown(ctx)
rabbitConn.Close()
db.Close(ctx)
```

---

### H-7: No Health Check Endpoints

**All services**

**Current:** No `/healthz` or `/readyz` endpoint exists on any service. gRPC health protocol is not implemented.

**Risk:** Kubernetes liveness/readiness probes cannot function. Services that are up but not yet connected to dependencies are treated as healthy.

**Fix:** Add HTTP health endpoints to the Frontend:
```go
router.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("ok"))
})
```
Add gRPC health service to the Server:
```go
import "google.golang.org/grpc/health/grpc_health_v1"
healthServer := health.NewServer()
grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
```

---

## Medium Priority

These issues affect maintainability, developer experience, or non-critical production behaviour.

### M-1: Database Connection Logic Duplicated

**Files:** `server/main.go`, `broker/main.go`, `makers/main.go`

All three services duplicate the same `connectToDB` / `openDB` retry loop. Consolidate into `internal/db/`.

---

### M-2: Tax Rate Inconsistency

**Files:** `server/gRPCAdmin.go` (10%), `server/gRPCInvoice.go` (8%)

Two different tax rates are applied depending on how an invoice is generated. Define a single constant:
```go
const TaxRate = 0.10
```
Apply consistently across both invoice creation paths.

---

### M-3: `initializeBakery` Seeds Data Unconditionally

**File:** `server/rabbitBakery.go`

On every server start, 7 bread types are inserted without checking for existence. Over time this creates duplicate records.

**Fix:** Use `INSERT ... ON CONFLICT (name) DO NOTHING` or check for existence before inserting.

---

### M-4: No Structured Logging Context

**All services**

`logrus` is imported but used in an ad-hoc way. No consistent log fields (e.g., `order_id`, `customer_id`, `request_id`) are attached to log entries, making debugging difficult.

**Fix:** Use `logrus.WithFields` consistently:
```go
log.WithFields(logrus.Fields{
    "order_uuid": order.BuyOrderUUID,
    "customer_id": order.CustomerID,
}).Info("Processing buy order")
```

---

### M-5: No Input Validation on gRPC Handlers

**File:** `server/gRPCAdmin.go`, `server/gRPCAuth.go`

`CreateBread`, `UpdateBread`, `CreateAdminUser`, etc. do not validate required fields. An empty `name` or negative `price` can be inserted into the database.

**Fix:** Add validation at the handler level before any database call:
```go
if req.Name == "" {
    return nil, status.Errorf(codes.InvalidArgument, "name is required")
}
if req.Price <= 0 {
    return nil, status.Errorf(codes.InvalidArgument, "price must be positive")
}
```

---

### M-6: No Pagination on List Endpoints

**Files:** `server/gRPCAdmin.go`, `frontend/cmd/web/admin_handlers.go`

All `GetAll*` RPCs return the complete dataset. This will cause performance degradation as data grows.

**Fix:** Add `page` and `page_size` fields to request messages, and implement `LIMIT` / `OFFSET` in queries.

---

### M-7: `BuyBreadStream` Uses Polling

**File:** `server/gRPCBakery.go`

Polls the database every 5 seconds for 50 seconds. This is inefficient and adds unnecessary DB load.

**Fix:** After the `bread-bought` message is received by the central dispatcher (see H-2), push the update directly to the waiting channel. Remove the DB polling loop.

---

### M-8: No `docker-compose.yml`

**Root directory**

Without a compose file, developers must manually start PostgreSQL, RabbitMQ, and all five services. This creates friction and inconsistency.

**Fix:** Add `docker-compose.yml` with all services, PostgreSQL, and RabbitMQ. Apply schema via an init container or `entrypoint`.

---

### M-9: Bread Quantity Can Go Negative

**File:** `broker/main.go`, `data/repository.go`

There is no `CHECK (quantity >= 0)` constraint in the database schema, and the Broker does not verify sufficiency before decrementing in all code paths.

**Fix:**
1. Add `ALTER TABLE bread ADD CONSTRAINT bread_quantity_non_negative CHECK (quantity >= 0);`
2. In the Broker, check `current_quantity >= requested_quantity` before calling `AdjustBreadQuantity`.

---

### M-10: Frontend Has No Error Pages

**File:** `frontend/cmd/web/`

Errors from gRPC calls or template rendering return either a blank page or an HTTP 500 with no user-facing message.

**Fix:** Add a `renderError(w, r, statusCode, message string)` helper and dedicated `error.html` template. Return appropriate HTTP status codes (404, 403, 500).

---

## Low Priority / Polish

### L-1: `README.md` Is Incomplete

The existing README describes the project at a high level but lacks setup instructions, prerequisites, and development workflow. Update to include: prerequisites (Go, Docker), local development steps, environment variable reference, and a quickstart.

### L-2: No `Makefile`

Common developer tasks (`go test`, `go build`, `protoc`, `docker-compose up`) should be encoded in a `Makefile` for discoverability and consistency.

### L-3: Proto-Generated Files Committed to Repository

Generated `.pb.go` files in `proto/` are committed. This creates merge conflicts and drift. Add `proto/` generated files to `.gitignore` and document the `protoc` command in the `Makefile`.

### L-4: Hard-Coded 35-Second Sleep in Buyers Service

**File:** `buyers/main.go`

The 35-second delay between purchase cycles is hard-coded. Extract to an environment variable for easier testing and configuration.

### L-5: No `.env.example` File

No template exists for required environment variables. Add `.env.example` with all variables and placeholder values.

### L-6: No Linting Configuration

Add `.golangci.yml` with standard linters (`errcheck`, `govet`, `staticcheck`, `gofmt`, `misspell`). Run in CI.

```yaml
# .golangci.yml
linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - gofmt
    - misspell
    - gocyclo
    - unused
```

### L-7: No API Documentation

gRPC services have no machine-readable API documentation. Add `buf.gen.yaml` to generate OpenAPI/Swagger documentation from the proto file, or publish the `.proto` file in a developer-facing `docs/` directory.

---

## Refactoring Roadmap

A suggested sequence for addressing the above issues without breaking functionality:

| Phase | Items | Goal |
|-------|-------|------|
| **Phase 1 — Security Hardening** | C-1, C-2, C-3, C-4, C-5 | Eliminate critical vulnerabilities |
| **Phase 2 — Reliability** | H-1, H-2, H-3, H-4, H-6, H-7 | Eliminate data loss and race conditions |
| **Phase 3 — Shared Infrastructure** | H-5, M-1 | Consolidate duplicated code |
| **Phase 4 — Testing Foundation** | Add `MockRepository`, unit tests per `TESTING_PLAN.md` | Achieve unit test coverage targets |
| **Phase 5 — Correctness** | M-2, M-3, M-5, M-9 | Fix business logic bugs |
| **Phase 6 — Developer Experience** | M-8, L-1–L-7 | Improve onboarding and workflow |
| **Phase 7 — Observability** | M-4, add OpenTelemetry, Prometheus metrics | Production readiness |
| **Phase 8 — Performance** | M-6, M-7 | Scale readiness |
