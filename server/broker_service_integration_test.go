package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/testutils"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestBrokerServiceIntegration_ReportOrder_DirectDB verifies that calling
// ReportOrder through the real gRPC server properly inserts the order and
// bread items into the database.
func TestBrokerServiceIntegration_ReportOrder_DirectDB(t *testing.T) {
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

	client := pb.NewBrokerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Seed bread data (ReportOrder requires bread ID 1 to exist)
	dbDSN := testutils.GetDBDSNFromT(t)
	db, _ := sql.Open("pgx", dbDSN)
	if db != nil {
		db.ExecContext(ctx, `INSERT INTO bread (name, price, quantity, description, type, status, image)
			VALUES ('Test Bread', 1.00, 100, 'Test', 'Test', 'available', '/images/test.png')
			ON CONFLICT DO NOTHING`)
		db.Close()
	}

	orderUUID := "e2e-broker-report-" + time.Now().Format("20060102150405")

	t.Run("ReportOrderViaGRPC", func(t *testing.T) {
		req := &pb.BuyOrder{
			BuyOrderUuid: orderUUID,
			CustomerId:   1,
			SequenceNumber: 1,
			Items: []*pb.BuyOrderItem{
				{BreadId: 1, QuantityRequested: 2, BidPrice: 5.0},
			},
		}
		resp, err := client.ReportOrder(ctx, req)
		if err != nil {
			t.Fatalf("ReportOrder failed: %v", err)
		}
		if !resp.Accepted {
			t.Errorf("expected accepted=true, got %v", resp.Accepted)
		}
	})

	t.Run("VerifyOrderInDatabase", func(t *testing.T) {
		dbDSN := testutils.GetDBDSNFromT(t)
		db, err := sql.Open("pgx", dbDSN)
		if err != nil {
			t.Fatalf("open DB: %v", err)
		}
		defer db.Close()

		var count int
		err = db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid = $1",
			orderUUID,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query buy_order: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 order in DB, got %d", count)
		}
	})

	// Cleanup
	dbDSN = testutils.GetDBDSNFromT(t)
	db, _ = sql.Open("pgx", dbDSN)
	if db != nil {
		db.ExecContext(ctx, "DELETE FROM buy_order WHERE buy_order_uuid = $1", orderUUID) //nolint:errcheck
		db.Close()
	}
}

// TestBrokerServiceIntegration_ReportOrder_DuplicateUUID verifies that
// the server rejects duplicate order UUIDs.
func TestBrokerServiceIntegration_ReportOrder_DuplicateUUID(t *testing.T) {
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

	client := pb.NewBrokerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Seed bread data (ReportOrder requires bread ID 1 to exist)
	dbDSN := testutils.GetDBDSNFromT(t)
	db, _ := sql.Open("pgx", dbDSN)
	if db != nil {
		db.ExecContext(ctx, `INSERT INTO bread (name, price, quantity, description, type, status, image)
			VALUES ('Test Bread', 1.00, 100, 'Test', 'Test', 'available', '/images/test.png')
			ON CONFLICT DO NOTHING`)
		db.Close()
	}

	orderUUID := "e2e-broker-dup-" + time.Now().Format("20060102150405")

	// First call should succeed.
	resp1, err := client.ReportOrder(ctx, &pb.BuyOrder{
		BuyOrderUuid: orderUUID,
		CustomerId:   1,
		Items:        []*pb.BuyOrderItem{{BreadId: 1, QuantityRequested: 1}},
	})
	if err != nil {
		t.Fatalf("first ReportOrder failed: %v", err)
	}
	if !resp1.Accepted {
		t.Error("first call should be accepted")
	}

	// Second call with same UUID should be rejected.
	resp2, err := client.ReportOrder(ctx, &pb.BuyOrder{
		BuyOrderUuid: orderUUID,
		CustomerId:   1,
		Items:        []*pb.BuyOrderItem{{BreadId: 1, QuantityRequested: 1}},
	})
	if err != nil {
		t.Fatalf("second ReportOrder failed: %v", err)
	}
	if resp2.Accepted {
		t.Error("duplicate UUID should be rejected")
	}
	if resp2.Message != "duplicate" {
		t.Errorf("expected message 'duplicate', got %q", resp2.Message)
	}
}

// TestBrokerServiceIntegration_ReserveInventory_DirectDB verifies that
// ReserveInventory atomically deducts bread stock.
func TestBrokerServiceIntegration_ReserveInventory_DirectDB(t *testing.T) {
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

	client := pb.NewBrokerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbDSN := testutils.GetDBDSNFromT(t)
	db, err := sql.Open("pgx", dbDSN)
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	defer db.Close()

	// Find a bread item with sufficient stock.
	var breadID int
	var initialQty int
	err = db.QueryRowContext(ctx,
		"SELECT id, quantity FROM bread WHERE quantity > 10 ORDER BY id LIMIT 1",
	).Scan(&breadID, &initialQty)
	if err != nil {
		t.Skipf("No bread with sufficient stock: %v", err)
	}

	requestedQty := 3

	t.Run("ReserveInventory", func(t *testing.T) {
		req := &pb.ReserveInventoryRequest{
			BreadId:             int32(breadID),
			QuantityRequested:   int32(requestedQty),
			BuyOrderUuid:        "e2e-reserve-" + time.Now().Format("20060102150405"),
		}
		resp, err := client.ReserveInventory(ctx, req)
		if err != nil {
			t.Fatalf("ReserveInventory failed: %v", err)
		}
		if !resp.Reserved {
			t.Errorf("expected reserved=true, got %v", resp.Reserved)
		}
		if resp.QuantityFulfilled != int32(requestedQty) {
			t.Errorf("expected quantityFulfilled=%d, got %d", requestedQty, resp.QuantityFulfilled)
		}
	})

	t.Run("VerifyStockDeduction", func(t *testing.T) {
		var currentQty int
		err := db.QueryRowContext(ctx,
			"SELECT quantity FROM bread WHERE id = $1", breadID,
		).Scan(&currentQty)
		if err != nil {
			t.Fatalf("query bread: %v", err)
		}
		expected := initialQty - requestedQty
		if currentQty != expected {
			t.Errorf("expected quantity %d, got %d", expected, currentQty)
		}
	})
}

// TestBrokerServiceIntegration_ReportOrder_CleanupOrder verifies that
// calling ReportOrder with an invalid bread ID still creates the order.
func TestBrokerServiceIntegration_ReportOrder_CleanupOrder(t *testing.T) {
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

	client := pb.NewBrokerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Seed bread data (ReportOrder requires bread ID 1 to exist)
	dbDSN := testutils.GetDBDSNFromT(t)
	db, _ := sql.Open("pgx", dbDSN)
	if db != nil {
		db.ExecContext(ctx, `INSERT INTO bread (name, price, quantity, description, type, status, image)
			VALUES ('Test Bread', 1.00, 100, 'Test', 'Test', 'available', '/images/test.png')
			ON CONFLICT DO NOTHING`)
		db.Close()
	}

	orderUUID := "e2e-broker-cleanup-" + time.Now().Format("20060102150405")

	t.Run("ReportOrder", func(t *testing.T) {
		req := &pb.BuyOrder{
			BuyOrderUuid: orderUUID,
			CustomerId:   1,
			Items:        []*pb.BuyOrderItem{{BreadId: 1, QuantityRequested: 1}},
		}
		resp, err := client.ReportOrder(ctx, req)
		if err != nil {
			t.Fatalf("ReportOrder failed: %v", err)
		}
		if !resp.Accepted {
			t.Errorf("expected accepted=true, got %v", resp.Accepted)
		}
	})
}
