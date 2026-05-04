package main

import (
	"context"
	"database/sql"
	"encoding/json"
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

// contextWithTimeout is a local alias to avoid import conflict.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

type RabbitMQBakery struct {
	Config
	orders      map[int]*OrderStatus
	mu          sync.Mutex
	rabbitmqURL string
	buffer      orderBuffer // incoming orders waiting for matching
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
	db.SetDSN(os.Getenv("DSN"))
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

	// Start the order-matching goroutine (buffered, batched)
	go startMatchingEngine(rabbitMQBakery)

	// Start the outbox publisher goroutine
	go startOutboxPublisher(rabbitMQBakery)

	select {}
}

// startMatchingEngine runs the order ingestion and matching pipeline.
// It consumes orders from RabbitMQ, buffers them, and processes them in batches.
func startMatchingEngine(rabbitMQBakery *RabbitMQBakery) {
	// Connect to RabbitMQ once for the matchLoop (batch timer).
	conn, err := rabbitmq.Dial(rabbitMQBakery.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ for matchLoop: %v", err)
		return
	}
	channel, err := conn.Channel()
	if err != nil {
		log.Errorf("Failed to open channel for matchLoop: %v", err)
		conn.Close()
		return
	}
	go rabbitMQBakery.matchLoop(conn, channel)

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

// matchLoop buffers incoming orders and processes them in batches.
// Each order is ACKed immediately upon ingestion. Matching happens
// in batches collected over matchBatchWindow or when matchBatchSize is reached.
func (app *RabbitMQBakery) matchLoop(conn *rabbitmq.Connection, channel *rabbitmq.Channel) {
	ticker := time.NewTicker(matchBatchWindow)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			batch := app.buffer.drain()
			if len(batch) > 0 {
				log.Infof("matchLoop: batch timer fired, processing %d orders", len(batch))
				app.processMatchingBatch(batch, channel)
			}
		default:
			if app.buffer.len() >= matchBatchSize {
				batch := app.buffer.drain()
				log.Infof("matchLoop: batch size reached (%d), processing", len(batch))
				app.processMatchingBatch(batch, channel)
			}
			time.Sleep(50 * time.Millisecond)
		}
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

// performBuyBread listens for buy bread orders and buffers them for batch matching.
//
// Key invariants:
//   - Every message is acknowledged exactly once, whether the order is valid or not.
//   - Orders are inserted into the database immediately, then buffered for matching.
//   - Duplicate messages (RabbitMQ redelivery after a broker crash) are detected
//     by checking the database for an existing record with the same UUID before
//     inserting a new one.
//   - Stock deduction happens asynchronously in the matching engine via
//     SELECT FOR UPDATE, which prevents two concurrent broker instances from
//     both deducting the same inventory.
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
		rabbit.processOneOrder(delivery)
	}

	return nil
}

// processOneOrder receives a buy-bread-order delivery, validates it,
// inserts the order into the database, and buffers it for batch matching.
// The delivery is ACKed immediately upon successful buffering.
func (rabbit *RabbitMQBakery) processOneOrder(delivery rabbitmq.Delivery) {
	var order data.BuyOrder
	if err := json.Unmarshal(delivery.Body, &order); err != nil {
		log.Errorf("Failed to unmarshal buy order: %v", err)
		delivery.Ack(false) //nolint:errcheck
		return
	}

	log.WithField("order_uuid", order.BuyOrderUUID).Info("Received buy order")

	// --- Deduplication ---
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

	// --- Buffer for matching engine ---
	// The matching engine will process this order asynchronously,
	// checking stock and fulfilling/partially fulfilling as appropriate.
	rabbit.buffer.add(order)

	delivery.Ack(false) //nolint:errcheck
	log.WithField("order_uuid", order.BuyOrderUUID).Info("Order buffered for matching")
}

// failOrder marks an order as Failed in the database.
// Called by the matching engine when fulfillment cannot proceed.
func (rabbit *RabbitMQBakery) failOrder(uuid string, status string) {
	if err := rabbit.Repo.UpdateOrderStatus(uuid, status); err != nil {
		log.Errorf("Failed to update order status to %s: %v", status, err)
	}
}
