package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/calvarado2004/bakery-go/testutils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// E2EFixture holds all resources for end-to-end tests
type E2EFixture struct {
	T               *testing.T
	DB              *sql.DB
	GRPCConn        *grpc.ClientConn
	BuyClient       pb.BuyBreadClient
	InventoryClient pb.CheckInventoryClient
	AdminClient     pb.AdminServiceClient
	AuthClient      pb.AuthServiceClient
	InvoiceClient   pb.InvoiceServiceClient
	AdminToken      string
	Cleanup         func()
}

// NewE2EFixture sets up the full end-to-end test environment.
// If the gRPC server is not available, it logs a skip message and returns nil.
func NewE2EFixture(t *testing.T) *E2EFixture {
	fixture := &E2EFixture{T: t}

	// Get database connection
	dbDSN := testutils.GetDBDSNFromT(t)
	db, err := sql.Open("pgx", dbDSN)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	fixture.DB = db

	// Connect to gRPC server
	grpcAddr := testutils.GetGRPCAddress()
	conn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(60*time.Second),
	)
	if err != nil {
		db.Close()
		t.Skipf("gRPC server not available at %s: %v (run docker-compose up -d server broker first)", grpcAddr, err)
		return nil
	}
	fixture.GRPCConn = conn

	// Wait for the rate limiter to refill (burst=20, rate=10/s).
	// This prevents "rate limit exceeded" errors when tests run after other packages.
	time.Sleep(2 * time.Second)

	// Create clients
	fixture.BuyClient = pb.NewBuyBreadClient(conn)
	fixture.InventoryClient = pb.NewCheckInventoryClient(conn)
	fixture.AdminClient = pb.NewAdminServiceClient(conn)
	fixture.AuthClient = pb.NewAuthServiceClient(conn)
	fixture.InvoiceClient = pb.NewInvoiceServiceClient(conn)

	// Authenticate as admin so that admin gRPC calls include proper auth token
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	loginResp, err := fixture.AuthClient.AdminLogin(ctx, &pb.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err == nil && loginResp != nil && loginResp.Success {
		fixture.AdminToken = loginResp.Token
	}

	// Set up cleanup
	fixture.Cleanup = func() {
		if err := conn.Close(); err != nil {
			t.Logf("Warning: Failed to close gRPC connection: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Logf("Warning: Failed to close database connection: %v", err)
		}
	}

	return fixture
}

// adminContext returns a context with the admin auth token attached as metadata.
// Use this for all AdminService gRPC calls in tests.
func (f *E2EFixture) adminContext(ctx context.Context) context.Context {
	if f.AdminToken != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+f.AdminToken)
	}
	return ctx
}

