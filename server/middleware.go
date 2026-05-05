package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/calvarado2004/bakery-go/pkg/resilience"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	log "github.com/sirupsen/logrus"
)

// ---------------------------------------------------------------------------
// Rate limiter (per-identity token bucket, shared with broker via pkg/resilience)
// ---------------------------------------------------------------------------

var globalRateLimiter = resilience.NewRateLimiter(10, 20) // 10 req/s, burst 20

// identity extracts a stable client identity from gRPC metadata.
func identityFromMetadata(md metadata.MD) string {
	if ids := md.Get("customer_id"); len(ids) > 0 && ids[0] != "" {
		return "cid:" + ids[0]
	}
	if ips := md.Get("x-forwarded-for"); len(ips) > 0 {
		return "ip:" + ips[0]
	}
	return "ip:unknown"
}

// ---------------------------------------------------------------------------
// Role-based access control
// ---------------------------------------------------------------------------

type roleKeyType struct{}

var roleKey roleKeyType

type minRole string

const (
	RoleCustomer minRole = "customer"
	RoleAdmin    minRole = "admin"
)

// methodRoles maps gRPC method names to their required minimum role.
var methodRoles = map[string]minRole{
	// ---- Admin-only endpoints ----
	"/bread.AdminService/GetDashboardStats":     RoleAdmin,
	"/bread.AdminService/GetAllCustomers":       RoleAdmin,
	"/bread.AdminService/GetAllBreadMakers":     RoleAdmin,
	"/bread.AdminService/GetAllBread":           RoleAdmin,
	"/bread.AdminService/GetBreadById":          RoleAdmin,
	"/bread.AdminService/CreateBread":           RoleAdmin,
	"/bread.AdminService/UpdateBread":           RoleAdmin,
	"/bread.AdminService/DeleteBread":           RoleAdmin,
	"/bread.AdminService/GetLowStockAlerts":     RoleAdmin,
	"/bread.AdminService/UpdateOrderStatus":     RoleAdmin,
	"/bread.AdminService/GetCustomerOrders":     RoleAdmin,
	"/bread.AdminService/GetMakerOrders":        RoleAdmin,
	"/bread.AdminService/GetAllOrders":          RoleAdmin,
	"/bread.AdminService/GetAllMakeOrders":      RoleAdmin,

	// ---- BrokerService (internal, no auth — broker is trusted) ----
	"/bread.BrokerService/ReportOrder":           "",
	"/bread.BrokerService/ReserveInventory":      "",
	"/bread.BrokerService/ReportMatchingResults": "",

	// ---- BuyBread (authenticated customer) ----
	"/bread.BuyBread/BuyBread":       "",
	"/bread.BuyBread/BuyBreadStream": "",

	// ---- BuyOrderService (authenticated customer) ----
	"/bread.BuyOrderService/BuyOrder":       RoleCustomer,
	"/bread.BuyOrderService/BuyOrderStream": RoleCustomer,

	// ---- CustomerPortal (authenticated customer) ----
	"/bread.CustomerPortalService/GetMyOrders":     RoleCustomer,
	"/bread.CustomerPortalService/GetMyInvoices":   RoleCustomer,
	"/bread.CustomerPortalService/GetOrderDetails": RoleCustomer,

	// ---- Open endpoints (no auth required) ----
	"/bread.CheckInventory/CheckBreadInventory":       "",
	"/bread.CheckInventory/CheckBreadInventoryStream": "",
	"/bread.MakeBread/BakeBread":                      "",
	"/bread.MakeBread/SendBreadToBakery":              "",
	"/bread.RemoveOldBread/RemoveBread":               "",
	"/bread.RemoveOldBread/RemoveBreadStream":         "",
	"/bread.AuthService/AdminLogin":                   "",
	"/bread.AuthService/CustomerLogin":                "",
	"/bread.AuthService/ValidateToken":                "",

	// ---- InvoiceService ----
	"/bread.InvoiceService/GetInvoice":             "",
	"/bread.InvoiceService/GetCustomerInvoices":    RoleCustomer,
	"/bread.InvoiceService/GetAllInvoices":         RoleAdmin,
	"/bread.InvoiceService/CreateInvoice":          RoleAdmin,
}

// getMethodRole looks up the required role for a gRPC method.
func getMethodRole(fullMethod string) minRole {
	if r, ok := methodRoles[fullMethod]; ok {
		return r
	}
	// Default: require customer auth for unknown endpoints (fail-safe).
	return RoleCustomer
}

// rbacCheck validates the JWT and checks the caller's role.
func rbacCheck(ctx context.Context, fullMethod string) (context.Context, error) {
	requiredRole := getMethodRole(fullMethod)
	if requiredRole == "" {
		return ctx, nil
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing request metadata")
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "authorization token is required")
	}

	tokenString := values[0]
	if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
		tokenString = tokenString[7:]
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return getJWTSecret(), nil
	})
	if err != nil || !token.Valid {
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}

	if claims.UserType != string(requiredRole) && claims.UserType != "admin" {
		return nil, status.Errorf(codes.PermissionDenied,
			"role %q required, got %q", requiredRole, claims.UserType)
	}

	return context.WithValue(ctx, roleKey, claims), nil
}

