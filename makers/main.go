package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	rabbitmq "github.com/rabbitmq/amqp091-go"

	log "github.com/sirupsen/logrus"
)

var rabbitmqAddress = os.Getenv("RABBITMQ_SERVICE_ADDR")

// makeBreadMessage represents the bread order received from the server.
type makeBreadMessage struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Quantity    int     `json:"quantity"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	Price       float64 `json:"price"`
	Status      string  `json:"status"`
	Image       string  `json:"image"`
}

// breadMadeMessage is published back to RabbitMQ after a maker finishes baking.
// The server consumes this and updates the database inventory.
type breadMadeMessage struct {
	BreadID  int `json:"breadId"`
	Quantity int `json:"quantity"`
}

var rabbitmqConnection *rabbitmq.Connection
var rabbitmqChannel *rabbitmq.Channel

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	log.Println("=== Makers Service Starting ===")
	log.Printf("RabbitMQ address: %s", rabbitmqAddress)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	// Start makers consumer with reconnection loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		runMakersConsumer(&wg)
	}()

	log.Println("Makers service is now listening for make-bread-order messages")

	// Wait for shutdown signal
	<-sigCh
	log.Println("Shutdown signal received, draining...")
	wg.Wait()
	log.Println("=== Makers Service Stopped ===")
}

func runMakersConsumer(wg *sync.WaitGroup) {
	reconnectDelay := 5 * time.Second

	for attempt := 1; ; attempt++ {
		log.Printf("Consumer loop attempt #%d", attempt)
		if err := runConsumerLoop(); err != nil {
			log.Errorf("Consumer loop error: %v, reconnecting in %v", err, reconnectDelay)
			time.Sleep(reconnectDelay)
			continue
		}

		// Consumer loop exited cleanly
		log.Println("Consumer loop exited cleanly, reconnecting in", reconnectDelay)
		time.Sleep(reconnectDelay)
	}
}

func runConsumerLoop() error {
	log.Println("[consumers] Initializing RabbitMQ connection...")

	// Lazy-init RabbitMQ connection so it's fresh on each reconnect
	if err := initializeRabbitMQ(rabbitmqAddress); err != nil {
		return fmt.Errorf("RabbitMQ initialization: %w", err)
	}
	log.Println("[consumers] RabbitMQ connection established, channel open")

	log.Println("[consumers] Setting up consumer channel with QoS(5)...")
	if err := setupConsumerChannel(); err != nil {
		return fmt.Errorf("consumer channel setup: %w", err)
	}
	log.Println("[consumers] Queue 'make-bread-order' declared, starting consumer...")

	breadsBought, err := rabbitmqChannel.Consume(
		"make-bread-order", // queue
		"",                 // consumer
		false,              // auto-ack
		false,              // exclusive
		false,              // no-local
		false,              // no-wait
		nil,                // arguments
	)
	if err != nil {
		return fmt.Errorf("Failed to consume from make bread order queue: %v", err)
	}
	log.Println("[consumers] Successfully consuming from 'make-bread-order' — waiting for messages...")

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for d := range breadsBought {
		wg.Add(1)
		go func(delivery rabbitmq.Delivery) {
			defer wg.Done()
			if err := processMakeBreadMessage(delivery.Body); err != nil {
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

	log.Printf("[connect] Dialing RabbitMQ at %s...", rabbitmqAddr)
	var err error
	rabbitmqConnection, err = rabbitmq.Dial(rabbitmqAddr)
	if err != nil {
		return fmt.Errorf("Failed to connect to RabbitMQ: %w", err)
	}
	log.Printf("[connect] RabbitMQ connection established (remote: %s, local: %s)", rabbitmqConnection.RemoteAddr(), rabbitmqConnection.LocalAddr())

	rabbitmqChannel, err = rabbitmqConnection.Channel()
	if err != nil {
		return fmt.Errorf("Failed to open a channel: %w", err)
	}
	log.Println("[connect] Channel opened successfully")

	return nil
}

// processMakeBreadMessage processes a make-bread-order by publishing a
// bread-made confirmation back to RabbitMQ. The server consumes this
// confirmation and updates the database inventory.
//
// This is the external-makers design: makers never touch the database
// directly. They only communicate via RabbitMQ.
func processMakeBreadMessage(body []byte) error {
	msg := &makeBreadMessage{}
	if err := json.Unmarshal(body, msg); err != nil {
		return fmt.Errorf("unmarshal make-bread message: %w", err)
	}

	log.Printf("[process] Received make request: %s (ID=%d, qty=%d, type=%s)", msg.Name, msg.ID, msg.Quantity, msg.Type)

	// Simulate bread baking (no DB access)
	// In production, this would call the actual bakery production system
	// No sleep here — process immediately

	// Publish confirmation to bread-made queue for the server to pick up
	confirmation := breadMadeMessage{
		BreadID:  msg.ID,
		Quantity: msg.Quantity,
	}
	data, err := json.Marshal(confirmation)
	if err != nil {
		return fmt.Errorf("marshal confirmation: %w", err)
	}

	if err := rabbitmqChannel.Publish(
		"",                // exchange
		"bread-made",      // routing key
		false,             // mandatory
		false,             // immediate
		rabbitmq.Publishing{
			ContentType:  "text/json",
			Body:         data,
			DeliveryMode: rabbitmq.Persistent,
		}); err != nil {
		return fmt.Errorf("publish bread-made confirmation: %w", err)
	}

	log.Printf("[process] Made bread %s (ID=%d), qty %d — published confirmation to 'bread-made' queue", msg.Name, msg.ID, msg.Quantity)
	return nil
}

// listenForMakeBread is the original entry point kept for test compatibility.
func listenForMakeBread() error {
	return runConsumerLoop()
}
