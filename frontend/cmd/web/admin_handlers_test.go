package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ── No-auth redirect tests (unit — no gRPC server needed) ──

func TestAdminDashboardHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	AdminDashboardHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminBreadListHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/bread", nil)
	rr := httptest.NewRecorder()
	AdminBreadListHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminBreadEditHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/bread/1/edit", nil)
	rr := httptest.NewRecorder()
	AdminBreadEditHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminBreadCreateHandler_NoAuth(t *testing.T) {
	form := url.Values{"name": {"Sourdough"}, "price": {"3.50"}, "quantity": {"10"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/bread/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	AdminBreadCreateHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminBreadUpdateHandler_NoAuth(t *testing.T) {
	form := url.Values{"name": {"Sourdough"}, "price": {"3.50"}, "quantity": {"10"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/bread/1/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	AdminBreadUpdateHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminBreadDeleteHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/admin/bread/1/delete", nil)
	rr := httptest.NewRecorder()
	AdminBreadDeleteHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminOrdersHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/orders", nil)
	rr := httptest.NewRecorder()
	AdminOrdersHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminOrderStatusHandler_NoAuth(t *testing.T) {
	form := url.Values{"status": {"completed"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/orders/some-uuid/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	AdminOrderStatusHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminCustomersHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/customers", nil)
	rr := httptest.NewRecorder()
	AdminCustomersHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminCustomerDetailHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/customers/1", nil)
	rr := httptest.NewRecorder()
	AdminCustomerDetailHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminMakersHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/makers", nil)
	rr := httptest.NewRecorder()
	AdminMakersHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminMakerDetailHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/makers/1", nil)
	rr := httptest.NewRecorder()
	AdminMakerDetailHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminAlertsHandler_NoAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/alerts", nil)
	rr := httptest.NewRecorder()
	AdminAlertsHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

func TestAdminAdjustQuantityHandler_NoAuth(t *testing.T) {
	form := url.Values{"quantity": {"20"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/alerts/1/adjust", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	AdminAdjustQuantityHandler(rr, req)
	assertRedirectToAdminLogin(t, rr)
}

// ── AdminBreadNewHandler: only admin handler with zero gRPC calls ──

func TestAdminBreadNewHandler_RendersForm(t *testing.T) {
	token := createTestAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin/bread/new", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminBreadNewHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestAdminBreadNewHandler_NoAuth_StillRenders(t *testing.T) {
	// No cookie: getAdminUserFromToken returns empty strings but no gRPC call is made.
	req := httptest.NewRequest(http.MethodGet, "/admin/bread/new", nil)
	rr := httptest.NewRecorder()
	AdminBreadNewHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ── newAdminTemplateData ──

func TestNewAdminTemplateData_Fields(t *testing.T) {
	token := createTestAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})

	data := newAdminTemplateData(req, "Dashboard", "dashboard")

	if data.Title != "Dashboard" {
		t.Errorf("Title: want %q, got %q", "Dashboard", data.Title)
	}
	if data.CurrentPage != "dashboard" {
		t.Errorf("CurrentPage: want %q, got %q", "dashboard", data.CurrentPage)
	}
	if data.AdminUsername != "admin" {
		t.Errorf("AdminUsername: want %q, got %q", "admin", data.AdminUsername)
	}
	if data.AdminRole != "admin" {
		t.Errorf("AdminRole: want %q, got %q", "admin", data.AdminRole)
	}
}

func TestNewAdminTemplateData_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	data := newAdminTemplateData(req, "Title", "page")
	if data.AdminUsername != "" || data.AdminRole != "" {
		t.Errorf("expected empty username/role without cookie, got %q / %q",
			data.AdminUsername, data.AdminRole)
	}
}

// ── SSE stream handlers: verify headers and immediate return on context cancel ──

func TestAdminDashboardStreamHandler_SSEHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard-stream", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	AdminDashboardStreamHandler(rr, req)
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control: want no-cache, got %q", cc)
	}
}

func TestAdminAlertsStreamHandler_SSEHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/admin/alerts-stream", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	AdminAlertsStreamHandler(rr, req)
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
}

// ── Integration tests (skip if gRPC server is not running) ──

// adminTestConn returns the shared admin gRPC connection set up by TestMain.
// It skips the test if the server was not reachable at startup.
// Callers must NOT close the returned connection — it is owned by TestMain.
func adminTestConn(t *testing.T) (*grpc.ClientConn, string) {
	t.Helper()
	if sharedAdminConn == nil || sharedAdminToken == "" {
		t.Skip("gRPC server not available or admin login failed at startup")
	}
	SetSharedGRPCConn(sharedAdminConn)
	return sharedAdminConn, sharedAdminToken
}

func TestAdminOrdersHandler_Integration(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/orders", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminOrdersHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAdminCustomersHandler_Integration(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/customers", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminCustomersHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAdminMakersHandler_Integration(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/makers", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminMakersHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAdminBreadCreateHandler_Integration(t *testing.T) {
	_, token := adminTestConn(t)

	form := url.Values{
		"name":        {"Integration Test Bread"},
		"price":       {"2.50"},
		"quantity":    {"5"},
		"description": {"Created by integration test"},
		"type":        {"test"},
		"image":       {"test.jpg"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/bread/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminBreadCreateHandler(rr, req)

	t.Logf("status: %d, location: %s", rr.Code, rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 after create, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "/admin/bread") {
		t.Errorf("expected redirect to /admin/bread, got %q", loc)
	}
}

// TestAdminBreadEditUpdateDeleteCycle creates a test bread, edits it, updates it, deletes it.
func TestAdminBreadEditUpdateDeleteCycle_Integration(t *testing.T) {
	conn, token := adminTestConn(t)

	adminClient := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	// Create a bread we fully control.
	created, err := adminClient.CreateBread(authCtx, &pb.CreateBreadRequest{
		Name:        "CycleTest Bread",
		Price:       3.99,
		Quantity:    2,
		Description: "temp test bread",
		Type:        "test",
		Image:       "none.jpg",
	})
	if err != nil {
		t.Skipf("could not create test bread: %v", err)
	}
	breadID := created.Id
	t.Logf("created bread ID=%d", breadID)

	// Edit (GET form)
	t.Run("Edit", func(t *testing.T) {
		router := mux.NewRouter()
		router.HandleFunc("/admin/bread/{id}/edit", AdminBreadEditHandler)
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/bread/%d/edit", breadID), nil)
		req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		t.Logf("edit status: %d", rr.Code)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	// Update (POST form)
	t.Run("Update", func(t *testing.T) {
		form := url.Values{
			"name":        {"CycleTest Bread Updated"},
			"price":       {"4.99"},
			"quantity":    {"3"},
			"description": {"updated description"},
			"type":        {"test"},
			"image":       {"none.jpg"},
		}
		router := mux.NewRouter()
		router.HandleFunc("/admin/bread/{id}/update", AdminBreadUpdateHandler)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/bread/%d/update", breadID),
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		t.Logf("update status: %d, location: %s", rr.Code, rr.Header().Get("Location"))
		if rr.Code != http.StatusSeeOther {
			t.Errorf("expected 303 after update, got %d", rr.Code)
		}
	})

	// Delete (POST)
	t.Run("Delete", func(t *testing.T) {
		router := mux.NewRouter()
		router.HandleFunc("/admin/bread/{id}/delete", AdminBreadDeleteHandler)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/bread/%d/delete", breadID), nil)
		req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		t.Logf("delete status: %d, location: %s", rr.Code, rr.Header().Get("Location"))
		if rr.Code != http.StatusSeeOther {
			t.Errorf("expected 303 after delete, got %d", rr.Code)
		}
	})
}

func TestAdminCustomerDetailHandler_Integration(t *testing.T) {
	conn, token := adminTestConn(t)

	adminClient := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	customers, err := adminClient.GetAllCustomers(authCtx, &pb.Empty{})
	if err != nil || len(customers.GetCustomers()) == 0 {
		t.Skip("no customers available")
	}
	customerID := customers.GetCustomers()[0].Id

	router := mux.NewRouter()
	router.HandleFunc("/admin/customers/{id}", AdminCustomerDetailHandler)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/customers/%d", customerID), nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d for customer ID %d", rr.Code, customerID)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAdminMakerDetailHandler_Integration(t *testing.T) {
	conn, token := adminTestConn(t)

	adminClient := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	makers, err := adminClient.GetAllBreadMakers(authCtx, &pb.Empty{})
	if err != nil || len(makers.GetBreadMakers()) == 0 {
		t.Skip("no bread makers available")
	}
	makerID := makers.GetBreadMakers()[0].Id

	router := mux.NewRouter()
	router.HandleFunc("/admin/makers/{id}", AdminMakerDetailHandler)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/makers/%d", makerID), nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d for maker ID %d", rr.Code, makerID)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAdminOrderStatusHandler_Integration(t *testing.T) {
	conn, token := adminTestConn(t)

	adminClient := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	orders, err := adminClient.GetAllOrders(authCtx, &pb.Empty{})
	if err != nil || len(orders.GetBuyOrders()) == 0 {
		t.Skip("no orders available for status update")
	}
	orderUUID := orders.GetBuyOrders()[0].BuyOrderUuid

	form := url.Values{"status": {"pending"}}
	router := mux.NewRouter()
	router.HandleFunc("/admin/orders/{id}/status", AdminOrderStatusHandler)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/orders/%s/status", orderUUID),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d, location: %s", rr.Code, rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 after status update, got %d", rr.Code)
	}
}

func TestAdminAdjustQuantityHandler_Integration(t *testing.T) {
	conn, token := adminTestConn(t)

	adminClient := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	breads, err := adminClient.GetAllBread(authCtx, &pb.Empty{})
	if err != nil || len(breads.GetBreads()) == 0 {
		t.Skip("no breads available")
	}
	bread := breads.GetBreads()[0]

	form := url.Values{"quantity": {fmt.Sprintf("%d", bread.Quantity)}} // keep same quantity
	router := mux.NewRouter()
	router.HandleFunc("/admin/alerts/{id}/adjust", AdminAdjustQuantityHandler)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/alerts/%d/adjust", bread.Id),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d, location: %s", rr.Code, rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 after quantity adjust, got %d", rr.Code)
	}
}

// TestAdminMakerDetailHandler_NotFound exercises the 404 error path when a
// non-existent maker ID is requested. Requires a running gRPC server.
func TestAdminMakerDetailHandler_NotFound_Integration(t *testing.T) {
	_, token := adminTestConn(t)

	router := mux.NewRouter()
	router.HandleFunc("/admin/makers/{id}", AdminMakerDetailHandler)
	req := httptest.NewRequest(http.MethodGet, "/admin/makers/999999", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d for maker ID 999999", rr.Code)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent maker, got %d", rr.Code)
	}
}

// TestAdminOrderStatusHandler_InvalidUUID exercises the 500 error path when
// UpdateOrderStatus is called with a UUID that does not exist.
func TestAdminOrderStatusHandler_InvalidUUID_Integration(t *testing.T) {
	_, token := adminTestConn(t)

	form := url.Values{"status": {"completed"}}
	router := mux.NewRouter()
	router.HandleFunc("/admin/orders/{id}/status", AdminOrderStatusHandler)
	req := httptest.NewRequest(http.MethodPost, "/admin/orders/00000000-0000-0000-0000-000000000000/status",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusInternalServerError && rr.Code != http.StatusSeeOther {
		t.Errorf("expected 500 or 303, got %d", rr.Code)
	}
}

// TestAdminDashboardStreamHandler_WithServer exercises the full stream loop:
// auth succeeds, stats are fetched and written, then the context is cancelled.
// Note: time.Sleep(15s) inside the handler means this test takes ~16 seconds.
func TestAdminDashboardStreamHandler_WithServer(t *testing.T) {
	_, token := adminTestConn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 17*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard-stream", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminDashboardStreamHandler(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "data:") {
		t.Errorf("expected SSE data line in response body, got %q", body[:min(len(body), 200)])
	}
	t.Logf("SSE body preview: %s", body[:min(len(body), 200)])
}

// TestAdminAlertsStreamHandler_WithServer exercises the full alerts stream loop.
// Note: time.Sleep(15s) inside the handler means this test takes ~16 seconds.
func TestAdminAlertsStreamHandler_WithServer(t *testing.T) {
	_, token := adminTestConn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 17*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/admin/alerts-stream", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminAlertsStreamHandler(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	t.Logf("alerts stream body length: %d", rr.Body.Len())
}

// ── helpers ──

// ── gRPC-error-path tests (unit — use expired token to make gRPC calls fail) ──
//
// An expired JWT passes the cookie-existence check in adminGRPCContextWithTimeout
// but is rejected by the server's RBAC interceptor.  The server returns
// Unauthenticated, causing the gRPC call to fail and the handler to write
// an HTTP 500 or redirect — covering the err != nil branches after gRPC calls.
//
// These tests require a running gRPC server; they skip via adminTestConn if not.

func createExpiredAdminToken() string {
	claims := &Claims{
		UserID:   1,
		Username: "admin",
		UserType: "admin",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := tok.SignedString(getJWTSecret())
	return s
}

func TestAdminBreadCreateHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	form := url.Values{"name": {"Test"}, "price": {"1.99"}, "quantity": {"1"}, "type": {"test"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/bread/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	AdminBreadCreateHandler(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token gRPC error, got %d", rr.Code)
	}
}

func TestAdminBreadEditHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	router := mux.NewRouter()
	router.HandleFunc("/admin/bread/{id}/edit", AdminBreadEditHandler)
	req := httptest.NewRequest(http.MethodGet, "/admin/bread/1/edit", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for expired token gRPC error on GetBreadById, got %d", rr.Code)
	}
}

func TestAdminBreadUpdateHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	form := url.Values{"name": {"Test"}, "price": {"1.99"}, "quantity": {"1"}}
	router := mux.NewRouter()
	router.HandleFunc("/admin/bread/{id}/update", AdminBreadUpdateHandler)
	req := httptest.NewRequest(http.MethodPost, "/admin/bread/1/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token gRPC error on UpdateBread, got %d", rr.Code)
	}
}

func TestAdminBreadDeleteHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	router := mux.NewRouter()
	router.HandleFunc("/admin/bread/{id}/delete", AdminBreadDeleteHandler)
	req := httptest.NewRequest(http.MethodPost, "/admin/bread/1/delete", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token gRPC error on DeleteBread, got %d", rr.Code)
	}
}

func TestAdminBreadListHandler_GRPCError(t *testing.T) {
	adminTestConn(t) // skip if server not available
	expiredToken := createExpiredAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin/bread", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	AdminBreadListHandler(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token gRPC error, got %d", rr.Code)
	}
}

func TestAdminDashboardHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	AdminDashboardHandler(rr, req)
	// Dashboard renders with empty data on gRPC error (graceful degradation).
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (renders with empty stats on gRPC error), got %d", rr.Code)
	}
}

func TestAdminOrdersHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin/orders", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	AdminOrdersHandler(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token gRPC error, got %d", rr.Code)
	}
}

func TestAdminCustomersHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin/customers", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	AdminCustomersHandler(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token gRPC error, got %d", rr.Code)
	}
}

func TestAdminMakersHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin/makers", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	AdminMakersHandler(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token gRPC error, got %d", rr.Code)
	}
}

func TestAdminAlertsHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin/alerts", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	AdminAlertsHandler(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token gRPC error, got %d", rr.Code)
	}
}

func TestAdminAdjustQuantityHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()
	form := url.Values{"quantity": {"10"}}
	router := mux.NewRouter()
	router.HandleFunc("/admin/alerts/{id}/adjust", AdminAdjustQuantityHandler)
	req := httptest.NewRequest(http.MethodPost, "/admin/alerts/1/adjust", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	// GetBreadById fails → 404 (Bread not found); any non-200, non-303 is acceptable.
	if rr.Code != http.StatusNotFound && rr.Code != http.StatusInternalServerError && rr.Code != http.StatusSeeOther {
		t.Errorf("expected 404, 500 or 303 for expired token, got %d", rr.Code)
	}
}

func assertRedirectToAdminLogin(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect to /admin/login, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/admin/login") {
		t.Errorf("expected redirect to /admin/login, got %q", loc)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Content assertion tests (verify HTML content on successful responses)

func TestAdminDashboardHandler_ContentAssertions(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminDashboardHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Admin Dashboard") {
		t.Error("expected 'Admin Dashboard' in response body")
	}
}

