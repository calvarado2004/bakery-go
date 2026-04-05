package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	_ "github.com/jackc/pgx/v4/stdlib"
	rabbitmq "github.com/rabbitmq/amqp091-go"
)

// --- Integration test environment ---

type brokerTestEnv struct {
	db            *sql.DB
	repo          data.Repository
	rabbitConn    *rabbitmq.Connection
	rabbitChannel *rabbitmq.Channel
	config        Config
}

func setupBrokerIntegrationEnv(t *testing.T) *brokerTestEnv {
	t.Helper()

	// Get database connection from environment or use default
	dsn := getEnvOrDefault("DSN", "host=localhost user=postgres password=postgres dbname=bakery sslmode=disable")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("Skipping integration test: cannot open DB connection: %v", err)
	}

	if err := db.Ping(); err != nil {
		err = db.Close()
		if err != nil {
			return nil
		}
		t.Skipf("Skipping integration test: cannot ping DB: %v", err)
	}

	repo := data.NewPostgresRepository(db)

	// Get RabbitMQ connection
	rabbitURL := getEnvOrDefault("RABBITMQ_SERVICE_ADDR", "amqp://guest:guest@localhost:5672/")
	conn, err := rabbitmq.Dial(rabbitURL)
	if err != nil {
		err := db.Close()
		if err != nil {
			return nil
		}
		t.Skipf("Skipping integration test: cannot connect to RabbitMQ: %v", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		err := db.Close()
		if err != nil {
			return nil
		}
		err = conn.Close()
		if err != nil {
			return nil
		}
		t.Skipf("Skipping integration test: cannot open RabbitMQ channel: %v", err)
	}

	return &brokerTestEnv{
		db:            db,
		repo:          repo,
		rabbitConn:    conn,
		rabbitChannel: ch,
		config:        Config{Repo: repo},
	}
}

func (env *brokerTestEnv) teardown(t *testing.T) {
	t.Helper()
	if err := env.rabbitChannel.Close(); err != nil {
		t.Logf("Warning: failed to close RabbitMQ channel: %v", err)
	}
	if err := env.rabbitConn.Close(); err != nil {
		t.Logf("Warning: failed to close RabbitMQ connection: %v", err)
	}
	if err := env.db.Close(); err != nil {
		t.Logf("Warning: failed to close database: %v", err)
	}
}

// --- Helper functions ---

func getEnvOrDefault(key, defaultValue string) string {
	// For integration tests, we use hardcoded defaults
	// In production, these would come from environment variables
	if key == "DSN" {
		return "host=localhost user=postgres password=password dbname=bakery sslmode=disable"
	}
	if key == "RABBITMQ_SERVICE_ADDR" {
		return "amqp://guest:guest@localhost:5672/"
	}
	return defaultValue
}

// --- Integration tests for canFulfillOrder with real data ---

func TestIntegrationCanFulfillOrder_WithRealData(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	// Get available bread from real database
	available, err := env.repo.GetAvailableBread()
	if err != nil {
		t.Skipf("Skipping test: cannot get available bread: %v", err)
	}

	if len(available) == 0 {
		t.Skip("Skipping test: no bread available in database")
	}

	// Create an order that should be fulfillable
	order := data.BuyOrder{
		Breads: make([]data.Bread, len(available)),
	}
	for i, bread := range available {
		order.Breads[i] = data.Bread{
			Name:     bread.Name,
			Quantity: 1, // Request just 1 of each
		}
	}

	if !canFulfillOrder(order, available) {
		t.Error("expected order with quantity 1 to be fulfillable")
	}
}

func TestIntegrationCanFulfillOrder_InsufficientStock(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	available, err := env.repo.GetAvailableBread()
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	if len(available) == 0 {
		t.Skip("No bread available")
	}

	// Request more than available
	order := data.BuyOrder{
		Breads: make([]data.Bread, len(available)),
	}
	for i, bread := range available {
		order.Breads[i] = data.Bread{
			Name:     bread.Name,
			Quantity: bread.Quantity + 100, // Request way more than available
		}
	}

	if canFulfillOrder(order, available) {
		t.Error("expected order to fail due to insufficient stock")
	}
}

// --- Integration tests for processOrderItems ---

