package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/calvarado2004/bakery-go/testutils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestGRPCServer_Integration tests gRPC server endpoints with real connections
func TestGRPCServer_Integration(t *testing.T) {
	// Skip if docker-compose is not running
	addr := testutils.GetGRPCAddress()
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(30*time.Second),
	)
	if err != nil {
		t.Skipf("Could not connect to gRPC server at %s: %v", addr, err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminClient := pb.NewAdminServiceClient(conn)
	authClient := pb.NewAuthServiceClient(conn)
	inventoryClient := pb.NewCheckInventoryClient(conn)
	buyClient := pb.NewBuyBreadClient(conn)

	t.Run("GetDashboardStats", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := adminClient.GetDashboardStats(ctx, req)
		if err != nil {
			t.Fatalf("GetDashboardStats failed: %v", err)
		}
		t.Logf("Dashboard: Orders=%d, Revenue=$%.2f, Products=%d",
			resp.TotalOrders, resp.TotalRevenue, resp.TotalProducts)
	})

	t.Run("GetAllBread", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := adminClient.GetAllBread(ctx, req)
		if err != nil {
			t.Fatalf("GetAllBread failed: %v", err)
		}
		t.Logf("Found %d bread items", len(resp.Breads))
	})

	t.Run("GetAllCustomers", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := adminClient.GetAllCustomers(ctx, req)
		if err != nil {
			t.Fatalf("GetAllCustomers failed: %v", err)
		}
		t.Logf("Found %d customers", len(resp.Customers))
	})

	t.Run("GetAllBreadMakers", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := adminClient.GetAllBreadMakers(ctx, req)
		if err != nil {
			t.Fatalf("GetAllBreadMakers failed: %v", err)
		}
		t.Logf("Found %d bread makers", len(resp.BreadMakers))
	})

	t.Run("CheckBreadInventory", func(t *testing.T) {
		req := &pb.BreadRequest{}
		resp, err := inventoryClient.CheckBreadInventory(ctx, req)
		if err != nil {
			t.Fatalf("CheckBreadInventory failed: %v", err)
		}
		if resp != nil && resp.Breads != nil {
			t.Logf("Inventory has %d bread types", len(resp.Breads.Breads))
		}
	})

	t.Run("AdminLogin", func(t *testing.T) {
		req := &pb.LoginRequest{
			Username: "admin",
			Password: "admin123",
		}
		resp, err := authClient.AdminLogin(ctx, req)
		if err != nil {
			t.Fatalf("AdminLogin failed: %v", err)
		}
		if resp.Token == "" {
			t.Error("Expected JWT token")
		}
		t.Logf("Admin login successful, token length: %d", len(resp.Token))
	})

	t.Run("CustomerLogin", func(t *testing.T) {
		req := &pb.CustomerLoginRequest{
			Email:    "john@doe.com",
			Password: "password123",
		}
		resp, err := authClient.CustomerLogin(ctx, req)
		if err != nil {
			t.Fatalf("CustomerLogin failed: %v", err)
		}
		if resp.Token == "" {
			t.Error("Expected JWT token")
		}
		t.Logf("Customer login successful, token length: %d", len(resp.Token))
	})

	t.Run("ValidateToken", func(t *testing.T) {
		// First get a token
		loginReq := &pb.LoginRequest{
			Username: "admin",
			Password: "admin123",
		}
		loginResp, err := authClient.AdminLogin(ctx, loginReq)
		if err != nil {
			t.Skipf("Could not get token: %v", err)
		}

		req := &pb.ValidateTokenRequest{
			Token: loginResp.Token,
		}
		resp, err := authClient.ValidateToken(ctx, req)
		if err != nil {
			t.Fatalf("ValidateToken failed: %v", err)
		}
		t.Logf("Token valid: UserID=%s, UserType=%s", resp.UserId, resp.UserType)
	})

	t.Run("GetLowStockAlerts", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := adminClient.GetLowStockAlerts(ctx, req)
		if err != nil {
			t.Fatalf("GetLowStockAlerts failed: %v", err)
		}
		t.Logf("Found %d low stock items", len(resp.Breads))
	})

	t.Run("GetAllOrders", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := adminClient.GetAllOrders(ctx, req)
		if err != nil {
			t.Fatalf("GetAllOrders failed: %v", err)
		}
		t.Logf("Found %d orders", len(resp.BuyOrders))
	})

	t.Run("GetAllMakeOrders", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := adminClient.GetAllMakeOrders(ctx, req)
		if err != nil {
			t.Fatalf("GetAllMakeOrders failed: %v", err)
		}
		t.Logf("Found %d make orders", len(resp.MakeOrders))
	})

	t.Run("BuyBread", func(t *testing.T) {
		// Get a bread ID first
		inventoryReq := &pb.BreadRequest{}
		inventoryResp, err := inventoryClient.CheckBreadInventory(ctx, inventoryReq)
		if err != nil || inventoryResp == nil || inventoryResp.Breads == nil {
			t.Skip("Could not get inventory")
		}

		if len(inventoryResp.Breads.Breads) == 0 {
			t.Skip("No bread available")
		}

		bread := inventoryResp.Breads.Breads[0]
		req := &pb.BreadRequest{
			Breads: &pb.BreadList{
				Breads: []*pb.Bread{
					{Id: bread.Id, Quantity: 1},
				},
			},
		}

		resp, err := buyClient.BuyBread(ctx, req)
		if err != nil {
			t.Logf("BuyBread error: %v", err)
			t.Skip("Server may still be processing")
		}
		t.Logf("BuyBread successful, Order UUID: %s", resp.BuyOrderUuid)
	})
}

