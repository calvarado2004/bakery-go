package data_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	"github.com/calvarado2004/bakery-go/testutils"
	_ "github.com/jackc/pgx/v4/stdlib"
)

// ---------------------------------------------------------------------------
// FulfillOrderTx — the most business-critical path in the data layer.
// These tests exercise atomic stock deduction, per-item partial fulfillment,
// order-level status, and deadlock prevention (sorted bread IDs).
// ---------------------------------------------------------------------------

func TestFulfillOrderTx_FullFulfillment(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}
	if err := dbHelper.SeedTestBread(); err != nil {
		t.Fatalf("seed bread: %v", err)
	}

	// Insert customer
	customer := data.Customer{Name: "FulfillTest", Email: "fulfill@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	// Get bread with known quantities
	breads, _ := repo.GetAvailableBread()
	breadA := breads[0] // Sourdough, qty=50
	breadB := breads[1] // Croissant, qty=100

	// Insert buy order (this creates header + order_details)
	order := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "fulfill-tx-full-" + breadA.Name,
		Status:       "pending",
		Breads: []data.Bread{
			{ID: breadA.ID, Quantity: 5, Price: breadA.Price},
			{ID: breadB.ID, Quantity: 3, Price: breadB.Price},
		},
	}
	orderID, err := repo.InsertBuyOrder(order, order.Breads)
	if err != nil {
		t.Fatalf("InsertBuyOrder: %v", err)
	}

	// Fulfill via transaction
	fulfillOrder := data.BuyOrder{
		ID:     orderID,
		Breads: order.Breads,
	}
	err = repo.FulfillOrderTx(fulfillOrder)
	if err != nil {
		t.Fatalf("FulfillOrderTx: %v", err)
	}

	// Verify order status → "processed"
	fetched, err := repo.GetBuyOrderByID(orderID)
	if err != nil {
		t.Fatalf("GetBuyOrderByID: %v", err)
	}
	if fetched.Status != "processed" {
		t.Errorf("order status: expected 'processed', got '%s'", fetched.Status)
	}

	// Verify stock was deducted
	updatedA, _ := repo.GetBreadByID(breadA.ID)
	if updatedA.Quantity != breadA.Quantity-5 {
		t.Errorf("bread A qty: expected %d, got %d", breadA.Quantity-5, updatedA.Quantity)
	}
	updatedB, _ := repo.GetBreadByID(breadB.ID)
	if updatedB.Quantity != breadB.Quantity-3 {
		t.Errorf("bread B qty: expected %d, got %d", breadB.Quantity-3, updatedB.Quantity)
	}
}

func TestFulfillOrderTx_PartialFulfillment(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	// Insert customer
	customer := data.Customer{Name: "PartialTest", Email: "partial@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	// Insert breads with controlled quantities
	breadA := data.Bread{Name: "Plenty", Price: 1.0, Quantity: 100, Type: "Bread", Status: "available"}
	idA, _ := repo.InsertBread(breadA)
	breadB := data.Bread{Name: "Scarce", Price: 2.0, Quantity: 3, Type: "Bread", Status: "available"}
	idB, _ := repo.InsertBread(breadB)

	// Order requests 10 of breadB (only 3 available) — partial fulfillment
	order := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "fulfill-tx-partial-1",
		Status:       "pending",
		Breads: []data.Bread{
			{ID: idA, Quantity: 2, Price: 1.0},
			{ID: idB, Quantity: 10, Price: 2.0},
		},
	}
	orderID, _ := repo.InsertBuyOrder(order, order.Breads)

	fulfillOrder := data.BuyOrder{ID: orderID, Breads: order.Breads}
	err := repo.FulfillOrderTx(fulfillOrder)
	if err != nil {
		t.Fatalf("FulfillOrderTx: %v", err)
	}

	fetched, _ := repo.GetBuyOrderByID(orderID)
	if fetched.Status != "partially_processed" {
		t.Errorf("order status: expected 'partially_processed', got '%s'", fetched.Status)
	}

	// breadA should be fully fulfilled
	updatedA, _ := repo.GetBreadByID(idA)
	if updatedA.Quantity != 98 {
		t.Errorf("bread A qty: expected 98, got %d", updatedA.Quantity)
	}
	// breadB should be partially fulfilled (3 of 10)
	updatedB, _ := repo.GetBreadByID(idB)
	if updatedB.Quantity != 0 {
		t.Errorf("bread B qty: expected 0, got %d", updatedB.Quantity)
	}
}

