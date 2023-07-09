package main

import (
	"database/sql"
	"encoding/json"
	"github.com/calvarado2004/bakery-go/data"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"time"
)

var rabbitmqConnection *rabbitmq.Connection
var rabbitmqChannel *rabbitmq.Channel

// init is called before the application starts, and sets up the RabbitMQ connection as well as the necessary queues
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

	// Declare the RabbitMQ buy-bread-order as durable
	_, err = rabbitmqChannel.QueueDeclare(
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
	_, err = rabbitmqChannel.QueueDeclare(
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

			breadMakeOrder.Breads = append(breadMakeOrder.Breads, bread)
			order, err := data.NewPostgresRepository(pgConn).InsertMakeOrder(breadMakeOrder, breads)
			if err != nil {
				return err
			}

			log.Printf("Make Bread Order ID %d created", order)
		}

	}

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
		ID:        2,
		Name:      "Another Bread Maker",
		Email:     "another_bread@maker.com",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	breadMakerID, err := data.NewPostgresRepository(pgConn).InsertBreadMaker(breadMaker)
	if err != nil {
		return

	}

	log.Printf("Bread Maker ID %d created", breadMakerID)

}

// performBuyBread listens for buy bread orders and updates the database
func performBuyBread(pgConn *sql.DB) {
	buyOrderMessage, err := rabbitmqChannel.Consume(
		"buy-bread-order", // queue
		"",                // consumer
		false,             // auto-ack
		false,             // exclusive
		false,             // no-local
		false,             // no-wait
		nil,               // args
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	for buyOrder := range buyOrderMessage {
		buyOrderType := data.BuyOrder{}
		err := json.Unmarshal(buyOrder.Body, &buyOrderType)
		if err != nil {
			log.Printf("Failed to unmarshal buy order: %v", err)
			continue
		}

		availableBread, err := data.NewPostgresRepository(pgConn).GetAvailableBread()
		if err != nil {
			return
		}

		allBreadAvailable := true // Initialize the flag

		quantityChange := 0

		for _, breadAvailable := range availableBread {
			for _, bread := range buyOrderType.Breads {
				if breadAvailable.Name == bread.Name {
					if breadAvailable.Quantity < bread.Quantity {
						allBreadAvailable = false // If not enough bread, set the flag to false
						log.Printf("Bread %s does not have enough quantity, required: %d, available: %d", bread.Name, bread.Quantity, breadAvailable.Quantity)
					} else {
						quantityChange = breadAvailable.Quantity - bread.Quantity

					}
				}
			}
		}

		if allBreadAvailable {
			log.Println("All bread available, processing order")
			for _, bread := range buyOrderType.Breads {
				err = data.NewPostgresRepository(pgConn).AdjustBreadQuantity(bread.ID, -bread.Quantity)
				if err != nil {
					log.Printf("Failed to adjust bread quantity: %v", err)
				}
				log.Printf("Selling bread %s, quantity %d", bread.Name, bread.Quantity)
				log.Printf("Bread %s, quantity changed to %d", bread.Name, quantityChange)

			}

			buyOrderType.CustomerID = 1
			buyOrderType.Customer = data.Customer{
				Name:  "John Doe",
				Email: "john@doe.com",
				ID:    1,
			}

			buyOrderID, err := data.NewPostgresRepository(pgConn).InsertBuyOrder(buyOrderType, buyOrderType.Breads)
			if err != nil {
				log.Printf("Failed to insert buy order to db: %v", err)
			}

			buyOrderType.ID = buyOrderID

			err = buyOrder.Ack(false)
			if err != nil {
				log.Printf("Failed to ack buy order on queue: %v", err)
			}

			log.Printf("Buy order with ID %v placed", buyOrderID)

			buyOrderData, err := json.Marshal(&buyOrderType)
			if err != nil {
				return
			}

			err = rabbitmqChannel.Publish(
				"",             // exchange
				"bread-bought", // routing key
				false,          // mandatory
				false,          // immediate
				rabbitmq.Publishing{
					ContentType:  "text/json",
					Body:         buyOrderData,
					DeliveryMode: rabbitmq.Persistent,
				})

		} else {
			log.Printf("Not all bread is available, requeuing the buy order")
			err = buyOrder.Nack(false, true) // requeue message
			if err != nil {
				return
			}
		}

		time.Sleep(3 * time.Second)

	}
}
