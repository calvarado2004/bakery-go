package data_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	"github.com/calvarado2004/bakery-go/testutils"
)

// ---------------------------------------------------------------------------
// GetAllBuyOrders — verify full structure: header + order_details + breads
// ---------------------------------------------------------------------------

func TestGetAllBuyOrders_Comprehensive(t *testing.T) {
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

	// Insert a customer (seed data has none after clear).
	customer := data.Customer{Name: "AllCust", Email: "allcust@test.com", Password: "pass"}
	customerID, err := repo.InsertCustomer(customer)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	_ = customerID

	// Insert two buy orders with different bread selections.
	order1 := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "getall-buy-1",
		Status:       "pending",
		Breads:       []data.Bread{{ID: 1, Quantity: 3, Price: 6.99}},
	}
	_, err = repo.InsertBuyOrder(order1, order1.Breads)
	if err != nil {
		t.Fatalf("insert order 1: %v", err)
	}

	order2 := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "getall-buy-2",
		Status:       "completed",
		Breads: []data.Bread{
			{ID: 2, Quantity: 2, Price: 3.49},
			{ID: 5, Quantity: 1, Price: 3.99},
		},
	}
	_, err = repo.InsertBuyOrder(order2, order2.Breads)
	if err != nil {
		t.Fatalf("insert order 2: %v", err)
	}

	orders, err := repo.GetAllBuyOrders()
	if err != nil {
		t.Fatalf("GetAllBuyOrders: %v", err)
	}

	// We expect exactly 2 orders.
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}

	// Build a map by UUID for easier checking.
	byUUID := make(map[string]data.BuyOrder)
	for _, o := range orders {
		byUUID[o.BuyOrderUUID] = o
	}

	// Order 1: single bread item.
	o1 := byUUID["getall-buy-1"]
	if o1.CustomerID != customerID {
		t.Errorf("order 1 customer: expected %d, got %d", customerID, o1.CustomerID)
	}
	if len(o1.Breads) != 1 {
		t.Fatalf("order 1 breads: expected 1, got %d", len(o1.Breads))
	}
	if o1.Breads[0].ID != 1 || o1.Breads[0].Quantity != 3 || o1.Breads[0].Price != 6.99 {
		t.Errorf("order 1 bread item: expected {ID:1,Qty:3,Price:6.99}, got %+v", o1.Breads[0])
	}

	// Order 2: multiple bread items.
	o2 := byUUID["getall-buy-2"]
	if len(o2.Breads) != 2 {
		t.Fatalf("order 2 breads: expected 2, got %d", len(o2.Breads))
	}
}

// ---------------------------------------------------------------------------
// GetAllMakeOrders — verify full structure: header + make_order_details + bread
// ---------------------------------------------------------------------------

func TestGetAllMakeOrders_Comprehensive(t *testing.T) {
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

	// Insert a maker (seed data has none after clear).
	maker := data.BreadMaker{Name: "AllMaker", Email: "allmaker@test.com"}
	makerID, err := repo.InsertBreadMaker(maker)
	if err != nil {
		t.Fatalf("insert maker: %v", err)
	}

	// Insert one make order with two bread items.
	order := data.MakeOrder{
		BreadMakerID: makerID,
		MakeOrderUUID: "getall-make-1",
		Breads: []data.Bread{
			{ID: 1, Quantity: 10, Price: 6.99},
			{ID: 3, Quantity: 5, Price: 4.99},
		},
	}
	orderID, err := repo.InsertMakeOrder(order, order.Breads)
	if err != nil {
		t.Fatalf("insert make order: %v", err)
	}

	allOrders, err := repo.GetAllMakeOrders()
	if err != nil {
		t.Fatalf("GetAllMakeOrders: %v", err)
	}

	found := false
	for _, o := range allOrders {
		if o.ID == orderID {
			found = true
			if o.BreadMakerID != makerID {
				t.Errorf("maker ID: expected %d, got %d", makerID, o.BreadMakerID)
			}
			if len(o.Breads) != 2 {
				t.Fatalf("make order breads: expected 2, got %d", len(o.Breads))
			}
			// Verify bread data is loaded via GetBreadByID calls.
			var foundSourdough, foundBaguette bool
			for _, b := range o.Breads {
				if b.Name == "Sourdough" && b.Quantity == 10 {
					foundSourdough = true
				}
				if b.Name == "Baguette" && b.Quantity == 5 {
					foundBaguette = true
				}
			}
			if !foundSourdough {
				t.Error("expected Sourdough with qty 10 in make order details")
			}
			if !foundBaguette {
				t.Error("expected Baguette with qty 5 in make order details")
			}
		}
	}
	if !found {
		t.Error("inserted make order not returned by GetAllMakeOrders")
	}
}

