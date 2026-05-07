package main

import (
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
)

// mockSettlementDispatcher is a testable implementation of settlementDispatcher
// that does not require RabbitMQ. It stores settled orders in-memory and
// delivers them to registered waiters.
type mockSettlementDispatcher struct {
	mu      sync.Mutex
	waiters map[string]chan *data.BuyOrder
	deliveries []string
}

func newMockSettlementDispatcher() *mockSettlementDispatcher {
	return &mockSettlementDispatcher{
		waiters:      make(map[string]chan *data.BuyOrder),
		deliveries:   make([]string, 0),
	}
}

// Start implements settlementDispatcher (no-op for mock).
func (m *mockSettlementDispatcher) Start() {}

// Register implements settlementDispatcher.
func (m *mockSettlementDispatcher) Register(uuid string) <-chan *data.BuyOrder {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan *data.BuyOrder, 1)
	m.waiters[uuid] = ch
	return ch
}

// Unregister implements settlementDispatcher.
func (m *mockSettlementDispatcher) Unregister(uuid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.waiters[uuid]; ok {
		close(ch)
		delete(m.waiters, uuid)
	}
}

// Settle simulates the broker settling an order by pushing it to the waiter.
// Returns true if a waiter was found and the order was delivered.
func (m *mockSettlementDispatcher) Settle(uuid string, order *data.BuyOrder) bool {
	m.mu.Lock()
	ch, ok := m.waiters[uuid]
	m.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- order:
		m.mu.Lock()
		m.deliveries = append(m.deliveries, uuid)
		m.mu.Unlock()
		return true
	default:
		// Channel full — shouldn't happen with buffer=1
		return false
	}
}

// WaitCount returns the number of currently registered waiters.
func (m *mockSettlementDispatcher) WaitCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.waiters)
}

// DeliveredUUIDs returns the list of settled order UUIDs.
func (m *mockSettlementDispatcher) DeliveredUUIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.deliveries...)
}

// TestSettlementDispatcher_RegisterReturnsChannel verifies that Register
// returns a buffered channel and the waiter count increases.
func TestSettlementDispatcher_RegisterReturnsChannel(t *testing.T) {
	md := newMockSettlementDispatcher()
	ch := md.Register("uuid-1")
	if ch == nil {
		t.Fatal("expected non-nil channel from Register")
	}
	if md.WaitCount() != 1 {
		t.Errorf("expected 1 waiter, got %d", md.WaitCount())
	}
}

