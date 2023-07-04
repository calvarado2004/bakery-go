package main

import (
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"log"
	"net"
	"os"
)

type MakeBreadServer struct {
	pb.MakeBreadServer
}

type CheckInventoryServer struct {
	pb.CheckInventoryServer
}

type BuyBreadServer struct {
	pb.BuyBreadServer
}

type RemoveOldBreadServer struct {
	pb.RemoveOldBreadServer
}

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

var activemqAddress = os.Getenv("ACTIVEMQ_SERVICE_ADDR")

func main() {

	listen, err := net.Listen("tcp", gRPCAddress)
	if err != nil {
		log.Print("error listening: ", err)
	}

	log.Printf("Server listening on %v", gRPCAddress)

	server := grpc.NewServer()
	pb.RegisterMakeBreadServer(server, &MakeBreadServer{})
	pb.RegisterBuyBreadServer(server, &BuyBreadServer{})
	pb.RegisterCheckInventoryServer(server, &CheckInventoryServer{})
	pb.RegisterRemoveOldBreadServer(server, &RemoveOldBreadServer{})

	reflection.Register(server)

	if err = server.Serve(listen); err != nil {
		log.Fatalf("Failed to serve gRPC server over %v", err)
	}

}