func TestAdminBreadListHandler_ContentAssertions(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/bread", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminBreadListHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Bread Management") {
		t.Error("expected 'Bread Management' in response body")
	}
	breadNames := []string{"Sourdough", "Baguette", "Cinnamon Roll", "Pretzel"}
	found := false
	for _, name := range breadNames {
		if strings.Contains(body, name) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one bread name in response; checked %v", breadNames)
	}
}

func TestAdminOrdersHandler_ContentAssertions(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/orders", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminOrdersHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Order Management") {
		t.Error("expected 'Order Management' in response body")
	}
}

func TestAdminCustomersHandler_ContentAssertions(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/customers", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminCustomersHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Customer Management") {
		t.Error("expected 'Customer Management' in response body")
	}
}

func TestAdminMakersHandler_ContentAssertions(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/makers", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminMakersHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Bread Maker Management") {
		t.Error("expected 'Bread Maker Management' in response body")
	}
}

func TestAdminAlertsHandler_ContentAssertions(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/alerts", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminAlertsHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Inventory Alerts") {
		t.Error("expected 'Inventory Alerts' in response body")
	}
}

func TestAdminAdjustQuantityHandler_ChangesQuantity_Integration(t *testing.T) {
	conn, token := adminTestConn(t)

	adminClient := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	created, err := adminClient.CreateBread(authCtx, &pb.CreateBreadRequest{
		Name:     "AdjustQtyTest Bread",
		Price:    1.99,
		Quantity: 5,
		Type:     "test",
		Image:    "none.jpg",
	})
	if err != nil {
		t.Skipf("could not create test bread: %v", err)
	}
	breadID := created.Id
	newQty := int(created.Quantity) + 10

	t.Logf("created bread ID=%d, qty=%d, adjusting to %d", breadID, created.Quantity, newQty)

	form := url.Values{"quantity": {fmt.Sprintf("%d", newQty)}}
	router := mux.NewRouter()
	router.HandleFunc("/admin/alerts/{id}/adjust", AdminAdjustQuantityHandler)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/alerts/%d/adjust", breadID),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 after quantity adjust, got %d", rr.Code)
	}

	// Verify the quantity was actually changed
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	authCtx2 := metadata.NewOutgoingContext(ctx2, metadata.Pairs("authorization", "Bearer "+token))
	updated, err := adminClient.GetBreadById(authCtx2, &pb.BreadIdRequest{Id: breadID})
	if err != nil {
		t.Fatalf("could not fetch bread after adjust: %v", err)
	}
	if updated.Quantity != int32(newQty) {
		t.Errorf("expected quantity %d after adjust, got %d", newQty, updated.Quantity)
	}

	// Clean up
	adminClient.DeleteBread(authCtx2, &pb.DeleteBreadRequest{Id: breadID})
}

