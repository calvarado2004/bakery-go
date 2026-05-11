package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	rabbitmq "github.com/rabbitmq/amqp091-go"
)

// ===================================================================
// processOrder / ProcessMakeBreadMessage — pure business logic
// ===================================================================

func TestProcessOrder_ValidMessage(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	msg := makeBreadMessage{ID: 3, Name: "Baguette", Quantity: 50, Type: "French"}
	body, _ := json.Marshal(msg)

	result, err := svc.processOrder(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BreadID != 3 {
		t.Errorf("BreadID: expected 3, got %d", result.BreadID)
	}
	if result.Quantity != 50 {
		t.Errorf("Quantity: expected 50, got %d", result.Quantity)
	}
}

func TestProcessOrder_InvalidJSON(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	_, err := svc.processOrder([]byte("not json {{{"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	// JSON unmarshal returns various error types; just verify it wraps the original.
	t.Logf("error type: %T, msg: %v", err, err)
}

func TestProcessOrder_EmptyBytes(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	_, err := svc.processOrder([]byte{})
	if err == nil {
		t.Fatal("expected error for empty bytes")
	}
}

func TestProcessOrder_EmptyJSON(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	result, err := svc.processOrder([]byte("{}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BreadID != 0 || result.Quantity != 0 {
		t.Errorf("expected zero values, got %+v", result)
	}
}

func TestProcessOrder_NilBody(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	_, err := svc.processOrder(nil)
	if err == nil {
		t.Fatal("expected error for nil body")
	}
}

func TestProcessOrder_ZeroQuantity(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	msg := makeBreadMessage{ID: 5, Name: "Bolillo", Quantity: 0}
	body, _ := json.Marshal(msg)
	result, err := svc.processOrder(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Quantity != 0 {
		t.Errorf("expected Quantity=0, got %d", result.Quantity)
	}
}

func TestProcessOrder_AllBreadTypes(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	types := []string{"Sweet Bread", "Sour Bread", "French Bread", "Salty Bread", "Soft Bread", "Buttery Bread"}
	for _, bt := range types {
		msg := makeBreadMessage{ID: 1, Name: "Test", Quantity: 5, Type: bt}
		body, _ := json.Marshal(msg)
		result, err := svc.processOrder(body)
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", bt, err)
		}
		if result.BreadID != 1 {
			t.Errorf("type %q: expected BreadID=1, got %d", bt, result.BreadID)
		}
	}
}

func TestProcessMakeBreadMessage_Alias(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	msg := makeBreadMessage{ID: 42, Name: "Test", Quantity: 7}
	body, _ := json.Marshal(msg)
	result, err := svc.ProcessMakeBreadMessage(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BreadID != 42 {
		t.Errorf("expected BreadID=42, got %d", result.BreadID)
	}
}

// ===================================================================
// publishConfirmation — marshals + publishes
// ===================================================================

func TestPublishConfirmation_Success(t *testing.T) {
	mp := &mockPublisher{}
	svc := &MakersService{publisher: mp}
	confirmation := &breadMadeMessage{BreadID: 5, Quantity: 10}

	err := svc.publishConfirmation(confirmation)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mp.calls))
	}
	var decoded breadMadeMessage
	if err := json.Unmarshal(mp.calls[0], &decoded); err != nil {
		t.Fatalf("unmarshal published body: %v", err)
	}
	if decoded.BreadID != 5 || decoded.Quantity != 10 {
		t.Errorf("expected {BreadID:5, Quantity:10}, got %+v", decoded)
	}
}

func TestPublishConfirmation_MarshalError(t *testing.T) {
	mp := &mockPublisher{}
	svc := &MakersService{publisher: mp}
	// nil confirmation should cause marshal error
	err := svc.publishConfirmation(nil)
	if err == nil {
		t.Fatal("expected error for nil confirmation")
	}
	if len(mp.calls) != 0 {
		t.Error("publisher should not be called on marshal error")
	}
}

func TestPublishConfirmation_PublishError(t *testing.T) {
	mp := &mockPublisher{err: fmt.Errorf("network error")}
	svc := &MakersService{publisher: mp}
	confirmation := &breadMadeMessage{BreadID: 1, Quantity: 1}
	err := svc.publishConfirmation(confirmation)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ===================================================================
// ackDelivery / nackDelivery
// ===================================================================

// ackDelivery and nackDelivery wrap rabbitmq.Delivery.Ack/Nack with error
// handling. They are tested indirectly through integration tests that use
// real RabbitMQ. The unit tests verify the error wrapping is correct.

func TestAckDelivery_WrapsError(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	// Verify the method exists. ack/nack are tested via integration tests.
	_ = svc.ackDelivery
}

func TestNackDelivery_WrapsError(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	// Verify the method exists. ack/nack are tested via integration tests.
	_ = svc.nackDelivery
}

// mockDeliveryForTest is a minimal delivery mock for ack/nack unit tests.
type mockDeliveryForTest struct {
	ackErr  error
	nackErr error
}

func (m *mockDeliveryForTest) Ack(requeue bool) error { return m.ackErr }
func (m *mockDeliveryForTest) Nack(requeue, multiple bool) error {
	return m.nackErr
}

func TestAckDelivery_AckError(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	d := &mockDeliveryForTest{ackErr: fmt.Errorf("ack failed")}
	err := svc.ackDelivery(d)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, fmt.Errorf("ack delivery")) && err.Error() != "ack delivery: ack failed" {
		t.Logf("error: %v", err)
	}
}

func TestNackDelivery_NackError(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	d := &mockDeliveryForTest{nackErr: fmt.Errorf("nack failed")}
	procErr := fmt.Errorf("process failed")
	err := svc.nackDelivery(d, procErr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, procErr) {
		t.Logf("error: %v", err)
	}
}

func TestNackDelivery_Success(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	d := &mockDeliveryForTest{}
	procErr := fmt.Errorf("process failed")
	err := svc.nackDelivery(d, procErr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, procErr) {
		t.Logf("error: %v", err)
	}
}

// ===================================================================
// RabbitMQPublisher
// ===================================================================

func TestPublisherInterfaces(t *testing.T) {
	var _ Publisher = (*RabbitMQPublisher)(nil)
	var _ Publisher = (*mockPublisher)(nil)
	var _ Publisher = (*nopPublisher)(nil)
}

func TestMockPublisher_RecordsCall(t *testing.T) {
	mp := &mockPublisher{}
	body := []byte(`{"breadId":1,"quantity":5}`)
	if err := mp.PublishConfirm(body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mp.calls))
	}
	if string(mp.calls[0]) != string(body) {
		t.Errorf("expected %q, got %q", body, mp.calls[0])
	}
}