// ---------------------------------------------------------------------------
// GetAllInvoices — verify structure: header + related data
// ---------------------------------------------------------------------------

func TestGetAllInvoices_Comprehensive(t *testing.T) {
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

	// Insert a customer.
	customer := data.Customer{Name: "InvCust", Email: "invcust@test.com", Password: "pass"}
	customerID, err := repo.InsertCustomer(customer)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}

	// Insert a buy order to link the invoice to.
	order := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "getall-inv-1",
		Status:       "completed",
		Breads:       []data.Bread{{ID: 1, Quantity: 2, Price: 6.99}},
	}
	orderID, err := repo.InsertBuyOrder(order, order.Breads)
	if err != nil {
		t.Fatalf("insert buy order: %v", err)
	}

	// Second order for invoice2.
	order2ID, _ := repo.InsertBuyOrder(data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "getall-inv-2",
		Status:       "completed",
		Breads:       []data.Bread{{ID: 2, Quantity: 1, Price: 3.49}},
	}, []data.Bread{{ID: 2, Quantity: 1, Price: 3.49}})

	// Insert two invoices.
	invoice1 := data.Invoice{
		BuyOrderID:    orderID,
		CustomerID:    customerID,
		InvoiceNumber: "INV-GETALL-001",
		Subtotal:      13.98,
		Tax:           1.12,
		Total:         15.10,
		Status:        "paid",
		CreatedAt:     time.Now(),
	}
	_, err = repo.InsertInvoice(invoice1)
	if err != nil {
		t.Fatalf("insert invoice 1: %v", err)
	}

	invoice2 := data.Invoice{
		BuyOrderID:    order2ID,
		CustomerID:    customerID,
		InvoiceNumber: "INV-GETALL-002",
		Subtotal:      5.00,
		Tax:           0.40,
		Total:         5.40,
		Status:        "pending",
		CreatedAt:     time.Now().Add(time.Hour),
	}
	_, err = repo.InsertInvoice(invoice2)
	if err != nil {
		t.Fatalf("insert invoice 2: %v", err)
	}

	invoices, err := repo.GetAllInvoices()
	if err != nil {
		t.Fatalf("GetAllInvoices: %v", err)
	}

	// We should have at least 2 invoices.
	if len(invoices) < 2 {
		t.Fatalf("expected at least 2 invoices, got %d", len(invoices))
	}

	// Build a map by invoice number.
	byNum := make(map[string]data.Invoice)
	for _, inv := range invoices {
		byNum[inv.InvoiceNumber] = inv
	}

	inv1 := byNum["INV-GETALL-001"]
	if inv1.Subtotal != 13.98 {
		t.Errorf("INV-001 subtotal: expected 13.98, got %f", inv1.Subtotal)
	}
	if inv1.Tax != 1.12 {
		t.Errorf("INV-001 tax: expected 1.12, got %f", inv1.Tax)
	}
	if inv1.Total != 15.10 {
		t.Errorf("INV-001 total: expected 15.10, got %f", inv1.Total)
	}
	if inv1.Status != "paid" {
		t.Errorf("INV-001 status: expected 'paid', got '%s'", inv1.Status)
	}
	if inv1.BuyOrderID != orderID {
		t.Errorf("INV-001 buy_order_id: expected %d, got %d", orderID, inv1.BuyOrderID)
	}

	inv2 := byNum["INV-GETALL-002"]
	if inv2.Status != "pending" {
		t.Errorf("INV-002 status: expected 'pending', got '%s'", inv2.Status)
	}
}

// ---------------------------------------------------------------------------
// GetOrderTotalCost — happy path with actual order details
// ---------------------------------------------------------------------------

