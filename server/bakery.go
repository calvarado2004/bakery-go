package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"time"
)

var rabbitmqConnection *rabbitmq.Connection
var rabbitmqChannel *rabbitmq.Channel

func init() {

	var err error
	rabbitmqConnection, err = rabbitmq.Dial(activemqAddress)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	rabbitmqChannel, err = rabbitmqConnection.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}

	// Declare the RabbitMQ make-bread-orderqueue as durable
	_, err = rabbitmqChannel.QueueDeclare(
		"make-bread-order", // name
		true,               // durable
		false,              // delete when unused
		false,              // exclusive
		false,              // no-wait
		nil,                // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

}

func checkBread(pgConn *sql.DB) error {

	breads, err := data.NewPostgresRepository(pgConn).GetAvailableBread()
	if err != nil {
		return err
	}

	if len(breads) == 0 {
		initializeBakery(pgConn)
	}

	breadMaker := data.BreadMaker{
		Name:  "Bread Maker",
		Email: "bread@maker.com",
		ID:    1,
	}

	breadMakeOrder := data.MakeOrder{
		BreadMaker:   breadMaker,
		BreadMakerID: breadMaker.ID,
	}

	for _, bread := range breads {
		if bread.Quantity > 10 {
			log.Printf("Enough bread of %s left, there are available %d", bread.Name, bread.Quantity)
		} else {
			log.Printf("There are only %d breads left of %s, ordering 50 more", bread.Quantity, bread.Name)
			bread.Quantity = 50
			breadData, err := json.Marshal(&bread)
			if err != nil {
				return status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
			}

			err = rabbitmqChannel.Publish(
				"",                 // exchange
				"make-bread-order", // routing key
				false,              // mandatory
				false,              // immediate
				rabbitmq.Publishing{
					ContentType:  "text/json",
					Body:         breadData,
					DeliveryMode: rabbitmq.Persistent,
				})

		}

		breadMakeOrder.Breads = append(breadMakeOrder.Breads, bread)
	}

	order, err := data.NewPostgresRepository(pgConn).InsertMakeOrder(breadMakeOrder, breads)
	if err != nil {
		return err
	}

	log.Printf("Make Bread Order ID %d created", order)

	time.Sleep(30 * time.Second)

	return nil

}

// initializeBakery creates the initial breads in the database
func initializeBakery(pgConn *sql.DB) {

	breads := []data.Bread{
		{
			Name:        "Cinnamon Roll",
			Quantity:    1,
			Price:       2.99,
			Description: "Cinnamon Roll, a classic bakery bread with cinnamon and sugar",
			Type:        "Sweet Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2019/12/25/17/55/cinnamon-roll-4719023_1280.jpg",
		},
		{
			Name:        "Sourdough Bread",
			Quantity:    1,
			Price:       1.99,
			Description: "Sourdough Bread, a classic bakery bread with a sour taste",
			Type:        "Sour Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2020/11/28/12/25/bread-5784572_1280.jpg",
		},
		{
			Name:        "Baguette",
			Quantity:    1,
			Price:       1.49,
			Description: "Baguette, a classic bakery bread with a long shape",
			Type:        "French Bread",
			Status:      "available",
			Image:       "hhttps://cdn.pixabay.com/photo/2017/06/23/23/57/bread-2436370_1280.jpg",
		},
		{
			Name:        "Pretzel",
			Quantity:    1,
			Price:       2.49,
			Description: "Pretzel, a classic bakery bread with a salty taste",
			Type:        "Salty Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2017/09/05/17/18/pretzel-2718477_1280.jpg",
		},
		{
			Name:        "Bolillo",
			Quantity:    1,
			Price:       0.79,
			Description: "Bolillo, a classic bakery bread with a soft texture",
			Type:        "Soft Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2019/02/07/21/19/bobbin-lace-3982200_1280.jpg",
		}, {
			Name:        "Croissant",
			Quantity:    1,
			Price:       1.19,
			Description: "Croissant, a classic bakery bread with a buttery taste",
			Type:        "Buttery Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2012/02/29/12/17/bread-18987_1280.jpg",
		},
		{
			Name:        "Brioche",
			Quantity:    1,
			Price:       1.59,
			Description: "Brioche, a classic bakery bread with a sweet taste",
			Type:        "Sweet Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2021/01/16/21/05/brioche-5923399_1280.jpg",
		},
	}

	for _, bread := range breads {
		breadID, err := data.NewPostgresRepository(pgConn).InsertBread(bread)
		if err != nil {
			return
		}
		log.Printf("Bread ID %d created", breadID)
	}

	breadMaker := data.BreadMaker{
		Name:      "Bread Maker",
		Email:     "bread@maker.com",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	breadMakerID, err := data.NewPostgresRepository(pgConn).InsertBreadMaker(breadMaker)
	if err != nil {
		return

	}

	log.Printf("Bread Maker ID %d created", breadMakerID)

}

func (s *MakeBreadServer) BakeBread(_ context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadsToMake := in.Breads.GetBreads()

	var breadMade pb.BreadList

	for _, bread := range breadsToMake {
		log.Println("Bread to make", bread.Name)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		err = rabbitmqChannel.Publish(
			"",              // exchange
			"bread-to-make", // routing key
			false,           // mandatory
			false,           // immediate
			rabbitmq.Publishing{
				ContentType:  "text/json",
				Body:         breadData,
				DeliveryMode: rabbitmq.Persistent,
			})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
		}

		breadMade.Breads = append(breadMade.Breads, bread)

	}

	return &pb.BreadResponse{Breads: &breadMade}, nil

}

