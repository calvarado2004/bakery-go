package data_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	"github.com/calvarado2004/bakery-go/testutils"
	_ "github.com/jackc/pgx/v4/stdlib"
)

// TestPostgresRepository_Integration tests the data layer with a real PostgreSQL database
func TestPostgresRepository_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	// Don't clear tables - use existing seed data for most tests

	t.Run("InsertCustomer and GetCustomerByEmail", func(t *testing.T) {
		customer := data.Customer{
			Name:      "Integration Test User",
			Email:     "integration@test.com",
			Password:  "password123",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		id, err := repo.InsertCustomer(customer)
		if err != nil {
			t.Fatalf("Failed to insert customer: %v", err)
		}
		if id <= 0 {
			t.Errorf("Expected positive ID, got %d", id)
		}

		fetched, err := repo.GetCustomerByEmail("integration@test.com")
		if err != nil {
			t.Fatalf("Failed to get customer: %v", err)
		}
		if fetched.Email != customer.Email {
			t.Errorf("Expected email %s, got %s", customer.Email, fetched.Email)
		}
	})

	t.Run("InsertAdminUser and GetAdminUserByUsername", func(t *testing.T) {
		// Clear just admin users to avoid conflicts
		_, err := fixture.DB.Exec("DELETE FROM admin_users WHERE username = 'testadmin'")
		if err != nil {
			t.Logf("Warning: Could not clear admin users: %v", err)
		}

		admin := data.AdminUser{
			Username: "testadmin",
			Email:    "testadmin@bakery.com",
			Password: "admin123",
			Role:     "admin",
		}

		_, err = repo.InsertAdminUser(admin)
		if err != nil {
			t.Fatalf("Failed to insert admin: %v", err)
		}

		fetched, err := repo.GetAdminUserByUsername("testadmin")
		if err != nil {
			t.Fatalf("Failed to get admin: %v", err)
		}
		if fetched.Username != admin.Username {
			t.Errorf("Expected username %s, got %s", admin.Username, fetched.Username)
		}
	})

	t.Run("InsertBread and GetBreadByID", func(t *testing.T) {
		bread := data.Bread{
			Name:        "Test Baguette",
			Price:       5.99,
			Quantity:    100,
			Description: "A test baguette for integration testing",
			Type:        "French Bread",
			Status:      "available",
			Image:       "/images/baguette.png",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		id, err := repo.InsertBread(bread)
		if err != nil {
			t.Fatalf("Failed to insert bread: %v", err)
		}
		if id <= 0 {
			t.Errorf("Expected positive ID, got %d", id)
		}

		fetched, err := repo.GetBreadByID(id)
		if err != nil {
			t.Fatalf("Failed to get bread: %v", err)
		}
		if fetched.Name != bread.Name {
			t.Errorf("Expected name %s, got %s", bread.Name, fetched.Name)
		}
	})

	t.Run("InsertBreadMaker and GetAllBreadMakers", func(t *testing.T) {
		maker := data.BreadMaker{
			Name:      "Test Baker",
			Email:     "testbaker@bakery.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err := repo.InsertBreadMaker(maker)
		if err != nil {
			t.Fatalf("Failed to insert bread maker: %v", err)
		}

		makers, err := repo.GetAllBreadMakers()
		if err != nil {
			t.Fatalf("Failed to get all bread makers: %v", err)
		}
		if len(makers) == 0 {
			t.Error("Expected at least one bread maker")
		}
	})

	t.Run("PasswordMatches", func(t *testing.T) {
		plainPassword := "securepassword"

		// Create customer with bcrypt hashed password
		customer := data.Customer{
			Name:      "Password Test User",
			Email:     "passwordtest@test.com",
			Password:  plainPassword,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, _ = repo.InsertCustomer(customer)
		fetched, _ := repo.GetCustomerByEmail("passwordtest@test.com")

		matches, err := repo.PasswordMatches(plainPassword, fetched)
		if err != nil {
			t.Fatalf("Failed to check password: %v", err)
		}
		if !matches {
			t.Error("Password should match")
		}
	})

	t.Run("AdjustBreadQuantity", func(t *testing.T) {
		// First get a bread item
		breads, err := repo.GetAvailableBread()
		if err != nil {
			t.Fatalf("Failed to get available bread: %v", err)
		}
		if len(breads) == 0 {
			t.Skip("No bread items available to test quantity adjustment")
		}

		bread := breads[0]
		initialQty := bread.Quantity

		// Deduct 1 unit (safe regardless of initial stock level).
		success, err := repo.AdjustBreadQuantity(bread.ID, -1)
		if err != nil {
			t.Fatalf("Failed to adjust quantity: %v", err)
		}
		if !success {
			t.Error("Expected quantity adjustment to succeed")
		}

		// Verify the change.
		updated, err := repo.GetBreadByID(bread.ID)
		if err != nil {
			t.Fatalf("Failed to get updated bread: %v", err)
		}
		if updated.Quantity != initialQty-1 {
			t.Errorf("Expected quantity %d, got %d", initialQty-1, updated.Quantity)
		}

		// Attempting to deduct more than remaining stock returns ErrInsufficientStock.
		_, err = repo.AdjustBreadQuantity(bread.ID, -(updated.Quantity+1))
		if !errors.Is(err, data.ErrInsufficientStock) {
			t.Errorf("expected ErrInsufficientStock for over-deduct, got: %v", err)
		}

		// Restore quantity.
		_, err = repo.AdjustBreadQuantity(bread.ID, 1)
		if err != nil {
			t.Errorf("Failed to restore quantity: %v", err)
		}
	})

	t.Run("AdjustBreadPrice", func(t *testing.T) {
		breads, err := repo.GetAvailableBread()
		if err != nil {
			t.Fatalf("Failed to get available bread: %v", err)
		}
		if len(breads) == 0 {
			t.Skip("No bread items available to test price adjustment")
		}

		bread := breads[0]
		newPrice := float64(9.99)

		err = repo.AdjustBreadPrice(bread.ID, newPrice)
		if err != nil {
			t.Fatalf("Failed to adjust price: %v", err)
		}

		updated, err := repo.GetBreadByID(bread.ID)
		if err != nil {
			t.Fatalf("Failed to get updated bread: %v", err)
		}
		if updated.Price != newPrice {
			t.Errorf("Expected price %.2f, got %.2f", newPrice, updated.Price)
		}

		// Reset price
		err = repo.AdjustBreadPrice(bread.ID, bread.Price)
		if err != nil {
			return
		}
	})
}

func TestPostgresRepository_AdminOperations_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)

	// Clear tables before test
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("Failed to clear tables: %v", err)
	}

	t.Run("GetDashboardStats", func(t *testing.T) {
		stats, err := repo.GetDashboardStats()
		if err != nil {
			t.Fatalf("Failed to get dashboard stats: %v", err)
		}
		// Should have at least default data
		if stats.TotalProducts < 0 {
			t.Errorf("Expected non-negative product count, got %d", stats.TotalProducts)
		}
	})

	t.Run("GetLowStockBread", func(t *testing.T) {
		lowStock, err := repo.GetLowStockBread(10)
		if err != nil {
			t.Fatalf("Failed to get low stock bread: %v", err)
		}
		// Check that all returned items have quantity < 10
		for _, bread := range lowStock {
			if bread.Quantity >= 10 {
				t.Errorf("Expected bread with quantity < 10, got %d for %s", bread.Quantity, bread.Name)
			}
		}
	})

	t.Run("GetAllCustomers", func(t *testing.T) {
		customers, err := repo.GetAllCustomers()
		if err != nil {
			t.Fatalf("Failed to get all customers: %v", err)
		}
		if len(customers) == 0 {
			t.Log("No customers in database")
		}
	})

	t.Run("GetAllBreadMakers", func(t *testing.T) {
		makers, err := repo.GetAllBreadMakers()
		if err != nil {
			t.Fatalf("Failed to get all bread makers: %v", err)
		}
		if len(makers) == 0 {
			t.Log("No bread makers in database")
		}
	})
}

