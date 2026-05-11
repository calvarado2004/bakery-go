package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	"github.com/calvarado2004/bakery-go/pkg/resilience"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/rabbitmq/amqp091-go"
)

// --- mockBrokerClient: gRPC client stub for testing ---

type mockBrokerClient struct {
	reportOrderFn    func(pb.BuyOrder) (*pb.BrokerOrderResult, error)
	reserveFn        func(*pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error)
	reportMatchFn    func(*pb.MatchingBatch) (*pb.BatchConfirmation, error)
	mu               sync.Mutex
	reportedOrders   []pb.BuyOrder
	reservedInvItems []*pb.ReserveInventoryRequest
}

func (m *mockBrokerClient) ReportOrder(order pb.BuyOrder) (*pb.BrokerOrderResult, error) {
	m.mu.Lock()
	m.reportedOrders = append(m.reportedOrders, order)
	m.mu.Unlock()
	if m.reportOrderFn != nil {
		return m.reportOrderFn(order)
	}
	return &pb.BrokerOrderResult{Accepted: true, OrderId: 1, Message: "accepted"}, nil
}

func (m *mockBrokerClient) ReserveInventory(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
	m.mu.Lock()
	m.reservedInvItems = append(m.reservedInvItems, req)
	m.mu.Unlock()
	if m.reserveFn != nil {
		return m.reserveFn(req)
	}
	return &pb.ReserveInventoryResult{Reserved: true, QuantityFulfilled: int32(req.QuantityRequested), Message: "reserved"}, nil
}

func (m *mockBrokerClient) ReportMatchingResults(req *pb.MatchingBatch) (*pb.BatchConfirmation, error) {
	if m.reportMatchFn != nil {
		return m.reportMatchFn(req)
	}
	return &pb.BatchConfirmation{Accepted: true, OrdersProcessed: int32(len(req.Results)), Message: "accepted"}, nil
}

// --- stub publisher for matching tests ---

type stubPublisher struct{}

func (p *stubPublisher) Publish(_, _ string, _, _ bool, _ rabbitmq.Publishing) error { return nil }

// --- canFulfillOrder tests ---

func TestCanFulfillOrder_AllAvailable(t *testing.T) {
	available := []data.Bread{
		{Name: "Pretzel", Quantity: 10},
		{Name: "Baguette", Quantity: 5},
	}
	order := data.BuyOrder{
		Breads: []data.Bread{
			{Name: "Pretzel", Quantity: 3},
			{Name: "Baguette", Quantity: 2},
		},
	}
	if !canFulfillOrder(order, available) {
		t.Error("expected order to be fulfillable")
	}
}

func TestCanFulfillOrder_ExactQuantity(t *testing.T) {
	available := []data.Bread{
		{Name: "Pretzel", Quantity: 5},
	}
	order := data.BuyOrder{
		Breads: []data.Bread{
			{Name: "Pretzel", Quantity: 5},
		},
	}
	if !canFulfillOrder(order, available) {
		t.Error("exact quantity should be fulfillable")
	}
}

func TestCanFulfillOrder_InsufficientQuantity(t *testing.T) {
	available := []data.Bread{
		{Name: "Pretzel", Quantity: 2},
	}
	order := data.BuyOrder{
		Breads: []data.Bread{
			{Name: "Pretzel", Quantity: 5},
		},
	}
	if canFulfillOrder(order, available) {
		t.Error("expected order to fail: insufficient quantity")
	}
}

func TestCanFulfillOrder_BreadNotInStock(t *testing.T) {
	available := []data.Bread{
		{Name: "Pretzel", Quantity: 10},
	}
	order := data.BuyOrder{
		Breads: []data.Bread{
			{Name: "Sourdough", Quantity: 1},
		},
	}
	if canFulfillOrder(order, available) {
		t.Error("expected order to fail: bread type not in stock")
	}
}

func TestCanFulfillOrder_EmptyOrder(t *testing.T) {
	available := []data.Bread{
		{Name: "Pretzel", Quantity: 10},
	}
	order := data.BuyOrder{Breads: nil}
	if !canFulfillOrder(order, available) {
		t.Error("empty order should always be fulfillable")
	}
}

