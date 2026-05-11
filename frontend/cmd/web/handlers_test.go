package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
)

// ===================================================================
// Mock implementations for testing
// ===================================================================

// mockAdminService implements AdminService for testing.
type mockAdminService struct {
	dashboardStats *pb.DashboardStats
	stockAlerts    *pb.BreadList
	allOrders      *pb.BuyOrderList
	allBread       *pb.BreadList
	allCustomers   *pb.CustomerList
	allMakers      *pb.BreadMakerList
	getBreadByID   *pb.Bread
	getCustomerORD *pb.CustomerOrdersResponse
	getMakerORD    *pb.MakerOrdersResponse
	updateStatus   *pb.BuyOrder
	err            error
	createCount    int
	updateCount    int
	deleteCount    int
}

func (m *mockAdminService) GetDashboardStats() (*pb.DashboardStats, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.dashboardStats != nil {
		return m.dashboardStats, nil
	}
	return &pb.DashboardStats{TotalOrders: 10, TotalRevenue: 100.0, TotalProducts: 7, TotalCustomers: 3, TotalBreadMakers: 2, LowStockCount: 1}, nil
}

func (m *mockAdminService) GetLowStockAlerts() (*pb.BreadList, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.stockAlerts != nil {
		return m.stockAlerts, nil
	}
	return &pb.BreadList{Breads: []*pb.Bread{{Name: "Low Bread", Quantity: 3}}}, nil
}

func (m *mockAdminService) GetAllOrders() (*pb.BuyOrderList, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.allOrders != nil {
		return m.allOrders, nil
	}
	return &pb.BuyOrderList{BuyOrders: []*pb.BuyOrder{{BuyOrderUuid: "uuid-1", Status: "pending"}}}, nil
}

func (m *mockAdminService) GetAllBread() (*pb.BreadList, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.allBread != nil {
		return m.allBread, nil
	}
	return &pb.BreadList{Breads: []*pb.Bread{{Id: 1, Name: "Sourdough", Quantity: 5}}}, nil
}

func (m *mockAdminService) CreateBread(req *pb.CreateBreadRequest) (*pb.Bread, error) {
	m.createCount++
	if m.err != nil {
		return nil, m.err
	}
	return &pb.Bread{Id: 1, Name: req.Name}, nil
}

func (m *mockAdminService) UpdateBread(req *pb.UpdateBreadRequest) (*pb.Bread, error) {
	m.updateCount++
	if m.err != nil {
		return nil, m.err
	}
	return &pb.Bread{Id: req.Id, Name: req.Name}, nil
}

func (m *mockAdminService) DeleteBread(req *pb.DeleteBreadRequest) (*pb.Empty, error) {
	m.deleteCount++
	if m.err != nil {
		return nil, m.err
	}
	return &pb.Empty{}, nil
}

func (m *mockAdminService) GetBreadById(id int32) (*pb.Bread, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.getBreadByID != nil {
		return m.getBreadByID, nil
	}
	return &pb.Bread{Id: id, Name: fmt.Sprintf("Bread %d", id)}, nil
}

func (m *mockAdminService) GetAllCustomers() (*pb.CustomerList, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.allCustomers != nil {
		return m.allCustomers, nil
	}
	return &pb.CustomerList{Customers: []*pb.Customer{{Id: 1, Name: "user1", Email: "user1@test.com"}}}, nil
}

func (m *mockAdminService) GetCustomerOrders(id int32) (*pb.CustomerOrdersResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.getCustomerORD != nil {
		return m.getCustomerORD, nil
	}
	return &pb.CustomerOrdersResponse{Customer: &pb.Customer{Id: id}}, nil
}

func (m *mockAdminService) GetAllBreadMakers() (*pb.BreadMakerList, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.allMakers != nil {
		return m.allMakers, nil
	}
	return &pb.BreadMakerList{BreadMakers: []*pb.BreadMakerProto{{Id: 1, Name: "Maker1"}}}, nil
}

func (m *mockAdminService) GetMakerOrders(id int32) (*pb.MakerOrdersResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.getMakerORD != nil {
		return m.getMakerORD, nil
	}
	return &pb.MakerOrdersResponse{Maker: &pb.BreadMakerProto{Id: id}}, nil
}

func (m *mockAdminService) UpdateOrderStatus(uuid, status string) (*pb.BuyOrder, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.updateStatus != nil {
		return m.updateStatus, nil
	}
	return &pb.BuyOrder{BuyOrderUuid: uuid, Status: status}, nil
}

// mockCustomerService implements CustomerService for testing.
type mockCustomerService struct {
	myOrders  *pb.CustomerOrdersResponse
	myInvoices *pb.InvoiceList
	orderDetail *pb.BuyOrderDetailsResponse
	err       error
}