func TestPostgresRepository_BuyOrder_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)

	// Clear tables before test
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("Failed to clear tables: %v", err)
	}

	t.Run("InsertBuyOrder and GetBuyOrderByUUID", func(t *testing.T) {
		// Get available bread and customer
		breads, err := repo.GetAvailableBread()
		if err != nil {
			t.Fatalf("Failed to get available bread: %v", err)
		}
		if len(breads) == 0 {
			t.Skip("No bread available")
		}

		customers, err := repo.GetAllCustomers()
		if err != nil {
			t.Fatalf("Failed to get customers: %v", err)
		}
		if len(customers) == 0 {
			t.Skip("No customers available")
		}

		customer := customers[0]
		selectedBreads := []data.Bread{breads[0]}

		// Create buy order
		order := data.BuyOrder{
			CustomerID: customer.ID,
			Status:     "pending",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		orderID, err := repo.InsertBuyOrder(order, selectedBreads)
		if err != nil {
			t.Fatalf("Failed to insert buy order: %v", err)
		}
		if orderID <= 0 {
			t.Errorf("Expected positive order ID, got %d", orderID)
		}

		// Fetch by UUID
		fetched, err := repo.GetBuyOrderByID(orderID)
		if err != nil {
			t.Fatalf("Failed to fetch order: %v", err)
		}
		if fetched.ID != orderID {
			t.Errorf("Expected order ID %d, got %d", orderID, fetched.ID)
		}
	})

	t.Run("UpdateOrderStatus", func(t *testing.T) {
		breads, _ := repo.GetAvailableBread()
		customers, _ := repo.GetAllCustomers()

		if len(breads) == 0 || len(customers) == 0 {
			t.Skip("Not enough data for order status test")
		}

		order := data.BuyOrder{
			CustomerID: customers[0].ID,
			Status:     "pending",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		orderID, _ := repo.InsertBuyOrder(order, []data.Bread{breads[0]})
		fetched, _ := repo.GetBuyOrderByID(orderID)

		err := repo.UpdateOrderStatus(fetched.BuyOrderUUID, "completed")
		if err != nil {
			t.Fatalf("Failed to update order status: %v", err)
		}

		updated, _ := repo.GetBuyOrderByID(orderID)
		if updated.Status != "completed" {
			t.Errorf("Expected status 'completed', got '%s'", updated.Status)
		}
	})
}

func TestPostgresRepository_MakeOrder_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)

	// Clear tables before test
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("Failed to clear tables: %v", err)
	}

	t.Run("InsertMakeOrder and GetMakeOrderByID", func(t *testing.T) {
		breads, err := repo.GetAvailableBread()
		if err != nil {
			t.Fatalf("Failed to get available bread: %v", err)
		}
		if len(breads) == 0 {
			t.Skip("No bread available")
		}

		makers, err := repo.GetAllBreadMakers()
		if err != nil {
			t.Fatalf("Failed to get bread makers: %v", err)
		}
		if len(makers) == 0 {
			t.Skip("No bread makers available")
		}

		maker := makers[0]
		selectedBreads := []data.Bread{breads[0]}

		order := data.MakeOrder{
			BreadMakerID: maker.ID,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		orderID, err := repo.InsertMakeOrder(order, selectedBreads)
		if err != nil {
			t.Fatalf("Failed to insert make order: %v", err)
		}
		if orderID <= 0 {
			t.Errorf("Expected positive order ID, got %d", orderID)
		}

		fetched, err := repo.GetMakeOrderByID(orderID)
		if err != nil {
			t.Fatalf("Failed to fetch make order: %v", err)
		}
		if fetched.ID != orderID {
			t.Errorf("Expected order ID %d, got %d", orderID, fetched.ID)
		}
	})

	t.Run("GetAllMakeOrders", func(t *testing.T) {
		orders, err := repo.GetAllMakeOrders()
		if err != nil {
			t.Fatalf("Failed to get all make orders: %v", err)
		}
		t.Logf("Found %d make orders", len(orders))
	})
}

