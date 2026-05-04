package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/google/uuid"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

func (s *MakeBreadServer) BakeBread(_ context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	connection, err := rabbitmq.Dial(s.RabbitMQBakery.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return nil, err
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return nil, err
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	breadsToMake := in.Breads.GetBreads()

	var breadMade pb.BreadList

	for _, bread := range breadsToMake {
		log.Println("Bread to make", bread.Name)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		err = channel.Publish(
			"",              // exchange
			"bread-to-make", // routing key
			false,           // mandatory
			false,           // immediate
			rabbitmq.Publishing{
				ContentType:  "text/json",
				Body:         breadData,
				DeliveryMode: rabbitmq.Persistent,
			})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
		}

		breadMade.Breads = append(breadMade.Breads, bread)

	}

	return &pb.BreadResponse{Breads: &breadMade}, nil

}

func (s *MakeBreadServer) SendBreadToBakery(_ context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	connection, err := rabbitmq.Dial(s.RabbitMQBakery.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return nil, err
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return nil, err
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	breadsToMake := in.Breads.GetBreads()

	var breadMade pb.BreadList

	for _, bread := range breadsToMake {
		log.Println("Sending fresh bread to bakery", bread.Name)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		bread.Status = "bread ready to consume"

		err = channel.Publish(
			"",                // exchange
			"bread-in-bakery", // routing key
			false,             // mandatory
			false,             // immediate
			rabbitmq.Publishing{
				ContentType:  "text/json",
				Body:         breadData,
				DeliveryMode: rabbitmq.Persistent,
			})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
		}

		breadMade.Breads = append(breadMade.Breads, bread)

	}

	return &pb.BreadResponse{Breads: &breadMade}, nil

}