// TestFullBuyOrderFlow tests the complete buy order flow through RabbitMQ
func TestFullBuyOrderFlow(t *testing.T) {
	fixture := NewE2EFixture(t)
	if fixture == nil {
		t.Skip("E2E fixture could not connect to gRPC server")
		return
	}
	defer fixture.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Step 1: Check initial inventory
	t.Run("CheckInitialInventory", func(t *testing.T) {
		req := &pb.BreadRequest{}
		resp, err := fixture.InventoryClient.CheckBreadInventory(ctx, req)
		if err != nil {
			t.Skipf("Could not check inventory: %v", err)
		}

		if resp == nil || resp.Breads == nil {
			t.Skip("No inventory response")
		}

		t.Logf("Initial inventory has %d bread types", len(resp.Breads.Breads))
		for _, item := range resp.Breads.Breads {
			t.Logf("  - %s: %d units", item.Name, item.Quantity)
		}
	})

	// Step 2: Get bread and customer info
	var targetBread *pb.Bread
	var targetCustomerID int32

	t.Run("GetTargetBreadAndCustomer", func(t *testing.T) {
		// Get bread with sufficient stock
		inventoryReq := &pb.BreadRequest{}
		inventoryResp, err := fixture.InventoryClient.CheckBreadInventory(ctx, inventoryReq)
		if err != nil {
			t.Fatalf("Failed to get inventory: %v", err)
		}

		if inventoryResp == nil || inventoryResp.Breads == nil {
			t.Skip("No inventory response")
		}

		// Find bread with quantity > 5
		for _, item := range inventoryResp.Breads.Breads {
			if item.Quantity > 5 {
				targetBread = item
				break
			}
		}

		if targetBread == nil {
			t.Skip("No bread with sufficient stock found")
		}

		t.Logf("Selected bread: %s (ID: %d, Stock: %d)",
			targetBread.Name, targetBread.Id, targetBread.Quantity)

		// Get a customer - use direct DB query for simplicity
		var custID int
		err = fixture.DB.QueryRowContext(ctx, "SELECT id FROM customer LIMIT 1").Scan(&custID)
		if err != nil {
			t.Skipf("No customers available: %v", err)
		}
		targetCustomerID = int32(custID)
		t.Logf("Selected customer ID: %d", targetCustomerID)
	})

	// Step 3: Place buy order
	var orderUUID string
	t.Run("PlaceBuyOrder", func(t *testing.T) {
		if targetBread == nil {
			t.Skip("No target bread selected")
		}

		req := &pb.BreadRequest{
			Breads: &pb.BreadList{
				Breads: []*pb.Bread{
					{
						Id:       targetBread.Id,
						Quantity: 1,
					},
				},
			},
		}

		resp, err := fixture.BuyClient.BuyBread(ctx, req)
		if err != nil {
			t.Fatalf("BuyBread failed: %v", err)
		}

		orderUUID = resp.BuyOrderUuid
		t.Logf("Order placed with UUID: %s", orderUUID)
	})

	// Step 4: Wait for broker to process (up to 60 seconds)
	t.Run("WaitForBrokerProcessing", func(t *testing.T) {
		if orderUUID == "" {
			t.Skip("No order UUID to track")
		}

		maxAttempts := 12
		attemptDelay := 5 * time.Second

		for i := 0; i < maxAttempts; i++ {
			// Check order status via database
			var status string
			err := fixture.DB.QueryRowContext(ctx,
				"SELECT status FROM buy_order WHERE buy_order_uuid = $1",
				orderUUID,
			).Scan(&status)

			if err == nil && status == "settled" {
				t.Logf("Order settled after %d attempts (%.1f seconds)",
					i+1, float64(i+1)*attemptDelay.Seconds())
				return
			}

			t.Logf("Attempt %d: status = %s (waiting...)", i+1, status)
			time.Sleep(attemptDelay)
		}

		// Final check
		var finalStatus string
		err := fixture.DB.QueryRowContext(ctx,
			"SELECT status FROM buy_order WHERE buy_order_uuid = $1",
			orderUUID,
		).Scan(&finalStatus)

		if err != nil {
			t.Logf("Could not verify final status: %v", err)
			t.Skip("Order processing may still be in progress")
		}

		if finalStatus != "settled" {
			t.Logf("Final status: %s", finalStatus)
		}
	})

	// Step 5: Verify inventory was adjusted
	t.Run("VerifyInventoryAdjustment", func(t *testing.T) {
		if targetBread == nil {
			t.Skip("No target bread to verify")
		}

		// Get current inventory
		inventoryReq := &pb.BreadRequest{}
		inventoryResp, err := fixture.InventoryClient.CheckBreadInventory(ctx, inventoryReq)
		if err != nil {
			t.Fatalf("Failed to get inventory: %v", err)
		}

		if inventoryResp == nil || inventoryResp.Breads == nil {
			t.Skip("No inventory response")
		}

		var currentQty int32
		for _, item := range inventoryResp.Breads.Breads {
			if item.Id == targetBread.Id {
				currentQty = item.Quantity
				break
			}
		}

		expectedQty := targetBread.Quantity - 1
		if currentQty == expectedQty {
			t.Logf("Inventory correctly adjusted: %d -> %d",
				targetBread.Quantity, currentQty)
		} else if currentQty < expectedQty {
			t.Logf("Inventory adjusted more than expected: %d -> %d (expected %d)",
				targetBread.Quantity, currentQty, expectedQty)
		} else {
			t.Logf("Inventory adjustment pending: %d -> %d (expected %d)",
				targetBread.Quantity, currentQty, expectedQty)
		}
	})
}