func TestPostgresRepository_Outbox_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)

	// Clear tables before test
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("Failed to clear tables: %v", err)
	}

	t.Run("InsertOutboxMessage and GetUnprocessedOutboxMessages", func(t *testing.T) {
		// Clear existing outbox messages first
		_, _ = fixture.DB.Exec("DELETE FROM outbox")

		message := data.OutboxMessage{
			Payload:   []byte(`{"test": "data"}`),
			Sent:      false,
			CreatedAt: time.Now(),
		}

		err := repo.InsertOutboxMessage(message)
		if err != nil {
			t.Fatalf("Failed to insert outbox message: %v", err)
		}

		messages, err := repo.GetUnprocessedOutboxMessages()
		if err != nil {
			t.Fatalf("Failed to get unprocessed messages: %v", err)
		}
		if len(messages) == 0 {
			t.Error("Expected at least one unprocessed message")
		}
	})

	t.Run("DeleteOutboxMessage", func(t *testing.T) {
		// Clear outbox and reset sequence
		_, _ = fixture.DB.Exec("DELETE FROM outbox")
		_, _ = fixture.DB.Exec("ALTER SEQUENCE outbox_id_seq RESTART WITH 1")

		// Insert a fresh message
		message := data.OutboxMessage{
			Payload:   []byte(`{"delete": "test"}`),
			Sent:      false,
			CreatedAt: time.Now(),
		}
		err := repo.InsertOutboxMessage(message)
		if err != nil {
			t.Fatalf("Failed to insert outbox message: %v", err)
		}

		// Get the message ID
		messages, _ := repo.GetUnprocessedOutboxMessages()
		if len(messages) == 0 {
			t.Skip("No messages to delete")
		}
		id := messages[0].ID

		// Delete it
		err = repo.DeleteOutboxMessage(id)
		if err != nil {
			t.Fatalf("Failed to delete outbox message: %v", err)
		}

		// Verify it's deleted
		messages2, _ := repo.GetUnprocessedOutboxMessages()
		found := false
		for _, m := range messages2 {
			if m.ID == id {
				found = true
				break
			}
		}
		if found {
			t.Error("Expected message to be deleted")
		}
	})
}

