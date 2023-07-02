package main

import (
	"calvarado2004/bakery-go/proto"
	"context"
	"encoding/json"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"math/rand"
)

const (
	address = "0.0.0.0:50051"
)

var (
	rabbitmqChannel *rabbitmq.Channel
	conn            *rabbitmq.Connection
	client          bread.BakeryBreadServiceClient
	allBreads       []string // Holds all bread types
)

func main() {
	var err error
	conn, err = rabbitmq.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Println("Failed to close RabbitMQ connection: ", err)
		}
	}(conn)

	rabbitmqChannel, err = conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer func(rabbitmqChannel *rabbitmq.Channel) {
		err := rabbitmqChannel.Close()
		if err != nil {
			log.Println("Failed to close RabbitMQ channel: ", err)
		}
	}(rabbitmqChannel)

	// Connect to the gRPC server
	grpcConn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer func(grpcConn *grpc.ClientConn) {
		err := grpcConn.Close()
		if err != nil {
			log.Println("Failed to close gRPC connection: ", err)
		}
	}(grpcConn)

	client = bread.NewBakeryBreadServiceClient(grpcConn)

	// Fetch the available breads from the server
	fetchAvailableBreads()

	// Start consuming the queue
	ConsumeBreadQueue()
}

// fetchAvailableBreads retrieves the available breads from the server and populates the allBreads slice.
func fetchAvailableBreads() {
	response, err := client.GetAvailableBreads(context.Background(), &bread.BakeryRequestList{})
	if err != nil {
		log.Fatalf("Failed to fetch available breads: %v", err)
	}

	for _, breads := range response.Breads {
		allBreads = append(allBreads, breads.Bread.Name)
	}
}

// RandomBread returns a random bread from allBreads.
func RandomBread() string {
	return allBreads[rand.Intn(len(allBreads))]
}

func BuyBread(breadToBuy string) {
	request := &bread.BuyRequest{
		Name:     breadToBuy,
		Quantity: 1,
	}

	for {
		response, err := client.BuyBreadFromQueue(context.Background(), request)
		if err != nil {
			log.Println("Error buying bread: ", err)
			continue
		}

		if response.Name == breadToBuy {
			log.Println("Bread bought: ", response.Name)
			break
		}
	}

}

func ConsumeBreadQueue() {
	err := rabbitmqChannel.Qos(1, 0, false) // This ensures that RabbitMQ will not give more than one message to this consumer at a time.
	if err != nil {
		log.Fatalf("Failed to set QoS: %v", err)
	}

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
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	forever := make(chan bool)

	// Declare a queue for bread that is not the one we want
	breadCheckedQueue, err := rabbitmqChannel.QueueDeclare(
		"bread-checked", // queue name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Declare a queue for bread that is not the one we want
	breadBoughtQueue, err := rabbitmqChannel.QueueDeclare(
		"bread-bought", // queue name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	go func() {

		for msg := range msgs {

			breadToBuy := RandomBread() // Get a random bread to buy

			receivedBread := &bread.Bread{}
			err = json.Unmarshal(msg.Body, receivedBread)
			if err != nil {
				err := msg.Nack(false, true)
				if err != nil {
					return
				} // requeue the message
				log.Fatalf("Failed to unmarshal bread data: %v", err)
			}

			if receivedBread.Name == breadToBuy {

				BuyBread(breadToBuy)

				log.Printf("Received bread %v, bought %v", receivedBread.Name, breadToBuy)

				err = msg.Ack(false)
				if err != nil {
					log.Println("Failed to ack message: ", err)
				}

				log.Println("Sending bread to bread-bought queue: ", receivedBread.Name)

				breadData, err := json.Marshal(&receivedBread)
				if err != nil {
					log.Println("Failed to marshal bread data: ", err)
				}

				err = rabbitmqChannel.Publish(
					"",                    // exchange
					breadBoughtQueue.Name, // routing key
					false,                 // mandatory
					false,                 // immediate
					rabbitmq.Publishing{
						ContentType: "text/plain",
						Body:        breadData,
					})
				if err != nil {
					log.Println("Failed to publish message: ", err)
				}

			} else {
				// This is not the bread we're looking for; requeue it and keep consuming
				err := msg.Nack(false, true)
				if err != nil {
					return
				}
				log.Printf("Received bread %v, but we want %v", receivedBread.Name, breadToBuy)

				log.Println("Sending bread to bread-checked queue: ", receivedBread.Name)

				breadData, err := json.Marshal(&receivedBread)
				if err != nil {
					log.Println("Failed to marshal bread data: ", err)
				}

				err = rabbitmqChannel.Publish(
					"",                     // exchange
					breadCheckedQueue.Name, // routing key
					false,                  // mandatory
					false,                  // immediate
					rabbitmq.Publishing{
						ContentType: "text/plain",
						Body:        breadData,
					})
				if err != nil {
					log.Println("Failed to publish bread data: ", err)
				}

			}

		}
	}()

	log.Printf(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever
}
