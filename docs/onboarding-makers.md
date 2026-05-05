# Maker Onboarding Guide

> **Protocols:** gRPC + RabbitMQ (AMQP 0.9.1)
> **Contract:** [`proto/bread.proto`](../proto/bread.proto)
> **Queue Ownership:** Makers declare and own `make-bread-order`

---

## 1. Architecture Overview

Makers are **external providers** that produce bread for the platform. They communicate via two channels:

1. **gRPC** — for baking notifications and inventory updates
2. **RabbitMQ** — for consuming production orders and publishing completion confirmations

```
┌──────────┐  gRPC          ┌──────────────┐
│  Maker   │ ── BakeBread ─▶│  SERVER      │
│ (provider)│ ◀─ Response  │  (gRPC + DB) │
└────┬─────┘               └──────────────┘
     │
     │ AMQP 0.9.1
     ▼
┌─────────────────────────────────┐
│  RabbitMQ "make-bread-order"    │  ← production orders
│  (declared & owned by makers)   │
└────────┬────────────────────────┘
         │ consume
         ▼
  ┌──────────────┐
  │  Maker       │  ← processes orders, bakes bread
  │  Consumer    │
  └──────┬───────┘
         │
         │ publish
         ▼
┌─────────────────────────────────┐
│  RabbitMQ "bread-made"          │  ← completion confirmations
│  (declared & owned by server)   │
└────────┬────────────────────────┘
         │ consume
         ▼
┌──────────────┐
│  SERVER      │  ← adjusts inventory on confirmation
└──────────────┘
```

**Key boundary rules:**
- Makers **own** the `make-bread-order` queue (declare it on startup)
- Makers **consume** from `make-bread-order` and **publish** to `bread-made`
- Makers **never** touch PostgreSQL directly
- Makers **must** implement reconnection logic and fault isolation
- Multiple makers can consume from the same queue (fan-out via separate consumers)

---

## 2. Queue Setup

### 2.1 Declare Queues

Makers must declare their queues on startup (before consuming):

```go
import "github.com/rabbitmq/amqp091-go"

func declareQueues(conn *amqp091.Connection) error {
    ch, err := conn.Channel()
    if err != nil { return err }
    defer ch.Close()

    // make-bread-order: production orders (declared by makers)
    if _, err := ch.QueueDeclare(
        "make-bread-order",
        true,   // durable
        false,  // delete when unused
        false,  // exclusive
        false,  // no-wait
        nil,
    ); err != nil {
        return err
    }

    return nil
}
```

### 2.2 Queue Properties

| Property | `make-bread-order` | `bread-made` |
|----------|-------------------|--------------|
| **Owner** | Makers | Server |
| **Durable** | Yes | Yes |
| **Exchange** | Direct (empty string) | Direct (empty string) |
| **Routing Key** | Empty (`""`) | Empty (`""`) |
| **Content Type** | `application/json` | `application/json` |

---

## 3. Consuming Production Orders

### 3.1 Message Format

Messages published to `make-bread-order` are JSON with this structure:

```json
{
    "bread_maker": {
        "id": 1,
        "name": "Bakery Name",
        "email": "maker@example.com"
    },
    "bread_maker_id": 1,
    "breads": [
        {
            "id": 1,
            "name": "Sourdough",
            "quantity": 10,
            "price": 5.99,
            "description": "Artisan sourdough bread",
            "type": "loaf",
            "status": "available",
            "image": "/images/sourdough.jpg"
        }
    ]
}
```

### 3.2 Consumer Setup with QoS