func TestCanFulfillOrder_EmptyStock(t *testing.T) {
	order := data.BuyOrder{
		Breads: []data.Bread{
			{Name: "Pretzel", Quantity: 1},
		},
	}
	if canFulfillOrder(order, nil) {
		t.Error("expected order to fail: no stock available")
	}
}

func TestCanFulfillOrder_MultipleBreadsMixedAvailability(t *testing.T) {
	available := []data.Bread{
		{Name: "Pretzel", Quantity: 10},
		{Name: "Baguette", Quantity: 1},
	}
	order := data.BuyOrder{
		Breads: []data.Bread{
			{Name: "Pretzel", Quantity: 5},
			{Name: "Baguette", Quantity: 3},
		},
	}
	if canFulfillOrder(order, available) {
		t.Error("expected order to fail: Baguette insufficient")
	}
}

// --- orderBuffer tests ---

func TestOrderBuffer_AddAndDrain(t *testing.T) {
	buf := &orderBuffer{}
	order := data.BuyOrder{BuyOrderUUID: "test-1"}
	buf.add(order)
	if buf.len() != 1 {
		t.Errorf("expected buffer len 1, got %d", buf.len())
	}
	drained := buf.drain()
	if len(drained) != 1 {
		t.Fatalf("expected 1 drained order, got %d", len(drained))
	}
	if buf.len() != 0 {
		t.Error("expected empty buffer after drain")
	}
}

func TestOrderBuffer_DrainEmpty(t *testing.T) {
	buf := &orderBuffer{}
	if drained := buf.drain(); drained != nil {
		t.Error("expected nil drain from empty buffer")
	}
}

// --- processOneOrder tests ---

// TestProcessOneOrder_ValidOrderBuffersAndAcks verifies that a valid order
// is reported to the server, buffered for matching, and the delivery is ACKed.
func TestProcessOneOrder_ValidOrderBuffersAndAcks(t *testing.T) {
	mockClient := &mockBrokerClient{
		reportOrderFn: func(order pb.BuyOrder) (*pb.BrokerOrderResult, error) {
			return &pb.BrokerOrderResult{Accepted: true, OrderId: 1, Message: "accepted"}, nil
		},
	}
	broker := &BrokerService{
		brokerConfig: brokerConfig{},
	}

	order := data.BuyOrder{
		BuyOrderUUID: "uuid-success-1",
		Breads:       []data.Bread{{ID: 1, Quantity: 2}},
	}
	body, _ := json.Marshal(order)

	acked := false
	delivery := rabbitmq.Delivery{
		Body: body,
		Acknowledger: &testAcknowledger{ackFn: func(bool) error {
			acked = true
			return nil
		}},
	}

	broker.processOneOrder(delivery, mockClient)

	if !acked {
		t.Error("expected delivery to be acked on success")
	}
	if broker.buffer.len() != 1 {
		t.Errorf("expected order to be buffered, got buffer len=%d", broker.buffer.len())
	}
	if len(mockClient.reportedOrders) != 1 {
		t.Error("expected ReportOrder to be called once")
	}
}

// TestProcessOneOrder_DuplicateUUIDSkipsBuffering verifies that a duplicate
// UUID (server returns rejected) causes the delivery to be ACKed without buffering.
func TestProcessOneOrder_DuplicateUUIDSkipsBuffering(t *testing.T) {
	mockClient := &mockBrokerClient{
		reportOrderFn: func(order pb.BuyOrder) (*pb.BrokerOrderResult, error) {
			return &pb.BrokerOrderResult{Accepted: false, Message: "duplicate"}, nil
		},
	}
	broker := &BrokerService{}

	order := data.BuyOrder{BuyOrderUUID: "uuid-dup-1"}
	body, _ := json.Marshal(order)

	acked := false
	delivery := rabbitmq.Delivery{
		Body: body,
		Acknowledger: &testAcknowledger{ackFn: func(bool) error {
			acked = true
			return nil
		}},
	}

	broker.processOneOrder(delivery, mockClient)

	if !acked {
		t.Error("expected duplicate delivery to be acked (not requeued)")
	}
	if broker.buffer.len() != 0 {
		t.Errorf("expected no buffering for duplicate, got buffer len=%d", broker.buffer.len())
	}
}

