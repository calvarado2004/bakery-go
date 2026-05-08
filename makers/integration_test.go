package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/testutils"
	rabbitmq "github.com/rabbitmq/amqp091-go"
)

// testRabbitMQURL is the AMQP URL used by all integration tests.
const testRabbitMQURL = "amqp://guest:guest@localhost:5672/"

func skipIfServerRunning(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "localhost:50051", 500*time.Millisecond)
	if err == nil {
		conn.Close() //nolint:errcheck
		t.Skip("server is running on :50051 and may consume shared queues; skipping makers integration test")
	}
}

// closableTestDialer dials RabbitMQ and holds a reference to the connection
// so tests can close it to unblock the consumer loop.
type closableTestDialer struct {
	url    string
	conn   *rabbitmq.Connection
	connMu sync.Mutex
}

func (d *closableTestDialer) Dial() (*rabbitmq.Connection, error) {
	conn, err := rabbitmq.Dial(d.url)
	if err != nil {
		return nil, err
	}
	d.connMu.Lock()
	d.conn = conn
	d.connMu.Unlock()
	return conn, nil
}

func (d *closableTestDialer) Close() {
	d.connMu.Lock()
	defer d.connMu.Unlock()
	if d.conn != nil {
		d.conn.Close() //nolint:errcheck
	}
}

// ---------------------------------------------------------------------------
// Integration tests for MakersService — real RabbitMQ, no mocks.
//
// Key design to avoid consumer races:
//   - Each test uses a closableTestDialer so we can close the connection
//     to unblock the consumer loop on shutdown (the producers never close)
//   - We consume from bread-made on a SEPARATE channel/connection before
//     starting the makers consumer, so we never miss the confirmation
// ---------------------------------------------------------------------------

func TestMakersService_Integration_MessageFlow(t *testing.T) {
	skipIfServerRunning(t)

	harness := testutils.NewTestHarness(t)
	defer harness.Cleanup()

	if err := harness.DeclareQueues(); err != nil {
		t.Fatalf("declare queues: %v", err)
	}
	if err := harness.PurgeQueues(); err != nil {
		t.Fatalf("purge queues: %v", err)
	}

	// Set up a consumer on bread-made BEFORE starting the makers service
	ch, err := harness.RabbitMQConn().Channel()
	if err != nil {
		t.Fatalf("open consumer channel: %v", err)
	}
	defer ch.Close()

	consumer, err := ch.Consume("bread-made", "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume bread-made: %v", err)
	}

	// Start the makers service with closable dialer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	dialer := &closableTestDialer{url: testRabbitMQURL}
	msvc := NewMakersService(dialer)
	var wg sync.WaitGroup
	msvc.Start(ctx, &wg)

	// Wait for makers to set up its consumer
	time.Sleep(2 * time.Second)

	// Publish a make-bread-order message
	msg := makeBreadMessage{
		ID:          1,
		Name:        "Integration Test Bread",
		Quantity:    10,
		Description: "Test",
		Type:        "Bread",
		Price:       2.99,
		Status:      "pending",
		Image:       "/test.png",
	}
	body, _ := json.Marshal(msg)

	if err := harness.PublishMakeBreadOrder(body); err != nil {
		t.Fatalf("publish make-bread-order: %v", err)
	}

	// Wait for bread-made confirmation
	select {
	case d := <-consumer:
		var confirmation breadMadeMessage
		if err := json.Unmarshal(d.Body, &confirmation); err != nil {
			t.Fatalf("unmarshal bread-made: %v", err)
		}
		if confirmation.BreadID != 1 {
			t.Errorf("expected breadId 1, got %d", confirmation.BreadID)
		}
		if confirmation.Quantity != 10 {
			t.Errorf("expected quantity 10, got %d", confirmation.Quantity)
		}
		d.Ack(false)
		t.Log("Successfully processed make-bread order and received bread-made confirmation")
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for bread-made confirmation")
	}

	// Shutdown: close connection to unblock consumer, then stop
	dialer.Close()
	msvc.Stop()
	cancel()
	wg.Wait()
}

