package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/calvarado2004/bakery-go/pkg/resilience"
)

// RabbitMQDialer abstracts RabbitMQ connection creation.
type RabbitMQDialer interface {
	Dial() (*rabbitmq.Connection, error)
}

type realRabbitMQDialer struct{}

func (realRabbitMQDialer) Dial() (*rabbitmq.Connection, error) {
	return rabbitmq.Dial(os.Getenv("RABBITMQ_SERVICE_ADDR"))
}

// brokerClient wraps the gRPC BrokerService client with circuit-breaker
// and exponential-backoff retry.
type brokerClient struct {
	client pb.BrokerServiceClient
	// Circuit breakers for each endpoint (Phase 10.10).
	reportOrderCB      *resilience.CircuitBreaker
	reserveInventoryCB *resilience.CircuitBreaker
	reportMatchingCB   *resilience.CircuitBreaker
}

func newBrokerClient(conn *grpc.ClientConn) *brokerClient {
	return &brokerClient{
		client:             pb.NewBrokerServiceClient(conn),
		reportOrderCB:      resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 5, ResetTimeout: 30 * time.Second}),
		reserveInventoryCB: resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 5, ResetTimeout: 30 * time.Second}),
		reportMatchingCB:   resilience.NewCircuitBreaker(resilience.Options{FailureThreshold: 3, ResetTimeout: 60 * time.Second}),
	}
}

// brokerClienter is the interface for gRPC broker operations.
// Used to allow mocking in tests.
type brokerClienter interface {
	ReportOrder(order pb.BuyOrder) (*pb.BrokerOrderResult, error)
	ReserveInventory(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error)
	ReportMatchingResults(req *pb.MatchingBatch) (*pb.BatchConfirmation, error)
}

func (c *brokerClient) ReportOrder(order pb.BuyOrder) (*pb.BrokerOrderResult, error) {
	var result *pb.BrokerOrderResult
	cfg := resilience.RetryConfig{
		MaxRetries:       3,
		BaseDelay:        100 * time.Millisecond,
		MaxDelay:         2 * time.Second,
		Multiplier:       2.0,
		CircuitBreaker:   c.reportOrderCB,
	}
	err := resilience.Retry(context.Background(), cfg, func(ctx context.Context) error {
		resp, err := c.client.ReportOrder(ctx, &order)
		if err != nil {
			return err
		}
		result = resp
		return nil
	})
	return result, err
}

func (c *brokerClient) ReserveInventory(req *pb.ReserveInventoryRequest) (*pb.ReserveInventoryResult, error) {
	var result *pb.ReserveInventoryResult
	cfg := resilience.RetryConfig{
		MaxRetries:       3,
		BaseDelay:        100 * time.Millisecond,
		MaxDelay:         2 * time.Second,
		Multiplier:       2.0,
		CircuitBreaker:   c.reserveInventoryCB,
	}
	err := resilience.Retry(context.Background(), cfg, func(ctx context.Context) error {
		resp, err := c.client.ReserveInventory(ctx, req)
		if err != nil {
			return err
		}
		result = resp
		return nil
	})
	return result, err
}

func (c *brokerClient) ReportMatchingResults(req *pb.MatchingBatch) (*pb.BatchConfirmation, error) {
	var result *pb.BatchConfirmation
	cfg := resilience.RetryConfig{
		MaxRetries:       3,
		BaseDelay:        100 * time.Millisecond,
		MaxDelay:         2 * time.Second,
		Multiplier:       2.0,
		CircuitBreaker:   c.reportMatchingCB,
	}
	err := resilience.Retry(context.Background(), cfg, func(ctx context.Context) error {
		resp, err := c.client.ReportMatchingResults(ctx, req)
		if err != nil {
			return err
		}
		result = resp
		return nil
	})
	return result, err
}

// breakers returns all circuit breakers for logging/inspection.
func (c *brokerClient) breakers() map[string]*resilience.CircuitBreaker {
	return map[string]*resilience.CircuitBreaker{
		"report_order":      c.reportOrderCB,
		"reserve_inventory": c.reserveInventoryCB,
		"report_matching":   c.reportMatchingCB,
	}
}

// brokerConfig holds the configuration for the broker service.
type brokerConfig struct {
	Client *http.Client
}

// BrokerService is the broker service. It has NO database access —
// all data operations go through the server's gRPC BrokerService.
//
// This is the instance-based version that can be started/stopped for testing.
type BrokerService struct {
	brokerConfig
	rabbitmqURL     string
	rabbitmqDialer  RabbitMQDialer
	grpcConn        *grpc.ClientConn
	bc              brokerClienter
	buffer          orderBuffer // incoming orders waiting for matching
	stopCh          chan struct{}
	stopped         atomic.Bool
	consumeTag      string // unique consumer tag for reconnection
}