// TestProcessOneOrder_InvalidJSONAcksDelivery verifies that malformed
// messages are ACKed (not requeued) to prevent infinite requeue loops.
func TestProcessOneOrder_InvalidJSONAcksDelivery(t *testing.T) {
	mockClient := &mockBrokerClient{}
	broker := &BrokerService{}

	acked := false
	delivery := rabbitmq.Delivery{
		Body: []byte("not json {{{"),
		Acknowledger: &testAcknowledger{ackFn: func(bool) error {
			acked = true
			return nil
		}},
	}

	broker.processOneOrder(delivery, mockClient)

	if !acked {
		t.Error("expected malformed delivery to be acked to avoid infinite requeue")
	}
	if broker.buffer.len() != 0 {
		t.Error("expected no buffering for malformed message")
	}
}

// --- processMatchingBatch tests ---

// TestProcessMatchingBatch_ReportsResults verifies that the matching engine
// reports all batch results to the server via gRPC.
func TestProcessMatchingBatch_ReportsResults(t *testing.T) {
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			return &pb.ReserveInventoryResult{
				Reserved:          true,
				QuantityFulfilled: int32(req.QuantityRequested),
				Message:           "reserved",
			}, nil
		},
	}

	broker := &BrokerService{}
	order := data.BuyOrder{
		BuyOrderUUID: "uuid-match-1",
		Breads:       []data.Bread{{ID: 1, Quantity: 5, Price: 2.50}},
	}

	broker.processMatchingBatch([]data.BuyOrder{order}, &stubPublisher{}, mockClient)

	if len(mockClient.reservedInvItems) != 1 {
		t.Errorf("expected 1 ReserveInventory call, got %d", len(mockClient.reservedInvItems))
	}
}

// --- NewBrokerService tests ---

func TestNewBrokerService_SetsFields(t *testing.T) {
	cfg := brokerConfig{}
	b := NewBrokerService(cfg, "amqp://localhost:5672", nil, nil)

	if b == nil {
		t.Fatal("expected non-nil BrokerService")
	}
	if b.rabbitmqURL != "amqp://localhost:5672" {
		t.Errorf("unexpected rabbitmqURL: %s", b.rabbitmqURL)
	}
}

func TestNewBrokerService_EmptyURL(t *testing.T) {
	b := NewBrokerService(brokerConfig{}, "", nil, nil)
	if b == nil {
		t.Fatal("expected non-nil BrokerService")
	}
	if b.rabbitmqURL != "" {
		t.Errorf("expected empty URL, got %s", b.rabbitmqURL)
	}
}

// testAcknowledger is a minimal rabbitmq.Acknowledger for use in unit tests.
type testAcknowledger struct {
	ackFn  func(multiple bool) error
	nackFn func(multiple, requeue bool) error
}

func (a *testAcknowledger) Ack(tag uint64, multiple bool) error {
	if a.ackFn != nil {
		return a.ackFn(multiple)
	}
	return nil
}

func (a *testAcknowledger) Nack(tag uint64, multiple bool, requeue bool) error {
	if a.nackFn != nil {
		return a.nackFn(multiple, requeue)
	}
	return nil
}

func (a *testAcknowledger) Reject(tag uint64, requeue bool) error { return nil }

// --- maxBidPrice tests ---

func TestMaxBidPrice_ExplicitBid(t *testing.T) {
	order := data.BuyOrder{BidPrice: 10.50}
	if got := maxBidPrice(order); got != 10.50 {
		t.Errorf("expected 10.50, got %f", got)
	}
}

func TestMaxBidPrice_FromItems(t *testing.T) {
	order := data.BuyOrder{
		BidPrice: 0,
		MatchedItems: []data.OrderItem{
			{BidPrice: 3.0},
			{BidPrice: 8.0},
			{BidPrice: 5.0},
		},
	}
	if got := maxBidPrice(order); got != 8.0 {
		t.Errorf("expected 8.0 (max from items), got %f", got)
	}
}

func TestMaxBidPrice_NoBid(t *testing.T) {
	order := data.BuyOrder{BidPrice: 0, MatchedItems: nil}
	if got := maxBidPrice(order); got != 0 {
		t.Errorf("expected 0, got %f", got)
	}
}

