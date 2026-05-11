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

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

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
type breadMadeMessage struct {
	BreadID  int `json:"breadId"`
	Quantity int `json:"quantity"`
}

// ---------------------------------------------------------------------------
// Publisher — abstracts RabbitMQ publish for testability
// ---------------------------------------------------------------------------

// Publisher abstracts RabbitMQ channel publishing operations.
type Publisher interface {
	PublishConfirm(body []byte) error
}

// RabbitMQPublisher wraps a *rabbitmq.Channel for publishing bread-made confirmations.
type RabbitMQPublisher struct {
	ch *rabbitmq.Channel
}

// NewRabbitMQPublisher creates a Publisher from a RabbitMQ channel.
func NewRabbitMQPublisher(ch *rabbitmq.Channel) *RabbitMQPublisher {
	return &RabbitMQPublisher{ch: ch}
}

// PublishConfirm publishes a bread-made confirmation to the "bread-made" queue.
func (p *RabbitMQPublisher) PublishConfirm(body []byte) error {
	return p.ch.Publish(
		"", "bread-made", false, false,
		rabbitmq.Publishing{
			ContentType:  "text/json",
			Body:         body,
			DeliveryMode: rabbitmq.Persistent,
		})
}

// nopPublisher is a no-op publisher used for testing where publishing is not required.
type nopPublisher struct{}

func (nopPublisher) PublishConfirm(_ []byte) error { return nil }

// ---------------------------------------------------------------------------
// RabbitMQDialer — abstracts RabbitMQ connection creation
// ---------------------------------------------------------------------------

type RabbitMQDialer interface {
	Dial() (*rabbitmq.Connection, error)
}

type realRabbitMQDialer struct{}

func (realRabbitMQDialer) Dial() (*rabbitmq.Connection, error) {
	return rabbitmq.Dial(os.Getenv("RABBITMQ_SERVICE_ADDR"))
}

// ---------------------------------------------------------------------------
// AMQPChannel — abstracts RabbitMQ channel operations for testability
// ---------------------------------------------------------------------------

// AMQPChannel abstracts the subset of *rabbitmq.Channel operations used by the
// makers service. This makes it possible to unit-test the consumer logic with a
// mock channel instead of a real RabbitMQ instance.
type AMQPChannel interface {
	Qos(prefetchCount, prefetchSize int, global bool) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args rabbitmq.Table) (rabbitmq.Queue, error)
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args rabbitmq.Table) (<-chan rabbitmq.Delivery, error)
}

// rabbitmqChannelAdapter wraps *rabbitmq.Channel to satisfy AMQPChannel.
type rabbitmqChannelAdapter struct{ ch *rabbitmq.Channel }

func (a *rabbitmqChannelAdapter) Qos(prefetchCount, prefetchSize int, global bool) error {
	return a.ch.Qos(prefetchCount, prefetchSize, global)
}

func (a *rabbitmqChannelAdapter) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args rabbitmq.Table) (rabbitmq.Queue, error) {
	return a.ch.QueueDeclare(name, durable, autoDelete, exclusive, noWait, args)
}

func (a *rabbitmqChannelAdapter) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args rabbitmq.Table) (<-chan rabbitmq.Delivery, error) {
	return a.ch.Consume(queue, consumer, autoAck, exclusive, noLocal, noWait, args)
}

// ---------------------------------------------------------------------------
// makeResult — carries the outcome of processing a single message
// ---------------------------------------------------------------------------

type makeResult struct {
	confirmation *breadMadeMessage
	body         []byte
	err          error
}

// ---------------------------------------------------------------------------
// workerPool — concurrent message processor
// ---------------------------------------------------------------------------

type workerPool struct {
	tasks   chan []byte
	results chan *makeResult
	proc    func([]byte) (*breadMadeMessage, error)
}

func newWorkerPool(workers int, proc func([]byte) (*breadMadeMessage, error)) *workerPool {
	wp := &workerPool{
		tasks:   make(chan []byte, workers*2),
		results: make(chan *makeResult, workers*2),
		proc:    proc,
	}
	for i := 0; i < workers; i++ {
		go wp.worker()
	}
	return wp
}

func (wp *workerPool) worker() {
	for body := range wp.tasks {
		confirmation, err := wp.proc(body)
		wp.results <- &makeResult{confirmation: confirmation, body: body, err: err}
	}
}

func (wp *workerPool) Submit(body []byte) { wp.tasks <- body }
func (wp *workerPool) Results() <-chan *makeResult { return wp.results }

