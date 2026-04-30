package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	rabbitmq "github.com/rabbitmq/amqp091-go"

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
	Status  string
	OrderId int
}

// publisher is the subset of rabbitmq.Channel used by processOneOrder.
// Defined as an interface so tests can stub out the network call.
type publisher interface {
	Publish(exchange, key string, mandatory, immediate bool, msg rabbitmq.Publishing) error
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
	startBroker(rabbitMQAddress)
}

// startBroker initializes the broker service with the given RabbitMQ URL.
// It sets up the database connection, configures the repository, and starts
// the background goroutines for processing orders and publishing outbox messages.
func startBroker(rabbitmqURL string) {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	pgConn := connectToDB()
	if pgConn == nil {
		log.Panic("Could not connect to database")
	}

	// Create a new RabbitMQBakery instance
	rabbitMQBakery := NewRabbitMQBakery(Config{}, rabbitmqURL)

	// Set up Postgres Repository for RabbitMQ Bakery
	rabbitMQBakery.setupRepo(pgConn)

	// Start the order-processing goroutine
	go startOrderProcessor(rabbitMQBakery)

	// Start the outbox publisher goroutine
	go startOutboxPublisher(rabbitMQBakery)

	select {}
}

// startOrderProcessor runs the order processing loop in the background.
// It continuously attempts to process buy bread orders from RabbitMQ.
func startOrderProcessor(rabbitMQBakery *RabbitMQBakery) {
	for {
		err := rabbitMQBakery.performBuyBread()
		if err != nil {
			log.Errorf("Failed to perform buy bread (main), sleeping 20 seconds...: %v", err)
			time.Sleep(20 * time.Second)
			continue
		}
		log.Printf("Ouch! Something went wrong with buy bread, we got disconnected from RabbitMQ, reconnecting in 20 seconds...")
		time.Sleep(20 * time.Second)
	}
}

// startOutboxPublisher runs the outbox message publishing loop in the background.
// It checks for unprocessed outbox messages every 45 seconds and publishes them to RabbitMQ.
func startOutboxPublisher(rabbitMQBakery *RabbitMQBakery) {
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
			processOutboxMessage(rabbitMQBakery.Repo, channel, message)
		}
	}
}

// processOutboxMessage publishes one outbox entry to the bread-bought queue and,
// on success, removes it from the outbox so it is not retried. On publish failure
// the record is left in place for the next tick.
func processOutboxMessage(repo data.Repository, pub publisher, msg data.OutboxMessage) {
	err := pub.Publish(
		"",             // exchange
		"bread-bought", // routing key
		false,          // mandatory
		false,          // immediate
		rabbitmq.Publishing{
			ContentType:  "text/json",
			Body:         msg.Payload,
			DeliveryMode: rabbitmq.Persistent,
		})
	if err != nil {
		log.Errorf("Failed to publish outbox message %d: %v", msg.ID, err)
		return
	}
	// Message successfully published — remove it so it is not re-sent.
	if err := repo.DeleteOutboxMessage(msg.ID); err != nil {
		log.Errorf("Failed to delete outbox message %d after publish: %v", msg.ID, err)
	}
}

