package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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

// --- repos for BuyBreadStream ---

// notifySuccessRepo: WaitForOrderNotification returns immediately (order ready).
type notifySuccessRepo struct {
	stubRepo
	order     data.BuyOrder
	totalCost float64
}

func (r *notifySuccessRepo) WaitForOrderNotification(_ context.Context, _ string) error {
	return nil
}

func (r *notifySuccessRepo) GetBuyOrderByUUID(_ string) (data.BuyOrder, error) {
	return r.order, nil
}

func (r *notifySuccessRepo) GetOrderTotalCost(_ int) (float64, error) {
	return r.totalCost, nil
}

// notifyTimeoutRepo: WaitForOrderNotification blocks until ctx is cancelled.
type notifyTimeoutRepo struct{ stubRepo }

func (r *notifyTimeoutRepo) WaitForOrderNotification(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

// notifyThenMissingRepo: notification arrives but order is gone from DB.
type notifyThenMissingRepo struct{ stubRepo }

func (r *notifyThenMissingRepo) WaitForOrderNotification(_ context.Context, _ string) error {
	return nil
}

func (r *notifyThenMissingRepo) GetBuyOrderByUUID(_ string) (data.BuyOrder, error) {
	return data.BuyOrder{}, errors.New("order vanished")
}

// --- BuyBreadStream unit tests ---

// TestBuyBreadStream_NotificationSuccess verifies that when the order
// notification arrives and the order exists in the DB, a single response
// is streamed with the correct fields.
func TestBuyBreadStream_NotificationSuccess(t *testing.T) {
	repo := &notifySuccessRepo{
		order: data.BuyOrder{
			ID:           42,
			BuyOrderUUID: "uuid-42",
		},
		totalCost: 12.50,
	}
	srv := newBuyBreadServer(repo)
	stream := &mockBreadStream{}

	req := &pb.BreadRequest{BuyOrderUuid: "uuid-42"}
	if err := srv.BuyBreadStream(req, stream); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stream.Received()
	if len(got) != 1 {
		t.Fatalf("expected 1 streamed response, got %d", len(got))
	}
	if got[0].BuyOrderId != 42 {
		t.Errorf("expected BuyOrderId 42, got %d", got[0].BuyOrderId)
	}
	if got[0].BuyOrderUuid != "uuid-42" {
		t.Errorf("expected BuyOrderUuid uuid-42, got %s", got[0].BuyOrderUuid)
	}
}

// TestBuyBreadStream_NotificationTimeout verifies that when the context
// deadline fires before a notification arrives, BuyBreadStream returns a
// DeadlineExceeded gRPC error.
func TestBuyBreadStream_NotificationTimeout(t *testing.T) {
	repo := &notifyTimeoutRepo{}
	srv := newBuyBreadServer(repo)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stream := &mockBreadStream{ctx: ctx}

	err := srv.BuyBreadStream(&pb.BreadRequest{BuyOrderUuid: "uuid-timeout"}, stream)
	if err == nil {
		t.Fatal("expected DeadlineExceeded error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", st.Code())
	}
}

// TestBuyBreadStream_OrderMissingAfterNotify verifies that if
// GetBuyOrderByUUID fails after the notification is received, an Internal
// gRPC error is returned.
func TestBuyBreadStream_OrderMissingAfterNotify(t *testing.T) {
	repo := &notifyThenMissingRepo{}
	srv := newBuyBreadServer(repo)
	stream := &mockBreadStream{}

	err := srv.BuyBreadStream(&pb.BreadRequest{BuyOrderUuid: "uuid-missing"}, stream)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

// TestBuyBreadStream_SendError verifies that when the stream's Send method
// returns an error (e.g. client disconnected), BuyBreadStream propagates it.
func TestBuyBreadStream_SendError(t *testing.T) {
	repo := &notifySuccessRepo{
		order:     data.BuyOrder{ID: 1, BuyOrderUUID: "uuid-1"},
		totalCost: 5.00,
	}
	srv := newBuyBreadServer(repo)
	stream := &mockBreadStream{sendErr: errors.New("client disconnected")}

	err := srv.BuyBreadStream(&pb.BreadRequest{BuyOrderUuid: "uuid-1"}, stream)
	if err == nil {
		t.Fatal("expected error from Send, got nil")
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
				totalCost: float64(i) * 1.5,
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
			if resp.BuyOrders.BuyOrders[0].TotalCost != float64(i)*1.5 {
				t.Errorf("goroutine %d: expected cost %f, got %f",
					i, float64(i)*1.5, resp.BuyOrders.BuyOrders[0].TotalCost)
			}
		}()
	}

	wg.Wait()
}