// ---------------------------------------------------------------------------
// MakersService — top-level service
// ---------------------------------------------------------------------------

// MakersService handles the external makers workflow: consuming
// make-bread-order messages and publishing bread-made confirmations.
type MakersService struct {
	dialer     RabbitMQDialer
	publisher  Publisher
	workerPool *workerPool
	stopCh     chan struct{}
	stopped    atomic.Bool
	consumeTag string
}

// NewMakersService creates a new MakersService.
// - dialer: RabbitMQ connection creator (nil = real dialer from env)
// - publisher: confirmation publisher (nil = nopPublisher)
// - workers: concurrency level (0 = 1)
func NewMakersService(dialer RabbitMQDialer, publisher Publisher, workers int) *MakersService {
	if dialer == nil {
		dialer = realRabbitMQDialer{}
	}
	if publisher == nil {
		publisher = &nopPublisher{}
	}
	if workers <= 0 {
		workers = 1
	}
	svc := &MakersService{
		dialer:     dialer,
		publisher:  publisher,
		stopCh:     make(chan struct{}),
		consumeTag: fmt.Sprintf("makers-%d", time.Now().UnixNano()),
	}
	svc.workerPool = newWorkerPool(workers, svc.processOrder)
	return svc
}

// Start begins consuming make-bread-order messages in a background goroutine.
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
		return
	}
	close(s.stopCh)
}

// ---------------------------------------------------------------------------
// runConsumerLoop — top-level orchestration (not unit-testable, intentionally)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// consumeMessages — connects RabbitMQ and delegates to processSingleMessage
// for each delivery. Extracted into a thin loop so the meat lives in
// processSingleMessage which can be unit-tested with a mock AMQPChannel.
// ---------------------------------------------------------------------------