```go
import "github.com/rabbitmq/amqp091-go"

func startConsumer(conn *amqp091.Connection, makerID int) error {
    ch, err := conn.Channel()
    if err != nil { return err }

    // Set QoS: max 5 messages in flight at a time
    if err := ch.Qos(5, 0, false); err != nil {
        return err
    }

    msgs, err := ch.Consume(
        "make-bread-order", // queue
        "",                 // consumer tag (auto-generated)
        false,              // no-local (false = can receive own messages)
        false,              // not exclusive
        false,              // no-wait
        false,              // no-ack (we ack manually)
        nil,
    )
    if err != nil { return err }

    go func() {
        for d := range msgs {
            go handleMessage(d, makerID)
        }
    }()

    return nil
}
```

### 3.3 Message Processing

```go
func handleMessage(delivery amqp091.Delivery, makerID int) {
    var order struct {
        BreadMaker   MakerInfo   `json:"bread_maker"`
        BreadMakerID int         `json:"bread_maker_id"`
        Breads       []BreadItem `json:"breads"`
    }

    if err := json.Unmarshal(delivery.Body, &order); err != nil {
        log.Warnf("bad message: %v", err)
        delivery.Nack(false, false) // discard, don't requeue
        return
    }

    // Process each bread item...
    for _, bread := range order.Breads {
        if err := bakeBread(bread, makerID); err != nil {
            log.Errorf("bake failed: %v", err)
            delivery.Nack(false, true) // requeue for retry
            return
        }
    }

    delivery.Ack(false) // success
}
```

### 3.4 Error Handling

| Error Type | Action | Reason |
|------------|--------|--------|
| JSON parse error | `Nack(false, false)` — discard | Bad message, requeueing won't help |
| Transient error (DB, network) | `Nack(false, true)` — requeue | May succeed on retry |
| Business error (invalid item) | `Nack(false, false)` — discard | Won't succeed on retry |
| Processing error | `Nack(false, true)` — requeue | Retry after back-off |

**Critical:** Never return errors from the message handler — always use `Nack`/`Ack`. A returned error kills the entire consumer.

---

## 4. Publishing Confirmations

### 4.1 Publish to `bread-made`

After baking is complete, publish a confirmation to `bread-made`:

```json
{
    "bread_maker_id": 1,
    "bread_id": 1,
    "quantity": 10,
    "status": "made"
}
```

```go
func publishConfirmation(conn *amqp091.Connection, breadID, quantity int) error {
    ch, err := conn.Channel()
    if err != nil { return err }
    defer ch.Close()

    payload, _ := json.Marshal(map[string]interface{}{
        "bread_maker_id": breadID,
        "bread_id":       breadID,
        "quantity":       quantity,
        "status":         "made",
    })

    err = ch.Publish(
        "",               // exchange
        "bread-made",     // routing key
        false,            // mandatory
        false,            // immediate
        amqp091.Publishing{
            ContentType:  "application/json",
            Body:         payload,
            DeliveryMode: amqp091.Persistent,
        },
    )
    return err
}
```

---

## 5. gRPC Interface

### 5.1 BakeBread (Notify server of new batch)

**RPC:** `MakeBread.BakeBread`

Notify the server that bread has been baked and is ready for sale.

```protobuf
rpc BakeBread(BreadRequest) returns (BreadResponse);
```

**Example (Go):**

```go
conn, _ := grpc.Dial("server:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
makeClient := pb.NewMakeBreadServer(conn)

resp, err := makeClient.BakeBread(ctx, &pb.BreadRequest{
    Breads: &pb.BreadList{
        Breads: []*pb.Bread{
            {Id: 1, Name: "Sourdough", Quantity: 10, Price: 5.99},
        },
    },
})
```

### 5.2 SendBreadToBakery (Ship bread to platform warehouse)

**RPC:** `MakeBread.SendBreadToBakery`

Notify the server that bread has been shipped to the central bakery/warehouse.

```protobuf
rpc SendBreadToBakery(BreadRequest) returns (BreadResponse);
```

### 5.3 MadeBreadStream (Stream baking progress)

**RPC:** `MakeBread.MadeBreadStream`

Server-streaming RPC for broadcasting baking progress to the platform.

```protobuf
rpc MadeBreadStream(BreadRequest) returns (stream BreadResponse);
```

