package main

import (
	pb "calvarado2004/bakery-go/proto"
	"context"
	"log"
)

func (s MakeBreadServer) MakeBread(ctx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadMade := pb.Bread{
		Name:     in.Bread.Name,
		Id:       in.Bread.Id,
		Type:     in.Bread.Type,
		Quantity: in.Bread.Quantity,
		Price:    in.Bread.Price,
		Message:  in.Bread.Message,
		Error:    in.Bread.Error,
	}

	log.Println("Bread to make", &breadMade)

	return &pb.BreadResponse{
		Bread: &breadMade,
	}, nil

}
