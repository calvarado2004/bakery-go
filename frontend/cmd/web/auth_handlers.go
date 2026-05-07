package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"
)

var _jwtSecret []byte
var _jwtOnce sync.Once

func getJWTSecret() []byte {
	_jwtOnce.Do(func() {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			log.Fatal("JWT_SECRET environment variable is not set")
		}
		_jwtSecret = []byte(secret)
	})
	return _jwtSecret
}

type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	UserType string `json:"user_type"`
	Role     string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

type AuthTemplateData struct {
	Title     string
	Error     string
	Message   string
	CSRFToken string
}

// adminGRPCContext returns a context with the authenticated admin's token
// attached as gRPC outgoing metadata. Use this context for any gRPC call that
// requires admin authentication on the server.
func adminGRPCContext(r *http.Request) (context.Context, error) {
	cookie, err := r.Cookie("admin_token")
	if err != nil {
		return nil, fmt.Errorf("admin_token cookie not present: %w", err)
	}
	if cookie.Value == "" {
		return nil, fmt.Errorf("admin_token cookie is empty")
	}
	md := metadata.Pairs("authorization", "Bearer "+cookie.Value)
	return metadata.NewOutgoingContext(r.Context(), md), nil
}

// adminGRPCContextWithTimeout is a convenience that wraps adminGRPCContext
// with a timeout. Returns (context, cancel, error) ready for immediate use.
func adminGRPCContextWithTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	grpcCtx, err := adminGRPCContext(r)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(grpcCtx, timeout)
	return ctx, cancel, nil
}

// ── Admin Login Handlers ──

func AdminLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("admin_token")
	if err == nil && cookie.Value != "" {
		if validateToken(cookie.Value, "admin") {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
	}

	data := AuthTemplateData{
		Title:     "Admin Login",
		Error:     r.URL.Query().Get("error"),
		CSRFToken: csrf.Token(r),
	}

	tmpl, ok := templates["admin/login.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminLoginHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Redirect(w, r, "/admin/login?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	client := pb.NewAuthServiceClient(sharedGRPCConn)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := client.AdminLogin(ctx, &pb.LoginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		log.Errorf("Login error: %v", err)
		http.Redirect(w, r, "/admin/login?error=Login+failed", http.StatusSeeOther)
		return
	}

	if !response.Success {
		http.Redirect(w, r, "/admin/login?error="+response.Message, http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    response.Token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400, // 24 hours
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func AdminLogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ── Customer Login Handlers ──

func CustomerLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("customer_token")
	if err == nil && cookie.Value != "" {
		if validateToken(cookie.Value, "customer") {
			http.Redirect(w, r, "/portal", http.StatusSeeOther)
			return
		}
	}

	data := AuthTemplateData{
		Title:     "Customer Login",
		Error:     r.URL.Query().Get("error"),
		CSRFToken: csrf.Token(r),
	}

	tmpl, ok := templates["portal/login.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func CustomerLoginHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Redirect(w, r, "/portal/login?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	client := pb.NewAuthServiceClient(sharedGRPCConn)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := client.CustomerLogin(ctx, &pb.CustomerLoginRequest{
		Email:    email,
		Password: password,
	})
	if err != nil {
		log.Errorf("Login error: %v", err)
		http.Redirect(w, r, "/portal/login?error=Login+failed", http.StatusSeeOther)
		return
	}

	if !response.Success {
		http.Redirect(w, r, "/portal/login?error="+response.Message, http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "customer_token",
		Value:    response.Token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400, // 24 hours
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/portal", http.StatusSeeOther)
}

func CustomerLogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "customer_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
}

// ── Auth Middleware ──

func RequireAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("admin_token")
		if err != nil || cookie.Value == "" {
			log.Infof("Admin auth failed: no token cookie for path %s", r.URL.Path)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		if !validateToken(cookie.Value, "admin") {
			log.Infof("Admin auth failed: invalid token for path %s", r.URL.Path)
			http.Redirect(w, r, "/admin/login?error=Session+expired", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func RequireCustomerAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("customer_token")
		if err != nil || cookie.Value == "" {
			log.Infof("Customer auth failed: no token cookie for path %s", r.URL.Path)
			http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
			return
		}

		if !validateToken(cookie.Value, "customer") {
			log.Infof("Customer auth failed: invalid token for path %s", r.URL.Path)
			http.Redirect(w, r, "/portal/login?error=Session+expired", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func validateToken(tokenString string, expectedType string) bool {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	})

	if err != nil || !token.Valid {
		return false
	}

	if expectedType != "" && claims.UserType != expectedType {
		return false
	}

	return true
}

func getCustomerIDFromToken(r *http.Request) int {
	cookie, err := r.Cookie("customer_token")
	if err != nil {
		return 0
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(cookie.Value, claims, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	})

	if err != nil || !token.Valid {
		return 0
	}

	return claims.UserID
}

func getAdminUserFromToken(r *http.Request) (int, string, string) {
	cookie, err := r.Cookie("admin_token")
	if err != nil {
		return 0, "", ""
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(cookie.Value, claims, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	})

	if err != nil || !token.Valid {
		return 0, "", ""
	}

	return claims.UserID, claims.Username, claims.Role
}

// customerGRPCContext returns a context with the authenticated customer's
// ID and Bearer token attached as gRPC outgoing metadata. This allows the
// server to validate the JWT (authorization) and extract the customer identity
// (customer_id) for order attribution.
func customerGRPCContext(r *http.Request) context.Context {
	customerID := getCustomerIDFromToken(r)
	cookie, err := r.Cookie("customer_token")
	hasCookie := err == nil && cookie.Value != ""

	if customerID == 0 && !hasCookie {
		return r.Context()
	}

	var md metadata.MD
	if hasCookie {
		md = metadata.Pairs("authorization", "Bearer "+cookie.Value)
	}
	if customerID > 0 {
		if md == nil {
			md = metadata.Pairs("customer_id", strconv.Itoa(customerID))
		} else {
			md = metadata.Pairs("authorization", "Bearer "+cookie.Value, "customer_id", strconv.Itoa(customerID))
		}
	}
	return metadata.NewOutgoingContext(r.Context(), md)
}

// customerGRPCContextWithTimeout is a convenience that wraps customerGRPCContext
// with a timeout. Returns (context, cancel, error) ready for immediate use.
func customerGRPCContextWithTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	grpcCtx := customerGRPCContext(r)
	ctx, cancel := context.WithTimeout(grpcCtx, timeout)
	return ctx, cancel, nil
}

// ── Customer Portal Handlers ──

type PortalTemplateData struct {
	Title          string
	ActivePage     string
	CustomerName   string
	CustomerID     int32
	Customer       *pb.Customer
	Orders         []*pb.BuyOrder
	Order          *pb.BuyOrder
	Details        []*pb.BuyOrderDetails
	Invoices       []*pb.Invoice
	Invoice        *pb.Invoice
	Message        string
	Error          string
	TotalCost      float64
	TotalOrders    int
	TotalInvoices  int
	TotalSpent     float64
	RecentOrders   []*pb.BuyOrder
	RecentInvoices []*pb.Invoice
}

func CustomerPortalDashboardHandler(w http.ResponseWriter, r *http.Request) {
	customerID := getCustomerIDFromToken(r)
	if customerID == 0 {
		http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
		return
	}

	portalClient := pb.NewCustomerPortalServiceClient(sharedGRPCConn)
	ctx := customerGRPCContext(r)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ordersResp, err := portalClient.GetMyOrders(ctx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil {
		log.Errorf("Error getting orders: %v", err)
	}

	invoicesResp, err := portalClient.GetMyInvoices(ctx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil {
		log.Errorf("Error getting invoices: %v", err)
	}

	data := PortalTemplateData{
		Title:      "My Dashboard",
		ActivePage: "dashboard",
	}

	if ordersResp != nil {
		data.Customer = ordersResp.Customer
		data.Orders = ordersResp.Orders
		data.TotalOrders = len(ordersResp.Orders)
		if ordersResp.Customer != nil {
			data.CustomerName = ordersResp.Customer.Name
			data.CustomerID = ordersResp.Customer.Id
		}
		var totalSpent float64
		for _, order := range ordersResp.Orders {
			totalSpent += order.TotalCost
		}
		data.TotalSpent = totalSpent
		if len(ordersResp.Orders) > 5 {
			data.RecentOrders = ordersResp.Orders[:5]
		} else {
			data.RecentOrders = ordersResp.Orders
		}
	}

	if invoicesResp != nil {
		data.Invoices = invoicesResp.Invoices
		data.TotalInvoices = len(invoicesResp.Invoices)
		if len(invoicesResp.Invoices) > 5 {
			data.RecentInvoices = invoicesResp.Invoices[:5]
		} else {
			data.RecentInvoices = invoicesResp.Invoices
		}
	}

	tmpl, ok := templates["portal/base.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func CustomerOrdersHandler(w http.ResponseWriter, r *http.Request) {
	customerID := getCustomerIDFromToken(r)
	if customerID == 0 {
		http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
		return
	}

	portalClient := pb.NewCustomerPortalServiceClient(sharedGRPCConn)
	ctx := customerGRPCContext(r)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ordersResp, err := portalClient.GetMyOrders(ctx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil {
		log.Errorf("Error getting orders: %v", err)
		http.Error(w, "Failed to get orders", http.StatusInternalServerError)
		return
	}

	data := PortalTemplateData{
		Title:      "My Orders",
		ActivePage: "orders",
		Customer:   ordersResp.Customer,
		Orders:     ordersResp.Orders,
	}
	if ordersResp.Customer != nil {
		data.CustomerName = ordersResp.Customer.Name
	}

	tmpl, ok := templates["portal/orders.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func CustomerOrderDetailHandler(w http.ResponseWriter, r *http.Request) {
	customerID := getCustomerIDFromToken(r)
	if customerID == 0 {
		http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	orderID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}

	portalClient := pb.NewCustomerPortalServiceClient(sharedGRPCConn)
	ctx := customerGRPCContext(r)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	orderResp, err := portalClient.GetOrderDetails(ctx, &pb.BuyOrderIdRequest{Id: int32(orderID)})
	if err != nil {
		log.Errorf("Error getting order details: %v", err)
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}

	if orderResp.Order.CustomerId != int32(customerID) {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	ordersResp, _ := portalClient.GetMyOrders(ctx, &pb.CustomerIdRequest{Id: int32(customerID)})
	customerName := ""
	if ordersResp != nil && ordersResp.Customer != nil {
		customerName = ordersResp.Customer.Name
	}

	data := PortalTemplateData{
		Title:        "Order Details",
		ActivePage:   "orders",
		CustomerName: customerName,
		Order:        orderResp.Order,
		Details:      orderResp.Details,
		TotalCost:    orderResp.TotalCost,
	}

	tmpl, ok := templates["portal/order_detail.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func CustomerInvoicesHandler(w http.ResponseWriter, r *http.Request) {
	customerID := getCustomerIDFromToken(r)
	if customerID == 0 {
		http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
		return
	}

	portalClient := pb.NewCustomerPortalServiceClient(sharedGRPCConn)
	ctx := customerGRPCContext(r)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	invoicesResp, err := portalClient.GetMyInvoices(ctx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil {
		log.Errorf("Error getting invoices: %v", err)
		http.Error(w, "Failed to get invoices", http.StatusInternalServerError)
		return
	}

	ordersResp, _ := portalClient.GetMyOrders(ctx, &pb.CustomerIdRequest{Id: int32(customerID)})
	customerName := ""
	if ordersResp != nil && ordersResp.Customer != nil {
		customerName = ordersResp.Customer.Name
	}

	data := PortalTemplateData{
		Title:        "My Invoices",
		ActivePage:   "invoices",
		CustomerName: customerName,
		Invoices:     invoicesResp.Invoices,
	}

	tmpl, ok := templates["portal/invoices.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func CustomerInvoiceDetailHandler(w http.ResponseWriter, r *http.Request) {
	customerID := getCustomerIDFromToken(r)
	if customerID == 0 {
		http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	invoiceID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid invoice ID", http.StatusBadRequest)
		return
	}

	invoiceClient := pb.NewInvoiceServiceClient(sharedGRPCConn)
	ctx := customerGRPCContext(r)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	invoice, err := invoiceClient.GetInvoice(ctx, &pb.InvoiceIdRequest{Id: int32(invoiceID)})
	if err != nil {
		log.Errorf("Error getting invoice: %v", err)
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}

	if invoice.CustomerId != int32(customerID) {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	portalClient := pb.NewCustomerPortalServiceClient(sharedGRPCConn)
	ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ordersResp, _ := portalClient.GetMyOrders(ctx, &pb.CustomerIdRequest{Id: int32(customerID)})
	customerName := ""
	if ordersResp != nil && ordersResp.Customer != nil {
		customerName = ordersResp.Customer.Name
	}

	data := PortalTemplateData{
		Title:        "Invoice Details",
		ActivePage:   "invoices",
		CustomerName: customerName,
		Invoice:      invoice,
	}

	tmpl, ok := templates["portal/invoice_detail.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
