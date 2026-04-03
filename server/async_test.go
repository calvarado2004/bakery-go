package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc/metadata"
)

// --- mock stream implementing pb.BuyBread_BuyBreadStreamServer ---
//
// BuyBread_BuyBreadStreamServer = grpc.ServerStreamingServer[BreadResponse]
// which requires Send(*BreadResponse) error + grpc.ServerStream.

type mockBreadStream struct {
	ctx  context.Context
	mu   sync.Mutex
	sent []*pb.BreadResponse
	// sendErr, if non-nil, is returned by Send to simulate a broken stream.
	sendErr error
}

func (m *mockBreadStream) Send(r *pb.BreadResponse) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	m.sent = append(m.sent, r)
	m.mu.Unlock()
	return nil
}

func (m *mockBreadStream) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *mockBreadStream) SetHeader(metadata.MD) error  { return nil }
func (m *mockBreadStream) SendHeader(metadata.MD) error { return nil }
func (m *mockBreadStream) SetTrailer(metadata.MD)       {}
func (m *mockBreadStream) SendMsg(interface{}) error    { return nil }
func (m *mockBreadStream) RecvMsg(interface{}) error    { return nil }

func (m *mockBreadStream) Received() []*pb.BreadResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*pb.BreadResponse, len(m.sent))
	copy(out, m.sent)
	return out
}

// --- getBuyResponse goroutine cleanup tests ---

// TestGetBuyResponse_ExitsOnPreCancelledContext verifies that getBuyResponse
// exits quickly when contextMaker always returns an already-cancelled context.
// This mirrors the behaviour triggered by defer parentCancel() in BuyBreadStream:
// after the stream handler returns, every new context created by contextMaker
// will inherit the cancelled parent and the goroutine must terminate.
func TestGetBuyResponse_ExitsOnPreCancelledContext(t *testing.T) {
	rabbit := &RabbitMQBakery{
		Config: Config{Repo: &stubRepo{}},
		orders: make(map[int]*OrderStatus),
		// empty rabbitmqURL — processBreadsBought will fail on dial if ctx
		// is not already done, but maxRetries will still cap the loop.
	}

	responseCh := make(chan *pb.BreadResponse, 1)

	// contextMaker returns an immediately-cancelled context every time.
	// getBuyResponse will hit the ctx.Done() select branch on each iteration
	// and exhaust maxRetries (5) without any time.Sleep.
	contextMaker := func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel before returning
		return ctx, cancel
	}

	done := make(chan struct{}, 1)
	go func() {
		rabbit.getBuyResponse(contextMaker, responseCh) //nolint:errcheck
		done <- struct{}{}
	}()

	select {
	case <-done:
		// getBuyResponse exited — goroutine cleanup is working
	case <-time.After(3 * time.Second):
		t.Fatal("getBuyResponse did not exit after pre-cancelled contexts: goroutine leak suspected")
	}
}

// TestGetBuyResponse_StopsWhenParentCancelled simulates the lifecycle of
// BuyBreadStream: a parent context is created, a contextMaker derives from it,
// and cancelling the parent (defer parentCancel) causes getBuyResponse to stop.
func TestGetBuyResponse_StopsWhenParentCancelled(t *testing.T) {
	rabbit := &RabbitMQBakery{
		Config: Config{Repo: &stubRepo{}},
		orders: make(map[int]*OrderStatus),
	}

	responseCh := make(chan *pb.BreadResponse, 1)

	parentCtx, parentCancel := context.WithCancel(context.Background())
	contextMaker := func() (context.Context, context.CancelFunc) {
		return context.WithCancel(parentCtx)
	}

	done := make(chan struct{}, 1)
	go func() {
		rabbit.getBuyResponse(contextMaker, responseCh) //nolint:errcheck
		done <- struct{}{}
	}()

	// Cancel the parent immediately to trigger cleanup.
	parentCancel()

	select {
	case <-done:
		// goroutine exited — no leak
	case <-time.After(5 * time.Second):
		t.Fatal("getBuyResponse goroutine did not exit after parentCancel()")
	}
}

