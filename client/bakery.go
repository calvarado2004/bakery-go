package main

import (
	pb "calvarado2004/bakery-go/proto"
	"context"
	"log"
)

func MakeBread(client pb.MakeBreadClient, bread *pb.Bread) {

	request := &pb.BreadRequest{
		Bread: bread,
	}

	response, err := client.MakeBread(context.Background(), request)
	if err != nil {
		log.Println("error making bread: ", err)
	}

	log.Println("Bread made: ", response.Bread)
}
