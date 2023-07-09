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

var (
	checkInventoryClient pb.CheckInventoryClient
	buyBreadClient       pb.BuyBreadClient
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

	buyBreadClient = pb.NewBuyBreadClient(conn)

	pretzelBread := &pb.Bread{
		Name:     "Pretzel",
		Quantity: 2,
	}

	baguetteBread := &pb.Bread{
		Name:     "Baguette",
		Quantity: 2,
	}

	cinnamonBread := &pb.Bread{
		Name:     "Cinnamon Roll",
		Quantity: 2,
	}

	croissantBread := &pb.Bread{
		Name:     "Croissant",
		Quantity: 2,
	}

	breadList := pb.BreadList{
		Breads: []*pb.Bread{
			pretzelBread,
			baguetteBread,
			cinnamonBread,
			croissantBread,
		},
	}

	request := pb.BreadRequest{
		Breads: &breadList,
	}

	log.Printf("Trying to buy bread: %v", request.Breads.Breads)

	response, err := buyBreadClient.BuyBread(context.Background(), &request)
	if err != nil {
		log.Fatalf("Failed to buy bread: %v", err)
	}

	time.Sleep(30 * time.Second)

	log.Printf("Bread bought: %v", response.Breads.Breads)

	time.Sleep(5 * time.Second)

}