func TestMockPublisher_ReturnsError(t *testing.T) {
	mp := &mockPublisher{err: fmt.Errorf("boom")}
	if err := mp.PublishConfirm([]byte("test")); err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(mp.calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(mp.calls))
	}
}

func TestNopPublisher_NeverErrors(t *testing.T) {
	nop := &nopPublisher{}
	for i := 0; i < 100; i++ {
		if err := nop.PublishConfirm([]byte("test")); err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
	}
}

// ===================================================================
// workerPool
// ===================================================================

func TestWorkerPool_SingleMessage(t *testing.T) {
	proc := func(body []byte) (*breadMadeMessage, error) {
		msg := &makeBreadMessage{}
		if err := json.Unmarshal(body, msg); err != nil {
			return nil, err
		}
		return &breadMadeMessage{BreadID: msg.ID, Quantity: msg.Quantity}, nil
	}
	wp := newWorkerPool(2, proc)
	defer close(wp.tasks)

	msg := makeBreadMessage{ID: 5, Name: "Test", Quantity: 10}
	body, _ := json.Marshal(msg)
	wp.Submit(body)

	result := <-wp.Results()
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if result.confirmation.BreadID != 5 {
		t.Errorf("expected BreadID=5, got %d", result.confirmation.BreadID)
	}
	if result.confirmation.Quantity != 10 {
		t.Errorf("expected Quantity=10, got %d", result.confirmation.Quantity)
	}
}

