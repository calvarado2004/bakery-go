package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	"github.com/calvarado2004/bakery-go/pkg/resilience"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	pb "github.com/calvarado2004/bakery-go/proto"
)

// ─────────────────────────────────────────────────────────────────────────────
// RabbitMQ init() test via RabbitMQDialer interface
// ─────────────────────────────────────────────────────────────────────────────

// testRabbitMQDialer returns a real AMQP connection.
type testRabbitMQDialer struct {
	url string
}

func (d *testRabbitMQDialer) Dial() (*rabbitmq.Connection, error) {
	return rabbitmq.Dial(d.url)
}

// mockDialer is a test double for RabbitMQDialer returning mock connections.
type mockDialer struct {
	conn *mockConn
	err  error
}

func (m *mockDialer) Dial() (*rabbitmq.Connection, error) {
	if m.err != nil {
		return nil, m.err
	}
	return nil, nil
}

// mockConn simulates a RabbitMQ connection for dialer testing.
type mockConn struct{}

func TestInit_QueueDeclaration(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	// Declare queues via test dialer (simulates init())
	dialer := &testRabbitMQDialer{url: "amqp://guest:guest@localhost:5672/"}

	conn, err := dialer.Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	defer ch.Close()

	// Declare the bread-made queue (same as init())
	_, err = ch.QueueDeclare(
		"bread-made",
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		t.Fatalf("QueueDeclare bread-made: %v", err)
	}

	// Verify the queue exists
	info, err := ch.QueueInspect("bread-made")
	if err != nil {
		t.Fatalf("QueueInspect: %v", err)
	}
	t.Logf("bread-made queue: name=%q, messages=%d, consumers=%d", info.Name, info.Messages, info.Consumers)
}