func (m *mockCustomerService) GetMyOrders(customerID int32) (*pb.CustomerOrdersResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.myOrders != nil {
		return m.myOrders, nil
	}
	return &pb.CustomerOrdersResponse{Customer: &pb.Customer{Id: customerID}}, nil
}

func (m *mockCustomerService) GetMyInvoices(customerID int32) (*pb.InvoiceList, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.myInvoices != nil {
		return m.myInvoices, nil
	}
	return &pb.InvoiceList{Invoices: []*pb.Invoice{{Id: 1, Total: 10.0}}}, nil
}

func (m *mockCustomerService) GetOrderDetails(orderID int32) (*pb.BuyOrderDetailsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.orderDetail != nil {
		return m.orderDetail, nil
	}
	return &pb.BuyOrderDetailsResponse{Order: &pb.BuyOrder{Id: orderID}}, nil
}

// ===================================================================
// AdminService interface tests
// ===================================================================

func TestAdminService_DashboardStats(t *testing.T) {
	svc := &mockAdminService{dashboardStats: &pb.DashboardStats{TotalProducts: 42}}
	stats, err := svc.GetDashboardStats()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.TotalProducts != 42 {
		t.Errorf("TotalProducts: want 42, got %d", stats.TotalProducts)
	}
}

func TestAdminService_DashboardStats_Error(t *testing.T) {
	svc := &mockAdminService{err: fmt.Errorf("db error")}
	_, err := svc.GetDashboardStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdminService_GetAllOrders(t *testing.T) {
	svc := &mockAdminService{allOrders: &pb.BuyOrderList{BuyOrders: []*pb.BuyOrder{
		{BuyOrderUuid: "u1", Status: "pending"},
		{BuyOrderUuid: "u2", Status: "completed"},
	}}}
	orders, err := svc.GetAllOrders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orders.BuyOrders) != 2 {
		t.Errorf("order count: want 2, got %d", len(orders.BuyOrders))
	}
}