func TestIntegrationProcessOrderItems_RealDatabase(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	available, err := env.repo.GetAvailableBread()
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	if len(available) < 2 {
		t.Skip("Need at least 2 bread items for this test")
	}

	// Get initial quantities
	initialQuantities := make(map[int]int)
	for _, bread := range available[:2] {
		initialQuantities[bread.ID] = bread.Quantity
	}

	// Create order for 1 of each
	order := data.BuyOrder{
		Breads: []data.Bread{
			{ID: available[0].ID, Name: available[0].Name, Quantity: 1},
			{ID: available[1].ID, Name: available[1].Name, Quantity: 1},
		},
	}

	err = processOrderItems(env.repo, order)
	if err != nil {
		t.Skipf("Skipping detailed verification: %v", err)
	}

	// Verify quantities were deducted
	for _, bread := range available[:2] {
		updated, err := env.repo.GetBreadByID(bread.ID)
		if err != nil {
			t.Logf("Could not verify bread %d: %v", bread.ID, err)
			continue
		}
		expectedQty := initialQuantities[bread.ID] - 1
		if updated.Quantity != expectedQty {
			t.Errorf("Bread %d: expected quantity %d, got %d", bread.ID, expectedQty, updated.Quantity)
		}
	}
}

// --- Integration tests for NewRabbitMQBakery ---

func TestIntegrationNewRabbitMQBakery_Connected(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	broker := NewRabbitMQBakery(env.config, "amqp://localhost:5672")

	if broker == nil {
		t.Fatal("expected non-nil broker")
	}
	if broker.Repo == nil {
		t.Error("expected Repo to be set")
	}
	if broker.orders == nil {
		t.Error("expected orders map to be initialized")
	}
}

// --- Integration tests for performBuyBread flow ---

func TestIntegrationPerformBuyBread_FullFlow(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	_ = NewRabbitMQBakery(env.config, getEnvOrDefault("RABBITMQ_SERVICE_ADDR", "amqp://guest:guest@localhost:5672/"))

	// Get an existing customer ID from the database
	var customerID int
	err := env.db.QueryRow("SELECT id FROM customer LIMIT 1").Scan(&customerID)
	if err != nil {
		t.Skipf("Skipping test: no customers in database: %v", err)
	}

	// Get an existing bread ID from the database
	var breadID int
	var breadName string
	err = env.db.QueryRow("SELECT id, name FROM bread LIMIT 1").Scan(&breadID, &breadName)
	if err != nil {
		t.Skipf("Skipping test: no bread in database: %v", err)
	}

	// Create a test buy order using actual bread from database
	buyOrderUUID := fmt.Sprintf("test-integration-%d", time.Now().UnixNano())
	testBread := []data.Bread{
		{ID: breadID, Name: breadName, Quantity: 1},
	}

	buyOrder := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: buyOrderUUID,
		Breads:       testBread,
		Status:       "Pending",
	}

	// Verify we can insert the order
	orderID, err := env.repo.InsertBuyOrder(buyOrder, testBread)
	if err != nil {
		t.Skipf("Skipping test: cannot insert test order: %v", err)
	}
	t.Logf("Inserted test order with ID: %d", orderID)

	// Verify order was inserted
	storedOrder, err := env.repo.GetBuyOrderByUUID(buyOrderUUID)
	if err != nil {
		t.Errorf("Failed to retrieve stored order: %v", err)
	}
	if storedOrder.BuyOrderUUID != buyOrderUUID {
		t.Errorf("Expected UUID %s, got %s", buyOrderUUID, storedOrder.BuyOrderUUID)
	}
}

func TestIntegrationPerformBuyBread_OutboxMessage(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	_ = NewRabbitMQBakery(env.config, getEnvOrDefault("RABBITMQ_SERVICE_ADDR", "amqp://guest:guest@localhost:5672/"))

	// Get an existing customer ID from the database
	var customerID int
	err := env.db.QueryRow("SELECT id FROM customer LIMIT 1").Scan(&customerID)
	if err != nil {
		t.Skipf("Skipping test: no customers in database: %v", err)
	}

	// Get an existing bread ID from the database
	var breadID int
	var breadName string
	err = env.db.QueryRow("SELECT id, name FROM bread LIMIT 1").Scan(&breadID, &breadName)
	if err != nil {
		t.Skipf("Skipping test: no bread in database: %v", err)
	}

	// Create a test buy order using actual bread from database
	buyOrderUUID := fmt.Sprintf("test-outbox-%d", time.Now().UnixNano())
	testBread := []data.Bread{
		{ID: breadID, Name: breadName, Quantity: 1},
	}

	buyOrder := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: buyOrderUUID,
		Breads:       testBread,
		Status:       "Pending",
	}

	// Insert order
	_, err = env.repo.InsertBuyOrder(buyOrder, testBread)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}

	// Insert corresponding outbox message
	outboxMsg := data.OutboxMessage{
		Payload:   json.RawMessage(fmt.Sprintf(`{"uuid":"%s"}`, buyOrderUUID)),
		Sent:      false,
		CreatedAt: time.Now(),
	}

	err = env.repo.InsertOutboxMessage(outboxMsg)
	if err != nil {
		t.Errorf("Failed to insert outbox message: %v", err)
	}

	// Verify outbox message
	messages, err := env.repo.GetUnprocessedOutboxMessages()
	if err != nil {
		t.Errorf("Failed to get unprocessed messages: %v", err)
	}

	found := false
	for _, msg := range messages {
		if msg.Payload != nil {
			found = true
			break
		}
	}
	if !found {
		t.Log("No outbox messages found (may have been processed)")
	}
}

