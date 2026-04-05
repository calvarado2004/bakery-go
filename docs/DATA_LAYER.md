# Bakery Service — Data Layer

## Table of Contents

1. [Overview](#overview)
2. [Database Schema](#database-schema)
3. [Go Domain Models](#go-domain-models)
4. [Repository Interface](#repository-interface)
5. [PostgreSQL Implementation](#postgresql-implementation)
6. [Test Repository (Mock)](#test-repository-mock)
7. [Connection Management](#connection-management)
8. [Data Integrity Considerations](#data-integrity-considerations)

---

## Overview

The data layer is located in the `data/` package and is divided into three files:

| File              | Responsibility                                                  |
|-------------------|-----------------------------------------------------------------|
| `models.go`       | Go struct definitions that map to database rows                 |
| `repository.go`   | `Repository` interface + `PostgresRepository` implementation    |
| `test_models.go`  | In-memory mock implementation of `Repository` for unit testing  |

The `Repository` interface is the central contract through which all services (Server, Broker) access persistent data. No service imports `database/sql` or `pgx` directly — all database access is mediated through this interface.

---

## Database Schema

Schema file: `bakery.sql`

### Table: `bread`

Stores the bakery's product catalogue.

| Column        | Type                       | Constraints              | Notes                               |
|---------------|----------------------------|--------------------------|-------------------------------------|
| `id`          | `SERIAL`                   | `PRIMARY KEY`            | Auto-increment surrogate key        |
| `name`        | `VARCHAR(255)`             | `NOT NULL`               | Display name                        |
| `price`       | `NUMERIC(10,2)`            | `NOT NULL`               | Unit price                          |
| `quantity`    | `INT`                      | `NOT NULL`               |                                     |
| `description` | `TEXT`                     |                          | Optional product description        |
| `type`        | `VARCHAR(100)`             |                          | Product category/type               |
| `status`      | `VARCHAR(50)`              |                          | e.g. `"available"`, `"unavailable"` |
| `created_at`  | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`          |                                     |
| `updated_at`  | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`          |                                     |
| `image`       | `TEXT`                     |                          | URL or relative path to image       |

### Table: `bread_maker`

Represents bakers who produce bread.

| Column       | Type                       | Constraints     |
|--------------|----------------------------|-----------------|
| `id`         | `SERIAL`                   | `PRIMARY KEY`   |
| `name`       | `VARCHAR(255)`             | `NOT NULL`      |
| `email`      | `VARCHAR(255)`             | `NOT NULL`      |
| `created_at` | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()` |
| `updated_at` | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()` |

### Table: `make_order`

Production orders assigned to bread makers.

| Column            | Type          | Constraints              |
|-------------------|---------------|--------------------------|
| `id`              | `SERIAL`      | `PRIMARY KEY`            |
| `bread_maker_id`  | `INT`         | `REFERENCES bread_maker` |
| `make_order_uuid` | `UUID`        | `NOT NULL`               |

### Table: `make_order_details`

Line items for a production order (composite primary key).

| Column          | Type  | Constraints                      |
|-----------------|-------|----------------------------------|
| `make_order_id` | `INT` | `PK`, `REFERENCES make_order`    |
| `bread_id`      | `INT` | `PK`, `REFERENCES bread`         |
| `quantity`      | `INT` | `NOT NULL`                       |

### Table: `customer`

Customer accounts for the storefront.

| Column       | Type                       | Constraints     | Notes                        |
|--------------|----------------------------|-----------------|------------------------------|
| `id`         | `SERIAL`                   | `PRIMARY KEY`   |                              |
| `name`       | `VARCHAR(255)`             | `NOT NULL`      |                              |
| `email`      | `VARCHAR(255)`             | `NOT NULL`      |                              |
| `password`   | `VARCHAR(255)`             | `NOT NULL`      | bcrypt hash                  |
| `created_at` | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()` |                              |
| `updated_at` | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()` |                              |

### Table: `buy_order`

Customer purchase orders (header record).

| Column           | Type          | Constraints                 | Notes                                         |
|------------------|---------------|-----------------------------|-----------------------------------------------|
| `id`             | `SERIAL`      | `PRIMARY KEY`               |                                               |
| `customer_id`    | `INT`         | `REFERENCES customer`       |                                               |
| `buy_order_uuid` | `UUID`        | `NOT NULL`                  | Business identifier used across services      |
| `status`         | `VARCHAR(50)` |                             | `"Pending"`, `"Processed"`, `"Failed"`, `"completed"` |

### Table: `order_details`

Line items for a customer purchase order (composite primary key).

| Column         | Type                       | Constraints                    |
|----------------|----------------------------|--------------------------------|
| `buy_order_id` | `INT`                      | `PK`, `REFERENCES buy_order`   |
| `bread_id`     | `INT`                      | `PK`, `REFERENCES bread`       |
| `quantity`     | `INT`                      | `NOT NULL`                     |
| `price`        | `NUMERIC(10,2)`            | `NOT NULL`                     |
| `created_at`   | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`                |
| `updated_at`   | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`                |

### Table: `orders_processed`

Audit log of completed transactions.

| Column         | Type                       | Constraints                  |
|----------------|----------------------------|------------------------------|
| `id`           | `SERIAL`                   | `PRIMARY KEY`                |
| `customer_id`  | `INT`                      | `REFERENCES customer`        |
| `buy_order_id` | `INT`                      | `REFERENCES buy_order`       |
| `created_at`   | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`              |
| `updated_at`   | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`              |

### Table: `outbox`

Implements the Outbox Pattern for reliable message delivery.

| Column       | Type                       | Constraints     | Notes                                     |
|--------------|----------------------------|-----------------|-------------------------------------------|
| `id`         | `SERIAL`                   | `PRIMARY KEY`   |                                           |
| `payload`    | `BYTEA`                    | `NOT NULL`      | JSON-serialized message payload           |
| `sent`       | `BOOLEAN`                  | `DEFAULT FALSE` | Set to `TRUE` once the message is relayed |
| `created_at` | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()` |                                           |

### Table: `admin_users`

Administrative user accounts.

| Column       | Type                       | Constraints              |
|--------------|----------------------------|--------------------------|
| `id`         | `SERIAL`                   | `PRIMARY KEY`            |
| `username`   | `VARCHAR(255)`             | `NOT NULL`, `UNIQUE`     |
| `email`      | `VARCHAR(255)`             | `NOT NULL`, `UNIQUE`     |
| `password`   | `VARCHAR(255)`             | `NOT NULL`               |
| `role`       | `VARCHAR(50)`              | `DEFAULT 'admin'`        |
| `created_at` | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`          |
| `updated_at` | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`          |

### Table: `invoices`

Billing documents generated upon order completion.

| Column           | Type                       | Constraints              | Notes                                |
|------------------|----------------------------|--------------------------|--------------------------------------|
| `id`             | `SERIAL`                   | `PRIMARY KEY`            |                                      |
| `buy_order_id`   | `INT`                      | `REFERENCES buy_order`   |                                      |
| `customer_id`    | `INT`                      | `REFERENCES customer`    |                                      |
| `invoice_number` | `VARCHAR(50)`              | `NOT NULL`, `UNIQUE`     | e.g. `INV-1-<timestamp>`             |
| `subtotal`       | `NUMERIC(10,2)`            | `NOT NULL`               |                                      |
| `tax`            | `NUMERIC(10,2)`            | `NOT NULL`               | 10% (AdminService) or 8% (InvoiceService) |
| `total`          | `NUMERIC(10,2)`            | `NOT NULL`               |                                      |
| `status`         | `VARCHAR(50)`              | `DEFAULT 'unpaid'`       | `"unpaid"`, `"paid"`                 |
| `created_at`     | `TIMESTAMP WITH TIME ZONE` | `DEFAULT NOW()`          |                                      |
| `due_date`       | `TIMESTAMP WITH TIME ZONE` |                          | 30 days from creation                |
| `paid_at`        | `TIMESTAMP WITH TIME ZONE` |                          | Nullable — set when status = `paid`  |

### Table: `invoice_items`

Line items for an invoice.

| Column       | Type            | Constraints              |
|--------------|-----------------|--------------------------|
| `id`         | `SERIAL`        | `PRIMARY KEY`            |
| `invoice_id` | `INT`           | `REFERENCES invoices`    |
| `bread_id`   | `INT`           | `REFERENCES bread`       |
| `bread_name` | `VARCHAR(255)`  | `NOT NULL`               |
| `quantity`   | `INT`           | `NOT NULL`               |
| `unit_price` | `NUMERIC(10,2)` | `NOT NULL`               |
| `total`      | `NUMERIC(10,2)` | `NOT NULL`               |

### Seed Data

The schema inserts the following defaults:

- **Customer:** `john@doe.com` — password: `password123` (bcrypt hash stored)
- **Bread Maker:** `Jake Maker` — `jake@maker.com`
- **Admin User:** `admin` — password: `admin123` (bcrypt hash stored)

> **Security Note:** Seed credentials must be changed before any deployment outside of a local development environment.

---

## Go Domain Models

File: `data/models.go`

### `Customer`

```go
type Customer struct {
    ID        int
    Name      string
    Email     string
    Password  string    // bcrypt hash
    CreatedAt time.Time
    UpdatedAt time.Time
    BuyOrders []BuyOrder
}
```

### `Bread`

```go
type Bread struct {
    ID          int
    Name        string
    Price       float64
    Quantity    int
    Description string
    Type        string
    Status      string
    Image       string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### `BuyOrder`

```go
type BuyOrder struct {
    ID           int
    CustomerID   int
    BuyOrderUUID string
    Customer     Customer
    Breads       []Bread
    Status       string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

### `OrdersProcessed`

```go
type OrdersProcessed struct {
    ID          int
    CustomerID  int
    BuyOrderID  int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### `BreadMaker`

```go
type BreadMaker struct {
    ID        int
    Name      string
    Email     string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### `MakeOrder`

```go
type MakeOrder struct {
    ID            int
    BreadMakerID  int
    BreadMaker    BreadMaker
    MakeOrderUUID string
    Breads        []Bread
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

### `AdminUser`

```go
type AdminUser struct {
    ID        int
    Username  string
    Email     string
    Password  string    // bcrypt hash
    Role      string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### `Invoice`

```go
type Invoice struct {
    ID            int
    BuyOrderID    int
    CustomerID    int
    InvoiceNumber string
    Subtotal      float64
    Tax           float64
    Total         float64
    Status        string
    CreatedAt     time.Time
    DueDate       time.Time
    PaidAt        *time.Time    // nullable
    Items         []InvoiceItem
}
```

### `InvoiceItem`

```go
type InvoiceItem struct {
    ID        int
    InvoiceID int
    BreadID   int
    BreadName string
    Quantity  int
    UnitPrice float64
    Total     float64
}
```

### `OutboxMessage`

```go
type OutboxMessage struct {
    ID        int
    Payload   []byte
    Sent      bool
    CreatedAt time.Time
}
```

### `DashboardStats`

```go
type DashboardStats struct {
    TotalOrders      int
    TotalRevenue     float64
    TotalProducts    int
    TotalCustomers   int
    TotalBreadMakers int
    LowStockCount    int
}
```

---

## Repository Interface

File: `data/repository.go`

The `Repository` interface defines all persistence operations. Every service that needs database access receives this interface via dependency injection.

```go
type Repository interface {
    // --- Customer ---
    InsertCustomer(customer Customer) (int, error)
    GetCustomerByID(id int) (*Customer, error)
    GetCustomerByEmail(email string) (*Customer, error)
    GetAllCustomers() ([]*Customer, error)

    // --- Bread ---
    InsertBread(bread Bread) (int, error)
    GetBreadByID(id int) (*Bread, error)
    GetAvailableBread() ([]*Bread, error)
    UpdateBread(bread Bread) error
    DeleteBread(id int) error
    AdjustBreadQuantity(id, quantity int) error
    AdjustBreadPrice(id int, price float64) error

    // --- Buy Orders ---
    InsertBuyOrder(order BuyOrder) (int, error)
    GetBuyOrderByID(id int) (*BuyOrder, error)
    GetBuyOrderByUUID(uuid string) (*BuyOrder, error)
    GetAllBuyOrders() ([]*BuyOrder, error)
    UpdateOrderStatus(id int, status string) error
    GetOrderTotalCost(orderID int) (float64, error)

    // --- Make Orders ---
    InsertMakeOrder(order MakeOrder) (int, error)
    GetMakeOrderByID(id int) (*MakeOrder, error)
    GetMakerOrders(makerID int) ([]*MakeOrder, error)
    GetAllMakeOrders() ([]*MakeOrder, error)

    // --- Bread Makers ---
    InsertBreadMaker(maker BreadMaker) (int, error)
    GetBreadMakerByID(id int) (*BreadMaker, error)
    GetAllBreadMakers() ([]*BreadMaker, error)

    // --- Admin ---
    GetDashboardStats() (*DashboardStats, error)
    GetLowStockBread() ([]*Bread, error)
    GetCustomerOrders(customerID int) ([]*BuyOrder, error)

    // --- Authentication ---
    GetAdminUserByUsername(username string) (*AdminUser, error)
    GetAdminUserByID(id int) (*AdminUser, error)
    InsertAdminUser(user AdminUser) (int, error)

    // --- Invoices ---
    InsertInvoice(invoice Invoice) (int, error)
    GetInvoiceByID(id int) (*Invoice, error)
    GetInvoicesByCustomerID(customerID int) ([]*Invoice, error)
    GetAllInvoices() ([]*Invoice, error)
    GetInvoiceByOrderID(orderID int) (*Invoice, error)

    // --- Outbox (Message Reliability) ---
    InsertOutboxMessage(payload []byte) error
    DeleteOutboxMessage(id int) error
    GetUnprocessedOutboxMessages() ([]*OutboxMessage, error)

    // --- Password Utilities ---
    PasswordMatches(hash, plaintext string) (bool, error)
}
```

---

## PostgreSQL Implementation

File: `data/repository.go` — `PostgresRepository` struct

```go
type PostgresRepository struct {
    DB *pgx.Conn
}
```

Constructor:

```go
func NewPostgresRepository(conn *pgx.Conn) *PostgresRepository
```

All methods execute SQL queries directly against the `pgx.Conn`. Key implementation notes:

- `GetAvailableBread` filters by `status = 'available'`
- `GetLowStockBread` filters by `quantity < 10`
- `AdjustBreadQuantity` uses `UPDATE bread SET quantity = quantity + $1`
- `GetOrderTotalCost` joins `order_details` with `bread` and sums `(quantity * price)`
- `GetDashboardStats` performs individual `COUNT` and `SUM` queries for each stat
- `PasswordMatches` wraps `bcrypt.CompareHashAndPassword`
- `InsertInvoice` inserts the header then each `InvoiceItem` in a loop (no transaction)

> **Issue:** `InsertInvoice` does not use a database transaction. If the loop fails partway through inserting invoice items, the invoice header will exist without all line items. This should be refactored to use `pgx` transactions.

---

## Test Repository (Mock)

File: `data/test_models.go`

`TestRepository` implements the full `Repository` interface with in-memory mock data. It is used to test service logic in isolation without a live PostgreSQL instance.

```go
type TestRepository struct{}

func NewTestRepository() *TestRepository
```

All methods return either static test data or `nil, nil`. This makes the mock suitable for verifying that service methods are called with correct arguments, but it does not simulate error conditions. Future iterations should evolve `TestRepository` into a configurable mock that can return specific errors or datasets on demand (see `docs/TESTING_PLAN.md`).

---

## Connection Management

File: `server/main.go`

```go
func connectToDB() *pgx.Conn
func openDB(dsn string) (*pgx.Conn, error)
```

`connectToDB` retries the connection up to **10 times** with a **5-second pause** between attempts, using `logrus` to log each retry. If all attempts fail, the server exits with `log.Panic`.

`openDB` calls `pgx.Connect` and runs a `Ping` to validate the connection is live before returning it.

> **Note:** The same connection-bootstrapping logic is duplicated in `broker/main.go` and `makers/main.go`. This should be extracted into a shared `internal/db` package.

---

## Data Integrity Considerations

| Concern                          | Current State                              | Recommended Fix                                     |
|----------------------------------|--------------------------------------------|-----------------------------------------------------|
| Invoice insert atomicity         | No transaction; partial inserts possible   | Wrap in `pgx.BeginTx` / `tx.Commit`                |
| Bread quantity goes negative     | No check before decrement in broker        | Add `CHECK (quantity >= 0)` constraint and validate |
| Duplicate seed data on restart   | `initializeBakery` inserts unconditionally | Guard with `ON CONFLICT DO NOTHING`                 |
| Order status transitions         | No state machine; any string accepted      | Define allowed transitions; validate in service     |
| Orphaned outbox messages         | Outbox rows deleted after relay; no TTL    | Add TTL column and a cleanup job                    |
| No foreign key cascades defined  | Schema references without ON DELETE rules  | Define `ON DELETE RESTRICT` or `CASCADE` explicitly |
| `password` stored as plain text in seed | Seed SQL uses bcrypt hash literals  | This is acceptable but should be documented clearly |
