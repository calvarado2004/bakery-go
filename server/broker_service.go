package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BrokerServiceServer implements the BrokerService gRPC service.
// It is the ONLY way the broker communicates data operations to the server.
// The broker has ZERO direct database access.
type BrokerServiceServer struct {
	pb.UnimplementedBrokerServiceServer
	RabbitMQBakery *RabbitMQBakery
}

// ReportOrder handles a new order consumed by the broker from RabbitMQ.
// The server checks UUID deduplication and persists the order + details.
//
// Flow:
//   1. Check if UUID already exists (dedup)
//   2. If duplicate → return BrokerOrderResult{accepted: false, message: "duplicate"}
//   3. If new → insert order header + details in a single transaction
//   4. Write matching result to outbox for delivery to bread-bought queue
//   5. Return BrokerOrderResult{accepted: true, orderId: <id>}
func (s *BrokerServiceServer) ReportOrder(ctx context.Context, req *pb.BuyOrder) (*pb.BrokerOrderResult, error) {
	log.WithField("order_uuid", req.BuyOrderUuid).Info("broker: ReportOrder received")

	repo := s.RabbitMQBakery.Repo
	if repo == nil {
		return nil, status.Error(codes.Internal, "repository not initialized")
	}

	// Check UUID deduplication.
	_, err := repo.GetBuyOrderByUUID(req.BuyOrderUuid)
	if err == nil {
		log.WithField("order_uuid", req.BuyOrderUuid).Warn("broker: duplicate order UUID, skipping")
		return &pb.BrokerOrderResult{
			Accepted: false,
			Message:  "duplicate",
		}, nil
	}

	// Convert proto BuyOrder → data.BuyOrder.
	order := protoToDataBuyOrder(req)
	breads := protoToDataBreads(req.Items)

	// Insert order in a single transaction.
	buyOrderID, err := repo.InsertBuyOrder(order, breads)
	if err != nil {
		log.Errorf("broker: failed to insert order %s: %v", req.BuyOrderUuid, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("insert order failed: %v", err))
	}

	log.WithField("order_uuid", req.BuyOrderUuid).WithField("order_id", buyOrderID).Info("broker: order persisted")

	return &pb.BrokerOrderResult{
		Accepted:  true,
		OrderId:   int32(buyOrderID),
		Message:   "accepted",
	}, nil
}

// ReserveInventory atomically checks stock and deducts for a single item.
// This replaces the broker's direct FulfillOrderItem DB call.
//
// Flow:
//   1. SELECT FOR UPDATE on bread row (prevents concurrent oversell)
//   2. Deduct stock: UPDATE ... SET quantity = quantity - N
//   3. Return actual quantity fulfilled (supports partial fulfillment)
func (s *BrokerServiceServer) ReserveInventory(ctx context.Context, req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
	log.WithField("bread_id", req.BreadId).
		WithField("requested", req.QuantityRequested).
		WithField("order_uuid", req.BuyOrderUuid).
		Debug("broker: ReserveInventory requested")

	repo := s.RabbitMQBakery.Repo
	if repo == nil {
		return nil, status.Error(codes.Internal, "repository not initialized")
	}

	fulfilled, err := repo.FulfillOrderItem(int(req.BreadId), int(req.QuantityRequested))
	if err != nil {
		log.WithField("bread_id", req.BreadId).Warnf("broker: reserve inventory failed: %v", err)
		return &pb.ReserveInventoryResult{
			Reserved:          false,
			QuantityFulfilled: 0,
			Message:           "insufficient_stock",
		}, nil
	}

	log.WithField("bread_id", req.BreadId).
		WithField("fulfilled", fulfilled).
		Debug("broker: inventory reserved")

	return &pb.ReserveInventoryResult{
		Reserved:          true,
		QuantityFulfilled: int32(fulfilled),
		Message:           "reserved",
	}, nil
}