func TestGetOrderTotalCost_HappyPath(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	customer := data.Customer{Name: "CostTest", Email: "cost2@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	// Insert bread with a known price.
	bread := data.Bread{Name: "CostBread", Price: 4.50, Quantity: 100, Type: "Bread", Status: "available"}
	breadID, _ := repo.InsertBread(bread)

	// Insert a second bread so we can have two detail items.
	bread2 := data.Bread{Name: "CostBread2", Price: 7.00, Quantity: 100, Type: "Bread", Status: "available"}
	bread2ID, _ := repo.InsertBread(bread2)

	// Insert buy order with two detail items.
	order := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "cost-happy-1",
		Status:       "processed",
		Breads: []data.Bread{
			{ID: breadID, Quantity: 3, Price: 4.50}, // 3 × 4.50 = 13.50
			{ID: bread2ID, Quantity: 2, Price: 7.00}, // 2 × 7.00 = 14.00
		},
	}
	orderID, err := repo.InsertBuyOrder(order, order.Breads)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}

	total, err := repo.GetOrderTotalCost(orderID)
	if err != nil {
		t.Fatalf("GetOrderTotalCost: %v", err)
	}

	// Expected: (3 × 4.50) + (2 × 7.00) = 13.50 + 14.00 = 27.50
	expected := 27.50
	if total != expected {
		t.Errorf("expected total %.2f, got %.2f", expected, total)
	}
}

// ---------------------------------------------------------------------------
// GetInvoiceByOrderID — proper setup and assertion
// ---------------------------------------------------------------------------

