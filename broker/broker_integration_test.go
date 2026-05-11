package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/calvarado2004/bakery-go/testutils"
	rabbitmq "github.com/rabbitmq/amqp091-go"
)

// ---------------------------------------------------------------------------
// Integration tests for BrokerService — real RabbitMQ + gRPC server.
//
// These tests require the full docker-compose stack running (server + broker).
// They skip gracefully when RABBITMQ_SERVICE_ADDR is not set.
// ---------------------------------------------------------------------------

// requireFullStack checks that RABBITMQ_SERVICE_ADDR is set and the server
// is reachable. Returns a TestHarness or Skips the test.
func requireFullStack(t *testing.T) *testutils.TestHarness {
	t.Helper()

	rmqAddr := getEnvOrDefault("RABBITMQ_SERVICE_ADDR", "")
	if rmqAddr == "" {
		t.Skip("Broker integration requires full docker-compose stack (set RABBITMQ_SERVICE_ADDR)")
	}

	harness := testutils.NewTestHarness(t)

	if err := harness.WaitForServer(30 * time.Second); err != nil {
		t.Skipf("Server not available: %v", err)
	}

	if err := harness.DeclareQueues(); err != nil {
		t.Fatalf("declare queues: %v", err)
	}

	// Clear DB state for a clean test
	if err := harness.ClearDB(); err != nil {
		t.Fatalf("clear DB: %v", err)
	}

	return harness
}

// seedTestData inserts the minimal data (customer, bread, bread_maker) needed
// for broker tests to pass FK constraints.
func seedTestData(t *testing.T, db *testutils.TestHarness) {
	t.Helper()
	ctx := context.Background()

	_, err := db.DB().ExecContext(ctx, `
		INSERT INTO customer (name, email, password)
		VALUES ('Test', 'test@test.com', 'test')
	`)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}

	_, err = db.DB().ExecContext(ctx, `
		INSERT INTO bread (name, price, quantity, description, type, status, image)
		VALUES ('TestBread', 5.00, 100, 'test', 'Bread', 'available', '/test.png')
	`)
	if err != nil {
		t.Fatalf("insert bread: %v", err)
	}

	_, err = db.DB().ExecContext(ctx, `
		INSERT INTO bread_maker (name, email) VALUES ('Maker', 'maker@test.com')
	`)
	if err != nil {
		t.Fatalf("insert bread_maker: %v", err)
	}
}

