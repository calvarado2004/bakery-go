# Buyer Onboarding Guide

> **Protocol:** gRPC over TLS (development: insecure)
> **Contract:** [`proto/bread.proto`](../proto/bread.proto)
> **Rate Limit:** 10 requests/second per customer (burst: 20)

---

## 1. Architecture Overview

Buyers are **external clients** that communicate with the bakery platform exclusively via gRPC. There is no direct database access.

```
┌──────────┐  gRPC + JWT  ┌──────────────┐
│  Buyer   │ ───────────▶ │  SERVER      │
│ (client) │ ◀─────────── │  (gRPC + DB) │
└──────────┘              └──────────────┘
                                    │
                              RabbitMQ (internal)
                                    │
                                    ▼
                              ┌──────────────┐
                              │    Broker    │  ← matching engine
                              └──────────────┘
```

**Key boundary rules:**
- Buyers connect **only** to the server's gRPC port (default `:50051`)
- Buyers authenticate with a **JWT Bearer token** (obtained via `AuthService.CustomerLogin`)
- Buyers **never** touch PostgreSQL directly — no DSN, no SQL
- All orders go through the broker's matching engine (priority + partial fulfillment)

---

## 2. Authentication Flow

### 2.1 Register (optional — admin must create account first)

Admins create customer accounts via the AdminService. External buyers contact the platform operator for account creation.

### 2.2 Login

**RPC:** `AuthService.CustomerLogin`

```protobuf
rpc CustomerLogin(CustomerLoginRequest) returns (CustomerLoginResponse);

message CustomerLoginRequest {
    string email = 1;
    string password = 2;
}

message CustomerLoginResponse {
    bool success = 1;
    string message = 2;
    string token = 3;          // JWT Bearer token (24h validity)
    Customer customer = 4;     // customer profile
}
```

**Example (Go):**

```go
conn, err := grpc.Dial("server:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil { log.Fatal(err) }

authClient := pb.NewAuthServiceClient(conn)
resp, err := authClient.CustomerLogin(ctx, &pb.CustomerLoginRequest{
    Email:    "buyer@example.com",
    Password: "correct-password",
})
if err != nil || !resp.Success {
    log.Fatal("login failed")
}

// The JWT token goes in the "authorization" metadata for all subsequent calls.
// metadata.Pairs("authorization", "Bearer " + resp.Token)
```

### 2.3 Token Validation

**RPC:** `AuthService.ValidateToken`

```protobuf
rpc ValidateToken(ValidateTokenRequest) returns (ValidateTokenResponse);

message ValidateTokenRequest {
    string token = 1;  // JWT to validate
}

message ValidateTokenResponse {
    bool valid = 1;
    string userId = 2;
    string userType = 3;  // "customer" or "admin"
}
```

### 2.4 Token Properties

| Property | Value |
|----------|-------|
| Algorithm | HMAC-SHA256 (HS256) |
| Issuer | `bakery-go` |
| Validity | 24 hours |
| Claims | `user_id` (int), `username` (email), `user_type` ("customer") |

---

## 3. Placing Orders

### 3.1 BuyBread (Synchronous — returns immediately)

**RPC:** `BuyBreadServer.BuyBread`

This is a **fire-and-forget** call. The server publishes the order to RabbitMQ and returns immediately. The actual matching happens asynchronously in the broker.

```protobuf
rpc BuyBread(BreadRequest) returns (BreadResponse);

message BreadRequest {
    BreadList breads = 1;           // items to buy
    string buy_order_uuid = 2;      // optional; generates UUID if empty (idempotency key)
    BuyOrderPreferences preferences = 5;  // matching engine rules
}

message BuyOrderPreferences {
    double bidPrice = 1;              // price per unit (0 = market price)
    bool allowPartial = 2;            // allow partial fulfillment
    bool skipUnavailableItems = 3;    // skip out-of-stock items
}

message BreadResponse {
    string message = 1;
    int32 buy_order_id = 5;
    string buy_order_uuid = 7;
}
```

**Required gRPC metadata:**
| Key | Value | Required |
|-----|-------|----------|
| `authorization` | `Bearer <jwt_token>` | Yes (for RBAC) |
| `customer_id` | `<numeric_id>` | Yes (identifies the buyer) |

**Example (Go):**

```go
md := metadata.Pairs(
    "authorization", "Bearer " + resp.Token,
    "customer_id", "42",  // extracted from JWT claims
)
ctx = metadata.NewOutgoingContext(context.Background(), md)

buyReq := &pb.BreadRequest{
    Breads: &pb.BreadList{
        Breads: []*pb.Bread{
            {Id: 1, Quantity: 5},
            {Id: 3, Quantity: 2},
        },
    },
    Preferences: &pb.BuyOrderPreferences{
        AllowPartial:        true,
        SkipUnavailableItems: true,
        BidPrice:            3.50,
    },
}

reply, err := buyClient.BuyBread(ctx, buyReq)
if err != nil {
    log.Printf("order failed: %v", err)
}
// reply.BuyOrderId and reply.BuyOrderUuid are set immediately
```

### 3.2 BuyBreadStream (Async — receives matching results)

**RPC:** `BuyBreadServer.BuyBreadStream`

A server-streaming RPC that delivers matching results as they become available. The buyer calls `BuyBread` first (fire-and-forget), then starts a stream to receive results.

```protobuf
rpc BuyBreadStream(BreadRequest) returns (stream BreadResponse);
```

The stream receives `BreadResponse` messages as the broker processes orders:

```go
stream, err := buyClient.BuyBreadStream(ctx, buyReq)
if err != nil { log.Fatal(err) }

for {
    resp, err := stream.Recv()
    if err == io.EOF { break }
    if err != nil { log.Fatal(err) }
    // resp.Breads contains fulfillment results
    // resp.BuyOrderId identifies the order
    // resp.Message contains status: "processed", "partially_processed", "rejected"
}
```

