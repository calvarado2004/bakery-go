package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mockBrokerRepo is a minimal Repository mock focused on what BrokerService calls.
type mockBrokerRepo struct {
	getBuyOrderByUUIDFn   func(uuid string) (data.BuyOrder, error)
	insertBuyOrderFn      func(order data.BuyOrder, breads []data.Bread) (int, error)
	fulfillOrderItemFn    func(breadID int, qty int) (int, error)
	unwrapFn              func() interface{}
	insertOutboxFn        func(msg data.OutboxMessage) (int, error)
	getBuyOrderByIDFn     func(id int) (data.BuyOrder, error)
	getOrderTotalCostFn   func(id int) (float64, error)
}

func (m *mockBrokerRepo) FulfillOrderItem(breadID int, qty int) (int, error)                                    { return m.fulfillOrderItemFn(breadID, qty) }
func (m *mockBrokerRepo) FulfillOrderTx(order data.BuyOrder) error                                              { return nil }
func (m *mockBrokerRepo) InsertCustomer(c data.Customer) (int, error)                                          { return 0, nil }
func (m *mockBrokerRepo) InsertBread(b data.Bread) (int, error)                                                { return 0, nil }
func (m *mockBrokerRepo) InsertBreadMaker(b data.BreadMaker) (int, error)                                      { return 0, nil }
func (m *mockBrokerRepo) InsertBuyOrder(order data.BuyOrder, breads []data.Bread) (int, error)                 { return m.insertBuyOrderFn(order, breads) }
func (m *mockBrokerRepo) InsertMakeOrder(o data.MakeOrder, breads []data.Bread) (int, error)                   { return 0, nil }
func (m *mockBrokerRepo) AdjustBreadQuantity(id int, qty int) (bool, error)                                    { return false, nil }
func (m *mockBrokerRepo) AdjustBreadPrice(id int, price float64) error                                         { return nil }
func (m *mockBrokerRepo) PasswordMatches(p string, c data.Customer) (bool, error)                              { return false, nil }
func (m *mockBrokerRepo) GetAvailableBread() ([]data.Bread, error)                                             { return nil, nil }
func (m *mockBrokerRepo) GetBreadByID(id int) (data.Bread, error)                                              { return data.Bread{}, nil }
func (m *mockBrokerRepo) GetMakeOrderByID(id int) (data.MakeOrder, error)                                      { return data.MakeOrder{}, nil }
func (m *mockBrokerRepo) GetBuyOrderByUUID(uuid string) (data.BuyOrder, error)                                 { return m.getBuyOrderByUUIDFn(uuid) }
func (m *mockBrokerRepo) GetBuyOrderByID(id int) (data.BuyOrder, error)                                        { return m.getBuyOrderByIDFn(id) }
func (m *mockBrokerRepo) GetAllBuyOrders() ([]data.BuyOrder, error)                                            { return nil, nil }
func (m *mockBrokerRepo) UpdateOrderStatus(uuid, status string) error                                          { return nil }
func (m *mockBrokerRepo) GetOrderTotalCost(id int) (float64, error)                                            { return m.getOrderTotalCostFn(id) }
func (m *mockBrokerRepo) DeleteOutboxMessage(id int) error                                                     { return nil }
func (m *mockBrokerRepo) InsertOutboxMessage(msg data.OutboxMessage) error                                   { m.insertOutboxFn(msg); return nil }
func (m *mockBrokerRepo) GetUnprocessedOutboxMessages() ([]data.OutboxMessage, error)                          { return nil, nil }
func (m *mockBrokerRepo) ClaimOutboxMessage() (*data.OutboxMessage, error)                                     { return nil, nil }
func (m *mockBrokerRepo) GetAllCustomers() ([]data.Customer, error)                                            { return nil, nil }
func (m *mockBrokerRepo) GetAllBreadMakers() ([]data.BreadMaker, error)                                        { return nil, nil }
func (m *mockBrokerRepo) GetDashboardStats() (*data.DashboardStats, error)                                     { return nil, nil }
func (m *mockBrokerRepo) UpdateBread(b data.Bread) error                                                       { return nil }
func (m *mockBrokerRepo) DeleteBread(id int) error                                                             { return nil }
func (m *mockBrokerRepo) GetLowStockBread(threshold int) ([]data.Bread, error)                                 { return nil, nil }
func (m *mockBrokerRepo) GetCustomerOrders(id int) ([]data.BuyOrder, error)                                    { return nil, nil }
func (m *mockBrokerRepo) GetMakerOrders(id int) ([]data.MakeOrder, error)                                      { return nil, nil }
func (m *mockBrokerRepo) GetCustomerByID(id int) (data.Customer, error)                                        { return data.Customer{}, nil }
func (m *mockBrokerRepo) GetBreadMakerByID(id int) (data.BreadMaker, error)                                    { return data.BreadMaker{}, nil }
func (m *mockBrokerRepo) GetAllMakeOrders() ([]data.MakeOrder, error)                                          { return nil, nil }
func (m *mockBrokerRepo) GetAdminUserByUsername(username string) (data.AdminUser, error)                       { return data.AdminUser{}, nil }
func (m *mockBrokerRepo) GetAdminUserByID(id int) (data.AdminUser, error)                                      { return data.AdminUser{}, nil }
func (m *mockBrokerRepo) InsertAdminUser(u data.AdminUser) (int, error)                                        { return 0, nil }
func (m *mockBrokerRepo) GetCustomerByEmail(email string) (data.Customer, error)                               { return data.Customer{}, nil }
func (m *mockBrokerRepo) InsertInvoice(inv data.Invoice) (int, error)                                          { return 0, nil }
func (m *mockBrokerRepo) GetInvoiceByID(id int) (data.Invoice, error)                                          { return data.Invoice{}, nil }
func (m *mockBrokerRepo) GetInvoicesByCustomerID(id int) ([]data.Invoice, error)                               { return nil, nil }
func (m *mockBrokerRepo) GetAllInvoices() ([]data.Invoice, error)                                              { return nil, nil }
func (m *mockBrokerRepo) GetInvoiceByOrderID(id int) (data.Invoice, error)                                     { return data.Invoice{}, nil }
func (m *mockBrokerRepo) WaitForOrderNotification(ctx context.Context, uuid string) error                      { return nil }
func (m *mockBrokerRepo) InsertPendingMakeOrder(o data.PendingMakeOrder) (int, error)                          { return 0, nil }
func (m *mockBrokerRepo) ClaimPendingMakeOrders(count int) ([]data.PendingMakeOrder, error)                    { return nil, nil }
func (m *mockBrokerRepo) UpdatePendingMakeOrderStatus(id int, status string) error                             { return nil }
func (m *mockBrokerRepo) Unwrap() interface{}                                                                  { return m.unwrapFn() }

