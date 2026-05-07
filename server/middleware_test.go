package main

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

// TestIdentityFromMetadata_CustomerID verifies that identity extraction
// prefers customer_id metadata over IP.
func TestIdentityFromMetadata_CustomerID(t *testing.T) {
	md := metadata.Pairs("customer_id", "42")
	id := identityFromMetadata(md)
	if id != "cid:42" {
		t.Errorf("expected 'cid:42', got %q", id)
	}
}

// TestIdentityFromMetadata_IPFallback verifies that when no customer_id is
// present, the identity falls back to x-forwarded-for IP.
func TestIdentityFromMetadata_IPFallback(t *testing.T) {
	md := metadata.Pairs("x-forwarded-for", "192.168.1.1")
	id := identityFromMetadata(md)
	if id != "ip:192.168.1.1" {
		t.Errorf("expected 'ip:192.168.1.1', got %q", id)
	}
}

// TestIdentityFromMetadata_NoIdentity verifies that with no relevant metadata,
// the identity is "ip:unknown".
func TestIdentityFromMetadata_NoIdentity(t *testing.T) {
	id := identityFromMetadata(metadata.MD{})
	if id != "ip:unknown" {
		t.Errorf("expected 'ip:unknown', got %q", id)
	}
}

// TestIdentityFromMetadata_EmptyCustomerID verifies that empty customer_id
// is ignored, falling through to x-forwarded-for.
func TestIdentityFromMetadata_EmptyCustomerID(t *testing.T) {
	md := metadata.Pairs("customer_id", "", "x-forwarded-for", "10.0.0.1")
	id := identityFromMetadata(md)
	if id != "ip:10.0.0.1" {
		t.Errorf("expected 'ip:10.0.0.1', got %q", id)
	}
}

// TestIdentityFromMetadata_OnlyEmptyCustomerID verifies that when only
// customer_id is present but empty, it falls through.
func TestIdentityFromMetadata_OnlyEmptyCustomerID(t *testing.T) {
	md := metadata.Pairs("customer_id", "")
	id := identityFromMetadata(md)
	if id != "ip:unknown" {
		t.Errorf("expected 'ip:unknown', got %q", id)
	}
}

// TestGetMethodRole_AdminEndpoints verifies that admin endpoints require RoleAdmin.
func TestGetMethodRole_AdminEndpoints(t *testing.T) {
	adminMethods := []string{
		"/bread.AdminService/GetDashboardStats",
		"/bread.AdminService/GetAllCustomers",
		"/bread.AdminService/GetAllBreadMakers",
		"/bread.AdminService/GetAllBread",
		"/bread.AdminService/GetBreadById",
		"/bread.AdminService/CreateBread",
		"/bread.AdminService/UpdateBread",
		"/bread.AdminService/DeleteBread",
		"/bread.AdminService/GetLowStockAlerts",
		"/bread.AdminService/UpdateOrderStatus",
		"/bread.AdminService/GetCustomerOrders",
		"/bread.AdminService/GetMakerOrders",
		"/bread.AdminService/GetAllOrders",
		"/bread.AdminService/GetAllMakeOrders",
	}
	for _, m := range adminMethods {
		role := getMethodRole(m)
		if role != RoleAdmin {
			t.Errorf("method %q: expected RoleAdmin, got %q", m, role)
		}
	}
}

// TestGetMethodRole_CustomerEndpoints verifies that customer endpoints require RoleCustomer.
func TestGetMethodRole_CustomerEndpoints(t *testing.T) {
	customerMethods := []string{
		"/bread.BuyOrderService/BuyOrder",
		"/bread.BuyOrderService/BuyOrderStream",
		"/bread.CustomerPortalService/GetMyOrders",
		"/bread.CustomerPortalService/GetMyInvoices",
		"/bread.CustomerPortalService/GetOrderDetails",
	}
	for _, m := range customerMethods {
		role := getMethodRole(m)
		if role != RoleCustomer {
			t.Errorf("method %q: expected RoleCustomer, got %q", m, role)
		}
	}
}