func TestAdminService_CreateBread(t *testing.T) {
	svc := &mockAdminService{}
	_, err := svc.CreateBread(&pb.CreateBreadRequest{Name: "Test", Price: 1.0, Quantity: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.createCount != 1 {
		t.Errorf("createCount: want 1, got %d", svc.createCount)
	}
}

func TestAdminService_UpdateBread(t *testing.T) {
	svc := &mockAdminService{}
	_, err := svc.UpdateBread(&pb.UpdateBreadRequest{Id: 5, Name: "Updated"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.updateCount != 1 {
		t.Errorf("updateCount: want 1, got %d", svc.updateCount)
	}
}

func TestAdminService_DeleteBread(t *testing.T) {
	svc := &mockAdminService{}
	_, err := svc.DeleteBread(&pb.DeleteBreadRequest{Id: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.deleteCount != 1 {
		t.Errorf("deleteCount: want 1, got %d", svc.deleteCount)
	}
}

func TestAdminService_GetBreadById(t *testing.T) {
	svc := &mockAdminService{getBreadByID: &pb.Bread{Id: 99, Name: "Custom"}}
	bread, err := svc.GetBreadById(99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bread.Id != 99 {
		t.Errorf("bread ID: want 99, got %d", bread.Id)
	}
}

func TestAdminService_GetAllCustomers(t *testing.T) {
	svc := &mockAdminService{allCustomers: &pb.CustomerList{Customers: []*pb.Customer{{Id: 1}}}}
	customers, err := svc.GetAllCustomers()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(customers.Customers) != 1 {
		t.Errorf("customer count: want 1, got %d", len(customers.Customers))
	}
}

func TestAdminService_UpdateOrderStatus(t *testing.T) {
	svc := &mockAdminService{}
	order, err := svc.UpdateOrderStatus("uuid-1", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.BuyOrderUuid != "uuid-1" {
		t.Errorf("uuid: want uuid-1, got %s", order.BuyOrderUuid)
	}
	if order.Status != "completed" {
		t.Errorf("status: want completed, got %s", order.Status)
	}
}

// ===================================================================
// CustomerService interface tests
// ===================================================================

func TestCustomerService_GetMyOrders(t *testing.T) {
	svc := &mockCustomerService{myOrders: &pb.CustomerOrdersResponse{Customer: &pb.Customer{Id: 5}}}
	resp, err := svc.GetMyOrders(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Customer.Id != 5 {
		t.Errorf("customer ID: want 5, got %d", resp.Customer.Id)
	}
}

func TestCustomerService_GetMyInvoices(t *testing.T) {
	svc := &mockCustomerService{myInvoices: &pb.InvoiceList{Invoices: []*pb.Invoice{{Id: 42, Total: 99.99}}}}
	invoices, err := svc.GetMyInvoices(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(invoices.Invoices) != 1 || invoices.Invoices[0].Id != 42 {
		t.Errorf("invoice: want ID 42, got %+v", invoices)
	}
}

func TestCustomerService_GetOrderDetails(t *testing.T) {
	svc := &mockCustomerService{orderDetail: &pb.BuyOrderDetailsResponse{Order: &pb.BuyOrder{Id: 7}}}
	detail, err := svc.GetOrderDetails(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Order.Id != 7 {
		t.Errorf("order ID: want 7, got %d", detail.Order.Id)
	}
}

// ===================================================================
// noopAdminService tests
// ===================================================================

func TestNoopAdminService_ReturnsError(t *testing.T) {
	svc := &noopAdminService{}
	_, err := svc.GetDashboardStats()
	if err == nil {
		t.Fatal("expected error from noop service")
	}
}

func TestNoopCustomerService_ReturnsError(t *testing.T) {
	svc := &noopCustomerService{}
	_, err := svc.GetMyOrders(1)
	if err == nil {
		t.Fatal("expected error from noop service")
	}
}

// ===================================================================
// TemplateService tests
// ===================================================================

func TestTemplateService_NotFound(t *testing.T) {
	svc := &fileTemplateService{}
	_, err := svc.Execute("nonexistent", AdminTemplateData{})
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}

// ===================================================================
// SSE WriteSSEResponse tests
// ===================================================================

func TestWriteSSEResponse_Headers(t *testing.T) {
	fetch := func() (interface{}, error) {
		return nil, nil // end stream immediately
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	ctx := req.Context()

	WriteSSEResponse(rr, ctx, fetch, 0)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control: want no-cache, got %q", cc)
	}
}

func TestWriteSSEResponse_SendsData(t *testing.T) {
	callCount := 0
	fetch := func() (interface{}, error) {
		callCount++
		if callCount >= 3 {
			return nil, nil // end after 2 calls
		}
		return map[string]string{"count": fmt.Sprintf("%d", callCount)}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	WriteSSEResponse(rr, ctx, fetch, 0)

	if callCount != 3 {
		t.Errorf("fetch calls: want 3, got %d", callCount)
	}
	body := rr.Body.String()
	if !contains(body, "data:") {
		t.Errorf("expected SSE data lines in body: %s", body)
	}
}

func TestWriteSSEResponse_ContextCancel(t *testing.T) {
	fetch := func() (interface{}, error) {
		return map[string]string{"ok": "1"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	rr := httptest.NewRecorder()

	// Start writing in a goroutine
	done := make(chan struct{})
	go func() {
		WriteSSEResponse(rr, ctx, fetch, 100*time.Millisecond)
		close(done)
	}()

	// Cancel immediately
	cancel()

	// Should exit quickly
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WriteSSEResponse did not exit after context cancel")
	}
}

func TestWriteSSEResponse_NoFlushing(t *testing.T) {
	fetch := func() (interface{}, error) {
		return nil, nil
	}

	rr := &noFlushResponse{httptest.NewRecorder()}
	ctx := context.Background()

	// Should not panic when response writer is not a http.Flusher
	WriteSSEResponse(rr, ctx, fetch, 0)

	// SSE headers should still be set
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
}

// noFlushResponse wraps a ResponseRecorder that is not a http.Flusher.
type noFlushResponse struct {
	*httptest.ResponseRecorder
}
func (n *noFlushResponse) Flush() {}

// ===================================================================
// HandlersConfig tests
// ===================================================================

func TestDefaultHandlersConfig(t *testing.T) {
	cfg := DefaultHandlersConfig()
	if cfg.Admin == nil {
		t.Fatal("Admin service should not be nil")
	}
	if cfg.Customer == nil {
		t.Fatal("Customer service should not be nil")
	}
	if cfg.Templates == nil {
		t.Fatal("Templates should not be nil")
	}
	if cfg.CSRFToken == nil {
		t.Fatal("CSRFToken should not be nil")
	}
}

// ===================================================================
// Helpers
// ===================================================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s != "" && (s == substr || len(substr) == 0 || 
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

// ===================================================================
// Mock for shared gRPC connection in tests
// ===================================================================

// mockGRPCConn is a minimal grpc.ClientConn mock for tests that need to
// avoid connecting to a real server.
type mockGRPCConn struct{}

func (m *mockGRPCConn) GetMethodConfig() grpc.MethodConfig          { return grpc.MethodConfig{} }
func (m *mockGRPCConn) Invoke(ctx context.Context, method string, req, resp interface{}, opts ...grpc.CallOption) error {
	return nil
}
func (m *mockGRPCConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("mock")
}
func (m *mockGRPCConn) Close() error                                { return nil }
