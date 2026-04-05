package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	"github.com/calvarado2004/bakery-go/testutils"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestBroker_Integration tests the broker with real RabbitMQ and PostgreSQL
func TestBroker_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	// Get connection to RabbitMQ
	rabbitMQAddr := testutils.GetRabbitMQAddress()
	conn, err := rabbitmq.Dial(rabbitMQAddr)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer ch.Close()

	t.Run("PublishAndConsumeBuyOrder", func(t *testing.T) {
		// Get a bread item to order
		dbDSN := testutils.GetDBDSNFromT(t)
		db, err := sql.Open("pgx", dbDSN)
		if err != nil {
			t.Fatalf("Failed to connect to database: %v", err)
		}
		defer db.Close()

		var breadID int
		err = db.QueryRow("SELECT id FROM bread LIMIT 1").Scan(&breadID)
		if err != nil {
			t.Skipf("No bread available: %v", err)
		}

		// Create buy order message
		buyOrder := data.BuyOrder{
			CustomerID:   1,
			BuyOrderUUID: "test-order-integration-1",
			Status:       "Pending",
			Breads: []data.Bread{
				{ID: breadID, Name: "Test Bread", Quantity: 1},
			},
		}

		payload, err := json.Marshal(buyOrder)
		if err != nil {
			t.Fatalf("Failed to marshal buy order: %v", err)
		}

		// Publish to RabbitMQ
		err = ch.Publish(
			"",                  // exchange
			"buy-bread-order",   // routing key
			false,               // mandatory
			false,               // immediate
			rabbitmq.Publishing{
				ContentType:  "application/json",
				Body:         payload,
				DeliveryMode: rabbitmq.Persistent,
			},
		)
		if err != nil {
			t.Fatalf("Failed to publish message: %v", err)
		}

		t.Logf("Published buy order: %s", buyOrder.BuyOrderUUID)

		// Wait for broker to process (up to 60 seconds)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		maxAttempts := 12
		for i := 0; i < maxAttempts; i++ {
			var status string
			err = db.QueryRowContext(ctx,
				"SELECT status FROM buy_order WHERE buy_order_uuid = $1",
				buyOrder.BuyOrderUUID,
			).Scan(&status)

			if err == nil && (status == "Processed" || status == "Failed") {
				t.Logf("Order processed with status: %s", status)
				return
			}

			time.Sleep(5 * time.Second)
		}

		// Final check
		var finalStatus string
		err = db.QueryRowContext(ctx,
			"SELECT status FROM buy_order WHERE buy_order_uuid = $1",
			buyOrder.BuyOrderUUID,
		).Scan(&finalStatus)

		if err == nil {
			t.Logf("Final order status: %s", finalStatus)
		}
	})

	t.Run("CheckBuyBreadOrderQueueExists", func(t *testing.T) {
		// Check if the queue exists
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		msgs, err := ch.Consume(
			"buy-bread-order",
			"test-consumer",
			true,                // auto-ack
			false,               // exclusive
			false,               // no-local
			false,               // no-wait
			nil,                 // args
		)
		if err != nil {
			t.Fatalf("Failed to consume from queue: %v", err)
		}

		// Just verify we can consume (queue exists)
		select {
		case <-msgs:
			t.Log("Queue has messages")
		case <-ctx.Done():
			t.Log("Queue is empty or consumer timed out")
		}
	})

	t.Run("CheckBreadBoughtQueueExists", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		msgs, err := ch.Consume(
			"bread-bought",
			"test-consumer",
			true,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			t.Fatalf("Failed to consume from bread-bought queue: %v", err)
		}

		select {
		case <-msgs:
			t.Log("bread-bought queue has messages")
		case <-ctx.Done():
			t.Log("bread-bought queue is empty")
		}
	})
}

