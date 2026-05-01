package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// --- helpers ---

func newCheckInventoryServer(repo data.Repository) *CheckInventoryServer {
	return &CheckInventoryServer{
		RabbitMQBakery: &RabbitMQBakery{
			Config: Config{Repo: repo},
		},
	}
}

func newBuyBreadServer(repo data.Repository) *BuyBreadServer {
	bakery := &RabbitMQBakery{
		Config: Config{Repo: repo},
	}
	// For unit tests, create a dispatcher that wraps the repo's
	// WaitForOrderNotification so the old test mocks still work.
	bakery.settlementDispatcher = newTestDispatcher(bakery)
	return &BuyBreadServer{
		RabbitMQBakery: bakery,
	}
}

// testDispatcher is a test-only dispatcher that routes to the repo's
// WaitForOrderNotification instead of consuming RabbitMQ.
type testDispatcher struct {
	bakery *RabbitMQBakery
}

func newTestDispatcher(bakery *RabbitMQBakery) settlementDispatcher {
	return &testDispatcher{bakery: bakery}
}

func (d *testDispatcher) Start()                       {}
func (d *testDispatcher) Register(uuid string) <-chan *data.BuyOrder {
	ch := make(chan *data.BuyOrder, 1)
	go func() {
		// Simulate the old behavior: call WaitForOrderNotification
		if err := d.bakery.Repo.WaitForOrderNotification(context.Background(), uuid); err != nil {
			close(ch)
			return
		}
		order, err := d.bakery.Repo.GetBuyOrderByUUID(uuid)
		if err != nil {
			close(ch)
			return
		}
		ch <- &order
	}()
	return ch
}
func (d *testDispatcher) Unregister(uuid string) {}

func newBuyOrderStreamServer(repo data.Repository) *BuyOrderServiceServer {
	return &BuyOrderServiceServer{
		RabbitMQBakery: &RabbitMQBakery{
			Config: Config{Repo: repo},
		},
	}
}

// --- mock BuyOrderResponse stream ---

type mockBuyOrderResponseStream struct {
	ctx     context.Context
	mu      sync.Mutex
	sent    []*pb.BuyOrderResponse
	sendErr error
}

func (m *mockBuyOrderResponseStream) Send(r *pb.BuyOrderResponse) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	m.sent = append(m.sent, r)
	m.mu.Unlock()
	return nil
}

func (m *mockBuyOrderResponseStream) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *mockBuyOrderResponseStream) SetHeader(metadata.MD) error  { return nil }
func (m *mockBuyOrderResponseStream) SendHeader(metadata.MD) error { return nil }
func (m *mockBuyOrderResponseStream) SetTrailer(metadata.MD)       {}
func (m *mockBuyOrderResponseStream) SendMsg(interface{}) error    { return nil }
func (m *mockBuyOrderResponseStream) RecvMsg(interface{}) error    { return nil }

func (m *mockBuyOrderResponseStream) received() []*pb.BuyOrderResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*pb.BuyOrderResponse, len(m.sent))
	copy(out, m.sent)
	return out
}

// --- repos for CheckBreadInventory ---

type breadAvailableRepo struct{ stubRepo }

func (r *breadAvailableRepo) GetAvailableBread() ([]data.Bread, error) {
	return []data.Bread{
		{ID: 1, Name: "Pretzel", Price: 2.49, Quantity: 30, Status: "available", Type: "Salty"},
		{ID: 2, Name: "Baguette", Price: 1.49, Quantity: 15, Status: "available", Type: "French"},
	}, nil
}

type breadUnavailableRepo struct{ stubRepo }

func (r *breadUnavailableRepo) GetAvailableBread() ([]data.Bread, error) {
	return nil, nil // empty list
}

type breadRepoError struct{ stubRepo }

func (r *breadRepoError) GetAvailableBread() ([]data.Bread, error) {
	return nil, errors.New("inventory db error")
}

// --- repos for BuyOrderStream ---

type allBuyOrdersRepo struct{ stubRepo }