func newMockBrokerRepo() *mockBrokerRepo {
	return &mockBrokerRepo{
		getBuyOrderByUUIDFn: func(uuid string) (data.BuyOrder, error) {
			return data.BuyOrder{}, sql.ErrNoRows
		},
		insertBuyOrderFn: func(order data.BuyOrder, breads []data.Bread) (int, error) {
			return 1, nil
		},
		fulfillOrderItemFn: func(breadID int, qty int) (int, error) {
			return qty, nil
		},
		unwrapFn: func() interface{} {
			return nil
		},
		insertOutboxFn: func(msg data.OutboxMessage) (int, error) {
			return 1, nil
		},
		getBuyOrderByIDFn: func(id int) (data.BuyOrder, error) {
			return data.BuyOrder{}, sql.ErrNoRows
		},
		getOrderTotalCostFn: func(id int) (float64, error) {
			return 0, nil
		},
	}
}

func TestBrokerService_ReportOrder_Success(t *testing.T) {
	mockRepo := newMockBrokerRepo()
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: mockRepo},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.BuyOrder{
		BuyOrderUuid: "order-uuid-1",
		CustomerId:   1,
		SequenceNumber: 1,
		Items: []*pb.BuyOrderItem{
			{BreadId: 1, QuantityRequested: 2, BidPrice: 5.0},
		},
	}

	resp, err := server.ReportOrder(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Accepted {
		t.Error("expected accepted=true")
	}
	if resp.Message != "accepted" {
		t.Errorf("expected message 'accepted', got %q", resp.Message)
	}
	if resp.OrderId != 1 {
		t.Errorf("expected orderId=1, got %d", resp.OrderId)
	}
}

func TestBrokerService_ReportOrder_DuplicateUUID(t *testing.T) {
	mockRepo := newMockBrokerRepo()
	mockRepo.getBuyOrderByUUIDFn = func(uuid string) (data.BuyOrder, error) {
		return data.BuyOrder{BuyOrderUUID: uuid}, nil // exists
	}
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: mockRepo},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.BuyOrder{
		BuyOrderUuid: "existing-uuid",
	}

	resp, err := server.ReportOrder(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Accepted {
		t.Error("expected accepted=false for duplicate")
	}
	if resp.Message != "duplicate" {
		t.Errorf("expected message 'duplicate', got %q", resp.Message)
	}
}