// GetClaimsFromContext extracts JWT claims from the gRPC context.
func GetClaimsFromContext(ctx context.Context) *Claims {
	if v, ok := ctx.Value(roleKey).(*Claims); ok {
		return v
	}
	return nil
}

// ---------------------------------------------------------------------------
// Server startup helpers
// ---------------------------------------------------------------------------

// BuildGRPCKeepaliveOptions returns gRPC server options for connection
// lifecycle management (keepalive, max message size, etc.).
func BuildGRPCKeepaliveOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:             30 * time.Second,
			Timeout:          10 * time.Second,
			MaxConnectionAge: 5 * time.Minute,
			MaxConnectionIdle: 2 * time.Minute,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             15 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(4 * 1024 * 1024), // 4 MB max message
		grpc.MaxSendMsgSize(4 * 1024 * 1024),
	}
}

// BuildInterceptorChain constructs the interceptor chain for the gRPC server.
// Order (outer to inner): rate limit → RBAC → customer ID extraction.
func BuildInterceptorChain() grpc.ServerOption {
	return grpc.UnaryInterceptor(
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			// 1. Rate limiting (outermost — rejects before auth cost).
			md, _ := metadata.FromIncomingContext(ctx)
			id := identityFromMetadata(md)
			if !globalRateLimiter.Allow(id) {
				log.Warnf("rate limit exceeded for %q (method %s)", id, info.FullMethod)
				return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
			}

			// 2. RBAC (validates JWT, checks role).
			ctx, err := rbacCheck(ctx, info.FullMethod)
			if err != nil {
				return nil, err
			}

			// 3. Customer ID injection (backward-compat).
			ctx = injectCustomerID(ctx)

			// 4. Handler.
			return handler(ctx, req)
		},
	)
}

// injectCustomerID reads customer_id from metadata and injects it into the
// gRPC context (existing behavior, preserved for backward compatibility).
func injectCustomerID(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	ids := md.Get("customer_id")
	if len(ids) == 0 || ids[0] == "" {
		return ctx
	}

	customerID := ids[0]
	valid := len(customerID) > 0
	if valid {
		for _, c := range customerID {
			if c < '0' || c > '9' {
				valid = false
				break
			}
		}
	}
	if valid {
		return context.WithValue(ctx, customerIDKey, customerID)
	}
	return ctx
}

// ---------------------------------------------------------------------------
// Per-endpoint circuit breakers for broker→server gRPC calls
// ---------------------------------------------------------------------------

// brokerBreakers holds the per-endpoint circuit breakers used by the broker
// service when calling the server's BrokerService gRPC.
var brokerBreakers = struct {
	sync.RWMutex
	reportOrder      *resilience.CircuitBreaker
	reserveInventory *resilience.CircuitBreaker
	reportMatching   *resilience.CircuitBreaker
}{
	reportOrder:      resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 5, ResetTimeout: 30 * time.Second}),
	reserveInventory: resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 5, ResetTimeout: 30 * time.Second}),
	reportMatching:   resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 3, ResetTimeout: 60 * time.Second}),
}

// getReportOrderBreaker returns the circuit breaker for ReportOrder calls.
func getReportOrderBreaker() *resilience.CircuitBreaker {
	brokerBreakers.RLock()
	defer brokerBreakers.RUnlock()
	return brokerBreakers.reportOrder
}

// getReserveInventoryBreaker returns the circuit breaker for ReserveInventory calls.
func getReserveInventoryBreaker() *resilience.CircuitBreaker {
	brokerBreakers.RLock()
	defer brokerBreakers.RUnlock()
	return brokerBreakers.reserveInventory
}

// getReportMatchingBreaker returns the circuit breaker for ReportMatchingResults calls.
func getReportMatchingBreaker() *resilience.CircuitBreaker {
	brokerBreakers.RLock()
	defer brokerBreakers.RUnlock()
	return brokerBreakers.reportMatching
}

// allBrokerBreakers returns all broker circuit breakers for logging.
func allBrokerBreakers() map[string]*resilience.CircuitBreaker {
	brokerBreakers.RLock()
	defer brokerBreakers.RUnlock()
	return map[string]*resilience.CircuitBreaker{
		"report_order":      brokerBreakers.reportOrder,
		"reserve_inventory": brokerBreakers.reserveInventory,
		"report_matching":   brokerBreakers.reportMatching,
	}
}

// LogCircuitStates logs all circuit breaker states at a fixed interval.
// Call from a background goroutine.
func LogCircuitStates(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for name, cb := range allBrokerBreakers() {
				if state := cb.State(); state != resilience.StateClosed {
					log.WithField("breaker", name).
						WithField("state", state).
						Warn("circuit-breaker non-closed state")
				}
			}
		}
	}
}
