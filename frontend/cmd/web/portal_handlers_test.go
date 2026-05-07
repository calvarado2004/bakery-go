package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ── No-customerID redirect tests (unit — no gRPC server needed) ──

func TestCustomerPortalDashboardHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	rr := httptest.NewRecorder()
	CustomerPortalDashboardHandler(rr, req)
	assertRedirectToPortalLogin(t, rr)
}

func TestCustomerOrdersHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal/orders", nil)
	rr := httptest.NewRecorder()
	CustomerOrdersHandler(rr, req)
	assertRedirectToPortalLogin(t, rr)
}

func TestCustomerOrderDetailHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal/orders/42", nil)
	rr := httptest.NewRecorder()
	CustomerOrderDetailHandler(rr, req)
	assertRedirectToPortalLogin(t, rr)
}

func TestCustomerInvoicesHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal/invoices", nil)
	rr := httptest.NewRecorder()
	CustomerInvoicesHandler(rr, req)
	assertRedirectToPortalLogin(t, rr)
}

func TestCustomerInvoiceDetailHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal/invoices/7", nil)
	rr := httptest.NewRecorder()
	CustomerInvoiceDetailHandler(rr, req)
	assertRedirectToPortalLogin(t, rr)
}

// ── Path parsing edge-cases (unit — no gRPC server needed) ──
//
// These tests route through mux.NewRouter so that mux.Vars(r) is populated
// with the {id} path parameter (handlers use mux.Vars instead of r.URL.Path slicing).

func TestCustomerOrderDetailHandler_InvalidID(t *testing.T) {
	token := createTestCustomerToken()
	router := mux.NewRouter()
	router.HandleFunc("/portal/orders/{id}", CustomerOrderDetailHandler)
	req := httptest.NewRequest(http.MethodGet, "/portal/orders/notanumber", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid order ID, got %d", rr.Code)
	}
}

func TestCustomerInvoiceDetailHandler_InvalidID(t *testing.T) {
	token := createTestCustomerToken()
	router := mux.NewRouter()
	router.HandleFunc("/portal/invoices/{id}", CustomerInvoiceDetailHandler)
	req := httptest.NewRequest(http.MethodGet, "/portal/invoices/notanumber", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid invoice ID, got %d", rr.Code)
	}
}

// ── customerGRPCContext: covers the customerID > 0 branch ──

func TestCustomerGRPCContext_WithRealToken(t *testing.T) {
	// createTestCustomerToken() embeds UserID=1, so getCustomerIDFromToken returns 1.
	// This exercises the md != nil && customerID > 0 branch inside customerGRPCContext.
	token := createTestCustomerToken()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})

	ctx := customerGRPCContext(req)

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected outgoing metadata in context")
	}
	if len(md.Get("authorization")) == 0 {
		t.Error("expected authorization metadata")
	}
	if len(md.Get("customer_id")) == 0 {
		t.Error("expected customer_id metadata")
	}
	if md.Get("customer_id")[0] != "1" {
		t.Errorf("expected customer_id=1, got %q", md.Get("customer_id")[0])
	}
}

// ── Integration tests (skip if gRPC server is not running) ──

// customerTestConn returns the shared customer gRPC connection set up by TestMain.
// It skips the test if the server was not reachable at startup.
// Callers must NOT close the returned connection — it is owned by TestMain.
func customerTestConn(t *testing.T) (*grpc.ClientConn, string) {
	t.Helper()
	if sharedCustomerConn == nil || sharedCustomerToken == "" {
		t.Skip("gRPC server not available or customer login failed at startup")
	}
	SetSharedGRPCConn(sharedCustomerConn)
	return sharedCustomerConn, sharedCustomerToken
}

