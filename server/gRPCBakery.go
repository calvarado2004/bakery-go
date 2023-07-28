package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		breadgRPC.CreatedAt = bread.CreatedAt.String()
		breadgRPC.UpdatedAt = bread.UpdatedAt.String()
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
			breadgRPC.CreatedAt = bread.CreatedAt.String()
			breadgRPC.UpdatedAt = bread.UpdatedAt.String()
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
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	buyOrder := data.BuyOrder{}

	if in.BuyOrderUuid != "" {
		buyOrder.BuyOrderUUID = in.BuyOrderUuid
	} else {
		buyOrder.BuyOrderUUID = uuid.NewString()
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

		orderStatus := &OrderStatus{
			Ch:      make(chan *pb.BreadResponse, 1),
			Status:  "Processing",
			OrderId: buyOrder.ID,
		}

		s.RabbitMQBakery.mu.Lock()
		s.RabbitMQBakery.orders[buyOrder.ID] = orderStatus
		s.RabbitMQBakery.mu.Unlock()

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

// BuyBreadStream is a server stream function that returns the status of the order
func (s *BuyBreadServer) BuyBreadStream(in *pb.BreadRequest, stream pb.BuyBread_BuyBreadStreamServer) error {
	// Maximum number of retries before giving up
	maxRetries := 10
	retryCount := 0
	responseCh := make(chan *pb.BreadResponse)
	contextMaker := func() (context.Context, context.CancelFunc) {
		return context.WithCancel(context.Background())
	}

	// Start a go-routine to listen for RabbitMQ messages
	go func() {

		err := s.RabbitMQBakery.getBuyResponse(contextMaker, responseCh)
		if err != nil {
			log.Fatalf("Error getting buy response: %v", err)
		}
	}()

	for retryCount < maxRetries {
		time.Sleep(5 * time.Second) // wait for a while before checking again

		// Fetch the order from the database
		s.RabbitMQBakery.mu.Lock()
		savedOrder, err := s.RabbitMQBakery.Repo.GetBuyOrderByUUID(in.BuyOrderUuid)
		s.RabbitMQBakery.mu.Unlock()

		// if there was an error or the order is not found, try again
		if err != nil {
			log.Printf("Failed to get order from database: %v", err)
			retryCount++
			continue
		}

		// The order was found
		res := &pb.BreadResponse{
			Message:      fmt.Sprintf("Order %v was found in the database", savedOrder.BuyOrderUUID),
			BuyOrderId:   int32(savedOrder.ID),
			BuyOrderUuid: savedOrder.BuyOrderUUID,
		}
		if err := stream.Send(res); err != nil {
			return err
		}

		break // exit the loop once the order is found
	}

	// If the order was not found after all retries, return an error
	if retryCount == maxRetries {
		return fmt.Errorf("order not found after %v attempts", maxRetries)
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
