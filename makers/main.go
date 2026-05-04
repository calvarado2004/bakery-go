package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	rabbitmq "github.com/rabbitmq/amqp091-go"

	log "github.com/sirupsen/logrus"
)

var rabbitmqAddress = os.Getenv("RABBITMQ_SERVICE_ADDR")

// Config is kept for backwards compatibility with tests.
// The Client field is unused by the makers service.
type Config struct {
	Repo   data.Repository
	Client *http.Client
}

// setupRepo initializes the repository on the Config. Kept for test compatibility.
func (c *Config) setupRepo(_ *sql.DB) {
	// No-op in tests; in production this would be called with a real DB connection
}

var rabbitmqConnection *rabbitmq.Connection
var rabbitmqChannel *rabbitmq.Channel

// counts tracks DB connection retry attempts (used by tests for backwards compatibility).
var counts int64

// connectToDB connects to the database using the DSN from the environment variable.
// Kept for backwards compatibility with tests.
func connectToDB() *sql.DB {
	return connectToDSN(os.Getenv("DSN"))
}

// connectToDSN connects to the database with the given DSN.
func connectToDSN(dsn string) *sql.DB {
	for i := 0; i < 10; i++ {
		connection, err := openDB(dsn)
		if err != nil {
			log.Errorf("Error opening database: %v (attempt %d/10)", err, i+1)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("Connected to database")
		return connection
	}

	log.Error("Max DB connection attempts reached")
	return nil
}

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Errorf("Failed to open database: %v", err)
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	dsn := os.Getenv("DSN")
	if dsn == "" {
		log.Fatal("DSN environment variable not set")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	// Start makers consumer with reconnection loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		runMakersConsumer(dsn, &wg)
	}()

	// Wait for shutdown signal
	<-sigCh
	log.Println("Shutdown signal received, draining...")
}

func runMakersConsumer(dsn string, wg *sync.WaitGroup) {
	reconnectDelay := 5 * time.Second

	for {
		pgConn := connectToDSN(dsn)
		if pgConn == nil {
			log.Errorf("Could not connect to database, retrying in %v", reconnectDelay)
			time.Sleep(reconnectDelay)
			continue
		}

		if err := runConsumerLoop(pgConn); err != nil {
			log.Errorf("Consumer loop error: %v, reconnecting in %v", err, reconnectDelay)
			pgConn.Close()
			time.Sleep(reconnectDelay)
			continue
		}

		// Consumer loop exited cleanly
		log.Println("Consumer loop exited cleanly, reconnecting in", reconnectDelay)
		pgConn.Close()
		time.Sleep(reconnectDelay)
	}
}

func runConsumerLoop(pgConn *sql.DB) error {
	// Lazy-init RabbitMQ connection so it's fresh on each reconnect
	if err := initializeRabbitMQ(rabbitmqAddress); err != nil {
		return fmt.Errorf("RabbitMQ initialization: %w", err)
	}

	if err := setupConsumerChannel(); err != nil {
		return fmt.Errorf("consumer channel setup: %w", err)
	}

	breadsBought, err := rabbitmqChannel.Consume(
		"make-bread-order", // queue
		"",                 // consumer
		false,              // auto-ack
		false,              // exclusive
		false,              // no-local
		false,              // no-wait
		nil,                // args
	)
	if err != nil {
		return fmt.Errorf("Failed to consume from make bread order queue: %v", err)
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for d := range breadsBought {
		wg.Add(1)
		go func(delivery rabbitmq.Delivery) {
			defer wg.Done()
			if err := processMakeBreadMessage(data.NewPostgresRepository(pgConn), delivery.Body); err != nil {
				log.Errorf("process error: %v", err)
				// Nack and requeue for retry on transient errors
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					log.Errorf("Failed to nack message: %v", nackErr)
				}
			} else {
				if ackErr := delivery.Ack(false); ackErr != nil {
					log.Errorf("Failed to ack message: %v", ackErr)
				}
			}
		}(d)

		// Periodically check if we should stop
		select {
		case <-ctx.Done():
			break
		default:
		}
	}

	wg.Wait()
	return nil
}

// setupConsumerChannel configures QoS and declares the make-bread-order queue.
func setupConsumerChannel() error {
	// Set QoS to prevent memory flooding — max 5 messages in flight
	if err := rabbitmqChannel.Qos(5, 0, false); err != nil {
		return fmt.Errorf("Failed to set QoS: %v", err)
	}

	// Declare the RabbitMQ make-bread-order queue as durable
	_, err := rabbitmqChannel.QueueDeclare(
		"make-bread-order", // name
		true,               // durable
		false,              // delete when unused
		false,              // exclusive
		false,              // no-wait
		nil,                // arguments
	)
	if err != nil {
		return fmt.Errorf("Failed to declare make-bread-order queue: %v", err)
	}

	return nil
}

// initializeRabbitMQ establishes a connection to RabbitMQ and opens a channel.
// It updates the global rabbitmqConnection and rabbitmqChannel variables.
func initializeRabbitMQ(rabbitmqAddr string) error {
	if rabbitmqAddr == "" {
		return fmt.Errorf("RABBITMQ_SERVICE_ADDR not set")
	}

	var err error
	rabbitmqConnection, err = rabbitmq.Dial(rabbitmqAddr)
	if err != nil {
		return fmt.Errorf("Failed to connect to RabbitMQ: %w", err)
	}

	rabbitmqChannel, err = rabbitmqConnection.Channel()
	if err != nil {
		return fmt.Errorf("Failed to open a channel: %w", err)
	}

	return nil
}

// listenForMakeBread is the original entry point kept for test compatibility.
// It wraps runConsumerLoop for backwards compatibility with existing tests.
func listenForMakeBread(pgConn *sql.DB) error {
	return runConsumerLoop(pgConn)
}