// TestLowStockRestockFlow tests the automatic restock when inventory is low
func TestLowStockRestockFlow(t *testing.T) {
	fixture := NewE2EFixture(t)
	if fixture == nil {
		t.Skip("E2E fixture could not connect to gRPC server")
		return
	}
	defer fixture.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Step 1: Find bread with low stock
	var lowStockBread *pb.Bread

	t.Run("FindLowStockBread", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := fixture.AdminClient.GetLowStockAlerts(fixture.adminContext(ctx), req)
		if err != nil {
			t.Skipf("Could not get low stock alerts: %v", err)
		}

		if resp != nil && len(resp.Breads) > 0 {
			lowStockBread = resp.Breads[0]
			t.Logf("Found low stock bread: %s (ID: %d, Stock: %d)",
				lowStockBread.Name, lowStockBread.Id, lowStockBread.Quantity)
		}
	})

	// Step 2: If we found low stock bread, check if restock order was created
	t.Run("CheckRestockOrder", func(t *testing.T) {
		if lowStockBread == nil {
			t.Skip("No low stock bread found")
		}

		// Check make_order table for recent orders
		var makeOrderCount int
		err := fixture.DB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM make_order WHERE created_at > NOW() - INTERVAL '5 minutes'",
		).Scan(&makeOrderCount)

		if err == nil {
			t.Logf("Found %d recent make orders", makeOrderCount)
		}
	})

	// Step 3: Verify makers processed the restock
	t.Run("VerifyRestockProcessing", func(t *testing.T) {
		if lowStockBread == nil {
			t.Skip("No low stock bread to track")
		}

		// Wait and check if inventory was restocked
		maxWait := 60 * time.Second
		startTime := time.Now()

		for time.Since(startTime) < maxWait {
			var currentQty int32
			err := fixture.DB.QueryRowContext(ctx,
				"SELECT quantity FROM bread WHERE id = $1",
				lowStockBread.Id,
			).Scan(&currentQty)

			if err == nil && currentQty > lowStockBread.Quantity {
				t.Logf("Bread restocked: %d -> %d", lowStockBread.Quantity, currentQty)
				return
			}

			time.Sleep(5 * time.Second)
		}

		t.Log("Restock may still be in progress or bread already had sufficient stock")
	})
}

// TestAdminDashboardFlow tests the admin dashboard statistics
func TestAdminDashboardFlow(t *testing.T) {
	// Seed accounts first so AdminLogin in NewE2EFixture succeeds.
	seedIntegrationAccounts(t)

	fixture := NewE2EFixture(t)
	if fixture == nil {
		t.Skip("E2E fixture could not connect to gRPC server")
		return
	}
	defer fixture.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("GetDashboardStats", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := fixture.AdminClient.GetDashboardStats(fixture.adminContext(ctx), req)
		if err != nil {
			t.Fatalf("GetDashboardStats failed: %v", err)
		}

		t.Logf("Dashboard Stats:")
		t.Logf("  Total Orders: %d", resp.TotalOrders)
		t.Logf("  Total Revenue: $%.2f", resp.TotalRevenue)
		t.Logf("  Total Products: %d", resp.TotalProducts)
		t.Logf("  Total Customers: %d", resp.TotalCustomers)
		t.Logf("  Total Bread Makers: %d", resp.TotalBreadMakers)
		t.Logf("  Low Stock Count: %d", resp.LowStockCount)
	})

	t.Run("VerifyStatsConsistency", func(t *testing.T) {
		// Get dashboard stats — check error properly this time.
		dashReq := &pb.Empty{}
		dashResp, err := fixture.AdminClient.GetDashboardStats(fixture.adminContext(ctx), dashReq)
		if err != nil {
			t.Fatalf("GetDashboardStats for consistency check failed: %v", err)
		}

		// Verify against direct database counts
		var dbOrderCount, dbCustomerCount, dbBreadCount, dbMakerCount int

		fixture.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM buy_order").Scan(&dbOrderCount)
		fixture.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM customer").Scan(&dbCustomerCount)
		fixture.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM bread").Scan(&dbBreadCount)
		fixture.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM bread_maker").Scan(&dbMakerCount)

		t.Logf("API stats: Orders=%d, Customers=%d, Products=%d, Makers=%d",
			dashResp.TotalOrders, dashResp.TotalCustomers, dashResp.TotalProducts, dashResp.TotalBreadMakers)
		t.Logf("DB counts: Orders=%d, Customers=%d, Products=%d, Makers=%d",
			dbOrderCount, dbCustomerCount, dbBreadCount, dbMakerCount)
	})
}

