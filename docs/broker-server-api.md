# Broker-Server gRPC API

> **Protocol:** gRPC over plain TCP (development)
> **Contract:** [`proto/bread.proto`](../proto/bread.proto) → `BrokerService`
> **Port:** `BAKERY_SERVICE_ADDR` (default `:50051`)

---

## 1. Overview

The **Broker** is a pure message dispatcher. It consumes orders from RabbitMQ, runs the matching engine, and communicates with the **Server** exclusively via the `BrokerService` gRPC API. The broker has **zero direct database access**.

```
┌──────────────┐     gRPC: BrokerService      ┌──────────────┐
│    BROKER    │ ──▶ ReportOrder ───────────▶ │    SERVER    │
│  (matching)  │ ◀─ BrokerOrderResult         │  (DB owner)  │
│              │                              │              │
│  RabbitMQ:   │ ──▶ ReserveInventory ──────▶ │              │
│  consume     │ ◀─ ReserveInventoryResult    │              │
│  from        │                              │              │
│  buy-bread-  │ ──▶ ReportMatchingResults ──▶ │              │
│  order       │ ◀─ BatchConfirmation          │              │
└──────────────┘                              └──────────────┘
                                                   │
                                             PostgreSQL
```

**Design principles:**
- The broker is stateless about data — it only holds an in-memory order buffer
- The server is the single source of truth for all data operations
- The broker uses **circuit breakers** and **retry with exponential back-off** for resilience
- If the server is unavailable, the broker's circuit breaker opens and calls fail fast

---

## 2. Service Definition

```protobuf
service BrokerService {
  // ReportOrder notifies the server of a new order consumed from
  // RabbitMQ. The server handles deduplication and persistence.
  rpc ReportOrder(BuyOrder) returns (BrokerOrderResult);

  // ReserveInventory atomically checks and deducts stock for one
  // item in a matching batch.
  rpc ReserveInventory(ReserveInventoryRequest) returns (ReserveInventoryResult);

  // ReportMatchingResults sends the complete batch matching results
  // to the server. The server updates order statuses and writes
  // results to the outbox in a single transaction.
  rpc ReportMatchingResults(MatchingBatch) returns (BatchConfirmation);
}
```

---

## 3. RPC: ReportOrder

### 3.1 Purpose

When the broker consumes a new order from `buy-bread-order`, it calls `ReportOrder` to persist the order in the database. The server handles UUID deduplication.

### 3.2 Request

```protobuf
message BuyOrder {
  int32 id = 1;
  int32 customerId = 2;
  string buyOrderUuid = 3;     // idempotency key
  double totalCost = 4;
  string status = 5;            // "processing"
  int64 sequenceNumber = 6;
  double bidPrice = 7;
  bool allowPartial = 8;
  bool skipUnavailableItems = 9;
  google.protobuf.Timestamp created_at = 10;
  repeated BuyOrderItem items = 11;
}

message BuyOrderItem {
  int32 breadId = 1;
  int32 quantityRequested = 2;
  int32 quantityFulfilled = 3;
  double bidPrice = 4;
  string status = 5;
}
```

### 3.3 Response

```protobuf
message BrokerOrderResult {
  bool accepted = 1;            // false if duplicate
  int32 orderId = 2;            // persisted order ID (0 if duplicate)
  string message = 3;           // "accepted" or "duplicate"
}
```

### 3.4 Server Behavior

1. Query `buy_order` by `buy_order_uuid`
2. If found → return `accepted: false, message: "duplicate"`
3. If not found → INSERT order + order_details in a transaction
4. Return `accepted: true, orderId: <new_id>`

### 3.5 Broker Behavior

- Called **once per order** as it's consumed from RabbitMQ
- The broker ACKs the RabbitMQ message **before** calling `ReportOrder`
- If `ReportOrder` fails, the broker retries with circuit-breaker + back-off
- If the order is a duplicate, the broker continues (it was already processed)

---

## 4. RPC: ReserveInventory

### 4.1 Purpose

During batch matching, the broker calls `ReserveInventory` for each item that needs stock. This is an atomic "check-and-deduct" operation.

### 4.2 Request

```protobuf
message ReserveInventoryRequest {
  string buyOrderUuid = 1;      // identifies the order
  int32 breadId = 2;            // bread to reserve
  int32 quantityRequested = 3;  // how much is requested
}
```

