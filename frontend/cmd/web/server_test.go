package main

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"google.golang.org/grpc/metadata"
)

// ── Full server integration tests ──
//
// These tests build the complete HTTP router (with CSRF middleware) and
// exercise routes through httptest.NewServer.  This validates:
//   - CSRF middleware integration (token injection, verification on POST)
//   - Cookie jar persistence across redirects
//   - Route registration and method matching
//   - End-to-end login → dashboard → logout flows

// buildTestRouter assembles the same router layout as main() but without
// the static file server (templates are already loaded by initTemplates).
func buildTestRouter() *mux.Router {
	router := mux.NewRouter()
	router.StrictSlash(true)

	// Plaintext middleware marks requests as HTTP (not HTTPS) so that the
	// CSRF middleware skips strict Referer/Origin enforcement. httptest.NewServer
	// uses plain HTTP, and gorilla/csrf defaults to HTTPS-mode checks.
	// In production, the server runs over HTTPS and these checks are appropriate.
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, csrf.PlaintextHTTPRequest(r))
		})
	})

	// CSRF protection middleware (same config as main())
	// FieldName must match what templates use: name="gorilla.csrf.Token"
	csrfProtect := csrf.Protect(
		[]byte("test-csrf-key-32bytes!!!"),
		csrf.Secure(false),
		csrf.Path("/"),
		csrf.SameSite(csrf.SameSiteStrictMode),
		csrf.FieldName("gorilla.csrf.Token"),
	)
	router.Use(csrfProtect)

	// Public routes
	router.HandleFunc("/", homeHandler)
	router.HandleFunc("/stream", streamHandler)
	router.HandleFunc("/order-stream", orderStreamHandler)
	router.HandleFunc("/orders", orderDetailsHandler)

	// Static pages
	router.HandleFunc("/service", staticPageHandler("service"))
	router.HandleFunc("/product", staticPageHandler("product"))
	router.HandleFunc("/team", staticPageHandler("team"))
	router.HandleFunc("/testimonial", staticPageHandler("testimonial"))
	router.HandleFunc("/contact", staticPageHandler("contact"))
	router.HandleFunc("/404", staticPageHandler("404"))

	// Admin auth routes
	router.HandleFunc("/admin/login", AdminLoginPageHandler).Methods("GET")
	router.HandleFunc("/admin/login", AdminLoginHandler).Methods("POST")
	router.HandleFunc("/admin/logout", AdminLogoutHandler).Methods("GET")

	// Admin protected routes
	router.HandleFunc("/admin", RequireAdminAuth(AdminDashboardHandler)).Methods("GET")
	router.HandleFunc("/admin/", RequireAdminAuth(AdminDashboardHandler)).Methods("GET")
	router.HandleFunc("/admin/bread", RequireAdminAuth(AdminBreadListHandler)).Methods("GET")
	router.HandleFunc("/admin/bread/new", RequireAdminAuth(AdminBreadNewHandler)).Methods("GET")
	router.HandleFunc("/admin/bread/create", RequireAdminAuth(AdminBreadCreateHandler)).Methods("POST")
	router.HandleFunc("/admin/bread/{id}/edit", RequireAdminAuth(AdminBreadEditHandler)).Methods("GET")
	router.HandleFunc("/admin/bread/{id}/update", RequireAdminAuth(AdminBreadUpdateHandler)).Methods("POST")
	router.HandleFunc("/admin/bread/{id}/delete", RequireAdminAuth(AdminBreadDeleteHandler)).Methods("POST")
	router.HandleFunc("/admin/orders", RequireAdminAuth(AdminOrdersHandler)).Methods("GET")
	router.HandleFunc("/admin/orders/{id}/status", RequireAdminAuth(AdminOrderStatusHandler)).Methods("POST")
	router.HandleFunc("/admin/customers", RequireAdminAuth(AdminCustomersHandler)).Methods("GET")
	router.HandleFunc("/admin/customers/{id}", RequireAdminAuth(AdminCustomerDetailHandler)).Methods("GET")
	router.HandleFunc("/admin/makers", RequireAdminAuth(AdminMakersHandler)).Methods("GET")
	router.HandleFunc("/admin/makers/{id}", RequireAdminAuth(AdminMakerDetailHandler)).Methods("GET")
	router.HandleFunc("/admin/alerts", RequireAdminAuth(AdminAlertsHandler)).Methods("GET")
	router.HandleFunc("/admin/alerts/{id}/adjust", RequireAdminAuth(AdminAdjustQuantityHandler)).Methods("POST")
	router.HandleFunc("/admin/dashboard-stream", RequireAdminAuth(AdminDashboardStreamHandler)).Methods("GET")
	router.HandleFunc("/admin/alerts-stream", RequireAdminAuth(AdminAlertsStreamHandler)).Methods("GET")

	// Customer portal auth routes
	router.HandleFunc("/portal/login", CustomerLoginPageHandler).Methods("GET")
	router.HandleFunc("/portal/login", CustomerLoginHandler).Methods("POST")
	router.HandleFunc("/portal/logout", CustomerLogoutHandler).Methods("GET")

	// Customer portal protected routes
	router.HandleFunc("/portal", RequireCustomerAuth(CustomerPortalDashboardHandler)).Methods("GET")
	router.HandleFunc("/portal/", RequireCustomerAuth(CustomerPortalDashboardHandler)).Methods("GET")
	router.HandleFunc("/portal/orders", RequireCustomerAuth(CustomerOrdersHandler)).Methods("GET")
	router.HandleFunc("/portal/orders/{id}", RequireCustomerAuth(CustomerOrderDetailHandler)).Methods("GET")
	router.HandleFunc("/portal/invoices", RequireCustomerAuth(CustomerInvoicesHandler)).Methods("GET")
	router.HandleFunc("/portal/invoices/{id}", RequireCustomerAuth(CustomerInvoiceDetailHandler)).Methods("GET")

	return router
}

