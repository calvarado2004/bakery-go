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

		err := rabbitMQBakery.performBuyBread()
		if err != nil {
			log.Errorf("Failed to perform buy bread (main): %v", err)
			return
		}
		log.Printf("Ouch! Something went wrong with buy bread, we got disconnected from RabbitMQ")

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

	for buyOrder := range buyOrderMessage {
		buyOrderType := data.BuyOrder{}
		err := json.Unmarshal(buyOrder.Body, &buyOrderType)
		if err != nil {
			log.Printf("Failed to unmarshal buy order: %v", err)
			return err
		}

		log.Printf("Received a buy order, a message has been consumed: %v", buyOrderType)

		// Insert the buy order first with status as "Pending"
		buyOrderType.Status = "Pending"
		_, err = rabbit.Repo.InsertBuyOrder(buyOrderType, buyOrderType.Breads)
		if err != nil {
			log.Printf("Failed to insert buy order to db: %v", err)
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

			// Update the status to "Processed"
			err := rabbit.Repo.UpdateOrderStatus(buyOrderType.BuyOrderUUID, "Processed")
			if err != nil {
				log.Printf("Failed to update order status: %v", err)
				return err
			}

			for _, bread := range buyOrderType.Breads {
				err = rabbit.Repo.AdjustBreadQuantity(bread.ID, -bread.Quantity)
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

			buyOrderID, err := rabbit.Repo.InsertBuyOrder(buyOrderType, buyOrderType.Breads)
			if err != nil {
				log.Printf("Failed to insert buy order to db: %v", err)
				return err
			}

			if buyOrderType.ID <= 0 {

				log.Warningf("Buy order ID is 0 or less, setting the ID to the one generated by the database %d", buyOrderType.ID)
				buyOrderType.ID = buyOrderID
			}

			err = buyOrder.Ack(false)
			if err != nil {
				log.Printf("Failed to ack buy order on queue: %v", err)
				return err
			}

			log.Printf("Buy order with ID %v placed", buyOrderType.ID)

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

			time.Sleep(34 * time.Second)

		} else {
			log.Printf("Not all bread is available, requeuing the buy order")
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

			time.Sleep(34 * time.Second)

		}

	}

	return nil
}
