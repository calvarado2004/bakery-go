package main

import (
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"os"
	"time"
)

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

var activemqAddress = os.Getenv("ACTIVEMQ_SERVICE_ADDR")

// Bread attributes
type BreadAttributes struct {
	id          int64
	price       float32
	description string
	typeName    string
}

// Map of bread types to attributes
var breadTypes = map[string]BreadAttributes{
	"Roll":        {id: 1, price: 1.0, description: "Roll bread, delicious!", typeName: "Salty"},
	"Baguette":    {id: 2, price: 1.5, description: "A baguette is always a good idea", typeName: "Sandwich"},
	"Sourdough":   {id: 3, price: 2.0, description: "Sourdough bread, yum!", typeName: "Sandwich"},
	"Rye":         {id: 4, price: 2.5, description: "Rye is the best!", typeName: "Salty"},
	"Whole Wheat": {id: 5, price: 2.0, description: "Whole wheat bread, delicious!", typeName: "Sandwich"},
	"Multigrain":  {id: 6, price: 1.5, description: "Multigrain bread, the healthy option", typeName: "Sandwich"},
	"Pita":        {id: 7, price: 1.2, description: "Pita bread, for your Middle eastern food.", typeName: "Salty"},
	"Naan":        {id: 8, price: 1.3, description: "Naan bread, for your Indian food.", typeName: "Salty"},
	"Focaccia":    {id: 9, price: 2.3, description: "For your Italian food, nothing better than a Focaccia.", typeName: "Salty"},
	"Ciabatta":    {id: 10, price: 1.8, description: "Cia-batta-bing, cia-batta-boom!", typeName: "Sandwich"},
}

func main() {
	conn, err := grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Could not connect to %v", err)
	}

	log.Printf("Connected to %v", gRPCAddress)

	breadClient := pb.NewMakeBreadClient(conn)

	defer func(client *grpc.ClientConn) {
		err := client.Close()
		if err != nil {
			log.Println("error closing client: ", err)
		}
	}(conn)

	// Start with zero bread made
	totalBreadMade := 0

	// Get the list of bread types for random selection
	breadTypeKeys := make([]string, 0, len(breadTypes))
	for k := range breadTypes {
		breadTypeKeys = append(breadTypeKeys, k)
	}

	for {
		// Sleep for a bit before checking the queue again
		time.Sleep(1 * time.Second)

		// Call CheckBreadQueue
		queueSize, err := CheckBreadQueue()
		if err != nil {
			log.Println("error checking bread queue: ", err)
			continue
		}

		log.Printf("Current bread queue size: %v", queueSize)

		// Check if the bread queue is below 50
		if queueSize < 80 {
			// Start making bread again
			totalBreadMade = 0

			for totalBreadMade < 100 {
				totalBreadMade += makeSomeBread(breadTypeKeys, breadTypes, breadClient)
			}
		}

	}
}
