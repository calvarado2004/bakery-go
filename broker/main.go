package main

import (
	"database/sql"
	"encoding/json"
	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/streadway/amqp"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4"
	_ "github.com/jackc/pgx/v4/stdlib"
	log "github.com/sirupsen/logrus"
)

type RabbitMQBakery struct {
	Config
	orders      map[int]*OrderStatus
	mu          sync.Mutex
	rabbitmqURL string
}

type OrderStatus struct {
	Ch      chan *pb.BreadResponse
	Status  string
	OrderId int
}

type Config struct {
	Repo   data.Repository
	Client *http.Client
}

var rabbitMQAddress = os.Getenv("RABBITMQ_SERVICE_ADDR")

var counts int64

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Errorf("Failed to open database: %v", err)
		return nil, err
	}

	if err = db.Ping(); err != nil {
		log.Errorf("Failed to ping database: %v", err)
		return nil, err
	}

	return db, nil

}

func connectToDB() *sql.DB {
	dsn := os.Getenv("DSN")

	for {
		connection, err := openDB(dsn)
		if err != nil {
			log.Warningf("Error opening database: %s", err)
			counts++
		} else {
			log.Println("Connected to database")
			return connection
		}

		if counts > 10 {
			log.Errorf("Could not connect to database after 10 attempts: %v", err)
			return nil
		}

		log.Println("Retrying in 5 seconds")
		time.Sleep(5 * time.Second)
		continue

	}
}

func (app *Config) setupRepo(conn *sql.DB) {
	db := data.NewPostgresRepository(conn)
	app.Repo = db

}

// NewRabbitMQBakery creates a new RabbitMQBakery instance with the provided config
func NewRabbitMQBakery(config Config, rabbitmqURL string) *RabbitMQBakery {
	return &RabbitMQBakery{
		Config:      config,
		orders:      make(map[int]*OrderStatus),
		rabbitmqURL: rabbitmqURL,
	}
}

func main() {

	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	pgConn := connectToDB()
	if pgConn == nil {
		log.Panic("Could not connect to database")
	}

	// Create a new RabbitMQBakery instance
	rabbitMQBakery := NewRabbitMQBakery(Config{}, rabbitMQAddress)

	// Setup Postgres Repository for RabbitMQ Bakery
	rabbitMQBakery.setupRepo(pgConn)

	// Consume from RabbitMQ message queue buy-bread-order and perform buy bread
	go func() {

		for {
			err := rabbitMQBakery.performBuyBread()
			if err != nil {
				log.Errorf("Failed to perform buy bread (main): %v", err)
				continue
			}
			log.Printf("Ouch! Something went wrong with buy bread, we got disconnected from RabbitMQ, reconnecting in 20 seconds...")
			time.Sleep(20 * time.Second)

		}

	}()

	// Publish outbox messages to RabbitMQ, check every 45 seconds for unprocessed messages
	go func() {

		connection, err := rabbitmq.Dial(rabbitMQBakery.rabbitmqURL)
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

		ticker := time.NewTicker(time.Second * 45)
		for range ticker.C {
			messages, err := rabbitMQBakery.Repo.GetUnprocessedOutboxMessages()
			if err != nil {
				log.Errorf("Failed to get unprocessed outbox messages: %v", err)
				continue
			}

			for _, message := range messages {
				err = channel.Publish(
					"",             // exchange
					"bread-bought", // routing key
					false,          // mandatory
					false,          // immediate
					rabbitmq.Publishing{
						ContentType:  "text/json",
						Body:         message.Payload,
						DeliveryMode: rabbitmq.Persistent,
					})

				if err != nil {
					log.Printf("Failed to publish buy order: %v", err)
				} else {
					// If the message was successfully published, mark it as processed in the database
					err := rabbitMQBakery.Repo.DeleteOutboxMessage(message.ID)
					if err != nil {
						log.Errorf("Failed to mark outbox message as processed: %v", err)
					}
				}
			}
		}
	}()

	select {}

}

