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
	buyBreadClient pb.BuyBreadClient
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
		log.Println("Iterating to buy bread...")
		time.Sleep(15 * time.Second)
		buySomeBread(grpcConn)
	}

}

func buySomeBread(conn *grpc.ClientConn) {

	buyBreadClient = pb.NewBuyBreadClient(conn)

	pretzelBread := &pb.Bread{
		Name:        "Pretzel",
		Quantity:    2,
		Price:       2.49,
		Description: "Pretzel, a classic bakery bread with a salty taste",
		Type:        "Salty Bread",
		Status:      "available",
		Image:       "https://cdn.pixabay.com/photo/2017/09/05/17/18/pretzel-2718477_1280.jpg",
		Id:          4,
	}

	baguetteBread := &pb.Bread{
		Name:        "Baguette",
		Quantity:    2,
		Price:       1.49,
		Description: "Baguette, a classic bakery bread with a long shape",
		Type:        "French Bread",
		Status:      "available",
		Image:       "hhttps://cdn.pixabay.com/photo/2017/06/23/23/57/bread-2436370_1280.jpg",
		Id:          3,
	}

	cinnamonBread := &pb.Bread{
		Name:        "Cinnamon Roll",
		Quantity:    2,
		Price:       2.99,
		Description: "Cinnamon Roll, a classic bakery bread with cinnamon and sugar",
		Type:        "Sweet Bread",
		Status:      "available",
		Image:       "https://cdn.pixabay.com/photo/2019/12/25/17/55/cinnamon-roll-4719023_1280.jpg",
		Id:          1,
	}

	croissantBread := &pb.Bread{
		Name:        "Croissant",
		Quantity:    1,
		Price:       1.19,
		Description: "Croissant, a classic bakery bread with a buttery taste",
		Type:        "Buttery Bread",
		Status:      "available",
		Image:       "https://cdn.pixabay.com/photo/2012/02/29/12/17/bread-18987_1280.jpg",
		Id:          6,
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
		log.Printf("Failed to buy bread: %v\n", err)
	}

	log.Printf("Bread bought: %v", response.Breads.GetBreads())
}
