package main

import (
	pb "calvarado2004/bakery-go/proto"
	"context"
	"log"
)

type MakeBreadServer struct {
	pb.MakeBreadServer
}

func (s *MakeBreadServer) MakeBread(ctx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	requestedBread := in.Bread

	log.Println("Bread requested to make: ", requestedBread)

	return &pb.BreadResponse{
		Bread: requestedBread,
	}, nil

}