### 4.3 Response

```protobuf
message ReserveInventoryResult {
  bool reserved = 1;            // true if stock was available
  int32 quantityFulfilled = 2;  // actual quantity deducted
  string message = 3;           // "reserved" or "insufficient_stock"
}
```

### 4.4 Server Behavior

1. `SELECT * FROM bread WHERE id = $1 FOR UPDATE` — locks the row
2. Check if `quantity >= quantityRequested`
3. If yes: `UPDATE bread SET quantity = quantity - quantityRequested` → return `reserved: true`
4. If no: return `reserved: false, quantityFulfilled: 0, message: "insufficient_stock"`

### 4.5 Broker Behavior

- Called **once per item** in a matching batch
- The broker accumulates results for each order (fulfilled, partially, skipped, rejected)
- If `ReserveInventory` fails (server unavailable), the circuit breaker opens and the batch is retried later

---

## 5. RPC: ReportMatchingResults

### 5.1 Purpose

After the matching engine completes a batch, the broker sends all results to the server in a single call. The server updates order statuses and writes outbox messages atomically.

### 5.2 Request

```protobuf
message MatchingBatch {
  repeated MatchingBatchResult results = 1;
}

message MatchingBatchResult {
  string buyOrderUuid = 1;
  string orderStatus = 2;       // "processed" | "partially_processed" | "rejected" | "failed"
  repeated MatchingItemResult items = 3;
  double totalCost = 4;
}

message MatchingItemResult {
  int32 breadId = 1;
  int32 quantityRequested = 2;
  int32 quantityFulfilled = 3;
  string status = 4;            // "fulfilled" | "partially_fulfilled" | "skipped" | "rejected"
}
```

### 5.3 Response

```protobuf
message BatchConfirmation {
  bool accepted = 1;
  int32 ordersProcessed = 2;    // number of orders in the batch
  string message = 3;           // "accepted"
}
```

### 5.4 Server Behavior

1. Begin transaction
2. For each result:
   a. `UPDATE buy_order SET status = $1 WHERE buy_order_uuid = $2`
   b. `INSERT INTO outbox (payload, sent, created_at) VALUES ($1, false, NOW())`
      — payload is a JSON object containing order UUID, order status, and per-item results
3. Commit transaction

### 5.5 Broker Behavior

- Called **once per batch** (not per order)
- The batch contains results from the entire matching window (500ms or 100 orders)
- The broker ACKs all consumed RabbitMQ messages **before** calling `ReportMatchingResults`
- If the call fails, the batch is retried with circuit-breaker + back-off

---

## 6. Resilience (Phase 10.10)

### 6.1 Circuit Breakers

Each RPC has its own circuit breaker with the following configuration:

| RPC | Failure Threshold | Reset Timeout | Max Retries |
|-----|------------------|---------------|-------------|
| `ReportOrder` | 5 | 30s | 3 |
| `ReserveInventory` | 5 | 30s | 3 |
| `ReportMatchingResults` | 3 | 60s | 3 |

**Circuit breaker states:**

```
     ┌──────────┐
     │  CLOSED  │ ← normal operation, count failures
     └────┬─────┘
          │ ≥N failures
          ▼
     ┌──────────┐
     │   OPEN   │ ← reject immediately, wait resetTimeout
     └────┬─────┘
          │ resetTimeout elapsed
          ▼
     ┌──────────┐
     │ HALF-OPEN│ ← allow ONE request to test
     └────┬─────┘
    ┌─────┴─────┐
    │ success   │ failure
    ▼           ▼
 CLOSED      OPEN
```

### 6.2 Retry Logic

All RPCs use exponential back-off retry:

| Parameter | Value |
|-----------|-------|
| Base delay | 100ms |
| Max delay | 2s |
| Multiplier | 2x |
| Max retries | 3 |