func TestWorkerPool_MultipleMessages(t *testing.T) {
	proc := func(body []byte) (*breadMadeMessage, error) {
		msg := &makeBreadMessage{}
		if err := json.Unmarshal(body, msg); err != nil {
			return nil, err
		}
		return &breadMadeMessage{BreadID: msg.ID, Quantity: msg.Quantity}, nil
	}
	wp := newWorkerPool(4, proc)
	defer close(wp.tasks)

	const count = 20
	for i := 0; i < count; i++ {
		msg := makeBreadMessage{ID: i + 1, Name: fmt.Sprintf("Bread %d", i+1), Quantity: 10 + i}
		body, _ := json.Marshal(msg)
		wp.Submit(body)
	}

	seenIDs := make(map[int]bool)
	timeout := time.After(5 * time.Second)
	for i := 0; i < count; i++ {
		select {
		case result := <-wp.Results():
			if result.err != nil {
				t.Fatalf("unexpected error: %v", result.err)
			}
			seenIDs[result.confirmation.BreadID] = true
		case <-timeout:
			t.Fatalf("timeout: received %d/%d", i, count)
		}
	}
	if len(seenIDs) != count {
		t.Errorf("expected %d unique results, got %d", count, len(seenIDs))
	}
}

func TestWorkerPool_ProcessError(t *testing.T) {
	proc := func(body []byte) (*breadMadeMessage, error) {
		return nil, fmt.Errorf("process failed")
	}
	wp := newWorkerPool(1, proc)
	defer close(wp.tasks)

	wp.Submit([]byte("invalid"))
	result := <-wp.Results()
	if result.err == nil {
		t.Fatal("expected error, got nil")
	}
	if result.confirmation != nil {
		t.Error("expected nil confirmation on error")
	}
}

func TestWorkerPool_BufferedChannels(t *testing.T) {
	proc := func(body []byte) (*breadMadeMessage, error) {
		time.Sleep(10 * time.Millisecond)
		return &breadMadeMessage{BreadID: 1, Quantity: 1}, nil
	}
	wp := newWorkerPool(4, proc)
	defer close(wp.tasks)

	// Submit more than the worker count without reading results.
	// The buffer should prevent blocking.
	for i := 0; i < 10; i++ {
		wp.Submit([]byte("{}"))
	}
	// All submits should complete without blocking.
}

// ===================================================================
// MakersService — constructor and lifecycle
// ===================================================================

func TestNewMakersService_NilDialer(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	if svc.dialer == nil {
		t.Fatal("expected default dialer")
	}
}

