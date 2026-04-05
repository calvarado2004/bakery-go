# Bakery Service — Messaging Layer (RabbitMQ)

## Table of Contents

1. [Overview](#overview)
2. [Queue Catalogue](#queue-catalogue)
3. [Message Formats](#message-formats)
4. [Publisher Descriptions](#publisher-descriptions)
5. [Consumer Descriptions](#consumer-descriptions)
6. [Outbox Pattern](#outbox-pattern)
7. [Error Handling and Retry Logic](#error-handling-and-retry-logic)
8. [Sequence Diagrams](#sequence-diagrams)
9. [Known Issues and Improvements](#known-issues-and-improvements)

---

## Overview

The Bakery Service uses **RabbitMQ** (via the `amqp091-go` client library) as its asynchronous messaging backbone. All queues are declared as **durable**, ensuring they survive broker restarts. Messages are serialised as **JSON**.

The messaging layer decouples three distinct concerns:

1. **Purchase processing** — buyer intent → broker execution → server confirmation
2. **Inventory replenishment** — server detection → makers execution → DB update
3. **Internal bread lifecycle** — make-bread events, bakery-ready events, removal events

---

## Queue Catalogue

| Queue Name          | Durable | Purpose                                                            | Publisher          | Consumer             |
|---------------------|---------|--------------------------------------------------------------------|--------------------|----------------------|
| `buy-bread-order`   | Yes     | Carry a JSON BuyOrder from Server to Broker for processing         | Server (BuyBread)  | Broker               |
| `bread-bought`      | Yes     | Carry Broker's confirmation of a processed order back to Server    | Broker             | Server (getBuyResponse, processBreadsBought) |
| `make-bread-order`  | Yes     | Signal Makers to increase bread inventory for a specific item      | Server (checkBread)| Makers               |
| `bread-to-make`     | Yes     | Internal: signal that a bread item should be queued for baking     | Server (BakeBread) | _(not consumed in current implementation)_ |
| `bread-in-bakery`   | Yes     | Internal: signal that bread is ready in the bakery                 | Server (SendBreadToBakery) | Server (MadeBreadStream) |
| `bread-removed`     | Yes     | Signal that stale/expired bread has been removed from inventory    | Server (RemoveBread) | Server (RemoveBreadStream) |

All queues are declared in `server/rabbitBakery.go` in the `init()` function using:

```go
ch.QueueDeclare(
    name,    // queue name
    true,    // durable
    false,   // auto-delete
    false,   // exclusive
    false,   // no-wait
    nil,     // arguments
)
```

---

## Message Formats

### `buy-bread-order` payload

A JSON-serialised `data.BuyOrder`:

```json
{
  "ID": 42,
  "CustomerID": 1,
  "BuyOrderUUID": "550e8400-e29b-41d4-a716-446655440000",
  "Status": "Pending",
  "Breads": [
    { "ID": 1, "Name": "Sourdough", "Price": 3.50, "Quantity": 2 },
    { "ID": 3, "Name": "Baguette",  "Price": 2.75, "Quantity": 1 }
  ]
}
```

### `bread-bought` payload

Same structure as `buy-bread-order`, but with `Status` updated to `"Processed"` or `"Failed"`:

```json
{
  "ID": 42,
  "CustomerID": 1,
  "BuyOrderUUID": "550e8400-e29b-41d4-a716-446655440000",
  "Status": "Processed",
  "Breads": [ ... ]
}
```

### `make-bread-order` payload

A JSON-serialised `data.Bread` (single item per message):

```json
{
  "ID": 2,
  "Name": "Croissant",
  "Price": 4.00,
  "Quantity": 20
}
```

The `Quantity` field in this context represents the **replenishment amount**, not the current stock.

### `bread-to-make`, `bread-in-bakery`, `bread-removed` payloads

Same structure as `make-bread-order`: single JSON-serialised `data.Bread`.

---

## Publisher Descriptions

### Server — `BuyBread` (gRPCBakery.go)

Triggered when a client calls the `BuyBread` RPC.

```
1. Insert BuyOrder into database
2. Marshal BuyOrder to JSON
3. Insert JSON into outbox table (sent = false)
4. Publish to "buy-bread-order"
5. Block waiting for bread-bought confirmation
```

Publishing call:
```go
ch.PublishWithContext(ctx,
    "",                  // default exchange
    "buy-bread-order",   // routing key (queue name)
    false,               // mandatory
    false,               // immediate
    amqp.Publishing{
        ContentType: "application/json",
        Body:        payload,
    },
)
```

### Server — `checkBread` (rabbitBakery.go)

Background goroutine running every **30 seconds**.

```
1. Query repo.GetAvailableBread()
2. For each bread with quantity < 10:
   a. Set replenishment quantity to 20
   b. Marshal Bread to JSON
   c. Publish to "make-bread-order"
```

### Broker — `bread-bought` (broker/main.go)

Published after the Broker has successfully processed or failed a buy order.

```
1. Receive buy-bread-order
2. Process order (validate, decrement quantities, update status)
3. Marshal updated BuyOrder to JSON
4. Insert into outbox table
5. Publish to "bread-bought"
6. Acknowledge "buy-bread-order"
```

---

## Consumer Descriptions

### Broker — `buy-bread-order` (broker/main.go)

```go
msgs, _ := ch.Consume("buy-bread-order", ...)
for msg := range msgs {
    performBuyBread(msg)
}
```

`performBuyBread` logic:

1. Unmarshal `BuyOrder` JSON from delivery body
2. For each `Bread` in the order:
   - Call `repo.GetBreadByID` to get current quantity
   - If `current_quantity >= requested_quantity`: decrement via `repo.AdjustBreadQuantity`
   - Else: mark order as `"Failed"`, stop processing
3. If all items validated: call `repo.UpdateOrderStatus("Processed")`
4. Publish updated order to `"bread-bought"`
5. Acknowledge the delivery (`msg.Ack(false)`)
6. On error: `msg.Nack(false, true)` to requeue

Additionally, a background goroutine in Broker publishes unprocessed outbox messages every **45 seconds**:

```go
for _, msg := range repo.GetUnprocessedOutboxMessages() {
    ch.PublishWithContext(ctx, "", "bread-bought", ...)
    // Note: does NOT delete from outbox after re-publish
}
```

> **Issue:** The outbox re-publish goroutine in the Broker does not delete or mark messages as sent after republishing, creating the risk of duplicate delivery.

### Server — `getBuyResponse` (rabbitBakery.go)

Called within `BuyBread` to wait for the `"bread-bought"` confirmation.

Retry strategy:
```
Attempt 1: wait 1 second
Attempt 2: wait 2 seconds
Attempt 3: wait 4 seconds
Attempt 4: wait 8 seconds
Attempt 5: wait 16 seconds (capped)
Max wait: ~31 seconds total
```

On each attempt:
1. Register a consumer on `"bread-bought"`
2. Wait for a delivery matching the expected `BuyOrderUUID`
3. If found: write the `BuyOrder` to the `OrderStatus.OrderChan`
4. If not found: exponential backoff and retry

> **Issue:** Each `getBuyResponse` call creates a **new dedicated consumer** on the `bread-bought` queue. When multiple orders are in-flight, each consumer will receive all messages from the queue, not just the one it is waiting for. This is a race condition and message routing design flaw.

### Server — `processBreadsBought` (rabbitBakery.go)

An alternative consumer path. Registers another consumer on `"bread-bought"` and writes deliveries to the `OrderStatus` map.

> **Issue:** Both `getBuyResponse` and `processBreadsBought` consume from the same `"bread-bought"` queue simultaneously. With AMQP round-robin dispatch, messages can be consumed by either consumer, causing missed confirmations.

### Makers — `make-bread-order` (makers/main.go)

```go
msgs, _ := ch.Consume("make-bread-order", ...)
for msg := range msgs {
    var bread data.Bread
    json.Unmarshal(msg.Body, &bread)
    repo.AdjustBreadQuantity(bread.ID, bread.Quantity)
    time.Sleep(1 * time.Second)  // simulate production time
    msg.Ack(false)
}
```

---

## Outbox Pattern

The Outbox Pattern ensures that a message is eventually delivered to RabbitMQ even if the broker is temporarily unavailable at the time of the original publish.

### Flow

```
Service (Server/Broker)
    │
    │  1. BEGIN DB operation
    │  2. Insert payload into outbox table (sent = false)
    │  3. Publish to RabbitMQ
    │
    ├─ If RabbitMQ publish succeeds:
    │      No further action needed (outbox entry can be cleaned up or ignored)
    │
    └─ If RabbitMQ publish fails or service restarts:
           Background goroutine (every 45s) reads outbox WHERE sent = false
           Re-publishes each payload
           (Marks as sent / deletes — see issue below)
```

### Outbox Table

```sql
CREATE TABLE outbox (
    id         SERIAL PRIMARY KEY,
    payload    BYTEA   NOT NULL,
    sent       BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
```

### Issues with Current Implementation

1. **Outbox rows are never deleted** in the re-publish path (the delete call is missing or inconsistent). Over time, this causes unbounded table growth.
2. **Duplicate messages** — at-least-once delivery is expected, but consumers must be idempotent. The Broker's order processing is not idempotent (it will re-decrement inventory if the same order is processed twice).
3. **No TTL** — stale outbox messages with no matching consumer will accumulate indefinitely.

---

## Sequence Diagrams

### Purchase Flow with Outbox and Confirmation

```
Buyers        Server            RabbitMQ          Broker           DB
  │               │                 │                  │             │
  │──BuyBread()──▶│                 │                  │             │
  │               │──InsertOrder───▶│                  │             │
  │               │──InsertOutbox──▶│                  │             │
  │               │──Publish───────▶│buy-bread-order   │             │
  │               │                 │─────────────────▶│             │
  │               │                 │                  │─Validate───▶│
  │               │                 │                  │─Decrement──▶│
  │               │                 │                  │─UpdateStatus▶│
  │               │──getBuyResponse─│                  │             │
  │               │  (retry loop)   │                  │             │
  │               │                 │◀────bread-bought─│             │
  │               │◀────────────────│                  │             │
  │               │─writes OrderChan│                  │             │
  │◀──BreadResp───│                 │                  │             │
```

### Inventory Replenishment Flow

```
Server (bg)       RabbitMQ        Makers            DB
     │                │               │              │
     │ (every 30s)    │               │              │
     │─CheckInventory─│               │              │
     │◀──bread qty < 10              │              │
     │──Publish──────▶│make-bread-order             │
     │                │──────────────▶│             │
     │                │               │─AdjustQty──▶│
     │                │               │─Ack()        │
```

---

## Known Issues and Improvements

| # | Component | Issue | Recommendation |
|---|-----------|-------|----------------|
| 1 | `getBuyResponse` | Creates new consumer per order — race condition | Use a single shared consumer with a UUID routing key or dedicated reply queue |
| 2 | `processBreadsBought` + `getBuyResponse` | Both consume `bread-bought` queue simultaneously | Consolidate into one consumer with a dispatch map |
| 3 | Broker outbox | Does not mark messages as sent after re-publish | Add `UPDATE outbox SET sent = true` after successful publish |
| 4 | Broker processing | Not idempotent — re-processing same order decrements inventory twice | Add deduplication check before processing (e.g., `orders_processed` table) |
| 5 | Outbox table | No cleanup mechanism | Add a periodic job to delete rows older than N days where `sent = true` |
| 6 | Queue error handling | `Nack` with `requeue = true` can cause infinite retry loop | Add a Dead Letter Exchange (DLX) and retry counter |
| 7 | No message priority | All messages treated equally | Consider priority queues for time-sensitive operations |
| 8 | No message TTL | Messages in queues can accumulate indefinitely | Set `x-message-ttl` on queue declarations |
| 9 | No connection pool | Single `amqp.Connection` shared across goroutines | Use connection + channel pooling for production load |
| 10 | `bread-to-make` queue | Published but never consumed | Determine intended use or remove |