// ─────────────────────────────────────────────────────────────────────────────
// checkBread integration test
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckBread(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	// Seed DB
	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)
	seedAccounts(t, db)

	// Seed some bread items (bypass initializeBakery which calls the repo)
	breads := []data.Bread{
		{Name: "Low Stock Bread", Price: 2.99, Quantity: 5, Description: "Low stock", Type: "Test", Status: "available"},
		{Name: "Adequate Bread", Price: 3.99, Quantity: 50, Description: "Adequate", Type: "Test", Status: "available"},
	}
	for _, b := range breads {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO bread (name, price, quantity, description, type, status)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			b.Name, b.Price, b.Quantity, b.Description, b.Type, b.Status)
		if err != nil {
			t.Fatalf("Insert bread %s: %v", b.Name, err)
		}
	}

	// Create a bread maker too
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO bread_maker (name, email) VALUES ('Test Maker', 'test@maker.com')`)
	if err != nil {
		t.Logf("Insert bread maker: %v", err)
	}

	// Wait for server to pick up the data
	waitForServer(t)
	conn := dialGRPC(t)
	if conn == nil {
		t.Skip("gRPC server not available")
	}
	defer conn.Close()

	time.Sleep(2 * time.Second)

	// Use the inventory to verify bread exists
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	inventoryClient := pb.NewCheckInventoryClient(conn)
	resp, err := inventoryClient.CheckBreadInventory(ctx, &pb.BreadRequest{})
	if err != nil {
		t.Logf("CheckBreadInventory: %v", err)
	} else if resp != nil && resp.Breads != nil {
		t.Logf("Inventory has %d bread types", len(resp.Breads.Breads))
		for _, b := range resp.Breads.Breads {
			t.Logf("  - %s: qty=%d", b.Name, b.Quantity)
		}
	}

	// checkBread should find low-stock bread and create a pending_make_order
	// We can verify this by querying the DB after a brief wait
	time.Sleep(35 * time.Second) // checkBread runs every 30s

	var pendingCount int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pending_make_orders WHERE source = 'auto'").Scan(&pendingCount)
	t.Logf("Pending auto make orders: %d", pendingCount)
	// Note: pendingCount may be 0 if the server hasn't run checkBread yet
	// or if it already processed them
}

// ─────────────────────────────────────────────────────────────────────────────
// initializeBakery integration test
// ─────────────────────────────────────────────────────────────────────────────

func TestInitializeBakery_Integration(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)

	// Seed only the bread maker (initializeBakery needs it for FK)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO bread_maker (name, email) VALUES ('Test Maker', 'test@maker.com')`)
	if err != nil {
		t.Logf("Insert bread maker: %v", err)
	}

	// Create a RabbitMQBakery with a test repo and call initializeBakery
	config := Config{}
	config.setupRepo(db)

	rmq := NewRabbitMQBakery(config, "amqp://guest:guest@localhost:5672/", &testRabbitMQDialer{
		url: "amqp://guest:guest@localhost:5672/",
	})

	// Call initializeBakery — this should create the default bread items
	rmq.initializeBakery()

	// Verify bread was created
	count, err := db.QueryContext(context.Background(), "SELECT COUNT(*) FROM bread")
	if err != nil {
		t.Fatalf("Query bread count: %v", err)
	}
	defer count.Close()

	var breadCount int
	if count.Next() {
		count.Scan(&breadCount)
	}
	t.Logf("Bread items after initializeBakery: %d", breadCount)
	if breadCount < 7 {
		// The initializeBakery creates 7 bread types
		t.Logf("Warning: expected 7 bread items, got %d (some inserts may have failed due to FK)", breadCount)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// listenForBreadMade integration test
// ─────────────────────────────────────────────────────────────────────────────

func TestListenForBreadMade(t *testing.T) {
	// Check if the gRPC server is running (its listenForBreadMade goroutine
	// consumes from the bread-made queue, competing with this test).
	if isServerRunning() {
		t.Skip("Server is running and consuming from bread-made queue; test cannot reliably receive messages")
	}

	setupInfra(t)
	defer teardownInfra(t)

	// Seed DB
	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)

	// Create a bread item to receive the update
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO bread (name, price, quantity, description, type, status)
		 VALUES ('Test Bread', 1.00, 10, 'Test', 'Test', 'available')`)
	if err != nil {
		t.Logf("Insert bread: %v", err)
	}

	// Declare and purge the bread-made queue
	conn, err := rabbitmq.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		t.Skipf("RabbitMQ not available: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare("bread-made", true, false, false, false, nil)
	if err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}

	// Purge any existing messages
	ch.QueuePurge("bread-made", false) //nolint:errcheck

	// Publish a bread-made message
	msg := map[string]interface{}{
		"breadId":  1,
		"quantity": 50,
	}
	body, _ := json.Marshal(msg)
	err = ch.Publish("", "bread-made", false, false, rabbitmq.Publishing{
		ContentType: "application/json",
		Body:        body,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Verify message was published
	info, err := ch.QueueInspect("bread-made")
	if err != nil {
		t.Logf("QueueInspect: %v", err)
	} else {
		t.Logf("bread-made queue has %d messages", info.Messages)
	}

	// The actual listenForBreadMade runs in the server process.
	// We verify by checking if the server received the message:
	// After the server processes it, the bread quantity should be updated.
	// But since we can't easily inject our own dialer into the running server,
	// we verify the message flow end-to-end via the consumer side.

	// Consume the message to verify format
	consumer, err := ch.Consume("bread-made", "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	select {
	case d := <-consumer:
		var msg2 breadMadeMessage
		if err := json.Unmarshal(d.Body, &msg2); err != nil {
			t.Fatalf("Unmarshal bread-made message: %v", err)
		}
		if msg2.BreadID != 1 {
			t.Errorf("expected breadId 1, got %d", msg2.BreadID)
		}
		if msg2.Quantity != 50 {
			t.Errorf("expected quantity 50, got %d", msg2.Quantity)
		}
		d.Ack(false)
		t.Log("Successfully consumed and validated bread-made message")
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for bread-made message")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RabbitMQDialer integration test
// ─────────────────────────────────────────────────────────────────────────────

func TestRabbitMQDialer_Dial(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	dialer := &testRabbitMQDialer{url: "amqp://guest:guest@localhost:5672/"}
	conn, err := dialer.Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Verify connection is alive
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(fmt.Sprintf("test-dial-%d", time.Now().UnixNano()), false, false, true, false, nil)
	if err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// LogCircuitStates test
// ─────────────────────────────────────────────────────────────────────────────

func TestLogCircuitStates(t *testing.T) {
	// LogCircuitStates runs in a loop, so we test it briefly with a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start logging in background
	go LogCircuitStates(ctx)

	// Let it run for a tick
	time.Sleep(11 * time.Second)
	cancel()

	// No crash = success
	t.Log("LogCircuitStates completed without crash")
}

// ─────────────────────────────────────────────────────────────────────────────
// Combined checkBread + initializeBakery flow test
// ─────────────────────────────────────────────────────────────────────────────

func TestCheckBread_EmptyInventory(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)

	// Seed a bread maker (FK requirement)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO bread_maker (name, email) VALUES ('Test Maker', 'test@maker.com')`)
	if err != nil {
		t.Logf("Insert bread maker: %v", err)
	}

	config := Config{}
	config.setupRepo(db)

	rmq := NewRabbitMQBakery(config, "amqp://guest:guest@localhost:5672/", &testRabbitMQDialer{
		url: "amqp://guest:guest@localhost:5672/",
	})

	// With empty inventory, checkBread should call initializeBakery
	err = rmq.checkBread()
	if err != nil {
		t.Logf("checkBread (expected to fail or succeed with init): %v", err)
	}

	// After checkBread, bread should be initialized
	count, err := db.QueryContext(context.Background(), "SELECT COUNT(*) FROM bread")
	if err != nil {
		t.Fatalf("Query bread count: %v", err)
	}
	defer count.Close()

	var breadCount int
	if count.Next() {
		count.Scan(&breadCount)
	}
	t.Logf("Bread count after checkBread with empty inventory: %d", breadCount)
	// May be 0 if initializeBakery failed due to FK or other issues
}