// NewBrokerService creates a new BrokerService with the given config.
// If dialer is nil, a realRabbitMQDialer is used.
// grpcConn is required and must be connected to the server's gRPC port.
func NewBrokerService(config brokerConfig, rabbitmqURL string, grpcConn *grpc.ClientConn, dialer RabbitMQDialer) *BrokerService {
	if dialer == nil {
		dialer = realRabbitMQDialer{}
	}
	bc := newBrokerClient(grpcConn)
	return &BrokerService{
		brokerConfig:   config,
		rabbitmqURL:    rabbitmqURL,
		rabbitmqDialer: dialer,
		grpcConn:       grpcConn,
		bc:             bc,
		stopCh:         make(chan struct{}),
		consumeTag:     "broker", // auto-generated; fine for tests since broker is short-lived
	}
}

// Start declares queues, starts the matching engine, and begins consuming buy orders.
// The goroutines run until Stop() is called or the context is cancelled.
func (s *BrokerService) Start(ctx context.Context, wg *sync.WaitGroup) {
	// Declare broker-owned queues (buy-bread-order, bread-bought).
	if err := s.declareQueues(); err != nil {
		log.Fatalf("Failed to declare broker queues: %v", err)
	}

	// Start circuit breaker state logger.
	go s.logCircuitStates(ctx)

	// Start the order-matching goroutine (buffered, batched).
	go s.startMatchingEngine(ctx)

	log.Println("Broker service started: listening for buy-bread-order messages")
}

// Stop signals all background goroutines to stop.
func (s *BrokerService) Stop() {
	if s.stopped.Swap(true) {
		return
	}
	close(s.stopCh)
}

// logCircuitStates logs all circuit breaker states at a fixed interval.
func (s *BrokerService) logCircuitStates(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			for name, cb := range s.bc.(*brokerClient).breakers() {
				if state := cb.State(); state != resilience.StateClosed {
					log.WithField("breaker", name).
						WithField("state", state).
						Warn("circuit-breaker non-closed state")
				}
			}
		}
	}
}

// declareQueues declares the RabbitMQ queues owned by the broker.
func (s *BrokerService) declareQueues() error {
	rabbitConn, err := s.rabbitmqDialer.Dial()
	if err != nil {
		return err
	}
	defer rabbitConn.Close()

	channel, err := rabbitConn.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()

	// buy-bread-order: where the server publishes incoming buy orders.
	if _, err := channel.QueueDeclare(
		"buy-bread-order", true, false, false, false, nil,
	); err != nil {
		return err
	}

	// bread-bought: where matching results are published.
	if _, err := channel.QueueDeclare(
		"bread-bought", true, false, false, false, nil,
	); err != nil {
		return err
	}

	log.Println("Broker queues declared: buy-bread-order, bread-bought")
	return nil
}

// startMatchingEngine runs the order ingestion and matching pipeline.
func (s *BrokerService) startMatchingEngine(ctx context.Context) {
	// Connect to RabbitMQ once for the matchLoop (batch timer).
	conn, err := s.rabbitmqDialer.Dial()
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ for matchLoop: %v", err)
		return
	}

	ch, err := conn.Channel()
	if err != nil {
		log.Errorf("Failed to open channel for matchLoop: %v", err)
		conn.Close()
		return
	}

	// matchLoop runs until ctx is cancelled or stopCh is closed.
	go s.matchLoop(ctx, conn, ch)

	// performBuyBread runs in a reconnect loop.
	s.runBuyBreadConsumer(ctx)
}

// matchLoop buffers incoming orders and processes them in batches.
func (s *BrokerService) matchLoop(ctx context.Context, conn *rabbitmq.Connection, ch *rabbitmq.Channel) {
	ticker := time.NewTicker(matchBatchWindow)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[broker] matchLoop: context cancelled, stopping")
			return
		case <-s.stopCh:
			log.Println("[broker] matchLoop: stop signal received, stopping")
			return
		case <-ticker.C:
			batch := s.buffer.drain()
			if len(batch) > 0 {
				log.Infof("matchLoop: batch timer fired, processing %d orders", len(batch))
				s.processMatchingBatch(batch, ch, s.bc)
			}
		default:
			if s.buffer.len() >= matchBatchSize {
				batch := s.buffer.drain()
				log.Infof("matchLoop: batch size reached (%d), processing", len(batch))
				s.processMatchingBatch(batch, ch, s.bc)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// runBuyBreadConsumer runs the buy-bread-order consumer in a reconnect loop.
func (s *BrokerService) runBuyBreadConsumer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}

		if err := s.performBuyBread(); err != nil {
			log.Errorf("[broker] performBuyBread error: %v, reconnecting in 20s", err)
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-time.After(20 * time.Second):
				continue
			}
		}

		log.Println("[broker] Disconnected from RabbitMQ, reconnecting in 20s")
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-time.After(20 * time.Second):
		}
	}
}

