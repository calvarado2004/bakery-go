# Bakery Service — gRPC Services

## Table of Contents

1. [Overview](#overview)
2. [Proto Definition Summary](#proto-definition-summary)
3. [Service Implementations](#service-implementations)
   - [MakeBreadServer](#makebreadserver)
   - [CheckInventoryServer](#checkinventoryserver)
   - [BuyBreadServer](#buybreadserver)
   - [BuyOrderServiceServer](#buyorderserviceserver)
   - [RemoveOldBreadServer](#removeoldbreadserver)
   - [AuthServiceServer](#authserviceserver)
   - [AdminServiceServer](#adminserviceserver)
   - [InvoiceServiceServer](#invoiceserviceserver)
   - [CustomerPortalServiceServer](#customerportalserviceserver)
4. [Shared Types](#shared-types)
5. [gRPC Server Bootstrap](#grpc-server-bootstrap)
6. [Error Handling Conventions](#error-handling-conventions)
7. [Known Issues and Improvements](#known-issues-and-improvements)

---

## Overview

All gRPC service implementations reside in the `server/` package. The proto file (`proto/bread.proto`) defines the service contracts. Generated code lives alongside the proto file in `proto/`.

The server process registers all nine service implementations on a single gRPC server listening on `BAKERY_SERVICE_ADDR` (default: `localhost:50051`).

All service structs hold a reference to a `*RabbitMQBakery` instance, which carries the `Repository`, `RabbitMQ` connection, and channels needed for cross-service coordination.

---

## Proto Definition Summary

File: `proto/bread.proto`

### Services

| Service Name              | File                 | RPC Methods |
|---------------------------|----------------------|-------------|
| `MakeBread`               | `gRPCBakery.go`      | `BakeBread`, `SendBreadToBakery`, `MadeBreadStream` |
| `CheckInventory`          | `gRPCBakery.go`      | `CheckBreadInventory`, `CheckBreadInventoryStream` |
| `BuyBread`                | `gRPCBakery.go`      | `BuyBread`, `BuyBreadStream` |
| `BuyOrderService`         | `gRPCBakery.go`      | `BuyOrder`, `BuyOrderStream` |
| `RemoveOldBread`          | `gRPCBakery.go`      | `RemoveBread`, `RemoveBreadStream` |
| `AdminService`            | `gRPCAdmin.go`       | 14 methods — see below |
| `AuthService`             | `gRPCAuth.go`        | `AdminLogin`, `CustomerLogin`, `ValidateToken`, `CreateAdminUser` |
| `InvoiceService`          | `gRPCInvoice.go`     | `CreateInvoice`, `GetInvoice`, `GetCustomerInvoices`, `GetAllInvoices` |
| `CustomerPortalService`   | `gRPCInvoice.go`     | `GetMyOrders`, `GetMyInvoices`, `GetOrderDetails` |

### Key Message Types

| Message                    | Fields (summary)                                                        |
|----------------------------|-------------------------------------------------------------------------|
| `BreadRequest`             | `bread_id`, `quantity`, `buy_order_uuid`, `customer_id`, `breads[]`     |
| `BreadResponse`            | `message`, `bread` (Bread), `buy_order` (BuyOrder)                      |
| `Bread`                    | `id`, `name`, `price`, `quantity`, `description`, `type`, `status`, `image`, `created_at`, `updated_at` |
| `BuyOrder`                 | `id`, `customer_id`, `buy_order_uuid`, `status`, `details[]`            |
| `BuyOrderRequest`          | `buy_order_uuid`, `customer_id`                                          |
| `BuyOrderResponse`         | `buy_order` (BuyOrder), `message`                                        |
| `LoginRequest`             | `username`, `password`                                                   |
| `LoginResponse`            | `token`, `admin_user` (AdminUser), `message`                            |
| `CustomerLoginRequest`     | `email`, `password`                                                      |
| `CustomerLoginResponse`    | `token`, `customer` (Customer), `message`                               |
| `Invoice`                  | `id`, `buy_order_id`, `customer_id`, `invoice_number`, `subtotal`, `tax`, `total`, `status`, `created_at`, `due_date`, `paid_at`, `items[]` |
| `DashboardStats`           | `total_orders`, `total_revenue`, `total_products`, `total_customers`, `total_bread_makers`, `low_stock_count` |
| `Empty`                    | _(no fields)_                                                            |

---

## Service Implementations

### MakeBreadServer

**File:** `server/gRPCBakery.go`
**Type:** `MakeBreadServer struct { bakery *RabbitMQBakery }`

This service handles bread production lifecycle. It is primarily used in internal workflows for queueing bread production jobs.

#### `BakeBread(ctx, *BreadRequest) -> *BreadResponse`

Publishes each bread item in the request to the `"bread-to-make"` RabbitMQ queue. Returns a `BreadResponse` with a confirmation message.

- Serialises each `Bread` to JSON
- Publishes to queue with `amqp.Publishing{ContentType: "application/json"}`
- Returns `"Baking bread"` on success

#### `SendBreadToBakery(ctx, *BreadRequest) -> *BreadResponse`

Publishes each bread item to the `"bread-in-bakery"` queue, signalling that bread has been placed in the oven.

- Same publishing pattern as `BakeBread`
- Returns `"Sent to bakery"` on success

#### `MadeBreadStream(ctx, *BreadRequest, stream) -> error`

Server-streaming RPC. Consumes messages from the `"bread-in-bakery"` queue and sends each as a `BreadResponse` to the client.

- Registers a consumer on the queue
- For each delivery, deserialises `Bread` JSON and sends `BreadResponse` with message `"Bread made"`
- Returns when the queue channel is exhausted or context is cancelled

---

### CheckInventoryServer

**File:** `server/gRPCBakery.go`
**Type:** `CheckInventoryServer struct { bakery *RabbitMQBakery }`

#### `CheckBreadInventory(ctx, *BreadRequest) -> *BreadResponse`

Returns a snapshot of all available bread from the database.

- Calls `repo.GetAvailableBread()`
- Converts each `data.Bread` to a proto `Bread` message
- Returns `BreadResponse` containing the full list

#### `CheckBreadInventoryStream(ctx, *BreadRequest, stream) -> error`

Server-streaming RPC. Sends the current inventory to the client every **15 seconds**.

- Runs in an infinite loop until context is cancelled
- Calls `GetAvailableBread()` on each tick
- Sends individual `BreadResponse` per bread item
- Used by the Frontend's SSE `/stream` endpoint to push live inventory updates

---

### BuyBreadServer

**File:** `server/gRPCBakery.go`
**Type:** `BuyBreadServer struct { bakery *RabbitMQBakery }`

#### `BuyBread(ctx, *BreadRequest) -> *BreadResponse`

Initiates a bread purchase by publishing a buy order to RabbitMQ.

1. Constructs a `data.BuyOrder` from the request
2. Inserts the order into the database via `repo.InsertBuyOrder`
3. Inserts an `OutboxMessage` with the JSON payload
4. Publishes the order JSON to the `"buy-bread-order"` queue
5. Creates an `OrderStatus` entry keyed by `buy_order_uuid`
6. Calls `getBuyResponse()` to wait for Broker's confirmation (via `"bread-bought"` queue)
7. Returns a `BreadResponse` with the confirmed `BuyOrder`

**Coordination Mechanism:**

`OrderStatus` is a struct containing a channel (`chan *data.BuyOrder`) and an error channel. `BuyBread` blocks on this channel until the Broker confirms the purchase (or a timeout occurs).

```go
type OrderStatus struct {
    OrderChan chan *data.BuyOrder
    ErrChan   chan error
}
```

#### `BuyBreadStream(ctx, *BreadRequest, stream) -> error`

Server-streaming RPC. Polls the database for the confirmed order.

- Polls `repo.GetBuyOrderByUUID(uuid)` every **5 seconds**
- Retries up to **10 times** (50-second maximum window)
- Sends a `BreadResponse` for each bread item in the order once status = `"Processed"`
- Returns `codes.Canceled` if polling exhausts retries without confirmation

> **Issue:** Polling with fixed intervals is inefficient. This should be replaced with a channel-based notification from the `bread-bought` consumer.

---

### BuyOrderServiceServer

**File:** `server/gRPCBakery.go`
**Type:** `BuyOrderServiceServer struct { bakery *RabbitMQBakery }`

#### `BuyOrder(ctx, *BuyOrderRequest) -> *BuyOrderResponse`

Retrieves a single buy order by its UUID.

- Calls `repo.GetBuyOrderByUUID(uuid)`
- Converts to proto `BuyOrder`
- Returns `BuyOrderResponse`

#### `BuyOrderStream(ctx, *BuyOrderRequest, stream) -> error`

Server-streaming RPC. Streams buy order(s) to the client.

- If `buy_order_uuid` is provided: streams the single matching order
- If empty: streams all orders from `repo.GetAllBuyOrders()`
- Each order is sent as a `BuyOrderResponse`

---

### RemoveOldBreadServer

**File:** `server/gRPCBakery.go`
**Type:** `RemoveOldBreadServer struct { bakery *RabbitMQBakery }`

#### `RemoveBread(ctx, *BreadRequest) -> *BreadResponse`

Publishes each bread item in the request to the `"bread-removed"` queue.

- Used to signal that stale/expired bread should be removed from inventory

#### `RemoveBreadStream(ctx, *BreadRequest, stream) -> error`

Consumes messages from the `"bread-removed"` queue and streams each as a `BreadResponse`.

- Same consumer pattern as `MadeBreadStream`

---

### AuthServiceServer

**File:** `server/gRPCAuth.go`
**Type:** `AuthServiceServer struct { bakery *RabbitMQBakery }`

Handles authentication for both admin users and customers.

#### JWT Claims Structure

```go
type Claims struct {
    UserID   int    `json:"user_id"`
    Username string `json:"username"`
    UserType string `json:"user_type"`  // "admin" or "customer"
    Role     string `json:"role"`
    jwt.RegisteredClaims
}
```

Tokens are signed with `HS256` and expire after **24 hours**.

#### `AdminLogin(ctx, *LoginRequest) -> *LoginResponse`

1. Fetches `AdminUser` by username from database
2. Verifies bcrypt password hash via `repo.PasswordMatches`
3. Generates JWT with `user_type = "admin"`, `role` from database
4. Returns token and `AdminUser` proto

#### `CustomerLogin(ctx, *CustomerLoginRequest) -> *CustomerLoginResponse`

1. Fetches `Customer` by email from database
2. Verifies bcrypt password
3. Generates JWT with `user_type = "customer"`
4. Returns token and `Customer` proto

#### `ValidateToken(ctx, *ValidateTokenRequest) -> *ValidateTokenResponse`

1. Parses JWT string using `jwt.ParseWithClaims`
2. Validates signature and expiration
3. Returns `valid = true/false` with parsed claims if valid

#### `CreateAdminUser(ctx, *CreateAdminUserRequest) -> *AdminUser`

1. Hashes plaintext password with bcrypt
2. Inserts new `AdminUser` into database
3. Returns the created user proto (without password)

#### `getJWTSecret() []byte`

Reads `JWT_SECRET` environment variable. Falls back to `"bakery-go-secret-key-change-in-production"` if not set.

> **Security Warning:** The fallback secret must not be used in production. Startup should panic if `JWT_SECRET` is not set.

---

### AdminServiceServer

**File:** `server/gRPCAdmin.go`
**Type:** `AdminServiceServer struct { bakery *RabbitMQBakery }`

Provides all administrative operations.

#### Dashboard

| Method | Description |
|--------|-------------|
| `GetDashboardStats(ctx, *Empty) -> *DashboardStats` | Returns aggregated stats: total orders, revenue, products, customers, bread makers, low stock count |

#### Bread Management

| Method | Description |
|--------|-------------|
| `GetAllBread(ctx, *Empty) -> *BreadList` | Returns all bread records |
| `GetBreadById(ctx, *BreadIdRequest) -> *Bread` | Returns single bread by ID |
| `CreateBread(ctx, *CreateBreadRequest) -> *Bread` | Inserts new bread; sets status = `"available"` |
| `UpdateBread(ctx, *UpdateBreadRequest) -> *Bread` | Updates bread fields; returns updated record |
| `DeleteBread(ctx, *DeleteBreadRequest) -> *Empty` | Deletes bread by ID |
| `GetLowStockAlerts(ctx, *Empty) -> *BreadList` | Returns bread with `quantity < 10` |

#### Order Management

| Method | Description |
|--------|-------------|
| `GetAllOrders(ctx, *Empty) -> *BuyOrderList` | Returns all buy orders with line item details |
| `UpdateOrderStatus(ctx, *UpdateOrderStatusRequest) -> *BuyOrder` | Updates order status; auto-generates invoice when status = `"completed"` |
| `GetAllMakeOrders(ctx, *Empty) -> *MakeOrderList` | Returns all production orders |

#### People Management

| Method | Description |
|--------|-------------|
| `GetAllCustomers(ctx, *Empty) -> *CustomerList` | Returns all customers |
| `GetAllBreadMakers(ctx, *Empty) -> *BreadMakerList` | Returns all bread makers |
| `GetCustomerOrders(ctx, *CustomerIdRequest) -> *CustomerOrdersResponse` | Returns customer details plus all their orders |
| `GetMakerOrders(ctx, *BreadMakerIdRequest) -> *MakerOrdersResponse` | Returns bread maker plus their production orders |

#### Invoice Auto-Generation

When `UpdateOrderStatus` is called with `status = "completed"`:

1. Calls `generateInvoiceForOrder(orderID)`
2. Retrieves `order_details` for the order
3. Calculates subtotal from `order_details.price * quantity`
4. Applies **10% tax**
5. Generates `invoice_number` in format: `INV-<orderID>-<unix_timestamp>`
6. Inserts invoice with `due_date = now() + 30 days`
7. Inserts one `InvoiceItem` per bread line

> **Issue:** Tax rate inconsistency — `AdminService` applies 10% while `InvoiceService.CreateInvoice` applies 8%. These should be unified via a constant or configuration value.

---

### InvoiceServiceServer

**File:** `server/gRPCInvoice.go`
**Type:** `InvoiceServiceServer struct { bakery *RabbitMQBakery }`

#### `CreateInvoice(ctx, *CreateInvoiceRequest) -> *Invoice`

1. Checks if an invoice already exists for the order via `repo.GetInvoiceByOrderID`
2. If found, returns existing invoice (idempotent)
3. Otherwise, calculates totals with **8% tax**
4. Inserts invoice and line items
5. Returns created invoice proto

#### `GetInvoice(ctx, *InvoiceIdRequest) -> *Invoice`

Returns a single invoice by ID via `repo.GetInvoiceByID`.

#### `GetCustomerInvoices(ctx, *CustomerInvoicesRequest) -> *InvoiceList`

Returns all invoices for a specific customer.

#### `GetAllInvoices(ctx, *Empty) -> *InvoiceList`

Returns all invoices in the system.

---

### CustomerPortalServiceServer

**File:** `server/gRPCInvoice.go`
**Type:** `CustomerPortalServiceServer struct { bakery *RabbitMQBakery }`

Customer-facing read-only access to their own orders and invoices.

#### `GetMyOrders(ctx, *CustomerIdRequest) -> *CustomerOrdersResponse`

Returns customer details and their complete order history.

- Calls `repo.GetCustomerByID` and `repo.GetCustomerOrders`

#### `GetMyInvoices(ctx, *CustomerIdRequest) -> *InvoiceList`

Returns all invoices belonging to the specified customer.

#### `GetOrderDetails(ctx, *BuyOrderIdRequest) -> *BuyOrderDetailsResponse`

Returns a single order with full line items.

- **Authorization check:** Verifies the order's `customer_id` matches the requesting customer's ID
- Returns `codes.PermissionDenied` if the IDs do not match

---

## Shared Types

### `RabbitMQBakery`

```go
type RabbitMQBakery struct {
    Repo         data.Repository
    Rabbit       *amqp.Connection
    Config       Config
    OrderStatuses map[string]*OrderStatus
}
```

All gRPC server structs embed or reference `*RabbitMQBakery`. This is the primary dependency injection container.

### `Config`

```go
type Config struct {
    Repo       data.Repository
    HTTPClient *http.Client
}
```

### `OrderStatus`

```go
type OrderStatus struct {
    OrderChan chan *data.BuyOrder
    ErrChan   chan error
}
```

Used to coordinate between `BuyBread` (publisher) and `getBuyResponse` (consumer) within the server process.

---

## gRPC Server Bootstrap

File: `server/main.go`

```
1. connectToDB()          → *pgx.Conn
2. setupRepo(conn)        → data.Repository
3. NewRabbitMQBakery()    → *RabbitMQBakery
4. init() (rabbitBakery)  → declares all queues
5. initializeBakery()     → seeds DB with 7 bread types
6. grpc.NewServer()       → register all 9 service implementations
7. net.Listen("tcp", addr)
8. go BakeryServer(listener, grpcServer)
    ├── go checkBread()   → background: publish make orders every 30s
    └── grpcServer.Serve(listener)
```

---

## Error Handling Conventions

All gRPC handlers follow this general pattern:

```go
if err != nil {
    return nil, status.Errorf(codes.Internal, "description: %v", err)
}
```

Standard gRPC status codes used:

| Code                  | Usage Context                                              |
|-----------------------|------------------------------------------------------------|
| `codes.Internal`      | Database errors, serialisation failures, queue publish errors |
| `codes.NotFound`      | Record not found in database                               |
| `codes.Canceled`      | Stream exhausted retries or context cancelled              |
| `codes.PermissionDenied` | Customer attempting to access another customer's data   |
| `codes.InvalidArgument` | _(currently not used — should be added for input validation)_ |

---

## Known Issues and Improvements

| # | Location | Issue | Recommendation |
|---|----------|-------|----------------|
| 1 | `gRPCAuth.go` | Default JWT secret hardcoded | Panic at startup if `JWT_SECRET` env var is not set |
| 2 | `gRPCAdmin.go` | Tax rate inconsistency (10% vs 8%) | Extract `TaxRate` constant; use consistently |
| 3 | `gRPCBakery.go` | `BuyBreadStream` uses polling | Replace with channel notification from `bread-bought` consumer |
| 4 | All files | `RabbitMQBakery` struct duplicated in `server/` and `broker/` | Extract to `internal/rabbitmq` shared package |
| 5 | `gRPCAdmin.go` | No input validation on `CreateBread`, `UpdateBread` | Add field validation; return `codes.InvalidArgument` |
| 6 | `gRPCAuth.go` | `CreateAdminUser` has no authorization check | Any caller can create admin users; should require admin token |
| 7 | All services | No request/response logging | Add gRPC interceptors for structured request logging |
| 8 | All services | No distributed tracing | Add OpenTelemetry interceptors |
| 9 | `gRPCBakery.go` | `CheckBreadInventoryStream` loops forever | Add graceful shutdown via context cancellation |
| 10 | `server/main.go` | All services share one gRPC server | Consider separating public and internal services |
