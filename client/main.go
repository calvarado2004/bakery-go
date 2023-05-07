package main

import (
	pb "calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
)

var address = "0.0.0.0:50051"

func main() {

	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Could not connect to %v", err)
	}

	log.Printf("Connected to %v", address)

	bread := &pb.Bread{
		Name:     "Sourdough",
		Id:       1,
		Type:     "Salty",
		Quantity: 100,
		Price:    2.99,
		Message:  "Sourdough bread is the best!",
		Error:    "",
	}

	log.Println("Making bread: ", bread)

	breadClient := pb.NewMakeBreadClient(conn)

	MakeBread(breadClient, bread)

	defer func(client *grpc.ClientConn) {
		err := client.Close()
		if err != nil {
			log.Println("error closing client: ", err)
		}
	}(conn)

}