func (s *MakeBreadServer) MadeBreadStream(_ *pb.BreadRequest, stream pb.MakeBread_MadeBreadStreamServer) error {

	connection, err := rabbitmq.Dial(s.RabbitMQBakery.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return err
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return err
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	msgs, err := channel.Consume(
		"bread-in-bakery", // queue
		"",                // consumer
		false,             // auto-ack
		false,             // exclusive
		false,             // no-local
		false,             // no-wait
		nil,               // args
	)
	if err != nil {

		return status.Errorf(codes.Internal, "Failed to consume from updates queue: %v", err)
	}

	var breadDelivered pb.BreadList

	for d := range msgs {
		bread := &pb.Bread{}
		err := json.Unmarshal(d.Body, bread)
		if err != nil {
			err := d.Nack(false, true)
			if err != nil {
				log.Errorf("Failed to unmarshal bread data: %v", err)
				return err
			}
			return status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		breadDelivered.Breads = append(breadDelivered.Breads, bread)

		breadResponse := &pb.BreadResponse{Breads: &breadDelivered}

		if err := stream.Send(breadResponse); err != nil {
			err := d.Nack(false, true)
			if err != nil {
				return err
			}
			return err
		}

		err = d.Ack(false)
		if err != nil {
			return err
		}

	}

	return nil

}

func (s *CheckInventoryServer) CheckBreadInventory(cx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breads, err := s.RabbitMQBakery.Repo.GetAvailableBread()
	if err != nil {
		log.Println("Error getting breads", err)
		return nil, err
	}

	if len(breads) == 0 {
		return nil, status.Errorf(codes.NotFound, "No breads found (CheckBreadInventory)")
	}

	breadsResponse := pb.BreadResponse{}

	breadList := pb.BreadList{}

	for _, bread := range breads {
		breadgRPC := pb.Bread{}
		breadgRPC.Name = bread.Name
		breadgRPC.Quantity = int32(bread.Quantity)
		breadgRPC.Status = bread.Status
		breadgRPC.CreatedAt = timestamppb.New(bread.CreatedAt)
		breadgRPC.UpdatedAt = timestamppb.New(bread.UpdatedAt)
		breadgRPC.Description = bread.Description
		breadgRPC.Price = bread.Price
		breadgRPC.Image = bread.Image
		breadgRPC.Type = bread.Type
		breadgRPC.Id = int32(bread.ID)
		breadList.Breads = append(breadList.Breads, &breadgRPC)

	}

	breadsResponse.Breads = &breadList

	return &breadsResponse, nil
}

func (s *CheckInventoryServer) CheckBreadInventoryStream(_ *pb.BreadRequest, stream pb.CheckInventory_CheckBreadInventoryStreamServer) error {

	for {
		breads, err := s.RabbitMQBakery.Repo.GetAvailableBread()
		if err != nil {
			return err
		}

		if len(breads) == 0 {
			return status.Errorf(codes.NotFound, "No breads found (CheckBreadInventoryStream)")
		}

		for _, bread := range breads {
			breadgRPC := pb.Bread{}
			breadgRPC.Name = bread.Name
			breadgRPC.Quantity = int32(bread.Quantity)
			breadgRPC.Status = bread.Status
			breadgRPC.CreatedAt = timestamppb.New(bread.CreatedAt)
			breadgRPC.UpdatedAt = timestamppb.New(bread.UpdatedAt)
			breadgRPC.Description = bread.Description
			breadgRPC.Price = bread.Price
			breadgRPC.Image = bread.Image
			breadgRPC.Type = bread.Type
			breadgRPC.Id = int32(bread.ID)

			breadsResponse := pb.BreadResponse{
				Breads: &pb.BreadList{
					Breads: []*pb.Bread{&breadgRPC},
				},
			}

			// Send the response to the client
			if err := stream.Send(&breadsResponse); err != nil {
				return err
			}
		}

		// Sleep for 15 seconds before next inventory check
		time.Sleep(15 * time.Second)
	}
}

// BuyBread is a server-streaming RPC to buy bread
func (s *BuyBreadServer) BuyBread(ctx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	connection, err := rabbitmq.Dial(s.RabbitMQBakery.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return nil, err
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return nil, err
	}

	buyOrder := data.BuyOrder{}

	if in.BuyOrderUuid != "" {
		buyOrder.BuyOrderUUID = in.BuyOrderUuid
	} else {
		buyOrder.BuyOrderUUID = uuid.NewString()
	}

	// Assign a monotonically increasing sequence number for matching engine priority.
	buyOrder.SequenceNumber = orderSequence.Add(1)

	// Pass through buyer's matching engine preferences.
	if in.Preferences != nil {
		buyOrder.BidPrice = in.Preferences.BidPrice
		buyOrder.AllowPartial = in.Preferences.AllowPartial
		buyOrder.SkipUnavailableItems = in.Preferences.SkipUnavailableItems
	}

	breadsToBuy := in.Breads.GetBreads()

	buyerCustomer := data.Customer{
		ID:        1,
		Name:      "John Doe",
		Email:     "john@doe.com",
		CreatedAt: time.Now(),
	}

	buyOrder.CustomerID = 1
	buyOrder.Customer = buyerCustomer

	for _, bread := range breadsToBuy {
		log.Println("Buying bread", bread.Name)
		breadDB := data.Bread{}
		breadDB.Name = bread.Name
		breadDB.Quantity = int(bread.Quantity)
		breadDB.Description = bread.Description
		breadDB.Price = bread.Price
		breadDB.Image = bread.Image
		breadDB.Type = bread.Type
		breadDB.UpdatedAt = time.Now()
		breadDB.ID = int(bread.Id)
		breadDB.Status = "Bought"
		buyOrder.Breads = append(buyOrder.Breads, breadDB)
	}

	orderData, err := json.Marshal(buyOrder)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to marshal order: %v", err)
	}

	select {
	case <-ctx.Done():
		// If the context is cancelled, return an error
		return nil, status.Error(codes.Canceled, "Request canceled by client")
	default:
		err = channel.Publish(
			"",
			"buy-bread-order",
			false,
			false,
			rabbitmq.Publishing{
				ContentType:  "text/json",
				Body:         orderData,
				DeliveryMode: rabbitmq.Persistent,
			})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to add bread order to queue: %v", err)
		}
		defer func(ch *rabbitmq.Channel) {
			err := ch.Close()
			if err != nil {
				log.Errorf("Failed to close channel: %v", err)
			}
		}(channel)

		boughtBreads := make([]*pb.Bread, len(buyOrder.Breads))
		for i, boughtBread := range buyOrder.Breads {
			boughtBreads[i] = &pb.Bread{
				Name:        boughtBread.Name,
				Description: boughtBread.Description,
				Price:       boughtBread.Price,
				Quantity:    int32(boughtBread.Quantity),
				Type:        boughtBread.Type,
				Image:       boughtBread.Image,
				Status:      boughtBread.Status,
				Id:          int32(boughtBread.ID),
			}
		}

		return &pb.BreadResponse{
			Message:    fmt.Sprintf("Bread buying process started, you'll receive the order that will be settled later. Buy order ID: %v", buyOrder.ID),
			Breads:     &pb.BreadList{Breads: boughtBreads},
			BuyOrderId: int32(buyOrder.ID),
		}, nil
	}
}