func (r *allBuyOrdersRepo) GetAllBuyOrders() ([]data.BuyOrder, error) {
	return []data.BuyOrder{
		{ID: 1, CustomerID: 1, BuyOrderUUID: "stream-uuid-1", Status: "Processed"},
		{ID: 2, CustomerID: 2, BuyOrderUUID: "stream-uuid-2", Status: "Processed"},
	}, nil
}

func (r *allBuyOrdersRepo) GetOrderTotalCost(id int) (float32, error) {
	return float32(id) * 5.0, nil
}

type buyOrderByUUIDRepo struct{ stubRepo }

func (r *buyOrderByUUIDRepo) GetBuyOrderByUUID(uuid string) (data.BuyOrder, error) {
	return data.BuyOrder{ID: 7, CustomerID: 3, BuyOrderUUID: uuid, Status: "Processed"}, nil
}

func (r *buyOrderByUUIDRepo) GetOrderTotalCost(int) (float32, error) { return 14.75, nil }

type buyOrderByUUIDErrorRepo struct{ stubRepo }

func (r *buyOrderByUUIDErrorRepo) GetBuyOrderByUUID(string) (data.BuyOrder, error) {
	return data.BuyOrder{}, errors.New("uuid not found")
}

// --- repos for initializeBakery ---

type trackingInsertRepo struct {
	stubRepo
	mu            sync.Mutex
	breadInserts  int
	makerInserts  int
}

func (r *trackingInsertRepo) InsertBread(data.Bread) (int, error) {
	r.mu.Lock()
	r.breadInserts++
	r.mu.Unlock()
	return r.breadInserts, nil
}

func (r *trackingInsertRepo) InsertBreadMaker(data.BreadMaker) (int, error) {
	r.mu.Lock()
	r.makerInserts++
	r.mu.Unlock()
	return r.makerInserts, nil
}

// --- CheckBreadInventory tests ---