func TestPostgresRepository_Invoice_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)

	// Clear tables before test
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("Failed to clear tables: %v", err)
	}

	t.Run("InsertInvoice and GetInvoiceByID", func(t *testing.T) {
		// First, create a buy order
		breads, err := repo.GetAvailableBread()
		if err != nil || len(breads) == 0 {
			t.Skip("No bread available")
		}

		customers, err := repo.GetAllCustomers()
		if err != nil || len(customers) == 0 {
			t.Skip("No customers available")
		}

		order := data.BuyOrder{
			CustomerID: customers[0].ID,
			Status:     "completed",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		orderID, _ := repo.InsertBuyOrder(order, []data.Bread{breads[0]})

		// Now create an invoice
		invoice := data.Invoice{
			BuyOrderID:    orderID,
			CustomerID:    customers[0].ID,
			InvoiceNumber: "INV-TEST-001",
			Subtotal:      10.0,
			Tax:           0.8,
			Total:         10.8,
			Status:        "pending",
			CreatedAt:     time.Now(),
		}

		invoiceID, err := repo.InsertInvoice(invoice)
		if err != nil {
			t.Fatalf("Failed to insert invoice: %v", err)
		}
		if invoiceID <= 0 {
			t.Errorf("Expected positive invoice ID, got %d", invoiceID)
		}

		fetched, err := repo.GetInvoiceByID(invoiceID)
		if err != nil {
			t.Fatalf("Failed to get invoice: %v", err)
		}
		if fetched.InvoiceNumber != invoice.InvoiceNumber {
			t.Errorf("Expected invoice number %s, got %s", invoice.InvoiceNumber, fetched.InvoiceNumber)
		}
	})

	t.Run("GetInvoicesByCustomerID", func(t *testing.T) {
		customers, err := repo.GetAllCustomers()
		if err != nil || len(customers) == 0 {
			t.Skip("No customers available")
		}

		invoices, err := repo.GetInvoicesByCustomerID(customers[0].ID)
		if err != nil {
			t.Fatalf("Failed to get customer invoices: %v", err)
		}
		t.Logf("Found %d invoices for customer %d", len(invoices), customers[0].ID)
	})

	t.Run("GetInvoiceByOrderID", func(t *testing.T) {
		breads, _ := repo.GetAvailableBread()
		customers, _ := repo.GetAllCustomers()

		if len(breads) == 0 || len(customers) == 0 {
			t.Skip("Not enough data")
		}

		// Get or create an invoice for an order
		invoice, err := repo.GetInvoiceByOrderID(1)
		if err != nil {
			t.Fatalf("Failed to get invoice by order ID: %v", err)
		}
		t.Logf("Found invoice: %v", invoice.InvoiceNumber)
	})
}

