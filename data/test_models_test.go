package data

import (
	"testing"
	"time"
)

// TestPostgresTestRepository exercises every method on the in-memory test stub
// to ensure the data package's test helpers stay in sync with the Repository
// interface and return sensible values.

func repo() *PostgresTestRepository {
	return NewPostgresTestRepository(nil)
}

func TestPostgresTestRepository_New(t *testing.T) {
	r := repo()
	if r == nil {
		t.Fatal("expected non-nil repository")
	}
}

func TestPostgresTestRepository_UpdateOrderStatus(t *testing.T) {
	r := repo()
	if err := r.UpdateOrderStatus("uuid-1", "Processed"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostgresTestRepository_InsertCustomer(t *testing.T) {
	r := repo()
	id, err := r.InsertCustomer(Customer{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}
}

func TestPostgresTestRepository_InsertBread(t *testing.T) {
	r := repo()
	id, err := r.InsertBread(Bread{Name: "Pretzel", Price: 2.49, Quantity: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}
}

func TestPostgresTestRepository_InsertBuyOrder(t *testing.T) {
	r := repo()
	id, err := r.InsertBuyOrder(BuyOrder{}, []Bread{{Name: "Pretzel"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}
}

func TestPostgresTestRepository_InsertBreadMaker(t *testing.T) {
	r := repo()
	id, err := r.InsertBreadMaker(BreadMaker{Name: "Bob"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}
}

func TestPostgresTestRepository_InsertMakeOrder(t *testing.T) {
	r := repo()
	id, err := r.InsertMakeOrder(MakeOrder{}, []Bread{{Name: "Baguette"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}
}

func TestPostgresTestRepository_PasswordMatches(t *testing.T) {
	r := repo()
	ok, err := r.PasswordMatches("password", Customer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected PasswordMatches to return true")
	}
}

func TestPostgresTestRepository_AdjustBreadQuantity(t *testing.T) {
	r := repo()
	ok, err := r.AdjustBreadQuantity(3, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected AdjustBreadQuantity to return true")
	}
}

func TestPostgresTestRepository_AdjustBreadPrice(t *testing.T) {
	r := repo()
	if err := r.AdjustBreadPrice(1, 3.49); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostgresTestRepository_GetAvailableBread(t *testing.T) {
	r := repo()
	breads, err := r.GetAvailableBread()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(breads) == 0 {
		t.Error("expected at least one bread")
	}
	if breads[0].Name != "roll-test" {
		t.Errorf("expected 'roll-test', got %s", breads[0].Name)
	}
}

func TestPostgresTestRepository_GetBreadByID(t *testing.T) {
	r := repo()
	bread, err := r.GetBreadByID(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// stub ignores the arg and always returns bread with ID=1
	if bread.Name != "roll-test" {
		t.Errorf("expected 'roll-test', got %s", bread.Name)
	}
}

func TestPostgresTestRepository_GetMakeOrderByID(t *testing.T) {
	r := repo()
	order, err := r.GetMakeOrderByID(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.BreadMakerID != 1 {
		t.Errorf("expected BreadMakerID=1, got %d", order.BreadMakerID)
	}
}

func TestPostgresTestRepository_GetBuyOrderByID(t *testing.T) {
	r := repo()
	order, err := r.GetBuyOrderByID(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Customer.Name != "John Doe" {
		t.Errorf("expected customer 'John Doe', got %s", order.Customer.Name)
	}
	if len(order.Breads) == 0 {
		t.Error("expected at least one bread in order")
	}
}

func TestPostgresTestRepository_GetAllBuyOrders(t *testing.T) {
	r := repo()
	orders, err := r.GetAllBuyOrders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orders) == 0 {
		t.Error("expected at least one buy order")
	}
	if orders[0].BuyOrderUUID == "" {
		t.Error("expected non-empty BuyOrderUUID")
	}
}

func TestPostgresTestRepository_DeleteOutboxMessage(t *testing.T) {
	r := repo()
	if err := r.DeleteOutboxMessage(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Verify that NewPostgresTestRepository can be created with a nil connection
// (used in tests where no real DB is needed).
func TestNewPostgresTestRepository_NilConn(t *testing.T) {
	r := NewPostgresTestRepository(nil)
	if r == nil {
		t.Fatal("expected non-nil repository with nil connection")
	}
	if r.Conn != nil {
		t.Error("expected nil Conn when constructed with nil")
	}
}

// TestBreadMaker_Fields verifies BreadMaker struct fields are accessible.
func TestBreadMaker_Fields(t *testing.T) {
	bm := BreadMaker{
		ID:        1,
		Name:      "Master Baker",
		Email:     "master@bakery.com",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if bm.Name != "Master Baker" {
		t.Errorf("unexpected Name: %s", bm.Name)
	}
}

// TestMakeOrder_Fields verifies MakeOrder struct fields are accessible.
func TestMakeOrder_Fields(t *testing.T) {
	mo := MakeOrder{
		ID:            1,
		BreadMakerID:  2,
		MakeOrderUUID: "uuid-test",
	}
	if mo.BreadMakerID != 2 {
		t.Errorf("unexpected BreadMakerID: %d", mo.BreadMakerID)
	}
}

// TestOutboxMessage_Fields verifies OutboxMessage struct fields are accessible.
func TestOutboxMessage_Fields(t *testing.T) {
	msg := OutboxMessage{
		ID:        1,
		Payload:   []byte(`{"test":"payload"}`),
		Sent:      false,
		CreatedAt: time.Now(),
	}
	if msg.Sent {
		t.Error("expected Sent=false")
	}
	if string(msg.Payload) != `{"test":"payload"}` {
		t.Errorf("unexpected payload: %s", msg.Payload)
	}
}

// TestDashboardStats_Fields verifies DashboardStats struct.
func TestDashboardStats_Fields(t *testing.T) {
	ds := DashboardStats{
		TotalOrders:      10,
		TotalRevenue:     99.99,
		TotalProducts:    7,
		TotalCustomers:   3,
		TotalBreadMakers: 2,
		LowStockCount:    1,
	}
	if ds.TotalOrders != 10 {
		t.Errorf("unexpected TotalOrders: %d", ds.TotalOrders)
	}
}

// TestInvoice_Fields verifies Invoice and InvoiceItem struct fields.
func TestInvoice_Fields(t *testing.T) {
	paidAt := time.Now()
	inv := Invoice{
		ID:            1,
		BuyOrderID:    2,
		CustomerID:    3,
		InvoiceNumber: "INV-2026-001",
		Subtotal:      10.00,
		Tax:           0.80,
		Total:         10.80,
		Status:        "paid",
		CreatedAt:     time.Now(),
		DueDate:       time.Now().AddDate(0, 0, 30),
		PaidAt:        &paidAt,
		Items: []InvoiceItem{
			{
				ID:        1,
				InvoiceID: 1,
				BreadID:   1,
				BreadName: "Pretzel",
				Quantity:  2,
				UnitPrice: 2.49,
				Total:     4.98,
			},
		},
	}
	if inv.InvoiceNumber != "INV-2026-001" {
		t.Errorf("unexpected InvoiceNumber: %s", inv.InvoiceNumber)
	}
	if len(inv.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(inv.Items))
	}
	if inv.Items[0].BreadName != "Pretzel" {
		t.Errorf("unexpected BreadName: %s", inv.Items[0].BreadName)
	}
}

// TestAdminUser_Fields verifies AdminUser struct.
func TestAdminUser_Fields(t *testing.T) {
	au := AdminUser{
		ID:       1,
		Username: "admin",
		Email:    "admin@bakery.com",
		Role:     "superadmin",
	}
	if au.Role != "superadmin" {
		t.Errorf("unexpected Role: %s", au.Role)
	}
}