// performBuyBread listens for buy bread orders and fulfills them atomically.
//
// Key invariants:
//   - Every message is acknowledged exactly once, whether the order succeeds or fails.
//   - Stock deduction uses FulfillOrderTx (SELECT FOR UPDATE), which prevents
//     two concurrent broker instances from both deducting the same inventory.
//   - Duplicate messages (RabbitMQ redelivery after a broker crash) are detected
//     by checking the database for an existing record with the same UUID before
//     inserting a new one.
func (rabbit *RabbitMQBakery) performBuyBread() error {

	connection, err := rabbitmq.Dial(rabbit.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return err
	}
	defer func(conn *rabbitmq.Connection) {
		if err := conn.Close(); err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return err
	}
	defer func(ch *rabbitmq.Channel) {
		if err := ch.Close(); err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	// Limit prefetch to 1 — RabbitMQ delivers one message at a time.
	// Without this, all queued messages are pre-fetched and held unacked in memory,
	// causing a massive backlog whenever the broker restarts.
	if err := channel.Qos(1, 0, false); err != nil {
		log.Fatalf("Failed to set QoS: %v", err)
	}

	buyOrderMessages, err := channel.Consume(
		"buy-bread-order", // queue
		"",                // consumer tag (auto-generated)
		false,             // auto-ack — we ack manually after processing
		false,             // exclusive
		false,             // no-local
		false,             // no-wait
		nil,               // args
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	log.Printf("Listening for buy bread orders on RabbitMQ queue...")

	for delivery := range buyOrderMessages {
		rabbit.processOneOrder(channel, delivery) // *rabbitmq.Channel satisfies publisher
	}

	return nil
}

// processOneOrder handles a single buy-bread-order delivery end-to-end.
// It always acknowledges the delivery before returning.
func (rabbit *RabbitMQBakery) processOneOrder(pub publisher, delivery rabbitmq.Delivery) {
	var order data.BuyOrder
	if err := json.Unmarshal(delivery.Body, &order); err != nil {
		log.Errorf("Failed to unmarshal buy order: %v", err)
		delivery.Ack(false) //nolint:errcheck
		return
	}

	log.WithField("order_uuid", order.BuyOrderUUID).Info("Received buy order")

	// --- Deduplication (TASK-03) ---
	// If a record with this UUID already exists in the database, this is a
	// RabbitMQ redelivery after a previous broker run crashed after inserting
	// but before acking. Ack and skip to avoid double-processing.
	if _, err := rabbit.Repo.GetBuyOrderByUUID(order.BuyOrderUUID); err == nil {
		log.WithField("order_uuid", order.BuyOrderUUID).
			Warn("Duplicate message detected (UUID already in DB), skipping")
		delivery.Ack(false) //nolint:errcheck
		return
	}

	// --- Insert order record ---
	order.Status = "Pending"
	buyOrderID, err := rabbit.Repo.InsertBuyOrder(order, order.Breads)
	if err != nil {
		log.Errorf("Failed to insert buy order: %v", err)
		// Nack so RabbitMQ redelivers; do not ack a message we couldn't record.
		delivery.Nack(false, true) //nolint:errcheck
		return
	}

	if order.ID <= 0 {
		order.ID = buyOrderID
	}

	// --- Outbox record for at-least-once publish reliability ---
	outbox := data.OutboxMessage{
		Payload:   delivery.Body,
		Sent:      false,
		ID:        buyOrderID,
		CreatedAt: time.Now(),
	}
	if err := rabbit.Repo.InsertOutboxMessage(outbox); err != nil {
		log.Errorf("Failed to insert outbox message: %v", err)
		// Continue — the outbox publisher will retry if the final publish fails.
	}

	// --- Atomic stock check + deduction (TASK-01) ---
	// FulfillOrderTx uses SELECT FOR UPDATE inside a transaction, so two
	// concurrent brokers cannot both pass the stock check and both deduct.
	if err := rabbit.Repo.FulfillOrderTx(order); err != nil {
		if errors.Is(err, data.ErrInsufficientStock) {
			log.WithField("order_uuid", order.BuyOrderUUID).
				Warn("Insufficient stock — marking order as failed")
		} else {
			log.WithField("order_uuid", order.BuyOrderUUID).
				Errorf("FulfillOrderTx error: %v", err)
		}
		rabbit.failOrder(order.BuyOrderUUID, buyOrderID, delivery)
		return
	}

	// --- Order fulfilled successfully ---
	order.CustomerID = 1
	order.Customer = data.Customer{Name: "John Doe", Email: "john@doe.com", ID: 1}

	if err := rabbit.Repo.UpdateOrderStatus(order.BuyOrderUUID, "Processed"); err != nil {
		log.Errorf("Failed to update order status to Processed: %v", err)
		rabbit.failOrder(order.BuyOrderUUID, buyOrderID, delivery)
		return
	}

	delivery.Ack(false) //nolint:errcheck
	log.WithField("order_uuid", order.BuyOrderUUID).Info("Buy order processed successfully")

	// Publish confirmation so the server stream can deliver the result to the client.
	orderData, err := json.Marshal(&order)
	if err != nil {
		log.Errorf("Failed to marshal processed order for publish: %v", err)
		// Outbox publisher will retry; do not delete the outbox entry.
		return
	}

	if err := pub.Publish("", "bread-bought", false, false, rabbitmq.Publishing{
		ContentType:  "text/json",
		Body:         orderData,
		DeliveryMode: rabbitmq.Persistent,
	}); err != nil {
		log.Errorf("Failed to publish bread-bought message: %v", err)
		// Outbox publisher will retry this publish; do not delete the entry.
		return
	}

	// Publish succeeded — remove the outbox entry so it is not re-sent.
	if err := rabbit.Repo.DeleteOutboxMessage(buyOrderID); err != nil {
		log.Errorf("Failed to delete outbox message: %v", err)
	}
}

// failOrder marks the order as Failed, acks the delivery, and cleans up the
// outbox entry. Used from processOneOrder when fulfillment cannot proceed.
func (rabbit *RabbitMQBakery) failOrder(uuid string, outboxID int, delivery rabbitmq.Delivery) {
	if err := rabbit.Repo.UpdateOrderStatus(uuid, "Failed"); err != nil {
		log.Errorf("Failed to update order status to Failed: %v", err)
	}
	delivery.Ack(false) //nolint:errcheck
	if err := rabbit.Repo.DeleteOutboxMessage(outboxID); err != nil {
		log.Errorf("Failed to delete outbox message on failure: %v", err)
	}
}