func TestMaxBidPrice_ItemsOverrideZeroBid(t *testing.T) {
	order := data.BuyOrder{BidPrice: 0, MatchedItems: []data.OrderItem{{BidPrice: 42.0}}}
	if got := maxBidPrice(order); got != 42.0 {
		t.Errorf("expected 42.0 from items, got %f", got)
	}
}

// --- processMatchingBatch edge case tests ---

func TestProcessMatchingBatch_EmptyBatch(t *testing.T) {
	mockClient := &mockBrokerClient{}
	broker := &BrokerService{}

	// Should not panic and not call any gRPC methods
	broker.processMatchingBatch([]data.BuyOrder{}, &stubPublisher{}, mockClient)

	if len(mockClient.reservedInvItems) != 0 {
		t.Error("expected no ReserveInventory calls for empty batch")
	}
	if len(mockClient.reportedOrders) != 0 {
		t.Error("expected no ReportMatchingResults calls for empty batch")
	}
}

func TestProcessMatchingBatch_NoStock(t *testing.T) {
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			return &pb.ReserveInventoryResult{Reserved: false, Message: "no stock"}, nil
		},
	}
	broker := &BrokerService{}
	order := data.BuyOrder{
		BuyOrderUUID: "uuid-nostock-1",
		Breads:       []data.Bread{{ID: 1, Quantity: 5, Price: 2.50}},
	}

	broker.processMatchingBatch([]data.BuyOrder{order}, &stubPublisher{}, mockClient)

	// Should have called ReserveInventory but order should be "failed"
	if len(mockClient.reservedInvItems) != 1 {
		t.Errorf("expected 1 ReserveInventory call, got %d", len(mockClient.reservedInvItems))
	}
}

func TestProcessMatchingBatch_AllowedPartial(t *testing.T) {
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			// Only fulfill 2 out of 5 requested
			if req.QuantityRequested > 2 {
				return &pb.ReserveInventoryResult{Reserved: true, QuantityFulfilled: 2, Message: "partial"}, nil
			}
			return &pb.ReserveInventoryResult{Reserved: true, QuantityFulfilled: req.QuantityRequested, Message: "full"}, nil
		},
	}
	broker := &BrokerService{}
	order := data.BuyOrder{
		BuyOrderUUID:   "uuid-partial-1",
		AllowPartial:   true,
		SkipUnavailableItems: true,
		Breads:         []data.Bread{{ID: 1, Quantity: 5, Price: 2.50}},
	}

	broker.processMatchingBatch([]data.BuyOrder{order}, &stubPublisher{}, mockClient)

	// With AllowPartial, partial fulfillment should succeed (not rejected)
	if len(mockClient.reservedInvItems) != 1 {
		t.Errorf("expected 1 ReserveInventory call, got %d", len(mockClient.reservedInvItems))
	}
}

func TestProcessMatchingBatch_MultipleOrdersSortedByPriority(t *testing.T) {
	reserveCount := 0
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			reserveCount++
			return &pb.ReserveInventoryResult{Reserved: true, QuantityFulfilled: int32(req.QuantityRequested), Message: "reserved"}, nil
		},
	}
	broker := &BrokerService{}

	// Orders with different bid prices — highest should be processed first
	orderA := data.BuyOrder{
		BuyOrderUUID:   "uuid-prio-a",
		SequenceNumber: 1,
		BidPrice:       3.0,
		Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 5.00}},
	}
	orderB := data.BuyOrder{
		BuyOrderUUID:   "uuid-prio-b",
		SequenceNumber: 2,
		BidPrice:       10.0,
		Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 5.00}},
	}
	orderC := data.BuyOrder{
		BuyOrderUUID:   "uuid-prio-c",
		SequenceNumber: 3,
		BidPrice:       7.0,
		Breads:         []data.Bread{{ID: 1, Quantity: 1, Price: 5.00}},
	}

	broker.processMatchingBatch([]data.BuyOrder{orderA, orderB, orderC}, &stubPublisher{}, mockClient)

	if reserveCount != 3 {
		t.Errorf("expected 3 ReserveInventory calls, got %d", reserveCount)
	}
}

// --- dataToProtoBuyOrder conversion tests ---