// performBuyBread listens for buy bread orders and updates the database
func (rabbit *RabbitMQBakery) performBuyBread() error {

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

	buyOrderMessage, err := channel.Consume(
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

	log.Printf("Listening for buy bread orders into RabbitMQ queue...")

	processedOrders := make(map[string]bool)

	for buyOrder := range buyOrderMessage {
		buyOrderType := data.BuyOrder{}
		err := json.Unmarshal(buyOrder.Body, &buyOrderType)
		if err != nil {
			log.Printf("Failed to unmarshal buy order: %v", err)
			return err
		}

		log.Printf("Received a buy order, a message has been consumed: %v", buyOrderType)

		// Check if the buy order has already been processed
		if _, processed := processedOrders[buyOrderType.BuyOrderUUID]; processed {
			log.Printf("Buy order with ID %v has already been processed, skipping", buyOrderType.BuyOrderUUID)
			continue
		}

		// Insert the buy order first with status as "Pending"
		buyOrderType.Status = "Pending"
		buyOrderID, err := rabbit.Repo.InsertBuyOrder(buyOrderType, buyOrderType.Breads)
		if err != nil {
			log.Errorf("Failed to insert buy order to db: %v", err)
			return err
		}

		// Create an OutboxMessage with the serialized buy order
		outboxMessage := data.OutboxMessage{
			Payload:   buyOrder.Body,
			Sent:      false,
			ID:        buyOrderID,
			CreatedAt: time.Now(),
		}

		// Insert the outbox message to the database
		err = rabbit.Repo.InsertOutboxMessage(outboxMessage)
		if err != nil {
			log.Errorf("Failed to insert outbox message to db: %v", err)
			return err
		}

		availableBread, err := rabbit.Repo.GetAvailableBread()
		if err != nil {
			return err
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

				// Update current bread quantity, if failed, mark the order as "Failed" and ack the message.
				err = rabbit.Repo.AdjustBreadQuantity(bread.ID, -bread.Quantity)
				if err != nil {
					log.Printf("Failed to adjust bread quantity: %v", err)

					// Update the status to "Failed"
					err := rabbit.Repo.UpdateOrderStatus(buyOrderType.BuyOrderUUID, "Failed")
					if err != nil {
						log.Printf("Failed to update order status: %v", err)
						return err
					}

					// Acknowledge the message regardless of bread availability
					err = buyOrder.Ack(false)
					if err != nil {
						log.Printf("Failed to ack buy order on queue: %v", err)
						return err
					}

					// Delete the outbox message, since the order has failed but message has been acked as per business logic
					err = rabbit.Repo.DeleteOutboxMessage(buyOrderID)
					if err != nil {
						log.Printf("Failed to delete outbox messaged: %v", err)
						return err
					}

					time.Sleep(34 * time.Second)

					return err
				}

				// Order processed successfully for current bread, move to the next bread
				log.Printf("Selling bread %s, quantity %d", bread.Name, bread.Quantity)
				log.Printf("Bread %s, quantity changed to %d", bread.Name, quantityChange)

			}

			buyOrderType.CustomerID = 1
			buyOrderType.Customer = data.Customer{
				Name:  "John Doe",
				Email: "john@doe.com",
				ID:    1,
			}

			if buyOrderType.ID <= 0 {

				buyOrderType.ID = buyOrderID
				log.Warningf("Buy order ID is 0 or less, setting the ID to the one generated by the database %d", buyOrderType.ID)

			}

			// Update the status to "Processed"
			err := rabbit.Repo.UpdateOrderStatus(buyOrderType.BuyOrderUUID, "Processed")
			if err != nil {
				log.Printf("Failed to update order status: %v", err)
				return err
			}

			err = buyOrder.Ack(false)
			if err != nil {
				log.Printf("Failed to ack buy order on queue: %v", err)
				return err
			}

			log.Printf("Buy order with ID %v marked as processed", buyOrderType.ID)

			buyOrderData, err := json.Marshal(&buyOrderType)
			if err != nil {
				return err
			}

			err = channel.Publish(
				"",             // exchange
				"bread-bought", // routing key
				false,          // mandatory
				false,          // immediate
				rabbitmq.Publishing{
					ContentType:  "text/json",
					Body:         buyOrderData,
					DeliveryMode: rabbitmq.Persistent,
				})
			if err != nil {
				log.Printf("Failed to publish buy order: %v", err)
			}

			// Delete the outbox message, since the order has been processed
			err = rabbit.Repo.DeleteOutboxMessage(buyOrderID)
			if err != nil {
				log.Printf("Failed to delete outbox message: %v", err)
				return err
			}

			// Mark the order as processed, so it won't be processed again
			processedOrders[buyOrderType.BuyOrderUUID] = true

			time.Sleep(34 * time.Second)

		} else {
			log.Printf("Not all bread is available, marking order as failed")
			// Update the status to "Failed"
			err := rabbit.Repo.UpdateOrderStatus(buyOrderType.BuyOrderUUID, "Failed")
			if err != nil {
				log.Printf("Failed to update order status: %v", err)
				return err
			}

			// Acknowledge the message regardless of bread availability
			err = buyOrder.Ack(false)
			if err != nil {
				log.Printf("Failed to ack buy order on queue: %v", err)
				return err
			}

			// Delete the outbox message, since the order has failed but message has been acked as per business logic
			err = rabbit.Repo.DeleteOutboxMessage(buyOrderID)
			if err != nil {
				log.Printf("Failed to delete outbox messaged: %v", err)
				return err
			}

			time.Sleep(34 * time.Second)

		}

	}

	return nil
}
