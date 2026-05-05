package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// sharedGRPCConn is the package-level shared gRPC connection used by all handlers.
// It is initialized once at startup with proper keep-alive parameters to prevent
// connection exhaustion under load (ARCHITECTURE_AUDIT §6.1).
var sharedGRPCConn *grpc.ClientConn
var sharedGRPCOnce sync.Once

// templates holds all pre-parsed templates keyed by their logical page name.
// Templates are parsed once at startup to avoid per-request parsing overhead
// (ARCHITECTURE_AUDIT §6.3).
var templates map[string]*template.Template

// getSharedGRPCClient returns the shared gRPC connection initialized at startup.
func getSharedGRPCClient() pb.AdminServiceClient {
	return pb.NewAdminServiceClient(sharedGRPCConn)
}

// SetSharedGRPCConn allows tests to inject a gRPC connection.
func SetSharedGRPCConn(conn *grpc.ClientConn) {
	sharedGRPCConn = conn
}

type BreadLog struct {
	ID       int
	Name     string
	Message  string
	Buyer    string
	Maker    string
	Quantity int
	Price    float64
	Image    string
}

type BuyOrder struct {
	ID           int     `json:"id"`
	CustomerID   int     `json:"customerId"`
	BuyOrderUuid string  `json:"buyOrderUuid"`
	TotalCost    float64 `json:"totalCost"`
}