func TestMakersService_Integration_MultipleMessages(t *testing.T) {
	skipIfServerRunning(t)

	harness := testutils.NewTestHarness(t)
	defer harness.Cleanup()

	if err := harness.DeclareQueues(); err != nil {
		t.Fatalf("declare queues: %v", err)
	}
	if err := harness.PurgeQueues(); err != nil {
		t.Fatalf("purge queues: %v", err)
	}

	// Consume bread-made on a dedicated channel
	ch, err := harness.RabbitMQConn().Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer ch.Close()

	consumer, err := ch.Consume("bread-made", "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume bread-made: %v", err)
	}

	// Start makers service
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialer := &closableTestDialer{url: testRabbitMQURL}
	msvc := NewMakersService(dialer)
	var wg sync.WaitGroup
	msvc.Start(ctx, &wg)

	time.Sleep(2 * time.Second)

	// Publish 5 messages
	const msgCount = 5
	for i := 0; i < msgCount; i++ {
		msg := makeBreadMessage{
			ID:       i + 1,
			Name:     fmt.Sprintf("Bread %d", i+1),
			Quantity: 10 + i,
			Type:     "Bread",
			Price:    2.99,
		}
		body, _ := json.Marshal(msg)
		if err := harness.PublishMakeBreadOrder(body); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	// Collect confirmations
	received := 0
	seenIDs := make(map[int]bool)
	timeout := time.After(20 * time.Second)

	for received < msgCount {
		select {
		case d := <-consumer:
			var confirmation breadMadeMessage
			if err := json.Unmarshal(d.Body, &confirmation); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if seenIDs[confirmation.BreadID] {
				t.Errorf("duplicate breadId %d", confirmation.BreadID)
			}
			seenIDs[confirmation.BreadID] = true
			d.Ack(false)
			received++
		case <-timeout:
			t.Fatalf("timeout: received %d/%d messages", received, msgCount)
		}
	}

	t.Logf("Successfully processed %d/%d messages", received, msgCount)

	dialer.Close()
	msvc.Stop()
	cancel()
	wg.Wait()
}

func TestMakersService_Integration_QueueDeclaration(t *testing.T) {
	skipIfServerRunning(t)

	harness := testutils.NewTestHarness(t)
	defer harness.Cleanup()

	// Start makers service
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dialer := &closableTestDialer{url: testRabbitMQURL}
	msvc := NewMakersService(dialer)
	var wg sync.WaitGroup
	msvc.Start(ctx, &wg)

	// Wait for queue to be declared
	time.Sleep(3 * time.Second)

	// Inspect the make-bread-order queue
	info, err := harness.RabbitMQChannel().QueueInspect("make-bread-order")
	if err != nil {
		t.Fatalf("QueueInspect make-bread-order: %v", err)
	}

	t.Logf("make-bread-order queue: name=%q, messages=%d, consumers=%d", info.Name, info.Messages, info.Consumers)
	if info.Consumers < 1 {
		t.Error("expected at least 1 consumer on make-bread-order queue")
	}

	dialer.Close()
	msvc.Stop()
	cancel()
	wg.Wait()
}

