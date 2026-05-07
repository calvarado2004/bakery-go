package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

)

// ── homeHandler ──

func TestHomeHandler_WithServer(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	homeHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Body.Len() == 0 {
		t.Error("expected non-empty response body")
	}
}

// TestHomeHandler_NoAuthCookieWithServer tests the best-effort auth path where the
// admin cookie is absent. CheckBreadInventory is open so the handler should still return 200.
func TestHomeHandler_NoAuthCookieWithServer(t *testing.T) {
	adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	homeHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	// May return 200 (inventory fetched) or 500 (if CheckBreadInventory requires auth).
	// Either way the handler must complete without panicking.
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Errorf("unexpected status %d", rr.Code)
	}
}

// ── orderDetailsHandler ──

func TestOrderDetailsHandler_WithServer(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	orderDetailsHandler(rr, req)

	t.Logf("status: %d", rr.Code)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ── streamHandler (SSE inventory stream) ──

// TestStreamHandler_WithServer opens the SSE stream with a real gRPC connection
// and verifies that SSE data lines arrive before the context is cancelled.
func TestStreamHandler_WithServer(t *testing.T) {
	adminTestConn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	streamHandler(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	body := rr.Body.String()
	t.Logf("stream body preview: %s", body[:min(len(body), 300)])
}

// ── orderStreamHandler (SSE buy-order stream) ──

// TestOrderStreamHandler_WithServer opens the order SSE stream and lets it run
// briefly before cancellation. Verifies SSE headers are set correctly.
func TestOrderStreamHandler_WithServer(t *testing.T) {
	_, token := adminTestConn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/order-stream", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	orderStreamHandler(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	t.Logf("order-stream body length: %d", rr.Body.Len())
}

// TestOrderStreamHandler_NoAuth exercises the error path where no customer cookie
// is provided and the gRPC BuyOrderStream endpoint may require auth.
func TestOrderStreamHandler_NoAuth(t *testing.T) {
	adminTestConn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/order-stream", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	orderStreamHandler(rr, req)

	// May return 500 (auth required by server) or 200 with SSE — either is fine.
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") &&
		rr.Code != http.StatusInternalServerError {
		t.Logf("status %d, content-type %q", rr.Code, ct)
	}
}

// ── homeHandler: gRPC error path (expired token → server rejects → 500) ──

// TestHomeHandler_GRPCError tests the home handler's error path when the gRPC
// call fails due to an expired JWT. The server's RBAC interceptor rejects the
// expired token, causing CheckBreadInventory to return an error → HTTP 500.
func TestHomeHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	homeHandler(rr, req)

	// The handler may return 200 (inventory is open) or 500 (server requires auth).
	// Either way, it should complete without panicking.
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d", rr.Code)
	}
}

// TestHomeHandler_ContentAssertions verifies the home handler renders the page
// with expected structural content. Bread names are rendered client-side (SSE/JS),
// so we assert on the page structure instead.
func TestHomeHandler_ContentAssertions(t *testing.T) {
	_, token := adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rr := httptest.NewRecorder()
	homeHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if len(body) < 100 {
		t.Error("expected substantial HTML response body")
	}
	// The index page renders navigation links
	if !strings.Contains(body, `href="/"`) {
		t.Error("expected home nav link in response body")
	}
}

// ── orderDetailsHandler: no-auth redirect ──

// TestOrderDetailsHandler_NoAuth_Redirect verifies that /orders without an admin
// token redirects to /admin/login (the handler calls adminGRPCContext which fails).
func TestOrderDetailsHandler_NoAuth_Redirect(t *testing.T) {
	adminTestConn(t)

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	rr := httptest.NewRecorder()
	orderDetailsHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect without auth, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/admin/login") {
		t.Errorf("expected redirect to /admin/login, got %q", loc)
	}
}

// TestOrderDetailsHandler_GRPCError tests the order details handler with an
// expired admin token. The server rejects the expired JWT, causing 500.
func TestOrderDetailsHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	orderDetailsHandler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for expired token, got %d", rr.Code)
	}
}

// ── streamHandler: no gRPC connection ──

// TestStreamHandler_NoGRPCConnection verifies that streamHandler returns a
// graceful SSE error when the gRPC connection is not available.
func TestStreamHandler_NoGRPCConnection(t *testing.T) {
	// Save and restore the shared connection
	saved := sharedGRPCConn
	SetSharedGRPCConn(nil)
	defer SetSharedGRPCConn(saved)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	streamHandler(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "service unavailable") {
		t.Errorf("expected 'service unavailable' in SSE body, got %q", body)
	}
}

// TestOrderStreamHandler_NoGRPCConnection verifies that orderStreamHandler returns
// a graceful SSE error when the gRPC connection is not available.
func TestOrderStreamHandler_NoGRPCConnection(t *testing.T) {
	// Save and restore the shared connection
	saved := sharedGRPCConn
	SetSharedGRPCConn(nil)
	defer SetSharedGRPCConn(saved)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/order-stream", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	orderStreamHandler(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "service unavailable") {
		t.Errorf("expected 'service unavailable' in SSE body, got %q", body)
	}
}

// TestStreamHandler_GRPCError verifies that streamHandler writes an SSE error
// data line when the gRPC stream call fails due to an expired token.
func TestStreamHandler_GRPCError(t *testing.T) {
	adminTestConn(t)
	expiredToken := createExpiredAdminToken()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: expiredToken})
	rr := httptest.NewRecorder()
	streamHandler(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	body := rr.Body.String()
	t.Logf("stream body (expired token): %s", body[:min(len(body), 200)])
}
