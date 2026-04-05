# Bakery Service — Frontend

## Table of Contents

1. [Overview](#overview)
2. [Project Structure](#project-structure)
3. [HTTP Router](#http-router)
4. [Authentication Middleware](#authentication-middleware)
5. [Public Handlers](#public-handlers)
6. [Admin Portal Handlers](#admin-portal-handlers)
7. [Customer Portal Handlers](#customer-portal-handlers)
8. [Server-Sent Events (SSE)](#server-sent-events-sse)
9. [Go Templates](#go-templates)
10. [gRPC Client Configuration](#grpc-client-configuration)
11. [Data Types](#data-types)
12. [Known Issues and Improvements](#known-issues-and-improvements)

---

## Overview

The Frontend service is a standard Go HTTP server built with `gorilla/mux`. It renders server-side HTML using Go's `html/template` package, communicates with the **Server Service** exclusively over gRPC, and pushes real-time updates to clients via **Server-Sent Events (SSE)**.

Entry point: `frontend/cmd/web/main.go`

The service exposes three distinct zones:

| Zone            | Prefix     | Authentication        |
|-----------------|------------|-----------------------|
| Public          | `/`        | None                  |
| Admin Portal    | `/admin`   | JWT (admin token)     |
| Customer Portal | `/portal`  | JWT (customer token)  |

---

## Project Structure

```
frontend/
├── cmd/
│   └── web/
│       ├── main.go              # Server bootstrap, router setup
│       ├── auth_handlers.go     # Login/logout + auth middleware + portal handlers
│       └── admin_handlers.go    # All admin portal route handlers
└── templates/
    ├── index.html               # Public home page
    ├── admin/
    │   ├── base.html            # Admin base layout
    │   ├── dashboard.html       # Admin dashboard
    │   ├── bread.html           # Bread listing
    │   ├── bread_new.html       # New bread form
    │   ├── bread_edit.html      # Edit bread form
    │   ├── orders.html          # Orders listing
    │   ├── customers.html       # Customer listing
    │   ├── customer_detail.html # Customer detail
    │   ├── makers.html          # Bread makers listing
    │   ├── maker_detail.html    # Maker detail
    │   └── alerts.html          # Low stock alerts
    └── portal/
        ├── base.html            # Portal base layout
        ├── login.html           # Customer login
        ├── dashboard.html       # Customer dashboard
        ├── orders.html          # Customer orders
        ├── order_detail.html    # Order detail
        ├── invoices.html        # Customer invoices
        └── invoice_detail.html  # Invoice detail
```

---

## HTTP Router

File: `frontend/cmd/web/main.go`

The router is initialised using `gorilla/mux`.

### Public Routes

| Method | Path            | Handler                  | Description                                     |
|--------|-----------------|--------------------------|--------------------------------------------------|
| `GET`  | `/`             | `homeHandler`            | Public storefront; renders current bread list    |
| `GET`  | `/stream`       | `streamHandler`          | SSE stream for live inventory (bread qty/price)  |
| `GET`  | `/order-stream` | `orderStreamHandler`     | SSE stream for recent order updates              |
| `GET`  | `/orders`       | `orderDetailsHandler`    | _(Public order status lookup — details unclear)_ |

### Admin Routes (protected by `RequireAdminAuth`)

| Method | Path                           | Handler                         | Description                            |
|--------|--------------------------------|---------------------------------|----------------------------------------|
| `GET`  | `/admin/login`                 | `AdminLoginPageHandler`         | Render login form                      |
| `POST` | `/admin/login`                 | `AdminLoginHandler`             | Process credentials, set cookie        |
| `GET`  | `/admin/logout`                | `AdminLogoutHandler`            | Clear admin token cookie               |
| `GET`  | `/admin`                       | `AdminDashboardHandler`         | Dashboard with stats and alerts        |
| `GET`  | `/admin/bread`                 | `AdminBreadListHandler`         | List all bread products                |
| `GET`  | `/admin/bread/new`             | `AdminBreadNewHandler`          | Render new bread form                  |
| `POST` | `/admin/bread/create`          | `AdminBreadCreateHandler`       | Create bread via gRPC                  |
| `GET`  | `/admin/bread/{id}/edit`       | `AdminBreadEditHandler`         | Render edit form for bread             |
| `POST` | `/admin/bread/{id}/update`     | `AdminBreadUpdateHandler`       | Update bread via gRPC                  |
| `POST` | `/admin/bread/{id}/delete`     | `AdminBreadDeleteHandler`       | Delete bread via gRPC                  |
| `GET`  | `/admin/orders`                | `AdminOrdersHandler`            | List all orders                        |
| `POST` | `/admin/orders/{id}/status`    | `AdminOrderStatusHandler`       | Update order status via gRPC           |
| `GET`  | `/admin/customers`             | `AdminCustomersHandler`         | List all customers                     |
| `GET`  | `/admin/customers/{id}`        | `AdminCustomerDetailHandler`    | Customer detail with order history     |
| `GET`  | `/admin/makers`                | `AdminMakersHandler`            | List all bread makers                  |
| `GET`  | `/admin/makers/{id}`           | `AdminMakerDetailHandler`       | Maker detail with production history   |
| `GET`  | `/admin/alerts`                | `AdminAlertsHandler`            | Low stock alert list                   |
| `POST` | `/admin/alerts/{id}/adjust`    | `AdminAdjustQuantityHandler`    | Adjust bread quantity                  |
| `GET`  | `/admin/dashboard-stream`      | _(stream handler)_              | SSE for live dashboard stats           |
| `GET`  | `/admin/alerts-stream`         | _(stream handler)_              | SSE for live low-stock alerts          |

### Customer Portal Routes (protected by `RequireCustomerAuth`)

| Method | Path                         | Handler                           | Description                        |
|--------|------------------------------|-----------------------------------|------------------------------------|
| `GET`  | `/portal/login`              | `CustomerLoginPageHandler`        | Render login form                  |
| `POST` | `/portal/login`              | `CustomerLoginHandler`            | Process credentials, set cookie    |
| `GET`  | `/portal/logout`             | `CustomerLogoutHandler`           | Clear customer token cookie        |
| `GET`  | `/portal`                    | `CustomerPortalDashboardHandler`  | Dashboard with recent orders       |
| `GET`  | `/portal/orders`             | `CustomerOrdersHandler`           | All customer orders                |
| `GET`  | `/portal/orders/{id}`        | `CustomerOrderDetailHandler`      | Single order with line items       |
| `GET`  | `/portal/invoices`           | `CustomerInvoicesHandler`         | All customer invoices              |
| `GET`  | `/portal/invoices/{id}`      | `CustomerInvoiceDetailHandler`    | Single invoice detail              |

---

## Authentication Middleware

File: `frontend/cmd/web/auth_handlers.go`

### `RequireAdminAuth(h http.HandlerFunc) http.HandlerFunc`

Wraps an admin handler. On each request:

1. Reads the `admin_token` cookie
2. Calls `validateToken(token, "admin")` to parse and verify the JWT
3. If valid: calls the wrapped handler
4. If invalid or missing: redirects to `/admin/login`

### `RequireCustomerAuth(h http.HandlerFunc) http.HandlerFunc`

Same pattern for customer routes:

1. Reads the `customer_token` cookie
2. Calls `validateToken(token, "customer")`
3. If valid: calls the wrapped handler
4. If invalid or missing: redirects to `/portal/login`

### `validateToken(tokenString string, expectedType string) (*Claims, error)`

Parses a JWT string using the shared `JWT_SECRET`. Returns an error if:

- The token signature is invalid
- The token has expired
- The `UserType` claim does not match `expectedType`

### Cookie Settings

Both admin and customer cookies are set with:

```go
http.Cookie{
    Name:     "admin_token" | "customer_token",
    Value:    jwtToken,
    Expires:  time.Now().Add(24 * time.Hour),
    HttpOnly: true,
    SameSite: http.SameSiteStrictMode,
    Path:     "/",
}
```

> `HttpOnly` prevents JavaScript access (XSS mitigation).
> `SameSite=Strict` prevents the cookie from being sent on cross-site requests (CSRF mitigation).

### Helper Functions

```go
// Extracts UserID, Username, Role from admin JWT
getAdminUserFromToken(r *http.Request) (id int, username string, role string, err error)

// Extracts CustomerID from customer JWT
getCustomerIDFromToken(r *http.Request) (id int, err error)
```

---

## Public Handlers

### `homeHandler`

Calls `CheckBreadInventory` gRPC to get the current inventory snapshot. Renders `index.html` with the bread list. Also connects via JavaScript to the `/stream` SSE endpoint for live updates.

### `streamHandler`

Opens a gRPC `CheckBreadInventoryStream`. For each streamed `BreadResponse`, writes an SSE event:

```
event: bread
data: {"ID":1,"Name":"Sourdough","Price":3.50,"Quantity":15,...}

```

The client-side JavaScript in `index.html` listens on `EventSource('/stream')` and updates the DOM in real time.

### `orderStreamHandler`

Opens a gRPC `BuyOrderStream`. Streams buy order events as SSE to connected clients.

---

## Admin Portal Handlers

File: `frontend/cmd/web/admin_handlers.go`

All admin handlers:

1. Extract admin identity from the JWT cookie
2. Make one or more gRPC calls to the Server Service
3. Populate an `AdminTemplateData` struct
4. Execute the appropriate Go template

### `AdminDashboardHandler`

gRPC calls: `GetDashboardStats`, `GetLowStockAlerts`, `GetAllOrders` (recent)

Renders: `admin/dashboard.html`

### `AdminBreadListHandler`

gRPC calls: `GetAllBread`

Renders: `admin/bread.html`

### `AdminBreadCreateHandler`

Parses the HTML form body, calls `CreateBread` gRPC, redirects to `/admin/bread` on success.

### `AdminBreadUpdateHandler`

Parses the form, calls `UpdateBread` gRPC, redirects on success.

### `AdminBreadDeleteHandler`

Calls `DeleteBread` gRPC, redirects to `/admin/bread`.

### `AdminOrdersHandler`

gRPC calls: `GetAllOrders`

Renders: `admin/orders.html`

### `AdminOrderStatusHandler`

Parses `status` from form, calls `UpdateOrderStatus` gRPC. When status = `"completed"`, the gRPC handler auto-generates an invoice. Redirects to `/admin/orders`.

### `AdminCustomersHandler` / `AdminCustomerDetailHandler`

gRPC calls: `GetAllCustomers` / `GetCustomerOrders`

### `AdminMakersHandler` / `AdminMakerDetailHandler`

gRPC calls: `GetAllBreadMakers` / `GetMakerOrders`

### `AdminAlertsHandler`

gRPC calls: `GetLowStockAlerts`

Renders: `admin/alerts.html` (bread with `quantity < 10`)

### `AdminAdjustQuantityHandler`

Parses `quantity` from form, calls `UpdateBread` gRPC (or dedicated adjust endpoint).

---

## Customer Portal Handlers

File: `frontend/cmd/web/auth_handlers.go`

### `CustomerPortalDashboardHandler`

gRPC calls: `GetMyOrders`, `GetMyInvoices`

Computes summary statistics:
- Recent orders (last 5)
- Total orders count
- Unpaid invoices count
- Total amount due

Renders: `portal/dashboard.html`

### `CustomerOrdersHandler`

gRPC calls: `GetMyOrders`

Renders: `portal/orders.html`

### `CustomerOrderDetailHandler`

gRPC calls: `GetOrderDetails` (includes authorization check in gRPC service)

Renders: `portal/order_detail.html`

### `CustomerInvoicesHandler`

gRPC calls: `GetMyInvoices`

Renders: `portal/invoices.html`

### `CustomerInvoiceDetailHandler`

gRPC calls: `GetInvoice`, `GetCustomerInvoices` (for authorization)

Verifies that the invoice belongs to the authenticated customer before rendering.

Renders: `portal/invoice_detail.html`

---

## Server-Sent Events (SSE)

The Frontend uses SSE to push real-time updates from gRPC streams to browser clients without WebSockets.

### Pattern

```go
func streamHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    // Open gRPC stream
    stream, _ := grpcClient.CheckBreadInventoryStream(ctx, &proto.BreadRequest{})

    for {
        resp, err := stream.Recv()
        if err != nil { break }

        data, _ := json.Marshal(resp.Bread)
        fmt.Fprintf(w, "event: bread\ndata: %s\n\n", data)
        w.(http.Flusher).Flush()
    }
}
```

### SSE Endpoints

| Endpoint                  | gRPC Source                   | Event Name  | Consumer         |
|---------------------------|-------------------------------|-------------|------------------|
| `/stream`                 | `CheckBreadInventoryStream`   | `bread`     | `index.html`     |
| `/order-stream`           | `BuyOrderStream`              | `order`     | _(public page)_  |
| `/admin/dashboard-stream` | `GetDashboardStats` (polled)  | `stats`     | `dashboard.html` |
| `/admin/alerts-stream`    | `GetLowStockAlerts` (polled)  | `alerts`    | `alerts.html`    |

---

## Go Templates

Templates are loaded at startup using `template.ParseFiles` or `template.ParseGlob`.

### Template Data Structs

#### `AdminTemplateData`

```go
type AdminTemplateData struct {
    Title        string
    CurrentPage  string
    AdminUsername string
    AdminRole    string
    Stats        *proto.DashboardStats
    Breads       []*proto.Bread
    Customers    []*proto.Customer
    Makers       []*proto.BreadMakerProto
    Orders       []*proto.BuyOrder
    Alerts       []*proto.Bread
    // Additional fields per handler
}
```

#### `PortalTemplateData`

```go
type PortalTemplateData struct {
    Title       string
    ActivePage  string
    CustomerName string
    CustomerID  int
    Orders      []*proto.BuyOrder
    Invoices    []*proto.Invoice
    Stats       PortalStats  // computed summary
}
```

#### `BreadLog`

```go
type BreadLog struct {
    ID       int
    Name     string
    Message  string
    Buyer    string
    Maker    string
    Quantity int
    Price    float64
    Image    string
}
```

### Template Features

- Bootstrap-based responsive design
- Status badges with colour coding:
  - `Pending` — yellow
  - `Processing` — blue
  - `Processed` / `Completed` — green
  - `Failed` — red
  - `Paid` — green
  - `Unpaid` — orange
- jQuery for SSE handling on the public page
- `gorilla/mux` path variables extracted with `mux.Vars(r)["id"]`

---

## gRPC Client Configuration

The Frontend creates gRPC client stubs at startup using `grpc.Dial`:

```go
conn, err := grpc.Dial(
    os.Getenv("BAKERY_SERVICE_ADDR"),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
```

Client stubs created:
- `proto.NewCheckInventoryClient(conn)`
- `proto.NewBuyBreadClient(conn)`
- `proto.NewBuyOrderServiceClient(conn)`
- `proto.NewAdminServiceClient(conn)`
- `proto.NewAuthServiceClient(conn)`
- `proto.NewInvoiceServiceClient(conn)`
- `proto.NewCustomerPortalServiceClient(conn)`

> **Security Warning:** `insecure.NewCredentials()` sends all gRPC traffic in plain text. TLS must be configured before any non-development deployment.

---

## Data Types

### `BuyOrder` (frontend local)

```go
type BuyOrder struct {
    ID           string
    CustomerID   string
    CustomerName string
    Status       string
    TotalCost    float64
    CreatedAt    string
}
```

### `BuyOrderDetail`

```go
type BuyOrderDetail struct {
    BreadName string
    Quantity  int
    Price     float64
    Total     float64
}
```

### `OrderData`

```go
type OrderData struct {
    Order   BuyOrder
    Details []BuyOrderDetail
}
```

---

## Known Issues and Improvements

| # | Component | Issue | Recommendation |
|---|-----------|-------|----------------|
| 1 | gRPC dial | Uses `insecure.NewCredentials()` | Configure TLS for all non-local environments |
| 2 | Template loading | Templates loaded at startup — changes require restart | Accept for production; add dev-mode reload |
| 3 | Form handling | No CSRF token on POST forms | Add `gorilla/csrf` middleware |
| 4 | Error display | HTTP errors return plain text or silent redirects | Implement user-friendly error pages |
| 5 | Auth middleware | Token validated on every request via local JWT parse — no revocation | Add token revocation table or short-lived tokens with refresh |
| 6 | `orderDetailsHandler` | Purpose is unclear; appears to be a stub | Define intent or remove |
| 7 | No pagination | All customers, orders, bread returned in full | Add pagination to list handlers |
| 8 | No input sanitisation | Form values passed directly to gRPC | Validate and sanitise all form inputs |
| 9 | Hardcoded gRPC address | Falls back to `localhost:50051` without error | Fail fast if `BAKERY_SERVICE_ADDR` is not set |
| 10 | No graceful shutdown | HTTP server has no `Shutdown` logic | Implement `os.Signal` handler for graceful drain |