// ReportMatchingResults receives the complete batch matching results from the
// broker and persists them to the database and outbox in a single transaction.
//
// Flow:
//   1. Begin transaction
//   2. For each MatchingBatchResult:
//      a. Update order status
//      b. Insert order details (fulfillment results) into outbox
//   3. Commit transaction
//   4. Outbox publisher will deliver messages to bread-bought queue
func (s *BrokerServiceServer) ReportMatchingResults(ctx context.Context, req *pb.MatchingBatch) (*pb.BatchConfirmation, error) {
	log.Infof("broker: ReportMatchingResults received for %d orders", len(req.Results))

	repo := s.RabbitMQBakery.Repo
	if repo == nil {
		return nil, status.Error(codes.Internal, "repository not initialized")
	}

	ctx2, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	// Access the raw *sql.DB for transaction operations.
	conn := repo.Unwrap()
	var db *sql.DB
	if conn != nil {
		var ok bool
		db, ok = conn.(*sql.DB)
		if !ok {
			return nil, status.Error(codes.Internal, "repository does not support raw DB access")
		}
	}
	tx, err := db.BeginTx(ctx2, nil)
	if err != nil {
		log.Errorf("broker: failed to begin transaction for matching results: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("begin tx failed: %v", err))
	}
	defer tx.Rollback() //nolint:errcheck

	for _, result := range req.Results {
		log.WithField("order_uuid", result.BuyOrderUuid).
			WithField("order_status", result.OrderStatus).
			Info("broker: processing matching result")

		// 1. Update order status.
		_, err := tx.ExecContext(ctx2,
			`UPDATE buy_order SET status = $1 WHERE buy_order_uuid = $2`,
			result.OrderStatus, result.BuyOrderUuid,
		)
		if err != nil {
			log.Errorf("broker: failed to update order %s status: %v", result.BuyOrderUuid, err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("update order status failed: %v", err))
		}

		// 2. Write outbox message for bread-bought delivery.
		// Let SERIAL auto-assign the ID (UnixNano() overflows int4).
		_, err = tx.ExecContext(ctx2,
			`INSERT INTO outbox (payload, sent, created_at) VALUES ($1, false, NOW())`,
			matchingResultToPayload(result),
		)
		if err != nil {
			log.Errorf("broker: failed to insert outbox message for order %s: %v", result.BuyOrderUuid, err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("insert outbox failed: %v", err))
		}
	}

	if err = tx.Commit(); err != nil {
		log.Errorf("broker: failed to commit matching results: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("commit failed: %v", err))
	}

	log.Infof("broker: matching results committed for %d orders", len(req.Results))

	return &pb.BatchConfirmation{
		Accepted:        true,
		OrdersProcessed: int32(len(req.Results)),
		Message:         "accepted",
	}, nil
}

// --- Conversion helpers ---

// protoToDataBuyOrder converts a proto BuyOrder to a data.BuyOrder.
func protoToDataBuyOrder(proto *pb.BuyOrder) data.BuyOrder {
	order := data.BuyOrder{
		CustomerID:           int(proto.CustomerId),
		BuyOrderUUID:         proto.BuyOrderUuid,
		Status:               "processing",
		SequenceNumber:       proto.SequenceNumber,
		BidPrice:             proto.BidPrice,
		AllowPartial:         proto.AllowPartial,
		SkipUnavailableItems: proto.SkipUnavailableItems,
	}
	if proto.CreatedAt != nil {
		order.CreatedAt = proto.CreatedAt.AsTime()
	}
	return order
}

// protoToDataBreads converts proto BuyOrderItem slice to data.Bread slice
// for order detail insertion.
func protoToDataBreads(items []*pb.BuyOrderItem) []data.Bread {
	breads := make([]data.Bread, len(items))
	for i, item := range items {
		breads[i] = data.Bread{
			ID:       int(item.BreadId),
			Quantity: int(item.QuantityRequested),
			Price:    item.BidPrice,
		}
	}
	return breads
}

// matchingResultToPayload converts a MatchingBatchResult to JSON payload
// for the outbox / bread-bought queue.
func matchingResultToPayload(result *pb.MatchingBatchResult) []byte {
	type outboxItem struct {
		BreadID           int     `json:"bread_id"`
		QuantityRequested int     `json:"quantity_requested"`
		QuantityFulfilled int     `json:"quantity_fulfilled"`
		Status            string  `json:"status"`
	}
	type outboxResult struct {
		OrderUUID       string         `json:"order_uuid"`
		OrderStatus     string         `json:"order_status"`
		Items           []outboxItem   `json:"items"`
		TotalCost       float64        `json:"total_cost"`
	}

	payload := outboxResult{
		OrderUUID:   result.BuyOrderUuid,
		OrderStatus: result.OrderStatus,
		TotalCost:   result.TotalCost,
	}
	for _, item := range result.Items {
		payload.Items = append(payload.Items, outboxItem{
			BreadID:           int(item.BreadId),
			QuantityRequested: int(item.QuantityRequested),
			QuantityFulfilled: int(item.QuantityFulfilled),
			Status:            item.Status,
		})
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Errorf("broker: failed to marshal outbox payload: %v", err)
		return []byte(`{"error":"marshal_failed"}`)
	}
	return data
}