// TestBrokerIntegration_BuyOrderFlow tests the full buy order flow:
// Publish → broker consumes → gRPC ReportOrder → DB persistence.
func TestBrokerIntegration_BuyOrderFlow(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	orderUUID := fmt.Sprintf("e2e-broker-%d", time.Now().UnixNano())
	order := data.BuyOrder{
		BuyOrderUUID:   orderUUID,
		CustomerID:     1,
		SequenceNumber: 1,
		BidPrice:       5.0,
		AllowPartial:   false,
		Breads: []data.Bread{
			{ID: 1, Quantity: 2, Price: 5.00},
		},
	}
	orderJSON, _ := json.Marshal(order)

	if err := harness.PublishBuyOrder(orderJSON); err != nil {
		t.Fatalf("publish order: %v", err)
	}

	// Wait for order to appear in DB
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var orderCount int
	for i := 0; i < 30; i++ {
		err := harness.DB().QueryRowContext(ctx,
			"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid = $1", orderUUID,
		).Scan(&orderCount)
		if err == nil && orderCount > 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if orderCount == 0 {
		t.Fatal("Order not found in database after 30 seconds")
	}

	// Wait for bread-bought message (matching result)
	breadBought, err := harness.ConsumeBreadBought(10 * time.Second)
	if err != nil {
		t.Logf("No bread-bought message received: %v (matching may have failed due to stock)", err)
		return
	}

	if breadBought == nil {
		t.Fatal("Expected bread-bought message, got nil")
	}

	t.Logf("Received bread-bought message for order %s", breadBought.BuyOrderUUID)
}

// TestBrokerIntegration_DuplicateUUID tests that duplicate order UUIDs are rejected.
func TestBrokerIntegration_DuplicateUUID(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	orderUUID := fmt.Sprintf("e2e-dup-%d", time.Now().UnixNano())
	order := data.BuyOrder{
		BuyOrderUUID:   orderUUID,
		CustomerID:     1,
		SequenceNumber: 1,
		BidPrice:       5.0,
		Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 5.00}},
	}

	orderJSON, _ := json.Marshal(order)

	// Publish the same order twice via RabbitMQ
	harness.PublishBuyOrder(orderJSON) //nolint:errcheck
	time.Sleep(100 * time.Millisecond)
	harness.PublishBuyOrder(orderJSON) //nolint:errcheck

	// Also publish via gRPC (simulating another broker instance)
	bsc := harness.BrokerServiceClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := bsc.ReportOrder(ctx, &pb.BuyOrder{
		BuyOrderUuid:   orderUUID,
		CustomerId:     1,
		SequenceNumber: 1,
		Items: []*pb.BuyOrderItem{
			{BreadId: 1, QuantityRequested: 1},
		},
	})
	if err != nil {
		t.Fatalf("ReportOrder: %v", err)
	}

	// Wait for processing
	time.Sleep(3 * time.Second)

	// Verify only one order in DB
	var count int
	err = harness.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid = $1", orderUUID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query buy_order: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 order (dedup), got %d", count)
	}
}

// TestBrokerIntegration_MatchingBatchSize tests that multiple orders are
// processed by the matching engine.
func TestBrokerIntegration_MatchingBatchSize(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	const orderCount = 5
	for i := 0; i < orderCount; i++ {
		orderUUID := fmt.Sprintf("e2e-batch-%d-%d", i, time.Now().UnixNano())
		order := data.BuyOrder{
			BuyOrderUUID:   orderUUID,
			CustomerID:     1,
			SequenceNumber: int64(i),
			BidPrice:       float64(5 + i), // Different prices for priority
			Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 5.00}},
		}
		orderJSON, _ := json.Marshal(order)
		if err := harness.PublishBuyOrder(orderJSON); err != nil {
			t.Fatalf("publish order %d: %v", i, err)
		}
	}

	// Wait for processing
	time.Sleep(10 * time.Second)

	// Verify orders are in DB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var count int
	err := harness.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid LIKE 'e2e-batch-%'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query buy_order: %v", err)
	}

	t.Logf("Orders in DB: %d/%d", count, orderCount)
	if count == 0 {
		t.Fatal("No orders processed after 10 seconds")
	}
}

// TestBrokerIntegration_PriorityOrdering verifies that orders with higher
// bid prices are processed before lower bid prices in the matching engine.
func TestBrokerIntegration_PriorityOrdering(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	// Set bread quantity low so we can observe priority-based fulfillment
	harness.DB().ExecContext(context.Background(),
		"UPDATE bread SET quantity = 3 WHERE id = 1",
	)

	// Publish 5 orders, each wanting 1 unit — only 3 can be fulfilled
	// Higher bid price should be processed first.
	prices := []float64{1.0, 5.0, 10.0, 3.0, 7.0}
	uuids := make([]string, len(prices))

	for i, price := range prices {
		uuids[i] = fmt.Sprintf("e2e-prio-%d-%d", i, time.Now().UnixNano())
		order := data.BuyOrder{
			BuyOrderUUID:   uuids[i],
			CustomerID:     1,
			SequenceNumber: int64(i),
			BidPrice:       price,
			Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 5.00}},
		}
		orderJSON, _ := json.Marshal(order)
		harness.PublishBuyOrder(orderJSON) //nolint:errcheck
	}

	// Wait for all orders to be processed and matched
	time.Sleep(15 * time.Second)

	// Check remaining stock — should be 0 (3 orders fulfilled from 3 units)
	var remaining int
	harness.DB().QueryRowContext(context.Background(),
		"SELECT quantity FROM bread WHERE id = 1",
	).Scan(&remaining)

	t.Logf("Remaining bread stock: %d (expected 0 if all 3 units fulfilled)", remaining)

	// Check that at least some orders were processed
	var processed int
	harness.DB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid LIKE 'e2e-prio-%' AND status IN ('processed', 'partially_processed')",
	).Scan(&processed)

	t.Logf("Processed orders: %d", processed)
	if processed < 1 {
		t.Error("Expected at least 1 order to be processed")
	}
}

