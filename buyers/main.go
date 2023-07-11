package main

import (
	"context"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"log"
	"os"
	"time"
)

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

var (
	buyBreadClient pb.BuyBreadClient
)

func main() {
	// Create a channel to control the buying attempts
	buyBreadChan := make(chan bool)

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

	buyBreadClient = pb.NewBuyBreadClient(grpcConn)

	// Start a goroutine to receive the BuyBreadStream
	go func() {
		for {
			buyBreadStream(grpcConn)
		}
	}()

	// Start a goroutine to buy bread
	go func() {
		for {
			// Wait for a signal to buy bread
			<-buyBreadChan
			buySomeBread(grpcConn)
		}
	}()

	// Regularly signal the goroutine to buy bread
	for true {
		log.Println("Iterating to buy bread...")
		buyBreadChan <- true
		time.Sleep(35 * time.Second)
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
		return
	}

	log.Printf("Buying bread started: %v", response.Breads.GetBreads())

}

// Function to receive and handle the BuyBreadStream
func buyBreadStream(conn *grpc.ClientConn) {
	breadReq := &pb.BreadRequest{
		// Fill your BreadRequest if needed
	}

	stream, err := buyBreadClient.BuyBreadStream(context.Background(), breadReq)
	if err != nil {
		log.Printf("Failed to start BuyBreadStream: %v", err)
		return
	}

	// Consume the stream
	for {
		response, err := stream.Recv()
		if err == io.EOF {
			// If we've received all updates, break out of the loop
			log.Printf("Received all updates, exiting...")
			break
		}
		if err != nil {
			log.Printf("Failed to receive update: %v", err)
			return
		}

		// Process the response
		log.Printf("Received bread response that has been settled: %v", response)
	}
}
