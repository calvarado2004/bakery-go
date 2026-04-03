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

// --- helpers ---

func newInvoiceServer(repo data.Repository) *InvoiceServiceServer {
	return &InvoiceServiceServer{
		RabbitMQBakery: &RabbitMQBakery{
			Config: Config{Repo: repo},
			orders: make(map[int]*OrderStatus),
		},
	}
}

func newPortalServer(repo data.Repository) *CustomerPortalServiceServer {
	return &CustomerPortalServiceServer{
		RabbitMQBakery: &RabbitMQBakery{
			Config: Config{Repo: repo},
			orders: make(map[int]*OrderStatus),
		},
	}
}

// orderWithBreadsRepo returns a buy order with breads for invoice calculation.
type orderWithBreadsRepo struct {
	stubRepo
	order   data.BuyOrder
	invoice data.Invoice // returned by GetInvoiceByOrderID when non-zero ID
}

func (r *orderWithBreadsRepo) GetBuyOrderByID(id int) (data.BuyOrder, error) {
	if r.order.ID == 0 {
		return data.BuyOrder{}, errors.New("order not found")
	}
	return r.order, nil
}

func (r *orderWithBreadsRepo) GetInvoiceByOrderID(int) (data.Invoice, error) {
	if r.invoice.ID > 0 {
		return r.invoice, nil
	}
	return data.Invoice{}, errors.New("no invoice")
}

func (r *orderWithBreadsRepo) InsertInvoice(inv data.Invoice) (int, error) {
	return 55, nil
}

func (r *orderWithBreadsRepo) GetInvoiceByID(id int) (data.Invoice, error) {
	if r.invoice.ID == id {
		return r.invoice, nil
	}
	return data.Invoice{}, errors.New("invoice not found")
}

func (r *orderWithBreadsRepo) GetInvoicesByCustomerID(customerID int) ([]data.Invoice, error) {
	if r.invoice.ID > 0 && r.invoice.CustomerID == customerID {
		return []data.Invoice{r.invoice}, nil
	}
	return nil, nil
}

func (r *orderWithBreadsRepo) GetAllInvoices() ([]data.Invoice, error) {
	if r.invoice.ID > 0 {
		return []data.Invoice{r.invoice}, nil
	}
	return nil, nil
}

