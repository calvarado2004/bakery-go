package main

import (
	"context"
	"encoding/json"
	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"time"
)

func (s *MakeBreadServer) BakeBread(_ context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadsToMake := in.Breads.GetBreads()

	var breadMade pb.BreadList

	for _, bread := range breadsToMake {
		log.Println("Bread to make", bread.Name)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		err = rabbitmqChannel.Publish(
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

	breadsToMake := in.Breads.GetBreads()

	var breadMade pb.BreadList

	for _, bread := range breadsToMake {
		log.Println("Sending fresh bread to bakery", bread.Name)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		bread.Status = "bread ready to consume"

		err = rabbitmqChannel.Publish(
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

	msgs, err := rabbitmqChannel.Consume(
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

	breads, err := s.Repo.GetAvailableBread()
	if err != nil {
		return nil, err
	}

	breadsResponse := &pb.BreadResponse{}

	breadList := &pb.BreadList{}

	for _, bread := range breads {
		breadResponse := &pb.Bread{}
		breadResponse.Name = bread.Name
		breadResponse.Quantity = int32(bread.Quantity)
		breadResponse.Status = bread.Status
		breadResponse.CreatedAt = bread.CreatedAt.String()
		breadResponse.UpdatedAt = bread.UpdatedAt.String()
		breadResponse.Description = bread.Description
		breadResponse.Price = bread.Price
		breadResponse.Image = bread.Image
		breadResponse.Type = bread.Type
		breadResponse.Id = int32(bread.ID)
		breadList.Breads = append(breadList.Breads, breadResponse)

	}

	return breadsResponse, nil
}

func (s *CheckInventoryServer) CheckBreadInventoryStream(_ *pb.BreadRequest, stream pb.CheckInventory_CheckBreadInventoryStreamServer) error {

	breads, err := s.Repo.GetAvailableBread()
	if err != nil {
		return err
	}

	breadsResponse := &pb.BreadResponse{}

	breadList := &pb.BreadList{}

	for _, bread := range breads {
		breadResponse := &pb.Bread{}
		breadResponse.Name = bread.Name
		breadResponse.Quantity = int32(bread.Quantity)
		breadResponse.Status = bread.Status
		breadResponse.CreatedAt = bread.CreatedAt.String()
		breadResponse.UpdatedAt = bread.UpdatedAt.String()
		breadResponse.Description = bread.Description
		breadResponse.Price = bread.Price
		breadResponse.Image = bread.Image
		breadResponse.Type = bread.Type
		breadResponse.Id = int32(bread.ID)
		breadList.Breads = append(breadList.Breads, breadResponse)

	}

	// Send the response to the client
	if err := stream.Send(breadsResponse); err != nil {
		return err
	}

	return nil
}

func (s *BuyBreadServer) BuyBread(cx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadsToBuy := in.Breads.GetBreads()

	buyOrder := data.BuyOrder{}

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

	err = rabbitmqChannel.Publish(
		"",                // exchange
		"buy-bread-order", // routing key
		false,             // mandatory
		false,             // immediate
		rabbitmq.Publishing{
			ContentType:  "text/json",
			Body:         orderData,
			DeliveryMode: rabbitmq.Persistent,
		})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to add bread order to queue: %v", err)
	}

	responseCh := make(chan *pb.BreadResponse)

	// Execute the function getBuyResponse as a goroutine and pass the response channel
	getBuyResponse(responseCh)

	// Create an array to store the bought breads
	boughtBreads := make([]*pb.Bread, len(buyOrder.Breads))

	// Convert each bought bread to the protobuf version and store in the array
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

	// Include the bought breads in the response
	return &pb.BreadResponse{
		Message: "Bread buying process started, you'll receive the order that will be settled later.",
		Breads:  &pb.BreadList{Breads: boughtBreads},
	}, nil
}

func (s *BuyBreadServer) BuyBreadStream(in *pb.BreadRequest, stream pb.BuyBread_BuyBreadStreamServer) error {

	// Make the response channel
	responseCh := make(chan *pb.BreadResponse)

	// Start the goroutine to listen for bread buy responses
	go getBuyResponse(responseCh)

	// Continuously listen for updates on the responseCh and stream them to the client
	for {
		select {
		case response, ok := <-responseCh:
			if !ok {
				// If the channel has been closed, return
				return nil
			}
			// Send the response to the client
			if err := stream.Send(response); err != nil {
				return err
			}
		case <-stream.Context().Done():
			// If the client has closed the connection, return
			return stream.Context().Err()
		}
	}
}

func (s *RemoveOldBreadServer) RemoveBread(cx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadToRemove := in.Breads.GetBreads()
	var breadRemoved pb.BreadList

	for _, bread := range breadToRemove {
		log.Println("Bread to remove", &bread)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		err = rabbitmqChannel.Publish(
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

	breadsRemoved, err := rabbitmqChannel.Consume(
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
			return err
		}
	}

	return nil
}
