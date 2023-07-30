package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	log "github.com/sirupsen/logrus"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

// ContextMaker creates a new context when necessary
type ContextMaker func() (context.Context, context.CancelFunc)

// init is called before the application starts, and sets up the RabbitMQ connection as well as the necessary queues
func (rabbit *RabbitMQBakery) init() {

	connection, err := rabbitmq.Dial(rabbit.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	// Declare the RabbitMQ make-bread-order queue as durable
	_, err = channel.QueueDeclare(
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

	// Declare the RabbitMQ buy-bread-order as durable
	_, err = channel.QueueDeclare(
		"buy-bread-order", // name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Declare the RabbitMQ bread-bought as durable
	_, err = channel.QueueDeclare(
		"bread-bought", // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

}

// checkBread checks if there is enough bread left in the bakery, if not, it orders more
func (rabbit *RabbitMQBakery) checkBread() error {

	connection, err := rabbitmq.Dial(rabbit.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return err
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return err
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	breads, err := rabbit.Repo.GetAvailableBread()
	if err != nil {
		return err
	}

	if len(breads) == 0 {
		rabbit.initializeBakery()
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

			err = channel.Publish(
				"",                 // exchange
				"make-bread-order", // routing key
				false,              // mandatory
				false,              // immediate
				rabbitmq.Publishing{
					ContentType:  "text/json",
					Body:         breadData,
					DeliveryMode: rabbitmq.Persistent,
				})
			if err != nil {
				return status.Errorf(codes.Internal, "Failed to publish a message: %v", err)
			}

			breadMakeOrder.Breads = append(breadMakeOrder.Breads, bread)
			order, err := rabbit.Repo.InsertMakeOrder(breadMakeOrder, breads)
			if err != nil {
				return err
			}

			log.Printf("Make Bread Order ID %d created", order)
		}

	}

	return nil

}

// initializeBakery creates the initial breads in the database
func (rabbit *RabbitMQBakery) initializeBakery() {

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
			Image:       "https://cdn.pixabay.com/photo/2017/06/23/23/57/bread-2436370_1280.jpg",
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
		breadID, err := rabbit.Repo.InsertBread(bread)
		if err != nil {
			return
		}
		log.Printf("Bread ID %d created", breadID)
	}

	breadMaker := data.BreadMaker{
		ID:        2,
		Name:      "Another Bread Maker",
		Email:     "another_bread@maker.com",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	breadMakerID, err := rabbit.Repo.InsertBreadMaker(breadMaker)
	if err != nil {
		return

	}

	log.Printf("Bread Maker ID %d created", breadMakerID)

}

// getBuyResponse listens for bread bought messages and sends them to the client, adding backoff retries if there is an error
func (rabbit *RabbitMQBakery) getBuyResponse(contextMaker ContextMaker, responseCh chan *pb.BreadResponse) error {
	retryInterval := time.Second // Start with a delay of 1 second

	maxRetries := 5 // Maximum number of retries
	retries := 0    // Number of retries made

	for {
		ctx, cancel := contextMaker()

		select {
		case <-ctx.Done():
			// If the context is done, instead of returning an error, restart the loop
			// But first check if maxRetries is hit
			if retries >= maxRetries {
				cancel()
				return ctx.Err()
			}
			retries++
			log.Errorf("Context done, restarting the loop: %v", ctx.Err())
			cancel()
			continue
		default:
			// If the context is not done, attempt to run the goroutine
			err := rabbit.processBreadsBought(ctx, responseCh)
			if err != nil {
				if maxRetries == 0 {
					// If there are no more retries, return the error
					cancel()
					return err
				}
				log.Errorf("Error processing breads bought: %v", err)
				// If there was an error, wait for retryInterval before trying again
				time.Sleep(retryInterval)
				// Increase the retryInterval for the next try
				retryInterval *= 2
				maxRetries--
			} else {
				// If no error, reset the retryInterval
				retryInterval = time.Second
			}
		}
		cancel() // Cancel the context after each loop iteration, as we're going to create a new one
	}
}

// processBreadsBought listens for breads bought messages and sends them to the client
func (rabbit *RabbitMQBakery) processBreadsBought(ctx context.Context, responseCh chan *pb.BreadResponse) error {

	connection, err := rabbitmq.Dial(rabbit.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return err
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return err
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	buyOrder := data.BuyOrder{}

	breadsBought, err := channel.Consume(
		"bread-bought", // queue
		"",             // consumer
		false,           // auto-ack
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)

	if err != nil {
		log.Errorf("Failed to consume from bought breads queue: %v", err)
		return err
	}

	for {
		select {
		case d := <-breadsBought:
			var breadBought pb.BreadList
			var message string

			log.Println("Received a message from the bread-bought queue")

			buyOrderType := data.BuyOrder{}

			err := json.Unmarshal(d.Body, &buyOrderType)
			if err != nil {
				log.Errorf("Failed to unmarshal buy order data: %v", err)
				return err
			}

			buyOrder.Breads = buyOrderType.Breads
			buyOrder.ID = buyOrderType.ID
			buyOrder.BuyOrderUUID = buyOrderType.BuyOrderUUID

			for _, bread := range buyOrder.Breads {

				breadBought.Breads = append(breadBought.Breads, &pb.Bread{
					Id:          int32(bread.ID),
					Name:        bread.Name,
					Quantity:    int32(bread.Quantity),
					Description: bread.Description,
					Price:       bread.Price,
					Image:       bread.Image,
					Type:        bread.Type,
				})
			}

			message = fmt.Sprintf("Bread order %d received for customer %s with uuid %s", buyOrder.ID, buyOrder.Customer.Name, buyOrder.BuyOrderUUID)

			log.Printf("Bread order with breads %s received for customer %s (inside Go function)", breadBought.Breads, buyOrder.Customer.Name)

			err = d.Ack(false)
			if err != nil {
				log.Errorf("Failed to Ack message: %v", err)
				return err
			}

			response := &pb.BreadResponse{
				Breads:       &breadBought,
				Message:      message,
				BuyOrderUuid: buyOrder.BuyOrderUUID,
			}

			responseCh <- response // send the response to the channel

		case <-ctx.Done():
			// If the context is done, return an error
			log.Warningf("Context done, returning error")
			return ctx.Err()
		}
	}

}