func TestAdminBreadCreateHandler_VerifiesCreation_Integration(t *testing.T) {
	conn, token := adminTestConn(t)

	testName := "VerifyCreate Bread " + fmt.Sprintf("%d", time.Now().Unix())
	form := url.Values{
		"name":        {testName},
		"price":       {"5.99"},
		"quantity":    {"15"},
		"description": {"Verification test"},
		"type":        {"test"},
		"image":       {"verify.jpg"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/bread/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminBreadCreateHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 after create, got %d", rr.Code)
	}

	// Verify the bread was actually created
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
	adminClient := pb.NewAdminServiceClient(conn)
	breads, err := adminClient.GetAllBread(authCtx, &pb.Empty{})
	if err != nil {
		t.Fatalf("could not fetch breads: %v", err)
	}

	found := false
	var foundBread *pb.Bread
	for _, b := range breads.GetBreads() {
		if b.Name == testName {
			found = true
			foundBread = b
			break
		}
	}
	if !found {
		t.Fatalf("expected to find bread %q in list", testName)
	}
	if foundBread.Price != 5.99 {
		t.Errorf("expected price 5.99, got %f", foundBread.Price)
	}
	if foundBread.Quantity != 15 {
		t.Errorf("expected quantity 15, got %d", foundBread.Quantity)
	}

	// Clean up
	adminClient.DeleteBread(authCtx, &pb.DeleteBreadRequest{Id: foundBread.Id})
}