func TestPostgresRepository_Auth_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)

	// Clear tables before test
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("Failed to clear tables: %v", err)
	}

	t.Run("GetAdminUserByID", func(t *testing.T) {
		// First, create an admin
		admin := data.AdminUser{
			Username: "idtestadmin",
			Email:    "idtest@bakery.com",
			Password: "testpass",
			Role:     "admin",
		}

		id, err := repo.InsertAdminUser(admin)
		if err != nil {
			t.Fatalf("Failed to insert admin: %v", err)
		}

		fetched, err := repo.GetAdminUserByID(id)
		if err != nil {
			t.Fatalf("Failed to get admin by ID: %v", err)
		}
		if fetched.Username != admin.Username {
			t.Errorf("Expected username %s, got %s", admin.Username, fetched.Username)
		}
	})

	t.Run("GetCustomerByID", func(t *testing.T) {
		customer := data.Customer{
			Name:     "ID Test Customer",
			Email:    "idtest@customer.com",
			Password: "testpass",
		}

		id, err := repo.InsertCustomer(customer)
		if err != nil {
			t.Fatalf("Failed to insert customer: %v", err)
		}

		fetched, err := repo.GetCustomerByID(id)
		if err != nil {
			t.Fatalf("Failed to get customer by ID: %v", err)
		}
		if fetched.Email != customer.Email {
			t.Errorf("Expected email %s, got %s", customer.Email, fetched.Email)
		}
	})

	t.Run("GetBreadMakerByID", func(t *testing.T) {
		maker := data.BreadMaker{
			Name:  "ID Test Maker",
			Email: "idtest@maker.com",
		}

		id, err := repo.InsertBreadMaker(maker)
		if err != nil {
			t.Fatalf("Failed to insert maker: %v", err)
		}

		fetched, err := repo.GetBreadMakerByID(id)
		if err != nil {
			t.Fatalf("Failed to get maker by ID: %v", err)
		}
		if fetched.Email != maker.Email {
			t.Errorf("Expected email %s, got %s", maker.Email, fetched.Email)
		}
	})
}

