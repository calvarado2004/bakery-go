package main

import (
	"context"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/golang-jwt/jwt/v5"
	log "github.com/sirupsen/logrus"
)

var jwtSecret = []byte(getJWTSecret())

func getJWTSecret() string {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "bakery-go-secret-key-change-in-production"
	}
	return secret
}

type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	UserType string `json:"user_type"`
	Role     string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

type AuthTemplateData struct {
	Title   string
	Error   string
	Message string
}

// Admin Login Handler
func AdminLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	// Check if already logged in
	cookie, err := r.Cookie("admin_token")
	if err == nil && cookie.Value != "" {
		if validateToken(cookie.Value, "admin") {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
	}

	data := AuthTemplateData{
		Title: "Admin Login",
		Error: r.URL.Query().Get("error"),
	}

	tmpl := template.Must(template.ParseFiles("./cmd/web/templates/admin/login.html"))
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

	conn, err := getGRPCConnection()
	if err != nil {
		http.Redirect(w, r, "/admin/login?error=Server+connection+failed", http.StatusSeeOther)
		return
	}
	defer conn.Close()

	client := pb.NewAuthServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Set cookie
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

// Customer Login Handler
func CustomerLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	// Check if already logged in
	cookie, err := r.Cookie("customer_token")
	if err == nil && cookie.Value != "" {
		if validateToken(cookie.Value, "customer") {
			http.Redirect(w, r, "/portal", http.StatusSeeOther)
			return
		}
	}

	data := AuthTemplateData{
		Title: "Customer Login",
		Error: r.URL.Query().Get("error"),
	}

	tmpl := template.Must(template.ParseFiles("./cmd/web/templates/portal/login.html"))
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

	conn, err := getGRPCConnection()
	if err != nil {
		http.Redirect(w, r, "/portal/login?error=Server+connection+failed", http.StatusSeeOther)
		return
	}
	defer conn.Close()

	client := pb.NewAuthServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Set cookie
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

// Auth Middleware - wraps a handler function to require admin authentication
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

// RequireCustomerAuth - wraps a handler function to require customer authentication
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
		return jwtSecret, nil
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
		return jwtSecret, nil
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
		return jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return 0, "", ""
	}

	return claims.UserID, claims.Username, claims.Role
}

// Customer Portal Handlers
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
	TotalCost      float32
	TotalOrders    int
	TotalInvoices  int
	TotalSpent     float32
	RecentOrders   []*pb.BuyOrder
	RecentInvoices []*pb.Invoice
}

func CustomerPortalDashboardHandler(w http.ResponseWriter, r *http.Request) {
	customerID := getCustomerIDFromToken(r)
	if customerID == 0 {
		http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
		return
	}

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	portalClient := pb.NewCustomerPortalServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		// Calculate total spent and get recent orders (up to 5)
		var totalSpent float32
		for _, order := range ordersResp.Orders {
			totalSpent += order.TotalCost
		}
		data.TotalSpent = totalSpent
		// Recent orders (up to 5)
		if len(ordersResp.Orders) > 5 {
			data.RecentOrders = ordersResp.Orders[:5]
		} else {
			data.RecentOrders = ordersResp.Orders
		}
	}

	if invoicesResp != nil {
		data.Invoices = invoicesResp.Invoices
		data.TotalInvoices = len(invoicesResp.Invoices)
		// Recent invoices (up to 5)
		if len(invoicesResp.Invoices) > 5 {
			data.RecentInvoices = invoicesResp.Invoices[:5]
		} else {
			data.RecentInvoices = invoicesResp.Invoices
		}
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/portal/base.html",
		"./cmd/web/templates/portal/dashboard.html",
	))
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

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	portalClient := pb.NewCustomerPortalServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/portal/base.html",
		"./cmd/web/templates/portal/orders.html",
	))
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

	// Get order ID from URL
	orderIDStr := r.URL.Path[len("/portal/orders/"):]
	orderID, err := strconv.Atoi(orderIDStr)
	if err != nil {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	portalClient := pb.NewCustomerPortalServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	orderResp, err := portalClient.GetOrderDetails(ctx, &pb.BuyOrderIdRequest{Id: int32(orderID)})
	if err != nil {
		log.Errorf("Error getting order details: %v", err)
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}

	// Verify this order belongs to the customer
	if orderResp.Order.CustomerId != int32(customerID) {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	// Get customer name
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

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/portal/base.html",
		"./cmd/web/templates/portal/order_detail.html",
	))
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

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	portalClient := pb.NewCustomerPortalServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	invoicesResp, err := portalClient.GetMyInvoices(ctx, &pb.CustomerIdRequest{Id: int32(customerID)})
	if err != nil {
		log.Errorf("Error getting invoices: %v", err)
		http.Error(w, "Failed to get invoices", http.StatusInternalServerError)
		return
	}

	// Get customer name
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

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/portal/base.html",
		"./cmd/web/templates/portal/invoices.html",
	))
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

	// Get invoice ID from URL
	invoiceIDStr := r.URL.Path[len("/portal/invoices/"):]
	invoiceID, err := strconv.Atoi(invoiceIDStr)
	if err != nil {
		http.Error(w, "Invalid invoice ID", http.StatusBadRequest)
		return
	}

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	invoiceClient := pb.NewInvoiceServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	invoice, err := invoiceClient.GetInvoice(ctx, &pb.InvoiceIdRequest{Id: int32(invoiceID)})
	if err != nil {
		log.Errorf("Error getting invoice: %v", err)
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}

	// Verify this invoice belongs to the customer
	if invoice.CustomerId != int32(customerID) {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	// Get customer name
	portalClient := pb.NewCustomerPortalServiceClient(conn)
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

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/portal/base.html",
		"./cmd/web/templates/portal/invoice_detail.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
