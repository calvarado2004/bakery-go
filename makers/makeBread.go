package main

import (
	"context"
	"fmt"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/streadway/amqp"
	"log"
	"math/rand"
	"time"
)

func MakeBread(client pb.MakeBreadClient, bread *pb.Bread) {

	request := &pb.BreadRequest{
		Bread: bread,
	}

	response, err := client.MakeBread(context.Background(), request)
	if err != nil {
		log.Println("error making bread: ", err)
	}

	log.Println("Bread made: ", response.Bread)

}

func makeSomeBread(breadTypeKeys []string, breadTypes map[string]BreadAttributes, breadClient pb.MakeBreadClient) int {
	// Randomly select a type of bread to make
	breadType := breadTypeKeys[rand.Intn(len(breadTypeKeys))]
	// Look up the attributes for this bread type
	breadAttributes := breadTypes[breadType]
	// Randomly set the quantity of bread to make (up to 10 at a time)
	quantity := rand.Int63n(10) + 1

	bread := &pb.Bread{
		Name:     breadType,
		Id:       breadAttributes.id,
		Type:     "Salty",
		Quantity: quantity,
		Price:    breadAttributes.price,
		Message:  "Freshly baked " + breadType + "!",
		Error:    "",
	}

	log.Println("Making bread: ", bread)

	MakeBread(breadClient, bread)

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

	// Get the queue
	q, err := ch.QueueInspect("bread-queue")
	if err != nil {
		return 0, fmt.Errorf("failed to inspect queue: %v", err)
	}

	// Return the number of messages in the queue
	return q.Messages, nil
}
