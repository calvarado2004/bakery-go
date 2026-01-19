package main

import (
	"context"
	"fmt"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InvoiceServiceServer struct {
	pb.UnimplementedInvoiceServiceServer
	RabbitMQBakery *RabbitMQBakery
}

func (s *InvoiceServiceServer) CreateInvoice(ctx context.Context, in *pb.CreateInvoiceRequest) (*pb.Invoice, error) {
	// Get the order
	order, err := s.RabbitMQBakery.Repo.GetBuyOrderByID(int(in.BuyOrderId))
	if err != nil {
		log.Errorf("Error getting order: %v", err)
		return nil, status.Errorf(codes.NotFound, "Order not found: %v", err)
	}

	// Check if invoice already exists
	existingInvoice, err := s.RabbitMQBakery.Repo.GetInvoiceByOrderID(int(in.BuyOrderId))
	if err == nil && existingInvoice.ID > 0 {
		return invoiceToProto(&existingInvoice), nil
	}

	// Calculate totals
	var subtotal float32
	var items []data.InvoiceItem
	for _, bread := range order.Breads {
		itemTotal := bread.Price * float32(bread.Quantity)
		subtotal += itemTotal
		items = append(items, data.InvoiceItem{
			BreadID:   bread.ID,
			BreadName: bread.Name,
			Quantity:  bread.Quantity,
			UnitPrice: bread.Price,
			Total:     itemTotal,
		})
	}

	tax := subtotal * 0.08 // 8% tax
	total := subtotal + tax

	// Generate invoice number
	invoiceNumber := fmt.Sprintf("INV-%d-%d", time.Now().Year(), time.Now().UnixNano()%100000)

	invoice := data.Invoice{
		BuyOrderID:    order.ID,
		CustomerID:    order.CustomerID,
		InvoiceNumber: invoiceNumber,
		Subtotal:      subtotal,
		Tax:           tax,
		Total:         total,
		Status:        "pending",
		DueDate:       time.Now().AddDate(0, 0, 30), // Due in 30 days
		Items:         items,
	}

	newID, err := s.RabbitMQBakery.Repo.InsertInvoice(invoice)
	if err != nil {
		log.Errorf("Error creating invoice: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to create invoice: %v", err)
	}

	invoice.ID = newID
	invoice.CreatedAt = time.Now()

	return invoiceToProto(&invoice), nil
}

func (s *InvoiceServiceServer) GetInvoice(ctx context.Context, in *pb.InvoiceIdRequest) (*pb.Invoice, error) {
	invoice, err := s.RabbitMQBakery.Repo.GetInvoiceByID(int(in.Id))
	if err != nil {
		log.Errorf("Error getting invoice: %v", err)
		return nil, status.Errorf(codes.NotFound, "Invoice not found: %v", err)
	}

	return invoiceToProto(&invoice), nil
}

func (s *InvoiceServiceServer) GetCustomerInvoices(ctx context.Context, in *pb.CustomerInvoicesRequest) (*pb.InvoiceList, error) {
	invoices, err := s.RabbitMQBakery.Repo.GetInvoicesByCustomerID(int(in.CustomerId))
	if err != nil {
		log.Errorf("Error getting customer invoices: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get invoices: %v", err)
	}

	var pbInvoices []*pb.Invoice
	for _, inv := range invoices {
		pbInvoices = append(pbInvoices, invoiceToProto(&inv))
	}

	return &pb.InvoiceList{Invoices: pbInvoices}, nil
}

func (s *InvoiceServiceServer) GetAllInvoices(ctx context.Context, in *pb.Empty) (*pb.InvoiceList, error) {
	invoices, err := s.RabbitMQBakery.Repo.GetAllInvoices()
	if err != nil {
		log.Errorf("Error getting all invoices: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get invoices: %v", err)
	}

	var pbInvoices []*pb.Invoice
	for _, inv := range invoices {
		pbInvoices = append(pbInvoices, invoiceToProto(&inv))
	}

	return &pb.InvoiceList{Invoices: pbInvoices}, nil
}

func invoiceToProto(invoice *data.Invoice) *pb.Invoice {
	var pbItems []*pb.InvoiceItem
	for _, item := range invoice.Items {
		pbItems = append(pbItems, &pb.InvoiceItem{
			Id:        int32(item.ID),
			InvoiceId: int32(item.InvoiceID),
			BreadId:   int32(item.BreadID),
			BreadName: item.BreadName,
			Quantity:  int32(item.Quantity),
			UnitPrice: item.UnitPrice,
			Total:     item.Total,
		})
	}

	paidAt := ""
	if invoice.PaidAt != nil {
		paidAt = invoice.PaidAt.String()
	}

	return &pb.Invoice{
		Id:            int32(invoice.ID),
		BuyOrderId:    int32(invoice.BuyOrderID),
		CustomerId:    int32(invoice.CustomerID),
		InvoiceNumber: invoice.InvoiceNumber,
		Subtotal:      invoice.Subtotal,
		Tax:           invoice.Tax,
		Total:         invoice.Total,
		Status:        invoice.Status,
		CreatedAt:     invoice.CreatedAt.String(),
		DueDate:       invoice.DueDate.String(),
		PaidAt:        paidAt,
		Items:         pbItems,
	}
}

// CustomerPortalServiceServer
type CustomerPortalServiceServer struct {
	pb.UnimplementedCustomerPortalServiceServer
	RabbitMQBakery *RabbitMQBakery
}

func (s *CustomerPortalServiceServer) GetMyOrders(ctx context.Context, in *pb.CustomerIdRequest) (*pb.CustomerOrdersResponse, error) {
	customer, err := s.RabbitMQBakery.Repo.GetCustomerByID(int(in.Id))
	if err != nil {
		log.Errorf("Error getting customer: %v", err)
		return nil, status.Errorf(codes.NotFound, "Customer not found: %v", err)
	}

	orders, err := s.RabbitMQBakery.Repo.GetCustomerOrders(int(in.Id))
	if err != nil {
		log.Errorf("Error getting customer orders: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get orders: %v", err)
	}

	var pbOrders []*pb.BuyOrder
	for _, o := range orders {
		totalCost, _ := s.RabbitMQBakery.Repo.GetOrderTotalCost(o.ID)
		pbOrders = append(pbOrders, &pb.BuyOrder{
			Id:           int32(o.ID),
			CustomerId:   int32(o.CustomerID),
			BuyOrderUuid: o.BuyOrderUUID,
			TotalCost:    totalCost,
		})
	}

	return &pb.CustomerOrdersResponse{
		Customer: &pb.Customer{
			Id:        int32(customer.ID),
			Name:      customer.Name,
			Email:     customer.Email,
			CreatedAt: customer.CreatedAt.String(),
			UpdatedAt: customer.UpdatedAt.String(),
		},
		Orders: pbOrders,
	}, nil
}

func (s *CustomerPortalServiceServer) GetMyInvoices(ctx context.Context, in *pb.CustomerIdRequest) (*pb.InvoiceList, error) {
	invoices, err := s.RabbitMQBakery.Repo.GetInvoicesByCustomerID(int(in.Id))
	if err != nil {
		log.Errorf("Error getting customer invoices: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get invoices: %v", err)
	}

	var pbInvoices []*pb.Invoice
	for _, inv := range invoices {
		pbInvoices = append(pbInvoices, invoiceToProto(&inv))
	}

	return &pb.InvoiceList{Invoices: pbInvoices}, nil
}

func (s *CustomerPortalServiceServer) GetOrderDetails(ctx context.Context, in *pb.BuyOrderIdRequest) (*pb.BuyOrderDetailsResponse, error) {
	order, err := s.RabbitMQBakery.Repo.GetBuyOrderByID(int(in.Id))
	if err != nil {
		log.Errorf("Error getting order: %v", err)
		return nil, status.Errorf(codes.NotFound, "Order not found: %v", err)
	}

	totalCost, _ := s.RabbitMQBakery.Repo.GetOrderTotalCost(order.ID)

	var pbDetails []*pb.BuyOrderDetails
	for _, bread := range order.Breads {
		pbDetails = append(pbDetails, &pb.BuyOrderDetails{
			BuyOrderId:   int32(order.ID),
			BuyOrderUuid: order.BuyOrderUUID,
			BreadId:      int32(bread.ID),
			Quantity:     int32(bread.Quantity),
			Price:        bread.Price,
			Status:       order.Status,
			CreatedAt:    order.CreatedAt.String(),
			UpdatedAt:    order.UpdatedAt.String(),
		})
	}

	return &pb.BuyOrderDetailsResponse{
		Order: &pb.BuyOrder{
			Id:           int32(order.ID),
			CustomerId:   int32(order.CustomerID),
			BuyOrderUuid: order.BuyOrderUUID,
			TotalCost:    totalCost,
		},
		Details:   pbDetails,
		TotalCost: totalCost,
	}, nil
}