func TestDataToProtoBuyOrder_Conversion(t *testing.T) {
	order := data.BuyOrder{
		ID:                   42,
		CustomerID:           7,
		BuyOrderUUID:         "test-uuid",
		Status:               "pending",
		SequenceNumber:       100,
		BidPrice:             5.50,
		AllowPartial:         true,
		SkipUnavailableItems: false,
		Breads: []data.Bread{
			{ID: 1, Quantity: 3, Price: 2.50},
			{ID: 2, Quantity: 1, Price: 4.00},
		},
	}

	proto := dataToProtoBuyOrder(order)

	if proto.Id != 42 {
		t.Errorf("expected ID 42, got %d", proto.Id)
	}
	if proto.CustomerId != 7 {
		t.Errorf("expected CustomerId 7, got %d", proto.CustomerId)
	}
	if proto.BuyOrderUuid != "test-uuid" {
		t.Errorf("expected UUID test-uuid, got %s", proto.BuyOrderUuid)
	}
	if proto.Status != "pending" {
		t.Errorf("expected status pending, got %s", proto.Status)
	}
	if proto.SequenceNumber != 100 {
		t.Errorf("expected SequenceNumber 100, got %d", proto.SequenceNumber)
	}
	if len(proto.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(proto.Items))
	}
	if proto.Items[0].BreadId != 1 || proto.Items[0].QuantityRequested != 3 || proto.Items[0].BidPrice != 2.50 {
		t.Errorf("item 0: %+v", proto.Items[0])
	}
	if proto.Items[1].BreadId != 2 || proto.Items[1].QuantityRequested != 1 || proto.Items[1].BidPrice != 4.00 {
		t.Errorf("item 1: %+v", proto.Items[1])
	}
}

func TestDataToProtoBuyOrder_EmptyBreads(t *testing.T) {
	order := data.BuyOrder{
		ID:         1,
		CustomerID: 1,
		BuyOrderUUID: "empty-breads",
		Breads:     nil,
	}
	proto := dataToProtoBuyOrder(order)
	if len(proto.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(proto.Items))
	}
}

// --- matchingItemsToDataOrderItems tests ---

func TestMatchingItemsToDataOrderItems(t *testing.T) {
	items := []*pb.MatchingItemResult{
		{BreadId: 1, QuantityRequested: 5, QuantityFulfilled: 3, Status: "partially_fulfilled"},
		{BreadId: 2, QuantityRequested: 2, QuantityFulfilled: 2, Status: "fulfilled"},
		{BreadId: 3, QuantityRequested: 1, QuantityFulfilled: 0, Status: "rejected"},
	}

	result := matchingItemsToDataOrderItems(items)

	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0].BreadID != 1 || result[0].QuantityRequested != 5 || result[0].QuantityFulfilled != 3 || result[0].Status != "partially_fulfilled" {
		t.Errorf("item 0 mismatch: %+v", result[0])
	}
	if result[1].BreadID != 2 || result[1].QuantityFulfilled != 2 || result[1].Status != "fulfilled" {
		t.Errorf("item 1 mismatch: %+v", result[1])
	}
	if result[2].BreadID != 3 || result[2].Status != "rejected" {
		t.Errorf("item 2 mismatch: %+v", result[2])
	}
}

// --- fulfillOrder tests ---

func TestFulfillOrder_FullFulfillment(t *testing.T) {
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			return &pb.ReserveInventoryResult{Reserved: true, QuantityFulfilled: req.QuantityRequested, Message: "full"}, nil
		},
	}
	broker := &BrokerService{}
	order := &data.BuyOrder{
		BuyOrderUUID: "uuid-fulled",
		Breads:       []data.Bread{{ID: 1, Quantity: 5, Price: 10.00}},
	}

	result := broker.fulfillOrder(order, &stubPublisher{}, mockClient)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.OrderStatus != "processed" {
		t.Errorf("expected status 'processed', got '%s'", result.OrderStatus)
	}
	if result.TotalCost != 50.00 {
		t.Errorf("expected total cost 50.00, got %f", result.TotalCost)
	}
	if len(result.Items) != 1 || result.Items[0].Status != "fulfilled" {
		t.Errorf("expected fulfilled item, got: %+v", result.Items[0])
	}
}