### 3.3 Idempotency

Orders are deduplicated by `buy_order_uuid`. If you send the same UUID twice, the second request returns the original order ID without creating a duplicate.

**To retry safely:** Always generate a UUID on the client side and pass it as `buy_order_uuid`. If the gRPC call times out, retry with the same UUID.

---

## 4. Order Matching Engine

### 4.1 How Orders Are Processed

1. Your order is published to the `buy-bread-order` queue
2. The broker batches orders for 500ms (or 100 orders, whichever comes first)
3. Orders are sorted by `bidPrice` DESC, then `sequenceNumber` ASC
4. Orders are fulfilled in priority order:
   - **Full fulfillment:** All items available → status `processed`
   - **Partial fulfillment:** Some items available → status `partially_processed` (if `allowPartial=true`)
   - **Skip:** Item out of stock, `skipUnavailableItems=true` → item status `skipped`
   - **Reject:** Item out of stock, `skipUnavailableItems=false` → status `rejected`

### 4.2 Price Discovery

| `bidPrice` | Behavior |
|------------|----------|
| 0 (default) | Market price — fills at lowest winning bid in the batch |
| > 0 | Limit order — fills only if `bidPrice >= market price`. Fills at `bidPrice` |

### 4.3 Fulfillment Rules

| `allowPartial` | `skipUnavailableItems` | Result |
|----------------|----------------------|--------|
| `false` | `false` | All items must be available, or entire order is rejected |
| `true` | `false` | Partial fill allowed; missing items cause rejection |
| `false` | `true` | Missing items are skipped; other items fulfilled |
| `true` | `true` | Partial fill + missing items skipped (most lenient) |

---

## 5. Querying Orders

### 5.1 Get My Orders

**RPC:** `BuyOrderService.BuyOrder`

```protobuf
rpc BuyOrder(BuyOrderRequest) returns (BuyOrderResponse);

message BuyOrderRequest {
    string buy_order_uuid = 1;
}
```

### 5.2 Stream Order Updates

**RPC:** `BuyOrderService.BuyOrderStream`

Streams order status changes in real-time as the broker processes them.

### 5.3 Customer Portal

**RPC:** `CustomerPortalService`

```protobuf
service CustomerPortalService {
    rpc GetMyOrders(Empty) returns (BuyOrderList);
    rpc GetMyInvoices(Empty) returns (InvoiceList);
    rpc GetMyBreadHistory(Empty) returns (BreadList);
}
```

---

## 6. Error Codes

| gRPC Status Code | Meaning | Action |
|-----------------|---------|--------|
| `Unauthenticated` | Missing or invalid JWT | Re-login, get fresh token |
| `PermissionDenied` | Wrong role (admin endpoint) | Use customer account |
| `ResourceExhausted` | Rate limit exceeded (10 req/s) | Back off and retry |
| `Internal` | Server/broker error | Retry with exponential back-off |
| `Unavailable` | Server unreachable | Retry with circuit-breaker awareness |

---

## 7. Rate Limiting

- **Default limit:** 10 requests/second per customer identity
- **Burst:** 20 requests (token bucket)
- **Per-identity:** Based on `customer_id` metadata or peer IP address
- **Exceeded response:** `codes.ResourceExhausted` ("rate limit exceeded")

**Recommended back-off strategy:**
```go
// Exponential back-off with jitter
delay := 100 * time.Millisecond
for attempt := 0; attempt < 5; attempt++ {
    resp, err := client.BuyBread(ctx, req)
    if err == nil { break }
    time.Sleep(delay + time.Duration(rand.Intn(50)) * time.Millisecond)
    delay *= 2
}
```

---

## 8. Connection Management

### 8.1 Shared Connection Pool

Buyers should maintain a **single shared gRPC connection** for all calls. Creating a new connection per request causes resource exhaustion.

```go
// Good: shared connection
conn, _ := grpc.Dial("server:50051", opts)
buyClient := pb.NewBuyBreadServer(conn)
orderClient := pb.NewBuyOrderServiceClient(conn)

// Bad: new connection per call
conn, _ := grpc.Dial("server:50051", opts)  // every request
```

### 8.2 Connection Options

```go
opts := []grpc.DialOption{
    grpc.WithTransportCredentials(insecure.NewCredentials()), // TLS in production
    grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4*1024*1024)),
    grpc.WithKeepaliveParams(keepalive.ClientParameters{
        Time:                30 * time.Second,
        Timeout:             10 * time.Second,
        PermitWithoutStream: true,
    }),
}
```

---

## 9. Quick Start Checklist

- [ ] Obtain customer credentials (email + password) from platform operator
- [ ] Login via `AuthService.CustomerLogin` to get JWT token
- [ ] Store JWT token; refresh before 24h expiry
- [ ] Create shared gRPC connection to server
- [ ] Attach `authorization: Bearer <token>` and `customer_id: <id>` metadata to every call
- [ ] Generate UUIDs for idempotency on order placement
- [ ] Implement exponential back-off for retries
- [ ] Monitor for `ResourceExhausted` errors (rate limit)
- [ ] Use `BuyBread` (fire-and-forget) + `BuyBreadStream` (async results) pattern

---

## Appendix: Environment Variables (Buyer Client)

| Variable | Description | Example |
|----------|-------------|---------|
| `BAKERY_SERVICE_ADDR` | Server gRPC address | `server:50051` |
| `JWT_TOKEN` | Bearer token from login | `eyJhbGc...` |
| `CUSTOMER_ID` | Numeric customer ID | `42` |

No `DSN` variable — buyers have no database access.
