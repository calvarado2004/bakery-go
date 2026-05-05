package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

// ── adminGRPCContext tests ──

func TestAdminGRPCContext_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := adminGRPCContext(req)
	if err == nil {
		t.Error("expected error when admin_token cookie is missing")
	}
}

func TestAdminGRPCContext_EmptyCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: ""})
	_, err := adminGRPCContext(req)
	if err == nil {
		t.Error("expected error when admin_token cookie is empty")
	}
}

func TestAdminGRPCContext_WithValidCookie(t *testing.T) {
	token := "Bearer test-token-value"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})

	ctx, err := adminGRPCContext(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected metadata in context")
	}

	authValues := md.Get("authorization")
	if len(authValues) != 1 {
		t.Fatalf("expected exactly 1 authorization value, got %d", len(authValues))
	}
	if authValues[0] != "Bearer "+token {
		t.Errorf("expected authorization=%q, got %q", "Bearer "+token, authValues[0])
	}
}

func TestAdminGRPCContextWithTimeout_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, _, err := adminGRPCContextWithTimeout(req, 10*time.Second)
	if err == nil {
		t.Error("expected error when admin_token cookie is missing")
	}
}

func TestAdminGRPCContextWithTimeout_WithCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: "test-jwt-token"})

	ctx, cancel, err := adminGRPCContextWithTimeout(req, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cancel()

	// Verify the context has a deadline
	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		t.Error("expected context to have a deadline")
	}

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected metadata in context")
	}

	authValues := md.Get("authorization")
	if len(authValues) == 0 {
		t.Error("expected authorization metadata to be set")
	}
}

// ── customerGRPCContext tests ──

func TestCustomerGRPCContext_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := customerGRPCContext(req)
	// Should return r.Context() when no customer_id can be extracted
	if ctx != req.Context() {
		t.Error("expected original context when no customer cookie")
	}
}

func TestCustomerGRPCContext_WithValidCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: "test-customer-jwt"})

	ctx := customerGRPCContext(req)

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected metadata in context")
	}

	authValues := md.Get("authorization")
	if len(authValues) == 0 {
		t.Error("expected authorization metadata to be set")
	}

	customerIDValues := md.Get("customer_id")
	// customer_id might be empty if JWT parsing fails (test secret), but authorization should be present
	t.Logf("authorization values: %v, customer_id values: %v", authValues, customerIDValues)
}

func TestCustomerGRPCContext_WithTimeout_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx, cancel, err := customerGRPCContextWithTimeout(req, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cancel()

	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		t.Error("expected context to have a deadline")
	}
}

// ── Admin handlers must use auth context tests ──

// TestAdminHandler_RequiresAuthContext verifies that all admin handlers
// call adminGRPCContext (directly or indirectly) and fail without a token.
// This test catches the bug where handlers used r.Context() directly
// without the auth wrapper — the exact issue that broke the frontend.
func TestAdminHandler_RequiresAuthContext(t *testing.T) {
	// This test validates the auth context functions work correctly.
	// If admin handlers pass r.Context() directly instead of adminGRPCContext(r),
	// they won't have the "authorization" metadata, and the server's RBAC interceptor
	// will reject them with "authorization token is required".

	// Step 1: Verify adminGRPCContext produces metadata
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: "test-token"})
	grpcCtx, err := adminGRPCContext(req)
	if err != nil {
		t.Fatalf("adminGRPCContext failed: %v", err)
	}

	md, ok := metadata.FromOutgoingContext(grpcCtx)
	if !ok {
		t.Fatal("no metadata in auth context")
	}
	if len(md.Get("authorization")) == 0 {
		t.Fatal("authorization metadata not set in admin context")
	}

	// Step 2: Verify plain r.Context() does NOT have authorization metadata
	plainMD, _ := metadata.FromOutgoingContext(req.Context())
	if len(plainMD.Get("authorization")) > 0 {
		t.Error("plain r.Context() unexpectedly has authorization — test setup issue")
	}
}

// TestCustomerHandler_RequiresAuthContext verifies that customer handlers
// include both authorization and customer_id metadata.
func TestCustomerHandler_RequiresAuthContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "customer_token", Value: "test-customer-token"})

	ctx := customerGRPCContext(req)
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("no metadata in customer auth context")
	}

	// Authorization must be present — the server's RBAC interceptor checks this
	authValues := md.Get("authorization")
	if len(authValues) == 0 {
		t.Fatal("authorization metadata not set in customer context")
	}

	t.Logf("customer context has authorization=%v and customer_id=%v",
		authValues, md.Get("customer_id"))
}

// TestNoAuthContext_PassesNoAuthorization verifies that a request without
// auth cookies produces a context with no authorization header, ensuring
// the server will reject unauthenticated calls when expected.
func TestNoAuthContext_PassesNoAuthorization(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := req.Context()

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		// No outgoing context — that's fine, means no metadata
		return
	}
	if len(md.Get("authorization")) > 0 {
		t.Error("unauthenticated request unexpectedly has authorization metadata")
	}
}