// BuyBreadStream waits for the broker to settle the order and streams the
// result back to the buyer.
//
// Architecture (electronic-market pattern):
//   1. Register with the central SettlementDispatcher (waits for AMQP
//      "bread-bought" messages from the broker).
//   2. If the AMQP message arrives, send the settlement immediately.
//   3. If the AMQP message is lost, fall back to a single DB poll after
//      a short grace period.
//
// This scales: one AMQP consumer serves N concurrent streams, with zero
// DB polling per stream under normal conditions.
func (s *BuyBreadServer) BuyBreadStream(in *pb.BreadRequest, stream pb.BuyBread_BuyBreadStreamServer) error {
	ctx := stream.Context()
	uuid := in.BuyOrderUuid

	log.Printf("BuyBreadStream started for order %s", uuid)

	dispatcher := s.RabbitMQBakery.settlementDispatcher
	if dispatcher == nil {
		log.Error("BuyBreadStream: settlement dispatcher not initialized")
		return status.Error(codes.Internal, "settlement dispatcher not ready")
	}

	// Register with the central dispatcher.
	waiter := dispatcher.Register(uuid)
	defer dispatcher.Unregister(uuid)

	var order *data.BuyOrder
	select {
	case o := <-waiter:
		if o == nil {
			// Dispatcher closed the channel without a settlement.
			// This means the notification arrived but the DB lookup failed.
			log.Warnf("BuyBreadStream: dispatcher closed without settlement for order %s (DB lookup failed)", uuid)
			return status.Errorf(codes.Internal, "order %s settled but could not be retrieved", uuid)
		}
		log.Printf("BuyBreadStream: received AMQP settlement for order %s", uuid)
		order = o
	case <-ctx.Done():
		log.Warnf("BuyBreadStream: context cancelled for order %s before AMQP settlement", uuid)
		return status.Errorf(codes.DeadlineExceeded, "order settlement timed out: %v", ctx.Err())
	case <-time.After(12 * time.Second):
		// AMQP message didn't arrive (lost message, or broker is slow).
		// Single DB fallback — no continuous polling.
		log.Warnf("BuyBreadStream: AMQP settlement missing for order %s, falling back to DB", uuid)
		order = s.fallbackGetSettledOrder(ctx, uuid)
		if order == nil {
			return status.Errorf(codes.DeadlineExceeded, "order %s not settled within timeout", uuid)
		}
	}

	totalCost, err := s.RabbitMQBakery.Repo.GetOrderTotalCost(order.ID)
	if err != nil {
		log.Errorf("BuyBreadStream: failed to get total cost for order %s: %v", uuid, err)
		return status.Errorf(codes.Internal, "failed to get order total cost: %v", err)
	}

	log.Printf("BuyBreadStream: sending settled response for order %s (total=$%.2f)", uuid, totalCost)

	return stream.Send(&pb.BreadResponse{
		Message:      fmt.Sprintf("Order %v settled, total cost $%.2f", order.BuyOrderUUID, totalCost),
		BuyOrderId:   int32(order.ID),
		BuyOrderUuid: order.BuyOrderUUID,
	})
}

