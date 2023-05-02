package main

import (
	pb "calvarado2004/bakery-go/proto"
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"net"
	"os"
)

const (
	port = ":50051"
)

func main() {
	address := os.Getenv("BAKERY_SERVICE_ADDR")

	listen, err := net.Listen("tcp", port)
	if err != nil {
		log.Print("error listening: ", err)
	}

	var management pb.BakeryManagementServer

	server := grpc.NewServer()
	pb.RegisterBakeryManagementServer(server, management)
	log.Print("Go Bakery server listening at port", port)

	if err := server.Serve(listen); err != nil {
		log.Print("error serving: ", address)
	}

	client, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Println("error dialing: ", err)
	}

	defer func(client *grpc.ClientConn) {
		err := client.Close()
		if err != nil {
			log.Println("error closing client: ", err)
		}
	}(client)

	ctx := context.Background()

	addClientToQueue(ctx, pb.NewBakeryManagementClient(client))

	getBread(ctx, pb.NewBakeryManagementClient(client))

	getCakes(ctx, pb.NewBakeryManagementClient(client))

	getCookies(ctx, pb.NewBakeryManagementClient(client))

}

func getBread(ctx context.Context, client pb.BakeryManagementClient) {
	_, err := client.GetBread(ctx, &pb.BreadRequest{})
	if err != nil {
		panic(err)
	}
}

func getCakes(ctx context.Context, client pb.BakeryManagementClient) {
	_, err := client.GetCakes(ctx, &pb.CakesRequest{})
	if err != nil {
		panic(err)
	}
}

func getCookies(ctx context.Context, client pb.BakeryManagementClient) {
	_, err := client.GetCookies(ctx, &pb.CookiesRequest{})
	if err != nil {
		panic(err)
	}
}

func addClientToQueue(ctx context.Context, client pb.BakeryManagementClient) {
	_, err := client.AddClientToQueue(ctx, &pb.ClientsInQueue{})
	if err != nil {
		panic(err)
	}
}
