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
	queue, err := rabbitmqChannel.QueueInspect("bread-in-bakery") // inspect the queue
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to inspect the queue: %v", err)
	}

	numberOfMessages := queue.Messages

	bread := &pb.Bread{
		Quantity: int32(numberOfMessages),
	}

	breadList := pb.BreadList{
		Breads: []*pb.Bread{bread},
	}

	breadsResponse := &pb.BreadResponse{Breads: &breadList}
	// using the number of messages in the queue

	return breadsResponse, nil
}

func (s *CheckInventoryServer) CheckBreadInventoryStream(_ *pb.BreadRequest, stream pb.CheckInventory_CheckBreadInventoryStreamServer) error {

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

	breadDelivered := &pb.BreadList{}

	log.Println("Waiting for breads to be available on the bakery on stream...")

	for d := range msgs {
		log.Printf("Bread available on Bakery: %s", d.Body)

		bread := &pb.Bread{}
		err := json.Unmarshal(d.Body, bread)
		if err != nil {
			log.Printf("Failed to unmarshal bread data: %v", err)
			err := d.Nack(false, true)
			if err != nil {
				return err
			}
		}

		breadDelivered.Breads = append(breadDelivered.Breads, bread)

		breadResponse := &pb.BreadResponse{Breads: breadDelivered}

		err = d.Nack(false, true)
		if err != nil {
			return err
		}

		if err := stream.Send(breadResponse); err != nil {
			log.Printf("Failed to send bread data: %v", err)
			return err
		}

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

	getBuyResponse(responseCh)

	select {
	case response := <-responseCh:
		return response, nil

	case <-time.After(30 * time.Second):

		return nil, status.Errorf(codes.Internal, "Failed to get bread response after 30 seconds: %v", err)
	}

}

func (s *BuyBreadServer) BuyBreadStream(in *pb.BreadRequest, stream pb.BuyBread_BuyBreadStreamServer) error {

	breadsBought, err := rabbitmqChannel.Consume(
		"bread-bought", // queue
		"",             // consumer
		false,          // auto-ack
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to consume from bought breads queue: %v", err)
	}

	for d := range breadsBought {
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
