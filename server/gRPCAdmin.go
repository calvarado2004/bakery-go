package main

import (
	"context"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AdminServiceServer struct {
	pb.UnimplementedAdminServiceServer
	RabbitMQBakery *RabbitMQBakery
}

func (s *AdminServiceServer) GetDashboardStats(ctx context.Context, in *pb.Empty) (*pb.DashboardStats, error) {
	stats, err := s.RabbitMQBakery.Repo.GetDashboardStats()
	if err != nil {
		log.Errorf("Error getting dashboard stats: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get dashboard stats: %v", err)
	}

	return &pb.DashboardStats{
		TotalOrders:     int32(stats.TotalOrders),
		TotalRevenue:    stats.TotalRevenue,
		TotalProducts:   int32(stats.TotalProducts),
		TotalCustomers:  int32(stats.TotalCustomers),
		TotalBreadMakers: int32(stats.TotalBreadMakers),
		LowStockCount:   int32(stats.LowStockCount),
	}, nil
}

func (s *AdminServiceServer) GetAllCustomers(ctx context.Context, in *pb.Empty) (*pb.CustomerList, error) {
	customers, err := s.RabbitMQBakery.Repo.GetAllCustomers()
	if err != nil {
		log.Errorf("Error getting all customers: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get customers: %v", err)
	}

	var pbCustomers []*pb.Customer
	for _, c := range customers {
		pbCustomers = append(pbCustomers, &pb.Customer{
			Id:        int32(c.ID),
			Name:      c.Name,
			Email:     c.Email,
			CreatedAt: c.CreatedAt.String(),
			UpdatedAt: c.UpdatedAt.String(),
		})
	}

	return &pb.CustomerList{Customers: pbCustomers}, nil
}

func (s *AdminServiceServer) GetAllBreadMakers(ctx context.Context, in *pb.Empty) (*pb.BreadMakerList, error) {
	makers, err := s.RabbitMQBakery.Repo.GetAllBreadMakers()
	if err != nil {
		log.Errorf("Error getting all bread makers: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get bread makers: %v", err)
	}

	var pbMakers []*pb.BreadMakerProto
	for _, m := range makers {
		pbMakers = append(pbMakers, &pb.BreadMakerProto{
			Id:        int32(m.ID),
			Name:      m.Name,
			Email:     m.Email,
			CreatedAt: m.CreatedAt.String(),
			UpdatedAt: m.UpdatedAt.String(),
		})
	}

	return &pb.BreadMakerList{BreadMakers: pbMakers}, nil
}

func (s *AdminServiceServer) GetAllBread(ctx context.Context, in *pb.Empty) (*pb.BreadList, error) {
	breads, err := s.RabbitMQBakery.Repo.GetAvailableBread()
	if err != nil {
		log.Errorf("Error getting all bread: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get bread: %v", err)
	}

	var pbBreads []*pb.Bread
	for _, b := range breads {
		pbBreads = append(pbBreads, &pb.Bread{
			Id:          int32(b.ID),
			Name:        b.Name,
			Price:       b.Price,
			Quantity:    int32(b.Quantity),
			Description: b.Description,
			Type:        b.Type,
			Status:      b.Status,
			CreatedAt:   b.CreatedAt.String(),
			UpdatedAt:   b.UpdatedAt.String(),
			Image:       b.Image,
		})
	}

	return &pb.BreadList{Breads: pbBreads}, nil
}

func (s *AdminServiceServer) GetBreadById(ctx context.Context, in *pb.BreadIdRequest) (*pb.Bread, error) {
	bread, err := s.RabbitMQBakery.Repo.GetBreadByID(int(in.Id))
	if err != nil {
		log.Errorf("Error getting bread by ID: %v", err)
		return nil, status.Errorf(codes.NotFound, "Bread not found: %v", err)
	}

	return &pb.Bread{
		Id:          int32(bread.ID),
		Name:        bread.Name,
		Price:       bread.Price,
		Quantity:    int32(bread.Quantity),
		Description: bread.Description,
		Type:        bread.Type,
		Status:      bread.Status,
		CreatedAt:   bread.CreatedAt.String(),
		UpdatedAt:   bread.UpdatedAt.String(),
		Image:       bread.Image,
	}, nil
}

func (s *AdminServiceServer) CreateBread(ctx context.Context, in *pb.CreateBreadRequest) (*pb.Bread, error) {
	bread := data.Bread{
		Name:        in.Name,
		Price:       in.Price,
		Quantity:    int(in.Quantity),
		Description: in.Description,
		Type:        in.Type,
		Image:       in.Image,
		Status:      "available",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	newID, err := s.RabbitMQBakery.Repo.InsertBread(bread)
	if err != nil {
		log.Errorf("Error creating bread: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to create bread: %v", err)
	}

	return &pb.Bread{
		Id:          int32(newID),
		Name:        bread.Name,
		Price:       bread.Price,
		Quantity:    int32(bread.Quantity),
		Description: bread.Description,
		Type:        bread.Type,
		Status:      bread.Status,
		Image:       bread.Image,
		CreatedAt:   bread.CreatedAt.String(),
		UpdatedAt:   bread.UpdatedAt.String(),
	}, nil
}

func (s *AdminServiceServer) UpdateBread(ctx context.Context, in *pb.UpdateBreadRequest) (*pb.Bread, error) {
	bread := data.Bread{
		ID:          int(in.Id),
		Name:        in.Name,
		Price:       in.Price,
		Quantity:    int(in.Quantity),
		Description: in.Description,
		Type:        in.Type,
		Image:       in.Image,
	}

	err := s.RabbitMQBakery.Repo.UpdateBread(bread)
	if err != nil {
		log.Errorf("Error updating bread: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to update bread: %v", err)
	}

	// Get updated bread
	updatedBread, err := s.RabbitMQBakery.Repo.GetBreadByID(int(in.Id))
	if err != nil {
		log.Errorf("Error getting updated bread: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get updated bread: %v", err)
	}

	return &pb.Bread{
		Id:          int32(updatedBread.ID),
		Name:        updatedBread.Name,
		Price:       updatedBread.Price,
		Quantity:    int32(updatedBread.Quantity),
		Description: updatedBread.Description,
		Type:        updatedBread.Type,
		Status:      updatedBread.Status,
		Image:       updatedBread.Image,
		CreatedAt:   updatedBread.CreatedAt.String(),
		UpdatedAt:   updatedBread.UpdatedAt.String(),
	}, nil
}

func (s *AdminServiceServer) DeleteBread(ctx context.Context, in *pb.DeleteBreadRequest) (*pb.Empty, error) {
	err := s.RabbitMQBakery.Repo.DeleteBread(int(in.Id))
	if err != nil {
		log.Errorf("Error deleting bread: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to delete bread: %v", err)
	}

	return &pb.Empty{}, nil
}

func (s *AdminServiceServer) GetLowStockAlerts(ctx context.Context, in *pb.Empty) (*pb.BreadList, error) {
	breads, err := s.RabbitMQBakery.Repo.GetLowStockBread(10)
	if err != nil {
		log.Errorf("Error getting low stock alerts: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get low stock alerts: %v", err)
	}

	var pbBreads []*pb.Bread
	for _, b := range breads {
		pbBreads = append(pbBreads, &pb.Bread{
			Id:          int32(b.ID),
			Name:        b.Name,
			Price:       b.Price,
			Quantity:    int32(b.Quantity),
			Description: b.Description,
			Type:        b.Type,
			Status:      b.Status,
			CreatedAt:   b.CreatedAt.String(),
			UpdatedAt:   b.UpdatedAt.String(),
			Image:       b.Image,
		})
	}

	return &pb.BreadList{Breads: pbBreads}, nil
}

func (s *AdminServiceServer) UpdateOrderStatus(ctx context.Context, in *pb.UpdateOrderStatusRequest) (*pb.BuyOrder, error) {
	err := s.RabbitMQBakery.Repo.UpdateOrderStatus(in.BuyOrderUuid, in.Status)
	if err != nil {
		log.Errorf("Error updating order status: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to update order status: %v", err)
	}

	order, err := s.RabbitMQBakery.Repo.GetBuyOrderByUUID(in.BuyOrderUuid)
	if err != nil {
		log.Errorf("Error getting updated order: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get updated order: %v", err)
	}

	totalCost, err := s.RabbitMQBakery.Repo.GetOrderTotalCost(order.ID)
	if err != nil {
		log.Errorf("Error getting order total cost: %v", err)
	}

	return &pb.BuyOrder{
		Id:           int32(order.ID),
		CustomerId:   int32(order.CustomerID),
		BuyOrderUuid: order.BuyOrderUUID,
		TotalCost:    totalCost,
	}, nil
}

func (s *AdminServiceServer) GetCustomerOrders(ctx context.Context, in *pb.CustomerIdRequest) (*pb.CustomerOrdersResponse, error) {
	customer, err := s.RabbitMQBakery.Repo.GetCustomerByID(int(in.Id))
	if err != nil {
		log.Errorf("Error getting customer: %v", err)
		return nil, status.Errorf(codes.NotFound, "Customer not found: %v", err)
	}

	orders, err := s.RabbitMQBakery.Repo.GetCustomerOrders(int(in.Id))
	if err != nil {
		log.Errorf("Error getting customer orders: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get customer orders: %v", err)
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

func (s *AdminServiceServer) GetMakerOrders(ctx context.Context, in *pb.BreadMakerIdRequest) (*pb.MakerOrdersResponse, error) {
	maker, err := s.RabbitMQBakery.Repo.GetBreadMakerByID(int(in.Id))
	if err != nil {
		log.Errorf("Error getting bread maker: %v", err)
		return nil, status.Errorf(codes.NotFound, "Bread maker not found: %v", err)
	}

	orders, err := s.RabbitMQBakery.Repo.GetMakerOrders(int(in.Id))
	if err != nil {
		log.Errorf("Error getting maker orders: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get maker orders: %v", err)
	}

	var pbOrders []*pb.MakeOrderProto
	for _, o := range orders {
		var pbBreads []*pb.Bread
		for _, b := range o.Breads {
			pbBreads = append(pbBreads, &pb.Bread{
				Id:          int32(b.ID),
				Name:        b.Name,
				Price:       b.Price,
				Quantity:    int32(b.Quantity),
				Description: b.Description,
				Type:        b.Type,
				Image:       b.Image,
			})
		}
		pbOrders = append(pbOrders, &pb.MakeOrderProto{
			Id:            int32(o.ID),
			BreadMakerId:  int32(o.BreadMakerID),
			MakeOrderUuid: o.MakeOrderUUID,
			CreatedAt:     o.CreatedAt.String(),
			UpdatedAt:     o.UpdatedAt.String(),
			Breads:        pbBreads,
		})
	}

	return &pb.MakerOrdersResponse{
		Maker: &pb.BreadMakerProto{
			Id:        int32(maker.ID),
			Name:      maker.Name,
			Email:     maker.Email,
			CreatedAt: maker.CreatedAt.String(),
			UpdatedAt: maker.UpdatedAt.String(),
		},
		Orders: pbOrders,
	}, nil
}

func (s *AdminServiceServer) GetAllOrders(ctx context.Context, in *pb.Empty) (*pb.BuyOrderList, error) {
	orders, err := s.RabbitMQBakery.Repo.GetAllBuyOrders()
	if err != nil {
		log.Errorf("Error getting all orders: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get orders: %v", err)
	}

	var pbOrders []*pb.BuyOrder
	var pbDetails []*pb.BuyOrderDetails
	for _, o := range orders {
		totalCost, _ := s.RabbitMQBakery.Repo.GetOrderTotalCost(o.ID)
		pbOrders = append(pbOrders, &pb.BuyOrder{
			Id:           int32(o.ID),
			CustomerId:   int32(o.CustomerID),
			BuyOrderUuid: o.BuyOrderUUID,
			TotalCost:    totalCost,
		})

		for _, b := range o.Breads {
			pbDetails = append(pbDetails, &pb.BuyOrderDetails{
				BuyOrderId:   int32(o.ID),
				BuyOrderUuid: o.BuyOrderUUID,
				BreadId:      int32(b.ID),
				Quantity:     int32(b.Quantity),
				Price:        b.Price,
				Status:       b.Status,
				CreatedAt:    o.CreatedAt.String(),
				UpdatedAt:    o.UpdatedAt.String(),
			})
		}
	}

	return &pb.BuyOrderList{
		BuyOrders:       pbOrders,
		BuyOrderDetails: pbDetails,
	}, nil
}

func (s *AdminServiceServer) GetAllMakeOrders(ctx context.Context, in *pb.Empty) (*pb.MakeOrderList, error) {
	orders, err := s.RabbitMQBakery.Repo.GetAllMakeOrders()
	if err != nil {
		log.Errorf("Error getting all make orders: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get make orders: %v", err)
	}

	var pbOrders []*pb.MakeOrderProto
	for _, o := range orders {
		var pbBreads []*pb.Bread
		for _, b := range o.Breads {
			pbBreads = append(pbBreads, &pb.Bread{
				Id:          int32(b.ID),
				Name:        b.Name,
				Price:       b.Price,
				Quantity:    int32(b.Quantity),
				Description: b.Description,
				Type:        b.Type,
				Image:       b.Image,
			})
		}
		pbOrders = append(pbOrders, &pb.MakeOrderProto{
			Id:            int32(o.ID),
			BreadMakerId:  int32(o.BreadMakerID),
			MakeOrderUuid: o.MakeOrderUUID,
			CreatedAt:     o.CreatedAt.String(),
			UpdatedAt:     o.UpdatedAt.String(),
			Breads:        pbBreads,
		})
	}

	return &pb.MakeOrderList{MakeOrders: pbOrders}, nil
}
