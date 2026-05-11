package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/gorilla/csrf"
	"google.golang.org/grpc"
)

// ---------------------------------------------------------------------------
// AdminService — interface for all admin-facing gRPC operations
// ---------------------------------------------------------------------------

// AdminService abstracts gRPC calls used by admin handlers.
// This makes it possible to unit-test handlers with a mock without a running
// gRPC server.
type AdminService interface {
	GetDashboardStats() (*pb.DashboardStats, error)
	GetLowStockAlerts() (*pb.BreadList, error)
	GetAllOrders() (*pb.BuyOrderList, error)
	GetAllBread() (*pb.BreadList, error)
	CreateBread(req *pb.CreateBreadRequest) (*pb.Bread, error)
	UpdateBread(req *pb.UpdateBreadRequest) (*pb.Bread, error)
	DeleteBread(req *pb.DeleteBreadRequest) (*pb.Empty, error)
	GetBreadById(id int32) (*pb.Bread, error)
	GetAllCustomers() (*pb.CustomerList, error)
	GetCustomerOrders(id int32) (*pb.CustomerOrdersResponse, error)
	GetAllBreadMakers() (*pb.BreadMakerList, error)
	GetMakerOrders(id int32) (*pb.MakerOrdersResponse, error)
	UpdateOrderStatus(uuid, status string) (*pb.BuyOrder, error)
}

// grpcAdminService wraps the gRPC admin client. It satisfies AdminService.
// The getRequestCtx closure is called before each gRPC call to produce a
// request-specific context (with auth headers and timeout).
type grpcAdminService struct {
	client pb.AdminServiceClient
	getCtx func() (context.Context, context.CancelFunc, error)
}

// newGRPCAdminService creates an AdminService wired to the shared gRPC client.
// The ctxFn produces a context for each request — typically adminGRPCContextWithTimeout.
func newGRPCAdminService(ctxFn func() (context.Context, context.CancelFunc, error)) *grpcAdminService {
	return &grpcAdminService{
		client: getSharedGRPCClient(),
		getCtx: ctxFn,
	}
}

func (s *grpcAdminService) GetDashboardStats() (*pb.DashboardStats, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetDashboardStats(ctx, &pb.Empty{})
}

func (s *grpcAdminService) GetLowStockAlerts() (*pb.BreadList, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetLowStockAlerts(ctx, &pb.Empty{})
}

func (s *grpcAdminService) GetAllOrders() (*pb.BuyOrderList, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetAllOrders(ctx, &pb.Empty{})
}

func (s *grpcAdminService) GetAllBread() (*pb.BreadList, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetAllBread(ctx, &pb.Empty{})
}

func (s *grpcAdminService) CreateBread(req *pb.CreateBreadRequest) (*pb.Bread, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.CreateBread(ctx, req)
}

func (s *grpcAdminService) UpdateBread(req *pb.UpdateBreadRequest) (*pb.Bread, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.UpdateBread(ctx, req)
}

func (s *grpcAdminService) DeleteBread(req *pb.DeleteBreadRequest) (*pb.Empty, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.DeleteBread(ctx, req)
}

func (s *grpcAdminService) GetBreadById(id int32) (*pb.Bread, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := s.client.GetBreadById(ctx, &pb.BreadIdRequest{Id: id})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *grpcAdminService) GetAllCustomers() (*pb.CustomerList, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetAllCustomers(ctx, &pb.Empty{})
}

func (s *grpcAdminService) GetCustomerOrders(id int32) (*pb.CustomerOrdersResponse, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetCustomerOrders(ctx, &pb.CustomerIdRequest{Id: id})
}

func (s *grpcAdminService) GetAllBreadMakers() (*pb.BreadMakerList, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetAllBreadMakers(ctx, &pb.Empty{})
}

func (s *grpcAdminService) GetMakerOrders(id int32) (*pb.MakerOrdersResponse, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetMakerOrders(ctx, &pb.BreadMakerIdRequest{Id: id})
}

func (s *grpcAdminService) UpdateOrderStatus(uuid, status string) (*pb.BuyOrder, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.UpdateOrderStatus(ctx, &pb.UpdateOrderStatusRequest{
		BuyOrderUuid: uuid,
		Status:       status,
	})
}

// ---------------------------------------------------------------------------
// CustomerService — interface for all customer-facing gRPC operations
// ---------------------------------------------------------------------------

// CustomerService abstracts gRPC calls used by customer/portal handlers.
type CustomerService interface {
	GetMyOrders(customerID int32) (*pb.CustomerOrdersResponse, error)
	GetMyInvoices(customerID int32) (*pb.InvoiceList, error)
	GetOrderDetails(orderID int32) (*pb.BuyOrderDetailsResponse, error)
}

type grpcCustomerService struct {
	client pb.CustomerPortalServiceClient
	getCtx func() (context.Context, context.CancelFunc, error)
}

func newGRPCCustomerService(ctxFn func() (context.Context, context.CancelFunc, error)) *grpcCustomerService {
	return &grpcCustomerService{
		client: pb.NewCustomerPortalServiceClient(sharedGRPCConn),
		getCtx: ctxFn,
	}
}

func (s *grpcCustomerService) GetMyOrders(customerID int32) (*pb.CustomerOrdersResponse, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetMyOrders(ctx, &pb.CustomerIdRequest{Id: customerID})
}

