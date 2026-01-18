package main

import (
	"context"
	"encoding/json"
	"fmt"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"html/template"
	"io"
	"net/http"
	"os"
	"time"
)

type BreadLog struct {
	ID       int
	Name     string
	Message  string
	Buyer    string
	Maker    string
	Quantity int
	Price    float32
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

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

func main() {

	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
	router := mux.NewRouter()

	// Public routes
	router.HandleFunc("/", homeHandler)
	router.HandleFunc("/stream", streamHandler)
	router.HandleFunc("/order-stream", orderStreamHandler)
	router.HandleFunc("/orders", orderDetailsHandler)

	// Admin routes
	router.HandleFunc("/admin", AdminDashboardHandler).Methods("GET")
	router.HandleFunc("/admin/bread", AdminBreadListHandler).Methods("GET")
	router.HandleFunc("/admin/bread/new", AdminBreadNewHandler).Methods("GET")
	router.HandleFunc("/admin/bread/create", AdminBreadCreateHandler).Methods("POST")
	router.HandleFunc("/admin/bread/{id}/edit", AdminBreadEditHandler).Methods("GET")
	router.HandleFunc("/admin/bread/{id}/update", AdminBreadUpdateHandler).Methods("POST")
	router.HandleFunc("/admin/bread/{id}/delete", AdminBreadDeleteHandler).Methods("POST")
	router.HandleFunc("/admin/orders", AdminOrdersHandler).Methods("GET")
	router.HandleFunc("/admin/orders/{id}/status", AdminOrderStatusHandler).Methods("POST")
	router.HandleFunc("/admin/customers", AdminCustomersHandler).Methods("GET")
	router.HandleFunc("/admin/customers/{id}", AdminCustomerDetailHandler).Methods("GET")
	router.HandleFunc("/admin/makers", AdminMakersHandler).Methods("GET")
	router.HandleFunc("/admin/makers/{id}", AdminMakerDetailHandler).Methods("GET")
	router.HandleFunc("/admin/alerts", AdminAlertsHandler).Methods("GET")
	router.HandleFunc("/admin/alerts/{id}/adjust", AdminAdjustQuantityHandler).Methods("POST")

	// Admin SSE streams
	router.HandleFunc("/admin/dashboard-stream", AdminDashboardStreamHandler).Methods("GET")
	router.HandleFunc("/admin/alerts-stream", AdminAlertsStreamHandler).Methods("GET")

	fs := http.FileServer(http.Dir("/cmd/web/templates/static"))
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	log.Fatal(http.ListenAndServe(":8080", router))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	// Setup the connection to the server
	conn, err := grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			log.Fatalf("Failed to close gRPC connection: %v", err)
		}
	}(conn)

	// Initialize the client
	client := pb.NewCheckInventoryClient(conn)

	// Call GetAvailableBreads service
	response, err := client.CheckBreadInventory(context.Background(), &pb.BreadRequest{})
	if err != nil {
		log.Fatalf("Error calling GetAvailableBreads service: %v", err)
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

	tmpl := template.Must(template.ParseFiles("./cmd/web/templates/index.html"))
	err = tmpl.Execute(w, breadLogs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func orderDetailsHandler(w http.ResponseWriter, r *http.Request) {

	// Initialize an empty slice of OrderData
	// Note: Populate this slice if you have actual data to pass to the template
	orderDetails := make([]OrderData, 0)

	// Parse the template
	tmpl, err := template.ParseFiles("./cmd/web/templates/order-details.html")
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
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			log.Fatalf("Failed to close gRPC connection: %v", err)
		}
	}(conn)

	// Initialize the client
	client := pb.NewCheckInventoryClient(conn)

	// Call gRPC stream
	stream, err := client.CheckBreadInventoryStream(context.Background(), &pb.BreadRequest{})
	if err != nil {
		log.Fatalf("Error calling BreadUpdates service: %v", err)
	}

	for {
		breadList, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Error receiving from stream: %v", err)
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

	// Call gRPC stream
	stream, err := client.BuyOrderStream(context.Background(), &pb.BuyOrderRequest{})
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