func (s *MakeBreadServer) SendBreadToBakery(_ context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadsToMake := in.Breads.GetBreads()

	var breadMade pb.BreadList

	for _, bread := range breadsToMake {
		log.Println("Sending fresh bread to bakery", bread.Name)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		bread.Status = "bread ready to consume"

		err = rabbitmqChannel.Publish(
			"",                // exchange
			"bread-in-bakery", // routing key
			false,             // mandatory
			false,             // immediate
			rabbitmq.Publishing{
				ContentType:  "text/json",
				Body:         breadData,
				DeliveryMode: rabbitmq.Persistent,
			})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
		}

		breadMade.Breads = append(breadMade.Breads, bread)

	}

	return &pb.BreadResponse{Breads: &breadMade}, nil

}

func (s *MakeBreadServer) MadeBreadStream(_ *pb.BreadRequest, stream pb.MakeBread_MadeBreadStreamServer) error {

	msgs, err := rabbitmqChannel.Consume(
		"bread-in-bakery", // queue
		"",                // consumer
		false,             // auto-ack
		false,             // exclusive
		false,             // no-local
		false,             // no-wait
		nil,               // args
	)
	if err != nil {

		return status.Errorf(codes.Internal, "Failed to consume from updates queue: %v", err)
	}

	var breadDelivered pb.BreadList

	for d := range msgs {
		bread := &pb.Bread{}
		err := json.Unmarshal(d.Body, bread)
		if err != nil {
			err := d.Nack(false, true)
			if err != nil {
				return err
			}
			return status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		breadDelivered.Breads = append(breadDelivered.Breads, bread)

		breadResponse := &pb.BreadResponse{Breads: &breadDelivered}

		if err := stream.Send(breadResponse); err != nil {
			err := d.Nack(false, true)
			if err != nil {
				return err
			}
			return err
		}

		err = d.Ack(false)
		if err != nil {
			return err
		}

	}

	return nil

}

func (s *CheckInventoryServer) CheckBreadInventory(cx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {
	queue, err := rabbitmqChannel.QueueInspect("bread-in-bakery") // inspect the queue
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to inspect the queue: %v", err)
	}

	numberOfMessages := queue.Messages

	bread := &pb.Bread{
		Quantity: int32(numberOfMessages),
	}

	breadList := pb.BreadList{
		Breads: []*pb.Bread{bread},
	}

	breadsResponse := &pb.BreadResponse{Breads: &breadList}
	// using the number of messages in the queue

	return breadsResponse, nil
}

func (s *CheckInventoryServer) CheckBreadInventoryStream(_ *pb.BreadRequest, stream pb.CheckInventory_CheckBreadInventoryStreamServer) error {

	msgs, err := rabbitmqChannel.Consume(
		"bread-in-bakery", // queue
		"",                // consumer
		false,             // auto-ack
		false,             // exclusive
		false,             // no-local
		false,             // no-wait
		nil,               // args
	)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to consume from updates queue: %v", err)
	}

	breadDelivered := &pb.BreadList{}

	log.Println("Waiting for breads to be available on the bakery on stream...")

	for d := range msgs {
		log.Printf("Bread available on Bakery: %s", d.Body)

		bread := &pb.Bread{}
		err := json.Unmarshal(d.Body, bread)
		if err != nil {
			log.Printf("Failed to unmarshal bread data: %v", err)
			err := d.Nack(false, true)
			if err != nil {
				return err
			}
		}

		breadDelivered.Breads = append(breadDelivered.Breads, bread)

		breadResponse := &pb.BreadResponse{Breads: breadDelivered}

		err = d.Nack(false, true)
		if err != nil {
			return err
		}

		if err := stream.Send(breadResponse); err != nil {
			log.Printf("Failed to send bread data: %v", err)
			return err
		}

	}

	return nil
}

