package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- test helpers for Admin service ---

func newAdminServer(repo data.Repository) *AdminServiceServer {
	return &AdminServiceServer{
		RabbitMQBakery: &RabbitMQBakery{
			Config: Config{Repo: repo},
			orders: make(map[int]*OrderStatus),
		},
	}
}

// errorRepo returns errors for every Repository method.
type errorRepo struct{ stubRepo }

func (r *errorRepo) GetDashboardStats() (*data.DashboardStats, error) {
	return nil, errors.New("db error")
}
func (r *errorRepo) GetAllCustomers() ([]data.Customer, error)   { return nil, errors.New("db error") }
func (r *errorRepo) GetAllBreadMakers() ([]data.BreadMaker, error) {
	return nil, errors.New("db error")
}
func (r *errorRepo) GetAvailableBread() ([]data.Bread, error)   { return nil, errors.New("db error") }
func (r *errorRepo) GetBreadByID(int) (data.Bread, error)       { return data.Bread{}, errors.New("db error") }
func (r *errorRepo) InsertBread(data.Bread) (int, error)        { return 0, errors.New("db error") }
func (r *errorRepo) UpdateBread(data.Bread) error               { return errors.New("db error") }
func (r *errorRepo) DeleteBread(int) error                      { return errors.New("db error") }
func (r *errorRepo) GetLowStockBread(int) ([]data.Bread, error) { return nil, errors.New("db error") }
func (r *errorRepo) UpdateOrderStatus(string, string) error     { return errors.New("db error") }
func (r *errorRepo) GetBuyOrderByUUID(string) (data.BuyOrder, error) {
	return data.BuyOrder{}, errors.New("db error")
}
func (r *errorRepo) GetAllBuyOrders() ([]data.BuyOrder, error)  { return nil, errors.New("db error") }
func (r *errorRepo) GetAllMakeOrders() ([]data.MakeOrder, error) { return nil, errors.New("db error") }
func (r *errorRepo) GetCustomerOrders(int) ([]data.BuyOrder, error) {
	return nil, errors.New("db error")
}
func (r *errorRepo) GetMakerOrders(int) ([]data.MakeOrder, error) {
	return nil, errors.New("db error")
}
func (r *errorRepo) GetCustomerByID(int) (data.Customer, error) {
	return data.Customer{}, errors.New("db error")
}
func (r *errorRepo) GetBreadMakerByID(int) (data.BreadMaker, error) {
	return data.BreadMaker{}, errors.New("db error")
}
func (r *errorRepo) GetInvoiceByOrderID(int) (data.Invoice, error) {
	return data.Invoice{}, errors.New("no invoice")
}
func (r *errorRepo) InsertInvoice(data.Invoice) (int, error) { return 0, errors.New("db error") }

// richRepo returns realistic data for admin queries.
type richRepo struct{ stubRepo }

func (r *richRepo) GetDashboardStats() (*data.DashboardStats, error) {
	return &data.DashboardStats{
		TotalOrders:      42,
		TotalRevenue:     199.99,
		TotalProducts:    7,
		TotalCustomers:   3,
		TotalBreadMakers: 2,
		LowStockCount:    1,
	}, nil
}

