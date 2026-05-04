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
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
	BuyOrders       []BuyOrder       `json:"buyOrders"`
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
	router.HandleFunc("/service", staticPageHandler(getTemplatePath("./cmd/web/templates/service.html")))
	router.HandleFunc("/product", staticPageHandler(getTemplatePath("./cmd/web/templates/product.html")))
	router.HandleFunc("/team", staticPageHandler(getTemplatePath("./cmd/web/templates/team.html")))
	router.HandleFunc("/testimonial", staticPageHandler(getTemplatePath("./cmd/web/templates/testimonial.html")))
	router.HandleFunc("/contact", staticPageHandler(getTemplatePath("./cmd/web/templates/contact.html")))
	router.HandleFunc("/404", staticPageHandler(getTemplatePath("./cmd/web/templates/404.html")))

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
	// Setup the connection to the server
	conn, err := grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Errorf("Failed to connect to gRPC server: %v", err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer func(conn *grpc.ClientConn) {
		if err := conn.Close(); err != nil {
			log.Errorf("Failed to close gRPC connection: %v", err)
		}
	}(conn)

	// Initialize the client
	client := pb.NewCheckInventoryClient(conn)

	// Call GetAvailableBreads service
	response, err := client.CheckBreadInventory(context.Background(), &pb.BreadRequest{})
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

	tmpl := template.Must(template.ParseFiles(getTemplatePath("./cmd/web/templates/index.html")))
	err = tmpl.Execute(w, breadLogs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func staticPageHandler(templatePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles(templatePath)
		if err != nil {
			http.Error(w, "Error parsing template: "+err.Error(), http.StatusInternalServerError)
			return
		}
		err = tmpl.Execute(w, nil)
		if err != nil {
			http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

func orderDetailsHandler(w http.ResponseWriter, r *http.Request) {

	// Initialize an empty slice of OrderData
	// Note: Populate this slice if you have actual data to pass to the template
	orderDetails := make([]OrderData, 0)

	// Parse the template
	tmpl, err := template.ParseFiles(getTemplatePath("./cmd/web/templates/order-details.html"))
	if err != nil {
		http.Error(w, "Error parsing template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Execute and render the template with the provided data
	err = tmpl.Execute(w, orderDetails)
	if err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

func streamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Setup the connection to the server
	conn, err := grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Errorf("Failed to connect to gRPC server: %v", err)
		fmt.Fprintf(w, "data: {\"error\": \"service unavailable\"}\n\n")
		return
	}
	defer func(conn *grpc.ClientConn) {
		if err := conn.Close(); err != nil {
			log.Errorf("Failed to close gRPC connection: %v", err)
		}
	}(conn)

	// Initialize the client
	client := pb.NewCheckInventoryClient(conn)

	// Call gRPC stream — use r.Context() so the stream stops when the client disconnects
	stream, err := client.CheckBreadInventoryStream(r.Context(), &pb.BreadRequest{})
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

	// Setup the connection to the server
	conn, err := grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		http.Error(w, "Failed to connect to gRPC server", http.StatusInternalServerError)
		return
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			log.Printf("Failed to close gRPC connection: %v", err)
		}
	}(conn)

	// Initialize the client
	client := pb.NewBuyOrderServiceClient(conn)

	// Call gRPC stream — use r.Context() so the stream stops when the client disconnects
	stream, err := client.BuyOrderStream(r.Context(), &pb.BuyOrderRequest{})
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
