package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	rabbitmq "github.com/rabbitmq/amqp091-go"

	log "github.com/sirupsen/logrus"
)

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

// RabbitMQDialer abstracts RabbitMQ connection creation.
type RabbitMQDialer interface {
	Dial() (*rabbitmq.Connection, error)
}

type realRabbitMQDialer struct{}

func (realRabbitMQDialer) Dial() (*rabbitmq.Connection, error) {
	return rabbitmq.Dial(os.Getenv("RABBITMQ_SERVICE_ADDR"))
}

// MakersService handles the external makers workflow:
// consuming make-bread-order messages and publishing bread-made confirmations.
// In the external-makers design, makers never touch the database directly.
type MakersService struct {
	dialer     RabbitMQDialer
	stopCh     chan struct{}
	stopped    atomic.Bool
	consumeTag string
}

// NewMakersService creates a new MakersService with the given dialer.
// If dialer is nil, a realRabbitMQDialer is used.
func NewMakersService(dialer RabbitMQDialer) *MakersService {
	if dialer == nil {
		dialer = realRabbitMQDialer{}
	}
	return &MakersService{
		dialer:     dialer,
		stopCh:     make(chan struct{}),
		consumeTag: fmt.Sprintf("makers-%d", time.Now().UnixNano()),
	}
}

// Start begins consuming make-bread-order messages in a background goroutine.
// The goroutine runs until Stop() is called or the context is cancelled.
func (s *MakersService) Start(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runConsumerLoop(ctx)
	}()
}

// Stop signals the service to stop consuming.
func (s *MakersService) Stop() {
	if s.stopped.Swap(true) {
		return // Already stopped
	}
	close(s.stopCh)
}

func (s *MakersService) runConsumerLoop(ctx context.Context) {
	reconnectDelay := 5 * time.Second

	for attempt := 1; ; attempt++ {
		log.Printf("[makers] Consumer loop attempt #%d", attempt)
		if err := s.consumeMessages(ctx); err != nil {
			log.Errorf("[makers] Consumer loop error: %v, reconnecting in %v", err, reconnectDelay)
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-time.After(reconnectDelay):
				continue
			}
		}

		// Consumer loop exited cleanly
		log.Println("[makers] Consumer loop exited cleanly, reconnecting in", reconnectDelay)
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (s *MakersService) consumeMessages(ctx context.Context) error {
	log.Println("[makers] Initializing RabbitMQ connection...")

	conn, err := s.dialer.Dial()
	if err != nil {
		return fmt.Errorf("RabbitMQ dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("RabbitMQ channel: %w", err)
	}
	defer ch.Close()

	log.Println("[makers] RabbitMQ connection established, channel open")

	log.Println("[makers] Setting up consumer channel with QoS(5)...")
	if err := ch.Qos(5, 0, false); err != nil {
		return fmt.Errorf("QoS setup: %w", err)
	}

	log.Println("[makers] Declaring make-bread-order queue...")
	if _, err := ch.QueueDeclare(
		"make-bread-order", true, false, false, false, nil,
	); err != nil {
		return fmt.Errorf("queue declare: %w", err)
	}

	log.Println("[makers] Starting consumer on 'make-bread-order'...")

	breadsBought, err := ch.Consume(
		"make-bread-order",
		s.consumeTag,
		false, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	for d := range breadsBought {
		// Keep consume/publish/ack on a single goroutine.
		// RabbitMQ channels are not safe for broad concurrent use, and running
		// this path serially avoids dropped confirmations in CI integration tests.
		if err := s.processMakeBreadMessage(ch, d.Body); err != nil {
			log.Errorf("[makers] process error: %v", err)
			if nackErr := d.Nack(false, true); nackErr != nil {
				log.Errorf("[makers] nack error: %v", nackErr)
			}
		} else {
			if ackErr := d.Ack(false); ackErr != nil {
				log.Errorf("[makers] ack error: %v", ackErr)
			}
		}

		select {
		case <-ctx.Done():
			break
		default:
		}
	}
	return nil
}

// processMakeBreadMessage processes a make-bread-order and publishes a
// bread-made confirmation to the given channel.
func (s *MakersService) processMakeBreadMessage(ch *rabbitmq.Channel, body []byte) error {
	msg := &makeBreadMessage{}
	if err := json.Unmarshal(body, msg); err != nil {
		return fmt.Errorf("unmarshal make-bread message: %w", err)
	}

	log.Printf("[makers] Received make request: %s (ID=%d, qty=%d, type=%s)", msg.Name, msg.ID, msg.Quantity, msg.Type)

	confirmation := breadMadeMessage{
		BreadID:  msg.ID,
		Quantity: msg.Quantity,
	}
	data, err := json.Marshal(confirmation)
	if err != nil {
		return fmt.Errorf("marshal confirmation: %w", err)
	}

	if err := ch.Publish(
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

	log.Printf("[makers] Made bread %s (ID=%d), qty %d — published confirmation", msg.Name, msg.ID, msg.Quantity)
	return nil
}

// ProcessMakeBreadMessage processes a make-bread-order body and returns the
// resulting confirmation. Exported for unit testing — does NOT publish.
func (s *MakersService) ProcessMakeBreadMessage(body []byte) (*breadMadeMessage, error) {
	msg := &makeBreadMessage{}
	if err := json.Unmarshal(body, msg); err != nil {
		return nil, fmt.Errorf("unmarshal make-bread message: %w", err)
	}

	log.Printf("[makers] Received make request: %s (ID=%d, qty=%d, type=%s)", msg.Name, msg.ID, msg.Quantity, msg.Type)

	return &breadMadeMessage{
		BreadID:  msg.ID,
		Quantity: msg.Quantity,
	}, nil
}

// main is the production entry point.
func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	log.Println("=== Makers Service Starting ===")
	log.Printf("RabbitMQ address: %s", os.Getenv("RABBITMQ_SERVICE_ADDR"))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := NewMakersService(nil) // nil dialer uses real one from env

	var wg sync.WaitGroup
	svc.Start(ctx, &wg)

	log.Println("Makers service is now listening for make-bread-order messages")

	<-sigCh
	log.Println("Shutdown signal received, draining...")
	svc.Stop()
	cancel()
	wg.Wait()
	log.Println("=== Makers Service Stopped ===")
}
