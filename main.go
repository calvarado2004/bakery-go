package main

import (
	"calvarado2004/bakery-go/api"
	pb "calvarado2004/bakery-go/proto"
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
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
	reflection.Register(server)
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

	clients := &pb.ClientsInQueue{
		Clients: 20,
	}

	_, err = api.AddClientToQueue(ctx, clients, nil)

	queue, err := api.ShowWaitingQueue(ctx, &pb.ClientsInQueue{}, nil)
	if err != nil {
		return
	}

	log.Println("queue: ", queue)

}