func (s *MakersService) consumeMessages(ctx context.Context) error {
	log.Println("[makers] Initializing RabbitMQ connection...")
	conn, err := s.dialer.Dial()
	if err != nil {
		return fmt.Errorf("RabbitMQ dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	ch := &rabbitmqChannelAdapter{ch: nil}
	ch.ch, err = conn.Channel()
	if err != nil {
		return fmt.Errorf("RabbitMQ channel: %w", err)
	}
	defer ch.ch.Close()

	log.Println("[makers] RabbitMQ connection established, channel open")

	log.Println("[makers] Setting up consumer channel with QoS(5)...")
	if err := ch.Qos(5, 0, false); err != nil {
		return fmt.Errorf("QoS setup: %w", err)
	}

	log.Println("[makers] Declaring make-bread-order queue...")
	if _, err := ch.QueueDeclare("make-bread-order", true, false, false, false, nil); err != nil {
		return fmt.Errorf("queue declare: %w", err)
	}

	log.Println("[makers] Starting consumer on 'make-bread-order'...")

	breadsBought, err := ch.Consume("make-bread-order", s.consumeTag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	// Delegate each delivery to processSingleMessage.
	for d := range breadsBought {
		if err := s.processSingleMessage(ctx, ch, d); err != nil {
			log.Errorf("[makers] per-message error: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// processSingleMessage — handles ONE delivery: process → publish → ack/nack.
// This is the core function that is testable with a mock AMQPChannel.
// ---------------------------------------------------------------------------

// singleMessageResult holds the outcome of processSingleMessage.
type singleMessageResult struct {
	published  bool
	acked      bool
	nacked     bool
	publishErr error
	processErr error
	ackErr     error
	nackErr    error
}

// processSingleMessage processes a make-bread-order delivery: submits it to the
// worker pool, publishes the confirmation if successful, and ack/nacks the
// delivery. Returns singleMessageResult for testing.
func (s *MakersService) processSingleMessage(ctx context.Context, ch AMQPChannel, d rabbitmq.Delivery) error {
	// 1. Process via worker pool (submits + waits for result synchronously).
	s.workerPool.Submit(d.Body)
	result := <-s.workerPool.Results()

	// 2. Handle result: publish confirmation or nack on error.
	if result.err != nil {
		return s.nackDelivery(d, fmt.Errorf("process error: %w", result.err))
	}

	// 3. Publish confirmation.
	if result.confirmation == nil {
		return s.nackDelivery(d, fmt.Errorf("nil confirmation"))
	}

	if err := s.publishConfirmation(result.confirmation); err != nil {
		return s.nackDelivery(d, fmt.Errorf("publish error: %w", err))
	}

	// 4. Ack delivery.
	if err := s.ackDelivery(d); err != nil {
		return err
	}

	log.Printf("[makers] Made bread ID=%d, qty %d — published confirmation",
		result.confirmation.BreadID, result.confirmation.Quantity)

	return nil
}

// ---------------------------------------------------------------------------
// publishConfirmation — marshals & publishes a bread-made message.
// Extracted so it can be unit-tested with a mock Publisher.
// ---------------------------------------------------------------------------

// publishConfirmationResult holds the outcome of publishConfirmation.
type publishConfirmationResult struct {
	body       []byte
	publishErr error
}

// publishConfirmation marshals a breadMadeMessage and publishes it via the
// Publisher. Returns the published body for verification in tests.
func (s *MakersService) publishConfirmation(confirmation *breadMadeMessage) error {
	if confirmation == nil {
		return fmt.Errorf("nil confirmation")
	}
	data, err := json.Marshal(confirmation)
	if err != nil {
		return fmt.Errorf("marshal confirmation: %w", err)
	}

	if err := s.publisher.PublishConfirm(data); err != nil {
		return fmt.Errorf("publish confirmation: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// ackDelivery / nackDelivery — delivery acknowledgment helpers.
// Extracted so they can be tested independently.
// ---------------------------------------------------------------------------

// ackDeliveryResult holds the outcome of ackDelivery.
type ackDeliveryResult struct {
	acked  bool
	ackErr error
}

// ackDelivery acknowledges a delivery. Returns ackDeliveryResult for testing.
func (s *MakersService) ackDelivery(d deliveryAckNack) error {
	if err := d.Ack(false); err != nil {
		return fmt.Errorf("ack delivery: %w", err)
	}
	return nil
}

// nackDeliveryResult holds the outcome of nackDelivery.
type nackDeliveryResult struct {
	nacked  bool
	nackErr error
}

// nackDelivery negatively acknowledges a delivery (requeue = true).
func (s *MakersService) nackDelivery(d deliveryAckNack, procErr error) error {
	if nackErr := d.Nack(false, true); nackErr != nil {
		return fmt.Errorf("nack delivery: %w (original error: %w)", nackErr, procErr)
	}
	return fmt.Errorf("nacked: %w", procErr)
}

// deliveryAckNack abstracts the Ack/Nack methods on rabbitmq.Delivery.
// This makes it possible to unit-test ack/nack logic without a real RabbitMQ instance.
type deliveryAckNack interface {
	Ack(requeue bool) error
	Nack(requeue, multiple bool) error
}

// ---------------------------------------------------------------------------
// processOrder — pure business logic (no I/O). Extracted from the main loop.
// This is the function that transforms a raw message body into a confirmation.
// ---------------------------------------------------------------------------

// processOrderResult holds the outcome of processOrder.
type processOrderResult struct {
	confirmation *breadMadeMessage
	body         []byte
	processErr   error
}

// processOrder processes a make-bread-order body and returns the resulting
// confirmation. It does NOT publish — used by the worker pool and unit tests.
func (s *MakersService) processOrder(body []byte) (*breadMadeMessage, error) {
	msg := &makeBreadMessage{}
	if err := json.Unmarshal(body, msg); err != nil {
		return nil, fmt.Errorf("unmarshal make-bread message: %w", err)
	}

	log.Printf("[makers] Received make request: %s (ID=%d, qty=%d, type=%s)",
		msg.Name, msg.ID, msg.Quantity, msg.Type)

	return &breadMadeMessage{
		BreadID:  msg.ID,
		Quantity: msg.Quantity,
	}, nil
}

// ProcessMakeBreadMessage is the public alias for processOrder.
// Exported for unit testing — does NOT publish.
func (s *MakersService) ProcessMakeBreadMessage(body []byte) (*breadMadeMessage, error) {
	return s.processOrder(body)
}

// ---------------------------------------------------------------------------
// main — production entry point
// ---------------------------------------------------------------------------

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

	workers := 1
	if w := os.Getenv("MAKERS_WORKERS"); w != "" {
		fmt.Sscanf(w, "%d", &workers)
		if workers < 1 {
			workers = 1
		}
	}

	svc := NewMakersService(nil, nil, workers)

	var wg sync.WaitGroup
	svc.Start(ctx, &wg)

	log.Printf("Makers service is now listening for make-bread-order messages (%d workers)", workers)

	<-sigCh
	log.Println("Shutdown signal received, draining...")
	svc.Stop()
	cancel()
	wg.Wait()
	log.Println("=== Makers Service Stopped ===")
}