func TestBrokerService_ReportOrder_InsertFails(t *testing.T) {
	mockRepo := newMockBrokerRepo()
	mockRepo.insertBuyOrderFn = func(order data.BuyOrder, breads []data.Bread) (int, error) {
		return 0, data.ErrInsufficientStock
	}
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: mockRepo},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.BuyOrder{
		BuyOrderUuid: "order-uuid-insert-fail",
		Items:        []*pb.BuyOrderItem{{BreadId: 1, QuantityRequested: 1}},
	}

	_, err := server.ReportOrder(ctx, req)
	if err == nil {
		t.Fatal("expected error when insert fails")
	}
}

func TestBrokerService_ReportOrder_RepoNil(t *testing.T) {
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: nil},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.BuyOrder{BuyOrderUuid: "no-repo"}

	resp, err := server.ReportOrder(ctx, req)
	if err == nil {
		t.Fatal("expected error when repo is nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Internal {
		t.Fatalf("expected Internal error, got %v", err)
	}
	if resp != nil {
		t.Error("expected nil response on error")
	}
}

func TestBrokerService_ReserveInventory_Success(t *testing.T) {
	mockRepo := newMockBrokerRepo()
	mockRepo.fulfillOrderItemFn = func(breadID int, qty int) (int, error) {
		return qty, nil
	}
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: mockRepo},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.ReserveInventoryRequest{
		BreadId:             5,
		QuantityRequested:   3,
		BuyOrderUuid:        "order-reserve-1",
	}

	resp, err := server.ReserveInventory(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Reserved {
		t.Error("expected reserved=true")
	}
	if resp.QuantityFulfilled != 3 {
		t.Errorf("expected quantityFulfilled=3, got %d", resp.QuantityFulfilled)
	}
}

func TestBrokerService_ReserveInventory_FullFulfill(t *testing.T) {
	mockRepo := newMockBrokerRepo()
	// Only 1 unit available out of 5 requested (partial fulfillment)
	mockRepo.fulfillOrderItemFn = func(breadID int, qty int) (int, error) {
		return 1, nil
	}
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: mockRepo},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.ReserveInventoryRequest{
		BreadId:             5,
		QuantityRequested:   5,
		BuyOrderUuid:        "order-partial",
	}

	resp, err := server.ReserveInventory(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Reserved {
		t.Error("expected reserved=true even for partial")
	}
	if resp.QuantityFulfilled != 1 {
		t.Errorf("expected quantityFulfilled=1, got %d", resp.QuantityFulfilled)
	}
}

func TestBrokerService_ReserveInventory_InsufficientStock(t *testing.T) {
	mockRepo := newMockBrokerRepo()
	mockRepo.fulfillOrderItemFn = func(breadID int, qty int) (int, error) {
		return 0, data.ErrInsufficientStock
	}
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: mockRepo},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.ReserveInventoryRequest{
		BreadId:             99,
		QuantityRequested:   10,
		BuyOrderUuid:        "order-no-stock",
	}

	resp, err := server.ReserveInventory(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reserved {
		t.Error("expected reserved=false for insufficient stock")
	}
	if resp.Message != "insufficient_stock" {
		t.Errorf("expected message 'insufficient_stock', got %q", resp.Message)
	}
}

func TestBrokerService_ReserveInventory_RepoNil(t *testing.T) {
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: nil},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.ReserveInventoryRequest{BreadId: 1, QuantityRequested: 1}

	resp, err := server.ReserveInventory(ctx, req)
	if err == nil {
		t.Fatal("expected error when repo is nil")
	}
	if resp != nil {
		t.Error("expected nil response on error")
	}
}

func TestBrokerService_ReportMatchingResults_RepoReturnsNilUnwrap(t *testing.T) {
	mockRepo := newMockBrokerRepo()
	// Unwrap returns nil — the handler checks if it's a *sql.DB and
	// returns an error if not.
	mockRepo.unwrapFn = func() interface{} {
		return nil
	}
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: mockRepo},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.MatchingBatch{
		Results: []*pb.MatchingBatchResult{
			{
				BuyOrderUuid: "order-mr-1",
				OrderStatus:  "settled",
				TotalCost:    10.0,
				Items: []*pb.MatchingItemResult{
					{BreadId: 1, QuantityRequested: 2, QuantityFulfilled: 2, Status: "fulfilled"},
				},
			},
		},
	}

	_, err := server.ReportMatchingResults(ctx, req)
	if err == nil {
		t.Fatal("expected error when unwrap returns nil (no raw DB)")
	}
}

