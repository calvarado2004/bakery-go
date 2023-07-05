package main

import (
	"context"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"os"
	"time"
)

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

var activemqAddress = os.Getenv("ACTIVEMQ_SERVICE_ADDR")

var (
	rabbitmqChannel      *rabbitmq.Channel
	conn                 *rabbitmq.Connection
	checkInventoryClient pb.CheckInventoryClient
	buyBreadClient       pb.BuyBreadClient
	allBreads            []string // Holds all bread types
)

func main() {

	// Connect to the gRPC server
	grpcConn, err := grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer func(grpcConn *grpc.ClientConn) {
		err := grpcConn.Close()
		if err != nil {
			log.Println("Failed to close gRPC connection: ", err)
		}
	}(grpcConn)

	for true {

		buySomeBread(grpcConn)
	}

}

func buySomeBread(conn *grpc.ClientConn) {

	checkInventoryClient = pb.NewCheckInventoryClient(conn)
	buyBreadClient = pb.NewBuyBreadClient(conn)

	breadToBuy := []string{"Sourdough", "Roll", "Naan", "Focaccia"}

	breadsInInventory := 0

	inventory, err := checkInventoryClient.CheckBreadInventory(context.Background(), &pb.BreadRequest{})
	if err != nil {
		return
	}

	breads := inventory.Breads.GetBreads()

	for _, bread := range breads {
		breadsInInventory = int(bread.Quantity)
	}

	var budget float32

	budget = 100.00

	if breadsInInventory > 0 {

		log.Printf("Breads in inventory: %v", breadsInInventory)

		log.Printf("Trying to Buy some bread: %v", breadToBuy)

		for _, bread := range breadToBuy {

			if budget < 3.00 {

				log.Printf("No more money to buy bread: %v", breadToBuy)

				time.Sleep(10 * time.Second)

			} else {

				log.Printf("Buying bread: %v", bread)

				log.Printf("Current budget: %v", budget)

				breadToBuy := &pb.Bread{
					Name: bread,
				}

				breadList := &pb.BreadList{
					Breads: []*pb.Bread{breadToBuy},
				}

				breadToBuyRequest := &pb.BreadRequest{
					Breads: breadList,
				}

				buyBreadResponse, err := buyBreadClient.BuyBread(context.Background(), breadToBuyRequest)
				if err != nil {
					log.Printf("Could not buy bread: %v", err)
				} else {
					log.Printf("Bread bought: %v", buyBreadResponse.GetBreads())
				}

				BreadsBought := buyBreadResponse.GetBreads()

				for _, bread := range BreadsBought.Breads {
					budget = budget - bread.Price
				}

				time.Sleep(10 * time.Second)
			}

		}

		time.Sleep(10 * time.Second)

	} else {
		log.Printf("No breads in inventory: %v", breadsInInventory)

		time.Sleep(10 * time.Second)
	}

}
