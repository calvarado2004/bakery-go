package main

import (
	"context"
	"encoding/json"
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
	rabbitmqConnection, err = rabbitmq.Dial(activemqAddress)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	rabbitmqChannel, err = rabbitmqConnection.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}

	// Declare the RabbitMQ bread-to-make queue as durable
	_, err = rabbitmqChannel.QueueDeclare(
		"bread-to-make", // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Declare the RabbitMQ bread-in-bakery queue as durable
	_, err = rabbitmqChannel.QueueDeclare(
		"bread-in-bakery", // name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Declare the RabbitMQ bread-bought queue as durable
	_, err = rabbitmqChannel.QueueDeclare(
		"bread-bought", // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Declare the RabbitMQ bread-removed queue as durable
	_, err = rabbitmqChannel.QueueDeclare(
		"bread-removed", // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}
}

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

	breadMade := in.Breads.GetBreads()

	var breadDelivered pb.BreadList

	for _, bread := range breadMade {
		log.Println("Bread to deliver to the bakery", &bread)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

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

		breadDelivered.Breads = append(breadDelivered.Breads, bread)

	}

	return &pb.BreadResponse{Breads: &breadDelivered}, nil

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
			return status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		breadDelivered.Breads = append(breadDelivered.Breads, bread)

		breadResponse := &pb.BreadResponse{Breads: &breadDelivered}

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

func (s *CheckInventoryServer) CheckBreadInventory(cx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadsResponse := &pb.BreadResponse{}

	breadsOnQueue, err := rabbitmqChannel.Consume(
		"bread-in-bakery", // queue
		"",                // consumer
		false,             // auto-ack
		false,             // exclusive
		false,             // no-local
		false,             // no-wait
		nil,               // args
	)
	if err != nil {
		return breadsResponse, status.Errorf(codes.Internal, "Failed to consume from updates queue: %v", err)
	}

	breadList := pb.BreadList{}

	breadsResponse.Breads = &breadList

	go func() {
		for d := range breadsOnQueue {
			log.Printf("Bread available on Bakery: %s", d.Body)

			bread := &pb.Bread{}
			err := json.Unmarshal(d.Body, bread)
			if err != nil {
				log.Printf("Failed to unmarshal bread data: %v", err)
			}

			err = d.Nack(false, true)
			if err != nil {
				return
			}

		}
	}()

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

	var breadBought pb.BreadList
	var breadToSendBack pb.BreadList

	breadsOnQueue, err := rabbitmqChannel.Consume(
		"bread-in-bakery", // queue
		"",                // consumer
		false,             // auto-ack
		false,             // exclusive
		false,             // no-local
		false,             // no-wait
		nil,               // args
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to consume from updates queue: %v", err)
	}

	for _, breadToBuy := range breadsToBuy {
		found := false
		for d := range breadsOnQueue {
			bread := &pb.Bread{}
			err := json.Unmarshal(d.Body, bread)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
			}

			// If the bread is found in the bakery
			if bread.Id == breadToBuy.Id {
				found = true
				breadBought.Breads = append(breadBought.Breads, bread)

				err = rabbitmqChannel.Publish(
					"",             // exchange
					"bread-bought", // routing key
					false,          // mandatory
					false,          // immediate
					rabbitmq.Publishing{
						ContentType:  "text/json",
						Body:         d.Body,
						DeliveryMode: rabbitmq.Persistent,
					})
				if err != nil {
					return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
				}

				// Acknowledge the message from the bread-in-bakery queue
				err := d.Ack(false)
				if err != nil {
					return nil, err
				}
			}
		}

		if !found {
			breadToSendBack.Breads = append(breadToSendBack.Breads, breadToBuy)
		}
	}

	for _, bread := range breadToSendBack.Breads {
		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

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
			return nil, status.Errorf(codes.Internal, "Failed to add bread back to queue: %v", err)
		}
	}

	return &pb.BreadResponse{Breads: &breadBought}, nil
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