type BuyOrderDetail struct {
	BuyOrderID   int       `json:"buyOrderId"`
	BuyOrderUuid string    `json:"buyOrderUuid"`
	BreadID      int       `json:"breadId"`
	Quantity     int       `json:"quantity"`
	Price        float64   `json:"price"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type OrderData struct {
	BuyOrders       []*pb.BuyOrder   `json:"buyOrders"`
	BuyOrderDetails []BuyOrderDetail `json:"buyOrderDetails"`
}

var gRPCAddress = func() string {
	if addr := os.Getenv("BAKERY_SERVICE_ADDR"); addr != "" {
		return addr
	}
	return "localhost:50051"
}()

// getTemplatePath returns the correct template path whether running from project root
// or from the package directory (frontend/cmd/web)
func getTemplatePath(relativePath string) string {
	// First try the relative path as-is (for tests running from package dir)
	if _, err := os.Stat(relativePath); err == nil {
		return relativePath
	}
	// Try from package directory context (./templates/... when in frontend/cmd/web)
	// Convert ./cmd/web/templates/... to ./templates/...
	trimmedPath := relativePath
	if strings.HasPrefix(trimmedPath, "./cmd/web/") {
		trimmedPath = "./" + strings.TrimPrefix(trimmedPath, "./cmd/web/")
	}
	if _, err := os.Stat(trimmedPath); err == nil {
		return trimmedPath
	}
	// Try from project root (./cmd/web/templates/...)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(".")))
	return filepath.Join(projectRoot, relativePath)
}

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// ── Phase 5.1+5.2: Pre-parse templates and initialize shared gRPC connection ──
	initTemplates()
	initSharedGRPCConnection()

	router := mux.NewRouter()
	router.StrictSlash(true)

	// CSRF protection middleware
	csrfKey := os.Getenv("CSRF_KEY")
	if csrfKey == "" {
		log.Fatal("CSRF_KEY environment variable is not set")
	}
	csrfProtect := csrf.Protect(
		[]byte(csrfKey),
		csrf.Secure(false), // Set to true when serving over HTTPS in production
		csrf.Path("/"),
		csrf.SameSite(csrf.SameSiteStrictMode),
	)
	router.Use(csrfProtect)

	// Public routes
	router.HandleFunc("/", homeHandler)
	router.HandleFunc("/stream", streamHandler)
	router.HandleFunc("/order-stream", orderStreamHandler)
	router.HandleFunc("/orders", orderDetailsHandler)

	// Static pages now use pre-parsed templates (Phase 5.2)
	router.HandleFunc("/service", staticPageHandler("service"))
	router.HandleFunc("/product", staticPageHandler("product"))
	router.HandleFunc("/team", staticPageHandler("team"))
	router.HandleFunc("/testimonial", staticPageHandler("testimonial"))
	router.HandleFunc("/contact", staticPageHandler("contact"))
	router.HandleFunc("/404", staticPageHandler("404"))

	// Admin auth routes (public - no auth required)
	router.HandleFunc("/admin/login", AdminLoginPageHandler).Methods("GET")
	router.HandleFunc("/admin/login", AdminLoginHandler).Methods("POST")
	router.HandleFunc("/admin/logout", AdminLogoutHandler).Methods("GET")

	// Admin protected routes (auth required)
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

	// Customer portal auth routes (public - no auth required)
	router.HandleFunc("/portal/login", CustomerLoginPageHandler).Methods("GET")
	router.HandleFunc("/portal/login", CustomerLoginHandler).Methods("POST")
	router.HandleFunc("/portal/logout", CustomerLogoutHandler).Methods("GET")

	// Customer portal protected routes (auth required)
	router.HandleFunc("/portal", RequireCustomerAuth(CustomerPortalDashboardHandler)).Methods("GET")
	router.HandleFunc("/portal/", RequireCustomerAuth(CustomerPortalDashboardHandler)).Methods("GET")
	router.HandleFunc("/portal/orders", RequireCustomerAuth(CustomerOrdersHandler)).Methods("GET")
	router.HandleFunc("/portal/orders/{id}", RequireCustomerAuth(CustomerOrderDetailHandler)).Methods("GET")
	router.HandleFunc("/portal/invoices", RequireCustomerAuth(CustomerInvoicesHandler)).Methods("GET")
	router.HandleFunc("/portal/invoices/{id}", RequireCustomerAuth(CustomerInvoiceDetailHandler)).Methods("GET")

	fs := http.FileServer(http.Dir("/cmd/web/templates/static"))
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	log.Fatal(http.ListenAndServe(":8080", router))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	// Use shared gRPC client (Phase 5.1)
	client := pb.NewCheckInventoryClient(sharedGRPCConn)

	// Best-effort auth context: include Bearer token if admin is logged in
	// CheckBreadInventory is open by default, but some server configs require auth.
	var ctx context.Context = r.Context()
	if authCtx, err := adminGRPCContext(r); err == nil {
		ctx = authCtx
	}

	response, err := client.CheckBreadInventory(ctx, &pb.BreadRequest{})
	if err != nil {
		log.Errorf("Error calling GetAvailableBreads service: %v", err)
		http.Error(w, "Failed to fetch inventory", http.StatusInternalServerError)
		return
	}

	log.Println("Response from server: ", response.Breads.GetBreads())

	breadCounts := make(map[string]int)
	for _, bread := range response.Breads.GetBreads() {
		breadCounts[bread.Name]++
	}

	breadLogs := make([]BreadLog, 0, len(breadCounts))
	for name, _ := range breadCounts {
		breadLogs = append(breadLogs, BreadLog{
			Name: name,
		})
	}

	// Use pre-parsed template (Phase 5.2)
	tmpl, ok := templates["index"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, breadLogs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// staticPageHandler returns an HTTP handler that serves a pre-parsed template
// by name. This avoids parsing templates on every request (Phase 5.2).
func staticPageHandler(templateName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl, ok := templates[templateName]
		if !ok {
			http.Error(w, "Template not found: "+templateName, http.StatusInternalServerError)
			return
		}
		err := tmpl.Execute(w, nil)
		if err != nil {
			http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// orderDetailsHandler fetches all buy orders via gRPC and renders them
// using the pre-parsed template (Phase 5.4).
func orderDetailsHandler(w http.ResponseWriter, r *http.Request) {
	// Use shared gRPC client (Phase 5.1)
	client := getSharedGRPCClient()

	// Admin auth context — GetAllOrders requires RoleAdmin
	ctx, cancel, err := adminGRPCContextWithTimeout(r, 10*time.Second)
	if err != nil {
		log.Errorf("Error getting admin auth context: %v", err)
		http.Redirect(w, r, "/admin/login?error=Session+expired", http.StatusSeeOther)
		return
	}
	defer cancel()

	orders, err := client.GetAllOrders(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting orders: %v", err)
		http.Error(w, "Failed to get orders", http.StatusInternalServerError)
		return
	}

	orderData := OrderData{
		BuyOrders:       orders.BuyOrders,
		BuyOrderDetails: nil, // Details would require per-order lookups
	}

	// Use pre-parsed template (Phase 5.2)
	tmpl, ok := templates["order-details"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, orderData)
	if err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

func streamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Use shared gRPC client (Phase 5.1)
	client := pb.NewCheckInventoryClient(sharedGRPCConn)

	// Best-effort auth context: include Bearer token if available
	var ctx context.Context = r.Context()
	if authCtx, err := adminGRPCContext(r); err == nil {
		ctx = authCtx
	}

	// Call gRPC stream — use r.Context() so the stream stops when the client disconnects
	stream, err := client.CheckBreadInventoryStream(ctx, &pb.BreadRequest{})
	if err != nil {
		log.Errorf("Error calling BreadUpdates service: %v", err)
		fmt.Fprintf(w, "data: {\"error\": \"failed to start stream\"}\n\n")
		return
	}

	for {
		breadList, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Errorf("Error receiving from stream: %v", err)
			break
		}

		breadCounts := make(map[string]int)
		for _, bread := range breadList.Breads.GetBreads() {
			breadCounts[bread.Name]++

			data := BreadLog{
				Name:     bread.Name,
				Quantity: int(bread.Quantity),
				Price:    bread.Price,
				Message:  bread.Description,
				Image:    bread.Image,
			}

			jsonData, err := json.Marshal(data)
			if err != nil {
				log.Errorf("Error serializing data: %v", err)
				continue
			}
			_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
			if err != nil {
				log.Errorf("Error writing to stream: %v", err)
				return
			}

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			} else {
				log.Errorf("Failed to flush")
			}
		}
	}
}

func orderStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Use shared gRPC client (Phase 5.1)
	client := pb.NewBuyOrderServiceClient(sharedGRPCConn)

	// Best-effort auth context: include customer token if available
	var ctx context.Context = r.Context()
	if authCtx := customerGRPCContext(r); authCtx != r.Context() {
		ctx = authCtx
	}

	// Call gRPC stream — use r.Context() so the stream stops when the client disconnects
	stream, err := client.BuyOrderStream(ctx, &pb.BuyOrderRequest{})
	if err != nil {
		http.Error(w, "Error calling BreadUpdates service", http.StatusInternalServerError)
		return
	}

	// Read from the stream and write to the HTTP response
	for {
		buyOrderResponse, err := stream.Recv()
		if err == io.EOF {
			// If the stream has ended, break the loop
			break
		}
		if err != nil {
			log.Printf("Failed to receive from stream: %v", err)
			http.Error(w, fmt.Sprintf("Error reading from the stream: %v", err), http.StatusInternalServerError)
			return
		}

		// Convert the response to JSON
		jsonData, err := json.Marshal(buyOrderResponse.GetBuyOrders())
		if err != nil {
			http.Error(w, "Error converting the response to JSON", http.StatusInternalServerError)
			return
		}

		// Write the response in Server-Sent Events (SSE) format
		_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
		if err != nil {
			return
		}

		log.Println("Response from server: ", buyOrderResponse.GetBuyOrders())

		// Flush the response writer to send the data immediately
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}
		flusher.Flush()
	}
}

// ── Initialization functions ──

// initTemplates pre-parses all templates at startup (Phase 5.2).
// Templates are stored in a map keyed by their logical page name
// and retrieved by handlers instead of parsing on every request.
func initTemplates() {
	templates = make(map[string]*template.Template)

	adminTemplates := []string{
		"base.html",
		"dashboard.html",
		"bread/list.html",
		"bread/form.html",
		"orders/list.html",
		"customers/list.html",
		"customers/detail.html",
		"makers/list.html",
		"makers/detail.html",
		"alerts.html",
		"login.html",
	}
	for _, t := range adminTemplates {
		path := getTemplatePath("./cmd/web/templates/admin/" + t)
		tmpl, err := template.ParseFiles(path)
		if err != nil {
			log.Fatalf("Failed to parse admin template %s: %v", t, err)
		}
		// Use "admin/" prefix to avoid collision with portal templates
		templates["admin/"+t] = tmpl
	}

	portalTemplates := []string{
		"base.html",
		"dashboard.html",
		"orders.html",
		"order_detail.html",
		"invoices.html",
		"invoice_detail.html",
		"login.html",
	}
	for _, t := range portalTemplates {
		path := getTemplatePath("./cmd/web/templates/portal/" + t)
		tmpl, err := template.ParseFiles(path)
		if err != nil {
			log.Fatalf("Failed to parse portal template %s: %v", t, err)
		}
		// Use "portal/" prefix to avoid collision with admin templates
		templates["portal/"+t] = tmpl
	}

	// Public page templates
	publicTemplates := []string{
		"index.html",
		"order-details.html",
		"service.html",
		"product.html",
		"team.html",
		"testimonial.html",
		"contact.html",
		"404.html",
	}
	for _, path := range publicTemplates {
		tmpl, err := template.ParseFiles(getTemplatePath("./cmd/web/templates/" + path))
		if err != nil {
			log.Fatalf("Failed to parse template %s: %v", path, err)
		}
		// Use the relative path as key to match staticPageHandler usage
		templates["./templates/"+path] = tmpl
	}

	log.Printf("Pre-parsed %d templates at startup", len(templates))
}

// initSharedGRPCConnection creates a single shared gRPC connection with
// proper keep-alive settings (Phase 5.1). This prevents connection exhaustion
// under load by reusing connections instead of creating/destroying them per request.
func initSharedGRPCConnection() {
	sharedGRPCOnce.Do(func() {
		dialTimeout := 5 * time.Second
		if t := os.Getenv("GRPC_DIAL_TIMEOUT"); t != "" {
			if d, err := time.ParseDuration(t); err == nil {
				dialTimeout = d
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
		defer cancel()

		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                30 * time.Second,
				Timeout:             20 * time.Second,
				PermitWithoutStream: true,
			}),
		}
		var err error
		sharedGRPCConn, err = grpc.DialContext(ctx, gRPCAddress, opts...)
		if err != nil {
			log.Fatalf("Failed to create shared gRPC connection: %v", err)
		}
		log.Printf("Shared gRPC connection initialized to %s", gRPCAddress)
	})
}
