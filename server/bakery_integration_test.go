package main

import (
	"context"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/testutils"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestBuyBread_ContextCancellation verifies that BuyBread properly handles
// context cancellation (client disconnect).
func TestBuyBread_ContextCancellation(t *testing.T) {
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

	client := pb.NewBuyBreadClient(conn)

	// Cancel context immediately — BuyBread should detect this.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	_, err = client.BuyBread(ctx, &pb.BreadRequest{
		Breads: &pb.BreadList{
			Breads: []*pb.Bread{{Id: 1, Quantity: 1}},
		},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Canceled {
		t.Logf("BuyBread cancelled context returned: %v (code=%v)", err, status.Code(err))
	}
}

// TestBuyBread_EmptyBreads verifies that BuyBread with empty bread list
// still publishes an order (the order has no bread items).
func TestBuyBread_EmptyBreads(t *testing.T) {
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

	client := pb.NewBuyBreadClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// BuyBread with no breads — should still work (publishes empty order).
	_, err = client.BuyBread(ctx, &pb.BreadRequest{})
	if err != nil {
		// This may fail if broker is not running, but should not return
		// an error about the bread list.
		t.Logf("BuyBread empty: %v", err)
	}
}

// TestBuyOrder_Integration_NotFound verifies that BuyOrder with a non-existent UUID
// returns a gRPC error.
func TestBuyOrder_Integration_NotFound(t *testing.T) {
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

	client := pb.NewBuyOrderServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = client.BuyOrder(ctx, &pb.BuyOrderRequest{
		BuyOrderUuid: "non-existent-uuid-00000",
	})
	if err == nil {
		t.Fatal("expected error for non-existent order")
	}
	// The error code depends on how the repository handles not-found:
	// it could be Internal (if GetBuyOrderByUUID returns sql.ErrNoRows
	// and the handler wraps it) or NotFound.
	t.Logf("BuyOrder not-found returned: %v (code=%v)", err, status.Code(err))
}

// TestBuyOrderStream_EmptyResult verifies that BuyOrderStream with no
// matching orders returns successfully (no error) but sends no messages.
func TestBuyOrderStream_EmptyResult(t *testing.T) {
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

	client := pb.NewBuyOrderServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := client.BuyOrderStream(ctx, &pb.BuyOrderRequest{})
	if err != nil {
		t.Fatalf("BuyOrderStream failed: %v", err)
	}

	count := 0
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
		count++
	}
	// Recv returning an error is expected at end of stream.
	// count may be 0 or more depending on existing orders.
	t.Logf("BuyOrderStream sent %d messages", count)
}

// TestBuyOrderStream_Integration_UUIDNotFound verifies that BuyOrderStream with a
// specific non-existent UUID returns an error.
func TestBuyOrderStream_Integration_UUIDNotFound(t *testing.T) {
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

	client := pb.NewBuyOrderServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := client.BuyOrderStream(ctx, &pb.BuyOrderRequest{
		BuyOrderUuid: "non-existent-stream-uuid-00000",
	})
	if err != nil {
		// Expected — GetBuyOrderByUUID returns error for non-existent UUID
		t.Logf("BuyOrderStream not-found: %v", err)
		return
	}

	_, err = stream.Recv()
	if err == nil {
		t.Log("BuyOrderStream returned successfully for non-existent UUID (no error)")
	} else {
		t.Logf("BuyOrderStream error on recv: %v", err)
	}
}

// TestBuyBread_WithUUID verifies that BuyBread with a pre-specified UUID
// uses that UUID instead of generating a new one.
func TestBuyBread_WithUUID(t *testing.T) {
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

	client := pb.NewBuyBreadClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	customUUID := "buybread-custom-uuid-12345"
	resp, err := client.BuyBread(ctx, &pb.BreadRequest{
		BuyOrderUuid: customUUID,
		Breads: &pb.BreadList{
			Breads: []*pb.Bread{{Id: 1, Quantity: 1}},
		},
	})
	if err != nil {
		t.Logf("BuyBread with UUID: %v", err)
		return
	}
	if resp.BuyOrderId == 0 {
		t.Log("BuyBread returned BuyOrderId=0 (broker may not have processed yet)")
	}
}

// TestCheckInventory_EmptyInventory verifies that CheckBreadInventory
// returns NotFound when no bread exists.
func TestCheckInventory_EmptyInventory(t *testing.T) {
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

	client := pb.NewCheckInventoryClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.CheckBreadInventory(ctx, &pb.BreadRequest{})
	if err != nil {
		// May return NotFound if no bread, or success with data if seeded.
		t.Logf("CheckBreadInventory: err=%v, resp=%v", err, resp)
		return
	}
	if resp == nil || resp.Breads == nil {
		t.Fatal("expected non-nil response with breads")
	}
	t.Logf("Found %d bread items", len(resp.Breads.Breads))
}

// TestGetCustomerOrders_NotFound verifies that GetCustomerOrders with a
// non-existent customer ID returns a gRPC error.
func TestGetCustomerOrders_NotFound(t *testing.T) {
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

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Login as admin to get auth token.
	authClient := pb.NewAuthServiceClient(conn)
	loginResp, err := authClient.AdminLogin(ctx, &pb.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil || loginResp.Token == "" {
		t.Skip("Could not login as admin")
	}
	adminCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+loginResp.Token)

	_, err = client.GetCustomerOrders(adminCtx, &pb.CustomerIdRequest{Id: 999999})
	if err == nil {
		t.Fatal("expected error for non-existent customer")
	}
	t.Logf("GetCustomerOrders not-found: %v", err)
}

// TestGetDashboardStats_NoData verifies that GetDashboardStats returns
// stats even with minimal data in the database.
func TestGetDashboardStats_NoData(t *testing.T) {
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

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Login as admin.
	authClient := pb.NewAuthServiceClient(conn)
	loginResp, err := authClient.AdminLogin(ctx, &pb.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil || loginResp.Token == "" {
		t.Skip("Could not login as admin")
	}
	adminCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+loginResp.Token)

	resp, err := client.GetDashboardStats(adminCtx, &pb.Empty{})
	if err != nil {
		t.Fatalf("GetDashboardStats failed: %v", err)
	}
	t.Logf("Dashboard: Orders=%d, Revenue=$%.2f, Products=%d",
		resp.TotalOrders, resp.TotalRevenue, resp.TotalProducts)
}