func (s *grpcCustomerService) GetMyInvoices(customerID int32) (*pb.InvoiceList, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetMyInvoices(ctx, &pb.CustomerIdRequest{Id: customerID})
}

func (s *grpcCustomerService) GetOrderDetails(orderID int32) (*pb.BuyOrderDetailsResponse, error) {
	ctx, cancel, err := s.getCtx()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.client.GetOrderDetails(ctx, &pb.BuyOrderIdRequest{Id: orderID})
}

// ---------------------------------------------------------------------------
// TemplateService — interface for template rendering
// ---------------------------------------------------------------------------

// TemplateService abstracts template execution for handler tests.
type TemplateService interface {
	Execute(name string, data AdminTemplateData) ([]byte, error)
}

// fileTemplateService wraps the global templates map.
type fileTemplateService struct{}

func (f *fileTemplateService) Execute(name string, data AdminTemplateData) ([]byte, error) {
	tmpl, ok := templates[name]
	if !ok {
		return nil, fmt.Errorf("template not found: %s", name)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// ---------------------------------------------------------------------------
// HandlersConfig — dependency injection container for all handlers
// ---------------------------------------------------------------------------

// HandlersConfig holds all dependencies for HTTP handlers.
// Handlers receive this config instead of reaching for package-level variables.
type HandlersConfig struct {
	Admin       AdminService
	Customer    CustomerService
	Templates   TemplateService
	GRPCConn    *grpc.ClientConn
	GetAdminCtx func(r *http.Request) (context.Context, func(), error)
	CSRFToken   func(r *http.Request) string
}

// DefaultHandlersConfig returns a config wired to the real gRPC client.
// The admin and customer services need request context closures; callers
// should set them per-request in the handler.
func DefaultHandlersConfig() *HandlersConfig {
	return &HandlersConfig{
		Admin:     &noopAdminService{},
		Customer:  &noopCustomerService{},
		Templates: &fileTemplateService{},
		GRPCConn:  sharedGRPCConn,
		CSRFToken: csrf.Token,
	}
}

// noopAdminService is a zero-value AdminService that always returns errors.
// Used when no real gRPC client is available (e.g., during handler tests).
type noopAdminService struct{}

func (n *noopAdminService) GetDashboardStats() (*pb.DashboardStats, error)      { return nil, errNoGRPC }
func (n *noopAdminService) GetLowStockAlerts() (*pb.BreadList, error)          { return nil, errNoGRPC }
func (n *noopAdminService) GetAllOrders() (*pb.BuyOrderList, error)            { return nil, errNoGRPC }
func (n *noopAdminService) GetAllBread() (*pb.BreadList, error)                { return nil, errNoGRPC }
func (n *noopAdminService) CreateBread(*pb.CreateBreadRequest) (*pb.Bread, error) { return nil, errNoGRPC }
func (n *noopAdminService) UpdateBread(*pb.UpdateBreadRequest) (*pb.Bread, error) { return nil, errNoGRPC }
func (n *noopAdminService) DeleteBread(*pb.DeleteBreadRequest) (*pb.Empty, error) { return nil, errNoGRPC }
func (n *noopAdminService) GetBreadById(int32) (*pb.Bread, error)              { return nil, errNoGRPC }
func (n *noopAdminService) GetAllCustomers() (*pb.CustomerList, error)         { return nil, errNoGRPC }
func (n *noopAdminService) GetCustomerOrders(int32) (*pb.CustomerOrdersResponse, error) { return nil, errNoGRPC }
func (n *noopAdminService) GetAllBreadMakers() (*pb.BreadMakerList, error)     { return nil, errNoGRPC }
func (n *noopAdminService) GetMakerOrders(int32) (*pb.MakerOrdersResponse, error) { return nil, errNoGRPC }
func (n *noopAdminService) UpdateOrderStatus(string, string) (*pb.BuyOrder, error) { return nil, errNoGRPC }

// noopCustomerService is a zero-value CustomerService.
type noopCustomerService struct{}

func (n *noopCustomerService) GetMyOrders(int32) (*pb.CustomerOrdersResponse, error) { return nil, errNoGRPC }
func (n *noopCustomerService) GetMyInvoices(int32) (*pb.InvoiceList, error)         { return nil, errNoGRPC }
func (n *noopCustomerService) GetOrderDetails(int32) (*pb.BuyOrderDetailsResponse, error) { return nil, errNoGRPC }

var errNoGRPC = fmt.Errorf("gRPC client not configured")

// ---------------------------------------------------------------------------
// SSE helper — extract stream logic for testability
// ---------------------------------------------------------------------------

// SSEFetchFunc is called repeatedly by the SSE writer. It returns (data, nil) to
// send a frame, (nil, nil) to end the stream, or (nil, non-nil) to log an error
// and continue.
type SSEFetchFunc func() (interface{}, error)

// WriteSSEResponse is the extracted SSE logic — testable without HTTP.
// The ctx parameter provides cancellation via ctx.Done().
func WriteSSEResponse(w http.ResponseWriter, ctx context.Context, fetch SSEFetchFunc, flushInterval time.Duration) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			data, err := fetch()
			if err != nil {
				continue
			}
			if data == nil {
				return
			}

			jsonData, jerr := json.Marshal(data)
			if jerr != nil {
				continue
			}

			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()

			time.Sleep(flushInterval)
		}
	}
}
