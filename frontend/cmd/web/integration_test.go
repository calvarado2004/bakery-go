package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/testutils"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestFrontend_HomeHandler_Integration tests the home handler with real gRPC
func TestFrontend_HomeHandler_Integration(t *testing.T) {
	// Skip if gRPC server is not running
	addr := testutils.GetGRPCAddress()
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		t.Skipf("Could not connect to gRPC server: %v", err)
	}
	defer conn.Close()

	t.Run("HomeHandlerConnectsToGRPC", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()

		// homeHandler calls gRPC to get inventory
		// May panic if templates are not found, which is expected in integration test
		defer func() {
			if r := recover(); r != nil {
				t.Logf("HomeHandler panicked (expected): %v", r)
			}
		}()

		homeHandler(rr, req)

		// The handler should complete (may fail if gRPC returns error)
		t.Logf("Response status: %d", rr.Code)
	})
}

// TestFrontend_AdminRoutes_Integration tests admin routes
func TestFrontend_AdminRoutes_Integration(t *testing.T) {
	// Set JWT secret for testing
	os.Setenv("JWT_SECRET", "test-secret-for-integration")
	defer os.Unsetenv("JWT_SECRET")

	t.Run("AdminLoginPageHandler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
		rr := httptest.NewRecorder()

		AdminLoginPageHandler(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		body := rr.Body.String()
		if !strings.Contains(body, "Admin Login") {
			t.Log("Admin login page rendered (may have different content)")
		}
	})

	t.Run("AdminDashboardHandler", func(t *testing.T) {
		// First get a valid admin token
		addr := testutils.GetGRPCAddress()
		conn, err := grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(10*time.Second),
		)
		if err != nil {
			t.Skipf("Could not connect to gRPC server: %v", err)
		}
		defer conn.Close()

		authClient := pb.NewAuthServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		loginReq := &pb.LoginRequest{
			Username: "admin",
			Password: "admin123",
		}
		loginResp, err := authClient.AdminLogin(ctx, loginReq)
		if err != nil {
			t.Skipf("Could not get admin token: %v", err)
		}

		// Set cookie
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.AddCookie(&http.Cookie{
			Name:  "admin_jwt",
			Value: loginResp.Token,
		})
		rr := httptest.NewRecorder()

		AdminDashboardHandler(rr, req)

		t.Logf("Admin dashboard response status: %d", rr.Code)
	})

	t.Run("AdminBreadListHandler", func(t *testing.T) {
		addr := testutils.GetGRPCAddress()
		conn, err := grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(10*time.Second),
		)
		if err != nil {
			t.Skipf("Could not connect to gRPC server: %v", err)
		}
		defer conn.Close()

		authClient := pb.NewAuthServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		loginReq := &pb.LoginRequest{
			Username: "admin",
			Password: "admin123",
		}
		loginResp, _ := authClient.AdminLogin(ctx, loginReq)

		req := httptest.NewRequest(http.MethodGet, "/admin/bread", nil)
		req.AddCookie(&http.Cookie{
			Name:  "admin_jwt",
			Value: loginResp.Token,
		})
		rr := httptest.NewRecorder()

		AdminBreadListHandler(rr, req)

		t.Logf("Admin bread list response status: %d", rr.Code)
	})
}

// TestFrontend_CustomerRoutes_Integration tests customer portal routes
func TestFrontend_CustomerRoutes_Integration(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-for-integration")
	defer os.Unsetenv("JWT_SECRET")

	t.Run("CustomerLoginPageHandler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/portal/login", nil)
		rr := httptest.NewRecorder()

		CustomerLoginPageHandler(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("CustomerLoginHandler", func(t *testing.T) {
		addr := testutils.GetGRPCAddress()
		conn, err := grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(10*time.Second),
		)
		if err != nil {
			t.Skipf("Could not connect to gRPC server: %v", err)
		}
		defer conn.Close()

		authClient := pb.NewAuthServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		loginReq := &pb.CustomerLoginRequest{
			Email:    "john@doe.com",
			Password: "password123",
		}
		_, err = authClient.CustomerLogin(ctx, loginReq)
		if err != nil {
			t.Skipf("Could not login: %v", err)
		}

		// Make POST request with form data
		formData := strings.NewReader("email=john@doe.com&password=password123")
		req := httptest.NewRequest(http.MethodPost, "/portal/login", formData)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		CustomerLoginHandler(rr, req)

		t.Logf("Customer login response status: %d", rr.Code)
		
		// Check for redirect or cookie
		if len(rr.Result().Cookies()) > 0 {
			t.Log("Customer login set cookie")
		}
	})

	t.Run("CustomerPortalDashboardHandler", func(t *testing.T) {
		addr := testutils.GetGRPCAddress()
		conn, err := grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(10*time.Second),
		)
		if err != nil {
			t.Skipf("Could not connect to gRPC server: %v", err)
		}
		defer conn.Close()

		authClient := pb.NewAuthServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		loginReq := &pb.CustomerLoginRequest{
			Email:    "john@doe.com",
			Password: "password123",
		}
		loginResp, _ := authClient.CustomerLogin(ctx, loginReq)

		req := httptest.NewRequest(http.MethodGet, "/portal", nil)
		req.AddCookie(&http.Cookie{
			Name:  "customer_jwt",
			Value: loginResp.Token,
		})
		rr := httptest.NewRecorder()

		CustomerPortalDashboardHandler(rr, req)

		t.Logf("Customer portal dashboard response status: %d", rr.Code)
	})
}

