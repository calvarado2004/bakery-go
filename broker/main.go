package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/calvarado2004/bakery-go/pkg/resilience"
)

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

// RabbitMQBakery is the broker service. It has NO database access —
// all data operations go through the server's gRPC BrokerService.
type RabbitMQBakery struct {
	brokerConfig
	rabbitmqURL string
	buffer      orderBuffer // incoming orders waiting for matching
}

// NewRabbitMQBakery creates a new RabbitMQBakery instance with the provided config
func NewRabbitMQBakery(config brokerConfig, rabbitmqURL string) *RabbitMQBakery {
	return &RabbitMQBakery{
		brokerConfig: config,
		rabbitmqURL:  rabbitmqURL,
	}
}

var rabbitMQAddress = os.Getenv("RABBITMQ_SERVICE_ADDR")
var serverGRPCAddr = os.Getenv("BAKERY_SERVICE_ADDR")

func main() {
	startBroker(rabbitMQAddress)
}

// startBroker initializes the broker service with the given RabbitMQ URL.
// The broker NO LONGER connects to PostgreSQL — all data operations go
// through the server's gRPC BrokerService.
//
// The broker now:
//   - Connects ONLY to RabbitMQ (consume/publish)
//   - Connects to the server's gRPC port for data operations
//   - Declares its own queues (buy-bread-order, bread-bought)
//   - Buffers orders and runs the matching engine
func startBroker(rabbitmqURL string) {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// Connect to the server's gRPC service.
	log.Infof("Connecting to server gRPC at %s", serverGRPCAddr)
	conn, err := grpc.Dial(serverGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to server gRPC: %v", err)
	}
	defer conn.Close()

	bc := newBrokerClient(conn)

	// Create a new RabbitMQBakery instance (no DB connection needed).
	rabbitMQBakery := NewRabbitMQBakery(brokerConfig{}, rabbitmqURL)

	// Start the order-matching goroutine (buffered, batched).
	go startMatchingEngine(rabbitMQBakery, bc)

	// Start the outbox publisher — REMOVED. The server now handles
	// outbox publishing via its own settlement dispatcher.
	// The broker no longer writes to or reads from the outbox.

	// Declare broker-owned queues (buy-bread-order, bread-bought).
	if err := rabbitMQBakery.declareQueues(conn); err != nil {
		log.Fatalf("Failed to declare broker queues: %v", err)
	}

	// Start circuit breaker state logger (Phase 10.10).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go logCircuitStates(ctx, bc)

	select {}
}

// logCircuitStates logs all circuit breaker states at a fixed interval.
func logCircuitStates(ctx context.Context, bc *brokerClient) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for name, cb := range bc.breakers() {
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
// In the external/internal boundary design (ARCHITECTURE_AUDIT §10.1),
// each service declares the queues it owns:
//   - Broker owns: buy-bread-order, bread-bought
//   - Server owns: bread-made (for maker confirmations)
//   - Makers own: make-bread-order
func (app *RabbitMQBakery) declareQueues(grpcConn *grpc.ClientConn) error {
	// We need a RabbitMQ connection, not the gRPC connection.
	rabbitConn, err := rabbitmq.Dial(app.rabbitmqURL)
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
		"buy-bread-order", // name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // args
	); err != nil {
		return err
	}

	// bread-bought: where matching results are published.
	if _, err := channel.QueueDeclare(
		"bread-bought", // name
		true,           // durable
		false,          // delete when unused
		false,          // exclusive
		false,          // no-wait
		nil,            // args
	); err != nil {
		return err
	}

	log.Println("Broker queues declared: buy-bread-order, bread-bought")
	return nil
}