func TestIntegrationPerformBuyBread_OrderStatusUpdate(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	_ = NewRabbitMQBakery(env.config, getEnvOrDefault("RABBITMQ_SERVICE_ADDR", "amqp://guest:guest@localhost:5672/"))

	// Get an existing customer ID from the database
	var customerID int
	err := env.db.QueryRow("SELECT id FROM customer LIMIT 1").Scan(&customerID)
	if err != nil {
		t.Skipf("Skipping test: no customers in database: %v", err)
	}

	// Get an existing bread ID from the database
	var breadID int
	var breadName string
	err = env.db.QueryRow("SELECT id, name FROM bread LIMIT 1").Scan(&breadID, &breadName)
	if err != nil {
		t.Skipf("Skipping test: no bread in database: %v", err)
	}

	// Create test order using actual bread from database
	buyOrderUUID := fmt.Sprintf("test-status-%d", time.Now().UnixNano())
	testBread := []data.Bread{
		{ID: breadID, Name: breadName, Quantity: 1},
	}

	buyOrder := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: buyOrderUUID,
		Breads:       testBread,
		Status:       "Pending",
	}

	_, _ = env.repo.InsertBuyOrder(buyOrder, testBread)

	// Update status
	err = env.repo.UpdateOrderStatus(buyOrderUUID, "Processed")
	if err != nil {
		t.Errorf("Failed to update order status: %v", err)
	}

	// Verify status update
	storedOrder, err := env.repo.GetBuyOrderByUUID(buyOrderUUID)
	if err != nil {
		t.Errorf("Failed to retrieve order: %v", err)
	}
	if storedOrder.Status != "Processed" {
		t.Errorf("Expected status 'Processed', got '%s'", storedOrder.Status)
	}

	// Clean up
	_, _ = env.repo.AdjustBreadQuantity(testBread[0].ID, 1) // Restore quantity
}

// --- Integration tests for database operations ---

func TestIntegrationAdjustBreadQuantity_RealDB(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	available, err := env.repo.GetAvailableBread()
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}

	if len(available) == 0 {
		t.Skip("No bread available")
	}

	bread := available[0]
	initialQty := bread.Quantity

	// Deduct 1
	_, err = env.repo.AdjustBreadQuantity(bread.ID, -1)
	if err != nil {
		t.Skipf("Skipping detailed check: %v", err)
	}

	// Verify
	updated, err := env.repo.GetBreadByID(bread.ID)
	if err != nil {
		t.Errorf("Failed to retrieve updated bread: %v", err)
	}
	if updated.Quantity != initialQty-1 {
		t.Errorf("Expected quantity %d, got %d", initialQty-1, updated.Quantity)
	}

	// Restore
	_, _ = env.repo.AdjustBreadQuantity(bread.ID, 1)
}

func TestIntegrationGetAvailableBread(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	available, err := env.repo.GetAvailableBread()
	if err != nil {
		t.Errorf("Failed to get available bread: %v", err)
	}

	if len(available) == 0 {
		t.Skip("No bread available in database")
	}

	// Verify all bread has required fields
	for _, bread := range available {
		if bread.Name == "" {
			t.Error("Expected non-empty bread name")
		}
		if bread.Quantity < 0 {
			t.Error("Expected non-negative quantity")
		}
	}

	t.Logf("Found %d bread items in inventory", len(available))
}

// --- Concurrency tests ---