// TestGetMethodRole_OpenEndpoints verifies that open endpoints require no auth.
func TestGetMethodRole_OpenEndpoints(t *testing.T) {
	openMethods := []string{
		"/bread.CheckInventory/CheckBreadInventory",
		"/bread.CheckInventory/CheckBreadInventoryStream",
		"/bread.MakeBread/BakeBread",
		"/bread.MakeBread/SendBreadToBakery",
		"/bread.RemoveOldBread/RemoveBread",
		"/bread.RemoveOldBread/RemoveBreadStream",
		"/bread.AuthService/AdminLogin",
		"/bread.AuthService/CustomerLogin",
		"/bread.AuthService/ValidateToken",
		"/bread.BuyBread/BuyBread",
		"/bread.BuyBread/BuyBreadStream",
		"/bread.BrokerService/ReportOrder",
		"/bread.BrokerService/ReserveInventory",
		"/bread.BrokerService/ReportMatchingResults",
	}
	for _, m := range openMethods {
		role := getMethodRole(m)
		if role != "" {
			t.Errorf("method %q: expected no auth (empty), got %q", m, role)
		}
	}
}

// TestGetMethodRole_InvoiceEndpoints verifies InvoiceService role requirements.
func TestGetMethodRole_InvoiceEndpoints(t *testing.T) {
	tests := []struct {
		method string
		role   minRole
	}{
		{"/bread.InvoiceService/GetInvoice", ""},
		{"/bread.InvoiceService/GetCustomerInvoices", RoleCustomer},
		{"/bread.InvoiceService/GetAllInvoices", RoleAdmin},
		{"/bread.InvoiceService/CreateInvoice", RoleAdmin},
	}
	for _, tc := range tests {
		got := getMethodRole(tc.method)
		if got != tc.role {
			t.Errorf("method %q: expected %q, got %q", tc.method, tc.role, got)
		}
	}
}

// TestGetMethodRole_DefaultFallback verifies that unknown methods default to
// RoleCustomer (fail-safe default).
func TestGetMethodRole_DefaultFallback(t *testing.T) {
	role := getMethodRole("/bread.UnknownService/SomeMethod")
	if role != RoleCustomer {
		t.Errorf("expected default RoleCustomer for unknown method, got %q", role)
	}
}

// TestGetMethodRole_BrokerServiceInternal verifies broker service endpoints
// are open (no auth required since broker is trusted).
func TestGetMethodRole_BrokerServiceInternal(t *testing.T) {
	brokerMethods := []string{
		"/bread.BrokerService/ReportOrder",
		"/bread.BrokerService/ReserveInventory",
		"/bread.BrokerService/ReportMatchingResults",
	}
	for _, m := range brokerMethods {
		role := getMethodRole(m)
		if role != "" {
			t.Errorf("method %q: broker service should be open, got %q", m, role)
		}
	}
}

// TestInjectCustomerID_ValidID verifies that valid customer_id metadata is
// injected into the context.
func TestInjectCustomerID_ValidID(t *testing.T) {
	md := metadata.Pairs("customer_id", "42")
	ctx := context.Background()
	ctx = metadata.NewIncomingContext(ctx, md)

	ctx = injectCustomerID(ctx)

	val := ctx.Value(customerIDKey)
	if val == nil {
		t.Fatal("expected customer_id in context")
	}
	if val.(string) != "42" {
		t.Errorf("expected '42', got %q", val)
	}
}

// TestInjectCustomerID_InvalidID verifies that non-numeric customer_id is
// rejected and not injected.
func TestInjectCustomerID_InvalidID(t *testing.T) {
	md := metadata.Pairs("customer_id", "abc123")
	ctx := context.Background()
	ctx = metadata.NewIncomingContext(ctx, md)

	ctx = injectCustomerID(ctx)

	if ctx.Value(customerIDKey) != nil {
		t.Error("expected non-numeric customer_id to be rejected")
	}
}

// TestInjectCustomerID_EmptyID verifies that empty customer_id is rejected.
func TestInjectCustomerID_EmptyID(t *testing.T) {
	md := metadata.Pairs("customer_id", "")
	ctx := context.Background()
	ctx = metadata.NewIncomingContext(ctx, md)

	ctx = injectCustomerID(ctx)

	if ctx.Value(customerIDKey) != nil {
		t.Error("expected empty customer_id to be rejected")
	}
}