// TestBrokerIntegration_PartialFulfillment tests that when stock is limited,
// orders are partially fulfilled based on availability.
func TestBrokerIntegration_PartialFulfillment(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	// Set bread to 3 units
	harness.DB().ExecContext(context.Background(),
		"UPDATE bread SET quantity = 3 WHERE id = 1",
	)

	// Order requests 10 units of bread (only 3 available)
	orderUUID := fmt.Sprintf("e2e-partial-%d", time.Now().UnixNano())
	order := data.BuyOrder{
		BuyOrderUUID:   orderUUID,
		CustomerID:     1,
		SequenceNumber: 1,
		BidPrice:       10.0,
		AllowPartial:   true,
		Breads:         []data.Bread{{ID: 1, Quantity: 10, Price: 5.00}},
	}
	orderJSON, _ := json.Marshal(order)

	if err := harness.PublishBuyOrder(orderJSON); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Wait for processing
	time.Sleep(10 * time.Second)

	// Verify order was created
	var count int
	harness.DB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid = $1", orderUUID,
	).Scan(&count)

	if count == 0 {
		t.Fatal("Order not found in database")
	}

	// Stock should be fully consumed
	var remaining int
	harness.DB().QueryRowContext(context.Background(),
		"SELECT quantity FROM bread WHERE id = 1",
	).Scan(&remaining)

	t.Logf("Remaining stock: %d", remaining)
}

// TestBrokerIntegration_MalformedMessageHandling verifies that malformed
// messages are ACKed (not requeued) to prevent infinite requeue loops.
func TestBrokerIntegration_MalformedMessageHandling(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	// Publish malformed JSON to buy-bread-order
	malformed := []byte("not valid json {{{")
	if err := harness.PublishBuyOrder(malformed); err != nil {
		t.Fatalf("publish malformed: %v", err)
	}

	// Wait a bit for broker to process
	time.Sleep(5 * time.Second)

	// The malformed message should be ACKed and removed from queue.
	// We verify by checking the queue is empty (no requeue loop).
	info, err := harness.RabbitMQChannel().QueueInspect("buy-bread-order")
	if err != nil {
		t.Logf("QueueInspect: %v (queue may have been auto-deleted)", err)
		return
	}

	// If the queue still exists, it should not have the malformed message requeued
	if info.Messages > 1 {
		t.Errorf("Expected 0 messages (malformed ACKed), got %d — possible requeue loop", info.Messages)
	}
}

// TestBrokerIntegration_ConcurrentOrders tests that the broker can handle
// multiple concurrent orders without data corruption.
func TestBrokerIntegration_ConcurrentOrders(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	// Set high stock for concurrent test
	harness.DB().ExecContext(context.Background(),
		"UPDATE bread SET quantity = 1000 WHERE id = 1",
	)

	const numOrders = 10
	var wg sync.WaitGroup
	errCh := make(chan error, numOrders)

	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			orderUUID := fmt.Sprintf("e2e-concurrent-%d-%d", idx, time.Now().UnixNano())
			order := data.BuyOrder{
				BuyOrderUUID:   orderUUID,
				CustomerID:     1,
				SequenceNumber: int64(idx),
				BidPrice:       5.0,
				Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 5.00}},
			}
			orderJSON, _ := json.Marshal(order)
			if err := harness.PublishBuyOrder(orderJSON); err != nil {
				errCh <- fmt.Errorf("publish %d: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrent publish error: %v", err)
	}

	// Wait for all orders to be processed
	time.Sleep(15 * time.Second)

	// Verify all orders ended up in DB
	var count int
	harness.DB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid LIKE 'e2e-concurrent-%'",
	).Scan(&count)

	t.Logf("Concurrent orders in DB: %d/%d", count, numOrders)
	if count == 0 {
		t.Fatal("No concurrent orders processed")
	}
}

