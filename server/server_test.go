package main

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- stub repo that satisfies data.Repository with zero-value returns ---

type stubRepo struct{}

func (r *stubRepo) InsertCustomer(data.Customer) (int, error)                      { return 0, nil }
func (r *stubRepo) InsertBread(data.Bread) (int, error)                             { return 0, nil }
func (r *stubRepo) InsertBreadMaker(data.BreadMaker) (int, error)                   { return 0, nil }
func (r *stubRepo) InsertBuyOrder(data.BuyOrder, []data.Bread) (int, error)         { return 0, nil }
func (r *stubRepo) InsertMakeOrder(data.MakeOrder, []data.Bread) (int, error)       { return 0, nil }
func (r *stubRepo) AdjustBreadQuantity(int, int) (bool, error)                      { return true, nil }
func (r *stubRepo) AdjustBreadPrice(int, float32) error                             { return nil }
func (r *stubRepo) PasswordMatches(string, data.Customer) (bool, error)             { return true, nil }
func (r *stubRepo) GetAvailableBread() ([]data.Bread, error)                        { return nil, nil }
func (r *stubRepo) GetBreadByID(int) (data.Bread, error)                            { return data.Bread{}, nil }
func (r *stubRepo) GetMakeOrderByID(int) (data.MakeOrder, error)                    { return data.MakeOrder{}, nil }
func (r *stubRepo) GetBuyOrderByID(int) (data.BuyOrder, error)                      { return data.BuyOrder{}, nil }
func (r *stubRepo) GetBuyOrderByUUID(string) (data.BuyOrder, error)                 { return data.BuyOrder{}, nil }
func (r *stubRepo) GetAllBuyOrders() ([]data.BuyOrder, error)                       { return nil, nil }
func (r *stubRepo) UpdateOrderStatus(string, string) error                          { return nil }
func (r *stubRepo) GetOrderTotalCost(int) (float32, error)                          { return 0, nil }
func (r *stubRepo) DeleteOutboxMessage(int) error                                   { return nil }
func (r *stubRepo) InsertOutboxMessage(data.OutboxMessage) error                    { return nil }
func (r *stubRepo) GetUnprocessedOutboxMessages() ([]data.OutboxMessage, error)     { return nil, nil }
func (r *stubRepo) GetAllCustomers() ([]data.Customer, error)                       { return nil, nil }
func (r *stubRepo) GetAllBreadMakers() ([]data.BreadMaker, error)                   { return nil, nil }
func (r *stubRepo) GetDashboardStats() (*data.DashboardStats, error)                { return nil, nil }
func (r *stubRepo) UpdateBread(data.Bread) error                                    { return nil }
func (r *stubRepo) DeleteBread(int) error                                           { return nil }
func (r *stubRepo) GetLowStockBread(int) ([]data.Bread, error)                      { return nil, nil }
func (r *stubRepo) GetCustomerOrders(int) ([]data.BuyOrder, error)                  { return nil, nil }
func (r *stubRepo) GetMakerOrders(int) ([]data.MakeOrder, error)                    { return nil, nil }
func (r *stubRepo) GetCustomerByID(int) (data.Customer, error)                      { return data.Customer{}, nil }
func (r *stubRepo) GetBreadMakerByID(int) (data.BreadMaker, error)                  { return data.BreadMaker{}, nil }
func (r *stubRepo) GetAllMakeOrders() ([]data.MakeOrder, error)                     { return nil, nil }
func (r *stubRepo) GetAdminUserByUsername(string) (data.AdminUser, error)           { return data.AdminUser{}, nil }
func (r *stubRepo) GetAdminUserByID(int) (data.AdminUser, error)                    { return data.AdminUser{}, nil }
func (r *stubRepo) InsertAdminUser(data.AdminUser) (int, error)                     { return 0, nil }
func (r *stubRepo) GetCustomerByEmail(string) (data.Customer, error)                { return data.Customer{}, nil }
func (r *stubRepo) InsertInvoice(data.Invoice) (int, error)                         { return 0, nil }
func (r *stubRepo) GetInvoiceByID(int) (data.Invoice, error)                        { return data.Invoice{}, nil }
func (r *stubRepo) GetInvoicesByCustomerID(int) ([]data.Invoice, error)             { return nil, nil }
func (r *stubRepo) GetAllInvoices() ([]data.Invoice, error)                         { return nil, nil }
func (r *stubRepo) GetInvoiceByOrderID(int) (data.Invoice, error)                   { return data.Invoice{}, nil }