func TestGetInvoiceByOrderID_ByOrder(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	customer := data.Customer{Name: "InvOrderTest", Email: "invo@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	bread := data.Bread{Name: "InvBread", Price: 5.00, Quantity: 50, Type: "Bread", Status: "available"}
	breadID, _ := repo.InsertBread(bread)

	order := data.BuyOrder{
		CustomerID:   customerID,
		BuyOrderUUID: "inv-order-1",
		Status:       "completed",
		Breads:       []data.Bread{{ID: breadID, Quantity: 1, Price: 5.00}},
	}
	orderID, _ := repo.InsertBuyOrder(order, order.Breads)

	// Insert an invoice linked to this order.
	invoice := data.Invoice{
		BuyOrderID:    orderID,
		CustomerID:    customerID,
		InvoiceNumber: "INV-ORD-TEST",
		Subtotal:      5.00,
		Tax:           0.40,
		Total:         5.40,
		Status:        "pending",
		CreatedAt:     time.Now(),
	}
	_, _ = repo.InsertInvoice(invoice)

	// Fetch by order ID.
	fetched, err := repo.GetInvoiceByOrderID(orderID)
	if err != nil {
		t.Fatalf("GetInvoiceByOrderID: %v", err)
	}
	if fetched.InvoiceNumber != "INV-ORD-TEST" {
		t.Errorf("invoice number: expected 'INV-ORD-TEST', got '%s'", fetched.InvoiceNumber)
	}
	if fetched.Total != 5.40 {
		t.Errorf("total: expected 5.40, got %f", fetched.Total)
	}
	if fetched.BuyOrderID != orderID {
		t.Errorf("buy_order_id: expected %d, got %d", orderID, fetched.BuyOrderID)
	}

	// Non-existent order ID should return ErrNoRows.
	_, err = repo.GetInvoiceByOrderID(99999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for missing order, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetAllCustomers — verify inserted customer is returned with correct fields
// ---------------------------------------------------------------------------

func TestGetAllCustomers_Comprehensive(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	// Insert two distinct customers.
	c1 := data.Customer{Name: "Alice", Email: "alice@all.com", Password: "pass"}
	c2 := data.Customer{Name: "Bob", Email: "bob@all.com", Password: "pass"}
	if _, err := repo.InsertCustomer(c1); err != nil {
		t.Fatalf("insert customer Alice: %v", err)
	}
	if _, err := repo.InsertCustomer(c2); err != nil {
		t.Fatalf("insert customer Bob: %v", err)
	}

	customers, err := repo.GetAllCustomers()
	if err != nil {
		t.Fatalf("GetAllCustomers: %v", err)
	}

	if len(customers) != 2 {
		t.Fatalf("expected exactly 2 customers, got %d", len(customers))
	}

	// Build a map by email for targeted assertions.
	byEmail := make(map[string]data.Customer)
	for _, c := range customers {
		byEmail[c.Email] = c
	}

	if alice, ok := byEmail["alice@all.com"]; ok {
		if alice.Name != "Alice" {
			t.Errorf("Alice name: expected 'Alice', got '%s'", alice.Name)
		}
		if alice.ID <= 0 {
			t.Error("Alice should have a positive ID")
		}
	} else {
		t.Error("alice@all.com not found in GetAllCustomers result")
	}

	if bob, ok := byEmail["bob@all.com"]; ok {
		if bob.Name != "Bob" {
			t.Errorf("Bob name: expected 'Bob', got '%s'", bob.Name)
		}
	} else {
		t.Error("bob@all.com not found in GetAllCustomers result")
	}
}

// ---------------------------------------------------------------------------
// GetAllBreadMakers — verify inserted maker is returned with correct fields
// ---------------------------------------------------------------------------

func TestGetAllBreadMakers_Comprehensive(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	// Insert two distinct makers.
	m1 := data.BreadMaker{Name: "Chef Marco", Email: "marco@bakers.com"}
	m2 := data.BreadMaker{Name: "Chef Yuki", Email: "yuki@bakers.com"}
	_, _ = repo.InsertBreadMaker(m1)
	_, _ = repo.InsertBreadMaker(m2)

	makers, err := repo.GetAllBreadMakers()
	if err != nil {
		t.Fatalf("GetAllBreadMakers: %v", err)
	}

	if len(makers) < 2 {
		t.Fatalf("expected at least 2 makers, got %d", len(makers))
	}

	byEmail := make(map[string]data.BreadMaker)
	for _, m := range makers {
		byEmail[m.Email] = m
	}

	if marco, ok := byEmail["marco@bakers.com"]; ok {
		if marco.Name != "Chef Marco" {
			t.Errorf("Marco name: expected 'Chef Marco', got '%s'", marco.Name)
		}
	} else {
		t.Error("marco@bakers.com not found in GetAllBreadMakers result")
	}

	if yuki, ok := byEmail["yuki@bakers.com"]; ok {
		if yuki.Name != "Chef Yuki" {
			t.Errorf("Yuki name: expected 'Chef Yuki', got '%s'", yuki.Name)
		}
	} else {
		t.Error("yuki@bakers.com not found in GetAllBreadMakers result")
	}
}

// ---------------------------------------------------------------------------
// GetDashboardStats — verify counts and revenue match known data
// ---------------------------------------------------------------------------

func TestGetDashboardStats_Accurate(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("clear tables: %v", err)
	}

	// Insert known quantities.
	customer := data.Customer{Name: "Dash", Email: "dash@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	_, _ = repo.InsertBread(data.Bread{Name: "BreadA", Price: 10.00, Quantity: 50, Type: "Bread", Status: "available"})
	_, _ = repo.InsertBread(data.Bread{Name: "BreadB", Price: 5.00, Quantity: 8, Type: "Bread", Status: "available"}) // low stock
	_, _ = repo.InsertBread(data.Bread{Name: "BreadC", Price: 3.00, Quantity: 2, Type: "Bread", Status: "available"}) // low stock
	_, _ = repo.InsertBreadMaker(data.BreadMaker{Name: "Maker1", Email: "m1@test.com"})

	// Insert 3 buy orders with known totals.
	// Order 1: 2 × 10.00 = 20.00
	_, _ = repo.InsertBuyOrder(data.BuyOrder{
		CustomerID: customerID, BuyOrderUUID: "dash-1", Status: "processed",
		Breads: []data.Bread{{ID: 1, Quantity: 2, Price: 10.00}},
	}, []data.Bread{{ID: 1, Quantity: 2, Price: 10.00}})

	// Order 2: 1 × 5.00 = 5.00
	_, _ = repo.InsertBuyOrder(data.BuyOrder{
		CustomerID: customerID, BuyOrderUUID: "dash-2", Status: "pending",
		Breads: []data.Bread{{ID: 2, Quantity: 1, Price: 5.00}},
	}, []data.Bread{{ID: 2, Quantity: 1, Price: 5.00}})

	// Order 3: 5 × 3.00 = 15.00
	_, _ = repo.InsertBuyOrder(data.BuyOrder{
		CustomerID: customerID, BuyOrderUUID: "dash-3", Status: "completed",
		Breads: []data.Bread{{ID: 3, Quantity: 5, Price: 3.00}},
	}, []data.Bread{{ID: 3, Quantity: 5, Price: 3.00}})

	stats, err := repo.GetDashboardStats()
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}

	// 1 customer (only the one we inserted — ClearAllTables removed seed data)
	if stats.TotalCustomers != 1 {
		t.Errorf("TotalCustomers: expected 1, got %d", stats.TotalCustomers)
	}
	// 1 bread maker (only the one we inserted)
	if stats.TotalBreadMakers != 1 {
		t.Errorf("TotalBreadMakers: expected 1, got %d", stats.TotalBreadMakers)
	}
	// 3 products
	if stats.TotalProducts != 3 {
		t.Errorf("TotalProducts: expected 3, got %d", stats.TotalProducts)
	}
	// 3 orders
	if stats.TotalOrders != 3 {
		t.Errorf("TotalOrders: expected 3, got %d", stats.TotalOrders)
	}
	// Revenue: (2×10) + (1×5) + (5×3) = 20 + 5 + 15 = 40.00
	if stats.TotalRevenue != 40.00 {
		t.Errorf("TotalRevenue: expected 40.00, got %.2f", stats.TotalRevenue)
	}
	// Low stock: BreadB (8) + BreadC (2) = 2 items with qty < 10
	if stats.LowStockCount != 2 {
		t.Errorf("LowStockCount: expected 2, got %d", stats.LowStockCount)
	}
}

// ---------------------------------------------------------------------------
// GetCustomerOrders — verify orders include their detail breads
// ---------------------------------------------------------------------------

func TestGetCustomerOrders_WithDetails(t *testing.T) {
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

	customer := data.Customer{Name: "CustOrd", Email: "custord@test.com", Password: "pass"}
	customerID, _ := repo.InsertCustomer(customer)

	// Insert two orders for this customer.
	order1, _ := repo.InsertBuyOrder(data.BuyOrder{
		CustomerID: customerID, BuyOrderUUID: "cust-1", Status: "pending",
		Breads: []data.Bread{{ID: 1, Quantity: 2, Price: 6.99}},
	}, []data.Bread{{ID: 1, Quantity: 2, Price: 6.99}})

	order2, _ := repo.InsertBuyOrder(data.BuyOrder{
		CustomerID: customerID, BuyOrderUUID: "cust-2", Status: "completed",
		Breads: []data.Bread{
			{ID: 2, Quantity: 1, Price: 3.49},
			{ID: 4, Quantity: 3, Price: 12.99},
		},
	}, []data.Bread{{ID: 2, Quantity: 1, Price: 3.49}, {ID: 4, Quantity: 3, Price: 12.99}})

	orders, err := repo.GetCustomerOrders(customerID)
	if err != nil {
		t.Fatalf("GetCustomerOrders: %v", err)
	}

	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}

	// Orders are returned ORDER BY id DESC, so order2 comes first.
	if orders[0].ID != order2 {
		t.Errorf("first order ID: expected %d (order2), got %d", order2, orders[0].ID)
	}
	if len(orders[0].Breads) != 2 {
		t.Errorf("order2 breads: expected 2, got %d", len(orders[0].Breads))
	}

	if orders[1].ID != order1 {
		t.Errorf("second order ID: expected %d (order1), got %d", order1, orders[1].ID)
	}
	if len(orders[1].Breads) != 1 {
		t.Errorf("order1 breads: expected 1, got %d", len(orders[1].Breads))
	}
}

// ---------------------------------------------------------------------------
// GetMakerOrders — verify maker orders include their bread details
// ---------------------------------------------------------------------------

func TestGetMakerOrders_WithDetails(t *testing.T) {
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

	maker := data.BreadMaker{Name: "MakerOrd", Email: "makerord@test.com"}
	makerID, _ := repo.InsertBreadMaker(maker)

	// Insert two make orders for this maker.
	order1, _ := repo.InsertMakeOrder(data.MakeOrder{
		BreadMakerID: makerID, MakeOrderUUID: "maker-1",
		Breads: []data.Bread{{ID: 1, Quantity: 5, Price: 6.99}},
	}, []data.Bread{{ID: 1, Quantity: 5, Price: 6.99}})

	order2, _ := repo.InsertMakeOrder(data.MakeOrder{
		BreadMakerID: makerID, MakeOrderUUID: "maker-2",
		Breads: []data.Bread{{ID: 3, Quantity: 10, Price: 4.99}},
	}, []data.Bread{{ID: 3, Quantity: 10, Price: 4.99}})

	orders, err := repo.GetMakerOrders(makerID)
	if err != nil {
		t.Fatalf("GetMakerOrders: %v", err)
	}

	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}

	// Orders are ORDER BY id DESC.
	orderIDs := make(map[int]bool)
	for _, o := range orders {
		orderIDs[o.ID] = true
		if len(o.Breads) == 0 {
			t.Errorf("maker order %d has no breads loaded", o.ID)
		}
	}
	if !orderIDs[order2] {
		t.Error("order2 not found in GetMakerOrders result")
	}
	if !orderIDs[order1] {
		t.Error("order1 not found in GetMakerOrders result")
	}
}