func TestFulfillOrderTx_SkipUnavailableItems(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	customer := data.Customer{Name: "SkipTest", Email: "skip@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	breadA := data.Bread{Name: "Available", Price: 1.0, Quantity: 50, Type: "Bread", Status: "available"}
	idA, _ := repo.InsertBread(breadA)
	breadB := data.Bread{Name: "OutOfStock", Price: 2.0, Quantity: 0, Type: "Bread", Status: "available"}
	idB, _ := repo.InsertBread(breadB)

	order := data.BuyOrder{
		CustomerID:           customerID,
		BuyOrderUUID:         "fulfill-tx-skip-1",
		Status:               "pending",
		SkipUnavailableItems: true,
		Breads: []data.Bread{
			{ID: idA, Quantity: 2, Price: 1.0},
			{ID: idB, Quantity: 5, Price: 2.0},
		},
	}
	orderID, _ := repo.InsertBuyOrder(order, order.Breads)

	fulfillOrder := data.BuyOrder{
		ID: orderID, Breads: order.Breads,
		SkipUnavailableItems: true,
	}
	err := repo.FulfillOrderTx(fulfillOrder)
	if err != nil {
		t.Fatalf("FulfillOrderTx with skip: %v", err)
	}

	fetched, _ := repo.GetBuyOrderByID(orderID)
	// breadA fulfilled, breadB skipped → partial
	if fetched.Status != "partially_processed" {
		t.Errorf("order status: expected 'partially_processed', got '%s'", fetched.Status)
	}

	// Verify breadB was NOT deducted (still 0)
	updatedB, _ := repo.GetBreadByID(idB)
	if updatedB.Quantity != 0 {
		t.Errorf("bread B should remain at 0, got %d", updatedB.Quantity)
	}
}

func TestFulfillOrderTx_RejectOnNoStock(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	customer := data.Customer{Name: "RejectTest", Email: "reject@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	// Both breads at zero stock
	breadA := data.Bread{Name: "NoneA", Price: 1.0, Quantity: 0, Type: "Bread", Status: "available"}
	idA, _ := repo.InsertBread(breadA)
	breadB := data.Bread{Name: "NoneB", Price: 2.0, Quantity: 0, Type: "Bread", Status: "available"}
	idB, _ := repo.InsertBread(breadB)

	order := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "fulfill-tx-reject-1",
		Status:       "pending",
		// SkipUnavailableItems=false, AllowPartial=false → reject
		Breads: []data.Bread{
			{ID: idA, Quantity: 1, Price: 1.0},
			{ID: idB, Quantity: 1, Price: 2.0},
		},
	}
	orderID, _ := repo.InsertBuyOrder(order, order.Breads)

	fulfillOrder := data.BuyOrder{ID: orderID, Breads: order.Breads}
	err := repo.FulfillOrderTx(fulfillOrder)
	// No error from FulfillOrderTx — it marks as rejected but commits
	if err != nil {
		t.Fatalf("FulfillOrderTx: %v", err)
	}

	fetched, _ := repo.GetBuyOrderByID(orderID)
	if fetched.Status != "rejected" {
		t.Errorf("order status: expected 'rejected', got '%s'", fetched.Status)
	}
}

func TestFulfillOrderTx_DeadlockPrevention(t *testing.T) {
	// Verifies that concurrent FulfillOrderTx calls on overlapping bread sets
	// do not deadlock (thanks to sorted bread IDs in SELECT FOR UPDATE).
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	customer := data.Customer{Name: "DeadlockTest", Email: "deadlock@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	// Insert two breads with enough stock
	breadA := data.Bread{Name: "DL-A", Price: 1.0, Quantity: 100, Type: "Bread", Status: "available"}
	idA, _ := repo.InsertBread(breadA)
	breadB := data.Bread{Name: "DL-B", Price: 2.0, Quantity: 100, Type: "Bread", Status: "available"}
	idB, _ := repo.InsertBread(breadB)

	const numOrders = 5
	var orderIDs []int

	for i := 0; i < numOrders; i++ {
		order := data.BuyOrder{
			CustomerID:   customerID,
			BuyOrderUUID: "deadlock-test-" + string(rune('A'+i)),
			Status:       "pending",
			Breads: []data.Bread{
				{ID: idA, Quantity: 2, Price: 1.0},
				{ID: idB, Quantity: 2, Price: 2.0},
			},
		}
		oid, _ := repo.InsertBuyOrder(order, order.Breads)
		orderIDs = append(orderIDs, oid)
	}

	// Launch concurrent fulfillments
	var wg sync.WaitGroup
	errCh := make(chan error, numOrders)

	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fo := data.BuyOrder{
				ID: orderIDs[idx],
				Breads: []data.Bread{
					{ID: idA, Quantity: 2, Price: 1.0},
					{ID: idB, Quantity: 2, Price: 2.0},
				},
			}
			// Reverse order for even-numbered orders to stress lock ordering
			if idx%2 == 0 {
				fo.Breads = []data.Bread{
					{ID: idB, Quantity: 2, Price: 2.0},
					{ID: idA, Quantity: 2, Price: 1.0},
				}
			}
			if err := repo.FulfillOrderTx(fo); err != nil {
				errCh <- err
			}
		}(i)
	}

	// Wait with timeout to detect deadlock
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished — no deadlock
	case <-time.After(15 * time.Second):
		t.Fatal("Deadlock detected: concurrent FulfillOrderTx calls did not complete in 15s")
	}

	// Check for any errors
	close(errCh)
	for err := range errCh {
		t.Errorf("FulfillOrderTx error: %v", err)
	}

	// Verify stock was deducted: 5 orders × 2 units = 10 units from each bread
	updatedA, _ := repo.GetBreadByID(idA)
	if updatedA.Quantity != 90 {
		t.Errorf("bread A qty: expected 90, got %d", updatedA.Quantity)
	}
	updatedB, _ := repo.GetBreadByID(idB)
	if updatedB.Quantity != 90 {
		t.Errorf("bread B qty: expected 90, got %d", updatedB.Quantity)
	}
}

