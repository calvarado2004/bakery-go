package main

import (
	"context"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"os"
	"time"
)

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

func main() {
	conn, err := grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Could not connect to %v", err)
	}

	log.Printf("Connected to %v", gRPCAddress)

	defer func(client *grpc.ClientConn) {
		err := client.Close()
		if err != nil {
			log.Println("error closing client: ", err)
		}
	}(conn)

	timeNow := time.Unix(time.Now().Unix(), 0).UTC().Format(time.UnixDate)

	sourdoughBread := &pb.Bread{
		Name:        "Sourdough",
		Description: "A delicious sourdough bread",
		Price:       4.99,
		Type:        "Salty",
		CreatedAt:   timeNow,
		Quantity:    1,
	}

	rollBread := &pb.Bread{
		Name:        "Roll",
		Description: "A delicious roll",
		Price:       2.99,
		Type:        "Sweet",
		CreatedAt:   timeNow,
		Quantity:    1,
	}

	naanBread := &pb.Bread{
		Name:        "Naan",
		Description: "A delicious naan bread",
		Price:       3.99,
		Type:        "Salty",
		CreatedAt:   timeNow,
		Quantity:    1,
	}

	focacciaBread := &pb.Bread{
		Name:        "Focaccia",
		Description: "A delicious focaccia bread",
		Price:       5.99,
		Type:        "Salty",
		CreatedAt:   timeNow,
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

	for true {

		makeSomeBread(conn, &request)

	}

}

func makeSomeBread(conn *grpc.ClientConn, request *pb.BreadRequest) {

	breadClient := pb.NewMakeBreadClient(conn)

	checkInventory := pb.NewCheckInventoryClient(conn)

	breadList := request.GetBreads()

	// Start with zero bread made
	totalBreadMade := 0

	maxBread := 100

	inventory, err := checkInventory.CheckBreadInventory(context.Background(), &pb.BreadRequest{})
	if err != nil {
		return
	}

	breads := inventory.Breads.GetBreads()

	breadsInInventory := 0

	for _, bread := range breads {
		breadsInInventory = int(bread.Quantity)
	}

	log.Printf("Breads in inventory: %v", breadsInInventory)

	if breadsInInventory >= 100 {

		log.Printf("Bakery has enough bread in inventory: %v", breads)

		time.Sleep(10 * time.Second)

		return

	} else {

		log.Printf("Bakery does not have enough bread in inventory: %v", breads)

		for totalBreadMade < maxBread {

			log.Printf("Making bread... %s", breadList.GetBreads())

			breadMadeList, err := breadClient.SendBreadToBakery(context.Background(), request)
			if err != nil {
				return
			}

			breadsMade := len(breadMadeList.Breads.GetBreads())

			totalBreadMade += breadsMade

			log.Printf("Bread made in this batch: %v, Total bread made so far: %v", breadsMade, totalBreadMade)

			time.Sleep(2 * time.Second)

		}

		log.Printf("Total bread made after reaching the limit: %v", totalBreadMade)

		time.Sleep(10 * time.Second)

	}
}
