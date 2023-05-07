package api

import (
	pb "calvarado2004/bakery-go/proto"
	"context"
	"google.golang.org/grpc"
)

type Offerings struct {
	pb.Bread
	pb.Cakes
	pb.Cookies
}

func MakeBread(ctx context.Context, in *pb.BreadRequest, opts ...grpc.CallOption) (*pb.BreadResponse, error) {

	return &pb.BreadResponse{
		Bread: in.Bread,
	}, nil
}

func GetBread(ctx context.Context, in *pb.BreadRequest, opts ...grpc.CallOption) (*pb.BreadResponse, error) {
	return &pb.BreadResponse{
		Bread: in.Bread,
	}, nil
}

func ShowWaitingQueue(ctx context.Context, in *pb.ClientsInQueue, opts ...grpc.CallOption) (*pb.ClientsInQueue, error) {
	return &pb.ClientsInQueue{
		Clients: in.Clients,
	}, nil
}

func AddClientToQueue(ctx context.Context, in *pb.ClientsInQueue, opts ...grpc.CallOption) (*pb.ClientsInQueue, error) {
	return &pb.ClientsInQueue{
		Clients: in.Clients,
	}, nil
}