func TestFulfillOrderTx_OrderDetailsStatus(t *testing.T) {
	// Verifies that order_details rows get correct per-item status.
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	customer := data.Customer{Name: "DetailTest", Email: "detail@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	breadA := data.Bread{Name: "Full", Price: 1.0, Quantity: 100, Type: "Bread", Status: "available"}
	idA, _ := repo.InsertBread(breadA)
	breadB := data.Bread{Name: "Partial", Price: 2.0, Quantity: 3, Type: "Bread", Status: "available"}
	idB, _ := repo.InsertBread(breadB)
	breadC := data.Bread{Name: "None", Price: 3.0, Quantity: 0, Type: "Bread", Status: "available"}
	idC, _ := repo.InsertBread(breadC)

	order := data.BuyOrder{
		CustomerID:           customerID,
		BuyOrderUUID:         "detail-status-1",
		Status:               "pending",
		SkipUnavailableItems: true,
		Breads: []data.Bread{
			{ID: idA, Quantity: 5, Price: 1.0},  // → fulfilled
			{ID: idB, Quantity: 10, Price: 2.0}, // → partially_fulfilled (3 of 10)
			{ID: idC, Quantity: 1, Price: 3.0},  // → skipped (qty=0)
		},
	}
	orderID, _ := repo.InsertBuyOrder(order, order.Breads)

	fulfillOrder := data.BuyOrder{
		ID: orderID, Breads: order.Breads,
		SkipUnavailableItems: true,
	}
	err := repo.FulfillOrderTx(fulfillOrder)
	if err != nil {
		t.Fatalf("FulfillOrderTx: %v", err)
	}

	// Read order_details statuses directly
	checkStatus := func(breadID int, expectedStatus string) {
		var status string
		err := fixture.DB.QueryRow(
			"SELECT status FROM order_details WHERE buy_order_id = $1 AND bread_id = $2",
			orderID, breadID,
		).Scan(&status)
		if err != nil {
			t.Errorf("query order_details for bread %d: %v", breadID, err)
			return
		}
		if status != expectedStatus {
			t.Errorf("bread %d status: expected '%s', got '%s'", breadID, expectedStatus, status)
		}
	}

	checkStatus(idA, "fulfilled")
	checkStatus(idB, "partially_fulfilled")
	checkStatus(idC, "skipped")
}

// ---------------------------------------------------------------------------
// FulfillOrderItem — per-item atomic stock deduction
// ---------------------------------------------------------------------------

func TestFulfillOrderItem_FullDeduction(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}
	if err := dbHelper.SeedTestBread(); err != nil {
		t.Fatalf("seed bread: %v", err)
	}

	breads, _ := repo.GetAvailableBread()
	bread := breads[0]
	initialQty := bread.Quantity

	fulfilled, err := repo.FulfillOrderItem(bread.ID, 5)
	if err != nil {
		t.Fatalf("FulfillOrderItem: %v", err)
	}
	if fulfilled != 5 {
		t.Errorf("expected 5 fulfilled, got %d", fulfilled)
	}

	updated, _ := repo.GetBreadByID(bread.ID)
	if updated.Quantity != initialQty-5 {
		t.Errorf("qty: expected %d, got %d", initialQty-5, updated.Quantity)
	}
}

