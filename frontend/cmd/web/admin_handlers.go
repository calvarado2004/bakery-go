package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// AdminTemplateData holds common data for admin templates
type AdminTemplateData struct {
	Title       string
	CurrentPage string
	Stats       *pb.DashboardStats
	Breads      []*pb.Bread
	Bread       *pb.Bread
	Customers   []*pb.Customer
	Customer    *pb.Customer
	Makers      []*pb.BreadMakerProto
	Maker       *pb.BreadMakerProto
	Orders      []*pb.BuyOrder
	MakeOrders  []*pb.MakeOrderProto
	Alerts      []*pb.Bread
	Message     string
	Error       string
}

func getGRPCConnection() (*grpc.ClientConn, error) {
	return grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func AdminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := client.GetDashboardStats(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting dashboard stats: %v", err)
		stats = &pb.DashboardStats{}
	}

	alerts, err := client.GetLowStockAlerts(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting low stock alerts: %v", err)
	}

	orders, err := client.GetAllOrders(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting orders: %v", err)
	}

	data := AdminTemplateData{
		Title:       "Admin Dashboard",
		CurrentPage: "dashboard",
		Stats:       stats,
	}
	if alerts != nil {
		data.Alerts = alerts.Breads
	}
	if orders != nil && len(orders.BuyOrders) > 5 {
		data.Orders = orders.BuyOrders[:5]
	} else if orders != nil {
		data.Orders = orders.BuyOrders
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/dashboard.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminBreadListHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	breads, err := client.GetAllBread(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting breads: %v", err)
		http.Error(w, "Failed to get breads", http.StatusInternalServerError)
		return
	}

	data := AdminTemplateData{
		Title:       "Bread Management",
		CurrentPage: "bread",
		Breads:      breads.Breads,
		Message:     r.URL.Query().Get("message"),
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/bread/list.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminBreadNewHandler(w http.ResponseWriter, r *http.Request) {
	data := AdminTemplateData{
		Title:       "New Bread",
		CurrentPage: "bread",
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/bread/form.html",
	))
	err := tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminBreadCreateHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	price, _ := strconv.ParseFloat(r.FormValue("price"), 32)
	quantity, _ := strconv.Atoi(r.FormValue("quantity"))

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = client.CreateBread(ctx, &pb.CreateBreadRequest{
		Name:        r.FormValue("name"),
		Price:       float32(price),
		Quantity:    int32(quantity),
		Description: r.FormValue("description"),
		Type:        r.FormValue("type"),
		Image:       r.FormValue("image"),
	})
	if err != nil {
		log.Errorf("Error creating bread: %v", err)
		http.Error(w, "Failed to create bread", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/bread?message=Bread+created+successfully", http.StatusSeeOther)
}

func AdminBreadEditHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.Atoi(vars["id"])

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bread, err := client.GetBreadById(ctx, &pb.BreadIdRequest{Id: int32(id)})
	if err != nil {
		log.Errorf("Error getting bread: %v", err)
		http.Error(w, "Bread not found", http.StatusNotFound)
		return
	}

	data := AdminTemplateData{
		Title:       "Edit Bread",
		CurrentPage: "bread",
		Bread:       bread,
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/bread/form.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminBreadUpdateHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.Atoi(vars["id"])

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	price, _ := strconv.ParseFloat(r.FormValue("price"), 32)
	quantity, _ := strconv.Atoi(r.FormValue("quantity"))

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = client.UpdateBread(ctx, &pb.UpdateBreadRequest{
		Id:          int32(id),
		Name:        r.FormValue("name"),
		Price:       float32(price),
		Quantity:    int32(quantity),
		Description: r.FormValue("description"),
		Type:        r.FormValue("type"),
		Image:       r.FormValue("image"),
	})
	if err != nil {
		log.Errorf("Error updating bread: %v", err)
		http.Error(w, "Failed to update bread", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/bread?message=Bread+updated+successfully", http.StatusSeeOther)
}

func AdminBreadDeleteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.Atoi(vars["id"])

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = client.DeleteBread(ctx, &pb.DeleteBreadRequest{Id: int32(id)})
	if err != nil {
		log.Errorf("Error deleting bread: %v", err)
		http.Error(w, "Failed to delete bread", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/bread?message=Bread+deleted+successfully", http.StatusSeeOther)
}

func AdminOrdersHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	orders, err := client.GetAllOrders(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting orders: %v", err)
		http.Error(w, "Failed to get orders", http.StatusInternalServerError)
		return
	}

	data := AdminTemplateData{
		Title:       "Order Management",
		CurrentPage: "orders",
		Orders:      orders.BuyOrders,
		Message:     r.URL.Query().Get("message"),
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/orders/list.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminOrderStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["id"]

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = client.UpdateOrderStatus(ctx, &pb.UpdateOrderStatusRequest{
		BuyOrderUuid: uuid,
		Status:       r.FormValue("status"),
	})
	if err != nil {
		log.Errorf("Error updating order status: %v", err)
		http.Error(w, "Failed to update order status", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/orders?message=Order+status+updated", http.StatusSeeOther)
}

func AdminCustomersHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	customers, err := client.GetAllCustomers(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting customers: %v", err)
		http.Error(w, "Failed to get customers", http.StatusInternalServerError)
		return
	}

	data := AdminTemplateData{
		Title:       "Customer Management",
		CurrentPage: "customers",
		Customers:   customers.Customers,
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/customers/list.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminCustomerDetailHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.Atoi(vars["id"])

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	response, err := client.GetCustomerOrders(ctx, &pb.CustomerIdRequest{Id: int32(id)})
	if err != nil {
		log.Errorf("Error getting customer: %v", err)
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}

	data := AdminTemplateData{
		Title:       "Customer Details",
		CurrentPage: "customers",
		Customer:    response.Customer,
		Orders:      response.Orders,
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/customers/detail.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminMakersHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	makers, err := client.GetAllBreadMakers(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting bread makers: %v", err)
		http.Error(w, "Failed to get bread makers", http.StatusInternalServerError)
		return
	}

	data := AdminTemplateData{
		Title:       "Bread Maker Management",
		CurrentPage: "makers",
		Makers:      makers.BreadMakers,
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/makers/list.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminMakerDetailHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.Atoi(vars["id"])

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	response, err := client.GetMakerOrders(ctx, &pb.BreadMakerIdRequest{Id: int32(id)})
	if err != nil {
		log.Errorf("Error getting bread maker: %v", err)
		http.Error(w, "Bread maker not found", http.StatusNotFound)
		return
	}

	data := AdminTemplateData{
		Title:       "Bread Maker Details",
		CurrentPage: "makers",
		Maker:       response.Maker,
		MakeOrders:  response.Orders,
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/makers/detail.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminAlertsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	alerts, err := client.GetLowStockAlerts(ctx, &pb.Empty{})
	if err != nil {
		log.Errorf("Error getting alerts: %v", err)
		http.Error(w, "Failed to get alerts", http.StatusInternalServerError)
		return
	}

	data := AdminTemplateData{
		Title:       "Inventory Alerts",
		CurrentPage: "alerts",
		Alerts:      alerts.Breads,
		Message:     r.URL.Query().Get("message"),
	}

	tmpl := template.Must(template.ParseFiles(
		"./cmd/web/templates/admin/base.html",
		"./cmd/web/templates/admin/alerts.html",
	))
	err = tmpl.ExecuteTemplate(w, "base", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func AdminDashboardStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		default:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			stats, err := client.GetDashboardStats(ctx, &pb.Empty{})
			cancel()
			if err != nil {
				log.Errorf("Error getting dashboard stats: %v", err)
				continue
			}

			jsonData, err := json.Marshal(stats)
			if err != nil {
				log.Errorf("Error marshaling stats: %v", err)
				continue
			}

			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()

			time.Sleep(15 * time.Second)
		}
	}
}

func AdminAlertsStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		default:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			alerts, err := client.GetLowStockAlerts(ctx, &pb.Empty{})
			cancel()
			if err != nil {
				log.Errorf("Error getting alerts: %v", err)
				continue
			}

			jsonData, err := json.Marshal(alerts)
			if err != nil {
				log.Errorf("Error marshaling alerts: %v", err)
				continue
			}

			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()

			time.Sleep(15 * time.Second)
		}
	}
}

func AdminAdjustQuantityHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.Atoi(vars["id"])

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	quantity, _ := strconv.Atoi(r.FormValue("quantity"))

	conn, err := getGRPCConnection()
	if err != nil {
		http.Error(w, "Failed to connect to server", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := pb.NewAdminServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get current bread
	bread, err := client.GetBreadById(ctx, &pb.BreadIdRequest{Id: int32(id)})
	if err != nil {
		log.Errorf("Error getting bread: %v", err)
		http.Error(w, "Bread not found", http.StatusNotFound)
		return
	}

	// Update with new quantity
	_, err = client.UpdateBread(ctx, &pb.UpdateBreadRequest{
		Id:          int32(id),
		Name:        bread.Name,
		Price:       bread.Price,
		Quantity:    int32(quantity),
		Description: bread.Description,
		Type:        bread.Type,
		Image:       bread.Image,
	})
	if err != nil {
		log.Errorf("Error updating bread quantity: %v", err)
		http.Error(w, "Failed to update quantity", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/alerts?message=Quantity+updated+successfully", http.StatusSeeOther)
}
