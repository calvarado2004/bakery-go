package main

import (
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"time"
)

var address = "0.0.0.0:50051"

// Bread attributes
type BreadAttributes struct {
	id    int64
	price float32
}

// Map of bread types to attributes
var breadTypes = map[string]BreadAttributes{
	"Roll":        {id: 1, price: 1.0},
	"Baguette":    {id: 2, price: 1.5},
	"Sourdough":   {id: 3, price: 2.0},
	"Rye":         {id: 4, price: 2.5},
	"Whole Wheat": {id: 5, price: 2.0},
	"Multigrain":  {id: 6, price: 1.5},
	"Pita":        {id: 7, price: 1.2},
	"Naan":        {id: 8, price: 1.3},
	"Focaccia":    {id: 9, price: 2.3},
	"Ciabatta":    {id: 10, price: 1.8},
}

func main() {
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Could not connect to %v", err)
	}

	log.Printf("Connected to %v", address)

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