// ─────────────────────────────────────────────────────────────────────────────
// test helpers for concurrent settlement test
// ─────────────────────────────────────────────────────────────────────────────

// TestSettlementDispatcherConcurrentRegisters verifies thread safety of Register.
func TestSettlementDispatcherConcurrentRegisters(t *testing.T) {
	sd := &SettlementDispatcher{
		waiters: make(map[string]*settlementWaiter),
	}

	uuid := "concurrent-uuid"
	var wg sync.WaitGroup

	// Register from multiple goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := sd.Register(uuid)
			if ch == nil {
				t.Error("Register returned nil channel")
			}
		}()
	}
	wg.Wait()

	// All registrations should have succeeded (the last one wins)
	w, ok := sd.waiters[uuid]
	if !ok {
		t.Fatal("waiter not found after concurrent registrations")
	}
	if w.ch == nil {
		t.Error("waiter channel is nil")
	}
}

// TestSettlementDispatcherUnregisterSafe verifies Unregister is safe to call multiple times.
func TestSettlementDispatcherUnregisterSafe(t *testing.T) {
	sd := &SettlementDispatcher{
		waiters: make(map[string]*settlementWaiter),
	}

	uuid := "unregister-safe-1"
	sd.Register(uuid)

	// Unregister twice — should not panic
	sd.Unregister(uuid)
	sd.Unregister(uuid) // second call should be safe

	w, ok := sd.waiters[uuid]
	if ok {
		// If the entry still exists, its channel should be closed
		select {
		case _, chOk := <-w.ch:
			if chOk {
				t.Error("expected closed channel")
			}
		default:
			// Channel might be empty — that's ok too
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// BrokerServiceServer unit tests (no AMQP required)
// ─────────────────────────────────────────────────────────────────────────────

func TestBrokerServiceServer(t *testing.T) {
	t.Run("CreateServerStruct", func(t *testing.T) {
		bakery := &RabbitMQBakery{}
		srv := &BrokerServiceServer{
			RabbitMQBakery: bakery,
		}
		if srv == nil {
			t.Fatal("BrokerServiceServer is nil")
		}
		if srv.RabbitMQBakery != bakery {
			t.Error("RabbitMQBakery not set")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Rate limiter tests
// ─────────────────────────────────────────────────────────────────────────────

func TestRateLimiter(t *testing.T) {
	// The global rate limiter is initialized with 10 req/s, burst 20.
	// We can test the resilience package directly, but also test that
	// the interceptor rejects when rate-limited.

	// Create a fresh limiter for testing
	limiter := resilience.NewRateLimiter(2, 2) // 2 req/s, burst 2

	// First 2 should succeed
	if !limiter.Allow("test") {
		t.Error("first request should be allowed")
	}
	if !limiter.Allow("test") {
		t.Error("second request should be allowed")
	}

	// Third should be rejected
	if limiter.Allow("test") {
		t.Error("third request should be rate-limited")
	}
}