// performBuyBread listens for buy bread orders from RabbitMQ and buffers
// them for batch matching. Orders are persisted via the server's gRPC
// BrokerService.ReportOrder instead of direct database access.
func (s *BrokerService) performBuyBread() error {
	connection, err := s.rabbitmqDialer.Dial()
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return err
	}
	defer connection.Close()

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return err
	}
	defer channel.Close()

	// Limit prefetch to 1 — RabbitMQ delivers one message at a time.
	if err := channel.Qos(1, 0, false); err != nil {
		log.Fatalf("Failed to set QoS: %v", err)
	}

	buyOrderMessages, err := channel.Consume(
		"buy-bread-order", // queue
		s.consumeTag,       // consumer tag
		false,             // auto-ack
		false,             // exclusive
		false,             // no-local
		false,             // no-wait
		nil,               // args
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	log.Printf("[broker] Listening for buy bread orders on RabbitMQ queue...")

	for delivery := range buyOrderMessages {
		s.processOneOrder(delivery, s.bc)
	}

	return nil
}

// processOneOrder receives a buy-bread-order delivery, validates it,
// reports the order to the server via gRPC (for dedup + persistence),
// and buffers it for batch matching.
// The delivery is ACKed immediately upon successful buffering.
func (s *BrokerService) processOneOrder(delivery rabbitmq.Delivery, bc brokerClienter) {
	var order data.BuyOrder
	if err := json.Unmarshal(delivery.Body, &order); err != nil {
		log.Errorf("[broker] Failed to unmarshal buy order: %v", err)
		delivery.Ack(false) //nolint:errcheck
		return
	}

	log.WithField("order_uuid", order.BuyOrderUUID).Info("[broker] Received buy order")

	// --- Report to server (dedup + insert) ---
	protoOrder := dataToProtoBuyOrder(order)

	result, err := bc.ReportOrder(*protoOrder)
	if err != nil {
		log.Errorf("[broker] Failed to report order to server: %v", err)
		delivery.Nack(false, true) //nolint:errcheck
		return
	}

	if !result.Accepted {
		log.WithField("order_uuid", order.BuyOrderUUID).
			Warn("[broker] Duplicate order detected (server returned duplicate), skipping")
		delivery.Ack(false) //nolint:errcheck
		return
	}

	if int32(order.ID) <= 0 {
		order.ID = int(result.OrderId)
	}

	// --- Buffer for matching engine ---
	s.buffer.add(order)

	delivery.Ack(false) //nolint:errcheck
	log.WithField("order_uuid", order.BuyOrderUUID).Info("[broker] Order buffered for matching")
}

// failOrder marks an order as Failed via the server's gRPC BrokerService.
func (s *BrokerService) failOrder(uuid string, status string) {
	log.Warnf("[broker] failOrder: no longer needed — broker reports matching results via gRPC")
}

// --- Data → Proto conversion helpers ---

// dataToProtoBuyOrder converts a data.BuyOrder to a proto.BuyOrder.
func dataToProtoBuyOrder(order data.BuyOrder) *pb.BuyOrder {
	items := make([]*pb.BuyOrderItem, len(order.Breads))
	for i, bread := range order.Breads {
		items[i] = &pb.BuyOrderItem{
			BreadId:           int32(bread.ID),
			QuantityRequested: int32(bread.Quantity),
			BidPrice:          bread.Price,
			Status:            "pending",
		}
	}

	return &pb.BuyOrder{
		Id:                   int32(order.ID),
		CustomerId:           int32(order.CustomerID),
		BuyOrderUuid:         order.BuyOrderUUID,
		Status:               order.Status,
		SequenceNumber:       order.SequenceNumber,
		BidPrice:             order.BidPrice,
		AllowPartial:         order.AllowPartial,
		SkipUnavailableItems: order.SkipUnavailableItems,
		Items:                items,
	}
}

// main is the production entry point.
func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	rabbitMQAddr := os.Getenv("RABBITMQ_SERVICE_ADDR")
	serverGRPCAddr := os.Getenv("BAKERY_SERVICE_ADDR")

	log.Infof("Connecting to server gRPC at %s", serverGRPCAddr)
	grpcConn, err := grpc.Dial(serverGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to server gRPC: %v", err)
	}
	defer grpcConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := NewBrokerService(brokerConfig{}, rabbitMQAddr, grpcConn, nil)

	var wg sync.WaitGroup
	svc.Start(ctx, &wg)

	// Wait for shutdown (in production, this would be a signal handler)
	select {}
}