func TestCustomerPortalDashboardHandler_Integration(t *testing.T) {
	_, token := customerTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	CustomerPortalDashboardHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCustomerOrdersHandler_Integration(t *testing.T) {
	_, token := customerTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/portal/orders", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	CustomerOrdersHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCustomerInvoicesHandler_Integration(t *testing.T) {
	_, token := customerTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/portal/invoices", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	CustomerInvoicesHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCustomerOrderDetailHandler_Integration(t *testing.T) {
	conn, token := customerTestConn(t)

	// Extract customer ID from token to fetch their orders.
	customerID := getCustomerIDFromToken(requestWithCookie("customer_token", token))
	if customerID == 0 {
		t.Skip("could not extract customer ID from token")
	}

	portalClient := pb.NewCustomerPortalServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	ordersResp, err := portalClient.GetMyOrders(authCtx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil || len(ordersResp.GetOrders()) == 0 {
		t.Skip("no orders available for customer")
	}
	orderID := ordersResp.GetOrders()[0].Id

	router := mux.NewRouter()
	router.HandleFunc("/portal/orders/{id}", CustomerOrderDetailHandler)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/portal/orders/%d", orderID), nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d for order ID %d", rr.Code, orderID)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCustomerInvoiceDetailHandler_Integration(t *testing.T) {
	conn, token := customerTestConn(t)

	customerID := getCustomerIDFromToken(requestWithCookie("customer_token", token))
	if customerID == 0 {
		t.Skip("could not extract customer ID from token")
	}

	portalClient := pb.NewCustomerPortalServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	invoicesResp, err := portalClient.GetMyInvoices(authCtx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil || len(invoicesResp.GetInvoices()) == 0 {
		t.Skip("no invoices available for customer")
	}
	invoiceID := invoicesResp.GetInvoices()[0].Id

	router := mux.NewRouter()
	router.HandleFunc("/portal/invoices/{id}", CustomerInvoiceDetailHandler)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/portal/invoices/%d", invoiceID), nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d for invoice ID %d", rr.Code, invoiceID)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ── Error-path integration tests (need gRPC but exercise 404/500 branches) ──

// TestCustomerOrderDetailHandler_NotFound hits the GetOrderDetails error path
// by requesting a non-existent order ID. Requires a running gRPC server.
func TestCustomerOrderDetailHandler_NotFound_Integration(t *testing.T) {
	_, token := customerTestConn(t)

	router := mux.NewRouter()
	router.HandleFunc("/portal/orders/{id}", CustomerOrderDetailHandler)
	req := httptest.NewRequest(http.MethodGet, "/portal/orders/999999", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent order, got %d", rr.Code)
	}
}

// TestCustomerInvoiceDetailHandler_NotFound hits the GetInvoiceDetails error path
// by requesting a non-existent invoice ID. Requires a running gRPC server.
func TestCustomerInvoiceDetailHandler_NotFound_Integration(t *testing.T) {
	_, token := customerTestConn(t)

	router := mux.NewRouter()
	router.HandleFunc("/portal/invoices/{id}", CustomerInvoiceDetailHandler)
	req := httptest.NewRequest(http.MethodGet, "/portal/invoices/999999", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent invoice, got %d", rr.Code)
	}
}

// TestCustomerOrderDetailHandler_Forbidden hits the cross-customer ownership check:
// a real order (belonging to customer 1) is requested using a token for customer 9999.
// GetOrderDetails has no server-side customer check, so the response is returned and
// the handler's ownership check fires → 403 Forbidden.
func TestCustomerOrderDetailHandler_Forbidden_Integration(t *testing.T) {
	conn, realToken := customerTestConn(t)

	// Find a real order ID belonging to the test customer (customer 1).
	customerID := getCustomerIDFromToken(requestWithCookie("customer_token", realToken))
	if customerID == 0 {
		t.Skip("could not extract customer ID from token")
	}
	portalClient := pb.NewCustomerPortalServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+realToken))
	ordersResp, err := portalClient.GetMyOrders(authCtx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil || len(ordersResp.GetOrders()) == 0 {
		t.Skip("no orders available for customer")
	}
	orderID := ordersResp.GetOrders()[0].Id

	// Build a token for a different (non-existent) customer — 9999.
	foreignToken := createTestCustomerTokenWithID(9999)

	router := mux.NewRouter()
	router.HandleFunc("/portal/orders/{id}", CustomerOrderDetailHandler)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/portal/orders/%d", orderID), nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: foreignToken})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d for order %d requested by foreign customer 9999", rr.Code, orderID)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for cross-customer order access, got %d", rr.Code)
	}
}

// TestCustomerInvoiceDetailHandler_Forbidden hits the cross-customer ownership check.
func TestCustomerInvoiceDetailHandler_Forbidden_Integration(t *testing.T) {
	conn, realToken := customerTestConn(t)

	customerID := getCustomerIDFromToken(requestWithCookie("customer_token", realToken))
	if customerID == 0 {
		t.Skip("could not extract customer ID from token")
	}
	portalClient := pb.NewCustomerPortalServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+realToken))
	invoicesResp, err := portalClient.GetMyInvoices(authCtx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil || len(invoicesResp.GetInvoices()) == 0 {
		t.Skip("no invoices available for customer")
	}
	invoiceID := invoicesResp.GetInvoices()[0].Id

	foreignToken := createTestCustomerTokenWithID(9999)

	router := mux.NewRouter()
	router.HandleFunc("/portal/invoices/{id}", CustomerInvoiceDetailHandler)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/portal/invoices/%d", invoiceID), nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: foreignToken})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d for invoice %d requested by foreign customer 9999", rr.Code, invoiceID)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for cross-customer invoice access, got %d", rr.Code)
	}
}

