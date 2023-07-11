package main

import (
	"context"
	"encoding/json"
	"fmt"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/gorilla/mux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
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

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

func main() {
	router := mux.NewRouter()

	router.HandleFunc("/", homeHandler)
	router.HandleFunc("/stream", streamHandler)

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
				log.Printf("Error serializing data: %v", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", jsonData)

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			} else {
				log.Println("Failed to flush")
			}
		}
	}
}