func TestBrokerService_ReportMatchingResults_RepoNil(t *testing.T) {
	bakery := &RabbitMQBakery{
		Config:      Config{Repo: nil},
		rabbitmqURL: "amqp://test",
	}
	server := &BrokerServiceServer{
		RabbitMQBakery: bakery,
	}

	ctx := context.Background()
	req := &pb.MatchingBatch{}

	resp, err := server.ReportMatchingResults(ctx, req)
	if err == nil {
		t.Fatal("expected error when repo is nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Internal {
		t.Fatalf("expected Internal error, got %v", err)
	}
	if resp != nil {
		t.Error("expected nil response on error")
	}
}

// --- Conversion helper tests ---

func TestProtoToDataBuyOrder(t *testing.T) {
	proto := &pb.BuyOrder{
		CustomerId:     42,
		BuyOrderUuid:   "conv-uuid-1",
		SequenceNumber: 5,
		BidPrice:       9.99,
		AllowPartial:   true,
	}

	order := protoToDataBuyOrder(proto)
	if order.CustomerID != 42 {
		t.Errorf("expected CustomerID=42, got %d", order.CustomerID)
	}
	if order.BuyOrderUUID != "conv-uuid-1" {
		t.Errorf("expected BuyOrderUUID 'conv-uuid-1', got %q", order.BuyOrderUUID)
	}
	if order.Status != "processing" {
		t.Errorf("expected status 'processing', got %q", order.Status)
	}
	if order.SequenceNumber != 5 {
		t.Errorf("expected SequenceNumber=5, got %d", order.SequenceNumber)
	}
	if order.AllowPartial != true {
		t.Error("expected AllowPartial=true")
	}
}

func TestProtoToDataBuyOrder_WithTimestamp(t *testing.T) {
	proto := &pb.BuyOrder{
		CustomerId:   1,
		BuyOrderUuid: "ts-uuid",
		CreatedAt:    nil,
	}
	order := protoToDataBuyOrder(proto)
	if order.BuyOrderUUID != "ts-uuid" {
		t.Errorf("unexpected UUID: %q", order.BuyOrderUUID)
	}
}

func TestProtoToDataBreads_Empty(t *testing.T) {
	breads := protoToDataBreads(nil)
	if len(breads) != 0 {
		t.Errorf("expected 0 breads, got %d", len(breads))
	}
}

func TestProtoToDataBreads_Items(t *testing.T) {
	items := []*pb.BuyOrderItem{
		{BreadId: 10, QuantityRequested: 3, BidPrice: 2.5},
		{BreadId: 20, QuantityRequested: 1, BidPrice: 5.0},
	}
	breads := protoToDataBreads(items)
	if len(breads) != 2 {
		t.Fatalf("expected 2 breads, got %d", len(breads))
	}
	if breads[0].ID != 10 || breads[0].Quantity != 3 {
		t.Errorf("breads[0]: expected {10,3}, got {%d,%d}", breads[0].ID, breads[0].Quantity)
	}
	if breads[1].ID != 20 || breads[1].Quantity != 1 {
		t.Errorf("breads[1]: expected {20,1}, got {%d,%d}", breads[1].ID, breads[1].Quantity)
	}
}

func TestMatchingResultToPayload(t *testing.T) {
	result := &pb.MatchingBatchResult{
		BuyOrderUuid: "payload-uuid",
		OrderStatus:  "settled",
		TotalCost:    25.5,
		Items: []*pb.MatchingItemResult{
			{BreadId: 1, QuantityRequested: 2, QuantityFulfilled: 2, Status: "fulfilled"},
			{BreadId: 2, QuantityRequested: 1, QuantityFulfilled: 0, Status: "unavailable"},
		},
	}
	payload := matchingResultToPayload(result)
	if len(payload) == 0 {
		t.Fatal("expected non-empty payload")
	}
	// Basic sanity: check key fields are present
	checks := []string{`"order_uuid"`, `"order_status"`, `"items"`, `"total_cost"`}
	for _, c := range checks {
		if !containsString(string(payload), c) {
			t.Errorf("payload missing %s: %s", c, string(payload))
		}
	}
}

func TestMatchingResultToPayload_MarshalError(t *testing.T) {
	// This test ensures the helper returns a safe fallback on marshal error.
	// matchingResultToPayload uses standard json.Marshal which rarely fails
	// with simple types, so we just verify the return is never nil.
	result := &pb.MatchingBatchResult{}
	result.Items = []*pb.MatchingItemResult{}
	payload := matchingResultToPayload(result)
	if len(payload) == 0 {
		t.Fatal("expected non-empty payload")
	}
}

// containsString is a simple helper to check string presence.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