---

## 6. Reconnection Logic

Makers **must** reconnect on failure. The server's `startMakersService` pattern is the recommended approach:

```go
func startMakersService(rabbitmqURL string) {
    for {
        if err := runConsumer(rabbitmqURL); err != nil {
            log.Warnf("consumer error, reconnecting in 10s: %v", err)
            time.Sleep(10 * time.Second)
            continue
        }
    }
}

func runConsumer(rabbitmqURL string) error {
    conn, err := amqp091.Dial(rabbitmqURL)
    if err != nil { return err }
    defer conn.Close()

    ch, err := conn.Channel()
    if err != nil { return err }

    if err := ch.Qos(5, 0, false); err != nil {
        return err
    }

    // ... set up consumer ...

    // Block until connection is closed (by server or network error)
    <-conn.NotifyClose(make(chan *amqp091.Error))
    return fmt.Errorf("connection closed")
}
```

---

## 7. Auto-Replenishment

The server monitors inventory and creates `pending_make_orders` for low-stock bread. Makers **should not** consume from these — they go to the `pending_make_orders` table with `source=auto`, not to the `make-bread-order` queue.

External makers only consume from `make-bread-order`, which is populated by:
- **Admin actions** via the `AdminService` (manual restock orders)
- **Maker-initiated** production scheduling

---

## 8. Fault Tolerance Checklist

- [ ] Set `channel.Qos(5, 0, false)` — limits in-flight messages to 5
- [ ] Process messages in goroutines — don't block the consumer loop
- [ ] Never `return` from the message handler — use `Nack`/`Ack`
- [ ] Implement reconnection loop with exponential back-off
- [ ] Handle `json.Unmarshal` errors by discarding (Nack + false)
- [ ] Handle transient errors by requeuing (Nack + true)
- [ ] Use `amqp091.Persistent` delivery mode for reliability
- [ ] Graceful shutdown: drain in-flight messages on SIGINT/SIGTERM

---

## 9. Quick Start Checklist

- [ ] Obtain RabbitMQ credentials and server gRPC address from platform operator
- [ ] Declare `make-bread-order` queue on startup
- [ ] Set `channel.Qos(5, 0, false)` before consuming
- [ ] Implement message processing with proper `Nack`/`Ack` handling
- [ ] Implement reconnection loop with back-off
- [ ] Publish confirmations to `bread-made` after each successful bake
- [ ] Use `BakeBread` gRPC to notify server of new batches
- [ ] Test with bad messages (ensure they're discarded, not requeued)
- [ ] Test reconnection (kill RabbitMQ, verify automatic recovery)

---

## Appendix: Message Schema Reference

### make-bread-order (input)

```go
type makeOrderMessage struct {
    BreadMaker   MakerInfo   `json:"bread_maker"`
    BreadMakerID int         `json:"bread_maker_id"`
    Breads       []breadItem `json:"breads"`
}

type breadItem struct {
    ID          int     `json:"id"`
    Name        string  `json:"name"`
    Quantity    int     `json:"quantity"`
    Price       float64 `json:"price"`
    Description string  `json:"description"`
    Type        string  `json:"type"`
    Status      string  `json:"status"`
    Image       string  `json:"image"`
}
```

### bread-made (output)

```json
{
    "bread_maker_id": <int>,
    "bread_id": <int>,
    "quantity": <int>,
    "status": "made"
}
```

---

## Environment Variables (Maker Client)

| Variable | Description | Example |
|----------|-------------|---------|
| `RABBITMQ_SERVICE_ADDR` | RabbitMQ connection string | `amqp://guest:guest@rabbitmq:5672/` |
| `BAKERY_SERVICE_ADDR` | Server gRPC address | `server:50051` |
| `MAKER_ID` | Unique maker identifier | `1` |
| `MAKER_NAME` | Human-readable maker name | `Sunrise Bakery` |
