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

	sourdoughBread := &pb.Bread{
		Name:        "Sourdough",
		Description: "A delicious sourdough bread",
		Price:       4.99,
		Type:        "Salty",
		Quantity:    1,
	}

	rollBread := &pb.Bread{
		Name:        "Roll",
		Description: "A delicious roll",
		Price:       2.99,
		Type:        "Sweet",
		Quantity:    1,
	}

	naanBread := &pb.Bread{
		Name:        "Naan",
		Description: "A delicious naan bread",
		Price:       3.99,
		Type:        "Salty",
		Quantity:    1,
	}

	focacciaBread := &pb.Bread{
		Name:        "Focaccia",
		Description: "A delicious focaccia bread",
		Price:       5.99,
		Type:        "Salty",
		Quantity:    1,
	}

	breadList := pb.BreadList{
		Breads: []*pb.Bread{
			sourdoughBread,
			rollBread,
			naanBread,
			focacciaBread,
		},
	}

	request := pb.BreadRequest{
		Breads: &breadList,
	}

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

		log.Printf("Trying to Buy some bread: %v", breadList.Breads)

		for _, bread := range breadList.Breads {

			if budget < 70.00 {

				log.Printf("No more money to buy bread: %v", breadList.Breads)

				time.Sleep(10 * time.Second)

			} else {

				log.Printf("Buying bread: %v", bread)

				log.Printf("Current budget: %v", budget)

				buyBreadResponse, err := buyBreadClient.BuyBread(context.Background(), &request)
				if err != nil {
					log.Printf("Could not buy bread: %v", err)
				} else {
					log.Printf("Bread bought: %v", buyBreadResponse.GetBreads())
				}

				BreadsBought := buyBreadResponse.Breads

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