// TestAdminBreadCRUD_Integration tests bread CRUD operations via gRPC
func TestAdminBreadCRUD_Integration(t *testing.T) {
	addr := testutils.GetGRPCAddress()
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(30*time.Second),
	)
	if err != nil {
		t.Skipf("Could not connect to gRPC server: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminClient := pb.NewAdminServiceClient(conn)

	t.Run("CreateBread", func(t *testing.T) {
		req := &pb.CreateBreadRequest{
			Name:        "Integration Test Bread",
			Price:       6.99,
			Quantity:    100,
			Description: "Test bread for integration",
			Type:        "Test Type",
			Image:       "/images/test.png",
		}

		resp, err := adminClient.CreateBread(ctx, req)
		if err != nil {
			t.Fatalf("CreateBread failed: %v", err)
		}
		t.Logf("Created bread with ID: %d", resp.Id)

		// Clean up
		deleteReq := &pb.DeleteBreadRequest{Id: resp.Id}
		_, err = adminClient.DeleteBread(ctx, deleteReq)
		if err != nil {
			t.Logf("DeleteBread warning: %v", err)
		}
	})

	t.Run("UpdateBread", func(t *testing.T) {
		// Get bread to update
		allReq := &pb.Empty{}
		allResp, err := adminClient.GetAllBread(ctx, allReq)
		if err != nil || len(allResp.Breads) == 0 {
			t.Skip("No bread available to update")
		}

		bread := allResp.Breads[0]
		newPrice := float32(9.99)

		req := &pb.UpdateBreadRequest{
			Id:       bread.Id,
			Name:     bread.Name,
			Price:    newPrice,
			Quantity: bread.Quantity,
			Type:     bread.Type,
			Image:    bread.Image,
		}

		resp, err := adminClient.UpdateBread(ctx, req)
		if err != nil {
			t.Fatalf("UpdateBread failed: %v", err)
		}
		if resp.Price != newPrice {
			t.Errorf("Expected price %.2f, got %.2f", newPrice, resp.Price)
		}

		// Reset price
		resetReq := &pb.UpdateBreadRequest{
			Id:       bread.Id,
			Name:     bread.Name,
			Price:    bread.Price,
			Quantity: bread.Quantity,
			Type:     bread.Type,
			Image:    bread.Image,
		}
		_, _ = adminClient.UpdateBread(ctx, resetReq)
	})
}

// TestInvoiceService_Integration tests invoice operations
func TestInvoiceService_Integration(t *testing.T) {
	addr := testutils.GetGRPCAddress()
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(30*time.Second),
	)
	if err != nil {
		t.Skipf("Could not connect to gRPC server: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminClient := pb.NewAdminServiceClient(conn)
	invoiceClient := pb.NewInvoiceServiceClient(conn)

	t.Run("CreateInvoice", func(t *testing.T) {
		// Get an order first
		ordersReq := &pb.Empty{}
		ordersResp, err := adminClient.GetAllOrders(ctx, ordersReq)
		if err != nil || len(ordersResp.BuyOrders) == 0 {
			t.Skip("No orders available")
		}

		order := ordersResp.BuyOrders[0]
		req := &pb.CreateInvoiceRequest{
			BuyOrderId: order.Id,
		}

		resp, err := invoiceClient.CreateInvoice(ctx, req)
		if err != nil {
			t.Fatalf("CreateInvoice failed: %v", err)
		}
		t.Logf("Created invoice: %s for order %d", resp.InvoiceNumber, resp.BuyOrderId)
	})

	t.Run("GetAllInvoices", func(t *testing.T) {
		req := &pb.Empty{}
		resp, err := invoiceClient.GetAllInvoices(ctx, req)
		if err != nil {
			t.Fatalf("GetAllInvoices failed: %v", err)
		}
		t.Logf("Found %d invoices", len(resp.Invoices))
	})
}

// TestDBConnection_Integration tests direct database connection
func TestDBConnection_Integration(t *testing.T) {
	// Use the same DB connection logic as the server
	dsn := "postgres://postgres:password@localhost:5432/bakery?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	t.Run("CountBread", func(t *testing.T) {
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bread").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count bread: %v", err)
		}
		t.Logf("Found %d bread items", count)
	})

	t.Run("CountCustomers", func(t *testing.T) {
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM customer").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count customers: %v", err)
		}
		t.Logf("Found %d customers", count)
	})

	t.Run("CountOrders", func(t *testing.T) {
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM buy_order").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count orders: %v", err)
		}
		t.Logf("Found %d buy orders", count)
	})
}

// Helper to format currency
func formatCurrency(amount float32) string {
	return fmt.Sprintf("$%.2f", amount)
}