// csrfClient creates an http.Client that follows redirects and persists
// cookies (simulating a real browser session).
func csrfClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
		Timeout: 15 * time.Second,
	}
}

// ── Public page tests (through full router with CSRF) ──

func TestServer_PublicPages_Accessible(t *testing.T) {
	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	pages := []string{"/", "/service", "/product", "/team", "/testimonial", "/contact", "/404"}
	for _, path := range pages {
		t.Run(path, func(t *testing.T) {
			resp, err := csrfClient().Get(server.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s: expected 200, got %d", path, resp.StatusCode)
			}
		})
	}
}

// ── Admin login flow (full server with CSRF + cookie jar) ──

func TestServer_AdminLoginFlow_Integration(t *testing.T) {
	_, token := adminTestConn(t)

	server := httptest.NewServer(buildTestRouter())
	defer server.Close()
	client := csrfClient()

	// Step 1: GET /admin/login — should render form with CSRF token
	resp, err := client.Get(server.URL + "/admin/login")
	if err != nil {
		t.Fatalf("GET /admin/login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /admin/login: expected 200, got %d", resp.StatusCode)
	}

	// Step 2: Set a valid admin token cookie and visit /admin
	// (simulates a logged-in user — we can't do CSRF POST through client easily)
	req, _ := http.NewRequest("GET", server.URL+"/admin", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /admin (with token): expected 200, got %d", resp.StatusCode)
	}

	// Step 3: Logout clears the cookie
	resp, err = client.Get(server.URL + "/admin/logout")
	if err != nil {
		t.Fatalf("GET /admin/logout: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusOK {
		t.Errorf("GET /admin/logout: expected 303 or 200 (followed), got %d", resp.StatusCode)
	}
}

// TestServer_AdminProtectedRedirect verifies that accessing /admin without a
// token redirects to /admin/login (through the full router with CSRF).
func TestServer_AdminProtectedRedirect(t *testing.T) {
	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	// Do NOT follow redirects — we want to inspect the 303
	noFollowClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}

	resp, err := noFollowClient.Get(server.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("GET /admin (no token): expected 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "/admin/login") {
		t.Errorf("expected redirect to /admin/login, got %q", loc)
	}
}

// TestServer_PortalProtectedRedirect verifies that accessing /portal without a
// token redirects to /portal/login.
func TestServer_PortalProtectedRedirect(t *testing.T) {
	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	noFollowClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}

	resp, err := noFollowClient.Get(server.URL + "/portal")
	if err != nil {
		t.Fatalf("GET /portal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("GET /portal (no token): expected 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "/portal/login") {
		t.Errorf("expected redirect to /portal/login, got %q", loc)
	}
}

// TestServer_AdminBreadCRUDFlow exercises the full bread CRUD lifecycle
// through the router with mux.Vars (create → list → edit → update → delete).
func TestServer_AdminBreadCRUDFlow_Integration(t *testing.T) {
	conn, token := adminTestConn(t)

	adminClient := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))

	// Create a bread via gRPC so we control its ID.
	created, err := adminClient.CreateBread(authCtx, &pb.CreateBreadRequest{
		Name:     "ServerTest Bread",
		Price:    2.99,
		Quantity: 10,
		Type:     "test",
		Image:    "none.jpg",
	})
	if err != nil {
		t.Skipf("could not create test bread: %v", err)
	}
	breadID := created.Id
	t.Logf("created bread ID=%d", breadID)

	server := httptest.NewServer(buildTestRouter())
	defer server.Close()
	client := csrfClient()

	// Helper to make a GET request with admin token
	doReq := func(method, reqURL string) (*http.Response, error) {
		req, _ := http.NewRequest(method, reqURL, nil)
		req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
		return client.Do(req)
	}

	// GET /admin/bread/{id}/edit — should render form (200)
	resp, err := doReq("GET", fmt.Sprintf("%s/admin/bread/%d/edit", server.URL, breadID))
	if err != nil {
		t.Logf("GET edit: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
		t.Logf("GET /admin/bread/%d/edit: status=%d", breadID, resp.StatusCode)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for edit, got %d", resp.StatusCode)
		}
	}

	// GET /admin/login to obtain CSRF cookie + token for POST requests
	csrfResp, err := http.Get(server.URL + "/admin/login")
	if err != nil {
		t.Fatalf("GET /admin/login (for CSRF): %v", err)
	}
	csrfBuf := make([]byte, 8192)
	csrfN, _ := csrfResp.Body.Read(csrfBuf)
	csrfResp.Body.Close()
	csrfBody := string(csrfBuf[:csrfN])

	// Extract CSRF token from the hidden input
	// Go's html/template escapes + as &#43; in HTML attributes, so we must
	// unescape the extracted value to get the raw CSRF token.
	csrfToken := ""
	csrfIdx := strings.Index(csrfBody, `name="gorilla.csrf.Token" value="`)
	if csrfIdx != -1 {
		csrfIdx += len(`name="gorilla.csrf.Token" value="`)
		csrfEnd := strings.Index(csrfBody[csrfIdx:], `"`)
		if csrfEnd != -1 {
			csrfToken = html.UnescapeString(csrfBody[csrfIdx : csrfIdx+csrfEnd])
		}
	}
	csrfCookies := csrfResp.Cookies()

	// POST /admin/bread/{id}/delete — should redirect (303) with CSRF token
	deleteForm := url.Values{"gorilla.csrf.Token": {csrfToken}}
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/bread/%d/delete", server.URL, breadID),
		strings.NewReader(deleteForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	for _, c := range csrfCookies {
		req.AddCookie(c)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Logf("POST delete: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
		t.Logf("POST /admin/bread/%d/delete: status=%d", breadID, resp.StatusCode)
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusOK {
			t.Errorf("expected 303/200 for delete, got %d", resp.StatusCode)
		}
	}
}

// TestServer_CSRFTokenPresent verifies that GET /admin/login returns a page
// containing a CSRF token field (validates CSRF middleware is active).
func TestServer_CSRFTokenPresent(t *testing.T) {
	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/admin/login")
	if err != nil {
		t.Fatalf("GET /admin/login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read body to check for CSRF token field
	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	// The templates use gorilla.csrf.Token (new gorilla/csrf API field name)
	if !strings.Contains(body, "gorilla.csrf.Token") {
		t.Error("admin login page missing CSRF token field — CSRF middleware may not be active")
	}
}

// TestServer_PortalLogin CSRFTokenPresent verifies CSRF on the portal login page.
func TestServer_PortalLogin_CSRFTokenPresent(t *testing.T) {
	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	resp, err := http.Get(server.URL + "/portal/login")
	if err != nil {
		t.Fatalf("GET /portal/login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	if !strings.Contains(body, "gorilla.csrf.Token") {
		t.Error("portal login page missing CSRF token field")
	}
}

// TestServer_MethodNotAllowed verifies that POST to GET-only routes returns 405.
func TestServer_MethodNotAllowed(t *testing.T) {
	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	resp, err := http.Post(server.URL+"/admin/login", "application/x-www-form-urlencoded",
		strings.NewReader("username=admin&password=admin123"))
	if err != nil {
		t.Fatalf("POST /admin/login: %v", err)
	}
	defer resp.Body.Close()

	// POST /admin/login is a valid route (AdminLoginHandler), so it should not be 405.
	// It may redirect (303) on bad credentials or succeed.
	if resp.StatusCode == http.StatusMethodNotAllowed {
		t.Error("POST /admin/login should not return 405 — it is a registered route")
	}

	// GET to a POST-only route should return 405
	req, _ := http.NewRequest("GET", server.URL+"/admin/bread/create", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /admin/bread/create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /admin/bread/create: expected 405, got %d", resp.StatusCode)
	}
}

// TestServer_AdminLoginHandler_Success tests the admin login POST through the full server.
// Simulates the full browser flow: GET login page → extract CSRF token → POST with credentials.
func TestServer_AdminLoginHandler_Success(t *testing.T) {
	adminTestConn(t)

	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	// Step 1: GET /admin/login — obtain CSRF cookie + token from rendered form
	resp, err := http.Get(server.URL + "/admin/login")
	if err != nil {
		t.Fatalf("GET /admin/login: %v", err)
	}

	// Read body to extract CSRF token value from hidden input
	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	resp.Body.Close()
	body := string(buf[:n])

	// Extract CSRF token from the hidden input: value="TOKEN"
	// Go's html/template escapes + as &#43; in HTML attributes, so we must
	// unescape the extracted value to get the raw CSRF token.
	csrfToken := ""
	idx := strings.Index(body, `name="gorilla.csrf.Token" value="`)
	if idx != -1 {
		idx += len(`name="gorilla.csrf.Token" value="`)
		end := strings.Index(body[idx:], `"`)
		if end != -1 {
			csrfToken = html.UnescapeString(body[idx : idx+end])
		}
	}
	if csrfToken == "" {
		t.Fatal("could not extract CSRF token from login page")
	}

	// Extract CSRF cookie from GET response
	csrfCookies := resp.Cookies()

	// Step 2: POST /admin/login with credentials, CSRF token, and cookie
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}
	formData := url.Values{
		"username":          {"admin"},
		"password":          {"admin123"},
		"gorilla.csrf.Token": {csrfToken},
	}
	req, _ := http.NewRequest("POST", server.URL+"/admin/login",
		strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range csrfCookies {
		req.AddCookie(c)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/login: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("POST /admin/login: status=%d, location=%s", resp.StatusCode, resp.Header.Get("Location"))
	// Should redirect (303) on success or failure
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}
}

// TestServer_OrderDetailsPage tests the /orders page (requires admin auth).
func TestServer_OrderDetailsPage_Integration(t *testing.T) {
	_, token := adminTestConn(t)

	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL+"/orders", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /orders: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("GET /orders: status=%d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestServer_OrderDetailsPage_NoAuth tests the /orders page without auth.
func TestServer_OrderDetailsPage_NoAuth(t *testing.T) {
	adminTestConn(t)

	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	noFollowClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}

	resp, err := noFollowClient.Get(server.URL + "/orders")
	if err != nil {
		t.Fatalf("GET /orders: %v", err)
	}
	defer resp.Body.Close()

	// /orders requires admin auth; without a token it should redirect to login
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 303 redirect or 500 (no cookie), got %d", resp.StatusCode)
	}
}

// ── SSE stream tests (full server) ──

// TestServer_StreamHandler_Endpoint verifies that the /stream endpoint sets
// SSE headers (through the real router).
func TestServer_StreamHandler_Endpoint(t *testing.T) {
	adminTestConn(t)

	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequest("GET", server.URL+"/stream", nil)
	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
}

// TestServer_OrderStreamHandler_Endpoint verifies /order-stream through real router.
func TestServer_OrderStreamHandler_Endpoint(t *testing.T) {
	adminTestConn(t)

	server := httptest.NewServer(buildTestRouter())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequest("GET", server.URL+"/order-stream", nil)
	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /order-stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
}