func TestFulfillOrderItem_PartialDeduction(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	bread := data.Bread{Name: "Limited", Price: 1.0, Quantity: 3, Type: "Bread", Status: "available"}
	id, _ := repo.InsertBread(bread)

	// Request 10, only 3 available → partial
	fulfilled, err := repo.FulfillOrderItem(id, 10)
	if err != nil {
		t.Fatalf("FulfillOrderItem: %v", err)
	}
	if fulfilled != 3 {
		t.Errorf("expected 3 fulfilled (partial), got %d", fulfilled)
	}

	updated, _ := repo.GetBreadByID(id)
	if updated.Quantity != 0 {
		t.Errorf("qty: expected 0, got %d", updated.Quantity)
	}
}

func TestFulfillOrderItem_ZeroStock(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	bread := data.Bread{Name: "Empty", Price: 1.0, Quantity: 0, Type: "Bread", Status: "available"}
	id, _ := repo.InsertBread(bread)

	_, err := repo.FulfillOrderItem(id, 1)
	if !errors.Is(err, data.ErrInsufficientStock) {
		t.Errorf("expected ErrInsufficientStock, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ClaimOutboxMessage — concurrent-safe message claiming with SKIP LOCKED
// ---------------------------------------------------------------------------

func TestClaimOutboxMessage_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	// Insert 3 outbox messages
	for i := 0; i < 3; i++ {
		msg := data.OutboxMessage{
			Payload:   []byte(`{"test":` + string(rune('0'+i)) + `}`),
			Sent:      false,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		}
		// Use direct DB insert to control ID
		fixture.DB.Exec("INSERT INTO outbox (id, payload, sent, created_at) VALUES ($1, $2, $3, $4)",
			i+1, msg.Payload, msg.Sent, msg.CreatedAt)
	}

	// Reset sequence
	fixture.DB.Exec("ALTER SEQUENCE outbox_id_seq RESTART WITH 4")

	// Claim one message
	msg1, err := repo.ClaimOutboxMessage()
	if err != nil {
		t.Fatalf("ClaimOutboxMessage: %v", err)
	}
	if msg1 == nil {
		t.Fatal("expected message, got nil")
	}
	if msg1.ID != 1 {
		t.Errorf("expected ID 1, got %d", msg1.ID)
	}
	if !msg1.Sent {
		t.Error("expected claimed message to have sent=true")
	}

	// Claim second message
	msg2, err := repo.ClaimOutboxMessage()
	if err != nil {
		t.Fatalf("ClaimOutboxMessage #2: %v", err)
	}
	if msg2 == nil || msg2.ID != 2 {
		t.Errorf("expected ID 2, got %v", msg2)
	}

	// Claim third message
	msg3, err := repo.ClaimOutboxMessage()
	if err != nil {
		t.Fatalf("ClaimOutboxMessage #3: %v", err)
	}
	if msg3 == nil || msg3.ID != 3 {
		t.Errorf("expected ID 3, got %v", msg3)
	}

	// No more messages → nil
	msg4, err := repo.ClaimOutboxMessage()
	if err != nil {
		t.Fatalf("ClaimOutboxMessage (empty): %v", err)
	}
	if msg4 != nil {
		t.Errorf("expected nil for empty queue, got ID=%d", msg4.ID)
	}
}

func TestClaimOutboxMessage_ConcurrentSkipLocked(t *testing.T) {
	// Verify that concurrent callers each claim different messages (no duplicates).
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	const numMessages = 20
	for i := 0; i < numMessages; i++ {
		fixture.DB.Exec("INSERT INTO outbox (id, payload, sent, created_at) VALUES ($1, $2, $3, $4)",
			i+1, []byte("msg"), false, time.Now().Add(time.Duration(i)*time.Millisecond))
	}
	fixture.DB.Exec("ALTER SEQUENCE outbox_id_seq RESTART WITH 21")

	const numClaimers = 5
	var mu sync.Mutex
	claimedIDs := make(map[int]bool)
	var wg sync.WaitGroup

	for i := 0; i < numClaimers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each claimer claims as many as it can
			for {
				msg, err := repo.ClaimOutboxMessage()
				if err != nil || msg == nil {
					return
				}
				mu.Lock()
				if claimedIDs[msg.ID] {
					t.Errorf("duplicate claim on message ID %d — SKIP LOCKED failed", msg.ID)
				}
				claimedIDs[msg.ID] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(claimedIDs) != numMessages {
		t.Errorf("expected %d unique claims, got %d", numMessages, len(claimedIDs))
	}
}

// ---------------------------------------------------------------------------
// WaitForOrderNotification — polling fallback path
// ---------------------------------------------------------------------------

func TestWaitForOrderNotification_NoDSN(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	// Do NOT call SetDSN — should fail with DSN error
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := repo.WaitForOrderNotification(ctx, "nonexistent-uuid")
	if err == nil {
		t.Error("expected error when DSN not set")
	}
}

func TestWaitForOrderNotification_PollingFallback(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	repo.SetDSN("postgres://postgres:password@localhost:5432/bakery?sslmode=disable")

	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}
	// Re-seed bread after clearing tables
	if err := dbHelper.SeedTestBread(); err != nil {
		t.Fatalf("seed bread: %v", err)
	}

	customer := data.Customer{Name: "NotifyTest", Email: "notify@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)
	breads, _ := repo.GetAvailableBread()
	if len(breads) == 0 {
		t.Fatal("no bread available after seeding")
	}

	order := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "notify-polling-1",
		Status:       "pending",
		Breads:       []data.Bread{{ID: breads[0].ID, Quantity: 1, Price: breads[0].Price}},
	}
	repo.InsertBuyOrder(order, order.Breads)

	// Start WaitForOrderNotification in a goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- repo.WaitForOrderNotification(ctx, "notify-polling-1")
	}()

	// Simulate the order being processed by updating status and triggering NOTIFY
	time.Sleep(500 * time.Millisecond)
	fixture.DB.Exec(`
		UPDATE buy_order SET status = 'processed'
		WHERE buy_order_uuid = 'notify-polling-1'
	`)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitForOrderNotification: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WaitForOrderNotification timed out — polling fallback did not detect status change")
	}
}

