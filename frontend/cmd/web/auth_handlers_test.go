package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
)

// ── AdminLogoutHandler ──

func TestAdminLogoutHandler_ClearsCookieAndRedirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/logout", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: "some-token"})
	rr := httptest.NewRecorder()
	AdminLogoutHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("expected redirect to /admin/login, got %q", loc)
	}
	// Cookie must be expired (MaxAge == -1 sets Max-Age=0 in the Set-Cookie header)
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_token" {
			found = true
			if c.MaxAge >= 0 {
				t.Errorf("expected negative MaxAge to clear cookie, got %d", c.MaxAge)
			}
			if c.Value != "" {
				t.Errorf("expected empty cookie value, got %q", c.Value)
			}
		}
	}
	if !found {
		t.Error("expected Set-Cookie header for admin_token, found none")
	}
}

func TestAdminLogoutHandler_NoCookie(t *testing.T) {
	// Logout should work even without an existing cookie.
	req := httptest.NewRequest(http.MethodGet, "/admin/logout", nil)
	rr := httptest.NewRecorder()
	AdminLogoutHandler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}
}

// ── CustomerLogoutHandler ──

func TestCustomerLogoutHandler_ClearsCookieAndRedirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal/logout", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: "some-token"})
	rr := httptest.NewRecorder()
	CustomerLogoutHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/portal/login" {
		t.Errorf("expected redirect to /portal/login, got %q", loc)
	}
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == "customer_token" {
			found = true
			if c.MaxAge >= 0 {
				t.Errorf("expected negative MaxAge to clear cookie, got %d", c.MaxAge)
			}
		}
	}
	if !found {
		t.Error("expected Set-Cookie header for customer_token, found none")
	}
}

// ── AdminLoginPageHandler ──

func TestAdminLoginPageHandler_RendersForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rr := httptest.NewRecorder()
	AdminLoginPageHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAdminLoginPageHandler_ValidToken_Redirects(t *testing.T) {
	token := createTestAdminToken()
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	AdminLoginPageHandler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect to /admin for already-authenticated user, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin" {
		t.Errorf("expected redirect to /admin, got %q", loc)
	}
}

func TestAdminLoginPageHandler_InvalidToken_ShowsForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/login?error=Session+expired", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: "invalid-garbage"})
	rr := httptest.NewRecorder()
	AdminLoginPageHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for invalid token (show login form), got %d", rr.Code)
	}
}

// ── CustomerLoginPageHandler ──

func TestCustomerLoginPageHandler_RendersForm(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal/login", nil)
	rr := httptest.NewRecorder()
	CustomerLoginPageHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCustomerLoginPageHandler_ValidToken_Redirects(t *testing.T) {
	token := createTestCustomerToken()
	req := httptest.NewRequest(http.MethodGet, "/portal/login", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	rr := httptest.NewRecorder()
	CustomerLoginPageHandler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect to /portal for already-authenticated user, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/portal" {
		t.Errorf("expected redirect to /portal, got %q", loc)
	}
}

// ── validateToken ──

func TestValidateToken_ValidAdminToken(t *testing.T) {
	token := createTestAdminToken()
	if !validateToken(token, "admin") {
		t.Error("expected valid admin token to pass validation")
	}
}

func TestValidateToken_ValidCustomerToken(t *testing.T) {
	token := createTestCustomerToken()
	if !validateToken(token, "customer") {
		t.Error("expected valid customer token to pass validation")
	}
}

func TestValidateToken_WrongType(t *testing.T) {
	adminToken := createTestAdminToken()
	if validateToken(adminToken, "customer") {
		t.Error("expected admin token to fail customer type check")
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	if validateToken("not.a.valid.jwt", "admin") {
		t.Error("expected garbage token to fail validation")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	claims := &Claims{
		UserID:   1,
		Username: "admin",
		UserType: "admin",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // expired
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(getJWTSecret())
	if validateToken(tokenString, "admin") {
		t.Error("expected expired token to fail validation")
	}
}

func TestValidateToken_EmptyTypeAcceptsAny(t *testing.T) {
	// Empty expectedType skips the type check.
	adminToken := createTestAdminToken()
	if !validateToken(adminToken, "") {
		t.Error("expected empty expectedType to accept any valid token")
	}
}

// ── getCustomerIDFromToken ──

func TestGetCustomerIDFromToken_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if id := getCustomerIDFromToken(req); id != 0 {
		t.Errorf("expected 0 without cookie, got %d", id)
	}
}

func TestGetCustomerIDFromToken_InvalidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: "garbage"})
	if id := getCustomerIDFromToken(req); id != 0 {
		t.Errorf("expected 0 for invalid token, got %d", id)
	}
}