// TestInjectCustomerID_NoMetadata verifies that when customer_id is absent,
// the context is returned unchanged.
func TestInjectCustomerID_NoMetadata(t *testing.T) {
	ctx := context.Background()
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{})

	ctx = injectCustomerID(ctx)

	if ctx.Value(customerIDKey) != nil {
		t.Error("expected no customer_id when metadata absent")
	}
}

// TestInjectCustomerID_NumericEdgeCases verifies edge cases of numeric validation.
func TestInjectCustomerID_NumericEdgeCases(t *testing.T) {
	tests := []struct {
		input   string
		wantOK  bool
	}{
		{"0", true},
		{"123456789", true},
		{"00", true},
		{"0a", false},
		{"123abc", false},
		{"abc123", false},
	}
	for _, tc := range tests {
		md := metadata.Pairs("customer_id", tc.input)
		ctx := metadata.NewIncomingContext(context.Background(), md)
		ctx = injectCustomerID(ctx)

		hasValue := ctx.Value(customerIDKey) != nil
		if hasValue != tc.wantOK {
			t.Errorf("input %q: expected hasValue=%v, got %v", tc.input, tc.wantOK, hasValue)
		}
	}
}

// TestGetCustomerIDFromContext_ValidID verifies extraction of a valid customer ID.
func TestGetCustomerIDFromContext_ValidID(t *testing.T) {
	ctx := context.WithValue(context.Background(), customerIDKey, "42")
	id := GetCustomerIDFromContext(ctx)
	if id != 42 {
		t.Errorf("expected id=42, got %d", id)
	}
}

// TestGetCustomerIDFromContext_NonNumericID verifies that non-numeric IDs return 0.
func TestGetCustomerIDFromContext_NonNumericID(t *testing.T) {
	ctx := context.WithValue(context.Background(), customerIDKey, "abc")
	id := GetCustomerIDFromContext(ctx)
	if id != 0 {
		t.Errorf("expected id=0 for non-numeric, got %d", id)
	}
}

// TestGetCustomerIDFromContext_AbsentKey verifies that absent context returns 0.
func TestGetCustomerIDFromContext_AbsentKey(t *testing.T) {
	ctx := context.Background()
	id := GetCustomerIDFromContext(ctx)
	if id != 0 {
		t.Errorf("expected id=0 for absent key, got %d", id)
	}
}

// TestGetCustomerIDFromContext_WrongType verifies that a non-string value
// in the context returns 0.
func TestGetCustomerIDFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), customerIDKey, 123) // int, not string
	id := GetCustomerIDFromContext(ctx)
	if id != 0 {
		t.Errorf("expected id=0 for wrong type, got %d", id)
	}
}

// TestGetCustomerIDFromContext_Zero verifies that "0" parses as integer 0.
func TestGetCustomerIDFromContext_Zero(t *testing.T) {
	ctx := context.WithValue(context.Background(), customerIDKey, "0")
	id := GetCustomerIDFromContext(ctx)
	if id != 0 {
		t.Errorf("expected id=0 for '0', got %d", id)
	}
}

// TestGetCustomerIDFromContext_LargeNumber verifies large numeric IDs work.
func TestGetCustomerIDFromContext_LargeNumber(t *testing.T) {
	ctx := context.WithValue(context.Background(), customerIDKey, "999999999")
	id := GetCustomerIDFromContext(ctx)
	if id != 999999999 {
		t.Errorf("expected id=999999999, got %d", id)
	}
}

// TestBuildGRPCKeepaliveOptions verifies the keepalive options are returned.
func TestBuildGRPCKeepaliveOptions(t *testing.T) {
	opts := BuildGRPCKeepaliveOptions()
	if len(opts) == 0 {
		t.Fatal("expected non-empty keepalive options")
	}
}

// TestBuildInterceptorChain verifies the interceptor chain returns options.
func TestBuildInterceptorChain(t *testing.T) {
	opts := BuildInterceptorChain()
	// opts should not panic and should be a valid ServerOption.
	if opts == nil {
		t.Fatal("expected non-nil interceptor chain")
	}
}