func TestWaitForOrderNotification_ContextCancellation(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	repo.SetDSN("postgres://postgres:password@localhost:5432/bakery?sslmode=disable")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := repo.WaitForOrderNotification(ctx, "never-inserted-uuid")
	if err == nil {
		t.Error("expected context deadline exceeded error")
	}
}

// ---------------------------------------------------------------------------
// Pending Make Orders — insert, claim, update status
// ---------------------------------------------------------------------------

func TestPendingMakeOrder_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}
	if err := dbHelper.SeedTestBread(); err != nil {
		t.Fatalf("seed bread: %v", err)
	}

	t.Run("InsertPendingMakeOrder", func(t *testing.T) {
		order := data.PendingMakeOrder{
			BreadID:           1,
			RequestedQuantity: 20,
			Source:            "auto",
			Status:            "pending",
		}
		id, err := repo.InsertPendingMakeOrder(order)
		if err != nil {
			t.Fatalf("InsertPendingMakeOrder: %v", err)
		}
		if id <= 0 {
			t.Errorf("expected positive ID, got %d", id)
		}
	})

	t.Run("ClaimPendingMakeOrders", func(t *testing.T) {
		// Insert more orders
		for i := 0; i < 3; i++ {
			repo.InsertPendingMakeOrder(data.PendingMakeOrder{
				BreadID:           2,
				RequestedQuantity: 10,
				Source:            "admin",
				Status:            "pending",
			})
		}

		orders, err := repo.ClaimPendingMakeOrders(2)
		if err != nil {
			t.Fatalf("ClaimPendingMakeOrders: %v", err)
		}
		if len(orders) != 2 {
			t.Errorf("expected 2 claimed orders, got %d", len(orders))
		}
		for _, o := range orders {
			if o.Status != "pending" {
				t.Errorf("claimed order status: expected 'pending', got '%s'", o.Status)
			}
		}
	})

	t.Run("UpdatePendingMakeOrderStatus", func(t *testing.T) {
		// Get a pending order to update
		orders, _ := repo.ClaimPendingMakeOrders(1)
		if len(orders) == 0 {
			// Insert one for this test
			id, _ := repo.InsertPendingMakeOrder(data.PendingMakeOrder{
				BreadID: 3, RequestedQuantity: 5, Source: "auto", Status: "pending",
			})
			orders = []data.PendingMakeOrder{{ID: id}}
		}

		err := repo.UpdatePendingMakeOrderStatus(orders[0].ID, "fulfilled")
		if err != nil {
			t.Fatalf("UpdatePendingMakeOrderStatus: %v", err)
		}

		// Verify by querying directly
		var status string
		err = fixture.DB.QueryRow(
			"SELECT status FROM pending_make_orders WHERE id = $1", orders[0].ID,
		).Scan(&status)
		if err != nil {
			t.Fatalf("query pending_make_orders: %v", err)
		}
		if status != "fulfilled" {
			t.Errorf("expected 'fulfilled', got '%s'", status)
		}
	})
}

// ---------------------------------------------------------------------------
// GetBuyOrderByUUID — verify it properly returns ErrNoRows
// ---------------------------------------------------------------------------

func TestGetBuyOrderByUUID_NoRows(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)

	_, err := repo.GetBuyOrderByUUID("nonexistent-uuid-12345")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetOrderTotalCost — NULL handling
// ---------------------------------------------------------------------------

func TestGetOrderTotalCost_NoOrderDetails(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	customer := data.Customer{Name: "CostTest", Email: "cost@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	// Insert buy order with NO order details
	order := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "cost-empty-1",
		Status:       "pending",
		Breads:       nil,
	}
	orderID, _ := repo.InsertBuyOrder(order, nil)

	total, err := repo.GetOrderTotalCost(orderID)
	if err != nil {
		t.Fatalf("GetOrderTotalCost: %v", err)
	}
	if total != 0 {
		t.Errorf("expected total 0 for empty order, got %f", total)
	}
}