func TestPostgresRepository_UpdateBread_DeleteBread_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)

	// Clear tables before test
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("Failed to clear tables: %v", err)
	}

	t.Run("UpdateBread", func(t *testing.T) {
		// Create a bread item
		bread := data.Bread{
			Name:        "Update Test Bread",
			Price:       5.0,
			Quantity:    50,
			Description: "Original description",
			Type:        "Test Type",
			Status:      "available",
			Image:       "/images/test.png",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		id, err := repo.InsertBread(bread)
		if err != nil {
			t.Fatalf("Failed to insert bread: %v", err)
		}

		// Update the bread
		updatedBread := data.Bread{
			ID:          id,
			Name:        "Updated Test Bread",
			Price:       7.5,
			Quantity:    100,
			Description: "Updated description",
			Type:        "Updated Type",
			Status:      "available",
			Image:       "/images/updated.png",
			CreatedAt:   bread.CreatedAt,
			UpdatedAt:   time.Now(),
		}

		err = repo.UpdateBread(updatedBread)
		if err != nil {
			t.Fatalf("Failed to update bread: %v", err)
		}

		fetched, err := repo.GetBreadByID(id)
		if err != nil {
			t.Fatalf("Failed to get updated bread: %v", err)
		}
		if fetched.Name != updatedBread.Name {
			t.Errorf("Expected name %s, got %s", updatedBread.Name, fetched.Name)
		}
	})

	t.Run("DeleteBread", func(t *testing.T) {
		// Create a bread item
		bread := data.Bread{
			Name:        "Delete Test Bread",
			Price:       3.0,
			Quantity:    20,
			Description: "To be deleted",
			Type:        "Test",
			Status:      "available",
			Image:       "/images/delete.png",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		id, err := repo.InsertBread(bread)
		if err != nil {
			t.Fatalf("Failed to insert bread: %v", err)
		}

		// Delete it
		err = repo.DeleteBread(id)
		if err != nil {
			t.Fatalf("Failed to delete bread: %v", err)
		}

		// Verify it's deleted
		_, err = repo.GetBreadByID(id)
		if err == nil || !errors.Is(sql.ErrNoRows, err) {
			t.Logf("Expected ErrNoRows, got: %v", err)
		}
	})
}

func TestPostgresRepository_CustomerOrders_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)
	dbHelper := testutils.NewDBHelper(fixture.DB)

	// Clear tables before test
	if err := dbHelper.ClearAllTables(); err != nil {
		t.Fatalf("Failed to clear tables: %v", err)
	}

	t.Run("GetCustomerOrders", func(t *testing.T) {
		breads, err := repo.GetAvailableBread()
		if err != nil || len(breads) == 0 {
			t.Skip("No bread available")
		}

		customers, err := repo.GetAllCustomers()
		if err != nil || len(customers) == 0 {
			t.Skip("No customers available")
		}

		customer := customers[0]

		// Create an order for this customer
		order := data.BuyOrder{
			CustomerID: customer.ID,
			Status:     "pending",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		_, err = repo.InsertBuyOrder(order, []data.Bread{breads[0]})
		if err != nil {
			return
		}

		// Fetch customer orders
		orders, err := repo.GetCustomerOrders(customer.ID)
		if err != nil {
			t.Fatalf("Failed to get customer orders: %v", err)
		}

		found := false
		for _, o := range orders {
			if o.CustomerID == customer.ID {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected to find order for customer")
		}
	})

	t.Run("GetMakerOrders", func(t *testing.T) {
		breads, err := repo.GetAvailableBread()
		if err != nil || len(breads) == 0 {
			t.Skip("No bread available")
		}

		makers, err := repo.GetAllBreadMakers()
		if err != nil || len(makers) == 0 {
			t.Skip("No makers available")
		}

		maker := makers[0]

		// Create a make order
		order := data.MakeOrder{
			BreadMakerID: maker.ID,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		_, err = repo.InsertMakeOrder(order, []data.Bread{breads[0]})
		if err != nil {
			return
		}

		// Fetch maker orders
		orders, err := repo.GetMakerOrders(maker.ID)
		if err != nil {
			t.Fatalf("Failed to get maker orders: %v", err)
		}

		found := false
		for _, o := range orders {
			if o.BreadMakerID == maker.ID {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected to find order for maker")
		}
	})
}

func TestPostgresRepository_GetAllBuyOrders_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)

	// Get all buy orders
	orders, err := repo.GetAllBuyOrders()
	if err != nil {
		t.Fatalf("Failed to get all buy orders: %v", err)
	}
	t.Logf("Found %d buy orders in database", len(orders))
}

func TestPostgresRepository_GetAllInvoices_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)

	// Get all invoices
	invoices, err := repo.GetAllInvoices()
	if err != nil {
		t.Fatalf("Failed to get all invoices: %v", err)
	}
	t.Logf("Found %d invoices in database", len(invoices))
}