func TestMakersService_Integration_MessageFormatCompatibility(t *testing.T) {
	skipIfServerRunning(t)

	harness := testutils.NewTestHarness(t)
	defer harness.Cleanup()

	if err := harness.DeclareQueues(); err != nil {
		t.Fatalf("declare queues: %v", err)
	}
	if err := harness.PurgeQueues(); err != nil {
		t.Fatalf("purge queues: %v", err)
	}

	// Set up consumer
	ch, err := harness.RabbitMQConn().Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	defer ch.Close()

	consumer, err := ch.Consume("bread-made", "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}

	// Start makers
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialer := &closableTestDialer{url: testRabbitMQURL}
	msvc := NewMakersService(dialer)
	var wg sync.WaitGroup
	msvc.Start(ctx, &wg)

	time.Sleep(2 * time.Second)

	// Test various message formats
	testCases := []struct {
		name        string
		msg         makeBreadMessage
		expectBread int
		expectQty   int
	}{
		{"basic", makeBreadMessage{ID: 10, Name: "Basic", Quantity: 5}, 10, 5},
		{"zero qty", makeBreadMessage{ID: 11, Name: "Zero", Quantity: 0}, 11, 0},
		{"large qty", makeBreadMessage{ID: 12, Name: "Large", Quantity: 999}, 12, 999},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.msg)
			if err := harness.PublishMakeBreadOrder(body); err != nil {
				t.Fatalf("publish: %v", err)
			}

			select {
			case d := <-consumer:
				var confirmation breadMadeMessage
				if err := json.Unmarshal(d.Body, &confirmation); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if confirmation.BreadID != tc.expectBread {
					t.Errorf("breadId: expected %d, got %d", tc.expectBread, confirmation.BreadID)
				}
				if confirmation.Quantity != tc.expectQty {
					t.Errorf("quantity: expected %d, got %d", tc.expectQty, confirmation.Quantity)
				}
				d.Ack(false)
			case <-time.After(10 * time.Second):
				t.Fatalf("timeout for message %s", tc.name)
			}
		})
	}

	dialer.Close()
	msvc.Stop()
	cancel()
	wg.Wait()
}

func TestMakersService_Integration_PublishToBreadMade(t *testing.T) {
	skipIfServerRunning(t)

	harness := testutils.NewTestHarness(t)
	defer harness.Cleanup()

	if err := harness.DeclareQueues(); err != nil {
		t.Fatalf("declare queues: %v", err)
	}
	if err := harness.PurgeQueues(); err != nil {
		t.Fatalf("purge queues: %v", err)
	}

	// Start makers
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialer := &closableTestDialer{url: testRabbitMQURL}
	msvc := NewMakersService(dialer)
	var wg sync.WaitGroup
	msvc.Start(ctx, &wg)

	time.Sleep(2 * time.Second)

	// Publish a message
	msg := makeBreadMessage{ID: 3, Name: "Test", Quantity: 5}
	body, _ := json.Marshal(msg)
	if err := harness.PublishMakeBreadOrder(body); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Inspect bread-made queue
	time.Sleep(3 * time.Second)
	info, err := harness.RabbitMQChannel().QueueInspect("bread-made")
	if err != nil {
		t.Fatalf("QueueInspect bread-made: %v", err)
	}

	t.Logf("bread-made queue: messages=%d", info.Messages)
	if info.Messages < 1 {
		t.Error("expected at least 1 message in bread-made queue")
	}

	dialer.Close()
	msvc.Stop()
	cancel()
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Lifecycle tests (no real RabbitMQ needed)
// ---------------------------------------------------------------------------

func TestMakersService_Lifecycle_Stop(t *testing.T) {
	svc := NewMakersService(&errDialer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	svc.Start(ctx, &wg)

	svc.Stop()
	cancel()
	time.Sleep(500 * time.Millisecond)

	// Double-stop should be safe
	svc.Stop()
}

func TestMakersService_Lifecycle_ContextCancel(t *testing.T) {
	svc := NewMakersService(&errDialer{})
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	svc.Start(ctx, &wg)

	// Cancel context to stop the service
	cancel()
	svc.Stop()

	// Wait for goroutine to exit (with timeout to avoid hanging)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(10 * time.Second):
		t.Fatal("makers service did not stop after context cancellation")
	}
}

// errDialer returns an error on Dial — used for lifecycle tests that
// don't need a real RabbitMQ connection.
type errDialer struct{}

func (d *errDialer) Dial() (*rabbitmq.Connection, error) {
	return nil, fmt.Errorf("errDialer: no real connection")
}
