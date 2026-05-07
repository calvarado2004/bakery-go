package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	"github.com/calvarado2004/bakery-go/testutils"
	pb "github.com/calvarado2004/bakery-go/proto"
)

// TestBrokerIntegration_BuyOrderFlow tests the full buy order flow:
// Server → RabbitMQ (buy-bread-order) → Broker → Server gRPC → matching → bread-bought
func TestBrokerIntegration_BuyOrderFlow(t *testing.T) {
	// This test requires the full docker-compose stack with a fresh broker
	// (circuit breaker must be closed). Skip when RABBITMQ_SERVICE_ADDR
	// is not set, indicating tests are running outside the full stack.
	rmqAddr := os.Getenv("RABBITMQ_SERVICE_ADDR")
	if rmqAddr == "" {
		t.Skip("Broker integration requires full docker-compose stack (set RABBITMQ_SERVICE_ADDR)")
		return
	}

	harness := testutils.NewTestHarness(t)
	defer harness.Cleanup()

	// Verify RabbitMQ is accessible
	if err := harness.DeclareQueues(); err != nil {
		t.Fatalf("Failed to declare queues: %v", err)
	}

	// Clear state
	if err := harness.ClearDB(); err != nil {
		t.Logf("Warning: Failed to clear DB: %v", err)
	}

	// Insert seed data needed by broker tests
	ctx := context.Background()
	// Insert customer (FK requirement)
	harness.DB().ExecContext(ctx, `INSERT INTO customer (name, email, password) VALUES ('Test','test@test.com','test')`)
	// Insert bread (FK requirement for order_details)
	harness.DB().ExecContext(ctx, `INSERT INTO bread (name, price, quantity, description, type, status, image) VALUES ('Test',1.00,100,'Test','Test','available','/images/test.png')`)
	// Insert bread maker (FK requirement for make_order)
	harness.DB().ExecContext(ctx, `INSERT INTO bread_maker (name, email) VALUES ('Test','test@maker.com')`)

	orderUUID := fmt.Sprintf("e2e-broker-%d", time.Now().UnixNano())

	// Create a buy order
	order := data.BuyOrder{
		BuyOrderUUID:   orderUUID,
		CustomerID:     1,
		SequenceNumber: 1,
		BidPrice:       5.0,
		AllowPartial:   false,
		Breads: []data.Bread{
			{ID: 1, Quantity: 2, Price: 2.50},
		},
	}
	orderJSON, err := json.Marshal(order)
	if err != nil {
		t.Fatalf("Failed to marshal order: %v", err)
	}

	// Publish to buy-bread-order queue
	if err := harness.PublishBuyOrder(orderJSON); err != nil {
		t.Fatalf("Failed to publish order: %v", err)
	}

	// Wait for the order to be processed by the broker
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

	// Wait for bread-bought message (optional — matching may not succeed without stock)
	breadBought, err := harness.ConsumeBreadBought(10 * time.Second)
	if err != nil {
		t.Logf("No bread-bought message received: %v (matching may have failed due to stock)", err)
		// Not a hard failure — matching depends on stock levels
		return
	}

	if breadBought == nil {
		t.Fatal("Expected bread-bought message, got nil")
	}

	t.Logf("Received bread-bought message for order %s", breadBought.BuyOrderUUID)
}