// TestBroker_Repos_Integration tests repository operations in broker context
func TestBroker_Repos_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	db := fixture.DB

	t.Run("InsertBuyOrder", func(t *testing.T) {
		repo := data.NewPostgresRepository(db)

		buyOrder := data.BuyOrder{
			CustomerID:   1,
			BuyOrderUUID: "integration-test-order",
			Status:       "Pending",
			Breads: []data.Bread{
				{ID: 1, Name: "Test", Quantity: 1},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		id, err := repo.InsertBuyOrder(buyOrder, buyOrder.Breads)
		if err != nil {
			t.Fatalf("Failed to insert buy order: %v", err)
		}

		if id <= 0 {
			t.Errorf("Expected positive ID, got %d", id)
		}

		t.Logf("Created buy order with ID: %d", id)
	})

	t.Run("InsertOutboxMessage", func(t *testing.T) {
		repo := data.NewPostgresRepository(db)

		// Clear existing messages first
		_, err := db.Exec("DELETE FROM outbox")
		if err != nil {
			t.Logf("Warning: Could not clear outbox: %v", err)
		}

		payload := []byte(`{"test": "outbox message"}`)
		outboxMsg := data.OutboxMessage{
			Payload:   payload,
			Sent:      false,
			CreatedAt: time.Now(),
		}

		err = repo.InsertOutboxMessage(outboxMsg)
		if err != nil {
			t.Fatalf("Failed to insert outbox message: %v", err)
		}

		// Verify
		messages, err := repo.GetUnprocessedOutboxMessages()
		if err != nil {
			t.Fatalf("Failed to get unprocessed messages: %v", err)
		}

		if len(messages) == 0 {
			t.Error("Expected at least one unprocessed message")
		}

		t.Logf("Inserted and verified outbox message")
	})

	t.Run("AdjustBreadQuantity", func(t *testing.T) {
		repo := data.NewPostgresRepository(db)

		// Get a bread item
		var bread data.Bread
		err := db.QueryRow("SELECT id, name, quantity FROM bread LIMIT 1").
			Scan(&bread.ID, &bread.Name, &bread.Quantity)
		if err != nil {
			t.Skipf("No bread available: %v", err)
		}

		initialQty := bread.Quantity

		// Adjust quantity
		success, err := repo.AdjustBreadQuantity(bread.ID, -5)
		if err != nil {
			t.Fatalf("Failed to adjust quantity: %v", err)
		}

		if !success {
			t.Error("Expected quantity adjustment to succeed")
		}

		// Verify
		var newQty int
		err = db.QueryRow("SELECT quantity FROM bread WHERE id = $1", bread.ID).Scan(&newQty)
		if err != nil {
			t.Fatalf("Failed to verify quantity: %v", err)
		}

		if newQty != initialQty-5 {
			t.Errorf("Expected quantity %d, got %d", initialQty-5, newQty)
		}

		t.Logf("Adjusted bread quantity: %d -> %d", initialQty, newQty)

		// Reset
		_, _ = repo.AdjustBreadQuantity(bread.ID, 5)
	})

	t.Run("UpdateOrderStatus", func(t *testing.T) {
		repo := data.NewPostgresRepository(db)

		// Create an order first
		buyOrder := data.BuyOrder{
			CustomerID:   1,
			BuyOrderUUID: "integration-status-test",
			Status:       "Pending",
			Breads:       []data.Bread{{ID: 1}},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		_, err := repo.InsertBuyOrder(buyOrder, buyOrder.Breads)
		if err != nil {
			t.Skipf("Could not create order: %v", err)
		}

		// Update status
		err = repo.UpdateOrderStatus(buyOrder.BuyOrderUUID, "Processed")
		if err != nil {
			t.Fatalf("Failed to update order status: %v", err)
		}

		// Verify
		var status string
		err = db.QueryRow("SELECT status FROM buy_order WHERE buy_order_uuid = $1",
			buyOrder.BuyOrderUUID).Scan(&status)
		if err != nil {
			t.Fatalf("Failed to verify status: %v", err)
		}

		if status != "Processed" {
			t.Errorf("Expected status 'Processed', got '%s'", status)
		}

		t.Logf("Updated order status to: %s", status)
	})
}

// TestBroker_NewRabbitMQBakery_Integration tests factory function
func TestBroker_NewRabbitMQBakery_Integration(t *testing.T) {
	dbDSN := testutils.GetDBDSNFromT(t)
	db, err := sql.Open("pgx", dbDSN)
	if err != nil {
		t.Skipf("Could not connect to database: %v", err)
	}
	defer db.Close()

	repo := data.NewPostgresRepository(db)
	cfg := Config{Repo: repo}

	bakery := NewRabbitMQBakery(cfg, "amqp://test:5672")

	if bakery == nil {
		t.Fatal("Expected non-nil RabbitMQBakery")
	}

	if bakery.rabbitmqURL != "amqp://test:5672" {
		t.Errorf("Expected rabbitmqURL 'amqp://test:5672', got '%s'", bakery.rabbitmqURL)
	}

	if bakery.orders == nil {
		t.Error("Expected orders map to be initialized")
	}

	if bakery.Repo == nil {
		t.Error("Expected Repo to be set")
	}

	t.Log("NewRabbitMQBakery correctly initialized")
}

// TestBroker_gRPCClient_Integration tests gRPC client setup in broker
func TestBroker_gRPCClient_Integration(t *testing.T) {
	addr := testutils.GetGRPCAddress()
	
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		t.Skipf("Could not connect to gRPC server: %v", err)
	}
	defer conn.Close()

	client := pb.NewBuyBreadClient(conn)

	t.Run("ConnectToBuyBreadClient", func(t *testing.T) {
		// Just verify we can create the client
		if client == nil {
			t.Error("Expected non-nil BuyBreadClient")
		}
		t.Log("BuyBreadClient created successfully")
	})
}

// TestBroker_canFulfillOrder_Integration tests the helper function
func TestBroker_canFulfillOrder_Integration(t *testing.T) {
	available := []data.Bread{
		{Name: "Bread1", Quantity: 10},
		{Name: "Bread2", Quantity: 5},
	}

	t.Run("CanFulfill", func(t *testing.T) {
		order := data.BuyOrder{
			Breads: []data.Bread{
				{Name: "Bread1", Quantity: 5},
				{Name: "Bread2", Quantity: 3},
			},
		}
		if !canFulfillOrder(order, available) {
			t.Error("Expected order to be fulfillable")
		}
	})

	t.Run("CannotFulfillInsufficient", func(t *testing.T) {
		order := data.BuyOrder{
			Breads: []data.Bread{
				{Name: "Bread1", Quantity: 15},
			},
		}
		if canFulfillOrder(order, available) {
			t.Error("Expected order to fail: insufficient quantity")
		}
	})

	t.Run("CannotFulfillMissing", func(t *testing.T) {
		order := data.BuyOrder{
			Breads: []data.Bread{
				{Name: "Bread3", Quantity: 1},
			},
		}
		if canFulfillOrder(order, available) {
			t.Error("Expected order to fail: bread not in stock")
		}
	})
}
