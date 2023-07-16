package main

import (
	"context"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"log"
	"os"
	"time"
)

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

// Config is the configuration struct for the program
type Config struct {
	conn           *grpc.ClientConn
	buyBreadClient pb.BuyBreadClient
}

type buyOrder struct {
	orderId      int
	buyChan      chan bool
	buyOrderUUID string
}

// main is the entry point of the program
func main() {
	breadBoughtChan := make(chan bool)
	done := make(chan bool)

	grpcConn, err := grpc.Dial(gRPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}

	config := Config{
		conn:           grpcConn,
		buyBreadClient: pb.NewBuyBreadClient(grpcConn),
	}

	buyOrderChan := make(chan buyOrder)

	for {
		log.Println("Sending a signal to buy bread")

		buyBreadChan := make(chan bool)

		buyOrderuuid := uuid.NewString()
		log.Printf("Generated a new buy order id: %v", buyOrderuuid)
		order := buyOrder{buyOrderUUID: buyOrderuuid, buyChan: buyBreadChan}
		buyOrderChan <- order
		buyOrderChan <- order
		done <- true
		buyBreadChan <- true
		log.Println("Done sending a signal to buy bread and waiting for 35 seconds...")

		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)

		go func() {
			log.Println("Starting buySomeBread goroutine...")
			for {
				buyOrder := <-buyOrderChan
				config.buySomeBread(ctx, buyOrder.buyChan, breadBoughtChan, done, buyOrder.buyOrderUUID)
			}
			log.Println("Exiting buySomeBread goroutine...")
		}()

		go func() {
			log.Println("Starting buyBreadStream goroutine...")
			for {
				buyOrder := <-buyOrderChan
				config.buyBreadStream(ctx, breadBoughtChan, done, buyOrder.buyOrderUUID)
			}
			log.Println("Exiting buyBreadStream goroutine...")
		}()

		<-ctx.Done()
		cancel()
		log.Println("Timeout reached, cancelling context and restarting")

	}

}

// buySomeBread sends a BuyBread request to the gRPC server and waits for a response
func (config *Config) buySomeBread(ctx context.Context, buyBreadChan <-chan bool, breadBoughtChan chan<- bool, done chan<- bool, buyOrderUuid string) {

	// Wait for a signal to buy bread
	for {
		select {
		case <-buyBreadChan:

			log.Println("Received a signal to buy bread")
			// Buy bread
			pretzelBread := &pb.Bread{
				Name:        "Pretzel",
				Quantity:    3,
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
				Quantity:    4,
				Price:       2.99,
				Description: "Cinnamon Roll, a classic bakery bread with cinnamon and sugar",
				Type:        "Sweet Bread",
				Status:      "available",
				Image:       "https://cdn.pixabay.com/photo/2019/12/25/17/55/cinnamon-roll-4719023_1280.jpg",
				Id:          1,
			}

			croissantBread := &pb.Bread{
				Name:        "Croissant",
				Quantity:    3,
				Price:       1.19,
				Description: "Croissant, a classic bakery bread with a buttery taste",
				Type:        "Buttery Bread",
				Status:      "available",
				Image:       "https://cdn.pixabay.com/photo/2012/02/29/12/17/bread-18987_1280.jpg",
				Id:          6,
			}

			briocheBread := &pb.Bread{
				Name:        "Brioche",
				Quantity:    4,
				Price:       1.59,
				Type:        "Sweet Bread",
				Status:      "available",
				Description: "Brioche, a classic bakery bread with a sweet taste",
				Image:       "https://cdn.pixabay.com/photo/2017/09/05/17/18/pretzel-2718477_1280.jpg",
				Id:          7,
			}

			breadList := pb.BreadList{
				Breads: []*pb.Bread{
					pretzelBread,
					baguetteBread,
					cinnamonBread,
					croissantBread,
					briocheBread,
				},
			}

			request := pb.BreadRequest{
				Breads:       &breadList,
				BuyOrderUuid: buyOrderUuid,
			}

			log.Printf("Trying to buy bread: %v", request.Breads.Breads)

			response, err := config.buyBreadClient.BuyBread(ctx, &request)
			if err != nil {
				log.Printf("Failed to buy bread: %v\n", err)
				return
			}

			log.Printf("Buying bread started: %v", response.Breads.GetBreads())

			// Signal that bread has been bought
			breadBoughtChan <- true

			// After the bread has been bought, signal that we're done
			done <- true
		}
	}

}

// buyBreadStream consumes the BuyBreadStream from the gRPC server
func (config *Config) buyBreadStream(ctx context.Context, breadBoughtChan <-chan bool, done chan<- bool, buyOrderUuid string) {

	breadReq := &pb.BreadRequest{
		BuyOrderUuid: buyOrderUuid,
	}

	stream, err := config.buyBreadClient.BuyBreadStream(ctx, breadReq)
	if err != nil {
		log.Printf("Failed to start BuyBreadStream: %v", err)
		return
	}

	for {
		log.Println("Waiting for bread to be bought...")
		select {
		case <-breadBoughtChan:
			// Consume the stream
			for {
				log.Println("Waiting for stream response...")
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

			// After the bread has been bought, signal that we're done
			done <- true
		}
	}
}
