package main

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/calvarado2004/bakery-go/data"
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
	broker := &RabbitMQBakery{
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
	broker := &RabbitMQBakery{}

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
	broker := &RabbitMQBakery{}

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

	broker := &RabbitMQBakery{}
	order := data.BuyOrder{
		BuyOrderUUID: "uuid-match-1",
		Breads:       []data.Bread{{ID: 1, Quantity: 5, Price: 2.50}},
	}

	broker.processMatchingBatch([]data.BuyOrder{order}, &stubPublisher{}, mockClient)

	if len(mockClient.reservedInvItems) != 1 {
		t.Errorf("expected 1 ReserveInventory call, got %d", len(mockClient.reservedInvItems))
	}
}

// --- NewRabbitMQBakery tests ---

func TestNewRabbitMQBakery_SetsFields(t *testing.T) {
	cfg := brokerConfig{}
	b := NewRabbitMQBakery(cfg, "amqp://localhost:5672")

	if b.rabbitmqURL != "amqp://localhost:5672" {
		t.Errorf("unexpected rabbitmqURL: %s", b.rabbitmqURL)
	}
}

func TestNewRabbitMQBakery_EmptyURL(t *testing.T) {
	b := NewRabbitMQBakery(brokerConfig{}, "")
	if b == nil {
		t.Fatal("expected non-nil RabbitMQBakery")
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