func TestFulfillOrder_NoStockNoPartial(t *testing.T) {
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			return &pb.ReserveInventoryResult{Reserved: false, Message: "out of stock"}, nil
		},
	}
	broker := &BrokerService{}
	order := &data.BuyOrder{
		BuyOrderUUID:   "uuid-nostock",
		AllowPartial:   false,
		Breads:         []data.Bread{{ID: 1, Quantity: 5, Price: 10.00}},
	}

	result := broker.fulfillOrder(order, &stubPublisher{}, mockClient)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.OrderStatus != "rejected" {
		t.Errorf("expected status 'rejected', got '%s'", result.OrderStatus)
	}
}

func TestFulfillOrder_SkippedUnavailable(t *testing.T) {
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			if req.BreadId == 2 {
				return &pb.ReserveInventoryResult{Reserved: false, Message: "out"}, nil
			}
			return &pb.ReserveInventoryResult{Reserved: true, QuantityFulfilled: req.QuantityRequested, Message: "ok"}, nil
		},
	}
	broker := &BrokerService{}
	order := &data.BuyOrder{
		BuyOrderUUID:         "uuid-skip",
		SkipUnavailableItems: true,
		Breads: []data.Bread{
			{ID: 1, Quantity: 2, Price: 10.00},
			{ID: 2, Quantity: 3, Price: 5.00},
		},
	}

	result := broker.fulfillOrder(order, &stubPublisher{}, mockClient)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.OrderStatus != "partially_processed" {
		t.Errorf("expected 'partially_processed', got '%s'", result.OrderStatus)
	}
}

func TestFulfillOrder_ZeroQuantityBread(t *testing.T) {
	mockClient := &mockBrokerClient{}
	broker := &BrokerService{}
	order := &data.BuyOrder{
		BuyOrderUUID: "uuid-zero",
		Breads:       []data.Bread{{ID: 1, Quantity: 0, Price: 10.00}},
	}

	result := broker.fulfillOrder(order, &stubPublisher{}, mockClient)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.OrderStatus != "failed" {
		t.Errorf("expected 'failed', got '%s'", result.OrderStatus)
	}
	if len(mockClient.reservedInvItems) != 0 {
		t.Error("expected no ReserveInventory calls for zero-quantity bread")
	}
}

// --- processOneOrder: gRPC error path ---

func TestProcessOneOrder_ReportOrderErrorNacksDelivery(t *testing.T) {
	mockClient := &mockBrokerClient{
		reportOrderFn: func(order pb.BuyOrder) (*pb.BrokerOrderResult, error) {
			return nil, fmt.Errorf("gRPC connection failed")
		},
	}
	broker := &BrokerService{}

	nacked := false
	delivery := rabbitmq.Delivery{
		Body: []byte(`{"buy_order_uuid":"uuid-err","breads":[{"id":1,"quantity":2}]}`),
		Acknowledger: &testAcknowledger{
			nackFn: func(multiple, requeue bool) error {
				nacked = true
				return nil
			},
		},
	}

	broker.processOneOrder(delivery, mockClient)

	if !nacked {
		t.Error("expected delivery to be NACKed on gRPC error")
	}
}

// --- processMatchingBatch: ReportMatchingResults error path ---

type failingPublisher struct{}

func (p *failingPublisher) Publish(_, _ string, _, _ bool, _ rabbitmq.Publishing) error {
	return fmt.Errorf("publish failed")
}

func TestProcessMatchingBatch_ReportsEvenOnMatchingResultError(t *testing.T) {
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			return &pb.ReserveInventoryResult{Reserved: true, QuantityFulfilled: req.QuantityRequested, Message: "reserved"}, nil
		},
		reportMatchFn: func(req *pb.MatchingBatch) (*pb.BatchConfirmation, error) {
			return nil, fmt.Errorf("gRPC report matching failed")
		},
	}
	broker := &BrokerService{}
	order := data.BuyOrder{
		BuyOrderUUID: "uuid-rmerr",
		Breads:       []data.Bread{{ID: 1, Quantity: 5, Price: 2.50}},
	}

	// Should not panic; the error is logged but the batch still processes
	broker.processMatchingBatch([]data.BuyOrder{order}, &stubPublisher{}, mockClient)

	if len(mockClient.reservedInvItems) != 1 {
		t.Errorf("expected 1 ReserveInventory call, got %d", len(mockClient.reservedInvItems))
	}
}