func (s *BuyBreadServer) BuyBread(cx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadsToBuy := in.Breads.GetBreads()

	var breadBought pb.BreadList

	for _, bread := range breadsToBuy {
		log.Println("Buying bread and sending it to bread bought queue", bread.Name)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		bread.Status = "bread already sold"

		err = rabbitmqChannel.Publish(
			"",             // exchange
			"bread-bought", // routing key
			false,          // mandatory
			false,          // immediate
			rabbitmq.Publishing{
				ContentType:  "text/json",
				Body:         breadData,
				DeliveryMode: rabbitmq.Persistent,
			})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
		}

		breadBought.Breads = append(breadBought.Breads, bread)

	}

	return &pb.BreadResponse{Breads: &breadBought}, nil
}

func (s *BuyBreadServer) BuyBreadStream(in *pb.BreadRequest, stream pb.BuyBread_BuyBreadStreamServer) error {

	breadsBought, err := rabbitmqChannel.Consume(
		"bread-bought", // queue
		"",             // consumer
		false,          // auto-ack
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to consume from bought breads queue: %v", err)
	}

	for d := range breadsBought {
		bread := &pb.Bread{}
		err := json.Unmarshal(d.Body, bread)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		breadResponse := &pb.BreadResponse{Breads: &pb.BreadList{Breads: []*pb.Bread{bread}}}
		if err := stream.Send(breadResponse); err != nil {
			return err
		}
		err = d.Ack(false)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *RemoveOldBreadServer) RemoveBread(cx context.Context, in *pb.BreadRequest) (*pb.BreadResponse, error) {

	breadToRemove := in.Breads.GetBreads()
	var breadRemoved pb.BreadList

	for _, bread := range breadToRemove {
		log.Println("Bread to remove", &bread)

		breadData, err := json.Marshal(&bread)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to marshal bread data: %v", err)
		}

		err = rabbitmqChannel.Publish(
			"",              // exchange
			"bread-removed", // routing key
			false,           // mandatory
			false,           // immediate
			rabbitmq.Publishing{
				ContentType:  "text/json",
				Body:         breadData,
				DeliveryMode: rabbitmq.Persistent,
			})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to add bread to queue: %v", err)
		}

		breadRemoved.Breads = append(breadRemoved.Breads, bread)

	}

	return &pb.BreadResponse{Breads: &breadRemoved}, nil
}

func (s *RemoveOldBreadServer) RemoveBreadStream(in *pb.BreadRequest, stream pb.RemoveOldBread_RemoveBreadStreamServer) error {

	breadsRemoved, err := rabbitmqChannel.Consume(
		"bread-removed", // queue
		"",              // consumer
		false,           // auto-ack
		false,           // exclusive
		false,           // no-local
		false,           // no-wait
		nil,             // args
	)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to consume from removed breads queue: %v", err)
	}

	for d := range breadsRemoved {
		bread := &pb.Bread{}
		err := json.Unmarshal(d.Body, bread)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		breadResponse := &pb.BreadResponse{Breads: &pb.BreadList{Breads: []*pb.Bread{bread}}}
		if err := stream.Send(breadResponse); err != nil {
			return err
		}

		err = d.Ack(false)
		if err != nil {
			return err
		}
	}

	return nil
}
