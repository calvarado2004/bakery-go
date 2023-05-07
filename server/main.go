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
	port = ":50051"
)

func main() {
	address := os.Getenv("BAKERY_SERVICE_ADDR")

	listen, err := net.Listen("tcp", port)
	if err != nil {
		log.Print("error listening: ", err)
	}

	var breadServer pb.MakeBreadServer

	server := grpc.NewServer()
	pb.RegisterMakeBreadServer(server, breadServer)
	reflection.Register(server)
	log.Print("Go Bakery bread server listening at port", port)

	if err := server.Serve(listen); err != nil {
		log.Print("error serving: ", address)
	}

}