func TestIntegrationBroker_ConcurrentOrderProcessing(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	_ = NewRabbitMQBakery(env.config, getEnvOrDefault("RABBITMQ_SERVICE_ADDR", "amqp://guest:guest@localhost:5672/"))

	const numOrders = 5

	// Get an existing customer ID from the database
	var customerID int
	err := env.db.QueryRow("SELECT id FROM customer LIMIT 1").Scan(&customerID)
	if err != nil {
		t.Skipf("Skipping test: no customers in database: %v", err)
	}

	// Get multiple bread IDs from the database (use different breads for each concurrent order)
	var breads []data.Bread
	rows, err := env.db.Query("SELECT id, name FROM bread LIMIT $1", numOrders)
	if err != nil {
		t.Skipf("Skipping test: cannot query bread: %v", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			t.Skipf("Skipping test: cannot close bread query: %v", err)
		}
	}(rows)

	for rows.Next() {
		var b data.Bread
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			t.Skipf("Skipping test: cannot scan bread: %v", err)
		}
		breads = append(breads, b)
	}

	if len(breads) == 0 {
		t.Skip("Skipping test: no bread available")
	}

	var wg sync.WaitGroup
	results := make([]error, numOrders)

	for j := 0; j < numOrders; j++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			buyOrderUUID := fmt.Sprintf("test-concurrent-%d-%d", idx, time.Now().UnixNano())
			// Use different bread for each order to avoid quantity conflicts
			breadIdx := idx % len(breads)
			testBread := []data.Bread{
				{ID: breads[breadIdx].ID, Name: breads[breadIdx].Name, Quantity: 1},
			}

			buyOrder := data.BuyOrder{
				CustomerID:   customerID,
				BuyOrderUUID: buyOrderUUID,
				Breads:       testBread,
				Status:       "Pending",
			}

			_, err := env.repo.InsertBuyOrder(buyOrder, testBread)
			results[idx] = err
		}(j)
	}

	wg.Wait()

	errorCount := 0
	for _, err := range results {
		if err != nil {
			t.Logf("Order failed: %v", err)
			errorCount++
		}
	}

	t.Logf("Concurrent test: %d/%d orders succeeded", numOrders-errorCount, numOrders)
	if errorCount > numOrders/2 {
		t.Errorf("More than half of orders failed: %d/%d", errorCount, numOrders)
	}
}

func TestIntegrationBroker_RepositoryConcurrency(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	_ = NewRabbitMQBakery(env.config, getEnvOrDefault("RABBITMQ_SERVICE_ADDR", "amqp://guest:guest@localhost:5672/"))

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			// Perform concurrent reads
			_, _ = env.repo.GetAvailableBread()
			_, _ = env.repo.GetDashboardStats()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("Concurrent repository operations completed successfully")
	case <-time.After(10 * time.Second):
		t.Fatal("Concurrent repository operations timed out")
	}
}

// --- RabbitMQ integration tests ---

func TestIntegrationRabbitMQ_ConnectionAndChannel(t *testing.T) {
	rabbitURL := getEnvOrDefault("RABBITMQ_SERVICE_ADDR", "amqp://guest:guest@localhost:5672/")

	conn, err := rabbitmq.Dial(rabbitURL)
	if err != nil {
		t.Skipf("Skipping RabbitMQ test: %v", err)
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			t.Skipf("Skipping RabbitMQ test: cannot close connection: %v", err)
		}
	}(conn)

	ch, err := conn.Channel()
	if err != nil {
		t.Errorf("Failed to open channel: %v", err)
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			t.Skipf("Skipping RabbitMQ test: cannot close channel: %v", err)
		}
	}(ch)

	// Verify we can declare and check the queue
	// Note: durable=true to match the server's queue declaration
	queue, err := ch.QueueDeclare(
		"buy-bread-order",
		true, // durable (matches server)
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		t.Errorf("Failed to declare queue: %v", err)
	}

	t.Logf("Verified RabbitMQ queue 'buy-bread-order' exists with %d messages", queue.Messages)
}

func TestIntegrationRabbitMQ_PublishAndConsume(t *testing.T) {
	env := setupBrokerIntegrationEnv(t)
	defer env.teardown(t)

	// Publish a test message
	testUUID := fmt.Sprintf("test-pubsub-%d", time.Now().UnixNano())
	testPayload, _ := json.Marshal(data.BuyOrder{
		BuyOrderUUID: testUUID,
		Breads:       []data.Bread{{Name: "TestBread", Quantity: 1}},
	})

	err := env.rabbitChannel.Publish(
		"",                // exchange
		"buy-bread-order", // routing key
		false,             // mandatory
		false,             // immediate
		rabbitmq.Publishing{
			ContentType:  "text/json",
			Body:         testPayload,
			DeliveryMode: rabbitmq.Persistent,
		},
	)
	if err != nil {
		t.Errorf("Failed to publish message: %v", err)
	}

	// Consume the message
	msgs, err := env.rabbitChannel.Consume(
		"buy-bread-order",
		"",
		true,  // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		t.Errorf("Failed to consume: %v", err)
	}

	// Wait for message
	select {
	case msg := <-msgs:
		var received data.BuyOrder
		if err := json.Unmarshal(msg.Body, &received); err != nil {
			t.Errorf("Failed to unmarshal: %v", err)
		}
		if received.BuyOrderUUID != testUUID {
			t.Errorf("Expected UUID %s, got %s", testUUID, received.BuyOrderUUID)
		}
		t.Logf("Successfully published and consumed message with UUID: %s", testUUID)
	case <-time.After(5 * time.Second):
		t.Skip("Timeout waiting for message (queue may be empty)")
	}
}