// startMatchingEngine runs the order ingestion and matching pipeline.
// It connects to the server's gRPC service for all data operations,
// and consumes from RabbitMQ for order ingestion.
func startMatchingEngine(rabbitMQBakery *RabbitMQBakery, bc brokerClienter) {
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
	go rabbitMQBakery.matchLoop(conn, channel, bc)

	for {
		err := rabbitMQBakery.performBuyBread(bc)
		if err != nil {
			log.Errorf("Failed to perform buy bread (main), sleeping 20 seconds...: %v", err)
			time.Sleep(20 * time.Second)
			continue
		}
		log.Printf("Disconnected from RabbitMQ, reconnecting in 20 seconds...")
		time.Sleep(20 * time.Second)
	}
}

// matchLoop buffers incoming orders and processes them in batches.
// Each order is ACKed immediately upon ingestion. Matching happens
// in batches collected over matchBatchWindow or when matchBatchSize is reached.
func (app *RabbitMQBakery) matchLoop(conn *rabbitmq.Connection, channel *rabbitmq.Channel, bc brokerClienter) {
	ticker := time.NewTicker(matchBatchWindow)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			batch := app.buffer.drain()
			if len(batch) > 0 {
				log.Infof("matchLoop: batch timer fired, processing %d orders", len(batch))
				app.processMatchingBatch(batch, channel, bc)
			}
		default:
			if app.buffer.len() >= matchBatchSize {
				batch := app.buffer.drain()
				log.Infof("matchLoop: batch size reached (%d), processing", len(batch))
				app.processMatchingBatch(batch, channel, bc)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// performBuyBread listens for buy bread orders from RabbitMQ and buffers
// them for batch matching. Orders are persisted via the server's gRPC
// BrokerService.ReportOrder instead of direct database access.
func (rabbit *RabbitMQBakery) performBuyBread(bc brokerClienter) error {

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
		rabbit.processOneOrder(delivery, bc)
	}

	return nil
}

// processOneOrder receives a buy-bread-order delivery, validates it,
// reports the order to the server via gRPC (for dedup + persistence),
// and buffers it for batch matching.
// The delivery is ACKed immediately upon successful buffering.
func (rabbit *RabbitMQBakery) processOneOrder(delivery rabbitmq.Delivery, bc brokerClienter) {
	var order data.BuyOrder
	if err := json.Unmarshal(delivery.Body, &order); err != nil {
		log.Errorf("Failed to unmarshal buy order: %v", err)
		delivery.Ack(false) //nolint:errcheck
		return
	}

	log.WithField("order_uuid", order.BuyOrderUUID).Info("Received buy order")

	// --- Report to server (dedup + insert) ---
	// Convert data.BuyOrder → proto.BuyOrder for gRPC call.
	protoOrder := dataToProtoBuyOrder(order)

	result, err := bc.ReportOrder(*protoOrder)
	if err != nil {
		log.Errorf("Failed to report order to server: %v", err)
		delivery.Nack(false, true) //nolint:errcheck
		return
	}

	if !result.Accepted {
		log.WithField("order_uuid", order.BuyOrderUUID).
			Warn("Duplicate order detected (server returned duplicate), skipping")
		delivery.Ack(false) //nolint:errcheck
		return
	}

	if int32(order.ID) <= 0 {
		order.ID = int(result.OrderId)
	}

	// --- Buffer for matching engine ---
	rabbit.buffer.add(order)

	delivery.Ack(false) //nolint:errcheck
	log.WithField("order_uuid", order.BuyOrderUUID).Info("Order buffered for matching")
}

// failOrder marks an order as Failed via the server's gRPC BrokerService.
func (rabbit *RabbitMQBakery) failOrder(uuid string, status string) {
	log.Warnf("failOrder: no longer needed — broker reports matching results via gRPC")
}

// --- Data → Proto conversion helpers ---

// dataToProtoBuyOrder converts a data.BuyOrder to a proto.BuyOrder.
func dataToProtoBuyOrder(order data.BuyOrder) *pb.BuyOrder {
	items := make([]*pb.BuyOrderItem, len(order.Breads))
	for i, bread := range order.Breads {
		items[i] = &pb.BuyOrderItem{
			BreadId:           int32(bread.ID),
			QuantityRequested: int32(bread.Quantity),
			BidPrice:          order.BidPrice,
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