func TestGetCustomerIDFromToken_ValidToken(t *testing.T) {
	token := createTestCustomerToken() // UserID = 1 in createTestCustomerToken
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: token})
	if id := getCustomerIDFromToken(req); id != 1 {
		t.Errorf("expected customerID=1, got %d", id)
	}
}

// ── getAdminUserFromToken ──

func TestGetAdminUserFromToken_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	id, username, role := getAdminUserFromToken(req)
	if id != 0 || username != "" || role != "" {
		t.Errorf("expected empty values without cookie, got id=%d username=%q role=%q", id, username, role)
	}
}

func TestGetAdminUserFromToken_InvalidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: "garbage"})
	id, username, role := getAdminUserFromToken(req)
	if id != 0 || username != "" || role != "" {
		t.Errorf("expected empty values for invalid token, got id=%d username=%q role=%q", id, username, role)
	}
}

func TestGetAdminUserFromToken_ValidToken(t *testing.T) {
	token := createTestAdminToken() // UserID=1, Username="admin", Role="admin"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	id, username, role := getAdminUserFromToken(req)
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}
	if username != "admin" {
		t.Errorf("expected username=admin, got %q", username)
	}
	if role != "admin" {
		t.Errorf("expected role=admin, got %q", role)
	}
}

// ── RequireAdminAuth / RequireCustomerAuth: missing token cases ──

func TestRequireAdminAuth_NoToken_Redirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	handler := RequireAdminAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect without token, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "/admin/login") {
		t.Errorf("expected redirect to /admin/login, got %q", loc)
	}
}

func TestRequireAdminAuth_InvalidToken_Redirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: "invalid"})
	rr := httptest.NewRecorder()
	handler := RequireAdminAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 for invalid token, got %d", rr.Code)
	}
}

func TestRequireCustomerAuth_NoToken_Redirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	rr := httptest.NewRecorder()
	handler := RequireCustomerAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect without token, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "/portal/login") {
		t.Errorf("expected redirect to /portal/login, got %q", loc)
	}
}

func TestRequireCustomerAuth_InvalidToken_Redirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: "invalid"})
	rr := httptest.NewRecorder()
	handler := RequireCustomerAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 for invalid token, got %d", rr.Code)
	}
}

// ── Integration tests (skip if gRPC server is not running) ──

// authTestConn dials and probes the gRPC server with real credentials.
// It injects the connection as the shared connection and returns it.
// The test is skipped (not failed) if the server is unreachable.
func authTestConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	conn, token := adminTestConn(t) // reuse helper from admin_handlers_test.go
	// adminTestConn already called SetSharedGRPCConn; just return conn.
	// We don't need the token here, suppress the unused-variable warning.
	_ = token
	return conn
}

func TestAdminLoginHandler_Integration(t *testing.T) {
	authTestConn(t)

	form := strings.NewReader("username=admin&password=admin123")
	req := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	AdminLoginHandler(rr, req)

	t.Logf("status: %d, location: %s", rr.Code, rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after successful login, got %d", rr.Code)
	}
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_token" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected admin_token cookie after successful login")
	}
}

func TestAdminLoginHandler_BadCredentials_Integration(t *testing.T) {
	authTestConn(t)

	form := strings.NewReader("username=admin&password=wrongpassword")
	req := httptest.NewRequest(http.MethodPost, "/admin/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	AdminLoginHandler(rr, req)

	t.Logf("status: %d, location: %s", rr.Code, rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect back to login, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/admin/login") {
		t.Errorf("expected redirect back to /admin/login on failure, got %q", loc)
	}
}

func TestCustomerLoginHandler_Integration(t *testing.T) {
	customerTestConn(t) // ensure server is available; skips if not

	form := strings.NewReader("email=john@doe.com&password=password123")
	req := httptest.NewRequest(http.MethodPost, "/portal/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	CustomerLoginHandler(rr, req)

	t.Logf("status: %d, location: %s", rr.Code, rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after successful login, got %d", rr.Code)
	}
	found := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == "customer_token" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected customer_token cookie after successful login")
	}
}