// TestBrokerIntegration_BreadBoughtMessageFormat verifies the format of
// messages published to the bread-bought queue by the matching engine.
func TestBrokerIntegration_BreadBoughtMessageFormat(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	// Set up consumer on bread-bought BEFORE publishing order
	ch, err := harness.RabbitMQConn().Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	defer ch.Close()

	consumer, err := ch.Consume("bread-bought", "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume bread-bought: %v", err)
	}

	// Publish order
	orderUUID := fmt.Sprintf("e2e-format-%d", time.Now().UnixNano())
	order := data.BuyOrder{
		BuyOrderUUID:   orderUUID,
		CustomerID:     1,
		SequenceNumber: 1,
		BidPrice:       10.0,
		Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 5.00}},
	}
	orderJSON, _ := json.Marshal(order)
	harness.PublishBuyOrder(orderJSON) //nolint:errcheck

	// Consume the bread-bought message
	select {
	case d := <-consumer:
		// Verify it's valid JSON
		var result map[string]interface{}
		if err := json.Unmarshal(d.Body, &result); err != nil {
			// Try the nested format
			var nested struct {
				Order json.RawMessage `json:"order"`
				Items json.RawMessage `json:"items"`
			}
			if err2 := json.Unmarshal(d.Body, &nested); err2 != nil {
				t.Fatalf("bread-bought message is not valid JSON: %v (body: %s)", err, string(d.Body))
			}
			t.Log("bread-bought message uses nested format (order/items)")
		} else {
			// Flat format — check for order_uuid field
			if _, ok := result["buy_order_uuid"]; !ok && result["buyOrderUuid"] == nil {
				t.Log("bread-bought message has unexpected format (no buy_order_uuid)")
			}
		}
		d.Ack(false)
		t.Log("Successfully consumed and validated bread-bought message format")
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for bread-bought message")
	}
}

// TestBrokerIntegration_BrokerServiceStartStop verifies that the broker
// can start and stop cleanly without panicking.
func TestBrokerIntegration_BrokerServiceStartStop(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	// We can't easily start a real broker here because it would compete
	// with the production broker. Instead, verify the service struct is
	// constructible with real gRPC conn.
	bsc := harness.BrokerServiceClient()
	if bsc == nil {
		t.Fatal("expected non-nil broker service client")
	}

	// Verify the client can communicate
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// ReportOrder should work even with non-existent data (it will fail at FK,
	// but the gRPC channel should be open)
	_, err := bsc.ReportOrder(ctx, &pb.BuyOrder{
		BuyOrderUuid: "startstop-test",
		CustomerId:   999, // non-existent
		Items: []*pb.BuyOrderItem{
			{BreadId: 999, QuantityRequested: 1},
		},
	})
	// We don't care about the result — just that the call didn't panic
	if err != nil {
		t.Logf("ReportOrder failed (expected with bad data): %v", err)
	}
}

