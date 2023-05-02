package api

import pb "calvarado2004/bakery-go/proto"

type Offerings struct {
	pb.Bread
	pb.Cakes
	pb.Cookies
}
