package main

import (
	"context"
	"encoding/json"
	"fmt"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/streadway/amqp"
	"log"
	"math/rand"
	"strconv"
	"time"
)

func MakeBread(client pb.MakeBreadClient, bread *pb.Bread) error {

	breadList := pb.BreadList{
		Breads: []*pb.Bread{
			bread,
		},
	}

	request := &pb.BreadRequest{
		Breads: &breadList,
	}

	response, err := client.BakeBread(context.Background(), request)
	if err != nil {
		return err
	}

	log.Println("Breads made: ", response.GetBreads())

	return nil

}

func makeSomeBread(breadTypeKeys []string, breadTypes map[string]BreadAttributes, breadClient pb.MakeBreadClient) int {
	// Connect to RabbitMQ server
	conn, err := amqp.Dial(activemqAddress)
	if err != nil {
		log.Fatalf("failed to connect to RabbitMQ: %v", err)
	}
	defer func(conn *amqp.Connection) {
		err := conn.Close()
		if err != nil {
			log.Println("error closing connection: ", err)
		}
	}(conn)

	// Create a channel
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("failed to open a channel: %v", err)
	}
	defer func(ch *amqp.Channel) {
		err := ch.Close()
		if err != nil {
			log.Println("error closing channel: ", err)
		}
	}(ch)

	// Declare the queue
	_, err = ch.QueueDeclare(
		"bread-in-bakery", // queue name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Randomly select a type of bread to make
	breadType := breadTypeKeys[rand.Intn(len(breadTypeKeys))]
	// Look up the attributes for this bread type
	breadAttributes := breadTypes[breadType]
	// Randomly set the quantity of bread to make (up to 10 at a time)
	quantity := rand.Int63n(10) + 1

	timeNow := time.Unix(time.Now().Unix(), 0)

	bread := &pb.Bread{
		Name:        breadType,
		Id:          strconv.FormatInt(breadAttributes.id, 10),
		Type:        breadAttributes.typeName,
		Quantity:    int32(quantity),
		Description: breadAttributes.description,
		Price:       breadAttributes.price,
		CreatedAt:   timeNow.Format(time.RFC3339),
		Status:      "baking bread",
	}

	log.Println("Making bread: ", bread)

	// Make the bread
	err = MakeBread(breadClient, bread)
	if err != nil {
		log.Fatalf("Failed to make bread: %v", err)
	}

	// Update the status of the bread after it has been made
	bread.Status = "ready to sell"

	breadData, err := json.Marshal(&bread)
	if err != nil {
		log.Fatalf("Failed to marshal bread: %v", err)
	}

	// Publish the bread to the queue
	err = ch.Publish(
		"",                // exchange
		"bread-in-bakery", // routing key
		false,             // mandatory
		false,             // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         breadData,
			DeliveryMode: amqp.Persistent,
		})
	if err != nil {
		log.Fatalf("Failed to publish a message: %v", err)
	}

	// Sleep for a bit before making more bread
	time.Sleep(1 * time.Second)

	// Return the amount of bread made
	return int(quantity)
}

func CheckBreadQueue() (int, error) {

	// Connect to RabbitMQ server
	conn, err := amqp.Dial(activemqAddress)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to RabbitMQ: %v", err)
	}
	defer func(conn *amqp.Connection) {
		err := conn.Close()
		if err != nil {
			log.Println("error closing connection: ", err)
		}
	}(conn)

	// Create a channel
	ch, err := conn.Channel()
	if err != nil {
		return 0, fmt.Errorf("failed to open a channel: %v", err)
	}
	defer func(ch *amqp.Channel) {
		err := ch.Close()
		if err != nil {
			log.Println("error closing channel: ", err)
		}
	}(ch)

	// Declare the queue
	_, err = ch.QueueDeclare(
		"bread-in-bakery", // queue name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Get the queue
	q, err := ch.QueueInspect("bread-in-bakery")
	if err != nil {
		return 0, fmt.Errorf("failed to inspect queue: %v", err)
	}

	// Return the number of messages in the queue
	return q.Messages, nil
}