// ── Expired token tests ──
//
// Customer portal handlers call getCustomerIDFromToken() which validates
// the JWT before making any gRPC call.  An expired token fails parsing,
// returns customerID=0, and the handler redirects to /portal/login.
// This is different from admin handlers which attach the raw cookie value
// to gRPC metadata (server-side RBAC rejects it).

func createExpiredCustomerToken() string {
	claims := &Claims{
		UserID:   1,
		Username: "expired@example.com",
		UserType: "customer",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := tok.SignedString(getJWTSecret())
	return s
}

func TestCustomerPortalDashboardHandler_ExpiredToken_Redirects(t *testing.T) {
	customerTestConn(t)
	expiredToken := createExpiredCustomerToken()
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	CustomerPortalDashboardHandler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for expired token, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/portal/login") {
		t.Errorf("expected redirect to /portal/login, got %q", loc)
	}
}

func TestCustomerOrdersHandler_ExpiredToken_Redirects(t *testing.T) {
	customerTestConn(t)
	expiredToken := createExpiredCustomerToken()
	req := httptest.NewRequest(http.MethodGet, "/portal/orders", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	CustomerOrdersHandler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for expired token, got %d", rr.Code)
	}
}

func TestCustomerInvoicesHandler_ExpiredToken_Redirects(t *testing.T) {
	customerTestConn(t)
	expiredToken := createExpiredCustomerToken()
	req := httptest.NewRequest(http.MethodGet, "/portal/invoices", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	CustomerInvoicesHandler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for expired token, got %d", rr.Code)
	}
}

// ── helpers ──

func assertRedirectToPortalLogin(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect to /portal/login, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/portal/login") {
		t.Errorf("expected redirect to /portal/login, got %q", loc)
	}
}

// requestWithCookie builds a minimal *http.Request carrying one named cookie.
func requestWithCookie(name, value string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: name, Value: value})
	return req
}

// createTestCustomerTokenWithID creates a customer JWT with a specific customer ID.
func createTestCustomerTokenWithID(customerID int) string {
	claims := &Claims{
		UserID:   customerID,
		Username: "test@example.com",
		UserType: "customer",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(getJWTSecret())
	return tokenString
}

// Customer portal gRPC error path tests (expired token → server-side RBAC rejection)

func TestCustomerOrderDetailHandler_GRPCError(t *testing.T) {
	customerTestConn(t)
	expiredToken := createExpiredCustomerToken()

	router := mux.NewRouter()
	router.HandleFunc("/portal/orders/{id}", CustomerOrderDetailHandler)
	req := httptest.NewRequest(http.MethodGet, "/portal/orders/1", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	// Expired token fails getCustomerIDFromToken → redirect to login
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for expired token, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/portal/login") {
		t.Errorf("expected redirect to /portal/login, got %q", loc)
	}
}

func TestCustomerInvoiceDetailHandler_GRPCError(t *testing.T) {
	customerTestConn(t)
	expiredToken := createExpiredCustomerToken()

	router := mux.NewRouter()
	router.HandleFunc("/portal/invoices/{id}", CustomerInvoiceDetailHandler)
	req := httptest.NewRequest(http.MethodGet, "/portal/invoices/1", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	// Expired token fails getCustomerIDFromToken → redirect to login
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for expired token, got %d", rr.Code)
	}
}

// Customer portal content assertion tests

func TestCustomerPortalDashboardHandler_ContentAssertions(t *testing.T) {
	_, token := customerTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	CustomerPortalDashboardHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	// The dashboard template defines title as "Dashboard" with "Customer Portal" suffix
	if !strings.Contains(body, "Customer Portal") {
		t.Error("expected 'Customer Portal' in response body")
	}
}

func TestCustomerOrdersHandler_ContentAssertions(t *testing.T) {
	_, token := customerTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/portal/orders", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	CustomerOrdersHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "My Orders") {
		t.Error("expected 'My Orders' in response body")
	}
}

func TestCustomerInvoicesHandler_ContentAssertions(t *testing.T) {
	_, token := customerTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/portal/invoices", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	CustomerInvoicesHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "My Invoices") {
		t.Error("expected 'My Invoices' in response body")
	}
}