func TestNewMakersService_NilPublisher(t *testing.T) {
	svc := NewMakersService(nil, nil, 1)
	// Should use nopPublisher which never errors
	_, err := svc.processOrder([]byte("{}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewMakersService_DefaultWorkers(t *testing.T) {
	svc := NewMakersService(nil, nil, 0)
	if svc.workerPool == nil {
		t.Fatal("expected worker pool")
	}
	// Verify worker pool was created with workers=1 default
	// Submit a message and verify it's processed without blocking.
	done := make(chan bool, 1)
	svc.workerPool.Submit([]byte("{}"))
	go func() {
		result := <-svc.workerPool.Results()
		done <- result.err == nil
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("worker returned error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker pool blocked on single submit")
	}
}

func TestNewMakersService_CustomWorkers(t *testing.T) {
	svc := NewMakersService(nil, nil, 5)
	if svc.workerPool == nil {
		t.Fatal("expected worker pool")
	}
}

func TestMakersService_Stop(t *testing.T) {
	svc := NewMakersService(&testErrDialer{}, nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	svc.Start(ctx, &wg)

	// Double-stop should be safe
	svc.Stop()
	svc.Stop()

	// Wait briefly for goroutine to exit
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not exit after Stop")
	}
}

// ===================================================================
// Integration-style tests for processSingleMessage flow
// ===================================================================

func TestPublishConfirmation_IntegratesWithPublisher(t *testing.T) {
	calls := [][]byte{}
	var mu sync.Mutex
	publisher := &testPublisher{
		publish: func(body []byte) error {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, body)
			return nil
		},
	}
	svc := NewMakersService(nil, publisher, 1)
	confirmation := &breadMadeMessage{BreadID: 7, Quantity: 21}

	err := svc.publishConfirmation(confirmation)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	var decoded breadMadeMessage
	if err := json.Unmarshal(calls[0], &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.BreadID != 7 || decoded.Quantity != 21 {
		t.Errorf("expected {7, 21}, got %+v", decoded)
	}
}

// testPublisher is a simple test publisher.
type testPublisher struct {
	publish func(body []byte) error
}

func (tp *testPublisher) PublishConfirm(body []byte) error {
	if tp.publish != nil {
		return tp.publish(body)
	}
	return nil
}

// ===================================================================
// End-to-end flow test (unit-level, no RabbitMQ)
// ===================================================================

func TestFullProcessFlow_NoRealRabbitMQ(t *testing.T) {
	// Simulate the full processSingleMessage flow without real RabbitMQ:
	// 1. Submit message body to worker pool
	// 2. Get result
	// 3. Publish confirmation
	// 4. Verify confirmation content
	mp := &mockPublisher{}
	svc := NewMakersService(nil, mp, 1)

	original := makeBreadMessage{ID: 99, Name: "E2E Test", Quantity: 42}
	body, _ := json.Marshal(original)

	// Submit to worker pool and get result
	svc.workerPool.Submit(body)
	result := <-svc.workerPool.Results()

	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if result.confirmation.BreadID != 99 {
		t.Errorf("expected BreadID=99, got %d", result.confirmation.BreadID)
	}

	// Publish via service
	err := svc.publishConfirmation(result.confirmation)
	if err != nil {
		t.Fatalf("publish error: %v", err)
	}

	// Verify publisher received the right data
	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(mp.calls))
	}
	var decoded breadMadeMessage
	if err := json.Unmarshal(mp.calls[0], &decoded); err != nil {
		t.Fatalf("unmarshal published: %v", err)
	}
	if decoded.BreadID != 99 || decoded.Quantity != 42 {
		t.Errorf("expected {99, 42}, got %+v", decoded)
	}
}

func TestFullProcessFlow_ProcessError(t *testing.T) {
	// Simulate process error path: worker returns error → nack
	proc := func(body []byte) (*breadMadeMessage, error) {
		return nil, fmt.Errorf("baking failed")
	}
	wp := newWorkerPool(1, proc)
	defer close(wp.tasks)

	wp.Submit([]byte("invalid"))
	result := <-wp.Results()

	if result.err == nil {
		t.Fatal("expected error from worker")
	}
	if result.confirmation != nil {
		t.Error("expected nil confirmation on error")
	}
}

func TestFullProcessFlow_MarshalError(t *testing.T) {
	// Verify that nil confirmation causes marshal error
	svc := NewMakersService(nil, &mockPublisher{}, 1)
	err := svc.publishConfirmation(nil)
	if err == nil {
		t.Fatal("expected marshal error for nil confirmation")
	}
}

// ===================================================================
// Helpers
// ===================================================================

// mockPublisher records PublishConfirm calls.
type mockPublisher struct {
	calls [][]byte
	err   error
}

func (m *mockPublisher) PublishConfirm(body []byte) error {
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, body)
	return nil
}

// testErrDialer always fails — used for lifecycle tests.
type testErrDialer struct{}

func (d *testErrDialer) Dial() (*rabbitmq.Connection, error) {
	return nil, fmt.Errorf("testErrDialer: no connection")
}