**Sequence for a failed RPC call:**
1. Attempt 1 → fails → wait 100ms
2. Check circuit breaker → if open, fail fast (don't retry)
3. Attempt 2 → fails → wait 200ms
4. Attempt 3 → fails → wait 400ms (capped at 2s)
5. Attempt 4 (final) → fails → return error to caller

### 6.3 Logging

Circuit breaker states are logged every 10 seconds:
- `StateClosed`: silent (healthy)
- `StateOpen`: warning with breaker name and remaining time
- `StateHalfOpen`: warning until resolved

---

## 7. Error Handling

### 7.1 gRPC Status Codes

| Status Code | Meaning | Broker Action |
|-------------|---------|---------------|
| `Unavailable` | Server unreachable | Retry + circuit breaker |
| `Internal` | Server error | Retry (may be transient) |
| `ResourceExhausted` | Rate limit | Back off and retry |
| `DeadlineExceeded` | Timeout | Retry (check deadline) |

### 7.2 Business Logic Errors

| Response | Meaning | Action |
|----------|---------|--------|
| `BrokerOrderResult{accepted: false, message: "duplicate"}` | Order already processed | Continue — order is in DB |
| `ReserveInventoryResult{reserved: false, message: "insufficient_stock"}` | Not enough stock | Mark item as skipped/rejected |
| `BatchConfirmation{accepted: false}` | Server rejected batch | Retry the entire batch |

---

## 8. Connection Management

### 8.1 Single Long-Lived Connection

The broker maintains **one gRPC connection** to the server for the lifetime of the process:

```go
conn, err := grpc.Dial(serverGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil {
    log.Fatalf("Failed to connect to server gRPC: %v", err)
}
defer conn.Close()

bc := newBrokerClient(conn)
```

### 8.2 Connection Recovery

If the gRPC connection drops:
1. Subsequent calls fail with `Unavailable`
2. Circuit breaker opens after threshold failures
3. Calls fail fast (no retry) until connection recovers
4. When connection re-establishes, circuit breaker transitions to half-open
5. Next successful call closes the circuit

The broker does **not** explicitly reconnect — gRPC handles connection recovery transparently.

---

## 9. Data Flow Summary

```
RabbitMQ "buy-bread-order"
        │
        ▼  consume
   ┌─────────┐
   │ Buffer  │  ← in-memory ring buffer (1000 orders)
   └────┬────┘
        │ batch every 500ms or when buffer ≥ 100
        ▼
   ┌─────────────────┐
   │  Matching Engine │  sort by bid DESC, sequence ASC
   └────┬────────────┘
        │
        ├────────────────────────────────────────────┐
        │                                            │
        ▼                                            ▼
   ┌───────────┐                          ┌──────────────────┐
   │ReportOrder │ ← order consumed        │ReportMatching    │ ← batch complete
   │(1x/order)  │                         │Results(1x/batch) │
   └─────┬─────┘                          └────────┬─────────┘
         │                                         │
         ▼                                         ▼
   ┌─────────────────────────────────────────────────────┐
   │              SERVER (PostgreSQL)                     │
   │                                                     │
   │  buy_order table ← status updates                   │
   │  outbox table ← matching results (sent=false)       │
   └─────────────────────────────────────────────────────┘
         │
         ▼
   Outbox Publisher → "bread-bought" queue → buyers
```

---

## 10. Deployment Notes

### 10.1 Startup Order

The broker and server are **independent** — neither depends on the other's health:

```yaml
# docker-compose.yml
broker:
  depends_on:
    postgres:
      condition: service_healthy
    rabbitmq:
      condition: service_healthy
  # NO depends_on: server

server:
  depends_on:
    postgres:
      condition: service_healthy
    rabbitmq:
      condition: service_healthy
  # NO depends_on: broker
```

### 10.2 Failure Scenarios

| Scenario | Broker Behavior | Server Recovery |
|----------|----------------|-----------------|
| Server starts after broker | Broker's circuit breaker opens, retries eventually succeed | N/A |
| Server restarts mid-batch | In-flight batch fails, retried on next matching window | Orders buffered in broker |
| Server down > resetTimeout | Circuit breaker opens, calls fail fast immediately | N/A |
| Network partition | gRPC connection drops, circuit breaker opens | N/A |
| Database locked | Server returns `Internal` errors, circuit breaker opens | Lock releases, circuit recovers |

### 10.3 Monitoring

Monitor these metrics:
- **Circuit breaker states** (logged every 10s)
- **Retry counts** per RPC
- **Buffer depth** (orders waiting for matching)
- **Matching throughput** (orders/second)
- **Queue depth** (`buy-bread-order` in RabbitMQ)