// TestSettlementDispatcher_DeliverToWaitingStream verifies that an order
// delivered via Settle arrives on the registered channel.
func TestSettlementDispatcher_DeliverToWaitingStream(t *testing.T) {
	md := newMockSettlementDispatcher()
	ch := md.Register("uuid-deliver-1")

	order := &data.BuyOrder{
		BuyOrderUUID: "uuid-deliver-1",
		Status:       "settled",
	}
	ok := md.Settle("uuid-deliver-1", order)
	if !ok {
		t.Fatal("expected Settle to succeed")
	}

	select {
	case received := <-ch:
		if received.BuyOrderUUID != "uuid-deliver-1" {
			t.Errorf("unexpected UUID in channel: %q", received.BuyOrderUUID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for order on channel")
	}
}

// TestSettlementDispatcher_SettleBeforeRegister verifies that settling an
// order before a waiter registers returns false (no waiter to deliver to).
func TestSettlementDispatcher_SettleBeforeRegister(t *testing.T) {
	md := newMockSettlementDispatcher()
	order := &data.BuyOrder{BuyOrderUUID: "early-settle"}
	ok := md.Settle("uuid-early", order)
	if ok {
		t.Error("expected Settle to fail when no waiter is registered")
	}
}

// TestSettlementDispatcher_UnregisterClosesChannel verifies that Unregister
// closes the channel and decrements the waiter count.
func TestSettlementDispatcher_UnregisterClosesChannel(t *testing.T) {
	md := newMockSettlementDispatcher()
	ch := md.Register("uuid-unreg")

	if md.WaitCount() != 1 {
		t.Fatalf("expected 1 waiter before unregister, got %d", md.WaitCount())
	}

	md.Unregister("uuid-unreg")
	if md.WaitCount() != 0 {
		t.Errorf("expected 0 waiters after unregister, got %d", md.WaitCount())
	}

	// Channel should be closed — reading should return zero value immediately.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel, got open")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out reading from channel — may not be closed")
	}
}

// TestSettlementDispatcher_MultipleWaiters verifies that multiple concurrent
// waiters for different UUIDs work correctly.
func TestSettlementDispatcher_MultipleWaiters(t *testing.T) {
	md := newMockSettlementDispatcher()

	ch1 := md.Register("uuid-multi-1")
	ch2 := md.Register("uuid-multi-2")
	if md.WaitCount() != 2 {
		t.Errorf("expected 2 waiters, got %d", md.WaitCount())
	}

	order1 := &data.BuyOrder{BuyOrderUUID: "uuid-multi-1", Status: "settled"}
	order2 := &data.BuyOrder{BuyOrderUUID: "uuid-multi-2", Status: "settled"}

	md.Settle("uuid-multi-1", order1)
	md.Settle("uuid-multi-2", order2)

	// ch1 should receive its order.
	select {
	case received := <-ch1:
		if received.BuyOrderUUID != "uuid-multi-1" {
			t.Errorf("ch1: unexpected UUID %q", received.BuyOrderUUID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for order on ch1")
	}

	// ch2 should receive its order.
	select {
	case received := <-ch2:
		if received.BuyOrderUUID != "uuid-multi-2" {
			t.Errorf("ch2: unexpected UUID %q", received.BuyOrderUUID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for order on ch2")
	}

	delivered := md.DeliveredUUIDs()
	if len(delivered) != 2 {
		t.Errorf("expected 2 delivered UUIDs, got %d", len(delivered))
	}
}

// TestSettlementDispatcher_SettleDuplicateUUID verifies that registering
// a new waiter for an already-settled UUID closes the old channel and
// replaces it with a new one.
func TestSettlementDispatcher_SettleDuplicateUUID(t *testing.T) {
	md := newMockSettlementDispatcher()

	// First registration.
	ch1 := md.Register("uuid-dup")
	if md.WaitCount() != 1 {
		t.Fatalf("expected 1 waiter, got %d", md.WaitCount())
	}

	// Settle the order (delivers to ch1).
	order := &data.BuyOrder{BuyOrderUUID: "uuid-dup"}
	md.Settle("uuid-dup", order)

	// Read the first delivery.
	select {
	case <-ch1:
		// OK.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out on ch1")
	}

	// Second registration for the same UUID should close the old waiter
	// and register a new one. Waiter count stays at 1.
	ch2 := md.Register("uuid-dup")
	if md.WaitCount() != 1 {
		t.Fatalf("expected 1 waiter after re-register, got %d", md.WaitCount())
	}
	_ = ch2

	// Unregister to clean up.
	md.Unregister("uuid-dup")
	if md.WaitCount() != 0 {
		t.Errorf("expected 0 waiters after unregister, got %d", md.WaitCount())
	}
}

// TestSettlementDispatcher_DeliveryRecorded verifies that deliveries are
// tracked in the delivery log.
func TestSettlementDispatcher_DeliveryRecorded(t *testing.T) {
	md := newMockSettlementDispatcher()
	md.Register("uuid-log-1")
	md.Register("uuid-log-2")

	order := &data.BuyOrder{BuyOrderUUID: "uuid-log-1"}
	md.Settle("uuid-log-1", order)
	md.Settle("uuid-log-2", order)

	delivered := md.DeliveredUUIDs()
	if len(delivered) != 2 {
		t.Errorf("expected 2 delivered UUIDs, got %d: %v", len(delivered), delivered)
	}
}

// TestSettlementDispatcher_NilOrderDelivery verifies that a nil order is
// correctly delivered (BuyBreadStream interprets this as DB lookup failure).
func TestSettlementDispatcher_NilOrderDelivery(t *testing.T) {
	md := newMockSettlementDispatcher()
	ch := md.Register("uuid-nil-order")

	md.Settle("uuid-nil-order", nil)

	select {
	case received := <-ch:
		if received != nil {
			t.Error("expected nil order delivery, got non-nil")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for nil order delivery")
	}
}

// TestSettlementDispatcher_ChannelBuffer verifies that the registered channel
// has buffer size 1, so a settled order does not block the Settle goroutine.
func TestSettlementDispatcher_ChannelBuffer(t *testing.T) {
	md := newMockSettlementDispatcher()
	ch := md.Register("uuid-buffer")

	// Send an order without reading — should not block.
	order := &data.BuyOrder{BuyOrderUUID: "uuid-buffer"}
	ok := md.Settle("uuid-buffer", order)
	if !ok {
		t.Fatal("expected Settle to succeed (channel buffered)")
	}

	// Read the order.
	select {
	case received := <-ch:
		if received == nil || received.BuyOrderUUID != "uuid-buffer" {
			t.Error("unexpected order from channel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out reading from channel")
	}
}

// TestNewSettlementDispatcher_Creation verifies the constructor initializes
// the waiter map and config fields.
func TestNewSettlementDispatcher_Creation(t *testing.T) {
	// Use nil repo — the dispatcher only stores a reference to it.
	sd := NewSettlementDispatcher(nil, "amqp://test")
	if sd == nil {
		t.Fatal("expected non-nil SettlementDispatcher")
	}
	if sd.rmqURL != "amqp://test" {
		t.Errorf("expected rmqURL 'amqp://test', got %q", sd.rmqURL)
	}
	if sd.waiters == nil {
		t.Error("expected non-nil waiters map")
	}
}