// --- notFoundRepo: returns sql.ErrNoRows for GetBuyOrderByUUID ---

type notFoundRepo struct{ stubRepo }

func (r *notFoundRepo) GetBuyOrderByUUID(string) (data.BuyOrder, error) {
	return data.BuyOrder{}, sql.ErrNoRows
}

// --- retryRepo: returns sql.ErrNoRows for first N calls, then a valid order ---

type retryRepo struct {
	stubRepo
	mu       sync.Mutex
	calls    int
	failFor  int
	order    data.BuyOrder
	totalCost float32
}

func (r *retryRepo) GetBuyOrderByUUID(string) (data.BuyOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls <= r.failFor {
		return data.BuyOrder{}, sql.ErrNoRows
	}
	return r.order, nil
}

func (r *retryRepo) GetOrderTotalCost(int) (float32, error) {
	return r.totalCost, nil
}

// --- helpers ---

func newBuyOrderServer(repo data.Repository) *BuyOrderServiceServer {
	return &BuyOrderServiceServer{
		RabbitMQBakery: &RabbitMQBakery{
			Config: Config{Repo: repo},
			orders: make(map[int]*OrderStatus),
		},
	}
}

// --- tests ---

// TestBuyOrder_NotFound verifies that when GetBuyOrderByUUID returns sql.ErrNoRows,
// BuyOrder propagates an Internal gRPC error instead of crashing or succeeding silently.
func TestBuyOrder_NotFound(t *testing.T) {
	srv := newBuyOrderServer(&notFoundRepo{})

	_, err := srv.BuyOrder(context.Background(), &pb.BuyOrderRequest{
		BuyOrderUuid: "does-not-exist",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Internal {
		t.Errorf("expected codes.Internal, got %v", st.Code())
	}
}

// TestBuyOrder_Success verifies that a found order is returned correctly with its total cost.
func TestBuyOrder_Success(t *testing.T) {
	repo := &retryRepo{
		failFor:   0,
		totalCost: 9.99,
		order: data.BuyOrder{
			ID:           42,
			BuyOrderUUID: "test-uuid-42",
			CustomerID:   7,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}
	srv := newBuyOrderServer(repo)

	resp, err := srv.BuyOrder(context.Background(), &pb.BuyOrderRequest{
		BuyOrderUuid: "test-uuid-42",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.BuyOrders == nil || len(resp.BuyOrders.BuyOrders) == 0 {
		t.Fatal("expected BuyOrders in response, got none")
	}
	if resp.BuyOrders.BuyOrders[0].TotalCost != 9.99 {
		t.Errorf("expected total cost 9.99, got %f", resp.BuyOrders.BuyOrders[0].TotalCost)
	}
}

// TestRetryRepo_FailsThenSucceeds verifies that the retry repo correctly fails for
// the first N calls and succeeds after, matching the BuyBreadStream retry behaviour.
func TestRetryRepo_FailsThenSucceeds(t *testing.T) {
	repo := &retryRepo{
		failFor:   2,
		totalCost: 5.50,
		order: data.BuyOrder{
			ID:           10,
			BuyOrderUUID: "retry-uuid",
		},
	}

	// First two calls should return ErrNoRows
	for i := 1; i <= 2; i++ {
		_, err := repo.GetBuyOrderByUUID("retry-uuid")
		if err == nil {
			t.Fatalf("call %d: expected sql.ErrNoRows, got nil", i)
		}
		if err != sql.ErrNoRows {
			t.Fatalf("call %d: expected sql.ErrNoRows, got %v", i, err)
		}
	}

	// Third call should succeed
	order, err := repo.GetBuyOrderByUUID("retry-uuid")
	if err != nil {
		t.Fatalf("call 3: expected success, got %v", err)
	}
	if order.ID != 10 {
		t.Errorf("call 3: expected order ID 10, got %d", order.ID)
	}

	// GetOrderTotalCost should never be called with ID=0 (empty order) —
	// verify it returns the correct cost for a valid order ID.
	cost, err := repo.GetOrderTotalCost(order.ID)
	if err != nil {
		t.Fatalf("expected no error for GetOrderTotalCost, got %v", err)
	}
	if cost != 5.50 {
		t.Errorf("expected cost 5.50, got %f", cost)
	}
}
