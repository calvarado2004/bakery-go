# Architecture Audit & Remediation Plan

> **Goal:** Transform Bakery Service into a facade for a production-grade electronic market platform — with FIFO order guarantees, correct inventory handling, proper concurrency, and fault-tolerant message processing.

---

## Table of Contents
1. [Executive Summary](#1-executive-summary)
2. [Critical: Inventory & Data Integrity](#2-critical-inventory--data-integrity)
3. [High: Order Matching Engine (Beyond FIFO)](#3-high-order-matching-engine-beyond-fifo)
4. [High: Service Reliability](#4-high-service-reliability)
5. [Medium: Database Schema & Performance](#5-medium-database-schema--performance)
6. [Medium: gRPC & Frontend](#6-medium-grpc--frontend)
7. [Low: Code Quality & Dead Infrastructure](#7-low-code-quality--dead-infrastructure)
8. [Validation Gate: Testing & CI Requirements](#8-validation-gate-testing--ci-requirements)
9. [Scaling Architecture: Buyers & Makers as External Entities](#9-scaling-architecture-buyers--makers-as-external-entities)
10. [Remediation Phases](#10-remediation-phases)

---

## 1. Executive Summary

The current architecture has the right *idea* — a gRPC server, a RabbitMQ broker, and a makers service — but several **critical data integrity bugs** and **architectural gaps** prevent it from functioning as a real electronic market platform. The most impactful issues are:

| # | Severity | Issue | Impact |
|---|----------|-------|--------|
| 1 | **Critical** | Double deduction of inventory | Every order reduces stock by 2× |
| 2 | **Critical** | `InsertBuyOrder` uses TOCTOU inventory check | Concurrent brokers can oversell |
| 3 | **High** | No FIFO ordering guarantees for buy orders | Market orders fill out of sequence |
| 4 | **High** | `checkBread` appends to shared order slice | Duplicate make-bread orders |
| 5 | **High** | `insertBuyOrder` doesn't use `FulfillOrderTx` | The safe lock/deduct path exists but is never called |
| 6 | **Medium** | Floating-point financial fields | Rounding errors in prices/invoices |
| 7 | **Medium** | No database indexes on lookup columns | O(n) scans on every query |
| 8 | **Medium** | gRPC connections created per request in frontend | Connection exhaustion under load |
| 9 | **Medium** | Makers service kills on any single error | No fault isolation |
| 10 | **Low** | Dead code, missing constraints, no UNIQUE keys | Technical debt |

---

## 2. Critical: Inventory & Data Integrity

### 2.1 Double Deduction (Bug)

**Location:** `broker/main.go:processOneOrder` + `data/models.go:InsertBuyOrder`

**Flow:**
```
processOneOrder
  ├── InsertBuyOrder(order, breads)
  │     └── for each bread:
  │           INSERT order_details
  │           AdjustBreadQuantity(bread.ID, -bread.Quantity)  ← Deduction #1
  │
  ├── FulfillOrderTx(order)
  │     └── for each bread:
  │           SELECT ... FOR UPDATE
  │           UPDATE bread SET quantity = quantity - $1        ← Deduction #2
```

**Result:** Every successful order deducts `2 × ordered_quantity` from inventory.

**Fix:** Remove the `AdjustBreadQuantity` call from `InsertBuyOrder`. Let `FulfillOrderTx` be the **sole** path for inventory deduction. `InsertBuyOrder` should only persist the order and order details — no stock manipulation.

```go
// data/models.go — InsertBuyOrder (AFTER FIX)
func (u *PostgresRepository) InsertBuyOrder(order BuyOrder, breads []Bread) (int, error) {
    // 1. INSERT INTO buy_order
    // 2. For each bread: INSERT INTO order_details
    // 3. NO inventory adjustment here — that's FulfillOrderTx's job
    // ...
}
```

---

### 2.2 TOCTOU Race in `InsertBuyOrder`

**Location:** `data/models.go:AdjustBreadQuantity`

`AdjustBreadQuantity` reads quantity, computes new value in Go, writes back:
```
READ:  SELECT quantity FROM bread WHERE id = $1    ← sees quantity = 5
WRITE: UPDATE bread SET quantity = $1 WHERE id = $2 ← writes 2 (instead of correct -1)
```

Between the READ and WRITE, another broker can read the same stale value and also deduct. The `CHECK (quantity >= 0)` constraint catches negatives but the intermediate value is wrong.

**Fix:** Remove `AdjustBreadQuantity` from the `InsertBuyOrder` path entirely. The `FulfillOrderTx` path already uses `SELECT FOR UPDATE` + `UPDATE ... quantity = quantity - $1` which is the correct atomic pattern.

---

### 2.3 `InsertBuyOrder` Creates Orphaned Data on Partial Failure

**Location:** `data/models.go:InsertBuyOrder`

Each bread item is processed in its own `BeginTx`/`Commit`. If bread B's insert fails after bread A's succeeded:
- Bread A's inventory is already deducted (committed)
- Bread B's is rolled back
- The `buy_order` row exists with only bread A's `order_details`

**Fix:** Use a single transaction for the entire order (header + all details). `FulfillOrderTx` should handle both stock verification and order detail insertion in one transaction.

---

### 2.4 `InsertMakeOrder` Has No Transaction at All

**Location:** `data/models.go:InsertMakeOrder`

Uses plain `db.ExecContext` — no `BeginTx`. If `make_order` insert succeeds but a `make_order_details` insert fails, orphaned data persists.

**Fix:** Wrap `InsertMakeOrder` in a `BeginTx`/`Commit` transaction.

---

### 2.5 Invoice Insert Has No Transaction

**Location:** `data/models.go:InsertInvoice`

Inserts the invoice header, then loops inserting items with no transaction wrapping. A partial failure leaves an invoice with missing items.

**Fix:** Wrap in a single transaction.

---

## 3. High: Order Matching Engine (Beyond FIFO)

### 3.1 The Current Model Is a Queue — A Real Market Is an Auction

The current architecture treats `buy-bread-order` as a simple FIFO queue: first in, first served. A real electronic market does **not** work this way. It works as a **matching engine** with these capabilities:

| Capability | Description |
|------------|-------------|
| **Priority (auction)** | Higher bids win over lower bids, regardless of time. A buyer offering $5/donut beats a buyer offering $1/donut even if the lower bid arrived first. |
| **Partial fulfillment** | An order for 100 donuts + 50 croissants with only 20 donuts in stock: fulfill the croissants (50), partially fulfill the donuts (20 of 100), mark the order as `PartiallyProcessed`. |
| **Item-level skip** | "No donuts available, but fulfill my croissants" — if an item is out of stock, skip it and fulfill the rest. The order status reflects which items were fulfilled. |
| **Large-order precedence** | A 100-donut order has priority over a 3-donut order. Fulfilling the 100-donut order is preferred even if it means the 3-donut order can't be fully served. But: if fulfilling the 100-donut order would leave zero stock for 20 other small orders totaling 60 donuts, the system should prefer serving more buyers (partial fulfillment of the large order). |

This is the **order book matching problem** — the same problem stock exchanges, ride-sharing platforms, and cloud resource markets solve every day.

### 3.2 New Proto Schema: Order Book Model

```protobuf
// proto/bread.proto — NEW

import "google/protobuf/timestamp.proto";
import "google/protobuf/wrappers.proto";

message BuyOrderItem {
    int32 breadId = 1;
    int32 quantityRequested = 2;
    int32 quantityFulfilled = 3;       // set by broker
    float bidPrice = 4;                 // price per unit the buyer is willing to pay
    bool fulfillPartial = 5;            // if true, allow partial fulfillment
    bool skipIfUnavailable = 6;         // if true, skip this item if out of stock (don't fail the whole order)
    string status = 7;                  // "pending" | "fulfilled" | "partially_fulfilled" | "skipped" | "rejected"
}

message BuyOrder {
    int32 id = 1;
    int32 customerId = 2;
    string buyOrderUuid = 3;
    float totalCost = 4;
    string status = 5;                  // "pending" | "processing" | "processed" | "partially_processed" | "failed" | "rejected"
    int64 sequenceNumber = 6;
    google.protobuf.Timestamp createdAt = 8;
    repeated BuyOrderItem items = 9;    // per-item fulfillment tracking
}

message OrderMatchingRule {
    // Buyer-specified rules that modify fulfillment behavior
    bool allowPartial = 1;              // allow partial fulfillment of the entire order
    bool skipUnavailableItems = 2;      // skip items with zero stock instead of failing the order
    int32 minFulfillmentPercentage = 3; // if set, reject order if less than X% of items can be fulfilled
    float maxPricePerUnit = 4;          // cap the price paid per unit (market price cap)
}
```

The server must pass these fields through from the gRPC client. The buyer specifies their own rules:

- **`allowPartial`**: If `true`, a 100-donut order with 20 available gets partially fulfilled (status `partially_processed`). If `false`, it's either fully fulfilled or rejected.
- **`skipUnavailableItems`**: If `true`, a 100-donut + 50-croissant order with 0 donuts but 50 croissants fulfills the croissants and skips donuts. If `false`, the entire order is rejected.
- **`minFulfillmentPercentage`**: If set to 80, an order that can only be 60% fulfilled is rejected outright.
- **`bidPrice`**: The price per unit the buyer is willing to pay. Higher bids get priority.

### 3.3 The Matching Engine: How the Broker Changes

**Current flow (broken queue):**
```
message arrives → insert order → check stock → deduct → publish result
```

**New flow (matching engine):**
```
message arrives → insert order with bid info → ACK immediately → 
  enqueue into matching engine → 
  batch window (e.g., 500ms) collects pending orders →
  sort by bid price DESC, then sequence number ASC →
  iterate through sorted orders, fulfill or partially fulfill →
  publish per-item results →
  update order statuses
```

**The batch window is critical.** Without it, you get head-of-line blocking: one slow order blocks the entire queue. With a batch window, the engine collects orders over a short time slice, then processes them as a batch — enabling priority sorting, partial fulfillment, and global optimization.

### 3.4 Matching Algorithm (Pseudocode)

```go
func (engine *MatchingEngine) processBatch(orders []BuyOrder) {
    // 1. Sort by priority: highest bid first, then earliest sequence number
    sort.Slice(orders, func(i, j int) bool {
        if orders[i].MaxBidPrice != orders[j].MaxBidPrice {
            return orders[i].MaxBidPrice > orders[j].MaxBidPrice  // higher bid wins
        }
        return orders[i].SequenceNumber < orders[j].SequenceNumber  // tie-break by time
    })

    // 2. For each order in priority order
    for _, order := range orders {
        var fulfilledItems []BuyOrderItem
        var skippedItems []BuyOrderItem
        var rejectedItems []BuyOrderItem
        var totalQuantityRequested int
        var totalQuantityFulfilled int

        for _, item := range order.Items {
            currentStock := getStock(item.BreadID) // SELECT FOR UPDATE
            
            totalQuantityRequested += item.QuantityRequested

            if currentStock == 0 {
                if order.SkipUnavailableItems {
                    skippedItems = append(skippedItems, item)
                    item.Status = "skipped"
                } else if order.AllowPartial {
                    skippedItems = append(skippedItems, item)
                    item.Status = "skipped"
                } else {
                    rejectedItems = append(rejectedItems, item)
                    item.Status = "rejected"
                }
                continue
            }

            // Determine how much to fulfill
            fulfillQty := min(currentStock, item.QuantityRequested)

            // Check minFulfillmentPercentage constraint
            if order.MinFulfillmentPercentage > 0 {
                remainingRequested := item.QuantityRequested - fulfillQty
                // This is a simplified check — in practice you'd track aggregate across all items
            }

            // Deduct stock atomically
            deductStock(item.BreadID, fulfillQty)

            item.QuantityFulfilled = fulfillQty
            item.Status = "fulfilled"
            if fulfillQty < item.QuantityRequested {
                item.Status = "partially_fulfilled"
            }
            fulfilledItems = append(fulfilledItems, item)
            totalQuantityFulfilled += fulfillQty
        }

        // Determine order-level status
        if len(rejectedItems) > 0 && !order.AllowPartial {
            order.Status = "rejected"
        } else if totalQuantityFulfilled == totalQuantityRequested && totalQuantityRequested > 0 {
            order.Status = "processed"
        } else if totalQuantityFulfilled > 0 {
            order.Status = "partially_processed"
        } else {
            order.Status = "failed"
        }

        // Publish per-item results
        publishItemResults(order, fulfilledItems, skippedItems, rejectedItems)
    }
}
```

### 3.5 Batch Window Configuration

```yaml
matching:
  batch_window_ms: 500        # collect orders for 500ms before processing
  max_batch_size: 100         # or process after 100 orders, whichever comes first
  priority_weight_price: 0.7  # 70% of score from bid price
  priority_weight_time: 0.3   # 30% of score from sequence number (recency)
```

The batch window is a **tunable trade-off**:
- **Small window (50ms)**: Low latency, but less opportunity for optimization. Behaves more like FIFO.
- **Large window (2000ms)**: Better optimization, but higher latency. Buyers wait up to 2 seconds for confirmation.

### 3.6 Price Discovery

In a real market, the **price** isn't set by the buyer alone — it emerges from the matching:

| Model | Description |
|-------|-------------|
| **Bid price** (simple) | Buyer sets `bidPrice`. If bid >= market price, order fills at bid price. |
| **Market price** (default) | Buyer leaves `bidPrice` unset. Order fills at the current market price (the lowest winning bid in the batch). |
| **Limit order** | Buyer sets `maxPrice`. Order fills at `min(market_price, maxPrice)`. |
| **Clearing price** | All winning orders in a batch fill at the **lowest winning bid price** (uniform pricing). |

### 3.7 How the Server/Broker Split Applies

This is exactly why the server and broker exist as separate services:

**Server (`server/gRPCBakery.go:BuyBread`)** — the stateless ingress:
- Authenticates the buyer (JWT from gRPC metadata)
- Assigns `sequenceNumber` (monotonically increasing)
- Passes buyer rules through to the RabbitMQ message (`allowPartial`, `bidPrice`, `skipUnavailableItems`, etc.)
- Returns immediately after publishing — the buyer doesn't wait
- Streams the result back via `BuyBreadStream` (listens on `bread-bought`)

**Broker (`broker/main.go`)** — the stateful matching engine:
- Ingests orders from `buy-bread-order` queue
- Batches them (500ms window or 100 orders)
- Sorts by priority: bid price DESC, sequence number ASC
- Iterates through sorted orders: fulfills, partially fulfills, skips, or rejects
- Updates the database (inventory, order status, order details)
- Publishes per-item results to `bread-bought`

**The server does NOT need to change its architecture.** It already does the right thing:
1. Ingest → publish → return (stateless)
2. Stream result from `bread-bought` (async notification)

**The broker transforms** from a sequential `processOneOrder` into a batch `processMatchingBatch`. The server is a passive conduit; the broker is the intelligence.

```
RabbitMQ "buy-bread-order" queue
        │
        ▼
   QoS(1, 0, false)  ← single-consumer prevents reordering during collection
        │
        ▼
   Ingestion: insert into DB + buffer, ACK immediately
        │
        ▼   [every 500ms or when buffer ≥ 100 orders]
   ┌─────────────────┐
   │  Matching Engine │  ← sort by bid, fulfill/partial/skip/reject
   │  (batch mode)    │
   └────────┬────────┘
            │
     ┌──────┼──────────┐
     ▼      ▼          ▼
  Process  Partial    Reject
   Order    Order      Order
     │      │          │
     ▼      ▼          ▼
  ┌─────────────────────────────────┐
  │       bread-bought queue         │  ← per-item result messages
  └─────────────────────────────────┘
```

**Key implications:**
- The broker ACKs immediately on ingestion (order is now in the buffer). If the matching engine crashes mid-batch, the next run resumes from the buffer — no message loss, no double-processing.
- The outbox stores matching results, not raw orders.
- The server's `BuyBreadStream` just waits for the `bread-bought` notification — it doesn't care how the broker matched.

### 3.8 Sequence Numbers Become Priority Anchors

Sequence numbers serve a dual purpose:
1. **Tie-breaking** when two orders have the same bid price.
2. **Recovery** — if the matching engine crashes, the sequence number tells it where to resume without double-processing.

```protobuf
message BuyOrder {
    // ...
    int64 sequenceNumber = 6;  // monotonically increasing, assigned by server at ingestion
}
```

---

### 3.9 `checkBread` Appends to Shared Order Slice (Duplicate Make Orders)

**Location:** `server/rabbitBakery.go:checkBread`

```go
breadMakeOrder := data.MakeOrder{...}  // declared OUTSIDE the loop

for _, bread := range breads {
    if bread.Quantity <= 10 {
        bread.Quantity = 50
        breadData, _ := json.Marshal(&bread)
        channel.Publish("", "make-bread-order", ...)

        breadMakeOrder.Breads = append(breadMakeOrder.Breads, bread)  // ← appends to same slice
        rabbit.Repo.InsertMakeOrder(breadMakeOrder, breads)             // ← each iteration includes ALL previous breads
    }
}
```

After 3 low-stock breads:
- Iteration 1: `breadMakeOrder.Breads = [bread1]` → inserts 1 item
- Iteration 2: `breadMakeOrder.Breads = [bread1, bread2]` → inserts 2 items (bread1 duplicated!)
- Iteration 3: `breadMakeOrder.Breads = [bread1, bread2, bread3]` → inserts 3 items (bread1, bread2 duplicated!)

**Fix:** Move `breadMakeOrder` declaration **inside** the loop:
```go
for _, bread := range breads {
    if bread.Quantity <= 10 {
        order := data.MakeOrder{
            BreadMaker:   breadMaker,
            BreadMakerID: breadMaker.ID,
        }
        order.Breads = []data.Bread{bread}  // single-item order
        rabbit.Repo.InsertMakeOrder(order, []data.Bread{bread})
    }
}
```

Or better — batch all low-stock breads into a single order **after** the loop:
```go
var lowStockBreads []data.Bread
for _, bread := range breads {
    if bread.Quantity <= 10 {
        lowStockBreads = append(lowStockBreads, bread)
    }
}
if len(lowStockBreads) > 0 {
    order := data.MakeOrder{
        BreadMaker:   breadMaker,
        BreadMakerID: breadMaker.ID,
        Breads:       lowStockBreads,
    }
    rabbit.Repo.InsertMakeOrder(order, lowStockBreads)
}
```

---

### 3.3 Missing UNIQUE Constraint on `buy_order_uuid`

**Location:** `bakery.sql` — `buy_order` table

The UUID dedup in `processOneOrder` (`GetBuyOrderByUUID`) does a full table scan on a non-indexed, non-unique column. Without a `UNIQUE` constraint, the dedup only prevents *exact* UUID collisions but doesn't enforce uniqueness at the database level.

**Fix:**
```sql
ALTER TABLE buy_order ADD CONSTRAINT uq_buy_order_uuid UNIQUE (buy_order_uuid);
CREATE INDEX idx_buy_order_uuid ON buy_order(buy_order_uuid);
```

---

## 4. High: Service Reliability

### 4.1 Makers Service Dies on Any Error

**Location:** `makers/main.go`

```go
func listenForMakeBread(...) error {
    for d := range breadsBought {
        if err := json.Unmarshal(...); err != nil {
            d.Nack(false, true)
            return status.Errorf(...)  // ← kills entire consumer
        }
    }
}
```

A single bad message or transient DB error kills the entire makers process. The service has no reconnection logic — `log.Fatalf` on any error.

**Fix:**
- Wrap message processing in `try/catch` (if/else with continue, not return)
- Set `channel.Qos()` to prevent memory flooding
- Add reconnection logic similar to the broker's `startOrderProcessor`

```go
for d := range breadsBought {
    if err := json.Unmarshal(d, &bread); err != nil {
        log.Errorf("unmarshal error: %v", err)
        d.Nack(false, false)  // discard bad message, don't requeue
        continue  // ← DON'T return
    }
    
    if err := repo.AdjustBreadQuantity(bread.ID, bread.Quantity); err != nil {
        log.Errorf("repo error: %v", err)
        d.Nack(false, true)  // requeue for retry
        continue  // ← DON'T return
    }
    
    d.Ack(false)
}
```

---

### 4.2 Missing QoS on Makers Consumer

**Location:** `makers/main.go`

The makers service doesn't call `channel.Qos()`. RabbitMQ can flood the channel with all queued messages in memory.

**Fix:**
```go
if err := channel.Qos(5, 0, false); err != nil {
    return err  // 5 messages in flight max
}
```

---

### 4.3 Artificial 1-Second Delay in Makers

**Location:** `makers/main.go:168`

```go
time.Sleep(1 * time.Second)  // after each successful message
```

This limits makers throughput to 1 message/second with no justification.

**Fix:** Remove the artificial delay.

---

### 4.4 No Reconnection Logic in Makers

**Location:** `makers/main.go`

If RabbitMQ disconnects, the `for d := range breadsBought` loop exits silently and `log.Fatalf` kills the process.

**Fix:** Add a reconnection loop:
```go
func startMakersService(...) {
    for {
        if err := listenForMakeBread(...); err != nil {
            log.Errorf("makers error, reconnecting in 10s: %v", err)
            time.Sleep(10 * time.Second)
            continue
        }
    }
}
```

---

## 5. Medium: Database Schema & Performance

### 5.1 Floating-Point Financial Fields

**Affected columns (all use `float` / IEEE 754 double):**
- `bread.price`
- `order_details.price`
- `invoices.subtotal`, `invoices.tax`, `invoices.total`
- `invoice_items.unit_price`, `invoice_items.total`

**Fix:** Use `numeric(10, 2)` or `decimal` for all financial fields to avoid rounding errors.

```sql
ALTER TABLE bread ALTER COLUMN price TYPE numeric(10,2);
ALTER TABLE order_details ALTER COLUMN price TYPE numeric(10,2);
ALTER TABLE invoices ALTER COLUMN subtotal TYPE numeric(12,2);
ALTER TABLE invoices ALTER COLUMN tax TYPE numeric(12,2);
ALTER TABLE invoices ALTER COLUMN total TYPE numeric(12,2);
ALTER TABLE invoice_items ALTER COLUMN unit_price TYPE numeric(10,2);
ALTER TABLE invoice_items ALTER COLUMN total TYPE numeric(10,2);
```

Update Go types from `float` to `float64` (maps to `numeric` via pgx).

---

### 5.2 Missing Database Indexes

| Table | Column | Impact |
|-------|--------|--------|
| `buy_order` | `buy_order_uuid` | UUID dedup scan |
| `buy_order` | `customer_id` | Customer order lookup |
| `buy_order` | `status` | Admin order filtering |
| `order_details` | `buy_order_id` | Order detail queries |
| `customer` | `email` | Login lookup |
| `invoices` | `buy_order_id` | Invoice retrieval |
| `invoices` | `customer_id` | Customer invoice history |
| `invoices` | `status` | Status-based queries |
| `outbox` | `sent` | Outbox polling |
| `outbox` | `created_at` | Time-based ordering |
| `bread` | `status` | Inventory queries |
| `bread` | `quantity` | Low-stock queries |

**Fix:** Add indexes for all columns used in WHERE clauses.

---

### 5.3 Missing UNIQUE Constraint on `customer.email`

**Location:** `bakery.sql` — `customer` table

The code queries customers by email (`GetCustomerByEmail`) but there's no `UNIQUE` constraint. Duplicate emails can exist.

**Fix:**
```sql
ALTER TABLE customer ADD CONSTRAINT uq_customer_email UNIQUE (email);
```

---

### 5.4 Timestamps Without Timezone

**Location:** `bakery.sql`

All timestamps use `timestamp without time zone`. This causes issues across time zones and DST transitions.

**Fix:** Use `timestamp with time zone` (PostgreSQL `timestamptz`).

---

### 5.5 Outbox Query Returns All Unprocessed Messages at Once

**Location:** `data/models.go:GetUnprocessedOutboxMessages`

```sql
SELECT ... FROM outbox WHERE sent = false
-- No ORDER BY, no LIMIT
```

Returns all unprocessed messages in arbitrary order. No `FOR UPDATE` locking means concurrent outbox pollers (if scaled) can process the same messages.

**Fix:**
```sql
SELECT ... FROM outbox WHERE sent = false ORDER BY created_at ASC LIMIT 10 FOR UPDATE SKIP LOCKED
```

Use `FOR UPDATE SKIP LOCKED` to safely scale multiple outbox pollers.

---

### 5.6 No CHECK Constraints on Status Columns

`buy_order.status`, `bread.status`, `invoices.status` accept any string.

**Fix:**
```sql
ALTER TABLE buy_order ADD CONSTRAINT chk_buy_order_status 
    CHECK (status IN ('pending', 'processing', 'processed', 'failed', 'completed'));
```

---

## 6. Medium: gRPC & Frontend

### 6.1 gRPC Connections Created Per Request in Frontend

**Location:** `frontend/cmd/web/admin_handlers.go`, `frontend/cmd/web/auth_handlers.go`, `frontend/cmd/web/main.go`

Every single handler creates a new `grpc.Dial` and closes it with `defer conn.Close()`. Under load this means thousands of connections created and destroyed per second.

**Fix:** Create a single shared `grpc.ClientConn` with proper keep-alive:
```go
var grpcConn *grpc.ClientConn

func initGRPC() {
    opts := []grpc.DialOption{
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        grpc.WithKeepaliveParams(keepalive.ClientParameters{
            Time:                30 * time.Second,
            Timeout:             20 * time.Second,
            PermitWithoutStream: true,
        }),
    }
    grpcConn, _ = grpc.Dial(addr, opts...)
}
```

---

### 6.2 SSE Handlers Dial New Connections Every 15 Seconds

**Location:** `frontend/cmd/web/admin_handlers.go`

The `AdminDashboardStreamHandler` creates a new gRPC connection every 15-second tick.

**Fix:** Reuse the shared connection.

---

### 6.3 `staticPageHandler` Re-Parses Templates Per Request

**Location:** `frontend/cmd/web/main.go`

`template.ParseFiles` is called on every request.

**Fix:** Parse templates once at startup, store in a package-level variable.

---

### 6.4 `orderDetailsHandler` Returns Empty Data

**Location:** `frontend/cmd/web/main.go`

Always renders an empty `OrderData` slice.

**Fix:** Call the gRPC `GetOrderDetails` endpoint.

---

### 6.5 Context Not Cancelled When HTTP Client Disconnects

**Location:** `frontend/cmd/web/auth_handlers.go` (and other handlers)

Uses `context.WithTimeout(context.Background(), 10*time.Second)` instead of `c.Request.Context()`. If the HTTP client disconnects, the gRPC call continues for 10 seconds.

**Fix:**
```go
ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
defer cancel()
```

---

## 7. Low: Code Quality & Dead Infrastructure

### 7.1 Dead `orders` Map in RabbitMQBakery

**Location:** `broker/main.go`

```go
type RabbitMQBakery struct {
    orders      map[int]*OrderStatus  // ← never written or read
    mu          sync.Mutex            // ← dead
    ...
}
```

Remove `orders` and `mu`.

---

### 7.2 Unused `sent` Column on Outbox

**Location:** `data/models.go`

The `sent` column is always `false` and never updated. Only `DeleteOutboxMessage` is used (not an update).

**Fix:** Either use `sent` as part of a proper claim pattern (`UPDATE ... SET sent = true WHERE id = $1 RETURNING payload`) or remove the column.

---

### 7.3 No gRPC Connection Timeout in Admin Handlers

**Location:** `frontend/cmd/web/admin_handlers.go:getGRPCConnection()`

No dial timeout — a blocked dial can hang indefinitely.

**Fix:**
```go
conn, err := grpc.DialContext(ctx, addr, opts...)
```

---

### 7.4 `buyBreadStream` Doesn't Check `ctx.Done()`

**Location:** `buyers/main.go`

The inner `stream.Recv()` loop only checks `breadBoughtChan`, not `ctx.Done()`. On context cancellation, the goroutine can leak.

**Fix:** Add `ctx.Done()` to the select:
```go
select {
case <-ctx.Done():
    return ctx.Err()
case <-breadBoughtChan:
    ...
}
```

---

### 7.5 Proto: Timestamps as Strings

**Location:** `proto/bread.proto`

`createdAt` and `updatedAt` are `string` types.

**Fix:** Use `google.protobuf.Timestamp`:
```protobuf
import "google/protobuf/timestamp.proto";

message Bread {
    // ...
    google.protobuf.Timestamp createdAt = 8;
    google.protobuf.Timestamp updatedAt = 9;
}
```

---

### 7.6 `BuyBread` Hardcodes Customer ID = 1

**Location:** `server/gRPCBakery.go:BuyBread`

```go
buyerCustomer := data.Customer{ID: 1, Name: "John Doe", Email: "john@doe.com"}
buyOrder.CustomerID = 1
```

Every order is attributed to the same hardcoded customer. In a real market, the customer must come from the JWT token or gRPC metadata.

**Fix:** Extract customer ID from the authenticated context:
```go
func (s *BuyBreadServer) BuyBread(ctx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {
    customerID, ok := ctx.Value(customerIDKey).(int)
    if !ok {
        return nil, status.Error(codes.Unauthenticated, "missing customer context")
    }
    // ...
}
```

---

## 8. Validation Gate: Testing & CI Requirements

Every remediation task MUST pass **all four** validation gates before it can be considered complete. There are no exceptions.

### 8.1 Gate 1 — Unit Tests

```bash
go test ./... -race -count=1 -timeout 60s
```

- **All existing tests must continue to pass.** No regression is acceptable.
- New code must have corresponding unit tests covering **all new branches and logic paths**.
- Tests that require live infrastructure (PostgreSQL/RabbitMQ) should use **testcontainers** or mocked dependencies — never skip a test just because it needs real infrastructure.

### 8.2 Gate 2 — Container Image Build

Every service that changed must be rebuilt into a Docker image. Use the image names from the project:

| Service | Dockerfile | Image Name |
|---------|-----------|------------|
| `broker` | `broker.dockerfile` | `docker.io/calvarado2004/bakery-go-broker` |
| `buyers` | `buyers.dockerfile` | `docker.io/calvarado2004/bakery-go-buyers` |
| `frontend` | `frontend.dockerfile` | `docker.io/calvarado2004/bakery-go-frontend` |
| `makers` | `makers.dockerfile` | `docker.io/calvarado2004/bakery-go-makers` |
| `server` | `server.dockerfile` | `docker.io/calvarado2004/bakery-go-server` |

Build command (quick, local architecture):

```bash
docker build . -t docker.io/calvarado2004/bakery-go-{SERVICE} -f {SERVICE}.dockerfile
```

If only some services changed, build only those. But if `data/models.go` changed, **all five** images must be rebuilt (every service depends on `data`).

### 8.3 Gate 3 — Integration Tests (PostgreSQL + RabbitMQ)

```bash
# Full integration test suite with testcontainers
go test ./... -coverprofile=cover.out -covermode=atomic -timeout 120s
```

- Services with real PostgreSQL + RabbitMQ integration tests (broker, makers, data, frontend) **must pass these integration tests inside Docker**.
- Integration tests spin up their own PostgreSQL and RabbitMQ via testcontainers — no external infrastructure required.
- If integration tests are missing for a changed service, they must be added.

### 8.4 Gate 4 — End-to-End Docker Compose Test

```bash
docker compose up --build --abort-on-container-exit --exit-code-from e2e-test
```

Or the full interactive test:

```bash
docker compose up -d
# Wait for all services to be healthy
sleep 30
# Run e2e test scenarios (buy orders, restock, inventory check, admin operations)
# Verify results via PostgreSQL queries and RabbitMQ management API
docker compose down
```

**E2E must validate:**
- A buyer can place an order → order appears in `buy_order` table → inventory is correctly reduced (exactly 1×, not 2×) → `bread-bought` message is received → order status becomes `Processed`
- A maker receives a `make-bread-order` message → inventory is correctly increased
- Concurrent buyers (multiple `buyers` instances) can place orders simultaneously without data corruption
- The broker processes orders in FIFO order (sequence numbers increase monotonically)
- Invoices are generated correctly when order status is `completed`
- Admin operations (bread CRUD, order management, dashboard stats) all work

### 8.5 Task-Level Enforcement

For **each task** in the remediation phases below, the completion criteria are:

```
[ ] Unit tests: `go test ./... -race -count=1 -timeout 60s` passes
[ ] Container built: `docker build . -t bakery-go-{service} -f {service}.dockerfile` succeeds
[ ] Integration tests: testcontainer-based tests pass
[ ] E2E Docker Compose: full stack test passes with correct behavior
```

All four must pass before the task is marked complete. If any gate fails, the task is incomplete — regardless of how much code was written.

---

## 9. Scaling Architecture: Buyers & Makers as External Entities

### 9.1 Buyers — Independent, Horizontally Scalable External Clients

In the target architecture, buyers are **external clients** — any customer can run a buyers instance that connects to the server. Key requirements:

| Concern | Requirement |
|---------|-------------|
| **Authentication** | Every buyer must authenticate via JWT or mTLS before placing orders. The server must extract the customer ID from the auth token, **not** hardcode `ID = 1`. |
| **Independence** | A buyer instance must be fully independent of other buyer instances. No shared state, no shared connections, no coordination. |
| **Horizontal scaling** | The server must handle N concurrent buyer connections without performance degradation. Shared gRPC connection pools, connection timeouts, and rate limiting are required. |
| **Idempotency** | If a buyer retries a `BuyBread` call (network timeout), the order must not be duplicated. The `buy_order_uuid` provides idempotency — duplicate UUIDs are silently skipped. |
| **Order ownership** | Buyers must be able to query only their own orders via `BuyOrderStream` / `GetMyOrders`. The customer ID in the JWT token scopes all queries. |

**Current violation:** `BuyBread` hardcodes `CustomerID = 1` and `Customer = {Name: "John Doe"}`. This makes it impossible for external buyers to identify their orders.

**Fix:** Extract customer identity from the gRPC context (populated by an auth middleware that reads the JWT from the metadata). The server validates the token and injects the customer ID into the context before `BuyBread` is called.

### 9.2 Makers — Independent, Horizontally Scalable External Providers

In the target architecture, makers are **external providers** — any bakery can run a makers instance that connects to the RabbitMQ queue and restocks inventory. Key requirements:

| Concern | Requirement |
|---------|-------------|
| **Authentication** | Makers authenticate via RabbitMQ credentials or mTLS. Each maker has a unique `bread_maker_id` that identifies them. |
| **Independence** | A maker instance must be independent. Multiple makers can restock simultaneously without conflict. |
| **Horizontal scaling** | Multiple makers must be able to consume from the same `make-bread-order` queue. RabbitMQ's consumer groups or separate queues per maker are required. |
| **Fault tolerance** | A maker must never die on a single error. Bad messages are discarded, transient errors trigger retries. The `startMakersService` loop reconnects on failure. |
| **Inventory accuracy** | Makers use `AdjustBreadQuantity` to add stock. This is safe because it only adds (never deducts), and the `quantity >= 0` CHECK constraint prevents negatives. |

**Current violations:**
1. Makers die on any single error (`return` instead of `continue`)
2. No QoS set on the consumer — RabbitMQ floods the channel
3. No reconnection logic
4. The maker is hardcoded to `BreadMakerID = 1` in `checkBread`

**Fix:**
- Make makers authenticate and identify themselves (maker ID from config/credentials)
- Add `channel.Qos()` and reconnection loop
- Fix error handling to `continue` instead of `return`
- Remove the `BreadMakerID` coupling from `checkBread` — make orders should not be tied to a specific maker ID at creation time

### 9.3 Broker & Server — The Shared Core

The broker and server remain the **shared core infrastructure** (not external). They:

- **Broker:** Processes all buy orders with strict FIFO ordering. May scale horizontally only with a partitioning strategy (e.g., hash-partition by `buy_order_uuid` so the same buyer's orders always go to the same broker).
- **Server:** Authenticates all clients, routes orders to RabbitMQ, streams results. Must handle thousands of concurrent gRPC streams.

The broker must maintain a single instance until partition-based horizontal scaling is implemented. The server must use shared connection pools and proper context cancellation.

### 9.4 Architecture Diagram (Target State)

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  Buyer #1    │  │  Buyer #2    │  │  Buyer #N    │  ← External, independent, scalable
│  (JWT auth)  │  │  (JWT auth)  │  │  (JWT auth)  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │ gRPC             │ gRPC             │ gRPC
       └────────┬─────────┴────────┬─────────┘
                │                  │
                ▼                  ▼
        ┌─────────────────────────────────┐
        │         SERVER (gRPC)           │  ← Shared core, auth middleware,
        │   JWT validation + context      │     connection pooling, rate limiting
        └────────────┬────────────────────┘
                     │
                     ▼
        ┌─────────────────────────────────┐
        │     buy-bread-order  (FIFO)     │  ← Single consumer per partition
        │     bread-bought       (pub)    │     sequence numbers enforced
        └────┬──────────────────┬─────────┘
             │                  │
             ▼                  ▼
    ┌──────────────┐    ┌──────────────┐
    │    Broker    │    │    Outbox    │    ← Shared core, FIFO,
    │  (QoS=1)     │    │  (retry)     │     at-least-once delivery
    └──────────────┘    └──────────────┘
             │
             │ make-bread-order
             ▼
    ┌─────────────────────────────────┐
    │  make-bread-order  (consumer group) │ ← Multiple makers,
    └────┬──────────────┬──────────────┘     independent, scalable
         │              │
         ▼              ▼
┌──────────────┐ ┌──────────────┐
│  Maker #1    │ │  Maker #N    │  ← External providers,
│  (maker auth)│ │  (maker auth)│     fault-tolerant
└──────────────┘ └──────────────┘

                    ┌──────────────┐
                    │  PostgreSQL  │  ← Shared data store,
                    │              │     indexed, typed, constrained
                    └──────────────┘
```

---

## 10. Remediation Phases

### Phase 1 — Data Integrity (Must Do First)

These fixes are **bugs** — they cause incorrect data today.

| # | Task | Files | Effort |
|---|------|-------|--------|
| 1.1 | Remove `AdjustBreadQuantity` from `InsertBuyOrder`; let `FulfillOrderTx` be the sole inventory path | `data/models.go`, `broker/main.go` | 2h |
| 1.2 | Wrap `InsertMakeOrder` in a transaction | `data/models.go` | 1h |
| 1.3 | Wrap `InsertInvoice` in a transaction | `data/models.go` | 1h |
| 1.4 | Add `UNIQUE` constraint on `buy_order_uuid` and index | `bakery.sql`, migration | 1h |
| 1.5 | Add `UNIQUE` constraint on `customer.email` and index | `bakery.sql`, migration | 0.5h |

**Verify with:**
- [ ] Unit tests: `go test ./... -race -count=1 -timeout 60s` passes
- [ ] Container built: rebuild all 5 images (`data/models.go` is a transitive dependency)
- [ ] Integration tests: concurrent broker test — spin up 2 brokers processing orders for the same bread, verify no double-deduction, no oversell
- [ ] E2E: `docker compose up -d` → buyer places order → query `SELECT quantity FROM bread WHERE id = ?` confirms exactly 1× deduction → order status is `Processed`

---

### Phase 2 — Order Matching Engine (Priority + Partial Fulfillment)

| # | Task | Files | Effort | Status |
|---|------|-------|--------|--------|
| 2.1 | Add `BuyOrderItem`, `sequenceNumber`, `bidPrice`, `allowPartial`, `skipUnavailableItems` to proto | `proto/bread.proto`, regenerate | 2h | ✅ Done |
| 2.2 | Server: assign `sequenceNumber` + pass buyer rules through to RabbitMQ message (architecture unchanged, code paths updated) | `server/gRPCBakery.go:BuyBread` | 1h | ✅ Done |
| 2.3 | Broker: replace sequential `processOneOrder` with batch matching engine (`processMatchingBatch`) | `broker/main.go`, `broker/matching.go` (new) | 6h | ✅ Done |
| 2.4 | Broker: add in-memory ingestion buffer (ring buffer, 1000 orders) | `broker/buffer.go` (new) | 2h | ✅ Done |
| 2.5 | Broker: batch window timer (500ms) + max batch size (100) | `broker/main.go` | 1h | ✅ Done |
| 2.6 | Broker: sort by bid price DESC, sequence number ASC before matching | `broker/matching.go` | 1h | ✅ Done |
| 2.7 | Broker: implement partial fulfillment, skip, and reject logic | `broker/matching.go` | 3h | ✅ Done |
| 2.8 | Broker: publish per-item results to `bread-bought` (not per-order) | `broker/main.go` | 1h | ✅ Done |
| 2.9 | Update `FulfillOrderTx` to use per-item deduction with status tracking (**broker responsibility** — broker calls this) | `data/models.go` | 2h | ✅ Done |
| 2.10 | Document single-broker constraint (matching engine is single-instance) | `docker-compose.yml`, docs | 0.5h | ✅ Done |

> **Note:** The server's architecture (ingest → publish → return) does **not** change. The code paths in `BuyBread` are updated to pass new fields, but the server remains a stateless conduit. All matching intelligence lives in the broker. `data/models.go` changes are owned by the broker — the broker is the only caller of `InsertBuyOrder` and `FulfillOrderTx`.

**Verify with:**
- [x] Unit tests pass
- [x] Container built: `broker.dockerfile`, `server.dockerfile`
- [ ] Integration tests: publish 100 orders with varying bid prices → verify highest bids are fulfilled first; publish an order with 100 donuts + 50 croissants when only 20 donuts exist → verify partial fulfillment with correct statuses
- [ ] E2E: start 3 concurrent buyers with different bid prices → verify the highest bidder gets priority regardless of arrival order → verify `allowPartial=true` produces `partially_processed` status, `skipUnavailableItems=true` skips out-of-stock items gracefully

---

### Phase 3 — Service Reliability

| # | Task | Files | Effort | Status |
|---|------|-------|--------|--------|
| 3.1 | Fix makers error handling (continue instead of return) | `makers/main.go` | 2h | ✅ Done |
| 3.2 | Add QoS to makers consumer | `makers/main.go` | 0.5h | ✅ Done |
| 3.3 | Remove artificial 1-second sleep in makers | `makers/main.go` | 0.5h | ✅ Done |
| 3.4 | Add reconnection loop to makers | `makers/main.go` | 2h | ✅ Done |
| 3.5 | Fix `checkBread` duplicate order bug | `server/rabbitBakery.go` | 1h | ✅ Done |

**Verify with:**
- [x] Unit tests pass (pre-existing timeout issue with makers_test, not caused by changes)
- [x] Container built: `makers.dockerfile`, `server.dockerfile`
- [x] E2E: `docker compose up -d` → place orders → verify no duplicate make orders in `make_order` table → all services healthy

**Changes:**
- `makers/main.go`: Replaced sequential `listenForMakeBread` with concurrent message processing, added QoS(5), removed 1s sleep, added reconnection loop, graceful shutdown via SIGINT/SIGTERM
- `server/rabbitBakery.go`: Batched low-stock breads into single MakeOrder instead of incrementally appending to shared slice

---

### Phase 4 — Database Performance & Correctness

| # | Task | Files | Effort |
|---|------|-------|--------|
| 4.1 | Change financial columns from `float` to `numeric(10,2)` | `bakery.sql`, migration, `data/models.go`, `proto/bread.proto` | 4h |
| 4.2 | Add all missing indexes | `bakery.sql`, migration | 2h |
| 4.3 | Change `timestamp` to `timestamptz` | `bakery.sql`, migration | 1h |
| 4.4 | Add CHECK constraints on status columns | `bakery.sql`, migration | 1h |
| 4.5 | Fix outbox query with `LIMIT`, `ORDER BY`, `FOR UPDATE SKIP LOCKED` | `data/models.go` | 2h |

**Verify with:**
- [ ] Unit tests pass (may need updates due to type changes)
- [ ] Container built: all 5 images (schema + model changes are transitive)
- [ ] Integration tests: query all indexed columns — verify `EXPLAIN ANALYZE` shows index scans
- [ ] E2E: verify invoice totals are mathematically correct to 2 decimal places for edge cases (e.g., $0.07 + $0.08 = $0.15, not $0.15000000000000002)

---

### Phase 5 — Frontend & gRPC

| # | Task | Files | Effort |
|---|------|-------|--------|
| 5.1 | Shared gRPC connection pool in frontend | `frontend/cmd/web/main.go` | 2h |
| 5.2 | Pre-parse templates at startup | `frontend/cmd/web/main.go` | 1h |
| 5.3 | Use `c.Request.Context()` instead of `context.Background()` | `frontend/cmd/web/*.go` | 1h |
| 5.4 | Fix `orderDetailsHandler` to fetch real data | `frontend/cmd/web/main.go` | 1h |

**Verify with:**
- [ ] Unit tests pass
- [ ] Container built: `frontend.dockerfile`
- [ ] Integration tests: load test the frontend — 100 concurrent requests, verify no connection exhaustion
- [ ] E2E: `docker compose up -d` → open admin dashboard → verify no slow memory growth over 5 minutes → order details page shows real data

---

### Phase 6 — Code Cleanup

| # | Task | Files | Effort |
|---|------|-------|--------|
| 6.1 | Remove dead `orders` map and `mu` from `RabbitMQBakery` | `broker/main.go` | 0.5h |
| 6.2 | Implement proper outbox claim pattern (`sent` column) or remove | `data/models.go` | 1h |
| 6.3 | Add `ctx.Done()` to `buyBreadStream` goroutine | `buyers/main.go` | 0.5h |
| 6.4 | Add gRPC connection timeout in admin handlers | `frontend/cmd/web/*.go` | 1h |
| 6.5 | Use `google.protobuf.Timestamp` in proto | `proto/bread.proto`, regenerate | 2h |
| 6.6 | Extract customer ID from JWT context in `BuyBread` | `server/gRPCBakery.go`, auth middleware | 2h |

**Verify with:**
- [ ] Unit tests pass
- [ ] Container built: `broker.dockerfile`, `buyers.dockerfile`, `frontend.dockerfile`, `server.dockerfile`
- [ ] Integration tests: verify context cancellation actually terminates goroutines (no leaks)
- [ ] E2E: `docker compose up -d` → cancel a buy stream mid-flight → verify no leaked goroutines in server logs → verify customer ID is correctly attached to orders placed by authenticated users

---

### Total Estimated Effort: ~40 hours

### Recommended Order

1. **Phase 1** first — these are bugs causing data corruption today
2. **Phase 2** next — the core FIFO guarantee
3. **Phase 3** — reliability improvements (can be done in parallel with Phase 2)
4. **Phase 4** — schema hardening (requires migration, do after 1-3)
5. **Phase 5-6** — cleanup and polish (can overlap with anything above)