// fallbackGetSettledOrder checks the DB once for a settled order.
// Returns nil if the order is not yet settled.
func (s *BuyBreadServer) fallbackGetSettledOrder(ctx context.Context, uuid string) *data.BuyOrder {
	order, err := s.RabbitMQBakery.Repo.GetBuyOrderByUUID(uuid)
	if err != nil {
		log.Errorf("BuyBreadStream: DB fallback failed to get order %s: %v", uuid, err)
		return nil
	}
	if order.Status == "Processed" || order.Status == "Failed" {
		log.Printf("BuyBreadStream: DB fallback found order %s status=%s", uuid, order.Status)
		return &order
	}
	return nil
}

func (s *BuyOrderServiceServer) BuyOrder(cx context.Context, in *pb.BuyOrderRequest) (*pb.BuyOrderResponse, error) {

	// Retrieve BuyOrder by UUID
	buyOrderByUUID, err := s.RabbitMQBakery.Repo.GetBuyOrderByUUID(in.BuyOrderUuid)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get buy order by UUID: %v", err)
	}

	// Retrieve BuyOrder total cost
	totalCost, err := s.RabbitMQBakery.Repo.GetOrderTotalCost(buyOrderByUUID.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get total cost: %v", err)
	}

	// Convert breads to proto Bread
	breads := make([]*pb.Bread, len(buyOrderByUUID.Breads))
	for i, bread := range buyOrderByUUID.Breads {
		breads[i] = &pb.Bread{
			Name:        bread.Name,
			Description: bread.Description,
			Price:       bread.Price,
			Quantity:    int32(bread.Quantity),
			Type:        bread.Type,
			Image:       bread.Image,
			Status:      bread.Status,
			Id:          int32(bread.ID),
		}
	}

	// Create BuyOrderDetails
	details := make([]*pb.BuyOrderDetails, len(breads))
	for _, bread := range breads {
		details = append(details, &pb.BuyOrderDetails{
			BreadId:      bread.Id,
			Quantity:     bread.Quantity,
			Price:        bread.Price,
			Status:       bread.Status,
			BuyOrderId:   int32(buyOrderByUUID.ID),
			BuyOrderUuid: buyOrderByUUID.BuyOrderUUID,
			CreatedAt:    timestamppb.New(buyOrderByUUID.CreatedAt),
			UpdatedAt:    timestamppb.New(buyOrderByUUID.UpdatedAt),
		})
	}

	// Create BuyOrder
	buyOrders := make([]*pb.BuyOrder, 1)
	buyOrders[0] = &pb.BuyOrder{
		BuyOrderUuid: buyOrderByUUID.BuyOrderUUID,
		Id:           int32(buyOrderByUUID.ID),
		CustomerId:   int32(buyOrderByUUID.CustomerID),
		TotalCost:    totalCost,
	}

	// Create BuyOrderList
	buyOrdersResponse := &pb.BuyOrderList{
		BuyOrderDetails: details,
		BuyOrders:       buyOrders,
	}

	// Create BuyOrderResponse
	buyOrderResponse := &pb.BuyOrderResponse{
		BuyOrders: buyOrdersResponse,
	}

	return buyOrderResponse, nil

}