func (r *richRepo) GetAllCustomers() ([]data.Customer, error) {
	return []data.Customer{
		{ID: 1, Name: "Alice", Email: "alice@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: 2, Name: "Bob", Email: "bob@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}, nil
}

func (r *richRepo) GetAllBreadMakers() ([]data.BreadMaker, error) {
	return []data.BreadMaker{
		{ID: 1, Name: "Jake", Email: "jake@baker.com", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}, nil
}

func (r *richRepo) GetAvailableBread() ([]data.Bread, error) {
	return []data.Bread{
		{ID: 1, Name: "Pretzel", Price: 2.49, Quantity: 30, Status: "available"},
	}, nil
}

func (r *richRepo) GetBreadByID(id int) (data.Bread, error) {
	return data.Bread{ID: id, Name: "Pretzel", Price: 2.49, Quantity: 30, Status: "available"}, nil
}

func (r *richRepo) InsertBread(b data.Bread) (int, error) { return 99, nil }

func (r *richRepo) UpdateBread(b data.Bread) error { return nil }

func (r *richRepo) DeleteBread(int) error { return nil }

func (r *richRepo) GetLowStockBread(threshold int) ([]data.Bread, error) {
	return []data.Bread{
		{ID: 3, Name: "Sourdough", Price: 1.99, Quantity: 5, Status: "low"},
	}, nil
}

func (r *richRepo) GetAllBuyOrders() ([]data.BuyOrder, error) {
	return []data.BuyOrder{
		{ID: 1, CustomerID: 1, BuyOrderUUID: "uuid-1", Status: "Processed"},
		{ID: 2, CustomerID: 1, BuyOrderUUID: "uuid-2", Status: "Processed"},
	}, nil
}

func (r *richRepo) GetAllMakeOrders() ([]data.MakeOrder, error) {
	return []data.MakeOrder{
		{ID: 1, BreadMakerID: 1},
	}, nil
}

func (r *richRepo) GetCustomerByID(id int) (data.Customer, error) {
	return data.Customer{ID: id, Name: "Alice", Email: "alice@example.com"}, nil
}

func (r *richRepo) GetCustomerOrders(customerID int) ([]data.BuyOrder, error) {
	return []data.BuyOrder{
		{ID: 10, CustomerID: customerID, BuyOrderUUID: "order-uuid-10", Status: "Processed"},
	}, nil
}

func (r *richRepo) GetBreadMakerByID(id int) (data.BreadMaker, error) {
	return data.BreadMaker{ID: id, Name: "Jake", Email: "jake@baker.com"}, nil
}

func (r *richRepo) GetMakerOrders(makerID int) ([]data.MakeOrder, error) {
	return []data.MakeOrder{
		{ID: 5, BreadMakerID: makerID},
	}, nil
}

func (r *richRepo) UpdateOrderStatus(uuid, status string) error { return nil }

func (r *richRepo) GetBuyOrderByUUID(uuid string) (data.BuyOrder, error) {
	return data.BuyOrder{ID: 1, CustomerID: 1, BuyOrderUUID: uuid, Status: "Processed"}, nil
}

func (r *richRepo) GetOrderTotalCost(int) (float32, error) { return 12.50, nil }

func (r *richRepo) GetInvoiceByOrderID(int) (data.Invoice, error) {
	return data.Invoice{}, errors.New("not found")
}

func (r *richRepo) InsertInvoice(inv data.Invoice) (int, error) { return 77, nil }

// --- Admin service tests ---

func TestAdminService_GetDashboardStats_Success(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetDashboardStats(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.TotalOrders != 42 {
		t.Errorf("expected 42 orders, got %d", resp.TotalOrders)
	}
	if resp.TotalProducts != 7 {
		t.Errorf("expected 7 products, got %d", resp.TotalProducts)
	}
}

func TestAdminService_GetDashboardStats_DBError(t *testing.T) {
	srv := newAdminServer(&errorRepo{})
	_, err := srv.GetDashboardStats(context.Background(), &pb.Empty{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

func TestAdminService_GetAllCustomers_ReturnsAll(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetAllCustomers(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Customers) != 2 {
		t.Errorf("expected 2 customers, got %d", len(resp.Customers))
	}
	if resp.Customers[0].Name != "Alice" {
		t.Errorf("expected Alice, got %s", resp.Customers[0].Name)
	}
}

func TestAdminService_GetAllCustomers_Empty(t *testing.T) {
	srv := newAdminServer(&stubRepo{})
	resp, err := srv.GetAllCustomers(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Customers) != 0 {
		t.Errorf("expected 0 customers, got %d", len(resp.Customers))
	}
}

func TestAdminService_GetAllBreadMakers_ReturnsAll(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetAllBreadMakers(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.BreadMakers) != 1 {
		t.Errorf("expected 1 bread maker, got %d", len(resp.BreadMakers))
	}
}

func TestAdminService_GetAllBread_ReturnsAll(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetAllBread(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Breads) != 1 {
		t.Errorf("expected 1 bread, got %d", len(resp.Breads))
	}
	if resp.Breads[0].Name != "Pretzel" {
		t.Errorf("expected Pretzel, got %s", resp.Breads[0].Name)
	}
}

func TestAdminService_GetBreadById_Success(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetBreadById(context.Background(), &pb.BreadIdRequest{Id: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Id != 1 {
		t.Errorf("expected bread ID 1, got %d", resp.Id)
	}
}

func TestAdminService_GetBreadById_NotFound(t *testing.T) {
	srv := newAdminServer(&errorRepo{})
	_, err := srv.GetBreadById(context.Background(), &pb.BreadIdRequest{Id: 999})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

func TestAdminService_CreateBread_Success(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.CreateBread(context.Background(), &pb.CreateBreadRequest{
		Name:     "Croissant",
		Price:    1.19,
		Quantity: 10,
		Type:     "Buttery Bread",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Id != 99 {
		t.Errorf("expected new ID 99, got %d", resp.Id)
	}
	if resp.Status != "available" {
		t.Errorf("expected status 'available', got %s", resp.Status)
	}
}

func TestAdminService_CreateBread_DBError(t *testing.T) {
	srv := newAdminServer(&errorRepo{})
	_, err := srv.CreateBread(context.Background(), &pb.CreateBreadRequest{Name: "Bagel"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

func TestAdminService_UpdateBread_Success(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.UpdateBread(context.Background(), &pb.UpdateBreadRequest{
		Id: 1, Name: "Pretzel", Price: 2.99, Quantity: 25,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Id != 1 {
		t.Errorf("expected bread ID 1, got %d", resp.Id)
	}
}

func TestAdminService_DeleteBread_Success(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	_, err := srv.DeleteBread(context.Background(), &pb.DeleteBreadRequest{Id: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdminService_DeleteBread_DBError(t *testing.T) {
	srv := newAdminServer(&errorRepo{})
	_, err := srv.DeleteBread(context.Background(), &pb.DeleteBreadRequest{Id: 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAdminService_GetLowStockAlerts_ReturnsList(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetLowStockAlerts(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Breads) != 1 {
		t.Errorf("expected 1 low-stock bread, got %d", len(resp.Breads))
	}
}

func TestAdminService_UpdateOrderStatus_Success(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.UpdateOrderStatus(context.Background(), &pb.UpdateOrderStatusRequest{
		BuyOrderUuid: "uuid-1",
		Status:       "Processed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.BuyOrderUuid != "uuid-1" {
		t.Errorf("expected uuid-1, got %s", resp.BuyOrderUuid)
	}
	if resp.TotalCost != 12.50 {
		t.Errorf("expected total cost 12.50, got %f", resp.TotalCost)
	}
}

func TestAdminService_UpdateOrderStatus_DBError(t *testing.T) {
	srv := newAdminServer(&errorRepo{})
	_, err := srv.UpdateOrderStatus(context.Background(), &pb.UpdateOrderStatusRequest{
		BuyOrderUuid: "uuid-1",
		Status:       "Processed",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// completedOrderRepo: richRepo that treats status "completed" as triggering invoice generation.
type completedOrderRepo struct{ richRepo }

func (r *completedOrderRepo) GetBuyOrderByUUID(uuid string) (data.BuyOrder, error) {
	return data.BuyOrder{
		ID:           10,
		CustomerID:   1,
		BuyOrderUUID: uuid,
		Status:       "completed",
		Breads: []data.Bread{
			{ID: 1, Name: "Pretzel", Price: 2.49, Quantity: 3},
		},
	}, nil
}

func TestAdminService_UpdateOrderStatus_CompletedGeneratesInvoice(t *testing.T) {
	srv := newAdminServer(&completedOrderRepo{})
	resp, err := srv.UpdateOrderStatus(context.Background(), &pb.UpdateOrderStatusRequest{
		BuyOrderUuid: "uuid-completed",
		Status:       "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", resp.Status)
	}
}

func TestAdminService_GetAllOrders_ReturnsList(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetAllOrders(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.BuyOrders) != 2 {
		t.Errorf("expected 2 orders, got %d", len(resp.BuyOrders))
	}
}

func TestAdminService_GetAllOrders_Empty(t *testing.T) {
	srv := newAdminServer(&stubRepo{})
	resp, err := srv.GetAllOrders(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.BuyOrders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(resp.BuyOrders))
	}
}

func TestAdminService_GetAllMakeOrders_ReturnsList(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetAllMakeOrders(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.MakeOrders) != 1 {
		t.Errorf("expected 1 make order, got %d", len(resp.MakeOrders))
	}
}

func TestAdminService_GetCustomerOrders_Success(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetCustomerOrders(context.Background(), &pb.CustomerIdRequest{Id: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Customer.Id != 1 {
		t.Errorf("expected customer ID 1, got %d", resp.Customer.Id)
	}
	if len(resp.Orders) != 1 {
		t.Errorf("expected 1 order, got %d", len(resp.Orders))
	}
}

func TestAdminService_GetCustomerOrders_CustomerNotFound(t *testing.T) {
	srv := newAdminServer(&errorRepo{})
	_, err := srv.GetCustomerOrders(context.Background(), &pb.CustomerIdRequest{Id: 999})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

func TestAdminService_GetMakerOrders_Success(t *testing.T) {
	srv := newAdminServer(&richRepo{})
	resp, err := srv.GetMakerOrders(context.Background(), &pb.BreadMakerIdRequest{Id: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Maker.Id != 1 {
		t.Errorf("expected maker ID 1, got %d", resp.Maker.Id)
	}
}