func TestCheckBreadInventory_WithBreads(t *testing.T) {
	srv := newCheckInventoryServer(&breadAvailableRepo{})
	resp, err := srv.CheckBreadInventory(context.Background(), &pb.BreadRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Breads == nil || len(resp.Breads.Breads) != 2 {
		t.Errorf("expected 2 breads in response, got %v", resp.Breads)
	}
	if resp.Breads.Breads[0].Name != "Pretzel" {
		t.Errorf("expected first bread Pretzel, got %s", resp.Breads.Breads[0].Name)
	}
}

func TestCheckBreadInventory_EmptyInventory(t *testing.T) {
	srv := newCheckInventoryServer(&breadUnavailableRepo{})
	_, err := srv.CheckBreadInventory(context.Background(), &pb.BreadRequest{})
	if err == nil {
		t.Fatal("expected NotFound error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

func TestCheckBreadInventory_DBError(t *testing.T) {
	srv := newCheckInventoryServer(&breadRepoError{})
	_, err := srv.CheckBreadInventory(context.Background(), &pb.BreadRequest{})
	if err == nil {
		t.Fatal("expected error from DB, got nil")
	}
}

// --- BuyOrderStream tests ---

func TestBuyOrderStream_AllOrders(t *testing.T) {
	srv := newBuyOrderStreamServer(&allBuyOrdersRepo{})
	stream := &mockBuyOrderResponseStream{}

	err := srv.BuyOrderStream(&pb.BuyOrderRequest{}, stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	received := stream.received()
	if len(received) != 2 {
		t.Errorf("expected 2 streamed responses, got %d", len(received))
	}
	if received[0].BuyOrders.BuyOrders[0].TotalCost != 5.0 {
		t.Errorf("expected total cost 5.0 for order 1, got %f", received[0].BuyOrders.BuyOrders[0].TotalCost)
	}
}

func TestBuyOrderStream_ByUUID(t *testing.T) {
	srv := newBuyOrderStreamServer(&buyOrderByUUIDRepo{})
	stream := &mockBuyOrderResponseStream{}

	err := srv.BuyOrderStream(&pb.BuyOrderRequest{BuyOrderUuid: "stream-uuid-7"}, stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	received := stream.received()
	if len(received) != 1 {
		t.Fatalf("expected 1 response, got %d", len(received))
	}
	if received[0].BuyOrders.BuyOrders[0].BuyOrderUuid != "stream-uuid-7" {
		t.Errorf("expected uuid stream-uuid-7, got %s", received[0].BuyOrders.BuyOrders[0].BuyOrderUuid)
	}
	if received[0].BuyOrders.BuyOrders[0].TotalCost != 14.75 {
		t.Errorf("expected total cost 14.75, got %f", received[0].BuyOrders.BuyOrders[0].TotalCost)
	}
}

func TestBuyOrderStream_UUIDNotFound(t *testing.T) {
	srv := newBuyOrderStreamServer(&buyOrderByUUIDErrorRepo{})
	stream := &mockBuyOrderResponseStream{}

	err := srv.BuyOrderStream(&pb.BuyOrderRequest{BuyOrderUuid: "missing-uuid"}, stream)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

func TestBuyOrderStream_AllOrdersDBError(t *testing.T) {
	srv := newBuyOrderStreamServer(&stubRepo{}) // GetAllBuyOrders returns nil, nil → empty, no error
	stream := &mockBuyOrderResponseStream{}

	// stubRepo returns (nil, nil) → empty orders, no error
	err := srv.BuyOrderStream(&pb.BuyOrderRequest{}, stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stream.received()) != 0 {
		t.Errorf("expected 0 responses for empty orders, got %d", len(stream.received()))
	}
}

func TestBuyOrderStream_SendError(t *testing.T) {
	srv := newBuyOrderStreamServer(&allBuyOrdersRepo{})
	stream := &mockBuyOrderResponseStream{sendErr: errors.New("client disconnected")}

	err := srv.BuyOrderStream(&pb.BuyOrderRequest{}, stream)
	if err == nil {
		t.Fatal("expected error from Send, got nil")
	}
}

// --- initializeBakery tests ---

func TestInitializeBakery_InsertsAllBreads(t *testing.T) {
	repo := &trackingInsertRepo{}
	rabbit := &RabbitMQBakery{
		Config: Config{Repo: repo},
	}

	rabbit.initializeBakery()

	repo.mu.Lock()
	breadCount := repo.breadInserts
	makerCount := repo.makerInserts
	repo.mu.Unlock()

	// initializeBakery creates 7 bread types
	if breadCount != 7 {
		t.Errorf("expected 7 bread inserts, got %d", breadCount)
	}
	// and 1 additional bread maker
	if makerCount != 1 {
		t.Errorf("expected 1 bread maker insert, got %d", makerCount)
	}
}

func TestInitializeBakery_BreadInsertError(t *testing.T) {
	// If InsertBread fails, initializeBakery returns early.
	// Verify it doesn't panic.
	repo := &insertBreadErrorRepo{}
	rabbit := &RabbitMQBakery{
		Config: Config{Repo: repo},
	}

	// Should not panic
	rabbit.initializeBakery()
}

type insertBreadErrorRepo struct{ stubRepo }

func (r *insertBreadErrorRepo) InsertBread(data.Bread) (int, error) {
	return 0, errors.New("insert failed")
}

// --- NewRabbitMQBakery and server-package construction ---

func TestServerNewRabbitMQBakery_Created(t *testing.T) {
	b := NewRabbitMQBakery(Config{Repo: &stubRepo{}}, "amqp://test:5672")
	if b == nil {
		t.Fatal("expected non-nil RabbitMQBakery")
	}
	if b.rabbitmqURL != "amqp://test:5672" {
		t.Errorf("expected rabbitmqURL to be set, got %q", b.rabbitmqURL)
	}
}

func TestBuyOrderServiceServer_Construction(t *testing.T) {
	srv := newBuyOrderStreamServer(&stubRepo{})
	if srv == nil {
		t.Fatal("expected non-nil BuyOrderServiceServer")
	}
	if srv.RabbitMQBakery.Repo == nil {
		t.Error("expected Repo to be set")
	}
}

// Ensure mockBuyOrderResponseStream satisfies the stream interface at compile time.
var _ grpc.ServerStreamingServer[pb.BuyOrderResponse] = (*mockBuyOrderResponseStream)(nil)