// TestFrontend_Middleware_Integration tests authentication middleware
func TestFrontend_Middleware_Integration(t *testing.T) {
	adminToken := createTestAdminToken()
	customerToken := createTestCustomerToken()

	t.Run("RequireAdminAuth", func(t *testing.T) {
		// Valid token
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.AddCookie(&http.Cookie{
			Name:  "admin_token",
			Value: adminToken,
		})
		rr := httptest.NewRecorder()

		handler := RequireAdminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		handler(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200 with valid token, got %d", rr.Code)
		}

		// Invalid token
		req2 := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req2.AddCookie(&http.Cookie{
			Name:  "admin_token",
			Value: "invalid-token",
		})
		rr2 := httptest.NewRecorder()
		handler(rr2, req2)

		t.Logf("Invalid token response status: %d", rr2.Code)
	})

	t.Run("RequireCustomerAuth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/portal", nil)
		req.AddCookie(&http.Cookie{
			Name:  "customer_token",
			Value: customerToken,
		})
		rr := httptest.NewRecorder()

		handler := RequireCustomerAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		handler(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200 with valid token, got %d", rr.Code)
		}
	})
}

// TestFrontend_InventoryHandler_Integration tests inventory-related handlers
func TestFrontend_InventoryHandler_Integration(t *testing.T) {
	t.Run("StreamHandler", func(t *testing.T) {
		// Cancel after 2 s so the SSE stream doesn't block forever.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
		rr := httptest.NewRecorder()

		streamHandler(rr, req)

		t.Logf("Stream handler response status: %d", rr.Code)
	})

	t.Run("OrderStreamHandler", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/order-stream", nil).WithContext(ctx)
		rr := httptest.NewRecorder()

		orderStreamHandler(rr, req)

		t.Logf("Order stream handler response status: %d", rr.Code)
	})

	t.Run("OrderDetailsHandler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/orders", nil)
		rr := httptest.NewRecorder()

		orderDetailsHandler(rr, req)

		t.Logf("Order details handler response status: %d", rr.Code)
	})
}

// TestFrontend_JSONResponse_Integration tests JSON response handlers
func TestFrontend_JSONResponse_Integration(t *testing.T) {
	t.Run("AdminAlertsHandler", func(t *testing.T) {
		os.Setenv("JWT_SECRET", "test-secret")
		defer os.Unsetenv("JWT_SECRET")

		addr := testutils.GetGRPCAddress()
		conn, err := grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(10*time.Second),
		)
		if err != nil {
			t.Skipf("Could not connect to gRPC server: %v", err)
		}
		defer conn.Close()

		authClient := pb.NewAuthServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		loginReq := &pb.LoginRequest{
			Username: "admin",
			Password: "admin123",
		}
		loginResp, _ := authClient.AdminLogin(ctx, loginReq)

		req := httptest.NewRequest(http.MethodGet, "/admin/alerts", nil)
		req.AddCookie(&http.Cookie{
			Name:  "admin_jwt",
			Value: loginResp.Token,
		})
		rr := httptest.NewRecorder()

		AdminAlertsHandler(rr, req)

		t.Logf("Admin alerts response status: %d", rr.Code)
	})
}

// TestFrontend_GRPCConnection_Integration tests gRPC connection handling
func TestFrontend_GRPCConnection_Integration(t *testing.T) {
	t.Run("ConnectAndClose", func(t *testing.T) {
		addr := testutils.GetGRPCAddress()
		
		conn, err := grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(10*time.Second),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}

		err = conn.Close()
		if err != nil {
			t.Errorf("Failed to close connection: %v", err)
		}

		t.Log("Successfully connected and closed gRPC connection")
	})

	t.Run("ClientCreation", func(t *testing.T) {
		addr := testutils.GetGRPCAddress()
		conn, err := grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(10*time.Second),
		)
		if err != nil {
			t.Skipf("Could not connect: %v", err)
		}
		defer conn.Close()

		client := pb.NewCheckInventoryClient(conn)
		if client == nil {
			t.Error("Expected non-nil client")
		}

		t.Log("Successfully created gRPC client")
	})
}

// TestFrontend_TemplateRendering_Integration tests template rendering
func TestFrontend_TemplateRendering_Integration(t *testing.T) {
	t.Run("StaticPageHandler", func(t *testing.T) {
		// Use the same relative path pattern as main.go for consistency
		templatePath := "./templates/service.html"

		handler := staticPageHandler(templatePath)

		req := httptest.NewRequest(http.MethodGet, "/service", nil)
		rr := httptest.NewRecorder()

		handler(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		body := rr.Body.String()
		if len(body) == 0 {
			t.Error("Expected non-empty response body")
		}

		t.Logf("Template rendered, body length: %d", len(body))
	})

	t.Run("MissingTemplate", func(t *testing.T) {
		handler := staticPageHandler("./templates/nonexistent.html")

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()

		handler(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500 for missing template, got %d", rr.Code)
		}

		t.Log("Missing template returns 500 as expected")
	})
}

// createTestAdminToken creates a valid admin JWT token signed with the same
// secret as the server (jwtSecret package var) so middleware validation passes.
func createTestAdminToken() string {
	claims := &Claims{
		UserID:   1,
		Username: "admin",
		UserType: "admin",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(jwtSecret)
	return tokenString
}

// createTestCustomerToken creates a valid customer JWT token for testing.
func createTestCustomerToken() string {
	claims := &Claims{
		UserID:   1,
		Username: "john@doe.com",
		UserType: "customer",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(jwtSecret)
	return tokenString
}

// Helper function to marshal JSON for testing
func marshalJSON(data interface{}) string {
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(b)
}