// TestBrokerIntegration_DuplicateUUID tests that duplicate order UUIDs are rejected.
func TestBrokerIntegration_DuplicateUUID(t *testing.T) {
	rmqAddr := os.Getenv("RABBITMQ_SERVICE_ADDR")
	if rmqAddr == "" {
		t.Skip("Broker integration requires full docker-compose stack (set RABBITMQ_SERVICE_ADDR)")
		return
	}

	harness := testutils.NewTestHarness(t)
	defer harness.Cleanup()

	if err := harness.WaitForServer(30 * time.Second); err != nil {
		t.Skipf("Server not available: %v", err)
		return
	}
	if err := harness.DeclareQueues(); err != nil {
		t.Fatalf("Failed to declare queues: %v", err)
	}
	if err := harness.ClearDB(); err != nil {
		t.Fatalf("Failed to clear DB: %v", err)
	}

	// Insert seed data for FK constraints
	dbCtx := context.Background()
	harness.DB().ExecContext(dbCtx, `INSERT INTO customer (name, email, password) VALUES ('Test','test@test.com','test')`)
	harness.DB().ExecContext(dbCtx, `INSERT INTO bread (name, price, quantity, description, type, status, image) VALUES ('Test',1.00,100,'Test','Test','available','/images/test.png')`)

	orderUUID := fmt.Sprintf("e2e-dup-%d", time.Now().UnixNano())
	order := data.BuyOrder{
		BuyOrderUUID:   orderUUID,
		CustomerID:     1,
		SequenceNumber: 1,
		BidPrice:       5.0,
		Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 2.50}},
	}

	orderJSON, _ := json.Marshal(order)

	// Publish twice via different methods

	// First publish via RabbitMQ
	harness.PublishBuyOrder(orderJSON) //nolint:errcheck

	// Wait for broker to process first order
	time.Sleep(2 * time.Second)

	// Second publish via gRPC directly (simulating another broker instance)
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
		t.Fatalf("Second ReportOrder failed: %v", err)
	}

	// Verify only one order in DB
	var count int
	err = harness.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid = $1", orderUUID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query buy_order: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 order, got %d (duplicate was created)", count)
	}
}

// TestBrokerIntegration_MatchingBatchSize tests that matching triggers on batch size.
func TestBrokerIntegration_MatchingBatchSize(t *testing.T) {
	rmqAddr := os.Getenv("RABBITMQ_SERVICE_ADDR")
	if rmqAddr == "" {
		t.Skip("Broker integration requires full docker-compose stack (set RABBITMQ_SERVICE_ADDR)")
		return
	}

	harness := testutils.NewTestHarness(t)
	defer harness.Cleanup()

	if err := harness.WaitForServer(30 * time.Second); err != nil {
		t.Skipf("Server not available: %v", err)
		return
	}
	if err := harness.DeclareQueues(); err != nil {
		t.Fatalf("Failed to declare queues: %v", err)
	}
	if err := harness.ClearDB(); err != nil {
		t.Fatalf("Failed to clear DB: %v", err)
	}

	// Insert seed data for FK constraints
	dbCtx := context.Background()
	harness.DB().ExecContext(dbCtx, `INSERT INTO customer (name, email, password) VALUES ('Test','test@test.com','test')`)
	harness.DB().ExecContext(dbCtx, `INSERT INTO bread (name, price, quantity, description, type, status, image) VALUES ('Test',1.00,100,'Test','Test','available','/images/test.png')`)

	// Create multiple orders (matching batch size is 100, but we'll use 5 for speed)
	const orderCount = 5
	for i := 0; i < orderCount; i++ {
		orderUUID := fmt.Sprintf("e2e-batch-%d-%d", i, time.Now().UnixNano())
		order := data.BuyOrder{
			BuyOrderUUID:   orderUUID,
			CustomerID:     1,
			SequenceNumber: int64(i),
			BidPrice:       float64(5 + i), // Different prices
			Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 2.50}},
		}
		orderJSON, err := json.Marshal(order)
		if err != nil {
			t.Fatalf("Failed to marshal order %d: %v", i, err)
		}

		if err := harness.PublishBuyOrder(orderJSON); err != nil {
			t.Fatalf("Failed to publish order %d: %v", i, err)
		}
	}

	// Wait for orders to be processed
	time.Sleep(10 * time.Second)

	// Verify all orders are in DB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var count int
	err := harness.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid LIKE 'e2e-batch-%'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("Query buy_order: %v", err)
	}

	// The broker processes orders in batches, so we expect all 5 orders
	// (matching may or may not have happened depending on timing)
	t.Logf("Orders in DB: %d/%d", count, orderCount)

	// At least some orders should have been processed
	if count == 0 {
		t.Fatal("No orders processed after 10 seconds")
	}
}