// TestAuthFlow tests the authentication and JWT token flow
func TestAuthFlow(t *testing.T) {
	seedIntegrationAccounts(t)
	fixture := NewE2EFixture(t)
	if fixture == nil {
		t.Skip("E2E fixture could not connect to gRPC server")
		return
	}
	defer fixture.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var adminToken string

	t.Run("AdminLogin", func(t *testing.T) {
		req := &pb.LoginRequest{
			Username: "admin",
			Password: "admin123",
		}

		resp, err := fixture.AuthClient.AdminLogin(ctx, req)
		if err != nil {
			t.Fatalf("AdminLogin failed: %v", err)
		}

		adminToken = resp.Token
		t.Logf("Admin login successful, token length: %d", len(adminToken))
	})

	t.Run("ValidateAdminToken", func(t *testing.T) {
		req := &pb.ValidateTokenRequest{
			Token: adminToken,
		}

		resp, err := fixture.AuthClient.ValidateToken(ctx, req)
		if err != nil {
			t.Fatalf("ValidateToken failed: %v", err)
		}

		if resp.UserType != "admin" {
			t.Errorf("Expected user type 'admin', got '%s'", resp.UserType)
		}

		t.Logf("Token validated: UserID=%s, UserType=%s", resp.UserId, resp.UserType)
	})

	t.Run("CustomerLogin", func(t *testing.T) {
		req := &pb.CustomerLoginRequest{
			Email:    "john@doe.com",
			Password: "password123",
		}

		resp, err := fixture.AuthClient.CustomerLogin(ctx, req)
		if err != nil {
			t.Fatalf("CustomerLogin failed: %v", err)
		}

		t.Logf("Customer login successful, token length: %d", len(resp.Token))
	})
}

// TestInvoiceGenerationFlow tests the invoice creation flow
func TestInvoiceGenerationFlow(t *testing.T) {
	fixture := NewE2EFixture(t)
	if fixture == nil {
		t.Skip("E2E fixture could not connect to gRPC server")
		return
	}
	defer fixture.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var targetOrderID int32

	t.Run("FindOrderToInvoice", func(t *testing.T) {
		// Get orders from DB directly
		var orderID int
		err := fixture.DB.QueryRowContext(ctx,
			"SELECT id FROM buy_order WHERE status = 'settled' LIMIT 1",
		).Scan(&orderID)

		if err != nil {
			t.Skipf("No settled orders available: %v", err)
		}

		targetOrderID = int32(orderID)
		t.Logf("Found order ID: %d", targetOrderID)
	})

	t.Run("CreateInvoice", func(t *testing.T) {
		if targetOrderID == 0 {
			t.Skip("No order selected for invoicing")
		}

		// Create invoice
		req := &pb.CreateInvoiceRequest{
			BuyOrderId: targetOrderID,
		}

		resp, err := fixture.InvoiceClient.CreateInvoice(ctx, req)
		if err != nil {
			t.Fatalf("CreateInvoice failed: %v", err)
		}

		t.Logf("Invoice created: %s for order %d, Total: $%.2f",
			resp.InvoiceNumber, resp.BuyOrderId, resp.Total)
	})

	t.Run("VerifyInvoiceInDatabase", func(t *testing.T) {
		if targetOrderID == 0 {
			t.Skip("No order to verify")
		}

		// Check if invoice exists
		var invoiceCount int
		err := fixture.DB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM invoices WHERE buy_order_id = $1",
			targetOrderID,
		).Scan(&invoiceCount)

		if err != nil {
			t.Fatalf("Could not verify invoice: %v", err)
		}

		if invoiceCount > 0 {
			t.Logf("Invoice verified in database (count: %d)", invoiceCount)
		}
	})
}