func (s *BuyOrderServiceServer) BuyOrderStream(in *pb.BuyOrderRequest, stream pb.BuyOrderService_BuyOrderStreamServer) error {

	var buyOrdersToProcess []data.BuyOrder // declare as slice
	var err error                          // declare error variable here to use it throughout the function

	if in.BuyOrderUuid != "" {
		// Retrieve BuyOrder by UUID
		buyOrder, err := s.RabbitMQBakery.Repo.GetBuyOrderByUUID(in.BuyOrderUuid)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to get buy order by UUID: %v", err)
		}
		buyOrdersToProcess = append(buyOrdersToProcess, buyOrder) // add the single order to the slice

	} else {
		// If no UUID is provided, get all orders
		buyOrdersToProcess, err = s.RabbitMQBakery.Repo.GetAllBuyOrders() // directly assign the slice of orders
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to get all buy orders: %v", err)
		}
	}

	for _, buyOrderToProcess := range buyOrdersToProcess {

		// Retrieve BuyOrder total cost
		totalCost, err := s.RabbitMQBakery.Repo.GetOrderTotalCost(buyOrderToProcess.ID)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to get total cost: %v", err)
		}

		// Convert breads to proto Bread
		breads := make([]*pb.Bread, len(buyOrderToProcess.Breads))
		for i, bread := range buyOrderToProcess.Breads {
			breads[i] = &pb.Bread{
				Name:        bread.Name,
				Description: bread.Description,
				Price:       bread.Price,
				Quantity:    int32(bread.Quantity),
				Type:        bread.Type,
				Image:       bread.Image,
				Status:      bread.Status,
				Id:          int32(bread.ID),
			}
		}

		// Create BuyOrderDetails
		details := make([]*pb.BuyOrderDetails, len(breads))
		for _, bread := range breads {
			details = append(details, &pb.BuyOrderDetails{
				BreadId:      bread.Id,
				Quantity:     bread.Quantity,
				Price:        bread.Price,
				Status:       bread.Status,
				BuyOrderId:   int32(buyOrderToProcess.ID),
				BuyOrderUuid: buyOrderToProcess.BuyOrderUUID,
				CreatedAt:    timestamppb.New(buyOrderToProcess.CreatedAt),
				UpdatedAt:    timestamppb.New(buyOrderToProcess.UpdatedAt),
			})
		}

		// Create BuyOrder
		buyOrders := make([]*pb.BuyOrder, 1)
		buyOrders[0] = &pb.BuyOrder{
			BuyOrderUuid: buyOrderToProcess.BuyOrderUUID,
			Id:           int32(buyOrderToProcess.ID),
			CustomerId:   int32(buyOrderToProcess.CustomerID),
			TotalCost:    totalCost,
		}

		// Create BuyOrderList
		buyOrdersResponse := &pb.BuyOrderList{
			BuyOrderDetails: details,
			BuyOrders:       buyOrders,
		}

		// Create BuyOrderResponse
		buyOrderResponse := &pb.BuyOrderResponse{
			BuyOrders: buyOrdersResponse,
		}

		// Send the response to the client
		if err := stream.Send(buyOrderResponse); err != nil {
			return status.Errorf(codes.Internal, "Failed to send buy order: %v", err)
		}

	}

	return nil
}

func (s *RemoveOldBreadServer) RemoveBread(cx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	connection, err := rabbitmq.Dial(s.RabbitMQBakery.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return nil, err
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return nil, err
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	breadToRemove := in.Breads.GetBreads()
	var breadRemoved pb.BreadList

	for _, bread := range breadToRemove {
		log.Println("Bread to remove", &bread)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		err = channel.Publish(
			"",              // exchange
			"bread-removed", // routing key
			false,           // mandatory
			false,           // immediate
			rabbitmq.Publishing{
				ContentType:  "text/json",
				Body:         breadData,
				DeliveryMode: rabbitmq.Persistent,
			})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
		}

		breadRemoved.Breads = append(breadRemoved.Breads, bread)

	}

	return &pb.BreadResponse{Breads: &breadRemoved}, nil
}

func (s *RemoveOldBreadServer) RemoveBreadStream(in *pb.BreadRequest, stream pb.RemoveOldBread_RemoveBreadStreamServer) error {

	connection, err := rabbitmq.Dial(s.RabbitMQBakery.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return err
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return err
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	breadsRemoved, err := channel.Consume(
		"bread-removed", // queue
		"",              // consumer
		false,           // auto-ack
		false,           // exclusive
		false,           // no-local
		false,           // no-wait
		nil,             // args
	)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to consume from removed breads queue: %v", err)
	}

	for d := range breadsRemoved {
		bread := &pb.Bread{}
		err := json.Unmarshal(d.Body, bread)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		breadResponse := &pb.BreadResponse{Breads: &pb.BreadList{Breads: []*pb.Bread{bread}}}
		if err := stream.Send(breadResponse); err != nil {
			return err
		}

		err = d.Ack(false)
		if err != nil {
			log.Errorf("Failed to acknowledge message: %v", err)
			return err
		}
	}

	return nil
}