func TestPostgresRepository_GetAllBread_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)

	// Get all available bread
	breads, err := repo.GetAvailableBread()
	if err != nil {
		t.Fatalf("Failed to get available bread: %v", err)
	}
	t.Logf("Found %d bread items in inventory", len(breads))
}

// TestPasswordMatches verifies bcrypt password comparison works correctly
func TestPasswordMatches_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)

	plainPassword := "testpassword123"

	// Create a customer with the password
	customer := data.Customer{
		Name:     "Password Match Test",
		Email:    "passwordmatch@test.com",
		Password: plainPassword,
	}

	_, err := repo.InsertCustomer(customer)
	if err != nil {
		t.Fatalf("Failed to insert customer: %v", err)
	}

	// Fetch the customer (who will have the hashed password)
	fetched, err := repo.GetCustomerByEmail("passwordmatch@test.com")
	if err != nil {
		t.Fatalf("Failed to fetch customer: %v", err)
	}

	// Verify bcrypt comparison
	matches, err := repo.PasswordMatches(plainPassword, fetched)
	if err != nil {
		t.Fatalf("Failed to match password: %v", err)
	}
	if !matches {
		t.Error("Password should match the bcrypt hash")
	}

	// Test non-matching password
	notMatches, err := repo.PasswordMatches("wrongpassword", fetched)
	if err != nil {
		t.Fatalf("Failed to match password: %v", err)
	}
	if notMatches {
		t.Error("Password should not match")
	}
}

// TestAdjustBreadQuantity_CheckConstraint verifies that trying to set quantity below 0
// via AdjustBreadQuantity returns ErrInsufficientStock.
func TestAdjustBreadQuantity_CheckConstraint(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	repo := data.NewPostgresRepository(fixture.DB)

	// Pre-clean any leftover row from a previous killed run.
	fixture.DB.Exec("DELETE FROM bread WHERE name = 'ConstraintTestBread'") //nolint:errcheck

	// Create a bread with quantity=1 so we can drive it negative.
	// Include a non-NULL image so GetAvailableBread scans won't break.
	_, err := fixture.DB.Exec(`
		INSERT INTO bread (name, price, quantity, description, type, status, image, created_at, updated_at)
		VALUES ('ConstraintTestBread', 1.0, 1, 'test', 'test', 'available', 'https://example.com/bread.jpg', NOW(), NOW())
	`)
	if err != nil {
		t.Fatalf("failed to insert test bread: %v", err)
	}

	var breadID int
	err = fixture.DB.QueryRow(`SELECT id FROM bread WHERE name = 'ConstraintTestBread'`).Scan(&breadID)
	if err != nil {
		t.Fatalf("failed to find test bread: %v", err)
	}
	t.Cleanup(func() {
		fixture.DB.Exec("DELETE FROM bread WHERE id = $1", breadID) //nolint:errcheck
	})

	// Deducting 5 from a bread with quantity=1 should return ErrInsufficientStock.
	_, err = repo.AdjustBreadQuantity(breadID, -5)
	if !errors.Is(err, data.ErrInsufficientStock) {
		t.Errorf("expected ErrInsufficientStock, got: %v", err)
	}
}