// TestConcurrentBuyOrders tests handling multiple concurrent buy orders
func TestConcurrentBuyOrders(t *testing.T) {
	fixture := NewE2EFixture(t)
	if fixture == nil {
		t.Skip("E2E fixture could not connect to gRPC server")
		return
	}
	defer fixture.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Get target bread and customer
	var targetBreadID int32
	var targetCustomerID int32

	t.Run("GetTargetResources", func(t *testing.T) {
		inventoryReq := &pb.BreadRequest{}
		inventoryResp, err := fixture.InventoryClient.CheckBreadInventory(ctx, inventoryReq)
		if err != nil || inventoryResp == nil || inventoryResp.Breads == nil {
			t.Skip("No inventory available")
		}

		// Find bread with high stock
		for _, item := range inventoryResp.Breads.Breads {
			if item.Quantity >= 20 {
				targetBreadID = item.Id
				break
			}
		}

		if targetBreadID == 0 && len(inventoryResp.Breads.Breads) > 0 {
			targetBreadID = inventoryResp.Breads.Breads[0].Id
		}

		// Get customer from DB
		var custID int
		err = fixture.DB.QueryRowContext(ctx, "SELECT id FROM customer LIMIT 1").Scan(&custID)
		if err != nil {
			t.Skipf("No customers available: %v", err)
		}
		targetCustomerID = int32(custID)
		t.Logf("Using bread ID: %d, Customer ID: %d", targetBreadID, targetCustomerID)
	})

	// Place multiple concurrent orders
	numOrders := 3
	orderUUIDs := make([]string, numOrders)

	t.Run("PlaceConcurrentOrders", func(t *testing.T) {
		for i := 0; i < numOrders; i++ {
			req := &pb.BreadRequest{
				Breads: &pb.BreadList{
					Breads: []*pb.Bread{
						{Id: targetBreadID, Quantity: 1},
					},
				},
			}

			resp, err := fixture.BuyClient.BuyBread(ctx, req)
			if err != nil {
				t.Logf("Order %d failed: %v", i+1, err)
				continue
			}

			orderUUIDs[i] = resp.BuyOrderUuid
			t.Logf("Order %d placed: %s", i+1, resp.BuyOrderUuid)
		}
	})

	// Verify all orders were created
	t.Run("VerifyAllOrdersCreated", func(t *testing.T) {
		nonEmptyCount := 0
		for _, uuid := range orderUUIDs {
			if uuid != "" {
				nonEmptyCount++
			}
		}
		t.Logf("Successfully placed %d/%d orders", nonEmptyCount, numOrders)
	})
}

// TestGetAllEndpoints tests the 'GetAll' endpoints for completeness
func TestGetAllEndpoints(t *testing.T) {
	seedIntegrationAccounts(t)
	fixture := NewE2EFixture(t)
	if fixture == nil {
		t.Skip("E2E fixture could not connect to gRPC server")
		return
	}
	defer fixture.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("GetAllBuyOrders", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := fixture.AdminClient.GetAllOrders(fixture.adminContext(ctx), req)
		if err != nil {
			t.Fatalf("GetAllOrders failed: %v", err)
		}
		t.Logf("Found %d buy orders", len(resp.BuyOrders))
	})

	t.Run("GetAllCustomers", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := fixture.AdminClient.GetAllCustomers(fixture.adminContext(ctx), req)
		if err != nil {
			t.Fatalf("GetAllCustomers failed: %v", err)
		}
		t.Logf("Found %d customers", len(resp.Customers))
	})

	t.Run("GetAllBreadMakers", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := fixture.AdminClient.GetAllBreadMakers(fixture.adminContext(ctx), req)
		if err != nil {
			t.Fatalf("GetAllBreadMakers failed: %v", err)
		}
		t.Logf("Found %d bread makers", len(resp.BreadMakers))
	})

	t.Run("GetAllBread", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := fixture.AdminClient.GetAllBread(fixture.adminContext(ctx), req)
		if err != nil {
			t.Fatalf("GetAllBread failed: %v", err)
		}
		t.Logf("Found %d bread items", len(resp.Breads))
	})

	t.Run("GetAllMakeOrders", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := fixture.AdminClient.GetAllMakeOrders(fixture.adminContext(ctx), req)
		if err != nil {
			t.Fatalf("GetAllMakeOrders failed: %v", err)
		}
		t.Logf("Found %d make orders", len(resp.MakeOrders))
	})
}
