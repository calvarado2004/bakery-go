package main

import (
	pb "calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"log"
	"net"
	"os"
)

const (
	address = "0.0.0.0:50051"
)

type MakeBreadServer struct {
	pb.MakeBreadServer
}

type BakeryBreadServiceServer struct {
	pb.BakeryBreadServiceServer
}

func main() {
	address := os.Getenv("BAKERY_SERVICE_ADDR")

	listen, err := net.Listen("tcp", address)
	if err != nil {
		log.Print("error listening: ", err)
	}

	log.Printf("Server listening on %v", address)

	server := grpc.NewServer()
	pb.RegisterMakeBreadServer(server, &MakeBreadServer{})
	pb.RegisterBakeryBreadServiceServer(server, &BakeryBreadServiceServer{})

	reflection.Register(server)

	if err = server.Serve(listen); err != nil {
		log.Fatalf("Failed to serve gRPC server over %v", err)
	}

}