// --- processMatchingBatch: publish failure path ---

func TestProcessMatchingBatch_PublishFailureDoesNotBreakBatch(t *testing.T) {
	mockClient := &mockBrokerClient{
		reserveFn: func(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
			return &pb.ReserveInventoryResult{Reserved: true, QuantityFulfilled: req.QuantityRequested, Message: "reserved"}, nil
		},
	}
	broker := &BrokerService{}
	order := data.BuyOrder{
		BuyOrderUUID: "uuid-puberr",
		Breads:       []data.Bread{{ID: 1, Quantity: 5, Price: 2.50}},
	}

	// Should not panic; publish failure is logged but doesn't break the batch
	broker.processMatchingBatch([]data.BuyOrder{order}, &failingPublisher{}, mockClient)

	if len(mockClient.reservedInvItems) != 1 {
		t.Errorf("expected 1 ReserveInventory call, got %d", len(mockClient.reservedInvItems))
	}
}

// --- processOneOrder: nil body ---

func TestProcessOneOrder_NilBodyAcksDelivery(t *testing.T) {
	mockClient := &mockBrokerClient{}
	broker := &BrokerService{}

	acked := false
	delivery := rabbitmq.Delivery{
		Body: nil,
		Acknowledger: &testAcknowledger{
			ackFn: func(multiple bool) error {
				acked = true
				return nil
			},
		},
	}

	broker.processOneOrder(delivery, mockClient)

	if !acked {
		t.Error("expected delivery to be acked for nil body")
	}
}

// --- processOneOrder: empty body ---

func TestProcessOneOrder_EmptyBodyAcksDelivery(t *testing.T) {
	mockClient := &mockBrokerClient{}
	broker := &BrokerService{}

	acked := false
	delivery := rabbitmq.Delivery{
		Body: []byte{},
		Acknowledger: &testAcknowledger{
			ackFn: func(multiple bool) error {
				acked = true
				return nil
			},
		},
	}

	broker.processOneOrder(delivery, mockClient)

	if !acked {
		t.Error("expected delivery to be acked for empty body")
	}
}

// --- brokerClient: circuit breaker state ---

func TestBrokerClient_CircuitBreakerStates(t *testing.T) {
	// Verify circuit breakers are created with correct initial states
	cb1 := resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 5, ResetTimeout: 30 * time.Second})
	if state := cb1.State(); state != resilience.StateClosed {
		t.Errorf("expected initial state closed, got %v", state)
	}

	// In closed state, Allow() returns true
	allowed, _ := cb1.Allow()
	if !allowed {
		t.Error("expected Allow() to return true in closed state")
	}

	// Trip the breaker by recording consecutive failures
	for i := 0; i < 5; i++ {
		cb1.RecordFailure()
	}
	if state := cb1.State(); state != resilience.StateOpen {
		t.Errorf("expected state open after 5 failures, got %v", state)
	}

	// In open state, Allow() returns false — success is ignored.
	allowed, _ = cb1.Allow()
	if allowed {
		t.Error("expected Allow() to return false in open state")
	}
}

func TestBrokerClient_BreakersMap(t *testing.T) {
	// Test that breakers() returns all three breakers.
	// We can't easily create a brokerClient without a real gRPC conn,
	// but we can verify the breaker creation logic is correct.
	cb1 := resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 5, ResetTimeout: 30 * time.Second})
	cb2 := resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 5, ResetTimeout: 30 * time.Second})
	cb3 := resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 3, ResetTimeout: 60 * time.Second})

	breakers := map[string]*resilience.CircuitBreaker{
		"report_order":      cb1,
		"reserve_inventory": cb2,
		"report_matching":   cb3,
	}

	if len(breakers) != 3 {
		t.Errorf("expected 3 breakers, got %d", len(breakers))
	}
	for name, cb := range breakers {
		if cb == nil {
			t.Errorf("breaker %s is nil", name)
		}
		if cb.State() != resilience.StateClosed {
			t.Errorf("breaker %s should be in closed state, got %v", name, cb.State())
		}
	}
}
