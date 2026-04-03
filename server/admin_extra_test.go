package main

import (
	"context"
	"errors"
	"testing"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- additional repos for error-path coverage ---

// allCustomersErrorRepo fails GetAllCustomers.
type allCustomersErrorRepo struct{ stubRepo }

func (r *allCustomersErrorRepo) GetAllCustomers() ([]data.Customer, error) {
	return nil, errors.New("db error")
}

// allBreadMakersErrorRepo fails GetAllBreadMakers.
type allBreadMakersErrorRepo struct{ stubRepo }

func (r *allBreadMakersErrorRepo) GetAllBreadMakers() ([]data.BreadMaker, error) {
	return nil, errors.New("db error")
}

// allBreadErrorRepo fails GetAvailableBread.
type allBreadErrorRepo struct{ stubRepo }

func (r *allBreadErrorRepo) GetAvailableBread() ([]data.Bread, error) {
	return nil, errors.New("db error")
}

// updateBreadSucceedsGetBreadFailsRepo: UpdateBread succeeds, GetBreadByID fails.
type updateBreadSucceedsGetBreadFailsRepo struct{ stubRepo }

func (r *updateBreadSucceedsGetBreadFailsRepo) UpdateBread(data.Bread) error { return nil }
func (r *updateBreadSucceedsGetBreadFailsRepo) GetBreadByID(int) (data.Bread, error) {
	return data.Bread{}, errors.New("get bread after update failed")
}

// updateBreadFailsRepo: UpdateBread itself fails.
type updateBreadFailsRepo struct{ stubRepo }

func (r *updateBreadFailsRepo) UpdateBread(data.Bread) error { return errors.New("update failed") }

// lowStockErrorRepo fails GetLowStockBread.
type lowStockErrorRepo struct{ stubRepo }

func (r *lowStockErrorRepo) GetLowStockBread(int) ([]data.Bread, error) {
	return nil, errors.New("db error")
}

// updateStatusSucceedsGetOrderFailsRepo: UpdateOrderStatus OK but GetBuyOrderByUUID fails.
type updateStatusSucceedsGetOrderFailsRepo struct{ stubRepo }

func (r *updateStatusSucceedsGetOrderFailsRepo) UpdateOrderStatus(string, string) error { return nil }
func (r *updateStatusSucceedsGetOrderFailsRepo) GetBuyOrderByUUID(string) (data.BuyOrder, error) {
	return data.BuyOrder{}, errors.New("order lookup failed")
}

// customerFoundOrdersFailRepo: GetCustomerByID succeeds, GetCustomerOrders fails.
type customerFoundOrdersFailRepo struct{ stubRepo }

func (r *customerFoundOrdersFailRepo) GetCustomerByID(id int) (data.Customer, error) {
	return data.Customer{ID: id, Name: "Alice"}, nil
}
func (r *customerFoundOrdersFailRepo) GetCustomerOrders(int) ([]data.BuyOrder, error) {
	return nil, errors.New("orders fetch failed")
}

// makerFoundOrdersFailRepo: GetBreadMakerByID succeeds, GetMakerOrders fails.
type makerFoundOrdersFailRepo struct{ stubRepo }

func (r *makerFoundOrdersFailRepo) GetBreadMakerByID(id int) (data.BreadMaker, error) {
	return data.BreadMaker{ID: id, Name: "Jake"}, nil
}
func (r *makerFoundOrdersFailRepo) GetMakerOrders(int) ([]data.MakeOrder, error) {
	return nil, errors.New("maker orders fetch failed")
}

// makerNotFoundRepo: GetBreadMakerByID fails.
type makerNotFoundRepo struct{ stubRepo }

func (r *makerNotFoundRepo) GetBreadMakerByID(int) (data.BreadMaker, error) {
	return data.BreadMaker{}, errors.New("maker not found")
}

// allOrdersErrorRepo: GetAllBuyOrders fails.
type allOrdersErrorRepo struct{ stubRepo }

func (r *allOrdersErrorRepo) GetAllBuyOrders() ([]data.BuyOrder, error) {
	return nil, errors.New("db error")
}

// allMakeOrdersErrorRepo: GetAllMakeOrders fails.
type allMakeOrdersErrorRepo struct{ stubRepo }

func (r *allMakeOrdersErrorRepo) GetAllMakeOrders() ([]data.MakeOrder, error) {
	return nil, errors.New("db error")
}

// ordersWithBreadsRepo: GetAllBuyOrders returns orders that have breads.
type ordersWithBreadsRepo struct{ stubRepo }

func (r *ordersWithBreadsRepo) GetAllBuyOrders() ([]data.BuyOrder, error) {
	return []data.BuyOrder{
		{
			ID:           1,
			CustomerID:   1,
			BuyOrderUUID: "uuid-with-bread",
			Status:       "Processed",
			Breads: []data.Bread{
				{ID: 1, Name: "Pretzel", Price: 2.49, Quantity: 3},
			},
		},
	}, nil
}

// makeOrdersWithBreadsRepo: GetAllMakeOrders returns make orders that have breads.
type makeOrdersWithBreadsRepo struct{ stubRepo }

func (r *makeOrdersWithBreadsRepo) GetAllMakeOrders() ([]data.MakeOrder, error) {
	return []data.MakeOrder{
		{
			ID:           1,
			BreadMakerID: 1,
			Breads: []data.Bread{
				{ID: 1, Name: "Sourdough", Price: 1.99, Quantity: 50},
			},
		},
	}, nil
}

// existingInvoiceForCompletedRepo: GetInvoiceByOrderID returns an existing invoice.
type existingInvoiceForCompletedRepo struct{ richRepo }

func (r *existingInvoiceForCompletedRepo) GetBuyOrderByUUID(uuid string) (data.BuyOrder, error) {
	return data.BuyOrder{
		ID:           20,
		CustomerID:   1,
		BuyOrderUUID: uuid,
		Status:       "completed",
		Breads:       []data.Bread{{ID: 1, Name: "Pretzel", Price: 2.49, Quantity: 2}},
	}, nil
}

func (r *existingInvoiceForCompletedRepo) GetInvoiceByOrderID(int) (data.Invoice, error) {
	return data.Invoice{ID: 5, BuyOrderID: 20, Status: "paid"}, nil
}

// --- GetAllCustomers error ---

func TestAdminService_GetAllCustomers_DBError(t *testing.T) {
	srv := newAdminServer(&allCustomersErrorRepo{})
	_, err := srv.GetAllCustomers(context.Background(), &pb.Empty{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

// --- GetAllBreadMakers error ---

func TestAdminService_GetAllBreadMakers_DBError(t *testing.T) {
	srv := newAdminServer(&allBreadMakersErrorRepo{})
	_, err := srv.GetAllBreadMakers(context.Background(), &pb.Empty{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

// --- GetAllBread error ---

func TestAdminService_GetAllBread_DBError(t *testing.T) {
	srv := newAdminServer(&allBreadErrorRepo{})
	_, err := srv.GetAllBread(context.Background(), &pb.Empty{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

// --- UpdateBread error paths ---

func TestAdminService_UpdateBread_UpdateFails(t *testing.T) {
	srv := newAdminServer(&updateBreadFailsRepo{})
	_, err := srv.UpdateBread(context.Background(), &pb.UpdateBreadRequest{Id: 1, Name: "Pretzel"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

func TestAdminService_UpdateBread_GetBreadFails(t *testing.T) {
	srv := newAdminServer(&updateBreadSucceedsGetBreadFailsRepo{})
	_, err := srv.UpdateBread(context.Background(), &pb.UpdateBreadRequest{Id: 1, Name: "Pretzel"})
	if err == nil {
		t.Fatal("expected error when GetBreadByID fails after update, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

// --- GetLowStockAlerts error ---

func TestAdminService_GetLowStockAlerts_DBError(t *testing.T) {
	srv := newAdminServer(&lowStockErrorRepo{})
	_, err := srv.GetLowStockAlerts(context.Background(), &pb.Empty{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

// --- UpdateOrderStatus error paths ---

func TestAdminService_UpdateOrderStatus_GetOrderFails(t *testing.T) {
	srv := newAdminServer(&updateStatusSucceedsGetOrderFailsRepo{})
	_, err := srv.UpdateOrderStatus(context.Background(), &pb.UpdateOrderStatusRequest{
		BuyOrderUuid: "uuid-x",
		Status:       "Processed",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAdminService_UpdateOrderStatus_InvoiceAlreadyExists(t *testing.T) {
	// Test that completing an order with an existing invoice doesn't fail
	srv := newAdminServer(&existingInvoiceForCompletedRepo{})
	resp, err := srv.UpdateOrderStatus(context.Background(), &pb.UpdateOrderStatusRequest{
		BuyOrderUuid: "uuid-completed-existing",
		Status:       "completed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("expected status=completed, got %s", resp.Status)
	}
}

// --- GetCustomerOrders error paths ---

func TestAdminService_GetCustomerOrders_OrdersFetchError(t *testing.T) {
	srv := newAdminServer(&customerFoundOrdersFailRepo{})
	_, err := srv.GetCustomerOrders(context.Background(), &pb.CustomerIdRequest{Id: 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

// --- GetMakerOrders error paths ---

func TestAdminService_GetMakerOrders_NotFound(t *testing.T) {
	srv := newAdminServer(&makerNotFoundRepo{})
	_, err := srv.GetMakerOrders(context.Background(), &pb.BreadMakerIdRequest{Id: 999})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

func TestAdminService_GetMakerOrders_OrdersFetchError(t *testing.T) {
	srv := newAdminServer(&makerFoundOrdersFailRepo{})
	_, err := srv.GetMakerOrders(context.Background(), &pb.BreadMakerIdRequest{Id: 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

// --- GetAllOrders error and bread-details path ---

func TestAdminService_GetAllOrders_DBError(t *testing.T) {
	srv := newAdminServer(&allOrdersErrorRepo{})
	_, err := srv.GetAllOrders(context.Background(), &pb.Empty{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

func TestAdminService_GetAllOrders_IncludesBreadDetails(t *testing.T) {
	srv := newAdminServer(&ordersWithBreadsRepo{})
	resp, err := srv.GetAllOrders(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.BuyOrders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(resp.BuyOrders))
	}
	// BuyOrderDetails should include a row for each bread
	if len(resp.BuyOrderDetails) == 0 {
		t.Error("expected bread details in response")
	}
}

// --- GetAllMakeOrders error and bread-details path ---

func TestAdminService_GetAllMakeOrders_DBError(t *testing.T) {
	srv := newAdminServer(&allMakeOrdersErrorRepo{})
	_, err := srv.GetAllMakeOrders(context.Background(), &pb.Empty{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

func TestAdminService_GetAllMakeOrders_IncludesBreadDetails(t *testing.T) {
	srv := newAdminServer(&makeOrdersWithBreadsRepo{})
	resp, err := srv.GetAllMakeOrders(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.MakeOrders) != 1 {
		t.Fatalf("expected 1 make order, got %d", len(resp.MakeOrders))
	}
	if len(resp.MakeOrders[0].Breads) == 0 {
		t.Error("expected breads in make order response")
	}
}
