package main

import (
	"context"
	"encoding/json"
	"fmt"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
)

var rabbitmqConnection *rabbitmq.Connection
var rabbitmqChannel *rabbitmq.Channel

func init() {
	var err error
	rabbitmqConnection, err = rabbitmq.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	rabbitmqChannel, err = rabbitmqConnection.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
}

func (s *MakeBreadServer) BuyBread(ctx context.Context, in *pb.BuyRequest) (*pb.BreadResponse, error) {
	msgs, err := rabbitmqChannel.Consume(
		"bread-queue", // queue
		"",            // consumer
		false,         // auto-ack
		false,         // exclusive
		false,         // no-local
		false,         // no-wait
		nil,           // args
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get bread from queue: %v", err)
	}

	select {
	case msg := <-msgs:
		bread := &pb.Bread{}
		err = json.Unmarshal(msg.Body, bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		log.Println("Bought bread from queue:", bread)
		return &pb.BreadResponse{Bread: bread}, nil
	case <-ctx.Done():
		return nil, status.Errorf(codes.Canceled, "Request canceled")
	}
}

func (s MakeBreadServer) MakeBread(ctx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadMade := pb.Bread{
		Name:     in.Bread.Name,
		Id:       in.Bread.Id,
		Type:     in.Bread.Type,
		Quantity: in.Bread.Quantity,
		Price:    in.Bread.Price,
		Message:  in.Bread.Message,
		Error:    in.Bread.Error,
	}

	log.Println("Bread to make", &breadMade)

	// Then, convert the bread data to JSON and add it to the RabbitMQ queue
	breadData, err := json.Marshal(&breadMade)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
	}

	// Declare the queue as durable
	queue, err := rabbitmqChannel.QueueDeclare(
		"bread-queue", // name
		true,          // durable
		false,         // delete when unused
		false,         // exclusive
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to declare a queue: %v", err)
	}

	err = rabbitmqChannel.Publish(
		"",         // exchange
		queue.Name, // routing key
		false,      // mandatory
		false,      // immediate
		rabbitmq.Publishing{
			ContentType: "text/plain",
			Body:        breadData,
		})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
	}

	resp := &pb.BreadResponse{Bread: &breadMade}
	return resp, nil

}

func (s *BakeryBreadServiceServer) BuyBreadFromQueue(ctx context.Context, in *pb.BuyRequest) (*pb.BuyResponse, error) {
	msgs, err := rabbitmqChannel.Consume(
		"bread-queue", // queue
		"",            // consumer
		false,         // auto-ack
		false,         // exclusive
		false,         // no-local
		false,         // no-wait
		nil,           // args
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get bread from queue: %v", err)
	}

	select {
	case msg := <-msgs:
		bread := &pb.Bread{}
		err = json.Unmarshal(msg.Body, bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		if bread.Quantity < in.GetQuantity() {
			return nil, status.Errorf(codes.InvalidArgument, "Not enough bread available: requested %v, available %v", in.GetQuantity(), bread.Quantity)
		}

		// Decrease the quantity of bread available
		bread.Quantity -= in.GetQuantity()

		log.Println("Bought bread from queue:", bread)
		return &pb.BuyResponse{Id: bread.Id, Name: bread.Name, Type: bread.Type, Quantity: in.GetQuantity(), Price: bread.Price}, nil
	case <-ctx.Done():
		return nil, status.Errorf(codes.Canceled, "Request canceled")
	}
}

func (s *BakeryBreadServiceServer) GetAvailableBreads(context.Context, *pb.BakeryRequestList) (*pb.BakeryResponseList, error) {
	// Create a connection to the RabbitMQ server
	conn, err := rabbitmq.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Create a channel
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %v", err)
	}
	defer ch.Close()

	// Declare a queue
	queue, err := ch.QueueDeclare(
		"bread-queue", // queue name
		true,          // durable
		false,         // delete when unused
		false,         // exclusive
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		return nil, fmt.Errorf("failed to declare a queue: %v", err)
	}

	// Get the number of messages in the queue
	queueSize := queue.Messages

	// Create a response with the available breads
	response := &pb.BakeryResponseList{}
	for i := 0; i < int(queueSize); i++ {
		// Consume a message from the queue
		msg, _, err := ch.Get(queue.Name, true)
		if err != nil {
			return nil, fmt.Errorf("failed to consume a message from the queue: %v", err)
		}

		// Unmarshal the message body into a Bread object
		bread := &pb.Bread{}
		err = json.Unmarshal(msg.Body, bread)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal bread data: %v", err)
		}

		breadResponse := &pb.BreadResponse{
			Bread:   bread,
			Message: "Bread available, buy it now!",
		}

		// Add the bread to the response
		response.Breads = append(response.Breads, breadResponse)
	}

	return response, nil
}