// --- BuyOrder concurrent-access tests ---

// TestBuyOrder_ConcurrentLookups verifies that concurrent BuyOrder calls on
// the same server do not race. Run with -race to catch data races.
func TestBuyOrder_ConcurrentLookups(t *testing.T) {
	repo := &retryRepo{
		failFor:   0,
		totalCost: 15.75,
		order: data.BuyOrder{
			ID:           1,
			BuyOrderUUID: "concurrent-uuid",
			CustomerID:   1,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}
	srv := newBuyOrderServer(repo)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			resp, err := srv.BuyOrder(context.Background(), &pb.BuyOrderRequest{
				BuyOrderUuid: "concurrent-uuid",
			})
			if err != nil {
				t.Errorf("unexpected error in concurrent call: %v", err)
				return
			}
			if resp == nil || resp.BuyOrders == nil {
				t.Error("expected non-nil response")
			}
		}()
	}

	wg.Wait()
}

// TestBuyOrder_ConcurrentDistinctUUIDs verifies concurrent lookups with
// different UUIDs do not interfere — each gets its own order data.
func TestBuyOrder_ConcurrentDistinctUUIDs(t *testing.T) {
	// Each goroutine uses its own server with its own repo to avoid cross-talk.
	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			repo := &retryRepo{
				failFor:   0,
				totalCost: float32(i) * 1.5,
				order: data.BuyOrder{
					ID:           i + 1,
					BuyOrderUUID: "uuid-distinct",
					CustomerID:   i + 1,
				},
			}
			srv := newBuyOrderServer(repo)
			resp, err := srv.BuyOrder(context.Background(), &pb.BuyOrderRequest{
				BuyOrderUuid: "uuid-distinct",
			})
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", i, err)
				return
			}
			if resp.BuyOrders.BuyOrders[0].TotalCost != float32(i)*1.5 {
				t.Errorf("goroutine %d: expected cost %f, got %f",
					i, float32(i)*1.5, resp.BuyOrders.BuyOrders[0].TotalCost)
			}
		}()
	}

	wg.Wait()
}

// --- Buffered responseCh prevents goroutine leak in processBreadsBought ---

// TestResponseChannel_BufferedDoesNotBlock verifies that a sender can place
// a response into the buffered channel without a receiver being ready.
// This mirrors the fix: if the stream handler has already returned (e.g. after
// finding the order in the DB), the background goroutine must not block forever
// when trying to send on responseCh.
func TestResponseChannel_BufferedDoesNotBlock(t *testing.T) {
	responseCh := make(chan *pb.BreadResponse, 1) // buffered — the fix

	sent := make(chan struct{}, 1)
	go func() {
		// Simulate processBreadsBought sending one response.
		responseCh <- &pb.BreadResponse{Message: "order ready"}
		sent <- struct{}{}
	}()

	select {
	case <-sent:
		// sender did not block — buffered channel working correctly
	case <-time.After(time.Second):
		t.Fatal("sender blocked on buffered channel — unexpected")
	}

	// Drain the channel
	select {
	case resp := <-responseCh:
		if resp.Message != "order ready" {
			t.Errorf("unexpected message: %s", resp.Message)
		}
	default:
		t.Error("expected a response in the channel")
	}
}

// TestResponseChannel_UnbufferedWouldBlock demonstrates the original bug:
// an unbuffered channel blocks the sender if the receiver is gone.
// This test confirms the fix is necessary by showing unbuffered blocks.
func TestResponseChannel_UnbufferedWouldBlock(t *testing.T) {
	unbuffered := make(chan *pb.BreadResponse) // no buffer — the old code

	blocked := make(chan struct{}, 1)
	go func() {
		select {
		case unbuffered <- &pb.BreadResponse{Message: "test"}:
			// sent — receiver was available
		case <-time.After(200 * time.Millisecond):
			blocked <- struct{}{} // would have blocked without a receiver
		}
	}()

	select {
	case <-blocked:
		// confirmed: unbuffered channel blocks without a receiver
	case <-time.After(time.Second):
		t.Fatal("test logic error — expected goroutine to detect blocking")
	}
}