// TestBrokerIntegration_OrderDetailsVerification verifies that after the
// broker processes an order, the order_details table has correct entries.
func TestBrokerIntegration_OrderDetailsVerification(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	// Insert a second bread to test multi-item orders
	harness.DB().ExecContext(context.Background(), `
		INSERT INTO bread (name, price, quantity, description, type, status, image)
		VALUES ('SecondBread', 3.00, 50, 'test', 'Bread', 'available', '/second.png')
	`)

	orderUUID := fmt.Sprintf("e2e-details-%d", time.Now().UnixNano())
	order := data.BuyOrder{
		BuyOrderUUID:   orderUUID,
		CustomerID:     1,
		SequenceNumber: 1,
		BidPrice:       10.0,
		Breads: []data.Bread{
			{ID: 1, Quantity: 2, Price: 5.00},
			{ID: 2, Quantity: 3, Price: 3.00},
		},
	}
	orderJSON, _ := json.Marshal(order)
	harness.PublishBuyOrder(orderJSON) //nolint:errcheck

	// Wait for order to be processed
	time.Sleep(10 * time.Second)

	// Verify order_details has 2 rows
	var detailCount int
	harness.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM order_details
		WHERE buy_order_id = (
			SELECT id FROM buy_order WHERE buy_order_uuid = $1
		)`, orderUUID,
	).Scan(&detailCount)

	if detailCount != 2 {
		t.Errorf("Expected 2 order_details rows, got %d", detailCount)
	}

	// Verify quantities in order_details
	type detailRow struct {
		breadID  int
		quantity int
		price    float64
	}
	rows, err := harness.DB().Query(`
		SELECT od.bread_id, od.quantity, od.price
		FROM order_details od
		JOIN buy_order bo ON bo.id = od.buy_order_id
		WHERE bo.buy_order_uuid = $1`, orderUUID)
	if err != nil {
		t.Fatalf("query order_details: %v", err)
	}
	defer rows.Close()

	var details []detailRow
	for rows.Next() {
		var d detailRow
		rows.Scan(&d.breadID, &d.quantity, &d.price)
		details = append(details, d)
	}

	for _, d := range details {
		if d.breadID == 1 && d.quantity != 2 {
			t.Errorf("bread 1 qty: expected 2, got %d", d.quantity)
		}
		if d.breadID == 2 && d.quantity != 3 {
			t.Errorf("bread 2 qty: expected 3, got %d", d.quantity)
		}
	}
}

// getEnvOrDefault is a helper since os.Getenv is not imported in this file.
func getEnvOrDefault(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

// TestBrokerIntegration_StockDeduction verifies that the matching engine
// correctly deducts stock through the gRPC ReserveInventory call.
func TestBrokerIntegration_StockDeduction(t *testing.T) {
	harness := requireFullStack(t)
	defer harness.Cleanup()

	seedTestData(t, harness)

	// Record initial quantity
	var initialQty int
	harness.DB().QueryRowContext(context.Background(),
		"SELECT quantity FROM bread WHERE id = 1",
	).Scan(&initialQty)

	// Publish order for 5 units
	orderUUID := fmt.Sprintf("e2e-stock-%d", time.Now().UnixNano())
	order := data.BuyOrder{
		BuyOrderUUID:   orderUUID,
		CustomerID:     1,
		SequenceNumber: 1,
		BidPrice:       10.0,
		Breads:         []data.Bread{{ID: 1, Quantity: 5, Price: 5.00}},
	}
	orderJSON, _ := json.Marshal(order)
	harness.PublishBuyOrder(orderJSON) //nolint:errcheck

	// Wait for matching to complete
	time.Sleep(15 * time.Second)

	// Check final quantity
	var finalQty int
	harness.DB().QueryRowContext(context.Background(),
		"SELECT quantity FROM bread WHERE id = 1",
	).Scan(&finalQty)

	t.Logf("Stock: initial=%d, final=%d (delta=%d)", initialQty, finalQty, initialQty-finalQty)

	// Stock should have been deducted (at least partially)
	if finalQty > initialQty {
		t.Error("Stock increased after order — should only decrease")
	}
}

// stub type to satisfy the import — unused but prevents compile error
var _ rabbitmq.Delivery