func (r *orderWithBreadsRepo) GetCustomerByID(id int) (data.Customer, error) {
	return data.Customer{ID: id, Name: "Test Customer", Email: "test@customer.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (r *orderWithBreadsRepo) GetCustomerOrders(customerID int) ([]data.BuyOrder, error) {
	if r.order.ID > 0 {
		return []data.BuyOrder{r.order}, nil
	}
	return nil, nil
}

func (r *orderWithBreadsRepo) GetOrderTotalCost(int) (float32, error) {
	var total float32
	for _, b := range r.order.Breads {
		total += b.Price * float32(b.Quantity)
	}
	return total, nil
}

// noOrderRepo makes GetBuyOrderByID fail.
type noOrderRepo struct{ stubRepo }

func (r *noOrderRepo) GetBuyOrderByID(int) (data.BuyOrder, error) {
	return data.BuyOrder{}, errors.New("order not found")
}

// invoiceErrorRepo makes InsertInvoice fail but order lookup succeed.
type invoiceErrorRepo struct {
	orderWithBreadsRepo
}

func (r *invoiceErrorRepo) InsertInvoice(data.Invoice) (int, error) {
	return 0, errors.New("insert failed")
}

// allInvoicesRepo returns a list of invoices from GetAllInvoices.
type allInvoicesRepo struct {
	stubRepo
	invoices []data.Invoice
}

func (r *allInvoicesRepo) GetAllInvoices() ([]data.Invoice, error) {
	return r.invoices, nil
}

// noCustomerPortalRepo makes GetCustomerByID fail.
type noCustomerPortalRepo struct{ stubRepo }

func (r *noCustomerPortalRepo) GetCustomerByID(int) (data.Customer, error) {
	return data.Customer{}, errors.New("customer not found")
}

// sampleOrder is a reusable buy order with two breads.
func sampleOrder() data.BuyOrder {
	return data.BuyOrder{
		ID:           10,
		CustomerID:   3,
		BuyOrderUUID: "invoice-uuid-10",
		Status:       "Processed",
		Breads: []data.Bread{
			{ID: 1, Name: "Pretzel", Price: 2.49, Quantity: 2},
			{ID: 2, Name: "Croissant", Price: 1.19, Quantity: 3},
		},
	}
}

// --- InvoiceService tests ---

func TestCreateInvoice_NewInvoice(t *testing.T) {
	order := sampleOrder()
	repo := &orderWithBreadsRepo{order: order}
	srv := newInvoiceServer(repo)

	resp, err := srv.CreateInvoice(context.Background(), &pb.CreateInvoiceRequest{
		BuyOrderId: int32(order.ID),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Id != 55 {
		t.Errorf("expected invoice ID=55, got %d", resp.Id)
	}
	if resp.BuyOrderId != int32(order.ID) {
		t.Errorf("expected buy_order_id=%d, got %d", order.ID, resp.BuyOrderId)
	}
	// subtotal: 2*2.49 + 3*1.19 = 4.98 + 3.57 = 8.55; tax 8% = 0.684; total ~9.234
	if resp.Subtotal <= 0 {
		t.Error("expected positive subtotal")
	}
	if resp.Tax <= 0 {
		t.Error("expected positive tax")
	}
	if resp.Total != resp.Subtotal+resp.Tax {
		t.Errorf("total should equal subtotal+tax: got subtotal=%f tax=%f total=%f",
			resp.Subtotal, resp.Tax, resp.Total)
	}
	if resp.Status != "pending" {
		t.Errorf("expected status=pending, got %s", resp.Status)
	}
}

func TestCreateInvoice_AlreadyExists(t *testing.T) {
	order := sampleOrder()
	existingInvoice := data.Invoice{
		ID:            99,
		BuyOrderID:    order.ID,
		CustomerID:    order.CustomerID,
		InvoiceNumber: "INV-2026-12345",
		Subtotal:      8.55,
		Tax:           0.68,
		Total:         9.23,
		Status:        "paid",
		CreatedAt:     time.Now(),
		DueDate:       time.Now().AddDate(0, 0, 30),
	}
	repo := &orderWithBreadsRepo{order: order, invoice: existingInvoice}
	srv := newInvoiceServer(repo)

	resp, err := srv.CreateInvoice(context.Background(), &pb.CreateInvoiceRequest{
		BuyOrderId: int32(order.ID),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the existing invoice, not a new one
	if resp.Id != 99 {
		t.Errorf("expected existing invoice ID=99, got %d", resp.Id)
	}
	if resp.Status != "paid" {
		t.Errorf("expected status=paid (existing), got %s", resp.Status)
	}
}

func TestCreateInvoice_OrderNotFound(t *testing.T) {
	srv := newInvoiceServer(&noOrderRepo{})

	_, err := srv.CreateInvoice(context.Background(), &pb.CreateInvoiceRequest{
		BuyOrderId: 999,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

func TestCreateInvoice_InsertFails(t *testing.T) {
	order := sampleOrder()
	repo := &invoiceErrorRepo{
		orderWithBreadsRepo: orderWithBreadsRepo{order: order},
	}
	srv := newInvoiceServer(repo)

	_, err := srv.CreateInvoice(context.Background(), &pb.CreateInvoiceRequest{
		BuyOrderId: int32(order.ID),
	})
	if err == nil {
		t.Fatal("expected error when InsertInvoice fails, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

func TestGetInvoice_Found(t *testing.T) {
	inv := data.Invoice{
		ID:            7,
		BuyOrderID:    10,
		CustomerID:    3,
		InvoiceNumber: "INV-2026-00007",
		Subtotal:      8.55,
		Tax:           0.68,
		Total:         9.23,
		Status:        "pending",
		CreatedAt:     time.Now(),
		DueDate:       time.Now().AddDate(0, 0, 30),
	}
	repo := &orderWithBreadsRepo{invoice: inv}
	srv := newInvoiceServer(repo)

	resp, err := srv.GetInvoice(context.Background(), &pb.InvoiceIdRequest{Id: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Id != 7 {
		t.Errorf("expected invoice ID=7, got %d", resp.Id)
	}
	if resp.InvoiceNumber != "INV-2026-00007" {
		t.Errorf("expected invoice number INV-2026-00007, got %s", resp.InvoiceNumber)
	}
}

func TestGetInvoice_NotFound(t *testing.T) {
	repo := &orderWithBreadsRepo{} // invoice.ID is 0, so GetInvoiceByID will fail
	srv := newInvoiceServer(repo)

	_, err := srv.GetInvoice(context.Background(), &pb.InvoiceIdRequest{Id: 999})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

func TestGetCustomerInvoices_ReturnsList(t *testing.T) {
	inv := data.Invoice{
		ID:         7,
		BuyOrderID: 10,
		CustomerID: 3,
		Status:     "pending",
		CreatedAt:  time.Now(),
		DueDate:    time.Now().AddDate(0, 0, 30),
	}
	repo := &orderWithBreadsRepo{invoice: inv}
	srv := newInvoiceServer(repo)

	resp, err := srv.GetCustomerInvoices(context.Background(), &pb.CustomerInvoicesRequest{CustomerId: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Invoices) != 1 {
		t.Errorf("expected 1 invoice, got %d", len(resp.Invoices))
	}
}

func TestGetCustomerInvoices_Empty(t *testing.T) {
	repo := &orderWithBreadsRepo{} // no invoices
	srv := newInvoiceServer(repo)

	resp, err := srv.GetCustomerInvoices(context.Background(), &pb.CustomerInvoicesRequest{CustomerId: 999})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Invoices) != 0 {
		t.Errorf("expected 0 invoices, got %d", len(resp.Invoices))
	}
}

func TestGetAllInvoices_ReturnsList(t *testing.T) {
	repo := &allInvoicesRepo{
		invoices: []data.Invoice{
			{ID: 1, BuyOrderID: 1, CustomerID: 1, Status: "paid", CreatedAt: time.Now(), DueDate: time.Now()},
			{ID: 2, BuyOrderID: 2, CustomerID: 2, Status: "pending", CreatedAt: time.Now(), DueDate: time.Now()},
		},
	}
	srv := newInvoiceServer(repo)

	resp, err := srv.GetAllInvoices(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Invoices) != 2 {
		t.Errorf("expected 2 invoices, got %d", len(resp.Invoices))
	}
}

func TestGetAllInvoices_Empty(t *testing.T) {
	srv := newInvoiceServer(&stubRepo{})

	resp, err := srv.GetAllInvoices(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Invoices) != 0 {
		t.Errorf("expected 0 invoices, got %d", len(resp.Invoices))
	}
}

// --- CustomerPortalService tests ---

func TestGetMyOrders_Success(t *testing.T) {
	order := sampleOrder()
	repo := &orderWithBreadsRepo{order: order}
	srv := newPortalServer(repo)

	resp, err := srv.GetMyOrders(context.Background(), &pb.CustomerIdRequest{Id: int32(order.CustomerID)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Customer == nil {
		t.Fatal("expected customer in response")
	}
	if len(resp.Orders) != 1 {
		t.Errorf("expected 1 order, got %d", len(resp.Orders))
	}
	if resp.Orders[0].BuyOrderUuid != order.BuyOrderUUID {
		t.Errorf("expected uuid=%s, got %s", order.BuyOrderUUID, resp.Orders[0].BuyOrderUuid)
	}
}

func TestGetMyOrders_CustomerNotFound(t *testing.T) {
	srv := newPortalServer(&noCustomerPortalRepo{})

	_, err := srv.GetMyOrders(context.Background(), &pb.CustomerIdRequest{Id: 999})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

func TestGetMyInvoices_ReturnsList(t *testing.T) {
	inv := data.Invoice{
		ID:         7,
		BuyOrderID: 10,
		CustomerID: 3,
		Status:     "pending",
		CreatedAt:  time.Now(),
		DueDate:    time.Now().AddDate(0, 0, 30),
	}
	repo := &orderWithBreadsRepo{invoice: inv}
	srv := newPortalServer(repo)

	resp, err := srv.GetMyInvoices(context.Background(), &pb.CustomerIdRequest{Id: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Invoices) != 1 {
		t.Errorf("expected 1 invoice, got %d", len(resp.Invoices))
	}
}

func TestGetOrderDetails_Success(t *testing.T) {
	order := sampleOrder()
	repo := &orderWithBreadsRepo{order: order}
	srv := newPortalServer(repo)

	resp, err := srv.GetOrderDetails(context.Background(), &pb.BuyOrderIdRequest{Id: int32(order.ID)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Order == nil {
		t.Fatal("expected order in response")
	}
	if resp.Order.Id != int32(order.ID) {
		t.Errorf("expected order ID=%d, got %d", order.ID, resp.Order.Id)
	}
	// Each bread should produce a detail entry
	if len(resp.Details) != len(order.Breads) {
		t.Errorf("expected %d details, got %d", len(order.Breads), len(resp.Details))
	}
	if resp.TotalCost <= 0 {
		t.Error("expected positive total cost")
	}
}

func TestGetOrderDetails_NotFound(t *testing.T) {
	srv := newPortalServer(&noOrderRepo{})

	_, err := srv.GetOrderDetails(context.Background(), &pb.BuyOrderIdRequest{Id: 999})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}
